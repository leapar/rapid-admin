// fieldpolicy —— 字段级可见性(列级权限)原语
//
// ## 这个包解决什么问题
//
// 行级数据范围(internal/pkg/scope)回答的是"这个人**能看哪些行**"。
// 但放行一整行之后,那一行里的每个字段也都放行了 ——
// "客服部能看这个业主"同时意味着"客服部能看到业主的手机号"。
//
// 本包把"一行里的哪些字段给他看"变成一条**可配置、可叠加、可执行**的规则:
//
//   - Mode:     明文 / 打码 / 隐藏
//   - MaskType: 手机号 / 身份证 / 姓名 / 邮箱 / 银行卡 / 合同号 / 整串
//   - Rule:     某个角色在某个业务域上对一个字段的一条规则
//   - Policy:   一组已解析的规则
//   - Apply:    把 Policy 作用到 JSON 字节 / JSON 树上
//   - Overlay:  默认规则组与业务域规则组的叠加
//
// ## 关键点
//
//   - **只收窄,不放宽** —— 本包能把已经在响应里的字段打码或删掉,
//     但**不能**把本来就不在响应里的字段(如 `json:"-"` 的加密手机号)变出来。
//   - **"hidden" 是删键,不是置空** —— 置空的话前端仍然知道"这里有个手机号字段",
//     而字段名本身可能就是信息。
//   - **默认组与业务域组是"逐字段叠加"** —— 业务域配的其它字段
//     **顶不掉**默认组的限制(与行级数据范围的"整组就近覆盖"故意不同)。
//   - **没配规则 ⇒ 响应逐字节不变** —— 规则层上线不改变任何现有接口的输出。
//   - **大整数不丢精度** —— 雪花主键是 17 位数,超过 float64 的 2^53;
//     中转 float64 会把 id 静默截断成**另一个 id**。
//
// ## 跑法
//
//	go run ./examples/fieldpolicy
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/fieldpolicy"
)

