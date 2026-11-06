// Phase 5 M2 — 菜单 seed 单元测试
// 文档:后台权限模型设计V1.0.md §17.19
//
// 覆盖:
//  1. menuList() 36 条菜单全部落地
//  2. parent_id 关联正确
//  3. Run() 幂等(重跑不报错,资源不重复)
//  4. 角色绑定 menu.* 资源(关键用户管理员/房源管理员)
package seed

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// newSeedTestDB 内存 sqlite + 全量 sys_* 表 autoMigrate
//
// 列表必须跟 seed.Run() 实际写入的表保持同步 —— 少一张,凡是走 Run() 的用例
// 都会在对应步骤炸 "no such table",而且是整片红,掩盖真实回归。
// SysConfig / SysDepartment 是 2026-10-19 后 seed 新增步骤时漏补的。
// 审批三表是 2026-10-24(M86-2)加默认流程模板 seed 时补的。
func newSeedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysRole{},
		&model.SysResource{},
		&model.SysRoleResource{},
		&model.SysUser{},
		&model.SysUserIdentity{},
		&model.SysLoginLog{},
		&model.SysSmsCode{},
		&model.SysDict{},       // 2026-10-18 L5:字典表
		&model.SysConfig{},     // 平台配置中心 seed
		&model.SysDepartment{}, // 部门树 seed
		// M86-2:默认审批流程模板 seed 落这三张表
		&model.SysApprovalTemplate{},
		&model.SysApprovalStage{},
		&model.SysApprovalAssigneeRule{},
	))
	return db
}

// TestMenuList_BasicInvariants menuList 内部数据合法性(不需要 DB)
func TestMenuList_BasicInvariants(t *testing.T) {
	menus := menuList()
	// 开源口径:23 = 5 顶级(dashboard / system / file / audit / approvalMgmt)
	// + 18 二级(system 10(含 skin)+ file 2 + audit 4 + approval 2)
	assert.Len(t, menus, 23, "菜单总数应为 23(5 顶级 + 18 二级)")

	// code 唯一
	seen := make(map[string]bool, len(menus))
	for _, m := range menus {
		assert.False(t, seen[m.Code], "code %s 重复", m.Code)
		seen[m.Code] = true
		assert.NotEmpty(t, m.Code)
		assert.NotEmpty(t, m.Name)
		assert.NotEmpty(t, m.Path, "menu %s path 必填", m.Code)
		assert.NotEmpty(t, m.Icon, "menu %s icon 必填", m.Code)
	}

	// parent_code 指向的菜单存在
	byCode := make(map[string]MenuDef, len(menus))
	for _, m := range menus {
		byCode[m.Code] = m
	}
	for _, m := range menus {
		if m.ParentCode == "" {
			continue
		}
		_, ok := byCode[m.ParentCode]
		assert.True(t, ok, "menu %s 父菜单 %s 不存在", m.Code, m.ParentCode)
	}
}

// TestRun_CreatesAllMenus Run() 后 sys_resource 应有 47 条 type=menu
func TestRun_CreatesAllMenus(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	// ⚠️ **M146a:sqlite 不再跳过 casbin 同步**,所以模型文件必须真的存在。
	//   原来这里写的是 `config/rbac_model.conf` —— 那个路径**从来就不存在**
	//   (真身在 configs/casbin_rbac.conf),能过是因为 sqlite 分支整段 skip,
	//   压根没去读它。同步不再跳过之后,这条假路径就露出来了。
	casbinPath := filepath.Join("..", "..", "configs", "casbin_rbac.conf")
	require.FileExists(t, casbinPath, "casbin model 文件必须存在 —— sqlite 路径也要真的同步策略")

	err := Run(db, casbinPath, "")
	require.NoError(t, err)

	var menus []model.SysResource
	require.NoError(t, db.Where("type = ?", "menu").Order("sort ASC, id ASC").Find(&menus).Error)
	assert.Len(t, menus, 23)

	// 顶级 menu 5 个(dashboard / system / file / audit / approvalMgmt)
	var topMenus []model.SysResource
	require.NoError(t, db.Where("type = ? AND parent_id = ?", "menu", 0).Find(&topMenus).Error)
	assert.Len(t, topMenus, 5, "顶级菜单应 5 个")

	// 每个顶级菜单的 parent_id 都应是顶级(0)
	for _, m := range menus {
		if m.ParentID == 0 {
			continue
		}
		// 二级菜单的父应是顶级
		var parent model.SysResource
		require.NoError(t, db.First(&parent, m.ParentID).Error)
		assert.Equal(t, "menu", parent.Type)
		assert.Equal(t, uint64(0), parent.ParentID, "menu %s 的父 %s 应是顶级", m.Code, parent.Code)
	}
}

