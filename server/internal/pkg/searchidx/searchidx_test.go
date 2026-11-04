// searchidx 单测(§1.5 #9 P2)
package searchidx

import (
	"testing"
)

func TestNgrams_ShortString_AsIsToken(t *testing.T) {
	got := Ngrams("abc", 4)
	if len(got) != 1 || got[0] != "abc" {
		t.Fatalf("short string = %v, want [abc]", got)
	}
}

func TestNgrams_ExactLength_OneToken(t *testing.T) {
	got := Ngrams("abcd", 4)
	if len(got) != 1 || got[0] != "abcd" {
		t.Fatalf("exact length = %v, want [abcd]", got)
	}
}

func TestNgrams_LongerString_Overlapping(t *testing.T) {
	got := Ngrams("abcdef", 4)
	want := []string{"abcd", "bcde", "cdef"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNgrams_CollapsesSpaces(t *testing.T) {
	// "ab cd" 切 2-gram → "ab" "b " " c" "cd"(去首尾 + 内部连续空格)
	// 但要 collapse: "ab cd" → "ab cd" 单空格
	got := Ngrams("  ab   cd  ", 2)
	// rune 序列:"ab cd"(5 rune),2-gram = "ab", "b ", " c", "cd"
	want := []string{"ab", "b ", " c", "cd"}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want = %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNgrams_Empty(t *testing.T) {
	if got := Ngrams("", 4); len(got) != 0 {
		t.Fatalf("empty = %v, want []", got)
	}
	if got := Ngrams("   ", 4); len(got) != 0 {
		t.Fatalf("whitespace = %v, want []", got)
	}
}

func TestNgrams_Unicode(t *testing.T) {
	// 中文字符:按 rune 切
	got := Ngrams("张三丰", 2)
	if len(got) != 2 || got[0] != "张三" || got[1] != "三丰" {
		t.Fatalf("chinese = %v, want [张三 三丰]", got)
	}
}

func TestBlindNgrams_Deterministic(t *testing.T) {
	secret := []byte("k")
	a := BlindNgrams(secret, "13800001234", 4)
	b := BlindNgrams(secret, "13800001234", 4)
	if len(a) != len(b) {
		t.Fatalf("len mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("[%d] not deterministic: %q vs %q", i, a[i], b[i])
		}
	}
	// 16 进制 sha256 = 64 字符
	if len(a[0]) != 64 {
		t.Errorf("token length = %d, want 64", len(a[0]))
	}
}

func TestBlindNgrams_DifferentSecret_DifferentToken(t *testing.T) {
	a := BlindNgrams([]byte("k1"), "abc", 4)
	b := BlindNgrams([]byte("k2"), "abc", 4)
	if a[0] == b[0] {
		t.Fatalf("diff secret produced same token")
	}
}

func TestBlindNgrams_SameSecret_DiffGram_DiffToken(t *testing.T) {
	secret := []byte("k")
	a := BlindNgrams(secret, "abcdef", 4)
	if a[0] == a[1] {
		t.Errorf("diff gram produced same token: %v", a)
	}
}

func TestSearchTokens_PrefixQuery(t *testing.T) {
	// "138" 比 n=4 短 → 整体 1 个 token
	secret := []byte("k")
	tokens := SearchTokens(secret, "138", 4)
	if len(tokens) != 1 {
		t.Fatalf("short query tokens = %d, want 1", len(tokens))
	}
	// 该 token 应能匹配索引中 "13800001234" 切出的第一个 4-gram "1380" 的盲索引?
	// 不,应匹配 "138" 整体索引;索引时 plain="138" 也会整体作为一个 token
	indexForShort := BlindNgrams(secret, "138", 4)
	if tokens[0] != indexForShort[0] {
		t.Fatal("SearchTokens('138') should match BlindNgrams('138') for exact '138' lookup")
	}
}

func TestSearchTokens_LongQuery(t *testing.T) {
	secret := []byte("k")
	tokens := SearchTokens(secret, "13800001234", 4)
	indexTokens := BlindNgrams(secret, "13800001234", 4)
	if len(tokens) != len(indexTokens) {
		t.Fatalf("len mismatch")
	}
	for i := range tokens {
		if tokens[i] != indexTokens[i] {
			t.Errorf("[%d] token mismatch: query=%q vs index=%q", i, tokens[i], indexTokens[i])
		}
	}
}

func TestMatchAtLeast(t *testing.T) {
	cases := []struct {
		actual, query, k int
		want             bool
	}{
		{0, 5, 0, false}, // k=0 → 至少 1
		{1, 5, 0, true},  // k=0 → 至少 1
		{5, 5, 0, true},
		{3, 5, 3, true},
		{2, 5, 3, false},
		{5, 5, -1, true},  // k<0 → 全部命中(queryTokenCount=5)
		{4, 5, -1, false}, // 缺 1 个
		{5, 5, 5, true},
		{4, 5, 5, false},
	}
	for i, c := range cases {
		got := MatchAtLeast(c.actual, c.query, c.k)
		if got != c.want {
			t.Errorf("[%d] MatchAtLeast(%d, %d, %d) = %v, want %v",
				i, c.actual, c.query, c.k, got, c.want)
		}
	}
}

func TestMinMatchTokens(t *testing.T) {
	cases := []struct {
		n      int
		ratio  float64
		expect int
	}{
		{10, 0, 1},
		{10, 0.5, 5},
		{10, 1.0, 10},
		{10, 1.5, 10},   // ratio>=1 → 全部
		{10, -0.1, 1},   // ratio<=0 → 1
		{0, 0.5, 0},     // n=0 → 0
		{3, 0.5, 1},     // 1.5 → int = 1
	}
	for i, c := range cases {
		got := MinMatchTokens(c.n, c.ratio)
		if got != c.expect {
			t.Errorf("[%d] MinMatchTokens(%d, %v) = %d, want %d", i, c.n, c.ratio, got, c.expect)
		}
	}
}
