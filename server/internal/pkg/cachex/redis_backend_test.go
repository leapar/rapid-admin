// Redis 真路径的回归网 —— M96(2026-10-26)
//
// # 这份网为什么现在才有
//
// 框架层清单 §1.7.1 审计表里有一行一直挂着:
//
//	"Redis 后端的真连仍无单测(包内约定不依赖真 Redis),这条留给 dev 环境端到端"
//
// M90 把后端抽象成 `Backend` 接口之后,**内存后端**让三防的真路径跑起来了
// (singleflight / null 占位 / TTL 抖动第一次被断言)。
// 但**生产上跑的那条路**(go-redis → 真正的 SCAN / 真正的 TTL / 真正的错误)
// 一次都没被跑过 —— 而 M95 把字典缓存挂到了这条路上,
// 于是"Redis 那一侧写错了"这件事,现在会直接变成"改了字典不生效"。
//
// # 用 miniredis 而不是 mock
//
// mock 掉 redis.Client 只能验"我们调了什么",验不了"Redis 会不会这么回"。
// miniredis 是**进程内真 Redis**:真 RESP 协议、真 SCAN 游标语义、真 TTL 过期。
// 它是**测试专用**依赖(只在 `_test.go` 里 import),不进生产二进制。
//
// # ⚠️ miniredis 的一个已知限制
//
// **它忽略 SCAN 的 COUNT 参数,永远一次返回全部匹配的 key、游标恒为 0。**
// (见 miniredis 源码 cmd_generic.go 的 cmdScan:"We ignore COUNT, which is
// allowed according to the Redis docs.")
//
// 所以"SCAN 分多批"这条路径(miniredis 造不出来)由 `prefix_test.go` 里
// 一个**会真的返回非零游标**的假后端守着 —— 那是唯一能覆盖分页循环的办法。
package cachex

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestRedis 起一个进程内 Redis —— 真协议、真 SCAN、真 TTL。
//
// ⚠️ miniredis.RunT 会自动注册 cleanup(关掉服务器),不用自己 defer。
func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, mr
}

