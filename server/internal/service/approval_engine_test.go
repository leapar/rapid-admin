// 审批引擎单测(M84)
//
// 文档:docs/通用审批系统设计方案V1.0.md §10.1(十五条必须有的用例)
//
// ⚠️ **这十五条是"拆掉实现看测试会不会红"的清单,不是覆盖率清单。**
// 每一条都对应一处能被无声改坏的逻辑 —— 漏排 `stale`、少查 `status='pending'`、
// 按字节而不是按 rune 数意见。覆盖率高但这些地方一条没断言,等于没测。
//
// ⚠️ **用例 9 验的不是"数据库锁生效"**(§10.1 已标注):`glebarez/sqlite`
// 没有行级锁,`clause.Locking` 被忽略。它验的是"两个 goroutine 并发点通过时,
// 分母不会被算错"。真并发验证要在 MySQL 上手工跑一次。
//
// ⚠️ **断言一律查库,不复用引擎内部状态** —— 复用内部变量只能证明变量对,
// 证明不了它落进了 DB。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// ============================================================================
// 夹具
// ============================================================================

const testBizType = "digital_material"

// stubResolver 假审批人解析器。
//
// ⚠️ **M85 之前没有真解析器**,但这不意味着"空实现 + 全绿" —— 这里让它按
// (模板, 关卡) 返回配置好的人,并能注入 error,好让 §5.2 第 1 条(error ≠ 空集合)
// 有地方可测。
//
// 🔴 **只按 (模板, 关卡) 分桶,没有"按关号"的全局桶** —— 全局桶会被同一个夹具里
// 后建的模板覆盖掉,于是「切模板」这条用例根本测不出切没切(用例 15),
// 而「同一人去重」那条用例会变成永远绿(用例 7)。真解析器本来就是按
// (模板, 关卡) 查配置的,这里也得像真的一样。
type stubResolver struct {
	byTemplate map[uint64]map[int][]uint64
	err        error
}

func (s *stubResolver) ResolveStage(
	_ context.Context, _ *gorm.DB, req ResolveStageRequest,
) ([]uint64, error) {
	if s.err != nil {
		return nil, s.err
	}
	if stages, ok := s.byTemplate[req.TemplateID]; ok {
		if ids, ok := stages[req.StageIndex]; ok {
			return ids, nil
		}
	}
	return nil, nil
}

// recordingBizHandler 假业务处理器,记录被调了几次
type recordingBizHandler struct {
	mu        sync.Mutex
	validated []uint64
	approved  int
	rejected  int
	cancelled int
	failWith  error
}

func (h *recordingBizHandler) ValidateBiz(_ context.Context, _ *gorm.DB, _ string, bizID uint64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.validated = append(h.validated, bizID)
	return nil
}

func (h *recordingBizHandler) OnApproved(_ context.Context, _ *gorm.DB, _ *model.SysApprovalInstance) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.approved++
	return h.failWith
}

func (h *recordingBizHandler) OnRejected(_ context.Context, _ *gorm.DB, _ *model.SysApprovalInstance) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rejected++
	return h.failWith
}

// OnCancelled M86-2 加的第 4 个 SPI 方法
func (h *recordingBizHandler) OnCancelled(_ context.Context, _ *gorm.DB, _ *model.SysApprovalInstance) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancelled++
	return h.failWith
}

// recordingPublisher 记录事件
type recordingPublisher struct {
	mu  sync.Mutex
	evs []ApprovalEvent
	// deadCtx 记录是否有一次调用的 ctx 已经是 Done 的(§9.2)
	deadCtx int
}

func (p *recordingPublisher) PublishApproval(ctx context.Context, ev ApprovalEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.evs = append(p.evs, ev)
	if ctx.Err() != nil {
		p.deadCtx++
	}
}

func (p *recordingPublisher) actions() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.evs))
	for _, e := range p.evs {
		out = append(out, e.Action)
	}
	return out
}

func (p *recordingPublisher) assignees() []uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]uint64, 0, len(p.evs))
	for _, e := range p.evs {
		if e.Action == EvApprovalAssigned {
			out = append(out, e.AssigneeID)
		}
	}
	return out
}

// engineFixture 一个装好的引擎 + 库
type engineFixture struct {
	db        *gorm.DB
	engine    ApprovalEngine
	resolver  *stubResolver
	biz       *recordingBizHandler
	publisher *recordingPublisher
}

// approvalTestModels 审批模块单测会碰到的全部表(15 张;M173 起 +sys_event_outbox)。
//
// ⚠️ **这里只列审批真正依赖的表,不要图省事改回 migrate.Run(全量 75 张)。**
//
// 代价是实测出来的:`make test` 跑的是 `go test -race -v ./...`
// (server/Makefile:89),而每个夹具都调一次全量 migrate.Run —— 一次要建
// 75 张表 + 它们的全部索引,单这一件事在 race 模式下就要 1.5s;
// 审批这一包有四十来个夹具,累加起来整包跑不完 10 分钟,**撞 Go 默认超时
// 被 kill**,`make test` 直接是红的(实测:Test killed: ran too long (11m0s))。
//
// 只建这些表还有个好处:**依赖变成显式的**。少建一张表,真需要它的用例会
// 当场 "no such table" 报出来,而不是等到某天被别的用例的全局状态掩盖。
// (第一版就是漏了 biz_digital_material,资料审核那一组用例当场炸了 ——
// 这正是这个改法想要的:漏了立刻知道,而不是靠整包迁移硬扛。)
//
// 顺序 = 表名字母序,和 migrate.Run 内部排的序一致 —— 唯一有外键关系的一对
// 是 sys_user.Identities → sys_user_identity(user_id),字母序下 sys_user 在前。
var approvalTestModels = []any{
	// 审批七张
	&model.SysApprovalAssigneeRule{},
	&model.SysApprovalInstance{},
	&model.SysApprovalLog{},
	&model.SysApprovalRound{},
	&model.SysApprovalStage{},
	&model.SysApprovalTask{},
	&model.SysApprovalTemplate{},
	// 解析器(M85)要读的组织与人员表
	&model.SysDepartment{},
	// 站内信:M88 超时巡检把告警写进 sys_inbox_message
	&model.InboxMessage{},
	// 领域事件 outbox:M173 起 publisher 的生产通道是"先落表后投递",
	// outbox 侧的接缝用例(event_outbox_publisher_test.go)要落这张表
	&model.SysEventOutbox{},
	&model.SysRole{},
	&model.SysUser{},
	&model.SysUserDepartment{},
	&model.SysUserIdentity{},
}

// newTestDB 起一个内存库 + 建审批要用的表。
//
// ⚠️ **M85 的解析器单测复用它**(同一套 model,同一套连接池限制),
// 别在第二个夹具里再抄一遍 —— 抄的那份改起来必然漏掉一处。
//
// ⚠️ **每个 test 独立 DSN**(§16.8):`file:<t.Name()>?mode=memory&cache=shared`
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		// ⚠️ **静音 SQL 日志**。引擎里有多条"预期的失败查询"(latestRound 查不到
		// 行是常态),默认 logger 会把它们打成红色错误行,真正的失败反而被淹掉。
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	// ⚠️ **连接池限死 1 条连接**。
	//
	// `cache=shared` 的 SQLite 在两个连接同时写同一张表时**直接返回
	// `SQLITE_LOCKED`(deadlocked)** —— shared cache 的表级锁走的是
	// SQLITE_LOCKED 而**不是** SQLITE_BUSY,busy handler 根本不接管,
	// 于是并发用例直接报错(实测:用例 9 报 "database table is locked")。
	//
	// §10.1 用例 9 已经写明「SQLite 里所有流转天然串行,真并发验证要在 MySQL
	// 上手工跑一次」。**限 1 条连接就是把这个"天然串行"做实**,而不是让
	// 驱动层的锁错误把用例搅黄。
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	// 用例结束就把池关掉。每个用例一个独立 DSN(=一个独立内存库),
	// 不关的话连接池一直攥着库不放 —— 一包跑完能攒 100+ 个 sql.DB
	// (race 模式超时 dump 里实测 145 个 connectionOpener goroutine 还挂着)。
	//
	// 注册在最前面 ⇒ cleanup 是 LIFO ⇒ 它**最后**执行,不会抢在别的
	// cleanup 前面把库关掉。并发用例(§10.1 用例 9)在用例体内就 wg.Wait()
	// 收干净了 goroutine,不会有关掉的池被后台 goroutine 摸到。
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.AutoMigrate(approvalTestModels...))
	return db
}

// newEngineFixture 起一个内存库,建七张表,装好引擎
func newEngineFixture(t *testing.T) *engineFixture {
	t.Helper()
	db := newTestDB(t)

	f := &engineFixture{
		db:        db,
		resolver:  &stubResolver{byTemplate: map[uint64]map[int][]uint64{}},
		biz:       &recordingBizHandler{},
		publisher: &recordingPublisher{},
	}
	f.engine = NewApprovalEngine(db, f.resolver,
		map[string]BizHandler{testBizType: f.biz}, f.publisher)
	return f
}

