package desensitize

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// ── 这一组守的是什么 ─────────────────────────────────────────────────────
//
// 审计中间件要把请求体写进 sys_operation_log(§9.5 `request_body -- 脱敏后`)。
// 脱敏器一旦漏一个字段,明文就进了另一张表 —— 而且**没有任何东西会红**。
//
// 三个最容易漏的点:
//  1. 词表按**子串**匹配,`old_password` / `new_password` / `access_token`
//     都得被遮 —— 少写一个词条就是明文落库。
//  2. 数字不能变成 float64 —— sonyflake 是 19 位整数,float64 只保 15~16 位,
//     审计记录里的 ID 会变成 1.2345678901234568e+18。
//  3. 截断不能切碎 UTF-8 —— 半个汉字会让 Go 的 json 编码器写成 U+FFFD,
//     审计表里出现 "���"。
//
// ── 可逆探针(拆掉实现看会不会红)────────────────────────────────────────
//
//	从词表里删掉 "password"        → TestM117_RedactJSON_MasksCredentialFields 红
//	把 UseNumber() 去掉            → TestM117_RedactJSON_KeepsBigIntExact 红
//	把 truncateRunes 换成按字节切   → TestM117_RedactJSON_OutputIsValidUTF8 红
//	把词表里加上 "code"            → TestM117_RedactJSON_KeepsBusinessFields 红

// ── 词表 ────────────────────────────────────────────────────────────────

func TestM117_IsSensitiveKey(t *testing.T) {
	masked := []string{
		"password", "Password", "old_password", "new_password", "pwd",
		"token", "access_token", "refresh_token", "idToken",
		"secret", "client_secret", "api_key", "apiKey", "private_key",
		"sms_code", "smsCode", "captcha_code", "captchaCode",
		"verify_code", "verifyCode", "verification_code",
		"id_card", "idCard", "idcard", "id_no", "identity_card",
		"bank_card", "bankCard", "bank_account", "card_no",
		"phone", "mobile", "Phone", "email", "Email",
		"signature",
	}
	for _, k := range masked {
		if !IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = false, 期望 true(该字段会明文落库)", k)
		}
	}

	// ⚠️ 这些**不能**被遮 —— 遮了审计记录就没意义了
	kept := []string{
		"code", "errcode", "status_code", // 业务码 / 错误码
		"name", "real_name", "username", "owner_name", // 业务内容
		"address", "description", "title", // 业务内容
		"idempotency_key", "cache_key", "sort_key", "key", // 泛化的 key
		"id", "ids", "page", "page_size", "projectId",
	}
	for _, k := range kept {
		if IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = true, 期望 false(遮了就把业务信息毁掉了)", k)
		}
	}
}

// ── RedactJSON ──────────────────────────────────────────────────────────

func TestM117_RedactJSON_MasksCredentialFields(t *testing.T) {
	raw := []byte(`{
		"username":"zhangsan",
		"password":"hunter2",
		"old_password":"old-pwd",
		"new_password":"new-pwd",
		"access_token":"eyJhbGciOi",
		"sms_code":"123456",
		"phone":"13800138000",
		"id_card":"110101199001011234",
		"bank_card":"6222021234567890",
		"email":"a@b.com",
		"name":"张三",
		"code":40100
	}`)

	out, ok := RedactJSON(raw)
	if !ok {
		t.Fatal("RedactJSON 返回 ok=false,期望解析成功")
	}

	// 明文一个都不许出现
	for _, secret := range []string{
		"hunter2", "old-pwd", "new-pwd", "eyJhbGciOi",
		"123456", "13800138000", "110101199001011234",
		"6222021234567890", "a@b.com",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("脱敏结果里出现了明文 %q:\n%s", secret, out)
		}
	}

	// 非敏感字段必须原样保留
	for _, keep := range []string{"zhangsan", "张三", "40100"} {
		if !strings.Contains(out, keep) {
			t.Errorf("脱敏结果里丢了业务字段 %q:\n%s", keep, out)
		}
	}

	// 被遮的字段值统一是 ***
	if !strings.Contains(out, `"password":"***"`) {
		t.Errorf("password 没有被替换成 %s:\n%s", RedactedMask, out)
	}
}

