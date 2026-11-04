// 审批人解析单测(M85)
//
// 文档:docs/通用审批系统设计方案V1.0.md §16.9(M85 的 Done 定义):
// 「三种来源各有 ≥1 用例 + §5.3 三层拦截各有 ≥1 用例 + 部门树环检测有用例」
//
// ⚠️ **这些用例盯的是「分得清故障和没人」**。整份实现里最容易写坏、又最难被
// 常规用例抓到的地方,就是把某条 SQL 的 err 当成"没查到人"往下走 ——
// 那样代码照跑、单子照发,只是发给了错的人,没有任何报错。
// 所以每条 err 分支都要有一条用例盯着它**必须报错**。
package service

import (
	"context"
	"strings"
	"testing"
	"time"

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

type resolverFixture struct {
	db *gorm.DB
	r  ApprovalResolver
}

func newResolverFixture(t *testing.T) *resolverFixture {
	t.Helper()
	db := newTestDB(t)
	return &resolverFixture{db: db, r: NewApprovalResolver(db)}
}

// forceBool 事后把布尔列按真值写死。
//
// 🔴 **`Create` 写不进 false,`Select` 也救不了** —— 核实过
// `gorm@v1.31.1/callbacks/create.go:268`:
//
//	if field := ...; !field.HasDefaultValue || field.DefaultValueInterface != nil {
//	    ... values.Columns = append(...)
//	}
//
// 带 `gorm:"default:true"` 的字段 `HasDefaultValue=true` 且
// `DefaultValueInterface==nil`,于是它**根本不会进 INSERT 的列清单** ——
// 无论传什么值、无论 `Select` 写没写它的名字。
//
// 后果:夹具里 `IsActive: false` 会被静默写成 true,而"停用的人不生成 task"
// (§5.2 第 3 条)那条断言就成了永远绿。**所以这里必须显式 UPDATE 回去。**
func (f *resolverFixture) forceBool(t *testing.T, m any, where string, args []any, cols map[string]any) {
	t.Helper()
	require.NoError(t, f.db.Model(m).Where(where, args...).Updates(cols).Error)
}

// addUser 建一个用户。
func (f *resolverFixture) addUser(t *testing.T, nickname string, active, leave bool) uint64 {
	t.Helper()
	u := &model.SysUser{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		Username:     nickname + "_" + nickname,
		PasswordHash: "x",
		Nickname:     nickname,
	}
	require.NoError(t, f.db.Create(u).Error)
	f.forceBool(t, &model.SysUser{}, "id = ?", []any{u.ID},
		map[string]any{"is_active": active, "is_leave": leave})

	// 🔴 **断言真的写进去了** —— forceBool 的存在理由就是 GORM 会跳过零值,
	// 这里回读一次,免得哪天 GORM 改了策略而夹具静默地变成"所有人都在职"。
	var back model.SysUser
	require.NoError(t, f.db.First(&back, "id = ?", u.ID).Error)
	require.Equal(t, active, back.IsActive, "夹具没把 is_active 写进去")
	require.Equal(t, leave, back.IsLeave, "夹具没把 is_leave 写进去")
	return u.ID
}

func (f *resolverFixture) addRole(t *testing.T, code string, active bool) uint64 {
	t.Helper()
	r := &model.SysRole{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Code:      code, Name: code,
	}
	require.NoError(t, f.db.Create(r).Error)
	f.forceBool(t, &model.SysRole{}, "id = ?", []any{r.ID}, map[string]any{"is_active": active})
	return r.ID
}

func (f *resolverFixture) addDept(t *testing.T, name, code string, parentID, leaderID uint64) uint64 {
	t.Helper()
	d := &model.SysDepartment{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Code:      code, Name: name,
	}
	require.NoError(t, f.db.Create(d).Error)
	f.forceBool(t, &model.SysDepartment{}, "id = ?", []any{d.ID},
		map[string]any{"parent_id": parentID, "leader_id": leaderID, "is_active": true})

	var back model.SysDepartment
	require.NoError(t, f.db.First(&back, "id = ?", d.ID).Error)
	require.Equal(t, parentID, back.ParentID, "夹具没把 parent_id 写进去")
	require.Equal(t, leaderID, back.LeaderID, "夹具没把 leader_id 写进去")
	return d.ID
}

// addIdentity 给用户加一个身份。
//
// 🔴 **valid_to 传零值 = 永久**,这是本仓 identity 的既有语义
// (`auth_impl.go:800`:`!ValidTo.IsZero() && ValidTo.Before(now)`)。
// 解析侧必须按同一个语义判,否则 SQL 里一句 `valid_to >= now` 就能让
// 全公司一个人都解析不出来。
func (f *resolverFixture) addIdentity(
	t *testing.T, userID, deptID uint64, roleCode string, active bool, from, to time.Time,
) uint64 {
	t.Helper()
	id := &model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    userID,
		RoleCode:  roleCode,
		DeptID:    deptID,
		ValidFrom: model.Time{Time: from},
		ValidTo:   model.Time{Time: to},
	}
	require.NoError(t, f.db.Create(id).Error)
	f.forceBool(t, &model.SysUserIdentity{}, "id = ?", []any{id.ID},
		map[string]any{"is_active": active, "dept_id": deptID})

	var back model.SysUserIdentity
	require.NoError(t, f.db.First(&back, "id = ?", id.ID).Error)
	require.Equal(t, active, back.IsActive, "夹具没把 is_active 写进去")
	require.Equal(t, deptID, back.DeptID, "夹具没把 dept_id 写进去")
	require.Equal(t, from.Unix(), back.ValidFrom.Unix(), "valid_from 没写进去")
	require.Equal(t, to.Unix(), back.ValidTo.Unix(), "valid_to 没写进去")
	return id.ID
}

