// 字段级可见性(列级权限)规则 —— service 层单测 —— M162(2026-11-02)
//
// # 这套用例守的是"配了的东西是不是他以为的那个意思"
//
// 最要紧的四条:
//  1. **配了不存在的字段 ⇒ 配置期就拒** —— 静默不生效正是这个机制要治的病
//  2. **全量替换(不是追加)** —— 配两次不会攒出两条
//  3. **默认组与业务域组是"逐字段叠加"** —— 业务域的其它规则顶不掉默认组的限制
//  4. **候选来自响应模型,不是数据库列** —— `json:"-"` 的列不该出现在下拉框里
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
	"rapid-admin/internal/pkg/fieldpolicy"
	"rapid-admin/internal/pkg/sonyflake"
)

// fieldPolicyTestModels 字段可见性用到的表
//
// ⚠️ **sys_resource 也要** —— moduleLabels 从它那里取中文标签。
var fieldPolicyTestModels = []any{
	&model.SysFieldPolicy{},
	&model.SysRole{},
	&model.SysResource{},
	&model.SysDynCollection{},
	&model.SysDynField{},
}

// newFieldPolicyDB 起一个内存库
//
// ⚠️ **连接池限死 1 条连接** —— shared cache 的 SQLite 两个连接同时写同一张表
// 直接返 SQLITE_LOCKED。与 scope_test.go 同一理由。
func newFieldPolicyDB(t *testing.T) *gorm.DB {
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
	require.NoError(t, db.AutoMigrate(fieldPolicyTestModels...))
	return db
}

// newFieldPolicyRole 建一个角色,返回 id 与 code
func newFieldPolicyRole(t *testing.T, db *gorm.DB, code string) uint64 {
	t.Helper()
	role := model.SysRole{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Code:      code, Name: code, IsActive: true,
	}
	require.NoError(t, db.Create(&role).Error)
	return role.ID
}

// fieldPolicyFieldNames 取某个业务域的候选字段名
func fieldPolicyFieldNames(t *testing.T, svc FieldPolicyService, module string) []string {
	t.Helper()
	mods, err := svc.ListFieldPolicyModules(context.Background())
	require.NoError(t, err)
	for _, m := range mods {
		if m.Module != module {
			continue
		}
		out := make([]string, 0, len(m.Fields))
		for _, f := range m.Fields {
			out = append(out, f.Field)
		}
		return out
	}
	return nil
}

// TestFieldPolicy_候选来自响应模型不是数据库列
//
// ⚠️ **这是整个模块的地基** ——
// model.SysUser.Phone 是真实存在的列,但 `json:"-"` ⇒ **永远不在响应里**。
// 若按列出候选,管理员会看到"手机号"并勾上"隐藏",然后**什么都不会发生**,
// 而他以为自己已经防住了。
func TestFieldPolicy_候选来自响应模型不是数据库列(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)

	names := fieldPolicyFieldNames(t, svc, "user")
	require.NotEmpty(t, names)

	assert.Contains(t, names, "username", "username 是 json tag 名,应该在候选里")
	assert.NotContains(t, names, "phone", "model.SysUser.Phone 是 json:\"-\",压根不在响应里")
	assert.NotContains(t, names, "email", "model.SysUser.Email 是 json:\"-\"")
	assert.NotContains(t, names, "password_hash", "model.SysUser.PasswordHash 是 json:\"-\"")

	// 对照:guide 域的候选走它自己的响应模型
	gnames := fieldPolicyFieldNames(t, svc, "guide")
	require.NotEmpty(t, gnames)
	assert.Contains(t, gnames, "title")

	// ⚠️ **id / created_at 这些基础列不该进候选** ——
	// 隐藏 id 会让页面直接废掉(前端靠 id 跳转、查详情)。
	assert.NotContains(t, names, "id")
	assert.NotContains(t, names, "created_at")
	assert.NotContains(t, names, "deleted_at")
}

