package dynx

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Choice 下拉选项里的一项
type Choice struct {
	// Value 存进数据库的值(英文)
	Value string `json:"value"`
	// Label 展示给用户看的名字(中文)
	Label string `json:"label"`
}

// Options select / multiselect 的选项集
//
// ⚠️ **存成 JSON 文本而不是 JSON 列** ——
// 本仓没有 `gorm.io/datatypes` 依赖(见 go.mod),JSON 列要么手写
// Scanner/Valuer,要么存 TEXT。存 TEXT 的代价只是"读的时候自己 unmarshal",
// 而收益是**跨方言一致**(MySQL 的 JSON 列类型在 SQLite 里不存在)。
type Options struct {
	// Choices 手工配置的选项集
	Choices []Choice `json:"choices"`
	// Source 选项来源(关联系统表)。
	//
	// ⚠️ **与 Choices 互斥** —— 一个字段要么手工填,要么关联一张表。
	// ⚠️ **关联表的选项不落库** —— 落库的只是"从哪张表取"这一条配置,
	// 选项是读的时候现查的 ⇒ 表里新增一行,表单里立刻多一个选项。
	Source *Source `json:"source,omitempty"`

	// Min 数值下限(int / number;nil = 不启用)
	//
	// ⚠️ **指针不是值** —— 0 是一个有意义的下限(如"数量 ≥ 0"),
	// 用值类型就永远表达不出"不启用"和"必须 ≥ 0"的区别(PocketBase 同款)。
	Min *float64 `json:"min,omitempty"`
	// Max 数值上限(int / number;nil = 不启用)
	Max *float64 `json:"max,omitempty"`
	// MinLength 文本最小长度(text / longtext;按字符数,中文算 1;nil = 不启用)
	MinLength *int `json:"min_length,omitempty"`
	// MaxLength 文本最大长度(text / longtext;按字符数;nil = 不启用)
	MaxLength *int `json:"max_length,omitempty"`
	// Pattern 文本正则(text;Go 语法,整串匹配;空 = 不启用)
	Pattern string `json:"pattern,omitempty"`

	// Relation 关联配置(relation 类型专用;其它类型为 nil)
	//
	// ⚠️ **与 select 的 Source 是两回事,别混** ——
	// Source 是"选项从哪张**系统表**来"(user / role / dept),落库的是那行的**值列**;
	// Relation 是"引用哪个**业务集合**的哪条**记录**",落库的是目标记录的 **id**。
	// 两者唯一的共同点是都"读的时候现查显示文本",落库形态完全不同。
	Relation *RelationOptions `json:"relation,omitempty"`
}

// RelationOptions relation 字段的配置(指向哪个集合、显示哪个字段)
type RelationOptions struct {
	// Target 目标集合的编码(物理表 = dyn_<target>)
	//
	// ⚠️ **存的是集合编码不是集合 id** ——
	// 集合编码不可改(dynx.ValidateCode + 设计面没有改 code 的入口),
	// 而 id 在"集合删了重建"之后会变。编码是稳定标识。
	Target string `json:"target"`
	// DisplayField 目标集合里用来显示的字段标识(空 = 自动挑目标集合的第一个文本字段)
	//
	// ⚠️ **显示字段被停用不算错** —— 读的时候回退(自动挑文本字段 / 显示 id),
	// 不让"管理员停了个字段"把别的集合的列表页打挂(旁路纪律)。
	// 设计期(ValidateRelationConfig)会校验它存在,运行期只做宽容回退。
	DisplayField string `json:"display_field,omitempty"`
}

// Source 选项来源(关联系统表)
//
// ⚠️ **只有一张白名单表可选,不是"任意表"** ——
// 让用户填表名等于把 SQL 的表名交给用户拼 ⇒ 那是注入面。
// 见 relation.go 的来源注册表。
type Source struct {
	// Kind 来源种类:v1 只有 "table"
	Kind string `json:"kind"`
	// Key 来源表的标识(见 relation.go)
	Key string `json:"key"`
}

