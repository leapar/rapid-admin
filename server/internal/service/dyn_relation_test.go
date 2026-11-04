// 动态集合 × 选项来源(关联系统表)× 附件字段 —— service 层单测(2026-11-02)
//
// ⚠️ **这批用例守的是"看不见的东西"** ——
// 选项来源那层最贵的失败模式不是"多显示了"(用户看到本不该看的选项),
// 而是"该显示的没显示还被当成显示对了"(用户以为系统里就这几个人,
// 于是把一条正经记录填错了,而**没有任何报错**)。
//
// ⇒ 每条用例都断言**读不到的那部分确实读不到**,
// 而不是只断言"读得到的那部分读得到"。
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
	"rapid-admin/internal/pkg/dynx"
)

// dynRelationTestModels 这批用例要建的表
//
// ⚠️ **sys_user / sys_user_department / sys_data_scope 都要在** ——
// 关联表的选项是现查系统表查出来的,表不在就炸在"no such table"上,
// 而不是炸在断言上(那样的红是**假红**)。
var dynRelationTestModels = []any{
	&model.SysDynCollection{},
	&model.SysDynField{},
	&model.SysResource{},
	&model.SysRole{},
	&model.SysRoleResource{},
	&model.SysUser{},
	&model.SysUserDepartment{},
	&model.SysDataScope{},
}

// newDynRelationFixture 起一个内存库 + 一个装了数据范围服务的 dynService
func newDynRelationFixture(t *testing.T) (*gorm.DB, DynService) {
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
	require.NoError(t, db.AutoMigrate(dynRelationTestModels...))
	return db, NewDynService(db, NewScopeService(db, nil), nil, nil)
}

// dynRelCaller 造一个调用者
func dynRelCaller(userID uint64, roleCode string) *Caller {
	return &Caller{UserID: userID, RoleCode: roleCode}
}

// dynRelSeedUser 建一个用户
//
// ⚠️ **is_active 显式补一次 UPDATE** ——
// GORM 对带 `default:true` 的字段,Create 时会**跳过零值**
// ⇒ `IsActive: false` 会被当成"没填"而落进数据库默认值 true。
// 那会让"停用"这条用例恒绿(停用的用户其实一直是启用状态)。
func dynRelSeedUser(t *testing.T, db *gorm.DB, id uint64, username, realName string, active bool) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel: model.BaseModel{ID: id},
		Username:  username,
		Nickname:  username + "_nick",
		RealName:  realName,
		IsActive:  active,
	}).Error)
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", id).
		Update("is_active", active).Error)
}

// dynRelSeedRole 建一个角色
func dynRelSeedRole(t *testing.T, db *gorm.DB, id uint64, code string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysRole{
		BaseModel: model.BaseModel{ID: id},
		Code:      code,
		Name:      code,
		IsActive:  true,
	}).Error)
}

// dynRelSeedScopeRule 写一条数据范围规则
func dynRelSeedScopeRule(t *testing.T, db *gorm.DB, roleID uint64, module, scopeType string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysDataScope{
		RoleID:    roleID,
		Module:    module,
		ScopeType: scopeType,
	}).Error)
}

// dynRelSeedCollection 建一个集合(带一个角色)
func dynRelSeedCollection(t *testing.T, svc DynService, code string) uint64 {
	t.Helper()
	cid, err := svc.CreateCollection(context.Background(),
		dynRelCaller(1001, "role_dyn_rel"), &DynCollectionInput{Code: code, Name: "巡检记录"})
	require.NoError(t, err)
	return cid
}

// dynRelSeedSelect 建一个单选字段(可带来源)
func dynRelSeedSelect(t *testing.T, svc DynService, cid uint64, key string, options string) {
	t.Helper()
	_, err := svc.CreateField(context.Background(), dynRelCaller(1001, "role_dyn_rel"), cid, &DynFieldInput{
		FieldKey: key, Label: "负责人", Type: "select", Options: options,
	})
	require.NoError(t, err)
}

// ============================================================================
// 选项来源
// ============================================================================

