// 审批通知 → 站内信 单测(M3)
//
// 文档:docs/通用审批系统设计方案V1.0.md §13(M3 落地记录)
//
// ⚠️ **这里测的是"接缝",不是引擎** —— 引擎发什么事件在
// `approval_engine_test.go` 里已经断言过了。这里断言的是:
//
//	① 收件人规则对不对(谁收到、谁**不该**收到);
//	② 事件照发(装饰器**不接管**原通道);
//	③ 站内信投递失败 / panic / 找不到实例,**都不许**影响审批主流程;
//	④ 引擎给的 `Result` 标志把「会签的中间票」和「整单通过」分开了。
//
// ⚠️ **④ 是这里最要紧的一条** —— `approved` 有两种语境(§9.2),
// 分错了就是给发起人发一条**假消息**("会签到一半就报审批已通过")。
//
// ─────────── 可逆探针记录(2026-10-24) ───────────
//
// 规矩:把实现改回"bug 版本",看测试会不会红。**SKIP(探针没匹配上)与
// GREEN(测试没盯住)都是失败**,只有 RED 算通过。
//
//	P1  终局 approved 丢掉 Result            → RED(补了或签用例之后)
//	P2  会签中间票误标 Result=true           → RED
//	P3  装饰器不认 Result(去掉那道闸)        → RED
//	P4  待办投错人(投给发起人)                → RED
//	P5  拿已取消的 ctx 写库(去掉 WithoutCancel)→ RED
//	P6  驳回事件丢掉 Result                  → RED
//	P7  投递器 panic 不再被隔离              → RED(进程被打死)
//	P8  会签终局(enterStage)丢掉 Result      → RED
//	P9  打回事件丢掉 Result                  → RED
//
// ⚠️ **P1 第一轮是 GREEN —— 那不是探针坏了,是测试没覆盖到那条路径**:
// `finishApproved`(或签)与 `enterStage`(会签终局)是**两条不同的代码路径**,
// 当时只有会签用例,终局走的是 `enterStage`,于是把 `finishApproved` 的
// `Result: true` 去掉照样绿。补了 `TestM3_Engine_ResultFlagOnOrSign` 之后
// P1 才变红。**用例没覆盖到的路径 = 改坏了也不知道。**
//
// ⚠️ **P3 是"唯一屏障"式的盯法** —— 去掉 `if !ev.Result` 之后,会签的中间票
// 会投一条"审批已通过"给发起人,`TestM3_Inbox_CountersignMidVoteSendsNothing`
// 立刻变红。**没有别的闸能挡住同一个 case**。
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/sonyflake"
)

// ─────────── 夹具 ───────────

// inboxFixture 装好的「站内信 publisher + 内存库」。
type inboxFixture struct {
	db        *gorm.DB
	inner     *recordingPublisher
	notifier  inbox.Notifier
	publisher ApprovalPublisher
}

func newInboxFixture(t *testing.T) *inboxFixture {
	t.Helper()
	db := newTestDB(t) // 建了 15 张表,含 sys_inbox_message
	f := &inboxFixture{
		db:       db,
		inner:    &recordingPublisher{},
		notifier: inbox.New(db),
	}
	f.publisher = NewInboxApprovalPublisher(f.inner, f.notifier, db)
	return f
}

// newInstance 造一个审批实例(标题 + 发起人)
func (f *inboxFixture) newInstance(t *testing.T, applicant uint64, title string) uint64 {
	t.Helper()
	inst := &model.SysApprovalInstance{
		BaseModel:           model.BaseModel{ID: sonyflake.Next()},
		BizType:             "test_biz",
		BizID:               sonyflake.Next(),
		Title:               title,
		TemplateID:          1,
		ApplicantUserID:     applicant,
		ApplicantIdentityID: 1,
		Status:              model.ApprovalStatusRunning,
	}
	require.NoError(t, f.db.Create(inst).Error)
	return inst.ID
}

