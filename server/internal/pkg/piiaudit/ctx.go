// Package piiaudit 上下文键 + 注入/读取 helper
//
// 与 authz 主体解耦:piiaudit 只在 extractFromCtx 里读 ctx 里的 principal/request_id/trace_id
// 各中间件用自己已存在的注入方式(access_log 注 request_id、otel 注 trace_id、principal middleware 注 principal)
//
// 这里提供:
//   - 中间件侧用的 Inject*:给 ctx 塞值(若中间件已有同名 hook,直接用那个)
//   - piiaudit 内部用的 fromContext*:从 ctx 取值
//   - 暴露给其他包:RequestIDFromContext / TraceIDFromContext / CtxKeyIP / CtxKeyUA
//     (cryptaudit 共用 ctx key,不重复定义)
package piiaudit

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// ---- request_id ----

type ctxKeyRequestID struct{}

// InjectRequestID 注入 request_id(给中间件 / 测试用)
func InjectRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID{}, id)
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(ctxKeyRequestID{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// RequestIDFromContext 暴露给其他包(原 lowercase 保留为内部用)
func RequestIDFromContext(ctx context.Context) string { return requestIDFromContext(ctx) }

// ---- trace_id ----

type ctxKeyTraceID struct{}

// InjectTraceID 注入 trace_id(给中间件 / 测试用)
func InjectTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyTraceID{}, id)
}

// traceIDFromContext 取 trace_id
//
// ⚠️ M100(2026-10-26):加了 **OTel span context 兜底**。
//
// 原来只有本包自己的 ctx key 一条路,而**全仓没有任何地方注入这个 key**
// (`InjectTraceID` 零调用方)→ `sys_pii_access_log.trace_id` 恒为空。
//
// 而 otel 中间件是把 trace_id 放在 **OTel span context** 里的
// (`api/middleware/otel.go` 的 TraceIDFromContext 就是这么读的),
// 跟本包的 ctx key 是**两套完全不同的键** —— 各写各的,谁也读不到谁。
//
// 现在:先读自己的 key(显式注入优先),读不到再从 OTel span context 取。
// 这样不依赖"谁去注入",otel 中间件起了 span 就一定有 trace_id。
func traceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(ctxKeyTraceID{}); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	// 兜底:OTel span context(otel 中间件起过 span 才有)
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// TraceIDFromContext 暴露给其他包
func TraceIDFromContext(ctx context.Context) string { return traceIDFromContext(ctx) }

// ---- ip / user_agent(cryptaudit 共用) ----

// CtxKeyIP ip 的 ctx key(类型用于 ctx.Value)
type CtxKeyIP struct{}

// CtxKeyUA user_agent 的 ctx key
type CtxKeyUA struct{}

// InjectIP 注入客户端 IP(给中间件 / 测试用)
//
// ⚠️ M100(2026-10-26)新增:这两个 key 早就存在、cryptaudit 也在读,
// 但**从来没有任何地方写入过** → `sys_pii_access_log.ip` / `user_agent`
// 恒为空。审计表少了"从哪台机器来"这一维,等保场景下这条记录基本没用。
func InjectIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, CtxKeyIP{}, ip)
}

// InjectUA 注入 User-Agent(给中间件 / 测试用)
func InjectUA(ctx context.Context, ua string) context.Context {
	return context.WithValue(ctx, CtxKeyUA{}, ua)
}
