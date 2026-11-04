// 通用审批系统 —— 审批事件 → 站内信投递(M3)
//
// 文档:docs/通用审批系统设计方案V1.0.md §13(M3 落地记录)
//
// ⚠️ **这个文件是「审批事件 → 站内信」的唯一接缝**,边界跟 approval_publisher.go
// («审批事件 → 事件通道»)完全平行:
//
//	引擎 ──ApprovalEvent──▶ pushApprovalPublisher   ──▶ Redis 广播(前端角标)
//	                      └▶ inboxApprovalPublisher ──▶ sys_inbox_message(站内信)
//
// ⚠️ **为什么是装饰器,不是改引擎**:M88 立了一条纪律 —— 引擎 → 事件通道的接缝
// **只有一个**,就是 `ApprovalPublisher` 接口(approval.go)。再加一条通道就得
// 再动一次引擎。所以本文件是**装饰器**:`NewInboxApprovalPublisher(inner, ...)`
// 包住原来的 publisher —— 事件照发(Redis 通道完全不受影响),顺带投一条站内信。
// **引擎对站内信一无所知。**
//
// ⚠️ **依赖方向:service → inbox,反过来不成立** —— inbox 包不知道审批是什么,
// 它只认 `inbox.Message`。
//
// 🔴 **投递永远是尽力而为的旁路** —— 审批单已经落库了,站内信投不出去
// **不许**让审批请求失败。只记日志。
package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/logger"
)

// approvalInboxDeliveryTimeout 单次站内信投递的兜底 deadline。
//
// ⚠️ 与 `approvalEventPublishTimeout`(2s)同量级但略宽 —— 站内信要**写一次库**
// (Redis PUBLISH 只是一次网络往返),DB 慢一点是常事。3s 已经宽到能覆盖
// 一次 GC 停顿,又不会让 goroutine 挂到天荒地老。
const approvalInboxDeliveryTimeout = 3 * time.Second

const (
	// approvalInboxMsgType 消息类型。
	//
	// ⚠️ **用 "system" 而不是 "biz"** —— 跟同模块的巡检告警(approval_scan.go)
	// 保持一致:同一条通知链路上的三类消息(待办 / 结果 / 巡检告警)用同一个
	// 类型,前端按类型筛选时不会出现"巡检告警一类、待办另一类"这种割裂。
	approvalInboxMsgType = "system"

	// approvalInboxRefType 关联资源类型。
	//
	// ⚠️ **与巡检告警的两个 ref_type("approval_no_approver" /
	// "approval_stalled")刻意不同** —— 那两个是**去重键**(同一张单子只告警
	// 一次,见 approval_scan.go 的 alreadyAlerted),这个是"这批消息的出处"。
	// 用同一个会让巡检的去重查询把正常的待办消息一起算进去。
	approvalInboxRefType = "approval"
)

// 收件人规则 —— **全仓唯一一张表**,别在别处再抄一份:
//
//	事件                              收件人    依据
//	-------------------------------  --------  --------------------------------
//	assigned(新待办)                 审批人    ev.AssigneeID(逐个审批人各一条)
//	approved(整单通过)               发起人    ev.Result == true
//	rejected(驳回)                   发起人    ev.Result == true
//	rolled_back(打回)                发起人    ev.Result == true
//	cancelled(作废)                  ——        发起人自己做的,不通知自己
//	approved(会签里"这一票记下了")   ——        ev.Result == false,还没定局
//
// ⚠️ **`ev.Result` 由引擎给**,不是这里推的 —— 会签的中间票与最后一票用的是
// 同一个 action(`approved`),旁路读实例 status 猜会有竞态(见
// `ApprovalEvent.Result` 的注释)。

// inboxApprovalPublisher 审批事件 → 站内信 的装饰器。
type inboxApprovalPublisher struct {
	inner    ApprovalPublisher
	notifier inbox.Notifier
	db       *gorm.DB
}

var _ ApprovalPublisher = (*inboxApprovalPublisher)(nil)