// TestRun_ParentIDLinked 验证父菜单关联正确(不是只 verify 顶级,还 verify 二级的父 ID)
func TestRun_ParentIDLinked(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	require.NoError(t, Run(db, "non-existent-path", ""))

	// 取 menu.system(顶级)
	var sysMenu model.SysResource
	require.NoError(t, db.Where("code = ?", "menu.system").First(&sysMenu).Error)
	assert.Equal(t, uint64(0), sysMenu.ParentID, "menu.system 应是顶级")

	// 取 menu.system.user(二级)
	var userMenu model.SysResource
	require.NoError(t, db.Where("code = ?", "menu.system.user").First(&userMenu).Error)
	assert.Equal(t, sysMenu.ID, userMenu.ParentID, "menu.system.user 的父应是 menu.system")

	// path/icon/sort 都落地
	assert.Equal(t, "/system", sysMenu.Path)
	assert.Equal(t, "system", sysMenu.Icon)
	assert.Equal(t, 10, sysMenu.Sort)
}

// TestRun_IsIdempotent 验证 Run 重跑不报错,资源不重复
func TestRun_IsIdempotent(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)
	runSeedForTest(t, db)
	runSeedForTest(t, db)

	var menuCount, apiCount int64
	require.NoError(t, db.Model(&model.SysResource{}).Where("type = ?", "menu").Count(&menuCount).Error)
	require.NoError(t, db.Model(&model.SysResource{}).Where("type = ?", "api").Count(&apiCount).Error)
	assert.Equal(t, int64(23), menuCount, "菜单不应重复")
	// api 资源数 = resourceList() 里【非 button.*】的条目数。
	// 不能直接拿 len(resourceList()):seed 按命名空间分流,button.* 落的是
	// type='button' 而不是 'api'(见 seed.go 资源 upsert 分支)。
	var wantAPI int64
	for _, c := range resourceList() {
		if !strings.HasPrefix(c, "button.") {
			wantAPI++
		}
	}
	assert.Equal(t, wantAPI, apiCount, "api 资源不应重复")
}

// TestRun_BindsMenuToSuperAdmin 验证 super_admin 拿到所有 menu.*
func TestRun_BindsMenuToSuperAdmin(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	// super_admin id
	var super model.SysRole
	require.NoError(t, db.Where("code = ?", model.RoleSuperAdmin).First(&super).Error)

	// super_admin 应绑全部 menu 资源(顶级 + 二级)
	var bindings []model.SysRoleResource
	require.NoError(t, db.Where("role_id = ?", super.ID).Find(&bindings).Error)
	menuCodeIDs := make(map[uint64]bool)
	var menus []model.SysResource
	require.NoError(t, db.Where("type = ?", "menu").Find(&menus).Error)
	for _, m := range menus {
		menuCodeIDs[m.ID] = true
	}
	got := 0
	for _, b := range bindings {
		if menuCodeIDs[b.ResourceID] {
			got++
		}
	}
	assert.Equal(t, 23, got, "super_admin 应绑 23 个 menu 资源")
}

// TestRun_BindsDomainMenuToPlatformOps 验证平台运营员拿到 system 域菜单(开源口径 3 角色)
func TestRun_BindsDomainMenuToPlatformOps(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	var ops model.SysRole
	require.NoError(t, db.Where("code = ?", model.RolePlatformOps).First(&ops).Error)

	// 平台运营员应绑 menu.system.* (7 个:顶级 + 6 个二级)
	var bindings []model.SysRoleResource
	require.NoError(t, db.Joins("INNER JOIN sys_resource r ON r.id = sys_role_resource.resource_id").
		Where("sys_role_resource.role_id = ? AND r.type = ?", ops.ID, "menu").
		Find(&bindings).Error)

	// 取绑定的 code 列表
	resourceIDs := make([]uint64, 0, len(bindings))
	for _, b := range bindings {
		resourceIDs = append(resourceIDs, b.ResourceID)
	}
	var resources []model.SysResource
	require.NoError(t, db.Where("id IN ?", resourceIDs).Find(&resources).Error)

	codes := make([]string, 0, len(resources))
	for _, r := range resources {
		codes = append(codes, r.Code)
	}
	sort.Strings(codes)

	// 应含 menu.dashboard(公共) + menu.system 全部
	assert.Contains(t, codes, "menu.dashboard")
	assert.Contains(t, codes, "menu.system")
	assert.Contains(t, codes, "menu.system.user")
	assert.Contains(t, codes, "menu.system.role")
	// 不应含 config/job/smsTemplate(无接口权的死菜单)
	for _, c := range []string{"menu.system.config", "menu.system.job", "menu.system.smsTemplate"} {
		assert.NotContains(t, codes, c, "平台运营员没有对应 API 码,不该有这三个菜单")
	}
}

