// signx 单测(§1.3 #20 P2)
package signx

import (
	"strconv"
	"testing"
	"time"
)

func TestSign_Deterministic(t *testing.T) {
	secret := []byte("test-secret")
	a := Sign(secret, "POST", "/rapid-admin/v1/biz/owner", "1700000000", []byte(`{"name":"x"}`))
	b := Sign(secret, "POST", "/rapid-admin/v1/biz/owner", "1700000000", []byte(`{"name":"x"}`))
	if a != b {
		t.Fatalf("Sign not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("hex sha256 should be 64 chars, got %d", len(a))
	}
}

func TestSign_DifferentInputs_DifferentSig(t *testing.T) {
	secret := []byte("test-secret")
	ts := "1700000000"
	body := []byte(`{"a":1}`)
	base := Sign(secret, "POST", "/x", ts, body)
	cases := []struct {
		name   string
		method string
		path   string
		ts     string
		body   []byte
	}{
		{"diff-method", "PUT", "/x", ts, body},
		{"diff-path", "POST", "/y", ts, body},
		{"diff-ts", "POST", "/x", "1700000001", body},
		{"diff-body", "POST", "/x", ts, []byte(`{"a":2}`)},
		{"diff-secret", "POST", "/x", ts, body}, // 单独校验
	}
	for _, c := range cases {
		if c.name == "diff-secret" {
			other := Sign([]byte("other-secret"), "POST", "/x", ts, body)
			if other == base {
				t.Errorf("diff-secret produced same sig")
			}
			continue
		}
		got := Sign(secret, c.method, c.path, c.ts, c.body)
		if got == base {
			t.Errorf("%s produced same sig", c.name)
		}
	}
}

func TestVerify_OK(t *testing.T) {
	secret := []byte("k")
	ts := "1700000000"
	sig := Sign(secret, "POST", "/x", ts, []byte(`{}`))
	if !Verify(secret, "POST", "/x", ts, []byte(`{}`), sig) {
		t.Fatal("Verify should accept freshly-signed request")
	}
}

func TestVerify_Tampered(t *testing.T) {
	secret := []byte("k")
	ts := "1700000000"
	sig := Sign(secret, "POST", "/x", ts, []byte(`{}`))
	if Verify(secret, "POST", "/x", ts, []byte(`{"x":1}`), sig) {
		t.Fatal("Verify should reject tampered body")
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	sig := Sign([]byte("k1"), "POST", "/x", "1", nil)
	if Verify([]byte("k2"), "POST", "/x", "1", nil, sig) {
		t.Fatal("Verify should reject wrong secret")
	}
}

func TestVerify_LenMismatch(t *testing.T) {
	// 不同长度 sig 立即返 false(不进入 ConstantTimeCompare)
	if Verify([]byte("k"), "POST", "/x", "1", nil, "abc") {
		t.Fatal("short sig should reject")
	}
}

func TestParseHeader(t *testing.T) {
	cases := []struct {
		in       string
		wantOK   bool
		wantTS   string
		wantSig  string
	}{
		{"", false, "", ""},
		{"t=1,sig=abc", true, "1", "abc"},
		{"ts=1,signature=abc", true, "1", "abc"},
		{"  t = 1 , sig = abc ", true, "1", "abc"},
		{"t=1", false, "", ""},  // 缺 sig
		{"sig=abc", false, "", ""}, // 缺 ts
		{"garbage", false, "", ""},
	}
	for _, c := range cases {
		ts, sig, ok := ParseHeader(c.in)
		if ok != c.wantOK || ts != c.wantTS || sig != c.wantSig {
			t.Errorf("ParseHeader(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.in, ts, sig, ok, c.wantTS, c.wantSig, c.wantOK)
		}
	}
}

func TestValidTS(t *testing.T) {
	now := int64(1700000300)
	cases := []struct {
		ts     string
		skew   int64
		want   bool
	}{
		{strconv.FormatInt(now, 10), 300, true},       // 现在
		{strconv.FormatInt(now-300, 10), 300, true},   // 边界
		{strconv.FormatInt(now-301, 10), 300, false},  // 超
		{strconv.FormatInt(now+300, 10), 300, true},   // 未来边界
		{strconv.FormatInt(now+301, 10), 300, false},  // 未来超
		{"abc", 300, false},                          // 非数字
		{"0", 300, false},                            // 太早
	}
	for _, c := range cases {
		got := ValidTS(c.ts, now, c.skew)
		if got != c.want {
			t.Errorf("ValidTS(%q, now=%d, skew=%d) = %v, want %v",
				c.ts, now, c.skew, got, c.want)
		}
	}
	// 时钟无关
	_ = time.Now() // 防止 unused import
}
