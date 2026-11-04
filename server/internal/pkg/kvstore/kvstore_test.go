// kvstore 的行为契约 —— 见 kvstore.go 的注释。
//
// 这些测试的价值全在**两个后端给同一个答案**。单测某个实现
// 只能锁住它自己;锁住"内存版与 Redis 版语义一致"才拦得住
// "切后端 = 换一套限流行为"这类事故。
package kvstore

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock 可注入的时钟。滑动窗口和计数器都靠时间判定过期,
// 真等会拖慢测试,而且等不出来"边界"这种最容易写错的分支。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newFakeCounter(t *testing.T) *MemoryCounter {
	t.Helper()
	c := NewMemoryCounter()
	c.now = newFakeClock().now
	return c
}

func newFakeWindow(t *testing.T) *MemoryWindow {
	t.Helper()
	w := NewMemoryWindow()
	w.now = newFakeClock().now
	return w
}

// ===== Counter =====

func TestMemoryCounter_IncrCountsAndFirstSetTTL(t *testing.T) {
	ctx := context.Background()
	c := newFakeCounter(t)

	// 连续加 3 次 → 返回值是"加完后"的值,不是加之前的
	for i := int64(1); i <= 3; i++ {
		n, err := c.Incr(ctx, "k", time.Minute)
		require.NoError(t, err)
		assert.Equal(t, i, n, "Incr 应返回加完后的值(登录失败计数要靠它判阈值)")
	}
	got, err := c.Get(ctx, "k")
	require.NoError(t, err)
	assert.EqualValues(t, 3, got)
}

// TestMemoryCounter_TTLNotExtendedOnRepeat 是**防免死金牌**的一条:
// 重复失败不能把锁定窗口无限续长,否则暴力破解永远撞不开锁。
func TestMemoryCounter_TTLNotExtendedOnRepeat(t *testing.T) {
	ctx := context.Background()
	clk := newFakeClock()
	c := NewMemoryCounter()
	c.now = clk.now

	_, err := c.Incr(ctx, "k", 10*time.Minute)
	require.NoError(t, err)

	clk.add(9 * time.Minute)
	_, err = c.Incr(ctx, "k", 10*time.Minute) // 第二次,ttl 不该重置
	require.NoError(t, err)

	clk.add(2 * time.Minute) // 距首次已 11min > 10min
	n, err := c.Get(ctx, "k")
	require.NoError(t, err)
	assert.EqualValues(t, 0, n,
		"第二次 Incr 把 TTL 续长了 —— 反复失败就能无限延长锁定窗口")

	// 再加一次应该从 1 重新起算
	n, err = c.Incr(ctx, "k", 10*time.Minute)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "过期后应从 1 重新起算,不是接着旧值")
}

func TestMemoryCounter_DelAndMissing(t *testing.T) {
	ctx := context.Background()
	c := newFakeCounter(t)

	_, err := c.Incr(ctx, "a", time.Minute)
	require.NoError(t, err)

	n, err := c.Get(ctx, "never-created")
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "不存在的 key 必须返回 0 而不是报错 —— "+
		"调用方历史上把 err 当'没失败过',让 err 非 nil 会改变登录行为")

	require.NoError(t, c.Del(ctx, "a"))
	n, err = c.Get(ctx, "a")
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "登录成功 Clear 后计数必须归零")

	// 传 0 个 key 是合法 no-op(调用方可能算出空列表)
	require.NoError(t, c.Del(ctx))
	// 传空 key 名也是 no-op,不能 panic
	require.NoError(t, c.Del(ctx, ""))
	require.NoError(t, func() error { _, e := c.Incr(ctx, "", time.Minute); return e }())
}

// TestMemoryCounter_SweepsExpired 控制内存不涨:过期条目不该一直挂着。
func TestMemoryCounter_SweepsExpired(t *testing.T) {
	ctx := context.Background()
	clk := newFakeClock()
	c := NewMemoryCounter()
	c.now = clk.now

	for i := 0; i < 5; i++ {
		_, err := c.Incr(ctx, string(rune('a'+i)), time.Minute)
		require.NoError(t, err)
	}
	assert.Equal(t, 5, c.Len())

	clk.add(2 * time.Minute)
	_, err := c.Incr(ctx, "z", time.Minute) // 一次写入顺带清过期
	require.NoError(t, err)
	assert.Equal(t, 1, c.Len(), "过期的 5 条没被清掉 —— 长期跑会把内存吃光")
}

// ===== Window =====

