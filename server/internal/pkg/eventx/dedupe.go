package eventx

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Deduper 消费侧幂等原语:同一 key 在 ttl 内只执行一次 fn。
//
// # 为什么需要它
//
// outbox 投递是 **at-least-once**("投出去"和"标记成功"两步之间进程死掉,
// 重启后会再投一次);多个实例同时消费也会收到同一条事件。
// 信号类事件(重拉列表)重复一次无害;有副作用的消费(计数、发通知)
// 必须自己去重 —— 本原语把"去重"从每个消费者各写一遍收口成一处。
//
// # 语义(两种实现一致)
//
//	fn 已被成功执行过(key 在 ttl 内)⇒ 返回 ErrDuplicate,fn 不执行;
//	fn 执行失败 ⇒ **不记录**,下次同 key 会再执行(重试语义);
//	fn 执行成功 ⇒ 记录 key。
//
// ⚠️ **并发窗口**:两个协程/实例同时对同一 key 调 Once,可能都通过
// "已见过?"检查 ⇒ 都执行。本原语做的是"尽力去重",不是精确一次;
// 要精确一次,业务侧还得加锁或用带唯一约束的落表。
type Deduper interface {
	// Once 同一 key 在 ttl 内只执行一次 fn。
	Once(ctx context.Context, key string, ttl time.Duration, fn func() error) error
}

// ErrDuplicate 同一 key 在 ttl 内已被成功消费过,fn 本次被跳过。
// 对消费方来说这通常是正常路径(重复投递被挡住),不是失败。
// (与 Enqueue 返回的是同一个 ErrDuplicate:发布侧的重复是"别再落一行",
// 消费侧的重复是"别再执行一次"。)

// MemoryDeduper 进程内去重(单实例 / 单测用)。
type MemoryDeduper struct {
	mu      sync.Mutex
	seen    map[string]time.Time
	maxKeys int // 防map无限涨:超限就整体清一次过期
}

// NewMemoryDeduper 构造进程内去重器。
func NewMemoryDeduper() *MemoryDeduper {
	return &MemoryDeduper{seen: make(map[string]time.Time), maxKeys: 4096}
}

// Once 实现 Deduper。
func (m *MemoryDeduper) Once(_ context.Context, key string, ttl time.Duration, fn func() error) error {
	if m.alive(key) {
		return ErrDuplicate
	}
	if err := fn(); err != nil {
		return err
	}
	m.record(key, time.Now().Add(ttl))
	return nil
}

func (m *MemoryDeduper) alive(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.seen[key]
	return ok && exp.After(time.Now())
}

func (m *MemoryDeduper) record(key string, exp time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.seen) >= m.maxKeys {
		// 简单兜底:满了就清一轮过期的。还满(全是活 key)就不再涨,
		// 新 key 记不进去 —— 代价只是漏去重,不会丢事件
		now := time.Now()
		for k, e := range m.seen {
			if e.Before(now) {
				delete(m.seen, k)
			}
		}
	}
	m.seen[key] = exp
}

// RedisDeduper 跨实例去重(Redis SETNX + TTL)。
//
// key 会加上前缀 eventx:dedupe:,与别的业务 key 隔离。
type RedisDeduper struct {
	rdb       *redis.Client
	keyPrefix string
}

// NewRedisDeduper 构造跨实例去重器。keyPrefix 为空时用默认前缀。
func NewRedisDeduper(rdb *redis.Client, keyPrefix string) *RedisDeduper {
	if keyPrefix == "" {
		keyPrefix = "eventx:dedupe:"
	}
	return &RedisDeduper{rdb: rdb, keyPrefix: keyPrefix}
}

// Once 实现 Deduper。
//
// Redis 挂了 ⇒ 去重退化成"不去重"(事件照常执行,**不会因为去重层故障
// 而丢事件**)—— 幂等是优化,不是正确性的前提。
func (r *RedisDeduper) Once(ctx context.Context, key string, ttl time.Duration, fn func() error) error {
	if r.rdb == nil {
		return fn()
	}
	if n, _ := r.rdb.Exists(ctx, r.keyPrefix+key).Result(); n > 0 {
		return ErrDuplicate
	}
	if err := fn(); err != nil {
		return err
	}
	// 记录失败只影响下一次的去重命中率,不影响本次已完成的执行
	_ = r.rdb.SetNX(ctx, r.keyPrefix+key, 1, ttl).Err()
	return nil
}