func (f *resolverFixture) addRule(
	t *testing.T, stageID uint64, seq int, ruleType string, userID, roleID uint64, levels int,
) model.SysApprovalAssigneeRule {
	t.Helper()
	r := &model.SysApprovalAssigneeRule{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		StageID:   stageID,
		Seq:       seq,
		RuleType:  ruleType,
		UserID:    userID,
		RoleID:    roleID,
		Levels:    levels,
	}
	require.NoError(t, f.db.Create(r).Error)
	return *r
}

// newStage 建一个模板 + 一关,回吐 (模板编码, 关卡 ID)
//
// ⚠️ **回吐的是 code 不是 id** —— `CreateDraftInput` 只认 `TemplateCode`
// (M84 的偏离记录:`SubmitReq`/`CreateDraftInput` 都不收 template_id,
// 让调用方能塞一个"指向别人模板"的 id 进来)。
func (f *resolverFixture) newStage(t *testing.T) (tplCode string, stageID uint64) {
	t.Helper()
	tpl := &model.SysApprovalTemplate{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Code:      "tpl_" + tplSuffix(t), Name: "t", BizType: testBizType, IsActive: true,
	}
	require.NoError(t, f.db.Create(tpl).Error)
	st := &model.SysApprovalStage{
		BaseModel:  model.BaseModel{ID: sonyflake.Next()},
		TemplateID: tpl.ID, StageIndex: 1, Name: "第1关",
		ReviewType: model.ApprovalReviewAny,
	}
	require.NoError(t, f.db.Create(st).Error)
	return tpl.Code, st.ID
}

// tplSuffix 把 `Test/Sub` 这种测试名压成能进 size:64 列的短串
func tplSuffix(t *testing.T) string {
	s := t.Name()
	s = strings.NewReplacer("/", "_", " ", "_").Replace(s)
	if len(s) > 40 {
		s = s[len(s)-40:]
	}
	return s
}

// resolve 是夹具上的快捷方式
func (f *resolverFixture) resolve(t *testing.T, stageID uint64, applicantUser, applicantIdentity uint64) ([]uint64, error) {
	t.Helper()
	return f.r.ResolveStage(context.Background(), f.db, ResolveStageRequest{
		TemplateID: 1, StageID: stageID, StageIndex: 1,
		ApplicantUserID: applicantUser, ApplicantIdentityID: applicantIdentity,
	})
}

// 有效期测试的三个锚点
var (
	// forward 身份有效期到天年 —— 本仓 identity 的「零值 = 永久」语义
	forward = time.Time{}
	past    = time.Now().Add(-24 * time.Hour)
	future  = time.Now().Add(24 * time.Hour)
)

// ============================================================================
// 来源 1:`user`
// ============================================================================