// runSeedForTest 跑一次 seed(casbin 模型用**真路径**)
//
// ⚠️ **为什么不能写 `Run(db, "", "")`** ——
// casbinx.Init 是 `sync.Once` + **局部 err**:第一次调用失败会把错误返回出去,
// 之后的调用 once 不再执行 ⇒ err 恒 nil。
// ⇒ 传空路径的测试**只有在"前面已经有别的测试用真路径初始化过 casbin"时才绿**
// —— 单独跑 `go test -run TestRun_xxx` 必红。
//
// ⚠️ **这正是"用例自己也会假绿"的典型** ——
// 它绿是因为别人替它把门开了(sync.Once 已经被别人触发),
// 不是因为它自己写的路径是对的。
func runSeedForTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	casbinPath := filepath.Join("..", "..", "configs", "casbin_rbac.conf")
	require.FileExists(t, casbinPath, "casbin model 文件必须存在 —— sqlite 路径也要真的同步策略")
	require.NoError(t, Run(db, casbinPath, ""))
}

// TestOpsSystemMenus_规则层就不含死菜单
//
// ⚠️ **这条钉的是"规则"本身,不是最终状态** ——
// 最终状态有**两条路**都能让它对:
// 规则里精确匹配(新库不绑)+ 定向撤权(老库删掉)
// ⇒ 只测最终状态的话,把规则改回前缀匹配**照样是绿的**
// (撤权那一步会把多绑的三条删掉,结果一模一样)。
// ⇒ 所以规则必须能**单独**被测 —— 这就是它被提成独立函数的原因。
func TestOpsSystemMenus_规则层就不含死菜单(t *testing.T) {
	all := []string{
		"menu.system", "menu.system.user", "menu.system.role", "menu.system.permission",
		"menu.system.department", "menu.system.dict", "menu.system.dyn",
		"menu.system.config", "menu.system.job", "menu.system.smsTemplate",
	}
	got := userAdminSystemMenus(all)

	for _, c := range []string{"menu.system.config", "menu.system.job", "menu.system.smsTemplate"} {
		assert.NotContains(t, got, c,
			"规则层就不能含这三个 —— 靠事后撤权来补救是本末倒置")
	}
	for _, c := range []string{
		"menu.system", "menu.system.user", "menu.system.role", "menu.system.permission",
		"menu.system.department", "menu.system.dict", "menu.system.dyn",
	} {
		assert.Contains(t, got, c)
	}
}

// TestRun_平台运营员不拿没有接口权的死菜单
//
// ⚠️ **"菜单可见"与"接口有权"是两份独立的授权列表** ——
// 菜单给了但 API 码没给,症状是侧边栏点得进去、一进去就 403,
// 而且**开发时很难注意到**(菜单能看见 ⇒ 以为功能是通的)。
//
// ⚠️ **这三条死菜单的来路** ——
// bindingRules 里写的是 `filter(menuCodes, "menu.system", ...)`,
// 而 filter 是**前缀**匹配 ⇒ "menu.system" 把 menu.system.* 全家桶都捞进来了,
// 与同一行注释声明的意图("系统配置菜单单独移除")正好相反。
//
// ⚠️ **menu.system.dyn 必须保住** ——
// 它是被 "menu.system" 这个前缀**顺带**捞进来的,不是显式列的;
// 换成精确匹配后不显式列就会掉(而 M151 明确给了用户管理员 dyn:design:*)。
// ⇒ 这条断言同时守着"该拿的还在"和"不该拿的没了"两侧。
func TestRun_平台运营员不拿没有接口权的死菜单(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	codes := roleMenuCodes(t, db, model.RolePlatformOps)

	// 不该有的:这三条对应的 API 码平台运营员一个都没有
	for _, c := range []string{"menu.system.config", "menu.system.job", "menu.system.smsTemplate"} {
		assert.NotContains(t, codes, c,
			"平台运营员没有对应的 API 码,菜单留着就是必然 403 的死入口")
	}

	// 该有的:system 域里他真正有权的那几条
	for _, c := range []string{
		"menu.system", "menu.system.user", "menu.system.role", "menu.system.permission",
		"menu.system.department", "menu.system.dict", "menu.system.dyn",
	} {
		assert.Contains(t, codes, c, "平台运营员该有的 system 菜单不能被收窄误伤")
	}
}