// TestDyn_选项集_手工配置原样返回 ——
// 手工选项集是设计者写死在字段定义里的,原样返回,不该被"来源"那套逻辑碰到。
func TestDyn_选项集_手工配置原样返回(t *testing.T) {
	db, svc := newDynRelationFixture(t)
	dynRelSeedRole(t, db, 9201, "role_dyn_rel")
	cid := dynRelSeedCollection(t, svc, "inspection")
	dynRelSeedSelect(t, svc, cid, "owner_kind",
		`{"choices":[{"value":"a","label":"甲"},{"value":"b","label":"乙"}]}`)

	fields, err := svc.ListFields(context.Background(), cid)
	require.NoError(t, err)

	sets, err := svc.ResolveFieldOptions(context.Background(),
		dynRelCaller(1001, "role_dyn_rel"), fields, nil)
	require.NoError(t, err)

	set := sets["owner_kind"]
	require.Len(t, set.Choices, 2)
	assert.Equal(t, "a", set.Choices[0].Value)
	assert.Equal(t, "乙", set.Choices[1].Label)
	assert.Empty(t, set.Source, "手工选项集没有来源")
	assert.True(t, set.Readable, "手工选项集恒为可读")
}

// TestDyn_选项集_关联表来源现查 ——
// 关联表的选项**不落库**,读的时候现查 ⇒ 系统表里新增一行,选项立刻多一个。
func TestDyn_选项集_关联表来源现查(t *testing.T) {
	db, svc := newDynRelationFixture(t)
	dynRelSeedRole(t, db, 9201, "role_dyn_rel")
	dynRelSeedUser(t, db, 1001, "zhangsan", "张三", true)
	dynRelSeedUser(t, db, 1002, "lisi", "李四", true)

	cid := dynRelSeedCollection(t, svc, "inspection")
	dynRelSeedSelect(t, svc, cid, "assignee",
		`{"choices":[],"source":{"kind":"table","key":"user"}}`)

	fields, err := svc.ListFields(context.Background(), cid)
	require.NoError(t, err)

	sets, err := svc.ResolveFieldOptions(context.Background(),
		dynRelCaller(1001, "role_dyn_rel"), fields, map[string]bool{"user": true})
	require.NoError(t, err)

	set := sets["assignee"]
	assert.Equal(t, "user", set.Source)
	assert.True(t, set.Readable)
	require.Len(t, set.Choices, 2, "两个在用用户 ⇒ 两个选项")
	// 显示名的口径:real_name → nickname → username
	assert.Equal(t, "张三", set.Choices[0].Label)
	assert.Equal(t, "李四", set.Choices[1].Label)
	// 取值列是 id,且**存成字符串形态**(与手工选项集同形态)
	assert.Equal(t, "1001", set.Choices[0].Value)
	assert.Equal(t, "1002", set.Choices[1].Value)
}

// TestDyn_选项集_读不到就降级成空选项 ——
// ⚠️ **这是本文件最重要的一条** ——
// 业务员没有 `user:list` 时,这个字段的选项是空的,
// 但**整页表单不许失败**(一个下拉框读不出来就白屏,比"下拉框是空的"严重得多)。
func TestDyn_选项集_读不到就降级成空选项(t *testing.T) {
	db, svc := newDynRelationFixture(t)
	dynRelSeedRole(t, db, 9201, "role_dyn_rel")
	dynRelSeedUser(t, db, 1001, "zhangsan", "张三", true)

	cid := dynRelSeedCollection(t, svc, "inspection")
	dynRelSeedSelect(t, svc, cid, "assignee",
		`{"choices":[],"source":{"kind":"table","key":"user"}}`)

	fields, err := svc.ListFields(context.Background(), cid)
	require.NoError(t, err)

	sets, err := svc.ResolveFieldOptions(context.Background(),
		dynRelCaller(1001, "role_dyn_rel"), fields, map[string]bool{"user": false})
	require.NoError(t, err, "读不到来源不是错误,是降级")

	set := sets["assignee"]
	assert.Empty(t, set.Choices, "读不到 ⇒ 没有选项")
	assert.False(t, set.Readable)
	assert.Equal(t, "user:list", set.RequiredPerm, "要告诉前端缺哪个权限码")
}

// TestDyn_选项集_来源不在注册表里也降级 ——
// 老配置指向的来源被删了 ⇒ 查不到 ⇒ 判成不可读,不报错。
func TestDyn_选项集_来源不在注册表里也降级(t *testing.T) {
	db, svc := newDynRelationFixture(t)
	dynRelSeedRole(t, db, 9201, "role_dyn_rel")
	cid := dynRelSeedCollection(t, svc, "inspection")
	// ⚠️ **直接写库绕开设计期校验** ——
	// 这里模拟的是"字段建好之后,后端把那个来源从注册表里删了"。
	dynRelSeedSelect(t, svc, cid, "assignee",
		`{"choices":[],"source":{"kind":"table","key":"user"}}`)
	require.NoError(t, db.Model(&model.SysDynField{}).
		Where("field_key = ?", "assignee").
		Update("options", `{"choices":[],"source":{"kind":"table","key":"gone_table"}}`).Error)

	fields, err := svc.ListFields(context.Background(), cid)
	require.NoError(t, err)

	sets, err := svc.ResolveFieldOptions(context.Background(),
		dynRelCaller(1001, "role_dyn_rel"), fields, map[string]bool{"user": true})
	require.NoError(t, err)

	set := sets["assignee"]
	assert.Empty(t, set.Choices)
	assert.False(t, set.Readable, "来源不在注册表里 ⇒ 判成不可读")
}

