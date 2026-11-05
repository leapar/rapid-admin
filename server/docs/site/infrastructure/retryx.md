# retryx —— 指数退避重试(带 full jitter)

> 调外部服务(OSS / 短信 / 第三方 API)时偶发失败是常态。
> retryx 把"退避 + 重试"标准化,顺手统一了"什么错误值得重试"。

## 功能

retryx 做一件事:包一个可能失败的函数,失败时按**指数退避 + 抖动**重试。

1. **指数退避**:第 1 次等 `InitialBackoff`,之后每次 ×`Multiplier`,封顶 `MaxBackoff`
2. **full jitter**:实际等待 = `uniform(0, backoff)` —— 避免大量客户端在同一刻一起重试
   (惊群 / thundering herd,AWS 那篇论文的推荐做法)
3. **显式重试条件**:`RetryIf` 由业务决定哪些 error 值得重试
4. **有界**:只按 `MaxAttempts` 封顶;ctx 一取消立刻退出,不浪费工作
5. **可选埋点**:`OnRetry` 每次重试前调一次,用来打日志 / 埋点

**不解决什么**:不负责幂等。重试一个**非幂等**的写操作(POST 创建订单)可能产生
重复数据 —— 幂等要靠 [idempotentx](../security/idempotentx.md) 或业务自己的
唯一键,retryx 只管"再试一次"。

## 配置

### 代码侧(构造选项)

```go
retryx.Do(ctx context.Context, opts Opts, fn func(ctx context.Context) error) error
```

| `Opts` 字段 | 默认 | 说明 |
|---|---|---|
| `MaxAttempts` | `1` | **总**尝试次数(含第一次)。`< 1` 会被改成 1(即不重试) |
| `InitialBackoff` | `100ms` | 第 1 次重试前的等待 |
| `MaxBackoff` | `10s` | 单次等待上限(指数曲线的封顶) |
| `Multiplier` | `2` | 退避倍数。**`<= 1` 会被改成 2** |
| `RetryIf` | `IsRetryable` | 返回 true 才重试。`nil` = 除了 ctx 错误**全都重试** |
| `OnRetry` | 默认 warn 日志 | 每次重试前调:`func(attempt int, err error, nextBackoff time.Duration)` |
| `Logger` | `logger.Default` | 自定义 zap logger;`nil` 用默认 |

### yaml 侧

**该包无 yaml 配置。** 重试策略是**每个调用点**的事(OSS 覆盖写和第三方
下单 API 的容忍度完全不同),没有全局开关。

## 使用方法

```go
err := retryx.Do(ctx, retryx.Opts{
    MaxAttempts:    4,
    InitialBackoff: 20 * time.Millisecond,
    MaxBackoff:     200 * time.Millisecond,
    Multiplier:     2,
    RetryIf: func(err error) bool {
        // 只有"上游临时不可用"才值得重试
        return errors.Is(err, errTemporary)
    },
    OnRetry: func(attempt int, err error, nextBackoff time.Duration) {
        log.Warn("重试中", zap.Int("attempt", attempt), zap.Error(err))
    },
}, func(ctx context.Context) error {
    return doSomething(ctx)
})
```

框架里的实际用法:本地磁盘存储的 rename 覆盖(Windows 上读方开着句柄会
`EPERM`),见 `internal/pkg/oss/local/local.go`。

## 注意事项

- ⚠️ **`RetryIf` 留空 = 所有错误都重试,包括 4xx 参数错误**。
  `IsRetryable` 只排除 `context.Canceled` 和 `context.DeadlineExceeded`,
  业务错误(参数不合法、鉴权失败)照样会被重试 `MaxAttempts` 次。
  **写业务时永远显式给 `RetryIf`**。
- ⚠️ **重试非幂等的写操作会重复执行**。`Do` 只保证"函数被调用了 N 次",
  不保证"副作用只发生一次"。POST 创建这类操作要么带上幂等键
  ([idempotentx](../security/idempotentx.md)),要么把 `RetryIf` 收紧到
  "只在明确没写进去的时候重试"。
- ⚠️ **`Multiplier` 配 1 会被静默改成 2**,配 0 也是。想"每次等固定时间"
  (不翻倍)做不到 —— 只能把 `MaxBackoff` 设成和 `InitialBackoff` 一样,
  让它在第一次就封顶。
- ⚠️ **full jitter 的实际等待是 `uniform(0, backoff)`**,平均只有名义值的一半;
  而且**可能刚好是 0**。别拿 `InitialBackoff` 当 SLA 算总耗时,
  也别指望它一定"等了" —— `InitialBackoff` 很小的时候可能连着几次零等待,
  变成一个紧凑的重试循环。
- ⚠️ **`OnRetry` 只在"真的要重试"时调** —— 即失败次数 < `MaxAttempts`。
  最后一次失败**不会**触发 `OnRetry`,所以回调次数是 `MaxAttempts - 1`。
  `attempt` 是**刚刚失败的那次**的序号(1 起)。
- ⚠️ **`Opts.Logger` 默认取 `logger.Default`,它在 `logger.New` 之前是 `nil`**。
  nil 是安全的(回调直接返回、不打日志),但意味着**进程刚起来、
  logger 还没初始化那段时间里的重试风暴是完全静默的**。
- ⚠️ **包注释提到 `MaxElapsed`,但 `Opts` 里没有这个字段**。
  总耗时只有 `MaxAttempts × MaxBackoff` 这个上界,没有独立的时限闸门 ——
  要时限就自己给 `ctx` 加 `context.WithTimeout`。
- ⚠️ **`Do` 不给每次尝试单独的 timeout**,`fn` 收到的就是外层 ctx。
  `fn` 自己卡住 ⇒ 整个重试预算卡住。
- ⚠️ **`Do` 返回的是最后一次的错误**(ctx 取消时是 `errors.Join(lastErr, ctx.Err())`),
  中间几次的错误不会出现在返回值里 —— 要看全部错误得自己在 `fn` 里记。

## 相关

- 源码:`internal/pkg/retryx/`
- 可运行示例:`examples/retryx/main.go` —— `go run ./examples/retryx`
- 重试的前提是幂等 → [idempotentx](../security/idempotentx.md)
- 唯一生产调用点(本地存储 rename)→ [oss](../storage/oss.md)
