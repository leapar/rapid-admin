// 审批单查询 + 建草稿单测(M86)
//
// ⚠️ **这些用例盯的是「列表能不能看到不该看的」和「详情能不能被绕过」两条。**
//
//	① **三个盒子是唯一的列表入口** —— `box` 传空必须报错,
//	   退化成「全部」= 广发 `approval:read` 立刻变成越权(§12 第 9 条)。
//	② **详情可见性 = assertor + isParty 两条路** —— 顺序不能反,
//	   反了 `ActiveGrants`(委托)那条路就被绕过了(§8.3)。
//
// ⚠️ **断言一律查库,不复用 service 的返回值** —— 返回值就是被测对象。
package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/errcode"
)

// ============================================================================
// 夹具
// ============================================================================

// 三个固定的人
const (
	// queryApprover 审批人 —— `engineFixture` 的假解析器把第 1 关的审批人定成 1000
	queryApprover uint64 = 1000
	// queryApplicant 发起人
	queryApplicant uint64 = 2000
	// queryStranger 无关的人
	queryStranger uint64 = 3000
)

type queryFixture struct {
	db  *gorm.DB
	svc ApprovalService
	f   *engineFixture
}

func newQueryFixture(t *testing.T) *queryFixture {
	t.Helper()
	f := newEngineFixture(t)
	return &queryFixture{
		db: f.db,
		// ⚠️ **真的 GenericAssertor** —— 可见性测试要验的就是它那条决策链
		svc: NewApprovalService(f.db, f.engine, authz.NewGenericAssertor()),
		f:   f,
	}
}

// addUser 造一个用户(让列表项里的姓名不是空的)
func (f *queryFixture) addUser(t *testing.T, id uint64, nickname string) {
	t.Helper()
	u := &model.SysUser{
		BaseModel:    model.BaseModel{ID: id},
		Username:     nickname,
		PasswordHash: "x",
		Nickname:     nickname,
		RealName:     nickname,
	}
	require.NoError(t, f.db.Create(u).Error)
}

// running 建一条「审批中」的单子(1 关 1 个审批人 = user 1000)
func (f *queryFixture) running(t *testing.T, applicant uint64, title string) uint64 {
	t.Helper()
	f.f.newTemplate(t, "tpl_"+tplSuffix(t),
		[]string{model.ApprovalReviewAny}, []int{1})
	id := f.f.draft(t, applicant, title, "{}")
	require.NoError(t, f.f.engine.Submit(context.Background(),
		callerOf(applicant), id, SubmitReq{Comment: "请审批"}))
	return id
}

// items 取列表项
func itemsOf(t *testing.T, res *model.PageResponse) []ApprovalListItem {
	t.Helper()
	require.NotNil(t, res)
	items, ok := res.List.([]ApprovalListItem)
	require.True(t, ok, "列表项类型不对")
	return items
}

// ============================================================================
// 列表:三个盒子(§8.2)
// ============================================================================

// TestApprovalList_BoxRequired box 必须显式给
//
// 🔴 **这是整个列表层最重要的一条** —— 空 box 退化成「全部」的话,
// 前端漏传一个参数就等于把全平台的审批单摊开给每一个有 `approval:read` 的人。
func TestApprovalList_BoxRequired(t *testing.T) {
	f := newQueryFixture(t)
	f.running(t, queryApplicant, "单子")

	for _, box := range []string{"", "all", "ALL", "unknown"} {
		res, err := f.svc.ListApprovals(context.Background(), callerOf(queryStranger),
			ApprovalListQuery{Box: box})
		require.Error(t, err, "box=%q 竟然没报错", box)
		assert.Equal(t, errcode.ParamInvalid, errcode.CodeOf(err))
		assert.Nil(t, res, "box=%q 报错时还返回了数据", box)
	}

	// ⚠️ **首尾空白会被 TrimSpace 掉** —— 这是有意的宽松:
	// 它只是把 `" todo "` 归一成 `"todo"`,不会把非法值变成合法值。
	// 真正的宽松(空 box → 全部)是被上面那条挡住的。
	res, err := f.svc.ListApprovals(context.Background(), callerOf(queryApprover),
		ApprovalListQuery{Box: " todo "})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)
}