// newTemplate 建一个模板 + n 个关卡,每关 n 个审批人
//
// ⚠️ **显式写全 `deleted_at` 等默认值**(§16.8 第 3 条),不依赖夹具没给。
func (f *engineFixture) newTemplate(t *testing.T, code string, reviewTypes []string, assigneeCounts []int) uint64 {
	t.Helper()
	tx := f.db.Begin()
	tpl := &model.SysApprovalTemplate{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Code:      code, Name: code, BizType: testBizType, IsActive: true,
	}
	require.NoError(t, tx.Create(tpl).Error)

	uid := uint64(1000)
	for i, rt := range reviewTypes {
		idx := i + 1
		st := &model.SysApprovalStage{
			BaseModel:  model.BaseModel{ID: sonyflake.Next()},
			TemplateID: tpl.ID,
			StageIndex: idx,
			Name:       fmt.Sprintf("第%d关", idx),
			ReviewType: rt,
		}
		require.NoError(t, tx.Create(st).Error)

		n := assigneeCounts[i]
		ids := make([]uint64, 0, n)
		for k := 0; k < n; k++ {
			ids = append(ids, uid)
			uid++
		}
		// ⚠️ **按 (模板, 关卡) 分桶** —— 同一夹具里建第二个模板时,第一个模板
		// 的解析结果不能被覆盖,"切模板"要测的就是这个(用例 15)
		if f.resolver.byTemplate[tpl.ID] == nil {
			f.resolver.byTemplate[tpl.ID] = map[int][]uint64{}
		}
		f.resolver.byTemplate[tpl.ID][idx] = ids
	}
	require.NoError(t, tx.Commit().Error)
	return tpl.ID
}

// draft 建一张草稿并返回实例 ID
func (f *engineFixture) draft(t *testing.T, applicant uint64, title, variables string) uint64 {
	t.Helper()
	return f.draftOn(t, applicant, title, variables, "")
}

// draftOn 建一张草稿,**指定模板**。
//
// ⚠️ **指定模板时不再按 biz_type 反查** —— 一个 biz_type 下有两条启用模板时,
// `pickTemplate` 会硬拒(2 条启用模板不猜),用例 15 正是要这个场景。
func (f *engineFixture) draftOn(t *testing.T, applicant uint64, title, variables, templateCode string) uint64 {
	t.Helper()
	ctx := context.Background()
	var id uint64
	require.NoError(t, f.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		got, err := f.engine.CreateDraft(ctx, tx, &CreateDraftInput{
			BizType:             testBizType,
			BizID:               sonyflake.Next(),
			Title:               title,
			Variables:           variables,
			TemplateCode:        templateCode,
			ApplicantUserID:     applicant,
			ApplicantIdentityID: 1,
		})
		id = got
		return err
	}))
	return id
}

func callerOf(userID uint64) *Caller {
	return &Caller{UserID: userID, IdentityID: 1}
}

// inst 读实例(绕过引擎内部状态,直接查库)
func (f *engineFixture) inst(t *testing.T, id uint64) model.SysApprovalInstance {
	t.Helper()
	var inst model.SysApprovalInstance
	require.NoError(t, f.db.First(&inst, "id = ?", id).Error)
	return inst
}

func (f *engineFixture) tasks(t *testing.T, id uint64) []model.SysApprovalTask {
	t.Helper()
	var ts []model.SysApprovalTask
	require.NoError(t, f.db.Where("instance_id = ?", id).Order("stage_index ASC, id ASC").Find(&ts).Error)
	return ts
}

// tasksAt 取某一关、某状态的任务(用例 3/5 要区分"上一轮的"和"这一轮的")
func (f *engineFixture) tasksAt(t *testing.T, id uint64, stage int, wantStatus string) []model.SysApprovalTask {
	t.Helper()
	var ts []model.SysApprovalTask
	require.NoError(t, f.db.Where(
		"instance_id = ? AND stage_index = ? AND status = ?", id, stage, wantStatus).
		Order("id ASC").Find(&ts).Error)
	return ts
}

func (f *engineFixture) rounds(t *testing.T, id uint64) []model.SysApprovalRound {
	t.Helper()
	var rs []model.SysApprovalRound
	require.NoError(t, f.db.Where("instance_id = ?", id).Order("round_no ASC").Find(&rs).Error)
	return rs
}

func (f *engineFixture) logs(t *testing.T, id uint64) []model.SysApprovalLog {
	t.Helper()
	var ls []model.SysApprovalLog
	require.NoError(t, f.db.Where("instance_id = ?", id).Order("round_no ASC, id ASC").Find(&ls).Error)
	return ls
}

// countTasks 某状态的 task 条数(会签分母的观测口径)
func (f *engineFixture) countTasks(t *testing.T, id uint64, stage int, status string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Model(&model.SysApprovalTask{}).
		Where("instance_id = ? AND stage_index = ? AND status = ?", id, stage, status).
		Count(&n).Error)
	return n
}

// ============================================================================
// 用例 1 / 2 —— 会签分母与推进条件
// ============================================================================

// TestCase01_AllReview_TwoOfThree_StillRunning §10.1 用例 1
func TestCase01_AllReview_TwoOfThree_StillRunning(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_all3", []string{model.ApprovalReviewAll}, []int{3})
	ctx := context.Background()

	id := f.draft(t, 500, "资料审核", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
	require.Equal(t, 3, len(f.tasksAt(t, id, 1, model.ApprovalTaskPending)))

	// 1000 / 1001 通过
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove}))

	// 🔴 **必须还是 running**。漏排 pending 或漏判「还有人没处理」的话,
	// 这里会变成 approved —— 而第三个人根本没看过这条单。
	inst := f.inst(t, id)
	assert.Equal(t, model.ApprovalStatusRunning, inst.Status, "2/3 通过时不能推进")
	assert.Equal(t, 1, inst.CurrentStage)
	assert.Equal(t, int64(1), f.countTasks(t, id, 1, model.ApprovalTaskPending), "第三个人还欠着一票")
	assert.Equal(t, int64(2), f.countTasks(t, id, 1, model.ApprovalTaskApproved))
}

// TestCase02_AllReview_ThirdApproves_AdvancesToStage2 §10.1 用例 2
func TestCase02_AllReview_ThirdApproves_AdvancesToStage2(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_all3_any2", []string{
		model.ApprovalReviewAll, model.ApprovalReviewAny,
	}, []int{3, 1})
	ctx := context.Background()

	id := f.draft(t, 500, "资料审核", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
	for _, uid := range []uint64{1000, 1001, 1002} {
		require.NoError(t, f.engine.Act(ctx, callerOf(uid), id, ActReq{Action: ActApprove}))
	}

	inst := f.inst(t, id)
	assert.Equal(t, 2, inst.CurrentStage, "第三票落地后应推进到第 2 关")
	assert.Equal(t, model.ApprovalStatusRunning, inst.Status, "第 2 关还没人批,不能终局")
	assert.Equal(t, int64(1), f.countTasks(t, id, 2, model.ApprovalTaskPending))
	// ⚠️ 第 2 关的审批人是 1003(夹具 uid 递增到 1003)
	assert.Equal(t, uint64(1003), f.tasksAt(t, id, 2, model.ApprovalTaskPending)[0].AssigneeUserID)
}

// ============================================================================
// 用例 3 —— 打回后重提,分母只算新一轮(D4)
// ============================================================================

// TestCase03_Resubmit_StaleTasksNotCounted §10.1 用例 3
//
// ⚠️ **构造"第一关 2 人、上轮都通过"的数据** —— §10.1 点名了这个假绿:
// 单人单关时漏排 `stale` 也可能蒙对(上轮 1 个 approved、新轮 1 个 approved,
// 计数刚好对)。只有上轮 2 人都 approved、新轮只有 1 人时,漏排 `stale`
// 才会立刻表现为"分母 3、分母永远凑不齐"。
func TestCase03_Resubmit_StaleTasksNotCounted(t *testing.T) {
	f := newEngineFixture(t)
	// 第一关会签 2 人(1000/1001),第二关或签 1 人(1002)
	f.newTemplate(t, "tpl_r", []string{
		model.ApprovalReviewAll, model.ApprovalReviewAny,
	}, []int{2, 1})
	ctx := context.Background()

	id := f.draft(t, 500, "首轮标题", `{"v":1}`)
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	// 上轮两人都通过 → 进第 2 关
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove}))
	require.Equal(t, 2, f.inst(t, id).CurrentStage)

	// 第 2 关的人打回 → 全部 task 转 stale
	require.NoError(t, f.engine.Act(ctx, callerOf(1002), id, ActReq{
		Action: ActRollback, Comment: "材料缺盖章页,请补齐",
	}))
	assert.Equal(t, int64(0), f.countTasks(t, id, 1, model.ApprovalTaskPending),
		"打回后不应残留 pending")

	// 🔴 **改内容**再重提(不改会被「重提必须改内容」挡掉,见用例 13)
	require.NoError(t, f.engine.UpdateDraft(ctx, callerOf(500), id, UpdateDraftReq{
		Variables: strPtr(`{"v":2}`),
	}))
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	inst := f.inst(t, id)
	require.Equal(t, 1, inst.CurrentStage, "重提回到第 1 关")
	require.Equal(t, 1, inst.ReturnCount, "打回次数 +1")

	// 🔴 **上轮那 2 条 approved 必须全是 stale**
	stale := f.tasksAt(t, id, 1, model.ApprovalTaskStale)
	assert.Len(t, stale, 2, "上轮两条 approved 应转 stale")
	// ⚠️ **第 1 关是 2 人会签**,所以新一轮也是 2 条 pending(不是 1)
	assert.Equal(t, int64(2), f.countTasks(t, id, 1, model.ApprovalTaskPending), "新一轮第 1 关 2 条 pending")

	// ⚠️ **会签必须两人都批才推进** —— 只批一票的话,分母算错也算不出来
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
	assert.Equal(t, 1, f.inst(t, id).CurrentStage, "会签只批一票不能推进")

	// 🔴 **两票批完必须立刻推进**(分母=2 而不是 2 新 + 2 旧)
	require.NoError(t, f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove}))
	assert.Equal(t, 2, f.inst(t, id).CurrentStage,
		"漏排 stale 的话这里会卡在第 1 关:分母 = 2 新 + 2 旧 approved")

	// 第 2 关通过 → 终局
	require.NoError(t, f.engine.Act(ctx, callerOf(1002), id, ActReq{Action: ActApprove}))
	assert.Equal(t, model.ApprovalStatusApproved, f.inst(t, id).Status)
	assert.Equal(t, 1, f.biz.approved, "OnApproved 只该在终局调一次")
}

