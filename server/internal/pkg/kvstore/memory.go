// 内存实现 —— 没有 Redis 时的兜底,保证"限流"这件事**永远有地方执行**。
package kvstore

import (
	"context"
	"sync"
	"time"
)

// # 边界(必须让运维知道)
//
//   - **只在本实例有效**。多副本下每个副本各数各的,实际阈值 = 配置值 × 副本数。
//   - **重启即清空**。所有计数和窗口归零,已累计的登录失败次数没了。
//
// 内存窗口不是定时归零的"固定窗口",是**真滑动窗口**:逐条记录命中时刻,
// 判定时先剔掉落在窗口外的。粗心的实现会用固定桶(比如每分钟一个计数),
// 那在桶边界能放过 2×max 次请求 —— 限流一旦能绕过去,不如没有。

// MemoryCounter 内存计数器:map[key]{值, 过期时刻}。
type MemoryCounter struct {
	mu      sync.RWMutex
	entries map[string]memEntry
	now     func() time.Time
}

type memEntry struct {
	val int64
	exp time.Time
}

// NewMemoryCounter 创建内存计数器
func NewMemoryCounter() *MemoryCounter {
	return &MemoryCounter{entries: make(map[string]memEntry), now: time.Now}
}

// Incr 加一并返回加完后的值。
func (m *MemoryCounter) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if key == "" {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	e, ok := m.entries[key]
	if !ok || now.After(e.exp) { // 过期 = 从 1 起算
		e = memEntry{val: 0, exp: now.Add(ttl)}
	}
	e.val++
	m.entries[key] = e
	m.sweepLocked(now)
	return e.val, nil
}

// Get 读当前值,不存在或过期返回 0。
func (m *MemoryCounter) Get(ctx context.Context, key string) (int64, error) {
	m.mu.RLock()
	e, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok || m.now().After(e.exp) {
		return 0, nil
	}
	return e.val, nil
}

// Del 删 key。传 0 个 key 是合法 no-op(调用方可能算出空列表)。
func (m *MemoryCounter) Del(ctx context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.entries, k)
	}
	return nil
}

// Len 当前条目数(测试 / /healthz)
func (m *MemoryCounter) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

func (m *MemoryCounter) sweepLocked(now time.Time) {
	for k, e := range m.entries {
		if now.After(e.exp) {
			delete(m.entries, k)
		}
	}
}

// MemoryWindow 内存滑动窗口:每个 key 一串命中时刻(ms)。
type MemoryWindow struct {
	mu      sync.Mutex
	entries map[string]*memWindowEntry
	now     func() time.Time
}

// memWindowEntry 单个 key 的窗口记录。
//
// ⚠️ windowMs 必须存在 entry 上,不能用"本次请求的窗口"去套所有 key:
// sweep 判"全部落窗外"时,短窗口 key 的 min 会把长窗口 key 窗口内的
// 记录整串删光,长窗口限流就退化成短窗口了(2026-11-03 审计 P1)。
type memWindowEntry struct {
	hits     []int64
	windowMs int64 // 该 key 见过的最大窗口长度(取 max,保守)
}

// NewMemoryWindow 创建内存滑动窗口
func NewMemoryWindow() *MemoryWindow {
	return &MemoryWindow{entries: make(map[string]*memWindowEntry), now: time.Now}
}

// Allow 记录一次命中,窗口内计数 > max 则拒。
//
// 与 Redis ZSET 版语义**逐条对齐**:
//   - 本次请求计入窗口
//   - 第 max 次放行、第 max+1 次拦
//   - 只剔除**严格早于** now-window 的时刻(和 ZRemRangeByScore 一样含边界)
func (m *MemoryWindow) Allow(ctx context.Context, key string, max int, window time.Duration) (bool, error) {
	if key == "" || max <= 0 {
		// max<=0 不是"全拦":Redis 版在 max<=0 时同样只会拦掉一切。
		// 这里选择与 Redis 版一致(拒),避免两套后端行为分叉 ——
		// 限流配置写错(比如 max 传了 0)时,**宁可全拦也不要全放**。
		return false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UnixMilli()
	min := now - window.Milliseconds()

	e := m.entries[key]
	if e == nil {
		e = &memWindowEntry{windowMs: window.Milliseconds()}
	}
	// 该 key 记下见过的最大窗口(取 max 保守):sweep 判"全部落窗外"要用它。
	if window.Milliseconds() > e.windowMs {
		e.windowMs = window.Milliseconds()
	}

	// 剔除窗口外的。
	//
	// 判据是 ts > min 而**不是** ts >= min:Redis 版走 ZRemRangeByScore(key,"0",min),
	// 区间**含**上界 min,所以恰好落在 min 上的记录是被剔掉的。
	// 这里写 >= 的话,内存版会比 Redis 版多留一条边界记录,
	// 表现为"同一时刻两套后端给的限流结果不一样",而且极难在真机上察觉。
	kept := e.hits[:0:0] // 新切片,不污染可能被并发读的那一份
	for _, ts := range e.hits {
		if ts > min {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, now)
	e.hits = kept
	m.entries[key] = e
	m.sweepLocked(now)

	return len(kept) <= max, nil
}

// Len 当前持有的窗口数(测试 / /healthz)
func (m *MemoryWindow) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// sweepLocked 删掉"已经放空"的窗口。
//
// 判据是**全部记录都落在窗口之外**(`min` 之外),也就是这个 key 现在
// 放行与否与残留记录无关 —— 新请求只会重新开一个窗口,不会跟它们相加。
// 这样才敢删;提前删等于放行,所以判据必须是"全部",不能是"最新一条"。
//
// ⚠️ 每个 key 用**它自己的** windowMs 算 min(见 memWindowEntry 的注释):
// 原实现把 Allow 本次请求的 min 套到所有 key 上,key A(1min)的 Allow
// 会把 key B(1h)窗口内的记录整串删光,B 的长窗口限流就退化了。
// 取"见过的最大窗口"是保守方向 —— 只会多留记录(多留 = 判得更严,不会放水)。
func (m *MemoryWindow) sweepLocked(now int64) {
	for k, e := range m.entries {
		if len(e.hits) == 0 {
			delete(m.entries, k)
			continue
		}
		min := now - e.windowMs
		fresh := false
		for _, t := range e.hits {
			if t > min {
				fresh = true
				break
			}
		}
		if !fresh {
			delete(m.entries, k)
		}
	}
}