// roleMenuCodes 取某个角色绑定的全部 menu 资源 code(已排序)
func roleMenuCodes(t *testing.T, db *gorm.DB, roleCode string) []string {
	t.Helper()
	var role model.SysRole
	require.NoError(t, db.Where("code = ?", roleCode).First(&role).Error)

	var bindings []model.SysRoleResource
	require.NoError(t, db.Joins("INNER JOIN sys_resource r ON r.id = sys_role_resource.resource_id").
		Where("sys_role_resource.role_id = ? AND r.type = ?", role.ID, "menu").
		Find(&bindings).Error)

	ids := make([]uint64, 0, len(bindings))
	for _, b := range bindings {
		ids = append(ids, b.ResourceID)
	}
	if len(ids) == 0 {
		return nil
	}
	var resources []model.SysResource
	require.NoError(t, db.Where("id IN ?", ids).Find(&resources).Error)
	codes := make([]string, 0, len(resources))
	for _, r := range resources {
		codes = append(codes, r.Code)
	}
	sort.Strings(codes)
	return codes
}

// TestRun_BindsFileMenuByPermission M82:菜单可见性必须跟着 file:* / folder:* 走
//
// 为什么单独钉:菜单和权限码是两份独立的授权列表,写错的表现是
// 「菜单点得进去但一进去全 403」,或者反过来「页面能看但侧边栏没入口」。
// 两种都很难在开发时注意到 —— 所以把"哪个角色能看到哪一层"写成断言。
func TestRun_BindsFileMenuByPermission(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	t.Run("只读员靠 filterByRead 命中 file:list / file:read / folder:list,文件三层可见", func(t *testing.T) {
		codes := roleMenuCodes(t, db, model.RoleReadOnly)
		assert.Contains(t, codes, "menu.file")
		assert.Contains(t, codes, "menu.file.list")
		// filterByRead 给了 folder:list(M170),文件夹页(只需 folder:list)合法可见
		assert.Contains(t, codes, "menu.file.folder")
	})

	t.Run("超管三层全拿", func(t *testing.T) {
		codes := roleMenuCodes(t, db, model.RoleSuperAdmin)
		assert.Contains(t, codes, "menu.file")
		assert.Contains(t, codes, "menu.file.list")
		assert.Contains(t, codes, "menu.file.folder")
	})

	t.Run("平台运营员有 file:* + folder:*,三层全拿", func(t *testing.T) {
		codes := roleMenuCodes(t, db, model.RolePlatformOps)
		assert.Contains(t, codes, "menu.file")
		assert.Contains(t, codes, "menu.file.list")
		assert.Contains(t, codes, "menu.file.folder")
	})
}

// roleResourceCodes 取某个角色绑定的全部资源 code(api + button + menu,已排序)
//
// ⚠️ **不过滤 type** —— 这个 helper 是给"这个角色到底拿到了哪些码"用的。
// 分两次查(type=api / type=button)会漏掉"某个码的 type 被改了"这种情况,
// 而 type 改错恰恰是"按钮该显示不显示 / 不该显示却显示"的一类根因。
func roleResourceCodes(t *testing.T, db *gorm.DB, roleCode string) []string {
	t.Helper()
	var role model.SysRole
	require.NoError(t, db.Where("code = ?", roleCode).First(&role).Error)

	var bindings []model.SysRoleResource
	require.NoError(t, db.Joins("INNER JOIN sys_resource r ON r.id = sys_role_resource.resource_id").
		Where("sys_role_resource.role_id = ?", role.ID).
		Find(&bindings).Error)

	if len(bindings) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(bindings))
	for _, b := range bindings {
		ids = append(ids, b.ResourceID)
	}
	var resources []model.SysResource
	require.NoError(t, db.Where("id IN ?", ids).Find(&resources).Error)
	codes := make([]string, 0, len(resources))
	for _, r := range resources {
		codes = append(codes, r.Code)
	}
	sort.Strings(codes)
	return codes
}