// TestApprovalList_TodoOnlyMine 待办 = 我的 pending task
func TestApprovalList_TodoOnlyMine(t *testing.T) {
	f := newQueryFixture(t)
	f.running(t, queryApplicant, "单子")

	res, err := f.svc.ListApprovals(context.Background(), callerOf(queryApprover),
		ApprovalListQuery{Box: string(BoxTodo)})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)

	// 发起人自己不是审批人
	res, err = f.svc.ListApprovals(context.Background(), callerOf(queryApplicant),
		ApprovalListQuery{Box: string(BoxTodo)})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Total)

	// 无关的人
	res, err = f.svc.ListApprovals(context.Background(), callerOf(queryStranger),
		ApprovalListQuery{Box: string(BoxTodo)})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Total)
}

// TestApprovalList_TodoExcludesStale 打回后上一轮的票不算待办
//
// 🔴 **D4 的实现细节** —— 打回把本实例所有 task 置 `stale`。
// 待办查询若只问「有没有我的 task」,打回后的人还能在列表里看到这条单子,
// 点进去发现按钮没了 —— 症状是「列表里有单子但审不了」。
func TestApprovalList_TodoExcludesStale(t *testing.T) {
	f := newQueryFixture(t)
	id := f.running(t, queryApplicant, "单子")

	require.NoError(t, f.f.engine.Act(context.Background(), callerOf(queryApprover), id,
		ActReq{Action: ActRollback, Comment: "材料不齐,补齐以后再提交"}))

	res, err := f.svc.ListApprovals(context.Background(), callerOf(queryApprover),
		ApprovalListQuery{Box: string(BoxTodo)})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Total, "打回后 stale 的 task 还被当成待办")

	// 单子本身回到申请人名下
	res, err = f.svc.ListApprovals(context.Background(), callerOf(queryApplicant),
		ApprovalListQuery{Box: string(BoxMine)})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)
}

func TestApprovalList_DoneAfterApproved(t *testing.T) {
	f := newQueryFixture(t)
	id := f.running(t, queryApplicant, "单子")

	require.NoError(t, f.f.engine.Act(context.Background(), callerOf(queryApprover), id,
		ActReq{Action: ActApprove}))

	res, err := f.svc.ListApprovals(context.Background(), callerOf(queryApprover),
		ApprovalListQuery{Box: string(BoxDone)})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)

	// 批过了就不再是待办
	res, err = f.svc.ListApprovals(context.Background(), callerOf(queryApprover),
		ApprovalListQuery{Box: string(BoxTodo)})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Total)
}

func TestApprovalList_DoneAfterRejected(t *testing.T) {
	f := newQueryFixture(t)
	id := f.running(t, queryApplicant, "单子")

	require.NoError(t, f.f.engine.Act(context.Background(), callerOf(queryApprover), id,
		ActReq{Action: ActReject, Comment: "这份材料不符合要求"}))

	res, err := f.svc.ListApprovals(context.Background(), callerOf(queryApprover),
		ApprovalListQuery{Box: string(BoxDone)})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)
}

func TestApprovalList_MineOnlyOwn(t *testing.T) {
	f := newQueryFixture(t)
	f.running(t, queryApplicant, "单子")

	res, err := f.svc.ListApprovals(context.Background(), callerOf(queryApplicant),
		ApprovalListQuery{Box: string(BoxMine)})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)

	res, err = f.svc.ListApprovals(context.Background(), callerOf(queryStranger),
		ApprovalListQuery{Box: string(BoxMine)})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Total)
}

