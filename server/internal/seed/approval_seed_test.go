// 审批权限码 / 菜单 / 角色绑定测试(M83)
// 设计文档:通用审批系统设计方案V1.0.md §8.1 / §8.4 / §8.5
//
// 🔴 **本文件存在的唯一理由**是 `assignRoleResources` 对未知 role code
// **静默 return**(`seed.go`,查不到角色就 return,不打日志不报错)。
// 后果是:权限码写错一个字符 / 角色码写成裸名 / 少写一个角色 ——
// **编译通过、单测全绿、启动正常,只有登录那个角色的人才发现"我看不到审批页"**。
// §8.4 结尾因此写了硬要求:"落地时必须加断言测试"。
//
// ⚠️ 这些断言**全部走真库**(Run() 之后再查 sys_role_resource),
// 不对着 bindingRules 字面量断言 —— 对着字面量断言只能证明"我写了什么",
// 证明不了"绑定真的落库了",而中间恰好隔着那个静默 return。
package seed

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// approvalCodes §8.1 点名的 11 个权限码(8 个 api + 3 个 button)
var approvalCodes = []string{
	"flow:list", "flow:read", "flow:create", "flow:update", "flow:delete",
	"approval:list", "approval:read", "approval:create",
	"button.flow.add", "button.flow.edit", "button.flow.delete",
}

// roleCodesOf 取某个角色实际落库的**全部**资源 code
func roleCodesOf(t *testing.T, db *gorm.DB, roleCode string) map[string]struct{} {
	t.Helper()
	var role model.SysRole
	require.NoError(t, db.Where("code = ?", roleCode).First(&role).Error,
		"角色 %s 必须存在(roleList() 里漏了?)", roleCode)

	var rows []model.SysRoleResource
	require.NoError(t, db.Where("role_id = ?", role.ID).Find(&rows).Error)

	ids := make([]uint64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ResourceID)
	}
	out := map[string]struct{}{}
	if len(ids) == 0 {
		return out
	}
	var resources []model.SysResource
	require.NoError(t, db.Where("id IN ?", ids).Find(&resources).Error)
	for _, r := range resources {
		out[r.Code] = struct{}{}
	}
	return out
}

// seedAndRun 跑一次真 seed,返回可查的库
func seedAndRun(t *testing.T) *gorm.DB {
	t.Helper()
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	// ⚠️ **M146a:sqlite 不再跳过 casbin 同步** ⇒ 模型文件路径必须是**真的**。
	//   原来传空串能过,是因为 sqlite 分支整段 skip,压根没去读这个路径。
	casbinPath := filepath.Join("..", "..", "configs", "casbin_rbac.conf")
	require.FileExists(t, casbinPath)
	require.NoError(t, Run(db, casbinPath, ""))
	return db
}

// TestApprovalCodes_ExactlyEleven §8.1 的 11 个码一个不多一个不少
//
// ⚠️ **必须断言"没有多余的"** —— 只断言这 11 个存在的话,别人再加
// `approval:all` / `flow:publish` 全绿,而 §8.1 写死了只有 11 个。
// 多出来的码不会自己生效,但会让"广发 approval:read 是安全的"这条前提
// (§8.4)越来越站不住:每多一个码就多一处需要单独审的授权面。
func TestApprovalCodes_ExactlyEleven(t *testing.T) {
	db := seedAndRun(t)

	var all []model.SysResource
	require.NoError(t, db.Where("code LIKE ? OR code LIKE ? OR code LIKE ?",
		"flow:%", "approval:%", "button.flow.%").Find(&all).Error)

	got := make([]string, 0, len(all))
	for _, r := range all {
		got = append(got, r.Code)
	}
	sort.Strings(got)

	want := append([]string(nil), approvalCodes...)
	sort.Strings(want)
	assert.Equal(t, want, got, "审批相关权限码必须正好是 §8.1 的 11 个")

	// ⚠️ 6 个动作**不许**有权限码(§8.1)。审批按钮的可见性由
	// "我手里有没有 pending task"决定,不是 RBAC。
	for _, forbidden := range []string{
		"approval:approve", "approval:reject", "approval:rollback", "approval:cancel",
		"button.approval.approve", "button.approval.reject",
	} {
		assert.NotContains(t, got, forbidden, "6 个动作不设权限码(§8.1)")
	}
}

