// 字段级可见性 —— 原语层单测 —— M162(2026-11-02)
//
// # 这套用例守的是"脱敏之后响应里还剩下什么"
//
// 最要紧的四条:
//  1. **大整数不丢精度** —— sonyflake 主键是 17 位数,走 float64 中转会静默截断
//  2. **空策略逐字节原样返回** —— "没配规则"不许改变任何现有响应
//  3. **hidden 是删键,不是置空** —— 连字段名都不留
//  4. **按字段名匹配,不按路径** —— 嵌套对象里的同名字段也要作用
package fieldpolicy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMode(t *testing.T) {
	cases := []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{in: "visible", want: ModeVisible},
		{in: "masked", want: ModeMasked},
		{in: "hidden", want: ModeHidden},
		{in: "  MASKED  ", want: ModeMasked},
		// ⚠️ 空串 = visible(与行级数据范围同一条约定:"没配" 与 "配成明文" 效果一样)
		{in: "", want: ModeVisible},
		{in: "all", wantErr: true},
		{in: "mask", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseMode(c.in)
		if c.wantErr {
			assert.Error(t, err, "输入 %q 应当报错", c.in)
			continue
		}
		require.NoError(t, err, "输入 %q", c.in)
		assert.Equal(t, c.want, got, "输入 %q", c.in)
	}
}

func TestParseMaskType(t *testing.T) {
	for _, s := range []string{"phone", "id_card", "name", "email", "bank_card", "contract_no", "full"} {
		got, err := ParseMaskType(s)
		require.NoError(t, err, s)
		assert.Equal(t, MaskType(s), got)
	}
	// 空串 = auto(按字段名推断)
	got, err := ParseMaskType("")
	require.NoError(t, err)
	assert.Equal(t, MaskAuto, got)

	_, err = ParseMaskType("tel")
	assert.Error(t, err, "不认识的打码方式必须报错,不能静默当成 auto")
}

func TestAutoMaskType(t *testing.T) {
	// ⚠️ **精确匹配,不做后缀匹配** —— project_name / owner_name 不是人名
	assert.Equal(t, MaskPhone, AutoMaskType("phone"))
	assert.Equal(t, MaskPhone, AutoMaskType("contact_phone"))
	assert.Equal(t, MaskIDCard, AutoMaskType("id_card"))
	assert.Equal(t, MaskName, AutoMaskType("real_name"))
	assert.Equal(t, MaskEmail, AutoMaskType("email"))
	assert.Equal(t, MaskBankCard, AutoMaskType("bank_card"))
	assert.Equal(t, MaskContractNo, AutoMaskType("contract_no"))
	// 不是敏感字段
	assert.Equal(t, MaskAuto, AutoMaskType("project_name"))
	assert.Equal(t, MaskAuto, AutoMaskType("owner_name"))
	assert.Equal(t, MaskAuto, AutoMaskType("dept_name"))
	assert.Equal(t, MaskAuto, AutoMaskType("remark"))
}

func TestMask(t *testing.T) {
	assert.Equal(t, "138****1234", Mask("13812341234", MaskPhone))
	assert.Equal(t, "1101**********1234", Mask("110101199001011234", MaskIDCard))
	assert.Equal(t, "张*", Mask("张三", MaskName))
	assert.Equal(t, "z****@qq.com", Mask("zhang@qq.com", MaskEmail))
	assert.Equal(t, "************7890", Mask("6222021234567890", MaskBankCard))
	// ⚠️ **打码方式猜不出来 ⇒ 整串打码(安全方向)** ——
	// 绝不能"原样返回"当没事发生。
	assert.Equal(t, "******", Mask("remark", MaskAuto))
	// 空串原样返回
	assert.Equal(t, "", Mask("", MaskPhone))
}

