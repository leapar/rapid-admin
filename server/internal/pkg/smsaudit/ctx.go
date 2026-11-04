// Package smsaudit 上下文键 + 注入/读取 helper
//
// # ctx key 统一到 piiaudit(不要自己再定义一套)
//
// 原来本包自定义了私有 `ctxKeyRequestID{}` / `ctxKeyTraceID{}`,
// 而**全仓唯一注入这两样东西的地方**是 `internal/api/middleware/trace.go`,
// 它用的是 **piiaudit** 的 key:
//
//	ctx = piiaudit.InjectRequestID(ctx, rid)
//	ctx = piiaudit.InjectIP(ctx, c.ClientIP())
//	ctx = piiaudit.InjectUA(ctx, string(c.UserAgent()))
//
// 两套 key 谁也读不到谁 ⇒ `sys_sms_send_log.request_id` / `trace_id`
// 在生产里**恒为空**;ip / user_agent 更是从来没读过。
//
// 现在本包的注入器直接转发给 piiaudit,读写都用同一套 key。
package smsaudit

import (
	"context"

	"rapid-admin/internal/pkg/piiaudit"
)

// InjectRequestID 注入 request_id
//
// 转发给 piiaudit —— 与中间件(trace.go)用的是同一个 ctx key。
func InjectRequestID(ctx context.Context, id string) context.Context {
	return piiaudit.InjectRequestID(ctx, id)
}

// InjectTraceID 注入 trace_id
//
// 转发给 piiaudit —— 与兄弟包 piiaudit / cryptaudit 是同一套 key。
func InjectTraceID(ctx context.Context, id string) context.Context {
	return piiaudit.InjectTraceID(ctx, id)
}

// InjectIP 注入客户端 IP
//
// 转发给 piiaudit —— 本包以前**从不读** ip,表里那一列恒为空。
func InjectIP(ctx context.Context, ip string) context.Context {
	return piiaudit.InjectIP(ctx, ip)
}

// InjectUA 注入 User-Agent
//
// 转发给 piiaudit —— 本包以前**从不读** ua,表里那一列恒为空。
func InjectUA(ctx context.Context, ua string) context.Context {
	return piiaudit.InjectUA(ctx, ua)
}
