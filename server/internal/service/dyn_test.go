// 动态业务集合 service 单测(M150)
//
// ⚠️ **这批用例是"拆掉实现看会不会红"的清单,不是覆盖率清单。**
// 每一条都钉住一处能被无声改坏的逻辑 —— 少登记一个资源码、设计面与数据面
// 对不上、软删字段的残留列漏进响应、owner_scoped 过滤失效。
//
// ⚠️ **断言一律查库 / 查物理表,不复用 service 内部状态** ——
// 复用内部变量只能证明变量对,证明不了它落进了 DB。
package service

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/dynx"
)

// dynTestModels 这批用例要建的表
//
// ⚠️ **只建用得到的,不全量 migrate** —— 全量 75 张表会让每个用例都慢一秒,
// 而且与本用例无关的表坏了会搅黄用例(夹具越薄,失败越指向真因)。
var dynTestModels = []any{
	&model.SysDynCollection{},
	&model.SysDynField{},
	&model.SysResource{},
	&model.SysRole{},
	&model.SysRoleResource{},
	// ⚠️ **sys_user 要在** —— 记录的创建人显示名(owner_name)是读的时候
	//   去 sys_user 查出来的。表不在,那一步会失败并降级成"显示 id"
	//   (见 dynOwnerNames 的旁路纪律)⇒ 断言 owner_name 的用例会恒绿。
	&model.SysUser{},
}

// newDynFixture 起一个内存库 + 一个 dynService
func newDynFixture(t *testing.T) (*gorm.DB, DynService) {
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
	require.NoError(t, db.AutoMigrate(dynTestModels...))
	// ⚠️ **scopeSvc 传 nil** —— 这批用例测的是"没装数据范围服务"时
	// 不过滤的既有行为(M160 之前 dyn 就是这个状态)。
	// 数据范围本身的用例见 dyn_scope_test.go。
	return db, NewDynService(db, nil, nil, nil)
}

// dynCaller 造一个调用者
func dynCaller(userID uint64, roleCode string) *Caller {
	return &Caller{UserID: userID, RoleCode: roleCode}
}

// dynRawCol 读物理表的某一列的原始值
//
// ⚠️ **用 Rows().Scan(&any),不用 Scan(&*T)** ——
// "扫进指针的指针"时 NULL 会不会把外层指针置 nil,取决于驱动和 GORM 的版本;
// 而 `*any` 的语义没有歧义:NULL 就是 nil,INTEGER 就是 int64。
//
// ⇒ 这一点很重要:断言"这一列是 NULL"只有在**同一段代码也读得出非 NULL 的值**
// 时才不是恒真的。见下面那两条用例里的对照断言。
func dynRawCol(t *testing.T, db *gorm.DB, sql string, args ...any) any {
	t.Helper()
	rows, err := db.Raw(sql, args...).Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next(), "查不到行")
	var v any
	require.NoError(t, rows.Scan(&v))
	return v
}

// dynSeedRole 建一个角色(资源要绑给某个角色才有 casbin 策略行)
func dynSeedRole(t *testing.T, db *gorm.DB, code string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysRole{
		BaseModel: model.BaseModel{ID: 9000},
		Code:      code,
		Name:      code,
		IsActive:  true,
	}).Error)
}

