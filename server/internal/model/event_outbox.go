package model

import "time"

// 事件 outbox 投递状态机。
//
// pending → delivered(投递成功)
// pending → dead(重试次数耗尽,留表人工排查)
//
// ⚠️ 没有 "failed" 这个状态 —— 投递失败的行仍然是 pending,
// 靠 attempts + next_retry_at 表达"这一条暂时还发不出去"。
// 状态越少,状态机越不可能写错。
const (
	// OutboxStatusPending 待投递(含"等待下次重试")
	OutboxStatusPending = "pending"
	// OutboxStatusDelivered 已投递成功
	OutboxStatusDelivered = "delivered"
	// OutboxStatusDead 死信(重试耗尽,不再投递)
	OutboxStatusDead = "dead"
)

// SysEventOutbox 事务性 outbox —— 领域事件先落表、后投递。
//
// # 这张表解决什么问题
//
// "业务写完库,事件却已经发出去了" 的反面:"业务成功了,事件却丢了"。
// 此前发布走裸 goroutine 直推 Redis,Redis 抖一下事件就静默没了 ——
// 没有人重试,也没有任何痕迹。落表之后:
//
//	业务侧(或事务内)只写这一行,commit 即"发布成功";
//	投递器(internal/pkg/eventx 的 Deliverer)轮询这张表,把到期的行
//	投给真正的通道(生产装配是 pkg/push 的 Redis transport),
//	失败按退避重试,重试耗尽置 dead(死信)。
//
// # 状态 / 重试 / 死信
//
//	status = pending 且 next_retry_at 为空或已到期 ⇒ 待投递;
//	投递失败 attempts+1,next_retry_at = now + 退避(指数,封顶);
//	attempts 达到上限 ⇒ status = dead,不再投递。
//
// # 幂等键 idem_key
//
// 可空。给了唯一键(uk_outbox_idem):同一 idem_key 只会落一行,
// 重复 Enqueue 静默去重(INSERT ... ON CONFLICT DO NOTHING)。
// 不给 ⇒ 每次调用都是一条新事件(用 NULL,不是空串 ——
// 唯一索引里多个 NULL 不冲突,多个空串会冲突,别改成 string)。
//
// # 已投递行的生命周期
//
// status=delivered 的行由投递器按保留时长清理(事件是信号不是数据,
// 没有审计价值);dead 行**不**自动清理,留给人排查。
type SysEventOutbox struct {
	BaseModel
	// EventName 事件名(如 approval.todo / dict.changed)
	EventName string `gorm:"size:64;not null" json:"event_name"`
	// Topic 投递目标通道名(如 push 的 topic)
	Topic string `gorm:"size:64;not null" json:"topic"`
	// Payload 事件载荷(JSON 序列化后的原文,投递时原样透传)
	Payload string `gorm:"type:text" json:"payload"`
	// Status 投递状态(pending / delivered / dead)
	//
	// idx_outbox_due = (status, next_retry_at):投递器每次轮询的
	// "到期行"查询(WHERE status=? AND next_retry_at<=?)走这条索引。
	Status string `gorm:"size:16;not null;default:pending;index:idx_outbox_due,priority:1" json:"status"`
	// Attempts 已投递尝试次数(成功即不再增长)
	Attempts int `json:"attempts"`
	// NextRetryAt 下次允许投递的时间(退避);NULL = 立即可投
	NextRetryAt *time.Time `gorm:"index:idx_outbox_due,priority:2" json:"next_retry_at"`
	// LastError 最后一次投递失败的错误摘要(截断到 512)
	LastError string `gorm:"size:512" json:"last_error"`
	// IdemKey 生产侧幂等键(可空;唯一索引,重复落表静默去重)
	IdemKey *string `gorm:"size:128;uniqueIndex:uk_outbox_idem" json:"idem_key,omitempty"`
}

// TableName 表名
func (SysEventOutbox) TableName() string { return "sys_event_outbox" }