// TestPolicy_ApplyTree 打码与删键
func TestPolicy_ApplyTree(t *testing.T) {
	p := NewPolicy([]Rule{
		{Field: "phone", Mode: ModeMasked, MaskType: MaskPhone},
		{Field: "id_card", Mode: ModeHidden},
	})
	in := map[string]any{
		"real_name": "张三",
		"phone":     "13812341234",
		"id_card":   "110101199001011234",
	}
	out, changed := p.ApplyTree(in)
	require.True(t, changed)
	m, ok := out.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "张三", m["real_name"])
	assert.Equal(t, "138****1234", m["phone"])
	// ⚠️ **hidden 是删键** —— 连字段名都不留
	_, has := m["id_card"]
	assert.False(t, has, "hidden 模式必须把键整个删掉")
}

// TestPolicy_ApplyTree_按字段名匹配任意深度
//
// ⚠️ 一个字段名在树里出现几次就作用几次 ——
// 列表里的每一行、嵌套对象里的同名键,都不能漏。
func TestPolicy_ApplyTree_按字段名匹配任意深度(t *testing.T) {
	p := NewPolicy([]Rule{{Field: "phone", Mode: ModeMasked, MaskType: MaskPhone}})
	in := map[string]any{
		"list": []any{
			map[string]any{"phone": "13812341234"},
			map[string]any{"nested": map[string]any{"phone": "13912341234"}},
		},
	}
	out, changed := p.ApplyTree(in)
	require.True(t, changed)
	m := out.(map[string]any)
	list := m["list"].([]any)
	first := list[0].(map[string]any)
	assert.Equal(t, "138****1234", first["phone"])
	second := list[1].(map[string]any)
	nested := second["nested"].(map[string]any)
	assert.Equal(t, "139****1234", nested["phone"])
}

// TestPolicy_ApplyJSON_大整数不丢精度
//
// ⚠️ **这是本模块最要紧的一条** ——
// sonyflake 主键是 17 位数(当前 ≈9.7e16),超出 float64 的 2^53 精确整数范围。
// 走 float64 中转 ⇒ 尾数被静默截断 ⇒ 前端拿到的是**另一个 id**。
func TestPolicy_ApplyJSON_大整数不丢精度(t *testing.T) {
	const bigID = "94883951113533126"
	raw := []byte(`{"id":` + bigID + `,"phone":"13812341234"}`)
	p := NewPolicy([]Rule{{Field: "phone", Mode: ModeMasked, MaskType: MaskPhone}})
	out, changed, err := p.ApplyJSON(raw)
	require.NoError(t, err)
	require.True(t, changed)
	assert.Contains(t, string(out), bigID, "大整数 id 不能被截断")
	assert.Contains(t, string(out), "138****1234")
}

// TestPolicy_ApplyJSON_空策略原样返回
//
// ⚠️ "没配规则"必须**逐字节**等于原响应 ——
// 否则"规则层上线"这件事本身就会改变所有现有接口的输出。
func TestPolicy_ApplyJSON_空策略原样返回(t *testing.T) {
	raw := []byte(`{"id":94883951113533126,"name":"张三"}`)
	out, changed, err := NewPolicy(nil).ApplyJSON(raw)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, string(raw), string(out))
}

// TestPolicy_ApplyJSON_没命中就不重排
//
// ⚠️ 策略配了但这个响应里没有那个字段 ⇒ 不该重新序列化
// (重新序列化会打乱键序,让响应"每次都不一样"没法比对)。
func TestPolicy_ApplyJSON_没命中就不重排(t *testing.T) {
	raw := []byte(`{"b":2,"a":1}`)
	p := NewPolicy([]Rule{{Field: "phone", Mode: ModeMasked, MaskType: MaskPhone}})
	out, changed, err := p.ApplyJSON(raw)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, string(raw), string(out))
}

// TestPolicy_Overlay_逐字段覆盖
//
// ⚠️ 默认组的限制**不会**被某个业务域的其它规则悄悄顶掉。
func TestPolicy_Overlay_逐字段覆盖(t *testing.T) {
	def := NewPolicy([]Rule{{Field: "phone", Mode: ModeHidden}})
	mod := NewPolicy([]Rule{{Field: "real_name", Mode: ModeMasked, MaskType: MaskName}})
	merged := def.Overlay(mod)

	_, has := merged.Lookup("phone")
	assert.True(t, has, "默认组隐藏 phone 不能被业务域的其它规则顶掉")
	rule, ok := merged.Lookup("phone")
	require.True(t, ok)
	assert.Equal(t, ModeHidden, rule.Mode)

	rule, ok = merged.Lookup("real_name")
	require.True(t, ok)
	assert.Equal(t, ModeMasked, rule.Mode)
}

