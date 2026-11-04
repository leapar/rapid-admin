package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/dynx"
)

// 动态集合的索引配置(M2)—— 定义存集合行(JSON),DDL 由服务层同步。
//
// ⚠️ **断言一律查 sqlite_master / 集合行,不复用 service 内部状态** ——
// 同 dyn_test.go 的纪律:复用内部变量只能证明变量对,证明不了它落进了 DB。

// dynSeedCollectionWithFields 建一个集合并加两个字段(no / amount)
//
// ⚠️ **字段类型选 text / int** —— 索引的列存在性校验与类型无关,
// 但导入用例要往里灌重复值,类型决定了 Normalize 的行为(text 才能灌重复串)。
func dynSeedCollectionWithFields(t *testing.T, db *gorm.DB, svc DynService) uint64 {
	t.Helper()
	dynSeedRole(t, db, "role_dyn_tester")
	id, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "order",
		Name: "订单",
	})
	require.NoError(t, err)
	for _, f := range []DynFieldInput{
		{FieldKey: "no", Label: "单号", Type: "text"},
		{FieldKey: "amount", Label: "金额", Type: "int"},
	} {
		fi := f
		_, err := svc.CreateField(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &fi)
		require.NoError(t, err)
	}
	return id
}

// dynUserIndexes 查物理表上的用户索引名(sqlite_master)
func dynUserIndexes(t *testing.T, db *gorm.DB, phys string) []string {
	t.Helper()
	var names []string
	require.NoError(t, db.Raw(
		"SELECT name FROM sqlite_master WHERE type='index' AND tbl_name = ? AND name LIKE 'dix\\_%' ESCAPE '\\'",
		phys).Scan(&names).Error)
	return names
}

// TestDyn_索引_配置后物理索引真的建出来
func TestDyn_索引_配置后物理索引真的建出来(t *testing.T) {
	db, svc := newDynFixture(t)
	id := dynSeedCollectionWithFields(t, db, svc)

	err := svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name: "订单",
		Indexes: strPtr(`[{"name":"no_unique","columns":["no"],"unique":true},
			{"name":"amount_idx","columns":["amount"],"unique":false}]`),
	})
	require.NoError(t, err)

	// ① 物理索引建出来了,唯一索引真的是 UNIQUE
	names := dynUserIndexes(t, db, "dyn_order")
	assert.Contains(t, names, "dix_dyn_order_no_unique")
	assert.Contains(t, names, "dix_dyn_order_amount_idx")
	sql := dynRawCol(t, db,
		"SELECT sql FROM sqlite_master WHERE type='index' AND name='dix_dyn_order_no_unique'").(string)
	assert.Contains(t, sql, "UNIQUE", "no_unique 必须是唯一索引")
	assert.Contains(t, sql, "`f_no`", "索引列必须是物理列名,不是字段标识")

	// ② 集合行里存下了定义
	var col model.SysDynCollection
	require.NoError(t, db.First(&col, id).Error)
	defs, err := dynx.ParseIndexes(col.Indexes)
	require.NoError(t, err)
	assert.Len(t, defs, 2)

	// ③ 重复保存(幂等):已存在的跳过,不报 duplicate index
	err = svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name: "订单",
		Indexes: strPtr(`[{"name":"no_unique","columns":["no"],"unique":true},
			{"name":"amount_idx","columns":["amount"],"unique":false}]`),
	})
	assert.NoError(t, err, "重复保存同一份定义必须幂等")

	// ④ 系统索引一个没动
	assert.True(t, db.Migrator().HasIndex("dyn_order", "idx_dyn_order_owner"))
	assert.True(t, db.Migrator().HasIndex("dyn_order", "idx_dyn_order_deleted"))
}