// TestMemoryWindow_SlidingNotFixed 是本文件**最关键**的一条。
//
// 固定窗口(每 60s 一个桶,或者"距上次重置满 60s 就清零")最经典的破绽是
// **跨桶瞬间双倍放行**:59.9s 发第 1 次(桶 0)、60.1s 发第 2 次(桶 1),
// 两边各自只数到 1 —— max=1 的限流被"200ms 内放 2 次"直接绕过。
//
// 所以这条只发两次请求,时刻特意骑在 60s 边界上。滑动窗口必须拒第 2 次
// (59.9s 那次还在尾窗里),固定窗口会放它。
//
// ⚠️ 别把这条改写成"往前走超过窗口长度就恢复放行"那种形状 ——
// 固定窗口同样能过,那样的断言测不出任何东西。
func TestMemoryWindow_SlidingNotFixed(t *testing.T) {
	ctx := context.Background()
	clk := newFakeClock()
	w := NewMemoryWindow()
	w.now = clk.now

	clk.add(59*time.Second + 900*time.Millisecond) // t = 59.9s,桶 0
	ok, err := w.Allow(ctx, "ip", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, ok, "窗口内第一次应放行")

	clk.add(200 * time.Millisecond) // t = 60.1s,刚跨过桶边界
	ok, err = w.Allow(ctx, "ip", 1, time.Minute)
	require.NoError(t, err)
	assert.False(t, ok,
		"跨桶瞬间被放行了 —— 这是固定窗口/重置式计数器,限流可被 200ms 内 2×max 次绕过")

	// 真正的滑动:等尾窗把 59.9s 那次挤出去才恢复
	clk.add(time.Minute)
	ok, err = w.Allow(ctx, "ip", 1, time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "旧记录出窗后应恢复放行")
}

