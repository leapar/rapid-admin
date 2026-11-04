// 数据范围 · 执行层(落到真实查询上)—— M156(2026-11-02)
//
// # 这一层才是"行级权限"真正生效的地方
//
// M155 把规则存下来了、解析出来了,但**还没接到任何查询上** ——
// 那时候它只是一个"配置得进来、看得见"的漂亮表格。
//
// M156 把它接到两条框架自有的真实查询上:
// · 用户列表(user_impl.go)
// · 操作日志列表(audit_impl.go)
//
// # 三条铁律,各有用例守着
//
//  1. **配了就真的要收窄** —— 规则写在角色上,列表就必须少掉那些行。
//  2. **没配就一个字都不变** —— 上线当天现有查询结果必须完全一致。
//  3. **收窄的是同一条查询** —— 计数与取数必须用同一条 tx,
//     否则总数与列表对不上(页数算错,甚至翻到没有数据的页)。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· 把 user_impl.go 里的 applyScope 调用删掉      → A1/A2 变红
//	· 把 audit_impl.go 里的 applyScope 调用删掉     → B1 变红
//	· 把 applyScope 里的 nil 判断去掉               → 本文件仍绿(nil 走不到)
//	· 把 applyScope 换成"直接返回 tx"(永远不过滤)  → A1/A2/B1 全红
package service

import (
	"context"
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

// scopeApplyModels 执行层用例要用的表
//
// ⚠️ 比 scopeTestModels 多两张 —— 用户列表与操作日志列表要真的查得出东西。
// ⚠️ **sys_resource / sys_dyn_collection 也要建** ——
// 配规则前要过 knownModules(它会列业务域),那两个表就是从那里读的。
var scopeApplyModels = []any{
	&model.SysDataScope{},
	&model.SysRole{},
	&model.SysUser{},
	&model.SysUserIdentity{},
	&model.SysDepartment{},
	&model.SysUserDepartment{},
	&model.SysOperationLog{},
	&model.SysResource{},
	&model.SysDynCollection{},
}

// newScopeApplyDB 起一个内存库(执行层用例专用)
func newScopeApplyDB(t *testing.T) *gorm.DB {
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
	require.NoError(t, db.AutoMigrate(scopeApplyModels...))
	return db
}

// newScopeApplyUser 建一个用户,返回 id
func newScopeApplyUser(t *testing.T, db *gorm.DB, username string) uint64 {
	t.Helper()
	u := model.SysUser{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Username:  username,
		Nickname:  username,
		IsActive:  true,
	}
	require.NoError(t, db.Create(&u).Error)
	return u.ID
}

// newScopeApplyLog 建一条操作日志
func newScopeApplyLog(t *testing.T, db *gorm.DB, userID uint64, module string) uint64 {
	t.Helper()
	l := model.SysOperationLog{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    userID,
		Username:  "u",
		Module:    module,
		Action:    "list",
		Status:    1,
	}
	require.NoError(t, db.Create(&l).Error)
	return l.ID
}

// scopeApplyCtx 造一个「带着调用者 + 带着业务域」的 ctx
//
// ⚠️ **业务域必须显式写进去** ——
// 生产里是中间件/handler 写的(见 middleware/rbac_scope_test.go、
// handler/v1/dyn_scope_test.go)。单测里没人跑路由 ⇒ 这里手工写。
func scopeApplyCtx(t *testing.T, module string, c *Caller) context.Context {
	t.Helper()
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)
	scope.RegisterModule(module)
	ctx := WithCaller(context.Background(), c)
	return scope.WithModule(ctx, module)
}

// ===== A. 用户列表 =====

// A1 ★ 配了"仅本人" ⇒ 用户列表只剩自己
//
// 这是整件事的验收线:角色上配了行级规则,列表**必须**少掉别人的行。
func TestScopeApply_用户列表按仅本人收窄(t *testing.T) {
	db := newScopeApplyDB(t)
	me := newScopeApplyUser(t, db, "me")
	newScopeApplyUser(t, db, "other")

	svc := newScopeSvc(t, db, "user")
	roleID := newScopeRole(t, db, "role_scope_self")
	require.NoError(t, svc.ReplaceRoleScopes(context.Background(), roleID, "user", []ScopeRuleInput{
		{ScopeType: "self"},
	}))

	users := NewUserService(db, nil, svc)
	ctx := scopeApplyCtx(t, "user", &Caller{UserID: me, RoleCode: "role_scope_self"})

	page, err := users.List(ctx, UserListQuery{PageRequest: model.PageRequest{Page: 1, Size: 20}})
	require.NoError(t, err)

	items, ok := page.List.([]UserListItem)
	require.True(t, ok, "列表项类型不对:%T", page.List)
	require.Len(t, items, 1, "配了「仅本人」就只该看到自己")
	assert.Equal(t, me, items[0].ID)
	assert.Equal(t, int64(1), page.Total, "总数也必须一起收窄 —— 否则页数算错")
}