func TestM117_RedactJSON_MasksNested(t *testing.T) {
	raw := []byte(`{
		"user":{"name":"李四","password":"p1","contacts":[{"phone":"13900000000"},{"phone":"13700000000"}]},
		"items":[{"secret":"s"},{"ok":1}]
	}`)

	out, ok := RedactJSON(raw)
	if !ok {
		t.Fatal("RedactJSON 返回 ok=false,期望解析成功")
	}
	if strings.Contains(out, "p1") || strings.Contains(out, "13900000000") || strings.Contains(out, "13700000000") || strings.Contains(out, `"s"`) {
		t.Errorf("嵌套结构里有明文没被遮:\n%s", out)
	}
	if !strings.Contains(out, "李四") || !strings.Contains(out, `"ok":1`) {
		t.Errorf("嵌套结构里的业务字段被误遮:\n%s", out)
	}
}

func TestM117_RedactJSON_KeepsBigIntExact(t *testing.T) {
	// sonyflake 是 19 位整数 —— float64 只保 15~16 位有效数字
	const id = "9223372036854775807"
	raw := []byte(`{"id":` + id + `}`)

	out, ok := RedactJSON(raw)
	if !ok {
		t.Fatal("RedactJSON 返回 ok=false,期望解析成功")
	}
	if !strings.Contains(out, id) {
		t.Errorf("大整数被改写了,审计记录里的 ID 对不上:\n%s", out)
	}
	if strings.Contains(out, "e+") || strings.Contains(out, "E+") {
		t.Errorf("大整数变成了科学计数法(说明 UseNumber 没生效):\n%s", out)
	}
}