func TestResolve_UserRule(t *testing.T) {
	t.Run("指定的人就是审批人", func(t *testing.T) {
		f := newResolverFixture(t)
		alice := f.addUser(t, "alice", true, false)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleUser, alice, 0, 0)

		ids, err := f.resolve(t, stage, 999, 0)
		require.NoError(t, err)
		assert.Equal(t, []uint64{alice}, ids)
	})

	t.Run("停用的人不生成 task —— 空集合,不是报错", func(t *testing.T) {
		f := newResolverFixture(t)
		off := f.addUser(t, "off", false, false)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleUser, off, 0, 0)

		// 🔴 §5.2 第 1 条:**查得到、结果为空 = 数据常态**。
		// 写成 error 会让"配模板时在岗、提交时已离职"这种正常场景直接失败。
		ids, err := f.resolve(t, stage, 999, 0)
		require.NoError(t, err, "离职/停用是常态,不能报错(§5.2 第 1 条)")
		assert.Empty(t, ids)
	})

	t.Run("离职的人同样不算", func(t *testing.T) {
		f := newResolverFixture(t)
		gone := f.addUser(t, "gone", true, true)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleUser, gone, 0, 0)

		ids, err := f.resolve(t, stage, 999, 0)
		require.NoError(t, err)
		assert.Empty(t, ids, "is_leave=1 是离职,不是停用,一样不生成 task")
	})
}

// ============================================================================
// 来源 2:`role`
// ============================================================================

func TestResolve_RoleRule(t *testing.T) {
	t.Run("展开成该角色下所有在职用户", func(t *testing.T) {
		f := newResolverFixture(t)
		roleID := f.addRole(t, "finance", true)
		a := f.addUser(t, "fin_a", true, false)
		b := f.addUser(t, "fin_b", true, false)
		f.addIdentity(t, a, 0, "finance", true, past, forward)
		f.addIdentity(t, b, 0, "finance", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleRole, 0, roleID, 0)

		ids, err := f.resolve(t, stage, 999, 0)
		require.NoError(t, err)
		assert.ElementsMatch(t, []uint64{a, b}, ids)
	})

	t.Run("停用 / 离职 / 身份停用 / 身份未生效 / 身份已过期 —— 一律滤掉", func(t *testing.T) {
		f := newResolverFixture(t)
		roleID := f.addRole(t, "finance", true)
		ok := f.addUser(t, "ok", true, false)
		off := f.addUser(t, "off", false, false)
		gone := f.addUser(t, "gone", true, true)
		idOff := f.addUser(t, "id_off", true, false)
		notYet := f.addUser(t, "not_yet", true, false)
		expired := f.addUser(t, "expired", true, false)

		f.addIdentity(t, ok, 0, "finance", true, past, forward)
		f.addIdentity(t, off, 0, "finance", true, past, forward)
		f.addIdentity(t, gone, 0, "finance", true, past, forward)
		f.addIdentity(t, idOff, 0, "finance", false, past, forward)
		f.addIdentity(t, notYet, 0, "finance", true, future, forward)
		f.addIdentity(t, expired, 0, "finance", true, past, past) // valid_to 已过

		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleRole, 0, roleID, 0)

		ids, err := f.resolve(t, stage, 999, 0)
		require.NoError(t, err)
		// 🔴 这条用例专治"把有效期写成 SQL 条件 `valid_to >= now`":
		// valid_to 零值是公元 1 年,那样写会把 **ok 自己也滤掉**(实测全空)。
		assert.Equal(t, []uint64{ok}, ids,
			"只有完全在职且身份在有效期内的那个该进名单")
	})

	t.Run("角色下没人 —— 空集合,不是报错", func(t *testing.T) {
		f := newResolverFixture(t)
		roleID := f.addRole(t, "empty_role", true)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleRole, 0, roleID, 0)

		ids, err := f.resolve(t, stage, 999, 0)
		require.NoError(t, err)
		assert.Empty(t, ids)
	})

	t.Run("角色被删 —— 报错,不是空集合", func(t *testing.T) {
		f := newResolverFixture(t)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleRole, 0, 88888, 0) // 不存在的 role_id

		_, err := f.resolve(t, stage, 999, 0)
		// 🔴 角色不存在 = 规则指向了空的东西,跟"这个角色下没人"是两回事。
		// 归成空集合的话,提交期只会报一句「解析不到审批人」,
		// 排障的人会去查模板,查不出任何异常 —— 真凶是那个角色没了。
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalRuleInvalid, errcode.CodeOf(err))
	})

	t.Run("角色被停用 —— 同样报错", func(t *testing.T) {
		f := newResolverFixture(t)
		roleID := f.addRole(t, "retired_role", false)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleRole, 0, roleID, 0)

		_, err := f.resolve(t, stage, 999, 0)
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalRuleInvalid, errcode.CodeOf(err))
	})
}

// ============================================================================
// 来源 3:`dept_leader`
// ============================================================================

