package dynx

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/timeutil"
)

// TestLookup_类型表覆盖全部常量 —— 漏登记一个类型,前端就能设计出一个
// 建表时才炸的字段。
func TestLookup_类型表覆盖全部常量(t *testing.T) {
	all := []Type{
		TypeText, TypeLongText, TypeInt, TypeNumber, TypeBool,
		TypeDate, TypeDateTime, TypeSelect, TypeMultiSelect, TypeJSON,
		TypeFile, TypeRelation,
	}
	for _, tp := range all {
		spec, ok := Lookup(tp)
		require.True(t, ok, "类型 %q 没登记", tp)
		assert.NotEmpty(t, spec.Label, "类型 %q 缺中文名", tp)
		assert.NotEmpty(t, spec.MySQLType, "类型 %q 缺 MySQL 列类型", tp)
		assert.NotEmpty(t, spec.SQLiteType, "类型 %q 缺 SQLite 列类型", tp)
	}
	assert.Len(t, Types(), len(all), "类型数量对不上")
}

// TestColumnType_按方言给不同的列类型 —— MySQL 有 JSON/DECIMAL,SQLite 没有。
func TestColumnType_按方言给不同的列类型(t *testing.T) {
	spec, ok := Lookup(TypeNumber)
	require.True(t, ok)
	assert.Equal(t, "DOUBLE", spec.ColumnType("mysql"))
	assert.Equal(t, "REAL", spec.ColumnType("sqlite"))
	// 未知方言回落 SQLite(类型亲和性更宽松,不会写出存不进去的表)
	assert.Equal(t, "REAL", spec.ColumnType("postgres"))
}

// TestValidateType_未知类型报错并列出可选值
func TestValidateType_未知类型报错并列出可选值(t *testing.T) {
	err := ValidateType("money")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "money")
	assert.Contains(t, err.Error(), "text", "报错里应列出可选类型")
	assert.NoError(t, ValidateType(TypeText))
}

// TestNormalize_文本 —— 数字要转成不带科学计数法的文本
func TestNormalize_文本(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"原样", "abc", "abc"},
		{"整数浮点不带尾巴", float64(1), "1"},
		{"小数不带尾巴", float64(1.5), "1.5"},
		{"布尔", true, "true"},
		{"整型", int64(7), "7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(TypeText, tc.in, Options{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	// 1000000 用 'f' 格式是 "1000000",不是 "1e+06"
	got, err := Normalize(TypeText, float64(1000000), Options{})
	require.NoError(t, err)
	assert.Equal(t, "1000000", got)
}

// TestNormalize_整数 —— 小数必须被拒(静默截断会让人以为存了 1.5)
func TestNormalize_整数(t *testing.T) {
	got, err := Normalize(TypeInt, float64(42), Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(42), got)

	_, err = Normalize(TypeInt, 1.5, Options{})
	require.Error(t, err, "1.5 不该被当成整数存进去")

	got, err = Normalize(TypeInt, "42", Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(42), got)

	_, err = Normalize(TypeInt, "abc", Options{})
	require.Error(t, err)
}

// TestNormalize_小数
func TestNormalize_小数(t *testing.T) {
	got, err := Normalize(TypeNumber, "1.25", Options{})
	require.NoError(t, err)
	assert.Equal(t, 1.25, got)

	got, err = Normalize(TypeNumber, 3, Options{})
	require.NoError(t, err)
	assert.Equal(t, float64(3), got)

	_, err = Normalize(TypeNumber, "x", Options{})
	require.Error(t, err)
}

// TestNormalize_开关 —— 落库是 0/1,读回来是 bool
func TestNormalize_开关(t *testing.T) {
	got, err := Normalize(TypeBool, true, Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), got)

	got, err = Normalize(TypeBool, false, Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), got)

	got, err = Normalize(TypeBool, "true", Options{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), got)

	_, err = Normalize(TypeBool, "开", Options{})
	require.Error(t, err, "认不出来的开关值必须报错,不能静默当 false")
}

// TestNormalize_日期 —— 带时间的输入按东八区折算
func TestNormalize_日期(t *testing.T) {
	got, err := Normalize(TypeDate, "2026-11-01", Options{})
	require.NoError(t, err)
	assert.Equal(t, "2026-11-01", got)

	// 2026-11-01T23:00:00Z = 东八区 11-02 07:00 ⇒ 日期是 11-02
	got, err = Normalize(TypeDate, "2026-11-01T23:00:00Z", Options{})
	require.NoError(t, err)
	assert.Equal(t, "2026-11-02", got)

	_, err = Normalize(TypeDate, "2026/11/01", Options{})
	require.Error(t, err)
}

