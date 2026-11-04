// Trace 中间件单测 —— 请求 ID 的生成 / 透传 / 消毒 / **写回请求头**
//
// # 本文件存在的理由
//
// M105 审计 §1.3 时发现:Trace 只把请求 ID 写进**响应头**,而下游 5 处读的是
// `c.GetHeader(response.HeaderRequestID)` —— 那是**请求头**。
// 于是:
//
//	access.log 的 requestId      → 空
//	audit.log / sys_operation_log 的 request_id → 空
//	service.Caller.RequestID     → 空
//	每个 JSON 响应体里的 traceId  → 空
//
// M100 把 piiaudit 那条 ctx 链修好了,但这 5 处仍然是断的。
// 这条网守的是"**写一处,处处对**"。
//
// 另外两条:
//   - 客户端可以自带 X-Request-Id。原样透传 → 超长值落进 `size:64` 的列 →
//     审计写不进去;带换行的值 → 日志注入。
//   - 客户端 IP 的可信代理网段(Hertz 默认"信任所有")—— 见 clientip_test.go。
//
// 用 `route.NewEngine` + `ut.PerformRequest` 跑真实请求(与 trace_m100_test.go 同款),
// 因为要验的正是"请求头 / 响应头"这两个真实载体。
package middleware

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"

	"rapid-admin/internal/pkg/piiaudit"
	"rapid-admin/internal/pkg/response"
)

// traceProbe 跑一次 Trace 中间件,把请求头 / 响应头 / ctx 三处的值都取回来
type traceProbe struct {
	respReqID string
	respTid   string
	reqReqID  string
	reqTid    string
	ctxReqID  string
	ctxIP     string
	ctxUA     string
}

func runTraceProbe(t *testing.T, headers ...ut.Header) traceProbe {
	t.Helper()
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(Trace())

	var p traceProbe
	e.GET("/m105/trace", func(ctx context.Context, c *app.RequestContext) {
		p.respReqID = string(c.Response.Header.Peek(response.HeaderRequestID))
		p.respTid = string(c.Response.Header.Peek(response.HeaderTraceID))
		p.reqReqID = string(c.Request.Header.Peek(response.HeaderRequestID))
		p.reqTid = string(c.Request.Header.Peek(response.HeaderTraceID))
		p.ctxReqID = piiaudit.RequestIDFromContext(ctx)
		p.ctxIP, _ = ctx.Value(piiaudit.CtxKeyIP{}).(string)
		p.ctxUA, _ = ctx.Value(piiaudit.CtxKeyUA{}).(string)
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "GET", "/m105/trace", nil, headers...)
	return p
}

func TestTrace_GeneratesRequestIDWhenClientSendsNone(t *testing.T) {
	p := runTraceProbe(t)
	if p.respReqID == "" {
		t.Fatal("Trace 必须生成请求 ID")
	}
	if len(p.respReqID) > maxRequestIDLen {
		t.Fatalf("生成的请求 ID 超长(%d),落不进审计表的 size:64 列", len(p.respReqID))
	}
}

// TestTrace_WritesRequestIDBackToRequestHeader 是 M105 的核心一条。
//
// 修之前:响应头有 ID,请求头没有 → 下游 c.GetHeader 读到空串。
// 修之后:两者一致。
func TestTrace_WritesRequestIDBackToRequestHeader(t *testing.T) {
	p := runTraceProbe(t)

	if p.respReqID == "" {
		t.Fatal("响应头里没有请求 ID")
	}
	if p.reqReqID != p.respReqID {
		t.Fatalf("请求头(%q)与响应头(%q)的请求 ID 不一致 —— 下游 c.GetHeader(HeaderRequestID) 读到的还是空串",
			p.reqReqID, p.respReqID)
	}
	if p.reqTid != p.respTid {
		t.Fatalf("请求头(%q)与响应头(%q)的 trace ID 不一致", p.reqTid, p.respTid)
	}
	// ctx 那条链(M100)不能被改坏
	if p.ctxReqID != p.respReqID {
		t.Fatalf("ctx(%q)与响应头(%q)的请求 ID 不一致", p.ctxReqID, p.respReqID)
	}
}

// TestTrace_ClientSuppliedIDIsPassedThrough 客户端自带 ID 要能用(跨服务串联)
func TestTrace_ClientSuppliedIDIsPassedThrough(t *testing.T) {
	p := runTraceProbe(t, ut.Header{Key: "X-Request-Id", Value: "abc-123_XYZ.9"})
	if p.respReqID != "abc-123_XYZ.9" {
		t.Fatalf("响应头请求 ID = %q, want abc-123_XYZ.9", p.respReqID)
	}
	if p.reqReqID != "abc-123_XYZ.9" {
		t.Fatalf("请求头请求 ID = %q, want abc-123_XYZ.9", p.reqReqID)
	}
}

// TestTrace_OversizedClientIDIsRejected 超长 ID 会落进 size:64 的列 → 审计写失败
func TestTrace_OversizedClientIDIsRejected(t *testing.T) {
	tooLong := strings.Repeat("a", maxRequestIDLen+1)
	p := runTraceProbe(t, ut.Header{Key: "X-Request-Id", Value: tooLong})

	if p.respReqID == tooLong {
		t.Fatal("超长的客户端请求 ID 被原样透传了 —— 它会落进 size:64 的审计列")
	}
	if len(p.respReqID) > maxRequestIDLen {
		t.Fatalf("请求 ID 仍然超长:%d", len(p.respReqID))
	}
	if p.respReqID == "" {
		t.Fatal("拒绝了超长 ID 却没有生成新的")
	}
}

// TestTrace_MalformedClientIDIsRejected 换行 / 控制字符 → 日志注入 + 头注入
func TestTrace_MalformedClientIDIsRejected(t *testing.T) {
	for _, bad := range []string{
		"abc\r\nX-Injected: 1",
		"abc\ndef",
		"abc def",
		"abc\tdef",
		"中文",
	} {
		p := runTraceProbe(t, ut.Header{Key: "X-Request-Id", Value: bad})
		if p.respReqID == bad {
			t.Fatalf("含非法字符的客户端请求 ID 被原样透传了:%q", bad)
		}
		if len(p.respReqID) > maxRequestIDLen {
			t.Fatalf("请求 ID 超长:%d", len(p.respReqID))
		}
	}
}

// TestTrace_InjectsIPAndUAIntoCtx M100 的那条链不能被 M105 改坏
func TestTrace_InjectsIPAndUAIntoCtx(t *testing.T) {
	p := runTraceProbe(t, ut.Header{Key: "User-Agent", Value: "probe-agent/1.0"})
	if p.ctxUA != "probe-agent/1.0" {
		t.Fatalf("ctx 里的 user_agent = %q", p.ctxUA)
	}
	if p.ctxIP == "" {
		t.Fatal("ctx 里没有 ip —— 审计表的 ip 会恒为空")
	}
}

// TestSanitizeRequestID_Unit 直接钉住消毒规则
func TestSanitizeRequestID_Unit(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"a-b_c.d:e", "a-b_c.d:e"},
		{"ABC123", "ABC123"},
		{"a b", ""},
		{"a\nb", ""},
		{strings.Repeat("a", 64), strings.Repeat("a", 64)},
		{strings.Repeat("a", 65), ""},
	}
	for _, tc := range cases {
		if got := sanitizeRequestID(tc.in); got != tc.want {
			t.Fatalf("sanitizeRequestID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