// TestRun_BindsDictListToAllRoles M93:字典只读权限必须给到**每一个**登录角色
//
// # 这个洞长什么样
//
// 字典是"代码 enum 的中央仓库",业务页的下拉 / 标签全靠它渲染
// (web/src/hooks/useDict.ts → GET /system/dicts?groupCode=xxx)。
// 而 `GET /system/dicts` 挂的是 `rbac("dict", "list")` ——
// `dict:list` 此前**只绑给了 super_admin 和 user_admin**,
// **10 个角色一个都没有**(user_admin 靠 `filter(resources, "dict:")` 顺带拿到,
// 只读员靠 `filterByRead` 按 ":list" 后缀意外捞到)。
//
// 症状:物业工作人员打开工单页 → 状态下拉**全空**(options = [])+
// 每开一页弹一次"没有权限"(403 → web/src/api/client.ts 的 message.error)。
// **超管看不出来** —— 开发期几乎只用超管登录,这条洞整整躲了 8 天。
//
// # 为什么只给 list
//
// 字典是只读契约:写通道(create/update/delete)仍然只有用户管理员 / 超管。
// 给 list 不构成越权 —— 那是一行 label 文本,任何登录用户都要用它渲染下拉。
//
// # 为什么必须写成测试而不是只改 seed
//
// `filterByRead` 按 ":list" 后缀捞,所以**只读员恰好有** dict:list ——
// 一个"按后缀猜出来的"权限,恰好掩盖了"按用途声明的"权限缺口。
// 只靠人读 seed 表格,下次有人重构 filterByRead 或调整 authBase,
// 同样的洞会再开一次,而且照样没人发现。
func TestRun_BindsDictListToAllRoles(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	t.Run("全部内置角色都能读字典", func(t *testing.T) {
		for _, r := range roleList() {
			codes := roleResourceCodes(t, db, r.Code)
			assert.Contains(t, codes, "dict:list",
				"角色 %s(%s)必须有 dict:list —— 否则业务页的字典下拉全空 + 每开一页弹一次 403",
				r.Code, r.Name)
		}
	})

	t.Run("字典写通道只归超管 / 平台运营员", func(t *testing.T) {
		for _, r := range roleList() {
			codes := roleResourceCodes(t, db, r.Code)
			for _, w := range []string{"dict:create", "dict:update", "dict:delete"} {
				if r.Code == model.RoleSuperAdmin || r.Code == model.RolePlatformOps {
					assert.Contains(t, codes, w, "%s 应有 %s", r.Code, w)
					continue
				}
				assert.NotContains(t, codes, w,
					"角色 %s 不该有 %s —— 字典写通道只归平台运营员 / 超管", r.Code, w)
			}
		}
	})
}

