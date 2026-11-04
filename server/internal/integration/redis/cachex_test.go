package redisint

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"rapid-admin/internal/pkg/cachex"
)

// cachex 的 L2 读写与 TTL 归一(真 Redis)。
//
// ⚠️ 这层以前只有 miniredis 覆盖。miniredis 与真 Redis 的差别在这几条上
// 恰好不重要,但**真 SCAN 的游标 / COUNT 语义只有真 Redis 才验得到**
// (见下面 DeletePrefix 那条)。
func TestCachex_真Redis上的读写与TTL归一(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	s := cachex.New(rdb)

	// 未命中 → (false, nil),不是错误
	var got string
	hit, err := s.Get(ctx, "k", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Fatal("不存在的 key 不该命中")
	}

	// 写 → 读
	if err := s.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	hit, err = s.Get(ctx, "k", &got)
	if err != nil || !hit || got != "v" {
		t.Fatalf("应读到 v,hit=%v got=%q err=%v", hit, got, err)
	}

	// TTL <= 0 → 永不过期(框架契约:ttl <= 0 = 不过期)
	if err := s.Set(ctx, "forever", "v", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ttl, err := rdb.TTL(ctx, "forever").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl != -1 {
		t.Fatalf("ttl<=0 的 key 不该有过期时间,got %v", ttl)
	}

	// Expire(0) → 取消过期,key 还在
	if err := s.Expire(ctx, "k", 0); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	hit, err = s.Get(ctx, "k", &got)
	if err != nil || !hit {
		t.Fatalf("Expire(0) 之后 key 必须还在,hit=%v err=%v", hit, err)
	}
	ttl, err = rdb.TTL(ctx, "k").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl != -1 {
		t.Fatalf("Expire(0) 之后不该再有 TTL,got %v", ttl)
	}
}

// 前缀失效在**真 Redis** 上跑 —— 真 SCAN 有游标、有 COUNT。
//
// ⚠️ miniredis 的 SCAN **永远一次返回全部、游标恒为 0** ⇒
// "分多批扫"这条路径在 miniredis 上根本没被走过。真 Redis 才会走。
//
// 断言三件事:
//  1. 前缀命中的全被删
//  2. **别的业务的 key 一个都不能被误删**
//  3. glob 元字符按字面量处理(前缀是字面量,不是 pattern)
func TestCachex_真Redis上的前缀失效(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	s := cachex.New(rdb)

	// 造够多的 key,让 SCAN 真的分批(COUNT=500 ⇒ 造 1200 个)
	want := 0
	for i := 0; i < 1200; i++ {
		k := fmt.Sprintf("dict:items:g%d", i)
		if err := rdb.Set(ctx, k, "v", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		want++
	}
	// 别的业务的 key —— 一个都不能被删
	others := []string{"other:key", "principal:1:x", "dictx:not-a-dict-key", "principal:12:34"}
	for _, k := range others {
		if err := rdb.Set(ctx, k, "v", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.DeletePrefix(ctx, "dict:")
	if err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	if n != int64(want) {
		t.Fatalf("应删 %d 条,got %d(少了 = 没删干净;多了 = 删到别人头上)", want, n)
	}
	for _, k := range others {
		if err := rdb.Get(ctx, k).Err(); err != nil {
			t.Fatalf("%s 被误删了: %v", k, err)
		}
	}
	// 真的一个都不剩
	left, err := rdb.Exists(ctx, "dict:items:g0").Result()
	if err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatal("前缀命中的 key 还在")
	}
}

// 前缀里的 glob 元字符必须按**字面量**处理。
//
// Redis 的 SCAN MATCH 走 glob:`*` `?` `[` `]` 都是通配符。
// 前缀 "a*b" 若不转义,pattern 变成 "a*b*" ⇒ "axb:2" 也被删。
func TestCachex_真Redis上glob元字符不误删(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	s := cachex.New(rdb)

	keys := []string{"a*b:1", "axb:2", "a?b:3", "a[b:4", "b:5", "other:6"}
	for _, k := range keys {
		if err := rdb.Set(ctx, k, "v", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.DeletePrefix(ctx, "a*b")
	if err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	if n != 1 {
		t.Fatalf("前缀 %q 只该命中 1 条,got %d", "a*b", n)
	}
	for _, k := range keys[1:] {
		if err := rdb.Get(ctx, k).Err(); err != nil {
			t.Fatalf("%s 被误删了 —— glob 元字符没转义: %v", k, err)
		}
	}
}

// 三防(null 占位 / TTL 抖动 / loader 错误不写缓存)在真 Redis 上成立。
func TestCachex_真Redis上的三防(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	s := cachex.New(rdb, cachex.WithNullTTL(time.Minute), cachex.WithJitter(0.2))

	// 1) null sentinel:loader 返 ErrNotFound → 写占位 → 第二次不回源
	calls := 0
	loader := func(ctx context.Context) (any, error) {
		calls++
		return nil, cachex.ErrNotFound
	}
	v, hit, err := s.GetOrLoad(ctx, "nf", time.Hour, loader)
	if err != nil {
		t.Fatalf("ErrNotFound 应翻译成零值+无错,got %v", err)
	}
	if v != nil || hit {
		t.Fatalf("ErrNotFound 应返零值,got %v hit=%v", v, hit)
	}
	if calls != 1 {
		t.Fatalf("loader 应被调 1 次,got %d", calls)
	}
	if _, _, err := s.GetOrLoad(ctx, "nf", time.Hour, loader); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("null 占位应挡住第二次回源,loader 被调 %d 次", calls)
	}

	// 2) TTL 抖动:写进去的 key 带 TTL 且落在 base ± 20%
	if err := s.Set(ctx, "j", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	ttl, err := rdb.TTL(ctx, "j").Result()
	if err != nil {
		t.Fatal(err)
	}
	lo := time.Duration(0.8 * float64(time.Minute))
	hi := time.Duration(1.2 * float64(time.Minute))
	if ttl < lo || ttl > hi {
		t.Fatalf("TTL 抖动后应在 [%v, %v],got %v", lo, hi, ttl)
	}

	// 3) loader 的真错误:透传 + 不写缓存
	boom := errors.New("db down")
	if _, _, err := s.GetOrLoad(ctx, "boom", time.Hour, func(ctx context.Context) (any, error) {
		return nil, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("loader 错误应透传,got %v", err)
	}
	if err := rdb.Get(ctx, "boom").Err(); err == nil {
		t.Fatal("loader 出错时不该写缓存")
	}
}

// uint64 精度:字典项 ID 超过 2^53 时,缓存往返不能掉精度。
//
// ⚠️ 这条在 miniredis 上也过,但**真 Redis 的 JSON 往返**才是生产的路径,
// 而且 miniredis 存的是同一个字符串 —— 真正会掉精度的是 Go 侧的
// `json.Marshal` / `Unmarshal` 与 `id,string` tag 的配合。
func TestCachex_真Redis上uint64精度不丢(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	s := cachex.New(rdb)

	type row struct {
		ID    uint64 `json:"id,string"`
		Label string `json:"label"`
	}
	big := uint64(96413000000000001) // > 2^53
	loader := func(ctx context.Context) ([]row, error) {
		return []row{{ID: big, Label: "已受理"}}, nil
	}

	first, _, err := cachex.GetOrLoadTypedL2(ctx, s, "dict:items:x", time.Minute, loader)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := cachex.GetOrLoadTypedL2(ctx, s, "dict:items:x", time.Minute, loader)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 {
		t.Fatalf("缓存往返后应仍有 1 条,got %d", len(second))
	}
	if second[0].ID != big {
		t.Fatalf("缓存往返不能掉 uint64 精度:got %d want %d", second[0].ID, big)
	}
	if second[0].Label != first[0].Label {
		t.Fatalf("label 不一致:%q vs %q", second[0].Label, first[0].Label)
	}
}
