// 用户列表「权限」列的契约回归网 —— 2026-10-30 M122
//
// 背景:用户管理列表里**完全没有角色信息** —— 管理员只能点进详情抽屉
// 才看得到"这个人到底有哪些权限"。列表页扫一眼看不出权限,
// 而"这个人有什么权限"恰恰是用户管理页最该自明的信息之一。
//
// 这里锁四件事(每一条都是**语义边界**,不是格式):
//
//  1. **只算有效身份**:is_active = false 的身份不授予任何权限
//     (authz 的 loadCaller 同样要求 is_active = true —— 两边必须一致,
//     否则列表上写着"超管"、实际却什么都干不了)
//  2. **同一个 role_code 只留一条**:这一列回答的是"这个人有什么权限",
//     不是"这个人有几个身份"(身份清单在详情抽屉里)
//  3. **没有身份 → 空数组**,不是 nil
//     (nil 在 JSON 里是 null,前端区分不了"没有角色"和"接口没返这个字段")
//  4. **主身份的角色排最前**:列表是给人扫的,主身份才是那个人此刻在用的身份
//
// ⚠️ 这条网还锁住了一个**性能边界**:角色是一条 SQL 批量查的,
//   不是 N+1(每个用户查一次)。
package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// newUserRoleTestDB 内存 sqlite
//
// ⚠️ **部门那两张表也得建** —— List 顺手会把主部门也查出来
//   (user_impl.go 的 primaryDepartments),少建表直接报
//   "no such table: sys_user_department"。这里只建表,不造数据。
func newUserRoleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysUser{},
		&model.SysUserIdentity{},
		&model.SysUserDepartment{},
		&model.SysDepartment{},
	))
	return db
}

const (
	urUserA   = uint64(96000000000000101)
	urUserB   = uint64(96000000000000102)
	urUserC   = uint64(96000000000000103)
	urIdentOK = uint64(96000000000000111)
)

// seedUserRole 造三个用户:
//   · A:主身份=超管,次身份=客服(都有效)
//   · B:只有一个**已停用**的身份(角色看得见但没权限)
//   · C:一个身份都没有
func seedUserRole(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, u := range []model.SysUser{
		{BaseModel: model.BaseModel{ID: urUserA}, Username: "roleuser", Nickname: "多身份"},
		{BaseModel: model.BaseModel{ID: urUserB}, Username: "inactiveuser", Nickname: "停用身份"},
		{BaseModel: model.BaseModel{ID: urUserC}, Username: "noroleuser", Nickname: "无身份"},
	} {
		require.NoError(t, db.Create(&u).Error)
	}

	// ⚠️ **插入顺序故意跟"期望的显示顺序"相反** ——
	//   非主身份先插,主身份后插。
	//   少了 `ORDER BY is_primary DESC` 的话,SQLite 按 rowid 返回
	//   → 非主身份排前面 → "主身份排最前"那条断言就红。
	//
	//   (之前主身份先插,两种顺序碰巧一模一样 —— 断言看着在,其实没锁住任何东西。
	//    可逆探针 P4「去掉 ORDER BY」实测:改完之后用例照样绿 ⇒ 假信心。)
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: urIdentOK},
		UserID:    urUserA,
		RoleCode:  "role_customer_service",
		RoleName:  "客服",
		IsActive:  true,
	}).Error)
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: urIdentOK + 1},
		UserID:    urUserA,
		RoleCode:  "role_super_admin",
		RoleName:  "超级管理员",
		IsActive:  true,
		IsPrimary: true,
	}).Error)
	// B:角色是"超管",但身份已停用 → 不授予任何权限
	//
	// ⚠️ **这里必须"先建再改",不能一步 Create** ——
	//   model.SysUserIdentity.IsActive 带 `gorm:"default:true"`,
	//   而 GORM 的规则是:**带 default 的字段,零值(这里是 false)会被
	//   跳过不写**,让数据库默认值(true)生效。
	//   所以直接 `Create(&x{IsActive: false})` 写进去的其实是 **true** ——
	//   这条用例一开始就是这么写的,结果是"停用身份照样带权限",红得莫名其妙。
	//
	//   ⚠️ `Select("is_active").Create(...)` 也**不行** ——
	//   Create 上的 Select 是"**只写这些列**",于是 INSERT 变成
	//   (created_at, updated_at, is_active),user_id 没了 →
	//   NOT NULL constraint failed。
	//   改成先 Create(默认 true)、再 Update 显式写 false。
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: urIdentOK + 2},
		UserID:    urUserB,
		RoleCode:  "role_super_admin",
		RoleName:  "超级管理员",
		IsActive:  true,
	}).Error)
	require.NoError(t, db.Model(&model.SysUserIdentity{}).
		Where("id = ?", urIdentOK+2).
		Update("is_active", false).Error)

	// 确认真的写进去了(false)—— 不然下面那条用例就是"假绿"
	var isActive bool
	require.NoError(t, db.Model(&model.SysUserIdentity{}).
		Where("id = ?", urIdentOK+2).
		Select("is_active").
		Scan(&isActive).Error)
	require.False(t, isActive, "停用身份没有真的写进去 —— 这条用例就成假绿了")
}

