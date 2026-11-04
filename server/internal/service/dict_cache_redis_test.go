// 字典缓存在**真 Redis** 上的端到端回归网 —— M96
//
// # 这份网补的是哪一块
//
// `role_dict_cache_test.go` 用**内存后端**验了 service 的接线
// (读走缓存 / 写完失效 / 失效早于广播)。
// 但内存后端**不是生产上跑的那条路** —— 生产是 go-redis → 真正的 SCAN /
// 真正的 TTL / 真正的错误。
//
// M96 在 cachex 层已经把这层翻译写成了单测(见
// `internal/pkg/cachex/redis_backend_test.go`),这一份是**接缝那一层**:
// service → DictCache → cachex.Safe → RedisBackend → go-redis → 真 Redis。
//
// 这条链上任何一环写错,症状都是同一个:**"改了字典不生效"** ——
// 而且不报错、不白屏,前端只是安安静静地显示旧值。
//
// ⚠️ **为什么不用 mock** —— mock 掉 redis.Client 只能验"我们调了什么",
//
//	验不了"Redis 会不会这么回"。miniredis 是进程内真 Redis:真 RESP 协议、
//	真 SCAN、真 TTL 过期。它是**测试专用**依赖,只在 `_test.go` 里 import。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cachex"
)

// newTestDictCacheRedis 装一个**真 Redis**(进程内)后面的字典缓存。
//
// ⚠️ miniredis.RunT 会自动注册 cleanup(关掉服务器),不用自己 defer。
func newTestDictCacheRedis(t *testing.T) (*DictCache, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	// 注意:用 New(rdb) 而不是 NewWithBackend —— New 走的是生产装配的那条路
	// (Safe 直接持 redis.Client → RedisBackend),装配错了这里也会红。
	return NewDictCache(cachex.New(rdb)), rdb
}

