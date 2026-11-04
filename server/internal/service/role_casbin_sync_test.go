// 角色绑定 → casbin 策略行的**运行时同步**(M147-f)
//
// # 这张网守的是什么
//
// casbin_rule 是「角色-资源绑定矩阵」的**物化副本** —— 只在 seed/cmd/migrate
// 时被整体重建一次。运行时管理员在界面上做的三件事:
//
//	建角色(CreateRole) / 改权限分配(AssignResources) / 删角色(DeleteRole)
//
// 改的是 sys_role / sys_role_resource,**casbin_rule 不会跟着动** ⇒
//
//	前端(按绑定表显示):菜单出来了、权限勾上了
//	后端(按 casbin 判)  :一律 403
//
// 而且**没有任何报错** —— casbin 只是"没匹配到策略行",正常返 false。
//
// # 期望值都是手抄的
//
// 不抄实现里的那份清单(§M103 的教训:测试用被测常量构造输入 = 恒绿)。
// 这里手写 "auth:me" / "resource:accessible" / "dict:list" 三个码,
// 实现改了而契约没改 ⇒ 红。
//
// # 口径统一(顺带守的)
//
// 停用的资源(is_active=false)**不该继续放行** ——
// 三处口径必须一致:seed 整体重建 / 运行时同步 / AccessibleResources(前端来源)。
package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// newRoleSyncDB 单连接内存库
//
// ⚠️ 单连接是必须的:事务里还要建 casbin_rule 表,多连接时各看各的
// 内存库 ⇒ "table not found"。
func newRoleSyncDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.SysRole{}, &model.SysResource{},
		&model.SysRoleResource{}, &model.SysUserIdentity{}))
	return db
}

// seedSyncRes 造一条资源(is_active 可控)
//
// ⚠️ **停用那条必须再 UPDATE 一次** —— SysResource.IsActive 的 gorm tag 是
// `default:true`,而 false 是零值 ⇒ GORM 建行时**把这个字段省掉**,
// 让数据库填默认值 true ⇒ "停用的资源"造出来其实还是启用的。
// (这是 GORM default tag 的经典坑:零值 + 有默认值 = 字段被省略。)
func seedSyncRes(t *testing.T, db *gorm.DB, id uint64, code string, active bool) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysResource{
		BaseModel: model.BaseModel{ID: id},
		Code:      code,
		Name:      code,
		Type:      "api",
		IsActive:  active,
	}).Error)
	if !active {
		require.NoError(t, db.Model(&model.SysResource{}).
			Where("id = ?", id).Update("is_active", false).Error)
	}
}

// casbinObjs 取某角色在 casbin_rule 里的 obj 列(资源码)
func casbinObjs(t *testing.T, db *gorm.DB, roleCode string) []string {
	t.Helper()
	var objs []string
	require.NoError(t, db.Table("casbin_rule").
		Where("ptype = ? AND v0 = ?", "p", "role:"+roleCode).
		Pluck("v2", &objs).Error)
	return objs
}

// ============ ① 建角色 ============

// 新建角色 → casbin 里必须马上有它的策略行
func TestCreateRole_SyncsCasbinPolicy(t *testing.T) {
	db := newRoleSyncDB(t)
	seedSyncRes(t, db, 92000000000000101, "auth:me", true)
	seedSyncRes(t, db, 92000000000000102, "resource:accessible", true)
	seedSyncRes(t, db, 92000000000000103, "dict:list", true)
	svc := NewRoleService(db, nil, nil)

	_, err := svc.CreateRole(context.Background(), &Caller{UserID: 1},
		&model.SysRole{Code: "role_sync_new", Name: "同步角色"})
	require.NoError(t, err)

	require.ElementsMatch(t,
		[]string{"auth:me", "resource:accessible", "dict:list"},
		casbinObjs(t, db, "role_sync_new"),
		"建角色后 casbin 里必须有对应的策略行 —— 没有的话这个角色在强制执行层是零权限")
}

// ============ ② 分配权限 ============

