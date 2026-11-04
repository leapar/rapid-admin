// Package desensitize 数据脱敏
// 文档 §9.3
//
// ⚠️ **M112(2026-10-29)修正:盲索引的 key 以前是仓库里的公开占位值**
//
//	旧实现自己算 HMAC,key 从环境变量 `PHONE_HASH_KEY` 读;没配就用
//	`"rapid-admin-dev-phone-hash-key-do-not-use-in-prod"` ——
//	**这个常量就写在仓库里,拿到代码的人都能算**。
//
//	而仓库里明明已经有一个审计过的盲索引器 `pkg/crypto.BlindIndex`
//	(M100 修过它的"空 key"洞:key < 16 字节直接拒绝),
//	并且 `crypto.blind_key` 配置项也早就存在(`main.go` 会装载它)——
//	只是 `desensitize` 压根没用它。
//
//	后果:生产上手机号盲索引用的是公开 key ⇒ HMAC-SHA256(公开 key)
//	退化成"任何人都能算的哈希" ⇒ 拿到库的人拿候选手机号算一遍就能把
//	`phone_hash` 反查成明文 ⇒ §9.3.3「永远不在接口返回完整手机号」的
//	底线从**索引这一侧**被绕过。
//
//	⇒ 现在优先用 `crypto.Blind()`(即配置里的 `crypto.blind_key`),
//	其次环境变量 `PHONE_HASH_KEY`,最后才是 dev 占位值(并告警)。
//
//	⚠️ **换 key ⇒ 已有的 phone_hash / real_name_hash / email_hash 全部失效**
//	(索引是对明文的派生值,key 一变就对不上)。dev 库里运行时创建的数据
//	需要重灌种子;生产还没上线,没有历史数据要迁移。
package desensitize

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"rapid-admin/internal/pkg/crypto"
)

// devBlindKey dev 占位 key —— **只允许本地开发用**
//
// ⚠️ 它写在仓库里 = 公开的。任何用它算出来的"盲索引"都可以被反查。
const devBlindKey = "rapid-admin-dev-phone-hash-key-do-not-use-in-prod"

// 盲索引 key 的来源(供启动日志对账)
const (
	BlindSourceConfig = "crypto.blind_key" // 配置里的 crypto.blind_key(唯一正解)
	BlindSourceEnv    = "env:PHONE_HASH_KEY"
	BlindSourceDev    = "dev-placeholder" // ⚠️ 公开占位值,只限本地开发
)

var (
	blindOnce sync.Once
	blindKey  []byte // 兜底 key(仅在 crypto.Blind() 不可用时用)
	blindSrc  string
)

// resolveBlind 解析盲索引 key。
//
// 只在**没有** crypto.Blind() 时才走兜底;兜底本身也只解析一次
// (sync.Once)—— 否则不同请求可能用不同的 key 算出不同的索引,
// 写进去查不到。
func resolveBlind() (key []byte, source string) {
	blindOnce.Do(func() {
		if k := strings.TrimSpace(os.Getenv("PHONE_HASH_KEY")); k != "" {
			if len(k) >= crypto.BlindIndexMinKeyLen {
				blindKey = []byte(k)
				blindSrc = BlindSourceEnv
				return
			}
		}
		blindKey = []byte(devBlindKey)
		blindSrc = BlindSourceDev
	})
	return blindKey, blindSrc
}

// BlindKeySource 返回当前盲索引用的是哪个 key 来源。
//
// "dev-placeholder" ⇒ **生产环境这是事故**:盲索引等于明文哈希。
func BlindKeySource() string {
	if crypto.TryBlind() != nil {
		return BlindSourceConfig
	}
	_, src := resolveBlind()
	return src
}

// blindHash 算盲索引(HMAC-SHA256,hex)
func blindHash(plain string) string {
	// 优先用配置里的盲索引器(M100 审计过的那个:key 长度有校验)
	if b := crypto.TryBlind(); b != nil {
		return b.Compute(plain)
	}
	key, _ := resolveBlind()
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(plain))
	return hex.EncodeToString(mac.Sum(nil))
}

// PhoneHash 11 位手机号盲索引(HMAC-SHA256)
// 用于: 不暴露明文的前提下按 phone 等值查找(如短信登录查 user)
func PhoneHash(phone string) string {
	return blindHash(phone)
}

// IDCardHash 身份证号盲索引(HMAC-SHA256)
func IDCardHash(idCard string) string {
	return blindHash(idCard)
}