// TestDyn_建集合_物理表与资源同时到位
//
// ⚠️ **这是整套东西的"入口承诺"** ——
// 用户设计一个业务表之后,应该"菜单自动有、接口自动有、权限自动有"。
// 三样缺一样,症状都是"设计了却用不了",而且不报错。
func TestDyn_建集合_物理表与资源同时到位(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")

	id, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "inspection",
		Name: "巡检记录",
	})
	require.NoError(t, err)
	require.NotZero(t, id)

	// ① 物理表建出来了
	require.True(t, db.Migrator().HasTable("dyn_inspection"), "物理表 dyn_inspection 必须存在")

	// ② 五个 api 资源码 + 一个菜单码 + 一个父菜单码
	var codes []string
	require.NoError(t, db.Model(&model.SysResource{}).Order("code").Pluck("code", &codes).Error)
	assert.Contains(t, codes, "dyn:inspection:list")
	assert.Contains(t, codes, "dyn:inspection:read")
	assert.Contains(t, codes, "dyn:inspection:create")
	assert.Contains(t, codes, "dyn:inspection:update")
	assert.Contains(t, codes, "dyn:inspection:delete")
	assert.Contains(t, codes, "menu.dyn.inspection")
	assert.Contains(t, codes, "menu.dyn")

	// ②b **登记的 api 码恰好等于动作清单里的那几条,一个不多一个不少**
	//
	// ⚠️ 这条钉的是"清单只有一份" ——
	// 2026-11-01 M151 真踩过:handler 侧多写了一个 "schema" 动作
	// (想给表单结构单开一个码),登记侧还是 5 个 ⇒
	// `dyn:<集合编码>:schema` 这条资源码**永远不存在** ⇒
	// 「角色管理」页没有这一行可勾 ⇒ 谁也绑不上 ⇒
	// 表单结构接口对所有非超管一律 403,**而日志里一条错误都没有**。
	// ⇒ 清单收在 internal/pkg/dynx/actions.go,两边引用同一份;
	// 这条断言保证登记侧不会偷偷少登记或多登记。
	var apiCodes []string
	for _, c := range codes {
		if strings.HasPrefix(c, "dyn:inspection:") {
			apiCodes = append(apiCodes, c)
		}
	}
	wantCodes := make([]string, 0, len(dynx.Actions()))
	for _, a := range dynx.Actions() {
		wantCodes = append(wantCodes, dynx.RecordCode("inspection", a))
	}
	sort.Strings(wantCodes)
	sort.Strings(apiCodes)
	assert.Equal(t, wantCodes, apiCodes,
		"登记的 api 资源码必须恰好是动作清单里的那几条 —— 少一条那个接口没人能用,多一条是运营看不懂的死码")

	// ③ 菜单的路径指向 /dyn/<code>,父节点指向 menu.dyn
	var menu model.SysResource
	require.NoError(t, db.Where("code = ?", "menu.dyn.inspection").First(&menu).Error)
	assert.Equal(t, "/dyn/inspection", menu.Path)
	var parent model.SysResource
	require.NoError(t, db.Where("code = ?", "menu.dyn").First(&parent).Error)
	assert.Equal(t, parent.ID, menu.ParentID)

	// ④ 资源绑给了创建者当前的角色 —— 不绑的话菜单不会出现(且不报错)
	var bindings int64
	require.NoError(t, db.Model(&model.SysRoleResource{}).
		Where("resource_id IN (?)", db.Model(&model.SysResource{}).
			Select("id").Where("code IN ?", []string{"dyn:inspection:list", "menu.dyn.inspection", "menu.dyn"})).
		Count(&bindings).Error)
	assert.Equal(t, int64(3), bindings, "五个 api 码 + 菜单 + 父菜单都要绑给创建者的角色")
}

// TestDyn_建集合_重名拒绝
func TestDyn_建集合_重名拒绝(t *testing.T) {
	_, svc := newDynFixture(t)
	_, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录2",
	})
	require.Error(t, err, "同一编码不能建两次")
}

// TestDyn_建字段_加列后能写能读
func TestDyn_建字段_加列后能写能读(t *testing.T) {
	db, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)

	fid, err := svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text", Required: true,
	})
	require.NoError(t, err)
	require.NotZero(t, fid)

	// 列真的加上了
	require.True(t, db.Migrator().HasColumn("dyn_inspection", "f_device_name"),
		"物理列 f_device_name 必须存在")

	// 写一条读一条
	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "3 号楼电梯",
	})
	require.NoError(t, err)
	require.NotZero(t, rid)

	rec, err := svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Equal(t, "3 号楼电梯", rec["device_name"])
	// 键是**字段标识**,不是物理列名
	assert.NotContains(t, rec, "f_device_name")
}

