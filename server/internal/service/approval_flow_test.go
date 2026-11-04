// 审批流程模板 CRUD 单测(M86)
//
// ⚠️ **这些用例盯的是两条「代码照跑、但功能不对」的坑**:
//
//	① **模板改不动** —— 唯一索引 `uk_app_stage_tpl_idx(template_id, stage_index)`
//	   不含 `deleted_at`,软删旧关卡再写同样的序号会直接撞库。
//	   `purgeStages` 改成硬删就是为了这个,这里就是它的回归网。
//	② **模板存成「没人审」** —— §5.3① 的配置期预检。漏了它,症状是
//	   「某个业务的单子发不出去」,而排障的人会去看业务代码,不在模板上。
//
// ⚠️ **断言一律查库,不复用 service 的返回值** —— 返回值就是被测对象,
// 拿它证明自己等于没测。
package service

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// ============================================================================
// 夹具
// ============================================================================

type flowFixture struct {
	db  *gorm.DB
	svc ApprovalFlowService
	// rf 复用 M85 的解析器夹具 —— 预检要走真解析器,
	// 用假的会让「预检到底拦不拦得住」测不出来
	rf *resolverFixture
}

func newFlowFixture(t *testing.T) *flowFixture {
	t.Helper()
	rf := newResolverFixture(t)
	return &flowFixture{
		db:  rf.db,
		svc: NewApprovalFlowService(rf.db, rf.r),
		rf:  rf,
	}
}

// flowCode 造一个合法的模板编码
//
// ⚠️ **必须小写** —— `^[a-z][a-z0-9_]{1,63}$` 不接受大写,
// 而 `t.Name()` 里全是 Go 导出风格的首字母大写。
//
// ⚠️ **必须剔掉非 ASCII** —— 子测试名里可能有中文(「指定的人已停用」),
// 而 code 的正则只收 `[a-z0-9_]`。留着中文的话,用例会在**编码校验**那一步
// 就被拒掉,根本走不到要测的预检分支 —— 而且报错长得像"预检没生效"。
func flowCode(t *testing.T, suffix string) string {
	t.Helper()
	s := strings.ToLower(t.Name())
	s = strings.NewReplacer("/", "_", " ", "_", "-", "_", ".", "_").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 30 {
		out = out[len(out)-30:]
	}
	return "flow_" + out + "_" + suffix
}

// req 造一个能过预检的建模板请求
func (f *flowFixture) req(t *testing.T, code string, approver uint64) FlowSaveReq {
	t.Helper()
	return FlowSaveReq{
		Code:    code,
		Name:    "测试模板",
		BizType: testBizType,
		Stages: []FlowStageInput{{
			Name:       "第1关",
			ReviewType: model.ApprovalReviewAny,
			Rules:      []FlowRuleInput{{RuleType: model.ApprovalRuleUser, UserID: approver}},
		}},
	}
}

// addInstance 手工插一条实例(用来测 D7:有在跑的单子时禁止改结构)
func (f *flowFixture) addInstance(t *testing.T, templateID uint64, status string) uint64 {
	t.Helper()
	inst := &model.SysApprovalInstance{
		BaseModel:           model.BaseModel{ID: sonyflake.Next()},
		BizType:             testBizType,
		BizID:               sonyflake.Next(),
		Title:               "在跑的单子",
		Variables:           "{}",
		TemplateID:          templateID,
		ApplicantUserID:     1,
		ApplicantIdentityID: 1,
		Status:              status,
	}
	require.NoError(t, f.db.Create(inst).Error)
	return inst.ID
}

// tplCount 模板行数(断言「失败时什么都没写进去」)
func (f *flowFixture) tplCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Model(&model.SysApprovalTemplate{}).Count(&n).Error)
	return n
}

// ============================================================================
// 建模板
// ============================================================================

