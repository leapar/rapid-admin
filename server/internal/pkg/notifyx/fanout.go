// Fanout —— 站内信 + 额外交付通道 的组合投递器
//
// ⚠️ **它实现的是 `inbox.Notifier`** —— 所以装配处只要把它塞进原来
//
//	  `inboxMgr` 的位置,**所有既有的站内信投递自动多出一条通道**,
//	  而调用方一行都不用改。
//
//		装配前:registry.InboxNotifier = inboxMgr
//		装配后:registry.InboxNotifier = notifyx.Fanout{inner: inboxMgr, ...}
//
// 三条语义(与 §1.9 的整体口径一致):
//
//  1. **站内信是"记录",额外交付是"送达"** —— 站内信写库失败 = 失败(返 error);
//     邮件/推送失败 = 送达失败(只记日志,不返 error)。
//     两者混成一个返回值,调用方就没法区分"没发出去"和"发出去但没收到"。
//
//  2. **额外交付异步** —— 邮件是一次网络往返(SMTP 可能好几秒),
//     它不该挂在 HTTP 响应的关键路径上。站内信那一次 DB 写是快的,留在同步。
//
//  3. **异步投递脱离请求 ctx** —— 请求超时后 ctx 已经死了,
//     拿它发邮件必然失败,表现是"审批成功了但收件人没收到邮件"。
//     用 context.WithoutCancel + 独立 deadline。
package notifyx

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/logger"
)

// extraDeliveryTimeout 额外交付(邮件 / 推送)的单次 deadline
//
// ⚠️ **不是 3s** —— 站内信是写库,3s 够;邮件要跟外网 SMTP 握手 + 传输,
//
//	3s 会把正常投递判成超时。30s 与 emailx / pushx 自己的默认超时同量级。
const extraDeliveryTimeout = 30 * time.Second

// Fanout 组合投递器
type Fanout struct {
	// inner 站内信投递(必填 —— 它是"记录")
	inner inbox.Notifier
	// dispatcher 额外交付通道(可空 = 只有站内信)
	dispatcher *Dispatcher
	// resolver UserID → 联系方式(可空 = 不投递)
	resolver Resolver

	// async 额外交付是否异步(默认 true)
	//
	// ⚠️ **只在测试里关掉** —— 生产的异步投递让 HTTP 响应不必等一封邮件。
	async bool
	// wg 跟踪异步投递(测试用它等投递结束)
	wg sync.WaitGroup
}

var _ inbox.Notifier = (*Fanout)(nil)

// NewFanout 构造(额外交付异步)
func NewFanout(inner inbox.Notifier, dispatcher *Dispatcher, resolver Resolver) *Fanout {
	return &Fanout{
		inner:      inner,
		dispatcher: dispatcher,
		resolver:   resolver,
		async:      true,
	}
}

// NewSyncFanout 构造(额外交付同步)—— **只给测试用**
//
// 生产路径一律用 NewFanout:同步投递会把一次 SMTP 往返挂在调用方的
// 关键路径上,而调用方可能正处在 HTTP 请求里。
func NewSyncFanout(inner inbox.Notifier, dispatcher *Dispatcher, resolver Resolver) *Fanout {
	return &Fanout{
		inner:      inner,
		dispatcher: dispatcher,
		resolver:   resolver,
		async:      false,
	}
}

// Wait 等所有异步额外交付结束(测试用)
func (f *Fanout) Wait() { f.wg.Wait() }

// Notify 投一条:站内信(同步) + 额外交付(异步)
//
// ⚠️ **站内信失败就返 error,额外交付失败不返** —— 见文件头的语义 1。
func (f *Fanout) Notify(ctx context.Context, msg inbox.Message) error {
	if f.inner != nil {
		if err := f.inner.Notify(ctx, msg); err != nil {
			return err
		}
	}
	f.dispatchExtra(ctx, msg)
	return nil
}

// NotifyBatch 批量投:站内信同事务 + 额外交付逐条
//
// ⚠️ **站内信批量是"要么全成要么全败"**(inbox.Manager 用一个事务),
//
//	额外交付是逐条尽力而为 —— 一封邮件发不出去不该让另外十九封也不发。
func (f *Fanout) NotifyBatch(ctx context.Context, msgs []inbox.Message) error {
	if f.inner != nil {
		if err := f.inner.NotifyBatch(ctx, msgs); err != nil {
			return err
		}
	}
	for _, msg := range msgs {
		f.dispatchExtra(ctx, msg)
	}
	return nil
}

// dispatchExtra 触发额外交付
func (f *Fanout) dispatchExtra(ctx context.Context, msg inbox.Message) {
	if f.dispatcher == nil || f.resolver == nil {
		return
	}
	if len(f.dispatcher.Channels()) == 0 {
		return
	}
	if !f.async {
		f.deliverExtra(ctx, msg)
		return
	}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		f.deliverExtra(ctx, msg)
	}()
}

// deliverExtra 解析收件人 + 分发到各通道
//
// 🔴 **三件套**:WithoutCancel(脱离请求 ctx)+ 独立 deadline + recover。
//
//	理由与 internal/service/approval_inbox.go 的 deliver 一字不差 ——
//	那条路是审批事件投站内信,本条是站内信顺带发邮件,形状完全一样。
func (f *Fanout) deliverExtra(ctx context.Context, msg inbox.Message) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.L().Error("notifyx: 额外交付 panic,已隔离",
				zap.Uint64("user_id", msg.UserID),
				zap.String("title", msg.Title),
				zap.Any("panic", rec))
		}
	}()

	safeCtx := context.WithoutCancel(ctx)
	deliverCtx, cancel := context.WithTimeout(safeCtx, extraDeliveryTimeout)
	defer cancel()

	r, err := f.resolver.Resolve(deliverCtx, msg.UserID)
	if err != nil {
		// ⚠️ **查不到收件人 = 这条通知没有额外通道可走** —— 站内信已经落库了,
		//   这里只是"没发出去",不是"整个通知失败"。记 Warn 就走。
		logger.L().Warn("notifyx: 解析收件人失败,跳过额外交付",
			zap.Uint64("user_id", msg.UserID),
			zap.Error(err))
		return
	}

	res := f.dispatcher.Dispatch(deliverCtx, r, fromInbox(msg))
	if len(res.Failed) > 0 {
		logger.L().Warn("notifyx: 额外交付部分失败",
			zap.Uint64("user_id", msg.UserID),
			zap.Strings("delivered", res.Delivered),
			zap.Strings("skipped", res.Skipped),
			zap.Any("failed", res.Failed))
	}
}

// fromInbox inbox.Message → notifyx.Message
//
// ⚠️ **只搬有对应语义的字段** —— inbox 里的 MsgType / SenderID / ExpiredAt
//
//	在"投递"这一层没有对应物,硬塞进 Data 只会让通道各写各的解读。
func fromInbox(m inbox.Message) Message {
	return Message{
		Title:   m.Title,
		Content: m.Content,
		Link:    m.Link,
		RefType: m.RefType,
		RefID:   m.RefID,
	}
}
