package redisint

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"rapid-admin/internal/pkg/kvstore"
	"rapid-admin/internal/pkg/loginlock"
)

// 🔴🔴 **Resolve 出来的 Redis 滑动窗口必须能用** —— 2026-11-01 抓到 P0。
//
// # 这个洞是怎么来的
//
// `RedisWindow` 有三个字段:
//
//	type RedisWindow struct {
//	    rdb    *redis.Client
//	    rand   func(int) string   // ← 生成 ZSET member,避免同毫秒两次命中互相覆盖
//	    prefix string             // (无)
//	}
//
// 构造器 `NewRedisWindow` 会把 `rand` 设成 `RandomID`,
// 但 `kvstore.redisStore()` **绕过了构造器**,直接写结构体字面量:
//
//	return &Store{Counter: &RedisCounter{rdb: rdb}, Window: &RedisWindow{rdb: rdb}, ...}
//	                                                              ↑ rand 是 nil
//
// 于是 `RedisWindow.Allow` 里的 `w.rand(4)` 是**空函数指针调用** ⇒ panic。
//
// # 为什么它一直没被发现
//
//  1. `RedisWindow` 的 Redis 版**从来没被任何测试跑过**(连 miniredis 都没有);
//     kvstore 的单测只跑内存版。
//  2. 内存版(`MemoryWindow`)根本没有 `rand` 字段 ⇒ 内存版永远是对的。
//  3. `kvstore.Resolve` 是**唯一**的装配入口,而 main.go 走的就是它
//     ⇒ 一旦配了 redis.addr,`kv.mode=auto` 就会拿到这个坏掉的窗口。
//  4. 后果不是"限流不准",是**每个带限流的接口直接 500**:
//     登录、发短信、刷新 token、验证码、切换身份 —— 全在限流名单上。
//     Recover 中间件把 panic 兜成 500,所以**日志里只有一条 recovered panic**,
//     看起来像"偶发"。
//
// 这条测试就是那道网:它走的**就是** `kvstore.Resolve` 那条装配路径。
func TestKVStore_Redis窗口_Resolve装配出来的不许panic(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)

	// ⚠️ 必须走 Resolve —— 直接 new 一个 RedisWindow 绕开了这次要守的洞。
	store, err := kvstore.Resolve(kvstore.ModeRedis, rdb)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if store.Backend != "redis" {
		t.Fatalf("backend 应为 redis,got %q", store.Backend)
	}

	// Allow 里用了 w.rand —— 之前这里是 nil 指针 panic。
	ok, err := store.Window.Allow(ctx, "rl:smoke:ip", 10, time.Minute)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !ok {
		t.Fatal("第一次请求应当放行")
	}
}

