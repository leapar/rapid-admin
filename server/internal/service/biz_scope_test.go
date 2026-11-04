// 业务查询 × 数据范围(行级权限)的接线单测
//
// ⚠️ **这批用例守的是"看不见的行"** ——
// 数据范围这层最贵的失败模式不是"过滤过头"(用户看不到该看的,会来投诉),
// 而是"过滤过头被当成过滤不够"(用户看到了不该看的,**没人会来投诉**)。
//
// ⇒ 每条用例都断言**看不到的那一行确实看不到**,而不是只断言"该看的那行看得见"。
//
// # ⚠️ **为什么必须用真 sqlite,而不是 mock 掉查询**
//
// 数据范围最终落成一段 WHERE;这一层最阴险的失败是
// **条件写对了但参数顺序错了** —— 那不报错,只是查出错的数据。
// ⇒ 断言必须落在**真的查出来的行**上。
package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/pkg/sonyflake"
)

// bizScopeTestModels 这批用例要建的表
//
// ⚠️ **少了哪张表,红的就是"no such table"而不是断言** ——
// 那样的红是**假红**,读的人会以为是自己测的东西坏了。
//
// ⚠️ **sys_dyn_collection 也要建** ——
// ListModules 会去查它(动态集合是候选业务域的一个来源),
// 少建一张 ⇒ 连"只列接了数据范围的业务域"那条都跑不起来。
var bizScopeTestModels = []any{
	&model.SysResource{},
	&model.SysRole{},
	&model.SysRoleResource{},
	&model.SysUserDepartment{},
	&model.SysUserIdentity{},
	&model.SysDataScope{},
	&model.SysDynCollection{},
	// 业务表:框架自带的示例业务域(guide)
	&model.Guide{},
}

// newBizScopeFixture 起一个内存库 + 装了数据范围服务的 scopeService
func newBizScopeFixture(t *testing.T) (*gorm.DB, ScopeService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	// shared cache 的 SQLite 两个连接同时写会直接 SQLITE_LOCKED ⇒ 限死 1 条连接
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(bizScopeTestModels...))
	// ⚠️ **safe 传 nil** —— 没有 Redis / cache.mode=off 时就是退化成每次回源,
	// **不改变函数的成功/失败语义**(与 cachex 全体一致)。
	return db, NewScopeService(db, nil)
}

// bizScopeSeedRole 建一个角色
func bizScopeSeedRole(t *testing.T, db *gorm.DB, id uint64, code string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysRole{
		BaseModel: model.BaseModel{ID: id},
		Code:      code,
		Name:      code,
		IsActive:  true,
	}).Error)
}

// bizScopeSeedRule 写一条数据范围规则
//
// ⚠️ **module 为空串 = 默认规则组** —— 某业务域没单独配时回落到它。
//
// ⚠️ **必须自己给 ID** —— model.BaseModel.ID 是 `autoIncrement:false`,
// 不给就是 0 ⇒ 同一个用例里插第二条规则时撞 UNIQUE(而不是"两条规则都进去了")。
func bizScopeSeedRule(t *testing.T, db *gorm.DB, roleID uint64, module, scopeType string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysDataScope{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		RoleID:    roleID,
		Module:    module,
		ScopeType: scopeType,
	}).Error)
}

// bizScopeSeedDept 把用户放进部门
func bizScopeSeedDept(t *testing.T, db *gorm.DB, userID, deptID uint64) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysUserDepartment{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    userID,
		DeptID:    deptID,
		IsPrimary: true,
	}).Error)
}

// bizScopeCaller 造一个带角色码的调用者
func bizScopeCaller(userID uint64, roleCode string) *Caller {
	return &Caller{UserID: userID, RoleCode: roleCode}
}

// bizScopeCtx 把调用者放进 ctx
func bizScopeCtx(c *Caller) context.Context {
	return WithCaller(context.Background(), c)
}

// bizScopeGuide 建一条指南(指定创建人与项目)
func bizScopeGuide(t *testing.T, db *gorm.DB, id uint64, title string, projectID, createdBy uint64) {
	t.Helper()
	require.NoError(t, db.Create(&model.Guide{
		BaseModel: model.BaseModel{ID: id},
		Title:     title,
		Category:  "faq",
		Content:   "内容",
		ProjectID: projectID,
		CreatedBy: createdBy,
	}).Error)
}

// ===== 主用例 =====

