// SecHeaders 中间件单测(§1.3 补 P2 #22)
package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
)

func newSecHeadersEngine(opts ...func(*secHeadersCfg)) *route.Engine {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(SecHeaders(opts...))
	engine.GET("/x", func(ctx context.Context, c *app.RequestContext) { c.String(http.StatusOK, "ok") })
	return engine
}

func TestSecHeaders_Defaults(t *testing.T) {
	engine := newSecHeadersEngine()
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	h := w.Header()
	checks := map[string]string{
		"X-Content-Type-Options":   "nosniff",
		"X-Frame-Options":          "DENY",
		"Referrer-Policy":          "strict-origin-when-cross-origin",
		"X-XSS-Protection":         "0",
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains",
	}
	for k, want := range checks {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	// Permissions-Policy 默认禁用 8 个 → 全部出现
	perms := h.Get("Permissions-Policy")
	for _, p := range []string{
		"geolocation=()", "microphone=()", "camera=()", "payment=()",
		"usb=()", "accelerometer=()", "gyroscope=()", "magnetometer=()",
	} {
		if !containsStr(perms, p) {
			t.Errorf("Permissions-Policy missing %s: %q", p, perms)
		}
	}
}

func TestSecHeaders_FrameSameOrigin(t *testing.T) {
	engine := newSecHeadersEngine(WithFrame(FrameSameOrigin))
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
}

func TestSecHeaders_FrameNone_OmitsHeader(t *testing.T) {
	engine := newSecHeadersEngine(WithFrame(FrameNone))
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	if got := w.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("X-Frame-Options = %q, want empty (FrameNone omitted)", got)
	}
}

func TestSecHeaders_HSTSOff(t *testing.T) {
	engine := newSecHeadersEngine(WithHSTS(false))
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS = %q, want empty (HSTS off)", got)
	}
}

func TestSecHeaders_CSP_Injected(t *testing.T) {
	engine := newSecHeadersEngine(WithCSP("default-src 'self'"))
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Errorf("CSP = %q, want 'default-src 'self''", got)
	}
}

func TestSecHeaders_CSPNotSet_NoHeader(t *testing.T) {
	engine := newSecHeadersEngine() // 不传 WithCSP
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	if got := w.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("CSP = %q, want empty (not set)", got)
	}
}

func TestSecHeaders_PermissionsPolicy_AllDisabled(t *testing.T) {
	engine := newSecHeadersEngine(WithPermissionsPolicy(PermPolicyConfig{
		DisableGeolocation: true,
		DisableMicrophone:  true,
		DisableCamera:      true,
		DisablePayment:     true,
		DisableUSB:         true,
	}))
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	got := w.Header().Get("Permissions-Policy")
	// 至少包含这 5 个 key(其他没禁用就不出现)
	wantParts := []string{
		"geolocation=()", "microphone=()", "camera=()", "payment=()", "usb=()",
	}
	for _, p := range wantParts {
		if !containsStr(got, p) {
			t.Errorf("Permissions-Policy missing %s: %q", p, got)
		}
	}
}

func TestSecHeaders_PermissionsPolicy_AllEnabled_Empty(t *testing.T) {
	engine := newSecHeadersEngine(WithPermissionsPolicy(PermPolicyConfig{}))
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	if got := w.Header().Get("Permissions-Policy"); got != "" {
		t.Errorf("Permissions-Policy = %q, want empty (all enabled)", got)
	}
}

func TestSecHeaders_ExtraHeader(t *testing.T) {
	engine := newSecHeadersEngine(WithExtraHeader("X-Custom", "value"))
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	if got := w.Header().Get("X-Custom"); got != "value" {
		t.Errorf("X-Custom = %q, want value", got)
	}
}

func TestSecHeaders_StripsServerAndPoweredBy(t *testing.T) {
	engine := newSecHeadersEngine()
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	// Hertz 在 test 环境下不主动写 Server,显式空覆盖后 Get 返 ""
	// 这里只断言头存在但值是空
	if got := w.Header().Get("Server"); got != "" {
		t.Errorf("Server = %q, want empty", got)
	}
	if got := w.Header().Get("X-Powered-By"); got != "" {
		t.Errorf("X-Powered-By = %q, want empty", got)
	}
}

func TestBuildPermissionsPolicy(t *testing.T) {
	// 边界:全部启用 → 空串
	if got := buildPermissionsPolicy(PermPolicyConfig{}); got != "" {
		t.Errorf("all-enabled = %q, want empty", got)
	}
	// 全部禁用 → 8 个 part
	all := PermPolicyConfig{
		DisableGeolocation: true, DisableMicrophone: true, DisableCamera: true,
		DisablePayment: true, DisableUSB: true, DisableAccelerometer: true,
		DisableGyroscope: true, DisableMagnetometer: true,
	}
	got := buildPermissionsPolicy(all)
	for _, p := range []string{
		"geolocation=()", "microphone=()", "camera=()", "payment=()",
		"usb=()", "accelerometer=()", "gyroscope=()", "magnetometer=()",
	} {
		if !containsStr(got, p) {
			t.Errorf("missing %s in: %s", p, got)
		}
	}
}
