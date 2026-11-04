// Package fieldpolicy 字段级可见性(列级权限)原语 —— M162(2026-11-02)
//
// # 这一层是"行级数据范围"的姊妹轴
//
// 行级数据范围(internal/pkg/scope)回答的是"这个人**能看哪些行**";
// 本包回答的是"这个人**能看一行里的哪些字段**"。
//
// 两根轴**互相独立**,谁也替代不了谁:
//
//	行级:决定一整行给不给你看
//	字段级:决定一行里的某列给不给你看
//
// ⇒ 行级放行了不代表列级也放行 —— "客服部能看这个业主" ≠ "客服部能看到业主的手机号"。
//
// # ⚠️ **本包只做"收窄",不做"放宽"**
//
// 本包能做的:把一个**已经在响应里**的字段打码或删掉。
// 本包做不到的:把一个**本来就不在响应里**的字段(如 `json:"-"` 的加密手机号)
// 变出来。
//
// ⇒ 要让某个角色看到某个字段,得业务侧先把值放进响应 —— 那是业务决定,不是框架决定。
// 详见 docs/框架层未完成项清单v1.md §1.13 #4。
//
// # ⚠️ **"不配 = 全部可见"是默认语义**
//
// 与行级数据范围同一条约定:规则层上线不改变任何现有响应。
// 没配规则 ⇒ 字段原样返回(与框架落地前逐字节相同)。
//
// # 纯函数,不碰 DB
//
// 与 internal/pkg/scope 同一条纪律:这一层只算,不查库。
// 规则的存取在 internal/service/field_policy.go。
package fieldpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"rapid-admin/internal/pkg/desensitize"
)

// Mode 一个字段的可见性模式
//
// ⚠️ **三态,不是两态** ——
//
//	ModeVisible 明文返回(不做任何处理)
//	ModeMasked  返回打码后的值
//	ModeHidden  这个键**整个从响应里删掉**
//
// # ⚠️ **Hidden 是"删键",不是"置空"**
//
// 置空的话,前端看到的是 `{"phone": null}` ——
// 它知道"这里有个手机号字段",而这个字段名本身可能就是信息
// (比如 `emergency_contact_phone` 说明这个人有紧急联系人)。
// 删键则连字段名都不出现。
type Mode string

const (
	// ModeVisible 明文返回
	ModeVisible Mode = "visible"
	// ModeMasked 打码后返回
	ModeMasked Mode = "masked"
	// ModeHidden 从响应里删掉这个键
	ModeHidden Mode = "hidden"
)

// allModes 全部合法模式(顺序即前端下拉框顺序)
var allModes = []Mode{ModeVisible, ModeMasked, ModeHidden}

// AllModes 全部合法的可见性模式
func AllModes() []Mode {
	out := make([]Mode, len(allModes))
	copy(out, allModes)
	return out
}

// ParseMode 解析可见性模式
//
// ⚠️ **空串 = visible** —— 与行级数据范围同一条约定:
// "没配" 与 "配成明文" 效果一样(都不处理)。
//
// ⚠️ **不认识的模式一律报错,不静默当成 visible** ——
// 静默当成 visible = 规则失效 = 该打码的字段原样吐出去。
func ParseMode(s string) (Mode, error) {
	m := Mode(strings.TrimSpace(strings.ToLower(s)))
	if m == "" {
		return ModeVisible, nil
	}
	for _, v := range allModes {
		if v == m {
			return v, nil
		}
	}
	return "", fmt.Errorf("未知的字段可见性模式 %q(合法值:%s)", s, modeListString())
}

// modeListString 合法模式列表(拼给错误信息看)
func modeListString() string {
	parts := make([]string, 0, len(allModes))
	for _, v := range allModes {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, ", ")
}

// MaskType 打码方式
//
// ⚠️ **MaskAuto 不是一种打码方式,是"按字段名猜"** ——
// 猜不出来时上层必须在**配置期**就拒(见 service 层的校验),
// 运行期兜底是"整串打码"(安全方向)。
type MaskType string

const (
	// MaskAuto 按字段名自动推断(手机号 / 身份证 / 姓名 / 邮箱 …)
	MaskAuto MaskType = ""
	// MaskPhone 手机号(138****1234)
	MaskPhone MaskType = "phone"
	// MaskIDCard 身份证(1101**********1234)
	MaskIDCard MaskType = "id_card"
	// MaskName 姓名(张*)
	MaskName MaskType = "name"
	// MaskEmail 邮箱(z******@qq.com)
	MaskEmail MaskType = "email"
	// MaskBankCard 银行卡(****1234)
	MaskBankCard MaskType = "bank_card"
	// MaskContractNo 合同编号(HFC****0234)
	MaskContractNo MaskType = "contract_no"
	// MaskFull 整串打码(等长星号)—— 猜不出类型时的兜底
	MaskFull MaskType = "full"
)