// TestBizScope_仅本人_只看自己建的行
//
// ⚠️ **这条是本文件的主用例** ——
// 角色在 "guide" 上配了「仅本人」⇒ 列表里**只有他自己建的行**。
func TestBizScope_仅本人_只看自己建的行(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	bizScopeSeedRule(t, db, 9101, "guide", "self")

	bizScopeGuide(t, db, 1, "我的", 10, 1001)
	bizScopeGuide(t, db, 2, "别人的", 10, 2002)

	svc := NewGuideService(db, scopeSvc)
	page, err := svc.ListGuides(bizScopeCtx(bizScopeCaller(1001, "role_biz_scope")), GuideListQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "只看自己建的 ⇒ 别人的那行必须看不见")

	rows, ok := page.List.([]model.Guide)
	require.True(t, ok)
	require.Len(t, rows, 1)
	assert.Equal(t, "我的", rows[0].Title)
}

// TestBizScope_本项目_只看本项目的行
func TestBizScope_本项目_只看本项目的行(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	bizScopeSeedRule(t, db, 9101, "guide", "project")

	bizScopeGuide(t, db, 1, "本项目", 10, 1001)
	bizScopeGuide(t, db, 2, "别的项目", 20, 1001)

	svc := NewGuideService(db, scopeSvc)
	caller := bizScopeCaller(1001, "role_biz_scope")
	caller.ProjectIDs = []uint64{10}
	page, err := svc.ListGuides(bizScopeCtx(caller), GuideListQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "只看本项目 ⇒ 别的项目那行必须看不见")
}

// TestBizScope_本部门_只看本部门的人建的行
//
// ⚠️ **业务表没有 dept_id 列** —— "本部门"落在**行归属人**那一列上,
// 通过 sys_user_department 反查。
func TestBizScope_本部门_只看本部门的人建的行(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	bizScopeSeedRule(t, db, 9101, "guide", "dept")
	bizScopeSeedDept(t, db, 1001, 7)

	bizScopeGuide(t, db, 1, "同部门", 10, 2002) // 2002 也在 7 号部门
	bizScopeGuide(t, db, 2, "别的部门", 10, 3003) // 3003 在 8 号部门
	bizScopeSeedDept(t, db, 2002, 7)
	bizScopeSeedDept(t, db, 3003, 8)

	svc := NewGuideService(db, scopeSvc)
	page, err := svc.ListGuides(bizScopeCtx(bizScopeCaller(1001, "role_biz_scope")), GuideListQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "只看本部门的人建的 ⇒ 别的部门那行必须看不见")

	rows, ok := page.List.([]model.Guide)
	require.True(t, ok)
	require.Len(t, rows, 1)
	assert.Equal(t, "同部门", rows[0].Title)
}

// TestBizScope_没配规则_不过滤
//
// ⚠️ **这是刻意的默认语义** —— 没配规则 = 全部数据。
// 反过来(没配 = 看不到任何数据)会让上线当天全站空列表。
func TestBizScope_没配规则_不过滤(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")

	bizScopeGuide(t, db, 1, "一", 10, 1001)
	bizScopeGuide(t, db, 2, "二", 20, 2002)

	svc := NewGuideService(db, scopeSvc)
	page, err := svc.ListGuides(bizScopeCtx(bizScopeCaller(1001, "role_biz_scope")), GuideListQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total)
}

// TestBizScope_默认规则组也管到业务表
//
// ⚠️ **"就近覆盖"** —— 业务域没单独配规则时回落到默认规则组(module 为空串的那一组)。
func TestBizScope_默认规则组也管到业务表(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	bizScopeSeedRule(t, db, 9101, "", "self")

	bizScopeGuide(t, db, 1, "我的", 10, 1001)
	bizScopeGuide(t, db, 2, "别人的", 10, 2002)

	svc := NewGuideService(db, scopeSvc)
	page, err := svc.ListGuides(bizScopeCtx(bizScopeCaller(1001, "role_biz_scope")), GuideListQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "默认规则组也要能管到业务表")
}

// TestBizScope_多条规则之间是或
func TestBizScope_多条规则之间是或(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	bizScopeSeedRule(t, db, 9101, "guide", "project")
	bizScopeSeedRule(t, db, 9101, "guide", "self")

	bizScopeGuide(t, db, 1, "本项目", 10, 9999)
	bizScopeGuide(t, db, 2, "我自己建的", 99, 1001)
	bizScopeGuide(t, db, 3, "都不沾", 99, 9999)

	svc := NewGuideService(db, scopeSvc)
	caller := bizScopeCaller(1001, "role_biz_scope")
	caller.ProjectIDs = []uint64{10}
	page, err := svc.ListGuides(bizScopeCtx(caller), GuideListQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total, "「本项目 或 仅本人」⇒ 两行都看得见,第三行看不见")
}