// deptChain 建一棵部门树:根 → 一级 → 二级 → 三级,每级一个有 leader
func deptChain(t *testing.T, f *resolverFixture) (uint64, uint64, uint64, uint64) {
	t.Helper()
	leaders := make([]uint64, 4)
	for i := range leaders {
		leaders[i] = f.addUser(t, "leader"+string(rune('A'+i)), true, false)
	}
	root := f.addDept(t, "root", "root", 0, leaders[0])
	l1 := f.addDept(t, "l1", "l1", root, leaders[1])
	l2 := f.addDept(t, "l2", "l2", l1, leaders[2])
	l3 := f.addDept(t, "l3", "l3", l2, leaders[3])
	return root, l1, l2, l3
}

func TestResolve_DeptLeader_Levels(t *testing.T) {
	t.Run("levels=1 即本部门主管", func(t *testing.T) {
		f := newResolverFixture(t)
		root, _, _, l3 := deptChain(t, f)
		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, l3, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 1)

		ids, err := f.resolve(t, stage, applicant, ident)
		require.NoError(t, err)
		assert.Len(t, ids, 1)
		assert.Equal(t, f.leaderOf(t, l3), ids[0], "levels=1 = 自己所在部门的主管")
		assert.NotEqual(t, f.leaderOf(t, root), ids[0])
	})

	t.Run("levels=2 是上级部门的主管", func(t *testing.T) {
		f := newResolverFixture(t)
		_, _, l2, l3 := deptChain(t, f)
		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, l3, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 2)

		ids, err := f.resolve(t, stage, applicant, ident)
		require.NoError(t, err)
		assert.Equal(t, []uint64{f.leaderOf(t, l2)}, ids, "爬一级取主管")
	})

	t.Run("levels<=0 一路爬到根", func(t *testing.T) {
		f := newResolverFixture(t)
		root, _, _, l3 := deptChain(t, f)
		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, l3, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 0) // <=0 = 爬到顶

		ids, err := f.resolve(t, stage, applicant, ident)
		require.NoError(t, err)
		assert.Equal(t, []uint64{f.leaderOf(t, root)}, ids)
	})

	t.Run("目标部门没配负责人 —— 空集合", func(t *testing.T) {
		f := newResolverFixture(t)
		root, _, _, _ := deptChain(t, f)
		bare := f.addDept(t, "bare", "bare", root, 0) // 没 leader
		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, bare, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 1)

		ids, err := f.resolve(t, stage, applicant, ident)
		require.NoError(t, err)
		assert.Empty(t, ids, "没配负责人是常态,不是故障")
	})

	t.Run("主管本人已离职 —— 空集合", func(t *testing.T) {
		f := newResolverFixture(t)
		_, _, _, l3 := deptChain(t, f)
		// 把 l3 的 leader 换成离职的人
		gone := f.addUser(t, "gone", true, true)
		require.NoError(t, f.db.Model(&model.SysDepartment{}).
			Where("id = ?", l3).UpdateColumn("leader_id", gone).Error)

		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, l3, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 1)

		ids, err := f.resolve(t, stage, applicant, ident)
		require.NoError(t, err)
		assert.Empty(t, ids, "主管离职了就不能生成 task(§5.2 第 3 条)")
	})

	t.Run("停用的中间部门仍然要穿过 —— 否则爬不到顶", func(t *testing.T) {
		f := newResolverFixture(t)
		root, _, _, _ := deptChain(t, f)
		mid := f.addDept(t, "mid", "mid", root, f.addUser(t, "midl", true, false))
		leaf := f.addDept(t, "leaf", "leaf", mid, f.addUser(t, "leafl", true, false))
		// 把中间层停用
		require.NoError(t, f.db.Model(&model.SysDepartment{}).
			Where("id = ?", mid).UpdateColumn("is_active", false).Error)

		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, leaf, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 0)

		ids, err := f.resolve(t, stage, applicant, ident)
		require.NoError(t, err)
		// 🔴 滤掉停用部门的话,链会在 mid 断掉,这里会返回 **mid 的主管** ——
		// 一个错的、静默的答案,没有任何报错。
		assert.Equal(t, []uint64{f.leaderOf(t, root)}, ids, "爬到顶必须穿过停用的中间部门")
	})

	t.Run("申请人没部门 —— 空集合", func(t *testing.T) {
		f := newResolverFixture(t)
		deptChain(t, f)
		applicant := f.addUser(t, "app", true, false) // 没身份,没 sys_user_department
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 1)

		ids, err := f.resolve(t, stage, applicant, 0)
		require.NoError(t, err)
		assert.Empty(t, ids)
	})

	t.Run("身份没部门时退到 sys_user_department 的主部门", func(t *testing.T) {
		f := newResolverFixture(t)
		_, _, l2, l3 := deptChain(t, f)
		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, 0, "owner", true, past, forward) // DeptID=0
		require.NoError(t, f.db.Create(&model.SysUserDepartment{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			UserID:    applicant, DeptID: l3, IsPrimary: true,
		}).Error)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 2)

		ids, err := f.resolve(t, stage, applicant, ident)
		require.NoError(t, err)
		assert.Equal(t, []uint64{f.leaderOf(t, l2)}, ids)
	})

	t.Run("层级不够 —— 空集合,不是报错(§5.2 第 1 条「超出层级」)", func(t *testing.T) {
		f := newResolverFixture(t)
		root, l1, _, _ := deptChain(t, f)
		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, l1, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 5) // 只有 2 级

		ids, err := f.resolve(t, stage, applicant, ident)
		require.NoError(t, err, "组织架构没那么深是常态,不是数据坏了")
		assert.Empty(t, ids)
		assert.NotNil(t, f.leaderOf(t, root)) // 根确实有主管,只是爬不到那一层
	})
}