// TestNormalize_日期时间 —— 不带时区的按东八区解释,落库统一 UTC
func TestNormalize_日期时间(t *testing.T) {
	got, err := Normalize(TypeDateTime, "2026-11-01 08:30:00", Options{})
	require.NoError(t, err)
	// 08:30 东八区 = 00:30 UTC
	assert.Equal(t, "2026-11-01T00:30:00Z", got)

	got, err = Normalize(TypeDateTime, "2026-11-01T00:30:00Z", Options{})
	require.NoError(t, err)
	assert.Equal(t, "2026-11-01T00:30:00Z", got)

	_, err = Normalize(TypeDateTime, "not-a-time", Options{})
	require.Error(t, err)
}

// TestNormalize_单选必须落在选项集里 —— 不校验的话列表页会显示空白标签
func TestNormalize_单选必须落在选项集里(t *testing.T) {
	opts := Options{Choices: []Choice{{Value: "a", Label: "甲"}, {Value: "b", Label: "乙"}}}

	got, err := Normalize(TypeSelect, "a", opts)
	require.NoError(t, err)
	assert.Equal(t, "a", got)

	_, err = Normalize(TypeSelect, "z", opts)
	require.Error(t, err, "z 不在选项集里,必须被拒")
}

// TestNormalize_多选 —— 去重 + 保序 + 落 JSON 文本
func TestNormalize_多选(t *testing.T) {
	opts := Options{Choices: []Choice{{Value: "a"}, {Value: "b"}}}

	got, err := Normalize(TypeMultiSelect, []any{"a", "b", "a"}, opts)
	require.NoError(t, err)
	assert.Equal(t, `["a","b"]`, got)

	_, err = Normalize(TypeMultiSelect, []any{"z"}, opts)
	require.Error(t, err)
}

// TestNormalize_JSON —— 半截 JSON 当场报错
func TestNormalize_JSON(t *testing.T) {
	got, err := Normalize(TypeJSON, map[string]any{"k": 1}, Options{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"k":1}`, got.(string))

	got, err = Normalize(TypeJSON, `{"k":1}`, Options{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"k":1}`, got.(string))

	_, err = Normalize(TypeJSON, `{"k":`, Options{})
	require.Error(t, err, "半截 JSON 必须被拒")
}

