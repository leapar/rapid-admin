// 动态集合数据面 · 行级批量操作 + 导入导出 的单测(§1.12.2 #13 + #14)
//
// ⚠️ **这批用例是"拆掉实现看会不会红"的清单,不是覆盖率清单**(与 dyn_test.go 同一纪律):
// 每一条都钉住一处能被无声改坏的逻辑 —— 批量口绕过归一化、批量口漏掉范围条件、
// 导出缺公式注入防护、导入的"全有才进"语义退化成"跳过错误行"。
//
// ⚠️ **断言一律查库 / 查物理表,不复用 service 内部状态**(同 dyn_test.go)。
package service

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// newDynBatchFixture 起一个内存库 + 一个 dynService + 一个带四个字段的集合
//
// 字段形状刻意覆盖"批量改最容易被绕过校验"的那几类:
// device_name=text(公式注入防护的目标类型)、count=int(类型归一化:CSV 来的是字符串 "3")、
// level=select(选项集校验:批量改 / 导入都不许存选项外的值)、passed=bool(布尔归一化:"yes" → true)。
func newDynBatchFixture(t *testing.T) (*gorm.DB, DynService, string) {
	t.Helper()
	db, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "inspection", Name: "巡检记录",
	})
	require.NoError(t, err)
	for _, f := range []DynFieldInput{
		{FieldKey: "device_name", Label: "设备名称", Type: "text"},
		{FieldKey: "count", Label: "数量", Type: "int"},
		{FieldKey: "level", Label: "等级", Type: "select", Options: `{"choices":[{"value":"a","label":"甲"},{"value":"b","label":"乙"}]}`},
		{FieldKey: "passed", Label: "是否合格", Type: "bool"},
	} {
		_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_x"), cid, &f)
		require.NoError(t, err, f.FieldKey)
	}
	return db, svc, "inspection"
}

// dynNewRecord 造一条记录,返回 id
func dynNewRecord(t *testing.T, svc DynService, code string, payload map[string]any) uint64 {
	t.Helper()
	rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_x"), code, payload)
	require.NoError(t, err)
	return rid
}

// dynRecordCount 物理表总行数(含软删)—— 断言"物理行真的在/不在"用
func dynRecordCount(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Table(table).Count(&n).Error)
	return n
}

// TestDyn_恢复_软删记录可恢复
//
// ⚠️ **恢复 = 回到普通列表** —— 软删后在普通列表看不见、在回收站(trash=1)看得见,
// 恢复之后正好反过来。两头都要断言,只断言一头是恒绿的。
func TestDyn_恢复_软删记录可恢复(t *testing.T) {
	_, svc, _ := newDynBatchFixture(t)
	ctx := context.Background()
	rid := dynNewRecord(t, svc, "inspection", map[string]any{"device_name": "电梯"})

	require.NoError(t, svc.DeleteRecord(ctx, dynCaller(1001, "role_x"), "inspection", rid))

	// 普通列表看不见,回收站看得见
	page, err := svc.ListRecords(ctx, dynCaller(1001, "role_x"), "inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total)
	page, err = svc.ListRecords(ctx, dynCaller(1001, "role_x"), "inspection", DynRecordQuery{Trash: true})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "回收站视图必须看得见软删的记录")

	// 恢复 → 两边反过来
	require.NoError(t, svc.RestoreRecord(ctx, dynCaller(1001, "role_x"), "inspection", rid))
	page, err = svc.ListRecords(ctx, dynCaller(1001, "role_x"), "inspection", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "恢复后必须回到普通列表")
	page, err = svc.ListRecords(ctx, dynCaller(1001, "role_x"), "inspection", DynRecordQuery{Trash: true})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total, "恢复后回收站必须空了")

	// 没删过的记录恢复 → 报错(不是"无事发生")
	err = svc.RestoreRecord(ctx, dynCaller(1001, "role_x"), "inspection", rid)
	require.Error(t, err, "没在回收站里的记录恢复要报错")
}

