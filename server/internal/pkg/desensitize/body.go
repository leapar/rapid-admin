// 通用请求体 / 查询串脱敏 —— 供审计采集端使用
//
// # 为什么要有这个
//
// 审计中间件要把"这次请求改了什么"记进 sys_operation_log,
// 技术方案 §9.5 的表结构里就有 `request_body TEXT -- 脱敏后` 与
// `response_summary TEXT` 两列。
//
// 但本包原来只有**按字段类型**的脱敏器(Phone / IDCard / Email / Name …)——
// 那些需要调用方**知道**这个字段是什么类型才能用。
// 请求体是**结构未知的 JSON**:调用方不可能逐字段判断类型。
//
// ⇒ 这里补一个**按字段名**的通用脱敏器:递归遍历 JSON,
//
//	字段名命中敏感词表 ⇒ 值替换成 `***`,其余原样保留。
//
// # 为什么不能"直接存原文"
//
// 审计表是**另一张表**——它有自己的访问路径(审计中心页面 / 导出 / 归档),
// 生命周期还比业务表长(§9.5:操作日志保留 5 年)。
// 明文密码 / token 落进去 = 把"最高危的字段"复制到一个"更容易被批量读到"的地方。
//
// # 脱敏口径(刻意不遮蔽业务内容)
//
//	**遮蔽**:凭证类(password / token / secret)、验证码类、证件与银行卡号、
//	  联系方式(phone / mobile / email)。
//	**不遮蔽**:姓名、住址、描述、标题等**业务内容**。
//
//	理由:审计日志要回答的是"谁、什么时候、把哪个业主的什么字段改成了什么"。
//	  把姓名和描述一起遮掉,审计表就只剩一堆 `***`,谁也查不出问题。
//	  而姓名/住址在业务表里本来就是明文(且接口侧另有脱敏),
//	  审计表再存一份不改变暴露面。
//
//	⚠️ **刻意不遮蔽的字段名**:
//	  - `code` —— 业务错误码 / 字典码,遮蔽它等于把审计记录废掉。
//	    验证码另用 `sms_code` / `captcha_code` / `verify_code` 精确匹配。
//	  - `name` / `address` —— 业务内容,理由同上。
//	  - `key` —— 太泛(`idempotency_key` / `cache_key` / `sort_key` 都会被误伤)。
//
// # 边界
//
//   - 非 JSON → 返回 ok=false,调用方决定写什么(本仓约定写 "<非 JSON,N 字节>")。
//   - 数字用 json.Number 保留原文 —— 用 `any` 直接 Unmarshal 会把
//     19 位 sonyflake ID 变成 `1.2345678901234568e+18`,审计记录里
//     的 ID 就对不上了。
//   - 深度 / 数组长度 / 字符串长度 / 总体积都有上限,防"深嵌套炸弹"与"超长文本"。
package desensitize

