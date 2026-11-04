// retryx —— 指数退避重试
//
// ## 这个包解决什么问题
//
// 调用外部服务(OSS / 短信 / 第三方 API)时,偶发失败是常态。
// retryx 把"退避 + 重试"这件事标准化:
//
//   - 指数退避(默认 ×2),带单次上限
//   - 抖动:避免大量请求在同一时刻一起重试(惊群)
//   - **只重试可重试的错误**:4xx 参数错误重试一万次也没用
//   - 尊重 ctx 取消
//
// ## 关键点
//
//   - RetryIf 决定"什么错误值得重试"。nil = 全部重试(慎用)
//   - OnRetry 每次重试前调,用来打日志 / 埋点
//   - MaxAttempts 是**总次数**(含第一次),=1 等同不重试
//
// ## 跑法
//
//	go run ./examples/retryx
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/retryx"
)

func main() {
	ctx := context.Background()

	// ── 一、失败的错误不重试 ────────────────────────────────────
	fmt.Println("=== 不可重试的错误 ===")
	attempts := 0
	err := retryx.Do(ctx, retryx.Opts{
		MaxAttempts:    5,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		Multiplier:     2,
		RetryIf: func(err error) bool {
			// 只有"服务端暂时不可用"才值得重试
			return errors.Is(err, errTemporary)
		},
	}, func(ctx context.Context) error {
		attempts++
		return errBusiness // 业务错误 ⇒ 不该重试
	})
	fmt.Printf("结果 err = %v,实际尝试 %d 次(不重试)\n", err, attempts)

	// ── 二、临时错误会重试,退避递增 ────────────────────────────
	fmt.Println()
	fmt.Println("=== 临时错误会重试 ===")
	attempts = 0
	start := time.Now()
	err = retryx.Do(ctx, retryx.Opts{
		MaxAttempts:    4,
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     200 * time.Millisecond,
		Multiplier:     2,
		RetryIf: func(err error) bool {
			return errors.Is(err, errTemporary)
		},
		OnRetry: func(attempt int, err error, nextBackoff time.Duration) {
			fmt.Printf("  第 %d 次失败(%v),%v 后重试\n", attempt, err, nextBackoff)
		},
	}, func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errTemporary
		}
		return nil
	})
	fmt.Printf("结果 err = %v,实际尝试 %d 次,总耗时 %v\n", err, attempts, time.Since(start).Round(time.Millisecond))

	// ── 三、重试次数用尽仍失败 ──────────────────────────────────
	fmt.Println()
	fmt.Println("=== 次数用尽 ===")
	attempts = 0
	err = retryx.Do(ctx, retryx.Opts{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		RetryIf: func(err error) bool {
			return errors.Is(err, errTemporary)
		},
	}, func(ctx context.Context) error {
		attempts++
		return errTemporary
	})
	fmt.Printf("结果 err = %v,实际尝试 %d 次\n", err, attempts)

	// ── 四、ctx 取消会立刻中止 ──────────────────────────────────
	fmt.Println()
	fmt.Println("=== ctx 取消 ===")
	cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	attempts = 0
	start = time.Now()
	err = retryx.Do(cancelCtx, retryx.Opts{
		MaxAttempts:    100,
		InitialBackoff: 30 * time.Millisecond,
		MaxBackoff:     30 * time.Millisecond,
		RetryIf: func(err error) bool {
			return errors.Is(err, errTemporary)
		},
	}, func(ctx context.Context) error {
		attempts++
		return errTemporary
	})
	fmt.Printf("结果 err = %v,实际尝试 %d 次,耗时 %v(没有跑满 100 次)\n",
		err, attempts, time.Since(start).Round(time.Millisecond))
}

// 两种典型错误
var (
	errTemporary = errors.New("上游临时不可用")
	errBusiness  = errors.New("参数不合法")
)