// TestBizScope_单条也受管_按id直取拿不到别人的行
//
// ⚠️ **只过滤列表的话,用户可以按 id 打开一条他看不见的记录** ——
// 那等于行级权限只挡了「翻页」,没挡「直取」。
func TestBizScope_单条也受管_按id直取拿不到别人的行(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	bizScopeSeedRule(t, db, 9101, "guide", "self")

	bizScopeGuide(t, db, 1, "我的", 10, 1001)
	bizScopeGuide(t, db, 2, "别人的", 10, 2002)

	svc := NewGuideService(db, scopeSvc)
	ctx := bizScopeCtx(bizScopeCaller(1001, "role_biz_scope"))

	got, err := svc.GetGuide(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "我的", got.Title)

	_, err = svc.GetGuide(ctx, 2)
	require.Error(t, err, "别人的行必须按 id 也拿不到")
	assert.Equal(t, errcode.NotFound, errcode.CodeOf(err))
}

// TestBizScope_维度落不到表上_响亮报错
//
// ⚠️ **有的表没有归属人列** —— 「仅本人」在上面**表达不出来**。
// ⇒ 报错,而不是静默跳过那一条子句(跳过会让可见集与管理员配的不一致)。
//
// 用一条临时登记(spec 只有 project 一维)模拟"这张表没有 created_by"。
func TestBizScope_维度落不到表上_响亮报错(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	bizScopeSeedRule(t, db, 9101, "temp_dimtest", "self")

	orig := bizScopeSpecs
	bizScopeSpecs = append(append([]bizScopeSpec{}, orig...), bizScopeSpec{
		module: "temp_dimtest", table: "biz_temp",
		project: "project_id IN ?", // 没有 self 模板
	})
	t.Cleanup(func() { bizScopeSpecs = orig })

	_, err := applyBizScope(bizScopeCtx(bizScopeCaller(1001, "role_biz_scope")),
		scopeSvc, db.Model(&model.Guide{}), "temp_dimtest")
	require.Error(t, err, "表达不出来的维度必须响亮报错")
	assert.Contains(t, err.Error(), "temp_dimtest", "报错要点名是哪个业务域")
	assert.Contains(t, err.Error(), "biz_temp", "报错要点名是哪张表")
}

// TestBizScope_调用方没有部门_本部门匹配不到任何行
//
// ⚠️ **判据是"解析出来的取值",不是"规则里写了什么"** ——
// 调用方一个部门都没有 ⇒ 这一维不产生子句 ⇒ 匹配不到任何行(**安全方向**)。
// 如果这里退化成"不过滤",他就看到了全部数据 —— 那是安全事故。
func TestBizScope_调用方没有部门_本部门匹配不到任何行(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	bizScopeSeedRule(t, db, 9101, "guide", "dept")

	bizScopeGuide(t, db, 1, "别人的", 10, 2002)

	svc := NewGuideService(db, scopeSvc)
	page, err := svc.ListGuides(bizScopeCtx(bizScopeCaller(1001, "role_biz_scope")), GuideListQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total, "调用方没有部门 ⇒ 看不到任何行(不是看到全部)")
}

// TestBizScope_业务域没登记_响亮报错
//
// ⚠️ **service 里写错业务域字符串 = 编程错误** ——
// 不能"安静地不过滤",那正是这层要治的病。
func TestBizScope_业务域没登记_响亮报错(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	bizScopeSeedRule(t, db, 9101, "guide", "self")

	_, err := applyBizScope(bizScopeCtx(bizScopeCaller(1001, "role_biz_scope")),
		scopeSvc, db.Model(&model.Guide{}), "查无此域")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查无此域")
}

// ===== 登记表自身的自检 =====

// TestBizScopeSpecs_每条登记都有表名与业务域
func TestBizScopeSpecs_每条登记都有表名与业务域(t *testing.T) {
	seen := map[string]bool{}
	for _, sp := range bizScopeSpecs {
		require.NotEmpty(t, sp.module, "业务域不能为空")
		require.NotEmpty(t, sp.table, "表名不能为空(错误信息里要给人看)")
		require.False(t, seen[sp.module], "业务域 %s 登记了两次", sp.module)
		seen[sp.module] = true
	}
	assert.NotEmpty(t, bizScopeSpecs)
}