// maskMiddle 中段掩码 —— 保留首 head 位、尾 tail 位,中间盖成 '*'
//
// ⚠️ **M112:与前端 `web/src/utils/format.ts` 的 maskMiddle 同源**
//
//	以前后端是"固定切法"(手机 3+4 / 身份证 4+4 / 银行卡 只留尾 4 位),
//	遇到**比标准长度短**的输入时保留段会把整串露出来:
//
//		Phone("1234567")  → "123****4567" —— **看着打了码,一个字没藏**
//		IDCard("12345678") → "12345678"    —— 同样 0 位被藏
//
//	(前端在 M110 修过完全相同的 bug,见 web/src/utils/format.ts 的注释)
//	⇒ 现在:标准切法只在"藏得住 ≥ 1/3"时用,否则 head / tail 各缩到
//	  floor(n/4),保证中段**一定盖住东西**。
func maskMiddle(s string, head, tail int) string {
	r := []rune(s)
	n := len(r)
	if n == 0 {
		return s
	}
	if head > n {
		head = n
	}
	if tail > n {
		tail = n
	}
	hidden := n - head - tail
	if hidden > 0 && hidden >= n/3 {
		return string(r[:head]) + strings.Repeat("*", hidden) + string(r[n-tail:])
	}
	// 藏不住 1/3 ⇒ 首尾各缩到 1/4,其余全盖
	h := n / 4
	t := n / 4
	if h+t >= n {
		return strings.Repeat("*", n)
	}
	return string(r[:h]) + strings.Repeat("*", n-h-t) + string(r[n-t:])
}

// Phone 138****1234
//
// ⚠️ M112:以前是 `if len(s) != 11 { return s }` —— 长度不是 11 就**原样返回**
//
//	⇒ 一个带 +86 前缀 / 中间带空格的手机号,会把**完整明文**塞进接口响应
//	(前端那层只是 UI 兜底,不是安全边界 —— 见技术方案 §9.3.4)。
//	现在:短于 7 位视为脏数据原样返回,其余按比例中段掩码。
func Phone(s string) string {
	if utf8.RuneCountInString(s) < 7 {
		return s
	}
	return maskMiddle(s, 3, 4)
}

// IDCard 1101**********1234
//
// ⚠️ M112:以前是 `s[:4] + Repeat("*", len-8) + s[len-4:]`,len==8 时
//
//	重复 0 次 ⇒ "12345678" **一个字符都没藏**。现在走 maskMiddle。
func IDCard(s string) string {
	if utf8.RuneCountInString(s) < 8 {
		return s
	}
	return maskMiddle(s, 4, 4)
}

// Name 张* / 张**
//
// ⚠️ M112:以前 n==2 时 `Repeat("*", n-2)` = **0 个星号** ⇒ "张李" 原样返回,
//
//	等于**完全没脱敏**。技术方案 §9.3.2 的展示矩阵要求姓名显示成"张*"。
//	(n≥3 保持原样:首字 + n-2 个星号 + 尾字。)
func Name(s string) string {
	r := []rune(s)
	n := len(r)
	if n <= 1 {
		return s
	}
	if n == 2 {
		return string(r[0]) + "*"
	}
	return string(r[0]) + strings.Repeat("*", n-2) + string(r[n-1])
}

// Email z******@qq.com(首字符 + n-1 个星号,星号数 = local-part 字数 - 1)
//
// ⚠️ M112:以前是 `parts[0][:1]` —— **按字节切**!local-part 首个字符是
//
//	多字节(中文 / 带音标字母)时切出来的是半个字符 ⇒ 输出非法 UTF-8
//	(Go 的 json.Marshal 会把它编码成 U+FFFD 乱码)。
//	现在按 rune 切;星号数也改成按 rune 数算(以前按字节数)。
//
// ⚠️ 多 @(如 "a@b@c.com")以前落进 `len(parts) != 2` 分支**整串明文返回**。
// 现在与 Phone 的 M112 口径对齐:形态异常也要藏住东西 —— 按第一个 @ 切,
// 只遮 local;local 只剩 1 字符没得遮时,退化为对整串做中段掩码。
// 无 @ 的输入维持原样返回:那是"根本不是邮箱"的脏数据(§9.3.4 第 2 条,
// TestM112_Email_NoAt 锁的就是这条契约)。
func Email(s string) string {
	parts := strings.Split(s, "@")
	if len(parts) > 2 {
		local := []rune(parts[0])
		if len(local) > 1 {
			return string(local[0]) + strings.Repeat("*", len(local)-1) + "@" + strings.Join(parts[1:], "@")
		}
		// local 只有 1 字符,遮 local 遮不住任何东西 ⇒ 整串中段掩码兜底
		return maskMiddle(s, 1, 3)
	}
	if len(parts) != 2 {
		return s
	}
	local := []rune(parts[0])
	if len(local) <= 1 {
		return s
	}
	return string(local[0]) + strings.Repeat("*", len(local)-1) + "@" + parts[1]
}

// BankCard ****1234
//
// ⚠️ M112:走 maskMiddle(0,4) —— 短输入时以前一个字符都没藏。
func BankCard(s string) string {
	if utf8.RuneCountInString(s) < 8 {
		return s
	}
	return maskMiddle(s, 0, 4)
}

// ContractNo HFC****0234
//
// ⚠️ M112:以前 len==6 时重复 0 次 ⇒ "HFC023" 原样返回。现在走 maskMiddle。
func ContractNo(s string) string {
	if utf8.RuneCountInString(s) < 6 {
		return s
	}
	return maskMiddle(s, 3, 3)
}
