// 数据范围规则 —— service 层单测 —— M155(2026-11-02)
//
// # 这套用例守的是"管理员配的东西是不是他以为的那个意思"
//
// 最要紧的三条:
//  1. 全量替换(不是追加)—— 配两次不会攒出两条
//  2. 业务域单独配了 ⇒ 完全覆盖默认组(不是叠加)
//  3. "本项目"规则 + 调用方没有项目 ⇒ 看不到任何数据(**不是**看到全部)
package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/pkg/sonyflake"
)

// scopeTestModels 数据范围用到的表
var scopeTestModels = []any{
	&model.SysDataScope{},
	&model.SysRole{},
	&model.SysUserIdentity{},
	&model.SysDepartment{},
	&model.SysUserDepartment{},
	&model.SysResource{},
	&model.SysDynCollection{},
}

// newScopeDB 起一个内存库
//
// ⚠️ **连接池限死 1 条连接** —— shared cache 的 SQLite 在两个连接同时写
// 同一张表时直接返回 SQLITE_LOCKED(不是 SQLITE_BUSY,busy handler 不接管)。
// 与 approval_engine_test.go 的 newTestDB 同一理由。
func newScopeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(scopeTestModels...))
	return db
}

// newScopeSvc 起一个数据范围 service,并把业务域登记表填好
//
// ⚠ **业务域登记表是全局的** —— 生产里由路由层的 rbac() 闭包填
// (见 internal/pkg/scope/module.go),单测里没人跑路由 ⇒ 这里手工填。
//
// ⚠ **每个用例都要重置** —— 上一轮用例登记的业务域留在全局里,
// 会让"这个业务域存在"变成假阳性(而那正是本模块要守的东西)。
//
// ⚠ **默认登记 "user" 与 "house"** —— 用例里用到的两个业务域。
func newScopeSvc(t *testing.T, db *gorm.DB, modules ...string) ScopeService {
	t.Helper()
	if len(modules) == 0 {
		modules = []string{"user", "guide"}
	}
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)
	for _, m := range modules {
		scope.RegisterModule(m)
	}
	return NewScopeService(db, nil)
}

// newScopeRole 建一个角色,返回 id 与 code
func newScopeRole(t *testing.T, db *gorm.DB, code string) uint64 {
	t.Helper()
	role := model.SysRole{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Code:      code, Name: code, IsActive: true,
	}
	require.NoError(t, db.Create(&role).Error)
	return role.ID
}

// newScopeIdentity 建一个身份
func newScopeIdentity(t *testing.T, db *gorm.DB, userID, projectID, deptID uint64) uint64 {
	t.Helper()
	ident := model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    userID, RoleCode: "test_role", ProjectID: projectID, DeptID: deptID,
		IsActive: true,
	}
	require.NoError(t, db.Create(&ident).Error)
	return ident.ID
}

// ===== 写 =====

// TestReplaceRoleScopes_全量替换不是追加 —— 配两次不会攒出两条
func TestReplaceRoleScopes_全量替换不是追加(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")

	ctx := context.Background()
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{
		{ScopeType: "dept"},
	}))
	// 再配一次,换成 self
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{
		{ScopeType: "self"},
	}))

	rules, err := svc.ListRoleScopes(ctx, roleID)
	require.NoError(t, err)
	require.Len(t, rules, 1, "第二次配是全量替换,不是追加")
	assert.Equal(t, "self", rules[0].ScopeType)

	// 库里也只能剩一行(软删的墓碑会挡唯一索引,这里硬删)
	var n int64
	require.NoError(t, db.Model(&model.SysDataScope{}).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}

// TestReplaceRoleScopes_重复保存同一条规则不会撞唯一索引
//
// ⚠️ **软删的墓碑会挡唯一索引** ——
// 唯一键是 (role_id, module, scope_type),不含 deleted_at。
// 软删之后那行还在表里,再插一条同组合的行就撞 UNIQUE,
// 整个事务回滚、接口返 500。
// 第二次保存同一个组合是最典型的触发路径(管理员改完备注再保存一次)。
// 见 binding_replace.go 的注释。
func TestReplaceRoleScopes_重复保存同一条规则不会撞唯一索引(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	ctx := context.Background()

	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{
		{ScopeType: "self", Remark: "第一版"},
	}))
	// ⚠️ **同样的类型再来一次** —— 这就是撞墓碑的那条路径
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{
		{ScopeType: "self", Remark: "第二版"},
	}))

	rules, err := svc.ListRoleScopes(ctx, roleID)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, "第二版", rules[0].Remark)
}

