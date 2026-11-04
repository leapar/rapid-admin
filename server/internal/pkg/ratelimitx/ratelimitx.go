// Package ratelimitx 进程内限流组件库(§1.11 #9 P2)
//
// 目的:
//   - 提供"非 Redis"、纯进程内的限流原语(令牌桶 / 漏桶 / per-key 桶)
//   - 与现有 middleware/ratelimit 互补:middleware 走 Redis ZSET(分布式),
//     本库走进程内存(单实例 + 极快)
//   - 业务可直接调,无需起 HTTP handler(API 网关、SDK 内部限流、worker 任务)
//
// 三种桶设计:
//  1. TokenBucket(令牌桶):capacity + rate,允许突发;适合"突发可放过 + 平均速率控制"
//  2. LeakyBucket(漏桶):capacity + rate,严格平滑输出;适合"削峰填谷"
//  3. KeyedBucket(per-key 多桶):LRU 内存池,适合 per-user / per-IP / per-API-key
//
// 线程安全:
//   - 单桶 (tokens, lastRefill) 由小 mutex 保护:两者必须**一起**读改写,
//     双 atomic 变量各自 CAS 组不成原子操作(见 TokenBucket 字段注释)
//   - KeyedBucket 持 LRU + sync.Mutex(锁粒度:桶元数据 + 过期清理)
//
// 与 M38 metricsx 集成(可选):
//   - ratelimitx.InitMetrics(metricsx.Registry()) 后,自动埋 4 个 collector
//   - 不调 → 内部 nil-safe,本库可独立使用
//
// 用法:
//
//	// 1. 全局限流(每用户 5 QPS,容量 10 突发)
//	bucket := ratelimitx.NewTokenBucket(10, 5)
//	for _, job := range jobs {
//	    if !bucket.Allow() { continue }
//	    go process(job)
//	}
//
//	// 2. per-key 限流(每用户独立桶,LRU 10000 桶上限)
//	pool := ratelimitx.NewKeyedBucket(5, 1, 10000, 10*time.Minute)
//	if !pool.Allow(userID) { return errors.New("rate limited") }
//
//	// 3. 漏桶(严格平滑)
//	leak := ratelimitx.NewLeakyBucket(100, 10) // 100 容量,每秒漏出 10
//	if !leak.Allow() { return errors.New("overflow") }
package ratelimitx

