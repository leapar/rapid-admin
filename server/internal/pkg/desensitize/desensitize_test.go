package desensitize

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"

	"rapid-admin/internal/pkg/crypto"
)

// ── 本文件的来历 ───────────────────────────────────────────────────────
//
// 这个包之前**一张测试都没有**。它是 §9.3「数据脱敏」的后端安全边界
// (前端那层 maskMiddle 只是 UI 兜底,不是安全边界 —— 见技术方案 §9.3.4)。
//
// 它有 4 条真 bug(2026-10-29 M112 修的):
//
//	P0 盲索引用的是**写在仓库里的公开 key** ⇒ 拿到库的人能反查手机号
//	P1 Name("张李") 返回 "张李" —— 一个星号都没有,等于没脱敏
//	P1 Email 按**字节**切 local-part ⇒ 多字节字符被切成半个 ⇒ 非法 UTF-8
//	P1 短输入(如 7 位手机号 / 8 位身份证 / 6 位合同号)时保留段盖不住任何东西
//
// ⚠️ **BankCard 没有 bug** —— 旧实现 `Repeat("*", len-4) + tail4` 与新的
//   `maskMiddle(s, 0, 4)` 在所有输入上**逐字节等价**。它下面的用例是
//   **特征测试**(锁住契约),不是回归测试 —— 所以它不进探针表。
//
// ── M112 可逆探针记录 ───────────────────────────────────────────────────
//
//	拆法                                        必须变红
//	────────────────────────────────────────  ───────────────────────────────────
//	盲索引退回仓库里的公开占位 key                TestM112_BlindIndex_KeySources
//	盲索引不跟 crypto.blind_key 走               TestM112_BlindIndex_KeySources
//	Name 两字不掩码                              TestM112_Name_TwoChars_Masked
//	Email 按字节切                               TestM112_Email_MultibyteLocalPart
//	短手机号不掩码                                TestM112_Phone_Short_StillMasked
//	短身份证不掩码                                TestM112_IDCard_Short_StillMasked
//	短合同号不掩码                                TestM112_ContractNo_Short_StillMasked
// ───────────────────────────────────────────────────────────────────────

// ── 手机号 ──────────────────────────────────────────────────────────────

// TestM112_Phone_StandardFormat 标准 11 位格式必须是 138****1234
func TestM112_Phone_StandardFormat(t *testing.T) {
	got := Phone("13812345678")
	if got != "138****5678" {
		t.Fatalf("标准手机号脱敏格式应为 138****5678,实际 %q", got)
	}
}

// TestM112_Phone_Short_StillMasked 短于标准长度的输入也必须真的藏住东西
//
// ⚠️ 这是最危险的一条:旧实现 `if len(s) != 11 { return s }` ——
// 一个带 +86 前缀或中间带空格的号码会**原样**进接口响应。
func TestM112_Phone_Short_StillMasked(t *testing.T) {
	for _, in := range []string{"1234567", "12345678", "138 1234 5678", "+8613812345678"} {
		got := Phone(in)
		if got == in {
			t.Fatalf("短/异常手机号 %q 原样返回了(等于没脱敏)", in)
		}
		if strings.Contains(got, strings.TrimPrefix(in, "+86")) && len(in) >= 8 {
			t.Fatalf("手机号 %q 脱敏后 %q 仍含完整明文", in, got)
		}
	}
}

// TestM112_Phone_TinyUnchanged 极短(脏数据)原样返回
func TestM112_Phone_TinyUnchanged(t *testing.T) {
	if got := Phone("123"); got != "123" {
		t.Fatalf("3 位脏数据应原样返回,实际 %q", got)
	}
	if got := Phone(""); got != "" {
		t.Fatalf("空串应原样返回,实际 %q", got)
	}
}

// ── 身份证 ──────────────────────────────────────────────────────────────

// TestM112_IDCard_StandardFormat 标准 18 位格式必须是 1101**********1234
func TestM112_IDCard_StandardFormat(t *testing.T) {
	got := IDCard("110101199001011234")
	if got != "1101**********1234" {
		t.Fatalf("标准身份证脱敏格式应为 1101**********1234,实际 %q", got)
	}
}

