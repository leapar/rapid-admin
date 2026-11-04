// Trace 中间件 —— 请求 ID + TraceID
// 文档 §10.1
package middleware

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/piiaudit"
	"rapid-admin/internal/pkg/response"
)

// maxRequestIDLen 请求 ID 的最大长度。
//
// 64 来自 `sys_operation_log.request_id` 的列宽(`gorm:"size:64"`)——
// 超过它,MySQL 严格模式下直接报 "Data too long for column",
// **审计写入失败**。也就是说:一个客户端发一个超长的 X-Request-Id,
// 就能让这次请求的审计记录写不进去。
const maxRequestIDLen = 64

// Trace 注入/透传 X-Request-Id + X-Trace-Id
//
// # ⚠️ M100(2026-10-26):除了写响应头,还把请求元信息**注入 ctx**
//
// 原来 request_id 只是写进了**响应头**,ctx 里没有 —— 而 PII 审计链
// (piiaudit / cryptaudit)是从 **ctx** 里取 request_id 的,于是
// `sys_pii_access_log.request_id` / `sys_crypto_audit_log.request_id`
// **恒为空**。
//
// 同样,ip / user_agent 这两个 ctx key(`pkg/piiaudit` 里定义、
// cryptaudit 一直在读)**从来没有任何地方写入过**。
//
// 结果:一张"谁在什么时候看了手机号"的审计表,三条关联链全断 ——
//
//   - 没有 request_id → 对不上 access.log(那次请求到底发生了没有)
//   - 没有 ip / user_agent → 对不上是谁的机器
//   - 没有 trace_id → 对不上 trace(otel 中间件把 trace_id 放在
//     **span context** 里,与本包的 ctx key 是两套键,谁也读不到谁)
//
// 表还在,行也在,但**查不出东西** —— 而这是 §1.5 #5 的 P0 等保项。
//
// 现在:Trace(全局链的第一个中间件)把 request_id / ip / user_agent 塞进 ctx,
// 下游所有 handler 都能读到;trace_id 由 otel 中间件的 span context 提供,
// piiaudit 侧加了兜底读取(见 `pkg/piiaudit/ctx.go`)。
//
// # ⚠️ M105(2026-10-28):同时写回**请求头**,并对客户端传的值做消毒
//
//  1. **写回请求头** —— 下面 5 处读的是 `c.GetHeader(response.HeaderRequestID)`,
//     那是**请求头**,而 Trace 原来只写**响应头**。两者不是一回事:
//
//     middleware/access_log.go:51   access.log 的 requestId
//     middleware/audit.go:58/79     audit.log + sys_operation_log 的 request_id
//     middleware/auth.go:163        service.Caller.RequestID
//     middleware/sseauth.go:81      service.Caller.RequestID(SSE 路径)
//     pkg/response/response.go:98   **每个响应体里的 traceId 字段**
//
//     于是:M100 修的是 piiaudit 那条 ctx 链,而这 5 处仍然是断的 ——
//     access.log 里的 requestId、审计表里的 request_id、caller.RequestID、
//     以及**每一个 JSON 响应里的 traceId**,全是空串。
//     前端拿 traceId 去报障,报上来的是空字符串。
//
//     写回请求头之后,`c.GetHeader` 与 `c.Response.Header` 拿到的是同一个值,
//     5 处读点不用改 —— 一处写,处处对。
//
//  2. **消毒** —— 客户端可以自带 X-Request-Id / X-Trace-Id。透传是刻意的
//     (跨服务串联要用),但**原样透传**有两个问题:
//
//     - 长度不限 → 落进 `size:64` 的列 → 审计写失败(见 maxRequestIDLen)
//     - 字符不限 → 换行/控制字符进日志行(日志注入)、进响应头
//
//     所以:长度 > 64 或含非 [A-Za-z0-9._:-] 字符 → **丢弃客户端的值,
//     改用自己生成的**。宁可换一个 ID,也不让客户端控制审计字段。
func Trace() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		rid := sanitizeRequestID(string(c.GetHeader("X-Request-Id")))
		if rid == "" {
			rid = newRequestID()
		}
		traceID := sanitizeRequestID(string(c.GetHeader("X-Trace-Id")))
		if traceID == "" {
			traceID = rid
		}
		c.Header(response.HeaderRequestID, rid)
		c.Header(response.HeaderTraceID, traceID)
		// ⚠️ M105:写回**请求头** —— 下游 5 处 `c.GetHeader(...)` 读的就是它。
		// 顺序放在响应头之后,保证"响应头 = 请求头 = 同一个值"。
		c.Request.Header.Set(response.HeaderRequestID, rid)
		c.Request.Header.Set(response.HeaderTraceID, traceID)
		// ⚠️ M100:请求元信息进 ctx(审计链 / 业务 handler 都能读)
		ctx = piiaudit.InjectRequestID(ctx, rid)
		ctx = piiaudit.InjectIP(ctx, c.ClientIP())
		ctx = piiaudit.InjectUA(ctx, string(c.UserAgent()))
		c.Next(ctx)
	}
}

// sanitizeRequestID 校验客户端传入的请求 ID / trace ID。
//
// 返回空串表示"不可用,调用方应改用自己生成的"。
//
// 规则:
//   - 长度 1..64(64 = 审计表列宽)
//   - 字符集 [A-Za-z0-9._:-]
//
// 换行 / 回车 / 控制字符一律拒 —— 它们进日志行就是日志注入,
// 进响应头就是头注入(虽然 Go 的 header writer 会兜底,但不指望下游)。
func sanitizeRequestID(v string) string {
	if v == "" || len(v) > maxRequestIDLen {
		return ""
	}
	for i := 0; i < len(v); i++ {
		ch := v[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case ch == '-', ch == '_', ch == '.', ch == ':':
		default:
			return ""
		}
	}
	return v
}

func newRequestID() string {
	return time.Now().UTC().Format("20060102150405") + "-" + RandomID(6)
}