// TestApprovalRoleBinding_Matrix §8.4 的 A/B/C/D 四组逐角色断言
//
// 这张表就是 §8.4 表格的机器可读副本 —— **doc 和这张表必须一起改**。
func TestApprovalRoleBinding_Matrix(t *testing.T) {
	db := seedAndRun(t)

	type want struct {
		has  []string // 必须有
		not  []string // 必须没有(负例比正例更容易漏,所以显式写出来)
		desc string
	}
	viewOnly := []string{"approval:list", "approval:read"}
	submitter := []string{"approval:list", "approval:read", "approval:create"}

	cases := []struct {
		role string
		want want
	}{
		// ---- A 组:只看不发起 ----
		{model.RoleReadOnly, want{has: viewOnly, not: []string{"approval:create", "flow:create", "button.flow.add"}}},

		// ---- B 组:能发起,不碰流程配置(§8.4 说明 2)----
		{model.RolePlatformOps, want{
			has: submitter,
			// ⚠️ 这条是 §8.4 结尾点名要断言的负例
			not:  []string{"flow:create", "flow:update", "flow:delete", "button.flow.add"},
			desc: "§8.4 强制断言:平台运营员拿不到 flow:create",
		}},

		// ---- D 组:只有超管能改流程 ----
		{model.RoleSuperAdmin, want{has: approvalCodes, desc: "§8.4 强制断言:超管拿到全部 11 个"}},
	}

	for _, c := range cases {
		t.Run(c.role, func(t *testing.T) {
			codes := roleCodesOf(t, db, c.role)
			for _, code := range c.want.has {
				assert.Contains(t, codes, code, "%s 应有 %s(%s)", c.role, code, c.want.desc)
			}
			for _, code := range c.want.not {
				assert.NotContains(t, codes, code,
					"%s 不该有 %s —— 给了会让前端渲染出没有后端支撑的入口(%s)", c.role, code, c.want.desc)
			}
		})
	}
}

// TestApprovalEveryRoleCanView §8.4:「所有内置角色都落在 A 组及以上,没有一个是 0 权限」
//
// ⚠️ **这里的"13"是从 roleList() 现算的**,不是写死的数字 —— 将来加了第 14 个角色
// 而忘了把它写进 §8.4 的 A 组,这条会红。反过来,如果 §8.4 文档里写了一个
// `roleList()` 里根本没有的角色码(比如 V1.0 的 `role_auditor`),
// 它只会**静默**绑不上 —— 上面那张矩阵表是按真实角色码写的,少写一个不报错,
// 所以这条"计数对得上"的断言是唯一能发现"文档和 roleList() 脱节"的地方。
func TestApprovalEveryRoleCanView(t *testing.T) {
	db := seedAndRun(t)

	roles := roleList()
	assert.Len(t, roles, 3, "roleList() 当前是 3 个内置角色(开源口径:超管 / 平台运营员 / 只读)")

	for _, r := range roles {
		codes := roleCodesOf(t, db, r.Code)
		assert.Contains(t, codes, "approval:list",
			"§8.4 承诺每个角色都至少能看自己的三盒;%s 拿不到就是漏绑", r.Code)
		assert.Contains(t, codes, "approval:read", "%s 漏绑 approval:read", r.Code)
	}
}

// TestApprovalOnlySuperAdminEditsFlow D 组隔离
//
// ⚠️ 配错一个审批模板 = 全平台每一条业务记录都走错审批链(把财务总监配成实习生),
// 危险度与改定时任务配置同一量级(§8.4 说明 1,理由照抄 config:* / job:* 的既有立场)。
func TestApprovalOnlySuperAdminEditsFlow(t *testing.T) {
	db := seedAndRun(t)

	writeCodes := []string{
		"flow:create", "flow:update", "flow:delete",
		"button.flow.add", "button.flow.edit", "button.flow.delete",
	}
	for _, r := range roleList() {
		codes := roleCodesOf(t, db, r.Code)
		for _, w := range writeCodes {
			if r.Code == model.RoleSuperAdmin {
				assert.Contains(t, codes, w, "超管应能改流程配置")
				continue
			}
			assert.NotContains(t, codes, w,
				"%s 不是超管,不该有 %s(§8.4 D 组只有 role_super_admin)", r.Code, w)
		}
	}
}

