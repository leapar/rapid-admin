// gray 中间件单测(§1.3 #21 P2)
//
// 注:Hertz RequestContext 的 Next() 行为走 router_test.go 覆盖,本文件只测
// pickTag 的 header 选择优先级:
package middleware

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/grayx"
)

// newCtxWithHeaders 构造带 header 的 *app.RequestContext
func newCtxWithHeaders(headers map[string]string) *app.RequestContext {
	c := &app.RequestContext{}
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c
}

func TestPickTag_Primary(t *testing.T) {
	c := newCtxWithHeaders(map[string]string{grayx.HeaderRelease: "canary"})
	if got := pickTag(c, grayx.HeaderRelease); got != "canary" {
		t.Fatalf("primary = %q, want canary", got)
	}
}

func TestPickTag_Missing_Empty(t *testing.T) {
	c := newCtxWithHeaders(map[string]string{})
	if got := pickTag(c, grayx.HeaderRelease); got != "" {
		t.Fatalf("missing = %q, want empty", got)
	}
}

func TestPickTag_FallbackCanary(t *testing.T) {
	c := newCtxWithHeaders(map[string]string{grayx.HeaderCanary: "fallback"})
	if got := pickTag(c, grayx.HeaderRelease); got != "fallback" {
		t.Fatalf("fallback canary = %q, want fallback", got)
	}
}

func TestPickTag_FallbackCohort(t *testing.T) {
	c := newCtxWithHeaders(map[string]string{grayx.HeaderCohort: "beta"})
	if got := pickTag(c, grayx.HeaderRelease); got != "beta" {
		t.Fatalf("fallback cohort = %q, want beta", got)
	}
}

func TestPickTag_PrimaryOverrides(t *testing.T) {
	// primary + canary 都有 → primary 胜
	c := newCtxWithHeaders(map[string]string{
		grayx.HeaderRelease: "primary",
		grayx.HeaderCanary:  "secondary",
	})
	if got := pickTag(c, grayx.HeaderRelease); got != "primary" {
		t.Fatalf("primary+canary = %q, want primary", got)
	}
}

func TestPickTag_CustomPrimary(t *testing.T) {
	// 用 X-Canary 作 primary
	c := newCtxWithHeaders(map[string]string{
		grayx.HeaderCanary: "x-canary-value",
		grayx.HeaderRelease: "x-release-value",
	})
	if got := pickTag(c, grayx.HeaderCanary); got != "x-canary-value" {
		t.Fatalf("custom primary = %q, want x-canary-value", got)
	}
}

func TestPickTag_CohortAfterCanary(t *testing.T) {
	// 没 canary 但有 cohort → 走 cohort
	c := newCtxWithHeaders(map[string]string{grayx.HeaderCohort: "production"})
	if got := pickTag(c, grayx.HeaderRelease); got != "production" {
		t.Fatalf("cohort fallback = %q, want production", got)
	}
}
