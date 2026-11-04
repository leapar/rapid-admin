// ratelimitx 单测(§1.11 #9 P2)
package ratelimitx

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenBucket_Allow(t *testing.T) {
	// 容量 5,速率 1/s:取 5 个快速 → 第 6 个拒绝
	b := NewTokenBucket(5, 1)
	for i := 0; i < 5; i++ {
		if !b.Allow() {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
	if b.Allow() {
		t.Fatal("6th should be rejected (capacity exhausted)")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	// 容量 10,速率 100/s:取完后等 50ms 应有 5 个左右
	b := NewTokenBucket(10, 100)
	for i := 0; i < 10; i++ {
		b.Allow()
	}
	if b.Allow() {
		t.Fatal("11th should be rejected immediately")
	}
	time.Sleep(50 * time.Millisecond)
	allowed := 0
	for i := 0; i < 20; i++ {
		if b.Allow() {
			allowed++
		}
	}
	// 50ms * 100/s = 5 个,允许 ±2 误差
	if allowed < 3 || allowed > 8 {
		t.Fatalf("refill = %d in 50ms, want ~5", allowed)
	}
}

func TestTokenBucket_AllowN(t *testing.T) {
	b := NewTokenBucket(10, 1)
	if !b.AllowN(5) {
		t.Fatal("AllowN(5) should succeed")
	}
	if !b.AllowN(5) {
		t.Fatal("AllowN(5) again should succeed (total 10)")
	}
	if b.AllowN(1) {
		t.Fatal("AllowN(1) should fail (no tokens left)")
	}
	if b.AllowN(0) {
		t.Fatal("AllowN(0) should fail (n<1)")
	}
}

func TestTokenBucket_Stats(t *testing.T) {
	b := NewTokenBucket(3, 1)
	b.Allow()
	b.Allow()
	b.Allow()
	b.Allow() // reject
	b.Allow() // reject
	stats := b.Stats()
	if stats.Capacity != 3 {
		t.Fatalf("capacity = %d, want 3", stats.Capacity)
	}
	if stats.Rate != 1 {
		t.Fatalf("rate = %d, want 1", stats.Rate)
	}
	if stats.Allowed != 3 {
		t.Fatalf("allowed = %d, want 3", stats.Allowed)
	}
	if stats.Rejected != 2 {
		t.Fatalf("rejected = %d, want 2", stats.Rejected)
	}
}

func TestTokenBucket_Concurrent_RaceFree(t *testing.T) {
	// 100 goroutine × 1 take,容量 10 → 恰好 10 成功
	b := NewTokenBucket(10, 1)
	var allowed, rejected atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.Allow() {
				allowed.Add(1)
			} else {
				rejected.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("allowed = %d, want exactly 10", allowed.Load())
	}
	if allowed.Load()+rejected.Load() != 100 {
		t.Fatalf("total = %d, want 100", allowed.Load()+rejected.Load())
	}
}

func TestLeakyBucket_Allow(t *testing.T) {
	// 容量 3,速率 1/s:入 3 个 → 第 4 个拒绝
	l := NewLeakyBucket(3, 1)
	if !l.Allow() || !l.Allow() || !l.Allow() {
		t.Fatal("first 3 should be allowed")
	}
	if l.Allow() {
		t.Fatal("4th should be rejected (overflow)")
	}
}

func TestLeakyBucket_Drain(t *testing.T) {
	// 容量 3,速率 100/s:入 3 个后等 50ms 应漏出 ~5 个 → 全空了
	l := NewLeakyBucket(3, 100)
	l.Allow()
	l.Allow()
	l.Allow()
	time.Sleep(50 * time.Millisecond)
	// 应漏 5 个,level 应 < 0?不,level ≥ 0,所以 level 一定是 0
	if l.Level() > 0 {
		t.Fatalf("after 50ms drain, level = %d, want 0", l.Level())
	}
	// 现在又可以入 3 个
	if !l.Allow() || !l.Allow() || !l.Allow() {
		t.Fatal("after drain, should accept 3 more")
	}
}

func TestLeakyBucket_Stats(t *testing.T) {
	l := NewLeakyBucket(2, 1)
	l.Allow()
	l.Allow()
	l.Allow() // reject
	stats := l.Stats()
	if stats.Capacity != 2 {
		t.Fatalf("capacity = %d", stats.Capacity)
	}
	if stats.Allowed != 2 {
		t.Fatalf("allowed = %d", stats.Allowed)
	}
	if stats.Rejected != 1 {
		t.Fatalf("rejected = %d", stats.Rejected)
	}
}

func TestKeyedBucket_PerKeyIsolation(t *testing.T) {
	// 每 key 独立桶:user1 跑满不影响 user2
	k := NewKeyedBucket(2, 1, 100, time.Minute)
	defer k.Stop()
	if !k.Allow("user1") || !k.Allow("user1") {
		t.Fatal("user1 first 2 should pass")
	}
	if k.Allow("user1") {
		t.Fatal("user1 3rd should fail")
	}
	// user2 独立
	if !k.Allow("user2") || !k.Allow("user2") {
		t.Fatal("user2 should have own bucket")
	}
	if k.Allow("user2") {
		t.Fatal("user2 3rd should fail (own bucket exhausted)")
	}
}

func TestKeyedBucket_LRUEviction(t *testing.T) {
	// LRU 上限 3:第 4 个 key 进来 → 最早的被淘汰
	k := NewKeyedBucket(5, 1, 3, time.Minute)
	defer k.Stop()
	k.Allow("a")
	k.Allow("b")
	k.Allow("c")
	if k.Len() != 3 {
		t.Fatalf("after 3 keys, len = %d, want 3", k.Len())
	}
	// 第 4 个进来 → 触发 LRU 淘汰
	k.Allow("d")
	if k.Len() > 3 {
		t.Fatalf("after 4th, len = %d, want <= 3 (LRU eviction)", k.Len())
	}
}

func TestKeyedBucket_Concurrent(t *testing.T) {
	// 100 goroutine × 10 key × 1 take,每 key 容量 2 → 每 key 恰好 2 成功
	k := NewKeyedBucket(2, 1, 100, time.Minute)
	defer k.Stop()
	allowedByKey := sync.Map{}
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		key := string(rune('a' + g))
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(kk string) {
				defer wg.Done()
				if k.Allow(kk) {
					v, _ := allowedByKey.LoadOrStore(kk, new(atomic.Int64))
					v.(*atomic.Int64).Add(1)
				}
			}(key)
		}
	}
	wg.Wait()
	// 每个 key 恰好 2 成功
	for g := 0; g < 10; g++ {
		key := string(rune('a' + g))
		v, ok := allowedByKey.Load(key)
		if !ok {
			t.Fatalf("key %s: no allowed calls", key)
		}
		if v.(*atomic.Int64).Load() != 2 {
			t.Fatalf("key %s: allowed = %d, want 2", key, v.(*atomic.Int64).Load())
		}
	}
}

func TestKeyedBucket_GetSameBucket(t *testing.T) {
	// Get(key) 返同一桶
	k := NewKeyedBucket(5, 1, 100, time.Minute)
	defer k.Stop()
	b1 := k.Get("x")
	b2 := k.Get("x")
	if b1 != b2 {
		t.Fatal("Get should return same bucket for same key")
	}
}

// TestTokenBucket_ConcurrentAllow_NoOverissue 并发 Allow 的累计放行不得超过
// "初始满桶 + 速率 × 实际耗时" 上限。曾经 refill 对 (tokens, lastRefill) 双
// atomic 各自 CAS:并发 refill 都在对方 Store 之前 Load 到旧 lastRefill →
// 都按同一段 elapsed 补 token → 超发,限流从硬上限退化成约等于
// (审计报告 ratelimitx #1)。rate 给大、上限收紧,竞态造成的翻倍放行才会
// 越过界 —— 绑实际耗时,慢机器上不会假红。
func TestTokenBucket_ConcurrentAllow_NoOverissue(t *testing.T) {
	const capacity, rate = 100000, 100000
	b := NewTokenBucket(capacity, rate)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100000; i++ {
				if b.Allow() {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	// 正确实现:每次 refill 恰好消耗 elapsed 对应的 token(lastRefill 推进
	// consumedNs ≤ elapsed),绝不重复计时;+2 容差覆盖取整损失。
	maxAllowed := int64(capacity) + int64(time.Since(start).Seconds()*rate) + 2
	if got := allowed.Load(); got > maxAllowed {
		t.Fatalf("并发 Allow 超发:%d > 上限 %d(速率 %d/s)", got, maxAllowed, rate)
	}
	if allowed.Load() == 0 {
		t.Fatal("满桶起步至少应放行一次")
	}
}

// TestLeakyBucket_ConcurrentAllow_NoOverissue 漏桶同一条不变式:并发 Allow
// 累计入队不得超过 "容量 + 速率 × 实际耗时"。曾经 drain 同样是双 atomic
// 各自 CAS,并发 drain 重复计时 → level 漏得比配置快 → 超额放行。
func TestLeakyBucket_ConcurrentAllow_NoOverissue(t *testing.T) {
	const capacity, rate = 100000, 100000
	l := NewLeakyBucket(capacity, rate)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100000; i++ {
				if l.Allow() {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	maxAllowed := int64(capacity) + int64(time.Since(start).Seconds()*rate) + 2
	if got := allowed.Load(); got > maxAllowed {
		t.Fatalf("并发 Allow 超漏:%d > 上限 %d(速率 %d/s)", got, maxAllowed, rate)
	}
	if allowed.Load() == 0 {
		t.Fatal("空桶起步至少应放行一次")
	}
}

// TestKeyedBucket_SweepExpired_MutexExcludesRenew reaper 的过期判定 + Remove
// 必须与 Get 的续期互斥(同一把 k.mu)。曾经判定在锁外:活跃桶刚被续期,
// 却仍被过期的旧判定 Remove,下一个请求未命中新建满桶 → 限流整桶清零
// (审计报告 ratelimitx #2)。
//
// 场景 1 用锁持有做确定性屏障:测试持着 k.mu,sweep 第一步就是 Lock ——
// 持锁期间 Len() 必须还是 1;修复前的锁外 Peek+Remove 会直接删掉。
func TestKeyedBucket_SweepExpired_MutexExcludesRenew(t *testing.T) {
	kb := NewKeyedBucket(1, 1, 100, time.Minute)
	defer kb.Stop()
	kb.Get("k") // 建桶

	// 场景 1:持锁期间 sweep 不得删桶
	kb.mu.Lock()
	v, ok := kb.buckets.Peek("k")
	if !ok {
		kb.mu.Unlock()
		t.Fatal("前置条件:桶应存在")
	}
	v.exp = time.Now().Add(-time.Hour) // 拨到已过期
	sweepDone := make(chan struct{})
	go func() {
		kb.sweepExpired(time.Now().Add(2 * time.Hour))
		close(sweepDone)
	}()
	if kb.Len() != 1 {
		kb.mu.Unlock()
		t.Fatal("持锁期间 sweep 不得删桶(判定/删除未与续期互斥)")
	}
	kb.mu.Unlock()
	<-sweepDone
	if kb.Len() != 0 {
		t.Fatal("锁释放后 sweep 应删掉已过期桶")
	}

	// 场景 2:刚被 Get 续期(重建)的桶不得被删 —— 判定必须读到续期后的 exp
	kb.Get("k")
	kb.sweepExpired(time.Now())
	if kb.Len() != 1 {
		t.Fatal("刚续期的活跃桶不得被 sweep 删除")
	}
}