// TestRun_BindsAuthBaseToAllRoles C5:自服务资源码必须给到**每一个**登录角色
//
// # 守的是什么
//
// `authBaseCodes`(seed.go 里那份清单)是"每个登录角色都该有的自服务权限"那一档:
// 改自己 / 切身份 / 看自己的设备 / 换绑手机号 / 看自己的站内信 / 读字典。
//
// 它**不是**"给几个角色顺手加的" —— 少一个,那个角色就会在自己的个人中心里
// 撞 403(前端弹"没有权限"),而**开发期几乎只用超管登录,这条洞躲好几天没人发现**
// (M93 的 `dict:list` 就是这么躲了 8 天的)。
//
// # 为什么还要有一份"抄一遍"的清单
//
// ①② 两条都是**拿实现去查实现**:遍历 authBaseCodes、查它有没有绑到角色。
// 如果有人把某个码**从 authBaseCodes 里删掉**,①② 会一起"看不见"它 ——
// 清单和断言来自同一处,一起少了就**全绿**,而线上那个角色已经少了一项能力。
//
// ⇒ 这里再抄一份**期望清单**逐项对(③)。测试里抄一遍不是 redundancy,
// 是"契约 vs 实现"这一对:实现改了而契约没改 ⇒ ③ 红。
//
// # 为什么还要查"资源池里存在这个码"
//
// 路由上的 `rbac("auth", "bind-phone")` 引用的码如果没进资源池,
// **启动时**就会被 `internal/api/router/consistency.go` 拦下(整个服务起不来)。
// 那条检查要连真库跑,单测里跑不了 ⇒ 这里补一条离线的。
func TestRun_BindsAuthBaseToAllRoles(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	// ③ 期望清单(抄一份)—— 少一个码就红
	expected := []string{
		"auth:me", "auth:logout",
		"auth:update-profile", "auth:change-password", "auth:reset-password",
		"auth:switch-identity",
		"auth:account-sms",
		"auth:bind-phone",
		"auth:sessions", "auth:revoke-sessions",
		"inbox:list", "inbox:read", "inbox:mark_read",
		"dict:list",
		// ⚠️ M147:拉自己的权限清单(前端菜单/按钮全靠它)
		"resource:accessible",
		// ⚠️ 2026-11-01:打开工作台(看板的 4 个接口)
		//
		//	少了它 ⇒ **9/13 的角色点开工作台就是 4 个 403**
		//	(菜单 menu.dashboard 绑给所有人,接口却只有 4 个角色能调:
		//	 super_admin / role_readonly / platform_ops_lead / platform_ops)。
		//	看板的**内容**已经按域权限过滤了,所以给这个码不放大数据面。
		"dashboard:read",
	}
	assert.ElementsMatch(t, expected, authBaseCodes,
		"authBaseCodes 的内容变了 —— 少一个码,那一项能力就对某类角色消失了(且下面两条都发现不了)")

	// ① 资源池里必须真的有这些码(否则 rbac() 引用不到 → 启动一致性检查失败)
	var pool []string
	require.NoError(t, db.Model(&model.SysResource{}).Pluck("code", &pool).Error)
	inPool := make(map[string]bool, len(pool))
	for _, c := range pool {
		inPool[c] = true
	}
	for _, c := range authBaseCodes {
		assert.True(t, inPool[c],
			"资源池里必须有 %q —— 路由上 rbac() 引用的码不在池里,启动时会被 consistency 检查拦下", c)
	}

	// ② 每个码都必须绑到每一个角色
	for _, r := range roleList() {
		codes := roleResourceCodes(t, db, r.Code)
		for _, c := range authBaseCodes {
			assert.Contains(t, codes, c,
				"角色 %s(%s)必须有 %q —— 少一个,那个角色在自己的个人中心里就撞 403", r.Code, r.Name, c)
		}
	}
}

// TestRun_BindsAuthBaseToNonBuiltinRoles M147:自服务能力要覆盖**非内置**角色
//
// # 守的是什么
//
// 上面那条只查 `roleList()`(13 个**内置**角色)。但业务上还会自建角色
// (走「角色管理」页建的,is_builtin=false)—— 它们不在 roleList() 里,
// seed 的 bindingRules 也不覆盖它们。
//
// M147 把通配策略行删掉之后,自建角色的身份**整个不可用**:
// 切过去 → `/auth/me` 就 403 → 前端拿不到用户信息 → 后台全空。
// (M147 之前靠通配行兜着,所以这条洞一直没人发现。)
//
// ⇒ seed 这一段要把自服务码补到**所有**角色上(只补缺,不删已有)。
//
// # 为什么这里要单独一条
//
// 运行期新建的角色靠 service 层建角色时直接绑
// (internal/service/role_impl.go 的 bindAuthBaseResources)——
// **存量**角色(建好的时候还没有那条规则)只能靠 seed 这一段补,
// 两边都要,只做一边就留下时间窗。
func TestRun_BindsAuthBaseToNonBuiltinRoles(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")

	// 先塞一个"业务自建角色"(seed 不认识它)
	require.NoError(t, db.Create(&model.SysRole{
		BaseModel: model.BaseModel{ID: 99000000000000001},
		Code:      "role_biz_runtime",
		Name:      "业务自建角色",
		Type:      "business",
		Sort:      999,
		IsBuiltin: false,
		IsActive:  true,
	}).Error)

	runSeedForTest(t, db)

	codes := roleResourceCodes(t, db, "role_biz_runtime")
	for _, c := range authBaseCodes {
		assert.Contains(t, codes, c,
			"非内置角色 %q 也必须有 %q —— M147 删掉通配策略行之后,少了它这个角色的身份整个不可用", "role_biz_runtime", c)
	}
}

// =============================================================================
// 字典 seed 测试(2026-10-18 L5 新增)
//   约束:必须字典组全部 seed,缺一即 fail
// =============================================================================

// TestSeedAllDicts_AllMustDictsPresent Run() 后 mustDicts 全部有 seed
func TestSeedAllDicts_AllMustDictsPresent(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	for _, gc := range mustDicts {
		var n int64
		require.NoError(t, db.Model(&model.SysDict{}).
			Where("group_code = ? AND is_active = ?", gc, true).
			Count(&n).Error)
		assert.Greater(t, n, int64(0),
			"mustDicts[%s] 应至少有 1 条 seed", gc)
	}
}