func TestFlow_Create_WritesStagesAndRules(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)
	bob := f.rf.addUser(t, "bob", true, false)

	req := f.req(t, flowCode(t, "a"), alice)
	req.Stages = []FlowStageInput{
		{
			Name:       "第1关",
			ReviewType: model.ApprovalReviewAll,
			Rules: []FlowRuleInput{
				{RuleType: model.ApprovalRuleUser, UserID: alice},
				{RuleType: model.ApprovalRuleUser, UserID: bob},
			},
		},
		{
			Name:       "第2关",
			ReviewType: model.ApprovalReviewAny,
			Rules:      []FlowRuleInput{{RuleType: model.ApprovalRuleUser, UserID: bob}},
		},
	}

	id, err := f.svc.CreateFlow(context.Background(), callerOf(1), req)
	require.NoError(t, err)
	require.NotZero(t, id)

	// 模板本体
	var tpl model.SysApprovalTemplate
	require.NoError(t, f.db.First(&tpl, id).Error)
	assert.Equal(t, req.Code, tpl.Code)
	assert.Equal(t, "测试模板", tpl.Name)
	assert.Equal(t, testBizType, tpl.BizType)
	// ⚠️ **建模板时 is_active 缺省是 true**(指针语义:nil = 建时默认开、改时不动)
	assert.True(t, tpl.IsActive)

	// 关卡:序号必须从 1 开始且连续
	var stages []model.SysApprovalStage
	require.NoError(t, f.db.Where("template_id = ?", id).
		Order("stage_index ASC").Find(&stages).Error)
	require.Len(t, stages, 2)
	assert.Equal(t, 1, stages[0].StageIndex)
	assert.Equal(t, 2, stages[1].StageIndex)
	assert.Equal(t, "第1关", stages[0].Name)
	assert.Equal(t, model.ApprovalReviewAll, stages[0].ReviewType)
	assert.Equal(t, model.ApprovalReviewAny, stages[1].ReviewType)

	// 规则:seq 从 1 开始连续,且按关卡分组
	var rules []model.SysApprovalAssigneeRule
	require.NoError(t, f.db.Where("stage_id IN ?", []uint64{stages[0].ID, stages[1].ID}).
		Order("stage_id ASC, seq ASC").Find(&rules).Error)
	require.Len(t, rules, 3)
	assert.Equal(t, 1, rules[0].Seq)
	assert.Equal(t, 2, rules[1].Seq)
	assert.Equal(t, alice, rules[0].UserID)
	assert.Equal(t, bob, rules[1].UserID)
	assert.Equal(t, 1, rules[2].Seq)
	assert.Equal(t, stages[1].ID, rules[2].StageID)
}

func TestFlow_Create_RejectsBadCode(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	cases := []struct {
		name string
		code string
	}{
		{"大写", "Flow"},
		{"数字开头", "1flow"},
		{"只有一个字符", "a"},
		{"含横线", "my-flow"},
		{"含中文", "流程模板"},
		{"超过 64", "a" + strings.Repeat("b", 64)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
				f.req(t, c.code, alice))
			require.Error(t, err)
			assert.Equal(t, errcode.ParamInvalid, errcode.CodeOf(err))
			assert.Equal(t, int64(0), f.tplCount(t), "校验没过却写进去了")
		})
	}

	// ⚠️ **空 code 是「缺失」不是「格式错」** —— 两者的 errcode 不同,
	// 前端提示词也不一样。
	t.Run("空", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			f.req(t, "", alice))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamMissing, errcode.CodeOf(err))
		assert.Equal(t, int64(0), f.tplCount(t), "校验没过却写进去了")
	})
}