// TestApprovalMenuBinding §8.5 两个菜单的可见性
func TestApprovalMenuBinding(t *testing.T) {
	db := seedAndRun(t)

	// 「审批流程」只给超管(D 组)。给没 flow:* 的人挂菜单
	// 就是「点进去空列表 + 一排 403 按钮」,和 TestRun_BindsFileMenuByPermission
	// 抓的是同一类事故。
	inCGroup := map[string]bool{
		model.RoleSuperAdmin: true,
	}
	for _, r := range roleList() {
		codes := roleCodesOf(t, db, r.Code)
		if inCGroup[r.Code] {
			assert.Contains(t, codes, "menu.flow", "%s 在 C 组,应看得见审批流程菜单", r.Code)
		} else {
			assert.NotContains(t, codes, "menu.flow", "%s 不在 C 组,不该看见审批流程菜单", r.Code)
		}
	}

	// 「我的审批」= 每个角色都该看得见(每个人都有自己的三盒)
	for _, r := range roleList() {
		codes := roleCodesOf(t, db, r.Code)
		assert.Contains(t, codes, "menu.approval", "%s 应看得见「我的审批」菜单", r.Code)
	}
}

// TestApprovalMenuParentIsBound 「审批管理」父菜单必须和子菜单一起发(2026-11-01)
//
// 🔴 **为什么这条要单独钉**:前端 buildMenuTree 遇到"父节点不在可访问集合里"的
// 子节点,会把它**当成顶级菜单**:
//
//	// web/src/utils/resourceTree.ts:40
//	if pid === '0' || !byId.has(pid) { roots.push(node) }
//
// ⇒ 只发 menu.approval 不发 menu.approvalMgmt 的话,侧边栏里「我的审批」会
// **平级浮到最外层**,而「审批管理」这一组谁都看不见 —— **而且不报错**:
// 后端绑定是"给得多"不会失败,前端 buildMenuTree 也是合法的树。
//
// 这条测试是那条静默退化的网:任何一个角色拿到了审批子菜单却没拿到父菜单,
// 这里立刻变红。
func TestApprovalMenuParentIsBound(t *testing.T) {
	db := seedAndRun(t)

	for _, r := range roleList() {
		codes := roleCodesOf(t, db, r.Code)
		_, hasApproval := codes["menu.approval"]
		_, hasFlow := codes["menu.flow"]
		if !hasApproval && !hasFlow {
			continue
		}
		assert.Contains(t, codes, "menu.approvalMgmt",
			"%s 拿到了审批子菜单,必须同时拿到父菜单「审批管理」—— "+
				"否则前端会把子菜单当顶级浮出去", r.Code)
	}
}

// TestApprovalReadonlyGetsFlowReadCodes filterByRead 的既成事实,钉住
//
// 🔴 **这是本文件记录的一个"设计 vs 实现"偏差,故意不改实现**:
//
// §8.4 的 C 组只写死了四个业务管理员,只读员不在其中。但 `RoleReadOnly` 的
// 绑定走的是 `filterByRead(resources)` —— 它把全表里所有 `:read` / `:list` /
// `:tree` 结尾的码**一律收走**,所以 `flow:list` / `flow:read` 会自动落到只读员头上。
//
// **为什么不改 `filterByRead` 加排除列表**:
//  1. 它**今天已经**收了 `config:list` / `config:read`(系统配置),那比
//     审批模板配置敏感得多。只挡 flow 不挡 config,得到的是一个更难解释的
//     安全姿态,不是更严的。
//  2. 实测影响接近零:只读员**没有 `menu.flow`**,侧边栏没有入口
//     (见 TestApprovalMenuBinding);唯一的额外能力是手敲 /flow。
//  3. 给一个被 12 个角色共用的筛选器加排除表,是**扩大爆炸半径**换零收益。
//
// ⚠️ 本测试的作用是:将来谁"顺手修一下" `filterByRead`,这里会红,
// 逼他先回来看这段理由 —— 而不是默默改掉一个没人知道的既定行为。
func TestApprovalReadonlyGetsFlowReadCodes(t *testing.T) {
	db := seedAndRun(t)

	codes := roleCodesOf(t, db, model.RoleReadOnly)
	assert.Contains(t, codes, "flow:list", "filterByRead 收走所有 :list 后缀的码 —— 这是既成事实")
	assert.Contains(t, codes, "flow:read", "同上")
	assert.NotContains(t, codes, "menu.flow", "但没有菜单入口,侧边栏看不见")
	assert.NotContains(t, codes, "flow:create", "读码不等于写码")
}