// innerEvents 读内层 publisher 收到的事件(带锁)
func (f *inboxFixture) innerEvents() []ApprovalEvent {
	f.inner.mu.Lock()
	defer f.inner.mu.Unlock()
	return append([]ApprovalEvent(nil), f.inner.evs...)
}

// messages 读站内信表
func (f *inboxFixture) messages(t *testing.T) []model.InboxMessage {
	t.Helper()
	var ms []model.InboxMessage
	require.NoError(t, f.db.Order("id ASC").Find(&ms).Error)
	return ms
}

// waitMessages 等到站内信条数变成 n(投递是异步的,必须轮询)
func (f *inboxFixture) waitMessages(t *testing.T, n int) []model.InboxMessage {
	t.Helper()
	var ms []model.InboxMessage
	require.Eventually(t, func() bool {
		ms = nil
		if err := f.db.Order("id ASC").Find(&ms).Error; err != nil {
			return false
		}
		return len(ms) == n
	}, 3*time.Second, 20*time.Millisecond, "期望 %d 条站内信", n)
	return ms
}

// assertNoMessage 等一会儿,断言没有站内信落库
func (f *inboxFixture) assertNoMessage(t *testing.T) {
	t.Helper()
	time.Sleep(250 * time.Millisecond)
	assert.Empty(t, f.messages(t), "不该有站内信")
}

// ─────────── fake Notifier ───────────

// errNotifier 投递必失败
type errNotifier struct{ reached chan struct{} }

func (e *errNotifier) Notify(ctx context.Context, msg inbox.Message) error {
	select {
	case e.reached <- struct{}{}:
	default:
	}
	return errors.New("投递失败(故意的)")
}

func (e *errNotifier) NotifyBatch(ctx context.Context, msgs []inbox.Message) error {
	return errors.New("投递失败(故意的)")
}

// panicNotifier 投递必 panic —— 验 recover 把它隔离了
type panicNotifier struct{ reached chan struct{} }

func (p *panicNotifier) Notify(ctx context.Context, msg inbox.Message) error {
	select {
	case p.reached <- struct{}{}:
	default:
	}
	// ⚠️ 故意 panic —— 没有 recover 的话,这个 panic 会**打死整个测试进程**,
	// 测试会以"整个包崩了"的形式失败,而不是一条干净的断言失败。
	panic("notifier 故意 panic")
}

func (p *panicNotifier) NotifyBatch(ctx context.Context, msgs []inbox.Message) error {
	panic("notifier 故意 panic")
}

// ─────────── ① 收件人规则 ───────────