// TestResolve_DeptLeader_Cycle 部门树成环(§5.2 第 2 条)
func TestResolve_DeptLeader_Cycle(t *testing.T) {
	t.Run("A→B→A 两节点成环 —— 报错", func(t *testing.T) {
		f := newResolverFixture(t)
		a := f.addDept(t, "a", "a", 0, 0)
		b := f.addDept(t, "b", "b", a, f.addUser(t, "bl", true, false))
		// 把 a 的上级改成 b,凑成 a→b→a
		require.NoError(t, f.db.Model(&model.SysDepartment{}).
			Where("id = ?", a).UpdateColumn("parent_id", b).Error)

		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, a, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 0)

		_, err := f.resolve(t, stage, applicant, ident)
		// 🔴 **没有环检测这就是死循环**,测试会挂到超时(比红更难查)。
		// §5.2 第 2 条要求的是 error 而不是"返回空集合"——
		// 空集合会让这一关显示"解析不到审批人",把数据损坏伪装成配置没配。
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalDeptTreeBroken, errcode.CodeOf(err))
	})

	t.Run("自环(parent_id 指向自己)—— 报错", func(t *testing.T) {
		f := newResolverFixture(t)
		a := f.addDept(t, "a", "a", 0, f.addUser(t, "al", true, false))
		require.NoError(t, f.db.Model(&model.SysDepartment{}).
			Where("id = ?", a).UpdateColumn("parent_id", a).Error)

		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, a, "owner", true, past, forward)
		_, stage := f.newStage(t)
		// ⚠️ **必须用 levels<=0(爬到顶)才测得到自环** —— levels=1 一步都不走,
		// 压根碰不到 parent_id,自然也发现不了环。这里写 levels=1 会得到一个
		// **恒绿**的用例(它根本没触发被测的分支)。
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 0)

		_, err := f.resolve(t, stage, applicant, ident)
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalDeptTreeBroken, errcode.CodeOf(err))
	})

	t.Run("parent_id 指向不存在的部门 —— 报错", func(t *testing.T) {
		f := newResolverFixture(t)
		a := f.addDept(t, "a", "a", 0, 0)
		require.NoError(t, f.db.Model(&model.SysDepartment{}).
			Where("id = ?", a).UpdateColumn("parent_id", 987654).Error)

		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, a, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 0)

		_, err := f.resolve(t, stage, applicant, ident)
		require.Error(t, err, "树不自洽和成环是同一类事故,不能当'没找到上级'放过")
		assert.Equal(t, errcode.ApprovalDeptTreeBroken, errcode.CodeOf(err))
	})

	t.Run("超过 10 级的深链 —— 报错(上限兜底)", func(t *testing.T) {
		f := newResolverFixture(t)
		parent := uint64(0)
		for i := 0; i < 12; i++ {
			parent = f.addDept(t, "d"+string(rune('a'+i)), "d"+string(rune('a'+i)),
				parent, f.addUser(t, "dl"+string(rune('a'+i)), true, false))
		}
		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, parent, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 0)

		_, err := f.resolve(t, stage, applicant, ident)
		require.Error(t, err, "链比 10 级还深,该有人来看一眼,而不是静默返回一个主管")
		assert.Equal(t, errcode.ApprovalDeptTreeBroken, errcode.CodeOf(err))
	})
}

// leaderOf 读一个部门的 leader_id(测试侧的观测口径)
func (f *resolverFixture) leaderOf(t *testing.T, deptID uint64) uint64 {
	t.Helper()
	var d model.SysDepartment
	require.NoError(t, f.db.First(&d, "id = ?", deptID).Error)
	return d.LeaderID
}

