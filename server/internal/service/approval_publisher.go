// 通用审批系统 —— 审批事件的生产者(M88;M173 收口到 eventx)
//
// 文档:docs/通用审批系统设计方案V1.0.md §9.2(事件推送)
//
// ⚠️ **这个文件是「引擎 → 事件通道」的唯一接缝**,两个方向的边界都在这儿:
//
// 引擎(M84)只认 `ApprovalEvent{InstanceID, Action, AssigneeID}` 三个字段,
// 它不认识 topic、不认识事件名、不认识 Redis。往 Event 上加字段等于
// 改广播契约(§11 R10),所以引擎那侧一个字段都不能多。
// 本文件负责把 `Action` 翻成 M73 定死的事件名 + payload 白名单。
//
// ⚠️ **依赖方向:service → push / eventx,反过来不成立** ——
// push 包不知道审批是什么,它只认 topic 字符串;eventx 更不知道。
//
// # 发布通道:两条,装配期二选一
//
//	NewApprovalPublisherWithOutbox(db) —— **生产装配用这条**(main.go):
//	  事件先落 sys_event_outbox(pending),commit 即"发布成功";
//	  eventx 的投递器把它推给 push 通道。Redis 抖一下事件不再丢
//	  (行还在库里,投递器按退避重试,耗尽进死信)。
//	NewApprovalPublisher() —— 无 outbox,直接异步直推(单测与
//	  "不想要 outbox"的部署)。行为与 M88 原版逐字节相同。
//
// 两条通道的**线上字节完全相同**(topic / event / payload 字段一致,
// 见 event_outbox_publisher_test.go 的对拍用例),差别只有:
// outbox 路径的推送时机晚一个投递轮询间隔(≤ 轮询周期),
// 以及失败不再静默(有重试、有死信、有 last_error)。
//
// 三件套(context.WithoutCancel + goroutine + recover)不再手写 ——
// 收口在 eventx.Async,**全仓只此一份实现**。
package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/eventx"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/push"
)

// approvalEventPublishTimeout 单次投递的兜底 deadline。
//
// ⚠️ `push.Publish` 本身永不返回 error(失败只记日志),但它内部要等 Redis
// 的网络往返 —— 万一 Redis 是「连得上但卡住」而不是「连不上」,一个没有
// deadline 的 Publish 会把审批请求的收尾流程一起拖住。
//
// 2s 与 `cmd/server/jobs_notify.go` 的 `jobEventPublishTimeout` 同值:
// 一次 Redis PUBLISH 在同机房是毫秒级,2s 已经宽松到能覆盖一次 GC 停顿。
const approvalEventPublishTimeout = 2 * time.Second

// pushApprovalPublisher 审批事件的生产者实现。
//
// outboxDB 非 nil ⇒ 事件先落 outbox;nil ⇒ 直接异步直推(单测/降级部署)。
type pushApprovalPublisher struct {
	outboxDB *gorm.DB
}

// NewApprovalPublisher 构造审批事件生产者(直推通道,无 outbox)。
//
// ⚠️ **返回的不是 nil** —— 引擎里那处 `publisher == nil` 判断是给单测和
// 「不想要推送」的部署留的,生产装配期永远给一个真的实现。
// 真正"发不出去"的情况(Redis 没连)由 `push.Publish` 内部处理:
// 记 Debug 日志后丢弃,**不静默**(push 包的设计第 3 条)。
func NewApprovalPublisher() ApprovalPublisher {
	return &pushApprovalPublisher{}
}

// NewApprovalPublisherWithOutbox 构造走 outbox 的审批事件生产者(生产装配)。
//
// db 为 nil 时退化为直推通道(与 NewApprovalPublisher 相同)。
func NewApprovalPublisherWithOutbox(db *gorm.DB) ApprovalPublisher {
	return &pushApprovalPublisher{outboxDB: db}
}

var _ ApprovalPublisher = (*pushApprovalPublisher)(nil)

