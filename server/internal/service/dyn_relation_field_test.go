// 动态集合 × 跨集合关联字段(M5)—— service 层单测
//
// ⚠️ **这批用例守的同样是"看不见的东西"** ——
// 关联字段最贵的失败模式是"存进去了但指向一条不存在的记录"
// (列表页显示一串 id,用户以为系统坏了)以及
// "删了被引用的记录,引用方全部悬空"(事后无法对账)。
//
// ⇒ 写入存在性 / 删除保护 / 集合停用保护 / 标签展开 / 候选项权限,
//
//	每条用例都断言"该被挡的真的被挡"。
package service

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/dynx"
)

// dynRelUintStr id → 字符串(relation 字段上 wire 的形态)
func dynRelUintStr(id uint64) string { return strconv.FormatUint(id, 10) }

// dynRelFieldSeed 建两个集合:device(被引用方)+ inspection(引用方)
//
// device 有一个文本字段 name(显示字段候选);
// inspection 有一个 relation 字段 device 指向 device 集合。
func dynRelFieldSeed(t *testing.T, svc DynService) (deviceID, inspectionID uint64) {
	t.Helper()
	ctx := context.Background()
	caller := dynCaller(1001, "role_dyn_tester")

	deviceID, err := svc.CreateCollection(ctx, caller, &DynCollectionInput{Code: "device", Name: "设备台账"})
	require.NoError(t, err)
	_, err = svc.CreateField(ctx, caller, deviceID, &DynFieldInput{
		FieldKey: "name", Label: "设备名", Type: "text",
	})
	require.NoError(t, err)
	// 一个不可做显示名的字段(json),给显示字段类型校验当靶子
	_, err = svc.CreateField(ctx, caller, deviceID, &DynFieldInput{
		FieldKey: "extra", Label: "扩展信息", Type: "json",
	})
	require.NoError(t, err)

	inspectionID, err = svc.CreateCollection(ctx, caller, &DynCollectionInput{Code: "inspection", Name: "巡检记录"})
	require.NoError(t, err)
	return deviceID, inspectionID
}

// dynRelFieldAdd 给指定集合加一个 relation 字段
func dynRelFieldAdd(t *testing.T, svc DynService, cid uint64, key string, options string) {
	t.Helper()
	_, err := svc.CreateField(context.Background(), dynCaller(1001, "role_dyn_tester"), cid, &DynFieldInput{
		FieldKey: key, Label: "关联" + key, Type: "relation", Options: options,
	})
	require.NoError(t, err)
}

// dynRelFieldCreateDevice 建一条设备记录,返回 id
func dynRelFieldCreateDevice(t *testing.T, svc DynService, name string) uint64 {
	t.Helper()
	id, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_dyn_tester"), "device",
		map[string]any{"name": name})
	require.NoError(t, err)
	return id
}

// TestDyn_关联字段_设计期校验
func TestDyn_关联字段_设计期校验(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")
	deviceID, inspectionID := dynRelFieldSeed(t, svc)
	ctx := context.Background()
	caller := dynCaller(1001, "role_dyn_tester")

	// ① 没配目标集合 ⇒ 拒
	_, err := svc.CreateField(ctx, caller, inspectionID, &DynFieldInput{
		FieldKey: "dev", Label: "所属设备", Type: "relation",
	})
	assert.ErrorContains(t, err, "目标集合")

	// ② 目标集合不存在 ⇒ 拒(错误里点名)
	_, err = svc.CreateField(ctx, caller, inspectionID, &DynFieldInput{
		FieldKey: "dev", Label: "所属设备", Type: "relation",
		Options: `{"relation":{"target":"ghost"}}`,
	})
	assert.ErrorContains(t, err, "ghost")

	// ③ 显示字段不存在 ⇒ 拒
	_, err = svc.CreateField(ctx, caller, inspectionID, &DynFieldInput{
		FieldKey: "dev", Label: "所属设备", Type: "relation",
		Options: `{"relation":{"target":"device","display_field":"nope"}}`,
	})
	assert.ErrorContains(t, err, "nope")

	// ④ 显示字段类型不可做显示名(json)⇒ 拒
	_, err = svc.CreateField(ctx, caller, inspectionID, &DynFieldInput{
		FieldKey: "dev", Label: "所属设备", Type: "relation",
		Options: `{"relation":{"target":"device","display_field":"extra"}}`,
	})
	assert.ErrorContains(t, err, "extra")

	// ⑤ 全对 ⇒ 过
	devFieldID, err := svc.CreateField(ctx, caller, inspectionID, &DynFieldInput{
		FieldKey: "dev", Label: "所属设备", Type: "relation",
		Options: `{"relation":{"target":"device","display_field":"name"}}`,
	})
	require.NoError(t, err)
	require.NotZero(t, devFieldID)
	require.NotZero(t, deviceID)

	// ⑥ 改字段时把显示字段换成不存在的 ⇒ 拒
	err = svc.UpdateField(ctx, caller, devFieldID, &DynFieldUpdate{
		Label: "所属设备", Required: false,
		Options:      `{"relation":{"target":"device","display_field":"changed_later"}}`,
		DefaultValue: "",
		Sort:         0,
	})
	assert.ErrorContains(t, err, "changed_later")
}

