// Semaphore 计数信号量(§1.11 #7 P2)
//
// 目的:
//   - 限制对某资源的并发访问(如:同时最多 5 个上传任务)
//   - 比 channel 实现更直观(acquire/release 不需要"忘记 release"踩坑)
//
// 设计:
//   - 内部用 buffered chan(初始塞 N 个 token)
//   - Acquire = 拿一个 token(阻塞 / ctx cancel / TryAcquire)
//   - Release = 还一个 token
//   - 多次 release 防御性:满则丢弃(不会爆 buffer)
//   - Close = close(done) 唤醒所有阻塞中的 Acquire
//
// 用法:
//
//	sem := syncx.NewSemaphore(5)
//	defer sem.Release() // 如果已 Acquire,defer 还
//
//	if err := sem.Acquire(ctx); err != nil { return err }
//	defer sem.Release()
package syncx

import (
	"context"
	"errors"
	"sync/atomic"
)

// ErrSemaphoreClosed 信号量已 close
var ErrSemaphoreClosed = errors.New("syncx: semaphore closed")

// Semaphore 计数信号量
type Semaphore struct {
	tokens   chan struct{}
	capacity int
	closed   atomic.Bool
	// done 关闭即"信号量已废弃"的广播:阻塞在 Acquire 的等待者靠它被唤醒。
	// 只用 closed 标志叫不醒任何人 —— 等待者已经挂起在 select 上了。
	done chan struct{}
}

// NewSemaphore 构造
//   cap 必须 >= 1
func NewSemaphore(cap int) *Semaphore {
	if cap < 1 {
		cap = 1
	}
	s := &Semaphore{
		tokens:   make(chan struct{}, cap),
		capacity: cap,
		done:     make(chan struct{}),
	}
	// 预填 token
	for i := 0; i < cap; i++ {
		s.tokens <- struct{}{}
	}
	return s
}

// Acquire 取一个槽位
//   - 阻塞直到拿到 / ctx 取消 / Close
func (s *Semaphore) Acquire(ctx context.Context) error {
	if s.closed.Load() {
		return ErrSemaphoreClosed
	}
	select {
	case <-s.tokens:
		// 拿了一个 token
		if s.closed.Load() {
			// 拿的瞬间被 close → 还回去
			s.releaseUnchecked()
			return ErrSemaphoreClosed
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		// Close 的广播:阻塞中的等待者统一从这里退出
		return ErrSemaphoreClosed
	}
}

// TryAcquire 非阻塞
//   - 拿到 → 返 true
//   - 拿不到 / closed → 返 false
func (s *Semaphore) TryAcquire() bool {
	if s.closed.Load() {
		return false
	}
	select {
	case <-s.tokens:
		if s.closed.Load() {
			s.releaseUnchecked()
			return false
		}
		return true
	default:
		return false
	}
}

// Release 还一个 token
//   - 多次 release 不会超过 capacity(防御性)
//   - Close 后 release no-op
func (s *Semaphore) Release() {
	if s.closed.Load() {
		return
	}
	s.releaseUnchecked()
}

// releaseUnchecked 内部用,不查 closed
func (s *Semaphore) releaseUnchecked() {
	select {
	case s.tokens <- struct{}{}:
	default:
		// 已满,丢弃
	}
}

// Close 关闭
//   - 后续 Acquire(含**阻塞中**的)返 ErrSemaphoreClosed
//   - Close 后 Release / TryAcquire no-op(信号量已废弃,token 无需归还)
//   - 重复 close no-op
func (s *Semaphore) Close() {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	// close(done) 才是真正唤醒阻塞者的动作。曾经这里是"清空 tokens channel"
	// —— 那叫不醒任何人:阻塞中的 Acquire 在等 channel 里**有东西可收**,
	// 把 token 拿走只会让它等得更久,ctx 不可取消时就是 goroutine 永久泄漏。
	close(s.done)
}

// Available 当前可用槽位(诊断用)
func (s *Semaphore) Available() int {
	return len(s.tokens)
}

// Capacity 总容量
func (s *Semaphore) Capacity() int {
	return s.capacity
}

// InUse 占用中
func (s *Semaphore) InUse() int {
	return s.capacity - len(s.tokens)
}