// NewInboxApprovalPublisher 构造「事件 + 站内信」双通道的 publisher。
//
// ⚠️ **inner / notifier / db 都可以是 nil**,各有一条降级路径:
//
//	inner == nil    → 只投站内信,不发事件(单测与"不想要广播"的部署)
//	notifier == nil → 只发事件,不投站内信(站内信投递是尽力而为的旁路)
//	db == nil       → 同上(没有 db 读不到实例,投不了)
//
// ⚠️ **生产装配期两个都要给** —— main.go 里是
// `NewInboxApprovalPublisher(NewApprovalPublisher(), inboxMgr, db)`。
func NewInboxApprovalPublisher(inner ApprovalPublisher, notifier inbox.Notifier, db *gorm.DB) ApprovalPublisher {
	return &inboxApprovalPublisher{inner: inner, notifier: notifier, db: db}
}

// PublishApproval 事件照发,顺带投一条站内信。
//
// ⚠️ **投递是异步的** —— 与 approval_publisher.go 同理:DB 写是网络 IO,
// 不能挂在审批请求的关键路径上。
func (p *inboxApprovalPublisher) PublishApproval(ctx context.Context, ev ApprovalEvent) {
	// ① 事件照发 —— 装饰器**不接管**原通道,只是加一条支线。
	if p.inner != nil {
		p.inner.PublishApproval(ctx, ev)
	}
	// ② 没有投递器或没有 db → 什么都不做(**不 panic**)。
	if p.notifier == nil || p.db == nil {
		return
	}
	// ③ 作废不通知:发起人自己做的,通知自己等于打扰。
	if ev.Action == EvApprovalCancelled {
		return
	}
	go p.deliver(ctx, ev)
}

// deliver 读实例 + 投一条站内信(在 goroutine 里跑)。
//
// 🔴 **goroutine + recover + WithoutCancel 三件套** —— 理由与
// approval_publisher.go 一字不差:
//
//	· WithoutCancel:调用方的 ctx 可能已经死了(HTTP 请求超时 / 上游断开),
//	  拿它写 DB 直接失败,表现是「审批成功了但收件人没收到提醒」。
//	· goroutine:DB 写是网络 IO,不能挂在审批请求的关键路径上。
//	· recover:goroutine 里逃出去的 panic 会**打死整个进程** ——
//	  一条提醒不该有能力把审批服务带走。
func (p *inboxApprovalPublisher) deliver(ctx context.Context, ev ApprovalEvent) {
	defer func() {
		if r := recover(); r != nil {
			logger.L().Error("approval: 站内信投递 panic,已隔离",
				zap.String("action", ev.Action),
				zap.Uint64("instance_id", ev.InstanceID),
				zap.Any("panic", r))
		}
	}()

	safeCtx := context.WithoutCancel(ctx)
	deliverCtx, cancel := context.WithTimeout(safeCtx, approvalInboxDeliveryTimeout)
	defer cancel()

	var inst model.SysApprovalInstance
	if err := p.db.WithContext(deliverCtx).
		Select("id", "title", "applicant_user_id", "return_count").
		Where("id = ?", ev.InstanceID).
		Take(&inst).Error; err != nil {
		// ⚠️ 查不到实例 = 单子被删了 / 数据坏了。**记一条 Warn 就走** ——
		// 投递失败不该拖住任何东西,更不该 panic。
		//
		// ⚠️ 这里**不区分"没找到"和"数据库炸了"** —— 两种情况的处置一样
		// (跳过 + 记日志),而区分它们要额外判 `errors.Is(err, gorm.ErrRecordNotFound)`,
		// 收益只是日志好看一点。
		logger.L().Warn("approval: 站内信投递找不到审批实例,已跳过",
			zap.String("action", ev.Action),
			zap.Uint64("instance_id", ev.InstanceID),
			zap.Error(err))
		return
	}

	msg, ok := p.buildMessage(&inst, ev)
	if !ok {
		return
	}
	if err := p.notifier.Notify(deliverCtx, msg); err != nil {
		logger.L().Error("approval: 站内信投递失败",
			zap.String("action", ev.Action),
			zap.Uint64("instance_id", ev.InstanceID),
			zap.Uint64("user_id", msg.UserID),
			zap.Error(err))
	}
}

