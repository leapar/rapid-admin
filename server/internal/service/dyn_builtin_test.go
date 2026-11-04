// 内置业务集合播种的用例(dyn_builtin.go)
//
// ⚠️ **播种是启动期行为,错了就是"整站起不来"或"静默没菜单"** ——
// 三条幂等口径(集合撞码跳过 / 字段逐个补 / 绑定 DoNothing)各钉一条用例。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// newBuiltinFixture 起一个内存库 + 播种要用的角色
//
// ⚠️ 不用 dynSeedRole —— 它把角色 id 写死成 9000,这里要建两个角色会撞 id。
func newBuiltinFixture(t *testing.T) (*gorm.DB, DynService) {
	t.Helper()
	db, svc := newDynFixture(t)
	for i, code := range []string{model.RolePlatformOps, model.RoleReadOnly} {
		require.NoError(t, db.Create(&model.SysRole{
			BaseModel: model.BaseModel{ID: uint64(9100 + i)},
			Code:      code,
			Name:      code,
			IsActive:  true,
		}).Error)
	}
	return db, svc
}

// TestDyn_内置集合_播种全量到位
//
// 这是播种的"入口承诺":启动一次之后,两个集合的
// 物理表 / 字段 / 资源码 / 角色绑定四样全部就位 ——
// 缺任何一样,症状都是"菜单在但用不了"或"菜单压根不出现",且不报错。
func TestDyn_内置集合_播种全量到位(t *testing.T) {
	db, svc := newBuiltinFixture(t)
	require.NoError(t, svc.EnsureBuiltinCollections(context.Background()))

	for _, def := range builtinCollections {
		phys := "dyn_" + def.code
		require.True(t, db.Migrator().HasTable(phys), "%s 物理表必须存在", phys)

		// 字段一个不少(且物理列都加上了)
		var col model.SysDynCollection
		require.NoError(t, db.Where("code = ?", def.code).First(&col).Error)
		var fields []model.SysDynField
		require.NoError(t, db.Where("collection_id = ?", col.ID).Find(&fields).Error)
		require.Len(t, fields, len(def.fields), "%s 的字段必须全部登记", def.code)
		for _, in := range def.fields {
			require.True(t, db.Migrator().HasColumn(phys, "f_"+in.FieldKey),
				"%s 的物理列 f_%s 必须存在", phys, in.FieldKey)
		}

		// 资源码 + 绑定给各自的业务角色(不跨绑)
		//
		// ⚠️ 角色 id 先查出来 —— GORM 对 `= ?` 传 *gorm.DB 子查询**不加括号**,
		// 生成的 SQL 是 `role_id = SELECT ...`,直接语法错误
		// (IN (?) 才会包一层括号;别依赖这个差异)。
		var roleID uint64
		require.NoError(t, db.Model(&model.SysRole{}).Select("id").Where("code = ?", def.roles[0]).Scan(&roleID).Error)
		var bindings int64
		require.NoError(t, db.Model(&model.SysRoleResource{}).
			Where("role_id = ? AND resource_id IN (?)", roleID,
				db.Model(&model.SysResource{}).Select("id").Where("code LIKE ?", "dyn:"+def.code+":%")).
			Count(&bindings).Error)
		assert.Positive(t, bindings, "%s 的资源必须绑给 %s", def.code, def.roles[0])
	}

	// 公告不绑给只读查看员(查看类角色不拿动态集合资源,别白送)
	var roRoleID uint64
	require.NoError(t, db.Model(&model.SysRole{}).Select("id").Where("code = ?", model.RoleReadOnly).Scan(&roRoleID).Error)
	var cross int64
	require.NoError(t, db.Model(&model.SysRoleResource{}).
		Where("role_id = ? AND resource_id IN (?)", roRoleID,
			db.Model(&model.SysResource{}).Select("id").Where("code LIKE ?", "dyn:notice:%")).
		Count(&cross).Error)
	assert.Zero(t, cross, "公告的资源不该绑给只读查看员")
}

// TestDyn_内置集合_重复播种幂等
//
// ⚠️ 每次启动都会跑一遍播种 —— 第二遍必须什么都不改:
// 不重建表、不重复登记字段、不报错。半截播种(字段缺)才会触发逐个补。
func TestDyn_内置集合_重复播种幂等(t *testing.T) {
	db, svc := newBuiltinFixture(t)
	ctx := context.Background()
	require.NoError(t, svc.EnsureBuiltinCollections(ctx))
	require.NoError(t, svc.EnsureBuiltinCollections(ctx))

	for _, def := range builtinCollections {
		var count int64
		require.NoError(t, db.Model(&model.SysDynCollection{}).Where("code = ?", def.code).Count(&count).Error)
		assert.Equal(t, int64(1), count, "%s 不得重复建", def.code)

		var col model.SysDynCollection
		require.NoError(t, db.Where("code = ?", def.code).First(&col).Error)
		var fields []model.SysDynField
		require.NoError(t, db.Where("collection_id = ?", col.ID).Find(&fields).Error)
		assert.Len(t, fields, len(def.fields), "%s 的字段不得重复登记", def.code)
	}
}

// TestDyn_内置集合_撞码跳过不覆盖
//
// 运营可能已经改了名 / 绑了审批模板 / 停了字段 ——
// 编码已存在 ⇒ 整个定义跳过,启动期一律不覆盖(这不是对账,是首次播种)。
func TestDyn_内置集合_撞码跳过不覆盖(t *testing.T) {
	db, svc := newBuiltinFixture(t)
	// 运营先自己建了一个同编码集合(名字不同)
	_, err := svc.CreateCollection(context.Background(), dynCaller(1001, model.RolePlatformOps), &DynCollectionInput{
		Code: "notice", Name: "我的公告",
	})
	require.NoError(t, err)
	require.NoError(t, svc.EnsureBuiltinCollections(context.Background()))

	var col model.SysDynCollection
	require.NoError(t, db.Where("code = ?", "notice").First(&col).Error)
	assert.Equal(t, "我的公告", col.Name, "已存在的集合必须原样保留,播种不得覆盖")
}

// TestDyn_内置集合_半截播种补字段
//
// 模拟"上次启动建了集合但字段只建了一半"(比如进程被杀)——
// 本次启动必须把缺的字段补上,已有的不动。
//
// ⚠️ 必须**硬删**(Unscoped)—— 软删的字段播种会跳过(运营可能故意停了它,
// Unscoped 总数还在),只有"从来没建过"才补。
func TestDyn_内置集合_半截播种补字段(t *testing.T) {
	db, svc := newBuiltinFixture(t)
	ctx := context.Background()
	// 先播种再硬删后半字段,模拟半截状态
	require.NoError(t, svc.EnsureBuiltinCollections(ctx))
	var col model.SysDynCollection
	require.NoError(t, db.Where("code = ?", "notice").First(&col).Error)
	require.NoError(t, db.Unscoped().Where("collection_id = ? AND field_key IN ?", col.ID, []string{"content", "priority", "publish_at", "status", "attachment"}).
		Delete(&model.SysDynField{}).Error)

	require.NoError(t, svc.EnsureBuiltinCollections(ctx))
	var fields []model.SysDynField
	require.NoError(t, db.Where("collection_id = ?", col.ID).Find(&fields).Error)
	require.Len(t, fields, len(builtinCollections[0].fields), "缺的字段必须补齐")
}