// TestM3_Inbox_BuildMessage_RecipientRules 收件人规则的表驱动测试。
//
// ⚠️ **这张表是 `approval_inbox.go` 文件头那张表的镜像** —— 两边不一致时
// 以代码为准,改代码必须回来改这张表。
func TestM3_Inbox_BuildMessage_RecipientRules(t *testing.T) {
	p := &inboxApprovalPublisher{}

	base := model.SysApprovalInstance{
		Title:           "资料审核",
		ApplicantUserID: 500,
	}

	cases := []struct {
		name       string
		inst       model.SysApprovalInstance
		ev         ApprovalEvent
		wantOK     bool
		wantUserID uint64
		wantTitle  string
		wantHas    string // content 必须包含的片段
	}{
		{
			name:       "新待办 → 审批人",
			inst:       base,
			ev:         ApprovalEvent{InstanceID: 9, Action: EvApprovalAssigned, AssigneeID: 1000},
			wantOK:     true,
			wantUserID: 1000,
			wantTitle:  "【待审批】资料审核",
			wantHas:    "需要您处理",
		},
		{
			name:   "新待办但没有审批人 → 不投",
			inst:   base,
			ev:     ApprovalEvent{InstanceID: 9, Action: EvApprovalAssigned, AssigneeID: 0},
			wantOK: false,
		},
		{
			name:       "整单通过 → 发起人",
			inst:       base,
			ev:         ApprovalEvent{InstanceID: 9, Action: EvApprovalApproved, Result: true},
			wantOK:     true,
			wantUserID: 500,
			wantTitle:  "【审批已通过】资料审核",
			wantHas:    "已审批通过",
		},
		{
			// 🔴 **这一条是整个 M3 的核心** —— 会签里"这一票记下了"
			// 用的也是 `approved`。Result=false 时必须**一条都不投**。
			name:   "会签的中间票 → 不投",
			inst:   base,
			ev:     ApprovalEvent{InstanceID: 9, Action: EvApprovalApproved, Result: false},
			wantOK: false,
		},
		{
			name:       "驳回 → 发起人",
			inst:       base,
			ev:         ApprovalEvent{InstanceID: 9, Action: EvApprovalRejected, Result: true},
			wantOK:     true,
			wantUserID: 500,
			wantTitle:  "【审批被驳回】资料审核",
			wantHas:    "驳回",
		},
		{
			name:       "打回 → 发起人",
			inst:       base,
			ev:         ApprovalEvent{InstanceID: 9, Action: EvApprovalRolledBack, Result: true},
			wantOK:     true,
			wantUserID: 500,
			wantTitle:  "【审批被打回】资料审核",
			wantHas:    "重新提交",
		},
		{
			// ⚠️ 驳回 / 打回**必须带 Result** —— 事件不带就是"不是结果",
			// 一条都不投(而不是默认当成结果投出去)。
			name:   "驳回但没有 Result → 不投",
			inst:   base,
			ev:     ApprovalEvent{InstanceID: 9, Action: EvApprovalRejected, Result: false},
			wantOK: false,
		},
		{
			name:   "作废 → 不投(发起人自己做的)",
			inst:   base,
			ev:     ApprovalEvent{InstanceID: 9, Action: EvApprovalCancelled},
			wantOK: false,
		},
		{
			name:   "未知动作 → 不投",
			inst:   base,
			ev:     ApprovalEvent{InstanceID: 9, Action: "not_a_real_action"},
			wantOK: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inst := c.inst
			msg, ok := p.buildMessage(&inst, c.ev)
			if !c.wantOK {
				assert.False(t, ok, "这一条不该投")
				assert.Equal(t, inbox.Message{}, msg, "不投的时候不能返回半条消息")
				return
			}
			require.True(t, ok, "这一条应该投")
			assert.Equal(t, c.wantUserID, msg.UserID, "收件人")
			assert.Equal(t, c.wantTitle, msg.Title, "标题")
			assert.Contains(t, msg.Content, c.wantHas, "正文")
			assert.Equal(t, approvalInboxMsgType, msg.MsgType)
			assert.Equal(t, approvalInboxRefType, msg.RefType)
			assert.Equal(t, c.ev.InstanceID, msg.RefID)
			assert.Equal(t, "/approval/"+strconv.FormatUint(c.ev.InstanceID, 10), msg.Link)
			assert.Zero(t, msg.SenderID, "系统发的消息不带发送人")
		})
	}
}

