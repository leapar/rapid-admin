// 权限可见性必须按**当前身份**算 —— 关闭"切了身份,界面还是超管那一套"那个洞。
//
// # 洞是什么
//
// `AccessibleResources`(前端菜单/按钮的来源)以前把**这个用户所有身份**的
// role_code 取并集来算:
//
//	roleSet = {role_super_admin, role_user_admin}
//	isSuper = "并集里有超管" ⇒ true ⇒ 返**全部**资源
//
// ⇒ 多身份用户(超管 + 普通身份)从超管切到普通身份后,菜单**完全不变**
// (超管那一套 45 个菜单全还在),按钮也一样(119 个)。
// 点下去后端才 403 —— 界面显示的和实际能做的对不上。
//
// 同样的错还有两处:`GetCurrentUser` 的资源树(同一个并集算法)、
// `platformFlags`(⇒ `is_platform_super_admin` 切走后仍是 true ——
// 那是**账号属性**,不是"我现在以什么身份行事")。
//
// # 执行层早就是按当前身份的
//
// RBAC 中间件的 `decideAccess` 用的是 `caller.RoleCode`
// (= token 里当前身份的角色),principal loader 也是按 `identityID` 查的。
// ⇒ 只有"显示层"是并集,执行层是当前身份 —— 两套真相。
//
// # 修法
//
// 显示层改成跟执行层同一口径:**界面显示的 = 实际能做的**。
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

// newScopeTestDB 建一个**带角色/资源表**的库
//
// newAuthTestDB 只迁移了用户/身份/登录日志那几张表,算资源要用另外三张。
func newScopeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysUser{},
		&model.SysUserIdentity{},
		&model.SysLoginLog{},
		&model.SysSmsCode{},
		&model.SysOperationLog{},
		&model.SysRole{},
		&model.SysResource{},
		&model.SysRoleResource{},
	))
	return db
}

// seedScopeResource 建一个资源;同码**复用**已有的那一行
//
// sys_resource.code 是 uniqueIndex —— 同一个资源可以被多个角色绑定
// (这正是"两个角色共享一个菜单"的现实),所以资源只能建一次。
func seedScopeResource(t *testing.T, db *gorm.DB, code string) uint64 {
	t.Helper()
	var existing model.SysResource
	if err := db.Where("code = ?", code).First(&existing).Error; err == nil {
		return existing.ID
	}
	resID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysResource{
		BaseModel: model.BaseModel{ID: resID},
		Type:      "menu",
		Code:      code,
		Name:      code,
		IsActive:  true,
	}).Error)
	return resID
}

// seedScopeRole 建一个角色,并把给定的资源码绑上去。返回 roleID。
func seedScopeRole(t *testing.T, db *gorm.DB, code string, resCodes ...string) uint64 {
	t.Helper()
	roleID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysRole{
		BaseModel: model.BaseModel{ID: roleID},
		Code:      code,
		Name:      code,
		Type:      "platform",
		IsActive:  true,
	}).Error)
	for _, rc := range resCodes {
		require.NoError(t, db.Create(&model.SysRoleResource{
			BaseModel:  model.BaseModel{ID: sonyflake.Next()},
			RoleID:     roleID,
			ResourceID: seedScopeResource(t, db, rc),
		}).Error)
	}
	return roleID
}

// seedScopeUser 建一个**双身份**用户(超管 + 普通身份),返回 userID 与两个 identityID
//
// seedAuthUser 只能建一个身份,这里要的是"一个人两个身份"那个形状 ——
// 那个形状正是这个洞的触发条件。
func seedScopeUser(t *testing.T, db *gorm.DB, username, primaryRole, secondRole string) (userID, primaryIdent, secondIdent uint64) {
	t.Helper()
	userID = sonyflake.Next()
	primaryIdent = sonyflake.Next()
	secondIdent = sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel:    model.BaseModel{ID: userID},
		Username:     username,
		PasswordHash: hashForSeed(t, "Pwd@1234567"),
		Nickname:     username,
		IsActive:     true,
	}).Error)
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: primaryIdent},
		UserID:    userID,
		RoleCode:  primaryRole,
		RoleName:  primaryRole,
		IsActive:  true,
		IsPrimary: true,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondIdent},
		UserID:    userID,
		RoleCode:  secondRole,
		RoleName:  secondRole,
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)
	return userID, primaryIdent, secondIdent
}