// TestApprovalList_ItemFields 列表项的补充字段要真的填上
func TestApprovalList_ItemFields(t *testing.T) {
	f := newQueryFixture(t)
	f.addUser(t, queryApplicant, "张三")
	f.addUser(t, queryApprover, "李四")
	f.running(t, queryApplicant, "材料审核")

	res, err := f.svc.ListApprovals(context.Background(), callerOf(queryApprover),
		ApprovalListQuery{Box: string(BoxTodo)})
	require.NoError(t, err)
	items := itemsOf(t, res)
	require.Len(t, items, 1)

	it := items[0]
	assert.Equal(t, "材料审核", it.Title)
	assert.Equal(t, model.ApprovalStatusRunning, it.Status)
	assert.Equal(t, 1, it.CurrentStage)
	assert.Equal(t, "第1关", it.CurrentStageName)
	assert.Equal(t, "张三", it.ApplicantName)
	assert.Equal(t, model.ApprovalTaskPending, it.MyTaskStatus)
	assert.NotEmpty(t, it.TemplateCode)
	assert.NotEmpty(t, it.TemplateName)
	assert.Equal(t, "running", it.Status)
	// ⚠️ **ID 必须是字符串** —— 雪花 ID 19 位,超出 JS 的 2^53
	assert.NotEmpty(t, it.ID)
	assert.NotEmpty(t, it.BizID)
}

// ============================================================================
// 详情可见性(§8.3)
// ============================================================================

func TestApprovalDetail_Visibility(t *testing.T) {
	f := newQueryFixture(t)
	id := f.running(t, queryApplicant, "材料审核")

	t.Run("发起人看得到", func(t *testing.T) {
		d, err := f.svc.GetApproval(context.Background(), callerOf(queryApplicant), id)
		require.NoError(t, err)
		assert.Equal(t, "材料审核", d.Title)
	})

	t.Run("审批人看得到", func(t *testing.T) {
		d, err := f.svc.GetApproval(context.Background(), callerOf(queryApprover), id)
		require.NoError(t, err)
		assert.Equal(t, "材料审核", d.Title)
	})

	t.Run("无关的人看不到", func(t *testing.T) {
		_, err := f.svc.GetApproval(context.Background(), callerOf(queryStranger), id)
		require.Error(t, err)
		assert.Equal(t, errcode.Forbidden, errcode.CodeOf(err))
	})

	t.Run("超管看得到", func(t *testing.T) {
		// ⚠️ **走 assertor 的超管短路** —— 与 isParty 无关
		_, err := f.svc.GetApproval(context.Background(),
			&Caller{UserID: 4000, IdentityID: 1, RoleCode: "role_super_admin"}, id)
		require.NoError(t, err)
	})

	t.Run("单子不存在", func(t *testing.T) {
		_, err := f.svc.GetApproval(context.Background(), callerOf(queryApprover), 999999)
		require.Error(t, err)
		assert.Equal(t, errcode.NotFound, errcode.CodeOf(err))
	})
}

// TestApprovalDetail_CanAct 「我能不能审」要跟 task 表一致
func TestApprovalDetail_CanAct(t *testing.T) {
	f := newQueryFixture(t)
	id := f.running(t, queryApplicant, "材料审核")

	d, err := f.svc.GetApproval(context.Background(), callerOf(queryApprover), id)
	require.NoError(t, err)
	assert.True(t, d.CanAct, "本关待办人应该能审")

	// 发起人不是审批人
	d, err = f.svc.GetApproval(context.Background(), callerOf(queryApplicant), id)
	require.NoError(t, err)
	assert.False(t, d.CanAct)

	// 批完之后就不能再审了
	require.NoError(t, f.f.engine.Act(context.Background(), callerOf(queryApprover), id,
		ActReq{Action: ActApprove}))
	d, err = f.svc.GetApproval(context.Background(), callerOf(queryApprover), id)
	require.NoError(t, err)
	assert.False(t, d.CanAct)
}