// TestFieldPolicy_配不存在的字段被拒
//
// ⚠️ **配置期就拒** —— 静默不生效是这个机制要治的病。
func TestFieldPolicy_配不存在的字段被拒(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	roleID := newFieldPolicyRole(t, db, "ops")

	err := svc.ReplaceRoleFieldPolicies(context.Background(), roleID, "guide",
		[]FieldPolicyInput{{Field: "not_exist_field", Mode: "hidden"}})
	require.Error(t, err, "这个域没有这个字段 ⇒ 必须报错,不能静默接受")
	assert.Contains(t, err.Error(), "not_exist_field")

	// ⚠️ **拒了之后库里不能有残留** ——
	var n int64
	require.NoError(t, db.Model(&model.SysFieldPolicy{}).Count(&n).Error)
	assert.Equal(t, int64(0), n)
}

// TestFieldPolicy_配json减号的列被拒
//
// ⚠️ **phone 在 sys_user 表里真实存在,但不在响应里** ——
// 配它 ⇒ 规则作用不到任何键上 ⇒ 静默不生效。
func TestFieldPolicy_配json减号的列被拒(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	roleID := newFieldPolicyRole(t, db, "ops")

	err := svc.ReplaceRoleFieldPolicies(context.Background(), roleID, "user",
		[]FieldPolicyInput{{Field: "phone", Mode: "masked", MaskType: "phone"}})
	require.Error(t, err, "json:\"-\" 的列不该配得上")
	assert.Contains(t, err.Error(), "phone")
}

// TestFieldPolicy_打码方式猜不出来被拒
//
// ⚠️ **运行期的兜底是"整串打码"** ——
// 管理员以为自己配的是"手机号打码",实际看到一堆意料之外的星号,
// 而页面上看不出区别。配置期就该问清楚。
func TestFieldPolicy_打码方式猜不出来被拒(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	roleID := newFieldPolicyRole(t, db, "ops")

	err := svc.ReplaceRoleFieldPolicies(context.Background(), roleID, "guide",
		[]FieldPolicyInput{{Field: "cover_image", Mode: "masked"}})
	require.Error(t, err, "cover_image 猜不出打码方式 ⇒ 必须要求显式指定")
	assert.Contains(t, err.Error(), "cover_image")
}

// TestFieldPolicy_业务域不存在被拒
func TestFieldPolicy_业务域不存在被拒(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	roleID := newFieldPolicyRole(t, db, "ops")

	err := svc.ReplaceRoleFieldPolicies(context.Background(), roleID, "no_such_module",
		[]FieldPolicyInput{{Field: "phone", Mode: "hidden"}})
	require.Error(t, err, "业务域不存在 ⇒ 必须报错")
	assert.Contains(t, err.Error(), "no_such_module")
}

// TestFieldPolicy_角色不存在被拒
func TestFieldPolicy_角色不存在被拒(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)

	err := svc.ReplaceRoleFieldPolicies(context.Background(), 999999, "guide",
		[]FieldPolicyInput{{Field: "title", Mode: "hidden"}})
	require.Error(t, err, "角色不存在 ⇒ 必须报错")
}

// TestFieldPolicy_全量替换
//
// ⚠️ **配两次不会攒出两条** ——
// 追加语义会让"我把手机号规则删了"这件事做不到。
func TestFieldPolicy_全量替换(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	roleID := newFieldPolicyRole(t, db, "ops")
	ctx := context.Background()

	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "guide",
		[]FieldPolicyInput{
			{Field: "contact_phone", Mode: "hidden"},
			{Field: "title", Mode: "masked", MaskType: "full"},
		}))
	got, err := svc.ListRoleFieldPolicies(ctx, roleID)
	require.NoError(t, err)
	require.Len(t, got, 2)

	// 第二次只配一条 ⇒ 之前那条被清掉
	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "guide",
		[]FieldPolicyInput{{Field: "contact_phone", Mode: "masked", MaskType: "phone"}}))
	got, err = svc.ListRoleFieldPolicies(ctx, roleID)
	require.NoError(t, err)
	require.Len(t, got, 1, "全量替换:第二次保存后只剩一条")
	assert.Equal(t, "contact_phone", got[0].Field)
	assert.Equal(t, "masked", got[0].Mode)
	assert.Equal(t, "phone", got[0].MaskType)

	// 第三次配空 ⇒ 全清
	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "guide", nil))
	got, err = svc.ListRoleFieldPolicies(ctx, roleID)
	require.NoError(t, err)
	assert.Empty(t, got, "配空 = 清掉这个业务域的全部规则")
}