// ============================================================================
// 规则层:未知 rule_type / 多条规则是「或」
// ============================================================================

func TestResolve_RuleErrors(t *testing.T) {
	t.Run("未知 rule_type —— 报错,不是空集合", func(t *testing.T) {
		f := newResolverFixture(t)
		alice := f.addUser(t, "alice", true, false)
		_, stage := f.newStage(t)
		// 一条好规则 + 一条垃圾规则:垃圾那条必须炸,不能被当成"这条没人"
		f.addRule(t, stage, 1, model.ApprovalRuleUser, alice, 0, 0)
		f.addRule(t, stage, 2, "leader_n", 0, 0, 1)

		_, err := f.resolve(t, stage, 999, 0)
		require.Error(t, err, "词表只有三种(§5.1),第四种是垃圾数据")
		assert.Equal(t, errcode.ApprovalRuleInvalid, errcode.CodeOf(err))
	})

	t.Run("多条规则是「或」—— 命中任一即可", func(t *testing.T) {
		f := newResolverFixture(t)
		a := f.addUser(t, "a", true, false)
		roleID := f.addRole(t, "hr", true)
		b := f.addUser(t, "b", true, false)
		f.addIdentity(t, b, 0, "hr", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleUser, a, 0, 0)
		f.addRule(t, stage, 2, model.ApprovalRuleRole, 0, roleID, 0)

		ids, err := f.resolve(t, stage, 999, 0)
		require.NoError(t, err)
		assert.ElementsMatch(t, []uint64{a, b}, ids, "两条规则的人都要进来")
	})

	t.Run("这一关没配规则 —— 空集合", func(t *testing.T) {
		f := newResolverFixture(t)
		_, stage := f.newStage(t)

		ids, err := f.resolve(t, stage, 999, 0)
		require.NoError(t, err)
		assert.Empty(t, ids)
	})
}

// ============================================================================
// §5.2 第 1 条:error ≠ 空集合
// ============================================================================

func TestResolve_DBFault_IsErrorNotEmpty(t *testing.T) {
	t.Run("规则表查不了 —— 必须报错", func(t *testing.T) {
		f := newResolverFixture(t)
		alice := f.addUser(t, "alice", true, false)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleUser, alice, 0, 0)
		// 把规则表删掉 —— 模拟"数据源故障"
		require.NoError(t, f.db.Migrator().DropTable(&model.SysApprovalAssigneeRule{}))

		ids, err := f.resolve(t, stage, 999, 0)
		// 🔴 **这是整份实现里最重要的一条断言。**
		// 返回空集合的话,§5.3② 会报「解析不到审批人」,申请人去找流程管理员,
		// 而流程管理员查模板一切正常 —— 故障被伪装成配置问题,而且每次必现。
		require.Error(t, err, "DB 故障必须报错,不能降级成空集合(§5.2 第 1 条)")
		assert.Nil(t, ids)
	})

	t.Run("用户表查不了 —— 必须报错", func(t *testing.T) {
		f := newResolverFixture(t)
		roleID := f.addRole(t, "finance", true)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleRole, 0, roleID, 0)
		require.NoError(t, f.db.Migrator().DropTable(&model.SysUser{}))

		_, err := f.resolve(t, stage, 999, 0)
		require.Error(t, err)
	})

	// 🔴 **这条是探针 Q09 打出来的** —— 前一版只测了 role 规则下的
	// 「用户表查不了」,而那条走的是 `resolveRole` 的 JOIN,压根碰不到
	// `resolveUser` 的 err 分支。把 resolveUser 那句改成 `return nil, nil`
	// 测试照样全绿 —— **探针跑过不等于探针打中**(M84 教训,§13)。
	t.Run("指定人规则:用户表查不了 —— 必须报错", func(t *testing.T) {
		f := newResolverFixture(t)
		alice := f.addUser(t, "alice", true, false)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleUser, alice, 0, 0)
		require.NoError(t, f.db.Migrator().DropTable(&model.SysUser{}))

		_, err := f.resolve(t, stage, 999, 0)
		require.Error(t, err, "resolveUser 的 err 分支不能降级成空集合")
	})

	t.Run("部门表查不了 —— 必须报错", func(t *testing.T) {
		f := newResolverFixture(t)
		_, _, _, l3 := deptChain(t, f)
		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, l3, "owner", true, past, forward)
		_, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 1)
		require.NoError(t, f.db.Migrator().DropTable(&model.SysDepartment{}))

		_, err := f.resolve(t, stage, applicant, ident)
		require.Error(t, err, "读不到部门表就不能定位主管,更不能返回空集合")
	})

	t.Run("db 传 nil —— 报错,不许悄悄用解析器自带的连接", func(t *testing.T) {
		f := newResolverFixture(t)
		_, stage := f.newStage(t)
		// 用自带的 db 读会绕开审批事务(D8),必须在入口就挡住
		_, err := f.r.ResolveStage(context.Background(), nil,
			ResolveStageRequest{StageID: stage, StageIndex: 1})
		require.Error(t, err)
	})
}