// TestM112_IDCard_Short_StillMasked 8 位(旧实现的 0 藏长度)也必须藏住
func TestM112_IDCard_Short_StillMasked(t *testing.T) {
	got := IDCard("12345678")
	if got == "12345678" {
		t.Fatal("8 位身份证原样返回了(旧实现 Repeat 0 次 ⇒ 等于没脱敏)")
	}
	if !strings.Contains(got, "*") {
		t.Fatalf("8 位身份证脱敏后 %q 没有星号", got)
	}
}

// ── 姓名 ────────────────────────────────────────────────────────────────

// TestM112_Name_TwoChars_Masked 两字姓名必须打码
//
// ⚠️ 旧实现 n==2 时 `Repeat("*", n-2)` = **0 个星号** ⇒ "张李" 原样返回。
func TestM112_Name_TwoChars_Masked(t *testing.T) {
	got := Name("张李")
	if got == "张李" {
		t.Fatal("两字姓名原样返回了(等于没脱敏)")
	}
	if got != "张*" {
		t.Fatalf("两字姓名脱敏应为 张*,实际 %q", got)
	}
}

// TestM112_Name_ThreeChars 三个及以上保持"首+星+尾"
func TestM112_Name_ThreeChars(t *testing.T) {
	if got := Name("张三丰"); got != "张*丰" {
		t.Fatalf("三字姓名应为 张*丰,实际 %q", got)
	}
}

// TestM112_Name_SingleUnchanged 单字姓名原样(没有可藏的)
func TestM112_Name_SingleUnchanged(t *testing.T) {
	if got := Name("张"); got != "张" {
		t.Fatalf("单字姓名应原样返回,实际 %q", got)
	}
	if got := Name(""); got != "" {
		t.Fatalf("空串应原样返回,实际 %q", got)
	}
}

// ── 邮箱 ────────────────────────────────────────────────────────────────

// TestM112_Email_StandardFormat 标准格式 z******@qq.com
//
// §9.3.2 矩阵写的是 `z******@qq.com` —— 6 个星号,对应 **7 个字符**的
// local-part(星号数 = local-part 字数 - 1,与 §9.3.1 的公式一致)。
func TestM112_Email_StandardFormat(t *testing.T) {
	got := Email("zhangsa@qq.com")
	if got != "z******@qq.com" {
		t.Fatalf("标准邮箱脱敏格式应为 z******@qq.com,实际 %q", got)
	}
	// 星号数 = local-part 字数 - 1
	if got := Email("zhangsan@qq.com"); got != "z*******@qq.com" {
		t.Fatalf("8 字符 local-part 应为 z*******@qq.com,实际 %q", got)
	}
}

// TestM112_Email_MultibyteLocalPart 多字节 local-part 不能被切成半个字符
//
// ⚠️ 旧实现 `parts[0][:1]` 按字节切 ⇒ "张san@qq.com" 会切出半个 "张"
//
//	⇒ 输出非法 UTF-8 ⇒ Go 的 json.Marshal 编成 U+FFFD 乱码。
func TestM112_Email_MultibyteLocalPart(t *testing.T) {
	got := Email("张san@qq.com")
	if !strings.HasPrefix(got, "张") {
		t.Fatalf("多字节 local-part 脱敏后应保留首字,实际 %q", got)
	}
	if strings.ContainsRune(got, '�') {
		t.Fatalf("多字节 local-part 脱敏产生了非法 UTF-8:%q", got)
	}
	if !strings.HasSuffix(got, "@qq.com") {
		t.Fatalf("域名部分不该被改,实际 %q", got)
	}
	if got == "张san@qq.com" {
		t.Fatal("多字节 local-part 原样返回了(等于没脱敏)")
	}
}

// TestM112_Email_SingleLocalUnchanged 单字符 local-part 原样
func TestM112_Email_SingleLocalUnchanged(t *testing.T) {
	if got := Email("z@qq.com"); got != "z@qq.com" {
		t.Fatalf("单字符 local-part 应原样返回,实际 %q", got)
	}
}

