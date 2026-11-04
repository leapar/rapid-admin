// OpenTelemetry trace 中间件 — 每个 HTTP 请求开一个 server span
// 文档 §1.3 #13 + §1.8 #7 P0
package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"rapid-admin/internal/pkg/response"
)

// Otel HTTP 入口开 server span
//   - 从 X-Trace-Id header 透传(W3C traceparent 格式优先,Otel SDK 自动解析)
//   - 走完 next 后:set http.response.status_code / http.request.method / url.path
//   - 5xx → span SetStatus(Error, statusText);其他 → Ok
//   - 响应 header 注入 X-Trace-Id(取 W3C trace_id 十六进制)
//
// 必须挂在 Trace() 中间件之后:Trace 先放 X-Trace-Id,Otel 再按需覆盖 W3C 格式。
//
// 注意:不依赖 hertz-contrib/otel(该包未发布稳定版),自实现 < 100 行。
func Otel(serviceName string) app.HandlerFunc {
	tracer := otel.Tracer(serviceName)
	propagator := otel.GetTextMapPropagator()

	return func(ctx context.Context, c *app.RequestContext) {
		// 1. 从 request header 抽 W3C trace context(透传上游)
		reqHeaders := httpHeadersCarrier{c: c}
		parentCtx := propagator.Extract(ctx, reqHeaders)

		// 2. 起新 span(若 parentCtx 已有 span,会成为 child;否则是新 root)
		// ⚠️ M99(2026-10-26):改用 metrics.go 的 routePath(路由模板)。
		// 此前这里用的是 otelRoutePattern —— 一份用**原始 path** 的拷贝,
		// 与 metrics 那份"一份对一份错",span 名基数会随 URL 实参爆炸。
		spanName := string(c.Method()) + " " + routePath(c)
		ctx, span := tracer.Start(parentCtx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.request.method", string(c.Method())),
				attribute.String("url.path", string(c.Path())),
				attribute.String("url.scheme", scheme(c)),
				attribute.String("server.address", string(c.Host())),
				attribute.String("user_agent.original", string(c.UserAgent())),
				attribute.String("client.address", c.ClientIP()),
			),
		)
		defer span.End()

		// 3. 注入下游调用用的 header
		propagator.Inject(ctx, reqHeaders)

		// 3'. ⚠️ M105(2026-10-28):span 一起就把 trace_id 写进
		// **请求头 + 响应头**,不再等到收尾时只写响应头。
		//
		// 原来写在 `c.Next(ctx)` **之后**:
		//   - handler panic 时,栈展开会跳过这一行 → Recover 兜出来的 500
		//     **没有 trace id**,前端报障拿不到链路标识;
		//   - 只写响应头 → 请求侧一直是 Trace() 放的占位符,
		//     于是 audit.log / sys_operation_log / Caller 里记的 trace_id
		//     是占位符而不是真正的 W3C trace_id(见 trace.go 的说明)。
		//
		// span 的 trace_id 在 Start 之后立刻就确定了,提前写没有任何副作用。
		if sc := span.SpanContext(); sc.IsValid() {
			tid := sc.TraceID().String()
			c.Header(response.HeaderTraceID, tid)
			c.Request.Header.Set(response.HeaderTraceID, tid)
		}

		// 4. 跑 handler
		start := time.Now()
		c.Next(ctx)
		dur := time.Since(start)

		// 5. 收尾:状态码 / 错误
		statusCode := c.Response.StatusCode()
		span.SetAttributes(
			attribute.Int("http.response.status_code", statusCode),
			attribute.Float64("http.server.request.duration_ms", float64(dur.Microseconds())/1000.0),
		)
		if statusCode >= 500 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}
	}
}

// httpHeadersCarrier 适配 Hertz RequestContext 到 OTel TextMapCarrier
//
// # ⚠️ M99(2026-10-26)修:Set 此前写的是**响应头**
//
// Hertz 的 `c.Header(k, v)` 就是 `ctx.Response.Header.Set(k, v)` ——
// 而 `propagator.Inject(ctx, reqHeaders)` 走的是这个 Set,
// 于是注入出去的 `traceparent` 落到了**响应**头上:
// 每个响应都多出一个 `traceparent: 00-...` 内部头。
//
// 语义上正确的位置是**请求头**:Inject 的含义是"把当前 trace 上下文
// 交给下游"(下游 handler / 下游服务从请求头里读),
// 而 carrier 的 Get 读的也正是请求头(`c.GetHeader` → `Request.Header.Peek`)。
// 读写不对称 = carrier 坏了。
//
// 注意:响应侧的 `X-Trace-Id` 是另一回事,由 Otel 中间件在收尾时
// 显式 `c.Header(response.HeaderTraceID, ...)` 写,不受本改动影响。
type httpHeadersCarrier struct {
	c *app.RequestContext
}

func (h httpHeadersCarrier) Get(key string) string {
	return string(h.c.GetHeader(key))
}

func (h httpHeadersCarrier) Set(key, value string) {
	// 写**请求头**:Inject 的语义是"把当前 trace 上下文交给下游"
	h.c.Request.Header.Set(key, value)
}

func (h httpHeadersCarrier) Keys() []string {
	// Hertz 没有 Keys API,返空即可(Inject/Extract 仍工作,只是不能列出所有 key)
	return nil
}

func scheme(c *app.RequestContext) string {
	if string(c.GetHeader("X-Forwarded-Proto")) == "https" {
		return "https"
	}
	return "http"
}

// TraceIDFromContext 取 context 中的 trace_id(16 字节十六进制) — 供 access_log 关联
//   - 没起 span(Init 没跑 / OTel 关闭)→ 返 ""
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// SpanIDFromContext 取当前 span_id(8 字节十六进制)
func SpanIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.SpanID().String()
}