// ============================================================================
// §5.3① 配置期预检
// ============================================================================

func TestPrecheck_ConfigTime(t *testing.T) {
	t.Run("某一关解析不到人 —— 拒绝保存", func(t *testing.T) {
		f := newResolverFixture(t)
		off := f.addUser(t, "off", false, false)
		err := f.r.PrecheckTemplate(context.Background(), f.db, TemplatePrecheckInput{
			TemplateCode: "material_review",
			Stages: []StageRuleCheck{
				{StageIndex: 1, Rules: []model.SysApprovalAssigneeRule{
					{RuleType: model.ApprovalRuleUser, UserID: off},
				}},
			},
		})
		require.Error(t, err, "§5.3①:空审批人在保存时就该拦住,而不是等人来提交")
		assert.Equal(t, errcode.ApprovalNoAssignee, errcode.CodeOf(err))
		assert.Contains(t, err.Error(), "第 1 关")
		assert.Contains(t, err.Error(), "material_review")
	})

	t.Run("一关没配任何规则 —— 拒绝保存", func(t *testing.T) {
		f := newResolverFixture(t)
		err := f.r.PrecheckTemplate(context.Background(), f.db, TemplatePrecheckInput{
			TemplateCode: "t", Stages: []StageRuleCheck{{StageIndex: 2}},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "第 2 关")
	})

	t.Run("多条规则里有一条命中就放行(「或」语义)", func(t *testing.T) {
		f := newResolverFixture(t)
		off := f.addUser(t, "off", false, false)
		ok := f.addUser(t, "ok", true, false)
		err := f.r.PrecheckTemplate(context.Background(), f.db, TemplatePrecheckInput{
			TemplateCode: "t", Stages: []StageRuleCheck{{StageIndex: 1,
				Rules: []model.SysApprovalAssigneeRule{
					{RuleType: model.ApprovalRuleUser, UserID: off},
					{RuleType: model.ApprovalRuleUser, UserID: ok},
				}}},
		})
		assert.NoError(t, err, "配 3 条主管规则、只有 2 个部门有人,是合法配置")
	})

	t.Run("全是 dept_leader —— 放行,交给提交期", func(t *testing.T) {
		f := newResolverFixture(t)
		err := f.r.PrecheckTemplate(context.Background(), f.db, TemplatePrecheckInput{
			TemplateCode: "t", Stages: []StageRuleCheck{{StageIndex: 1,
				Rules: []model.SysApprovalAssigneeRule{
					{RuleType: model.ApprovalRuleDeptLeader, Levels: 1},
				}}},
		})
		// 🔴 dept_leader 的结果本质依赖发起人,保存模板时还不知道发起人是谁。
		// 这里只能判 "levels 合法",判不了 "有没有人" —— 判成不放行会让
		// 「部门主管审批」这种最常见的流程根本存不下来。
		assert.NoError(t, err)
	})

	t.Run("角色规则指向不存在的角色 —— 拒绝保存", func(t *testing.T) {
		f := newResolverFixture(t)
		err := f.r.PrecheckTemplate(context.Background(), f.db, TemplatePrecheckInput{
			TemplateCode: "t", Stages: []StageRuleCheck{{StageIndex: 1,
				Rules: []model.SysApprovalAssigneeRule{
					{RuleType: model.ApprovalRuleRole, RoleID: 424242},
				}}},
		})
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalRuleInvalid, errcode.CodeOf(err))
	})

	t.Run("全部关卡都有人 —— 放行", func(t *testing.T) {
		f := newResolverFixture(t)
		a := f.addUser(t, "a", true, false)
		b := f.addUser(t, "b", true, false)
		err := f.r.PrecheckTemplate(context.Background(), f.db, TemplatePrecheckInput{
			TemplateCode: "t", Stages: []StageRuleCheck{
				{StageIndex: 1, Rules: []model.SysApprovalAssigneeRule{
					{RuleType: model.ApprovalRuleUser, UserID: a}}},
				{StageIndex: 2, Rules: []model.SysApprovalAssigneeRule{
					{RuleType: model.ApprovalRuleUser, UserID: b}}},
			},
		})
		assert.NoError(t, err)
	})
}