// TestM3_Inbox_BuildMessage_Edges 几个容易漏的边界
func TestM3_Inbox_BuildMessage_Edges(t *testing.T) {
	p := &inboxApprovalPublisher{}

	t.Run("没有标题 → 兜底成『审批单 #<id>』", func(t *testing.T) {
		// ⚠️ 不兜底的话收件人看到的是「【待审批】」后面直接空着 ——
		// 一条**没有主语**的消息。
		inst := model.SysApprovalInstance{ApplicantUserID: 500}
		msg, ok := p.buildMessage(&inst, ApprovalEvent{
			InstanceID: 77, Action: EvApprovalAssigned, AssigneeID: 1000,
		})
		require.True(t, ok)
		assert.Equal(t, "【待审批】审批单 #77", msg.Title)
	})

	t.Run("没有发起人 → 不投(而不是发给 user 0)", func(t *testing.T) {
		inst := model.SysApprovalInstance{Title: "资料审核"} // ApplicantUserID = 0
		_, ok := p.buildMessage(&inst, ApprovalEvent{
			InstanceID: 9, Action: EvApprovalApproved, Result: true,
		})
		assert.False(t, ok)
	})

	t.Run("第二次及以后被打回 → 正文带轮次", func(t *testing.T) {
		// ⚠️ 收件箱里躺着两条一模一样的「【审批被打回】」时,分不清哪条是最近的。
		inst := model.SysApprovalInstance{Title: "资料审核", ApplicantUserID: 500, ReturnCount: 3}
		msg, ok := p.buildMessage(&inst, ApprovalEvent{
			InstanceID: 9, Action: EvApprovalRolledBack, Result: true,
		})
		require.True(t, ok)
		assert.Contains(t, msg.Content, "第 3 次")
	})

	t.Run("第一次被打回 → 正文不带轮次", func(t *testing.T) {
		inst := model.SysApprovalInstance{Title: "资料审核", ApplicantUserID: 500, ReturnCount: 1}
		msg, ok := p.buildMessage(&inst, ApprovalEvent{
			InstanceID: 9, Action: EvApprovalRolledBack, Result: true,
		})
		require.True(t, ok)
		assert.NotContains(t, msg.Content, "第 1 次")
	})
}

// ─────────── ② 端到端投递 ───────────

// TestM3_Inbox_PublishesTodoToAssignee 新待办投给审批人
func TestM3_Inbox_PublishesTodoToAssignee(t *testing.T) {
	f := newInboxFixture(t)
	id := f.newInstance(t, 500, "资料审核")

	f.publisher.PublishApproval(context.Background(), ApprovalEvent{
		InstanceID: id, Action: EvApprovalAssigned, AssigneeID: 1000,
	})

	msgs := f.waitMessages(t, 1)
	m := msgs[0]
	assert.Equal(t, uint64(1000), m.UserID, "待办投给审批人,不是发起人")
	assert.Equal(t, "【待审批】资料审核", m.Title)
	assert.Equal(t, approvalInboxMsgType, m.MsgType)
	assert.Equal(t, approvalInboxRefType, m.RefType)
	assert.Equal(t, id, m.RefID)
	assert.Equal(t, "/approval/"+strconv.FormatUint(id, 10), m.Link)
	assert.Nil(t, m.ReadAt, "新投递的消息是未读")
	assert.NotZero(t, m.ID, "id 必须有值(应用侧雪花)")
}

// TestM3_Inbox_PublishesResultToApplicant 结果投给发起人
func TestM3_Inbox_PublishesResultToApplicant(t *testing.T) {
	f := newInboxFixture(t)
	id := f.newInstance(t, 500, "资料审核")

	f.publisher.PublishApproval(context.Background(), ApprovalEvent{
		InstanceID: id, Action: EvApprovalApproved, Result: true,
	})

	msgs := f.waitMessages(t, 1)
	assert.Equal(t, uint64(500), msgs[0].UserID, "结果投给发起人")
	assert.Equal(t, "【审批已通过】资料审核", msgs[0].Title)
}

// TestM3_Inbox_CountersignMidVoteSendsNothing 会签的中间票一条都不投
func TestM3_Inbox_CountersignMidVoteSendsNothing(t *testing.T) {
	f := newInboxFixture(t)
	id := f.newInstance(t, 500, "会签单")

	// 会签第 1 票:Result=false
	f.publisher.PublishApproval(context.Background(), ApprovalEvent{
		InstanceID: id, Action: EvApprovalApproved, Result: false,
	})
	f.assertNoMessage(t)
}

// TestM3_Inbox_CancelledSendsNothing 作废不投
func TestM3_Inbox_CancelledSendsNothing(t *testing.T) {
	f := newInboxFixture(t)
	id := f.newInstance(t, 500, "资料审核")

	f.publisher.PublishApproval(context.Background(), ApprovalEvent{
		InstanceID: id, Action: EvApprovalCancelled,
	})
	f.assertNoMessage(t)
}

