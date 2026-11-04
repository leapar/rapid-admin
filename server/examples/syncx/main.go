// syncx —— 并发原语
//
// ## 这个包解决什么问题
//
// 两种"限制并发"的需求:
//
//   - Semaphore:信号量 —— 限制**同时进行中**的任务数(比如同时最多 5 个导出)
//   - WorkerPool:工作池 —— 固定 N 个 worker + 有界队列(把无限 goroutine 变成有界)
//
// ## 关键点
//
//   - WorkerPool 的队列**有界**:满了 Submit 会**阻塞**(不是无限攒 goroutine
//     把内存吃光);要"拿不到就放弃"用 SubmitCtx(ctx 取消即返 false)
//   - Submit 只在两种情况返 false:池子已关 / task 为 nil
//   - PoolStats 里 Blocked **只统计 Submit 路径的阻塞**,不是所有阻塞等待
//     —— 别把这个字段当"所有阻塞等待次数"读
//
// ## 跑法
//
//	go run ./examples/syncx
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"rapid-admin/internal/pkg/syncx"
)

func main() {
	// ── 一、信号量:限制并发数 ──────────────────────────────────
	fmt.Println("=== 信号量(容量 2)===")
	sem := syncx.NewSemaphore(2)
	fmt.Printf("  容量 %d,当前空闲 %d\n", sem.Capacity(), sem.Available())

	var wg sync.WaitGroup
	var running, maxRunning int64
	for i := 1; i <= 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = sem.Acquire(context.Background())
			defer sem.Release()

			cur := atomic.AddInt64(&running, 1)
			for {
				old := atomic.LoadInt64(&maxRunning)
				if cur <= old || atomic.CompareAndSwapInt64(&maxRunning, old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&running, -1)
		}(i)
	}
	wg.Wait()
	fmt.Printf("  6 个任务跑完,峰值并发 = %d(被信号量压在 2)\n", maxRunning)

	// TryAcquire:拿不到就立刻返回,不排队
	fmt.Println()
	fmt.Println("=== TryAcquire ===")
	sem2 := syncx.NewSemaphore(1)
	fmt.Printf("  第一次 = %v\n", sem2.TryAcquire())
	fmt.Printf("  第二次 = %v(拿不到,立刻返回)\n", sem2.TryAcquire())
	sem2.Release()

	// ── 二、工作池:固定 worker + 有界队列 ──────────────────────
	fmt.Println()
	fmt.Println("=== 工作池(2 个 worker,队列 4)===")
	pool := syncx.NewWorkerPool(2, 4)
	defer pool.Close()

	var done int64
	for i := 1; i <= 5; i++ {
		ok := pool.Submit(func() {
			atomic.AddInt64(&done, 1)
			time.Sleep(30 * time.Millisecond)
		})
		fmt.Printf("  提交第 %d 个任务 → %v\n", i, ok)
	}
	time.Sleep(200 * time.Millisecond)
	fmt.Printf("  已执行 %d 个\n", atomic.LoadInt64(&done))
	fmt.Printf("  池状态: %+v\n", pool.Stats())

	// 队列满时 Submit **阻塞**(不是拒绝)—— 调用方要考虑这个背压
	fmt.Println()
	fmt.Println("=== 队列满时阻塞 ===")
	pool2 := syncx.NewWorkerPool(1, 1)
	defer pool2.Close()
	start := time.Now()
	for i := 0; i < 5; i++ {
		pool2.Submit(func() { time.Sleep(100 * time.Millisecond) })
	}
	fmt.Printf("  提交 5 个任务(1 个 worker + 1 个队列位),耗时 %v ⇒ 后面的在等\n",
		time.Since(start).Round(10*time.Millisecond))
	fmt.Printf("  池状态: %+v\n", pool2.Stats())

	// 想要"拿不到就放弃"用 SubmitCtx —— ctx 取消即返 false
	fmt.Println()
	fmt.Println("=== SubmitCtx:ctx 取消即放弃 ===")
	pool3 := syncx.NewWorkerPool(1, 1)
	defer pool3.Close()
	for i := 0; i < 3; i++ {
		pool3.Submit(func() { time.Sleep(300 * time.Millisecond) })
	}
	shortCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	ok := pool3.SubmitCtx(shortCtx, func() {})
	fmt.Printf("  队列满 + ctx 超时 → SubmitCtx 返 %v(任务被丢弃,调用方要自己兜底)\n", ok)
	fmt.Printf("  池状态: %+v\n", pool3.Stats())
}
