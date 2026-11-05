# syncx —— 并发原语:计数信号量 + 固定大小工作池

> 限并发、隔离 goroutine 数量这类"每个后台都要重写一遍"的东西,抽成两个原语。
> syncx 只有两个类型:`Semaphore`(限**同时进行中**的数)和 `WorkerPool`(固定 worker + 有界队列)。

## 功能

syncx 提供两件东西,只用 stdlib(并发原语本身)+ zap(打日志),不引第三方库:

1. **`Semaphore`** —— 计数信号量
   - `Acquire(ctx)` 阻塞取槽位 / `TryAcquire()` 非阻塞 / `Release()` 还槽位
   - 内部是预填 N 个 token 的 buffered chan,比手写 channel 少一个"忘记 release"的坑
2. **`WorkerPool`** —— 固定大小 goroutine 池
   - N 个 worker + 一个**有界**队列
   - 队列满时 `Submit` **阻塞**(背压,防把内存吃光);`SubmitCtx` 在 ctx 取消时放弃
   - `Close` 优雅(跑完队列再退)/ `CloseNow` 立即(丢弃剩余队列)

**不解决什么**:单飞(singleflight)在 [cachex](../storage/cachex.md) 里;
按时间窗口的限流在 `ratelimitx`;定时触发在 `cronx` —— syncx 只管"并发数",不管"什么时候跑"。

### 接线状态(读源码前先看这个)

| 原语 | 生产调用方 |
|---|---|
| `Semaphore` | `cachex` 的启动预热限流(`internal/pkg/cachex/warmer.go`) |
| `WorkerPool` | **零** —— 只有 `examples/syncx` 在用 |

框架层原语"先落地、后接线"是既定顺序(见 `internal/pkg/cachex/memory_backend.go`
的同一段说明)。`WorkerPool` 目前是**零生产调用方**的完整实现 —— 写业务时可以直接用,
但别假设它在框架里已经跑着。

## 配置

### 代码侧(构造选项)

```go
syncx.NewSemaphore(cap int) *Semaphore
syncx.NewWorkerPool(size int, queueSize ...int) *WorkerPool
```

| 参数 | 默认 | 说明 |
|---|---|---|
| `NewSemaphore(cap)` | `cap < 1` 时**静默改成 1** | 槽位总数。不报错,写 `0` 不会 panic,但也拿不到"零并发" |
| `NewWorkerPool(size)` | `size < 1` 时**静默改成 4** | worker 数量 |
| `NewWorkerPool(size, queueSize)` | `64` | 任务队列缓冲。`0` = 无缓冲,`Submit` 直接等 worker 来接 |

`PoolConfig{Size, QueueSize}` 这个结构体**是导出的但没有任何构造函数接受它** ——
`NewWorkerPool` 走的是位置参数。想用这个结构体得自己传进去。

### yaml 侧

**该包无 yaml 配置。** 并发上限是代码里的事(每个调用点的合理值不一样),
没有全局开关。

## 使用方法

### Semaphore:限同时进行数

```go
sem := syncx.NewSemaphore(2)
defer sem.Close()

fmt.Printf("容量 %d,当前空闲 %d\n", sem.Capacity(), sem.Available())

if err := sem.Acquire(ctx); err != nil {
    return err
}
defer sem.Release()

// 不想排队就非阻塞拿
if sem.TryAcquire() {
    defer sem.Release()
}
```

`Acquire` 拿不到时返回 `ctx.Err()`;信号量已 `Close` 时返回 `syncx.ErrSemaphoreClosed`。

### WorkerPool:固定 worker + 有界队列

```go
pool := syncx.NewWorkerPool(2, 4)
defer pool.Close()

for _, job := range jobs {
    pool.Submit(func() { process(job) })   // 队列满 → 这里阻塞
}

// "拿不到就放弃"用 SubmitCtx
if !pool.SubmitCtx(ctx, func() { process(job) }) {
    // ctx 取消了,这个任务被丢弃 —— 调用方要自己兜底
}
```

