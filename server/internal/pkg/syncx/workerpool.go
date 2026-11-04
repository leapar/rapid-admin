// Package syncx 并发原语库(§1.11 #7 P2)
//
// 目的:
//   - 业务高频模式抽出来(限流、并发上限、批量任务)
//   - 不依赖第三方库,纯 stdlib + golang.org/x/sync(已有)
//
// 提供:
//   - WorkerPool:固定大小 goroutine 池 + 任务队列
//   - Semaphore:计数信号量(Acquire / Release / TryAcquire)
//
// 用法:
//
//	// WorkerPool:批量任务并发上限
//	pool := syncx.NewWorkerPool(20)
//	defer pool.Close()
//	for _, job := range jobs {
//	    pool.Submit(func() { process(job) })
//	}
//
//	// Semaphore:某资源并发上限(如:同时跑 5 个长任务)
//	sem := syncx.NewSemaphore(5)
//	if err := sem.Acquire(ctx); err != nil { return err }
//	defer sem.Release()
package syncx

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// WorkerPool 固定大小 goroutine 池
//
// 关键设计:
//   - 任务队列满时 Submit 阻塞(背压;防爆内存)
//   - Close 优雅退出:等队列清空 / CloseNow 立即退出(丢弃剩余队列)
//   - 异常 panic 由 task 内部 recover;不传出去
//   - 无 lock-step 同步(不要等所有任务,Submit 是 fire-and-forget)
type WorkerPool struct {
	tasks chan func()
	stop  chan struct{} // CloseNow 触发,workers 立即退出
	wg    sync.WaitGroup
	// workers worker 数量,构造后不变(immutable)。
	//
	// ⚠️ **必须有这个字段**:曾经 Stats().Workers 直接写 `cap(p.tasks)`,那是
	// **队列缓冲大小**不是 worker 数 —— 默认构造(size=4, queue=64)会报"64 个
	// worker"。实际 worker 数在构造函数里 spawn 完就用完了,不存下来就只能靠
	// cap(p.tasks) 猜,而 cap(p.tasks) 猜出来的是另一回事。
	workers int
	// queueSize 队列缓冲容量,构造后不变。Stats 里单独暴露 ——
	// 以前它被错当成 Workers 报出去,现在归位。
	queueSize int

	// 状态
	closed  atomic.Bool // 已 Close(不可再 Submit)
	stopped atomic.Bool // CloseNow 已触发:未开始的任务一律丢弃(在 close(stop) 之前置位)

	// 统计
	stats poolStats
}

// poolStats 内部计数
type poolStats struct {
	// 累计提交成功
	submitted atomic.Int64
	// 累计已执行
	executed atomic.Int64
	// 累计 Submit 失败(已 close 后)
	rejected atomic.Int64
	// 累计 Submit 阻塞等待(队列满)
	blocked atomic.Int64
	// 当前 running goroutine 数
	running atomic.Int64
	// 当前队列长度(实时)
	queued atomic.Int64
}

// PoolConfig 构造选项
type PoolConfig struct {
	// Size worker 数量(必须 >= 1)
	Size int
	// QueueSize 任务队列缓冲(0 = 无缓冲,Submit 等 worker 接收)
	// 建议 >= Size × 4,避免频繁阻塞
	QueueSize int
}