// SourceKindTable 关联系统表
const SourceKindTable = "table"

// HasSource 有没有配置来源
func (o Options) HasSource() bool {
	return o.Source != nil && strings.TrimSpace(o.Source.Key) != ""
}

// HasChoices 有没有选项
func (o Options) HasChoices() bool { return len(o.Choices) > 0 }

// Contains 该值是不是一个合法选项
func (o Options) Contains(v string) bool {
	for _, c := range o.Choices {
		if c.Value == v {
			return true
		}
	}
	return false
}

// Values 列出全部选项值(顺序固定)
func (o Options) Values() []string {
	out := make([]string, 0, len(o.Choices))
	for _, c := range o.Choices {
		out = append(out, c.Value)
	}
	return out
}

// Validate 选项集自身的合法性:值非空 + 不重复
//
// ⚠️ **重复值必须在这里挡住** ——
// 重复的选项值会让"这个记录的单选值是哪个"变得有歧义,
// 而这种歧义在数据存进去之后是**查不出来**的。
//
// ⚠️ **这里不校验来源的 key 在不在注册表里** ——
// Validate 会在**读**的时候被调(ParseOptions),而注册表是会变的
// (加表 / 删表)⇒ 一个老字段的来源被删了,读的时候不该整个选项集解析失败
// (那会让那个字段的选项**悄无声息地消失**)。
// ⇒ "key 是不是合法的来源"在**设计期**查(见 ValidateSourceKey)。
func (o Options) Validate() error {
	seen := make(map[string]struct{}, len(o.Choices))
	for _, c := range o.Choices {
		if strings.TrimSpace(c.Value) == "" {
			return fmt.Errorf("选项值不能为空")
		}
		if _, dup := seen[c.Value]; dup {
			return fmt.Errorf("选项值 %q 重复", c.Value)
		}
		seen[c.Value] = struct{}{}
	}
	if o.Source != nil {
		kind := strings.TrimSpace(o.Source.Kind)
		if kind != "" && kind != SourceKindTable {
			return fmt.Errorf("未知的选项来源种类 %q(只支持 %q)", kind, SourceKindTable)
		}
		if strings.TrimSpace(o.Source.Key) == "" {
			return fmt.Errorf("选项来源的 key 不能为空")
		}
	}
	if o.Relation != nil && strings.TrimSpace(o.Relation.Target) == "" {
		return fmt.Errorf("关联字段的目标集合不能为空")
	}
	return nil
}

// ValidateSourceKey 校验来源的 key 在不在注册表里(设计期用)
//
// ⚠️ **与 Options.Validate 分开** —— 见 Validate 的说明:
// 读的时候不能因为"注册表变了"就让老字段的选项集解析失败。
func ValidateSourceKey(key string) error {
	k := strings.TrimSpace(key)
	if k == "" {
		return fmt.Errorf("选项来源的 key 不能为空")
	}
	if _, ok := LookupRelationSource(k); !ok {
		return fmt.Errorf("未知的选项来源 %q(可选:%s)", k, strings.Join(RelationSourceKeys(), ", "))
	}
	return nil
}

// ParseOptions 解析选项集(JSON 文本)
//
// ⚠️ **空串不算错** —— 一个还没配选项的 select 字段,存的就是空串。
// 解析失败才算错(那是真数据坏了)。
func ParseOptions(raw string) (Options, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Options{}, nil
	}
	var o Options
	if err := json.Unmarshal([]byte(trimmed), &o); err != nil {
		return Options{}, fmt.Errorf("选项集不是合法 JSON: %w", err)
	}
	if err := o.Validate(); err != nil {
		return Options{}, err
	}
	return o, nil
}