// TestFieldPolicy_默认组与业务域组逐字段叠加
//
// ⚠️ **这是与行级数据范围故意不同的地方** ——
// 行级是"整组就近覆盖",字段级是"逐字段覆盖":
// 默认组隐藏 contact_phone,guide 域配了别的字段 ⇒ contact_phone **仍然隐藏**。
func TestFieldPolicy_默认组与业务域组逐字段叠加(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	roleID := newFieldPolicyRole(t, db, "ops")
	ctx := context.Background()

	// 默认组:隐藏 contact_phone
	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "",
		[]FieldPolicyInput{{Field: "contact_phone", Mode: "hidden"}}))
	// guide 域:给 title 打码(**没碰 contact_phone**)
	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "guide",
		[]FieldPolicyInput{{Field: "title", Mode: "masked", MaskType: "full"}}))

	caller := &Caller{RoleCode: "ops"}

	// guide 域:两条都在 —— 默认组的 contact_phone 没被顶掉
	p, err := svc.EffectiveFieldPolicy(ctx, caller, "guide")
	require.NoError(t, err)
	phone, ok := p.Lookup("contact_phone")
	require.True(t, ok, "默认组的限制不能被业务域的其它规则悄悄顶掉")
	assert.Equal(t, fieldpolicy.ModeHidden, phone.Mode)
	title, ok := p.Lookup("title")
	require.True(t, ok)
	assert.Equal(t, fieldpolicy.ModeMasked, title.Mode)

	// 别的域:只有默认组那条
	p2, err := svc.EffectiveFieldPolicy(ctx, caller, "user")
	require.NoError(t, err)
	phone2, ok := p2.Lookup("contact_phone")
	require.True(t, ok)
	assert.Equal(t, fieldpolicy.ModeHidden, phone2.Mode)
	_, ok = p2.Lookup("title")
	assert.False(t, ok, "guide 域的规则不该漏到别的域")
}

// TestFieldPolicy_没配规则时策略为空
//
// ⚠️ **"没配 = 什么都不做"** —— 规则层上线不许改变任何现有响应。
func TestFieldPolicy_没配规则时策略为空(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	newFieldPolicyRole(t, db, "ops")

	p, err := svc.EffectiveFieldPolicy(context.Background(), &Caller{RoleCode: "ops"}, "owner")
	require.NoError(t, err)
	assert.True(t, p.Empty(), "没配规则 ⇒ 空策略 ⇒ 响应原样透传")

	// 角色压根不存在 ⇒ 也是空策略,不是错误
	p2, err := svc.EffectiveFieldPolicy(context.Background(), &Caller{RoleCode: "nobody"}, "owner")
	require.NoError(t, err)
	assert.True(t, p2.Empty())
}