func TestFlow_Create_RejectsBadStages(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	one := FlowStageInput{
		Name:  "第1关",
		Rules: []FlowRuleInput{{RuleType: model.ApprovalRuleUser, UserID: alice}},
	}
	mk := func(mut func(*FlowSaveReq)) FlowSaveReq {
		r := f.req(t, flowCode(t, "s"), alice)
		r.Stages = []FlowStageInput{one}
		mut(&r)
		return r
	}

	t.Run("一个关卡都没有", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) { r.Stages = nil }))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamMissing, errcode.CodeOf(err))
	})

	t.Run("关卡超过 10 个", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) {
				r.Stages = nil
				for i := 0; i < 11; i++ {
					r.Stages = append(r.Stages, one)
				}
			}))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamInvalid, errcode.CodeOf(err))
	})

	t.Run("关卡名空", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) { r.Stages[0].Name = "  " }))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamMissing, errcode.CodeOf(err))
	})

	t.Run("关卡没有规则", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) { r.Stages[0].Rules = nil }))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamMissing, errcode.CodeOf(err))
	})

	t.Run("review_type 不在词表内", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) { r.Stages[0].ReviewType = "some" }))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamInvalid, errcode.CodeOf(err))
	})

	t.Run("rule_type 不在词表内", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) { r.Stages[0].Rules[0].RuleType = "manager" }))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamInvalid, errcode.CodeOf(err))
	})

	t.Run("user 规则没填 user_id", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) { r.Stages[0].Rules[0].UserID = 0 }))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamInvalid, errcode.CodeOf(err))
	})

	t.Run("role 规则没填 role_id", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) {
				r.Stages[0].Rules[0] = FlowRuleInput{RuleType: model.ApprovalRuleRole}
			}))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamInvalid, errcode.CodeOf(err))
	})

	t.Run("模板名空", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) { r.Name = "" }))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamMissing, errcode.CodeOf(err))
	})

	t.Run("biz_type 空", func(t *testing.T) {
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			mk(func(r *FlowSaveReq) { r.BizType = "" }))
		require.Error(t, err)
		assert.Equal(t, errcode.ParamMissing, errcode.CodeOf(err))
	})

	assert.Equal(t, int64(0), f.tplCount(t), "校验没过却写进去了")
}

// TestFlow_Create_ClearsUnusedIDs 规则里的另外两个 ID 必须清零
//
// ⚠️ **不清零的后果**:一条 `rule_type=user` 的规则带着 `role_id`,
// 解析时按 user 走,但读模板的人看到 role_id 非零会以为"这规则还认角色" ——
// 两种解释同时成立,改代码的人只能靠猜。
func TestFlow_Create_ClearsUnusedIDs(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	req := f.req(t, flowCode(t, "clear"), alice)
	req.Stages[0].Rules[0].RoleID = 999 // 故意多传

	_, err := f.svc.CreateFlow(context.Background(), callerOf(1), req)
	require.NoError(t, err)

	var rules []model.SysApprovalAssigneeRule
	require.NoError(t, f.db.Find(&rules).Error)
	require.Len(t, rules, 1)
	assert.Equal(t, alice, rules[0].UserID)
	assert.Equal(t, uint64(0), rules[0].RoleID)
}

// TestFlow_Create_PrecheckBlocksUnresolvable §5.3① 配置期预检
//
// 🔴 **这是 M85 落地后唯一有调用方的地方** —— 预检写在 `CreateFlow` /
// `UpdateFlow` 的事务里。漏了它,「指定的人已离职」这种模板照样能存进去,
// 然后每个走到这一关的单子都发不出去。
func TestFlow_Create_PrecheckBlocksUnresolvable(t *testing.T) {
	t.Run("指定的人已停用", func(t *testing.T) {
		f := newFlowFixture(t)
		alice := f.rf.addUser(t, "alice", false, false) // 停用

		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			f.req(t, flowCode(t, "off"), alice))
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalNoAssignee, errcode.CodeOf(err))
		assert.Equal(t, int64(0), f.tplCount(t), "预检没过却写进去了")
	})

	t.Run("指定的人已离职", func(t *testing.T) {
		f := newFlowFixture(t)
		alice := f.rf.addUser(t, "alice", true, true) // 离职

		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			f.req(t, flowCode(t, "leave"), alice))
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalNoAssignee, errcode.CodeOf(err))
		assert.Equal(t, int64(0), f.tplCount(t), "预检没过却写进去了")
	})

	t.Run("指定的人根本不存在", func(t *testing.T) {
		f := newFlowFixture(t)

		_, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			f.req(t, flowCode(t, "ghost"), 123456789))
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalNoAssignee, errcode.CodeOf(err))
		assert.Equal(t, int64(0), f.tplCount(t), "预检没过却写进去了")
	})

	t.Run("关卡全是 dept_leader 时放行", func(t *testing.T) {
		// ⚠️ **dept_leader 本层判不了**(部门主管是谁取决于发起人在哪个部门),
		// 所以预检放行,交到提交期(§5.3③)。这是有意的,不是漏判。
		f := newFlowFixture(t)
		req := f.req(t, flowCode(t, "dept"), 1)
		req.Stages[0].Rules = []FlowRuleInput{
			{RuleType: model.ApprovalRuleDeptLeader, Levels: 1},
		}
		_, err := f.svc.CreateFlow(context.Background(), callerOf(1), req)
		require.NoError(t, err)
		assert.Equal(t, int64(1), f.tplCount(t))
	})
}

