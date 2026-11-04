// 部门写路径(改)的回归网 —— 部门编辑弹窗配套
//
// # 这张网守的是什么
//
// 2026-10-30 用户报:"部门编辑弹窗,负责人为空时显示数值 0,父部门为空时也显示 0"。
// 界面上那两处"0"只是表象,**底下压着三个真的错值 bug**:
//
//  1. **负责人清不掉** —— 旧代码 `if dept.LeaderID > 0` 把"清空"这个动作
//     整个跳过了 ⇒ 用户清空负责人 → 保存 → 界面刷新 → **负责人又回来了**。
//     (而且前端发的是 "0",`> 0` 判定不成立。)
//  2. **父部门判断是恒真式** —— 旧代码 `if dept.ParentID > 0 || dept.ParentID == 0`
//     (uint64 不可能既 >0 又 ==0)⇒ parent_id **永远被写**。
//     后果反过来了:一个**只想改名字**的 PATCH(没带 parent_id)会把部门
//     **静默搬到根** —— 用户的部门结构被一次改名操作改掉了,而且**不报错**。
//  3. **sort 归 0 存不进去** —— `if dept.Sort != 0`。
//
// ⇒ 修法是把入参从 `*model.SysDepartment`(值类型,分不清"没传"和"传了要清空")
//
//	换成 `*DepartmentUpdateInput`(指针字段,nil = 没传 / 非 nil = 传了)。
//
// # 为什么这类 bug 单靠界面看不出来
//
// "清空类"操作失败**不报错** —— 接口返 200,列表刷新一下,看着一切正常,
// 只有对照着"我明明清掉了"才知道没生效。所以必须把每一种清空/归零
// 都钉成一条用例。
package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

func newDeptTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysDepartment{}))
	return db
}

// 造一个部门
func seedDept(t *testing.T, db *gorm.DB, id uint64, code, name string, parentID, leaderID uint64, sort int) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysDepartment{
		BaseModel: model.BaseModel{ID: id},
		Code:      code,
		Name:      name,
		ParentID:  parentID,
		LeaderID:  leaderID,
		Sort:      sort,
		IsActive:  true,
	}).Error)
}

func loadDept(t *testing.T, db *gorm.DB, id uint64) model.SysDepartment {
	t.Helper()
	var d model.SysDepartment
	require.NoError(t, db.First(&d, id).Error)
	return d
}

// ============ 清空类操作 ============

// 负责人可以被清空 —— 旧代码 `if dept.LeaderID > 0` 让这个操作**整个是死的**
func TestUpdateDepartment_CanClearLeader(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 90000000000000099, 1)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000001, &DepartmentUpdateInput{LeaderID: resStrPtr("0")}))

	require.Equal(t, uint64(0), loadDept(t, db, 90000000000000001).LeaderID,
		"清空负责人必须真的生效 —— 否则用户清空、保存、刷新,负责人又回来了")
}

// 父部门可以被清空(提到顶级)
func TestUpdateDepartment_CanPromoteToTopLevel(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 1)
	seedDept(t, db, 90000000000000002, "dept.b", "部门B", 90000000000000001, 0, 1)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000002, &DepartmentUpdateInput{ParentID: resStrPtr("0")}))

	require.Equal(t, uint64(0), loadDept(t, db, 90000000000000002).ParentID,
		"提到顶级必须真的生效")
}

// sort 可以改回 0
func TestUpdateDepartment_CanSetSortToZero(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 7)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000001, &DepartmentUpdateInput{Sort: resIntPtr(0)}))

	require.Equal(t, 0, loadDept(t, db, 90000000000000001).Sort)
}

// ============ PATCH 语义 ============

