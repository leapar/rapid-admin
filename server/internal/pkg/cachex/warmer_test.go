// cachex warmer 单测(§1.7 #8 P2)
package cachex

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeStore 内存 mock,用于测 WarmAll 不依赖 Redis
type fakeStore struct {
	mu        sync.Mutex
	data      map[string]any
	setErr    error // 可注入 Set 错误
	setCalls  int
}

func newFakeStore() *fakeStore { return &fakeStore{data: make(map[string]any)} }

func (f *fakeStore) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	f.data[key] = val
	return nil
}

func (f *fakeStore) get(key string) (any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	return v, ok
}

func TestWarmAll_NilCache_NoOp(t *testing.T) {
	stats := WarmAll(context.Background(), nil, []Warmer{
		{Key: "k1", Loader: func(ctx context.Context) (any, error) { return "v", nil }, TTL: time.Minute},
	})
	if stats.Total != 1 || stats.Success != 0 || stats.Errored != 0 {
		t.Fatalf("nil cache should no-op, got %+v", stats)
	}
}

func TestWarmAll_EmptyList_NoOp(t *testing.T) {
	store := newFakeStore()
	stats := WarmAll(context.Background(), store, nil)
	if stats.Total != 0 {
		t.Fatalf("empty should be 0, got %+v", stats)
	}
	if store.setCalls != 0 {
		t.Fatal("no Set should be called")
	}
}

func TestWarmAll_AllSuccess(t *testing.T) {
	store := newFakeStore()
	warmers := []Warmer{
		{Key: "k1", Loader: func(ctx context.Context) (any, error) { return "v1", nil }, TTL: time.Minute},
		{Key: "k2", Loader: func(ctx context.Context) (any, error) { return 42, nil }, TTL: time.Minute},
		{Key: "k3", Loader: func(ctx context.Context) (any, error) { return map[string]any{"a": 1}, nil }, TTL: time.Minute},
	}
	stats := WarmAll(context.Background(), store, warmers, WithWarmConcurrency(3))
	if stats.Success != 3 {
		t.Fatalf("success = %d, want 3", stats.Success)
	}
	if stats.Errored != 0 {
		t.Fatalf("errored = %d, want 0", stats.Errored)
	}
	if stats.Total != 3 {
		t.Fatalf("total = %d, want 3", stats.Total)
	}
	// 验证 cache 真的被填
	if v, ok := store.get("k1"); !ok || v != "v1" {
		t.Fatalf("k1 = %v, want v1", v)
	}
	if v, ok := store.get("k2"); !ok || v != 42 {
		t.Fatalf("k2 = %v, want 42", v)
	}
	if _, ok := store.get("k3"); !ok {
		t.Fatalf("k3 missing")
	}
	// 每 key 都应有耗时
	if len(stats.PerKey) != 3 {
		t.Fatalf("per_key size = %d, want 3", len(stats.PerKey))
	}
}

func TestWarmAll_LoaderError(t *testing.T) {
	store := newFakeStore()
	var onErrorCalled atomic.Int32
	warmers := []Warmer{
		{Key: "ok1", Loader: func(ctx context.Context) (any, error) { return "v", nil }, TTL: time.Minute},
		{Key: "fail", Loader: func(ctx context.Context) (any, error) { return nil, errors.New("boom") }, TTL: time.Minute},
		{Key: "ok2", Loader: func(ctx context.Context) (any, error) { return "v", nil }, TTL: time.Minute},
	}
	stats := WarmAll(context.Background(), store, warmers,
		WithWarmConcurrency(3),
		WithWarmOnError(func(key string, err error) {
			onErrorCalled.Add(1)
		}),
	)
	if stats.Success != 2 {
		t.Fatalf("success = %d, want 2", stats.Success)
	}
	if stats.Errored != 1 {
		t.Fatalf("errored = %d, want 1", stats.Errored)
	}
	if onErrorCalled.Load() != 1 {
		t.Fatalf("OnError called = %d, want 1", onErrorCalled.Load())
	}
	// fail 的 key 不应进 cache
	if _, ok := store.get("fail"); ok {
		t.Fatal("failed warm should not be cached")
	}
}

func TestWarmAll_CacheSetError(t *testing.T) {
	store := newFakeStore()
	store.setErr = errors.New("redis down")
	var onErrorCalled atomic.Int32
	warmers := []Warmer{
		{Key: "k1", Loader: func(ctx context.Context) (any, error) { return "v", nil }, TTL: time.Minute},
	}
	stats := WarmAll(context.Background(), store, warmers,
		WithWarmOnError(func(key string, err error) {
			onErrorCalled.Add(1)
			if !contains(err.Error(), "redis down") {
				t.Errorf("expected redis down in err, got %v", err)
			}
		}),
	)
	if stats.Errored != 1 {
		t.Fatalf("errored = %d, want 1", stats.Errored)
	}
	if onErrorCalled.Load() != 1 {
		t.Fatal("OnError not called")
	}
}

