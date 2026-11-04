// desensitize —— 敏感字段脱敏
//
// ## 这个包解决什么问题
//
// 日志、错误信息、接口响应里经常混进手机号、身份证、银行卡。
// 脱敏要在**两个地方**做:
//
//   - 结构化字段:Phone() / IDCard() / Name() / Email() ……
//   - 整块文本:RedactJSON() / RedactQuery() —— 按 key 名识别敏感字段再盖掉
//
// ## 关键点
//
//   - 长度不够的**原样返回**:太短的串盖了等于全没了,不如不盖
//   - 按**字符**处理(不是字节),中文不会被劈成乱码
//   - 哈希版本(PhoneHash / IDCardHash)是**盲索引**:可用于"完全相等"查询,
//     但不可逆
//   - IsSensitiveKey 判的是"key 名里有没有敏感词"(phone / password / id_card …)
//
// ## 跑法
//
//	go run ./examples/desensitize
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/desensitize"
)

func main() {
	// ── 一、按类型脱敏 ──────────────────────────────────────────
	fmt.Println("=== 按类型脱敏 ===")
	fmt.Printf("手机号   %-18s → %s\n", "13800001234", desensitize.Phone("13800001234"))
	fmt.Printf("身份证   %-18s → %s\n", "110101199001011234", desensitize.IDCard("110101199001011234"))
	fmt.Printf("姓名     %-18s → %s\n", "张伟丰", desensitize.Name("张伟丰"))
	fmt.Printf("姓名(两字)%-16s → %s\n", "张伟", desensitize.Name("张伟"))
	fmt.Printf("邮箱     %-18s → %s\n", "zhangwei@example.com", desensitize.Email("zhangwei@example.com"))
	fmt.Printf("银行卡   %-18s → %s\n", "6222021234567890123", desensitize.BankCard("6222021234567890123"))
	fmt.Printf("合同号   %-18s → %s\n", "HT20261101000123", desensitize.ContractNo("HT20261101000123"))

	// 太短的原样返回 —— 盖了等于全没了
	fmt.Println()
	fmt.Println("太短的串原样返回:")
	fmt.Printf("手机号 %q → %q\n", "138", desensitize.Phone("138"))
	fmt.Printf("身份证 %q → %q\n", "1101", desensitize.IDCard("1101"))

	// ── 二、盲索引哈希 ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 盲索引哈希 ===")
	h1 := desensitize.PhoneHash("13800001234")
	h2 := desensitize.PhoneHash("13800001234")
	h3 := desensitize.PhoneHash("13800009999")
	fmt.Printf("同号哈希一致 = %v,异号哈希不同 = %v\n", h1 == h2, h1 != h3)
	fmt.Printf("哈希值 = %s\n", h1)
	fmt.Println("不可逆,只能拿同样的明文算出同样的值去比对")

	// ── 三、按 key 名识别敏感字段 ───────────────────────────────
	fmt.Println()
	fmt.Println("=== key 名识别 ===")
	for _, k := range []string{"phone", "user_name", "id_card", "password", "bank_card", "house_id"} {
		fmt.Printf("  %-12s 敏感=%v\n", k, desensitize.IsSensitiveKey(k))
	}

	// ── 四、整块 JSON 脱敏 ──────────────────────────────────────
	fmt.Println()
	fmt.Println("=== JSON 脱敏 ===")
	raw := []byte(`{"id":8801,"name":"RapidAdmin","owner_phone":"13800001234","id_card":"110101199001011234","remark":"正常字段"}`)
	out, changed := desensitize.RedactJSON(raw)
	fmt.Printf("原文: %s\n", raw)
	fmt.Printf("脱敏: %s\n", out)
	fmt.Printf("有改动 = %v\n", changed)

	// ── 五、query string 脱敏 ───────────────────────────────────
	fmt.Println()
	fmt.Println("=== Query 脱敏 ===")
	q := "page=1&phone=13800001234&keyword=示例&password=secret"
	fmt.Printf("原文: %s\n", q)
	fmt.Printf("脱敏: %s\n", desensitize.RedactQuery(q))
}