// allMaskTypes 全部合法打码方式(顺序即前端下拉框顺序)
var allMaskTypes = []MaskType{
	MaskPhone, MaskIDCard, MaskName, MaskEmail,
	MaskBankCard, MaskContractNo, MaskFull,
}

// AllMaskTypes 全部合法的打码方式
func AllMaskTypes() []MaskType {
	out := make([]MaskType, len(allMaskTypes))
	copy(out, allMaskTypes)
	return out
}

// ParseMaskType 解析打码方式
//
// ⚠️ **空串 = auto** —— 按字段名推断。
func ParseMaskType(s string) (MaskType, error) {
	t := MaskType(strings.TrimSpace(strings.ToLower(s)))
	if t == MaskAuto {
		return MaskAuto, nil
	}
	for _, v := range allMaskTypes {
		if v == t {
			return v, nil
		}
	}
	return "", fmt.Errorf("未知的打码方式 %q(合法值:%s)", s, maskTypeListString())
}

// maskTypeListString 合法打码方式列表(拼给错误信息看)
func maskTypeListString() string {
	parts := make([]string, 0, len(allMaskTypes))
	for _, v := range allMaskTypes {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, ", ")
}

// autoMaskTypes 字段名 → 打码方式的推断表
//
// ⚠️ **精确匹配,不做前缀/后缀匹配** ——
// 后缀匹配会把 `project_name` / `dept_name` / `owner_name` 全打成 "张*",
// 而那些是**机构名不是人名** —— 打完之后整个列表没法看了。
//
// ⚠️ **`name` 这一项是特例** —— 它精确匹配的只有"字段名就叫 name"的情况。
// 动态集合的字段标识是用户自己起的("姓名"→ key `name`),那里确实该按人名打。
var autoMaskTypes = map[string]MaskType{
	"phone":           MaskPhone,
	"mobile":          MaskPhone,
	"telephone":       MaskPhone,
	"contact_phone":   MaskPhone,
	"submitter_phone": MaskPhone,
	"to_phone":        MaskPhone,
	"id_card":         MaskIDCard,
	"idcard":          MaskIDCard,
	"id_no":           MaskIDCard,
	"real_name":       MaskName,
	"contact_name":    MaskName,
	"name":            MaskName,
	"email":           MaskEmail,
	"mail":            MaskEmail,
	"bank_card":       MaskBankCard,
	"card_no":         MaskBankCard,
	"contract_no":     MaskContractNo,
}

// AutoMaskType 按字段名推断打码方式
//
// ⚠️ **推断不出来返回 MaskAuto(空串)** ——
// 调用方(配置期)必须据此拒绝,而不是"随便打个码"。
func AutoMaskType(field string) MaskType {
	return autoMaskTypes[strings.TrimSpace(strings.ToLower(field))]
}

// Rule 一条字段级可见性规则
//
// ⚠️ **一条规则 = 一个字段** ——
// 同一角色同一业务域上,同一个字段只能有一条规则
// (唯一键 role_id + module + field)。
type Rule struct {
	// Field 字段名(响应 JSON 里的键名)
	Field string `json:"field"`
	// Mode 可见性模式
	Mode Mode `json:"mode"`
	// MaskType 打码方式(仅 ModeMasked 时有意义)
	MaskType MaskType `json:"mask_type"`
}

// Policy 一组已解析的字段级规则
//
// ⚠️ **不可变** —— 解析一次,到处用。
// 并发读不需要锁(与 pkg/scope.Scope 同一条纪律)。
type Policy struct {
	rules map[string]Rule
}

// NewPolicy 把一组规则解析成策略
//
// ⚠️ **同名字段后者覆盖前者** ——
// 调用方(服务层)读到的行本身就有顺序,覆盖语义与"后写的赢"一致。
func NewPolicy(rules []Rule) Policy {
	m := make(map[string]Rule, len(rules))
	for _, r := range rules {
		if strings.TrimSpace(r.Field) == "" {
			continue
		}
		m[r.Field] = r
	}
	return Policy{rules: m}
}

