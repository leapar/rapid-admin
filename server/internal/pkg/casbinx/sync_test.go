// M147-f:运行时策略同步(角色绑定 → casbin_rule → 强制执行层)
//
// # 这张网守的是什么
//
// casbin_rule 是「角色-资源绑定矩阵」的**物化副本** —— 只在 seed/cmd/migrate
// 时被整体重建一次。运行时管理员改绑定(建角色 / 分配权限 / 删角色),
// casbin_rule 不跟着动 ⇒ 后端按 casbin 判、前端按绑定表显示,
// 两套真相分叉(勾了权限却 403),而且**没有任何报错**。
//
// # 这层为什么用「全局 enforcer」测
//
// SyncRolePolicies 只写库;真正生效靠 enforcer 的内存模型 —— 内存模型
// 不会自动读库,必须 Reload。所以这里必须验「写完 + Reload 之后
// Enforce 真的变了」,而不是只验库里多了几行。
//
// ⚠️ enforcer 是**进程级单例**(sync.Once)⇒ 这里用 ResetForTest 隔离,
//
//	测完还原,不许影响同包其它用例。
package casbinx

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newSyncDB 单连接内存库(多连接时各看各的内存库 ⇒ 表"凭空消失")
func newSyncDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	return db
}

// initGlobalEnforcer 用全局 enforcer 跑一遍(测完还原)
func initGlobalEnforcer(t *testing.T, db *gorm.DB) {
	t.Helper()
	ResetForTest()
	t.Cleanup(ResetForTest)
	confPath, err := filepath.Abs("../../../configs/casbin_rbac.conf")
	require.NoError(t, err)
	require.NoError(t, Init(db, confPath))
}

// 同步之后,绑定的资源码必须能过;没绑的必须过不了
func TestSyncRolePolicies_TakesEffectAfterReload(t *testing.T) {
	db := newSyncDB(t)
	initGlobalEnforcer(t, db)

	require.NoError(t, SyncRolePolicies(db, "role_sync_x", []string{"user:list", "user:create"}))

	// ⚠️ 写库 ≠ 生效 —— 内存模型还没刷新,这时 Enforce 必须**还是旧的**
	ok, err := Enforce("role:role_sync_x", "global", "user:list", "list")
	require.NoError(t, err)
	require.False(t, ok, "写完库但没 Reload 时不该生效(内存模型不会自动读库)")

	require.NoError(t, ReloadIfInitialized())

	ok, err = Enforce("role:role_sync_x", "global", "user:list", "list")
	require.NoError(t, err)
	require.True(t, ok, "同步 + Reload 之后绑定的资源码必须能过")

	ok, err = Enforce("role:role_sync_x", "global", "user:delete", "delete")
	require.NoError(t, err)
	require.False(t, ok, "没绑的资源码不能过")
}

// 覆盖式:第二次同步只留一个码,被收回的那个必须**立刻**失效
func TestSyncRolePolicies_OverwritesOldRows(t *testing.T) {
	db := newSyncDB(t)
	initGlobalEnforcer(t, db)

	require.NoError(t, SyncRolePolicies(db, "role_sync_x", []string{"user:list", "user:create"}))
	require.NoError(t, ReloadIfInitialized())

	require.NoError(t, SyncRolePolicies(db, "role_sync_x", []string{"user:list"}))
	require.NoError(t, ReloadIfInitialized())

	ok, err := Enforce("role:role_sync_x", "global", "user:create", "create")
	require.NoError(t, err)
	require.False(t, ok,
		"覆盖后被收回的权限必须立刻失效 —— 否则界面收回权限、接口还能调")

	ok, err = Enforce("role:role_sync_x", "global", "user:list", "list")
	require.NoError(t, err)
	require.True(t, ok, "保留下来的权限必须还在")
}

// 角色删了 → 策略行必须一起删(否则同 code 的新角色继承旧权限)
func TestRemoveRolePolicies_ClearsRole(t *testing.T) {
	db := newSyncDB(t)
	initGlobalEnforcer(t, db)

	require.NoError(t, SyncRolePolicies(db, "role_sync_x", []string{"user:list"}))
	require.NoError(t, ReloadIfInitialized())
	ok, err := Enforce("role:role_sync_x", "global", "user:list", "list")
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, RemoveRolePolicies(db, "role_sync_x"))
	require.NoError(t, ReloadIfInitialized())

	ok, err = Enforce("role:role_sync_x", "global", "user:list", "list")
	require.NoError(t, err)
	require.False(t, ok, "角色删了策略行必须一起删")
}

// 行格式:sub = role:<code>,dom = global,act = *,eft = allow
func TestPolicyRow_Shape(t *testing.T) {
	require.Equal(t,
		[]string{"role:role_house_admin", "global", "user:list", "*", "allow"},
		PolicyRow("role_house_admin", "user:list"),
		"策略行的格式变了 ⇒ 老行还在库里,老角色会突然失权(而且不报错)")
}