func TestMemoryWindow_BoundaryMatchesZRemRangeByScore(t *testing.T) {
	ctx := context.Background()
	clk := newFakeClock()
	w := NewMemoryWindow()
	w.now = clk.now

	ok, err := w.Allow(ctx, "ip", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// 恰好卡在窗口边界上。Redis 版是 ZRemRangeByScore(key, "0", min),
	// 区间**含**上界 min,所以边界上那条会被剔掉;内存版必须一模一样。
	// (判据写成 ts >= min 就会多留一条,两套后端在边界上给出不同答案。)
	clk.add(time.Minute)
	ok, err = w.Allow(ctx, "ip", 1, time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "边界上的记录应被剔掉(与 ZRemRangeByScore 含边界一致)")
}

func TestMemoryWindow_KeysAreIndependent(t *testing.T) {
	ctx := context.Background()
	w := NewMemoryWindow()

	ok, err := w.Allow(ctx, "ipA", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = w.Allow(ctx, "ipA", 1, time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = w.Allow(ctx, "ipB", 1, time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "A 被限了不能连带限掉 B —— 否则一个 IP 打满全站被锁")
}

// TestMemoryWindow_RejectsNonPositiveMax 覆盖配置写错的情形。
// 两个后端在 max<=0 时都选择**全拒**:限流配置错了宁可全拦不可全放,
// 反向的默认值会静默把限流关掉。
func TestMemoryWindow_RejectsNonPositiveMax(t *testing.T) {
	ctx := context.Background()
	w := NewMemoryWindow()
	for _, max := range []int{0, -1} {
		ok, err := w.Allow(ctx, "ip", max, time.Minute)
		require.NoError(t, err)
		assert.False(t, ok, "max=%d 应全拦", max)
	}
}

// TestMemoryWindow_SweepsDeadWindows 控制内存:一个 key 打满一次之后
// 窗口彻底滑空就该删掉,否则每个被限过的 IP 都留一条切片。
func TestMemoryWindow_SweepsDeadWindows(t *testing.T) {
	ctx := context.Background()
	clk := newFakeClock()
	w := NewMemoryWindow()
	w.now = clk.now

	for i := 0; i < 5; i++ {
		_, err := w.Allow(ctx, string(rune('a'+i)), 1, time.Minute)
		require.NoError(t, err)
	}
	assert.Equal(t, 5, w.Len())

	clk.add(2 * time.Minute)
	_, err := w.Allow(ctx, "z", 1, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 1, w.Len(), "滑空的老窗口没被回收 —— 长期跑会按 IP 数无限涨")
}

// TestMemoryWindow_SweepUsesPerKeyWindow 防"短窗口请求清掉长窗口记录"。
//
// 2026-11-03 审计 P1:sweepLocked 原来拿 Allow **本次请求**算出的 min 套
// 所有 key —— key A(1min)的 Allow 会把 key B(1h)窗口内的记录整串删光,
// B 的长窗口限流退化成 A 的短窗口。修后每个 key 用自己的 windowMs 判
// "全部落窗外才删"。
func TestMemoryWindow_SweepUsesPerKeyWindow(t *testing.T) {
	ctx := context.Background()
	clk := newFakeClock()
	w := NewMemoryWindow()
	w.now = clk.now

	// B 用 1h 窗口攒 2 条记录(t=0 / t=1s),max=2 恰好放满
	for i := 0; i < 2; i++ {
		ok, err := w.Allow(ctx, "B", 2, time.Hour)
		require.NoError(t, err)
		require.True(t, ok)
		clk.add(time.Second)
	}
	// 推进到 t=2min:A 用 1min 窗口 Allow 一次。
	// 此时 A 的 min = 60s > 0,B 的两条记录(ts=0/1s)按 A 的 min 已"全部落窗外",
	// 但按 B 自己的 1h 窗口都还在窗口内 —— 旧实现会在这里把 B 清空。
	clk.add(2 * time.Minute)
	ok, err := w.Allow(ctx, "A", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// B 再来一条:记录若被清,只数到 1 ≤ max 会放行(错);
	// 记录还在则是 3 条 > max=2,必须拒。
	clk.add(time.Second)
	ok, err = w.Allow(ctx, "B", 2, time.Hour)
	require.NoError(t, err)
	assert.False(t, ok,
		"A 的短窗口 Allow 把 B 的长窗口窗口内记录清掉了 —— B 的限流被退化")
}

// ===== 并发 =====

// TestMemoryStore_Concurrent 必带 -race 跑:这两个结构都是 map + 锁,
// 而它们防的恰恰是"被并发请求同时命中"这件事 —— 单线程测等于没测。
func TestMemoryStore_Concurrent(t *testing.T) {
	ctx := context.Background()
	st := memoryStore()

	const workers, each = 16, 100
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				if _, err := st.Counter.Incr(ctx, "shared", time.Minute); err != nil {
					t.Error(err)
					return
				}
				if _, err := st.Window.Allow(ctx, "w", 1000, time.Minute); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	n, err := st.Counter.Get(ctx, "shared")
	require.NoError(t, err)
	assert.EqualValues(t, workers*each, n, "并发下计数丢失更新")
}

// ===== Resolve =====

func TestResolve_BackendsAndErrors(t *testing.T) {
	t.Run("auto 无 Redis → 内存,且不是空指针", func(t *testing.T) {
		s, err := Resolve(ModeAuto, nil)
		require.NoError(t, err)
		assert.Equal(t, "memory", s.Backend)
		require.NotNil(t, s.Counter)
		require.NotNil(t, s.Window)
	})

	t.Run("redis 显式 + 无 Redis → 报错", func(t *testing.T) {
		// 这条是"配了就必须对":多副本下内存限流等于把阈值放大 N 倍,
		// 静默降级的话没有任何人知道限流实际是关着的。
		_, err := Resolve(ModeRedis, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kv.mode=redis")
	})

	t.Run("memory 显式 → 内存(不碰 Redis)", func(t *testing.T) {
		s, err := Resolve(ModeMemory, nil)
		require.NoError(t, err)
		assert.Equal(t, "memory", s.Backend)
	})

	t.Run("非法值 → 报错并列出可选值", func(t *testing.T) {
		_, err := Resolve("mongo", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "auto")
		assert.Contains(t, err.Error(), "redis")
		assert.Contains(t, err.Error(), "memory")
	})
}

// TestDefaultStore_NeverNil 是"决策点唯一"的兜底:即使 main 忘了注入,
// 调用点拿到的也是一个能用的存储,而不是 nil 让上层再写一层判空 ——
// 那层判空正是本层要消灭的东西。
func TestDefaultStore_NeverNil(t *testing.T) {
	st := DefaultStore()
	require.NotNil(t, st)
	require.NotNil(t, st.Counter)
	require.NotNil(t, st.Window)
}

// TestSetDefault_NilPanics 把"忘了注入"变成响亮失败。
func TestSetDefault_NilPanics(t *testing.T) {
	assert.Panics(t, func() { SetDefault(nil) },
		"SetDefault(nil) 静默通过 = 退回内存限流 = 所有限流断言假绿")
}

// TestRedisImpl_CompileTimeShape 保证 Redis 实现确实满足接口
// (构造路径只在 Resolve 里,漏接一个接口方法要等到线上才炸)。
var (
	_ Counter = (*RedisCounter)(nil)
	_ Window  = (*RedisWindow)(nil)
	_ Counter = (*MemoryCounter)(nil)
	_ Window  = (*MemoryWindow)(nil)
	_         = redis.Nil // 保持 redis 依赖可见,redis.go 用它判 Nil
)