### 看状态

```go
st := pool.Stats()
// st.Submitted / Executed / Rejected / Blocked / Running / Queued
// st.Workers 是真实 worker 数,st.QueueSize 是队列缓冲容量
```

## 注意事项

- ⚠️ **`WorkerPool.Submit` 在队列满时是阻塞的,不是 fire-and-forget**。
  1 个 worker + 1 个队列位连续投 5 个任务,调用方会被卡住约 4 个任务的执行时间
  (示例里专门演示了这一段)。要"塞不进去就跳过"必须用 `SubmitCtx`。
- ⚠️ **`WorkerPool.Close()` 不能与 `Submit` 并发调用**。`Close` 会 `close(p.tasks)`,
  而一个正阻塞在 `p.tasks <- task`(队列满)上的 `Submit` 会 panic:
  `send on closed channel`。这是"关池子之后再投递就是 bug"的**响亮**违约方式。
- ⚠️ **`Semaphore.Close()` 不会唤醒阻塞在 `Acquire` 上的 goroutine**。
  源码注释写的是"清空 channel(让阻塞的 Acquire 退出)",但阻塞中的 `Acquire`
  意味着 channel 是空的,清空它是空操作;而 `Close` 之后 `Release` 变成 no-op,
  token 再也回不来 ⇒ 那个 goroutine **只能靠 ctx 取消**才能退出。
  传进来的 ctx 一定要带超时。
- ⚠️ **`Semaphore.Close()` 之后再 `Release()` 是 no-op**,槽位永远回不来;
  此时 `Available()` 恒为 0、`InUse()` 恒等于 `Capacity()` —— Close 之后这两个诊断值没有意义。
- ⚠️ **`PoolStats.Executed` 统计的是"跑完的任务",不是"成功的任务"**。
  `safeRun` 会 recover 掉 panic(不让池子少一个并发位),但 recover 之后
  `executed` 照样 +1。任务崩了指标是绿的 —— 唯一的痕迹是
  `syncx: worker task panicked` 这条 warn 日志(带 stack)。
- ⚠️ **`PoolStats.Blocked` 只统计 `Submit` 路径的阻塞**,`SubmitCtx` 的阻塞不计数。
  别把它当"所有阻塞等待次数"读。
- ⚠️ **`SubmitCtx` 在"队列有空位"和"ctx 已取消"同时成立时,可能照样把任务塞进去** ——
  Go 的 `select` 在多个 case 同时 ready 时是**均匀随机**挑的,没有优先级。
  想严格保证"ctx 取消就不投",`SubmitCtx` 的返回值必须检查。
- ⚠️ **`Submit(nil)` 返回 false 但不计入 `Rejected`** —— 它在计数之前就返回了。
- ⚠️ **`CloseNow()` 与 `Close()` 的差别不只是快慢**:`CloseNow` 会**丢弃**队列里
  还没开始的任务,`Close` 会跑完。源码里 `stopped` 标志必须在 `close(p.stop)`
  **之前**置位,否则 select 随机拉到的那批任务照样会跑完 ——
  "立即丢弃剩余队列"就成了十次里错两三次的承诺。
- ⚠️ **`WorkerPool` 没有 lock-step 同步**。`Submit` 是 fire-and-forget,
  包里**没有**"等所有任务跑完"的 API(只有 `Close` 会等)。要等结果得自己用 `sync.WaitGroup`。

## 相关

- 源码:`internal/pkg/syncx/`(`semaphore.go` / `workerpool.go`)
- 可运行示例:`examples/syncx/main.go` —— `go run ./examples/syncx`
- `Semaphore` 的唯一生产用法(启动预热限流)→ [cachex](../storage/cachex.md)
- 框架原语的使用约定 → [框架约定](../getting-started/conventions.md)