// TestReplaceRoleScopes_空数组等于清空
func TestReplaceRoleScopes_空数组等于清空(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	ctx := context.Background()

	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "dept"}}))
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", nil))

	rules, err := svc.ListRoleScopes(ctx, roleID)
	require.NoError(t, err)
	assert.Empty(t, rules)
}

// TestReplaceRoleScopes_不同业务域互不影响
func TestReplaceRoleScopes_不同业务域互不影响(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	ctx := context.Background()

	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "dept"}}))
	// ⚠️ 用 "guide" 当对照业务域 —— 它已登记数据范围接线
	// (这条用例只关心"两个业务域互不影响",维度本身是顺带选的。)
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "guide", []ScopeRuleInput{{ScopeType: "project"}}))

	rules, err := svc.ListRoleScopes(ctx, roleID)
	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, "guide", rules[0].Module)
	assert.Equal(t, "user", rules[1].Module)
}

// TestReplaceRoleScopes_业务域不存在时拒绝
//
// ⚠️ 这是"配错一个字符 ⇒ 规则静默不生效"那道闸
func TestReplaceRoleScopes_业务域不存在时拒绝(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	ctx := context.Background()

	// ⚠ **"user" 是已知业务域 —— 来自路由层登记**
	// (newScopeSvc 已登记),**不是从资源码反推**
	// —— 反推不准(模块与动作都可能含冒号)。

	err := svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "dept"}})
	require.NoError(t, err, "已知业务域要能配")

	err = svc.ReplaceRoleScopes(ctx, roleID, "uesr", []ScopeRuleInput{{ScopeType: "dept"}})
	require.Error(t, err, "拼错的业务域必须当场拒绝(否则规则静默不生效)")
	assert.Contains(t, err.Error(), "不存在")
}

// TestReplaceRoleScopes_自定义列名非法时拒绝
func TestReplaceRoleScopes_自定义列名非法时拒绝(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	ctx := context.Background()

	err := svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{
		{ScopeType: "custom", CustomColumn: "a; drop table x", CustomValues: []string{"1"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "非法字符")
}

// TestReplaceRoleScopes_自定义范围必须给取值
func TestReplaceRoleScopes_自定义范围必须给取值(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")

	err := svc.ReplaceRoleScopes(context.Background(), roleID, "user", []ScopeRuleInput{
		{ScopeType: "custom", CustomColumn: "kind"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "至少给一个取值")
}

// TestReplaceRoleScopes_同类型重复时拒绝
func TestReplaceRoleScopes_同类型重复时拒绝(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")

	err := svc.ReplaceRoleScopes(context.Background(), roleID, "user", []ScopeRuleInput{
		{ScopeType: "dept"},
		{ScopeType: "dept"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "重复")
}

// TestReplaceRoleScopes_未知类型时拒绝
func TestReplaceRoleScopes_未知类型时拒绝(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")

	err := svc.ReplaceRoleScopes(context.Background(), roleID, "user", []ScopeRuleInput{
		{ScopeType: "nope"},
	})
	require.Error(t, err)
}

// ===== 解析 =====

// TestEffectiveScope_没配规则就是不限 —— 规则层上线不改变任何现有查询
func TestEffectiveScope_没配规则就是不限(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	newScopeRole(t, db, "role_a")
	identID := newScopeIdentity(t, db, 1, 10, 0)
	ctx := context.Background()

	sc, err := svc.EffectiveScope(ctx, &Caller{
		UserID: 1, IdentityID: identID, RoleCode: "role_a", ProjectIDs: []uint64{10},
	}, "user")
	require.NoError(t, err)
	assert.False(t, sc.Restrictive)
	assert.False(t, sc.All)
}

// TestEffectiveScope_本项目但调用方没有项目时看不到任何数据
//
// ⚠️ **安全方向上的关键区别** ——
// 若解析成"空 = 不过滤",他就看到了全部数据。
func TestEffectiveScope_本项目但调用方没有项目时看不到任何数据(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	// 身份没有项目(project_id=0),token 里也没有工作范围
	identID := newScopeIdentity(t, db, 1, 0, 0)
	ctx := context.Background()

	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "project"}}))

	sc, err := svc.EffectiveScope(ctx, &Caller{
		UserID: 1, IdentityID: identID, RoleCode: "role_a",
	}, "user")
	require.NoError(t, err)
	assert.True(t, sc.Restrictive, "有限制性规则就必须标记为 restrictive")
	assert.True(t, sc.Empty(), "一个项目都没有 ⇒ 没有任何子句")
	assert.False(t, sc.All)
}

// TestEffectiveScope_身份上的项目优先于token里的并集
func TestEffectiveScope_身份上的项目优先于token里的并集(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	// 身份属于项目 10;token 里的工作范围是 [10, 20](多身份并集)
	identID := newScopeIdentity(t, db, 1, 10, 0)
	ctx := context.Background()

	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "project"}}))

	sc, err := svc.EffectiveScope(ctx, &Caller{
		UserID: 1, IdentityID: identID, RoleCode: "role_a", ProjectIDs: []uint64{10, 20},
	}, "user")
	require.NoError(t, err)
	assert.Equal(t, []uint64{10}, sc.ProjectIDs, "当前身份在项目 10 ⇒ 只看项目 10")
}

