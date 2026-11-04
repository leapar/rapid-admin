package dynx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIndexes_空串与合法JSON(t *testing.T) {
	defs, err := ParseIndexes("")
	require.NoError(t, err)
	assert.Nil(t, defs)

	defs, err = ParseIndexes(`[{"name":"amount_unique","columns":["amount"],"unique":true}]`)
	require.NoError(t, err)
	require.Len(t, defs, 1)
	assert.True(t, defs[0].Unique)
	assert.Equal(t, []string{"amount"}, defs[0].Columns)
}

func TestParseIndexes_坏JSON与重名(t *testing.T) {
	_, err := ParseIndexes(`not json`)
	assert.ErrorContains(t, err, "不是合法 JSON")

	_, err = ParseIndexes(`[{"name":"a","columns":["x"]},{"name":"a","columns":["y"]}]`)
	assert.ErrorContains(t, err, "重复")
}

func TestValidateIndex_结构与列(t *testing.T) {
	// 名字非法(大写开头)
	assert.Error(t, ValidateIndex(IndexDef{Name: "Bad", Columns: []string{"a"}}))
	// 空列
	assert.Error(t, ValidateIndex(IndexDef{Name: "ok", Columns: nil}))
	// 列重复
	assert.Error(t, ValidateIndex(IndexDef{Name: "ok", Columns: []string{"a", "a"}}))
	// 空列名
	assert.Error(t, ValidateIndex(IndexDef{Name: "ok", Columns: []string{"a", ""}}))
	// 列数超限
	many := make([]string, maxIndexColumns+1)
	for i := range many {
		many[i] = "c"
	}
	assert.Error(t, ValidateIndex(IndexDef{Name: "ok", Columns: many}))
	// 合法
	assert.NoError(t, ValidateIndex(IndexDef{Name: "owner_status", Columns: []string{"owner_id", "status"}, Unique: true}))
}

func TestUserIndexName_长度上限(t *testing.T) {
	name, err := UserIndexName("dyn_short", "status")
	require.NoError(t, err)
	assert.Equal(t, "dix_dyn_short_status", name)
	assert.Equal(t, UserIndexPrefix("dyn_short")+"status", name)

	// 64 字符上限:phys 60 + dix_(4) + _(1) + name —— 拼出来必然超
	_, err = UserIndexName(strings.Repeat("a", 60), strings.Repeat("b", 10))
	assert.ErrorContains(t, err, "索引名过长")
}

func TestMarshalIndexes_空集返空串(t *testing.T) {
	out, err := MarshalIndexes(nil)
	require.NoError(t, err)
	assert.Equal(t, "", out)

	out, err = MarshalIndexes([]IndexDef{{Name: "a", Columns: []string{"x"}}})
	require.NoError(t, err)
	var back []IndexDef
	require.NoError(t, json.Unmarshal([]byte(out), &back))
	assert.Len(t, back, 1)
}

func TestCreateIndexSQL_普通与唯一(t *testing.T) {
	sql := CreateIndexSQL("dyn_order", "dix_dyn_order_owner_status",
		IndexDef{Name: "owner_status", Columns: []string{"owner_id", "status"}},
		[]string{"f_owner_id", "f_status"})
	assert.Equal(t,
		"CREATE INDEX `dix_dyn_order_owner_status` ON `dyn_order` (`f_owner_id`, `f_status`)", sql)

	sql = CreateIndexSQL("dyn_order", "dix_dyn_order_no",
		IndexDef{Name: "no", Columns: []string{"no"}, Unique: true}, []string{"f_no"})
	assert.Equal(t, "CREATE UNIQUE INDEX `dix_dyn_order_no` ON `dyn_order` (`f_no`)", sql)
}
