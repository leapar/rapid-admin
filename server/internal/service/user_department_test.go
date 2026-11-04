// 用户"部门"契约的回归网 —— 2026-10-25 M5d
//
// 背景:GET /system/users/:id 之前只 Preload 了 Identities,**不返部门**。
// 于是前端编辑弹窗只能把"部门"置为只读 —— 因为 department_ids 是
// "清空重建"式全量替换,发一份不完整的清单等于把这个人的次要部门洗掉。
//
// 同一个 commit 里还修了另一个同源的洞:List 的 UserListItem 里
// 有 department_id / department_name 两个字段,但**从来没人给它们赋值**
// → 前端"部门"列恒为空。
//
// 这里锁三件事:
//  1. Get 返全部部门(带 is_primary,主部门排最前)
//  2. List 填主部门(id + 名称)
//  3. 没有部门的用户 → 详情返空数组(不是 null),列表部门为空
package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// newUserDeptTestDB 内存 sqlite,只迁移用户/部门用到的模型
func newUserDeptTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysUser{},
		&model.SysUserDepartment{},
		&model.SysUserIdentity{},
		&model.SysDepartment{},
	))
	return db
}

const (
	udUserA   = uint64(96000000000000001)
	udUserB   = uint64(96000000000000002)
	udDeptOne = uint64(96000000000000011)
	udDeptTwo = uint64(96000000000000012)
)

// seedUserDept 造一个用户 + 两个部门 + 绑定关系
func seedUserDept(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel: model.BaseModel{ID: udUserA},
		Username:  "deptuser",
		Nickname:  "部门测试",
	}).Error)
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel: model.BaseModel{ID: udUserB},
		Username:  "nodeptuser",
		Nickname:  "无部门",
	}).Error)
	require.NoError(t, db.Create(&model.SysDepartment{
		BaseModel: model.BaseModel{ID: udDeptOne},
		Code:      "D1",
		Name:      "运维部",
		Sort:      1,
	}).Error)
	require.NoError(t, db.Create(&model.SysDepartment{
		BaseModel: model.BaseModel{ID: udDeptTwo},
		Code:      "D2",
		Name:      "客服部",
		Sort:      2,
	}).Error)
	// 主部门 = D2,次要部门 = D1
	require.NoError(t, db.Create(&model.SysUserDepartment{
		BaseModel: model.BaseModel{ID: 96000000000000021},
		UserID:    udUserA,
		DeptID:    udDeptTwo,
		IsPrimary: true,
	}).Error)
	require.NoError(t, db.Create(&model.SysUserDepartment{
		BaseModel: model.BaseModel{ID: 96000000000000022},
		UserID:    udUserA,
		DeptID:    udDeptOne,
	}).Error)
}

func TestUserService_GetReturnsDepartments(t *testing.T) {
	db := newUserDeptTestDB(t)
	seedUserDept(t, db)
	svc := NewUserService(db, nil, nil)

	detail, err := svc.Get(context.Background(), udUserA)
	require.NoError(t, err)

	// 全部部门都要在,不能只给主部门 ——
	// 前端编辑弹窗的回填靠它,少一个就等于"保存一次洗掉一个"
	require.Len(t, detail.Departments, 2)

	// 主部门排最前,且带 is_primary 标记
	require.Equal(t, udDeptTwo, detail.Departments[0].ID)
	require.Equal(t, "客服部", detail.Departments[0].Name)
	require.True(t, detail.Departments[0].IsPrimary)
	require.Equal(t, udDeptOne, detail.Departments[1].ID)
	require.Equal(t, "运维部", detail.Departments[1].Name)
	require.False(t, detail.Departments[1].IsPrimary)

	// 嵌入的 SysUser 字段照旧(身份在详情里)
	require.Equal(t, "deptuser", detail.Username)
}

func TestUserService_GetNoDepartmentsReturnsEmptySlice(t *testing.T) {
	db := newUserDeptTestDB(t)
	seedUserDept(t, db)
	svc := NewUserService(db, nil, nil)

	detail, err := svc.Get(context.Background(), udUserB)
	require.NoError(t, err)
	// 空数组而不是 null:前端 `detail.departments?.length` 两种都能吃,
	// 但 null 在 JSON 里是"字段缺失",前端区分不了"没有"和"接口没返"
	require.NotNil(t, detail.Departments)
	require.Len(t, detail.Departments, 0)
}

func TestUserService_ListFillsPrimaryDepartment(t *testing.T) {
	db := newUserDeptTestDB(t)
	seedUserDept(t, db)
	svc := NewUserService(db, nil, nil)

	res, err := svc.List(context.Background(), UserListQuery{
		PageRequest: model.PageRequest{Page: 1, Size: 20},
	})
	require.NoError(t, err)
	items, ok := res.List.([]UserListItem)
	require.True(t, ok, "List 返的应是 []UserListItem,实际 %T", res.List)
	require.Len(t, items, 2)

	// 列表顺序不保证,按 id 找
	var withDept, withoutDept *UserListItem
	for i := range items {
		it := items[i]
		switch it.ID {
		case udUserA:
			withDept = &it
		case udUserB:
			withoutDept = &it
		}
	}
	require.NotNil(t, withDept)
	require.NotNil(t, withoutDept)

	// 主部门的 id + 名称都要填上 —— 以前这两个字段永远是零值,
	// 前端"部门"列因此恒为空
	require.Equal(t, udDeptTwo, withDept.DepartmentID)
	require.Equal(t, "客服部", withDept.DepartmentName)

	// 没有部门的用户:留空(而不是填一个 id=0 的假部门)
	require.Equal(t, uint64(0), withoutDept.DepartmentID)
	require.Equal(t, "", withoutDept.DepartmentName)
}
