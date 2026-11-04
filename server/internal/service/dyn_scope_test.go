// 动态业务集合 × 数据范围(行级权限)的接线单测 —— M160(2026-11-02)
//
// ⚠️ **这批用例守的是"看不见的行"** ——
// 数据范围这层最贵的失败模式不是"过滤过头"(用户看不到该看的,会来投诉),
// 而是"过滤过头被当成过滤不够"(用户看到了不该看的,**没人会来投诉**)。
//
// ⇒ 每条用例都断言**看不到的那一行确实看不到**,而不是只断言"该看的那行看得见"。
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
)

// dynScopeTestModels 这批用例要建的表
//
// ⚠️ **多了两张表,都是数据范围那条链上真正被查到的** ——//
//   - sys_data_scope:规则存在这里
//   - sys_user_department:scopeService.actorFor 要查它(算"本部门")
//
// 少建一张 ⇒ 用例炸在"no such table"上,而不是炸在断言上 ——
// 那样的红是**假红**,读的人会以为是自己测的东西坏了。
var dynScopeTestModels = []any{
	&model.SysDynCollection{},
	&model.SysDynField{},
	&model.SysResource{},
	&model.SysRole{},
	&model.SysRoleResource{},
	&model.SysUserDepartment{},
	&model.SysDataScope{},
}

// newDynScopeFixture 起一个内存库 + 装了数据范围服务的 dynService
func newDynScopeFixture(t *testing.T) (*gorm.DB, DynService) {
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
	require.NoError(t, db.AutoMigrate(dynScopeTestModels...))
	// ⚠️ **safe 传 nil** —— 没有 Redis / cache.mode=off 时就是退化成每次回源,
	// **不改变函数的成功/失败语义**(与 cachex 全体一致)。
	return db, NewDynService(db, NewScopeService(db, nil), nil, nil)
}

// dynScopeSeedRole 建一个角色
func dynScopeSeedRole(t *testing.T, db *gorm.DB, id uint64, code string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysRole{
		BaseModel: model.BaseModel{ID: id},
		Code:      code,
		Name:      code,
		IsActive:  true,
	}).Error)
}

// dynScopeSeedRule 写一条数据范围规则
//
// ⚠️ **module 为空串 = 默认规则组** —— 某业务域没单独配时回落到它。
func dynScopeSeedRule(t *testing.T, db *gorm.DB, roleID uint64, module, scopeType string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysDataScope{
		RoleID:    roleID,
		Module:    module,
		ScopeType: scopeType,
	}).Error)
}

// dynScopeCaller 造一个带角色码的调用者
func dynScopeCaller(userID uint64, roleCode string) *Caller {
	return &Caller{UserID: userID, RoleCode: roleCode}
}

// dynScopeSeedCollection 建一个集合 + 一个文本字段
func dynScopeSeedCollection(t *testing.T, svc DynService, code string) {
	t.Helper()
	caller := dynScopeCaller(1001, "role_dyn_scope")
	cid, err := svc.CreateCollection(context.Background(), caller, &DynCollectionInput{
		Code: code, Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), caller, cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)
}