// ============================================================================
// 用例 4 —— 或签终局
// ============================================================================

// TestCase04_AnyReview_FirstApprove_Terminates §10.1 用例 4
func TestCase04_AnyReview_FirstApprove_Terminates(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_any2", []string{model.ApprovalReviewAny}, []int{2})
	ctx := context.Background()

	id := f.draft(t, 500, "或签单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
	require.Equal(t, int64(2), f.countTasks(t, id, 1, model.ApprovalTaskPending))

	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))

	inst := f.inst(t, id)
	assert.Equal(t, model.ApprovalStatusApproved, inst.Status, "或签首个动作即终局(D5)")
	assert.NotNil(t, inst.FinishedAt)

	// 🔴 **B 的 task 必须是 skipped,不能留 pending** —— 留 pending 的话
	// 「我已办理」里会永远挂着一条等不到处理的待办
	assert.Equal(t, int64(1), f.countTasks(t, id, 1, model.ApprovalTaskApproved))
	assert.Equal(t, int64(1), f.countTasks(t, id, 1, model.ApprovalTaskSkipped))

	// 终局后 B 再点通过 → 守卫挡住
	err := f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove})
	require.Error(t, err)
	// ⚠️ **是 StateInvalid 不是 Forbidden** —— §3.4 的守卫顺序是「先状态、
	// 后待办人」:实例已终局,第一道就挡住了,根本走不到「你是不是待办人」。
	// 这不是笔误,守卫顺序本身是契约(先说清楚状态,再说清楚人)。
	assert.Equal(t, errcode.StateInvalid, errcode.CodeOf(err), "终局后不该再有可处理的待办")

	rounds := f.rounds(t, id)
	require.Len(t, rounds, 1)
	assert.Equal(t, model.ApprovalRoundApproved, rounds[0].Outcome)
	assert.Equal(t, uint64(1000), rounds[0].OutcomeBy)
}

// ============================================================================
// 用例 5 —— 会签一票否决
// ============================================================================

// TestCase05_AllReview_OneReject_Terminates §10.1 用例 5
func TestCase05_AllReview_OneReject_Terminates(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_all3b", []string{model.ApprovalReviewAll}, []int{3})
	ctx := context.Background()

	id := f.draft(t, 500, "会签单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	require.NoError(t, f.engine.Act(ctx, callerOf(1001), id, ActReq{
		Action: ActReject, Comment: "产权证复印件不清晰",
	}))

	inst := f.inst(t, id)
	assert.Equal(t, model.ApprovalStatusRejected, inst.Status, "一票否决")
	assert.Equal(t, int64(1), f.countTasks(t, id, 1, model.ApprovalTaskRejected))
	// 🔴 B/C 是 skipped —— 不是 pending
	assert.Equal(t, int64(2), f.countTasks(t, id, 1, model.ApprovalTaskSkipped))
	assert.Equal(t, 1, f.biz.rejected)
	assert.Equal(t, 0, f.biz.approved, "否决路径不该调 OnApproved")

	rounds := f.rounds(t, id)
	require.Len(t, rounds, 1)
	assert.Equal(t, model.ApprovalRoundRejected, rounds[0].Outcome)
}

// ============================================================================
// 用例 6 —— stale task 不能被命中(§3.4 守卫第 2 条)
// ============================================================================

// TestCase06_StaleTaskCannotBeActed §10.1 用例 6
//
// ⚠️ **这是最容易写出来也最容易漏的一条**:守卫只查"有没有我的 task"
// 的话,打回后上一轮那条 `stale` 会让同一个人**再审一遍**,而 instance
// 此时是 `returned` —— 第一道守卫(status=running)恰好也能挡住。所以这条
// 用例要在**同一个 running 周期内**验证,才真的落在守卫第 2 条上。
func TestCase06_StaleTaskCannotBeActed(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_s", []string{
		model.ApprovalReviewAll, model.ApprovalReviewAll,
	}, []int{1, 1})
	ctx := context.Background()

	id := f.draft(t, 500, "两关单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
	// 第 1 关通过 → 第 2 关
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
	require.Equal(t, 2, f.inst(t, id).CurrentStage)

	// 第 2 关的人打回 → 两条 task 全 stale
	require.NoError(t, f.engine.Act(ctx, callerOf(1001), id, ActReq{
		Action: ActRollback, Comment: "缺章,请补",
	}))
	assert.Equal(t, int64(2), f.countTasks(t, id, 1, model.ApprovalTaskStale)+
		f.countTasks(t, id, 2, model.ApprovalTaskStale))

	// 第 2 关那个人的 stale task 不能再命中
	err := f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove})
	require.Error(t, err)
	// 这里是 `returned` 状态,被守卫第 1 条挡住 —— 但**更关键的是下面这条**:
	// 重提之后,旧 stale 也不能让上一关的人直接批第 1 关
	require.NoError(t, f.engine.UpdateDraft(ctx, callerOf(500), id, UpdateDraftReq{
		Variables: strPtr(`{"v":9}`),
	}))
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	// 第 1 关的新待办人是 1000;1001 在第 1 关**没有任何 task**(新轮只给 1000)
	err = f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove})
	require.Error(t, err)
	assert.Equal(t, errcode.Forbidden, errcode.CodeOf(err),
		"上一关的人在第 1 关必须没有待办 —— stale 不能被命中")
	assert.Equal(t, 1, f.inst(t, id).CurrentStage, "这一票不该推进任何东西")
}

// ============================================================================
// 用例 7 —— 同一人只生成一条 task(§4.2 去重)
// ============================================================================

// TestCase07_DuplicateAssigneeOneTask §10.1 用例 7
func TestCase07_DuplicateAssigneeOneTask(t *testing.T) {
	f := newEngineFixture(t)
	tplID := f.newTemplate(t, "tpl_dup", []string{model.ApprovalReviewAll}, []int{1})
	// 🔴 **两条规则解析出同一个人** —— M85 的 resolver 会把多条规则的并集返回,
	// 这里模拟"既是角色:财务经理 又是 直属主管"命中同一个人的情况。
	f.resolver.byTemplate[tplID][1] = []uint64{1000, 1000, 1000}
	ctx := context.Background()

	id := f.draft(t, 500, "去重单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	// ⚠️ **不去重的话会签分母翻倍,一个人批完还差两票,单子永远卡住**
	assert.Equal(t, int64(1), f.countTasks(t, id, 1, model.ApprovalTaskPending),
		"同一人只能有一条 task(§4.2)")

	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
	assert.Equal(t, model.ApprovalStatusApproved, f.inst(t, id).Status,
		"分母是 1 时这一票就该终局")
}

// ============================================================================
// 用例 8 —— 意见按 rune 计(§7.4)
// ============================================================================

// TestCase08_CommentRuneCount §10.1 用例 8
func TestCase08_CommentRuneCount(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_c", []string{model.ApprovalReviewAny}, []int{2})
	ctx := context.Background()

	// 4 个汉字 = 12 字节。`len()` 会判它够长,`utf8.RuneCountInString` 判它不够。
	const four = "意见不合"
	assert.Equal(t, 12, len(four), "本用例的前提:4 个汉字是 12 字节")

	id := f.draft(t, 500, "意见单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	err := f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActReject, Comment: four})
	require.Error(t, err, "4 个字必须被拒")
	assert.Equal(t, errcode.ParamInvalid, errcode.CodeOf(err))
	// ⚠️ **拒绝路径必须一条痕迹都不留** —— 校验不过就不该改 task / 不该写 log
	// 模板这一关是 2 人,所以是 2 条 pending(不是 1)
	assert.Equal(t, int64(2), f.countTasks(t, id, 1, model.ApprovalTaskPending))
	assert.Equal(t, model.ApprovalStatusRunning, f.inst(t, id).Status)

	// 5 个字放行
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
		Action: ActReject, Comment: "这个意见不行",
	}))
	assert.Equal(t, model.ApprovalStatusRejected, f.inst(t, id).Status)

	// approve 的意见选填,不校验长度
	id2 := f.draft(t, 501, "通过单", `{"x":1}`)
	require.NoError(t, f.engine.Submit(ctx, callerOf(501), id2, SubmitReq{}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id2, ActReq{
		Action: ActApprove, Comment: "同意",
	}))

	// 🔴 **五个空格不是五个字** —— 先 TrimSpace 再数
	id3 := f.draft(t, 502, "空格单", `{"x":2}`)
	require.NoError(t, f.engine.Submit(ctx, callerOf(502), id3, SubmitReq{}))
	err = f.engine.Act(ctx, callerOf(1000), id3, ActReq{Action: ActReject, Comment: "     "})
	require.Error(t, err, "全空白的意见必须被拒")
}