// 🔴 **Redis 后端的每一行都必须被真 Redis 跑过** ——
//
// 这层的职责只有一件:把 redis 专有语义翻译成 Backend 契约
// (redis.Nil → ErrMiss、SCAN 的 glob 前缀转义、TTL 的 0 / 负数)。
// 翻译错了,包内其余部分**完全看不出来** —— 它们只认 Backend 契约。
func TestM96_RedisBackend_Basics(t *testing.T) {
	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	b := NewRedisBackend(rdb)

	// 未命中 → ErrMiss(**不是** redis.Nil,也不是 ("", nil))
	if _, err := b.Get(ctx, "nope"); !errors.Is(err, ErrMiss) {
		t.Fatalf("未命中应返 ErrMiss,got %v", err)
	}

	// 写 → 读
	if err := b.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := b.Get(ctx, "k")
	if err != nil || got != "v" {
		t.Fatalf("读回 %q, err %v", got, err)
	}

	// SetNX:key 已存在 → false;不存在 → true
	ok, err := b.SetNX(ctx, "k", "v2", time.Minute)
	if err != nil || ok {
		t.Fatalf("key 已存在时 SetNX 应返 false,got %v err %v", ok, err)
	}
	ok, err = b.SetNX(ctx, "k2", "v", time.Minute)
	if err != nil || !ok {
		t.Fatalf("key 不存在时 SetNX 应返 true,got %v err %v", ok, err)
	}

	// Del
	if err := b.Del(ctx, "k", "k2"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get(ctx, "k"); !errors.Is(err, ErrMiss) {
		t.Fatalf("Del 之后应未命中,got %v", err)
	}

	// Expire:重设过期时间
	if err := b.Set(ctx, "k3", "v", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := b.Expire(ctx, "k3", time.Minute); err != nil {
		t.Fatal(err)
	}
	ttl, err := rdb.TTL(ctx, "k3").Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 0 || ttl > time.Minute {
		t.Fatalf("Expire 之后 TTL 应在 (0, 1min],got %v", ttl)
	}

	// Ping
	if err := b.Ping(ctx); err != nil {
		t.Fatalf("Ping 应成功: %v", err)
	}
}

// ⚠️ **ttl <= 0 = 不过期**,这是 Backend 契约写着的语义。
//
// Redis 的 SET 不带 EX 就是不过期 ✓,但 go-redis 有个坑:
// **`KeepTTL = -1`**,传 -1 会让 go-redis 加上 `KEEPTTL` 参数 ——
// 保留旧 TTL,而不是"不过期"。
//
// 后端契约说的是"ttl <= 0 表示不过期",那 -1 也得过期的语义。
// (现状:传 -1 会保留旧 TTL —— 键会**比预期更早消失**。)
//
// ⚠️ **"写一个新 key 再看 TTL"验不出这个 bug** —— 新 key 本来就没有 TTL,
// KEEPTTL 作用在"没有 TTL"上等于没动。所以必须**先写一个带 TTL 的 key**,
// 再用 ttl<=0 重写它,断言旧 TTL 被清掉。
func TestM96_RedisBackend_Set_NonPositiveTTL_NeverExpires(t *testing.T) {
	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	b := NewRedisBackend(rdb)

	for _, ttl := range []time.Duration{0, -1, -5 * time.Second} {
		key := fmt.Sprintf("k%d", ttl)

		// 先写一个**带 TTL** 的 key —— 后面的重写必须把 TTL 清掉
		if err := b.Set(ctx, key, "v", 10*time.Minute); err != nil {
			t.Fatal(err)
		}
		if ttlGot, err := rdb.TTL(ctx, key).Result(); err != nil || ttlGot <= 0 {
			t.Fatalf("前提:key 应带 TTL,got %v err %v", ttlGot, err)
		}

		// 再用 ttl <= 0 重写 → 必须变成"永不过期"
		if err := b.Set(ctx, key, "v2", ttl); err != nil {
			t.Fatal(err)
		}
		ttlGot, err := rdb.TTL(ctx, key).Result()
		if err != nil {
			t.Fatal(err)
		}
		// Redis:TTL 返回 -1 = 没有过期时间;-2 = key 不存在
		if ttlGot != -1 {
			t.Fatalf("ttl=%v 时 key 不该有过期时间,Redis TTL = %v", ttl, ttlGot)
		}

		// 值也要真的写进去了(不能因为归一化把写入搞丢了)
		got, err := b.Get(ctx, key)
		if err != nil || got != "v2" {
			t.Fatalf("重写后应读到 v2,got %q err %v", got, err)
		}
	}
}

// ⚠️ **Expire 的 ttl <= 0 = 取消过期**,两个后端必须一致。
//
// Redis 原生的 `EXPIRE key 0` 是**删掉这个 key**;内存后端是"取消过期"。
// 同一份 Backend 契约,两种后端两种行为 —— 调用方按哪个算都对不上。
// 框架层选了"取消过期"(与 `Set` 的 ttl<=0 语义一致,也与内存后端一致)。
func TestM96_RedisBackend_Expire_NonPositiveTTL_CancelsExpiry(t *testing.T) {
	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	b := NewRedisBackend(rdb)

	if err := b.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := b.Expire(ctx, "k", 0); err != nil {
		t.Fatal(err)
	}
	// key 必须还在(Redis 原生 EXPIRE 0 会把它删掉)
	got, err := b.Get(ctx, "k")
	if err != nil || got != "v" {
		t.Fatalf("Expire(0) 之后 key 必须还在,got %q err %v", got, err)
	}
	ttl, err := rdb.TTL(ctx, "k").Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl != -1 {
		t.Fatalf("Expire(0) 之后不该再有 TTL,got %v", ttl)
	}
}

// 🔴 **前缀失效在真 Redis 上跑一遍** —— 这是 M95 字典缓存的命脉。
//
// 断言三件事:
//  1. 前缀命中的全被删
//  2. **别的业务的 key 一个都不能被误删**(删多了和没删一样危险)
//  3. 返回的条数是真实条数(调用方靠它判断"到底删了几条")
func TestM96_Safe_DeletePrefix_OnRedis(t *testing.T) {
	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	s := New(rdb)

	for _, k := range []string{"dict:items:issue_status", "dict:items:", "dict:groups", "other:key", "principal:1:x"} {
		if err := rdb.Set(ctx, k, "v", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.DeletePrefix(ctx, "dict:")
	if err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	if n != 3 {
		t.Fatalf("应删 3 条,got %d", n)
	}
	for _, k := range []string{"dict:items:issue_status", "dict:items:", "dict:groups"} {
		if err := rdb.Get(ctx, k).Err(); err == nil {
			t.Fatalf("%s 应被删掉", k)
		}
	}
	for _, k := range []string{"other:key", "principal:1:x"} {
		if err := rdb.Get(ctx, k).Err(); err != nil {
			t.Fatalf("%s 被误删了 —— 前缀匹配过宽: %v", k, err)
		}
	}
}

// ⚠️ **SetNX 的 ttl <= 0 也必须是"不过期"** —— go-redis 的 SetNX 同样有坑。
//
// `SetNX` 在 go-redis 里是三条分支:
//   - expiration == 0       → `SETNX k v`(老命令,**不带过期**)
//   - expiration == KeepTTL → `SET k v KEEPTTL NX`(**保留旧 TTL**)
//   - 其它                   → `SET k v EX <n> NX`,负数会直接报错
//
// 不归一化的话:传 -1 → 保留旧 TTL(键比预期更早消失);
// 传 -5s → `EX -5` → Redis 报错 "invalid expire time in 'set' command"。
func TestM96_RedisBackend_SetNX_NonPositiveTTL_NeverExpires(t *testing.T) {
	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	b := NewRedisBackend(rdb)

	for _, ttl := range []time.Duration{0, -1, -5 * time.Second} {
		key := fmt.Sprintf("nx%d", ttl)

		// 先放一个**带 TTL** 的 key —— SetNX 在它存在时不该动它
		if err := b.Set(ctx, key, "old", 10*time.Minute); err != nil {
			t.Fatal(err)
		}
		ok, err := b.SetNX(ctx, key, "new", ttl)
		if err != nil {
			t.Fatalf("ttl=%v 时 SetNX 不该报错: %v", ttl, err)
		}
		if ok {
			t.Fatalf("ttl=%v: key 已存在时 SetNX 应返 false", ttl)
		}

		// 再对一个不存在的 key 用 ttl<=0 SetNX → 写进去且永不过期
		fresh := key + ":new"
		ok, err = b.SetNX(ctx, fresh, "v", ttl)
		if err != nil {
			t.Fatalf("ttl=%v 时 SetNX 不该报错: %v", ttl, err)
		}
		if !ok {
			t.Fatalf("ttl=%v: key 不存在时 SetNX 应返 true", ttl)
		}
		ttlGot, err := rdb.TTL(ctx, fresh).Result()
		if err != nil {
			t.Fatal(err)
		}
		if ttlGot != -1 {
			t.Fatalf("ttl=%v 时 key 不该有过期时间,Redis TTL = %v", ttl, ttlGot)
		}
	}
}

// 🔴🔴 **前缀里的 glob 元字符必须被当成字面量**(M96 抓到的真 bug)
//
// Redis 的 SCAN MATCH 是 **glob 语法**:`*` `?` `[...]` 都是通配符。
// 旧实现把前缀直接拼成 `前缀 + "*"` 当 pattern 用,于是:
//
//	DeletePrefix(ctx, "a*b")  →  pattern "a*b*"
//	  → Redis 把中间的 `*` 当通配符 → "axb:2" 也被删了
//
// **症状是"删多了"** —— 别的业务的缓存被连带清掉。
// 内存后端也不对:它取第一个 `*` 之前当前缀,于是 `a*b*` 变成前缀 "a",
// 匹配范围更宽。
//
// 修法:`Scanner` 的契约从"pattern"改成**字面前缀**,
// 由**会 glob 的那个后端**(Redis)自己转义。
func TestM96_Safe_DeletePrefix_GlobMeta_DoesNotOverDelete(t *testing.T) {
	ctx := context.Background()

	// 这些 key 里,只有 "a*b:1" 是前缀 "a*b" 的合法命中
	keys := []string{"a*b:1", "axb:2", "a?b:3", "a[b:4", "b:5", "other:6"}

	for _, tc := range []struct {
		name    string
		backend Backend
	}{
		{"内存后端", NewMemoryBackend()},
		{"Redis 后端", NewRedisBackend(mustRedis(t))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewWithBackend(tc.backend)
			for _, k := range keys {
				if err := tc.backend.Set(ctx, k, "v", time.Minute); err != nil {
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
			if _, err := tc.backend.Get(ctx, "a*b:1"); !errors.Is(err, ErrMiss) {
				t.Fatalf("a*b:1 应被删掉,got %v", err)
			}
			for _, k := range []string{"axb:2", "a?b:3", "a[b:4", "b:5", "other:6"} {
				if _, err := tc.backend.Get(ctx, k); err != nil {
					t.Fatalf("%s 被误删了 —— glob 元字符没转义: %v", k, err)
				}
			}
		})
	}
}

// mustRedis 起一个进程内 Redis(给表驱动用例当参数用)
func mustRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb, _ := newTestRedis(t)
	return rdb
}

// ⚠️ **三防在真 Redis 上也要成立** —— 内存后端跑绿不代表 Redis 跑绿。
//
// 断言:
//  1. null sentinel:loader 返 ErrNotFound → 写占位 → 第二次不再回源
//  2. TTL 抖动:写进去的 key 带 TTL(而且是 base ± 20%)
//  3. loader 的真错误透传,且**不写缓存**
func TestM96_Safe_ThreeDefenses_OnRedis(t *testing.T) {
	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	s := New(rdb, WithNullTTL(time.Minute), WithJitter(0.2))

	// 1) null sentinel
	calls := 0
	loader := func(ctx context.Context) (any, error) {
		calls++
		return nil, ErrNotFound
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
	// 第二次:应该命中 null 占位,不再回源
	_, hit, err = s.GetOrLoad(ctx, "nf", time.Hour, loader)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("null 占位应挡住第二次回源,loader 被调 %d 次", calls)
	}
	_ = hit

	// 2) TTL 抖动:写进去的 key 必须带 TTL,且落在 base ± 20% 内
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

// ⚠️ **uint64 精度在真 Redis 上也不能丢** ——
//
// `SysDict.ID` 是 uint64 且 json tag 是 `id,string`。
// 2^53 以上的 ID 一旦掉精度,前端拿到的就是另一个字典项。
func TestM96_GetOrLoadTypedL2_OnRedis_PreservesUint64ID(t *testing.T) {
	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	s := New(rdb)

	big := uint64(96413000000000001) // > 2^53
	require := func(cond bool, msg string, args ...any) {
		t.Helper()
		if !cond {
			t.Fatalf(msg, args...)
		}
	}
	require(big > uint64(1)<<53, "前提:ID 超过 2^53")

	type row struct {
		ID    uint64 `json:"id,string"`
		Label string `json:"label"`
	}
	loader := func(ctx context.Context) ([]row, error) {
		return []row{{ID: big, Label: "已受理"}}, nil
	}

	first, _, err := GetOrLoadTypedL2(ctx, s, "dict:items:x", time.Minute, loader)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := GetOrLoadTypedL2(ctx, s, "dict:items:x", time.Minute, loader)
	if err != nil {
		t.Fatal(err)
	}
	require(len(second) == 1, "缓存往返后应仍有 1 条,got %d", len(second))
	require(second[0].ID == big, "缓存往返不能掉 uint64 精度: got %d want %d", second[0].ID, big)
	require(second[0].Label == first[0].Label, "缓存往返后 label 应一致")
}

// ⚠️ **key 前缀(keyPrefix)与失效前缀必须对齐** ——
//
// `Safe` 的 keyPrefix 是"同 Redis 跑多业务"的隔离手段。
// 若失效时没把它算进去,就会**删不到自己的 key**(前缀失效静默不生效)。
func TestM96_Safe_KeyPrefix_AlignsWithInvalidation(t *testing.T) {
	ctx := context.Background()
	rdb, _ := newTestRedis(t)
	s := New(rdb, WithPrefix("app"))

	if err := s.Set(ctx, "dict:items:x", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	// key 必须真的带上前缀
	if err := rdb.Get(ctx, "app:dict:items:x").Err(); err != nil {
		t.Fatalf("keyPrefix 没生效: %v", err)
	}

	n, err := s.DeletePrefix(ctx, "dict:")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("带前缀时应删 1 条,got %d", n)
	}
	if err := rdb.Get(ctx, "app:dict:items:x").Err(); err == nil {
		t.Fatal("key 没被删掉 —— 失效前缀与写 key 的前缀对不齐")
	}
}
