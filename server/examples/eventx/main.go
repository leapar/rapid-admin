// eventx —— 领域事件总线 + 事务性 outbox + 消费侧幂等
//
// ## 这个包解决什么问题
//
// 跨模块通知此前是"context.WithoutCancel + go func() + recover()"手抄两遍,
// 裸 goroutine 直推下游 —— 业务成功了,下游抖一下事件就静默丢了。
// eventx 收口成三个原语:
//
//  1. 进程内 Bus:类型化事件名,发布 / 订阅
//  2. 事务性 outbox:业务事务内落表,投递器轮询重试(见 outbox.go / Deliverer)
//  3. Deduper:同一事件 TTL 内只消费一次(见 dedupe.go)
//
// ## 关键纪律
//
// 事件是信号不是数据:Payload 只放"够订阅方决定要不要去拉"的最小信息,
// 业务正文一律由订阅方自己回查。
//
// ## 跑法
//
//	go run ./examples/eventx
package main

import (
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/eventx"
)

// 事件名约定:点号分段的反向域名风格(approval.todo / dict.changed)
const (
	evOrderPaid  eventx.Name = "order.paid"
	evOrderShips eventx.Name = "order.shipped"
)

func main() {
	ctx := context.Background()
	bus := eventx.NewBus()

	// 一、订阅:返回取消订阅的函数(幂等,可重复调用)
	unsub := bus.Subscribe(evOrderPaid, func(_ context.Context, ev eventx.Event) {
		fmt.Printf("  [库存模块] 收到 %s(负载=%v)→ 回查订单正文再扣库存\n", ev.Name, ev.Payload)
	})
	bus.Subscribe(evOrderPaid, func(_ context.Context, ev eventx.Event) {
		fmt.Printf("  [通知模块] 收到 %s → 发一条站内信\n", ev.Name)
	})

	// 二、同步发布:按注册顺序调用处理器
	fmt.Println("Publish(同步):")
	bus.Publish(ctx, eventx.Event{Name: evOrderPaid, ID: "order-1001", Payload: map[string]any{"order_id": 1001}})

	// 三、异步发布:panic 被隔离,不炸主流程
	fmt.Println("PublishAsync(异步):")
	bus.PublishAsync(ctx, eventx.Event{Name: evOrderShips, Payload: map[string]any{"order_id": 1001}})
	time.Sleep(100 * time.Millisecond) // 等 demo 里的异步处理器跑完

	// 四、取消订阅:之后的事件不再送达这个处理器
	unsub()
	fmt.Println("取消订阅库存模块后,再发一次 ——")
	bus.Publish(ctx, eventx.Event{Name: evOrderPaid, Payload: map[string]any{"order_id": 1002}})

	// 五、跨进程 / 事务性场景:用 Enqueue(事务内落 sys_event_outbox 表,
	//    投递器 Deliverer 轮询重试、耗尽进死信)+ NewMemoryDeduper(消费幂等)。
	//    那两个原语需要真实 DB / Redis,这里不展开,见包注释。
	fmt.Println("\n事务性 outbox / 消费幂等:见 eventx 包注释与 outbox.go / dedupe.go")
}