// TestDyn_恢复_ownerScoped_别人的恢复不了
//
// ⚠️ **恢复也是一条写路径,owner_scoped / 数据范围必须落在它上面** ——
// 漏了的话"能恢复别人的记录"就是一条新的越权通道。
func TestDyn_恢复_ownerScoped_别人的恢复不了(t *testing.T) {
	_, svc, _ := newDynBatchFixture(t)
	ctx := context.Background()

	// 甲(owner_scoped 集合)删了自己的记录;乙来恢复
	cid, err := svc.CreateCollection(ctx, dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "private", Name: "私有表", OwnerScoped: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateField(ctx, dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)
	rid, err := svc.CreateRecord(ctx, dynCaller(1001, "role_x"), "private", map[string]any{"device_name": "甲的"})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteRecord(ctx, dynCaller(1001, "role_x"), "private", rid))

	err = svc.RestoreRecord(ctx, dynCaller(2002, "role_x"), "private", rid)
	require.Error(t, err, "别人的软删记录不许恢复(按不存在处理)")
}

// TestDyn_批量删除_只删看得见的行
//
// ⚠️ **ids 里混着别人看不见的记录 ⇒ 返回数 < len(ids) 是正确行为** ——
// 批量口的 WHERE 必须带归属 / 数据范围条件,否则它是越权口。
func TestDyn_批量删除_只删看得见的行(t *testing.T) {
	_, svc, _ := newDynBatchFixture(t)
	ctx := context.Background()

	cid, err := svc.CreateCollection(ctx, dynCaller(1001, "role_x"), &DynCollectionInput{
		Code: "private", Name: "私有表", OwnerScoped: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateField(ctx, dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "device_name", Label: "设备名称", Type: "text",
	})
	require.NoError(t, err)
	aID, err := svc.CreateRecord(ctx, dynCaller(1001, "role_x"), "private", map[string]any{"device_name": "甲的"})
	require.NoError(t, err)
	bID, err := svc.CreateRecord(ctx, dynCaller(2002, "role_x"), "private", map[string]any{"device_name": "乙的"})
	require.NoError(t, err)

	// 乙(owner_scoped 生效)点名删 [甲的, 乙的]
	count, err := svc.BatchDeleteRecords(ctx, dynCaller(2002, "role_x"), "private",
		[]string{strconv.FormatUint(aID, 10), strconv.FormatUint(bID, 10)})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "只能删掉自己看得见的那一条")

	// 甲的记录还在(乙的删了)
	page, err := svc.ListRecords(ctx, dynCaller(1001, "role_x"), "private", DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "甲的记录不许被乙的批量删除碰到")

	// 非法 id 整批拒绝(静默跳过的话,少删的那条恰恰是用户点名要删的)
	_, err = svc.BatchDeleteRecords(ctx, dynCaller(2002, "role_x"), "private", []string{"abc"})
	require.Error(t, err)
	_, err = svc.BatchDeleteRecords(ctx, dynCaller(2002, "role_x"), "private", nil)
	require.Error(t, err, "空 ids 要报错")
}

