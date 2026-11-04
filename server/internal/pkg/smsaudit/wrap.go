// Package smsaudit Sender 装饰器 —— 自动把 Send 调用结果写审计
//
// 用法(在 main.go 组装 SMS sender 处):
//
//	realSender := buildSmsSender(cfg.SMS)
//	smsSender := smsaudit.Wrap(realSender) // 装饰:计时 + 写审计
//	registry.Auth = service.NewAuthService(db, jwtMulti, smsSender, cfg.SMS)
//
// 注:
//   - 装饰器记 success / error / cost_ms / provider
//   - 写审计失败不影响主流程(异步批量,失败只打 error)
//   - 不重复记录(不是 wrapper of wrapper)
package smsaudit

import (
	"context"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/msn"
)

// Wrap 装饰 msn.Sender 自动计 send 耗时 + 写审计
func Wrap(inner msn.Sender) msn.Sender {
	return &auditSender{inner: inner}
}

type auditSender struct {
	inner msn.Sender
}

func (a *auditSender) Send(ctx context.Context, phone, code, scene string) error {
	start := time.Now()
	err := a.inner.Send(ctx, phone, code, scene)
	cost := int(time.Since(start).Milliseconds())

	success := err == nil
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
		logger.L().Warn("sms send failed",
			zap.String("phone", phone), zap.String("scene", scene),
			zap.String("provider", a.inner.Provider()),
			zap.Error(err))
	}

	Record(ctx, SendLog{
		Phone:    phone,
		Scene:    scene,
		Code:     code,
		Provider: a.inner.Provider(),
		Success:  success,
		ErrorMsg: errMsg,
		CostMs:   cost,
	})
	return err
}

func (a *auditSender) Provider() string { return a.inner.Provider() }
