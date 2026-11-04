// AccessLog 访问日志(独立 access.log 文件)
// 文档 §10.4
package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/service"
)

// AccessLog 写独立 access logger
// 入口:使用 logger.Access() 取得 zap.Core(由 main 注入)
// traceId 优先取 ctx 里的 OTel span_context(若 OTel 中间件已注入),fallback 到 header
//
// # ⚠️ M99(2026-10-26)修:此前写的是**无名** logger,access.log 从来没被写过
//
// `pkg/logger` 的"按名路由到独立文件"靠的是 `zapcore.Entry.LoggerName`,
// 而 LoggerName 只有 `Named(...)` 派生的 logger 才非空。原来的 `L().Info(...)`
// 是无名 logger → LoggerName == "" → 路由直接跳过它。
//
// 结果:**access.log 这个文件根本不会被创建**(rotator 是懒创建的),
// 运维排查时想看访问日志,那个文件永远是空的 —— 而 info.log 里混着
// 全部业务日志,没法当访问日志用。
//
// 现在改用 `logger.Access()`(Named("access"))→ 真的落 access.log。
func AccessLog() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		start := time.Now()
		c.Next(ctx)
		dur := time.Since(start)
		method := string(c.Method())
		path := string(c.Path())
		// trace_id 优先 ctx(Otel 起过 span 才有),否则用 header(Otel 关时)
		traceID := TraceIDFromContext(ctx)
		if traceID == "" {
			traceID = string(c.GetHeader(response.HeaderTraceID))
		}
		spanID := SpanIDFromContext(ctx)
		// 写到 **Named("access")** logger —— 只有具名 logger 才会被
		// pkg/logger 的 namedCore 路由到 access.log(见上面的说明)
		fields := []zap.Field{
			zap.String("method", string(c.Method())),
			zap.String("path", string(c.Path())),
			zap.Int("status", c.Response.StatusCode()),
			zap.Duration("cost", dur),
			zap.String("ip", c.ClientIP()),
			zap.String("ua", string(c.UserAgent())),
			zap.String("requestId", string(c.GetHeader(response.HeaderRequestID))),
			zap.String("traceId", traceID),
			zap.String("spanId", spanID),
		}
		logger.Access().Info("access", fields...)
		// ---- 系统日志钩子(2026-11-03):慢接口 / 5xx 落 sys_system_log ----
		// zap access.log 照写不误;这里是把"值得在审计中心看一眼"的请求
		// 再落一份结构化记录。判断放一个 if 里:正常请求零额外开销。
		status := c.Response.StatusCode()
		if SystemLogSvc != nil && (dur >= systemLogSlowHTTPThreshold || status >= 500) {
			level, source := "info", "slow_http"
			msg := fmt.Sprintf("慢接口: %s %s 耗时 %dms", method, path, dur.Milliseconds())
			if status >= 500 {
				level, source = "error", "http_error"
				msg = fmt.Sprintf("接口 5xx: %s %s status=%d", method, path, status)
			}
			recordSystemLog(&service.SystemLogEntry{
				Level:      level,
				Source:     source,
				Method:     method,
				Path:       path,
				Message:    msg,
				TraceID:    traceID,
				UserID:     userIDFromCtx(ctx),
				DurationMs: dur.Milliseconds(),
			})
		}
	}
}

// systemLogSlowHTTPThreshold 慢接口阈值
//
// 慢 SQL 是 500ms(slowquery),慢接口定 2s —— 接口比单条 SQL 多了
// 网络往返 / 多条查询 / 业务计算,阈值放宽一档,不然告警全被慢 SQL 覆盖。
const systemLogSlowHTTPThreshold = 2 * time.Second

// recordSystemLog 系统日志薄封装(nil 安全)
//
// caller 已判过 SystemLogSvc != nil,这里再判一道是兜底;UserID 在这里补
// (Principal 中间件在 AccessLog 内层已注入 caller,此时取得到)。
func recordSystemLog(entry *service.SystemLogEntry) {
	if SystemLogSvc == nil || entry == nil {
		return
	}
	SystemLogSvc.RecordSystemLog(entry)
}
