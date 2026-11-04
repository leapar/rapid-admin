package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 记录列表的查询增强(M3):skipTotal / fields 裁剪 / 按字段多值筛选。
//
// ⚠️ **断言一律看返回值 / 查库,不复用 service 内部状态** —— 同 dyn_test.go 的纪律。

// dynSeedQueryCollection 建一个集合:no(text)/ amount(int)/ tags(multiselect)+ 三条记录
//
// ⚠️ **tags 专门放进来** —— multiselect 是"不开放筛选"的白名单反面,
// 得有一条用例证明它真的被拒了。
func dynSeedQueryCollection(t *testing.T, db *gorm.DB, svc DynService) {
	t.Helper()
	dynSeedRole(t, db, "role_dyn_tester")
	id, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "query",
		Name: "查询测试",
	})
	require.NoError(t, err)
	for _, f := range []DynFieldInput{
		{FieldKey: "no", Label: "单号", Type: "text"},
		{FieldKey: "amount", Label: "金额", Type: "int"},
		{FieldKey: "tags", Label: "标签", Type: "multiselect", Options: `{"choices":[{"value":"a","label":"甲"},{"value":"b","label":"乙"}]}`},
	} {
		fi := f
		_, err := svc.CreateField(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &fi)
		require.NoError(t, err)
	}
	for _, p := range []map[string]any{
		{"no": "A001", "amount": int64(100), "tags": []string{"a"}},
		{"no": "A002", "amount": int64(200), "tags": []string{"a", "b"}},
		{"no": "B001", "amount": int64(300), "tags": []string{"b"}},
	} {
		_, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_dyn_tester"), "query", p)
		require.NoError(t, err)
	}
}

func dynList(t *testing.T, svc DynService, q DynRecordQuery) ([]map[string]any, int64, error) {
	t.Helper()
	res, err := svc.ListRecords(context.Background(), dynCaller(1001, "role_dyn_tester"), "query", q)
	if err != nil {
		return nil, 0, err
	}
	return res.List.([]map[string]any), res.Total, nil
}

func TestDyn_列表_skipTotal跳过统计(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedQueryCollection(t, db, svc)

	// 默认:真统计
	_, total, err := dynList(t, svc, DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)

	// skipTotal:total = -1(哨兵,不是 0 —— 0 是合法的统计结果)
	list, total, err := dynList(t, svc, DynRecordQuery{SkipTotal: true})
	require.NoError(t, err)
	assert.Equal(t, int64(-1), total)
	assert.Len(t, list, 3)
}

func TestDyn_列表_fields裁剪(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedQueryCollection(t, db, svc)

	list, _, err := dynList(t, svc, DynRecordQuery{Fields: "no"})
	require.NoError(t, err)
	require.Len(t, list, 3)
	row := list[0]
	assert.Contains(t, row, "no")
	assert.NotContains(t, row, "amount", "没选的字段必须摘掉")
	assert.NotContains(t, row, "tags")
	// 系统键恒保留
	assert.Contains(t, row, "id")
	assert.Contains(t, row, "created_at")

	// 引用不存在的字段直接报错(不静默忽略)
	_, _, err = dynList(t, svc, DynRecordQuery{Fields: "no,ghost"})
	assert.ErrorContains(t, err, "ghost")
}

func TestDyn_列表_按字段多值筛选(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedQueryCollection(t, db, svc)

	// 文本多值 IN
	list, _, err := dynList(t, svc, DynRecordQuery{
		Filters: map[string][]string{"no": {"A001", "A002"}},
	})
	require.NoError(t, err)
	assert.Len(t, list, 2)
	for _, row := range list {
		assert.Contains(t, []string{"A001", "A002"}, row["no"])
	}

	// 整数字段:值被解析成真类型再进 IN(字符串比 int 在两方言行为不一)
	list, _, err = dynList(t, svc, DynRecordQuery{
		Filters: map[string][]string{"amount": {"100"}},
	})
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "A001", list[0]["no"])

	// 多个字段同时筛(AND)
	list, _, err = dynList(t, svc, DynRecordQuery{
		Filters: map[string][]string{"no": {"A001", "A002", "B001"}, "amount": {"200"}},
	})
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "A002", list[0]["no"])

	// 整数字段传非整数 → 报错(不静默当 0)
	_, _, err = dynList(t, svc, DynRecordQuery{
		Filters: map[string][]string{"amount": {"abc"}},
	})
	assert.ErrorContains(t, err, "整数")

	// 不存在的字段 → 报错
	_, _, err = dynList(t, svc, DynRecordQuery{
		Filters: map[string][]string{"ghost": {"x"}},
	})
	assert.ErrorContains(t, err, "ghost")

	// multiselect 不开放(JSON 文本上做 IN 的语义是错的)
	_, _, err = dynList(t, svc, DynRecordQuery{
		Filters: map[string][]string{"tags": {"a"}},
	})
	assert.ErrorContains(t, err, "不支持")
}

// TestDyn_列表_筛选表达式(M4)—— dynfilter.Compile 的服务层接线
//
// ⚠️ **这一条是"可逆探针"的锚点**:表达式必须真被编译进 WHERE,
// 而不是被静默忽略(忽略 = 返回全表,比报错危险得多)。
func TestDyn_列表_筛选表达式(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedQueryCollection(t, db, svc)

	// 简单条件
	list, _, err := dynList(t, svc, DynRecordQuery{Filter: `no = 'A001'`})
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "A001", list[0]["no"])

	// 逻辑组合 + 比较
	list, _, err = dynList(t, svc, DynRecordQuery{Filter: `amount > 100 && amount < 300`})
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "A002", list[0]["no"])

	// IN
	list, _, err = dynList(t, svc, DynRecordQuery{Filter: `no ?= ('A001','B001')`})
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// 包含(文本 LIKE)
	list, _, err = dynList(t, svc, DynRecordQuery{Filter: `no ~ 'A'`})
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// null:三条记录都填了 amount,IS NULL 应为空
	list, _, err = dynList(t, svc, DynRecordQuery{Filter: `amount = null`})
	require.NoError(t, err)
	assert.Empty(t, list)

	// 系统列也进白名单
	list, _, err = dynList(t, svc, DynRecordQuery{Filter: `id > 0`})
	require.NoError(t, err)
	assert.Len(t, list, 3)

	// 坏表达式 → 可读的错(不是静默返回全表)
	_, _, err = dynList(t, svc, DynRecordQuery{Filter: `staus = 'x'`})
	assert.ErrorContains(t, err, "不存在")
	_, _, err = dynList(t, svc, DynRecordQuery{Filter: `(`})
	assert.Error(t, err)

	// 与 f_ 多值筛选是 AND 关系,不是二选一
	list, _, err = dynList(t, svc, DynRecordQuery{
		Filter:  `amount > 100`,
		Filters: map[string][]string{"no": {"A001", "A002", "B001"}},
	})
	require.NoError(t, err)
	assert.Len(t, list, 2)
}