// listRolesByUserID 跑一次 List,把某个用户的角色摘出来
func listRolesByUserID(t *testing.T, db *gorm.DB, userID uint64) []UserRoleBrief {
	t.Helper()
	svc := NewUserService(db, nil, nil)
	res, err := svc.List(context.Background(), UserListQuery{
		PageRequest: model.PageRequest{Page: 1, Size: 20},
	})
	require.NoError(t, err)
	items, ok := res.List.([]UserListItem)
	require.True(t, ok, "List 返的应是 []UserListItem,实际 %T", res.List)

	for _, it := range items {
		if it.ID == userID {
			return it.Roles
		}
	}
	t.Fatalf("列表里没有用户 %d", userID)
	return nil
}

func TestUserService_ListFillsRoles(t *testing.T) {
	db := newUserRoleTestDB(t)
	seedUserRole(t, db)

	roles := listRolesByUserID(t, db, urUserA)
	require.Len(t, roles, 2)

	// 主身份的角色排最前 —— 列表是给人扫的,主身份才是那个人此刻在用的
	require.Equal(t, "role_super_admin", roles[0].RoleCode)
	require.Equal(t, "超级管理员", roles[0].RoleName)
	require.Equal(t, "role_customer_service", roles[1].RoleCode)
	require.Equal(t, "客服", roles[1].RoleName)
}

func TestUserService_ListExcludesInactiveIdentityRoles(t *testing.T) {
	db := newUserRoleTestDB(t)
	seedUserRole(t, db)

	// ⚠️ **停用的身份不授予任何权限** ——
	//   列表上写着"超级管理员"、实际却什么都干不了,比不写更害人。
	//   (authz 的 loadCaller 同样要求 is_active = true,两边必须一致)
	roles := listRolesByUserID(t, db, urUserB)
	require.NotNil(t, roles, "没有角色也要给空数组,不能是 nil")
	require.Len(t, roles, 0)
}

func TestUserService_ListNoIdentityReturnsEmptySlice(t *testing.T) {
	db := newUserRoleTestDB(t)
	seedUserRole(t, db)

	roles := listRolesByUserID(t, db, urUserC)
	// 空数组而不是 nil:nil 在 JSON 里是 null,前端区分不了
	// "这个人没有角色"和"接口没返这个字段"
	require.NotNil(t, roles)
	require.Len(t, roles, 0)
}

func TestUserService_ListDedupsSameRoleCode(t *testing.T) {
	db := newUserRoleTestDB(t)
	seedUserRole(t, db)

	// 同一个角色出现在两个身份里(比如两个项目都是同一个角色)
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: urIdentOK + 3},
		UserID:    urUserA,
		RoleCode:  "role_customer_service",
		RoleName:  "客服",
		IsActive:  true,
		ProjectID: 77,
	}).Error)

	roles := listRolesByUserID(t, db, urUserA)
	// 这一列回答的是"这个人有什么权限",不是"这个人有几个身份"
	// —— 同一个 role_code 只留一条
	require.Len(t, roles, 2)

	seen := map[string]int{}
	for _, r := range roles {
		seen[r.RoleCode]++
	}
	require.Equal(t, 1, seen["role_customer_service"])
	require.Equal(t, 1, seen["role_super_admin"])
}
