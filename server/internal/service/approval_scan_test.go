// 审批超时巡检单测(M88)
//
// 文档:docs/通用审批系统设计方案V1.0.md §9.4
//
// ⚠️ **这里用的是真的 `inbox.Manager`,不是 fake** —— 去重是拿
// `sys_inbox_message` 里的 (ref_type, ref_id, created_at) 判的。
// fake notifier 不落库,「第二次跑不重复告警」那条断言就成了永远绿。
//
// ⚠️ **断言一律查库,不复用返回值** —— 同 M86 的规矩:返回值就是被测对象,
// 拿它证明自己等于没测。
//
// ⚠️ **每条用例都盯一个"恒真/恒假"的坑** —— 这份实现里最容易写坏的地方
// 全是「集合为空时的判断」和「时间下界的比较」,它们不会报错,只会安静地
// 少发 / 多发告警。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/sonyflake"
)

// ============================================================================
// 夹具
// ============================================================================

type scanFixture struct {
	rf       *resolverFixture
	notifier inbox.Notifier
}

func newScanFixture(t *testing.T) *scanFixture {
	t.Helper()
	rf := newResolverFixture(t)
	return &scanFixture{rf: rf, notifier: inbox.New(rf.db)}
}

// addInstance 造一条 running 的审批实例
func (f *scanFixture) addInstance(
	t *testing.T, applicant uint64, submittedAt time.Time,
) uint64 {
	t.Helper()
	return f.addInstanceAtStage(t, applicant, submittedAt, 1)
}

// addInstanceAtStage 造一条 running 的审批实例(可指定当前关卡)
func (f *scanFixture) addInstanceAtStage(
	t *testing.T, applicant uint64, submittedAt time.Time, stage int,
) uint64 {
	t.Helper()
	inst := &model.SysApprovalInstance{
		BaseModel:       model.BaseModel{ID: sonyflake.Next()},
		BizType:         "scan_test",
		BizID:           sonyflake.Next(),
		Title:           "巡检用例",
		ApplicantUserID: applicant,
		Status:          model.ApprovalStatusRunning,
		CurrentStage:    stage,
		SubmittedAt:     &submittedAt,
	}
	require.NoError(t, f.rf.db.Create(inst).Error)
	return inst.ID
}

// addTask 造一条审批任务
func (f *scanFixture) addTask(
	t *testing.T, instID uint64, stage int, assignee uint64, status string,
) {
	t.Helper()
	task := &model.SysApprovalTask{
		BaseModel:      model.BaseModel{ID: sonyflake.Next()},
		InstanceID:     instID,
		StageIndex:     stage,
		AssigneeUserID: assignee,
		Status:         status,
	}
	require.NoError(t, f.rf.db.Create(task).Error)
}

// messages 读回站内信
func (f *scanFixture) messages(t *testing.T) []model.InboxMessage {
	t.Helper()
	var rows []model.InboxMessage
	require.NoError(t, f.rf.db.Model(&model.InboxMessage{}).Order("id ASC").Find(&rows).Error)
	return rows
}

// byRef 按 ref_type 过滤站内信
func (f *scanFixture) byRef(t *testing.T, refType string) []model.InboxMessage {
	t.Helper()
	out := make([]model.InboxMessage, 0)
	for _, m := range f.messages(t) {
		if m.RefType == refType {
			out = append(out, m)
		}
	}
	return out
}

// scan 跑一次巡检
func (f *scanFixture) scan(t *testing.T) error {
	t.Helper()
	return scanStaleApprovals(context.Background(), f.rf.db, f.notifier)
}

// ============================================================================
// ① 审批人全部停用 / 离职 → 告警平台管理员
// ============================================================================

