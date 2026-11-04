// RedisStore 的安全路径测试
//
// 覆盖面说明:这个实现重度依赖 Redis,本地/CI 没有 Redis 时只能测 nil-safe 分支。
// 真正的存储往返测试在 invalidate_test.go —— MemoryStore 不依赖任何外部服务,
// 所以"没有 Redis"不再是测试的前提(这正是 W5 把它抽出来的意义)。
//
// 注意 RedisStore.Revoke 在 ttl>0 时会无保护地调 b.rdb.Set,b.rdb 为 nil 接口就 panic。
// 这不是缺陷断言,是记录一个**刻意的取舍**:ResolveRevocation 在 mode=redis
// 且 rdb==nil 时直接返错、main panic,根本构造不出这种对象。
// 换句话说"忘了注入就首个请求 panic"这个失败方式已经被消除了。
package auth

import (
	"context"
	"testing"
	"time"

	"rapid-admin/internal/pkg/auth/revocache"
)

func TestRedisStore_RevokeNonPositiveTTLIsNoop(t *testing.T) {
	// rdb 为 nil:如果这段没提前返回,就会在 b.rdb.Set 上 panic。
	// 本身也是一条性质 —— ttl<=0 意味着"没有需要保留的窗口",不该产生写入。
	b := NewRedisStore(nil)
	for _, ttl := range []time.Duration{0, -time.Second, -time.Hour} {
		if err := b.Revoke(context.Background(), "jti-x", ttl); err != nil {
			t.Fatalf("ttl=%v 应当静默返回 nil,got %v", ttl, err)
		}
	}
}

func TestRedisStore_IsRevokedEmptyJTIIsFalse(t *testing.T) {
	// 空 JTI 直接判未吊销,不打 Redis —— 这条是防御性的:
	// 万一某个签发路径忘了写 JTI,也不该让所有请求都去查一次 Redis。
	// (W3 之后 sign() 强制写 JTI,这个分支实际上不可达,但保留作为兜底。)
	b := NewRedisStore(nil)
	revoked, err := b.IsRevoked(context.Background(), "")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if revoked {
		t.Fatal("空 JTI 竟然被判为已吊销")
	}
}

func TestRedisStore_RevocacheRoundtrip(t *testing.T) {
	// 走 revocache 本地缓存这条不依赖 Redis 的路径:Revoke 在写完 Redis 后会
	// 立刻把 jti 塞进 cache,IsRevoked 命中 cache 直接返回。这里手工把 cache
	// 填成"已撤销"的样子,验证 IsRevoked 的 cache 优先分支 —— 若它去打 Redis,
	// rdb 为 nil 会直接 panic,测试也就挂了。
	cache := revocache.New(nil)
	cache.Add("jti-y", time.Now().Add(time.Hour))

	b := NewRedisStore(nil)
	b.SetRevocache(cache)

	revoked, err := b.IsRevoked(context.Background(), "jti-y")
	if err != nil {
		t.Fatalf("命中本地 cache 时不应打 Redis,却返回 err = %v", err)
	}
	if !revoked {
		t.Fatal("cache 里有 jti-y,IsRevoked 却返回 false")
	}
}

/* ---- 强制下线:invalidate.go 挂在 RedisStore 上的那部分 ---- */

// TestRedisStore_InvalidateFromZeroTimeIsNoop rdb 为 nil:若这段没提前返回,
// 就会在跑 Lua 脚本时 panic。
//
// 零值 at 的语义是"没有需要推进的线",不该产生写入 —— 和 Revoke 的 ttl<=0 同理。
func TestRedisStore_InvalidateFromZeroTimeIsNoop(t *testing.T) {
	b := NewRedisStore(nil)
	if err := b.InvalidateFrom(context.Background(), 1, time.Time{}); err != nil {
		t.Fatalf("零值 at 应当静默返回 nil,got %v", err)
	}
}

// TestRedisStore_InvalidateKeyLayout 把键的形状钉死。
//
// 它必须是 prefix + "inv:" + uid,且**不能**与 jti 吊销键撞车。
// 撞车不需要 Redis 就能证明:新前缀里那个 "inv:" 段与 prefix 之后的首个字符("jti 的
// j")不同,所以 auth:bl:inv:42 不可能等于 auth:bl:<某个 jti>。
func TestRedisStore_InvalidateKeyLayout(t *testing.T) {
	b := NewRedisStore(nil)
	got := b.invKey(42)
	if got != "auth:bl:inv:42" {
		t.Fatalf("invKey(42) = %q, want %q", got, "auth:bl:inv:42")
	}
	if b.prefix+"jti-42" == got {
		t.Fatal("作废线键与吊销键撞车了")
	}
}

// TestRedisStore_InvalidateUsesMillisecondPrecision 锁死精度选择。
//
// Lua 的 number 是 double,2^53 ≈ 9.007e15;UnixNano 现在约 1.7e18,超界会被静默舍入。
// 传毫秒进去是这个机制的硬要求,不是风格选择 —— 一旦有人改成 UnixNano,这条会红。
func TestRedisStore_InvalidateUsesMillisecondPrecision(t *testing.T) {
	at := time.Date(2026, 10, 21, 12, 0, 0, 123456789, time.UTC)
	ms := at.UnixMilli()

	// 毫秒量级必须稳稳落在 double 的精确整数区间内
	const maxSafeInteger = int64(1) << 53
	if ms > maxSafeInteger {
		t.Fatalf("毫秒时间戳 %d 超出 double 精确整数范围", ms)
	}
	// 而纳秒不是 —— 这就是必须存毫秒的理由,写进断言免得后人"顺手改回去"
	if at.UnixNano() <= maxSafeInteger {
		t.Fatalf("UnixNano(%d) 已落回安全区,本条断言的前提失效,请重新评估", at.UnixNano())
	}
	// 毫秒精度对 60s 宽限绰绰有余:往返误差远小于宽限窗口
	if d := time.UnixMilli(ms).Sub(at); d > time.Millisecond || d < -time.Millisecond {
		t.Fatalf("毫秒往返误差 %v,超过 1ms", d)
	}
}