// codeSet 把资源列表转成 code 集合,便于比较
func codeSet(list []model.SysResource) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, r := range list {
		out[r.Code] = true
	}
	return out
}

// TestAccessibleResources_ScopedToCurrentIdentity 核心断言:同一个用户,
// 当前身份不同 ⇒ 拿到的菜单**必须不同**。
//
// ⚠️ 这条在 M149 之前是**红的**:并集算法下,两个 caller 拿到的是同一份
// "全部资源"(因为并集里有超管 ⇒ isSuper=true ⇒ 走超管分支)。
func TestAccessibleResources_ScopedToCurrentIdentity(t *testing.T) {
	db := newScopeTestDB(t)
	seedScopeRole(t, db, "role_super_admin", "menu.sys", "menu.user")
	seedScopeRole(t, db, "role_user_admin", "menu.user")
	userID, superIdent, userIdent := seedScopeUser(t, db, "dual", "role_super_admin", "role_user_admin")

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	// 当前身份 = 超管
	asSuper, err := svc.AccessibleResources(ctx,
		&Caller{UserID: userID, IdentityID: superIdent, RoleCode: "role_super_admin"}, "menu")
	require.NoError(t, err)
	assert.True(t, codeSet(asSuper)["menu.sys"], "超管应当看得到 menu.sys")

	// 当前身份 = 用户管理员
	asUser, err := svc.AccessibleResources(ctx,
		&Caller{UserID: userID, IdentityID: userIdent, RoleCode: "role_user_admin"}, "menu")
	require.NoError(t, err)

	set := codeSet(asUser)
	assert.True(t, set["menu.user"], "用户管理员应当看得到自己绑的 menu.user")
	assert.False(t, set["menu.sys"],
		"用户管理员**不该**看得到超管的 menu.sys —— 那是并集算法的残留")
	assert.Len(t, asUser, 1, "用户管理员只应有 1 个菜单,不是超管那一套")
}

// TestAccessibleResources_SuperAdminGetsEverything 对照组:
// 超管身份**仍然**拿得到全部资源(别把超管也一起削了)。
func TestAccessibleResources_SuperAdminGetsEverything(t *testing.T) {
	db := newScopeTestDB(t)
	seedScopeRole(t, db, "role_super_admin", "menu.sys")
	seedScopeRole(t, db, "role_user_admin", "menu.user")
	userID, superIdent, _ := seedScopeUser(t, db, "dual2", "role_super_admin", "role_user_admin")

	svc := newAuthTestSvc(t, db)
	res, err := svc.AccessibleResources(context.Background(),
		&Caller{UserID: userID, IdentityID: superIdent, RoleCode: "role_super_admin"}, "menu")
	require.NoError(t, err)
	set := codeSet(res)
	assert.True(t, set["menu.sys"])
	assert.True(t, set["menu.user"], "超管看得到全部菜单,不止自己绑的那几个")
}

// TestAccessibleResources_EmptyRoleGetsNothing 角色为空 ⇒ 空集。
//
// 与执行层一致:role: 空角色匹配不到任何策略行 ⇒ 一样是拒绝。
// (返回"全部资源"才是危险的 —— 那等于给一个没角色的人看整个后台。)
func TestAccessibleResources_EmptyRoleGetsNothing(t *testing.T) {
	db := newScopeTestDB(t)
	seedScopeRole(t, db, "role_super_admin", "menu.sys")
	userID, ident, _ := seedScopeUser(t, db, "norole", "role_super_admin", "role_user_admin")

	svc := newAuthTestSvc(t, db)
	res, err := svc.AccessibleResources(context.Background(),
		&Caller{UserID: userID, IdentityID: ident, RoleCode: ""}, "menu")
	require.NoError(t, err)
	assert.Empty(t, res, "没有角色 = 没有任何资源")
}