func TestM88_Scan_AlertsWhenAllAssigneesGone(t *testing.T) {
	f := newScanFixture(t)

	admin := f.rf.addUser(t, "admin", true, false)
	ops := f.rf.addUser(t, "ops", true, false)
	f.rf.addIdentity(t, admin, 0, model.RoleSuperAdmin, true, time.Time{}, time.Time{})
	f.rf.addIdentity(t, ops, 0, model.RolePlatformOps, true, time.Time{}, time.Time{})

	// 审批人已离职
	gone := f.rf.addUser(t, "gone", true, true)
	applicant := f.rf.addUser(t, "applicant", true, false)

	instID := f.addInstance(t, applicant, time.Now())
	f.addTask(t, instID, 1, gone, model.ApprovalTaskPending)

	require.NoError(t, f.scan(t))

	msgs := f.byRef(t, refTypeApprovalNoApprover)
	require.Len(t, msgs, 2, "超管 + 平台运营负责人各收一条")
	got := map[uint64]bool{}
	for _, m := range msgs {
		got[m.UserID] = true
	}
	assert.True(t, got[admin], "超管要收到告警")
	assert.True(t, got[ops], "平台运营负责人要收到告警")
	assert.Equal(t, instID, msgs[0].RefID, "告警要指回是哪张单")
	assert.Contains(t, msgs[0].Content, "scan_test", "告警正文要带上业务类型")

	assert.Empty(t, f.byRef(t, refTypeApprovalStalled),
		"刚提交的单子不该被算成「卡住」")
}

func TestM88_Scan_NoAlertWhenOneAssigneeStillActive(t *testing.T) {
	f := newScanFixture(t)

	admin := f.rf.addUser(t, "admin", true, false)
	f.rf.addIdentity(t, admin, 0, model.RoleSuperAdmin, true, time.Time{}, time.Time{})

	gone := f.rf.addUser(t, "gone", true, true)
	alive := f.rf.addUser(t, "alive", true, false)
	applicant := f.rf.addUser(t, "applicant", true, false)

	instID := f.addInstance(t, applicant, time.Now())
	f.addTask(t, instID, 1, gone, model.ApprovalTaskPending)
	f.addTask(t, instID, 1, alive, model.ApprovalTaskPending)

	require.NoError(t, f.scan(t))
	assert.Empty(t, f.byRef(t, refTypeApprovalNoApprover),
		"还有一个审批人在岗,不算「全部离职」")
}

// ============================================================================
// ② 空集合 / stale task —— 两个恒真的坑
// ============================================================================

func TestM88_Scan_NoAssigneesIsNotAllGone(t *testing.T) {
	f := newScanFixture(t)

	admin := f.rf.addUser(t, "admin", true, false)
	f.rf.addIdentity(t, admin, 0, model.RoleSuperAdmin, true, time.Time{}, time.Time{})

	applicant := f.rf.addUser(t, "applicant", true, false)
	// 没有任何 task —— 空集上做「全部」判断是恒真
	f.addInstance(t, applicant, time.Now())

	require.NoError(t, f.scan(t))
	assert.Empty(t, f.byRef(t, refTypeApprovalNoApprover),
		"当前关没有待办人 ≠ 审批人全离职")
}

func TestM88_Scan_StaleTaskNotCountedAsAssignee(t *testing.T) {
	f := newScanFixture(t)

	admin := f.rf.addUser(t, "admin", true, false)
	f.rf.addIdentity(t, admin, 0, model.RoleSuperAdmin, true, time.Time{}, time.Time{})

	gone := f.rf.addUser(t, "gone", true, true)
	applicant := f.rf.addUser(t, "applicant", true, false)

	instID := f.addInstance(t, applicant, time.Now())
	// 上一轮打回留下的 stale task —— 不该被算成当前关的审批人
	f.addTask(t, instID, 1, gone, model.ApprovalTaskStale)

	require.NoError(t, f.scan(t))
	assert.Empty(t, f.byRef(t, refTypeApprovalNoApprover),
		"stale task 不是当前关的待办人")
}

func TestM88_Scan_TaskOfOtherStageNotCounted(t *testing.T) {
	f := newScanFixture(t)

	admin := f.rf.addUser(t, "admin", true, false)
	f.rf.addIdentity(t, admin, 0, model.RoleSuperAdmin, true, time.Time{}, time.Time{})

	gone := f.rf.addUser(t, "gone", true, true)
	applicant := f.rf.addUser(t, "applicant", true, false)

	instID := f.addInstanceAtStage(t, applicant, time.Now(), 2)
	// 第 1 关的 task,但实例已经推进到第 2 关
	f.addTask(t, instID, 1, gone, model.ApprovalTaskPending)

	require.NoError(t, f.scan(t))
	assert.Empty(t, f.byRef(t, refTypeApprovalNoApprover),
		"上一关的待办人不是当前关的待办人")
}