// 分配权限 → casbin 跟着变(加进来的有、被收回的没了)
func TestAssignResources_SyncsCasbinPolicy(t *testing.T) {
	db := newRoleSyncDB(t)
	seedSyncRes(t, db, 92000000000000201, "auth:me", true)
	seedSyncRes(t, db, 92000000000000202, "resource:accessible", true)
	seedSyncRes(t, db, 92000000000000203, "dict:list", true)
	seedSyncRes(t, db, 92000000000000204, "user:list", true)
	svc := NewRoleService(db, nil, nil)

	roleID, err := svc.CreateRole(context.Background(), &Caller{UserID: 1},
		&model.SysRole{Code: "role_sync_assign", Name: "分配权限角色"})
	require.NoError(t, err)

	require.NoError(t, svc.AssignResources(context.Background(), &Caller{UserID: 1},
		roleID, []uint64{92000000000000204}))
	require.ElementsMatch(t,
		[]string{"auth:me", "resource:accessible", "dict:list", "user:list"},
		casbinObjs(t, db, "role_sync_assign"),
		"分配权限后 casbin 里必须有新勾的资源码")

	// 取消勾选(覆盖式保存)→ user:list 必须从 casbin 里消失
	require.NoError(t, svc.AssignResources(context.Background(), &Caller{UserID: 1},
		roleID, []uint64{}))
	got := casbinObjs(t, db, "role_sync_assign")
	require.NotContains(t, got, "user:list",
		"取消勾选的权限必须从 casbin 里消失 —— 否则界面收回权限、接口还能调")
	require.ElementsMatch(t,
		[]string{"auth:me", "resource:accessible", "dict:list"},
		got)
}

// 停用的资源不给策略行("停用资源"不是死开关)
func TestAssignResources_SkipsInactiveResource(t *testing.T) {
	db := newRoleSyncDB(t)
	seedSyncRes(t, db, 92000000000000301, "auth:me", true)
	seedSyncRes(t, db, 92000000000000302, "resource:accessible", true)
	seedSyncRes(t, db, 92000000000000303, "dict:list", true)
	seedSyncRes(t, db, 92000000000000304, "user:delete", false) // 停用
	svc := NewRoleService(db, nil, nil)

	roleID, err := svc.CreateRole(context.Background(), &Caller{UserID: 1},
		&model.SysRole{Code: "role_sync_inactive", Name: "停用资源角色"})
	require.NoError(t, err)

	require.NoError(t, svc.AssignResources(context.Background(), &Caller{UserID: 1},
		roleID, []uint64{92000000000000304}))

	require.NotContains(t, casbinObjs(t, db, "role_sync_inactive"), "user:delete",
		"停用的资源不该继续放行 —— 前端已经把它从菜单里拿掉了(AccessibleResources 过滤 is_active)")
}

// 停用资源 → 绑着它的角色的策略行要重算(权限收回);重新启用 → 还回去
func TestUpdateResource_IsActiveChangeResyncsRoles(t *testing.T) {
	db := newRoleSyncDB(t)
	seedSyncRes(t, db, 92000000000000401, "auth:me", true)
	seedSyncRes(t, db, 92000000000000402, "resource:accessible", true)
	seedSyncRes(t, db, 92000000000000403, "dict:list", true)
	seedSyncRes(t, db, 92000000000000404, "user:list", true)
	svc := NewRoleService(db, nil, nil)

	roleID, err := svc.CreateRole(context.Background(), &Caller{UserID: 1},
		&model.SysRole{Code: "role_sync_toggle", Name: "停用切换角色"})
	require.NoError(t, err)
	require.NoError(t, svc.AssignResources(context.Background(), &Caller{UserID: 1},
		roleID, []uint64{92000000000000404}))
	require.Contains(t, casbinObjs(t, db, "role_sync_toggle"), "user:list")

	// 停用资源
	inactive := false
	require.NoError(t, svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		92000000000000404, &ResourceUpdateInput{IsActive: &inactive}))
	require.NotContains(t, casbinObjs(t, db, "role_sync_toggle"), "user:list",
		"停用资源必须立刻收回权限 —— 否则'停用'只是个界面上的死开关")

	// 重新启用 → 权限还回去(停用是临时收回,不是解绑)
	active := true
	require.NoError(t, svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		92000000000000404, &ResourceUpdateInput{IsActive: &active}))
	require.Contains(t, casbinObjs(t, db, "role_sync_toggle"), "user:list",
		"重新启用资源要把权限还回去")
}

// ============ ③ 删角色 ============

