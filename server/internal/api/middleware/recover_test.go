// recover 中间件单测(§1.4 #10 P2)
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

func TestRecover_NoPanic_PassesThrough(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(Recover())
	called := false
	engine.GET("/ok", func(ctx context.Context, c *app.RequestContext) {
		called = true
		c.String(http.StatusOK, "ok")
	})
	w := ut.PerformRequest(engine, "GET", "/ok", nil)
	if !called {
		t.Fatal("handler should be called when no panic")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestRecover_Panic_Returns500(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(Recover())
	engine.GET("/boom", func(ctx context.Context, c *app.RequestContext) {
		panic("intentional test panic")
	})
	w := ut.PerformRequest(engine, "GET", "/boom", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	// body 应包含 errcode.Internal 的 default 文案 "服务内部错误"
	if !containsStr(w.Body.String(), "服务内部错误") {
		t.Fatalf("body should contain '服务内部错误', got: %s", w.Body.String())
	}
}

func TestRecover_AfterPanic_NextRequestOK(t *testing.T) {
	// panic 后,后续请求仍能正常处理(不污染 engine 状态)
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(Recover())
	engine.GET("/panic", func(ctx context.Context, c *app.RequestContext) { panic("x") })
	engine.GET("/ok", func(ctx context.Context, c *app.RequestContext) { c.String(http.StatusOK, "ok") })

	_ = ut.PerformRequest(engine, "GET", "/panic", nil)
	w := ut.PerformRequest(engine, "GET", "/ok", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("post-panic request status = %d, want 200", w.Code)
	}
}

func TestUserIDFromCtx_NilCaller(t *testing.T) {
	// 缺失 caller → 0
	if got := userIDFromCtx(context.Background()); got != 0 {
		t.Fatalf("nil caller = %d, want 0", got)
	}
}

// 简单 contains 工具
func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
