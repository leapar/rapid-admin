package redisint

import (
	"testing"
	"time"

	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/auth/revocache"
)

// RedisStore 的真实往返:Revoke → SET(带 TTL)→ IsRevoked → EXISTS。
//
// ⚠️ 这条路径以前**一次都没在真 Redis 上跑过** ——
// `auth/redis_store_test.go` 只测了 nil-safe 分支与"命中本地 cache 不打 Redis"。
func TestAuth_RedisStore_吊销往返(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)

	store := auth.NewRedisStore(rdb)

	// 没吊销过 → 未吊销
	revoked, err := store.IsRevoked(ctx, "jti-not-revoked")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if revoked {
		t.Fatal("没吊销过的 jti 不该判为已吊销")
	}

	// 吊销
	if err := store.Revoke(ctx, "jti-abc", time.Hour); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revoked, err = store.IsRevoked(ctx, "jti-abc")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if !revoked {
		t.Fatal("吊销过的 jti 应判为已吊销")
	}

	// TTL 必须在(Redis 里的 key 是带 TTL 的,不能是永久的)
	ttl, err := rdb.TTL(ctx, "auth:bl:jti-abc").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > time.Hour {
		t.Fatalf("吊销 key 应带 TTL(0, 1h],got %v", ttl)
	}

	// ttl <= 0 → no-op(不写 Redis)
	if err := store.Revoke(ctx, "jti-zero", 0); err != nil {
		t.Fatalf("ttl<=0 应静默返回 nil,got %v", err)
	}
	if n, _ := rdb.Exists(ctx, "auth:bl:jti-zero").Result(); n != 0 {
		t.Fatal("ttl<=0 不该写 Redis")
	}
}

// 作废线(Lua 脚本 invalidateFromScript)的真实行为。
//
// ⚠️ **这段 Lua 从来没在真 Redis 上跑过** —— miniredis 的 Lua 是 gopher-lua,
// 语义与 Redis 自带的 Lua 有细微差别(尤其 `tonumber` 与 nil 比较)。
//
// 要守的性质:
//  1. 作废线**只前进不后退**(并发改密码时不能被更早的时间戳覆盖)
//  2. 毫秒精度够用(2^53 以内,double 不掉精度)
//  3. 从未作废 → 零值 + nil(不是错误)
func TestAuth_RedisStore_作废线只前进不后退(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	store := auth.NewRedisStore(rdb)

	// 从未作废 → 零值 + nil
	at, err := store.InvalidateBefore(ctx, 42)
	if err != nil {
		t.Fatalf("InvalidateBefore: %v", err)
	}
	if !at.IsZero() {
		t.Fatalf("从未作废应返零值,got %v", at)
	}

	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)

	if err := store.InvalidateFrom(ctx, 42, t1); err != nil {
		t.Fatalf("InvalidateFrom: %v", err)
	}
	got, err := store.InvalidateBefore(ctx, 42)
	if err != nil {
		t.Fatalf("InvalidateBefore: %v", err)
	}
	if got.UnixMilli() != t1.UnixMilli() {
		t.Fatalf("作废线应为 %d,got %d", t1.UnixMilli(), got.UnixMilli())
	}

	// 更早的时间戳**不能**把线往回拉
	if err := store.InvalidateFrom(ctx, 42, t1.Add(-time.Hour)); err != nil {
		t.Fatalf("InvalidateFrom: %v", err)
	}
	got, err = store.InvalidateBefore(ctx, 42)
	if err != nil {
		t.Fatalf("InvalidateBefore: %v", err)
	}
	if got.UnixMilli() != t1.UnixMilli() {
		t.Fatalf("作废线被拉回去了:应为 %d,got %d", t1.UnixMilli(), got.UnixMilli())
	}

	// 更晚的时间戳要能推上去
	if err := store.InvalidateFrom(ctx, 42, t2); err != nil {
		t.Fatalf("InvalidateFrom: %v", err)
	}
	got, err = store.InvalidateBefore(ctx, 42)
	if err != nil {
		t.Fatalf("InvalidateBefore: %v", err)
	}
	if got.UnixMilli() != t2.UnixMilli() {
		t.Fatalf("作废线应为 %d,got %d", t2.UnixMilli(), got.UnixMilli())
	}

	// 零值 → no-op
	if err := store.InvalidateFrom(ctx, 42, time.Time{}); err != nil {
		t.Fatalf("零值应静默返回 nil,got %v", err)
	}
}

// 跨实例广播:实例 A 吊销 ⇒ 实例 B 的本地缓存里出现这个 jti。
//
// ⚠️ revocache 的 pub/sub **从来没在真 Redis 上跑过**。
// 这条链路一旦断掉,症状是"改完密码在别的机器上不生效",而且没有任何报错。
func TestAuth_revocache_跨实例广播(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx30(t)

	// 实例 B:先订阅
	cacheB := revocache.New(rdb)
	cacheB.Start(ctx)
	t.Cleanup(cacheB.Stop)
	// 等订阅真的建立(否则 A 的广播会发在 B 订阅之前)
	time.Sleep(300 * time.Millisecond)

	// 实例 A:吊销并广播
	storeA := auth.NewRedisStore(rdb)
	cacheA := revocache.New(rdb)
	storeA.SetRevocache(cacheA)
	if err := storeA.Revoke(ctx, "jti-broadcast", time.Hour); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// B 的本地缓存应当出现这个 jti
	ok := waitFor(t, 5*time.Second, func() bool { return cacheB.IsRevoked("jti-broadcast") })
	if !ok {
		t.Fatalf("实例 B 没收到广播(本地缓存里没有 jti-broadcast)。\n" +
			"  后果:改完密码后,别的副本仍然放行旧 token —— 而且没有任何报错。\n" +
			"  这条链路以前从来没在真 Redis 上跑过。")
	}
}

// 启动期全量加载:进程起来时把 Redis 里已有的吊销记录读进本地缓存。
//
// ⚠️ 这条路径以前也没在真 Redis 上跑过。它用的是 `SCAN auth:bl:*`,
// 而作废线的 key 也在这个前缀下(`auth:bl:inv:<uid>`)——
// 必须确认它**不会**把作废线当成 jti 收进缓存,也不会因此报错。
func TestAuth_revocache_启动期全量加载(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)

	// 先造两条真实的吊销记录 + 一条作废线
	store := auth.NewRedisStore(rdb)
	if err := store.Revoke(ctx, "jti-existing-1", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, "jti-existing-2", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.InvalidateFrom(ctx, 7, time.Now()); err != nil {
		t.Fatal(err)
	}

	cache := revocache.New(rdb)
	cache.Start(ctx)
	t.Cleanup(cache.Stop)

	if !cache.IsRevoked("jti-existing-1") {
		t.Fatal("启动期应把已存在的吊销记录加载进本地缓存")
	}
	if !cache.IsRevoked("jti-existing-2") {
		t.Fatal("启动期应把已存在的吊销记录加载进本地缓存")
	}
	// 作废线不该被当成 jti 收进来(它的 TTL 是 -1,加载逻辑会跳过)
	if cache.IsRevoked("inv:7") {
		t.Fatal("作废线被当成 jti 收进了本地缓存")
	}
}
