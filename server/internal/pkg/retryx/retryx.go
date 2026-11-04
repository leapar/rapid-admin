// Package retryx —— 通用重试 / 退避(指数回退 + 抖动)
// 文档 §1.11 #8 P1
//
// 设计原则:
//   - 显式重试条件(RetryIf):业务决定哪些 error 值得重试
//   - 显式 deadline(Attempts + MaxElapsed):不无限重试
//   - full jitter 抖动:避免雷鸣群
//   - ctx cancel 立即退出:不浪费工作
//   - 可选 metrics + logger 注入:不强制依赖
//
// 用法:
//   err := retryx.Do(ctx, retryx.Opts{
//     MaxAttempts: 3,
//     InitialBackoff: 200 * time.Millisecond,
//     MaxBackoff: 5 * time.Second,
//   }, func(ctx context.Context) error {
//     return doSomething(ctx)
//   })
//
// 装饰 HTTP/SQL/任意函数都可以。
package retryx

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// Opts 重试配置
type Opts struct {
	// MaxAttempts 总尝试次数(>=1)。=1 等同不重试
	MaxAttempts int
	// InitialBackoff 第 1 次重试前等待
	InitialBackoff time.Duration
	// MaxBackoff 单次重试等待上限(指数曲线封顶)
	MaxBackoff time.Duration
	// Multiplier 退避倍数(默认 2)
	Multiplier float64
	// RetryIf 返回 true 时才重试;nil = 所有 error 都重试
	//   - 用于过滤"已知的不可重试 error"(如业务参数错误 4xx)
	RetryIf func(error) bool
	// OnRetry 每次重试前调(用于打日志/埋点);nil = 默认 warn log
	OnRetry func(attempt int, err error, nextBackoff time.Duration)
	// Logger 自定义 logger;nil 用 default
	Logger *zap.Logger
}

// IsRetryable 默认 RetryIf:所有非 context.Canceled 错误都重试
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

// Do 执行 fn 并按 opts 重试
//   - 第 1 次失败后等待 InitialBackoff,再重试
//   - 每次 backoff 翻倍(Multiplier),封顶 MaxBackoff
//   - full jitter:backoff * random(0, 1) 实际等待
//   - 满足 RetryIf(err)==false 立即返回
//   - ctx done 立即返回
//   - fn 成功 → 立即返回 nil
func Do(ctx context.Context, opts Opts, fn func(ctx context.Context) error) error {
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = 100 * time.Millisecond
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 10 * time.Second
	}
	if opts.Multiplier <= 1 {
		opts.Multiplier = 2
	}
	if opts.RetryIf == nil {
		opts.RetryIf = IsRetryable
	}
	if opts.Logger == nil {
		opts.Logger = logger.Default
	}
	if opts.OnRetry == nil {
		opts.OnRetry = defaultOnRetry(opts.Logger)
	}

	var lastErr error
	backoff := opts.InitialBackoff

	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		// 检查 ctx
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return errors.Join(lastErr, err)
			}
			return err
		}
		// 执行
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		// 不重试
		if !opts.RetryIf(err) {
			return err
		}
		// 已达最大次数
		if attempt == opts.MaxAttempts {
			return err
		}
		// 计算 backoff + jitter
		wait := jitter(backoff)
		opts.OnRetry(attempt, err, wait)
		// 等待(可被 ctx 打断)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return errors.Join(err, ctx.Err())
		case <-t.C:
		}
		// 翻倍(封顶)
		backoff = nextBackoff(backoff, opts.Multiplier, opts.MaxBackoff)
	}
	return lastErr
}

// jitter full jitter:实际等待 = uniform(0, backoff)
// 避免雷鸣群("thundering herd") — AWS 论文推荐
func jitter(backoff time.Duration) time.Duration {
	if backoff <= 0 {
		return 0
	}
	randMu.Lock()
	defer randMu.Unlock()
	return time.Duration(rng.Int63n(int64(backoff)))
}

var (
	randMu sync.Mutex
	rng    = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func nextBackoff(cur time.Duration, mult float64, max time.Duration) time.Duration {
	next := time.Duration(float64(cur) * mult)
	if next > max {
		return max
	}
	return next
}

func defaultOnRetry(log *zap.Logger) func(int, error, time.Duration) {
	return func(attempt int, err error, wait time.Duration) {
		if log == nil {
			return
		}
		log.Warn("retryx: 重试中",
			zap.Int("attempt", attempt),
			zap.Error(err),
			zap.Duration("next_backoff", wait),
		)
	}
}