// TestEffectiveScope_身份没有项目时用token里的工作范围
func TestEffectiveScope_身份没有项目时用token里的工作范围(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	identID := newScopeIdentity(t, db, 1, 0, 0)
	ctx := context.Background()

	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "project"}}))

	sc, err := svc.EffectiveScope(ctx, &Caller{
		UserID: 1, IdentityID: identID, RoleCode: "role_a", ProjectIDs: []uint64{10, 20},
	}, "user")
	require.NoError(t, err)
	assert.Equal(t, []uint64{10, 20}, sc.ProjectIDs)
}

// TestEffectiveScope_业务域单独配了就完全覆盖默认组
//
// ⚠️ **不是叠加** —— 叠加的话默认组里的 "all" 会一票放行,
// 业务域上的规则永远不生效。
func TestEffectiveScope_业务域单独配了就完全覆盖默认组(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	identID := newScopeIdentity(t, db, 1, 10, 0)
	ctx := context.Background()

	// 默认组 = 全部数据
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "", []ScopeRuleInput{{ScopeType: "all"}}))
	// user 业务域 = 仅本人
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "self"}}))

	sc, err := svc.EffectiveScope(ctx, &Caller{
		UserID: 1, IdentityID: identID, RoleCode: "role_a", ProjectIDs: []uint64{10},
	}, "user")
	require.NoError(t, err)
	assert.False(t, sc.All, "user 上配了 self ⇒ 默认组的 all 不参与")
	assert.Equal(t, []uint64{1}, sc.SelfUserIDs)

	// 别的业务域没配 ⇒ 回落到默认组
	sc, err = svc.EffectiveScope(ctx, &Caller{
		UserID: 1, IdentityID: identID, RoleCode: "role_a", ProjectIDs: []uint64{10},
	}, "house")
	require.NoError(t, err)
	assert.True(t, sc.All, "house 没单独配 ⇒ 用默认组(全部数据)")
}

// TestEffectiveScope_本部门及下级把子部门也算进来
func TestEffectiveScope_本部门及下级把子部门也算进来(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")

	// 部门 1(根) → 2(子) → 3(孙)
	for _, d := range []struct {
		id, parent uint64
	}{{1, 0}, {2, 1}, {3, 2}} {
		require.NoError(t, db.Create(&model.SysDepartment{
			BaseModel: model.BaseModel{ID: d.id},
			ParentID:  d.parent, Name: "d", Code: fmt.Sprintf("d%d", d.id), IsActive: true,
		}).Error)
	}
	identID := newScopeIdentity(t, db, 1, 0, 1)
	ctx := context.Background()

	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "dept_and_child"}}))

	sc, err := svc.EffectiveScope(ctx, &Caller{
		UserID: 1, IdentityID: identID, RoleCode: "role_a",
	}, "user")
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 2, 3}, sc.DeptIDs)
}

// TestEffectiveScope_部门树有环时不会无限递归
//
// ⚠️ 部门表的 parent_id 被改坏(成环)是很现实的事故 ——
// 没有 visited 集合的话,walk 会无限递归,直接把请求线程栈打爆。
func TestEffectiveScope_部门树有环时不会无限递归(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")

	// 1 的父是 2,2 的父是 1 —— 成环
	require.NoError(t, db.Create(&model.SysDepartment{
		BaseModel: model.BaseModel{ID: 1}, ParentID: 2, Name: "a", Code: "a", IsActive: true,
	}).Error)
	require.NoError(t, db.Create(&model.SysDepartment{
		BaseModel: model.BaseModel{ID: 2}, ParentID: 1, Name: "b", Code: "b", IsActive: true,
	}).Error)

	identID := newScopeIdentity(t, db, 1, 0, 1)
	ctx := context.Background()
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "dept_and_child"}}))

	sc, err := svc.EffectiveScope(ctx, &Caller{
		UserID: 1, IdentityID: identID, RoleCode: "role_a",
	}, "user")
	require.NoError(t, err)
	assert.ElementsMatch(t, []uint64{1, 2}, sc.DeptIDs)
}