func TestWarmAll_Concurrency_LimitsParallelism(t *testing.T) {
	// 5 个 warmer 各 sleep 100ms,concurrency=3 → 总耗时 ~200ms(3 + 2 批)
	// 若 concurrency=1 → ~500ms
	// 若 concurrency=5 → ~100ms
	store := newFakeStore()
	mkW := func(k string) Warmer {
		return Warmer{Key: k, Loader: func(ctx context.Context) (any, error) {
			select {
			case <-time.After(100 * time.Millisecond):
				return k, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, TTL: time.Minute}
	}
	warmers := []Warmer{mkW("k1"), mkW("k2"), mkW("k3"), mkW("k4"), mkW("k5")}

	// concurrency=3
	t0 := time.Now()
	stats := WarmAll(context.Background(), store, warmers, WithWarmConcurrency(3))
	dur := time.Since(t0)
	if stats.Success != 5 {
		t.Fatalf("success = %d, want 5", stats.Success)
	}
	// 期望 200-400ms(给些误差)
	if dur < 150*time.Millisecond || dur > 400*time.Millisecond {
		t.Fatalf("dur = %v, want ~200ms (3+2 batches)", dur)
	}
}

func TestWarmAll_PerTimeout(t *testing.T) {
	// 1 个 warmer sleep 500ms,perTimeout=100ms → 该条失败
	store := newFakeStore()
	var onErrorKey atomic.Value
	warmers := []Warmer{
		{Key: "slow", Loader: func(ctx context.Context) (any, error) {
			select {
			case <-time.After(500 * time.Millisecond):
				return "v", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, TTL: time.Minute},
	}
	stats := WarmAll(context.Background(), store, warmers,
		WithWarmTimeout(100*time.Millisecond),
		WithWarmOnError(func(key string, err error) {
			onErrorKey.Store(key)
		}),
	)
	if stats.Errored != 1 {
		t.Fatalf("errored = %d, want 1", stats.Errored)
	}
	if stats.Success != 0 {
		t.Fatalf("success = %d, want 0", stats.Success)
	}
	if v := onErrorKey.Load(); v != "slow" {
		t.Fatalf("OnError key = %v, want slow", v)
	}
}

func TestWarmAll_ContextCancel_StopsAll(t *testing.T) {
	// 启动前 cancel ctx → 所有 warmer 立即退出
	store := newFakeStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 提前取消
	warmers := []Warmer{
		{Key: "k1", Loader: func(ctx context.Context) (any, error) { return "v", nil }, TTL: time.Minute},
		{Key: "k2", Loader: func(ctx context.Context) (any, error) { return "v", nil }, TTL: time.Minute},
	}
	stats := WarmAll(ctx, store, warmers)
	if stats.Errored != 2 {
		t.Fatalf("errored = %d, want 2", stats.Errored)
	}
	if stats.Success != 0 {
		t.Fatalf("success = %d, want 0", stats.Success)
	}
}

func TestWarmAll_LoaderReturnsErrNotFound(t *testing.T) {
	// loader 返 ErrNotFound → cache.Set 不应被调(避免负缓存污染)
	// 注:实现里我们不强制区分(loader 返 ErrNotFound 当成 error → 不进 cache)
	store := newFakeStore()
	warmers := []Warmer{
		{Key: "missing", Loader: func(ctx context.Context) (any, error) {
			return nil, ErrNotFound
		}, TTL: time.Minute},
	}
	stats := WarmAll(context.Background(), store, warmers)
	if stats.Errored != 1 {
		t.Fatalf("errored = %d, want 1", stats.Errored)
	}
	if _, ok := store.get("missing"); ok {
		t.Fatal("ErrNotFound should not be cached")
	}
}

func TestWarmAll_Concurrent_ManyWarmers(t *testing.T) {
	// 50 个 warmer,concurrency=10,验证不报 race + 全成
	store := newFakeStore()
	warmers := make([]Warmer, 50)
	for i := range warmers {
		warmers[i] = Warmer{
			Key: "k" + string(rune('0'+i%10)) + string(rune('0'+i/10)),
			Loader: func(ctx context.Context) (any, error) {
				return i, nil
			},
			TTL: time.Minute,
		}
	}
	stats := WarmAll(context.Background(), store, warmers, WithWarmConcurrency(10))
	if stats.Success != 50 {
		t.Fatalf("success = %d, want 50", stats.Success)
	}
}

// contains 简单 string contains
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