import (
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// TokenBucket 令牌桶
//
// 算法:
//   - 维护 tokens(当前 token 数) + lastRefill(上次补充时间)
//   - 每次 Allow:先根据 (now - lastRefill) 补 token(refill = elapsed * rate),
//     再判 tokens >= 1 → 减 1 返 true
type TokenBucket struct {
	capacity   int64
	ratePerSec int64
	// mu 保护 (tokens, lastRefill) 这对状态的读改写。曾经用两个 atomic 变量
	// 各自 CAS:refill 里"CAS tokens 成功 → Store lastRefill"不是原子的,
	// 两个并发 refill 都在对方 Store 之前 Load 到旧 lastRefill → 都按同一段
	// elapsed 补 token → 超发,限流从"硬上限"退化成"约等于"。单桶一把小
	// mutex 把两个变量绑成一个原子更新,开销可接受(审计报告 ratelimitx #1)。
	mu         sync.Mutex
	tokens     int64 // 当前 token 数(已转 int64 浮点 * 1e6 存,精度高)
	lastRefill int64 // unix nanos
	// 统计(单变量,atomic 足够,不参与上面的成对更新)
	allowed  atomic.Int64
	rejected atomic.Int64
}

// NewTokenBucket 构造令牌桶
//   - capacity: 桶容量(最大突发),< 1 视为 1
//   - ratePerSec: 每秒补充 token 数,< 1 视为 1
func NewTokenBucket(capacity, ratePerSec int) *TokenBucket {
	if capacity < 1 {
		capacity = 1
	}
	if ratePerSec < 1 {
		ratePerSec = 1
	}
	b := &TokenBucket{
		capacity:   int64(capacity),
		ratePerSec: int64(ratePerSec),
	}
	b.tokens = int64(capacity) * 1e6 // 满桶
	b.lastRefill = time.Now().UnixNano()
	return b
}

// Allow 取一个 token(非阻塞)
func (b *TokenBucket) Allow() bool {
	return b.AllowN(1)
}

// AllowN 取 n 个 token
//   - n <= 0 → 视为 1
//   - 桶不足 → 返 false(不阻塞)
func (b *TokenBucket) AllowN(n int) bool {
	if n < 1 {
		n = 1
	}
	need := int64(n) * 1e6
	b.mu.Lock()
	b.refillLocked()
	if b.tokens < need {
		b.mu.Unlock()
		b.rejected.Add(1)
		return false
	}
	b.tokens -= need
	b.mu.Unlock()
	b.allowed.Add(int64(n))
	return true
}

// refillLocked 按 (now - lastRefill) 补充 token;不消费
//   - 调用方必须已持 b.mu:(tokens, lastRefill) 的读改写整体互斥是正确性前提
//   - 仅推进 lastRefill 到"已用完 add 对应时间"位置(避免 idle 后突满抢并发者 token)
func (b *TokenBucket) refillLocked() {
	nowNs := time.Now().UnixNano()
	elapsedNs := nowNs - b.lastRefill
	if elapsedNs <= 0 {
		return
	}
	add := elapsedNs * b.ratePerSec / 1e9
	if add <= 0 {
		return
	}
	consumedNs := add * 1e9 / b.ratePerSec
	newLast := b.lastRefill + consumedNs
	if newLast > nowNs {
		newLast = nowNs
	}
	newVal := b.tokens + add*1e6
	if maxTokens := b.capacity * 1e6; newVal > maxTokens {
		newVal = maxTokens
	}
	b.tokens = newVal
	b.lastRefill = newLast
}

// Capacity 桶容量(诊断用)
func (b *TokenBucket) Capacity() int { return int(b.capacity) }

// RatePerSec 每秒速率(诊断用)
func (b *TokenBucket) RatePerSec() int { return int(b.ratePerSec) }

// Available 当前可用 token(诊断;先 refill 再读)
func (b *TokenBucket) Available() int {
	b.mu.Lock()
	b.refillLocked()
	t := b.tokens
	b.mu.Unlock()
	return int(t / 1e6)
}

// Stats 拉取统计
type BucketStats struct {
	Capacity  int   `json:"capacity"`
	Rate      int   `json:"rate_per_sec"`
	Available int   `json:"available"`
	Allowed   int64 `json:"allowed"`
	Rejected  int64 `json:"rejected"`
}

// Stats 桶状态(诊断 / 监控)
func (b *TokenBucket) Stats() BucketStats {
	return BucketStats{
		Capacity:  b.Capacity(),
		Rate:      b.RatePerSec(),
		Available: b.Available(),
		Allowed:   b.allowed.Load(),
		Rejected:  b.rejected.Load(),
	}
}

/* ====================== LeakyBucket 漏桶 ====================== */

// LeakyBucket 漏桶
//
// 算法:
//   - 维持 level(当前队列数)
//   - 每次 Allow:先按 (now - lastDrain) 漏出(减少 level),再判 level < capacity → 加 1
//   - 适合"严格平滑输出"场景
type LeakyBucket struct {
	capacity   int64
	ratePerSec int64
	// 与 TokenBucket 同一条理由:(level, lastDrain) 必须成对读改写,
	// 双 atomic 各自 CAS 组不成原子操作 → 并发 drain 超漏。
	mu         sync.Mutex
	level      int64
	lastDrain  int64 // unix nanos
	// 统计(单变量,atomic 足够)
	allowed  atomic.Int64
	rejected atomic.Int64
}

// NewLeakyBucket 构造漏桶
//   - capacity: 桶容量(队列上限)
//   - ratePerSec: 每秒漏出速率
func NewLeakyBucket(capacity, ratePerSec int) *LeakyBucket {
	if capacity < 1 {
		capacity = 1
	}
	if ratePerSec < 1 {
		ratePerSec = 1
	}
	l := &LeakyBucket{
		capacity:   int64(capacity),
		ratePerSec: int64(ratePerSec),
	}
	l.lastDrain = time.Now().UnixNano()
	return l
}

// Allow 入队(非阻塞)
//   - 队列满 → 返 false
//   - 队列未满 → 入队 + 返 true
func (l *LeakyBucket) Allow() bool {
	l.mu.Lock()
	l.drainLocked()
	if l.level >= l.capacity {
		l.mu.Unlock()
		l.rejected.Add(1)
		return false
	}
	l.level++
	l.mu.Unlock()
	l.allowed.Add(1)
	return true
}

// drainLocked 按 (now - lastDrain) 漏出
//   - 调用方必须已持 l.mu(同 TokenBucket.refillLocked 的理由)
func (l *LeakyBucket) drainLocked() {
	nowNs := time.Now().UnixNano()
	elapsedNs := nowNs - l.lastDrain
	if elapsedNs <= 0 {
		return
	}
	drain := elapsedNs * l.ratePerSec / 1e9
	if drain <= 0 {
		return
	}
	consumedNs := drain * 1e9 / l.ratePerSec
	newLast := l.lastDrain + consumedNs
	if newLast > nowNs {
		newLast = nowNs
	}
	newVal := l.level - drain
	if newVal < 0 {
		newVal = 0
	}
	l.level = newVal
	l.lastDrain = newLast
}

// Level 当前队列长度(诊断;先 drain 再读)
func (l *LeakyBucket) Level() int {
	l.mu.Lock()
	l.drainLocked()
	v := l.level
	l.mu.Unlock()
	return int(v)
}

// Capacity 桶容量
func (l *LeakyBucket) Capacity() int { return int(l.capacity) }

// LeakyStats 漏桶状态
type LeakyStats struct {
	Capacity int   `json:"capacity"`
	Rate     int   `json:"rate_per_sec"`
	Level    int   `json:"level"`
	Allowed  int64 `json:"allowed"`
	Rejected int64 `json:"rejected"`
}

// Stats 漏桶状态
func (l *LeakyBucket) Stats() LeakyStats {
	return LeakyStats{
		Capacity: l.Capacity(),
		Rate:     int(l.ratePerSec),
		Level:    l.Level(),
		Allowed:  l.allowed.Load(),
		Rejected: l.rejected.Load(),
	}
}

/* ====================== KeyedBucket per-key 桶池 ====================== */

// KeyedBucket per-key 桶池
//
// 适用场景:
//   - per-user / per-IP / per-API-key 限流
//   - 自动创建/淘汰桶(LRU + 空闲 TTL)
//
// 并发:
//   - LRU 自身线程安全(golang-lru/v2)
//   - mu 保护桶元数据(tokenWithExpiry.exp 的读写)与过期清理:
//     Get 续期、reaper 判过期+删除都走同一把锁,续期与移除互斥
type KeyedBucket struct {
	// mu 保护 exp 的读写与 Remove:Get 续期 exp 也持这把锁。曾经 reaper 在
	// 锁外 Peek 读 exp 再 Remove,与 Get 的续期构成 check-then-act 竞态 ——
	// 活跃桶刚被续期却又被过期的旧判定删掉,下一个请求未命中新建满桶,
	// 限流被整桶清零(卡点请求可周期性重置自己的桶)。顺带修掉了
	// "锁外读 exp vs 锁内写 exp" 的数据 race。
	mu       sync.Mutex
	buckets  *lru.Cache[string, *tokenWithExpiry]
	capacity int
	rate     int
	ttl      time.Duration
	stop     chan struct{}
	stopOnce sync.Once
}

type tokenWithExpiry struct {
	bucket *TokenBucket
	exp    time.Time
}

// NewKeyedBucket 构造
//   - cap: 单桶容量
//   - rate: 单桶速率
//   - maxKeys: LRU 上限(超过则淘汰最久未用)
//   - ttl: 单桶空闲 TTL(过期后被 LRU 自动淘汰)
func NewKeyedBucket(cap, rate, maxKeys int, ttl time.Duration) *KeyedBucket {
	if maxKeys < 1 {
		maxKeys = 1000
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	c, _ := lru.New[string, *tokenWithExpiry](maxKeys)
	kb := &KeyedBucket{
		buckets:  c,
		capacity: cap,
		rate:     rate,
		ttl:      ttl,
		stop:     make(chan struct{}),
	}
	// 启动空闲清理 goroutine
	go kb.reaper()
	return kb
}

// Allow 对 key 拿 1 个 token;key 不存在 → 自动建桶
func (k *KeyedBucket) Allow(key string) bool {
	b := k.Get(key)
	return b.Allow()
}

// Get 拿 key 对应的桶(不存在 → 建)
//   - 持锁(短):保证 exp 字段并发写安全 + LRU Add 串行
//   - 读多写少场景下,锁开销可接受
func (k *KeyedBucket) Get(key string) *TokenBucket {
	k.mu.Lock()
	defer k.mu.Unlock()
	if v, ok := k.buckets.Get(key); ok {
		v.exp = time.Now().Add(k.ttl) // 续期
		return v.bucket
	}
	b := NewTokenBucket(k.capacity, k.rate)
	k.buckets.Add(key, &tokenWithExpiry{bucket: b, exp: time.Now().Add(k.ttl)})
	return b
}

// Len 当前 key 数(LRU 中)
func (k *KeyedBucket) Len() int { return k.buckets.Len() }

// Stop 停掉后台 reaper
func (k *KeyedBucket) Stop() {
	k.stopOnce.Do(func() { close(k.stop) })
}

// reaper 每 1min 扫一次,清掉过期桶(LRU 也会自动淘汰,这里做主动清理)
func (k *KeyedBucket) reaper() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-k.stop:
			return
		case <-t.C:
			k.sweepExpired(time.Now())
		}
	}
}

// sweepExpired 删掉 exp 已过期的桶,reaper 每 tick 调一次。
//
// 判定与 Remove 必须整体在 k.mu 内(与 Get 的续期互斥):曾经是"锁外 Peek
// 读 exp → 判过期 → Remove"的 check-then-act 竞态 —— 活跃桶刚好在过期边界
// 被 Get 续期,却仍被这把过期的旧判定删除,下一个请求未命中、新建满桶立即
// 放行,限流被整桶清零。顺带修掉"锁外读 exp vs 锁内写 exp"的数据 race。
func (k *KeyedBucket) sweepExpired(now time.Time) {
	keys := k.buckets.Keys()
	for _, key := range keys {
		k.mu.Lock()
		if v, ok := k.buckets.Peek(key); ok && now.After(v.exp) {
			k.buckets.Remove(key)
		}
		k.mu.Unlock()
	}
}
