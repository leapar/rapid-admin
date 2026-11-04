// MemoryBackend 单测
//
// 重点是 TTL 过期:过期条目必须当"没有"(ErrMiss),不能当"命中一个旧值"。
// 这条一旦破了,缓存会开始返过期数据 —— 而且很难查(数据"看起来是对的",
// 只是旧了一点)。
package cachex

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newClockedBackend 造一个可控时钟的内存后端
func newClockedBackend() (*MemoryBackend, func(time.Duration)) {
	m := NewMemoryBackend()
	now := time.Now()
	m.now = func() time.Time { return now }
	return m, func(d time.Duration) { now = now.Add(d) }
}

func TestMemoryBackend_SetGet(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryBackend()

	if _, err := m.Get(ctx, "nope"); !errors.Is(err, ErrMiss) {
		t.Fatalf("未命中应返 ErrMiss,got %v", err)
	}
	if err := m.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "k")
	if err != nil || got != "v" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if m.Len() != 1 {
		t.Fatalf("Len = %d, want 1", m.Len())
	}
}

func TestMemoryBackend_Expiry(t *testing.T) {
	ctx := context.Background()
	m, advance := newClockedBackend()

	if err := m.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	advance(59 * time.Second)
	if got, err := m.Get(ctx, "k"); err != nil || got != "v" {
		t.Fatalf("未到期应命中: got %q err=%v", got, err)
	}

	advance(2 * time.Second) // 共 61s > 60s
	if _, err := m.Get(ctx, "k"); !errors.Is(err, ErrMiss) {
		t.Fatalf("过期应返 ErrMiss,got %v", err)
	}
	// 过期条目读的时候顺手删掉,别白占内存
	if m.Len() != 0 {
		t.Fatalf("过期条目应被清掉,Len = %d", m.Len())
	}
}

func TestMemoryBackend_SetNX(t *testing.T) {
	ctx := context.Background()
	m, advance := newClockedBackend()

	ok, err := m.SetNX(ctx, "k", "v1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("首次 SetNX 应成功: ok=%v err=%v", ok, err)
	}
	ok, err = m.SetNX(ctx, "k", "v2", time.Minute)
	if err != nil || ok {
		t.Fatalf("已存在时 SetNX 应失败: ok=%v err=%v", ok, err)
	}
	if got, _ := m.Get(ctx, "k"); got != "v1" {
		t.Fatalf("值不该被覆盖,got %q", got)
	}

	// 过期之后 SetNX 应重新成功
	advance(2 * time.Minute)
	ok, err = m.SetNX(ctx, "k", "v3", time.Minute)
	if err != nil || !ok {
		t.Fatalf("过期后 SetNX 应成功: ok=%v err=%v", ok, err)
	}
}

func TestMemoryBackend_DelExpire(t *testing.T) {
	ctx := context.Background()
	m, advance := newClockedBackend()

	_ = m.Set(ctx, "k1", "v", time.Minute)
	_ = m.Set(ctx, "k2", "v", time.Minute)

	if err := m.Del(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, "k1"); !errors.Is(err, ErrMiss) {
		t.Fatalf("k1 应被删掉,got %v", err)
	}
	if _, err := m.Get(ctx, "k2"); err != nil {
		t.Fatalf("k2 不该被误删: %v", err)
	}

	// Expire 缩短寿命
	if err := m.Expire(ctx, "k2", time.Second); err != nil {
		t.Fatal(err)
	}
	advance(2 * time.Second)
	if _, err := m.Get(ctx, "k2"); !errors.Is(err, ErrMiss) {
		t.Fatalf("Expire 后应过期,got %v", err)
	}

	// Expire 一个不存在的 key 不该报错(fail-open)
	if err := m.Expire(ctx, "nope", time.Second); err != nil {
		t.Fatalf("Expire 不存在的 key 不该报错: %v", err)
	}
}

func TestMemoryBackend_Scan(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryBackend()

	_ = m.Set(ctx, "principal:1:1", "a", time.Minute)
	_ = m.Set(ctx, "principal:1:2", "b", time.Minute)
	_ = m.Set(ctx, "dict:1", "c", time.Minute)

	// ⚠️ prefix 是**字面前缀**,不是 pattern(2026-10-26 M96 改)
	keys, cur, err := m.Scan(ctx, 0, "principal:", 100)
	if err != nil {
		t.Fatal(err)
	}
	if cur != 0 {
		t.Fatalf("内存后端没有游标,cursor = %d", cur)
	}
	if len(keys) != 2 {
		t.Fatalf("应扫到 2 个,got %d (%v)", len(keys), keys)
	}
	for _, k := range keys {
		if k == "dict:1" {
			t.Fatalf("前缀不匹配: %v", keys)
		}
	}
}

func TestMemoryBackend_Ping(t *testing.T) {
	if err := NewMemoryBackend().Ping(context.Background()); err != nil {
		t.Fatalf("内存后端应永远 ping 通: %v", err)
	}
}
