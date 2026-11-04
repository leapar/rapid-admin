// Package eventx 领域事件总线 + 事务性 outbox。
//
// # 这个包解决什么问题
//
// 模块之间"发事件"此前没有框架原语:跨模块只能直接 import service 硬调,
// 而仅有的两个手写发布者(approval_publisher / dict_publisher)把同一套
// 三件套(context.WithoutCancel + go func() + recover())**抄了两遍**,
// 且都是裸 goroutine 直推 Redis —— 业务成功了,Redis 抖一下事件就静默丢了,
// 没有人重试,也没有任何痕迹。
//
// 本包把这件事收口成三个原语,**每个只写一次**:
//
//  1. **发布 / 订阅**(本文件):类型化事件名 + 进程内 Bus,
//     异步发布的三件套收口在 Async / Bus.PublishAsync 里。
//  2. **事务性 outbox**(outbox.go + Deliverer):业务事务内落表
//     (model.SysEventOutbox),投递器轮询、按退避重试、耗尽进死信。
//     "发布成功"的定义从"推到了 Redis"变成"commit 了一行"。
//  3. **消费侧幂等**(dedupe.go):Deduper 原语,同一事件在 TTL 内
//     只消费一次。
//
// # 依赖方向
//
// eventx 不认识任何业务,也不认识 pkg/push —— 它只把行投给装配期注入的
// DeliverFunc。pkg/push(SSE 传输层)在 eventx 的下游:main 装配期把
// "投一条 push 事件"作为 DeliverFunc 传进来。反过来不成立。
//
// ⚠️ **别和 pkg/pushx 搞混**(那是给手机的离线推送),也**别和
// pkg/notifyx / pkg/inbox 混**(那是"某个业务动作 ⇒ 推一条消息给某个人",
// 有明确的收件人)。本包是"某个业务动作 ⇒ 广播一个信号,谁关心谁订阅"。
package eventx

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// Name 类型化事件名。约定用点号分段的反向域名风格(与 pkg/push 一致):
// approval.todo / dict.changed / job.finished。
//
// 用独立类型而不是裸 string,是为了让"事件名"在函数签名里可辨识 ——
// 拿错两个字符串参数编译器帮不上忙,拿错两个 Name 一眼能看出。
type Name string

// Event 一条领域事件。
type Event struct {
	// Name 事件名
	Name Name
	// ID 事件唯一标识(可选)。消费侧幂等的去重键;空 = 发布方不提供,
	// 消费方用"内容自己判重"或不去重。
	ID string
	// Payload 事件载荷。⚠️ 事件是信号不是数据:只放"够订阅方决定要不要
	// 去拉"的最小信息,业务正文一律由订阅方自己回查(与 pkg/push 同一条纪律)
	Payload any
}

// Handler 事件处理器。panic 会被 Bus 隔离(见 Publish),但别依赖这个 ——
// 处理器内部该自己兜住可预期的错误。
type Handler func(ctx context.Context, ev Event)

// Bus 进程内事件总线。
//
// 线程安全。订阅关系在运行期可增删;发布时按注册顺序同步调用处理器。
type Bus struct {
	mu   sync.RWMutex
	subs map[Name][]Handler
}

// NewBus 构造一个空的总线。
func NewBus() *Bus {
	return &Bus{subs: make(map[Name][]Handler)}
}

// Subscribe 订阅某事件名,返回取消订阅的函数(幂等,可重复调用)。
func (b *Bus) Subscribe(name Name, h Handler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[name] = append(b.subs[name], h)
	idx := len(b.subs[name]) - 1
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		hs := b.subs[name]
		if idx >= len(hs) {
			return
		}
		// 置 nil 而不是切片重排:保序 + 简单。发布时跳过 nil。
		hs[idx] = nil
		b.subs[name] = hs
	}
}

// Publish 同步发布:逐个调用订阅了该事件名的处理器,**等全部返回才返回**。
//
// 单个处理器 panic 被隔离(记日志),不影响其余处理器,也不外抛 ——
// 发布方不该因为"某个订阅方写崩了"而失败。
func (b *Bus) Publish(ctx context.Context, ev Event) {
	for _, h := range b.handlers(ev.Name) {
		runHandler(ctx, h, ev)
	}
}

// PublishAsync 异步发布:在独立 goroutine 里调用全部处理器,立即返回。
//
// 三件套(context.WithoutCancel + goroutine + recover)收口在 Async,
// 这里只是它的一个具体用法。ctx 已取消(如 HTTP 请求已超时)也不影响
// 处理器执行 —— 处理器拿到的是剥过取消链的活 ctx。
func (b *Bus) PublishAsync(ctx context.Context, ev Event) {
	handlers := b.handlers(ev.Name)
	if len(handlers) == 0 {
		return
	}
	Async(ctx, string(ev.Name), func(ctx context.Context) {
		for _, h := range handlers {
			runHandler(ctx, h, ev)
		}
	})
}

// handlers 取某事件名的处理器快照(跳过已取消订阅的 nil 槽位)。
func (b *Bus) handlers(name Name) []Handler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	src := b.subs[name]
	out := make([]Handler, 0, len(src))
	for _, h := range src {
		if h != nil {
			out = append(out, h)
		}
	}
	return out
}

// runHandler 带 recover 地调用一个处理器。
func runHandler(ctx context.Context, h Handler, ev Event) {
	defer func() {
		if r := recover(); r != nil {
			logger.L().Error("eventx: 事件处理器 panic,已隔离",
				zap.String("event", string(ev.Name)),
				zap.Any("panic", r))
		}
	}()
	h(ctx, ev)
}

// Async 在独立 goroutine 里跑 fn —— **三件套的唯一实现,全仓只此一处**。
//
//	WithoutCancel:调用方的 ctx 可能已经死了(HTTP 请求超时 / 上游断开)。
//	不剥取消链的话,fn 拿着已取消的 ctx 去做 IO 会直接失败,
//	表现是"业务成功了但事件没发出去"。
//	(context.WithoutCancel 是幂等的,外面已经剥过再剥一层没有代价。)
//
//	goroutine:投递是网络 IO,不能挂在业务请求的关键路径上。
//
//	recover:goroutine 里逃出去的 panic 会**打死整个进程**。
//	一个"提醒别人刷新"的旁路,不该有能力把服务带走。
//
// name 只用于 panic 日志定位(传事件名或调用点说明都行)。
//
// ⚠️ fn 里的 panic 不会传播到调用方 —— 这是刻意的:需要同步感知失败的话,
// 别用 Async,直接同步调用。
func Async(ctx context.Context, name string, fn func(ctx context.Context)) {
	safeCtx := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.L().Error("eventx: 异步任务 panic,已隔离",
					zap.String("event", name),
					zap.Any("panic", r))
			}
		}()
		fn(safeCtx)
	}()
}
