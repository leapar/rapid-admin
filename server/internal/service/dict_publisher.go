// 字典事件的生产者(M94;M173 收口到 eventx)
//
// ⚠️ **这个文件是「字典写路径 → 事件通道」的唯一接缝**,和
// `approval_publisher.go` 是同一形状 —— 不是巧合,是刻意对齐:
// 两个文件都只做一件事,把「业务侧知道发生了什么」翻译成
// 「事件通道上的一个信号」。
//
// 背景(M94 要修的问题):
//
//	字典是"代码 enum 的中央仓库",前端 `web/src/stores/dict.ts` 把它缓存
//	**30 分钟**(`CACHE_TTL = 30 * 60 * 1000`)。管理员在字典管理页改了
//	一个 label,后端写完库就返回 200 —— **前端一个字节都不知道**:
//	· 管理员自己的浏览器:最多 30 分钟后才看到新标签
//	· 别人的浏览器:同样最多 30 分钟,而且**没有任何提示**
//
//	也就是说「改了不生效」是**默认行为**,不是异常。
//
// # 发布通道(与 approval_publisher.go 同一套,细节见那边的说明)
//
//	NewDictPublisherWithOutbox(db) —— 生产装配(main.go):先落
//	  sys_event_outbox,投递器负责推出;Redis 抖动不再丢事件。
//	NewDictPublisher() —— 直推通道(单测/降级部署),行为与 M94 原版一致。
//
// 三件套(context.WithoutCancel + goroutine + recover)收口在 eventx.Async。
package service

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/eventx"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/push"
)

// dictEventPublishTimeout 单次投递的兜底 deadline。
//
// ⚠️ `push.Publish` 本身永不返回 error(失败只记日志),但它内部要等 Redis
// 的网络往返 —— 万一 Redis 是「连得上但卡住」而不是「连不上」,一个没有
// deadline 的 Publish 会把「保存字典」这条请求的收尾一起拖住。
//
// 2s 与 `approval_publisher.go` 的 `approvalEventPublishTimeout` 同值:
// 一次 Redis PUBLISH 在同机房是毫秒级,2s 已经宽松到能覆盖一次 GC 停顿。
const dictEventPublishTimeout = 2 * time.Second

// DictPublisher 字典变更事件的生产者
type DictPublisher interface {
	// PublishDictChanged 广播「某个字典组被改了」
	//
	// ⚠️ **本方法永不返回错误** —— 推送是尽力而为的旁路,投递方是
	// 「业务写完库的请求」,不该因为「推送没发出去」就让主流程失败。
	// 失败只记日志。
	PublishDictChanged(ctx context.Context, groupCode string)
}

// pushDictPublisher 字典事件的生产者实现。
//
// outboxDB 非 nil ⇒ 事件先落 outbox;nil ⇒ 直接异步直推(单测/降级部署)。
type pushDictPublisher struct {
	outboxDB *gorm.DB
}

// NewDictPublisher 构造字典事件生产者(直推通道,无 outbox)。
//
// ⚠️ **返回的不是 nil** —— 生产装配期永远给一个真的实现。
// 真正「发不出去」的情况(Redis 没连)由 `push.Publish` 内部处理:
// 记 Debug 日志后丢弃(**不静默** —— push 包的设计第 3 条)。
func NewDictPublisher() DictPublisher {
	return &pushDictPublisher{}
}

// NewDictPublisherWithOutbox 构造走 outbox 的字典事件生产者(生产装配)。
//
// db 为 nil 时退化为直推通道(与 NewDictPublisher 相同)。
func NewDictPublisherWithOutbox(db *gorm.DB) DictPublisher {
	return &pushDictPublisher{outboxDB: db}
}

var _ DictPublisher = (*pushDictPublisher)(nil)

// PublishDictChanged 把一条「字典组被改了」的事件投递到事件通道。
func (p *pushDictPublisher) PublishDictChanged(ctx context.Context, groupCode string) {
	// ⚠️ **空 group_code 不发** —— 前端拿到它不知道该作废哪个组的缓存,
	// 只能"全清"(而 store 的 invalidate 接受不到具体组名)。
	// 数据库里 group_code 是 NOT NULL,理论上不会是空;
	// 这里挡一道是为了不让"上游传了个空串"变成"前端全量重拉"。
	if groupCode == "" {
		logger.L().Warn("dict: group_code 为空,事件已丢弃",
			zap.String("event", push.EventDictChanged))
		return
	}

	if p.outboxDB != nil {
		_, err := eventx.Enqueue(context.WithoutCancel(ctx), p.outboxDB, eventx.OutboxMessage{
			EventName: push.EventDictChanged,
			Topic:     push.TopicDict,
			Payload:   push.DictChangedPayload{GroupCode: groupCode},
		})
		switch {
		case err == nil:
			return
		case errors.Is(err, eventx.ErrDuplicate):
			return
		default:
			logger.L().Warn("dict: 事件落 outbox 失败,退回直推",
				zap.String("event", push.EventDictChanged),
				zap.String("group_code", groupCode),
				zap.Error(err))
		}
	}

	eventx.Async(ctx, push.EventDictChanged, func(ctx context.Context) {
		pubCtx, cancel := context.WithTimeout(ctx, dictEventPublishTimeout)
		defer cancel()

		push.Publish(pubCtx, []string{push.TopicDict}, push.Event{
			Topic: push.TopicDict,
			Event: push.EventDictChanged,
			// At 不填:Publish 内部在 ev.At==0 时补时间戳,这里再算一遍
			// 只是多一个「时间戳该由谁产生」的歧义点
			Payload: push.DictChangedPayload{GroupCode: groupCode},
		})
	})
}