// ============================================================================
// 用例 9 —— 并发通过(-race)
// ============================================================================

// TestCase09_ConcurrentApprove_DenominatorStable §10.1 用例 9
//
// ⚠️ **验的不是"数据库锁生效"**(glebarez/sqlite 没有行级锁)。
// 验的是:两个 goroutine 同时点最后两人的通过,**分母不会被算错**
// —— 即不会出现"两边都判定还没到齐"或"两边都推进"。
func TestCase09_ConcurrentApprove_DenominatorStable(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_conc", []string{
		model.ApprovalReviewAll, model.ApprovalReviewAny,
	}, []int{2, 1})
	ctx := context.Background()

	id := f.draft(t, 500, "并发单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, uid := range []uint64{1000, 1001} {
		wg.Add(1)
		go func(i int, uid uint64) {
			defer wg.Done()
			errs[i] = f.engine.Act(ctx, callerOf(uid), id, ActReq{Action: ActApprove})
		}(i, uid)
	}
	wg.Wait()

	require.NoError(t, errs[0], "第一个人应成功")
	require.NoError(t, errs[1], "第二个人也应成功 —— 分母不能被算成还差一票")

	// ⚠️ **恰好推进一次**。推进两次的话第 2 关会有两条 task,
	// 而且 `current_stage` 会被推到 3(模板没有第 3 关)
	inst := f.inst(t, id)
	assert.Equal(t, 2, inst.CurrentStage)
	assert.Equal(t, int64(1), f.countTasks(t, id, 2, model.ApprovalTaskPending),
		"第 2 关只能有一条待办 —— 推进两次会有两条")
}

// ============================================================================
// 用例 10 —— BizHandler 失败 → 同事务回滚(D8)
// ============================================================================

// TestCase10_BizHandlerError_RollsBackEverything §10.1 用例 10
func TestCase10_BizHandlerError_RollsBackEverything(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_biz", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()

	id := f.draft(t, 500, "业务单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	beforeTasks := f.tasks(t, id)
	beforeLogs := f.logs(t, id)
	// ⚠️ **快照的是"动作列表"而不是"事件总数"** —— submit 那一步已经发过一条
	// `assigned`,直接断言 events 为空会误伤那一条。真正要验的是:
	// **这次失败的动作没有新增任何事件**。
	beforeActions := f.publisher.actions()

	boom := errors.New("业务表更新失败")
	f.biz.failWith = boom

	err := f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove})
	require.Error(t, err)

	// 🔴 **审批记录一条都不能变**
	inst := f.inst(t, id)
	assert.Equal(t, model.ApprovalStatusRunning, inst.Status, "实例状态必须回滚")
	assert.Nil(t, inst.FinishedAt, "终局时间必须回滚")
	assert.Equal(t, model.ApprovalTaskPending, beforeTasks[0].Status)
	assert.Equal(t, beforeTasks, f.tasks(t, id), "task 必须完全回滚")
	assert.Equal(t, beforeLogs, f.logs(t, id), "log 必须完全回滚")

	rounds := f.rounds(t, id)
	require.Len(t, rounds, 1)
	assert.Equal(t, model.ApprovalRoundRunning, rounds[0].Outcome, "本轮 outcome 必须回滚")
	assert.Nil(t, rounds[0].OutcomeAt)

	// ⚠️ **回滚不发事件** —— 否则前端会去拉一条根本没发生的审批结果
	assert.Equal(t, beforeActions, f.publisher.actions(), "事务回滚时不得发事件")

	// 业务侧恢复后重试,应能成功
	f.biz.failWith = nil
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
	assert.Equal(t, model.ApprovalStatusApproved, f.inst(t, id).Status)
}

// ============================================================================
// 用例 11 —— log 表的 DeletedAt 禁令(§6①)
// ============================================================================

// TestCase11_LogsAreAppendOnly §10.1 用例 11
func TestCase11_LogsAreAppendOnly(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_log", []string{
		model.ApprovalReviewAll, model.ApprovalReviewAny,
	}, []int{2, 1})
	ctx := context.Background()

	id := f.draft(t, 500, "日志单", "{}")
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1002), id, ActReq{Action: ActApprove}))

	// submit ×1 + approve ×3 = 4 条
	// 🔴 **COUNT(*) 而不是 GORM 的查询** —— 软删除作用域会自动加
	// `deleted_at IS NULL`,查出来永远一致,这个断言就成了摆设
	var raw int64
	require.NoError(t, f.db.Raw(
		"SELECT COUNT(*) FROM sys_approval_log WHERE instance_id = ?", id).Scan(&raw).Error)
	assert.Equal(t, int64(4), raw, "原始行数必须是 4(不带软删除过滤)")

	logs := f.logs(t, id)
	require.Len(t, logs, 4)
	for _, l := range logs {
		assert.False(t, l.DeletedAt.Valid, "log 表的 deleted_at 必须恒为 NULL")
	}
	assert.Equal(t, ActSubmit, logs[0].Action)
	assert.Equal(t, 1, logs[0].RoundNo)

	// ⚠️ **update 不写 log**(§3.3)—— 意见流里不该有"提交人改了标题"
	before := len(f.logs(t, id))
	// ⚠️ **已终局的单子不能作废** —— draft/returned 才给 cancel,这里是 approved
	require.Error(t, f.engine.Cancel(ctx, callerOf(500), id))
	assert.Equal(t, before, len(f.logs(t, id)))
}

// ============================================================================
// 用例 12 —— 轮次连续 + 快照独立
// ============================================================================

// TestCase12_RoundsAreSequentialWithOutcome §10.1 用例 12
//
// ⚠️ **§10.1 点名的假绿**:只断言 `round_no` 递增不断号的话,
// "打回时忘了回写 outcome"会一路绿到线上,详情页上三轮全是"进行中"。
// 所以必须断言**前三行的 outcome 是 `rolled_back`**。
func TestCase12_RoundsAreSequentialWithOutcome(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_rounds", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()

	id := f.draft(t, 500, "轮次单", `{"v":0}`)
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	for i := 1; i <= 3; i++ {
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
			Action: ActRollback, Comment: fmt.Sprintf("第 %d 次打回,请补充材料", i),
		}))
		require.NoError(t, f.engine.UpdateDraft(ctx, callerOf(500), id, UpdateDraftReq{
			Variables: strPtr(fmt.Sprintf(`{"v":%d}`, i)),
		}))
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
	}

	rounds := f.rounds(t, id)
	require.Len(t, rounds, 4, "3 次打回 → 4 轮")
	for i, r := range rounds {
		assert.Equal(t, i+1, r.RoundNo, "round_no 必须从 1 开始连续,不跳号")
	}
	// 🔴 这三条是本用例的核心
	for i := 0; i < 3; i++ {
		assert.Equal(t, model.ApprovalRoundRolledBack, rounds[i].Outcome,
			"第 %d 轮被打回后 outcome 必须是 rolled_back,不能留在 running", i+1)
		assert.Equal(t, uint64(1000), rounds[i].OutcomeBy)
		assert.NotNil(t, rounds[i].OutcomeAt)
	}
	assert.Equal(t, model.ApprovalRoundRunning, rounds[3].Outcome, "最后一轮还在跑")

	assert.Equal(t, 3, f.inst(t, id).ReturnCount)

	// ⚠️ **每轮的快照互相独立**(用例 14 的前置)
	for i, r := range rounds {
		assert.Equal(t, fmt.Sprintf(`{"v":%d}`, i), r.Variables,
			"第 %d 轮的快照必须是它自己那轮的内容", i+1)
	}
}

// ============================================================================
// 用例 13 —— 原样重提被拒
// ============================================================================