func TestM117_RedactJSON_KeepsBusinessFields(t *testing.T) {
	// `code` / `name` / `key` 都不能被遮(见词表用例的说明)
	raw := []byte(`{"code":0,"name":"交付验收","idempotency_key":"abc-123","sort_key":"created_at"}`)

	out, ok := RedactJSON(raw)
	if !ok {
		t.Fatal("RedactJSON 返回 ok=false,期望解析成功")
	}
	for _, keep := range []string{`"code":0`, `"name":"交付验收"`, `"idempotency_key":"abc-123"`, `"sort_key":"created_at"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("业务字段被误遮 %s:\n%s", keep, out)
		}
	}
}

func TestM117_RedactJSON_RejectsNonJSON(t *testing.T) {
	cases := []string{
		"",                // 空
		"   ",             // 空白
		"not json at all", // 纯文本
		"<xml/>",          // XML
		`{"a":1}{"b":2}`,  // 两个值
		`{"a":1`,          // 截断的 JSON
	}
	for _, in := range cases {
		out, ok := RedactJSON([]byte(in))
		if ok {
			t.Errorf("RedactJSON(%q) 返回 ok=true,期望 false(非 JSON 必须被拒,否则调用方会把原文写进审计表)", in)
		}
		if out != "" {
			t.Errorf("RedactJSON(%q) 返回了非空结果 %q,期望空串", in, out)
		}
	}
}

func TestM117_RedactJSON_OutputIsValidUTF8(t *testing.T) {
	// 一个全是汉字的长字符串 —— 截断点必须落在 rune 边界上
	long := strings.Repeat("交付验收签字单", 100)
	raw := []byte(`{"title":"` + long + `"}`)

	out, ok := RedactJSON(raw)
	if !ok {
		t.Fatal("RedactJSON 返回 ok=false,期望解析成功")
	}
	if !utf8.ValidString(out) {
		t.Errorf("脱敏结果不是合法 UTF-8(截断切碎了多字节字符):\n%q", out)
	}
	// ⚠️ **必须同时查 JSON 转义形态** ——
	// Go 的 encoding/json 遇到非法 UTF-8 **不会**原样写坏字节,
	// 它写成 6 个字符的转义序列 `\ufffd`。
	// ⇒ 只查 utf8.ValidString 会**恒绿**(输出确实是合法 UTF-8),
	//   坏字节已经被悄悄替换掉了 —— 而替换本身就是我们要发现的症状。
	if strings.Contains(out, "\ufffd") || strings.Contains(out, `\ufffd`) {
		t.Errorf("脱敏结果里出现了替换字符 U+FFFD(截断切碎了多字节字符):\n%q", out)
	}
	// 结果必须仍是合法 JSON(能直接给人看 / 再被解析)
	var back map[string]any
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Errorf("脱敏结果不是合法 JSON:%v\n%s", err, out)
	}
}

func TestM117_RedactJSON_Limits(t *testing.T) {
	t.Run("深度超限被截断而不是爆栈", func(t *testing.T) {
		deep := strings.Repeat(`{"a":`, 50) + `1` + strings.Repeat(`}`, 50)
		out, ok := RedactJSON([]byte(deep))
		if !ok {
			t.Fatal("RedactJSON 返回 ok=false,期望解析成功")
		}
		if !strings.Contains(out, "深度超限") {
			t.Errorf("深嵌套没有被截断标记:\n%s", out)
		}
	})

	t.Run("数组条数超限被截断", func(t *testing.T) {
		items := make([]string, 0, 120)
		for i := 0; i < 120; i++ {
			items = append(items, "1")
		}
		raw := []byte(`{"ids":[` + strings.Join(items, ",") + `]}`)

		out, ok := RedactJSON(raw)
		if !ok {
			t.Fatal("RedactJSON 返回 ok=false,期望解析成功")
		}
		if !strings.Contains(out, "超出条数上限") {
			t.Errorf("数组条数没有被截断:\n%s", out)
		}
	})

	t.Run("单值超长被截断", func(t *testing.T) {
		raw := []byte(`{"text":"` + strings.Repeat("A", 1000) + `"}`)
		out, ok := RedactJSON(raw)
		if !ok {
			t.Fatal("RedactJSON 返回 ok=false,期望解析成功")
		}
		if len(out) > maxOutputBytes+64 {
			t.Errorf("结果长度 %d 超过上限 %d", len(out), maxOutputBytes)
		}
	})

	t.Run("总体积超限被截断", func(t *testing.T) {
		// ⚠️ key 必须各不相同 —— JSON 对象的 key 唯一,
		// 重复的 key 会被 map 覆盖掉,payload 反而变小。
		parts := make([]string, 0, 200)
		for i := 0; i < 200; i++ {
			parts = append(parts, `"k`+strconv.Itoa(i)+`":"`+strings.Repeat("x", 100)+`"`)
		}
		raw := []byte(`{` + strings.Join(parts, ",") + `}`)

		out, ok := RedactJSON(raw)
		if !ok {
			t.Fatal("RedactJSON 返回 ok=false,期望解析成功")
		}
		if len(out) > maxOutputBytes+64 {
			t.Errorf("结果长度 %d 超过上限 %d", len(out), maxOutputBytes)
		}
		if !strings.Contains(out, "截断") {
			t.Errorf("截断没有标记:\n%s", out[len(out)-64:])
		}
	})
}

// ── RedactQuery ─────────────────────────────────────────────────────────

func TestM117_RedactQuery(t *testing.T) {
	out := RedactQuery("page=1&keyword=张三&token=abc123&phone=13800138000&size=20")
	if strings.Contains(out, "abc123") || strings.Contains(out, "13800138000") {
		t.Errorf("query 里出现了明文:\n%s", out)
	}
	for _, keep := range []string{"page=1", "keyword=张三", "size=20"} {
		if !strings.Contains(out, keep) {
			t.Errorf("query 里丢了正常参数 %s:\n%s", keep, out)
		}
	}
	if !strings.Contains(out, "token="+RedactedMask) {
		t.Errorf("token 没有被替换成 %s:\n%s", RedactedMask, out)
	}
}

func TestM117_RedactQuery_EmptyAndMalformed(t *testing.T) {
	if out := RedactQuery(""); out != "" {
		t.Errorf("空 query 应返回空串,得到 %q", out)
	}
	if out := RedactQuery("   "); out != "" {
		t.Errorf("空白 query 应返回空串,得到 %q", out)
	}

	// 坏的百分号序列 ⇒ url.ParseQuery 报错 ⇒ 走手工切分兜底
	out := RedactQuery("token=abc&bad=%zz")
	if strings.Contains(out, "abc") {
		t.Errorf("兜底路径没有脱敏 token:\n%s", out)
	}
	if !strings.Contains(out, "bad=") {
		t.Errorf("兜底路径把正常参数丢了:\n%s", out)
	}
}

func TestM117_RedactQuery_OutputIsStable(t *testing.T) {
	// 同样的输入 ⇒ 同样的输出(审计记录要能 diff / 能比较)
	a := RedactQuery("b=2&a=1&c=3")
	b := RedactQuery("b=2&a=1&c=3")
	if a != b {
		t.Errorf("输出不稳定:%q vs %q", a, b)
	}
	// 且是排序过的
	if a != "a=1&b=2&c=3" {
		t.Errorf("输出没有按 key 排序:%q", a)
	}
}