// ============================================================================
// ③ 停留超 7 天 → 告警发起人
// ============================================================================

func TestM88_Scan_AlertsApplicantWhenStalled(t *testing.T) {
	f := newScanFixture(t)

	applicant := f.rf.addUser(t, "applicant", true, false)
	reviewer := f.rf.addUser(t, "reviewer", true, false)

	instID := f.addInstance(t, applicant, time.Now().Add(-8*24*time.Hour))
	f.addTask(t, instID, 1, reviewer, model.ApprovalTaskPending)

	require.NoError(t, f.scan(t))

	msgs := f.byRef(t, refTypeApprovalStalled)
	require.Len(t, msgs, 1, "只告警发起人")
	assert.Equal(t, applicant, msgs[0].UserID)
	assert.Equal(t, instID, msgs[0].RefID)
	assert.Contains(t, msgs[0].Content, "8", "告警正文要带上停留天数")
}

func TestM88_Scan_NoAlertWhenNotStalledLongEnough(t *testing.T) {
	f := newScanFixture(t)

	applicant := f.rf.addUser(t, "applicant", true, false)
	reviewer := f.rf.addUser(t, "reviewer", true, false)

	instID := f.addInstance(t, applicant, time.Now().Add(-3*24*time.Hour))
	f.addTask(t, instID, 1, reviewer, model.ApprovalTaskPending)

	require.NoError(t, f.scan(t))
	assert.Empty(t, f.byRef(t, refTypeApprovalStalled), "才 3 天,不到 7 天的线")
}

// ============================================================================
// ④ 去重(幂等)
// ============================================================================

func TestM88_Scan_DedupesSecondRun(t *testing.T) {
	f := newScanFixture(t)

	admin := f.rf.addUser(t, "admin", true, false)
	f.rf.addIdentity(t, admin, 0, model.RoleSuperAdmin, true, time.Time{}, time.Time{})

	gone := f.rf.addUser(t, "gone", true, true)
	applicant := f.rf.addUser(t, "applicant", true, false)

	instID := f.addInstance(t, applicant, time.Now())
	f.addTask(t, instID, 1, gone, model.ApprovalTaskPending)

	require.NoError(t, f.scan(t))
	require.Len(t, f.byRef(t, refTypeApprovalNoApprover), 1)

	// 每天跑一次,第二天不该再发一遍同样的告警
	require.NoError(t, f.scan(t))
	assert.Len(t, f.byRef(t, refTypeApprovalNoApprover), 1,
		"同一轮提交里不该重复告警")
}

func TestM88_Scan_ResubmitAllowsNewAlert(t *testing.T) {
	f := newScanFixture(t)

	applicant := f.rf.addUser(t, "applicant", true, false)
	reviewer := f.rf.addUser(t, "reviewer", true, false)

	// 第 1 轮:10 天前提交,8 天前就卡住了 → 告警过一次
	instID := f.addInstance(t, applicant, time.Now().Add(-10*24*time.Hour))
	f.addTask(t, instID, 1, reviewer, model.ApprovalTaskPending)

	require.NoError(t, f.scan(t))
	require.Len(t, f.byRef(t, refTypeApprovalStalled), 1)

	// ⚠️ **把那条告警的 created_at 往前挪** —— 站内信由 inbox.Manager 写
	// created_at = time.Now(),而"上一轮是 10 天前告的警"这个事实只能靠改
	// 时间戳模拟。这不是为了让用例通过而改代码,是**把时钟拨回去**。
	require.NoError(t, f.rf.db.Model(&model.InboxMessage{}).
		Where("ref_type = ?", refTypeApprovalStalled).
		Update("created_at", time.Now().Add(-10*24*time.Hour)).Error)

	// 发起人改完重提(8 天前)→ submitted_at 刷新 → 那一轮该重新允许告警
	require.NoError(t, f.rf.db.Model(&model.SysApprovalInstance{}).
		Where("id = ?", instID).
		Update("submitted_at", time.Now().Add(-8*24*time.Hour)).Error)

	require.NoError(t, f.scan(t))
	assert.Len(t, f.byRef(t, refTypeApprovalStalled), 2,
		"重提之后是新一轮,应该能再提醒一次")
}

