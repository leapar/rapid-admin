// 角色的「自服务能力」绑定(M147)—— 建角色 / 分配权限两个写入口的回归网
//
// # 这张网守的是什么
//
// 「自服务能力」(auth:me / resource:accessible / dict:list …)是**每个登录角色**
// 都必须有的 —— 少了 auth:me,那个身份切过去之后前端连"我是谁"都拿不到
// (整个后台是空的);少了 resource:accessible,菜单和按钮全空。
//
// ⚠️ **M147 之前这条洞被通配策略行盖住了**:seed 给每个角色都发了一条
// `p, role:X, global, *, *, allow`,于是任何角色都能调 `/auth/me`。
// M147 把通配行删掉之后,这个洞才露出来 ——
// 而 seed 只给 **13 个内置角色**绑了自服务码,业务上自建的角色
// (走「角色管理」页建的)一个都没有 ⇒ 那种角色的身份**整个不可用**。
//
// # 为什么期望清单是手抄的
//
// 抄 `seed.AuthBaseResourceCodes()` 的话,实现里少一个码时测试跟着一起少
// ⇒ 恒绿(§M103 的教训:测试用被测常量构造输入 = 恒绿)。
// 这里手抄三个最要紧的码,实现改了而契约没改 ⇒ 红。
//
// # 两个写入口都要守
//
//	① 建角色(CreateRole)—— 新角色一建出来就必须带自服务码
//	② 分配权限(AssignResources)—— 覆盖式保存不许把自服务码抹掉
package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// authBaseExpect 手抄的期望自服务码(不抄实现的那份 —— 见文件头)
var authBaseExpect = []string{"auth:me", "resource:accessible", "dict:list"}

func newRoleAuthBaseDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysRole{}, &model.SysResource{}, &model.SysRoleResource{}))
	return db
}

// 造一条资源
func seedAuthBaseRes(t *testing.T, db *gorm.DB, id uint64, code string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysResource{
		BaseModel: model.BaseModel{ID: id},
		Code:      code,
		Name:      code,
		Type:      "api",
		IsActive:  true,
	}).Error)
}

// 该角色绑到的资源码集合
func roleBoundCodes(t *testing.T, db *gorm.DB, roleID uint64) []string {
	t.Helper()
	var codes []string
	require.NoError(t, db.Table("sys_role_resource").
		Select("sys_resource.code").
		Joins("JOIN sys_resource ON sys_resource.id = sys_role_resource.resource_id").
		Where("sys_role_resource.role_id = ?", roleID).
		Pluck("sys_resource.code", &codes).Error)
	return codes
}

// ============ ① 建角色 ============

// 新建角色必须自带自服务能力
func TestCreateRole_BindsAuthBaseResources(t *testing.T) {
	db := newRoleAuthBaseDB(t)
	seedAuthBaseRes(t, db, 90000000000000101, "auth:me")
	seedAuthBaseRes(t, db, 90000000000000102, "resource:accessible")
	seedAuthBaseRes(t, db, 90000000000000103, "dict:list")
	seedAuthBaseRes(t, db, 90000000000000104, "user:delete")
	svc := NewRoleService(db, nil, nil)

	roleID, err := svc.CreateRole(context.Background(), &Caller{UserID: 1},
		&model.SysRole{Code: "role_biz_new", Name: "业务新角色"})
	require.NoError(t, err)

	codes := roleBoundCodes(t, db, roleID)
	for _, c := range authBaseExpect {
		require.Contains(t, codes, c,
			"新建角色必须自带 %q —— 少了它,这个身份切过去之后前端连'我是谁'都拿不到 / 菜单全空", c)
	}
	require.NotContains(t, codes, "user:delete",
		"自服务码**只**是自服务码 —— 建角色不等于给全部权限")
}

// 资源池里没有自服务码时,建角色不能失败(只是那几项能力没有)
func TestCreateRole_ToleratesMissingAuthBaseResources(t *testing.T) {
	db := newRoleAuthBaseDB(t)
	// 一个资源都不造
	svc := NewRoleService(db, nil, nil)

	roleID, err := svc.CreateRole(context.Background(), &Caller{UserID: 1},
		&model.SysRole{Code: "role_biz_bare", Name: "裸角色"})
	require.NoError(t, err, "资源池里没有自服务码不该让建角色失败")

	codes := roleBoundCodes(t, db, roleID)
	require.Empty(t, codes)
}

// ============ ② 分配权限 ============

// 分配权限是**覆盖式**的 —— 但不许把自服务码抹掉
func TestAssignResources_CannotWipeAuthBase(t *testing.T) {
	db := newRoleAuthBaseDB(t)
	seedAuthBaseRes(t, db, 90000000000000201, "auth:me")
	seedAuthBaseRes(t, db, 90000000000000202, "resource:accessible")
	seedAuthBaseRes(t, db, 90000000000000203, "dict:list")
	seedAuthBaseRes(t, db, 90000000000000204, "user:delete")
	svc := NewRoleService(db, nil, nil)

	roleID, err := svc.CreateRole(context.Background(), &Caller{UserID: 1},
		&model.SysRole{Code: "role_biz_assign", Name: "分配权限角色"})
	require.NoError(t, err)

	// 只勾 user:delete(模拟"管理员把自服务码取消勾选后保存")
	require.NoError(t, svc.AssignResources(context.Background(), &Caller{UserID: 1},
		roleID, []uint64{90000000000000204}))

	codes := roleBoundCodes(t, db, roleID)
	for _, c := range authBaseExpect {
		require.Contains(t, codes, c,
			"分配权限不许把自服务码 %q 抹掉 —— 抹掉之后那个角色的身份整个不可用(/auth/me 就 403)", c)
	}
	require.Contains(t, codes, "user:delete", "正常分配的资源要绑上")
}

// 分配权限仍然能**加**新资源(不能因为要保底就把别的挡掉)
func TestAssignResources_StillAddsRequestedResources(t *testing.T) {
	db := newRoleAuthBaseDB(t)
	seedAuthBaseRes(t, db, 90000000000000301, "auth:me")
	seedAuthBaseRes(t, db, 90000000000000302, "resource:accessible")
	seedAuthBaseRes(t, db, 90000000000000303, "dict:list")
	seedAuthBaseRes(t, db, 90000000000000304, "user:list")
	svc := NewRoleService(db, nil, nil)

	roleID, err := svc.CreateRole(context.Background(), &Caller{UserID: 1},
		&model.SysRole{Code: "role_biz_add", Name: "加资源角色"})
	require.NoError(t, err)

	require.NoError(t, svc.AssignResources(context.Background(), &Caller{UserID: 1},
		roleID, []uint64{90000000000000304}))

	codes := roleBoundCodes(t, db, roleID)
	require.Contains(t, codes, "user:list")
	require.Contains(t, codes, "auth:me")
}