// TestDyn_批量修改_走归一化与选项集
//
// ⚠️ **批量口绕过校验 = 单条 update 的全部防线整个作废** ——
// 这里逐条钉住:类型归一化(int 收 "3" → 3)、选项集外的值拒绝、
// 必填字段不许批量改成空、不存在的字段拒绝。
func TestDyn_批量修改_走归一化与选项集(t *testing.T) {
	_, svc, _ := newDynBatchFixture(t)
	ctx := context.Background()
	rid := dynNewRecord(t, svc, "inspection", map[string]any{"device_name": "电梯", "count": 1})
	ridStr := strconv.FormatUint(rid, 10)

	// 类型归一化:字符串 "3" → int64 3(证据:详情接口里读到的是整数)
	count, err := svc.BatchUpdateRecords(ctx, dynCaller(1001, "role_x"), "inspection",
		[]string{ridStr}, "count", "3")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	rec, err := svc.GetRecord(ctx, dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Equal(t, int64(3), rec["count"], "批量改必须走类型归一化(字符串 3 → 整数 3)")

	// 布尔归一化:"yes" → true
	_, err = svc.BatchUpdateRecords(ctx, dynCaller(1001, "role_x"), "inspection",
		[]string{ridStr}, "passed", "yes")
	require.NoError(t, err)
	rec, err = svc.GetRecord(ctx, dynCaller(1001, "role_x"), "inspection", rid)
	require.NoError(t, err)
	assert.Equal(t, true, rec["passed"])

	// 选项集外的值拒绝
	_, err = svc.BatchUpdateRecords(ctx, dynCaller(1001, "role_x"), "inspection",
		[]string{ridStr}, "level", "zzz")
	require.Error(t, err, "批量改不许存选项集外的值")

	// 不存在的字段拒绝
	_, err = svc.BatchUpdateRecords(ctx, dynCaller(1001, "role_x"), "inspection",
		[]string{ridStr}, "no_such_field", "x")
	require.Error(t, err)

	// 必填字段不许批量改成空 —— 另起一个带必填字段的集合
	cid, err := svc.CreateCollection(ctx, dynCaller(1001, "role_x"), &DynCollectionInput{Code: "req", Name: "必填表"})
	require.NoError(t, err)
	_, err = svc.CreateField(ctx, dynCaller(1001, "role_x"), cid, &DynFieldInput{
		FieldKey: "title", Label: "标题", Type: "text", Required: true,
	})
	require.NoError(t, err)
	reqID, err := svc.CreateRecord(ctx, dynCaller(1001, "role_x"), "req", map[string]any{"title": "原值"})
	require.NoError(t, err)
	_, err = svc.BatchUpdateRecords(ctx, dynCaller(1001, "role_x"), "req",
		[]string{strconv.FormatUint(reqID, 10)}, "title", nil)
	require.Error(t, err, "必填字段不许批量改成空")
}

// TestDyn_导出_全量_筛选_公式注入
//
// ⚠️ **导出的是全量(不带分页),且带上当前筛选** ——
// 只导当前页的话,用户拿到的"导出结果"与界面上的筛选结果不是同一批。
//
// ⚠️ **公式注入防护** —— 文本值以 = - @ 等开头时必须加 `'` 前缀
// (Excel 会把 =cmd 之类的单元格当公式执行;前端 useTablePro 的 exportCSV 有这道防护,
// 后端导出缺席的话,恶意数据经"前端录入 → 后端导出 → 别人用 Excel 打开"照样引爆)。
//
// ⚠️ **数字的负值不加前缀** —— "-5" 是正常数字,加前缀会让 Excel 里的
// 负数变成字符串(左对齐、不能求和)。
func TestDyn_导出_全量_筛选_公式注入(t *testing.T) {
	_, svc, _ := newDynBatchFixture(t)
	ctx := context.Background()
	dynNewRecord(t, svc, "inspection", map[string]any{"device_name": "=1+1", "count": 5})
	dynNewRecord(t, svc, "inspection", map[string]any{"device_name": "-5 号水泵", "count": -5})
	dynNewRecord(t, svc, "inspection", map[string]any{"device_name": "电梯", "count": 7, "passed": true})

	var buf bytes.Buffer
	require.NoError(t, svc.ExportRecords(ctx, dynCaller(1001, "role_x"), "inspection",
		DynRecordQuery{PageRequest: model.PageRequest{OrderBy: "id", OrderDir: "asc"}}, &buf))
	out := buf.String()

	assert.True(t, strings.HasPrefix(out, "\uFEFF"), "必须带 BOM 头(Excel 中文乱码)")
	lines := strings.Split(strings.TrimPrefix(out, "\uFEFF"), "\n")
	require.True(t, len(lines) >= 4, "三行数据 + 表头(全量,不带分页)")
	assert.Equal(t, "设备名称,数量,等级,是否合格", lines[0], "列头必须是字段 label")
	// 公式注入:文本里的 "=1+1" 加了前缀;数字列的 -5 不加
	assert.Contains(t, out, `'=1+1`, "文本单元格以 = 开头必须加前缀防公式注入")
	assert.Contains(t, out, ",-5,", "数字列的负值不许被加前缀(否则 Excel 里不能求和)")
	assert.Contains(t, out, "电梯,7,,true\n", "布尔导出为 true/false(导入侧认)")

	// 关键字筛选也生效(复用 list 的 recordWhere)
	buf.Reset()
	require.NoError(t, svc.ExportRecords(ctx, dynCaller(1001, "role_x"), "inspection",
		DynRecordQuery{Keyword: "电梯"}, &buf))
	out = buf.String()
	assert.Equal(t, 2, strings.Count(strings.TrimPrefix(out, "\uFEFF"), "\n"), "带筛选的导出只含命中的行(表头 + 1 行)")
	assert.NotContains(t, out, "'=1+1")
}

// TestDyn_导入_默认全有才进
//
// ⚠️ **all(默认)语义:任何一行有任何错误 ⇒ 一行都不入库** ——
// 退化成"跳过错误行"的话,用户以为"导进去了",实际只有一半,而且没人告诉他。
func TestDyn_导入_默认全有才进(t *testing.T) {
	db, svc, code := newDynBatchFixture(t)
	ctx := context.Background()

	csvData := "设备名称,数量,等级\n" +
		"电梯,3,a\n" +
		"水泵,7,zzz\n" + // 等级不在选项集里 → 这行错
		"风机,,a\n" // 数量空:选填,可以
	res, err := svc.ImportRecords(ctx, dynCaller(1001, "role_x"), code, []byte(csvData), "")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 3, res.Total)
	assert.Equal(t, 0, res.Inserted, "有错误行 ⇒ 一行都不入库")
	assert.Equal(t, 3, res.Skipped)
	require.Len(t, res.Errors, 1)
	assert.Equal(t, 2, res.Errors[0].Row, "错误报告要写第几行(数据行号,表头不算)")
	assert.Equal(t, "level", res.Errors[0].Field, "错误报告要写哪个字段")
	assert.Equal(t, int64(0), dynRecordCount(t, db, "dyn_inspection"), "库里必须一行都没进")
}

