// relation 字段的值层(归一化 / 反归一化)单测(M5)
//
// ⚠️ **断言的是"落库形态"与"上 wire 形态"两头的契约** ——
// 落库必须是 int64(INTEGER 列,与系统 id 同型,IN/JOIN 才对得齐);
// 上 wire 必须是字符串(19 位 id 走 JSON number 会丢精度)。
package dynx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalize_Relation(t *testing.T) {
	rel := Options{Relation: &RelationOptions{Target: "device"}}

	// "没填"的形态 ⇒ nil(不是 0)
	for _, raw := range []any{nil, "", "   "} {
		v, err := Normalize(TypeRelation, raw, rel)
		require.NoError(t, err, "输入 %#v", raw)
		assert.Nil(t, v, "输入 %#v 应归一化成 nil", raw)
	}

	// 合法值 ⇒ int64
	for _, tc := range []struct {
		raw  any
		want int64
	}{
		{float64(42), 42}, // JSON number
		{"42", 42},        // JSON string(前端大 id 的常规形态)
		{int64(42), 42},   // Go 内部
		{uint64(1844674407370955161), 1844674407370955161}, // sonyflake 量级
		{uint(7), 7},
	} {
		v, err := Normalize(TypeRelation, tc.raw, rel)
		require.NoError(t, err, "输入 %#v", tc.raw)
		assert.Equal(t, tc.want, v, "输入 %#v", tc.raw)
	}

	// 非法值 ⇒ 报错(且当场报,不带脏数据过闸)
	for _, raw := range []any{float64(0), "-3", float64(1.5), "abc", true, []any{1}} {
		_, err := Normalize(TypeRelation, raw, rel)
		assert.Error(t, err, "输入 %#v 应被拒", raw)
	}
}

func TestDenormalize_Relation(t *testing.T) {
	// int64 / string / []byte 进来都出字符串 id
	assert.Equal(t, "42", Denormalize(TypeRelation, int64(42)))
	assert.Equal(t, "42", Denormalize(TypeRelation, "42"))
	assert.Equal(t, "42", Denormalize(TypeRelation, []byte("42")))
	// nil 原样(没填)
	assert.Nil(t, Denormalize(TypeRelation, nil))
}