// TestDyn_记录_创建人显示名
//
// ⚠️ **owner_name 是读的时候查出来的,不是物理列** ——
//   物理表上只有 owner_id。列表页要显示"谁建的",显示名必须在读侧补。
//
// ⚠️ **显示名口径:真实姓名 → 昵称 → 账号** ——
//   只取 real_name 的话,后台账号的 real_name 长期是空的 ⇒ 这一列永远是空白
//   (同 config_center_impl.go 的 operatorNameSQL)。
func TestDyn_记录_创建人显示名(t *testing.T) {
	db, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)

	// 三个用户,分别对应三级 fallback
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel: model.BaseModel{ID: 1001},
		Username:  "u_full", Nickname: "昵称甲", RealName: "真实甲",
	}).Error)
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel: model.BaseModel{ID: 1002},
		Username:  "u_nick", Nickname: "昵称乙", RealName: "",
	}).Error)
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel: model.BaseModel{ID: 1003},
		Username:  "u_only", Nickname: "", RealName: "",
	}).Error)

	newRecord := func(uid uint64) map[string]any {
		rid, err := svc.CreateRecord(context.Background(), dynCaller(uid, "role_x"), "inspection", map[string]any{
			"device_name": "x",
		})
		require.NoError(t, err)
		rec, err := svc.GetRecord(context.Background(), dynCaller(uid, "role_x"), "inspection", rid)
		require.NoError(t, err)
		return rec
	}

	// 真实姓名优先
	rec := newRecord(1001)
	assert.Equal(t, "真实甲", rec["owner_name"])
	assert.Equal(t, "1001", rec["owner_id"])

	// 真实姓名为空 → 昵称
	rec = newRecord(1002)
	assert.Equal(t, "昵称乙", rec["owner_name"])

	// 两个都空 → 账号
	rec = newRecord(1003)
	assert.Equal(t, "u_only", rec["owner_name"])

	// ⚠️ **列表也要有** —— 页面上"创建人"那一列读的是列表接口的响应
	page, err := svc.ListRecords(context.Background(), dynCaller(1001, "role_x"), "inspection", DynRecordQuery{})
	require.NoError(t, err)
	list, ok := page.List.([]map[string]any)
	require.True(t, ok, "列表类型不对")
	require.Len(t, list, 3)
	byOwner := map[string]any{}
	for _, item := range list {
		byOwner[item["owner_id"].(string)] = item["owner_name"]
	}
	assert.Equal(t, "真实甲", byOwner["1001"])
	assert.Equal(t, "昵称乙", byOwner["1002"])
	assert.Equal(t, "u_only", byOwner["1003"])
}

// TestDyn_记录_创建人查不到时降级
//
// ⚠️ **查不到显示名不许让整个请求失败** ——
//   owner_id 指向的用户被删了 / id 是 0,显示名就是空串,
//   前端回落到显示 owner_id。记录本身查得到,就还是查得到。
func TestDyn_记录_创建人查不到时降级(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)

	// ⚠️ **故意不建 sys_user 行** —— owner_id 指向一个不存在的人
	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "x",
	})
	require.NoError(t, err)

	rec, err := svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Equal(t, "", rec["owner_name"])
	assert.Equal(t, "1001", rec["owner_id"])
}

// TestDyn_记录_未知字段报错
//
// ⚠️ **不能静默丢弃** —— 静默丢弃的话,用户改了个字段名提交,
// 后端返回"成功",而数据根本没存进去。
func TestDyn_记录_未知字段报错(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)

	_, err = svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "x", "typo_field": "y",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "typo_field")
}

// TestDyn_记录_必填校验
func TestDyn_记录_必填校验(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text", Required: true,
	})
	require.NoError(t, err)

	_, err = svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "设备名称")
}

// TestDyn_记录_类型归一化
//
// ⚠️ **布尔不能被静默当成 false** —— 用户传 "开" 应该是报错,不是 false。
func TestDyn_记录_类型归一化(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	for _, f := range []DynFieldInput{
		{FieldKey: "count", Label: "数量", Type: "int"},
		{FieldKey: "passed", Label: "是否合格", Type: "bool"},
		{FieldKey: "day", Label: "日期", Type: "date"},
		{FieldKey: "level", Label: "等级", Type: "select", Options: `{"choices":[{"value":"a","label":"甲"}]}`},
	} {
		_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &f)
		require.NoError(t, err, f.FieldKey)
	}

	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"count": "3", "passed": "yes", "day": "2026-11-01", "level": "a",
	})
	require.NoError(t, err)
	rec, err := svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Equal(t, int64(3), rec["count"])
	assert.Equal(t, true, rec["passed"])
	assert.Equal(t, "2026-11-01", rec["day"])
	assert.Equal(t, "a", rec["level"])

	// 单选值不在选项集里 ⇒ 报错
	_, err = svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"level": "zzz",
	})
	require.Error(t, err)

	// 布尔传不认识的值 ⇒ 报错(不是静默 false)
	_, err = svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"passed": "开",
	})
	require.Error(t, err)
}