// buildMessage 由「实例 + 事件」算出要投给谁、投什么。
//
// ⚠️ **返回 false = 这一条不投** —— 不是"投一条空消息",是**一条都不投**。
// 会签里"这一票记下了"的 `approved` 就走这条路。
//
// ⚠️ **近乎纯函数** —— 不看时钟、不碰 DB(只多一条 Warn 日志)。
// 收件人规则就是文件头那张表,单测直接对着表逐行断言。
func (p *inboxApprovalPublisher) buildMessage(
	inst *model.SysApprovalInstance, ev ApprovalEvent,
) (inbox.Message, bool) {
	subject := strings.TrimSpace(inst.Title)
	if subject == "" {
		// ⚠️ 标题可以为空(建单时没填标题)—— 兜底成"审批单 #<id>"。
		// 不然收件人看到的是一条**没有主语**的消息("【待审批】"后面直接空)。
		subject = fmt.Sprintf("审批单 #%d", ev.InstanceID)
	}

	base := inbox.Message{
		MsgType: approvalInboxMsgType,
		Link:    "/approval/" + strconv.FormatUint(ev.InstanceID, 10),
		RefType: approvalInboxRefType,
		RefID:   ev.InstanceID,
		// ⚠️ SenderID 留 0 = "系统发的"。收件人看得见的是标题和正文,
		// 而"谁批的 / 什么意见"在详情页里看得到 —— 消息本身不必带
		// (带了反而越权:站内信列表的可见面比审批意见宽,§11 R10 同一条纪律)。
	}

	switch ev.Action {
	case EvApprovalAssigned:
		// ⚠️ **AssigneeID == 0 不是"发给所有人",是"没有收件人"**。
		// §9.2 的约定是"只有新待办才非零" —— 这里再挡一次,
		// 因为 0 会被 inbox 当成"发给 user 0",那是查不到的人。
		if ev.AssigneeID == 0 {
			return inbox.Message{}, false
		}
		base.UserID = ev.AssigneeID
		base.Title = "【待审批】" + subject
		base.Content = "有一条审批单需要您处理,请及时查看。"
		return base, true

	case EvApprovalApproved, EvApprovalRejected, EvApprovalRolledBack:
		// 🔴 **只有"结果"事件才通知发起人** —— ev.Result 由引擎给。
		// 会签里"这一票记下了"的 approved 走不到这里(引擎给的是 false)。
		if !ev.Result {
			return inbox.Message{}, false
		}
		if inst.ApplicantUserID == 0 {
			// ⚠️ 发起人丢了 = 数据坏了。宁可不发,也不发成"发给 user 0"。
			logger.L().Warn("approval: 站内信投递跳过 —— 实例没有发起人",
				zap.String("action", ev.Action),
				zap.Uint64("instance_id", ev.InstanceID))
			return inbox.Message{}, false
		}
		base.UserID = inst.ApplicantUserID
		switch ev.Action {
		case EvApprovalApproved:
			base.Title = "【审批已通过】" + subject
			base.Content = "您提交的审批单已审批通过。"
		case EvApprovalRejected:
			base.Title = "【审批被驳回】" + subject
			base.Content = "您提交的审批单被驳回,请查看详情了解驳回意见。"
		case EvApprovalRolledBack:
			base.Title = "【审批被打回】" + subject
			base.Content = "您提交的审批单被打回,请修改后重新提交。"
			// ⚠️ **第二次及以后被打回,正文里带上轮次** —— 否则收件箱里躺着
			// 两条一模一样的"【审批被打回】",分不清哪条是最近的那次。
			// (return_count 在打回的同一个事务里 +1,事件发出来时已经是新值。)
			if inst.ReturnCount > 1 {
				base.Content = fmt.Sprintf("您提交的审批单第 %d 次被打回,请修改后重新提交。", inst.ReturnCount)
			}
		}
		return base, true
	}

	// cancelled(发起人自己做的)与未知动作 → 不投。
	return inbox.Message{}, false
}