// PublishApproval 把一条审批事件投递到事件通道。
//
// ⚠️ **本方法永不返回错误** —— 推送是尽力而为的旁路,投递方是"业务写完库的
// 请求",不该因为"推送没发出去"就让主流程失败。失败只记日志。
func (p *pushApprovalPublisher) PublishApproval(ctx context.Context, ev ApprovalEvent) {
	evt, payload, ok := buildApprovalEvent(ev)
	if !ok {
		// ⚠️ 未知动作 = 契约被破坏了。这条必须显眼,不能悄悄吞掉 ——
		// 前端会永远收不到这条事件,而现场没有任何线索。
		logger.L().Error("approval: 未知的事件动作,事件已丢弃",
			zap.String("action", ev.Action),
			zap.Uint64("instance_id", ev.InstanceID))
		return
	}

	if p.outboxDB != nil {
		// ⚠️ 这里剥取消链:调用方的 ctx 可能已死(HTTP 超时 / 上游断开),
		// 拿它落表会直接失败。落表是同步的(小 insert),成功即发布完成。
		_, err := eventx.Enqueue(context.WithoutCancel(ctx), p.outboxDB, eventx.OutboxMessage{
			EventName: evt,
			Topic:     push.TopicApproval,
			Payload:   payload,
		})
		switch {
		case err == nil:
			return // 已落表,投递器负责
		case errors.Is(err, eventx.ErrDuplicate):
			return // 同一事件已落过表,不算失败
		default:
			// ⚠️ 落表失败(DB 抖动)不能丢事件 —— 退回直推通道,
			// 语义与无 outbox 部署完全一致(失败只记日志,不阻断业务)
			logger.L().Warn("approval: 事件落 outbox 失败,退回直推",
				zap.String("event", evt),
				zap.Uint64("instance_id", ev.InstanceID),
				zap.Error(err))
		}
	}

	eventx.Async(ctx, evt, func(ctx context.Context) {
		pubCtx, cancel := context.WithTimeout(ctx, approvalEventPublishTimeout)
		defer cancel()

		push.Publish(pubCtx, []string{push.TopicApproval}, push.Event{
			Topic: push.TopicApproval,
			Event: evt,
			// At 不填:Publish 内部在 ev.At==0 时补时间戳,这里再算一遍
			// 只是多一个"时间戳该由谁产生"的歧义点
			Payload: payload,
		})
	})
}

// buildApprovalEvent 把引擎的动作翻成 M73 的事件契约。
//
// ⚠️ **动作 → 事件名 的映射只有这一处**,前端 `useEventStream.ts` 的
// `EVENT.approvalTodo / approvalDone` 与它一一对应。
//
// 映射表:
//
//	assigned  → approval.todo  (新待办)
//	其余四种 → approval.done  (这条我处理完了)
//
// ⚠️ **`Kind` 恒为 `todo`**(§9.2 的表格)—— `ApprovalPayload.Kind` 的
// 注释说它区分"todo / approval"两类,但本设计的五类触发全是待办侧,
// 另一类没有生产者。**不为了填满字段而编一个值。**
func buildApprovalEvent(ev ApprovalEvent) (string, push.ApprovalPayload, bool) {
	payload := push.ApprovalPayload{
		TodoID: strconv.FormatUint(ev.InstanceID, 10),
		Kind:   "todo",
		Action: ev.Action,
	}

	switch ev.Action {
	case EvApprovalAssigned:
		// ⚠️ **只有"新待办"才带 assignee_id** —— 审批动作 / 作废一律不带。
		// 那不是"发给某个人"的消息,谁关心谁自己去拉列表。
		//
		// ⚠️ **判断挂在 assigned 这个分支上,不是"AssigneeID != 0"** ——
		// 后者会让任何带了 AssigneeID 的动作都带上它,而 §16.2 的约定是
		// "只有新待办才非零"。两边都非零时,以前者为准。
		if ev.AssigneeID != 0 {
			payload.AssigneeID = strconv.FormatUint(ev.AssigneeID, 10)
		}
		return push.EventApprovalTodo, payload, true
	case EvApprovalApproved, EvApprovalRejected, EvApprovalRolledBack, EvApprovalCancelled:
		return push.EventApprovalDone, payload, true
	default:
		return "", push.ApprovalPayload{}, false
	}
}