// TestM112_Email_NoAt 原样
func TestM112_Email_NoAt(t *testing.T) {
	if got := Email("not-an-email"); got != "not-an-email" {
		t.Fatalf("不含 @ 的应原样返回,实际 %q", got)
	}
}

// TestEmail_MultiAt_Masked 多 @ 不能整串明文返回
//
// 探针来源:旧实现 `len(parts) != 2` 一律原样返回 ⇒ "a@b@c.com" 的
// local 和域名全明文。现在按第一个 @ 切只遮 local;local 仅 1 字符
// 没得遮时退化为整串中段掩码(与 Phone 的 M112 口径对齐)。
func TestEmail_MultiAt_Masked(t *testing.T) {
	// local 多字符:遮 local,@ 之后的 domain 侧原样透传
	if got := Email("abc@x@y.com"); got != "a**@x@y.com" {
		t.Fatalf("多 @ 邮箱应只遮 local,实际 %q", got)
	}
	// local 仅 1 字符:整串中段掩码兜底,必须藏住东西
	got := Email("a@b@c.com")
	if got == "a@b@c.com" {
		t.Fatal("多 @ 且 local 仅 1 字符的原样返回了(等于没脱敏)")
	}
	if !strings.Contains(got, "*") {
		t.Fatalf("多 @ 邮箱脱敏后 %q 没有星号", got)
	}
	if strings.Contains(got, "b@c") {
		t.Fatalf("多 @ 邮箱脱敏后 %q 仍露出 local@domain 明文", got)
	}
}

// ── 银行卡 / 合同号 ─────────────────────────────────────────────────────

// TestM112_BankCard_StandardFormat 标准格式只留尾 4 位
func TestM112_BankCard_StandardFormat(t *testing.T) {
	got := BankCard("6222021234567890")
	if !strings.HasSuffix(got, "7890") {
		t.Fatalf("银行卡应保留尾 4 位,实际 %q", got)
	}
	if strings.Contains(got, "6222") {
		t.Fatalf("银行卡头 4 位不该露出来,实际 %q", got)
	}
}

// TestM112_BankCard_Short_StillMasked 短银行卡也必须藏住东西
//
// ⚠️ **特征测试,不是回归测试** —— 旧实现(Repeat("*", len-4) + tail4)
//
//	与新实现(maskMiddle(s, 0, 4))在所有输入上等价,这条锁的是契约本身。
func TestM112_BankCard_Short_StillMasked(t *testing.T) {
	got := BankCard("12345678")
	if got == "12345678" {
		t.Fatal("8 位银行卡原样返回了(等于没脱敏)")
	}
	if !strings.Contains(got, "*") {
		t.Fatalf("8 位银行卡脱敏后 %q 没有星号", got)
	}
}

// TestM112_ContractNo_StandardFormat 标准格式 HFC****0234
func TestM112_ContractNo_StandardFormat(t *testing.T) {
	got := ContractNo("HFC20240001")
	if !strings.HasPrefix(got, "HFC") {
		t.Fatalf("合同号应保留首 3 位,实际 %q", got)
	}
	if !strings.HasSuffix(got, "001") {
		t.Fatalf("合同号应保留尾 3 位,实际 %q", got)
	}
}

// TestM112_ContractNo_Short_StillMasked 6 位(旧实现的 0 藏长度)也必须藏住
func TestM112_ContractNo_Short_StillMasked(t *testing.T) {
	got := ContractNo("HFC023")
	if got == "HFC023" {
		t.Fatal("6 位合同号原样返回了(旧实现 Repeat 0 次 ⇒ 等于没脱敏)")
	}
	if !strings.Contains(got, "*") {
		t.Fatalf("6 位合同号脱敏后 %q 没有星号", got)
	}
}

// ── 盲索引 key 来源 ─────────────────────────────────────────────────────