// NewWorkerPool 构造一个固定大小 worker pool
//   - 默认 Size=4, QueueSize=64
func NewWorkerPool(size int, queueSize ...int) *WorkerPool {
	cfg := PoolConfig{Size: size, QueueSize: 64}
	if len(queueSize) > 0 && queueSize[0] >= 0 {
		cfg.QueueSize = queueSize[0]
	}
	if cfg.Size < 1 {
		cfg.Size = 4
	}
	p := &WorkerPool{
		tasks:     make(chan func(), cfg.QueueSize),
		stop:      make(chan struct{}),
		workers:   cfg.Size,
		queueSize: cfg.QueueSize,
	}
	for i := 0; i < cfg.Size; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// worker 主循环
func (p *WorkerPool) worker() {
	defer p.wg.Done()
	for {
		// 优先检查 stop 标志(CloseNow 路径)
		// 避免 select 在 stop + tasks 都 ready 时随机拉到 task
		if p.stopped.Load() {
			p.drainPending()
			return
		}
		select {
		case <-p.stop:
			// CloseNow 路径:不再接新任务,直接退出
			p.drainPending()
			return
		case task, ok := <-p.tasks:
			if !ok {
				return // tasks channel 关闭(Close 路径)
			}
			// 拿到 task 后再确认 stop(select 可能刚好在 close 的瞬间拉到 task)。
			//
			// 这一步是 CloseNow 能"丢弃剩余队列"的**唯一**保证,注释里写的
			// "避免 select 随机拉到 task"靠的其实是这一行 —— select 在两个 case
			// 同时 ready 时是均匀随机挑的,没有任何"优先级"。所以只要 stopped
			// 在 close(stop) 之前就被置上,随机拉到的那一批也会在这里被丢掉。
			//
			// ⚠️ 早先的写法是 close(p.stop) → wg.Wait() → stopped.Store(true),
			// 等于"等所有 worker 都退出了才置位",那这一行永远读到 false ——
			// 100 次里有几次 CloseNow 之后任务照样跑完。
			if p.stopped.Load() {
				p.stats.queued.Add(-1)
				p.drainPending()
				return
			}
			p.stats.running.Add(1)
			p.stats.queued.Add(-1)
			safeRun(task)
			p.stats.running.Add(-1)
			p.stats.executed.Add(1)
		}
	}
}

// drainPending 把队列里剩下的任务丢掉,并把 queued 计数同步归零。
//
// CloseNow 丢弃任务时不会走到 worker 主循环里的 `queued.Add(-1)`,不补这一步的话
// Stats().Queued 会在关闭后永远停在一个假数字上。
//
// 多个 worker 同时 drain 是安全的:channel 接收是独占的,每个任务被恰好一个 worker
// 取走、恰好减一次。
func (p *WorkerPool) drainPending() {
	for {
		select {
		case <-p.tasks:
			p.stats.queued.Add(-1)
		default:
			return
		}
	}
}

// safeRun 跑 task,recover panic
//
// ⚠️ **recover 不是"处理",是"止血"**。任务 panic 后池子必须活着(否则死掉
// 的是 worker goroutine,整个池少一个并发位,业务却什么都不知道),所以这里
// 一定 recover —— 但**必须留下痕迹**。
//
// 曾经这里是 `_ = recover()`,纯静默:崩掉的任务照样被 `executed.Add(1)` 计成
// "已执行",Stats 全绿,而任务其实死了。池子不会崩也不会卡,只是有一批任务
// 在业务侧永远不执行,监控上完全看不出来。
//
// logger.L() 在未初始化时返回 zap.NewNop()(见 pkg/logger/logger.go),所以这里
// 调它是 nil-safe 的 —— 包初始化顺序早于 logger.New 也不会出问题。
//
// ⚠️ 记 panic 的 **stack 而不是只有 message**:task 是匿名闭包,只有堆栈才
// 能定位到是哪个调用方的代码炸了。
func safeRun(task func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.L().Warn("syncx: worker task panicked",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
	}()
	task()
}

// Submit 投递任务
//   - 队列满 → 阻塞(背压)
//   - 已 close → 立即返 false(不阻塞)
func (p *WorkerPool) Submit(task func()) bool {
	if task == nil {
		return false
	}
	if p.closed.Load() {
		p.stats.rejected.Add(1)
		return false
	}
	// 先尝试非阻塞入队
	select {
	case p.tasks <- task:
		p.stats.queued.Add(1)
		p.stats.submitted.Add(1)
		return true
	default:
		// 队列满,阻塞
	}
	p.stats.blocked.Add(1)
	p.tasks <- task
	p.stats.queued.Add(1)
	p.stats.submitted.Add(1)
	// 阻塞 send 成功之后再复核一次 stopped:阻塞期间 CloseNow 可能已经触发 ——
	// 此时这个任务要么被 worker 丢弃、要么永远留在 channel 里(worker 已全部
	// 退出,不会再有人来取)。两种结局都执行不了,返 true 是骗调用方
	// "投递成功",submitted/queued 也会停在假数字上,所以回滚计数并返 false。
	// (worker 在丢弃路径自己会扣 queued;这里是池子已关闭、无人接手时的兜底,
	// 极端交错下 queued 可能 ±1 的瞬时偏差 —— 对一个已关闭的池子无实际影响。)
	if p.stopped.Load() {
		p.stats.queued.Add(-1)
		p.stats.submitted.Add(-1)
		p.stats.rejected.Add(1)
		return false
	}
	return true
}

// SubmitCtx 投递任务(ctx 取消则放弃)
//   - 队列空 + ctx cancel → 返 false
func (p *WorkerPool) SubmitCtx(ctx context.Context, task func()) bool {
	if task == nil {
		return false
	}
	if p.closed.Load() {
		p.stats.rejected.Add(1)
		return false
	}
	select {
	case p.tasks <- task:
		p.stats.queued.Add(1)
		p.stats.submitted.Add(1)
		return true
	case <-ctx.Done():
		p.stats.rejected.Add(1)
		return false
	}
}

// Close 优雅关闭:不再接受 Submit,等队列清空 + workers 退出
//   - 已 close 后再调 → no-op
//   - 阻塞直到所有 in-flight 任务完成
//
// ⚠️ **不要与 Submit 并发调用**。Close 会 close(p.tasks),而一个正阻塞在
// `p.tasks <- task`(队列满)上的 Submit 会 panic:send on closed channel。
// 这是"关池子之后再投递就是 bug"的**响亮**违约方式 —— 宁可当场炸,也不要
// 静默吞掉一个任务。
func (p *WorkerPool) Close() {
	if p.closed.Swap(true) {
		return
	}
	close(p.tasks) // 关闭后 worker 读完队列会退出
	p.wg.Wait()
	// 这里**不要**置 stopped:它是 CloseNow 的"丢弃剩余队列"开关,Close 的语义是
	// 跑完为止。wg.Wait() 返回时 worker 全退了,那一行本来就是死的 —— 但留着会让
	// 下一个人以为 Close 也在走丢弃路径,照着把它挪到 close(p.tasks) 之前,
	// 优雅关闭就静默变成"只跑一半"。
}

// CloseNow 立即关闭:不接新任务,丢弃剩余队列,等 in-flight 完成
//   - 比 Close 快,适合 SIGTERM / shutdown
//
// 顺序要紧:stopped 必须**在** close(p.stop) 之前置上。反过来的话,worker 在
// select 里随机拉到任务后那次 `stopped.Load()` 读到的是 false,任务照跑 ——
// "立即丢弃剩余队列"就成了十次里错两三次的承诺。
func (p *WorkerPool) CloseNow() {
	if p.closed.Swap(true) {
		return
	}
	p.stopped.Store(true)
	close(p.stop) // workers 立即退出,不再接任务
	p.wg.Wait()
}

// PoolStats 状态快照
type PoolStats struct {
	Submitted int64 `json:"submitted"`
	Executed  int64 `json:"executed"`
	Rejected  int64 `json:"rejected"`
	// ⚠️ **只统计 Submit 路径的阻塞**(队列满后 `p.tasks <- task` 挂住)。
	// SubmitCtx 也会阻塞,但它不计数 —— 见该函数说明:补这个计数要改
	// "ctx 取消 vs 有空位同时就绪"时的判定方向,为一个尚无调用方的 API
	// 改那个语义不值当。**别把这个字段当成"所有阻塞等待次数"读。**
	Blocked int64 `json:"blocked"`
	Running int64 `json:"running"`
	Queued  int64 `json:"queued"`
	Closed  bool  `json:"closed"`
	// Workers 真实 worker 数(构造时的 size),**不是**队列缓冲大小
	Workers int `json:"workers"`
	// QueueSize 队列缓冲容量,单独一个字段 —— 以前 Workers 报的就是它
	QueueSize int `json:"queue_size"`
}

// Stats 拉取当前状态
func (p *WorkerPool) Stats() PoolStats {
	return PoolStats{
		Submitted: p.stats.submitted.Load(),
		Executed:  p.stats.executed.Load(),
		Rejected:  p.stats.rejected.Load(),
		Blocked:   p.stats.blocked.Load(),
		Running:   p.stats.running.Load(),
		Queued:    p.stats.queued.Load(),
		Closed:    p.closed.Load(),
		Workers:   p.workers,
		QueueSize: p.queueSize,
	}
}