// TestDyn_记录_改与删(软删)
func TestDyn_记录_改与删(t *testing.T) {
	db, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)

	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "旧名",
	})
	require.NoError(t, err)

	require.NoError(t, svc.UpdateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid,
		map[string]any{"device_name": "新名"}))
	rec, err := svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Equal(t, "新名", rec["device_name"])

	// 软删
	require.NoError(t, svc.DeleteRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid))
	_, err = svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.Error(t, err, "软删之后读不到")

	// ⚠️ **物理行还在**(软删不是物理删)
	var raw int64
	require.NoError(t, db.Table("dyn_inspection").Where("id = ?", rid).Count(&raw).Error)
	assert.Equal(t, int64(1), raw, "软删只写 deleted_at,物理行保留")

	// 列表里也看不见
	page, err := svc.ListRecords(context.Background(), dynCaller(1001, "role_x"), "inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total)
}

// TestDyn_记录_改同值不误报"不存在"
//
// ⚠️ **MySQL 的 affected rows 是"变了行数"** ——
// 改成一样的值,affected=0,但记录是在的。误报 404 是最难查的一类假故障。
func TestDyn_记录_改同值不误报不存在(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)
	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "同名",
	})
	require.NoError(t, err)
	require.NoError(t, svc.UpdateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid,
		map[string]any{"device_name": "同名"}))
}

// TestDyn_记录_ownerScoped_只看自己的
func TestDyn_记录_ownerScoped_只看自己的(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录", OwnerScoped: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)

	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "甲的",
	})
	require.NoError(t, err)

	// 另一个普通用户看不见
	page, err := svc.ListRecords(context.Background(), dynCaller(2002, "role_x"), "inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total)

	// 改和删也看不见(一律按"不存在"处理)
	err = svc.UpdateRecord(context.Background(), dynCaller(2002, "role_x"), "inspection", rid,
		map[string]any{"device_name": "乙改的"})
	require.Error(t, err)
	err = svc.DeleteRecord(context.Background(), dynCaller(2002, "role_x"), "inspection", rid)
	require.Error(t, err)

	// 超管不受 owner_scoped 限制
	page, err = svc.ListRecords(context.Background(), dynCaller(3003, model.RoleSuperAdmin), "inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total)
}

// TestDyn_删字段_软删后不再出现在响应里
//
// ⚠️ **列留着,但读写按"在用字段"过滤** ——
// 否则新记录会把已删字段写成默认值,老记录里那个字段的值还会冒出来。
func TestDyn_删字段_软删后不再出现在响应里(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	f1, err := svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "remark", Label: "备注", Type: "text",
	})
	require.NoError(t, err)

	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "电梯", "remark": "已处理",
	})
	require.NoError(t, err)

	require.NoError(t, svc.DeleteField(context.Background(), dynCaller(1001, "role_x"), f1))
	rec, err := svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.NotContains(t, rec, "device_name", "已删字段不该出现在响应里")
	assert.Contains(t, rec, "remark")
}

// TestDyn_停用集合_菜单跟着停用
func TestDyn_停用集合_菜单跟着停用(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)

	off := false
	require.NoError(t, svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), cid,
		&DynCollectionUpdate{Name: "巡检记录", IsActive: &off}))

	var menu model.SysResource
	require.NoError(t, db.Where("code = ?", "menu.dyn.inspection").First(&menu).Error)
	assert.False(t, menu.IsActive, "停用集合 ⇒ 菜单资源停用")

	// 数据面也不该再通
	_, err = svc.ListRecords(context.Background(), dynCaller(1001, "role_dyn_tester"), "inspection", DynRecordQuery{})
	require.Error(t, err)
}

