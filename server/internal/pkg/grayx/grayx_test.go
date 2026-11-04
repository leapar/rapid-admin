// grayx 灰度 tag 流转单测(§1.3 #21 P2)
package grayx

import (
	"context"
	"testing"
)

func TestWithReleaseTag_Basic(t *testing.T) {
	ctx := WithReleaseTag(context.Background(), "canary")
	if got := ReleaseTag(ctx); got != "canary" {
		t.Fatalf("ReleaseTag = %q, want canary", got)
	}
}

func TestReleaseTag_Empty(t *testing.T) {
	// 缺失 tag → ""
	if got := ReleaseTag(context.Background()); got != "" {
		t.Fatalf("missing tag = %q, want empty", got)
	}
	// 显式空 → ""
	ctx := WithReleaseTag(context.Background(), "")
	if got := ReleaseTag(ctx); got != "" {
		t.Fatalf("explicit empty = %q, want empty", got)
	}
}

func TestReleaseTag_NilCtx(t *testing.T) {
	// nil ctx 不 panic
	if got := ReleaseTag(nil); got != "" { //nolint:staticcheck
		t.Fatalf("nil ctx = %q, want empty", got)
	}
}

func TestWithReleaseTag_Overwrite(t *testing.T) {
	// 派生 ctx 可覆盖父 ctx 的 tag
	parent := WithReleaseTag(context.Background(), "canary")
	child := WithReleaseTag(parent, "production")
	if got := ReleaseTag(parent); got != "canary" {
		t.Fatalf("parent = %q, want canary (父 ctx 不应被改)", got)
	}
	if got := ReleaseTag(child); got != "production" {
		t.Fatalf("child = %q, want production", got)
	}
}

func TestWithReleaseTag_CrossGoroutine(t *testing.T) {
	// 跨 goroutine 传递(模拟异步任务)
	parent := WithReleaseTag(context.Background(), "beta")
	type result struct{ tag string }
	ch := make(chan result, 1)
	go func() {
		ch <- result{ReleaseTag(parent)}
	}()
	r := <-ch
	if r.tag != "beta" {
		t.Fatalf("cross goroutine tag = %q, want beta", r.tag)
	}
}
