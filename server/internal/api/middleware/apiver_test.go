// APIVersion 中间件单测(§1.3 #19 P2)
package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"

	"rapid-admin/internal/pkg/apiverx"
)

func newAPIVerEngine(opts ...func(*apiverx.Opt)) *route.Engine {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(APIVersion(opts...))
	var gotVer int
	engine.GET("/echo", func(ctx context.Context, c *app.RequestContext) {
		gotVer = apiverx.Major(ctx)
		c.String(http.StatusOK, "ok")
	})
	_ = gotVer // 通过 w.Body / w.Header 读不到,只能走 ctx 注入验证
	return engine
}

func TestAPIVer_Default_HeaderMissing(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(APIVersion())
	var seen int
	engine.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		seen = apiverx.Major(ctx)
		c.String(http.StatusOK, "ok")
	})
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if seen != apiverx.DefaultVersion {
		t.Fatalf("seen = %d, want default %d", seen, apiverx.DefaultVersion)
	}
	// 响应回写
	if got := string(w.Header().Get(apiverx.HeaderName)); got != "1" {
		t.Fatalf("response header = %q, want 1", got)
	}
}

func TestAPIVer_ValidHeader_InjectsAndEchoes(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(APIVersion())
	var seen int
	engine.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		seen = apiverx.Major(ctx)
		c.String(http.StatusOK, "ok")
	})
	w := ut.PerformRequest(engine, "GET", "/x", nil, ut.Header{Key: apiverx.HeaderName, Value: "v3"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if seen != 3 {
		t.Fatalf("seen = %d, want 3", seen)
	}
	if got := string(w.Header().Get(apiverx.HeaderName)); got != "3" {
		t.Fatalf("response header = %q, want 3", got)
	}
}

func TestAPIVer_InvalidHeader_DefaultsToOne(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(APIVersion())
	var seen int
	engine.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		seen = apiverx.Major(ctx)
		c.String(http.StatusOK, "ok")
	})
	w := ut.PerformRequest(engine, "GET", "/x", nil, ut.Header{Key: apiverx.HeaderName, Value: "garbage"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (容错)", w.Code)
	}
	if seen != 1 {
		t.Fatalf("seen = %d, want 1 (default fallback)", seen)
	}
}

func TestAPIVer_MaxMajor_Allowed(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(APIVersion(apiverx.WithMaxMajor(3)))
	engine.GET("/x", func(ctx context.Context, c *app.RequestContext) { c.String(http.StatusOK, "ok") })
	w := ut.PerformRequest(engine, "GET", "/x", nil, ut.Header{Key: apiverx.HeaderName, Value: "3"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (==MaxMajor)", w.Code)
	}
}

func TestAPIVer_MaxMajor_Rejected(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(APIVersion(apiverx.WithMaxMajor(3)))
	called := false
	engine.GET("/x", func(ctx context.Context, c *app.RequestContext) { called = true; c.String(http.StatusOK, "ok") })
	w := ut.PerformRequest(engine, "GET", "/x", nil, ut.Header{Key: apiverx.HeaderName, Value: "5"})
	if w.Code < 400 {
		t.Fatalf("status = %d, want >=400 (5 > MaxMajor 3)", w.Code)
	}
	if called {
		t.Fatal("handler should not be called when version rejected")
	}
	// body 应包含 '不被支持' 文案
	if !containsStr(w.Body.String(), "不被支持") {
		t.Fatalf("body = %q, want containing '不被支持'", w.Body.String())
	}
}

func TestAPIVer_NoMaxMajor_AcceptsAny(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(APIVersion()) // 不传 MaxMajor → 0 → 不校验
	engine.GET("/x", func(ctx context.Context, c *app.RequestContext) { c.String(http.StatusOK, "ok") })
	w := ut.PerformRequest(engine, "GET", "/x", nil, ut.Header{Key: apiverx.HeaderName, Value: "9999"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no MaxMajor)", w.Code)
	}
}

func TestIntToString(t *testing.T) {
	cases := map[int]string{
		0:       "0",
		1:       "1",
		9:       "9",
		10:      "10",
		42:      "42",
		100:     "100",
		12345:   "12345",
	}
	for n, want := range cases {
		if got := intToString(n); got != want {
			t.Errorf("intToString(%d) = %q, want %q", n, got, want)
		}
	}
}

// 防止 newAPIVerEngine unused
var _ = newAPIVerEngine