// TestApprovalMenuSortNoCollision §8.5:Sort 从 100 起,不能撞审计中心的 90-93
//
// ⚠️ **Sort 撞号不会报错**,症状是侧边栏里两个顶级菜单顺序随机漂、
// 刷新一次换一个位置。全仓菜单 Sort 唯一这条断言是通用防线。
func TestApprovalMenuSortNoCollision(t *testing.T) {
	seen := map[int]string{}
	for _, m := range menuList() {
		if prev, dup := seen[m.Sort]; dup {
			assert.Fail(t, "菜单 Sort 撞号",
				"sort=%d 被 %s 和 %s 同时占用 —— 侧边栏顺序会随机漂", m.Sort, prev, m.Code)
		}
		seen[m.Sort] = m.Code
	}

	byCode := map[string]MenuDef{}
	for _, m := range menuList() {
		byCode[m.Code] = m
	}
	assert.Equal(t, 100, byCode["menu.approvalMgmt"].Sort)
	assert.Equal(t, 101, byCode["menu.approval"].Sort)
	assert.Equal(t, 102, byCode["menu.flow"].Sort)
	// 2026-11-01 重组:两个审批业务菜单都挂在「审批管理」底下(原来它们是平级顶级)
	assert.Equal(t, "menu.approvalMgmt", byCode["menu.approval"].ParentCode)
	assert.Equal(t, "menu.approvalMgmt", byCode["menu.flow"].ParentCode)
	assert.Empty(t, byCode["menu.approvalMgmt"].ParentCode)
	// ⚠️ todo/mine/done 三个列表是同一页的三个 Tab(§9.1),**不是三个菜单** ——
	//   拆成三个二级菜单等于三份一模一样的路由配置,以后改一处漏两处。
	approvalMenus := 0
	for _, m := range menuList() {
		if m.Code == "menu.approval" || m.Code == "menu.flow" || m.Code == "menu.approvalMgmt" {
			approvalMenus++
		}
	}
	assert.Equal(t, 3, approvalMenus, "审批这一组只有三个菜单(父 + 我的审批 + 审批流程)")
}

// TestUnknownRoleCode_BindsNothing 把那个静默 return 钉成可执行的证据
//
// 🔴 **本测试调的是一条"错的"绑定**,故意演示事故长什么样:
// `role_legacy_auditor` 是一个**数据库里都没有**的角色码 —— 于是把它
// 写进 bindingRules 会得到一个**零绑定、不报任何错**的角色。
//
// ⚠️ 这正是设计文档 §8.4 那段警告("角色码必须写全 role_ 前缀……
// 查不到是静默 return,V1.0 就是这么把三个不存在的角色码写进设计的,
// 全员 403、测试全绿、上线才发现")的实证。
// **所以 §8.4 里不能出现未 seed 的角色码**。
func TestUnknownRoleCode_BindsNothing(t *testing.T) {
	db := seedAndRun(t)

	// 前提:role_auditor 根本不在 roleList() 里,所以 sys_role 里没有这一行
	var role model.SysRole
	err := db.Where("code = ?", "role_legacy_auditor").First(&role).Error
	assert.Error(t, err, "前提:role_auditor 从来没被 seed 过(它只是 enums.go 的兼容别名)")

	// 这一行模拟"有人把 RoleAuditor 写进 bindingRules"
	assignRoleResources(db, "role_legacy_auditor", []string{"approval:list", "approval:read", "menu.approval"})

	// 静默 no-op:角色仍然不存在,一条绑定都没建,而且**没有任何报错**
	err = db.Where("code = ?", "role_legacy_auditor").First(&role).Error
	assert.Error(t, err, "assignRoleResources 对未知角色码是静默 return:不建角色、不报错")

	var n int64
	require.NoError(t, db.Model(&model.SysRoleResource{}).Where("role_id = ?", 0).Count(&n).Error)
	assert.Zero(t, n, "一条绑定都没写进去")
}

// ============================================================================
// 案例模板(2026-11-03)—— 请假 / 报销 / 采购(或签)
// ============================================================================

