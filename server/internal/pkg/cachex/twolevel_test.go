package cachex

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 注:L2(Safe) 行为已由 safe_test.go 覆盖,TwoLevel 的测试聚焦:
//   - L1 命中 / 过期 / null 占位 / 删除 / Purge
//   - 与 Safe 配合时,L1 回写 + Stats
// 测试用 nil Safe → 只测 L1 行为,避免依赖真 Redis
// (L2 集成在 dev 环境跑 service 端到端验证)

func TestTwoLevel_L1Hit_AvoidsLoader(t *testing.T) {
	c := NewTwoLevel(100, 5*time.Second, nil)
	ctx := context.Background()

	var calls atomic.Int32
	loader := func(ctx context.Context) (any, error) {
		calls.Add(1)
		return "v1", nil
	}
	v, hit, err := c.GetOrLoad(ctx, "k1", 5*time.Second, loader)
	if err != nil || hit {
		t.Fatalf("first call: hit=%v err=%v", hit, err)
	}
	if v != "v1" || calls.Load() != 1 {
		t.Fatalf("first call wrong: v=%v calls=%d", v, calls.Load())
	}
	v2, hit2, _ := c.GetOrLoad(ctx, "k1", 5*time.Second, loader)
	if !hit2 || v2 != "v1" {
		t.Fatalf("second should hit L1: hit=%v v=%v", hit2, v2)
	}
	if calls.Load() != 1 {
		t.Fatalf("loader should not be called again, got %d", calls.Load())
	}
	stats := c.GetStats()
	if stats.L1Hit != 1 || stats.L1Miss != 1 {
		t.Fatalf("stats wrong: %+v", stats)
	}
}

func TestTwoLevel_L1Expire_ReLoad(t *testing.T) {
	c := NewTwoLevel(10, 50*time.Millisecond, nil)
	ctx := context.Background()
	var calls atomic.Int32
	loader := func(ctx context.Context) (any, error) {
		calls.Add(1)
		return "x", nil
	}
	c.GetOrLoad(ctx, "k", time.Second, loader)
	if calls.Load() != 1 {
		t.Fatal("first load")
	}
	c.GetOrLoad(ctx, "k", time.Second, loader)
	if calls.Load() != 1 {
		t.Fatal("should be L1 hit")
	}
	time.Sleep(80 * time.Millisecond)
	c.GetOrLoad(ctx, "k", time.Second, loader)
	if calls.Load() != 2 {
		t.Fatalf("after expire should reload, calls=%d", calls.Load())
	}
	stats := c.GetStats()
	if stats.L1Expire < 1 {
		t.Fatalf("expected L1Expire >= 1, got %d", stats.L1Expire)
	}
}

func TestTwoLevel_NullSentinel_NoLoader(t *testing.T) {
	c := NewTwoLevel(10, 200*time.Millisecond, nil)
	ctx := context.Background()
	var calls atomic.Int32
	loader := func(ctx context.Context) (any, error) {
		calls.Add(1)
		return nil, ErrNotFound
	}
	v, hit, err := c.GetOrLoad(ctx, "missing", 5*time.Second, loader)
	if err != nil || hit {
		t.Fatalf("notfound: err=%v hit=%v", err, hit)
	}
	if v != nil {
		t.Fatalf("notfound value: %v", v)
	}
	v2, hit2, _ := c.GetOrLoad(ctx, "missing", 5*time.Second, loader)
	if !hit2 {
		t.Fatalf("null should hit L1, got hit=%v", hit2)
	}
	if v2 != nil || calls.Load() != 1 {
		t.Fatalf("null hit: v=%v calls=%d", v2, calls.Load())
	}
}