// TestDyn_索引_清空定义会退役物理索引
func TestDyn_索引_清空定义会退役物理索引(t *testing.T) {
	db, svc := newDynFixture(t)
	id := dynSeedCollectionWithFields(t, db, svc)
	err := svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name:    "订单",
		Indexes: strPtr(`[{"name":"no_unique","columns":["no"],"unique":true}]`),
	})
	require.NoError(t, err)
	require.Contains(t, dynUserIndexes(t, db, "dyn_order"), "dix_dyn_order_no_unique")

	// 传空串 = 全部清掉
	err = svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name:    "订单",
		Indexes: strPtr(""),
	})
	require.NoError(t, err)
	assert.Empty(t, dynUserIndexes(t, db, "dyn_order"))

	// 不传 = 不动
	err = svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name: "订单",
	})
	require.NoError(t, err)
	assert.Empty(t, dynUserIndexes(t, db, "dyn_order"))
}

// TestDyn_索引_引用不存在的字段被拒
func TestDyn_索引_引用不存在的字段被拒(t *testing.T) {
	db, svc := newDynFixture(t)
	id := dynSeedCollectionWithFields(t, db, svc)

	err := svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name:    "订单",
		Indexes: strPtr(`[{"name":"ghost","columns":["ghost_key"],"unique":false}]`),
	})
	assert.ErrorContains(t, err, "ghost_key")
	assert.Empty(t, dynUserIndexes(t, db, "dyn_order"), "校验失败不许留半截索引")
}

// TestDyn_索引_唯一索引撞存量重复值要报可读的错
func TestDyn_索引_唯一索引撞存量重复值要报可读的错(t *testing.T) {
	db, svc := newDynFixture(t)
	id := dynSeedCollectionWithFields(t, db, svc)
	// 灌两条 no 相同的记录
	for i := 0; i < 2; i++ {
		_, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_dyn_tester"), "order",
			map[string]any{"no": "A001"})
		require.NoError(t, err)
	}

	err := svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name:    "订单",
		Indexes: strPtr(`[{"name":"no_unique","columns":["no"],"unique":true}]`),
	})
	assert.ErrorContains(t, err, "重复值")
}

// TestDyn_索引_停用字段连带退役其索引
//
// ⚠️ **这条钉住的是"口径一致"** ——
// 索引定义引用的字段必须在「在用字段」里;字段停用后如果定义还留着,
// 下一次改集合(哪怕只改个名)就会被这个没人碰过的索引卡死。
// ⇒ 停用字段 = 引用它的索引一起消失(物理索引 drop,定义从集合行摘除)。
// 列本身不删(数据不丢)。
func TestDyn_索引_停用字段连带退役其索引(t *testing.T) {
	db, svc := newDynFixture(t)
	id := dynSeedCollectionWithFields(t, db, svc)

	// 先配一个引用 amount 的索引
	err := svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name:    "订单",
		Indexes: strPtr(`[{"name":"amount_idx","columns":["amount"],"unique":false}]`),
	})
	require.NoError(t, err)
	require.Contains(t, dynUserIndexes(t, db, "dyn_order"), "dix_dyn_order_amount_idx")

	// 停用 amount 字段 ⇒ 索引连带退役
	fields, err := svc.ListFields(context.Background(), id)
	require.NoError(t, err)
	var amountFieldID uint64
	for _, f := range fields {
		if f.FieldKey == "amount" {
			amountFieldID = f.ID
		}
	}
	require.NotZero(t, amountFieldID)
	require.NoError(t, svc.DeleteField(context.Background(), dynCaller(1001, "role_dyn_tester"), amountFieldID))

	assert.NotContains(t, dynUserIndexes(t, db, "dyn_order"), "dix_dyn_order_amount_idx",
		"引用已停用字段的索引必须一起退役")
	// 集合行里的定义也摘掉了 ⇒ 之后再改集合不会被卡死
	err = svc.UpdateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name: "订单改名",
	})
	assert.NoError(t, err, "停用字段后改集合不能被残留索引定义卡死")
	var col model.SysDynCollection
	require.NoError(t, db.First(&col, id).Error)
	assert.Empty(t, col.Indexes, "集合行里引用已停用字段的定义必须被摘除")
}
