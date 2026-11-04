// 值约束(M1,2026-11-03)—— Options 约束字段 + Normalize 落地 + ValidateForType 设计期校验
//
// ⚠️ **探针纪律**:下列每条断言都对应一个真实失败模式
// (约束丢失=写进非法值,空串误伤=选填字段没法留空,字节长度误伤=中文名存不进)。
package dynx

import (
	"strings"
	"testing"
)

func TestNormalize_文本约束_长度与正则(t *testing.T) {
	min2, max4 := 2, 4
	opts := Options{MinLength: &min2, MaxLength: &max4, Pattern: `^[A-Z]{2,4}$`}

	// 正常值
	if _, err := Normalize(TypeText, "AB", opts); err != nil {
		t.Fatalf("AB 应通过:%v", err)
	}
	// 超长(中文按 1 个字算,不按字节)
	long := strings.Repeat("张", 5)
	if _, err := Normalize(TypeText, long, opts); err == nil {
		t.Fatal("5 个字应超 max_length=4")
	}
	// 过短
	if _, err := Normalize(TypeText, "A", opts); err == nil {
		t.Fatal("1 个字应低于 min_length=2")
	}
	// 正则不匹配(整串匹配:小写不行)
	if _, err := Normalize(TypeText, "ab", opts); err == nil {
		t.Fatal("ab 应不符合 ^[A-Z]{2,4}$")
	}
	// 空串放行(选填字段留空是必填校验的事,不是长度约束的事)
	if _, err := Normalize(TypeText, "", opts); err != nil {
		t.Fatalf("空串应放行:%v", err)
	}
	// nil 放行(没填)
	if _, err := Normalize(TypeText, nil, opts); err != nil {
		t.Fatalf("nil 应放行:%v", err)
	}
}

func TestNormalize_数值约束_MinMax(t *testing.T) {
	lo, hi := 1.0, 100.0
	opts := Options{Min: &lo, Max: &hi}

	// 边界值都通过
	for _, v := range []any{1, 100, 50.5} {
		if _, err := Normalize(TypeNumber, v, opts); err != nil {
			t.Fatalf("%v 应通过:%v", v, err)
		}
	}
	// 越界
	if _, err := Normalize(TypeNumber, 0.5, opts); err == nil {
		t.Fatal("0.5 应低于 min=1")
	}
	if _, err := Normalize(TypeNumber, 100.1, opts); err == nil {
		t.Fatal("100.1 应超过 max=100")
	}
	// int 字段同样生效
	if _, err := Normalize(TypeInt, 200, opts); err == nil {
		t.Fatal("int 200 应超过 max=100")
	}
	if v, err := Normalize(TypeInt, 50, opts); err != nil || v != int64(50) {
		t.Fatalf("int 50 应通过且落 int64:%v %v", v, err)
	}
	// 下限 0 是有意义的(指针语义):数量 ≥ 0
	zero := 0.0
	if _, err := Normalize(TypeNumber, -1, Options{Min: &zero}); err == nil {
		t.Fatal("min=0 也必须拦住负数 —— 指针语义的核心用例")
	}
}

// TestNormalize_文本约束_Pattern未锚定自动整串 兑现「整串匹配」的承诺:
// 未锚定的 "[0-9]{6}" 必须拒绝 "abc123456xyz"(子串匹配会放行它),
// 设计者自己写了锚点的(^abc$)行为不变。
func TestNormalize_文本约束_Pattern未锚定自动整串(t *testing.T) {
	opts := Options{Pattern: `[0-9]{6}`}
	// 正好 6 位:通过
	if _, err := Normalize(TypeText, "123456", opts); err != nil {
		t.Fatalf("123456 应通过:%v", err)
	}
	// 6 位数字混在字母里:子串匹配会放行,整串匹配必须拒
	if _, err := Normalize(TypeText, "abc123456xyz", opts); err == nil {
		t.Fatal("abc123456xyz 不该通过 —— 子串匹配让未锚定约束失效")
	}
	// 7 位数字包含 6 位子串:同样要拒
	if _, err := Normalize(TypeText, "1234567", opts); err == nil {
		t.Fatal("1234567 不该通过(包含 6 位子串)")
	}
	// 或分组:自动包 (?:...) 别把 | 的作用域改掉("ab" 里含子串 "a")
	alt := Options{Pattern: `a|b`}
	if _, err := Normalize(TypeText, "a", alt); err != nil {
		t.Fatalf("a 应通过 a|b:%v", err)
	}
	if _, err := Normalize(TypeText, "ab", alt); err == nil {
		t.Fatal("ab 不该通过 a|b(整串只能是 a 或 b)")
	}
	// 设计者自己写了锚点:既有行为不变
	anchored := Options{Pattern: `^abc$`}
	if _, err := Normalize(TypeText, "abc", anchored); err != nil {
		t.Fatalf("^abc$ 应接受 abc:%v", err)
	}
	if _, err := Normalize(TypeText, "xabc", anchored); err == nil {
		t.Fatal("^abc$ 应拒绝 xabc")
	}
	// 坏正则仍然报错(锚定后照样编不过)
	if _, err := Normalize(TypeText, "x", Options{Pattern: "[a-"}); err == nil {
		t.Fatal("坏正则该报配置无效")
	}
}

func TestValidateForType_约束类型适配(t *testing.T) {
	one := 1.0
	two := 2.0
	twoInt := 2
	mk := func() Options { return Options{MinLength: &twoInt} }

	// 类型不认的约束 → 设计期拒绝(静默忽略=用户以为配了就生效)
	if err := ValidateForType(TypeSelect, mk()); err == nil {
		t.Fatal("select 上配 min_length 应被拒")
	}
	if err := ValidateForType(TypeInt, Options{Pattern: "^x$"}); err == nil {
		t.Fatal("int 上配 pattern 应被拒")
	}
	if err := ValidateForType(TypeText, Options{Min: &one}); err == nil {
		t.Fatal("text 上配 min 应被拒")
	}
	// 认的约束通过
	if err := ValidateForType(TypeText, Options{MinLength: &twoInt}); err != nil {
		t.Fatalf("text 配 min_length 应通过:%v", err)
	}
	// min > max
	if err := ValidateForType(TypeNumber, Options{Min: &two, Max: &one}); err == nil {
		t.Fatal("min>max 应被拒")
	}
	// 坏正则
	if err := ValidateForType(TypeText, Options{Pattern: "[a-"}); err == nil {
		t.Fatal("坏正则应被拒")
	}
	// JSON/bool/date 不认任何约束
	if err := ValidateForType(TypeJSON, Options{Min: &one, Pattern: "x"}); err == nil {
		t.Fatal("json 不认数值与正则约束")
	}
}
