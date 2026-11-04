package cachex

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type dictEntry struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

func loaderEntry(v dictEntry, calls *atomic.Int32) TypedLoader[dictEntry] {
	return func(ctx context.Context) (dictEntry, error) {
		if calls != nil {
			calls.Add(1)
		}
		return v, nil
	}
}

// TestGetOrLoadTyped_RoundTrip 结构体原样往返
func TestGetOrLoadTyped_RoundTrip(t *testing.T) {
	c := NewTwoLevel(100, time.Minute, nil)
	ctx := context.Background()
	want := dictEntry{Code: "house_type", Label: "户型"}

	var calls atomic.Int32
	v, hit, err := GetOrLoadTyped(ctx, c, "dict:1", time.Minute, loaderEntry(want, &calls))
	if err != nil || hit {
		t.Fatalf("首次: hit=%v err=%v", hit, err)
	}
	if v != want {
		t.Fatalf("首次取值 = %+v, want %+v", v, want)
	}
	v2, hit2, err := GetOrLoadTyped(ctx, c, "dict:1", time.Minute, loaderEntry(want, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if !hit2 || v2 != want {
		t.Fatalf("第二次应命中且值相同: hit=%v v=%+v", hit2, v2)
	}
	if calls.Load() != 1 {
		t.Fatalf("loader 只该跑一次,got %d", calls.Load())
	}
}

// TestGetOrLoadTyped_ErrNotFound_ZeroValueNoError
//
// ErrNotFound 表示"查过库了,确实没有",不是错误。
// 吞掉它 → 调用方拿到零值以为业务失败;放行它 → 穿透防御失效,查不存在的东西
// 每次都打库。两个方向都是 bug。
func TestGetOrLoadTyped_ErrNotFound_ZeroValueNoError(t *testing.T) {
	c := NewTwoLevel(100, time.Minute, nil)
	ctx := context.Background()

	v, hit, err := GetOrLoadTyped(ctx, c, "dict:missing", time.Minute,
		func(ctx context.Context) (dictEntry, error) {
			return dictEntry{}, ErrNotFound
		})
	if err != nil {
		t.Fatalf("ErrNotFound 不该冒泡成 error,got %v", err)
	}
	if v != (dictEntry{}) {
		t.Fatalf("应返零值,got %+v", v)
	}
	if hit {
		t.Fatal("首次回源不该算命中")
	}
}

// TestGetOrLoadTyped_LoaderError_Propagates 真错误必须冒泡
func TestGetOrLoadTyped_LoaderError_Propagates(t *testing.T) {
	c := NewTwoLevel(100, time.Minute, nil)
	boom := errors.New("db down")
	_, _, err := GetOrLoadTyped(context.Background(), c, "dict:1", time.Minute,
		func(ctx context.Context) (dictEntry, error) { return dictEntry{}, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("真错误应原样冒泡,got %v", err)
	}
}

// TestGetOrLoadTyped_NilPool_Passthrough 没有缓存时行为不变
//
// cache.mode=off 时 Build 返 nil。这条保证"关掉缓存"不改变任何接口的成功/失败语义。
func TestGetOrLoadTyped_NilPool_Passthrough(t *testing.T) {
	ctx := context.Background()
	want := dictEntry{Code: "a", Label: "b"}

	var calls atomic.Int32
	var nilPool *TwoLevel
	for i := 0; i < 3; i++ {
		v, hit, err := GetOrLoadTyped(ctx, nilPool, "k", time.Minute, loaderEntry(want, &calls))
		if err != nil || hit || v != want {
			t.Fatalf("第 %d 次: v=%+v hit=%v err=%v", i, v, hit, err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("无缓存时每次都该回源,got %d", calls.Load())
	}
	// ErrNotFound 也要照样变成"零值无错"
	v, hit, err := GetOrLoadTyped(ctx, nilPool, "k", time.Minute,
		func(ctx context.Context) (dictEntry, error) { return dictEntry{}, ErrNotFound })
	if err != nil || hit || v != (dictEntry{}) {
		t.Fatalf("nil pool + ErrNotFound: v=%+v hit=%v err=%v", v, hit, err)
	}
}

// TestReType_AnyShapeToStruct 这是 GetOrLoadTyped 存在的全部理由
//
// Safe.GetOrLoad 内部把 JSON 反序列化进 any,所以命中时拿到的是 map[string]any,
// 不是结构体。没有 reType 的话业务只能自己再 Marshal 一遍 —— 框架能力比手写
// map 缓存还难用,这正是 cachex 零调用的原因。
func TestReType_AnyShapeToStruct(t *testing.T) {
	// 模拟真实命中形态:JSON → any
	var anyv any
	if err := json.Unmarshal([]byte(`{"code":"house_type","label":"户型"}`), &anyv); err != nil {
		t.Fatal(err)
	}
	got, err := reType[dictEntry](anyv)
	if err != nil {
		t.Fatalf("map[string]any → struct 应成功: %v", err)
	}
	if got != (dictEntry{Code: "house_type", Label: "户型"}) {
		t.Fatalf("got %+v", got)
	}
}

// TestReType_Incompatible_ReturnsError 形状对不上要报错(好让上层决定回源)
func TestReType_Incompatible_ReturnsError(t *testing.T) {
	if _, err := reType[dictEntry]("我是一个字符串"); err == nil {
		t.Fatal("字符串 → struct 应报错")
	}
	if _, err := reType[dictEntry](func() {}); err == nil {
		t.Fatal("函数 → struct 应报错(marshal 不过)")
	}
}

// TestGetOrLoadTypedL2_NilSafe_Passthrough L2-only 入口在没 Redis 时也要能透传
func TestGetOrLoadTypedL2_NilSafe_Passthrough(t *testing.T) {
	ctx := context.Background()
	want := dictEntry{Code: "a", Label: "b"}
	for _, s := range []*Safe{nil, {}} {
		v, hit, err := GetOrLoadTypedL2(ctx, s, "k", time.Minute,
			func(ctx context.Context) (dictEntry, error) { return want, nil })
		if err != nil || hit || v != want {
			t.Fatalf("v=%+v hit=%v err=%v", v, hit, err)
		}
	}
}