// RedisCounter 的往返 + TTL 语义。
//
// ⚠️ **TTL 只在计数器刚创建时设一次** —— 与内存版一致。
// 重复失败要把锁定窗口无限续长的话,那是个登录爆破的免死金牌。
func TestKVStore_RedisCounter_往返与TTL只设一次(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	c := kvstore.NewRedisCounter(rdb)

	// 不存在 → Get 返 0 且 err 为 nil("没有失败记录"是正常状态,不是错误)
	n, err := c.Get(ctx, "login:fail:1.2.3.4:alice")
	if err != nil {
		t.Fatalf("Get 不该报错,got %v", err)
	}
	if n != 0 {
		t.Fatalf("不存在的 key 应返 0,got %d", n)
	}

	// 第一次 Incr → 1,并带上 TTL
	n, err = c.Incr(ctx, "login:fail:1.2.3.4:alice", 30*time.Second)
	if err != nil {
		t.Fatalf("Incr: %v", err)
	}
	if n != 1 {
		t.Fatalf("首次 Incr 应返 1,got %d", n)
	}
	ttl, err := rdb.TTL(ctx, "login:fail:1.2.3.4:alice").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 30*time.Second {
		t.Fatalf("首次 Incr 后应有 TTL(0, 30s],got %v", ttl)
	}

	// 第二次 Incr → 2,但 **TTL 不能被推后**
	before := ttl
	time.Sleep(1100 * time.Millisecond) // 让 TTL 自然走掉一截,便于分辨
	n, err = c.Incr(ctx, "login:fail:1.2.3.4:alice", 30*time.Second)
	if err != nil {
		t.Fatalf("Incr: %v", err)
	}
	if n != 2 {
		t.Fatalf("第二次 Incr 应返 2,got %d", n)
	}
	after, err := rdb.TTL(ctx, "login:fail:1.2.3.4:alice").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if after > before {
		t.Fatalf("TTL 被推后了(计数窗口会无限续长):before=%v after=%v", before, after)
	}

	// Del → 真的删掉
	if err := c.Del(ctx, "login:fail:1.2.3.4:alice"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if err := rdb.Get(ctx, "login:fail:1.2.3.4:alice").Err(); !errors.Is(err, redis.Nil) {
		t.Fatalf("Del 之后 key 应不存在,got %v", err)
	}
}

// 🔴 **计数器的 INCR 与 EXPIRE 必须是原子的** —— 2026-11-01 抓到。
//
// # 现象
//
// `RedisCounter.Incr` 是两条命令:
//
//	n, _ := rdb.Incr(ctx, key)          // 第 1 条
//	if n == 1 { rdb.Expire(ctx, key, ttl) }  // 第 2 条
//
// 中间那个窗口里,**key 已经存在、但没有 TTL**。
// 此刻进程挂掉 / 网络断 / 被 SIGKILL ⇒ key **永久不过期** ⇒
//
//	登录失败计数永不清零 ⇒ 那个 (ip, 用户名) 被**永久锁定**。
//
// 内存版没这个洞(`MemoryCounter.Incr` 建条目时一次算好过期时刻),
// 而**只有内存版被测过** ⇒ 两版语义不一致,没人发现。
//
// # 这条测试怎么证明"没有窗口"
//
// 用一个**并发的观察者**去读同一批 key 的 TTL:
// 原子实现(Lua)里 key 从诞生起就带 TTL,观察者永远读不到 -1;
// 非原子实现里,观察者总有机会落在 INCR 与 EXPIRE 之间读到 -1。
//
// ⚠️ **必须带对照组** —— 光看"没抓到"不能证明实现是对的,
// 也可能只是探针不敏感。对照组是**故意**用两条命令建 key 的"坏实现",
// 它必须被抓到;抓不到 ⇒ 探针本身不灵 ⇒ 这条用例**拒绝下结论**(而不是
// 报"通过")。
func TestKVStore_RedisCounter_INCR与EXPIRE之间不留无TTL的窗口(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx30(t)
	c := kvstore.NewRedisCounter(rdb)

	// 探针:一个专门的连接,死盯"当前正在建的 key"的 TTL。
	type probe struct {
		mu       sync.Mutex
		current  string
		noTTL    atomic.Int64
		stop     chan struct{}
		done     sync.WaitGroup
		watchErr atomic.Value // string
	}
	newProbe := func() *probe {
		p := &probe{stop: make(chan struct{})}
		p.done.Add(1)
		go func() {
			defer p.done.Done()
			for {
				select {
				case <-p.stop:
					return
				default:
				}
				p.mu.Lock()
				k := p.current
				p.mu.Unlock()
				if k == "" {
					continue
				}
				ttl, err := rdb.TTL(ctx, k).Result()
				if err != nil {
					p.watchErr.Store(err.Error())
					return
				}
				if ttl == -1 {
					// key 在,但没有过期时间 ⇒ 落在 INCR 与 EXPIRE 之间
					p.noTTL.Add(1)
				}
			}
		}()
		return p
	}
	setCurrent := func(p *probe, k string) {
		p.mu.Lock()
		p.current = k
		p.mu.Unlock()
	}
	stop := func(p *probe) int64 {
		close(p.stop)
		p.done.Wait()
		if v := p.watchErr.Load(); v != nil {
			t.Fatalf("观察者出错:%v", v)
		}
		return p.noTTL.Load()
	}

	const rounds = 200

	// ---- 对照组:故意用两条命令(INCR 后隔一拍再 EXPIRE) ----
	ctrl := newProbe()
	for i := 0; i < rounds; i++ {
		key := fmt.Sprintf("ctrl:%d", i)
		setCurrent(ctrl, key)
		if _, err := rdb.Incr(ctx, key).Result(); err != nil {
			t.Fatalf("对照组 INCR: %v", err)
		}
		// 故意留一个窗口(比真实实现的窗口还大)
		time.Sleep(2 * time.Millisecond)
		if err := rdb.Expire(ctx, key, 30*time.Second).Err(); err != nil {
			t.Fatalf("对照组 EXPIRE: %v", err)
		}
	}
	ctrlHits := stop(ctrl)
	if ctrlHits == 0 {
		t.Skip("跳过:探针对照组都没抓到 —— 这台机器上观察者抢不到窗口," +
			"本用例**不下结论**(既不能说实现有 bug,也不能说它没问题)")
	}
	t.Logf("对照组(故意的坏实现)被抓到 %d 次 —— 探针是灵的", ctrlHits)

	// ---- 被测:框架真正的实现 ----
	real := newProbe()
	for i := 0; i < rounds; i++ {
		key := fmt.Sprintf("real:%d", i)
		setCurrent(real, key)
		if _, err := c.Incr(ctx, key, 30*time.Second); err != nil {
			t.Fatalf("Incr: %v", err)
		}
	}
	realHits := stop(real)
	if realHits > 0 {
		t.Fatalf("❌ RedisCounter.Incr 在 INCR 与 EXPIRE 之间留了窗口:"+
			"观察者抓到 %d 次「key 存在但没有 TTL」。\n"+
			"  后果:进程在这两条命令之间挂掉 ⇒ 计数 key 永不过期 ⇒ "+
			"登录失败计数永久累积 ⇒ 该 (ip, 用户名) 被永久锁定。\n"+
			"  修法:与 smslimit.windowScript 一样,用一段 Lua 把 INCR+EXPIRE 合成一步。",
			realHits)
	}
}

// RedisWindow 的滑动窗口语义(与内存版逐条对齐)。
func TestKVStore_RedisWindow_滑动窗口语义(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	w := kvstore.NewRedisWindow(rdb)
	key := "rl:login:ip"

	// 第 1..3 次放行(max=3)
	for i := 1; i <= 3; i++ {
		ok, err := w.Allow(ctx, key, 3, time.Minute)
		if err != nil {
			t.Fatalf("第 %d 次 Allow: %v", i, err)
		}
		if !ok {
			t.Fatalf("第 %d 次应放行(窗口内第 max 次放行)", i)
		}
	}
	// 第 4 次拒
	ok, err := w.Allow(ctx, key, 3, time.Minute)
	if err != nil {
		t.Fatalf("第 4 次 Allow: %v", err)
	}
	if ok {
		t.Fatal("第 4 次应被拒(本次请求计入窗口)")
	}

	// 窗口滑过去之后重新放行
	short := kvstore.NewRedisWindow(rdb)
	ok, err = short.Allow(ctx, "rl:short:ip", 1, 200*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("第一次应放行:ok=%v err=%v", ok, err)
	}
	ok, err = short.Allow(ctx, "rl:short:ip", 1, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if ok {
		t.Fatal("窗口内第二次应被拒")
	}
	time.Sleep(300 * time.Millisecond)
	ok, err = short.Allow(ctx, "rl:short:ip", 1, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !ok {
		t.Fatal("窗口滑过去之后应重新放行")
	}

	// max <= 0 → 宁可全拦也不全放(与内存版一致)
	ok, err = w.Allow(ctx, "rl:zero:ip", 0, time.Minute)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if ok {
		t.Fatal("max<=0 应全拦")
	}
}

// loginlock 走的是 kvstore 的真实 Redis 计数。
func TestLoginlock_走真Redis的失败计数与锁定(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)

	store, err := kvstore.Resolve(kvstore.ModeRedis, rdb)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// ⚠️ kvstore.SetDefault 是**全局单例**,改之前先记住原来的,用例结束还原 ——
	// 否则后面的用例会拿到这个连着真 Redis 的 store,用例之间互相污染。
	prev := kvstore.DefaultStore()
	kvstore.SetDefault(store)
	t.Cleanup(func() { kvstore.SetDefault(prev) })

	const max = 3
	user, ip := "bob", "10.0.0.9"

	// 未到阈值 → 放行
	if err := loginlock.PreCheck(ctx, user, ip, max); err != nil {
		t.Fatalf("不该锁: %v", err)
	}
	for i := 0; i < max; i++ {
		if err := loginlock.AfterFail(ctx, user, ip, time.Minute); err != nil {
			t.Fatalf("AfterFail: %v", err)
		}
	}
	// 达到阈值 → 锁
	if err := loginlock.PreCheck(ctx, user, ip, max); err == nil {
		t.Fatal("失败次数达到阈值后应锁住")
	}

	// 登录成功 → 清零 → 重新放行
	if err := loginlock.Clear(ctx, user, ip); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if err := loginlock.PreCheck(ctx, user, ip, max); err != nil {
		t.Fatalf("清零后不该锁: %v", err)
	}
}