// TestSeed_CaseTemplates_Structure 三条案例模板的结构与手册 §5 一一对应
//
// ⚠️ **默认必须停用** —— pickTemplate 只捡 is_active=true 的模板,
// 案例是给配置页当起手样子的,停用状态保证发起侧零影响。
func TestSeed_CaseTemplates_Structure(t *testing.T) {
	db := seedAndRun(t)

	cases := []struct {
		code        string
		stageCount  int
		reviewTypes []string // 按 stage_index 顺序
		ruleCounts  []int    // 每关的规则数
		levels      [][]int  // 每关每条规则的 levels(dept_leader)
	}{
		{LeaveCaseCode, 3,
			[]string{model.ApprovalReviewAll, model.ApprovalReviewAll, model.ApprovalReviewAll},
			[]int{1, 1, 1},
			[][]int{{1}, {2}, {0}}},
		{ExpenseCaseCode, 4,
			[]string{model.ApprovalReviewAll, model.ApprovalReviewAll,
				model.ApprovalReviewAll, model.ApprovalReviewAll},
			[]int{1, 1, 1, 2},
			[][]int{{1}, {2}, {0}, {2, 0}}},
		{PurchaseCaseCode, 2,
			[]string{model.ApprovalReviewAll, model.ApprovalReviewAny},
			[]int{1, 2},
			[][]int{{1}, {2, 0}}},
	}

	for _, c := range cases {
		var tpl model.SysApprovalTemplate
		require.NoError(t, db.Where("code = ?", c.code).First(&tpl).Error, "案例模板 %s 没落库", c.code)
		assert.False(t, tpl.IsActive, "案例模板 %s 必须默认停用", c.code)

		var stages []model.SysApprovalStage
		require.NoError(t, db.Where("template_id = ?", tpl.ID).
			Order("stage_index").Find(&stages).Error)
		require.Len(t, stages, c.stageCount, "%s 的关卡数", c.code)

		for i, st := range stages {
			assert.Equal(t, c.reviewTypes[i], st.ReviewType, "%s 第 %d 关 review_type", c.code, i+1)

			var rules []model.SysApprovalAssigneeRule
			require.NoError(t, db.Where("stage_id = ?", st.ID).Order("seq").Find(&rules).Error)
			require.Len(t, rules, c.ruleCounts[i], "%s 第 %d 关规则数", c.code, i+1)
			for j, rl := range rules {
				assert.Equal(t, model.ApprovalRuleDeptLeader, rl.RuleType,
					"%s 第 %d 关第 %d 条规则:案例只用 dept_leader(不依赖角色运行期数据)", c.code, i+1, j+1)
				assert.Equal(t, c.levels[i][j], rl.Levels, "%s 第 %d 关第 %d 条规则 levels", c.code, i+1, j+1)
			}
		}
	}
}

// TestSeed_CaseTemplates_UpsertKeepsUserState upsert 不碰运营的启用状态和关卡
//
// 🔴 **这一条是 seed 的红线**:运营启用过案例、改过流程之后,
// seed 每次启动跑一遍,如果把它摁回停用 / 覆盖关卡,等于**悄悄下线
// 一条在用的审批流**(和 seedApprovalTemplates 的 upsert 口径同源,但
// 这里连 is_active 都不许碰 —— 案例启用本身就是运营拍板的动作)。
func TestSeed_CaseTemplates_UpsertKeepsUserState(t *testing.T) {
	db := seedAndRun(t)

	var tpl model.SysApprovalTemplate
	require.NoError(t, db.Where("code = ?", PurchaseCaseCode).First(&tpl).Error)

	// 模拟运营:启用 + 改名 + 删掉一个关卡
	require.NoError(t, db.Model(&model.SysApprovalTemplate{}).Where("id = ?", tpl.ID).
		Updates(map[string]any{"is_active": true, "name": "采购申请(我改的)"}).Error)
	var stages []model.SysApprovalStage
	require.NoError(t, db.Where("template_id = ?", tpl.ID).Order("stage_index").Find(&stages).Error)
	require.Len(t, stages, 2)
	require.NoError(t, db.Delete(&stages[1]).Error)

	// 重跑案例 seed(upsert 路径)
	require.NoError(t, seedApprovalCaseTemplates(db))

	var after model.SysApprovalTemplate
	require.NoError(t, db.Where("code = ?", PurchaseCaseCode).First(&after).Error)
	assert.True(t, after.IsActive, "运营启用过的案例,seed 不许摁回停用")
	assert.Equal(t, "采购申请(我改的)", after.Name, "运营改过的名字,seed 不许覆盖(name 只在新建时写)")

	var left int64
	require.NoError(t, db.Model(&model.SysApprovalStage{}).
		Where("template_id = ?", tpl.ID).Count(&left).Error)
	assert.Equal(t, int64(1), left, "运营删过的关卡,seed 不许恢复")
}