// Empty 这条策略有没有任何规则
//
// ⚠️ **空策略 = 什么都不做** ——
// 上层据此**原样透传响应**(不做 marshal/unmarshal 往返),
// 保证"没配规则"时响应逐字节不变。
func (p Policy) Empty() bool { return len(p.rules) == 0 }

// Rules 规则列表(按字段名排序)
func (p Policy) Rules() []Rule {
	out := make([]Rule, 0, len(p.rules))
	for _, r := range p.rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// Lookup 查某个字段的规则
func (p Policy) Lookup(field string) (Rule, bool) {
	r, ok := p.rules[field]
	return r, ok
}

// Report 一条策略会收窄哪些字段
//
// # 这份报告是给**前端**看的 —— M168
//
// 服务端"隐藏"是**删键** ⇒ 响应里**根本没有那个键** ⇒
// 而前端的表格列定义还列着它 ⇒ **列还在、格是空的**;
// 表单里那个字段也还显示 ⇒ 用户还能把**打码后的值**原样提交回去(数据损坏)。
//
// ⇒ 前端必须知道"这次响应里哪些字段被收窄了"才能把列/字段藏起来。
//
// # ⚠️ **为什么不让前端自己算**
//
// 前端若自己拿"规则表"算一遍,就多了一份实现 —— 与服务端那份漂移时
// 表现是"界面说有这列、后端说没有"或反过来(后者是**泄露**)。
//
// ⇒ **报告由执行层(服务端)算,前端只读**(与 M149 同一条纪律:
// 「显示层不许自己算,必须读执行层那个权威字段」)。
type Report struct {
	// Hidden 被隐藏的字段名(响应里根本没有这个键)
	Hidden []string `json:"hidden"`
	// Masked 被打码的字段名(值是打码后的)
	Masked []string `json:"masked"`
}

// Empty 这份报告有没有内容
func (r Report) Empty() bool {
	return len(r.Hidden) == 0 && len(r.Masked) == 0
}

// Report 这条策略会收窄哪些字段
//
// ⚠️ **报的是"规则说了什么",不是"这次响应里改了什么"** ——
// 哪怕这次响应里根本没有那个字段,也要报出来。
//
// 理由:前端要的是"这一列该不该存在",而不是"这一次有没有动过它"。
// 若只在"真的改了"时报,那"响应里本来就没有这个字段"的情形下
// 前端拿不到报告 ⇒ **列照样留着**(空列)⇒ 症状没治好。
func (p Policy) Report() Report {
	out := Report{Hidden: []string{}, Masked: []string{}}
	for _, r := range p.Rules() {
		switch r.Mode {
		case ModeHidden:
			out.Hidden = append(out.Hidden, r.Field)
		case ModeMasked:
			out.Masked = append(out.Masked, r.Field)
		}
	}
	return out
}

// ApplyTree 把策略作用到一棵 JSON 树(any)上
//
// ⚠️ **按字段名匹配,不按路径** ——
// 一个字段名在树里出现几次就作用几次(列表里的每一行、嵌套对象里的同名键)。
//
// ⇒ 这个方向是**安全的**:多打码一次只是少给一点信息,
// 而"按路径精确匹配"漏掉一处 = 泄露。
//
// ⚠️ **只处理 JSON 解码后的形状**(`map[string]any` / `[]any`)——
// 非 map / 非 slice 的值原样返回。
func (p Policy) ApplyTree(v any) (any, bool) {
	if len(p.rules) == 0 {
		return v, false
	}
	return p.applyValue(v)
}

// applyValue 递归处理一个值
func (p Policy) applyValue(v any) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return p.applyMap(t)
	case []any:
		changed := false
		for i, item := range t {
			nv, c := p.applyValue(item)
			if c {
				t[i] = nv
				changed = true
			}
		}
		return t, changed
	default:
		return v, false
	}
}

// applyMap 处理一个 JSON 对象
func (p Policy) applyMap(m map[string]any) (any, bool) {
	changed := false
	for key, val := range m {
		rule, ok := p.rules[key]
		if !ok {
			// 没有规则 ⇒ 继续往里走(嵌套对象里的同名字段也要作用)
			nv, c := p.applyValue(val)
			if c {
				m[key] = nv
				changed = true
			}
			continue
		}
		switch rule.Mode {
		case ModeHidden:
			// ⚠️ **整个键删掉** —— 连字段名都不留下
			delete(m, key)
			changed = true
		case ModeMasked:
			nv, c := p.maskValue(val, rule.MaskType)
			if c {
				m[key] = nv
				changed = true
			}
			// 打码之后仍要往里走:值本身是个对象时,里面的同名字段也要作用
			if inner, ic := p.applyValue(m[key]); ic {
				m[key] = inner
				changed = true
			}
		default: // ModeVisible
			nv, c := p.applyValue(val)
			if c {
				m[key] = nv
				changed = true
			}
		}
	}
	return m, changed
}