// TestDyn_数据范围_仅本人_只看自己建的记录
//
// ⚠️ **这条是本文件的主用例** ——
// 角色在 `dyn:inspection` 上配了「仅本人」⇒ 列表里**只有他自己建的行**。
func TestDyn_数据范围_仅本人_只看自己建的记录(t *testing.T) {
	db, svc := newDynScopeFixture(t)
	dynScopeSeedRole(t, db, 9101, "role_dyn_scope")
	dynScopeSeedRule(t, db, 9101, "dyn:inspection", "self")
	dynScopeSeedCollection(t, svc, "inspection")

	// 两个用户各建一条
	mine, err := svc.CreateRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "我的"})
	require.NoError(t, err)
	other, err := svc.CreateRecord(context.Background(), dynScopeCaller(2002, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "别人的"})
	require.NoError(t, err)
	require.NotEqual(t, mine, other)

	// 1001 只看得见自己那条
	page, err := svc.ListRecords(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", DynRecordQuery{})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total, "「仅本人」下只能看到自己建的记录")
	list, ok := page.List.([]map[string]any)
	require.True(t, ok)
	require.Len(t, list, 1)
	assert.Equal(t, "我的", list[0]["device_name"])

	// 2002 也只看得见自己那条
	page, err = svc.ListRecords(context.Background(), dynScopeCaller(2002, "role_dyn_scope"),
		"inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total)
}

// TestDyn_数据范围_没配规则_不过滤
//
// ⚠️ **这条守"接上线之后没有把默认行为改严"** ——
// 规则层没配 = 不过滤 = 全部数据。
// 如果哪天改成"没配 = 看不到任何行",dyn 的列表会在**上线当天**全空。
func TestDyn_数据范围_没配规则_不过滤(t *testing.T) {
	db, svc := newDynScopeFixture(t)
	dynScopeSeedRole(t, db, 9102, "role_dyn_scope")
	dynScopeSeedCollection(t, svc, "inspection")

	_, err := svc.CreateRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "甲"})
	require.NoError(t, err)
	_, err = svc.CreateRecord(context.Background(), dynScopeCaller(2002, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "乙"})
	require.NoError(t, err)

	// 角色在库里,但**没配任何规则** ⇒ 不过滤
	page, err := svc.ListRecords(context.Background(), dynScopeCaller(3003, "role_dyn_scope"),
		"inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total, "没配规则 = 不过滤 = 全部数据")
}

// TestDyn_数据范围_默认规则组也管到dyn
//
// ⚠️ **默认组(module="")是"这个角色在所有业务域上的默认行级权限"** ——
// dyn:inspection 没单独配 ⇒ 回落到默认组。
// 这条用例守的是那个回落真的发生了(而不是"dyn 集合天生不受管")。
func TestDyn_数据范围_默认规则组也管到dyn(t *testing.T) {
	db, svc := newDynScopeFixture(t)
	dynScopeSeedRole(t, db, 9103, "role_dyn_scope")
	dynScopeSeedRule(t, db, 9103, "", "self") // 默认规则组
	dynScopeSeedCollection(t, svc, "inspection")

	_, err := svc.CreateRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "甲"})
	require.NoError(t, err)
	_, err = svc.CreateRecord(context.Background(), dynScopeCaller(2002, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "乙"})
	require.NoError(t, err)

	page, err := svc.ListRecords(context.Background(), dynScopeCaller(3003, "role_dyn_scope"),
		"inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total,
		"默认规则组配了「仅本人」,调用方看不到别人的行(自己也一条没建)")
}

// TestDyn_数据范围_本项目_响亮报错而不是静默放过
//
// ⚠️ **这是本文件最要紧的一条** ——
// dyn 物理表**没有 project_id 列** ⇒ 「本项目」在 dyn 上表达不出来。
//
// 表达不出来有三种处理:跳过那一条子句 / 退化成不过滤 / 报错。
// 前两种在"或"组里都会让可见集**变宽** ⇒ **数据越权**。
// ⇒ 必须是报错。
func TestDyn_数据范围_本项目_响亮报错而不是静默放过(t *testing.T) {
	db, svc := newDynScopeFixture(t)
	dynScopeSeedRole(t, db, 9104, "role_dyn_scope")
	dynScopeSeedRule(t, db, 9104, "dyn:inspection", "project")
	dynScopeSeedCollection(t, svc, "inspection")

	_, err := svc.CreateRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "甲"})
	require.NoError(t, err)

	// 调用方有项目身份 ⇒ 这一维会产生子句 ⇒ 落不到这张表上 ⇒ 报错
	caller := dynScopeCaller(3003, "role_dyn_scope")
	caller.ProjectIDs = []uint64{10}
	_, err = svc.ListRecords(context.Background(), caller, "inspection", DynRecordQuery{})
	require.Error(t, err, "「本项目」在 dyn 上表达不出来,必须报错")
	assert.Contains(t, err.Error(), "本项目")
	assert.Contains(t, err.Error(), "inspection")
	// ⚠️ **必须断言这句独有的话** ——
	// pkg/scope 层的通用报错("本项目范围落不到这张表上")也含"本项目"两个字,
	// 只断言那两个字的话,哪天 dyn 这一层的提示没了、只剩通用报错,这条用例照样绿。
	assert.Contains(t, err.Error(), "没有项目/部门维度的列", "要的是 dyn 这一层自己的提示")

	// ⚠️ **详情路径也要报** —— 只在列表报错的话,用户绕过去按 id 打开还是能看
	_, err = svc.GetRecord(context.Background(), caller, "inspection", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "没有项目/部门维度的列")
}