import (
	"bytes"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

const (
	// RedactedMask 脱敏后的占位值。
	//
	// ⚠️ **不是空串** —— 空串与"字段本来就为空"不可区分,
	// 审计时看到 `""` 分不清是"没传"还是"被脱敏了"。
	RedactedMask = "***"

	// maxDepth 递归深度上限(超过就截断,防深嵌套 JSON 打爆栈)。
	maxDepth = 8

	// maxArrayItems 数组元素上限(超过的丢弃并标注)。
	maxArrayItems = 50

	// maxStringRunes 单个字符串值的字符上限。
	maxStringRunes = 200

	// maxOutputBytes 脱敏结果的总字节上限。
	maxOutputBytes = 4096
)

// sensitiveKeySubstrings 敏感字段名词表(全部小写;按**子串**匹配)
//
// ⚠️ **词表是"宁可多遮"的** —— 遮蔽一个无害字段(如 `token_count`)
// 只是让审计记录少一个数字;漏遮一个 `password` 就是明文落库。
// 但也不能太泛:`code` / `key` / `name` / `sign` 都不在表里
// (见文件头「刻意不遮蔽的字段名」)。
var sensitiveKeySubstrings = []string{
	// 凭证
	"password", "passwd", "pwd",
	"token", "secret",
	"apikey", "api_key", "accesskey", "access_key", "private_key", "privatekey",

	// 验证码(短信 / 图形)—— 注意**不含裸 `code`**
	"sms_code", "smscode",
	"captcha_code", "captchacode",
	"verify_code", "verifycode",
	"verification_code", "verificationcode",

	// 证件 / 银行卡
	"id_card", "idcard",
	"id_no", "idno",
	"identity_card", "identitycard",
	"bank_card", "bankcard",
	"bank_account", "bankaccount",
	"card_no", "cardno",

	// 联系方式
	"phone", "mobile", "email",

	// 签名(防重放签名 / 私钥签名)
	"signature",
}

// IsSensitiveKey 判断字段名是否需要脱敏(大小写不敏感的子串匹配)。
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeySubstrings {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// RedactJSON 把 JSON 请求体脱敏成可入审计表的文本。
//
// 返回 (脱敏后的 JSON 文本, 是否成功解析)。
// 解析失败时 ok=false —— **调用方不要把原文写进审计表**。
func RedactJSON(raw []byte) (string, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	// ⚠️ UseNumber:不让数字变成 float64 ——
	// sonyflake 是 19 位整数,float64 只能保 15~16 位有效数字,
	// 不开这个开关的话审计记录里的 ID 会变成 1.2345678901234568e+18。
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	// 尾部还有内容(`{}{}`)⇒ 不是单个 JSON 值,当作无效
	if dec.More() {
		return "", false
	}
	red := redactValue(v, 0)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// 审计表给人看 ⇒ 不转义 HTML(否则 < > & 会变成 < 之类)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(red); err != nil {
		return "", false
	}
	out := strings.TrimRight(buf.String(), "\n")
	out = truncateBytes(out, maxOutputBytes)
	return out, true
}

// redactValue 递归脱敏一个 JSON 值。
func redactValue(v any, depth int) any {
	if depth > maxDepth {
		return "<深度超限>"
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if IsSensitiveKey(k) {
				out[k] = RedactedMask
				continue
			}
			out[k] = redactValue(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, 0, min(len(t), maxArrayItems))
		for i, val := range t {
			if i >= maxArrayItems {
				out = append(out, "<超出条数上限>")
				break
			}
			out = append(out, redactValue(val, depth+1))
		}
		return out
	case string:
		return truncateRunes(t, maxStringRunes)
	case json.Number:
		// 保留数字原文(不转 float)
		return t
	default:
		return v
	}
}

// RedactQuery 把 query string 脱敏成可入审计表的文本。
//
// 与 RedactJSON 的区别:query 没有结构,只有 k=v 列表;
// 但**脱敏词表是同一份** —— 否则同一个 `token` 参数在 body 里被遮、
// 在 query 里没被遮,就是一个洞。
//
// 空串返回空串(没有 query 就没有内容,不是错误)。
func RedactQuery(rawQuery string) string {
	if strings.TrimSpace(rawQuery) == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		// 解析失败(有裸 & 或 % 坏序列)⇒ 手工切,逐段脱敏
		return redactRawQuery(rawQuery)
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	// 排序 ⇒ 输出稳定(审计记录可 diff、可比较)
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := values[k]
		if IsSensitiveKey(k) {
			parts = append(parts, k+"="+RedactedMask)
			continue
		}
		for _, v := range vs {
			parts = append(parts, k+"="+truncateRunes(v, maxStringRunes))
		}
	}
	out := strings.Join(parts, "&")
	return truncateBytes(out, maxOutputBytes)
}

// redactRawQuery query 解析失败时的兜底:按 & 切,逐段处理。
func redactRawQuery(rawQuery string) string {
	parts := strings.Split(rawQuery, "&")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		k, v, found := strings.Cut(p, "=")
		if !found {
			out = append(out, truncateRunes(p, maxStringRunes))
			continue
		}
		if IsSensitiveKey(k) {
			out = append(out, k+"="+RedactedMask)
			continue
		}
		out = append(out, k+"="+truncateRunes(v, maxStringRunes))
	}
	return truncateBytes(strings.Join(out, "&"), maxOutputBytes)
}

// truncateBytes 按字节截断,但**不切碎 UTF-8 字符**。
//
// ⚠️ 直接 `s[:n]` 会把多字节字符切一半 ⇒ 非法 UTF-8 ⇒
// Go 的 json 编码器把它替换成 U+FFFD ⇒ 审计表里出现 "���"。
func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !isUTF8Boundary(s, cut) {
		cut--
	}
	return s[:cut] + "…(截断)"
}

// truncateRunes 按 rune 截断(不劈开 UTF-8 多字节字符)。
//
// 与 cryptaudit / service.sms_template 里的同名函数同实现 ——
// 仓库里已有几份(各包私有),这里再放一份是因为 Go 的包私有性:
// 跨包复用要把公共 helper 提到 pkg/ 下,那是另一件事,不在本次范围内。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// isUTF8Boundary 判断 s 的第 i 个字节是否是一个 rune 的起始位置。
func isUTF8Boundary(s string, i int) bool {
	if i <= 0 || i >= len(s) {
		return true
	}
	// 续字节的形态是 10xxxxxx
	return s[i]&0xC0 != 0x80
}
