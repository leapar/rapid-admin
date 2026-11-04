// 进程内缓存后端 —— Backend 的内存实现。
//
// # 用途
//
//  1. **测试**:三防(singleflight / null 占位 / TTL 抖动)此前**没法被真跑一遍** ——
//     cachex 的老测试只能塞一个指向 127.0.0.1:6379 的死客户端,于是每次读都走
//     "连接失败 → 降级回源"那条分支,测的是降级不是缓存。有了它,真路径可测。
//  2. **单副本部署**:不依赖 Redis 的进程内缓存。
//
// # ⚠️ 多副本部署不能拿它当 L2
//
// 每个副本各持一份,失效通知到不了别的副本 —— "改完权限立刻生效"会退化成
// "最多 TTL 后生效"。框架层原语先落地、后接线是既定顺序(见 syncx.WorkerPool
// 的先例),但**接线时必须先想清楚多副本这一条**:需要跨副本一致,就用 Redis。
package cachex

import (
	"context"
	"strings"
	"sync"
	"time"
)

// MemoryBackend 进程内缓存后端(map + TTL)。
//
// 线程安全(读写锁)。过期条目在读的时候顺手删掉,不需要后台 janitor ——
// 与 auth.MemoryStore 的取舍一致:不为一件稀疏的事开一个常驻 goroutine。
type MemoryBackend struct {
	mu      sync.RWMutex
	entries map[string]memEntry
	now     func() time.Time
}

// memEntry 内存条目
type memEntry struct {
	val string
	// expire 过期时刻;零值 = 永不过期
	expire time.Time
}

// NewMemoryBackend 构造内存后端
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		entries: make(map[string]memEntry),
		now:     time.Now,
	}
}

// Get 读 key;未命中或已过期返回 ErrMiss。
func (m *MemoryBackend) Get(_ context.Context, key string) (string, error) {
	m.mu.RLock()
	e, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok {
		return "", ErrMiss
	}
	if !e.expire.IsZero() && m.now().After(e.expire) {
		// 已过期 = 没有。顺手删掉,免得条目白占内存。
		m.mu.Lock()
		if cur, still := m.entries[key]; still && cur.expire.Equal(e.expire) {
			delete(m.entries, key)
		}
		m.mu.Unlock()
		return "", ErrMiss
	}
	return e.val, nil
}

// Set 写 key。ttl <= 0 → 永不过期。
func (m *MemoryBackend) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := memEntry{val: value}
	if ttl > 0 {
		e.expire = m.now().Add(ttl)
	}
	m.entries[key] = e
	return nil
}

// SetNX 仅当 key 不存在(或已过期)时写。
func (m *MemoryBackend) SetNX(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entries[key]; ok {
		if e.expire.IsZero() || m.now().Before(e.expire) {
			return false, nil
		}
	}
	e := memEntry{val: value}
	if ttl > 0 {
		e.expire = m.now().Add(ttl)
	}
	m.entries[key] = e
	return true, nil
}

// Del 删 key。
func (m *MemoryBackend) Del(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.entries, k)
	}
	return nil
}

// Expire 重设过期时间。key 不存在返回 nil(fail-open)。
func (m *MemoryBackend) Expire(_ context.Context, key string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok {
		return nil
	}
	if ttl > 0 {
		e.expire = m.now().Add(ttl)
	} else {
		e.expire = time.Time{}
	}
	m.entries[key] = e
	return nil
}

// Scan 按**字面前缀**扫描(实现 Scanner)。内存后端没有游标概念,一次返回全部匹配项。
//
// ⚠️ prefix 是**字面量**,不是 glob pattern(2026-10-26 M96 改)。
// 旧实现把 pattern 当成 "prefix*" 来解析(取第一个 `*` 之前当前缀),
// 于是前缀里自带 `*` 时匹配范围反而更宽 —— 与 Redis 后端各错各的。
// 现在两个后端都是"字面前缀",行为一致。
func (m *MemoryBackend) Scan(_ context.Context, _ uint64, prefix string, _ int64) ([]string, uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []string
	for k := range m.entries {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, 0, nil
}

// Ping 探活(实现 Pinger)。进程内后端永远"通"。
func (m *MemoryBackend) Ping(_ context.Context) error { return nil }

// Len 当前条目数(含已过期但还没被读掉的)。
func (m *MemoryBackend) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// TTLOf 取某个 key 的剩余寿命(诊断 / 测试用)。
// key 不存在返回 0。
func (m *MemoryBackend) TTLOf(key string) time.Duration {
	m.mu.RLock()
	e, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok || e.expire.IsZero() {
		return 0
	}
	return e.expire.Sub(m.now())
}

// 编译期断言
var (
	_ Backend = (*MemoryBackend)(nil)
	_ Scanner = (*MemoryBackend)(nil)
	_ Pinger  = (*MemoryBackend)(nil)
)