// TestEffectiveScope_部门优先取身份的,其次取用户的
func TestEffectiveScope_部门优先取身份的其次取用户的(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")

	for _, d := range []uint64{10, 20} {
		require.NoError(t, db.Create(&model.SysDepartment{
			BaseModel: model.BaseModel{ID: d},
			Name:      "d", Code: fmt.Sprintf("d%d", d), IsActive: true,
		}).Error)
	}
	// 用户属于部门 10(主)与 20
	require.NoError(t, db.Create(&model.SysUserDepartment{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    1, DeptID: 20, IsPrimary: false,
	}).Error)
	require.NoError(t, db.Create(&model.SysUserDepartment{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    1, DeptID: 10, IsPrimary: true,
	}).Error)

	ctx := context.Background()
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "dept"}}))

	// 身份没有部门 ⇒ 用用户的部门
	identID := newScopeIdentity(t, db, 1, 0, 0)
	sc, err := svc.EffectiveScope(ctx, &Caller{
		UserID: 1, IdentityID: identID, RoleCode: "role_a",
	}, "user")
	require.NoError(t, err)
	assert.ElementsMatch(t, []uint64{10, 20}, sc.DeptIDs)

	// 身份有部门 ⇒ 只看身份的
	identID2 := newScopeIdentity(t, db, 2, 0, 10)
	sc, err = svc.EffectiveScope(ctx, &Caller{
		UserID: 2, IdentityID: identID2, RoleCode: "role_a",
	}, "user")
	require.NoError(t, err)
	assert.Equal(t, []uint64{10}, sc.DeptIDs)
}

// ===== 落到查询上 =====

// scopeItem 测试用表:三种维度都有
type scopeItem struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement:false"`
	ProjectID uint64
	DeptID    uint64
	OwnerID   uint64
	Name      string
}

func (scopeItem) TableName() string { return "scope_item" }

// TestApply_真库上按范围过滤 —— 断言的是**返回的行**
//
// ⚠️ SQL 文本对得上不代表过滤对得上(列顺序/括号/OR-AND 优先级都能骗过文本断言)。
func TestApply_真库上按范围过滤(t *testing.T) {
	db := newScopeDB(t)
	require.NoError(t, db.AutoMigrate(&scopeItem{}))
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	identID := newScopeIdentity(t, db, 1, 10, 0)

	rows := []scopeItem{
		{ID: 1, ProjectID: 10, DeptID: 100, OwnerID: 1, Name: "a"},
		{ID: 2, ProjectID: 10, DeptID: 101, OwnerID: 2, Name: "b"},
		{ID: 3, ProjectID: 20, DeptID: 100, OwnerID: 3, Name: "c"},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}

	ctx := context.Background()
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "project"}}))

	caller := &Caller{UserID: 1, IdentityID: identID, RoleCode: "role_a", ProjectIDs: []uint64{10}}
	ctx = WithCaller(ctx, caller)
	ctx = scope.WithModule(ctx, "user")

	tx, err := svc.Apply(ctx, db.Model(&scopeItem{}), scope.Columns{
		Project: "project_id IN ?",
		Dept:    "dept_id IN ?",
		Self:    "owner_id IN ?",
	})
	require.NoError(t, err)
	var ids []uint64
	require.NoError(t, tx.Order("id").Pluck("id", &ids).Error)
	assert.Equal(t, []uint64{1, 2}, ids, "本项目(10)⇒ 只看项目 10 的行")
}

// TestApply_没有调用者就不过滤
//
// 内部调用(定时任务)没有用户身份 ⇒ 没有"这个人能看哪些行"这件事。
func TestApply_没有调用者就不过滤(t *testing.T) {
	db := newScopeDB(t)
	require.NoError(t, db.AutoMigrate(&scopeItem{}))
	svc := newScopeSvc(t, db)

	require.NoError(t, db.Create(&scopeItem{ID: 1, Name: "a"}).Error)
	tx, err := svc.Apply(context.Background(), db.Model(&scopeItem{}), scope.Columns{})
	require.NoError(t, err)
	var ids []uint64
	require.NoError(t, tx.Order("id").Pluck("id", &ids).Error)
	assert.Equal(t, []uint64{1}, ids)
}