// TestSeedDictGuard_OK 完整 seed 后 SeedDictGuard 应通过
func TestSeedDictGuard_OK(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)
	require.NoError(t, SeedDictGuard(db))
}

// TestSeedDictGuard_FailOnMissingGroup 删一条字典后 SeedDictGuard 应 fail
//
// 这是核心反向测试:任何"误删字典 seed"或"新加 mustDicts 项但忘记 seedAllDicts"
//
//	都会被这个测试抓住
func TestSeedDictGuard_FailOnMissingGroup(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	// 删掉 user_status 全部 seed
	require.NoError(t, db.Where("group_code = ?", "user_status").
		Delete(&model.SysDict{}).Error)

	err := SeedDictGuard(db)
	assert.Error(t, err, "user_status 缺 seed 时 SeedDictGuard 必须失败")
	assert.Contains(t, err.Error(), "user_status",
		"错误信息应明确指出哪个 group_code 缺 seed")
}

// TestSeedAllDicts_IsIdempotent 重复 Run() 字典不重复、不丢失
func TestSeedAllDicts_IsIdempotent(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)
	runSeedForTest(t, db)
	runSeedForTest(t, db)

	for _, gc := range mustDicts {
		var n int64
		require.NoError(t, db.Model(&model.SysDict{}).
			Where("group_code = ? AND is_active = ?", gc, true).
			Count(&n).Error)
		// 与 seedAllDicts 中该 group 的项数严格一致(不重复)
		expected := dictItemCountByGroup(gc)
		assert.Equal(t, int64(expected), n,
			"%s Run 三次后应仍为 %d 条(不重复)", gc, expected)
	}
}

// =============================================================================
// 字典 seed 的"只写真正变了的"契约(M138,2026-10-31)
// =============================================================================

// TestSeedAllDicts_SecondRunWritesNothing 第二次 seed **一行都不该再改**
//
// ⚠️ **这条守的是性能改写,不是业务语义**:
//
//	M138 把字典 seed 从"逐条 SELECT + 无条件 UPDATE"改成
//	「一次查全表 + **只写真正变了的**」。
//	差别就在 `updated_at`:无条件 UPDATE 会把**每一行**的时间戳都刷新一遍,
//	于是"这条字典是什么时候被改过的"永远是**最近一次启动的时刻** ——
//	这个字段就失去了意义(而它在审计页上是给人看的)。
//
//	判据用 `updated_at`:GORM 的 `Updates(map)` 会自动带上 `updated_at`
//	(已用探针验过:Updates 确实会刷新它)⇒ **内容没变 ⇒ 时间戳不动**。
//
//	⚠️ **如果哪天有人改回无条件 UPDATE,这条会红** —— 那正是我们要的:
//	性能改写被改回去时,这里第一时间报警,而不是让启动又慢 0.7 秒。
func TestSeedAllDicts_SecondRunWritesNothing(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	var before []model.SysDict
	require.NoError(t, db.Find(&before).Error)
	require.NotEmpty(t, before)

	runSeedForTest(t, db)

	var after []model.SysDict
	require.NoError(t, db.Find(&after).Error)
	require.Len(t, after, len(before), "第二次 Run 不该增删行")

	for i := range before {
		assert.Equal(t, before[i].UpdatedAt, after[i].UpdatedAt,
			"字典 %s/%s 内容没变,updated_at 不该被刷新"+
				"(第二次 Run 还在无条件 UPDATE ⇒ 「只写真正变了的」被改回去了)",
			before[i].GroupCode, before[i].ItemKey)
	}
}

