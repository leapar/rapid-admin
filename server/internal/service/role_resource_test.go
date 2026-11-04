// 资源写路径(建/改)的回归网 —— 权限资源编辑弹窗配套
//
// # 这张网守的是什么
//
// 资源编辑弹窗是**运营改权限点**的唯一入口。2026-10-30 用户报了三件事:
//   1. 图标是自由输入 —— 改成下拉选择(前端);
//   2. HTTP 方法没有「全部」 —— 加了之后必须能**真的清空**这一列;
//   3. 父资源可以跨类型选 —— 要按类型约束。
//
// 第 2、3 条都要落到后端才有意义:
//   · 前端下拉里选了「全部」,后端 `if res.Method != ""` 把它跳过了
//     ⇒ **改了没生效**,用户看着界面以为改掉了,其实库里还是老的;
//   · 前端过滤了候选项,直接打 API 还是能写进去
//     ⇒ 菜单挂到按钮下面 ⇒ 侧边栏里这个菜单**永远不出现**(静默消失)。
//
// ⚠️ **"清空类"操作以前全是死的** —— 这类 bug 不会报错,
//   只会让"改了没生效",所以必须钉死。
package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

func newResourceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysResource{}))
	return db
}

// 造一条资源
func seedResource(t *testing.T, db *gorm.DB, id uint64, code, typ string, parentID uint64) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysResource{
		BaseModel: model.BaseModel{ID: id},
		Code:      code,
		Name:      code,
		Type:      typ,
		ParentID:  parentID,
		Sort:      3,
		Path:      "/x",
		Method:    "GET",
		Icon:      "system",
		IsActive:  true,
	}).Error)
}

func loadResource(t *testing.T, db *gorm.DB, id uint64) model.SysResource {
	t.Helper()
	var r model.SysResource
	require.NoError(t, db.First(&r, id).Error)
	return r
}

func resStrPtr(s string) *string { return &s }
func resIntPtr(i int) *int       { return &i }
func resBoolPtr(b bool) *bool    { return &b }

// ============ 清空类操作 ============

// method 可以被清空(前端「全部」= 不限定)
func TestUpdateResource_CanClearMethod(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "user:list", "api", 0)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000001, &ResourceUpdateInput{Method: resStrPtr("")}))

	require.Equal(t, "", loadResource(t, db, 90000000000000001).Method,
		"method 必须能清空 —— 前端选「全部」要是存不进去,用户改了跟没改一样")
}

// icon 可以被清空(菜单不需要图标)
func TestUpdateResource_CanClearIcon(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "menu.system", "menu", 0)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000001, &ResourceUpdateInput{Icon: resStrPtr("")}))

	require.Equal(t, "", loadResource(t, db, 90000000000000001).Icon)
}

// sort 可以改回 0
func TestUpdateResource_CanSetSortToZero(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "menu.system", "menu", 0)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000001, &ResourceUpdateInput{Sort: resIntPtr(0)}))

	require.Equal(t, 0, loadResource(t, db, 90000000000000001).Sort)
}

// parent_id 可以改回 0(提到顶级)——
// 以前 `if res.ParentID > 0` 把它跳过了,下拉里的「— 顶级 —」对已有资源是死的
func TestUpdateResource_CanPromoteToTopLevel(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "menu.parent", "menu", 0)
	seedResource(t, db, 90000000000000002, "menu.child", "menu", 90000000000000001)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000002, &ResourceUpdateInput{ParentID: resStrPtr("0")}))

	require.Equal(t, uint64(0), loadResource(t, db, 90000000000000002).ParentID,
		"提到顶级必须真的生效")
}

// 没传的字段保持原值(PATCH 语义)
func TestUpdateResource_OmittedFieldsAreUntouched(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "user:list", "api", 0)
	svc := NewRoleService(db, nil, nil)

	// 只改 name,不带 method / icon / sort / parent_id
	require.NoError(t, svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000001, &ResourceUpdateInput{Name: resStrPtr("新名字")}))

	got := loadResource(t, db, 90000000000000001)
	require.Equal(t, "新名字", got.Name)
	require.Equal(t, "GET", got.Method, "没传的 method 不能被清掉")
	require.Equal(t, "system", got.Icon)
	require.Equal(t, 3, got.Sort)
}

// 非法 HTTP 方法要报错
func TestUpdateResource_RejectsInvalidMethod(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "user:list", "api", 0)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000001, &ResourceUpdateInput{Method: resStrPtr("FOO")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 方法")
}