func TestFlow_Create_DuplicateCodeRejected(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)
	code := flowCode(t, "dup")

	_, err := f.svc.CreateFlow(context.Background(), callerOf(1), f.req(t, code, alice))
	require.NoError(t, err)

	_, err = f.svc.CreateFlow(context.Background(), callerOf(1), f.req(t, code, alice))
	require.Error(t, err)
	assert.Equal(t, errcode.Conflict, errcode.CodeOf(err))
	assert.Equal(t, int64(1), f.tplCount(t))
}

// ============================================================================
// 改模板
// ============================================================================

// TestFlow_Update_ReplacesStagesAndRules 改模板是「整体替换」
//
// 🔴 **这条是硬删决策的回归网** —— 若把 `purgeStages` 改回软删,
// 写回同样的 `stage_index` 会撞 `uk_app_stage_tpl_idx`,
// 症状是「模板第一次改就报错」,而且报错信息是数据库的 UniqueConstraint。
func TestFlow_Update_ReplacesStagesAndRules(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)
	bob := f.rf.addUser(t, "bob", true, false)

	id, err := f.svc.CreateFlow(context.Background(), callerOf(1),
		f.req(t, flowCode(t, "u"), alice))
	require.NoError(t, err)

	// 改成 2 关(原来 1 关),第 1 关序号不变
	upd := f.req(t, "", alice)
	upd.Stages = []FlowStageInput{
		{
			Name:       "新第1关",
			ReviewType: model.ApprovalReviewAny,
			Rules:      []FlowRuleInput{{RuleType: model.ApprovalRuleUser, UserID: bob}},
		},
		{
			Name:  "新第2关",
			Rules: []FlowRuleInput{{RuleType: model.ApprovalRuleUser, UserID: alice}},
		},
	}
	require.NoError(t, f.svc.UpdateFlow(context.Background(), callerOf(1), id, upd))

	// ⚠️ **Unscoped 查** —— 软删的行也算数,要证明旧的确实被物理删了
	var stages []model.SysApprovalStage
	require.NoError(t, f.db.Unscoped().Where("template_id = ?", id).
		Order("stage_index ASC").Find(&stages).Error)
	require.Len(t, stages, 2, "旧的关卡没删干净,或者新关卡没写进去")
	assert.Equal(t, 1, stages[0].StageIndex)
	assert.Equal(t, "新第1关", stages[0].Name)
	assert.Equal(t, 2, stages[1].StageIndex)

	// 旧规则也不能留 —— 留着的规则还挂在被删掉的 stage 上,永远不会被解析
	var rules []model.SysApprovalAssigneeRule
	require.NoError(t, f.db.Unscoped().Find(&rules).Error)
	require.Len(t, rules, 2)
	for _, r := range rules {
		assert.Contains(t, []uint64{stages[0].ID, stages[1].ID}, r.StageID)
	}
}