// TestApprovalDetail_StagesAndAssignees 关卡视图要按 task 表算
func TestApprovalDetail_StagesAndAssignees(t *testing.T) {
	f := newQueryFixture(t)
	f.addUser(t, queryApprover, "李四")
	id := f.running(t, queryApplicant, "材料审核")

	d, err := f.svc.GetApproval(context.Background(), callerOf(queryApprover), id)
	require.NoError(t, err)
	require.Len(t, d.Stages, 1)

	st := d.Stages[0]
	assert.Equal(t, 1, st.StageIndex)
	assert.Equal(t, "第1关", st.Name)
	assert.Equal(t, "current", st.Status)
	assert.Equal(t, 1, st.TotalCount)
	assert.Equal(t, 0, st.ApprovedCount)
	require.Len(t, st.Assignees, 1)
	assert.Equal(t, model.ApprovalTaskPending, st.Assignees[0].Status)
	assert.Equal(t, "李四", st.Assignees[0].Name)

	// 批过之后计数要变
	require.NoError(t, f.f.engine.Act(context.Background(), callerOf(queryApprover), id,
		ActReq{Action: ActApprove}))
	d, err = f.svc.GetApproval(context.Background(), callerOf(queryApprover), id)
	require.NoError(t, err)
	require.Len(t, d.Stages, 1)
	assert.Equal(t, 1, d.Stages[0].ApprovedCount)
}

// TestApprovalDetail_RoundsAndLogs 轮次快照与意见流水
//
// 🔴 **打回后本轮 outcome 必须是 `rolled_back`** —— §10.1 用例 12 点名过这个假绿:
// 详情页上会出现三轮全是「进行中」。
func TestApprovalDetail_RoundsAndLogs(t *testing.T) {
	f := newQueryFixture(t)
	id := f.running(t, queryApplicant, "材料审核")

	require.NoError(t, f.f.engine.Act(context.Background(), callerOf(queryApprover), id,
		ActReq{Action: ActRollback, Comment: "材料不齐,补齐以后再提交"}))

	// 改内容后重提(重提必须改内容,否则被守卫挡下)
	newTitle := "材料审核(补齐)"
	require.NoError(t, f.f.engine.UpdateDraft(context.Background(),
		callerOf(queryApplicant), id, UpdateDraftReq{Title: &newTitle}))
	require.NoError(t, f.f.engine.Submit(context.Background(),
		callerOf(queryApplicant), id, SubmitReq{Comment: "已补齐,再审一次"}))

	d, err := f.svc.GetApproval(context.Background(), callerOf(queryApplicant), id)
	require.NoError(t, err)

	require.Len(t, d.Rounds, 2)
	assert.Equal(t, 1, d.Rounds[0].RoundNo)
	assert.Equal(t, model.ApprovalRoundRolledBack, d.Rounds[0].Outcome)
	assert.Equal(t, 2, d.Rounds[1].RoundNo)
	assert.Equal(t, model.ApprovalRoundRunning, d.Rounds[1].Outcome)
	assert.Equal(t, "材料审核", d.Rounds[0].Title)
	assert.Equal(t, newTitle, d.Rounds[1].Title)

	// 意见流水:submit / rollback / submit
	require.Len(t, d.Logs, 3)
	assert.Equal(t, ActSubmit, d.Logs[0].Action)
	assert.Equal(t, ActRollback, d.Logs[1].Action)
	assert.Equal(t, ActSubmit, d.Logs[2].Action)
	assert.Equal(t, 1, d.Logs[0].RoundNo)
	assert.Equal(t, 1, d.Logs[1].RoundNo)
	assert.Equal(t, 2, d.Logs[2].RoundNo)
}

// ============================================================================
// 建草稿(§7.3)
// ============================================================================

