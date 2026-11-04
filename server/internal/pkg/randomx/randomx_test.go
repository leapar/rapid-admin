// randomx 单测(§1.11 补 P2)
package randomx

import (
	"strings"
	"testing"
)

func TestToken_Length(t *testing.T) {
	for _, n := range []int{8, 16, 32, 64, 100} {
		got := Token(n)
		if len(got) != n {
			t.Errorf("Token(%d) length = %d, want %d", n, len(got), n)
		}
	}
}

func TestToken_URLSafe(t *testing.T) {
	got := Token(1000)
	for _, c := range got {
		if !strings.ContainsRune(CharsetURLSafe, c) {
			t.Errorf("Token char %q not in CharsetURLSafe", c)
		}
	}
}

func TestToken_Unique(t *testing.T) {
	// 1000 个 token 互不相同(熵足够,冲突概率近 0)
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		s := Token(32)
		if _, dup := seen[s]; dup {
			t.Fatalf("Token duplicate: %s", s)
		}
		seen[s] = struct{}{}
	}
}

func TestToken_ZeroOrNegative(t *testing.T) {
	if got := Token(0); got != "" {
		t.Errorf("Token(0) = %q, want empty", got)
	}
	if got := Token(-1); got != "" {
		t.Errorf("Token(-1) = %q, want empty", got)
	}
}

func TestNumericCode_OnlyDigits(t *testing.T) {
	got := NumericCode(1000)
	for _, c := range got {
		if c < '0' || c > '9' {
			t.Errorf("NumericCode char %q not digit", c)
		}
	}
}

func TestNumericCode_Length(t *testing.T) {
	for _, n := range []int{4, 6, 8} {
		if got := NumericCode(n); len(got) != n {
			t.Errorf("NumericCode(%d) length = %d", n, len(got))
		}
	}
}

func TestInviteCode_NoConfusingChars(t *testing.T) {
	got := InviteCode(1000)
	for _, c := range got {
		if !strings.ContainsRune(CharsetNoConfusing, c) {
			t.Errorf("InviteCode char %q not in CharsetNoConfusing (含 0/O/Q/1/I/L)", c)
		}
	}
}

func TestHex_Length(t *testing.T) {
	for _, n := range []int{1, 8, 16, 32} {
		got := Hex(n)
		if len(got) != n*2 {
			t.Errorf("Hex(%d) length = %d, want %d", n, len(got), n*2)
		}
		for _, c := range got {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("Hex char %q not hex", c)
			}
		}
	}
}

func TestBase64_Length(t *testing.T) {
	// RawURLEncoding:n 字节 → (4n+2)/3 字符(无 padding)
	cases := []struct {
		n, want int
	}{
		{1, 2}, {2, 3}, {3, 4},
		{4, 6}, {6, 8}, {8, 11},
		{16, 22}, {24, 32},
	}
	for _, c := range cases {
		got := Base64(c.n)
		if len(got) != c.want {
			t.Errorf("Base64(%d) length = %d, want %d", c.n, len(got), c.want)
		}
		// 不含 padding
		if strings.Contains(got, "=") {
			t.Errorf("Base64 should be unpadded, got %q", got)
		}
		// 字符在 base64 字符集
		for _, ch := range got {
			if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", ch) {
				t.Errorf("Base64 char %q not in URL-safe base64", ch)
			}
		}
	}
}

func TestAlnumCode_OnlyAlphaNum(t *testing.T) {
	got := AlnumCode(1000)
	for _, c := range got {
		if !strings.ContainsRune(CharsetAlphaNum, c) {
			t.Errorf("AlnumCode char %q not in CharsetAlphaNum", c)
		}
	}
}

func TestFromCharset_Default(t *testing.T) {
	// 字符集空 → fallback URLSafe
	got := FromCharset(50, "")
	if len(got) != 50 {
		t.Errorf("FromCharset empty charset length = %d", len(got))
	}
}

// TestFromCharset_LongCharset 超长字符集(byte 截断回归)
//
// 探针来源:旧实现 csLen 用 byte 承接 len(charset) —— 长度 256 截断成 0
// ⇒ 模零 panic;257 截断成 1 ⇒ 熵坍缩,所有输出都是 charset[0]。
func TestFromCharset_LongCharset(t *testing.T) {
	cs256 := strings.Repeat("x", 255) + "y" // 256 长
	// 256 长不 panic(旧实现这里直接模零崩)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("256 长字符集不该 panic: %v", r)
		}
	}()
	got := FromCharset(64, cs256)
	if len(got) != 64 {
		t.Fatalf("length = %d, want 64", len(got))
	}
	for _, c := range got {
		if c != 'x' && c != 'y' {
			t.Fatalf("char %q not in 256-length charset", c)
		}
	}

	// 257 长:必须能产出多种字符(旧实现截断成 1,永远只有 'x')
	cs257 := cs256 + "z"
	seen := make(map[byte]struct{})
	for i := 0; i < 100; i++ {
		s := FromCharset(32, cs257)
		for j := 0; j < len(s); j++ {
			seen[s[j]] = struct{}{}
		}
	}
	if len(seen) < 2 {
		t.Fatalf("257 长字符集 100 次只产出 %d 种字符 —— 熵坍缩成单字符了", len(seen))
	}
}

func TestEntropy(t *testing.T) {
	cases := []struct {
		n          int
		charsetLen int
		approx     float64
	}{
		{16, 64, 96},    // 16*6 = 96
		{32, 64, 192},   // 32*6 = 192
		{6, 10, 20},     // 6*3.32 ≈ 19.93
		{0, 64, 0},      // n=0 → 0
		{16, 1, 0},      // charset=1 → 0
		{16, 0, 0},      // charset=0 → 0
	}
	for _, c := range cases {
		got := Entropy(c.n, c.charsetLen)
		diff := got - c.approx
		if diff < -0.1 || diff > 0.1 {
			t.Errorf("Entropy(%d, %d) = %f, want ≈ %f", c.n, c.charsetLen, got, c.approx)
		}
	}
}

func TestMustToken_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("MustToken panicked: %v", r)
		}
	}()
	if got := MustToken(32); len(got) != 32 {
		t.Errorf("MustToken length = %d", len(got))
	}
}

func TestString(t *testing.T) {
	// 32 字符默认
	if got := String(); len(got) != 32 {
		t.Errorf("String length = %d, want 32", len(got))
	}
}

func TestPINCode_LengthAndCharset(t *testing.T) {
	// PINCode 跟 NumericCode 行为一致(纯数字 + 指定长度)
	got := PINCode(6)
	if len(got) != 6 {
		t.Errorf("PINCode(6) length = %d, want 6", len(got))
	}
	for _, c := range got {
		if c < '0' || c > '9' {
			t.Errorf("PINCode char %q not digit", c)
		}
	}
}