// TestM3_Inbox_InstanceNotFoundSendsNothing 实例没了 → 跳过,不 panic
func TestM3_Inbox_InstanceNotFoundSendsNothing(t *testing.T) {
	f := newInboxFixture(t)

	assert.NotPanics(t, func() {
		f.publisher.PublishApproval(context.Background(), ApprovalEvent{
			InstanceID: 999999, Action: EvApprovalAssigned, AssigneeID: 1000,
		})
	})
	f.assertNoMessage(t)
}

// TestM3_Inbox_SurvivesCancelledContext ctx 死了也必须投出去
//
// ⚠️ **这条只能直接测 publish** —— 端到端跑一遍 Act 时 ctx 根本不会死,
// 那条断言恒绿,是假绿(同 M88 的 `TestM88_Publisher_SurvivesCancelledContext`)。
func TestM3_Inbox_SurvivesCancelledContext(t *testing.T) {
	f := newInboxFixture(t)
	id := f.newInstance(t, 500, "资料审核")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟"HTTP 请求已超时 / 上游已断开"

	f.publisher.PublishApproval(ctx, ApprovalEvent{
		InstanceID: id, Action: EvApprovalAssigned, AssigneeID: 1000,
	})

	msgs := f.waitMessages(t, 1)
	assert.Equal(t, uint64(1000), msgs[0].UserID,
		"ctx 死了也必须把站内信投出去 —— 拿已取消的 ctx 写 DB 会直接失败,"+
			"症状是「审批成功了但收件人没收到提醒」")
}

// ─────────── ③ 事件照发 + 失败不影响主流程 ───────────

// TestM3_Inbox_StillPublishesEvent 装饰器**不接管**原通道
func TestM3_Inbox_StillPublishesEvent(t *testing.T) {
	f := newInboxFixture(t)
	id := f.newInstance(t, 500, "资料审核")

	f.publisher.PublishApproval(context.Background(), ApprovalEvent{
		InstanceID: id, Action: EvApprovalAssigned, AssigneeID: 1000,
	})

	evs := f.innerEvents()
	require.Len(t, evs, 1, "事件必须照发(Redis 通道不受影响)")
	assert.Equal(t, EvApprovalAssigned, evs[0].Action)
	assert.Equal(t, id, evs[0].InstanceID)
	assert.Equal(t, uint64(1000), evs[0].AssigneeID)
}

// TestM3_Inbox_NotifierErrorDoesNotBreak 投递失败不许影响审批主流程
func TestM3_Inbox_NotifierErrorDoesNotBreak(t *testing.T) {
	f := newInboxFixture(t)
	id := f.newInstance(t, 500, "资料审核")
	n := &errNotifier{reached: make(chan struct{}, 1)}
	p := NewInboxApprovalPublisher(f.inner, n, f.db)

	assert.NotPanics(t, func() {
		p.PublishApproval(context.Background(), ApprovalEvent{
			InstanceID: id, Action: EvApprovalApproved, Result: true,
		})
	})
	select {
	case <-n.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("投递器没被调用")
	}
	// 事件照发 —— 站内信投递失败**不许**把事件通道一起带走
	assert.Len(t, f.inner.evs, 1, "站内信投递失败,事件仍要照发")
}

// TestM3_Inbox_NotifierPanicIsolated 投递器 panic 不许打死进程
func TestM3_Inbox_NotifierPanicIsolated(t *testing.T) {
	f := newInboxFixture(t)
	id := f.newInstance(t, 500, "资料审核")
	n := &panicNotifier{reached: make(chan struct{}, 1)}
	p := NewInboxApprovalPublisher(f.inner, n, f.db)

	assert.NotPanics(t, func() {
		p.PublishApproval(context.Background(), ApprovalEvent{
			InstanceID: id, Action: EvApprovalApproved, Result: true,
		})
	})
	select {
	case <-n.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("投递器没被调用")
	}
	// ⚠️ **给 recover 一点时间** —— 没有 recover 的话,那个 panic 会在
	// 这里把整个测试进程打死(测试以"整个包崩了"的形式失败)。
	time.Sleep(200 * time.Millisecond)
	assert.Len(t, f.inner.evs, 1)
}