// TestDyn_选项集_关联表来源受数据范围约束 ——
// ⚠️ **数据范围是第二道闸** ——
// 资源码判过了也不代表能看全部行:配了「仅本人」就只看到自己。
func TestDyn_选项集_关联表来源受数据范围约束(t *testing.T) {
	db, svc := newDynRelationFixture(t)
	dynRelSeedRole(t, db, 9201, "role_dyn_rel")
	dynRelSeedScopeRule(t, db, 9201, "user", "self")
	dynRelSeedUser(t, db, 1001, "zhangsan", "张三", true)
	dynRelSeedUser(t, db, 1002, "lisi", "李四", true)

	cid := dynRelSeedCollection(t, svc, "inspection")
	dynRelSeedSelect(t, svc, cid, "assignee",
		`{"choices":[],"source":{"kind":"table","key":"user"}}`)

	fields, err := svc.ListFields(context.Background(), cid)
	require.NoError(t, err)

	sets, err := svc.ResolveFieldOptions(context.Background(),
		dynRelCaller(1001, "role_dyn_rel"), fields, map[string]bool{"user": true})
	require.NoError(t, err)

	set := sets["assignee"]
	require.Len(t, set.Choices, 1, "「仅本人」⇒ 只看得到自己")
	assert.Equal(t, "1001", set.Choices[0].Value)
}

// TestDyn_选项集_停用的用户不出现在选项里 ——
// 停用 / 软删的用户不该再被选成新记录的负责人。
func TestDyn_选项集_停用的用户不出现在选项里(t *testing.T) {
	db, svc := newDynRelationFixture(t)
	dynRelSeedRole(t, db, 9201, "role_dyn_rel")
	dynRelSeedUser(t, db, 1001, "zhangsan", "张三", true)
	dynRelSeedUser(t, db, 1002, "lisi", "李四", false)

	cid := dynRelSeedCollection(t, svc, "inspection")
	dynRelSeedSelect(t, svc, cid, "assignee",
		`{"choices":[],"source":{"kind":"table","key":"user"}}`)

	fields, err := svc.ListFields(context.Background(), cid)
	require.NoError(t, err)

	sets, err := svc.ResolveFieldOptions(context.Background(),
		dynRelCaller(1001, "role_dyn_rel"), fields, map[string]bool{"user": true})
	require.NoError(t, err)

	set := sets["assignee"]
	require.Len(t, set.Choices, 1)
	assert.Equal(t, "1001", set.Choices[0].Value, "停用的用户不该出现在选项里")
}

// TestDyn_写入_关联表来源不校验成员资格 ——
// ⚠️ **这是一条有代价的取舍,必须钉住** ——
// 关联表是"活的":表里的行会被删。若也校验成员资格,
// **一条历史记录只要它引用的那一行被删了,这条记录就再也存不进去任何修改**
// (用户只想改个备注,后端报"不是合法选项")——
// 这比"多存了一个野值"严重得多。
func TestDyn_写入_关联表来源不校验成员资格(t *testing.T) {
	db, svc := newDynRelationFixture(t)
	dynRelSeedRole(t, db, 9201, "role_dyn_rel")
	cid := dynRelSeedCollection(t, svc, "inspection")
	dynRelSeedSelect(t, svc, cid, "assignee",
		`{"choices":[],"source":{"kind":"table","key":"user"}}`)

	// 999999 不在用户表里 —— 但历史记录里就是会有这种值
	_, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"inspection", map[string]any{"assignee": "999999"})
	require.NoError(t, err, "关联表来源不做成员资格校验")
}