func TestTwoLevel_Set_Delete_Get(t *testing.T) {
	c := NewTwoLevel(10, time.Second, nil)
	ctx := context.Background()
	if err := c.Set(ctx, "k", "v", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	v, ok := c.Get("k")
	if !ok || v != "v" {
		t.Fatalf("Get L1: ok=%v v=%v", ok, v)
	}
	c.Delete(ctx, "k")
	_, ok2 := c.Get("k")
	if ok2 {
		t.Fatal("after Delete, L1 should miss")
	}
	// 再 GetOrLoad → 走 loader
	var calls atomic.Int32
	c.GetOrLoad(ctx, "k", time.Second, func(ctx context.Context) (any, error) {
		calls.Add(1)
		return "x", nil
	})
	if calls.Load() != 1 {
		t.Fatalf("after delete, should reload: calls=%d", calls.Load())
	}
}

func TestTwoLevel_Purge(t *testing.T) {
	c := NewTwoLevel(10, time.Second, nil)
	ctx := context.Background()
	loader := func(ctx context.Context) (any, error) { return "x", nil }
	c.GetOrLoad(ctx, "a", time.Second, loader)
	c.GetOrLoad(ctx, "b", time.Second, loader)
	if c.GetStats().L1Size != 2 {
		t.Fatalf("expected 2 L1 entries, got %d", c.GetStats().L1Size)
	}
	c.Purge()
	if c.GetStats().L1Size != 0 {
		t.Fatalf("after Purge: %d", c.GetStats().L1Size)
	}
}

func TestTwoLevel_LoaderError_NotCached(t *testing.T) {
	c := NewTwoLevel(10, time.Second, nil)
	ctx := context.Background()
	loaderErr := errors.New("db down")
	var calls atomic.Int32
	loader := func(ctx context.Context) (any, error) {
		calls.Add(1)
		return nil, loaderErr
	}
	_, _, err := c.GetOrLoad(ctx, "k", time.Second, loader)
	if !errors.Is(err, loaderErr) {
		t.Fatalf("err: %v", err)
	}
	_, _, err = c.GetOrLoad(ctx, "k", time.Second, loader)
	if !errors.Is(err, loaderErr) {
		t.Fatalf("err2: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("error should not cache, calls=%d", calls.Load())
	}
}

func TestTwoLevel_ConcurrentLoader_NoL2_SingleInflight(t *testing.T) {
	// 无 L2 时:同 key 并发 → Safe 仍会用 singleflight?不,无 Safe
	// 这里测 TwoLevel 自己的并发:20 goroutine 同 key,loader 应被调多次(无 L2 singleflight)
	// (有 L2 时由 Safe 内部 singleflight 兜底)
	// 因此:本测试只验证"无 L2 不会 panic + 最终能拿到值"
	c := NewTwoLevel(100, time.Second, nil)
	ctx := context.Background()
	var calls atomic.Int32
	loader := func(ctx context.Context) (any, error) {
		time.Sleep(5 * time.Millisecond)
		calls.Add(1)
		return "x", nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, _, err := c.GetOrLoad(ctx, "k", time.Second, loader)
			if err != nil || v != "x" {
				t.Errorf("v=%v err=%v", v, err)
			}
		}()
	}
	wg.Wait()
	// 无 L2 不会有 singleflight,所有 goroutine 都跑 loader
	if calls.Load() != 20 {
		t.Fatalf("expected 20 loader calls (no L2 singleflight), got %d", calls.Load())
	}
	// 但后续单 goroutine 调:L1 命中
	v, hit, _ := c.GetOrLoad(ctx, "k", time.Second, loader)
	if !hit || v != "x" {
		t.Fatalf("post-race L1: hit=%v v=%v", hit, v)
	}
}

func TestTwoLevel_KeyPrefixIsolation(t *testing.T) {
	// nil Safe → fkey = 原 key
	c := NewTwoLevel(10, time.Second, nil)
	if c.fkey("k") != "k" {
		t.Fatalf("nil safe: fkey should be plain key, got %s", c.fkey("k"))
	}
}

func TestTwoLevel_L1Cap_LRUEvicts(t *testing.T) {
	// LRU 容量 = 3,装 5 个,旧应被淘汰
	c := NewTwoLevel(3, time.Second, nil)
	ctx := context.Background()
	loader := func(ctx context.Context) (any, error) { return "v", nil }
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		c.GetOrLoad(ctx, k, time.Second, loader)
	}
	if c.GetStats().L1Size != 3 {
		t.Fatalf("expected 3 L1 entries (cap=3), got %d", c.GetStats().L1Size)
	}
	// 早期 key "a"/"b" 应被淘汰
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should be evicted")
	}
}