// TestM3_Inbox_NilNotifierDoesNotPanic 没有投递器 = 退化成只发事件
func TestM3_Inbox_NilNotifierDoesNotPanic(t *testing.T) {
	db := newTestDB(t)
	inner := &recordingPublisher{}
	p := NewInboxApprovalPublisher(inner, nil, db)

	assert.NotPanics(t, func() {
		p.PublishApproval(context.Background(), ApprovalEvent{
			InstanceID: 1, Action: EvApprovalApproved, Result: true,
		})
	})
	time.Sleep(150 * time.Millisecond)
	assert.Len(t, inner.evs, 1)
}

// TestM3_Inbox_NilDBDoesNotPanic 没有 db = 读不到实例 = 退化成只发事件
func TestM3_Inbox_NilDBDoesNotPanic(t *testing.T) {
	inner := &recordingPublisher{}
	p := NewInboxApprovalPublisher(inner, inbox.New(nil), nil)

	assert.NotPanics(t, func() {
		p.PublishApproval(context.Background(), ApprovalEvent{
			InstanceID: 1, Action: EvApprovalApproved, Result: true,
		})
	})
	time.Sleep(150 * time.Millisecond)
	assert.Len(t, inner.evs, 1)
}

// ─────────── ④ 引擎侧的 Result 标志 ───────────

// TestM3_Engine_ResultFlagDistinguishesCountersign
//
// 🔴 **这一条是 M3 全部设计的落点** —— 会签里"这一票记下了"与整单通过
// 用的是**同一个 action**(`approved`)。如果引擎不给 Result,旁路就只能猜,
// 而猜错的后果是给发起人发一条**假消息**("会签到一半就报审批已通过")。
func TestM3_Engine_ResultFlagDistinguishesCountersign(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_m3_all2", []string{model.ApprovalReviewAll}, []int{2})
	ctx := context.Background()

	id := f.draft(t, 500, "会签单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	// submit → 逐个审批人各一条 assigned
	evs := f.publisher.evs
	require.Len(t, evs, 2)
	for _, e := range evs {
		assert.Equal(t, EvApprovalAssigned, e.Action)
		assert.False(t, e.Result, "新待办不是结果")
	}

	// 第 1 票 —— 还剩 1 个人,整单没定局
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
	evs = f.publisher.evs
	require.Len(t, evs, 3)
	last := evs[2]
	assert.Equal(t, EvApprovalApproved, last.Action)
	assert.False(t, last.Result,
		"会签的中间票**不是结果** —— 带上它就是给发起人发假消息")
	assert.Equal(t, model.ApprovalStatusRunning, f.inst(t, id).Status)

	// 第 2 票 —— 齐了,终局
	require.NoError(t, f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove}))
	evs = f.publisher.evs
	require.Len(t, evs, 4)
	last = evs[3]
	assert.Equal(t, EvApprovalApproved, last.Action)
	assert.True(t, last.Result, "整单通过是结果 —— 发起人必须知道")
	assert.Equal(t, model.ApprovalStatusApproved, f.inst(t, id).Status)
}

