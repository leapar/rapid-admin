// otel 中间件单测 —— §1.8(M99)
//
// # M99 在这里实证出的 2 个真 bug
//
//  1. **span 名用原始 path** —— trace 后端基数爆炸
//     此前 otel.go 里有一份**同名同职责但行为不同**的拷贝(`otelRoutePattern`),
//     它用 `c.Path()`(原始请求路径)。于是每个带 ID 的 URL 都产生一个新 span 名。
//     而 metrics 那边用的是路由模板 —— 两份拷贝一份对一份错。
//     修法:合并成 metrics.go 的 `routePath`。
//  2. **traceparent 写进了响应头**
//     `httpHeadersCarrier.Set` 用的是 `c.Header(k, v)`,而 hertz 的
//     `c.Header` 就是 `ctx.Response.Header.Set` ——
//     于是 Inject 出去的 traceparent 落到了**响应**头上,
//     每个响应都多出一个 `traceparent: 00-...` 内部头。
//     语义上正确的位置是**请求头**(Inject 的含义是"把 trace 上下文交给下游")。
//     修法:carrier 的 Set 改写 `c.Request.Header`。
//
// # M99(2026-10-26)可逆探针记录
//
//	拆法                                                 必须变红
//	────────────────────────────────────────────  ──────────────────────────────────
//	P11 span 名改回原始 path(c.Path())        TestOtel_SpanNameUsesRouteTemplate
//	P14 carrier.Set 改回 c.Header(响应头)     TestOtel_TraceparentGoesToRequestNotResponse
package middleware

import (
	"context"
	"net/http"
	"regexp"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"rapid-admin/internal/pkg/response"
)

// obsSetupTracer 装一个会记录 span 的 TracerProvider
//
// ⚠️ 必须在**构造 Otel 中间件之前**调用:中间件在构造时就
// `otel.Tracer(serviceName)` 把 tracer 拿走了,之后再换 provider 就来不及了。
func obsSetupTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return rec
}

// TestOtel_SpanNameUsesRouteTemplate span 名必须是**路由模板**
//
// # M99 修的第一个 bug
//
// 旧实现用原始 path → 每个带 ID 的 URL 一个新 span 名 → trace 后端基数爆炸。
//
// ⚠️ 断言要点:必须同时断言"模板在"和"原始路径**不**在" ——
// 只断言前者的话,一个把两者都记下来的实现也能过。
func TestOtel_SpanNameUsesRouteTemplate(t *testing.T) {
	rec := obsSetupTracer(t)

	e := obsNewEngine()
	e.Use(Otel("m99-svc"))
	e.GET("/m99/otel/users/:id", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "GET", "/m99/otel/users/42", nil)

	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("没有记录到 span")
	}
	var names []string
	for _, s := range spans {
		names = append(names, s.Name())
	}
	var found bool
	for _, s := range spans {
		if s.Name() == "GET /m99/otel/users/:id" {
			found = true
		}
		if s.Name() == "GET /m99/otel/users/42" {
			t.Errorf("span 名用了原始路径(基数会爆):%v", names)
		}
	}
	if !found {
		t.Errorf("没有找到路由模板的 span,实际 span 名 = %v", names)
	}
}

// TestOtel_SpanKindIsServer span kind 必须是 server
func TestOtel_SpanKindIsServer(t *testing.T) {
	rec := obsSetupTracer(t)

	e := obsNewEngine()
	e.Use(Otel("m99-svc"))
	e.GET("/m99/otel/kind", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "GET", "/m99/otel/kind", nil)

	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("没有记录到 span")
	}
	if spans[0].SpanKind() != trace.SpanKindServer {
		t.Errorf("span kind = %v,want SpanKindServer", spans[0].SpanKind())
	}
}

// TestOtel_TraceparentGoesToRequestNotResponse traceparent 只能进请求头
//
// # M99 修的第二个 bug
//
// 旧实现的 carrier.Set 走 `c.Header(k,v)` = **响应头**,
// 于是每个响应都带着一个 `traceparent: 00-...` 内部头。
//
// 断言两条:
//  1. handler 里读**请求头**能读到 traceparent(注入真的落到了请求头)
//  2. **响应头**里没有 traceparent
//
// ⚠️ 第 1 条不能省:只断言"响应里没有"的话,
// 一个"哪儿都不注入"的实现也能过 —— 那是把功能拆了,不是修好。
func TestOtel_TraceparentGoesToRequestNotResponse(t *testing.T) {
	obsSetupTracer(t)

	var inHandler string
	e := obsNewEngine()
	e.Use(Otel("m99-svc"))
	e.GET("/m99/otel/inject", func(ctx context.Context, c *app.RequestContext) {
		inHandler = string(c.GetHeader("traceparent"))
		c.String(http.StatusOK, "ok")
	})
	w := ut.PerformRequest(e, "GET", "/m99/otel/inject", nil)

	if inHandler == "" {
		t.Error("handler 里读不到请求头里的 traceparent —— 注入没落到请求头")
	}
	if got := w.Header().Get("traceparent"); got != "" {
		t.Errorf("响应头里有 traceparent=%q —— 它只该出现在请求头里(旧实现写错了 header 袋)", got)
	}
}