// 删角色 → 策略行必须一起删(否则同 code 的新角色继承旧权限)
func TestDeleteRole_RemovesCasbinPolicy(t *testing.T) {
	db := newRoleSyncDB(t)
	seedSyncRes(t, db, 92000000000000501, "auth:me", true)
	seedSyncRes(t, db, 92000000000000502, "resource:accessible", true)
	seedSyncRes(t, db, 92000000000000503, "dict:list", true)
	seedSyncRes(t, db, 92000000000000504, "user:list", true)
	svc := NewRoleService(db, nil, nil)

	roleID, err := svc.CreateRole(context.Background(), &Caller{UserID: 1},
		&model.SysRole{Code: "role_sync_del", Name: "待删角色"})
	require.NoError(t, err)
	require.NoError(t, svc.AssignResources(context.Background(), &Caller{UserID: 1},
		roleID, []uint64{92000000000000504}))
	require.NotEmpty(t, casbinObjs(t, db, "role_sync_del"))

	require.NoError(t, svc.DeleteRole(context.Background(), &Caller{UserID: 1}, roleID))
	require.Empty(t, casbinObjs(t, db, "role_sync_del"),
		"角色删了策略行必须一起删 —— 否则同 code 的新角色会继承旧角色的全部权限")
}

// ============ ④ 删掉 → 再建同 code ============

// 软删的角色**仍然占着 code**(uniqueIndex 不看 deleted_at)
// ⇒ "删掉 → 再建同 code"必须能走通(复活那个角色行),而不是 500 + 裸 SQL 错误。
func TestCreateRole_RevivesSoftDeletedRoleWithSameCode(t *testing.T) {
	db := newRoleSyncDB(t)
	seedSyncRes(t, db, 93000000000000101, "auth:me", true)
	seedSyncRes(t, db, 93000000000000102, "resource:accessible", true)
	seedSyncRes(t, db, 93000000000000103, "dict:list", true)
	seedSyncRes(t, db, 93000000000000104, "user:list", true)
	svc := NewRoleService(db, nil, nil)
	ctx := context.Background()

	roleID, err := svc.CreateRole(ctx, &Caller{UserID: 1},
		&model.SysRole{Code: "role_sync_revive", Name: "复活角色"})
	require.NoError(t, err)
	require.NoError(t, svc.AssignResources(ctx, &Caller{UserID: 1},
		roleID, []uint64{93000000000000104}))
	require.NoError(t, svc.DeleteRole(ctx, &Caller{UserID: 1}, roleID))

	// 再建同 code —— 必须成功(复活),不能是 500
	newID, err := svc.CreateRole(ctx, &Caller{UserID: 1},
		&model.SysRole{Code: "role_sync_revive", Name: "复活角色2"})
	require.NoError(t, err, "软删的同 code 角色必须能复活 —— 否则这个 code 再也建不了")
	require.Equal(t, roleID, newID, "复活用的是原来那个角色行(ID 不变)")

	// 复活后的权限是**全新算**的 —— 老角色的绑定在删除时已经硬删
	require.ElementsMatch(t,
		[]string{"auth:me", "resource:accessible", "dict:list"},
		casbinObjs(t, db, "role_sync_revive"),
		"复活角色的权限是全新算的 —— 不能留着老角色的绑定")

	// 复活后角色重新可见(列表走默认 scope,软删的看不见)
	page, err := svc.ListRoles(ctx, RoleListQuery{})
	require.NoError(t, err)
	list, ok := page.List.([]RoleListItem)
	require.True(t, ok, "列表元素类型不对")
	found := false
	for _, r := range list {
		if r.Code == "role_sync_revive" {
			found = true
		}
	}
	require.True(t, found, "复活后的角色必须重新出现在列表里")
}

// 活着的同 code → 409(不是裸 SQL 错误)
func TestCreateRole_RejectsLiveDuplicateCode(t *testing.T) {
	db := newRoleSyncDB(t)
	seedSyncRes(t, db, 93000000000000201, "auth:me", true)
	svc := NewRoleService(db, nil, nil)
	ctx := context.Background()

	_, err := svc.CreateRole(ctx, &Caller{UserID: 1},
		&model.SysRole{Code: "role_sync_dup", Name: "重复角色"})
	require.NoError(t, err)

	_, err = svc.CreateRole(ctx, &Caller{UserID: 1},
		&model.SysRole{Code: "role_sync_dup", Name: "重复角色2"})
	require.Error(t, err, "活着的同 code 必须被拒")
	require.Contains(t, err.Error(), "角色 code 已存在",
		"必须是业务错误(409 + 中文提示),不是裸 SQL 错误")
}