// devHMAC 用仓库里的公开占位 key 算一遍(测试里用来证明"已经不用它了")
func devHMAC(plain string) string {
	mac := hmac.New(sha256.New, []byte(devBlindKey))
	mac.Write([]byte(plain))
	return hex.EncodeToString(mac.Sum(nil))
}

// TestM112_BlindIndex_KeySources 盲索引 key 的三个来源
//
// ⚠️ 这个测试**故意把所有断言放在一个函数里**,因为
// `crypto.InitBlindIndex` 是**单向的**(全局一旦注入就回不去)。
// 拆成多个函数 ⇒ 顺序一变就假红。
//
// 三段:
// A. crypto 未注入时 —— 兜底 key(环境变量 / dev 占位)
// B. crypto 注入后 —— 必须走 crypto.blind_key
// C. 断言:注入后算出来的值与"公开占位 key"算出来的**不一样**
//
//	(这一条是 M112 那条 P0 的判别式)
func TestM112_BlindIndex_KeySources(t *testing.T) {
	const phone = "13812345678"

	// ── 前置:crypto 必须是未注入状态(否则这个测试没有意义) ──
	if crypto.TryBlind() != nil {
		t.Fatal("crypto 盲索引器已被注入:本测试必须在注入前跑(同包内先跑这个函数)")
	}

	// ── A. 未注入:兜底路径 ──
	// A1 没有环境变量 ⇒ 落到 dev 占位值
	t.Setenv("PHONE_HASH_KEY", "")
	blindOnce = sync.Once{}
	key, src := resolveBlind()
	if string(key) != devBlindKey {
		t.Fatalf("无环境变量时应落到 dev 占位 key,实际 %q", string(key))
	}
	if src != BlindSourceDev {
		t.Fatalf("来源应为 %q,实际 %q", BlindSourceDev, src)
	}
	if got := BlindKeySource(); got != BlindSourceDev {
		t.Fatalf("BlindKeySource 应为 %q,实际 %q", BlindSourceDev, got)
	}

	// A2 环境变量够长 ⇒ 用环境变量
	t.Setenv("PHONE_HASH_KEY", "0123456789abcdef0123456789abcdef")
	blindOnce = sync.Once{}
	key, src = resolveBlind()
	if src != BlindSourceEnv {
		t.Fatalf("来源应为 %q,实际 %q", BlindSourceEnv, src)
	}
	if string(key) != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("应使用环境变量里的 key,实际 %q", string(key))
	}

	// A3 环境变量太短 ⇒ 拒绝(跟 M100 的 crypto.BlindIndex 同一口径)⇒ 落回 dev
	t.Setenv("PHONE_HASH_KEY", "short")
	blindOnce = sync.Once{}
	_, src = resolveBlind()
	if src != BlindSourceDev {
		t.Fatalf("过短的环境变量应被拒绝并落回 %q,实际 %q", BlindSourceDev, src)
	}

	// A4 兜底路径下 blindHash = 本地 HMAC(锁住当前的兜底语义)
	t.Setenv("PHONE_HASH_KEY", "")
	blindOnce = sync.Once{}
	if got, want := PhoneHash(phone), devHMAC(phone); got != want {
		t.Fatalf("兜底路径的盲索引应等于本地 HMAC")
	}

	// ── B. 注入 crypto 盲索引器 ──
	const cfgKey = "prod-blind-key-0123456789abcdef"
	if err := crypto.InitBlindIndex([]byte(cfgKey)); err != nil {
		t.Fatalf("注入 crypto 盲索引器失败:%v", err)
	}

	if got := BlindKeySource(); got != BlindSourceConfig {
		t.Fatalf("注入后来源应为 %q,实际 %q", BlindSourceConfig, got)
	}

	// ── C. 关键判别式 ──
	//   注入后,盲索引必须由 crypto.blind_key 算出来,
	//   且**必须不等于**仓库里那个公开占位 key 算出来的值。
	if got, want := PhoneHash(phone), crypto.Blind().Compute(phone); got != want {
		t.Fatalf("盲索引没有走 crypto.blind_key:\n got=%s\nwant=%s", got, want)
	}
	if got := PhoneHash(phone); got == devHMAC(phone) {
		t.Fatal("盲索引仍在用仓库里那个公开占位 key —— 拿到库的人能反查手机号")
	}

	// 确定性:同样的输入必须得到同样的索引(否则查不到)
	h1, h2 := PhoneHash(phone), PhoneHash(phone)
	if h1 != h2 {
		t.Fatal("盲索引不确定")
	}
	// 不同输入 → 不同索引
	if PhoneHash(phone) == PhoneHash("13812345679") {
		t.Fatal("不同手机号算出了相同索引")
	}
	// 索引必须是 64 位 hex(HMAC-SHA256)
	h := PhoneHash(phone)
	if len(h) != 64 {
		t.Fatalf("盲索引长度应为 64,实际 %d(%s)", len(h), h)
	}
	for _, c := range h {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("盲索引必须是小写 hex,实际 %q", h)
		}
	}

	// IDCardHash 走同一个 key
	if got, want := IDCardHash("110101199001011234"), crypto.Blind().Compute("110101199001011234"); got != want {
		t.Fatal("IDCardHash 没有走 crypto.blind_key")
	}
}