// TestDyn_重启用集合_菜单回来
//
// ⚠️ **重新启用必须走"重新登记"而不是只翻 is_active** ——
// 只翻 is_active 的话,菜单回来了但资源码没绑给任何角色,点进去就是 403。
func TestDyn_重启用集合_菜单回来(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)

	off := false
	require.NoError(t, svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), cid,
		&DynCollectionUpdate{Name: "巡检记录", IsActive: &off}))
	on := true
	require.NoError(t, svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), cid,
		&DynCollectionUpdate{Name: "巡检记录", IsActive: &on}))

	var menu model.SysResource
	require.NoError(t, db.Where("code = ?", "menu.dyn.inspection").First(&menu).Error)
	assert.True(t, menu.IsActive, "重新启用 ⇒ 菜单资源恢复")

	// 资源码仍在绑定表里
	var bound int64
	require.NoError(t, db.Model(&model.SysRoleResource{}).
		Where("resource_id = ?", menu.ID).Count(&bound).Error)
	assert.Equal(t, int64(1), bound)
}

// TestDyn_改集合名_菜单名跟着改
func TestDyn_改集合名_菜单名跟着改(t *testing.T) {
	db, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	require.NoError(t, svc.UpdateCollection(context.Background(), dynCaller(1001, "role_x"), cid,
		&DynCollectionUpdate{Name: "设备巡检"}))
	var menu model.SysResource
	require.NoError(t, db.Where("code = ?", "menu.dyn.inspection").First(&menu).Error)
	assert.Equal(t, "设备巡检", menu.Name, "集合改名 ⇒ 菜单显示名跟着改")
}

// TestDyn_记录_关键字搜索只搜文本类字段
func TestDyn_记录_关键字搜索只搜文本类字段(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "count", Label: "数量", Type: "int",
	})
	require.NoError(t, err)

	_, err = svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "3 号楼电梯", "count": 5,
	})
	require.NoError(t, err)
	_, err = svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "1 号楼水泵", "count": 7,
	})
	require.NoError(t, err)

	page, err := svc.ListRecords(context.Background(), dynCaller(1001, "role_x"), "inspection",
		DynRecordQuery{Keyword: "电梯"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total)

	// 数字列不参与关键字匹配
	page, err = svc.ListRecords(context.Background(), dynCaller(1001, "role_x"), "inspection",
		DynRecordQuery{Keyword: "5"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total)
}

// TestDyn_记录_选填字段没填落成NULL而不是零值
//
// ⚠️ **"没填"与"填了零值"在业务上是两件事** ——
// 金额 0 元 ≠ 没填金额;数量 0 ≠ 没填数量。
// dynx 的读侧认的是 NULL(= 没填 ⇒ 界面显示"未填"),
// 所以写侧也必须落 NULL,不能拿零值顶替。
//
// ⚠️ **这条钉的是建表口径** ——
// 字段列写成 NOT NULL 时,"没填"这一路直接撞数据库约束:
// NOT NULL constraint failed: dyn_inspection.f_count
// ⇒ 用户**清空一个选填的整数框就存不进去**(前端发的就是 null)。
func TestDyn_记录_选填字段没填落成NULL而不是零值(t *testing.T) {
	db, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "count", Label: "数量", Type: "int",
	})
	require.NoError(t, err)

	// 只给必填之外的文本字段 —— count 没给
	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "3 号楼电梯",
	})
	require.NoError(t, err)

	// ① 接口层读回来是 nil( = 没填),不是 0
	rec, err := svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Nil(t, rec["count"], "没填的选填字段必须读成 nil(= 没填),不能是 0(= 填了 0)")

	// ② 库里那一列真的是 NULL(不是 0)
	assert.Nil(t, dynRawCol(t, db, "SELECT f_count FROM dyn_inspection WHERE id = ?", rid),
		"物理列必须真的存 NULL")

	// ③ 清空(发 null)必须能存进去 —— 以前这里是 500 NOT NULL constraint failed
	require.NoError(t, svc.UpdateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid,
		map[string]any{"count": 5}))

	// ⚠️ **这一条是上面两条的"对照组"** ——
	// 同一个读取方式、同一个列,填了值就读得到 ⇒ 上面那个"读成 nil"是真的 NULL,
	// 而不是"这段读法永远返回 nil"的假象。
	require.Equal(t, int64(5),
		dynRawCol(t, db, "SELECT f_count FROM dyn_inspection WHERE id = ?", rid),
		"填了 5 这一列必须读得到 5 —— 否则上面的 NULL 断言是恒真的")

	require.NoError(t, svc.UpdateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid,
		map[string]any{"count": nil}))
	rec, err = svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Nil(t, rec["count"], "清空之后必须回到 nil")
	assert.Nil(t, dynRawCol(t, db, "SELECT f_count FROM dyn_inspection WHERE id = ?", rid),
		"清空之后物理列也要回到 NULL")
}

