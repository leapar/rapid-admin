// cachex 启动预热(§1.7 #8 P2)
//
// 设计目标:
//  1. 服务启动时把热点数据(DB/远端 RPC)灌进缓存,避免冷启动"第一个用户付 DB 慢查询"罚
//  2. 并发跑,单个 warmer 失败不阻断其他(走 OnError 回调,默认 logger.Warn)
//  3. 单个 warmer 独立超时(防止某个慢 loader 拖死整体)
//  4. 与 §1.11 #7 syncx.Semaphore 配合做并发限流
//
// 用法:
//
//	warmers := []cachex.Warmer{
//	    {Key: "dict:all", Loader: loadAllDict, TTL: 10*time.Minute},
//	    {Key: "config:sys", Loader: loadSysConfig, TTL: 5*time.Minute},
//	}
//	stats := cachex.WarmAll(ctx, cachex.New(repository.GetRedis()), warmers,
//	    cachex.WithWarmConcurrency(10),
//	    cachex.WithWarmTimeout(30*time.Second),
//	)
//	log.Info("cache warmed", zap.Int("success", stats.Success), zap.Int("error", stats.Errored))
//
// 与 M38 缓存监控配合:
//   - WarmAll 结束后 L1/L2 命中率从 t=0 就是有效值
//   - 配合 M33 slowquery 看到 "cache warm done" 后业务 SQL 显著下降
package cachex

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/syncx"
)

// CacheStore 最小写接口 —— Safe / TwoLevel 都实现 Set
//   - 测试用 fake store 也实现这个接口
//   - 不引入读路径(warm 不需要查 cache)
type CacheStore interface {
	Set(ctx context.Context, key string, val any, ttl time.Duration) error
}

// Warmer 一条预热项 —— 描述"启动时从哪里拿数据 → 灌到哪个 key → 保留多久"
type Warmer struct {
	// Key 缓存 key(必须与业务 GetOrLoad 用的 key 一致,warm 后业务就能命中)
	Key string
	// Loader 数据源(可以是 DB 查询 / 远端 RPC / 任意 io)
	//   - 返 (nil, ErrNotFound) → 不写 cache(防止负缓存污染)
	//   - 返 (val, nil) → cache.Set(key, val, ttl)
	//   - 返其他 error → 走 OnError 回调,该条 warm 失败,不影响其他
	Loader func(ctx context.Context) (any, error)
	// TTL 写入 cache 的 TTL(应 >= 业务读路径期望的 TTL)
	TTL time.Duration
}

// WarmOption 预热选项
type WarmOption func(*warmOpts)

type warmOpts struct {
	Concurrency int                           // 并发上限(默认 10)
	PerTimeout  time.Duration                 // 单个 warmer 超时(默认 30s)
	OnError     func(key string, err error)   // 错误回调(默认 logger.Warn)
}

// WithWarmConcurrency 设置并发上限(默认 10)
func WithWarmConcurrency(n int) WarmOption {
	return func(o *warmOpts) {
		if n > 0 {
			o.Concurrency = n
		}
	}
}

// WithWarmTimeout 设置单个 warmer 超时(默认 30s,防止慢 loader 拖死整体)
func WithWarmTimeout(d time.Duration) WarmOption {
	return func(o *warmOpts) {
		if d > 0 {
			o.PerTimeout = d
		}
	}
}

// WithWarmOnError 自定义错误回调(默认 logger.Warn)
func WithWarmOnError(fn func(key string, err error)) WarmOption {
	return func(o *warmOpts) {
		if fn != nil {
			o.OnError = fn
		}
	}
}

// WarmStats 预热结果(用于打日志 / 监控)
type WarmStats struct {
	Total    int                      `json:"total"`     // 预热项总数
	Success  int                      `json:"success"`   // 成功数(loader + cache.Set 全成)
	Errored  int                      `json:"errored"`   // 失败数(loader / Set / 超时 / ctx cancel)
	Duration time.Duration            `json:"duration"`  // 总耗时(并发上限时)
	PerKey   map[string]time.Duration `json:"per_key"`   // 每项耗时(key → dur)
}

// WarmAll 并发跑所有预热项,返回统计
//   - cache == nil → 静默 no-op,返空 stats(启动期不阻断;典型场景:rdb 未配时 cache 是 nil)
//   - warmers 空 → 0 stats,no-op
//   - 任一 loader error 不阻断其他(走 OnError)
//   - 整体 ctx cancel 立即中断所有进行中的 warmer
//   - 每个 warmer 独立 PerTimeout(防止慢 loader 拖死整体)
func WarmAll(ctx context.Context, cache CacheStore, warmers []Warmer, opts ...WarmOption) WarmStats {
	stats := WarmStats{
		Total:  len(warmers),
		PerKey: make(map[string]time.Duration, len(warmers)),
	}
	if cache == nil || len(warmers) == 0 {
		return stats
	}

	o := warmOpts{
		Concurrency: 10,
		PerTimeout:  30 * time.Second,
		OnError:     defaultOnError,
	}
	for _, opt := range opts {
		opt(&o)
	}

	start := time.Now()

	// 并发限流:用 M37 syncx.Semaphore
	sem := syncx.NewSemaphore(o.Concurrency)
	defer sem.Close()

	var (
		wg                   sync.WaitGroup
		success, errored     atomic.Int64
		statsMu              sync.Mutex // 保护 stats.PerKey 并发写
	)

	for _, w := range warmers {
		w := w // 闭包捕获
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 先查 ctx:已 cancel 直接退出,避免 select 随机性导致部分 goroutine 拿到 token
			if cerr := ctx.Err(); cerr != nil {
				errored.Add(1)
				statsMu.Lock()
				stats.PerKey[w.Key] = 0
				statsMu.Unlock()
				o.OnError(w.Key, fmt.Errorf("ctx: %w", cerr))
				return
			}
			if err := sem.Acquire(ctx); err != nil {
				errored.Add(1)
				statsMu.Lock()
				stats.PerKey[w.Key] = 0
				statsMu.Unlock()
				o.OnError(w.Key, fmt.Errorf("semaphore acquire: %w", err))
				return
			}
			defer sem.Release()

			wctx, cancel := context.WithTimeout(ctx, o.PerTimeout)
			defer cancel()
			t0 := time.Now()
			val, err := w.Loader(wctx)
			dur := time.Since(t0)
			if err != nil {
				errored.Add(1)
				statsMu.Lock()
				stats.PerKey[w.Key] = dur
				statsMu.Unlock()
				o.OnError(w.Key, err)
				return
			}
			if serr := cache.Set(wctx, w.Key, val, w.TTL); serr != nil {
				errored.Add(1)
				statsMu.Lock()
				stats.PerKey[w.Key] = time.Since(t0)
				statsMu.Unlock()
				o.OnError(w.Key, fmt.Errorf("cache.Set: %w", serr))
				return
			}
			success.Add(1)
			statsMu.Lock()
			stats.PerKey[w.Key] = dur
			statsMu.Unlock()
		}()
	}
	wg.Wait()

	stats.Success = int(success.Load())
	stats.Errored = int(errored.Load())
	stats.Duration = time.Since(start)
	return stats
}

// defaultOnError 默认错误处理(走 zap logger,key + err)
func defaultOnError(key string, err error) {
	logger.L().Warn("cachex warm failed",
		zap.String("key", key),
		zap.String("err", err.Error()))
}