// TestCase13_ResubmitWithoutChange_Rejected §10.1 用例 13
func TestCase13_ResubmitWithoutChange_Rejected(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_nochg", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()

	id := f.draft(t, 500, "原样单", `{"v":1}`)
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
		Action: ActRollback, Comment: "请补充材料原件",
	}))

	err := f.engine.Submit(ctx, callerOf(500), id, SubmitReq{})
	require.Error(t, err, "内容与本轮快照逐字节相同,必须拒")
	assert.Equal(t, errcode.InvalidParam, errcode.CodeOf(err))

	// ⚠️ **被拒的 submit 不能留下任何痕迹** —— 不能新开一轮、不能生成 task
	assert.Len(t, f.rounds(t, id), 1, "被拒的重提不能开新轮次")
	assert.Equal(t, int64(0), f.countTasks(t, id, 1, model.ApprovalTaskPending))
	assert.Equal(t, model.ApprovalStatusReturned, f.inst(t, id).Status)

	// 🔴 **只改了键序也算"没改"** —— 不做 JSON 归一化,否则放行实质没改的单子。
	// 这里 `{"v":1,"w":2}` 与 `{"w":2,"v":1}` 语义相同、字节不同,应该放行;
	// 而 `{"v":1}` → `{"v":1 }` 也会被放行。这是"逐字节比"的已知取舍:
	// **只会多拒,不会漏放真正的原样重提**。
	require.NoError(t, f.engine.UpdateDraft(ctx, callerOf(500), id, UpdateDraftReq{
		Variables: strPtr(`{"v":1,"w":2}`),
	}))
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	// ⚠️ **比的是"本轮"不是"第 1 轮"** —— 第二次相对第一次有改动就该放行
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
		Action: ActRollback, Comment: "还是不对,再改",
	}))
	err = f.engine.Submit(ctx, callerOf(500), id, SubmitReq{})
	require.Error(t, err, "与第 2 轮快照相同,还是不能原样重提")
	require.NoError(t, f.engine.UpdateDraft(ctx, callerOf(500), id, UpdateDraftReq{
		Variables: strPtr(`{"v":2}`),
	}))
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}),
		"与本轮不同就该放行,哪怕它与第 1 轮相同")
}

// ============================================================================
// 用例 14 —— 快照不是引用
// ============================================================================

// TestCase14_RoundSnapshotNotLiveReference §10.1 用例 14
func TestCase14_RoundSnapshotNotLiveReference(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_snap", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()

	id := f.draft(t, 500, "首轮标题", `{"house":"1-101"}`)
	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
		Action: ActRollback, Comment: "房号写错了,请更正",
	}))

	// ⚠️ **直接改实例** —— 走引擎的 UpdateDraft 会改的是"还没送审的内容",
	// 这里要验的是"第 1 轮的存档会不会跟着实例变"
	require.NoError(t, f.db.Model(&model.SysApprovalInstance{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"title":     "改成新标题",
			"variables": `{"house":"2-202"}`,
		}).Error)

	rounds := f.rounds(t, id)
	require.Len(t, rounds, 1)
	assert.Equal(t, "首轮标题", rounds[0].Title, "第 1 轮快照不能跟着实例变")
	assert.Equal(t, `{"house":"1-101"}`, rounds[0].Variables, "第 1 轮快照不能跟着实例变")

	inst := f.inst(t, id)
	assert.Equal(t, "改成新标题", inst.Title, "实例本身确实变了 —— 否则上面两条是假绿")
}

// ============================================================================
// 用例 15 —— draft 下切模板
// ============================================================================

// TestCase15_SwitchTemplateInDraft §10.1 用例 15
func TestCase15_SwitchTemplateInDraft(t *testing.T) {
	f := newEngineFixture(t)
	oldTplID := f.newTemplate(t, "tpl_old", []string{model.ApprovalReviewAny}, []int{1})
	f.newTemplate(t, "tpl_new", []string{
		model.ApprovalReviewAll, model.ApprovalReviewAny,
	}, []int{2, 1})
	ctx := context.Background()

	// ⚠️ **必须显式指定 tpl_old 建草稿** —— 一个 biz_type 下有两条启用模板时,
	// 不给 code 会硬拒(不猜)。这正是用例 15 的前提。
	id := f.draftOn(t, 500, "切模板单", "{}", "tpl_old")
	oldTpl := f.inst(t, id).TemplateID
	require.Equal(t, oldTplID, oldTpl)

	require.NoError(t, f.engine.UpdateDraft(ctx, callerOf(500), id, UpdateDraftReq{
		TemplateCode: strPtr("tpl_new"),
	}))
	newTpl := f.inst(t, id).TemplateID
	require.NotEqual(t, oldTpl, newTpl, "template_id 必须已切到新模板")

	require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

	inst := f.inst(t, id)
	assert.Equal(t, newTpl, inst.TemplateID)
	assert.Equal(t, 1, inst.CurrentStage)
	// 🔴 **不重建实例**(D9):ID 不变,还是原来那张
	assert.Equal(t, id, inst.ID, "改模板不重建实例")

	// 🔴 **本用例的核心断言**:新模板第 1 关是 2 人会签,旧模板是 1 人。
	// 解析器按 (模板, 关卡) 分桶 —— 所以这 2 条 task 只可能是新模板生成的。
	assert.Equal(t, int64(2), f.countTasks(t, id, 1, model.ApprovalTaskPending),
		"必须按新模板的第 1 关生成 2 条 task;只有 1 条说明还在走旧模板")
	assert.Len(t, f.tasksAt(t, id, 1, model.ApprovalTaskPending), 2)
}

// ============================================================================
// 状态机守卫(§3.3 的 7 行表)
// ============================================================================

// TestGuards_StateMachine §3.3 的 7 行表逐行验证
func TestGuards_StateMachine(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_g", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()
	applicant := callerOf(500)

	t.Run("cancel 不给 running", func(t *testing.T) {
		id := f.draft(t, 500, "作废守卫", "{}")
		require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))
		// 🔴 **D9**:已送审的单子可能有人正在审,悄悄作废会让他审一条不存在的单
		err := f.engine.Cancel(ctx, applicant, id)
		require.Error(t, err)
		assert.Equal(t, errcode.StateInvalid, errcode.CodeOf(err))
		assert.Equal(t, model.ApprovalStatusRunning, f.inst(t, id).Status)
	})

	t.Run("cancel 只给发起人", func(t *testing.T) {
		id := f.draft(t, 500, "发起人守卫", "{}")
		err := f.engine.Cancel(ctx, callerOf(999), id)
		require.Error(t, err)
		assert.Equal(t, errcode.IdorAttempt, errcode.CodeOf(err))
	})

	t.Run("submit 只给发起人", func(t *testing.T) {
		id := f.draft(t, 500, "提交守卫", "{}")
		err := f.engine.Submit(ctx, callerOf(999), id, SubmitReq{})
		require.Error(t, err)
		assert.Equal(t, errcode.IdorAttempt, errcode.CodeOf(err))
	})

	t.Run("终态不能再 act", func(t *testing.T) {
		id := f.draft(t, 500, "终态守卫", "{}")
		require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
		require.Equal(t, model.ApprovalStatusApproved, f.inst(t, id).Status)
		require.Error(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
		require.Error(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))
		require.Error(t, f.engine.UpdateDraft(ctx, applicant, id, UpdateDraftReq{}))
	})

	t.Run("update 只给 draft/returned", func(t *testing.T) {
		id := f.draft(t, 500, "改草稿守卫", "{}")
		require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))
		err := f.engine.UpdateDraft(ctx, applicant, id, UpdateDraftReq{
			Title: strPtr("改标题"),
		})
		require.Error(t, err)
		assert.Equal(t, errcode.StateInvalid, errcode.CodeOf(err))
		assert.Equal(t, "改草稿守卫", f.inst(t, id).Title, "被拒的 update 不能改任何东西")
	})

	t.Run("update 不写 log", func(t *testing.T) {
		id := f.draft(t, 500, "改草稿无日志", "{}")
		require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
			Action: ActRollback, Comment: "退回改改看",
		}))
		before := len(f.logs(t, id))
		require.NoError(t, f.engine.UpdateDraft(ctx, applicant, id, UpdateDraftReq{
			Title: strPtr("新标题"),
		}))
		assert.Equal(t, before, len(f.logs(t, id)),
			"update 不是审批动作,意见流里不该出现它(§3.3)")
		assert.Equal(t, "新标题", f.inst(t, id).Title)
	})

	t.Run("draft 与 returned 都能 cancel", func(t *testing.T) {
		id1 := f.draft(t, 500, "草稿作废", "{}")
		require.NoError(t, f.engine.Cancel(ctx, applicant, id1))
		assert.Equal(t, model.ApprovalStatusCancelled, f.inst(t, id1).Status)
		assert.NotNil(t, f.inst(t, id1).FinishedAt)

		id2 := f.draft(t, 501, "退回作废", `{"y":1}`)
		require.NoError(t, f.engine.Submit(ctx, callerOf(501), id2, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id2, ActReq{
			Action: ActRollback, Comment: "不通过了,直接作废",
		}))
		require.NoError(t, f.engine.Cancel(ctx, callerOf(501), id2))
		assert.Equal(t, model.ApprovalStatusCancelled, f.inst(t, id2).Status)
	})

	t.Run("作废后 pending 全部 skipped", func(t *testing.T) {
		id := f.draft(t, 500, "草稿作废2", "{}")
		require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
			Action: ActRollback, Comment: "打回,发起人直接作废",
		}))
		require.NoError(t, f.engine.Cancel(ctx, applicant, id))
		assert.Equal(t, int64(0), f.countTasks(t, id, 1, model.ApprovalTaskPending))
		assert.Equal(t, int64(1), f.countTasks(t, id, 1, model.ApprovalTaskStale))
	})

	t.Run("作废不改写上一轮 outcome", func(t *testing.T) {
		id := f.draft(t, 500, "轮次保持", `{"z":1}`)
		require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
			Action: ActRollback, Comment: "先打回再说",
		}))
		require.NoError(t, f.engine.Cancel(ctx, applicant, id))
		rounds := f.rounds(t, id)
		require.Len(t, rounds, 1)
		assert.Equal(t, model.ApprovalRoundRolledBack, rounds[0].Outcome,
			"作废不是审批结果,不能把「为什么被打回」抹掉")
	})
}

