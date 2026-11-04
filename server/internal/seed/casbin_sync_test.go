// M146a —— casbin 策略同步(SQLite 也必须同步)
//
// # 这个文件在防什么
//
// syncCasbinPolicies 原来对 SQLite **直接跳过**("adapter SavePolicy 会卡住"),
// 于是 dev 库里的 casbin_rule **永远是空的**。
// 空策略表 ⇒ 除超管(RBAC 中间件直���)外的所有角色**一律 403**。
//
// 表现是「右上角切身份 → 切完就退出」:
// 切到任何非超管身份 → `/auth/me` 403 → 前端 PermissionLoader 把 403
// 当成"登录态失效"清掉会话 → 跳登录页。
//
// 所以这里守两件事:
//  1. 策略**内容**与驱动无关(buildRolePolicies 是唯一来源)
//  2. 写进去的行**真能被 adapter 读回来并生效**(列名对不上是最阴的那种错)
package seed

import (
	"path/filepath"
	"testing"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// casbinModelPath 定位仓库里的 casbin 模型文件
func casbinModelPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "configs", "casbin_rbac.conf"))
	require.NoError(t, err)
	return p
}

// newCasbinSyncDB 装一张"有角色 + 有资源 + 有绑定"的库
func newCasbinSyncDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysRole{},
		&model.SysResource{},
		&model.SysRoleResource{},
	))
	// 角色
	require.NoError(t, db.Create(&model.SysRole{
		BaseModel: model.BaseModel{ID: 1}, Code: model.RoleSuperAdmin, Name: "超级管理员",
	}).Error)
	require.NoError(t, db.Create(&model.SysRole{
		BaseModel: model.BaseModel{ID: 2}, Code: model.RoleReadOnly, Name: "只读查看员",
	}).Error)
	// 资源
	require.NoError(t, db.Create(&model.SysResource{
		BaseModel: model.BaseModel{ID: 10}, Code: "auth:me", Name: "当前用户",
	}).Error)
	require.NoError(t, db.Create(&model.SysResource{
		BaseModel: model.BaseModel{ID: 11}, Code: "user:list", Name: "用户列表",
	}).Error)
	// 绑定:只读查看员拿 auth:me + user:list
	require.NoError(t, db.Create(&model.SysRoleResource{
		BaseModel: model.BaseModel{ID: 100}, RoleID: 2, ResourceID: 10,
	}).Error)
	require.NoError(t, db.Create(&model.SysRoleResource{
		BaseModel: model.BaseModel{ID: 101}, RoleID: 2, ResourceID: 11,
	}).Error)
	return db
}

// TestBuildRolePolicies_只按绑定生成 + 没有通配行
//
// ⚠️ **策略内容与驱动无关** —— SQLite 与 MySQL 两条路都调这个函数,
// dev 与生产不允许有策略差异(那正是 M146a 要消灭的洞)。
//
// # 🔄 2026-11-01(M147):这条的语义变了
//
// 老实现给**每个角色**加一条 `role:<code>, global, *, *, allow`,
// 本用例原来断言的就是"13 个角色 → 13 条通配"。
// **那个断言把 bug 写进了测试** —— 通配行让 casbin 的 keyMatch2(r.obj,"*") 恒真,
// 于是**任何角色都能访问任何接口**,13×394 的绑定矩阵在强制执行层是空壳。
//
// 现在:**策略行只能来自绑定**。一条绑定一行,obj 是资源码,act 是 "*"。
func TestBuildRolePolicies_BindingsOnlyNoWildcard(t *testing.T) {
	db := newCasbinSyncDB(t)
	policies := buildRolePolicies(db)

	// 通配行必须**一条都没有**
	for _, p := range policies {
		require.Len(t, p, 5, "策略行必须是 sub/dom/obj/act/eft 五段")
		require.NotEqual(t, "*", p[2],
			"不能有通配行 —— keyMatch2(r.obj,\"*\") 恒真,一行就等于「所有角色都能访问一切」")
		require.Equal(t, "*", p[3], "策略行的 act 恒为 \"*\"(权限身份是整个资源码)")
		require.Equal(t, "allow", p[4], "绑定行只产生 allow")
	}

	// 两条绑定 ⇒ 两条资源策略
	found := map[string]bool{}
	for _, p := range policies {
		if p[2] == "auth:me" {
			found["auth:me"] = true
		}
		if p[2] == "user:list" {
			found["user:list"] = true
		}
	}
	require.True(t, found["auth:me"], "auth:me 的绑定必须生成策略")
	require.True(t, found["user:list"], "user:list 的绑定必须生成策略")

	// 策略行数 == 绑定数(这个库里正好两条绑定)
	require.Len(t, policies, 2, "策略行必须**只**来自绑定 —— 多出来的就是通配行")
}

