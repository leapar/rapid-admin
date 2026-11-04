// Recover panic 恢复 + 统一兜底(§1.4 #10 P2)
//
// 改进点(对比 v1):
//   1. 走 errcode.Internal 的 default 文案(不硬编码 "服务内部错误")
//   2. log 带 stack trace(runtime/debug.Stack)+ path + user_id + trace_id
//   3. metrics:app_panic_total{path,method} 计数,告警用
//   4. 响应统一走 response.Fail → errcode.Internal(自动带 trace_id 给前端)
//
// 行为:
//   - panic → 记 Error 级 log(stack + ctx info) + 返回 500 + abort
//   - 非 panic → 透传
//
// 文档 §10.1
package middleware

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/service"
)

// Recover panic 恢复 + 统一兜底
func Recover() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			// 1. 收集诊断信息
			path := string(c.Path())
			method := string(c.Method())
			stack := debug.Stack()
			userID := userIDFromCtx(ctx)
			traceID := response.TraceIDFrom(c)

			// 2. log:Error 级 + 结构化字段(stack 用 ByteString 避免换行炸掉 JSON 日志)
			logger.L().Error("panic recovered",
				zap.Any("err", r),
				zap.String("path", path),
				zap.String("method", method),
				zap.Uint64("user_id", userID),
				zap.String("trace_id", traceID),
				zap.ByteString("stack", stack),
			)

			// 系统日志钩子(2026-11-03):panic 落 sys_system_log
			recordSystemLog(&service.SystemLogEntry{
				Level:   "error",
				Source:  "panic",
				Method:  method,
				Path:    path,
				Message: fmt.Sprintf("panic: %v", r),
				Detail:  string(stack),
				TraceID: traceID,
				UserID:  userID,
			})

			// 3. 统一响应(用 errcode.Internal default 文案,响应里带 trace_id)
			response.Fail(c, errcode.Internal, errcode.Msg(errcode.Internal))
			c.Abort()
		}()
		c.Next(ctx)
	}
}

// userIDFromCtx 从 ctx 拿 caller.UserID(Principal middleware 已注入)
//   - 未登录/未挂 Principal → 0
func userIDFromCtx(ctx context.Context) uint64 {
	if caller, ok := service.FromContext(ctx); ok && caller != nil {
		return caller.UserID
	}
	return 0
}