// MarshalOptions 把选项集序列化成 JSON 文本
//
// ⚠️ **有 source 时一定要序列化出来** ——
// 以前这里是 `if len(o.Choices) == 0 { return "", nil }`,
// 那样"关联表来源"的配置会被**静默丢掉**:
// 字段建好了,却变成一个没有任何选项的单选 —— 而前端不报错,
// 用户只能在录入时发现"下拉框是空的"。
func MarshalOptions(o Options) (string, error) {
	if err := o.Validate(); err != nil {
		return "", err
	}
	if len(o.Choices) == 0 && !o.HasSource() && o.Relation == nil {
		return "", nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		return "", fmt.Errorf("序列化选项集失败: %w", err)
	}
	return string(b), nil
}

// ============================================================================
// 值约束(2026-11-03 M1,对标 PocketBase text.Min/Max/Pattern、number.Min/Max)
// ============================================================================

// constraintsByType 各类型认哪些约束
//
// ⚠️ **类型不认的约束 ≠ 错误,但设计期要挡** ——
// 在 select 上配 min 是设计错误(选项集自己就是约束),静默忽略的话
// 用户以为"配了就生效",实际录入时没有任何限制 —— 比报错危险得多。
// ⇒ 设计期(ValidateForType)报错,运行期(ParseOptions→Normalize)忽略。
var constraintsByType = map[Type][]string{
	TypeText:        {"min_length", "max_length", "pattern"},
	TypeLongText:    {"min_length", "max_length"},
	TypeInt:         {"min", "max"},
	TypeNumber:      {"min", "max"},
	TypeSelect:      {},
	TypeMultiSelect: {},
	TypeBool:        {},
	TypeDate:        {},
	TypeDateTime:    {},
	TypeJSON:        {},
	TypeFile:        {},
	TypeRelation:    {},
}

// ValidateForType 设计期校验:约束自洽 + 约束只配给认它的类型
//
// ⚠️ **只在校验选项集时调一次(设计期),不在读路径上调** ——
// 读路径(ParseOptions)只跑 Options.Validate(结构校验),
// 约束校验失败不该让老字段的**读**挂掉(设计器存进去的都是校验过的;
// 就算历史数据有坏约束,读出来照旧,写入时 Normalize 才拦)。
func ValidateForType(t Type, o Options) error {
	allowed, ok := constraintsByType[t]
	if !ok {
		return fmt.Errorf("未知的字段类型 %q", t)
	}
	used := map[string]bool{}
	if o.Min != nil {
		used["min"] = true
	}
	if o.Max != nil {
		used["max"] = true
	}
	if o.MinLength != nil {
		used["min_length"] = true
	}
	if o.MaxLength != nil {
		used["max_length"] = true
	}
	if strings.TrimSpace(o.Pattern) != "" {
		used["pattern"] = true
	}
	for u := range used {
		okAllowed := false
		for _, a := range allowed {
			if a == u {
				okAllowed = true
				break
			}
		}
		if !okAllowed {
			return fmt.Errorf("约束 %s 不适用于 %s 类型(该类型支持:%s)",
				u, t, orNone(allowed))
		}
	}
	// 数值:min ≤ max
	if o.Min != nil && o.Max != nil && *o.Min > *o.Max {
		return fmt.Errorf("数值下限(%v)不能大于上限(%v)", *o.Min, *o.Max)
	}
	// 文本:MinLength ≤ MaxLength,且都必须 > 0
	if o.MinLength != nil && *o.MinLength <= 0 {
		return fmt.Errorf("最小长度必须大于 0(不启用就别配)")
	}
	if o.MaxLength != nil && *o.MaxLength <= 0 {
		return fmt.Errorf("最大长度必须大于 0(不启用就别配)")
	}
	if o.MinLength != nil && o.MaxLength != nil && *o.MinLength > *o.MaxLength {
		return fmt.Errorf("最小长度(%d)不能大于最大长度(%d)", *o.MinLength, *o.MaxLength)
	}
	// 正则:必须能编译
	if p := strings.TrimSpace(o.Pattern); p != "" {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("格式约束不是合法正则: %w", err)
		}
	}
	return nil
}

// orNone 空切片显示成"(无)"
func orNone(ss []string) string {
	if len(ss) == 0 {
		return "(无)"
	}
	return strings.Join(ss, ", ")
}