// TestDyn_导入_skip跳错行
func TestDyn_导入_skip跳错行(t *testing.T) {
	db, svc, code := newDynBatchFixture(t)
	ctx := context.Background()

	csvData := "设备名称,数量,等级\n" +
		"电梯,3,a\n" +
		"水泵,7,zzz\n"
	res, err := svc.ImportRecords(ctx, dynCaller(1001, "role_x"), code, []byte(csvData), "skip")
	require.NoError(t, err)
	assert.Equal(t, 2, res.Total)
	assert.Equal(t, 1, res.Inserted, "合法行入库")
	assert.Equal(t, 1, res.Skipped, "错误行跳过")
	require.Len(t, res.Errors, 1)
	assert.Equal(t, 2, res.Errors[0].Row)
	assert.Equal(t, int64(1), dynRecordCount(t, db, "dyn_inspection"), "库里只有合法的那一行")

	// 入库的行真的走归一化("3" → 整数)
	page, err := svc.ListRecords(ctx, dynCaller(1001, "role_x"), code, DynRecordQuery{})
	require.NoError(t, err)
	list, ok := page.List.([]map[string]any)
	require.True(t, ok, "List 应是 []map[string]any")
	require.Len(t, list, 1)
	assert.Equal(t, int64(3), list[0]["count"])
}