// TestOtel_TraceIDHeaderInjected 响应要带 X-Trace-Id
//
// 供前端 / LB / 网关把日志关联到 trace。
func TestOtel_TraceIDHeaderInjected(t *testing.T) {
	obsSetupTracer(t)

	e := obsNewEngine()
	e.Use(Otel("m99-svc"))
	e.GET("/m99/otel/traceid", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	w := ut.PerformRequest(e, "GET", "/m99/otel/traceid", nil)

	if got := w.Header().Get("X-Trace-Id"); got == "" {
		t.Error("响应头里没有 X-Trace-Id")
	}
}

// TestOtel_ErrorStatusMarksSpan 5xx 要把 span 标成 Error
func TestOtel_ErrorStatusMarksSpan(t *testing.T) {
	rec := obsSetupTracer(t)

	e := obsNewEngine()
	e.Use(Otel("m99-svc"))
	e.GET("/m99/otel/err", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusInternalServerError, "boom")
	})
	ut.PerformRequest(e, "GET", "/m99/otel/err", nil)

	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("没有记录到 span")
	}
	if spans[0].Status().Code != codes.Error {
		t.Errorf("span status code = %v,want Error", spans[0].Status().Code)
	}
}

// ── M105(2026-10-28):trace id 必须**在 handler 跑之前**就落地 ──────────
//
// 原来的写法把 trace id 放在 `c.Next(ctx)` **之后**,两个后果:
//  1. handler panic → 栈展开跳过那一行 → Recover 兜出来的 500 **没有 trace id**
//     —— 而"出错了却查不到链路标识"恰恰是最需要标识的情况;
//  2. 只写响应头 → 请求侧一直是 Trace() 放的占位符,
//     audit.log / sys_operation_log / Caller 里记的是占位符而不是真的 trace_id。
//
// 修法:span 一起就写(trace_id 在 Start 之后立刻确定,提前写没有副作用)。

// m105TraceIDRe W3C trace_id 是 32 位十六进制
var m105TraceIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TestOtel_WritesTraceIDToRequestAndResponseHeader 写一处,处处对
func TestOtel_WritesTraceIDToRequestAndResponseHeader(t *testing.T) {
	obsSetupTracer(t)

	var reqTid, respTid string
	e := obsNewEngine()
	e.Use(Otel("m105-svc"))
	e.GET("/m105/otel", func(ctx context.Context, c *app.RequestContext) {
		reqTid = string(c.Request.Header.Peek(response.HeaderTraceID))
		respTid = string(c.Response.Header.Peek(response.HeaderTraceID))
		c.String(http.StatusOK, "ok")
	})
	w := ut.PerformRequest(e, "GET", "/m105/otel", nil)

	if !m105TraceIDRe.MatchString(respTid) {
		t.Fatalf("响应头 trace id = %q,不是 32 位十六进制", respTid)
	}
	if reqTid != respTid {
		t.Fatalf("请求头(%q)与响应头(%q)的 trace id 不一致 —— 请求侧还是占位符", reqTid, respTid)
	}
	if got := w.Header().Get(response.HeaderTraceID); got != respTid {
		t.Fatalf("客户端看到的 X-Trace-Id(%q)与服务端写的不一致(%q)", got, respTid)
	}
}

// TestOtel_PanicPathStillCarriesTraceID 是 M105 修的那条
func TestOtel_PanicPathStillCarriesTraceID(t *testing.T) {
	obsSetupTracer(t)

	e := obsNewEngine()
	e.Use(Recover())
	e.Use(Otel("m105-svc"))
	e.GET("/m105/otel-panic", func(ctx context.Context, c *app.RequestContext) {
		panic("boom")
	})
	w := ut.PerformRequest(e, "GET", "/m105/otel-panic", nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("panic 后的状态码 = %d, want 500", w.Code)
	}
	tid := w.Header().Get(response.HeaderTraceID)
	if !m105TraceIDRe.MatchString(tid) {
		t.Fatalf("panic 响应没有 trace id(%q) —— 出错时最需要的就是链路标识", tid)
	}
}