// TestDyn_关联字段_写入存在性与值形状
func TestDyn_关联字段_写入存在性与值形状(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")
	_, inspectionID := dynRelFieldSeed(t, svc)
	dynRelFieldAdd(t, svc, inspectionID, "dev", `{"relation":{"target":"device","display_field":"name"}}`)
	ctx := context.Background()
	caller := dynCaller(1001, "role_dyn_tester")

	// ① 引用不存在的记录 ⇒ 拒
	_, err := svc.CreateRecord(ctx, caller, "inspection", map[string]any{"dev": 424242})
	assert.ErrorContains(t, err, "不存在")

	// ② 非正整数 id ⇒ 拒
	_, err = svc.CreateRecord(ctx, caller, "inspection", map[string]any{"dev": "abc"})
	assert.Error(t, err)
	_, err = svc.CreateRecord(ctx, caller, "inspection", map[string]any{"dev": 0})
	assert.ErrorContains(t, err, "正整数")
	_, err = svc.CreateRecord(ctx, caller, "inspection", map[string]any{"dev": -3})
	assert.Error(t, err)

	// ③ 引用真实记录 ⇒ 过,落库是 INTEGER
	devID := dynRelFieldCreateDevice(t, svc, "3 号水泵")
	recID, err := svc.CreateRecord(ctx, caller, "inspection", map[string]any{"dev": devID})
	require.NoError(t, err)
	assert.Equal(t, int64(devID), dynRawCol(t, db,
		"SELECT f_dev FROM dyn_inspection WHERE id = ?", int64(recID)),
		"关联字段必须落成整数 id")

	// ④ 没填(nil)⇒ NULL;空串 ⇒ NULL(不是 0)
	rec2, err := svc.CreateRecord(ctx, caller, "inspection", map[string]any{})
	require.NoError(t, err)
	assert.Nil(t, dynRawCol(t, db, "SELECT f_dev FROM dyn_inspection WHERE id = ?", int64(rec2)))
	rec3, err := svc.CreateRecord(ctx, caller, "inspection", map[string]any{"dev": ""})
	require.NoError(t, err)
	assert.Nil(t, dynRawCol(t, db, "SELECT f_dev FROM dyn_inspection WHERE id = ?", int64(rec3)))
}

// TestDyn_关联字段_被引用的记录不可删
func TestDyn_关联字段_被引用的记录不可删(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")
	_, inspectionID := dynRelFieldSeed(t, svc)
	dynRelFieldAdd(t, svc, inspectionID, "dev", `{"relation":{"target":"device","display_field":"name"}}`)
	ctx := context.Background()
	caller := dynCaller(1001, "role_dyn_tester")

	devID := dynRelFieldCreateDevice(t, svc, "空压机 A")
	_, err := svc.CreateRecord(ctx, caller, "inspection", map[string]any{"dev": devID})
	require.NoError(t, err)

	// ① 单删 ⇒ 挡,报错里点名引用方
	err = svc.DeleteRecord(ctx, caller, "device", devID)
	assert.ErrorContains(t, err, "巡检记录")

	// ② 批量删 ⇒ 同样挡
	n, err := svc.BatchDeleteRecords(ctx, caller, "device", []string{dynRelUintStr(devID)})
	assert.ErrorContains(t, err, "巡检记录")
	assert.Zero(t, n)

	// ③ 引用方记录删掉之后 ⇒ 目标记录可删
	rcs, err := svc.ListRecords(ctx, caller, "inspection", DynRecordQuery{})
	require.NoError(t, err)
	rows := rcs.List.([]map[string]any)
	require.Len(t, rows, 1)
	for _, row := range rows {
		idStr, _ := row["id"].(string)
		require.NoError(t, svc.DeleteRecord(ctx, caller, "inspection", parseUint(t, idStr)))
	}
	assert.NoError(t, svc.DeleteRecord(ctx, caller, "device", devID))
}

// TestDyn_关联字段_被引用的集合不可删不可停用
func TestDyn_关联字段_被引用的集合不可删不可停用(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")
	deviceID, inspectionID := dynRelFieldSeed(t, svc)
	dynRelFieldAdd(t, svc, inspectionID, "dev", `{"relation":{"target":"device","display_field":"name"}}`)
	ctx := context.Background()
	caller := dynCaller(1001, "role_dyn_tester")

	// ① 删目标集合 ⇒ 挡
	assert.ErrorContains(t, svc.DeleteCollection(ctx, caller, deviceID), "关联")

	// ② 停用目标集合 ⇒ 挡
	no := false
	assert.ErrorContains(t, svc.UpdateCollection(ctx, caller, deviceID, &DynCollectionUpdate{
		Name: "设备台账", IsActive: &no,
	}), "关联")

	// ③ 引用字段停用后 ⇒ 两条路都放行
	fields, err := svc.ListFields(ctx, inspectionID)
	require.NoError(t, err)
	for _, f := range fields {
		if f.Type == string(dynx.TypeRelation) {
			ia := false
			require.NoError(t, svc.UpdateField(ctx, caller, f.ID, &DynFieldUpdate{
				Label: f.Label, Required: f.Required,
				Options: f.Options, DefaultValue: f.DefaultValue,
				Sort: f.Sort, IsActive: &ia,
			}))
		}
	}
	assert.NoError(t, svc.DeleteCollection(ctx, caller, deviceID))
}