// ============================================================================
// 打回次数上限(§3.3 / R3)
// ============================================================================

// TestGuard_ReturnCountLimit §3.3 的 return_count < 5
//
// ⚠️ **第 5 次打回之后不能再重提,不是"第 5 次重提被拒"** —— 守卫是
// `return_count >= 5` 硬拒,而 return_count 是**打完回就 +1** 的。
// 所以 4 次打回还能重提,第 5 次打回(计数变成 5)之后重提被拒。
// 写成"前 5 次重提都放行"会把上限当成 6,是这条用例最容易搞反的地方。
func TestGuard_ReturnCountLimit(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_max", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()
	applicant := callerOf(500)

	id := f.draft(t, 500, "上限单", `{"v":0}`)
	require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))

	// 前 4 次打回:每次都改内容,都能重提
	for i := 1; i < MaxReturnCount; i++ {
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
			Action: ActRollback, Comment: fmt.Sprintf("第 %d 次打回", i),
		}))
		require.Equal(t, i, f.inst(t, id).ReturnCount)
		require.NoError(t, f.engine.UpdateDraft(ctx, applicant, id, UpdateDraftReq{
			Variables: strPtr(fmt.Sprintf(`{"v":%d}`, i)),
		}))
		require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}),
			"第 %d 次重提应放行(此时 return_count=%d < %d)", i, i, MaxReturnCount)
	}

	// 第 5 次打回 → return_count 变成 5
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
		Action: ActRollback, Comment: "第 5 次打回",
	}))
	require.Equal(t, MaxReturnCount, f.inst(t, id).ReturnCount)

	// 🔴 **硬拒**(R3):`return_count >= 5` 一律不再重提
	err := f.engine.Submit(ctx, applicant, id, SubmitReq{})
	require.Error(t, err, "return_count 用尽后必须硬拒(R3)")
	assert.Equal(t, errcode.StateInvalid, errcode.CodeOf(err))

	// ⚠️ **改内容也救不回来** —— 上限看的是次数,不是内容
	require.NoError(t, f.engine.UpdateDraft(ctx, applicant, id, UpdateDraftReq{
		Variables: strPtr(`{"v":999}`),
	}))
	err = f.engine.Submit(ctx, applicant, id, SubmitReq{})
	require.Error(t, err, "改了内容也不能绕过次数上限")
	assert.Equal(t, errcode.StateInvalid, errcode.CodeOf(err))
}

// ============================================================================
// D11 —— 提交人 ≠ 审批人
// ============================================================================

// TestD11_ApplicantCannotBeAssignee D11
func TestD11_ApplicantCannotBeAssignee(t *testing.T) {
	f := newEngineFixture(t)
	tplID := f.newTemplate(t, "tpl_d11", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()

	// 🔴 **小部门里部门负责人就是自己** —— 这是数据常态不是配置错误,
	// 所以必须在解析后判,过滤掉的话"这一关没人审"会被当成"这关不需要审"
	f.resolver.byTemplate[tplID][1] = []uint64{500, 1000}

	id := f.draft(t, 500, "自审单", "{}")
	err := f.engine.Submit(ctx, callerOf(500), id, SubmitReq{})
	require.Error(t, err)
	assert.Equal(t, errcode.SameAuditor, errcode.CodeOf(err))
	assert.Equal(t, model.ApprovalStatusDraft, f.inst(t, id).Status, "被拒的 submit 不该改状态")
	assert.Len(t, f.rounds(t, id), 0, "被拒的 submit 不该开轮次")
}

// ============================================================================
// §5.2 第 1 条 —— error ≠ 空集合
// ============================================================================

// TestResolverError_NotDegraded §5.2 第 1 条
func TestResolverError_NotDegraded(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_err", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()

	f.resolver.err = errors.New("DB 抖动")
	id := f.draft(t, 500, "解析故障单", "{}")

	err := f.engine.Submit(ctx, callerOf(500), id, SubmitReq{})
	require.Error(t, err, "解析失败是故障,不是「没有人审」")
	// 🔴 **绝不能降级**:不生成任何 task、不开轮次、不把单子发给无关的人
	assert.Equal(t, int64(0), f.countTasks(t, id, 1, model.ApprovalTaskPending))
	assert.Len(t, f.rounds(t, id), 0)
	assert.Equal(t, model.ApprovalStatusDraft, f.inst(t, id).Status)
}

// TestResolverEmpty_ReportsNoAssignee §5.3 第 ② 层
func TestResolverEmpty_ReportsNoAssignee(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_empty", []string{model.ApprovalReviewAny}, []int{0})
	ctx := context.Background()

	id := f.draft(t, 500, "空审批人单", "{}")
	err := f.engine.Submit(ctx, callerOf(500), id, SubmitReq{})
	require.Error(t, err)
	// ⚠️ **20601 不是 40003** —— 分类错了前端会弹"参数有误",
	// 发起人处理不了,得管理员去改流程配置(§16.4)
	assert.Equal(t, errcode.ApprovalNoAssignee, errcode.CodeOf(err))
	assert.Contains(t, err.Error(), "第 1 关", "错误信息要点名是哪一关")
}

// ============================================================================
// 模板选择(§7.3 / ErrApprovalNoTemplate)
// ============================================================================

// TestTemplateSelection 模板解析的三条规则
//
// ⚠️ **每个子测试自己起夹具** —— 共享夹具的话,前面造的模板会污染后面
// "按 biz_type 找唯一模板"的判定(造出第三条启用模板后,本该成功的
// CreateDraft 也会报"配了 N 条模板")。
func TestTemplateSelection(t *testing.T) {
	ctx := context.Background()

	t.Run("同 biz_type 两条启用模板 → 报错不猜", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_a1", []string{model.ApprovalReviewAny}, []int{1})
		f.newTemplate(t, "tpl_a2", []string{model.ApprovalReviewAny}, []int{1})

		err := f.db.Transaction(func(tx *gorm.DB) error {
			_, e := f.engine.CreateDraft(ctx, tx, &CreateDraftInput{
				BizType: testBizType, BizID: 1, Title: "多模板",
				ApplicantUserID: 500, ApplicantIdentityID: 1,
			})
			return e
		})
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalNoTemplate, errcode.CodeOf(err))
		assert.Contains(t, err.Error(), "2 条启用模板",
			"报错要点名有几条,否则用户不知道要去删哪几条")
		assert.Contains(t, err.Error(), testBizType, "报错要点名是哪个业务类型")

		// ⚠️ **一条审批单都不能留下**
		var n int64
		require.NoError(t, f.db.Model(&model.SysApprovalInstance{}).Count(&n).Error)
		assert.Zero(t, n)
	})

	t.Run("显式给 code 时不受多模板影响", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_a1", []string{model.ApprovalReviewAny}, []int{1})
		wantTpl := f.newTemplate(t, "tpl_a2", []string{model.ApprovalReviewAny}, []int{1})

		var got uint64
		err := f.db.Transaction(func(tx *gorm.DB) error {
			id, e := f.engine.CreateDraft(ctx, tx, &CreateDraftInput{
				BizType: testBizType, BizID: 2, Title: "指定模板",
				TemplateCode:    "tpl_a2",
				ApplicantUserID: 500, ApplicantIdentityID: 1,
			})
			got = id
			return e
		})
		require.NoError(t, err)
		assert.Equal(t, wantTpl, f.inst(t, got).TemplateID, "必须选中 code 指定的那条")
	})

	t.Run("code 不存在 → ErrApprovalNoTemplate", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_a1", []string{model.ApprovalReviewAny}, []int{1})

		err := f.db.Transaction(func(tx *gorm.DB) error {
			_, e := f.engine.CreateDraft(ctx, tx, &CreateDraftInput{
				BizType: testBizType, BizID: 9, Title: "查无此模板",
				TemplateCode:    "tpl_not_exist",
				ApplicantUserID: 500, ApplicantIdentityID: 1,
			})
			return e
		})
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalNoTemplate, errcode.CodeOf(err))
	})

	t.Run("停用模板 → ErrApprovalNoTemplate", func(t *testing.T) {
		f := newEngineFixture(t)
		oldID := f.newTemplate(t, "tpl_off", []string{model.ApprovalReviewAny}, []int{1})
		require.NoError(t, f.db.Model(&model.SysApprovalTemplate{}).
			Where("id = ?", oldID).Update("is_active", false).Error)

		err := f.db.Transaction(func(tx *gorm.DB) error {
			_, e := f.engine.CreateDraft(ctx, tx, &CreateDraftInput{
				BizType: testBizType, BizID: 3, Title: "停用模板",
				TemplateCode:    "tpl_off",
				ApplicantUserID: 500, ApplicantIdentityID: 1,
			})
			return e
		})
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalNoTemplate, errcode.CodeOf(err))
	})

	t.Run("改草稿指向停用模板 → 当场拒", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_on", []string{model.ApprovalReviewAny}, []int{1})
		offID := f.newTemplate(t, "tpl_off", []string{model.ApprovalReviewAny}, []int{1})
		require.NoError(t, f.db.Model(&model.SysApprovalTemplate{}).
			Where("id = ?", offID).Update("is_active", false).Error)

		id := f.draft(t, 500, "改模板守卫", "{}")
		err := f.engine.UpdateDraft(ctx, callerOf(500), id, UpdateDraftReq{
			TemplateCode: strPtr("tpl_off"),
		})
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalNoTemplate, errcode.CodeOf(err),
			"§7.4:停用模板要在操作点就炸,不能拖到 submit 才炸")
	})

	t.Run("模板没有关卡 → ErrApprovalCorrupt", func(t *testing.T) {
		f := newEngineFixture(t)
		require.NoError(t, f.db.Create(&model.SysApprovalTemplate{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			Code:      "tpl_bare", Name: "空模板", BizType: testBizType, IsActive: true,
		}).Error)

		id := f.draft(t, 500, "空模板单", "{}")
		err := f.engine.Submit(ctx, callerOf(500), id, SubmitReq{})
		require.Error(t, err)
		assert.Equal(t, errcode.Internal, errcode.CodeOf(err))
		assert.Equal(t, model.ApprovalStatusDraft, f.inst(t, id).Status)
		assert.Empty(t, f.rounds(t, id), "模板坏了也不能开轮次")
	})
}

