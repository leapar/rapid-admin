// 绑定表"全量替换"的回归网 —— 见 binding_replace.go 的注释。
//
// 2026-10-21 真机复现:
//   PATCH /system/users/:id {"department_ids":[...]}  第 1 次 0,第 2/3 次 50000
//   POST  /system/roles/:id/resources                 第 1 次 0,第 2 次 50000
// 根因:tx.Delete() 对内嵌 BaseModel 的绑定表是软删,而 uk_ud / uk_rr
// 不含 deleted_at → 第二次插入同一条绑定撞 UNIQUE,事务整体回滚。
package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// newBindingTestDB 内存 sqlite,只迁移绑定表用到的模型。
// 注意:AutoMigrate 会照 uniqueIndex tag 建**真正的**唯一索引 ——
// 正是这个索引让软删撞车,所以不能拿 mock DB 测,必须真库。
func newBindingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysUser{},
		&model.SysUserDepartment{},
		&model.SysUserIdentity{},
		&model.SysRole{},
		&model.SysResource{},
		&model.SysRoleResource{},
	))
	return db
}

const (
	testUserID    = uint64(94883951113467590)
	testRoleID    = uint64(94883947674138310)
	testResourceA = uint64(94883947691046598)
	testResourceB = uint64(94883947691112134)
	testDeptA     = uint64(95024008554149574)
	testDeptB     = uint64(95024008554215110)
)

// freshID 模拟 sonyflake.Next():每次插入给一个**全新**的主键。
//
// 这不是细节,是本测试成立的前提。生产写绑定行时主键总是新生成的,
// 所以软删重插**不会**撞主键 —— 撞的是 uk_ud(user_id,dept_id)。
// 如果测试里用固定主键,它会在 PK 上先炸,报错变成
// "UNIQUE constraint failed: sys_user_department.id",
// 把人引向完全错误的排查方向(去查 sonyflake 有没有重复),
// 而线上真实的报错里根本没有 id 这个字段。
func freshID() func() uint64 {
	var n uint64 = 95435016523282118
	return func() uint64 { n++; return n }
}

// TestClearBindings_SurvivesRepeatedReplace 是本文件的核心断言:
// **同一个"全量替换"做两次都必须成功**。
//
// 只做一次是测不出来的 —— 第一次永远成功,坏的实现只在第二次炸。
// 这个"第二次"是这类 bug 唯一的可观测信号,别把它省掉。
func TestClearBindings_SurvivesRepeatedReplace(t *testing.T) {
	db := newBindingTestDB(t)

	nextID := freshID()
	replace := func(deptIDs []uint64) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := clearBindings(tx, &model.SysUserDepartment{}, "user_id = ?", testUserID); err != nil {
				return err
			}
			for _, d := range deptIDs {
				row := &model.SysUserDepartment{
					BaseModel: model.BaseModel{ID: nextID()},
					UserID:    testUserID,
					DeptID:    d,
				}
				if err := tx.Create(row).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}

	// 第一次:写 2 条
	require.NoError(t, replace([]uint64{testDeptA, testDeptB}),
		"第一次全量替换失败")

	// 第二次:**写一模一样的 2 条** —— 软删实现必然在这里 UNIQUE 冲突
	require.NoError(t, replace([]uint64{testDeptA, testDeptB}),
		"第二次全量替换失败:绑定行被软删后重插会撞 uk_ud(user_id,dept_id)")

	// 第三次:换一批 ID 再换回来,证明不是"第一次特殊"
	require.NoError(t, replace([]uint64{testDeptA}))
	require.NoError(t, replace([]uint64{testDeptA, testDeptB}))

	// 终态:2 行,且**没有墓碑**
	var live []model.SysUserDepartment
	require.NoError(t, db.Find(&live).Error)
	assert.Len(t, live, 2, "最终有效绑定应为 2 条")

	var total int64
	require.NoError(t, db.Unscoped().Model(&model.SysUserDepartment{}).Count(&total).Error)
	assert.EqualValues(t, 2, total,
		"表里多出了墓碑行:clearBindings 退化成软删了 —— 每保存一次只增不减")
}

// TestClearBindings_KeepsOtherRows 把作用域钉死:只清匹配的行
func TestClearBindings_KeepsOtherRows(t *testing.T) {
	db := newBindingTestDB(t)
	const otherUser = uint64(387093976500448304)

	require.NoError(t, db.Create(&model.SysUserDepartment{
		BaseModel: model.BaseModel{ID: 1}, UserID: testUserID, DeptID: testDeptA,
	}).Error)
	require.NoError(t, db.Create(&model.SysUserDepartment{
		BaseModel: model.BaseModel{ID: 2}, UserID: otherUser, DeptID: testDeptA,
	}).Error)

	require.NoError(t, clearBindings(db, &model.SysUserDepartment{}, "user_id = ?", testUserID))

	var left int64
	require.NoError(t, db.Model(&model.SysUserDepartment{}).Count(&left).Error)
	assert.EqualValues(t, 1, left, "别的用户的绑定被误删了")
}

// TestClearBindings_SameOnRoleResource 覆盖另一张有唯一索引的绑定表
// uk_rr(role_id,resource_id,project_id) —— 分配权限抽屉走的正是这条。
func TestClearBindings_SameOnRoleResource(t *testing.T) {
	db := newBindingTestDB(t)

	nextID := freshID()
	assign := func(resIDs []uint64) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := clearBindings(tx, &model.SysRoleResource{}, "role_id = ?", testRoleID); err != nil {
				return err
			}
			for _, r := range resIDs {
				row := &model.SysRoleResource{
					BaseModel:  model.BaseModel{ID: nextID()},
					RoleID:     testRoleID,
					ResourceID: r,
				}
				if err := tx.Create(row).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}

	require.NoError(t, assign([]uint64{testResourceA, testResourceB}))
	require.NoError(t, assign([]uint64{testResourceA, testResourceB}),
		"第二次分配权限失败:分配权限抽屉第二次点保存就会 500")

	var got []uint64
	require.NoError(t, db.Model(&model.SysRoleResource{}).
		Where("role_id = ?", testRoleID).
		Order("resource_id").Pluck("resource_id", &got).Error)
	assert.Equal(t, []uint64{testResourceA, testResourceB}, got)
}

// TestClearBindings_NoLeakWhenShrinking 空集合也是合法输入:
// 前端全不勾就保存 = 清空。不许报错,也不许留墓碑。
func TestClearBindings_NoLeakWhenShrinking(t *testing.T) {
	db := newBindingTestDB(t)

	require.NoError(t, db.Create(&model.SysUserDepartment{
		BaseModel: model.BaseModel{ID: 1}, UserID: testUserID, DeptID: testDeptA,
	}).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return clearBindings(tx, &model.SysUserDepartment{}, "user_id = ?", testUserID)
	}))

	var live, total int64
	require.NoError(t, db.Model(&model.SysUserDepartment{}).Count(&live).Error)
	require.NoError(t, db.Unscoped().Model(&model.SysUserDepartment{}).Count(&total).Error)
	assert.EqualValues(t, 0, live, "全不勾应该清空,而不是报错")
	assert.EqualValues(t, 0, total, "清空后不该留墓碑")
}