// TestDyn_关联字段_列表与详情展开标签
func TestDyn_关联字段_列表与详情展开标签(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")
	_, inspectionID := dynRelFieldSeed(t, svc)
	dynRelFieldAdd(t, svc, inspectionID, "dev", `{"relation":{"target":"device","display_field":"name"}}`)
	ctx := context.Background()
	caller := dynCaller(1001, "role_dyn_tester")

	devID := dynRelFieldCreateDevice(t, svc, "3 号水泵")
	_, err := svc.CreateRecord(ctx, caller, "inspection", map[string]any{"dev": devID})
	require.NoError(t, err)

	// ① 列表:_rels[dev] == 设备名;字段值是字符串形态的 id
	list, err := svc.ListRecords(ctx, caller, "inspection", DynRecordQuery{})
	require.NoError(t, err)
	lrows := list.List.([]map[string]any)
	require.Len(t, lrows, 1)
	row := lrows[0]
	assert.Equal(t, "3 号水泵", row["_rels"].(map[string]string)["dev"])
	assert.Equal(t, dynRelUintStr(devID), row["dev"], "关联值必须以字符串 id 上 wire")

	// ② 详情同口径
	view, err := svc.GetRecord(ctx, caller, "inspection", parseUint(t, row["id"].(string)))
	require.NoError(t, err)
	assert.Equal(t, "3 号水泵", view["_rels"].(map[string]string)["dev"])

	// ③ 没填关联的行不产生 _rels 键
	_, err = svc.CreateRecord(ctx, caller, "inspection", map[string]any{})
	require.NoError(t, err)
	list2, err := svc.ListRecords(ctx, caller, "inspection", DynRecordQuery{})
	require.NoError(t, err)
	lrows2 := list2.List.([]map[string]any)
	require.Len(t, lrows2, 2)
	for _, r := range lrows2 {
		if r["dev"] == nil {
			assert.Nil(t, r["_rels"], "没有关联值的行不该有 _rels")
		}
	}
	_ = db
}

// TestDyn_关联字段_候选项与权限闸
func TestDyn_关联字段_候选项与权限闸(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")
	_, inspectionID := dynRelFieldSeed(t, svc)
	dynRelFieldAdd(t, svc, inspectionID, "dev", `{"relation":{"target":"device","display_field":"name"}}`)
	ctx := context.Background()
	caller := dynCaller(1001, "role_dyn_tester")

	idB := dynRelFieldCreateDevice(t, svc, "水泵 B")
	idA := dynRelFieldCreateDevice(t, svc, "水泵 A")

	fields, err := svc.ListFields(ctx, inspectionID)
	require.NoError(t, err)

	// ① 有权限:候选按显示名排序,值是目标记录 id 文本,Source 是 collection: 前缀
	sets, err := svc.ResolveFieldOptions(ctx, caller, fields, map[string]bool{"collection:device": true})
	require.NoError(t, err)
	set := sets["dev"]
	assert.Equal(t, "collection:device", set.Source)
	assert.True(t, set.Readable)
	require.Len(t, set.Choices, 2)
	assert.Equal(t, "水泵 A", set.Choices[0].Label, "候选要按显示名排序")
	assert.Equal(t, dynRelUintStr(idA), set.Choices[0].Value)
	assert.Equal(t, "水泵 B", set.Choices[1].Label)
	assert.Equal(t, dynRelUintStr(idB), set.Choices[1].Value)

	// ② 没权限:空候选 + 点名缺哪个权限码
	sets2, err := svc.ResolveFieldOptions(ctx, caller, fields, map[string]bool{"collection:device": false})
	require.NoError(t, err)
	set2 := sets2["dev"]
	assert.False(t, set2.Readable)
	assert.Equal(t, "dyn:device:list", set2.RequiredPerm)
	assert.Empty(t, set2.Choices)

	// ③ 目标集合被停用(行还在但 is_active=false)⇒ 空候选,不算权限问题
	require.NoError(t, db.Model(&model.SysDynCollection{}).Where("code = ?", "device").
		Update("is_active", false).Error)
	sets3, err := svc.ResolveFieldOptions(ctx, caller, fields, map[string]bool{"collection:device": true})
	require.NoError(t, err)
	assert.Empty(t, sets3["dev"].Choices)
	assert.True(t, sets3["dev"].Readable)
}
