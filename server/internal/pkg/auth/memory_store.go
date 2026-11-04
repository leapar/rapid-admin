// 进程内吊销存储 —— 没有 Redis 时的兜底,保证"吊销"这件事**永远有地方执行**。
package auth

import (
	"context"
	"sync"
	"time"
)

// MemoryStore 内存吊销存储:map[jti]过期时间 + RWMutex。
//
// # 边界(必须让运维知道,不能只写在代码里)
//
//   - **只在本实例有效**。多副本部署下,用户请求打到另一个副本,被吊销的
//     token 又活了。要多副本一致必须配 Redis(auth.revocation_mode=redis)。
//   - **重启即清空**。吊销记录全丢 —— 重启会"复活"所有本该失效的 token。
//     access token 最长 8h,所以最坏情况是"重启后 8 小时内吊销失效"。
//
// 这两条不是缺点,是"降级"本身的定义:宁可吊销能力有限,也不要没有吊销。
// 之前的问题恰恰相反 —— 配错 Redis 时吊销**完全不存在**,而且没人知道。
//
// # 内存不会无限涨
//
// 每条记录 ttl 到期即失效,写入时顺带清一遍过期项;条目数上界由
// (并发会话数 × 在吊销的 token 数)决定,被 token 剩余寿命天然限长。
// 不需要额外的 janitor goroutine —— 见 sweepLocked。
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]time.Time // jti → 过期时刻
	// invalid:userID → 作废线(见 invalidate.go)
	//
	// **不过期**:它不是缓存,是一条"这个用户的令牌在某个时刻之前一律作废"的
	// 事实。给它加 TTL 会引入一个很难发现的失效模式 —— 标记先于它要作废的
	// 令牌消失,那批令牌就整批复活。条数上界 = 改过密码的用户数,是个小数字。
	invalid map[uint64]time.Time
	// now 可注入,测试里换掉就能断言过期逻辑,不真等
	now func() time.Time
}

// NewMemoryStore 创建内存吊销存储
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[string]time.Time),
		invalid: make(map[uint64]time.Time),
		now:     time.Now,
	}
}

// Revoke 记录 jti 在 ttl 内已吊销。
//
// 并发注意:对同一个 jti 重复 Revoke 是幂等的 —— 取更晚的过期时间。
// (轮换 refresh 时新旧 JTI 不同,但重放同一个 token 会重复写同一 key。)
func (m *MemoryStore) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if jti == "" || ttl <= 0 {
		return nil
	}
	exp := m.now().Add(ttl)

	m.mu.Lock()
	defer m.mu.Unlock()
	// 已存在的记录如果过期更晚,不缩短它 —— 否则重复吊销会把 TTL 意外砍短,
	// 导致 token 在自己的有效期内"提前复活"。
	if old, ok := m.entries[jti]; !ok || exp.After(old) {
		m.entries[jti] = exp
	}
	m.sweepLocked()
	return nil
}

// IsRevoked 查询 jti 是否仍在吊销期内。
func (m *MemoryStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	m.mu.RLock()
	exp, ok := m.entries[jti]
	m.mu.RUnlock()
	if !ok {
		return false, nil
	}
	if m.now().After(exp) {
		// 已过期 = 没吊销。这里顺手删掉,避免条目留着白占内存。
		m.mu.Lock()
		if cur, still := m.entries[jti]; still && !cur.After(exp) {
			delete(m.entries, jti)
		}
		m.mu.Unlock()
		return false, nil
	}
	return true, nil
}

// Len 当前持有的吊销条目数(测试与 /healthz 用)
func (m *MemoryStore) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// InvalidateBefore 取该用户的作废线。没有记录时返回零值(配 nil error)。
func (m *MemoryStore) InvalidateBefore(ctx context.Context, userID uint64) (time.Time, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.invalid[userID], nil // 缺失时 map 返回零值 —— 正是"从未作废"的语义
}

// InvalidateFrom 把作废线推到 at。只前进不后退。
//
// 为什么要防后退:两个实例并发改密码时,后到的那个请求可能带着**更早**的
// time.Now() 到达(请求在网络上排了队)。若无条件覆盖,作废线会从 T2 退回 T1,
// 于是 [T1, T2) 之间签发的 token 全部复活 —— 一次本该更严格的收紧被放松了。
func (m *MemoryStore) InvalidateFrom(ctx context.Context, userID uint64, at time.Time) error {
	if at.IsZero() {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.invalid[userID]; !ok || at.After(cur) {
		m.invalid[userID] = at
	}
	return nil
}

// sweepLocked 清掉已过期的条目。调用方必须持有写锁。
//
// 为什么放在写路径而不是定时器:吊销写入本身很稀疏(只有登出/改密码/切换身份),
// 拿它当清理时机就够了,省掉一个后台 goroutine —— 在内存里做安全相关的
// 后台任务,收益不抵"多一个可能泄漏/卡死的 goroutine"的成本。
func (m *MemoryStore) sweepLocked() {
	now := m.now()
	for jti, exp := range m.entries {
		if now.After(exp) {
			delete(m.entries, jti)
		}
	}
}