func TestFlow_Update_RejectsBadCode(t *testing.T) {
	// 🔴 **M89-2 的回归网。**
	//
	// 原来 `normalizeFlow` 的 code 格式校验整体挂在 `if forCreate` 里面,
	// 于是 `PUT /flows/:id` 能把 code 改成任何字符串 —— "Bad Code!"、中文都收。
	// 后果:
	//   ① `deletedFlowCode` 按**字节**截断,一个中文 code 会被从中间劈开,
	//      库里躺了半个 UTF-8 字符;
	//   ② `code` 是业务侧引用模板的键,脏值一路传下去;
	//   ③ §7.2 的"服务端校验"清单里明写着 `code` 格式 `^[a-z][a-z0-9_]{1,63}$`。
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	// ⚠️ **模板只建一次** —— 每个子用例都用同一个 code 建模板的话,
	// 第二个子用例会撞唯一索引报 409("模板编码已存在"),那不是被测的东西。
	id, err := f.svc.CreateFlow(context.Background(), callerOf(1),
		f.req(t, flowCode(t, "code"), alice))
	require.NoError(t, err)
	origCode := flowCode(t, "code")

	badCodes := []struct {
		name string
		code string
	}{
		{"大写", "Flow"},
		{"数字开头", "1flow"},
		{"只有一个字符", "a"},
		{"含横线", "my-flow"},
		{"含中文", "流程模板"},
		{"含空格", "my flow"},
	}
	for _, c := range badCodes {
		t.Run(c.name, func(t *testing.T) {
			upd := f.req(t, c.code, alice)
			err := f.svc.UpdateFlow(context.Background(), callerOf(1), id, upd)
			require.Error(t, err, "脏 code 被收下了")
			assert.Equal(t, errcode.ParamInvalid, errcode.CodeOf(err))

			// ⚠️ **库里必须还是原来那个 code** —— 校验失败不许写进去
			var tpl model.SysApprovalTemplate
			require.NoError(t, f.db.First(&tpl, id).Error)
			assert.Equal(t, origCode, tpl.Code)
		})
	}

	t.Run("空 code = 不改,不是格式错", func(t *testing.T) {
		id, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			f.req(t, flowCode(t, "keep"), alice))
		require.NoError(t, err)

		upd := f.req(t, "", alice)
		upd.Name = "只改名字"
		require.NoError(t, f.svc.UpdateFlow(context.Background(), callerOf(1), id, upd))

		var tpl model.SysApprovalTemplate
		require.NoError(t, f.db.First(&tpl, id).Error)
		assert.Equal(t, flowCode(t, "keep"), tpl.Code, "code 不该被动")
		assert.Equal(t, "只改名字", tpl.Name)
	})

	t.Run("合法的新 code 改得动", func(t *testing.T) {
		id, err := f.svc.CreateFlow(context.Background(), callerOf(1),
			f.req(t, flowCode(t, "old"), alice))
		require.NoError(t, err)

		newCode := flowCode(t, "new")
		upd := f.req(t, newCode, alice)
		require.NoError(t, f.svc.UpdateFlow(context.Background(), callerOf(1), id, upd))

		var tpl model.SysApprovalTemplate
		require.NoError(t, f.db.First(&tpl, id).Error)
		assert.Equal(t, newCode, tpl.Code)
	})
}

func TestFlow_Update_ToFewerStages(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	req := f.req(t, flowCode(t, "few"), alice)
	req.Stages = append(req.Stages, FlowStageInput{
		Name:  "第2关",
		Rules: []FlowRuleInput{{RuleType: model.ApprovalRuleUser, UserID: alice}},
	})
	id, err := f.svc.CreateFlow(context.Background(), callerOf(1), req)
	require.NoError(t, err)

	// 改成 1 关
	upd := f.req(t, "", alice)
	require.NoError(t, f.svc.UpdateFlow(context.Background(), callerOf(1), id, upd))

	var stages []model.SysApprovalStage
	require.NoError(t, f.db.Unscoped().Where("template_id = ?", id).Find(&stages).Error)
	require.Len(t, stages, 1)
}

// TestFlow_Update_IsActivePointerSemantics `is_active` 的指针语义
//
// ⚠️ **值类型分不清「不传」和「传 false」** —— 改模板时若把「不传」当成
// 「置 false」,一次普通改名就会把模板停用掉,单子再也发不出去。
func TestFlow_Update_IsActivePointerSemantics(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	id, err := f.svc.CreateFlow(context.Background(), callerOf(1),
		f.req(t, flowCode(t, "act"), alice))
	require.NoError(t, err)

	// 不传 is_active → 不动
	upd := f.req(t, "", alice)
	upd.Name = "改名了"
	require.NoError(t, f.svc.UpdateFlow(context.Background(), callerOf(1), id, upd))

	var tpl model.SysApprovalTemplate
	require.NoError(t, f.db.First(&tpl, id).Error)
	assert.True(t, tpl.IsActive, "没传 is_active 却把它置成了 false")
	assert.Equal(t, "改名了", tpl.Name)

	// 显式传 false → 关掉
	off := false
	upd2 := f.req(t, "", alice)
	upd2.IsActive = &off
	require.NoError(t, f.svc.UpdateFlow(context.Background(), callerOf(1), id, upd2))
	require.NoError(t, f.db.First(&tpl, id).Error)
	assert.False(t, tpl.IsActive)
}