// ============================================================================
// §5.3② / ③ 提交期拦截 + **没有运行期兜底**(端到端)
// ============================================================================

// realEngine 装一个用真解析器的引擎
func (f *resolverFixture) realEngine() ApprovalEngine {
	return NewApprovalEngine(f.db, f.r,
		map[string]BizHandler{testBizType: &recordingBizHandler{}}, nil)
}

// TestLayer23_Submit_EmptyAssignee 真解析器 + 引擎:解析不到人时提交必须失败
func TestLayer23_Submit_EmptyAssignee(t *testing.T) {
	ctx := context.Background()

	t.Run("② 提交期:解析不到人 → 20601,且单子留在草稿", func(t *testing.T) {
		f := newResolverFixture(t)
		off := f.addUser(t, "off", false, false) // 已停用
		tplCode, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleUser, off, 0, 0)
		e := f.realEngine()

		var id uint64
		require.NoError(t, f.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			got, err := e.CreateDraft(ctx, tx, &CreateDraftInput{
				BizType: testBizType, BizID: sonyflake.Next(),
				Title: "资料", Variables: "{}", TemplateCode: tplCode,
				ApplicantUserID: 500, ApplicantIdentityID: 1,
			})
			id = got
			return err
		}))

		err := e.Submit(ctx, callerOf(500), id, SubmitReq{})
		require.Error(t, err)
		assert.Equal(t, errcode.ApprovalNoAssignee, errcode.CodeOf(err))

		var inst model.SysApprovalInstance
		require.NoError(t, f.db.First(&inst, "id = ?", id).Error)
		assert.Equal(t, model.ApprovalStatusDraft, inst.Status, "提交失败不该推进状态")
	})

	t.Run("③ 没有运行期兜底:不生成任何 task,不自动升级给任何人", func(t *testing.T) {
		f := newResolverFixture(t)
		off := f.addUser(t, "off", false, false)
		admin := f.addUser(t, "admin", true, false) // 假设有个超管
		tplCode, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleUser, off, 0, 0)
		e := f.realEngine()

		var id uint64
		require.NoError(t, f.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			got, err := e.CreateDraft(ctx, tx, &CreateDraftInput{
				BizType: testBizType, BizID: sonyflake.Next(),
				Title: "资料", Variables: "{}", TemplateCode: tplCode,
				ApplicantUserID: 500, ApplicantIdentityID: 1,
			})
			id = got
			return err
		}))
		require.Error(t, e.Submit(ctx, callerOf(500), id, SubmitReq{}))

		var n, nAdmin int64
		require.NoError(t, f.db.Model(&model.SysApprovalTask{}).
			Where("instance_id = ?", id).Count(&n).Error)
		require.NoError(t, f.db.Model(&model.SysApprovalTask{}).
			Where("instance_id = ? AND assignee_user_id = ?", id, admin).
			Count(&nAdmin).Error)
		// 🔴 §5.3③ 明确「运行期不做兜底」:主流引擎那种「空审批人自动升级给
		// 租户管理员」的策略本项目不采用。**一条 task 都不该落库** ——
		// 哪怕是 admin 那条。静默发给一个无关的人比报错糟糕得多。
		assert.Zero(t, n, "运行期兜底会把单子发给一个无关的人,一条 task 都不该有")
		assert.Zero(t, nAdmin, "更不能把单子自动塞给超管")
	})

	t.Run("部门主管已离职 → 提交失败,不是自动升级", func(t *testing.T) {
		f := newResolverFixture(t)
		root, _, _, _ := deptChain(t, f)
		applicant := f.addUser(t, "app", true, false)
		ident := f.addIdentity(t, applicant, root, "owner", true, past, forward)
		// 主管离职
		require.NoError(t, f.db.Model(&model.SysUser{}).
			Where("id = ?", f.leaderOf(t, root)).UpdateColumn("is_leave", true).Error)

		tplCode, stage := f.newStage(t)
		f.addRule(t, stage, 1, model.ApprovalRuleDeptLeader, 0, 0, 1)
		e := f.realEngine()

		var id uint64
		require.NoError(t, f.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			got, err := e.CreateDraft(ctx, tx, &CreateDraftInput{
				BizType: testBizType, BizID: sonyflake.Next(),
				Title: "资料", Variables: "{}", TemplateCode: tplCode,
				ApplicantUserID: applicant, ApplicantIdentityID: ident,
			})
			id = got
			return err
		}))
		require.Error(t, e.Submit(ctx, callerOf(applicant), id, SubmitReq{}))
	})
}