// TestFieldPolicy_改规则后立刻生效
//
// ⚠️ **缓存必须被失效** ——
// 没失效的话"改了规则但旧规则还在生效",而且**没有任何报错**。
func TestFieldPolicy_改规则后立刻生效(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	roleID := newFieldPolicyRole(t, db, "ops")
	ctx := context.Background()
	caller := &Caller{RoleCode: "ops"}

	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "guide",
		[]FieldPolicyInput{{Field: "contact_phone", Mode: "hidden"}}))
	p, err := svc.EffectiveFieldPolicy(ctx, caller, "guide")
	require.NoError(t, err)
	rule, ok := p.Lookup("contact_phone")
	require.True(t, ok)
	assert.Equal(t, fieldpolicy.ModeHidden, rule.Mode)

	// 改成明文
	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "guide",
		[]FieldPolicyInput{{Field: "contact_phone", Mode: "visible"}}))
	p, err = svc.EffectiveFieldPolicy(ctx, caller, "guide")
	require.NoError(t, err)
	rule, ok = p.Lookup("contact_phone")
	require.True(t, ok)
	assert.Equal(t, fieldpolicy.ModeVisible, rule.Mode)
}

// TestFieldPolicy_默认组候选是所有业务域的并集
//
// ⚠️ **默认组的规则作用在每一个业务域上** ——
// 候选取并集才表达得完整(guide 有 title,user 有 username)。
func TestFieldPolicy_默认组候选是所有业务域的并集(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)

	def := fieldPolicyFieldNames(t, svc, "")
	require.NotEmpty(t, def)
	assert.Contains(t, def, "title", "guide 域的字段应该在默认组候选里")
	assert.Contains(t, def, "username", "user 域的字段也该在")
}

// TestFieldPolicy_动态集合候选来自sys_dyn_field
//
// ⚠️ **动态集合的字段是运行期建的** —— 候选必须现算,不能写死。
func TestFieldPolicy_动态集合候选来自sys_dyn_field(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	roleID := newFieldPolicyRole(t, db, "ops")
	ctx := context.Background()

	col := model.SysDynCollection{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Code:      "repair",
		Name:      "报修单",
		PhysTable: "dyn_repair",
		IsActive:  true,
	}
	require.NoError(t, db.Create(&col).Error)
	fields := []model.SysDynField{
		{
			BaseModel:    model.BaseModel{ID: sonyflake.Next()},
			CollectionID: col.ID,
			FieldKey:     "phone",
			Label:        "联系电话",
			Column:       "f_phone",
			Type:         "string",
			Sort:         1,
			IsActive:     true,
		},
		{
			BaseModel:    model.BaseModel{ID: sonyflake.Next()},
			CollectionID: col.ID,
			FieldKey:     "desc",
			Label:        "问题描述",
			Column:       "f_desc",
			Type:         "text",
			Sort:         2,
			IsActive:     true,
		},
		{
			// ⚠️ **停用的字段不在候选里** —— 记录视图里已经没有了
			BaseModel:    model.BaseModel{ID: sonyflake.Next()},
			CollectionID: col.ID,
			FieldKey:     "old_field",
			Label:        "旧字段",
			Column:       "f_old",
			Type:         "string",
			Sort:         3,
			IsActive:     true,
		},
	}
	for i := range fields {
		require.NoError(t, db.Create(&fields[i]).Error)
	}
	// ⚠️ **停用要走 Update,不能靠 Create 时写 false** ——
	// GORM 的 `default:true` 会把零值字段从 INSERT 里剔掉,DB 默认值(true)生效。
	// 生产侧(dyn_impl.go)也是这么干的:建字段时一律 true,停用时显式 Update。
	require.NoError(t, db.Model(&model.SysDynField{}).
		Where("id = ?", fields[2].ID).Update("is_active", false).Error)

	names := fieldPolicyFieldNames(t, svc, "dyn:repair")
	require.NotEmpty(t, names)
	assert.Contains(t, names, "phone")
	assert.Contains(t, names, "desc")
	assert.NotContains(t, names, "old_field", "停用字段不该出现在候选里")

	// 在动态集合上配规则 —— phone 猜得出打码方式(phone ⇒ phone)
	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "dyn:repair",
		[]FieldPolicyInput{{Field: "phone", Mode: "masked"}}))
	got, err := svc.ListRoleFieldPolicies(ctx, roleID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "phone", got[0].MaskType, "auto 应被归一成具体打码方式")

	// desc 猜不出打码方式 ⇒ 显式指定才行
	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "dyn:repair",
		[]FieldPolicyInput{{Field: "desc", Mode: "masked", MaskType: "full"}}))
}