// **没传的字段保持原值** —— 这条守的是"只想改名字"不会顺手把部门搬到根。
//
// ⚠️ 旧代码的 `if dept.ParentID > 0 || dept.ParentID == 0` 是**恒真式**
//
//	(uint64 不可能既 >0 又 ==0)⇒ parent_id 永远被写。
//	那时"不传"和"传 0"在 wire 上长得一模一样(都是 model 的零值 0),
//	后端根本分不出来 ⇒ 一次改名就把部门静默搬到根,而且不报错。
func TestUpdateDepartment_OmittedFieldsAreUntouched(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 1)
	seedDept(t, db, 90000000000000002, "dept.b", "部门B", 90000000000000001, 90000000000000099, 5)
	svc := NewRoleService(db, nil, nil)

	// 只改 name,不带 parent_id / leader_id / sort / is_active
	require.NoError(t, svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000002, &DepartmentUpdateInput{Name: resStrPtr("新名字")}))

	got := loadDept(t, db, 90000000000000002)
	require.Equal(t, "新名字", got.Name)
	require.Equal(t, uint64(90000000000000001), got.ParentID,
		"没传 parent_id 就不许动它 —— 只想改个名字把部门搬到根,是静默的数据损坏")
	require.Equal(t, uint64(90000000000000099), got.LeaderID, "没传 leader_id 就不许动它")
	require.Equal(t, 5, got.Sort, "没传 sort 就不许动它")
	require.True(t, got.IsActive, "没传 is_active 就不许动它")
}

// 显式传 parent_id 时才写
func TestUpdateDepartment_ExplicitParentIDIsWritten(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 1)
	seedDept(t, db, 90000000000000002, "dept.b", "部门B", 0, 0, 1)
	seedDept(t, db, 90000000000000003, "dept.c", "部门C", 0, 0, 1)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000002, &DepartmentUpdateInput{ParentID: resStrPtr("90000000000000003")}))

	require.Equal(t, uint64(90000000000000003), loadDept(t, db, 90000000000000002).ParentID)
}

// is_active 可以被置 false(停用)
func TestUpdateDepartment_CanDisable(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 1)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000001, &DepartmentUpdateInput{IsActive: resBoolPtr(false)}))

	require.False(t, loadDept(t, db, 90000000000000001).IsActive)
}

// ============ 校验 ============

// 名称不能被清空
func TestUpdateDepartment_RejectsEmptyName(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 1)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000001, &DepartmentUpdateInput{Name: resStrPtr("   ")})
	require.Error(t, err, "名字不能被清成空白")
	require.Contains(t, err.Error(), "部门名称")
}

// code 不能撞别人(code 上有唯一索引,撞了会变成一条看不懂的 500)
func TestUpdateDepartment_RejectsDuplicateCode(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 1)
	seedDept(t, db, 90000000000000002, "dept.b", "部门B", 0, 0, 1)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000002, &DepartmentUpdateInput{Code: resStrPtr("dept.a")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "code 已存在")
}

// code 撞自己不算撞(改别的字段时带上原 code 是正常操作)
func TestUpdateDepartment_AllowsOwnCode(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 1)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000001, &DepartmentUpdateInput{
			Code: resStrPtr("dept.a"),
			Name: resStrPtr("部门A改名"),
		}))

	require.Equal(t, "部门A改名", loadDept(t, db, 90000000000000001).Name)
}

// 非法的 ID 串要报错,不能悄悄当 0 处理
func TestUpdateDepartment_RejectsNonNumericID(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 1)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000001, &DepartmentUpdateInput{LeaderID: resStrPtr("abc")})
	require.Error(t, err, "非法 ID 串必须报错 —— 静默当 0 处理等于把负责人清空了")
}

// 部门不存在
func TestUpdateDepartment_RejectsMissingDepartment(t *testing.T) {
	db := newDeptTestDB(t)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateDepartment(context.Background(), &Caller{UserID: 1},
		90000000000000099, &DepartmentUpdateInput{Name: resStrPtr("x")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "部门不存在")
}

// 未登录不能改
func TestUpdateDepartment_RejectsAnonymous(t *testing.T) {
	db := newDeptTestDB(t)
	seedDept(t, db, 90000000000000001, "dept.a", "部门A", 0, 0, 1)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateDepartment(context.Background(), nil,
		90000000000000001, &DepartmentUpdateInput{Name: resStrPtr("x")})
	require.Error(t, err)
}