// TestDyn_记录_加字段后老行的空列不影响关键字搜索
//
// ⚠️ **NULL 文本列是会真实出现的** ——
// 给一个**已有数据**的集合新加字段时,老行的这一列是 NULL(不是空串)
// (新列可空 ⇒ 加列时老行填 NULL,见 addColumn)。
// ⇒ "按新字段搜"搜不到是应该的(本来就没内容);
// 但"按老字段搜"必须照样搜得到 —— 不能因为有一列是 NULL 就把整行吞掉。
//
// ⚠️ **这条钉的是"用户看得见的行为",不是某一行 SQL** ——
// SQL 的三值逻辑里,`TRUE OR NULL` 仍是 TRUE,`FALSE OR NULL` 是 NULL,
// 而 WHERE 把 NULL 当"不成立" ⇒ 就这一组而言 NULL 与 false 效果一样。
// (所以把 COALESCE 去掉,这条用例**照样是绿的** —— 别拿它当"COALESCE 的证据"。)
// 它防的是**将来**把这一组 AND 到别的条件上时 NULL 传染:那时这条会红。
func TestDyn_记录_加字段后老行的空列不影响关键字搜索(t *testing.T) {
	db, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)

	// 先写一条记录(这时候还没有"备注"这一列)
	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{
		"device_name": "3 号楼电梯",
	})
	require.NoError(t, err)

	// 再加一个字段 ⇒ 老行的这一列是 NULL
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "remark", Label: "备注", Type: "text",
	})
	require.NoError(t, err)

	assert.Nil(t, dynRawCol(t, db, "SELECT f_remark FROM dyn_inspection WHERE id = ?", rid),
		"加字段后老行的这一列必须是 NULL")

	// 老行读出来那一列是 nil(= 没填)
	rec, err := svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Nil(t, rec["remark"])

	// 按**别的**字段搜,这条记录必须照样搜得到
	page, err := svc.ListRecords(context.Background(), dynCaller(1001, "role_x"), "inspection",
		DynRecordQuery{Keyword: "电梯"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total,
		"备注是 NULL 不能让这条记录搜不出来 —— 一列是 NULL 就把整行吞掉是错的")

	// ⚠️ **对照组** —— 同一个读取方式,填了值就读得到。
	// 放在最后:前面的断言都在"这一列是 NULL"的状态下成立,
	// 这条则证明那个 NULL 是**真的读出来了**,不是"这段读法永远返回 nil"。
	require.NoError(t, svc.UpdateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid,
		map[string]any{"remark": "补一句"}))
	assert.Equal(t, "补一句", dynRawCol(t, db, "SELECT f_remark FROM dyn_inspection WHERE id = ?", rid),
		"填了值这一列必须读得到 —— 否则上面的 NULL 断言是恒真的")
}

// TestDyn_记录_排序字段必须是已知字段
func TestDyn_记录_排序字段必须是已知字段(t *testing.T) {
	_, svc := newDynFixture(t)
	_, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.ListRecords(context.Background(), dynCaller(1001, "role_x"), "inspection",
		DynRecordQuery{PageRequest: model.PageRequest{OrderBy: "id; DROP TABLE x"}})
	require.Error(t, err, "排序字段必须走白名单,不能拼进 SQL")
}