// TestFieldPolicy_模块列表里默认组排最前
//
// ⚠️ **默认组是最常用的一项** —— 不该藏在列表最后面。
func TestFieldPolicy_模块列表里默认组排最前(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)

	mods, err := svc.ListFieldPolicyModules(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, mods)
	assert.Equal(t, "", mods[0].Module, "默认规则组必须排第一")
	assert.Equal(t, "default", mods[0].Source)

	seen := map[string]bool{}
	for _, m := range mods {
		assert.False(t, seen[m.Module], "业务域 %s 重复了", m.Module)
		seen[m.Module] = true
	}
}

// ===== M163:不支持的域,默认组不生效 =====

// TestFieldPolicy_不支持的域默认组不生效
//
// # ⚠️ **这条守的是"默认组不许撒到 auth/config 上"**
//
// 默认规则组(module="")是"这个角色在所有**业务域**上的默认字段规则",
// 不是"所有接口的默认规则"。
//
// 若 EffectiveFieldPolicy 对 "auth" 这种非业务域也去叠加默认组,
// 一条默认组规则"隐藏 username"就会打到 /auth/me 的响应上 ——
// 管理员配的是"业主列表里藏用户名",结果**自己的个人中心也空了**。
//
// # 可逆探针
//
//	· 删掉 field_policy_impl.go 里的 fieldPolicyDomainSupported 早退 → 变红
//	  (那时 "auth" 也会去叠加默认组,username 规则就回来了)
func TestFieldPolicy_不支持的域默认组不生效(t *testing.T) {
	db := newFieldPolicyDB(t)
	svc := NewFieldPolicyService(db, nil)
	roleID := newFieldPolicyRole(t, db, "ops")
	ctx := context.Background()

	// 默认组:隐藏 username
	require.NoError(t, svc.ReplaceRoleFieldPolicies(ctx, roleID, "",
		[]FieldPolicyInput{{Field: "username", Mode: "hidden"}}))

	caller := &Caller{RoleCode: "ops"}

	// ⚠️ **"auth" 不是业务域** —— 默认组的规则不许叠上来
	p, err := svc.EffectiveFieldPolicy(ctx, caller, "auth")
	require.NoError(t, err)
	assert.True(t, p.Empty(), "auth 不是业务域,默认组的规则不许撒到它头上")

	// 对照组:业务域上默认组**照样生效**(别把"收窄"做成"全没了")
	p2, err := svc.EffectiveFieldPolicy(ctx, caller, "user")
	require.NoError(t, err)
	_, ok := p2.Lookup("username")
	assert.True(t, ok, "业务域上默认组必须生效 —— 收窄不能做成全没了")
}

// TestFieldPolicy_domainSupported表
//
// ⚠️ **"dyn:<编码>" 是受管的**(动态集合的字段是用户自己定的,
// 里面出现手机号/身份证是常事);"audit:operation" 是登记过的业务域。
func TestFieldPolicy_domainSupported表(t *testing.T) {
	assert.True(t, fieldPolicyDomainSupported(""), "默认组本身")
	assert.True(t, fieldPolicyDomainSupported("guide"))
	assert.True(t, fieldPolicyDomainSupported("user"))
	assert.True(t, fieldPolicyDomainSupported("audit:operation"), "带冒号的业务域也是业务域")
	assert.True(t, fieldPolicyDomainSupported("dyn:repair"), "动态集合的数据面受管")

	assert.False(t, fieldPolicyDomainSupported("auth"), "认证接口没有业务行")
	assert.False(t, fieldPolicyDomainSupported("config"), "配置项不是业务行的投影")
	assert.False(t, fieldPolicyDomainSupported("dict"))
	assert.False(t, fieldPolicyDomainSupported("no_such_module"), "没登记的域一律不受管")
}