// TestBuildRolePolicies_查不到的绑定跳过
//
// ⚠️ 绑定了不存在的 role / resource 时**跳过**,不生成半截策略行
// (半截行会让 casbin 匹配到空 obj,等于放行一切)。
func TestBuildRolePolicies_SkipsDanglingBindings(t *testing.T) {
	db := newCasbinSyncDB(t)
	require.NoError(t, db.Create(&model.SysRoleResource{
		BaseModel: model.BaseModel{ID: 102}, RoleID: 999, ResourceID: 999,
	}).Error)

	for _, p := range buildRolePolicies(db) {
		require.NotEqual(t, "", p[0])
		require.NotEqual(t, "", p[2], "obj 为空的策略行会让 casbin 匹配到一切")
	}
}

// TestWritePoliciesDirect_RowsAreLoadableAndEnforceable ★ 回归线
//
// ⚠️ **这条是 M146a 的回归守卫**:写进去的行必须**真能被 adapter 读回来**。
// 只断言"行数对"是不够的 —— 列名对不上时行数照样对,
// 但 adapter 读出来全是空串,策略"有 N 条"却一条都不生效,而且**不报错**。
//
// 这里用**真的 adapter + 真的 enforcer** 读回同一个库,
// 然后用**真的请求**去 enforce,断言非超管角色拿得到 auth:me。
func TestWritePoliciesDirect_RowsAreLoadableAndEnforceable(t *testing.T) {
	db := newCasbinSyncDB(t)
	policies := buildRolePolicies(db)
	require.NoError(t, writePoliciesDirect(db, policies))

	// 行确实在库里
	var rows []gormadapter.CasbinRule
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, len(policies), "写进去的行数必须与策略数一致")

	// ⚠️ **用真的 adapter 读回来** —— 这是关键的一步
	adapter, err := gormadapter.NewAdapterByDB(db)
	require.NoError(t, err)
	enf, err := casbin.NewEnforcer(casbinModelPath(t), adapter)
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	// 非超管角色能访问 auth:me(这正是"切身份后 /auth/me 403"的反面)
	//
	// ⚠️ M147:obj 传**完整资源码**("auth:me"),不是模块名("auth")——
	//   这是生产口径(rbac("auth","me") → r.obj = "auth:me")。
	ok, rules, err := enf.EnforceEx("role:"+model.RoleReadOnly, "global", "auth:me", "me")
	require.NoError(t, err)
	require.True(t, ok, "只读查看员必须能访问 auth:me —— 切身份后前端就是靠它拿当前身份(实际规则:%v)", rules)

	// 没绑的码必须被拒(这正是 M147 修的那个洞)
	ok, _, err = enf.EnforceEx("role:"+model.RoleReadOnly, "global", "user:delete", "delete")
	require.NoError(t, err)
	require.False(t, ok, "只读查看员在这个夹具里**没绑** user:delete ⇒ 必须拒绝")
}

// TestWritePoliciesDirect_ReplacesStaleRows
//
// ⚠️ **旧行必须被清掉** —— 角色被删 / 资源被改名之后,
// 老策略留在库里继续放行(这是"改了权限却不生效"的经典来源)。
func TestWritePoliciesDirect_ReplacesStaleRows(t *testing.T) {
	db := newCasbinSyncDB(t)

	// 先塞一条陈旧的策略:一个已经不存在的角色拿到了 user:list
	require.NoError(t, writePoliciesDirect(db, [][]string{
		{"role:role_gone", "global", "user:list", "*", "allow"},
	}))

	// 再跑一次正常同步
	require.NoError(t, writePoliciesDirect(db, buildRolePolicies(db)))

	adapter, err := gormadapter.NewAdapterByDB(db)
	require.NoError(t, err)
	enf, err := casbin.NewEnforcer(casbinModelPath(t), adapter)
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	ok, _, err := enf.EnforceEx("role:role_gone", "global", "user:list", "*")
	require.NoError(t, err)
	require.False(t, ok, "陈旧策略必须随同步一起消失")
}