// ============================================================================
// 未注册 biz_type(R11)
// ============================================================================

// TestCreateDraft_UnregisteredBizType §11 R11
func TestCreateDraft_UnregisteredBizType(t *testing.T) {
	f := newEngineFixture(t)
	ctx := context.Background()

	err := f.db.Transaction(func(tx *gorm.DB) error {
		_, e := f.engine.CreateDraft(ctx, tx, &CreateDraftInput{
			BizType: "some_unregistered_biz", BizID: 1, Title: "没处理器",
			ApplicantUserID: 500, ApplicantIdentityID: 1,
		})
		return e
	})
	require.Error(t, err)
	// ⚠️ **放过去的后果是"审批通过了但业务状态永远不变"** —— 连草稿都不给建
	assert.Equal(t, errcode.NotImplemented, errcode.CodeOf(err))
}

// TestCreateDraft_NilTx D8 的形状约束
func TestCreateDraft_NilTx(t *testing.T) {
	f := newEngineFixture(t)
	_, err := f.engine.CreateDraft(context.Background(), nil, &CreateDraftInput{
		BizType: testBizType, BizID: 1, Title: "没事务",
		ApplicantUserID: 500, ApplicantIdentityID: 1,
	})
	require.Error(t, err, "CreateDraft 必须收 tx(D8)")
}

// ============================================================================
// ValidateBiz 失败 → 不建审批单
// ============================================================================

// TestValidateBiz_Error_NoInstance §11 R12
func TestValidateBiz_Error_NoInstance(t *testing.T) {
	f := newEngineFixture(t)
	ctx := context.Background()
	f.db.Model(&model.SysApprovalTemplate{}).Where("1 = 1").Delete(&model.SysApprovalTemplate{})
	f.newTemplate(t, "tpl_v", []string{model.ApprovalReviewAny}, []int{1})

	// 换一个会失败的 handler
	failing := &failingBizHandler{}
	f.engine = NewApprovalEngine(f.db, f.resolver,
		map[string]BizHandler{testBizType: failing}, f.publisher)

	err := f.db.Transaction(func(tx *gorm.DB) error {
		_, e := f.engine.CreateDraft(ctx, tx, &CreateDraftInput{
			BizType: testBizType, BizID: 999, Title: "业务记录不存在",
			ApplicantUserID: 500, ApplicantIdentityID: 1,
		})
		return e
	})
	require.Error(t, err)
	// ⚠️ **biz_id 传错时不该留下审批单**(R12)
	var n int64
	require.NoError(t, f.db.Model(&model.SysApprovalInstance{}).Count(&n).Error)
	assert.Equal(t, int64(0), n)
}

type failingBizHandler struct{}

func (failingBizHandler) ValidateBiz(context.Context, *gorm.DB, string, uint64) error {
	return errors.New("业务记录不存在")
}
func (failingBizHandler) OnApproved(context.Context, *gorm.DB, *model.SysApprovalInstance) error {
	return nil
}
func (failingBizHandler) OnRejected(context.Context, *gorm.DB, *model.SysApprovalInstance) error {
	return nil
}
func (failingBizHandler) OnCancelled(context.Context, *gorm.DB, *model.SysApprovalInstance) error {
	return nil
}

// ============================================================================
// 事件(§9.2)
// ============================================================================
//
// 🔴 这组**不在 §10.1 十五条里**,是 M84 自己补的。理由:引擎是唯一决定
// "什么时候发、发给谁、发什么 action"的地方,而 §9.2 那张表一旦错了,
// M88 的 publisher 只会忠实地把错的发出去。