// TestSeedAllDicts_RestoresTamperedRow 被改歪的那一行,再 seed 必须被拉回来
//
// ⚠️ **为什么必须单独测这条**:稳态下字典"没有变化" ⇒ **update 路径根本不会执行**
//
//	(上面那条用例只跑了 insert 路径 + 全跳过路径)。
//	⇒ "只写真正变了的"里的"**变了的**"这一半,没有这条用例就是**零覆盖**。
//
//	这里模拟运营在字典管理页改了 label / sort / is_active
//	(那个页面的编辑模式是允许改 label/sort/is_active/description 的),
//	然后再 Run() ⇒ seed 必须把它拉回 seed 里的值。
func TestSeedAllDicts_RestoresTamperedRow(t *testing.T) {
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	runSeedForTest(t, db)

	// 找到 user_status/active 这一行,记下 seed 给的期望值
	var row model.SysDict
	require.NoError(t, db.Where("group_code = ? AND item_key = ?", "user_status", "active").
		First(&row).Error)
	wantLabel, wantSort, wantDesc := row.Label, row.Sort, row.Description

	// 把它改歪
	require.NoError(t, db.Model(&row).Updates(map[string]any{
		"label":       "被运营改坏了",
		"sort":        99,
		"description": "被改坏了",
		"is_active":   false,
	}).Error)

	// 别的行的时间戳(用来证明"只写真正变了的")
	var others []model.SysDict
	require.NoError(t, db.Where("NOT (group_code = ? AND item_key = ?)", "user_status", "active").
		Find(&others).Error)
	otherStamps := make(map[uint64]time.Time, len(others))
	for _, o := range others {
		otherStamps[o.ID] = o.UpdatedAt
	}

	runSeedForTest(t, db)

	var after model.SysDict
	require.NoError(t, db.Where("group_code = ? AND item_key = ?", "user_status", "active").
		First(&after).Error)
	assert.Equal(t, wantLabel, after.Label, "label 必须被 seed 拉回")
	assert.Equal(t, wantSort, after.Sort, "sort 必须被 seed 拉回")
	assert.Equal(t, wantDesc, after.Description, "description 必须被 seed 拉回")
	assert.True(t, after.IsActive, "is_active 必须被 seed 拉回 true")

	// ⚠️ **别的行一条都不该被写**(这才是"只写真正变了的"的另一半)
	var othersAfter []model.SysDict
	require.NoError(t, db.Where("NOT (group_code = ? AND item_key = ?)", "user_status", "active").
		Find(&othersAfter).Error)
	for _, o := range othersAfter {
		assert.Equal(t, otherStamps[o.ID], o.UpdatedAt,
			"字典 %s/%s 内容没变,不该被写(只有被改歪的那一行该动)", o.GroupCode, o.ItemKey)
	}
}

// dictItemCountByGroup helper:从 seedAllDicts 静态统计每个 groupCode 的项数
//
//	设计:与 seedAllDicts 数组硬一致,若不匹配测试就红
//	替代方案是用反射统计 dictItem 列表,但本项目以硬编码 + 测试看护更直接
//
//	2026-10-18 晚扩 P2 14 组:本函数硬一致
func dictItemCountByGroup(gc string) int {
	counts := map[string]int{
		// P0 + P1(原 15 组)
		"user_status":            3,
		"role_type":              2,
		"owner_audit_status":     3,
		"owner_binding_status":   4,
		"material_status":        7, // + rejected/archived(2026-10-23 跟 M83-b 补的枚举同步)
		"notice_type":            3,
		"notice_status":          4,
		"notice_priority":        4,
		"issue_status":           3,
		"issue_type":             3,
		"issue_priority":         4,
		"audit_op_status":        2,
		"audit_change_operation": 3, // 2026-10-30 审计中心"数据变更"页新增
		"sys_log_level":          3, // 2026-11-03 审计中心「系统日志」页(info/warn/error)
		"config_change_status":   5, // + expired(2026-10-18)
		"config_value_type":      7, // + cert_pem + private_key_pem(2026-10-18 晚)
		"config_effective_type":  3,
		// P2 通用
		"common_status": 3,
		// P2 房源/合同
		"house_status":    4,
		"contract_status": 5,
		// P2 交付
		"delivery_status":       3,
		"delivery_issue_status": 6,
		"delivery_stage":        7,
		// P2 活动
		"activity_status":        5,
		"activity_signup_status": 5,
		// P2 反馈/指南
		"feedback_status": 3,
		"guide_status":    3,
		// P2 设备
		"device_online_status": 4,
		"device_bind_status":   3,
		// P2 微信模板
		"wxtemplate_status": 3,
		// M115 短信模板审核状态(与 pkg/smstpl.Status 的 7 个字面量一一对应)
		"sms_template_status": 7,
		// 2026-10-18 起 seed 新增、但本表漏登记的两组。
		// 漏登记时 expected 落到 map 零值 0,断言会以 "期望 0 条" 的形式炸红,
		// 掩盖真正的问题 —— 保持与 seedAllDicts 硬一致。
		"resource_type":   3, // menu / button / api
		"config_ui_label": 1, // config.value
	}
	return counts[gc]
}