// TestDyn_数据范围_单条也受管_不能按id直取
//
// ⚠️ **列表挡住了、直取没挡住 = 行级权限只挡了"翻页"** ——
// 用户照着别人的 id 调 GetRecord 照样能拿到那一行。
func TestDyn_数据范围_单条也受管_不能按id直取(t *testing.T) {
	db, svc := newDynScopeFixture(t)
	dynScopeSeedRole(t, db, 9105, "role_dyn_scope")
	dynScopeSeedRule(t, db, 9105, "dyn:inspection", "self")
	dynScopeSeedCollection(t, svc, "inspection")

	rid, err := svc.CreateRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "甲的"})
	require.NoError(t, err)

	caller := dynScopeCaller(2002, "role_dyn_scope")

	// 详情:按"不存在"处理(不是 403 —— 403 会泄漏"这个 id 确实存在")
	_, err = svc.GetRecord(context.Background(), caller, "inspection", rid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "记录不存在")

	// 改:同样看不见
	err = svc.UpdateRecord(context.Background(), caller, "inspection", rid,
		map[string]any{"device_name": "乙改的"})
	require.Error(t, err)

	// 删:同样看不见
	err = svc.DeleteRecord(context.Background(), caller, "inspection", rid)
	require.Error(t, err)

	// ⚠️ **原记录必须还在** —— 上面三步如果真的改了/删了,这条断言会红
	rec, err := svc.GetRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", rid)
	require.NoError(t, err)
	assert.Equal(t, "甲的", rec["device_name"])
}