func TestM88_Scan_TwoAlertKindsDoNotBlockEachOther(t *testing.T) {
	f := newScanFixture(t)

	admin := f.rf.addUser(t, "admin", true, false)
	f.rf.addIdentity(t, admin, 0, model.RoleSuperAdmin, true, time.Time{}, time.Time{})

	gone := f.rf.addUser(t, "gone", true, true)
	applicant := f.rf.addUser(t, "applicant", true, false)

	// 同一张单同时命中两条:审批人全离职 + 停了 8 天
	instID := f.addInstance(t, applicant, time.Now().Add(-8*24*time.Hour))
	f.addTask(t, instID, 1, gone, model.ApprovalTaskPending)

	require.NoError(t, f.scan(t))
	assert.Len(t, f.byRef(t, refTypeApprovalNoApprover), 1, "离职告警要发")
	assert.Len(t, f.byRef(t, refTypeApprovalStalled), 1,
		"卡住告警不能被离职告警的去重记录挡掉")
}

// ============================================================================
// ⑤ 边界:没人可告警 / ctx 取消
// ============================================================================

func TestM88_Scan_NoRecipientIsNotAnError(t *testing.T) {
	f := newScanFixture(t)

	// 没配超管 / 平台运营负责人
	gone := f.rf.addUser(t, "gone", true, true)
	applicant := f.rf.addUser(t, "applicant", true, false)

	instID := f.addInstance(t, applicant, time.Now())
	f.addTask(t, instID, 1, gone, model.ApprovalTaskPending)

	require.NoError(t, f.scan(t), "没人可告警不是错误,不能让整个任务失败")
	assert.Empty(t, f.byRef(t, refTypeApprovalNoApprover))
}

func TestM88_Scan_RespectsCancelledContext(t *testing.T) {
	f := newScanFixture(t)

	admin := f.rf.addUser(t, "admin", true, false)
	f.rf.addIdentity(t, admin, 0, model.RoleSuperAdmin, true, time.Time{}, time.Time{})

	gone := f.rf.addUser(t, "gone", true, true)
	applicant := f.rf.addUser(t, "applicant", true, false)

	instID := f.addInstance(t, applicant, time.Now())
	f.addTask(t, instID, 1, gone, model.ApprovalTaskPending)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := scanStaleApprovals(ctx, f.rf.db, f.notifier)
	require.Error(t, err, "ctx 死了就必须停手,不能继续发告警")
	assert.Empty(t, f.messages(t), "ctx 取消后一条告警都不该发出去")
}

func TestM88_Scan_MissingDepsError(t *testing.T) {
	f := newScanFixture(t)
	assert.Error(t, scanStaleApprovals(context.Background(), nil, f.notifier))
	assert.Error(t, scanStaleApprovals(context.Background(), f.rf.db, nil))
}

// ============================================================================
// ⑥ 任务注册(cronx 契约)
// ============================================================================

func TestM88_Scan_CronExprParsesWithRepoParser(t *testing.T) {
	// 🔴 **本仓的 cronx 解析器是 5 段,没有 Seconds** ——
	// 设计稿 §9.4 的示例写的是 `"0 10 3 * * *"`,在本仓解析器上直接报错。
	// 这条用例把这个事实钉住:谁把设计稿那一行抄进代码,这里就红。
	_, err := cronx.ParseCron("10 3 * * *")
	require.NoError(t, err, "巡检用的表达式必须能被本仓解析器接受")

	_, err = cronx.ParseCron("0 10 3 * * *")
	assert.Error(t, err, "6 段表达式在本仓解析器上是错的(没有 Seconds)")
}

func TestM88_Scan_JobKeyUsesFrameworkPrefix(t *testing.T) {
	assert.Equal(t, "framework.approval.timeout_scan", KeyApprovalTimeoutScan,
		"框架层任务的 key 前缀必须是 framework.*(§9.4)")
}

func TestM88_Scan_RegisterIsIdempotentlyGuarded(t *testing.T) {
	// ⚠️ **同一个进程里只能注册一次** —— cronx.Register 撞 key 会 panic。
	//   这里注册一次,再断言第二次会 panic(这是有意的设计,不是 bug)。
	f := newScanFixture(t)
	RegisterApprovalScanJob(f.rf.db, f.notifier)
	assert.Panics(t, func() {
		RegisterApprovalScanJob(f.rf.db, f.notifier)
	}, "重复注册必须 panic,不能悄悄覆盖")
}