// TestNormalize_nil原样返回 —— "没填"归必填校验管,不当成非法值
func TestNormalize_nil原样返回(t *testing.T) {
	got, err := Normalize(TypeText, nil, Options{})
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestDenormalize_各类读回 —— 同一列在两个方言下类型不同,这一层要都能读
func TestDenormalize_各类读回(t *testing.T) {
	assert.Equal(t, "abc", Denormalize(TypeText, "abc"))
	assert.Equal(t, "abc", Denormalize(TypeText, []byte("abc")))
	assert.Equal(t, int64(42), Denormalize(TypeInt, int64(42)))
	assert.Equal(t, int64(42), Denormalize(TypeInt, float64(42)))
	assert.Equal(t, int64(42), Denormalize(TypeInt, []byte("42")))
	assert.Equal(t, 1.5, Denormalize(TypeNumber, []byte("1.5")))
	assert.Equal(t, true, Denormalize(TypeBool, int64(1)))
	assert.Equal(t, false, Denormalize(TypeBool, int64(0)))
	assert.Equal(t, true, Denormalize(TypeBool, "true"))
	assert.Equal(t, []string{"a", "b"}, Denormalize(TypeMultiSelect, `["a","b"]`))
	assert.Equal(t, []string{}, Denormalize(TypeMultiSelect, ""))
	assert.Equal(t, map[string]any{"k": float64(1)}, Denormalize(TypeJSON, `{"k":1}`))
	assert.Nil(t, Denormalize(TypeJSON, ""))
}

// TestDenormalize_日期保持原样 —— 归一化已经统一成 UTC ISO 串
func TestDenormalize_日期保持原样(t *testing.T) {
	assert.Equal(t, "2026-11-01", Denormalize(TypeDate, "2026-11-01"))
	assert.Equal(t, "2026-11-01T00:30:00Z", Denormalize(TypeDateTime, "2026-11-01T00:30:00Z"))
}

// Test往返_归一化再反归一化值不变 —— 这条是整个包的中心不变量
func Test往返_归一化再反归一化值不变(t *testing.T) {
	opts := Options{Choices: []Choice{{Value: "a"}, {Value: "b"}}}
	cases := []struct {
		tp   Type
		in   any
		opts Options
	}{
		{TypeText, "abc", Options{}},
		{TypeInt, int64(42), Options{}},
		{TypeNumber, 1.5, Options{}},
		{TypeBool, true, Options{}},
		{TypeDate, "2026-11-01", Options{}},
		{TypeDateTime, "2026-11-01 08:30:00", Options{}},
		{TypeSelect, "a", opts},
		{TypeMultiSelect, []string{"a", "b"}, opts},
	}
	for _, tc := range cases {
		t.Run(string(tc.tp), func(t *testing.T) {
			stored, err := Normalize(tc.tp, tc.in, tc.opts)
			require.NoError(t, err)
			back := Denormalize(tc.tp, stored)
			if tc.tp == TypeDateTime {
				// 日期时间存的是 UTC,读回来也是 UTC 串
				assert.Equal(t, "2026-11-01T00:30:00Z", back)
				return
			}
			assert.Equal(t, tc.in, back)
		})
	}
}

// TestParseOptions —— 空串合法、非法 JSON 报错、重复值报错
func TestParseOptions(t *testing.T) {
	o, err := ParseOptions("")
	require.NoError(t, err)
	assert.False(t, o.HasChoices())

	o, err = ParseOptions(`{"choices":[{"value":"a","label":"甲"}]}`)
	require.NoError(t, err)
	require.True(t, o.HasChoices())
	assert.True(t, o.Contains("a"))
	assert.False(t, o.Contains("b"))
	assert.Equal(t, []string{"a"}, o.Values())

	_, err = ParseOptions(`{`)
	require.Error(t, err)

	_, err = ParseOptions(`{"choices":[{"value":"a"},{"value":"a"}]}`)
	require.Error(t, err, "重复的选项值必须被拒")
}

// TestMarshalOptions —— 空选项集存空串
func TestMarshalOptions(t *testing.T) {
	s, err := MarshalOptions(Options{})
	require.NoError(t, err)
	assert.Equal(t, "", s)

	s, err = MarshalOptions(Options{Choices: []Choice{{Value: "a", Label: "甲"}}})
	require.NoError(t, err)
	assert.Contains(t, s, `"value":"a"`)
}

// TestValidateFieldKey —— 字段标识必须是小写英文标识符
func TestValidateFieldKey(t *testing.T) {
	require.NoError(t, ValidateFieldKey("device_name"))
	require.NoError(t, ValidateFieldKey("a1"))
	assert.Error(t, ValidateFieldKey(""))
	assert.Error(t, ValidateFieldKey("Device"))
	assert.Error(t, ValidateFieldKey("1device"))
	assert.Error(t, ValidateFieldKey("device-name"))
	assert.Error(t, ValidateFieldKey("device name"))
}

// TestValidateFieldKey_保留名 —— 系统列名不能当字段标识
//
// ⚠️ 物理列有 `f_` 前缀不会撞,**JSON 键会撞** ——
// 记录的 JSON 形状里 `id` / `owner_id` / `created_at` / `updated_at`
// 是系统键,用户字段同名会把系统的值盖掉。
func TestValidateFieldKey_保留名(t *testing.T) {
	for _, key := range []string{"id", "owner_id", "created_at", "updated_at", "deleted_at"} {
		assert.Error(t, ValidateFieldKey(key), "保留名 %q 必须被拒", key)
	}
}

// TestColumnName_带前缀 —— 前缀保证用户字段不会撞上系统列
func TestColumnName_带前缀(t *testing.T) {
	assert.Equal(t, "f_device_name", ColumnName("device_name"))
	assert.NotEqual(t, "owner_id", ColumnName("owner_id"))
}

// TestPhysTableName —— 表名由编码定死,改名不改表
func TestPhysTableName(t *testing.T) {
	assert.Equal(t, "dyn_inspection", PhysTableName("inspection"))
}

// TestValidateCode
func TestValidateCode(t *testing.T) {
	require.NoError(t, ValidateCode("inspection"))
	assert.Error(t, ValidateCode(""))
	assert.Error(t, ValidateCode("Inspection"))
}

// TestNormalize_时间按东八区解释 —— 与本仓其它业务时间同一口径
func TestNormalize_时间按东八区解释(t *testing.T) {
	ts := time.Date(2026, 11, 1, 8, 30, 0, 0, timeutil.AsiaShanghai)
	got, err := Normalize(TypeDateTime, ts, Options{})
	require.NoError(t, err)
	assert.Equal(t, "2026-11-01T00:30:00Z", got)
}

// TestNormalize_JSON数组文本往返
func TestNormalize_JSON数组文本往返(t *testing.T) {
	stored, err := Normalize(TypeJSON, `["a","b"]`, Options{})
	require.NoError(t, err)
	var decoded []string
	require.NoError(t, json.Unmarshal([]byte(stored.(string)), &decoded))
	assert.Equal(t, []string{"a", "b"}, decoded)
}
