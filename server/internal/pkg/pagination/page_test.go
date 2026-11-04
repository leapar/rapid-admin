package pagination

import (
	"testing"
)

// ── 本文件的来历 ───────────────────────────────────────────────────────
//
// 这个包之前**一张测试都没有**,而它是全部分页查询的偏移量来源。
//
// 它有一条 P1 真 bug(2026-10-29 M112 修的):
// `Offset()` 直接算 `(p.Page - 1) * p.Size`,而 `Normalize()` 是**另一个方法**
// —— 调用方忘了调 ⇒ Page=0 ⇒ Offset = -Size ⇒ SQL `OFFSET -20` 直接报错,
// 而且是在 repository 层炸,报错信息跟"分页参数非法"八竿子打不着。
//
// ── M112 可逆探针记录 ───────────────────────────────────────────────────
//
//	拆法                          必须变红
//	──────────────────────────  ─────────────────────────────────────────
//	Offset 不做归一化(退回老实现)  TestM112_Offset_Normalizes
//	Offset 越界 size 被截断        TestM112_Offset_ClampsSize
//	Offset 改了调用方的 PageRequest TestM112_Offset_NoSideEffect
// ───────────────────────────────────────────────────────────────────────

// ── Normalize ───────────────────────────────────────────────────────────

func TestM112_Normalize_Defaults(t *testing.T) {
	p := PageRequest{Page: 0, Size: 0}
	p.Normalize()
	if p.Page != DefaultPage {
		t.Fatalf("Page<=0 应规整为 %d,实际 %d", DefaultPage, p.Page)
	}
	if p.Size != DefaultSize {
		t.Fatalf("Size<=0 应规整为 %d,实际 %d", DefaultSize, p.Size)
	}
}

func TestM112_Normalize_Negative(t *testing.T) {
	p := PageRequest{Page: -5, Size: -10}
	p.Normalize()
	if p.Page != DefaultPage || p.Size != DefaultSize {
		t.Fatalf("负数应规整为默认值,实际 page=%d size=%d", p.Page, p.Size)
	}
}

func TestM112_Normalize_ClampsSize(t *testing.T) {
	p := PageRequest{Page: 1, Size: MaxSize + 500}
	p.Normalize()
	if p.Size != MaxSize {
		t.Fatalf("Size 超过 %d 应被截断,实际 %d", MaxSize, p.Size)
	}
}

func TestM112_Normalize_KeepsValid(t *testing.T) {
	p := PageRequest{Page: 3, Size: 50}
	p.Normalize()
	if p.Page != 3 || p.Size != 50 {
		t.Fatalf("合法值不该被改,实际 page=%d size=%d", p.Page, p.Size)
	}
}

// ── Offset ──────────────────────────────────────────────────────────────

func TestM112_Offset_Normal(t *testing.T) {
	p := PageRequest{Page: 3, Size: 20}
	p.Normalize()
	if got := p.Offset(); got != 40 {
		t.Fatalf("第 3 页每页 20 条的偏移量应为 40,实际 %d", got)
	}
}

// TestM112_Offset_Normalizes ⚠️ 核心回归:没调 Normalize 也不能算出负数
//
// 老实现直接 `(p.Page - 1) * p.Size` ⇒ Page=0 ⇒ Offset = -Size
// ⇒ SQL `OFFSET -20` 在 repository 层炸,报错跟"分页参数非法"八竿子打不着。
func TestM112_Offset_Normalizes(t *testing.T) {
	cases := []struct {
		name string
		p    PageRequest
		want int
	}{
		{"零值", PageRequest{}, 0},
		{"负页码", PageRequest{Page: -3, Size: 20}, 0},
		{"负 size", PageRequest{Page: 2, Size: -20}, (2 - 1) * DefaultSize},
		{"超大 size", PageRequest{Page: 2, Size: 9999}, (DefaultPage) * MaxSize},
		{"合法", PageRequest{Page: 3, Size: 20}, 40},
	}
	for _, c := range cases {
		got := c.p.Offset()
		if got < 0 {
			t.Fatalf("%s:偏移量不能为负,实际 %d", c.name, got)
		}
		if got != c.want {
			t.Fatalf("%s:偏移量应为 %d,实际 %d", c.name, c.want, got)
		}
	}
}

// TestM112_Offset_ClampsSize size 超上限时按上限算
func TestM112_Offset_ClampsSize(t *testing.T) {
	p := PageRequest{Page: 2, Size: 9999}
	if got := p.Offset(); got != MaxSize {
		t.Fatalf("size 超上限时偏移量应为 %d,实际 %d", MaxSize, got)
	}
}

// TestM112_Offset_NoSideEffect Offset 不该改调用方的 PageRequest
//
// Offset 是个"取值"方法,顺手把调用方的 Page/Size 改了是副作用 ——
// 后面 handler 拿 p.Page 拼响应就会跟真正查库用的参数对不上。
func TestM112_Offset_NoSideEffect(t *testing.T) {
	p := PageRequest{Page: 0, Size: 0}
	_ = p.Offset()
	if p.Page != 0 || p.Size != 0 {
		t.Fatalf("Offset 不该修改 PageRequest,实际 page=%d size=%d", p.Page, p.Size)
	}
}