// TestPolicy_Overlay_业务域可以显式放宽
//
// ⚠️ 逐字段覆盖也意味着业务域**可以**覆盖默认组 ——
// 默认组说"隐藏 phone",业务域说"明文",业务域赢(管理员显式配的)。
func TestPolicy_Overlay_业务域可以显式放宽(t *testing.T) {
	def := NewPolicy([]Rule{{Field: "phone", Mode: ModeHidden}})
	mod := NewPolicy([]Rule{{Field: "phone", Mode: ModeVisible}})
	merged := def.Overlay(mod)
	rule, ok := merged.Lookup("phone")
	require.True(t, ok)
	assert.Equal(t, ModeVisible, rule.Mode)
}

// TestPolicy_ApplyJSON_非字符串不被打码
//
// ⚠️ 数字不是敏感信息,硬把它变成字符串会破坏前端类型契约。
func TestPolicy_ApplyJSON_非字符串不被打码(t *testing.T) {
	raw := []byte(`{"age":30,"active":true}`)
	p := NewPolicy([]Rule{{Field: "age", Mode: ModeMasked, MaskType: MaskPhone}})
	out, changed, err := p.ApplyJSON(raw)
	require.NoError(t, err)
	assert.False(t, changed)

	var m map[string]any
	require.NoError(t, json.Unmarshal(out, &m))
	assert.Equal(t, float64(30), m["age"])
}

// ===== Report(给前端看的"这次收窄了哪些字段")—— M168 =====

// TestPolicy_Report_按模式分类
//
// ⚠️ **visible 的字段不进报告** ——
// 前端拿到报告就当"这个字段不存在/被改过",把 visible 报进去
// 会让前端把**本来能看的列也藏掉**。
func TestPolicy_Report_按模式分类(t *testing.T) {
	p := NewPolicy([]Rule{
		{Field: "phone", Mode: ModeHidden},
		{Field: "real_name", Mode: ModeMasked, MaskType: MaskName},
		{Field: "remark", Mode: ModeVisible},
	})
	r := p.Report()
	assert.Equal(t, []string{"phone"}, r.Hidden, "hidden 的字段进 Hidden")
	assert.Equal(t, []string{"real_name"}, r.Masked, "masked 的字段进 Masked")
	assert.NotContains(t, r.Hidden, "remark")
	assert.NotContains(t, r.Masked, "remark")
	assert.False(t, r.Empty(), "有内容就不该是空报告")
}

// TestPolicy_Report_空策略是空报告
func TestPolicy_Report_空策略是空报告(t *testing.T) {
	r := NewPolicy(nil).Report()
	assert.True(t, r.Empty())
	// ⚠️ **切片不能是 nil** —— 前端拿 JSON 反序列化时,null 与 [] 行为不同
	assert.NotNil(t, r.Hidden)
	assert.NotNil(t, r.Masked)
}

// TestPolicy_Report_报的是规则说了什么不是改了什么
//
// ⚠️ **这是 M168 的关键语义** ——
// 哪怕这次响应里根本没有那个字段,也要报出来:
// 前端要靠它决定"这一列该不该存在",而不是"这一次有没有动过它"。
// 若只在"真的改了"时报 ⇒ "响应里本来就没这个字段"时前端拿不到报告
// ⇒ **列照样留着**(空列)⇒ 症状没治好。
func TestPolicy_Report_报的是规则说了什么不是改了什么(t *testing.T) {
	p := NewPolicy([]Rule{{Field: "phone", Mode: ModeHidden}})
	raw := []byte(`{"name":"张三"}`)
	_, changed, err := p.ApplyJSON(raw)
	require.NoError(t, err)
	assert.False(t, changed, "响应里没有那个字段 ⇒ 什么都没改")

	r := p.Report()
	assert.Equal(t, []string{"phone"}, r.Hidden, "报告照样要带上 phone")
}