// TestDyn_建字段_默认值不合法当场拒绝
//
// ⚠️ **默认值必须当场验,不能等到写记录时才炸** ——
// 默认值是在"新建记录"时被填进去的,那时候用户已经填完一整张表了。
// 一个填错的默认值要等到某个人新建记录时才暴露,那时候已经没人记得
// 是谁填的、也不记得当时想填什么了。
//
// ⚠️ **校验走的是 dynx.Normalize,与写记录是同一套规则** ——
// 自己再写一套判断必然与写入侧漂移(写入侧后来加了一个类型,这边忘了加)。
func TestDyn_建字段_默认值不合法当场拒绝(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		in   DynFieldInput
	}{
		{"整数字段的默认值不是整数", DynFieldInput{
			FieldKey: "count", Label: "数量", Type: "int", DefaultValue: "abc",
		}},
		{"开关字段的默认值认不出来", DynFieldInput{
			FieldKey: "passed", Label: "是否合格", Type: "bool", DefaultValue: "开",
		}},
		{"单选字段的默认值不在选项集里", DynFieldInput{
			FieldKey: "level", Label: "等级", Type: "select",
			Options:      `{"choices":[{"value":"a","label":"甲"}]}`,
			DefaultValue: "zzz",
		}},
		{"JSON 字段的默认值不是合法 JSON", DynFieldInput{
			FieldKey: "extra", Label: "附加", Type: "json", DefaultValue: "{坏掉的",
		}},
		{"日期字段的默认值不是日期", DynFieldInput{
			FieldKey: "day", Label: "日期", Type: "date", DefaultValue: "下周二",
		}},
	} {
		_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &tc.in)
		require.Error(t, err, tc.name)
		assert.Contains(t, err.Error(), "默认值", tc.name)
	}

	// ⚠️ **空串是"没有默认值",不是错** ——
	//   dynx.Normalize 对空串的处理按类型不同(int 会报"需要整数,收到 \"\"")，
	//   所以必须先挡掉空串再交给 Normalize。
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "count", Label: "数量", Type: "int", DefaultValue: "",
	})
	require.NoError(t, err, "空默认值必须放过")
}

// TestDyn_建字段_默认值生效
//
// ⚠️ **默认值不是装饰** —— 请求里**没带**这个字段时,它必须真的被填进去。
func TestDyn_建字段_默认值生效(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "count", Label: "数量", Type: "int", DefaultValue: "42",
	})
	require.NoError(t, err)

	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", map[string]any{})
	require.NoError(t, err)
	rec, err := svc.GetRecord(context.Background(), dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Equal(t, int64(42), rec["count"], "没带这个字段时必须落默认值")
}

// TestDyn_改字段_默认值与选项集也要验
//
// ⚠️ **改字段是唯一能"把设计改坏"的入口** ——
// 类型不可改,但选项集和默认值都能改 ⇒ 这里不验的话,
// "把单选字段的选项全删了"会让之后任何字符串都能存进去,
// 而列表页按选项集翻译显示名时会显示成空白标签(数据已经写进去了)。
func TestDyn_改字段_默认值与选项集也要验(t *testing.T) {
	_, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	fid, err := svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "count", Label: "数量", Type: "int",
	})
	require.NoError(t, err)
	sid, err := svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "level", Label: "等级", Type: "select",
		Options: `{"choices":[{"value":"a","label":"甲"}]}`,
	})
	require.NoError(t, err)

	// 默认值改成认不出来的 → 拒绝
	err = svc.UpdateField(context.Background(), dynCaller(1001, "role_x"), fid, &DynFieldUpdate{
		Label: "数量", DefaultValue: "abc",
	})
	require.Error(t, err, "改默认值也要验")
	assert.Contains(t, err.Error(), "默认值")

	// 把单选字段的选项全删了 → 拒绝
	err = svc.UpdateField(context.Background(), dynCaller(1001, "role_x"), sid, &DynFieldUpdate{
		Label: "等级", Options: "",
	})
	require.Error(t, err, "单选字段不能把选项集清空")
	assert.Contains(t, err.Error(), "选项")

	// 改成合法值 → 放过
	err = svc.UpdateField(context.Background(), dynCaller(1001, "role_x"), fid, &DynFieldUpdate{
		Label: "数量", DefaultValue: "7",
	})
	require.NoError(t, err)
}