// TestDyn_导入_表头认标识与显示名_未知表头拒绝
//
// ⚠️ **认不出列表头 ⇒ 该列的数据不进库,错误报告里写明是哪一列** ——
// 静默忽略的话,用户把列名打错一个字,那一列的数据就悄悄丢了。
func TestDyn_导入_表头认标识与显示名_未知表头拒绝(t *testing.T) {
	_, svc, code := newDynBatchFixture(t)
	ctx := context.Background()

	// field_key 与 label 混用都认
	res, err := svc.ImportRecords(ctx, dynCaller(1001, "role_x"), code,
		[]byte("device_name,数量\n电梯,3\n"), "")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Inserted)
	assert.Empty(t, res.Errors)

	// 未知表头 → 该行不进(all 模式下报错 ⇒ 一行都不进)
	res, err = svc.ImportRecords(ctx, dynCaller(1001, "role_x"), code,
		[]byte("device_name,打错字的列\n电梯,x\n"), "")
	require.NoError(t, err)
	assert.Equal(t, 0, res.Inserted)
	require.NotEmpty(t, res.Errors)
	assert.Equal(t, 0, res.Errors[0].Row, "表头问题记 Row=0")
	assert.Equal(t, "打错字的列", res.Errors[0].Field)
}

// TestDyn_导入_行数上限
func TestDyn_导入_行数上限(t *testing.T) {
	_, svc, code := newDynBatchFixture(t)
	ctx := context.Background()
	var sb strings.Builder
	sb.WriteString("设备名称\n")
	for i := 0; i <= dynImportMaxRows; i++ {
		sb.WriteString("x\n")
	}
	_, err := svc.ImportRecords(ctx, dynCaller(1001, "role_x"), code, []byte(sb.String()), "")
	require.Error(t, err, "超过行数上限要拒绝")
	assert.Contains(t, err.Error(), strconv.Itoa(dynImportMaxRows))
}

// TestDyn_导入导出_round_trip
//
// ⚠️ **导出的文件必须能原样导回** —— 这是导入导出的存在理由:
// "改完再导回来"是最常见的用法。round-trip 一断,两个接口各自"看起来正常",
// 合起来是坏的 —— 而且没有报错。
func TestDyn_导入导出_round_trip(t *testing.T) {
	_, svc, code := newDynBatchFixture(t)
	ctx := context.Background()
	id1 := dynNewRecord(t, svc, code, map[string]any{"device_name": "电梯", "count": 3, "level": "a", "passed": true})
	id2 := dynNewRecord(t, svc, code, map[string]any{"device_name": "水泵", "count": 7, "level": "b", "passed": false})

	var buf bytes.Buffer
	require.NoError(t, svc.ExportRecords(ctx, dynCaller(1001, "role_x"), code, DynRecordQuery{}, &buf))
	csvBytes := buf.Bytes()

	// 清空(软删)后用导出的字节**原样**导回
	_, err := svc.BatchDeleteRecords(ctx, dynCaller(1001, "role_x"), code,
		[]string{strconv.FormatUint(id1, 10), strconv.FormatUint(id2, 10)})
	require.NoError(t, err)
	page, err := svc.ListRecords(ctx, dynCaller(1001, "role_x"), code, DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total)

	res, err := svc.ImportRecords(ctx, dynCaller(1001, "role_x"), code, csvBytes, "")
	require.NoError(t, err)
	assert.Empty(t, res.Errors, "导出的文件原样导回不该有错误")
	assert.Equal(t, 2, res.Total)
	assert.Equal(t, 2, res.Inserted, "两条记录都导回来了")

	// 值逐字段对上(类型归一化在导回这一侧也走了)
	page, err = svc.ListRecords(ctx, dynCaller(1001, "role_x"), code, DynRecordQuery{})
	require.NoError(t, err)
	list, ok := page.List.([]map[string]any)
	require.True(t, ok)
	require.Len(t, list, 2)
	byName := make(map[string]map[string]any, 2)
	for _, row := range list {
		byName[row["device_name"].(string)] = row
	}
	assert.Equal(t, int64(3), byName["电梯"]["count"])
	assert.Equal(t, "a", byName["电梯"]["level"])
	assert.Equal(t, true, byName["电梯"]["passed"])
	assert.Equal(t, int64(7), byName["水泵"]["count"])
	assert.Equal(t, "b", byName["水泵"]["level"])
	assert.Equal(t, false, byName["水泵"]["passed"])
}
