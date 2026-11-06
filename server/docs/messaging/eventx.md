# eventx —— 领域事件总线 + 事务性 outbox

> 模块间"发事件"的框架原语,收口成三件:**发布 / 订阅**(进程内 Bus)、
> **事务性 outbox**(业务事务内落表,投递器轮询重试,耗尽进死信)、
> **消费侧幂等**(Deduper)。在它之前,发布者把
> `WithoutCancel + go func + recover` 这套三件套抄了两遍,且裸 goroutine
> 直推 Redis —— 业务成功了 Redis 抖一下事件就静默丢,没人重试、没有痕迹。
>
> ⚠️ 别和 [pushx](pushx.md)(手机离线推送)、[notifyx](notifyx.md) /
> [inbox](inbox.md)(有明确收件人的消息)搞混 —— eventx 是
> "某个业务动作 ⇒ 广播一个信号,谁关心谁订阅"。

## 功能

- **类型化事件总线**:`Name` 是独立类型(点号分段反向域名风格,如
  `approval.todo`),事件名在函数签名里可辨识;同步 `Publish`(等全部
  处理器返回,单处理器 panic 被隔离)/ 异步 `PublishAsync`(立即返回);
- **事务性 outbox**:业务事务内 `Enqueue` 落 `sys_event_outbox`,
  **commit 即"发布成功"**,Redis 死活不再影响这件事;投递器每秒轮询、
  指数退避重试(默认 8 次进死信,死信不清,留人排查);
- **消费侧幂等**:`Deduper` 同一 key 在 TTL 内只执行一次 fn
  (进程内 / Redis SETNX 两种实现);
- **三件套全仓唯一实现**:`eventx.Async`(`WithoutCancel + goroutine +
  recover`),fn 的 panic 不传播到调用方。

## outbox 怎么保证事件不丢

```
业务事务内 Enqueue(可传 tx,同生共死)→ sys_event_outbox 一行(pending)
    → Deliverer 轮询(1s,扫 pending 且 next_retry_at 到期)
    → DeliverFunc(装配期注入)→ 成功置 delivered(24h 后清)
                              ↘ 失败:指数退避 base*2^(n-1) 封顶 5m,
                                attempts ≥ 8 置 dead(永不自动清)
```

状态机只有 `pending → delivered / dead`,**没有 failed** —— 失败的行仍是
pending,靠 `attempts + next_retry_at` 表达"暂时发不出"。

两类错误**退避但不烧尝试次数**:`ErrTransportUnavailable`(通道死了 ≠
消息有问题)、外层 ctx 已死(停机窗口,下一轮接着投)。

## 使用方法

订阅 / 发布(片段出自 [examples/eventx/main.go](../../examples/eventx/main.go)):

```go
// 事件名约定:点号分段的反向域名风格(approval.todo / dict.changed)
const (
	evOrderPaid  eventx.Name = "order.paid"
	evOrderShips eventx.Name = "order.shipped"
)

	unsub := bus.Subscribe(evOrderPaid, func(_ context.Context, ev eventx.Event) {
		fmt.Printf("  [库存模块] 收到 %s(负载=%v)→ 回查订单正文再扣库存\n", ev.Name, ev.Payload)
	})

	fmt.Println("Publish(同步):")
	bus.Publish(ctx, eventx.Event{Name: evOrderPaid, ID: "order-1001", Payload: map[string]any{"order_id": 1001}})

	fmt.Println("PublishAsync(异步):")
	bus.PublishAsync(ctx, eventx.Event{Name: evOrderShips, Payload: map[string]any{"order_id": 1001}})
	time.Sleep(100 * time.Millisecond) // 等 demo 里的异步处理器跑完

	unsub()
	fmt.Println("取消订阅库存模块后,再发一次 ——")
	bus.Publish(ctx, eventx.Event{Name: evOrderPaid, Payload: map[string]any{"order_id": 1002}})
```

跑最小示例:

```bash
go run ./examples/eventx
```

生产装配(发布侧落 outbox + 失败退回直推,出自
[service/approval_publisher.go](../../internal/service/approval_publisher.go)):

```go
	_, err := eventx.Enqueue(context.WithoutCancel(ctx), p.outboxDB, eventx.OutboxMessage{
		EventName: evt,
		Topic:     push.TopicApproval,
		Payload:   payload,
	})
```

投递器在 `cmd/server/main.go` 装配:`DeliverFunc` 里通道不可用返回
`ErrTransportUnavailable`,配置 `DelivererConfig{}` 零值走默认,
`go outboxDeliverer.Run(ctx)` 起轮询。

## 关键纪律

- **事件是信号不是数据**:Payload 只放"够订阅方决定要不要去拉"的最小
  信息,业务正文一律由订阅方自己回查;
- **消费方必须容忍重复**:outbox 是 at-least-once(投出去和标记成功是
  两步,进程死在中间会重投),多实例无锁轮询还会各投一次 —— 有副作用的
  消费必须配 `Deduper` / 幂等键,别指望精确一次;
- **通道不可用必须返回 `ErrTransportUnavailable`** —— 混在普通错误里,
  Redis 一次宕机就把积压事件全送进死信;
- **Enqueue 的主键自己发、事务性靠传 tx**:不赋 `sonyflake.Next()` 第二行
  就撞唯一键;传 db 是独立小事务,传业务 tx 才同生共死;
- **剥取消链是刻意的**:`Async` 与落 outbox 都要 `WithoutCancel` ——
  防"业务成功了,事件却因为 HTTP ctx 超时没发出去";需要同步感知失败
  就别用 Async,直接同步调。