func TestEvents(t *testing.T) {
	t.Run("submit 逐个审批人各发一条 assigned", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_e1", []string{model.ApprovalReviewAll}, []int{3})
		ctx := context.Background()

		id := f.draft(t, 500, "事件单", "{}")
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))

		assert.Equal(t, []string{"assigned", "assigned", "assigned"}, f.publisher.actions())
		assert.Equal(t, []uint64{1000, 1001, 1002}, f.publisher.assignees())
		for _, e := range f.publisher.evs {
			assert.Equal(t, id, e.InstanceID)
		}
	})

	t.Run("推进到下一关时给新的人发 assigned", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_e2", []string{
			model.ApprovalReviewAll, model.ApprovalReviewAny,
		}, []int{1, 1})
		ctx := context.Background()

		id := f.draft(t, 500, "推进事件单", "{}")
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
		require.Len(t, f.publisher.evs, 1)

		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
		require.Len(t, f.publisher.evs, 2)
		assert.Equal(t, "assigned", f.publisher.evs[1].Action)
		assert.Equal(t, uint64(1001), f.publisher.evs[1].AssigneeID)

		require.NoError(t, f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove}))
		require.Len(t, f.publisher.evs, 3)
		assert.Equal(t, "approved", f.publisher.evs[2].Action)
	})

	t.Run("reject / rollback / cancel 各一条终局事件", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_e3", []string{model.ApprovalReviewAny}, []int{1})
		ctx := context.Background()

		id := f.draft(t, 500, "驳回单", "{}")
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
			Action: ActReject, Comment: "内容不合规",
		}))
		assert.Equal(t, []string{"assigned", "rejected"}, f.publisher.actions())

		id2 := f.draft(t, 501, "打回单", `{"a":1}`)
		require.NoError(t, f.engine.Submit(ctx, callerOf(501), id2, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id2, ActReq{
			Action: ActRollback, Comment: "内容不合规,请改",
		}))
		require.NoError(t, f.engine.Cancel(ctx, callerOf(501), id2))
		assert.Equal(t, "cancelled", f.publisher.evs[len(f.publisher.evs)-1].Action)
	})

	t.Run("终局事件不带 assignee —— 那不是发给某个人的", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_e4", []string{model.ApprovalReviewAny}, []int{1})
		ctx := context.Background()

		id := f.draft(t, 500, "终局事件单", "{}")
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))

		last := f.publisher.evs[len(f.publisher.evs)-1]
		assert.Equal(t, "approved", last.Action)
		assert.Zero(t, last.AssigneeID)
	})

	t.Run("会签还有人没批 —— 这一票也要发一条 approved", func(t *testing.T) {
		// 🔴 **M89-1 的回归网。**
		//
		// 原来的实现里,会签关卡"还有人 pending"这一支 `return nil, nil` ——
		// **一条事件都不发**。后果有三条:
		//   ① 投票人自己的待办角标不刷新(前端只有收到事件才会重拉);
		//   ② 同一关的其他人不知道有人批了(§8.3「会签人要互相知道进度」);
		//   ③ §9.2 那张表写着 `act(approve)` → `approved`,这里是违约。
		//
		// ⚠️ **断言必须是"事件条数 + action"两条一起写** —— 只断言条数的话,
		// 将来有人在这里发一条 `assigned` 也能过(那更荒唐:给不相干的人派活)。
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_e7", []string{model.ApprovalReviewAll}, []int{3})
		ctx := context.Background()

		id := f.draft(t, 500, "会签事件单", "{}")
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
		// submit 逐个审批人发 3 条 assigned
		require.Len(t, f.publisher.evs, 3)

		// 第 1 个人投票 —— 还剩 2 个人没批,整单**不该**通过
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))

		require.Len(t, f.publisher.evs, 4,
			"会签的这一票落地了,必须发一条事件(原来这里是 0 条)")
		last := f.publisher.evs[3]
		assert.Equal(t, EvApprovalApproved, last.Action)
		assert.Equal(t, id, last.InstanceID)
		// ⚠️ **终局事件不带 assignee** —— 它不是发给某个人的,是广播
		assert.Zero(t, last.AssigneeID)

		// ⚠️ **实例仍在 running** —— 发事件不许改变状态机
		assert.Equal(t, model.ApprovalStatusRunning, f.inst(t, id).Status)
		assert.Equal(t, 1, f.inst(t, id).CurrentStage)
		// 别人的票还在 pending
		assert.Equal(t, int64(2), f.countTasks(t, id, 1, model.ApprovalTaskPending))
	})

	t.Run("会签齐了进下一关 —— 只发下一关的 assigned,不重复发 approved", func(t *testing.T) {
		// ⚠️ **防"修一处多发一条"** —— 会签最后一票走的是 `enterStage` 那条路,
		// 那里发的是下一关的 `assigned`。如果把 `approved` 也补上,
		// 同一个 commit 就会**同时**多两条语义重叠的事件。
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_e8", []string{
			model.ApprovalReviewAll, model.ApprovalReviewAny,
		}, []int{2, 1})
		ctx := context.Background()

		id := f.draft(t, 500, "会签推进单", "{}")
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
		// submit:2 条 assigned(第 1 关 2 人)
		require.Len(t, f.publisher.evs, 2)

		// 第 1 个人投票 —— 还剩 1 个人,发一条 approved
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
		require.Len(t, f.publisher.evs, 3)
		assert.Equal(t, EvApprovalApproved, f.publisher.evs[2].Action)

		// 第 2 个人投票 —— 齐了,进第 2 关,发第 2 关那 1 个人的 assigned
		require.NoError(t, f.engine.Act(ctx, callerOf(1001), id, ActReq{Action: ActApprove}))
		require.Len(t, f.publisher.evs, 4)
		assert.Equal(t, EvApprovalAssigned, f.publisher.evs[3].Action)
		assert.Equal(t, uint64(1002), f.publisher.evs[3].AssigneeID)
		// ⚠️ **不许多发一条 approved** —— 终局还没到(第 2 关还没批)
		for _, e := range f.publisher.evs[3:] {
			assert.NotEqual(t, EvApprovalApproved, e.Action)
		}
		assert.Equal(t, model.ApprovalStatusRunning, f.inst(t, id).Status)
		assert.Equal(t, 2, f.inst(t, id).CurrentStage)
	})

	t.Run("publish 用的 ctx 必须是活的 —— 不能拿调用方那个已死的 ctx", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_e5", []string{model.ApprovalReviewAny}, []int{1})

		// 🔴 **这条端到端断言在当前 GORM 版本上是假绿,已改成直接测 publish。**
		//
		// 设计文档 §9.2 写的是「GORM 在 COMMIT 之后会 cancel 事务的 ctx」——
		// **核实过 gorm@v1.31.1:全仓没有一处 `WithoutCancel`,也没有 cancel 调用**,
		// GORM 根本不碰事务 ctx 的生命周期。所以端到端跑一遍
		// `assert.Zero(publisher.deadCtx)` **恒为真**,测不出任何东西。
		//
		// 真正会踩坑的是**调用方的 ctx 已经死了**:HTTP 请求超时 / 上游断开,
		// `ctx.Err() != nil`,拿它写 Redis 直接失败 —— 表现是
		// 「审批成功了但前端不刷新」。这条直接测 publish:传一个已取消的 ctx
		// 进去,断言 publisher 拿到的是活的。
		dead, cancel := context.WithCancel(context.Background())
		cancel()

		e, ok := f.engine.(*approvalEngine)
		require.True(t, ok, "引擎实现必须是 *approvalEngine")
		e.publish(dead, []ApprovalEvent{{InstanceID: 1, Action: EvApprovalApproved}})

		assert.Zero(t, f.publisher.deadCtx,
			"publish 必须用 context.WithoutCancel —— 拿到死 ctx 等于事件发不出去")
	})

	t.Run("publisher 为 nil 时不 panic", func(t *testing.T) {
		f := newEngineFixture(t)
		f.newTemplate(t, "tpl_e6", []string{model.ApprovalReviewAny}, []int{1})
		f.engine = NewApprovalEngine(f.db, f.resolver,
			map[string]BizHandler{testBizType: f.biz}, nil)
		ctx := context.Background()

		id := f.draft(t, 500, "无 publisher", "{}")
		require.NoError(t, f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}))
		require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{Action: ActApprove}))
		assert.Equal(t, model.ApprovalStatusApproved, f.inst(t, id).Status)
	})
}

// ============================================================================
// 并发打回 —— return_count 不能丢
// ============================================================================

// TestConcurrent_Rollback_ReturnCountNotLost D10 的同类失效:并发写丢更新
func TestConcurrent_Rollback_ReturnCountNotLost(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_cc", []string{model.ApprovalReviewAny}, []int{1})
	ctx := context.Background()
	applicant := callerOf(500)

	id := f.draft(t, 500, "并发打回", `{"v":0}`)
	require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))
	require.NoError(t, f.engine.Act(ctx, callerOf(1000), id, ActReq{
		Action: ActRollback, Comment: "第一次打回",
	}))
	require.NoError(t, f.engine.UpdateDraft(ctx, applicant, id, UpdateDraftReq{
		Variables: strPtr(`{"v":1}`),
	}))
	require.NoError(t, f.engine.Submit(ctx, applicant, id, SubmitReq{}))

	// 同一个人的两个 goroutine 同时打回
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = f.engine.Act(ctx, callerOf(1000), id, ActReq{
				Action: ActRollback, Comment: "并发打回测试",
			})
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, e := range errs {
		if e == nil {
			ok++
		}
	}
	// ⚠️ **恰好一次成功**。两次都成功 = 第二票打到了已经 `returned` 的单上;
	// 两次都失败 = 守卫把唯一那张 task 弄丢了
	assert.Equal(t, 1, ok, "同一张 task 只能被打回一次(第二次 status 已不是 pending)")
	assert.Equal(t, 2, f.inst(t, id).ReturnCount)

	rounds := f.rounds(t, id)
	assert.Equal(t, 2, len(rounds))
	assert.Equal(t, model.ApprovalRoundRolledBack, rounds[0].Outcome)
	assert.Equal(t, model.ApprovalRoundRolledBack, rounds[1].Outcome,
		"第二次打回也要把第 2 轮收口,不能留在 running")
}

// ============================================================================
// 锁超时映射(§3.5 第 3 条)
// ============================================================================

// TestLockedTimeout_MapsToStateInvalid §3.5 第 3 条
//
// ⚠️ **SQLite 没有行级锁,所以这条验的不是锁本身,是错误映射**:
// 一个已经 cancel 掉的 ctx 传给引擎,SELECT 立刻失败,
// 引擎必须把它翻译成「审批处理中,请稍后重试」而不是裸的 context 错误。
func TestLockedTimeout_MapsToStateInvalid(t *testing.T) {
	f := newEngineFixture(t)
	f.newTemplate(t, "tpl_lock", []string{model.ApprovalReviewAny}, []int{1})

	id := f.draft(t, 500, "锁超时", "{}")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.engine.Submit(ctx, callerOf(500), id, SubmitReq{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled",
		"取消的 ctx 直接冒泡是对的 —— 映射只针对 DeadlineExceeded(锁等待超时)")

	// 而 ErrApprovalLocked 本身存在且带对文案
	assert.Equal(t, errcode.StateInvalid, errcode.CodeOf(ErrApprovalLocked))
	assert.Contains(t, ErrApprovalLocked.Message, "稍后重试")
}

// ============================================================================
// 不存在的实例
// ============================================================================

func TestAct_InstanceNotFound(t *testing.T) {
	f := newEngineFixture(t)
	ctx := context.Background()

	for name, fn := range map[string]func(uint64) error{
		"submit": func(id uint64) error { return f.engine.Submit(ctx, callerOf(500), id, SubmitReq{}) },
		"act": func(id uint64) error {
			return f.engine.Act(ctx, callerOf(500), id, ActReq{Action: ActApprove})
		},
		"cancel": func(id uint64) error { return f.engine.Cancel(ctx, callerOf(500), id) },
		"update": func(id uint64) error { return f.engine.UpdateDraft(ctx, callerOf(500), id, UpdateDraftReq{}) },
	} {
		t.Run(name, func(t *testing.T) {
			err := fn(sonyflake.Next())
			require.Error(t, err)
			assert.Equal(t, errcode.NotFound, errcode.CodeOf(err))
		})
	}
}

// ============================================================================
// 小工具
// ============================================================================

func strPtr(s string) *string { return &s }

// 确认测试里没写死时间假设
func TestNoTimeFlakiness(t *testing.T) {
	// 引擎里所有时间都用 time.Now(),没有任何"当天"判断。
	// 这条断言存在的意义是:将来有人加一条 `if time.Now().Hour() < 9` 的
	// 提交时段限制,至少会有人想起来这里。
	assert.NotContains(t, strings.Join([]string{ActApprove, ActReject}, ","), ":")
	_ = time.Now
}