func main() {
	// ── 一、模式与打码方式 ──────────────────────────────────────
	fmt.Println("=== 可见性模式 ===")
	for _, m := range fieldpolicy.AllModes() {
		fmt.Printf("  %-10s %s\n", m, modeLabel(m))
	}

	fmt.Println("\n=== 打码方式 ===")
	for _, t := range fieldpolicy.AllMaskTypes() {
		fmt.Printf("  %-12s %s\n", t, maskSample(t))
	}

	// ── 二、按字段名推断打码方式 ────────────────────────────────
	fmt.Println("\n=== 按字段名推断(精确匹配,不做后缀匹配)===")
	for _, f := range []string{
		"phone", "contact_phone", "id_card", "real_name", "email",
		"bank_card", "contract_no",
		// ⚠️ **下面三个不是人名** —— 后缀匹配会把它们打成"张*",
		// 而那是机构名,打完之后整个列表没法看了。
		"project_name", "owner_name", "dept_name",
	} {
		mt := fieldpolicy.AutoMaskType(f)
		label := "推不出来(须显式指定 mask_type)"
		if mt != fieldpolicy.MaskAuto {
			label = string(mt)
		}
		fmt.Printf("  %-16s %s\n", f, label)
	}

	// ── 三、作用到响应上 ────────────────────────────────────────
	//
	// ⚠️ 这段 JSON 里的 id 是 17 位数(当前 ≈9.7e16),
	// 超过 float64 的 2^53 精确整数范围。
	raw := []byte(`{"id":94883951113533126,"real_name":"张三","contact_phone":"13812341234","email":"zhang@qq.com"}`)

	fmt.Println("\n=== 原始响应 ===")
	fmt.Printf("  %s\n", raw)

	fmt.Println("\n=== 三条规则:姓名打码 / 手机号打码 / 邮箱隐藏 ===")
	p := fieldpolicy.NewPolicy([]fieldpolicy.Rule{
		{Field: "real_name", Mode: fieldpolicy.ModeMasked, MaskType: fieldpolicy.MaskName},
		{Field: "contact_phone", Mode: fieldpolicy.ModeMasked, MaskType: fieldpolicy.MaskPhone},
		{Field: "email", Mode: fieldpolicy.ModeHidden},
	})
	out, changed, err := p.ApplyJSON(raw)
	if err != nil {
		panic(err)
	}
	fmt.Printf("  变了没有: %v\n", changed)
	fmt.Printf("  %s\n", out)
	fmt.Println("  ⚠️ email 这个键整个没了(hidden 是删键,不是置空)")
	fmt.Println("  ⚠️ id 仍然是 94883951113533126 —— 没被 float64 截断")

	// ── 四、没配规则 ⇒ 逐字节不变 ────────────────────────────────
	fmt.Println("\n=== 没配规则 ⇒ 响应逐字节不变 ===")
	same, changed2, err := fieldpolicy.NewPolicy(nil).ApplyJSON(raw)
	if err != nil {
		panic(err)
	}
	fmt.Printf("  变了没有: %v\n", changed2)
	fmt.Printf("  与原文逐字节相同: %v\n", string(same) == string(raw))

	// ⚠️ **配了但这个响应里没有那个字段 ⇒ 也不该重排** ——
	// 重新序列化会打乱键序,让响应"每次都不一样"没法比对。
	fmt.Println("\n=== 配了但没命中 ⇒ 不重排 ===")
	noHit, changed3, err := fieldpolicy.NewPolicy([]fieldpolicy.Rule{
		{Field: "id_card", Mode: fieldpolicy.ModeHidden},
	}).ApplyJSON(raw)
	if err != nil {
		panic(err)
	}
	fmt.Printf("  变了没有: %v\n", changed3)
	fmt.Printf("  与原文逐字节相同: %v\n", string(noHit) == string(raw))

	// ── 五、默认组与业务域组叠加 ────────────────────────────────
	//
	// ⚠️ **逐字段覆盖** —— 业务域配的"real_name 打码"
	// **顶不掉**默认组的"id_card 隐藏"。
	fmt.Println("\n=== 默认组 + 业务域组(逐字段叠加)===")
	def := fieldpolicy.NewPolicy([]fieldpolicy.Rule{
		{Field: "id_card", Mode: fieldpolicy.ModeHidden},
		{Field: "contact_phone", Mode: fieldpolicy.ModeHidden},
	})
	mod := fieldpolicy.NewPolicy([]fieldpolicy.Rule{
		{Field: "real_name", Mode: fieldpolicy.ModeMasked, MaskType: fieldpolicy.MaskName},
	})
	merged := def.Overlay(mod)
	for _, r := range merged.Rules() {
		fmt.Printf("  %-16s %-8s %s\n", r.Field, r.Mode, r.MaskType)
	}
	fmt.Println("  ⚠️ contact_phone 仍然隐藏 —— 业务域没写它,就不能悄悄把它放开")

	// ── 六、嵌套结构里的同名字段 ────────────────────────────────
	fmt.Println("\n=== 按字段名匹配,嵌套里的同名字段也作用 ===")
	nested := []byte(`{"list":[{"contact_phone":"13812341234"},{"nested":{"contact_phone":"13912341234"}}]}`)
	out2, _, err := p.ApplyJSON(nested)
	if err != nil {
		panic(err)
	}
	fmt.Printf("  %s\n", out2)
}

// modeLabel 模式的中文说明
func modeLabel(m fieldpolicy.Mode) string {
	switch m {
	case fieldpolicy.ModeVisible:
		return "明文返回(不做处理)"
	case fieldpolicy.ModeMasked:
		return "打码后返回"
	case fieldpolicy.ModeHidden:
		return "从响应里删掉这个键"
	default:
		return ""
	}
}

// maskSample 打码方式的效果样例
func maskSample(t fieldpolicy.MaskType) string {
	switch t {
	case fieldpolicy.MaskPhone:
		return fieldpolicy.Mask("13812341234", t)
	case fieldpolicy.MaskIDCard:
		return fieldpolicy.Mask("110101199001011234", t)
	case fieldpolicy.MaskName:
		return fieldpolicy.Mask("张三", t)
	case fieldpolicy.MaskEmail:
		return fieldpolicy.Mask("zhang@qq.com", t)
	case fieldpolicy.MaskBankCard:
		return fieldpolicy.Mask("6222021234567890", t)
	case fieldpolicy.MaskContractNo:
		return fieldpolicy.Mask("HFC2024001", t)
	case fieldpolicy.MaskFull:
		return fieldpolicy.Mask("随便什么内容", t)
	default:
		return ""
	}
}
