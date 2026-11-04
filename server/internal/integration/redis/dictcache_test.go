package redisint

import (
	"context"
	"testing"
	"time"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cachex"
	"rapid-admin/internal/seed"
	"rapid-admin/internal/service"
)

// 字典缓存(service.DictCache)在真 Redis 上的"写 → 失效 → 广播"里的中间那一步。
//
// ⚠️ 这条路径以前**只在 miniredis 上跑过**(service 包的单测),
// 而它的失效手段是 `cachex.DeletePrefix` —— 里面是 `SCAN ... MATCH`。
//
// miniredis 的 SCAN **永远一次返回全部、游标恒为 0** ⇒
// "分多批扫"这条路径在 miniredis 上根本没被走过;
// 真 Redis 才会走(COUNT 有上限、游标会回绕)。
//
// 要守的性质:
//  1. 读一次之后命中缓存(loader 不再被调)
//  2. Invalidate 之后回到源(loader 再被调)
//  3. Invalidate **只删 `dict:` 这一族**,别的业务的 key 一个都不能碰
func TestDictCache_真Redis上的读与失效(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)

	dc := service.NewDictCache(cachex.New(rdb))

	calls := 0
	loader := func(ctx context.Context) ([]model.SysDict, error) {
		calls++
		return []model.SysDict{{GroupCode: "g1", ItemKey: "k1", ItemValue: "v1", Label: "已受理"}}, nil
	}

	// 第一次:回源
	items, err := dc.Items(ctx, "g1", loader)
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) != 1 || items[0].Label != "已受理" {
		t.Fatalf("字典项不对:%+v", items)
	}
	if calls != 1 {
		t.Fatalf("loader 应被调 1 次,got %d", calls)
	}

	// 第二次:命中缓存
	if _, err := dc.Items(ctx, "g1", loader); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if calls != 1 {
		t.Fatalf("命中缓存时 loader 不该被调,got %d 次", calls)
	}

	// 分组列表也缓存一份
	groupCalls := 0
	groupLoader := func(ctx context.Context) ([]seed.DictGroupKV, error) {
		groupCalls++
		return []seed.DictGroupKV{{Code: "g1", Label: "受理状态"}}, nil
	}
	if _, err := dc.Groups(ctx, groupLoader); err != nil {
		t.Fatalf("Groups: %v", err)
	}
	if _, err := dc.Groups(ctx, groupLoader); err != nil {
		t.Fatalf("Groups: %v", err)
	}
	if groupCalls != 1 {
		t.Fatalf("分组列表应命中缓存,loader 却被调 %d 次", groupCalls)
	}

	// ⚠️ 别的业务的 key —— 失效时一个都不能被删
	other := "principal:1:roles"
	if err := rdb.Set(ctx, other, "v", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	// 失效
	if err := dc.Invalidate(ctx); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	// 缓存里的字典 key 必须没了
	if n, err := rdb.Exists(ctx, "dict:items:g1").Result(); err != nil || n != 0 {
		t.Fatalf("失效后 dict:items:g1 应当不存在,got n=%d err=%v", n, err)
	}
	if n, err := rdb.Exists(ctx, "dict:groups").Result(); err != nil || n != 0 {
		t.Fatalf("失效后 dict:groups 应当不存在,got n=%d err=%v", n, err)
	}
	// 别的业务的 key 必须还在
	if n, err := rdb.Exists(ctx, other).Result(); err != nil || n != 1 {
		t.Fatalf("失效把别的业务的 key 删了:%s(n=%d err=%v)", other, n, err)
	}

	// 失效后回到源
	if _, err := dc.Items(ctx, "g1", loader); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if calls != 2 {
		t.Fatalf("失效后应回源,loader 被调 %d 次", calls)
	}
}

// 超长 group_code 不建缓存条目。
//
// ⚠️ `sys_dict.group_code` 列宽是 64 —— 超过这个长度必然查不到任何东西。
// 直接回源,一次查空,完事。
func TestDictCache_超长groupCode不建缓存(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)

	dc := service.NewDictCache(cachex.New(rdb))

	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	calls := 0
	loader := func(ctx context.Context) ([]model.SysDict, error) {
		calls++
		return nil, nil
	}
	for i := 0; i < 2; i++ {
		if _, err := dc.Items(ctx, string(long), loader); err != nil {
			t.Fatalf("Items: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("超长 group_code 每次都该回源,loader 被调 %d 次", calls)
	}
	// 也不该在 Redis 里留下 key
	keys, err := rdb.Keys(ctx, "dict:items:"+string(long)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("超长 group_code 不该建缓存条目,却留下了 %v", keys)
	}
}