// TestDyn_写入_手工选项集仍校验成员资格 ——
// ⚠️ **反向的那一条** ——
// 手工选项集是"死的",写进去的值永远应该在里面。
// 放行它等于把"下拉框里选不到的值也能存进来"这个老洞重新打开。
func TestDyn_写入_手工选项集仍校验成员资格(t *testing.T) {
	db, svc := newDynRelationFixture(t)
	dynRelSeedRole(t, db, 9201, "role_dyn_rel")
	cid := dynRelSeedCollection(t, svc, "inspection")
	dynRelSeedSelect(t, svc, cid, "owner_kind",
		`{"choices":[{"value":"a","label":"甲"}]}`)

	_, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"inspection", map[string]any{"owner_kind": "zzz"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不是合法选项")
}

// ============================================================================
// 附件字段
// ============================================================================

// stubFileResolver 一个假的文件解析器
type stubFileResolver struct {
	known map[string]FileRef
	err   error
}

func (s *stubFileResolver) ResolveKeys(_ context.Context, keys []string) (map[string]FileRef, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]FileRef, len(keys))
	for _, k := range keys {
		if ref, ok := s.known[k]; ok {
			out[k] = ref
		}
	}
	return out, nil
}

// newDynFileFixture 起一个内存库 + 一个带文件解析器的 dynService
func newDynFileFixture(t *testing.T, files FileResolver) (*gorm.DB, DynService) {
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
	require.NoError(t, db.AutoMigrate(
		&model.SysDynCollection{}, &model.SysDynField{}, &model.SysResource{},
		&model.SysRole{}, &model.SysRoleResource{}, &model.SysUser{},
	))
	return db, NewDynService(db, nil, files, nil)
}

// dynFileSeed 建一个集合 + 一个附件字段
func dynFileSeed(t *testing.T, svc DynService, code string) {
	t.Helper()
	cid, err := svc.CreateCollection(context.Background(),
		dynRelCaller(1001, "role_dyn_rel"), &DynCollectionInput{Code: code, Name: "报修记录"})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynRelCaller(1001, "role_dyn_rel"), cid, &DynFieldInput{
		FieldKey: "attachment", Label: "现场照片", Type: "file",
	})
	require.NoError(t, err)
}

// TestDyn_写入_附件文件不存在时报错 ——
// 形状校验挡不住"指向死文件的路径" ⇒ 写的时候就说清楚。
func TestDyn_写入_附件文件不存在时报错(t *testing.T) {
	_, svc := newDynFileFixture(t, &stubFileResolver{known: map[string]FileRef{}})
	dynFileSeed(t, svc, "repair")

	_, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"repair", map[string]any{"attachment": "rapid-admin/1/dyn/0/nope.pdf"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "文件不存在")
}

// TestDyn_写入_附件存在时通过 ——
// 对照组:上面那条用例的"同一段代码也要读得出合法值",否则那条红是恒真的。
func TestDyn_写入_附件存在时通过(t *testing.T) {
	key := "rapid-admin/1/dyn/0/ok.pdf"
	_, svc := newDynFileFixture(t, &stubFileResolver{known: map[string]FileRef{
		key: {Name: "ok.pdf", URL: "/rapid-admin/1/dyn/0/ok.pdf"},
	}})
	dynFileSeed(t, svc, "repair")

	_, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"repair", map[string]any{"attachment": key})
	require.NoError(t, err)
}

// TestDyn_写入_文件服务查不动时放行 ——
// "附件服务暂时查不动"不该让整个表单存不进去(旁路纪律)。
func TestDyn_写入_文件服务查不动时放行(t *testing.T) {
	_, svc := newDynFileFixture(t, &stubFileResolver{err: assert.AnError})
	dynFileSeed(t, svc, "repair")

	_, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"repair", map[string]any{"attachment": "rapid-admin/1/dyn/0/whatever.pdf"})
	require.NoError(t, err, "解析服务报错时降级放行")
}

// TestDyn_写入_下划线开头的键被忽略 ——
// 读记录时视图里多一个 `_files` 映射;"读出来 → 改两个字段 → 原样提交"
// 是最常见的用法 ⇒ 回传 `_files` 不该被当成"未知字段"拒掉。
func TestDyn_写入_下划线开头的键被忽略(t *testing.T) {
	key := "rapid-admin/1/dyn/0/ok.pdf"
	_, svc := newDynFileFixture(t, &stubFileResolver{known: map[string]FileRef{
		key: {Name: "ok.pdf", URL: "/rapid-admin/1/dyn/0/ok.pdf"},
	}})
	dynFileSeed(t, svc, "repair")

	_, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"repair", map[string]any{
			"attachment": key,
			"_files": map[string]any{
				"attachment": map[string]any{"name": "ok.pdf", "url": "/rapid-admin/1/dyn/0/ok.pdf"},
			},
		})
	require.NoError(t, err, "`_` 开头的键必须被忽略,不是拒掉")
}