// TestM3_Engine_ResultFlagOnOrSign 或签(ApprovalReviewAny)的第一票就是终局
//
// ⚠️ **这条覆盖的是 `finishApproved` 那条路径** —— 与会签的终局
// (`enterStage` 里"没有下一关")**是两条不同的代码路径**,两个都要盯。
//
// ⚠️ **这条是被探针逼出来的**:2026-10-24 的 P1 探针把 `finishApproved` 的
// `Result: true` 去掉,测试**照样绿** —— 因为当时只有会签用例,终局走的是
// `enterStage`。**用例没覆盖到的路径 = 改坏了也不知道。**
func TestM3_Engine_ResultFlagOnOrSign(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_m3_any", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()

	id := f.draft(t, 500, "或签单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))

	evs := f.publisher.evs
	require.Len(t, evs, 2)
	assert.Equal(t, EvApprovalApproved, evs[1].Action)
	assert.True(t, evs[1].Result, "或签的第一票就是终局 —— 发起人必须知道")
	assert.Equal(t, model.ApprovalStatusApproved, f.inst(t, id).Status)
}

// TestM3_Engine_ResultFlagOnRejectAndRollback 驳回 / 打回都是"结果"
func TestM3_Engine_ResultFlagOnRejectAndRollback(t *testing.T) {
	t.Run("驳回 → Result=true", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_m3_rej", []string{model.ApprovalReviewAny}, []int{1})
		ctx := context.Background()

		id := f.draft(t, 500, "驳回单", "{}")
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
			Action: ActReject, Comment: "材料不齐,重新上传",
		}))

		evs := f.publisher.evs
		require.Len(t, evs, 2)
		assert.Equal(t, EvApprovalRejected, evs[1].Action)
		assert.True(t, evs[1].Result, "驳回是结果 —— 发起人必须知道")
	})

	t.Run("打回 → Result=true", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_m3_rb", []string{model.ApprovalReviewAny}, []int{1})
		ctx := context.Background()

		id := f.draft(t, 500, "打回单", "{}")
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
			Action: ActRollback, Comment: "改一下再提交",
		}))

		evs := f.publisher.evs
		require.Len(t, evs, 2)
		assert.Equal(t, EvApprovalRolledBack, evs[1].Action)
		assert.True(t, evs[1].Result, "打回是结果 —— 发起人必须知道")
	})

	t.Run("作废 → Result=false", func(t *testing.T) {
		// ⚠️ 作废是发起人自己做的 —— 不通知自己。
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_m3_cancel", []string{model.ApprovalReviewAny}, []int{1})
		ctx := context.Background()

		id := f.draft(t, 500, "作废单", "{}")
		require.NoError(t, f.engine.Cancel(ctx, callerOf(500), id))

		evs := f.publisher.evs
		require.Len(t, evs, 1)
		assert.Equal(t, EvApprovalCancelled, evs[0].Action)
		assert.False(t, evs[0].Result)
	})
}

// TestM3_Inbox_EngineEndToEnd 引擎 → 站内信 的端到端
//
// ⚠️ **这一条是"接缝接上了"的证明** —— 引擎发事件 → 装饰器投站内信 →
// 库里真的多了一条消息,收件人是该收到的那个人。
func TestM3_Inbox_EngineEndToEnd(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_m3_e2e", []string{model.ApprovalReviewAny}, []int{1})

	// 把引擎的 publisher 换成「事件 + 站内信」双通道
	inner := &recordingPublisher{}
	notifier := inbox.New(f.db)
	f.engine = NewApprovalEngine(f.db, f.resolver,
		map[string]BizHandler{testBizType: f.biz},
		NewInboxApprovalPublisher(inner, notifier, f.db))

	ctx := context.Background()
	id := f.draft(t, 500, "端到端单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	// 审批人 1000 收到待办
	require.Eventually(t, func() bool {
		var n int64
		if err := f.db.Model(&model.InboxMessage{}).
			Where("user_id = ?", uint64(1000)).Count(&n).Error; err != nil {
			return false
		}
		return n == 1
	}, 3*time.Second, 20*time.Millisecond, "审批人应当收到待办站内信")

	// 审批人 1000 通过 → 发起人 500 收到结果
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
	require.Eventually(t, func() bool {
		var n int64
		if err := f.db.Model(&model.InboxMessage{}).
			Where("user_id = ?", uint64(500)).Count(&n).Error; err != nil {
			return false
		}
		return n == 1
	}, 3*time.Second, 20*time.Millisecond, "发起人应当收到结果站内信")

	var msgs []model.InboxMessage
	require.NoError(t, f.db.Where("user_id = ?", uint64(500)).Find(&msgs).Error)
	require.Len(t, msgs, 1)
	assert.Equal(t, "【审批已通过】端到端单", msgs[0].Title)
	assert.Contains(t, msgs[0].Link, fmt.Sprintf("/approval/%d", id))
}