// TestApply_有限制但表达不出来时匹配不到任何行
func TestApply_有限制但表达不出来时匹配不到任何行(t *testing.T) {
	db := newScopeDB(t)
	require.NoError(t, db.AutoMigrate(&scopeItem{}))
	svc := newScopeSvc(t, db)
	roleID := newScopeRole(t, db, "role_a")
	identID := newScopeIdentity(t, db, 1, 0, 0)

	require.NoError(t, db.Create(&scopeItem{ID: 1, ProjectID: 10, Name: "a"}).Error)

	ctx := context.Background()
	require.NoError(t, svc.ReplaceRoleScopes(ctx, roleID, "user", []ScopeRuleInput{{ScopeType: "project"}}))

	caller := &Caller{UserID: 1, IdentityID: identID, RoleCode: "role_a"}
	ctx = WithCaller(ctx, caller)
	ctx = scope.WithModule(ctx, "user")

	tx, err := svc.Apply(ctx, db.Model(&scopeItem{}), scope.Columns{Project: "project_id IN ?"})
	require.NoError(t, err)
	var ids []uint64
	require.NoError(t, tx.Order("id").Pluck("id", &ids).Error)
	assert.Empty(t, ids, "看不到任何数据,而不是看到全部")
}

// ===== 业务域候选 =====

// TestListModules_业务域来自路由登记而不是从资源码反推
//
// ⚠️ **这是 M155 改过一次的语义** ——
// 业务域的权威定义是 rbac(module, action) 的第一个参数,
// 而 code = module + ":" + action,**两者都可能含冒号**:
//
//	"audit:operation:list" ⇒ module 是 "audit:operation"(不是 "audit")
//	"job:run:read"         ⇒ module 是 "job"(不是 "job:run")
//
// 反推出来的 module 一旦与运行时注入的对不上,
// 规则就会静默失配 ⇒ 退化成"不过滤" ⇒ 越权数据被看到。
func TestListModules_业务域来自路由登记而不是从资源码反推(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)

	// 模拟路由层登记(生产里由 router 的 rbac() 闭包做)
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)
	scope.RegisterModule("user")
	scope.RegisterModule("audit:operation")

	for _, r := range []struct{ typ, code, name string }{
		{"api", "user:list", "用户列表"},
		{"api", "user:read", "用户详情"},
		{"api", "audit:operation:list", "操作日志"},
		{"menu", "menu.system.user", "用户管理"},
	} {
		require.NoError(t, db.Create(&model.SysResource{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			Type:      r.typ, Code: r.code, Name: r.name, IsActive: true,
		}).Error)
	}
	// 动态集合
	require.NoError(t, db.Create(&model.SysDynCollection{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Code:      "contract", Name: "合同表", PhysTable: "dyn_contract", IsActive: true,
	}).Error)

	mods, err := svc.ListModules(context.Background())
	require.NoError(t, err)

	byMod := map[string]ScopeModuleView{}
	for _, m := range mods {
		byMod[m.Module] = m
	}
	require.Contains(t, byMod, "user")
	require.Contains(t, byMod, "audit:operation")
	require.Contains(t, byMod, "dyn:contract")
	// ⚠️ **"audit" 不是业务域** —— 它只是资源码 "audit:operation:list"
	// 的前缀。把它列进候选 = 管理员能配上一条永远不生效的规则。
	require.NotContains(t, byMod, "audit")
	assert.Equal(t, "用户管理", byMod["user"].Label, "菜单名覆盖接口名")
	assert.Equal(t, "api", byMod["user"].Source)
	assert.Equal(t, "dyn", byMod["dyn:contract"].Source)
	assert.Equal(t, "合同表", byMod["dyn:contract"].Label)
}

// TestListModules_默认规则组排在第一个且可以被配置
//
// ⚠️ **默认规则组(module="")是唯一能配"全局默认"的入口** ——
// 某业务域没单独配规则时回落到它(见 loadRules 的"就近覆盖")。
// 它不在候选列表里,管理员就只能挨个业务域配一遍。
func TestListModules_默认规则组排在第一个且可以被配置(t *testing.T) {
	db := newScopeDB(t)
	svc := newScopeSvc(t, db)

	scope.ResetModules()
	t.Cleanup(scope.ResetModules)
	scope.RegisterModule("user")

	mods, err := svc.ListModules(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, mods)
	assert.Equal(t, "", mods[0].Module, "默认规则组排在最前")
	assert.Equal(t, "default", mods[0].Source)

	// 默认规则组必须能保存
	roleID := newScopeRole(t, db, "role_a")
	require.NoError(t, svc.ReplaceRoleScopes(
		context.Background(), roleID, "", []ScopeRuleInput{{ScopeType: "dept"}}))
	rules, err := svc.ListRoleScopes(context.Background(), roleID)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, "", rules[0].Module)
	assert.Equal(t, "dept", rules[0].ScopeType)
}