// TestCreateDraft_DuplicateBizRejected D9:同一条业务记录只允许一个实例
func TestCreateDraft_DuplicateBizRejected(t *testing.T) {
	f := newQueryFixture(t)
	f.f.newTemplate(t, "tpl_"+tplSuffix(t), []string{model.ApprovalReviewAny}, []int{1})

	req := CreateApprovalReq{
		BizType:   testBizType,
		BizID:     555,
		Title:     "材料审核",
		Variables: json.RawMessage(`{}`),
	}
	_, err := f.svc.CreateDraft(context.Background(), callerOf(queryApplicant), req)
	require.NoError(t, err)

	_, err = f.svc.CreateDraft(context.Background(), callerOf(queryApplicant), req)
	require.Error(t, err)
	assert.Equal(t, errcode.Conflict, errcode.CodeOf(err))
}

// TestCreateDraft_VariablesPreserved variables 必须逐字节保留
//
// 🔴 **引擎要逐字节比较「这次重提的内容和本轮快照是否完全一样」** ——
// 中间任何一次 map → JSON 的往返都会打乱键序,把「没改」变成「改了」。
func TestCreateDraft_VariablesPreserved(t *testing.T) {
	f := newQueryFixture(t)
	f.f.newTemplate(t, "tpl_"+tplSuffix(t), []string{model.ApprovalReviewAny}, []int{1})

	// ⚠️ **故意用非字母序的键** —— map 序列化会把它排成字母序
	vars := `{"z":1,"a":2,"m":{"y":1,"b":2}}`
	id, err := f.svc.CreateDraft(context.Background(), callerOf(queryApplicant),
		CreateApprovalReq{
			BizType:   testBizType,
			BizID:     777,
			Title:     "材料审核",
			Variables: json.RawMessage(vars),
		})
	require.NoError(t, err)

	var inst model.SysApprovalInstance
	require.NoError(t, f.db.First(&inst, id).Error)
	assert.Equal(t, vars, inst.Variables)
}

// TestCreateDraft_EmptyVariablesBecomesObject 空 variables 归一成 {}
//
// ⚠️ **MySQL 的 JSON 列不接受空串** —— 归一化放在查询 service 这一层,
// 引擎自己不归一(引擎的调用方是业务 service,那条路自己带内容)。
func TestCreateDraft_EmptyVariablesBecomesObject(t *testing.T) {
	f := newQueryFixture(t)
	f.f.newTemplate(t, "tpl_"+tplSuffix(t), []string{model.ApprovalReviewAny}, []int{1})

	id, err := f.svc.CreateDraft(context.Background(), callerOf(queryApplicant),
		CreateApprovalReq{
			BizType:   testBizType,
			BizID:     888,
			Title:     "材料审核",
			Variables: nil,
		})
	require.NoError(t, err)

	var inst model.SysApprovalInstance
	require.NoError(t, f.db.First(&inst, id).Error)
	assert.Equal(t, "{}", inst.Variables)
}

func TestCreateDraft_RequiresFields(t *testing.T) {
	f := newQueryFixture(t)
	f.f.newTemplate(t, "tpl_"+tplSuffix(t), []string{model.ApprovalReviewAny}, []int{1})

	cases := []struct {
		name string
		req  CreateApprovalReq
	}{
		{"没有 biz_type", CreateApprovalReq{BizID: 1, Title: "t"}},
		{"没有 biz_id", CreateApprovalReq{BizType: testBizType, Title: "t"}},
		{"没有 title", CreateApprovalReq{BizType: testBizType, BizID: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.svc.CreateDraft(context.Background(), callerOf(queryApplicant), c.req)
			require.Error(t, err)
			assert.Equal(t, errcode.ParamMissing, errcode.CodeOf(err))
		})
	}
}

// TestCreateDraft_Unauthorized 没登录不许建
func TestCreateDraft_Unauthorized(t *testing.T) {
	f := newQueryFixture(t)
	_, err := f.svc.CreateDraft(context.Background(), nil,
		CreateApprovalReq{BizType: testBizType, BizID: 1, Title: "t"})
	require.Error(t, err)
	assert.Equal(t, errcode.Unauthorized, errcode.CodeOf(err))
}