// ── 通用性质 ────────────────────────────────────────────────────────────

// TestM112_Masking_NeverLeaksAllOfInput 掩码后不得还留着完整明文
//
// ⚠️ 每个掩码器只用**它自己阈值以上**的输入 —— 阈值以下的输入按设计
//
//	原样返回(那是脏数据,不是"漏脱敏",见技术方案 §9.3.4 的第 2 条)。
func TestM112_Masking_NeverLeaksAllOfInput(t *testing.T) {
	cases := []struct {
		name   string
		fn     func(string) string
		inputs []string
	}{
		{"Phone", Phone, []string{"13812345678", "1234567", "12345678", "138 1234 5678"}},
		{"IDCard", IDCard, []string{"110101199001011234", "11010119900101123", "12345678"}},
		{"Name", Name, []string{"张李", "张三丰", "欧阳娜娜"}},
		{"Email", Email, []string{"zhangsan@qq.com", "张san@qq.com"}},
		{"BankCard", BankCard, []string{"6222021234567890", "12345678"}},
		{"ContractNo", ContractNo, []string{"HFC20240001", "HFC023"}},
	}
	for _, c := range cases {
		for _, in := range c.inputs {
			got := c.fn(in)
			if got == in {
				t.Fatalf("%s(%q) 原样返回了", c.name, in)
			}
			if !strings.Contains(got, "*") {
				t.Fatalf("%s(%q) = %q 没有星号", c.name, in, got)
			}
		}
	}
}

// TestM112_Masking_DirtyDataUnchanged 阈值以下的脏数据按设计原样返回
//
// 技术方案 §9.3.4 第 2 条:"长度不足'根本不是这个号码'的阈值时原样返回 ——
// 那不是'漏脱敏',那是脏数据,照原样显示才能让人看出来。"
func TestM112_Masking_DirtyDataUnchanged(t *testing.T) {
	if got := Phone("123456"); got != "123456" {
		t.Fatalf("6 位手机号是脏数据,应原样返回,实际 %q", got)
	}
	if got := IDCard("1234567"); got != "1234567" {
		t.Fatalf("7 位身份证是脏数据,应原样返回,实际 %q", got)
	}
	if got := BankCard("1234567"); got != "1234567" {
		t.Fatalf("7 位银行卡是脏数据,应原样返回,实际 %q", got)
	}
	if got := ContractNo("HFC02"); got != "HFC02" {
		t.Fatalf("5 位合同号是脏数据,应原样返回,实际 %q", got)
	}
}

// TestM112_Masking_IdempotentEnough 掩码幂等性:再掩一次不会越藏越少
func TestM112_Masking_IdempotentEnough(t *testing.T) {
	once := Phone("13812345678")
	twice := Phone(once)
	if len([]rune(twice)) != len([]rune(once)) {
		t.Fatalf("再次掩码长度变了:%q → %q", once, twice)
	}
}