// TestBizScopeSpecs_列模板各带恰好一个问号
//
// ⚠️ **问号个数不对 ⇒ 参数串位** —— 那不报错,只是查出错的数据。
func TestBizScopeSpecs_列模板各带恰好一个问号(t *testing.T) {
	for _, sp := range bizScopeSpecs {
		for _, tmpl := range []string{sp.project, sp.dept, sp.self} {
			if tmpl == "" {
				continue
			}
			assert.Equal(t, 1, countQ(tmpl), "业务域 %s 的列模板 %q 必须恰好一个 ?", sp.module, tmpl)
		}
	}
}

// countQ 数问号
func countQ(s string) int {
	n := 0
	for _, r := range s {
		if r == '?' {
			n++
		}
	}
	return n
}

// TestScopeModuleSupported_未登记的业务域不算接上了
func TestScopeModuleSupported_未登记的业务域不算接上了(t *testing.T) {
	_, ok := scopeModuleSupported("根本没有这个业务域")
	assert.False(t, ok)

	types, ok := scopeModuleSupported("guide")
	assert.True(t, ok)
	assert.Contains(t, types, string(scope.TypeProject), "指南表有 project_id")
	assert.Contains(t, types, string(scope.TypeSelf), "指南表有 created_by")
	assert.Contains(t, types, string(scope.TypeDept))
}

// TestScopeModuleSupported_动态集合只支持归属人一维
//
// ⚠️ **动态集合的物理表是 `dyn_<编码>`** ——
// 列只有 id / owner_id / created_at / updated_at / deleted_at + f_<字段>,
// **没有 project_id、也没有 dept_id**。
//
// ⇒ 它只支持「全部数据」「仅本人」「自定义」。
func TestScopeModuleSupported_动态集合只支持归属人一维(t *testing.T) {
	types, ok := scopeModuleSupported("dyn:巡检记录")
	require.True(t, ok, "动态集合的数据面业务域必须被认领")
	assert.Contains(t, types, string(scope.TypeSelf))
	assert.Contains(t, types, string(scope.TypeAll))
	assert.Contains(t, types, string(scope.TypeCustom))
	assert.NotContains(t, types, string(scope.TypeProject), "动态表没有 project_id")
	assert.NotContains(t, types, string(scope.TypeDept), "动态表没有 dept_id")
}

// ===== 配置期的校验 =====

// TestReplaceRoleScopes_不支持的维度被拒
//
// ⚠️ **配的时候就要说清楚** ——
// 等到用这条规则的人查询时才炸,那时候管理员已经走了。
//
// 未登记的业务域(guide 之外什么都没接)配任何维度都该被拒。
func TestReplaceRoleScopes_不支持的维度被拒(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)
	scope.RegisterModule("news") // 接了路由,没接数据范围

	err := scopeSvc.ReplaceRoleScopes(context.Background(), 9101, "news",
		[]ScopeRuleInput{{ScopeType: "self"}})
	require.Error(t, err)
	assert.Equal(t, errcode.BadRequest, errcode.CodeOf(err))
	assert.Contains(t, err.Error(), "news")
}

// TestReplaceRoleScopes_没有数据范围的业务域被拒
//
// ⚠️ **平台域(角色/字典/配置……)配了也不生效** ——
// 与"业务域拼错一个字符"是同一类失败,只是更隐蔽(拼错会报错,这个不报错)。
func TestReplaceRoleScopes_没有数据范围的业务域被拒(t *testing.T) {
	db, scopeSvc := newBizScopeFixture(t)
	bizScopeSeedRole(t, db, 9101, "role_biz_scope")
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)
	scope.RegisterModule("role")

	err := scopeSvc.ReplaceRoleScopes(context.Background(), 9101, "role",
		[]ScopeRuleInput{{ScopeType: "self"}})
	require.Error(t, err)
	assert.Equal(t, errcode.BadRequest, errcode.CodeOf(err))
	assert.Contains(t, err.Error(), "role")
}

// TestListModules_只列接了数据范围的业务域
func TestListModules_只列接了数据范围的业务域(t *testing.T) {
	_, scopeSvc := newBizScopeFixture(t)
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)
	scope.RegisterModule("guide") // 接了
	scope.RegisterModule("user")  // 走 ctx 业务域那条路,也算接了
	scope.RegisterModule("role")  // 平台域,没接

	mods, err := scopeSvc.ListModules(context.Background())
	require.NoError(t, err)

	names := make([]string, 0, len(mods))
	for _, m := range mods {
		names = append(names, m.Module)
	}
	assert.Contains(t, names, "guide")
	assert.Contains(t, names, "user")
	assert.NotContains(t, names, "role", "没接数据范围的业务域不该出现在候选里")
	assert.Contains(t, names, "", "默认规则组必须在")
}

