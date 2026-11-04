package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"

	"rapid-admin/internal/pkg/piiaudit"
	"rapid-admin/internal/pkg/response"
)

// TestTrace_InjectsRequestIDIPUAIntoContext Trace 必须把请求元信息**注入 ctx**
//
// # M100 修的那个 bug
//
// 原来 Trace 只把 request_id 写进**响应头**,ctx 里没有 ——
// 而 PII 审计链(piiaudit / cryptaudit)是从 **ctx** 里取 request_id 的,
// 于是 `sys_pii_access_log.request_id` / `sys_crypto_audit_log.request_id`
// **恒为空**。
//
// 同样,ip / user_agent 这两个 ctx key(`pkg/piiaudit` 里定义、cryptaudit
// 一直在读)**从来没有任何地方写入过**。
//
// 结果:一张"谁在什么时候看了手机号"的审计表,三条关联链全断 ——
//
//	没有 request_id → 对不上 access.log
//	没有 ip / user_agent → 对不上是谁的机器
//	没有 trace_id → 对不上 trace
//
// 表还在,行也在,但**查不出东西** —— 而这是 §1.5 #5 的 P0 等保项。
func TestTrace_InjectsRequestIDIPUAIntoContext(t *testing.T) {
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(Trace())

	var (
		gotCtx    context.Context
		gotIP     string
		gotUA     string
		gotReqID  string
		respReqID string
	)
	e.GET("/m100/trace/ping", func(ctx context.Context, c *app.RequestContext) {
		gotCtx = ctx
		gotIP = c.ClientIP()
		gotUA = string(c.UserAgent())
		gotReqID = piiaudit.RequestIDFromContext(ctx)
		respReqID = string(c.Response.Header.Get(response.HeaderRequestID))
		c.String(http.StatusOK, "ok")
	})

	w := ut.PerformRequest(e, "GET", "/m100/trace/ping", nil,
		ut.Header{Key: "User-Agent", Value: "M100-Agent/1.0"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gotCtx == nil {
		t.Fatal("handler 没拿到 ctx")
	}

	// request_id 必须进 ctx(而且和响应头一致)
	if gotReqID == "" {
		t.Fatal("ctx 里没有 request_id —— 审计表的 request_id 会恒为空")
	}
	if gotReqID != respReqID {
		t.Fatalf("ctx 里的 request_id(%q)和响应头(%q)不一致", gotReqID, respReqID)
	}

	// ip 必须进 ctx
	ip, _ := gotCtx.Value(piiaudit.CtxKeyIP{}).(string)
	if ip != gotIP {
		t.Fatalf("ctx 里的 ip(%q)!= c.ClientIP()(%q)", ip, gotIP)
	}
	if ip == "" {
		t.Fatal("ctx 里没有 ip —— 审计表的 ip 会恒为空")
	}

	// user_agent 必须进 ctx
	ua, _ := gotCtx.Value(piiaudit.CtxKeyUA{}).(string)
	if ua != gotUA {
		t.Fatalf("ctx 里的 user_agent(%q)!= c.UserAgent()(%q)", ua, gotUA)
	}
	if ua != "M100-Agent/1.0" {
		t.Fatalf("user_agent = %q", ua)
	}
}

// TestTrace_KeepsClientRequestID 客户端带的 X-Request-Id 要透传进 ctx
func TestTrace_KeepsClientRequestID(t *testing.T) {
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(Trace())

	var got string
	e.GET("/m100/trace/echo", func(ctx context.Context, c *app.RequestContext) {
		got = piiaudit.RequestIDFromContext(ctx)
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "GET", "/m100/trace/echo", nil,
		ut.Header{Key: "X-Request-Id", Value: "client-req-123"})

	if got != "client-req-123" {
		t.Fatalf("ctx 里的 request_id = %q, want client-req-123", got)
	}
}
