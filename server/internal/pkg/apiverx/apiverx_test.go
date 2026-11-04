// apiverx 单测(§1.3 #19 P2)
package apiverx

import (
	"context"
	"testing"
)

func TestParse_Empty_DefaultsTo1(t *testing.T) {
	v, ok := Parse("")
	if !ok || v != 1 {
		t.Fatalf("empty = (%d, %v), want (1, true)", v, ok)
	}
}

func TestParse_Digit(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"1", 1, true},
		{"2", 2, true},
		{"42", 42, true},
		{"v1", 1, true},
		{"V2", 2, true},
		{"v42", 42, true},
		{"abc", 1, false},
		{"0", 1, false},      // 0 不合法
		{"-1", 1, false},     // 负数不合法
		{"1.2", 1, true},     // 遇 '.' 立即停止,major 段
		{"v", 1, false},      // 只有 v
		{"vabc", 1, false},   // v 后非数字
		{"1.2.3", 1, true},   // 第一个 '.' 后停止
		{"1a", 1, false},     // 字母混合
	}
	for _, c := range cases {
		v, ok := Parse(c.in)
		if v != c.want || ok != c.ok {
			t.Errorf("Parse(%q) = (%d, %v), want (%d, %v)", c.in, v, ok, c.want, c.ok)
		}
	}
}

func TestParse_LargeNumber_BoundsCheck(t *testing.T) {
	// > 1<<20 → 拒绝
	v, ok := Parse("99999999999999999999")
	if ok || v != 1 {
		t.Fatalf("huge = (%d, %v), want (1, false)", v, ok)
	}
}

func TestMajor_DefaultIfMissing(t *testing.T) {
	if got := Major(context.Background()); got != DefaultVersion {
		t.Fatalf("missing ctx = %d, want %d", got, DefaultVersion)
	}
}

func TestMajor_RoundTrip(t *testing.T) {
	ctx := WithVersion(context.Background(), 7)
	if got := Major(ctx); got != 7 {
		t.Fatalf("roundtrip = %d, want 7", got)
	}
}

func TestMajor_ZeroOrNegative_BackToDefault(t *testing.T) {
	// type assertion 失败(Major 拿到的不是 int)→ DefaultVersion
	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, "x")
	if got := Major(ctx); got != DefaultVersion {
		t.Fatalf("wrong-typed ctx = %d, want %d", got, DefaultVersion)
	}
}
