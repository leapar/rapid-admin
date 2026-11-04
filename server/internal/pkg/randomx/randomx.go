// Package randomx 安全随机 token 生成(§1.11 补 P2)
//
// 目的:
//   - 提供"加密学安全 + URL-safe"的随机 token 工具
//   - 统一替代散落的 fmt.Sprintf("%d", rand.Int()) / uuid.New().String()
//   - 场景:验证码 / API key / one-time token / 上传 ID / 邀请码
//
// 与 crypto/rand 的关系:
//   - 全部走 crypto/rand(线程安全,熵源来自 OS)
//   - 不用 math/rand(非加密安全,seed 可见)
//
// 与 uuid 的关系:
//   - uuid 固定 36 字符,本包支持任意长度
//   - uuid 含 4 个连字符,本包全部 URL-safe 字符(无 - 或 _)
//
// 用法:
//
//	// 默认 32 字符 URL-safe(43 字节原始熵 → 256 位)
//	id := randomx.Token(32)
//
//	// 数字验证码(6 位)
//	code := randomx.NumericCode(6)
//
//	// 自定义字符集(大写字母+数字,无易混字符 0/O/1/I/L)
//	invite := randomx.FromCharset(8, randomx.CharsetNoConfusing)
//
//	// 16 进制(用于 secret key / hash salt)
//	hex := randomx.Hex(16)  // 32 字符
package randomx

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"math"
)

// charset URL-safe base64 字符集(0-9 a-z A-Z - _)
//   默认走这个 → 适合 URL / 文件名 / header
const CharsetURLSafe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// CharsetAlphaNum 大小写字母+数字
const CharsetAlphaNum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// CharsetNoConfusing 排除易混字符(0/O/Q, 1/I/L)的大写字母+数字
//
//	适合"人眼读"的邀请码 / 兑换码
const CharsetNoConfusing = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // 无 0/O/Q/1/I/L

// CharsetNumeric 纯数字
const CharsetNumeric = "0123456789"

// Token 生成 n 字符的 URL-safe 随机 token
//
//	n: 字符数(不是字节数);内部按 6 bit/字符 编码
//	推荐 n >= 16(96 bit 熵,够大部分场景)
func Token(n int) string {
	return FromCharset(n, CharsetURLSafe)
}

// FromCharset 自定义字符集
//
//	字符集长度推荐为 2 的幂(2/4/8/16/32/64)以保证均匀
//	否则会有微小的模偏置(实际影响可忽略)
func FromCharset(n int, charset string) string {
	if n <= 0 {
		return ""
	}
	if charset == "" {
		charset = CharsetURLSafe
	}
	// 🔴 csLen 必须是 int:用 byte 承接 len(charset) 时长度 ≥256 会截断
	// (256 → 0 ⇒ 模零 panic;257 → 1 ⇒ 熵坍缩成 charset[0] 单字符)。
	csLen := len(charset)
	out := make([]byte, n)
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 几乎不会失败;失败时返空串
		return ""
	}
	for i := 0; i < n; i++ {
		out[i] = charset[int(buf[i])%csLen]
	}
	return string(out)
}

// Hex 生成 n 字节的 hex 字符串(2n 字符)
//
//	用于 secret key / hash salt
//	16 → 32 字符(128 bit)
func Hex(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// Base64 生成 n 字节的 base64 字符串(无 padding)
//
//	适合 header / cookie
func Base64(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// NumericCode 生成 n 位纯数字验证码
//
//	常用于 SMS 验证码
//	首位可能为 0(允许)
func NumericCode(n int) string {
	return FromCharset(n, CharsetNumeric)
}

// AlnumCode 生成 n 位大小写字母+数字
func AlnumCode(n int) string {
	return FromCharset(n, CharsetAlphaNum)
}

// InviteCode 生成 n 位无易混字符的邀请码(人眼可读)
func InviteCode(n int) string {
	return FromCharset(n, CharsetNoConfusing)
}

// PINCode 生成 n 位 PIN(纯数字,适合 4-6 位密码)
func PINCode(n int) string {
	return NumericCode(n)
}

// String 安全随机字符串(默认 32 字符 URL-safe)
//   - 兼容旧代码中 fmt.Sprintf("%d", rand.Int()) 场景
func String() string {
	return Token(32)
}

// MustToken 跟 Token 一样,但 panic on err(crypto/rand 失败)
//   实际不会失败,作为 fmt.Sprintf("%x", rand.Int63()) 的替代
func MustToken(n int) string {
	s := Token(n)
	if s == "" {
		panic("randomx: crypto/rand failed (impossible)")
	}
	return s
}

// Entropy 估算 n 字符在 charset 上的熵(bit)
//
//	charsetLen=64 → 6 bit/字符
//	charsetLen=36 → ~5.17 bit/字符
//	charsetLen=10 → 3.32 bit/字符
func Entropy(n int, charsetLen int) float64 {
	if n <= 0 || charsetLen <= 1 {
		return 0
	}
	return float64(n) * math.Log2(float64(charsetLen))
}