// ============ 父资源约束 ============

// 菜单不能挂在按钮下面(挂了就静默消失)
func TestUpdateResource_RejectsMenuUnderButton(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "button.user.export", "button", 0)
	seedResource(t, db, 90000000000000002, "menu.system", "menu", 0)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000002, &ResourceUpdateInput{ParentID: resStrPtr("90000000000000001")})
	require.Error(t, err, "菜单挂到按钮下面必须被拒 —— 前端过滤只是建议,后端才是约束")
	require.Contains(t, err.Error(), "父资源类型")
}

// 按钮不能挂任何父级(2026-11-03 用户拍板:parent_id 只有菜单树一个读者,
// 按钮在所有消费方里都是平铺的 ⇒ 挂父级是死数据。原口径"按钮挂菜单合法"作废)
func TestUpdateResource_RejectsButtonUnderMenu(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "menu.system", "menu", 0)
	seedResource(t, db, 90000000000000002, "button.user.export", "button", 0)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000002, &ResourceUpdateInput{ParentID: resStrPtr("90000000000000001")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "父资源类型")
}

// 没传 parent_id 的 PATCH 不碰父级 —— 历史上挂过父级的按钮/接口
// 改其他字段不受影响(PATCH 语义,不追溯清洗存量)
func TestUpdateResource_ButtonOmittedParentUntouched(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "menu.system", "menu", 0)
	// 存量脏数据:按钮挂着菜单(新规则下不会再产生,但不追溯)
	seedResource(t, db, 90000000000000002, "button.user.export", "button", 90000000000000001)
	svc := NewRoleService(db, nil, nil)

	require.NoError(t, svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000002, &ResourceUpdateInput{Name: resStrPtr("改名")}))

	require.Equal(t, uint64(90000000000000001), loadResource(t, db, 90000000000000002).ParentID,
		"没传 parent_id 就不能动存量父级")
}

// 父资源不能是自己
func TestUpdateResource_RejectsSelfParent(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "menu.system", "menu", 0)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000001, &ResourceUpdateInput{ParentID: resStrPtr("90000000000000001")})
	require.Error(t, err)
}

// 父资源不能是自己的后代(成环 ⇒ 菜单在树里静默消失)
func TestUpdateResource_RejectsParentCycle(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "menu.a", "menu", 0)
	seedResource(t, db, 90000000000000002, "menu.a.b", "menu", 90000000000000001)
	seedResource(t, db, 90000000000000003, "menu.a.b.c", "menu", 90000000000000002)
	svc := NewRoleService(db, nil, nil)

	// 把 A 的父设成 A 的孙子 C ⇒ A → C → B → A 成环
	err := svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000001, &ResourceUpdateInput{ParentID: resStrPtr("90000000000000003")})
	require.Error(t, err, "成环必须被拒 —— 环上的节点在 buildMenuTree 里会凭空消失")
	require.Contains(t, err.Error(), "环")
}

// 父资源必须存在
func TestUpdateResource_RejectsMissingParent(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "menu.system", "menu", 0)
	svc := NewRoleService(db, nil, nil)

	err := svc.UpdateResource(context.Background(), &Caller{UserID: 1},
		90000000000000001, &ResourceUpdateInput{ParentID: resStrPtr("90000000000000099")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "父资源不存在")
}

// ============ 创建路径 ============

// 创建时也要校验父资源类型
func TestCreateResource_RejectsIllegalParentType(t *testing.T) {
	db := newResourceTestDB(t)
	seedResource(t, db, 90000000000000001, "button.user.export", "button", 0)
	svc := NewRoleService(db, nil, nil)

	_, err := svc.CreateResource(context.Background(), &Caller{UserID: 1}, &model.SysResource{
		Code:     "menu.new",
		Name:     "新菜单",
		Type:     "menu",
		ParentID: 90000000000000001,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "父资源类型")
}

// 创建时非法 HTTP 方法要报错
func TestCreateResource_RejectsInvalidMethod(t *testing.T) {
	db := newResourceTestDB(t)
	svc := NewRoleService(db, nil, nil)

	_, err := svc.CreateResource(context.Background(), &Caller{UserID: 1}, &model.SysResource{
		Code:   "user:list",
		Name:   "用户列表",
		Type:   "api",
		Method: "FOO",
	})
	require.Error(t, err)
}
