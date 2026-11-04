package retryx

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDo_SuccessFirstTry(t *testing.T) {
	var calls atomic.Int32
	err := Do(context.Background(), Opts{MaxAttempts: 3}, func(ctx context.Context) error {
		calls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", calls.Load())
	}
}

func TestDo_RetryUntilSuccess(t *testing.T) {
	var calls atomic.Int32
	err := Do(context.Background(), Opts{
		MaxAttempts:    5,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}, func(ctx context.Context) error {
		n := calls.Add(1)
		if n < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", calls.Load())
	}
}

func TestDo_ExhaustRetries(t *testing.T) {
	var calls atomic.Int32
	sentinel := errors.New("always fail")
	err := Do(context.Background(), Opts{
		MaxAttempts:    3,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
	}, func(ctx context.Context) error {
		calls.Add(1)
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", calls.Load())
	}
}

func TestDo_RetryIfBlocks(t *testing.T) {
	var calls atomic.Int32
	sentinel := errors.New("do not retry")
	err := Do(context.Background(), Opts{
		MaxAttempts:    5,
		InitialBackoff: 1 * time.Millisecond,
		RetryIf: func(e error) bool {
			return e.Error() != "do not retry"
		},
	}, func(ctx context.Context) error {
		calls.Add(1)
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", calls.Load())
	}
}

// TestDo_ContextCancelStops ctx 取消 ⇒ 重试循环停下
//
// ⚠️ **不能断言"调用次数 ≤ 3"** —— `Do` 的退避是 **full jitter**
// (`wait = uniform(0, backoff)`,见 retryx.go 的 `jitter`),所以
// "deadline 之前能重试几次"本身是**随机**的:抽到接近 0 就连着转,
// 50ms 的 deadline 里转 4 次以上是常事。
//
// 旧断言(`calls > 3` 就红)实测**单独跑 10 次红 3 次** —— 它守的不是行为,
// 是运气 ⇒ 一个 30% 概率红的守卫 = 没有守卫(跑的人只会学会忽略它)。
//
// 换成三个**确定性**的断言(每个都不依赖随机数):
//
//   - **A** 已经取消的 ctx ⇒ fn **一次都不该被调**(循环开头就查 ctx);
//   - **B** fn 里取消 ctx ⇒ 停,**且错误链里带 `context.Canceled`**
//     (是等待被 ctx 打断,不是撞了 MaxAttempts);
//   - **C** deadline 到点 ⇒ 停,**且错误链里带 `context.DeadlineExceeded`**。
//
// B / C 里"错误链带 ctx 错"才是**强断言**:实现若无视 ctx,循环会一路重试到
// MaxAttempts(100 次)才返回 **fn 的原始错误** —— 那条错误链里**没有** ctx 错
// ⇒ 断言变红(而不是"跑得慢一点"这种含糊的失败)。
func TestDo_ContextCancelStops(t *testing.T) {
	t.Run("已取消的 ctx ⇒ fn 一次不调", func(t *testing.T) {
		var calls atomic.Int32
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // 提前取消
		err := Do(ctx, Opts{MaxAttempts: 3}, func(ctx context.Context) error {
			calls.Add(1)
			return nil
		})
		if calls.Load() != 0 {
			t.Fatalf("ctx 已取消,fn 不该被调用,实际调了 %d 次", calls.Load())
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("错误链里应有 context.Canceled,实际 %v", err)
		}
	})

	t.Run("fn 里取消 ⇒ 停,且错误链带 context.Canceled", func(t *testing.T) {
		var calls atomic.Int32
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := Do(ctx, Opts{
			MaxAttempts:    100,
			InitialBackoff: 30 * time.Millisecond,
			MaxBackoff:     100 * time.Millisecond,
		}, func(ctx context.Context) error {
			calls.Add(1)
			cancel() // ← 第一次就取消
			return errors.New("fail")
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("错误链里应有 context.Canceled,实际 %v", err)
		}
		if calls.Load() != 1 {
			t.Fatalf("ctx 在 fn 里被取消 ⇒ 应当只调 1 次,实际 %d 次", calls.Load())
		}
	})

	t.Run("deadline 到点 ⇒ 停,且错误链带 context.DeadlineExceeded", func(t *testing.T) {
		var calls atomic.Int32
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		err := Do(ctx, Opts{
			MaxAttempts:    100,
			InitialBackoff: 30 * time.Millisecond,
			MaxBackoff:     100 * time.Millisecond,
		}, func(ctx context.Context) error {
			calls.Add(1)
			return errors.New("fail")
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("错误链里应有 context.DeadlineExceeded,实际 %v", err)
		}
		// 停的原因是 **ctx 到点**,不是把 MaxAttempts 用光。
		if calls.Load() >= 100 {
			t.Fatalf("应当是 ctx 到点停的,不是撞了 MaxAttempts(实际调了 %d 次)", calls.Load())
		}
	})
}

func TestIsRetryable_DefaultBehavior(t *testing.T) {
	if IsRetryable(nil) {
		t.Fatal("nil should not be retryable")
	}
	if IsRetryable(context.Canceled) {
		t.Fatal("context.Canceled should not be retryable")
	}
	if IsRetryable(context.DeadlineExceeded) {
		t.Fatal("context.DeadlineExceeded should not be retryable")
	}
	if !IsRetryable(errors.New("any other")) {
		t.Fatal("other errors should be retryable by default")
	}
}