// maskValue 给一个值打码
//
// ⚠️ **只打码字符串** ——
// 非字符串(数字 / 布尔 / 对象 / 数组)原样返回。
// 数字类字段不是敏感信息,硬把它变成字符串会破坏前端类型契约。
func (p Policy) maskValue(v any, mt MaskType) (any, bool) {
	s, ok := v.(string)
	if !ok {
		return v, false
	}
	return Mask(s, mt), true
}

// Mask 按打码方式给一个字符串打码
//
// ⚠️ **打码方式为空(猜不出来)⇒ 整串打码** ——
// 这是**安全方向**的兜底:宁可全打码,也不能"原样返回"当没事发生。
//
// ⚠️ **掩码函数全部复用 internal/pkg/desensitize** ——
// 那一组是仓库里审计过的(M112 修过"输入比标准长度短时一个字符都没藏"的洞),
// 自己再写一套必然搓得不一样。
func Mask(s string, mt MaskType) string {
	if s == "" {
		return s
	}
	switch mt {
	case MaskPhone:
		return desensitize.Phone(s)
	case MaskIDCard:
		return desensitize.IDCard(s)
	case MaskName:
		return desensitize.Name(s)
	case MaskEmail:
		return desensitize.Email(s)
	case MaskBankCard:
		return desensitize.BankCard(s)
	case MaskContractNo:
		return desensitize.ContractNo(s)
	case MaskFull:
		return fullMask(s)
	default:
		// MaskAuto / 未知 ⇒ 兜底整串打码
		return fullMask(s)
	}
}

// fullMask 整串打码(等长星号)
//
// ⚠️ **保留长度** —— 前端的表格列宽 / 校验规则不会因为长度变了而错乱。
func fullMask(s string) string {
	n := len([]rune(s))
	if n == 0 {
		return s
	}
	return strings.Repeat("*", n)
}

// ApplyJSON 把策略作用到一段 JSON 字节上
//
// ⚠️ **UseNumber 是必须的,不是优化** ——
// sonyflake 主键是 17 位数(当前 ≈9.7e16),超出 float64 的 2^53 精确整数范围。
// 走 float64 中转 ⇒ 尾数被静默截断 ⇒ 前端拿到的是**另一个 id** ⇒
// 按那个 id 查详情 404(仓库里记过同一个坑的另一个入口)。
// json.Number 保留原始字面量,重新序列化时逐字输出。
//
// ⚠️ **没改过就返回原始字节** ——
// 不做无谓的 marshal/unmarshal 往返,保证"没配规则"时响应逐字节不变。
func (p Policy) ApplyJSON(raw []byte) ([]byte, bool, error) {
	if len(p.rules) == 0 {
		return raw, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return raw, false, fmt.Errorf("解析响应 JSON 失败: %w", err)
	}
	out, changed := p.ApplyTree(v)
	if !changed {
		return raw, false, nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw, false, fmt.Errorf("序列化脱敏后的响应失败: %w", err)
	}
	return b, true, nil
}

// Overlay 把另一条策略叠在本策略上(后者优先)
//
// ⚠️ **这是"逐字段就近覆盖"的实现** ——
// 默认规则组(module="")管所有业务域,业务域专属规则只覆盖**它自己写了的那些字段**。
//
// ⇒ 与行级数据范围的"整组就近覆盖"**故意不一样**:
// 行级是"某业务域配了就完全用这一组,默认组不参与";
// 字段级是"默认组的限制不会被某个业务域的其它规则悄悄顶掉"。
//
// 举例:默认组配了"phone 隐藏",owner 域又配了"real_name 打码" ——
// 整组覆盖 ⇒ owner 上 phone 露出来了(限制被悄悄顶掉);
// 逐字段覆盖 ⇒ owner 上 phone 仍然隐藏,real_name 打码。
//
// ⇒ 安全方向:多藏一点没关系,少藏一点是泄露。
func (p Policy) Overlay(other Policy) Policy {
	if len(other.rules) == 0 {
		return p
	}
	m := make(map[string]Rule, len(p.rules)+len(other.rules))
	for k, v := range p.rules {
		m[k] = v
	}
	for k, v := range other.rules {
		m[k] = v
	}
	return Policy{rules: m}
}