// 🔴 **读走缓存 / 写路径失效,整条链在真 Redis 上跑一遍**
//
// 这是 M95 那条承诺("改了字典马上生效")在**生产后端**上的验证。
// 内存后端那一份验的是接线,这一份验的是"接线和 Redis 那一侧合起来还成立"。
func TestM96_ListDict_CachesAndUpdateDict_Invalidates_OnRedis(t *testing.T) {
	ctx := context.Background()
	db := newDictTestDB(t)
	seedDictItem(t, db, "issue_status", "open", "待处理")

	cache, rdb := newTestDictCacheRedis(t)
	svc := NewRoleService(db, nil, cache)

	// 第一次读:回源,并在 Redis 里留下缓存
	rows, err := svc.ListDict(ctx, "issue_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "待处理", rows[0].Label)

	// ⚠️ **key 必须真的落在 Redis 里** —— 没落进去的话,后面"读命中"
	//   可能只是碰巧走了别的路径,这条测试就成了假绿。
	require.NoError(t, rdb.Get(ctx, "dict:items:issue_status").Err(),
		"字典缓存必须真的写进 Redis")

	// 直接改库(绕开 service,也就绕开了失效)→ 再读还是旧值 = 命中缓存
	require.NoError(t, db.Model(&model.SysDict{}).Where("id = ?", testDictID).
		Update("label", "库外改的").Error)
	rows, err = svc.ListDict(ctx, "issue_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "待处理", rows[0].Label, "读路径必须命中缓存(直接改库看不到变化)")

	// 走 service 改 → 失效 → 再读就是新值
	require.NoError(t, svc.UpdateDict(ctx, &Caller{UserID: 1}, testDictID, &model.SysDict{
		Label:    "已受理",
		IsActive: true,
	}))

	// ⚠️ **失效必须真的把 Redis 里的 key 删掉** —— 不只是"再读是新值"
	//   (新值也可能来自 TTL 过期后的回源,那就不是失效的功劳)。
	require.Error(t, rdb.Get(ctx, "dict:items:issue_status").Err(),
		"写路径必须把 Redis 里的字典缓存删掉")

	rows, err = svc.ListDict(ctx, "issue_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "已受理", rows[0].Label, "写路径必须让缓存失效")
}

// 🔴 **失效必须早于广播** —— 在真 Redis 上重验一次顺序。
//
// 假 publisher 在广播那一刻同步读一次字典:读到新值说明失效先于广播发生了。
// M94 让前端收到 `dict.changed` 就立刻重拉,顺序反了的话前端会拿着旧值重拉,
// 然后把旧值又写回缓存 —— "改了没生效"会顽固地活下来。
func TestM96_UpdateDict_InvalidatesBeforePublish_OnRedis(t *testing.T) {
	ctx := context.Background()
	db := newDictTestDB(t)
	seedDictItem(t, db, "issue_status", "open", "待处理")

	cache, _ := newTestDictCacheRedis(t)
	pub := &capturingPublisher{}
	svc := NewRoleService(db, pub, cache)
	pub.list = svc.ListDict

	// 先读一次,把旧值放进 Redis
	rows, err := svc.ListDict(ctx, "issue_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "待处理", rows[0].Label)

	require.NoError(t, svc.UpdateDict(ctx, &Caller{UserID: 1}, testDictID, &model.SysDict{
		Label:    "已受理",
		IsActive: true,
	}))

	require.Len(t, pub.seen, 1, "改完字典必须广播一次")
	assert.Equal(t, "已受理", pub.seen[0],
		"广播那一刻读到的必须是新值 —— 失效必须早于广播")
}

// ⚠️ **失效只删"字典"这一族** —— 别的业务的 key 一个都不能被牵连。
//
// 这一条在内存后端上验过(内存后端没有 glob,不会误伤);
// 真 Redis 上必须再验一遍,因为**它才是会 glob 的那个**。
func TestM96_DictCache_Invalidate_OnlyTouchesDictPrefix_OnRedis(t *testing.T) {
	ctx := context.Background()
	db := newDictTestDB(t)
	seedDictItem(t, db, "issue_status", "open", "待处理")

	cache, rdb := newTestDictCacheRedis(t)
	svc := NewRoleService(db, nil, cache)

	// 别的业务的 key(前缀里带 glob 元字符,是最容易误伤的那种)
	for _, k := range []string{"principal:1:x", "other:key", "cfg:app:name"} {
		require.NoError(t, rdb.Set(ctx, k, "v", time.Minute).Err())
	}

	_, err := svc.ListDict(ctx, "issue_status")
	require.NoError(t, err)

	require.NoError(t, cache.Invalidate(ctx))

	for _, k := range []string{"principal:1:x", "other:key", "cfg:app:name"} {
		require.NoError(t, rdb.Get(ctx, k).Err(),
			"%s 被误删了 —— 前缀匹配过宽", k)
	}
}

// ⚠️ **缓存条目必须带 TTL** —— 否则 Redis 里的字典缓存会永生。
//
// 字典缓存的 TTL 是"失效失败时的兜底"(见 dict_cache.go 的注释):
// 兜底不存在 = 一次失效失败就是永久的陈旧数据。
func TestM96_DictCache_Entries_HaveTTL_OnRedis(t *testing.T) {
	ctx := context.Background()
	db := newDictTestDB(t)
	seedDictItem(t, db, "issue_status", "open", "待处理")

	cache, rdb := newTestDictCacheRedis(t)
	svc := NewRoleService(db, nil, cache)

	_, err := svc.ListDict(ctx, "issue_status")
	require.NoError(t, err)

	ttl, err := rdb.TTL(ctx, "dict:items:issue_status").Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0), "字典缓存必须带 TTL(失效失败时的兜底)")
	// ⚠️ **上界要算上 TTL 抖动** —— cachex 默认 ±20%,所以最大是 1.2 倍。
	//   (写死 dictCacheTTL+1min 会在这条上翻车:实测 10min 抖成了 11m53s。)
	assert.LessOrEqual(t, ttl, dictCacheTTL*5/4, "TTL 不该超过配置值太多(抖动上限 1.2 倍)")
}