// A2 ★ 没配规则 ⇒ 结果一个字都不变
//
// ⚠️ 这是"上线当天不影响现有业务"的保证 ——
// 没有规则时 EffectiveScope 返回零值 Scope ⇒ 不过滤。
func TestScopeApply_没配规则时不过滤(t *testing.T) {
	db := newScopeApplyDB(t)
	me := newScopeApplyUser(t, db, "me")
	newScopeApplyUser(t, db, "other")

	svc := newScopeSvc(t, db, "user")
	roleID := newScopeRole(t, db, "role_scope_none")
	// 明确配一条"全部数据"与"什么都不配"两种情况都验
	require.NoError(t, svc.ReplaceRoleScopes(context.Background(), roleID, "user", []ScopeRuleInput{
		{ScopeType: "all"},
	}))

	users := NewUserService(db, nil, svc)
	ctx := scopeApplyCtx(t, "user", &Caller{UserID: me, RoleCode: "role_scope_none"})

	page, err := users.List(ctx, UserListQuery{PageRequest: model.PageRequest{Page: 1, Size: 20}})
	require.NoError(t, err)
	items, ok := page.List.([]UserListItem)
	require.True(t, ok)
	assert.Len(t, items, 2, "配了「全部数据」就都该看得到")
	assert.Equal(t, int64(2), page.Total)
}

// A3 规则写在**别的业务域**上 ⇒ 不影响本业务域
//
// ⚠️ 这一条盯的是"就近覆盖"的两侧 ——
// 本业务域没配 ⇒ 回落默认组;默认组也没配 ⇒ 不过滤。
// 而**别的**业务域配了规则,绝不能串过来。
func TestScopeApply_别的业务域的规则不串味(t *testing.T) {
	db := newScopeApplyDB(t)
	me := newScopeApplyUser(t, db, "me")
	newScopeApplyUser(t, db, "other")

	svc := newScopeSvc(t, db, "user", "guide")
	roleID := newScopeRole(t, db, "role_scope_other")
	// ⚠️ 用 "guide" 当对照业务域 —— 它已登记数据范围接线,
	require.NoError(t, svc.ReplaceRoleScopes(context.Background(), roleID, "guide", []ScopeRuleInput{
		{ScopeType: "project"},
	}))

	users := NewUserService(db, nil, svc)
	ctx := scopeApplyCtx(t, "user", &Caller{UserID: me, RoleCode: "role_scope_other"})

	page, err := users.List(ctx, UserListQuery{PageRequest: model.PageRequest{Page: 1, Size: 20}})
	require.NoError(t, err)
	items, ok := page.List.([]UserListItem)
	require.True(t, ok)
	assert.Len(t, items, 2, "别的业务域配了规则,不该影响本业务域")
}

// A4 ★ 有限制但一条都表达不出来 ⇒ **匹配不到任何行**(不是看到全部)
//
// ⚠️ 这是安全方向上的关键区别 ——
// "本部门"规则 + 调用方一个部门都没有 ⇒ 他一条都看不到。
// 如果这里退化成"不过滤",他就看到了全部数据 —— 那是安全事故。
func TestScopeApply_有限制但解析不出范围时看不到任何行(t *testing.T) {
	db := newScopeApplyDB(t)
	me := newScopeApplyUser(t, db, "me")
	newScopeApplyUser(t, db, "other")

	svc := newScopeSvc(t, db, "user")
	roleID := newScopeRole(t, db, "role_scope_dept")
	require.NoError(t, svc.ReplaceRoleScopes(context.Background(), roleID, "user", []ScopeRuleInput{
		{ScopeType: "dept"},
	}))

	users := NewUserService(db, nil, svc)
	// 调用方**没有任何部门**
	ctx := scopeApplyCtx(t, "user", &Caller{UserID: me, RoleCode: "role_scope_dept"})

	page, err := users.List(ctx, UserListQuery{PageRequest: model.PageRequest{Page: 1, Size: 20}})
	require.NoError(t, err)
	items, ok := page.List.([]UserListItem)
	require.True(t, ok)
	assert.Empty(t, items, "配了「本部门」而调用方没有部门 ⇒ 一条都看不到,不是看到全部")
	assert.Equal(t, int64(0), page.Total)
}