// TestListModules_每个候选都报得出自己支持哪些维度
//
// ⚠️ **前端靠这个字段过滤维度下拉框** ——
// 不给的话前端只能给全部六种,管理员就会配出"后端必然拒"的组合,
// 点保存才报错。
func TestListModules_每个候选都报得出自己支持哪些维度(t *testing.T) {
	_, scopeSvc := newBizScopeFixture(t)
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)
	scope.RegisterModule("guide")

	mods, err := scopeSvc.ListModules(context.Background())
	require.NoError(t, err)

	byModule := map[string][]string{}
	for _, m := range mods {
		byModule[m.Module] = m.SupportedTypes
	}
	require.NotEmpty(t, byModule["guide"])
	assert.Contains(t, byModule["guide"], "project", "指南表有 project_id")
	assert.Contains(t, byModule["guide"], "self", "指南表有 created_by")
	assert.Contains(t, byModule["guide"], "dept", "指南表有 created_by 可反查部门")
}

// TestListModules_默认规则组给的是所有业务域的交集
//
// ⚠️ **默认组的规则会在每个业务域上各自求值** ——
// 取并集就会出现"某些表表达不出来"的维度 ⇒ 那些业务域的查询直接报错。
//
// ⇒ 默认组必须是交集。
func TestListModules_默认规则组给的是所有业务域的交集(t *testing.T) {
	_, scopeSvc := newBizScopeFixture(t)
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)
	scope.RegisterModule("guide")     // project/dept/self 全支持
	scope.RegisterModule("dyn:示例集合") // 只有 all/self/custom

	mods, err := scopeSvc.ListModules(context.Background())
	require.NoError(t, err)

	var def *ScopeModuleView
	for i := range mods {
		if mods[i].Module == "" {
			def = &mods[i]
			break
		}
	}
	require.NotNil(t, def, "默认规则组必须在候选里")
	assert.Contains(t, def.SupportedTypes, "all")
	assert.Contains(t, def.SupportedTypes, "self", "两个域都支持仅本人 ⇒ 交集里有")
	assert.NotContains(t, def.SupportedTypes, "project", "动态表不支持本项目 ⇒ 交集里不能有")
	assert.NotContains(t, def.SupportedTypes, "dept", "动态表不支持本部门 ⇒ 交集里不能有")
}

// TestListModules_默认规则组在没有任何业务域时给全部维度
//
// ⚠️ **不能变成"什么都配不了"** ——
// 一个业务域都没有时,默认组是唯一能配的东西,
// 此时把维度清空 = 管理员一个规则都配不了(与 dyn 那边同一条教训)。
func TestListModules_默认规则组在没有任何业务域时给全部维度(t *testing.T) {
	_, scopeSvc := newBizScopeFixture(t)
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)

	mods, err := scopeSvc.ListModules(context.Background())
	require.NoError(t, err)

	require.NotEmpty(t, mods)
	assert.Len(t, mods[0].SupportedTypes, len(scope.AllTypes()),
		"没有任何业务域时,默认组要能配全部维度")
}

// TestCheckScopeWiring_漏登记的业务域被抓出来
//
// ⚠️ **这是清单 §1.13 #2 要的那句"不声明 ⇒ 启动失败"** ——
// 路由层新加了一个业务域却忘了在业务查询里接数据范围,
// 后台配的规则对它永远不生效,而且没有任何报错。
func TestCheckScopeWiring_漏登记的业务域被抓出来(t *testing.T) {
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)

	scope.RegisterModule("guide") // 接了
	scope.RegisterModule("role")  // 平台域,在账本里
	require.NoError(t, CheckScopeWiring())

	scope.RegisterModule("新闻") // 谁都没登记
	err := CheckScopeWiring()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "新闻")
}

// TestBizScopeUnwired_每一项都写了原因
//
// ⚠️ **账本不是逃逸名单** —— 每一项都必须说清"为什么它没有行级范围"。
func TestBizScopeUnwired_每一项都写了原因(t *testing.T) {
	require.NotEmpty(t, bizScopeUnwired)
	for m, reason := range bizScopeUnwired {
		assert.NotEmpty(t, reason, "业务域 %s 必须写清为什么没有行级数据范围", m)
		assert.GreaterOrEqual(t, len(reason), 8, "业务域 %s 的原因太短,像是没写", m)
	}
	// 已接上的业务域不该同时出现在"没接"的账本里
	for _, sp := range bizScopeSpecs {
		assert.NotContains(t, bizScopeUnwired, sp.module)
	}
	for m := range bizScopeCtxWired {
		assert.NotContains(t, bizScopeUnwired, m)
	}
}