// TestFlow_Update_BlockedByRunningInstance D7
//
// 🔴 **有在跑的单子时禁止改结构** —— 改完之后,正在跑的实例的「第 2 关」
// 会指向一个完全不同的关卡,症状是单子卡死,而没人会想到是模板被改过。
func TestFlow_Update_BlockedByRunningInstance(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	id, err := f.svc.CreateFlow(context.Background(), callerOf(1),
		f.req(t, flowCode(t, "run"), alice))
	require.NoError(t, err)
	f.addInstance(t, id, model.ApprovalStatusRunning)

	err = f.svc.UpdateFlow(context.Background(), callerOf(1), id, f.req(t, "", alice))
	require.Error(t, err)
	assert.Equal(t, errcode.StateInvalid, errcode.CodeOf(err))

	// 模板没被改动
	var tpl model.SysApprovalTemplate
	require.NoError(t, f.db.First(&tpl, id).Error)
	assert.Equal(t, "测试模板", tpl.Name)
}

func TestFlow_Update_AllowedWhenAllInstancesTerminal(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	id, err := f.svc.CreateFlow(context.Background(), callerOf(1),
		f.req(t, flowCode(t, "term"), alice))
	require.NoError(t, err)
	// 终态实例不算「在跑」
	f.addInstance(t, id, model.ApprovalStatusApproved)
	f.addInstance(t, id, model.ApprovalStatusRejected)
	f.addInstance(t, id, model.ApprovalStatusCancelled)

	require.NoError(t, f.svc.UpdateFlow(context.Background(), callerOf(1), id, f.req(t, "", alice)))
}

func TestFlow_Update_NotFound(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	err := f.svc.UpdateFlow(context.Background(), callerOf(1), 999999, f.req(t, "", alice))
	require.Error(t, err)
	assert.Equal(t, errcode.NotFound, errcode.CodeOf(err))
}

// ============================================================================
// 删模板
// ============================================================================

// TestFlow_Delete_RewritesCodeAndFreesSlot D7
//
// 🔴 **唯一索引不含 deleted_at** —— 软删之后同名模板再建不出来,
// 所以删模板必须改写 `code`。
func TestFlow_Delete_RewritesCodeAndFreesSlot(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)
	code := flowCode(t, "del")

	id, err := f.svc.CreateFlow(context.Background(), callerOf(1), f.req(t, code, alice))
	require.NoError(t, err)

	require.NoError(t, f.svc.DeleteFlow(context.Background(), callerOf(1), id))

	// 软删:默认查询查不到
	var tpl model.SysApprovalTemplate
	err = f.db.First(&tpl, id).Error
	require.Error(t, err)

	// 但行还在,code 被改写过、且已停用
	var raw model.SysApprovalTemplate
	require.NoError(t, f.db.Unscoped().First(&raw, id).Error)
	assert.NotEqual(t, code, raw.Code)
	// ⚠️ **前缀可能被截断**(code 列只有 64,后缀自己就占 27),
	// 所以只能断言「改写后的前缀是原 code 的前缀」,不能断言「原 code + 后缀」
	assert.Contains(t, raw.Code, "#deleted#")
	prefix := raw.Code[:strings.Index(raw.Code, "#deleted#")]
	assert.True(t, strings.HasPrefix(code, prefix), "改写后的前缀不是原 code 的前缀")
	assert.False(t, raw.IsActive, "删掉的模板还留着 is_active=true,会被 pickTemplate 选中")

	// 同名模板可以再建
	newID, err := f.svc.CreateFlow(context.Background(), callerOf(1), f.req(t, code, alice))
	require.NoError(t, err)
	assert.NotEqual(t, id, newID)
}

// TestFlow_Delete_LongCodeFitsColumn code 列只有 64
//
// ⚠️ **改写后缀本身要 27 个字符**(`#deleted#` + 19 位雪花 ID),
// 原 code 最长只能留 37 个 —— 不截断就是「删模板」这一步自己把 code 撑爆。
func TestFlow_Delete_LongCodeFitsColumn(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)
	long := "a" + strings.Repeat("b", 63) // 正好 64

	id, err := f.svc.CreateFlow(context.Background(), callerOf(1), f.req(t, long, alice))
	require.NoError(t, err)

	require.NoError(t, f.svc.DeleteFlow(context.Background(), callerOf(1), id))

	var raw model.SysApprovalTemplate
	require.NoError(t, f.db.Unscoped().First(&raw, id).Error)
	assert.LessOrEqual(t, len(raw.Code), 64, "改写后的 code 超出列宽")
	assert.NotEqual(t, long, raw.Code)
}