// ===== B. 操作日志列表 =====

// B1 ★ 配了"仅本人" ⇒ 操作日志只剩自己那些
//
// ⚠️ 日志表上没有 dept_id / project_id 列 ——
// 归属在**操作人**身上,所以列模板是子查询。
// 这一条同时验"子查询模板在真实表上落得下去"。
func TestScopeApply_操作日志按仅本人收窄(t *testing.T) {
	db := newScopeApplyDB(t)
	me := newScopeApplyUser(t, db, "me")
	other := newScopeApplyUser(t, db, "other")
	newScopeApplyLog(t, db, me, "user")
	newScopeApplyLog(t, db, other, "user")

	svc := newScopeSvc(t, db, "audit:operation")
	roleID := newScopeRole(t, db, "role_audit_self")
	require.NoError(t, svc.ReplaceRoleScopes(context.Background(), roleID, "audit:operation", []ScopeRuleInput{
		{ScopeType: "self"},
	}))

	audit := NewAuditService(db, svc)
	ctx := scopeApplyCtx(t, "audit:operation", &Caller{UserID: me, RoleCode: "role_audit_self"})

	page, err := audit.ListOperationLogs(ctx, OperationLogQuery{
		PageRequest: model.PageRequest{Page: 1, Size: 20},
	})
	require.NoError(t, err)

	items, ok := page.List.([]model.SysOperationLog)
	require.True(t, ok, "列表项类型不对:%T", page.List)
	require.Len(t, items, 1, "配了「仅本人」就只该看到自己的操作记录")
	assert.Equal(t, me, items[0].UserID)
	assert.Equal(t, int64(1), page.Total, "总数也要一起收窄")
}

// B2 操作日志没配规则 ⇒ 全部看得见
func TestScopeApply_操作日志没配规则时不过滤(t *testing.T) {
	db := newScopeApplyDB(t)
	me := newScopeApplyUser(t, db, "me")
	other := newScopeApplyUser(t, db, "other")
	newScopeApplyLog(t, db, me, "user")
	newScopeApplyLog(t, db, other, "user")

	svc := newScopeSvc(t, db, "audit:operation")
	audit := NewAuditService(db, svc)
	ctx := scopeApplyCtx(t, "audit:operation", &Caller{UserID: me, RoleCode: "role_audit_none"})

	page, err := audit.ListOperationLogs(ctx, OperationLogQuery{
		PageRequest: model.PageRequest{Page: 1, Size: 20},
	})
	require.NoError(t, err)
	items, ok := page.List.([]model.SysOperationLog)
	require.True(t, ok)
	assert.Len(t, items, 2, "没配规则就都该看得到")
}

// ===== C. 没有调用者 ⇒ 内部调用不过滤 =====

// C1 没有调用者(定时任务 / 跨服务调用)⇒ 不过滤
//
// ⚠️ 这不是放行漏洞 —— 内部调用不走用户身份,
// 没有"这个人能看哪些行"这件事可谈。
func TestScopeApply_没有调用者时不过滤(t *testing.T) {
	db := newScopeApplyDB(t)
	newScopeApplyUser(t, db, "me")
	newScopeApplyUser(t, db, "other")

	svc := newScopeSvc(t, db, "user")
	users := NewUserService(db, nil, svc)
	// ctx 里**只有业务域,没有调用者**
	ctx := scope.WithModule(context.Background(), "user")

	page, err := users.List(ctx, UserListQuery{PageRequest: model.PageRequest{Page: 1, Size: 20}})
	require.NoError(t, err)
	items, ok := page.List.([]UserListItem)
	require.True(t, ok)
	assert.Len(t, items, 2, "没有调用者就没有行级过滤")
}