// TestDyn_数据范围_与关键字和owner过滤并存时结果不错
//
// ⚠️ **这条守的是参数顺序** ——
// 数据范围片段带自己的参数(owner_id),它跟关键字(LIKE ?)、
// owner 过滤(= ?)的参数混在同一条 SQL 里。
// **参数错位不报错**,只是查出错的数据(甚至查到别人的行)。
//
// ⇒ 这里让三种条件同时存在,并断言"该出现的那行出现了、别的行没出现"。
func TestDyn_数据范围_与关键字和owner过滤并存时结果不错(t *testing.T) {
	db, svc := newDynScopeFixture(t)
	dynScopeSeedRole(t, db, 9106, "role_dyn_scope")
	dynScopeSeedRule(t, db, 9106, "dyn:inspection", "self")
	dynScopeSeedCollection(t, svc, "inspection")

	// 1001 建两条,2002 建一条
	_, err := svc.CreateRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "电梯甲"})
	require.NoError(t, err)
	_, err = svc.CreateRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "水泵甲"})
	require.NoError(t, err)
	_, err = svc.CreateRecord(context.Background(), dynScopeCaller(2002, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "电梯乙"})
	require.NoError(t, err)

	caller := dynScopeCaller(2002, "role_dyn_scope")

	// 关键字 + 数据范围:只该看到 2002 自己的那条"电梯乙"
	page, err := svc.ListRecords(context.Background(), caller, "inspection",
		DynRecordQuery{Keyword: "电梯"})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total, "关键字 + 数据范围,只该命中自己那条")
	list, ok := page.List.([]map[string]any)
	require.True(t, ok)
	assert.Equal(t, "电梯乙", list[0]["device_name"])

	// 关键字 + owner 过滤 + 数据范围:owner 指向别人的 ⇒ 与数据范围矛盾 ⇒ 零行
	page, err = svc.ListRecords(context.Background(), caller, "inspection",
		DynRecordQuery{Keyword: "电梯", Owner: "1001"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total,
		"owner 过滤指向别人的行,又与「仅本人」矛盾 ⇒ 一条都不该命中")

	// owner 过滤指向自己 + 关键字 ⇒ 命中自己那条
	page, err = svc.ListRecords(context.Background(), caller, "inspection",
		DynRecordQuery{Keyword: "电梯", Owner: "2002"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total)
}

// TestDyn_数据范围_超管不受owner_scoped限制
//
// ⚠️ **两条行级闸门是"且",不是"或"** ——
// owner_scoped:集合设计里的开关(只对非超管生效)
// 数据范围「仅本人」:角色上的规则(跟是不是超管无关)
//
// 这条用例守 owner_scoped 那一半:超管看得见别人的行。
// (数据范围那一半在下面那条用例里。)
func TestDyn_数据范围_超管不受owner_scoped限制(t *testing.T) {
	db, svc := newDynScopeFixture(t)
	dynScopeSeedRole(t, db, 9107, "role_dyn_scope")
	caller := dynScopeCaller(1001, "role_dyn_scope")
	cid, err := svc.CreateCollection(context.Background(), caller, &DynCollectionInput{
		Code: "inspection", Name: "巡检记录", OwnerScoped: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), caller, cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)

	_, err = svc.CreateRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "甲"})
	require.NoError(t, err)

	// 超管:owner_scoped 放行 ⇒ 看得见
	super := &Caller{UserID: 3003, RoleCode: model.RoleSuperAdmin}
	page, err := svc.ListRecords(context.Background(), super, "inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "超管不受 owner_scoped 限制")

	// 普通用户:owner_scoped 收窄 ⇒ 看不见
	page, err = svc.ListRecords(context.Background(), dynScopeCaller(2002, "role_dyn_scope"),
		"inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total, "owner_scoped 的集合,别人看不见")
}

// TestDyn_数据范围_规则配在超管角色上也生效
//
// ⚠️ **数据范围这层不因超管而豁免** ——
// 它是"角色上的规则":管理员给超管这个角色配了「仅本人」,那就生效。
//
// ⚠️ **这跟 owner_scoped 是两条不同的闸门** ——
// owner_scoped 写死了"超管放行",数据范围没有这个特例。
// 两条混起来就会出现"超管什么都看得见"这种说不清的状态。
func TestDyn_数据范围_规则配在超管角色上也生效(t *testing.T) {
	db, svc := newDynScopeFixture(t)
	dynScopeSeedRole(t, db, 9108, model.RoleSuperAdmin)
	dynScopeSeedRule(t, db, 9108, "dyn:inspection", "self")
	dynScopeSeedCollection(t, svc, "inspection")

	_, err := svc.CreateRecord(context.Background(), dynScopeCaller(1001, "role_dyn_scope"),
		"inspection", map[string]any{"device_name": "甲"})
	require.NoError(t, err)
	_, err = svc.CreateRecord(context.Background(), &Caller{UserID: 3003, RoleCode: model.RoleSuperAdmin},
		"inspection", map[string]any{"device_name": "超管的"})
	require.NoError(t, err)

	// 超管也只看得见自己的那条
	super := &Caller{UserID: 3003, RoleCode: model.RoleSuperAdmin}
	page, err := svc.ListRecords(context.Background(), super, "inspection", DynRecordQuery{})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total, "数据范围这层不因超管而豁免")
	list, ok := page.List.([]map[string]any)
	require.True(t, ok)
	assert.Equal(t, "超管的", list[0]["device_name"])
}