// TestDyn_写入_真的不认识的字段仍然拒掉 ——
// ⚠️ **反向的那一条** ——
// 放行 `_` 前缀不能变成"什么键都收":字段名写错了照样要报错,
// 否则用户改了个字段名提交,后端返回"成功",而数据根本没存进去。
func TestDyn_写入_真的不认识的字段仍然拒掉(t *testing.T) {
	_, svc := newDynFileFixture(t, &stubFileResolver{known: map[string]FileRef{}})
	dynFileSeed(t, svc, "repair")

	_, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"repair", map[string]any{"attchment": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不存在")
}

// TestDyn_读取_记录带_files映射 ——
// 字段列里只有 key,原文件名与直链不在列里 ⇒ 读记录时另外解析出来。
func TestDyn_读取_记录带_files映射(t *testing.T) {
	key := "rapid-admin/1/dyn/0/户型图.pdf"
	_, svc := newDynFileFixture(t, &stubFileResolver{known: map[string]FileRef{
		key: {Name: "户型图.pdf", URL: "/rapid-admin/1/dyn/0/%E6%88%B7%E5%9E%8B%E5%9B%BE.pdf"},
	}})
	dynFileSeed(t, svc, "repair")

	id, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"repair", map[string]any{"attachment": key})
	require.NoError(t, err)

	rec, err := svc.GetRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"), "repair", id)
	require.NoError(t, err)

	assert.Equal(t, key, rec["attachment"], "字段值就是那个 key")
	files, ok := rec["_files"].(map[string]FileRef)
	require.True(t, ok, "记录视图里要有 _files 映射")
	// ⚠️ **`_files` 的键是**字段标识**,不是文件 key** ——
	// 前端按字段渲染,拿字段标识当键才能少一次映射。
	ref, ok := files["attachment"]
	require.True(t, ok, "_files 里要有这个附件")
	assert.Equal(t, "户型图.pdf", ref.Name)
	assert.NotEmpty(t, ref.URL)
}

// TestDyn_读取_文件不在了也不让整页失败 ——
// 文件被删 / key 写错 ⇒ 那一项不出现,其余字段照常返回。
func TestDyn_读取_文件不在了也不让整页失败(t *testing.T) {
	_, svc := newDynFileFixture(t, &stubFileResolver{known: map[string]FileRef{}})
	dynFileSeed(t, svc, "repair")

	// ⚠️ **先造一条没有附件的记录,再直接写库塞一个死 key** ——
	// 走 CreateRecord 会被存在性校验挡住(那是另一条用例)。
	id, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"repair", map[string]any{})
	require.NoError(t, err)
	require.NoError(t, dynSetDeadKey(t, svc, "repair", id))

	rec, err := svc.GetRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"), "repair", id)
	require.NoError(t, err, "附件解析失败不许让整页记录失败")
	assert.Equal(t, "rapid-admin/1/dyn/0/dead.pdf", rec["attachment"])
}

// dynSetDeadKey 直接写库塞一个不存在的附件 key(绕过写入侧的校验)
func dynSetDeadKey(t *testing.T, s DynService, code string, id uint64) error {
	t.Helper()
	ds, ok := s.(*dynService)
	require.True(t, ok)
	rc, err := ds.loadRecordCtx(context.Background(), code)
	require.NoError(t, err)
	return ds.db.WithContext(context.Background()).Exec(
		"UPDATE `"+rc.col.PhysTable+"` SET `f_attachment` = ? WHERE `id` = ?",
		"rapid-admin/1/dyn/0/dead.pdf", id).Error
}

// TestDyn_文件字段的值必须是相对路径 ——
// 设计文档 §2.9:字段存的是**不带域名的路径**。
func TestDyn_文件字段的值必须是相对路径(t *testing.T) {
	_, svc := newDynFileFixture(t, &stubFileResolver{known: map[string]FileRef{}})
	dynFileSeed(t, svc, "repair")

	_, err := svc.CreateRecord(context.Background(), dynRelCaller(1001, "role_dyn_rel"),
		"repair", map[string]any{"attachment": "https://cdn.example.com/rapid-admin/1/a.pdf"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "域名")
}

// TestDyn_附件字段的列宽对齐文件key ——
// dynx 那边的列宽必须与 sys_file.file_key 一致(512),
// 短一截就是"存不下"。
func TestDyn_附件字段的列宽对齐文件key(t *testing.T) {
	spec, ok := dynx.Lookup(dynx.TypeFile)
	require.True(t, ok)
	assert.Equal(t, "VARCHAR(512)", spec.MySQLType)
	assert.Equal(t, "TEXT", spec.SQLiteType)
}