func TestFlow_Delete_BlockedByRunningInstance(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	id, err := f.svc.CreateFlow(context.Background(), callerOf(1),
		f.req(t, flowCode(t, "delrun"), alice))
	require.NoError(t, err)
	f.addInstance(t, id, model.ApprovalStatusRunning)

	err = f.svc.DeleteFlow(context.Background(), callerOf(1), id)
	require.Error(t, err)
	assert.Equal(t, errcode.StateInvalid, errcode.CodeOf(err))

	// 模板还在,而且没被改写 code
	var tpl model.SysApprovalTemplate
	require.NoError(t, f.db.First(&tpl, id).Error)
	assert.Equal(t, flowCode(t, "delrun"), tpl.Code)
}

func TestFlow_Delete_NotFound(t *testing.T) {
	f := newFlowFixture(t)
	err := f.svc.DeleteFlow(context.Background(), callerOf(1), 999999)
	require.Error(t, err)
	assert.Equal(t, errcode.NotFound, errcode.CodeOf(err))
}

// ============================================================================
// 读
// ============================================================================

func TestFlow_Get_ReturnsStagesAndRules(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	req := f.req(t, flowCode(t, "get"), alice)
	req.Stages = append(req.Stages, FlowStageInput{
		Name:  "第2关",
		Rules: []FlowRuleInput{{RuleType: model.ApprovalRuleUser, UserID: alice}},
	})
	id, err := f.svc.CreateFlow(context.Background(), callerOf(1), req)
	require.NoError(t, err)

	d, err := f.svc.GetFlow(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, req.Code, d.Code)
	require.Len(t, d.Stages, 2)
	assert.Equal(t, 1, d.Stages[0].StageIndex)
	assert.Equal(t, 2, d.Stages[1].StageIndex)
	require.Len(t, d.Stages[0].Rules, 1)
	assert.Equal(t, 1, d.Stages[0].Rules[0].Seq)
	// ⚠️ **ID 是字符串** —— 雪花 ID 19 位,超出 JS 的 2^53
	assert.Equal(t, strconv.FormatUint(alice, 10), d.Stages[0].Rules[0].UserID)
	// ⚠️ **ID 是字符串** —— 雪花 ID 19 位,超出 JS 的 2^53
	assert.NotEmpty(t, d.ID)
	assert.NotEmpty(t, d.Stages[0].ID)
}

func TestFlow_Get_NotFound(t *testing.T) {
	f := newFlowFixture(t)
	_, err := f.svc.GetFlow(context.Background(), 999999)
	require.Error(t, err)
	assert.Equal(t, errcode.NotFound, errcode.CodeOf(err))
}

func TestFlow_List_FiltersAndStageCount(t *testing.T) {
	f := newFlowFixture(t)
	alice := f.rf.addUser(t, "alice", true, false)

	req := f.req(t, flowCode(t, "l1"), alice)
	req.Stages = append(req.Stages, FlowStageInput{
		Name:  "第2关",
		Rules: []FlowRuleInput{{RuleType: model.ApprovalRuleUser, UserID: alice}},
	})
	_, err := f.svc.CreateFlow(context.Background(), callerOf(1), req)
	require.NoError(t, err)

	other := f.req(t, flowCode(t, "l2"), alice)
	other.BizType = "other_biz"
	_, err = f.svc.CreateFlow(context.Background(), callerOf(1), other)
	require.NoError(t, err)

	// 按 biz_type 过滤
	res, err := f.svc.ListFlows(context.Background(), FlowListQuery{BizType: "other_biz"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)

	// 关卡数要统计出来
	res, err = f.svc.ListFlows(context.Background(), FlowListQuery{BizType: testBizType})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)
	items, ok := res.List.([]FlowListItem)
	require.True(t, ok)
	require.Len(t, items, 1)
	assert.Equal(t, 2, items[0].StageCount)

	// 关键词过滤
	res, err = f.svc.ListFlows(context.Background(), FlowListQuery{Keyword: flowCode(t, "l2")})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)
}
