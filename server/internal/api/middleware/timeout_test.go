// Timeout 中间件单测 —— 超时必须真的返 504
//
// # 本文件存在的理由
//
// M105 审计 §1.3 时发现:超时分支的判据是 `c.Response.StatusCode() < 100`,
// 意图是"handler 还没写过响应"。但 **Hertz 的 StatusCode() 在没写过响应时
// 返回 200**(hertz@v0.9.6 pkg/protocol/header.go:978):
//
//	func (h *ResponseHeader) StatusCode() int {
//	    if h.statusCode == 0 { return consts.StatusOK }   // ← 200
//	    return h.statusCode
//	}
//
// 于是 `200 < 100` 恒为 false → **超时分支永远不成立**:
// 超时的请求返回的是**空 body 的 200**,不是 504。
//
// 连带后果:熔断器判失败看 `status >= 500` —— 一个 200 的空响应不算失败 →
// "连续失败 N 次跳闸"对超时场景完全失效。
//
// 这条网守的是"**超时 = 504**",以及"**不覆盖 handler 已经写好的响应**"。
package middleware

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
)

// runTimeout 跑一次带 Timeout 中间件的请求
//
//   - slow:   handler 睡多久(模拟 DB 慢查询)
//   - write:  handler 是否写响应
//   - status: handler 写什么状态码(写 body)
func runTimeout(t *testing.T, limit time.Duration, slow time.Duration, write bool, status int) *ut.ResponseRecorder {
	t.Helper()
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(Timeout(limit))
	e.GET("/m105/timeout", func(ctx context.Context, c *app.RequestContext) {
		select {
		case <-ctx.Done():
		case <-time.After(slow):
		}
		if write {
			c.String(status, "handler 自己写的")
		}
	})
	return ut.PerformRequest(e, "GET", "/m105/timeout", nil)
}

// TestTimeout_Returns504WhenHandlerWritesNothing 是 M105 的核心一条
func TestTimeout_Returns504WhenHandlerWritesNothing(t *testing.T) {
	w := runTimeout(t, 50*time.Millisecond, 300*time.Millisecond, false, 0)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("超时请求的 HTTP 状态码 = %d, want 504 —— handler 没写响应时必须补 504", w.Code)
	}
	if body := w.Body.String(); body == "" {
		t.Fatal("504 的响应体是空的 —— 客户端连一句错误说明都拿不到")
	}
}

// TestTimeout_DoesNotOverwriteAResponseTheHandlerWrote 不能覆盖 handler 已经写好的响应
func TestTimeout_DoesNotOverwriteAResponseTheHandlerWrote(t *testing.T) {
	w := runTimeout(t, 50*time.Millisecond, 300*time.Millisecond, true, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Fatalf("HTTP 状态码 = %d, want 200 —— handler 已经写好的响应不该被超时分支覆盖", w.Code)
	}
	if body := w.Body.String(); body != "handler 自己写的" {
		t.Fatalf("响应体 = %q, want handler 自己写的", body)
	}
}

// TestTimeout_DoesNotOverwriteAnErrorResponse handler 写了 500 就不该被改成 504
func TestTimeout_DoesNotOverwriteAnErrorResponse(t *testing.T) {
	w := runTimeout(t, 50*time.Millisecond, 300*time.Millisecond, true, http.StatusInternalServerError)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("HTTP 状态码 = %d, want 500", w.Code)
	}
}

// TestTimeout_FastRequestIsNotAffected 没超时就不该碰响应
func TestTimeout_FastRequestIsNotAffected(t *testing.T) {
	w := runTimeout(t, 5*time.Second, 0, true, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Fatalf("HTTP 状态码 = %d, want 200", w.Code)
	}
	if body := w.Body.String(); body != "handler 自己写的" {
		t.Fatalf("响应体 = %q", body)
	}
}

// TestTimeout_NonPositiveDurationIsNoop d <= 0 时等价于不挂中间件
func TestTimeout_NonPositiveDurationIsNoop(t *testing.T) {
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(Timeout(0))
	e.GET("/m105/timeout0", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	w := ut.PerformRequest(e, "GET", "/m105/timeout0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP 状态码 = %d, want 200", w.Code)
	}
}

// TestResponseUnwritten 直接钉住判据
func TestResponseUnwritten(t *testing.T) {
	// 什么都没写 → 没写过
	c := app.NewContext(0)
	if !responseUnwritten(c) {
		t.Fatal("空响应被判定成'已写过'")
	}

	// 写了 body → 写过
	c2 := app.NewContext(0)
	c2.String(http.StatusOK, "hello")
	if responseUnwritten(c2) {
		t.Fatal("写了 body 的响应被判定成'没写过'")
	}

	// 写了 204(空 body)→ 写过(status != 200)
	c3 := app.NewContext(0)
	c3.Status(http.StatusNoContent)
	if responseUnwritten(c3) {
		t.Fatal("204 响应被判定成'没写过'")
	}

	// 写了 500 → 写过
	c4 := app.NewContext(0)
	response.Fail(c4, errcode.Internal, "boom")
	if responseUnwritten(c4) {
		t.Fatal("500 响应被判定成'没写过'")
	}
}
