// Redis 实现的**装配契约**白盒测试。
//
// ⚠️ 为什么这些断言要放在包内(而不是 integration/redis 那套黑盒网):
//
//	2026-11-01 抓到的 P0 是「redisStore() 绕过了 NewRedisWindow,
//	RedisWindow.rand 是 nil ⇒ Allow 里 w.rand(4) 空函数指针调用 ⇒ panic」。
//	修法有两层:① 装配改走构造器;② Allow 内部对 rand 为 nil 兜底。
//
//	**第②层会让黑盒测试看不出第①层退化** —— 只要兜底在,黑盒的
//	"不许 panic" 就一直绿。所以这里用白盒把第①层单独钉住:
//	rand 字段必须是**非 nil 的函数**。
//
//	这条断言不是"实现细节洁癖":它是那个 P0 的**根因**。
//	没有它,任何人再写一次结构体字面量都不会有人发现。
package kvstore

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisStore 装配出来的窗口必须带 rand —— 这是 2026-11-01 那个 P0 的根因。
func TestRedisStore_窗口必须走构造器(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}) // 不连,只是造个非 nil client
	t.Cleanup(func() { _ = rdb.Close() })

	store := redisStore(rdb)
	if store.Backend != "redis" {
		t.Fatalf("backend 应为 redis,got %q", store.Backend)
	}

	w, ok := store.Window.(*RedisWindow)
	if !ok {
		t.Fatalf("Window 应是 *RedisWindow,got %T", store.Window)
	}
	if w.rand == nil {
		t.Fatal("❌ RedisWindow.rand 是 nil —— 装配绕过了 NewRedisWindow。\n" +
			"  后果:RedisWindow.Allow 里的 rand(4) 是空函数指针调用 ⇒ panic\n" +
			"  ⇒ 一旦配了 redis.addr,**每一个带限流的接口直接 500**\n" +
			"  (登录 / 发短信 / 刷新 token / 验证码 / 切换身份 全在限流名单上)")
	}
	// 顺带把"rand 真的能用"也钉住
	if s := w.rand(4); s == "" {
		t.Fatal("rand 应当返回非空串")
	}

	if _, ok := store.Counter.(*RedisCounter); !ok {
		t.Fatalf("Counter 应是 *RedisCounter,got %T", store.Counter)
	}
}

// Resolve 走 redis 分支时装配出来的东西必须一样完整。
func TestResolve_redis模式装配完整(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })

	for _, mode := range []string{ModeRedis, ModeAuto} {
		store, err := Resolve(mode, rdb)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		w, ok := store.Window.(*RedisWindow)
		if !ok {
			t.Fatalf("%s: Window 应是 *RedisWindow,got %T", mode, store.Window)
		}
		if w.rand == nil {
			t.Fatalf("%s: RedisWindow.rand 是 nil", mode)
		}
	}
}

// 第二道防线:即便有人又用结构体字面量造了窗口,Allow 也不能 panic。
//
// ⚠️ 这条是**兜底**的兜底 —— 它的存在意义是"根因被改回来时,线上不会全线 500"。
func TestRedisWindow_rand为nil时不panic(t *testing.T) {
	// 连一个没人听的端口:Allow 会走完整个流程再在网络层失败,
	// 我们要断言的是"它没在 rand 那一行就炸了"。
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 200 * time.Millisecond,
		ReadTimeout: 200 * time.Millisecond,
	})
	t.Cleanup(func() { _ = rdb.Close() })

	w := &RedisWindow{rdb: rdb} // 故意不走构造器:rand 为 nil
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("❌ rand 为 nil 时 Allow panic 了:%v", rec)
		}
	}()

	// 连的是死地址,Allow 会返回 error 而不是 panic —— 断言的是"不 panic"
	_, err := w.Allow(context.Background(), "k", 10, time.Minute)
	if err == nil {
		t.Log("Allow 返回 nil(Redis 可达),不 panic —— 符合预期")
	}
}