// TestGetCurrentUser_ResourcesFollowCurrentIdentity `/auth/me` 的**资源树**
// 必须跟**当前身份**走 —— 它就是前端菜单的来源。
//
// ⚠️ 以前是按身份**列表**的并集算 ⇒ 从超管切到普通身份后,资源树纹丝不动
// (超管那一套全还在),点下去后端才 403。
func TestGetCurrentUser_ResourcesFollowCurrentIdentity(t *testing.T) {
	db := newScopeTestDB(t)
	seedScopeRole(t, db, "role_super_admin", "menu.sys")
	seedScopeRole(t, db, "role_user_admin", "menu.user")
	userID, superIdent, userIdent := seedScopeUser(t, db, "dual3", "role_super_admin", "role_user_admin")

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	// 当前身份 = 超管
	_, ids, res, err := svc.GetCurrentUser(ctx,
		&Caller{UserID: userID, IdentityID: superIdent, RoleCode: "role_super_admin"})
	require.NoError(t, err)
	assert.Len(t, ids, 2, "身份列表仍然是**全部**身份 —— 身份切换器要的就是这张表")
	assert.Len(t, res, 2, "资源按当前身份算:超管看得到全部")

	// 当前身份 = 普通身份
	_, _, res2, err := svc.GetCurrentUser(ctx,
		&Caller{UserID: userID, IdentityID: userIdent, RoleCode: "role_user_admin"})
	require.NoError(t, err)
	assert.Len(t, res2, 1, "资源按当前身份算:只到自己绑的那几个")
}

// TestCurrentUserView_CurrentRoleCodeIsAuthoritative 服务端必须把
// 「当前生效身份的角色码」**直接下发**(current_role_code),
// 前端不许再靠 `identities.find(i => i.id === current_identity_id)` 去猜。
//
// 猜会错在两个地方:
//
//	① 身份被管理员改过 ⇒ `writeUserIdentitiesFromItems` 是**全量替换**
//	   (身份行的 ID 全变)⇒ 旧 session 的 `current_identity_id` 在列表里
//	   **找不到** ⇒ 前端 roleCodes 变空 ⇒ 角色门控(AuthButton 的 roleAnyOf)
//	   整个失效(按钮凭空消失);
//
//	② 库里角色改了、token 还是旧角色 ⇒ 前端显示**新**角色,
//	   后端按 token 里的**旧**角色判(执行层用的是 token claim)。
//
// ⇒ 权威值 = token claims 里那个 role_code —— 与 RBAC 中间件**同一个来源**。
func TestCurrentUserView_CurrentRoleCodeIsAuthoritative(t *testing.T) {
	view := NewCurrentUserView(&model.SysUser{}, nil, "role_user_admin", 42)
	assert.Equal(t, "role_user_admin", view.CurrentRoleCode,
		"当前角色码必须原样下发 —— 前端靠它判角色门控")
	assert.False(t, view.IsPlatformSuperAdmin,
		"平台标记跟着**当前身份**走,不是账号属性")
	assert.False(t, view.IsPlatformOps)

	// wire 上的字段名不能变(前端按名字取)
	raw, err := json.Marshal(view)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Contains(t, m, "current_role_code")
	assert.Equal(t, "role_user_admin", m["current_role_code"])
}

// TestSwitchIdentity_ThenAccessibleResourcesChange 端到端:
// 登录(主身份=超管)→ 切到普通身份 → 用**新 token** 的 caller 拉资源,
// 拿到的必须已经变了。
//
// 这是用户报的那个现象的直接对应物:"切了身份,界面还是一样"。
func TestSwitchIdentity_ThenAccessibleResourcesChange(t *testing.T) {
	db := newScopeTestDB(t)
	seedScopeRole(t, db, "role_super_admin", "menu.sys", "menu.user")
	seedScopeRole(t, db, "role_user_admin", "menu.user")
	_, _, userIdent := seedScopeUser(t, db, "dual4", "role_super_admin", "role_user_admin")

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, err := svc.Login(ctx, LoginRequest{Username: "dual4", Password: preHash("Pwd@1234567")})
	require.NoError(t, err)

	before, err := svc.AccessibleResources(ctx, callerFromTokens(t, tok.Token), "menu")
	require.NoError(t, err)
	require.True(t, codeSet(before)["menu.sys"], "前置条件:切之前是超管,看得到 menu.sys")

	res, err := svc.SwitchIdentity(ctx, callerFromTokens(t, tok.Token), userIdent, "")
	require.NoError(t, err)

	after, err := svc.AccessibleResources(ctx, callerFromTokens(t, res.Token), "menu")
	require.NoError(t, err)
	assert.False(t, codeSet(after)["menu.sys"],
		"切到普通身份后,超管的 menu.sys 必须消失 —— 否则界面还是超管那一套")
	assert.Len(t, after, 1)
}
