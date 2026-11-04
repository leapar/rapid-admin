// Per-route 请求超时中间件
// 文档 §1.3 #14 P0
package middleware

import (
	"context"
	"errors"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
)

// Timeout 包装 handler,设 ctx deadline
//   - d > 0: 创建 ctx with timeout 传给下游 handler
//   - d <= 0: noop(等价于不挂中间件)
//
// 重要:本中间件是"软超时" — 仅设置 ctx deadline,handler 是否真中断取决于:
//  1. handler 调了 ctx.Done() 自检(标准 SQL driver / redis client 都遵守)
//  2. 或 GORM WithContext 传递了 ctx(本项目已统一)
//
// 不强制 kill handler goroutine(Go 没法定时 kill goroutine),超时的请求在 DB/IO
// 完成前会继续占连接,只是返回路径立刻返 503。
//
// 超时分支处理:
//   - 已写入响应(status >= 100)→ 不覆盖,handler 自己负责
//   - 未写入 → 返 503 + 业务码 SERVICE_UNAVAILABLE
func Timeout(d time.Duration) app.HandlerFunc {
	if d <= 0 {
		return func(ctx context.Context, c *app.RequestContext) { c.Next(ctx) }
	}
	return func(ctx context.Context, c *app.RequestContext) {
		timeoutCtx, cancel := context.WithTimeout(ctx, d)
		defer cancel()

		c.Next(timeoutCtx)

		// 超时判断:仅在响应未写入时返 504
		if timeoutCtx.Err() == context.DeadlineExceeded && responseUnwritten(c) {
			L().Warn("request timeout",
				zap.String("path", string(c.Path())),
				zap.String("method", string(c.Method())),
				zap.Duration("limit", d),
			)
			response.Fail(c, errcode.GatewayTimeout, "请求处理超时")
		}
	}
}

// responseUnwritten 判断"handler 还没写出响应"。
//
// # ⚠️ M105(2026-10-28):这个判据换过一次,原来那个是死代码
//
// 原来的判据是 `c.Response.StatusCode() < 100` —— 意图是"还没写过响应"。
// 但 **Hertz 的 `Response.StatusCode()` 在没写过响应时返回 200**,
// 不是 0:
//
//	hertz@v0.9.6 pkg/protocol/header.go:978
//	func (h *ResponseHeader) StatusCode() int {
//	    if h.statusCode == 0 { return consts.StatusOK }   // ← 200
//	    return h.statusCode
//	}
//
// 于是 `200 < 100` 恒为 false → **超时分支永远不成立**:
// 超时的请求返回的是**空 body 的 200**,不是 504。
//
// 连带后果:熔断器(`Breaker`)判失败看的是 `status >= 500` ——
// 一个 200 的空响应**不算失败** → 熔断器从不把超时计为失败 →
// "连续失败 N 次跳闸"对超时场景完全失效。
//
// 现在改成"status 还是默认 200 **且** body 为空"才算没写过:
//   - handler 什么都没写 → 补 504 ✓
//   - handler 写了 200 + 有 body → 不覆盖(它是有效响应)✓
//   - handler 写了 500 / 403 等 → status != 200 → 不覆盖 ✓
//   - handler 写了 204(空 body)→ status != 200 → 不覆盖 ✓
func responseUnwritten(c *app.RequestContext) bool {
	return c.Response.StatusCode() == 200 && len(c.Response.Body()) == 0
}

// ErrHandlerTimeout 显式错误 — 留作 handler 自检到 ctx.Done() 时返
var ErrHandlerTimeout = errors.New("handler timeout")
