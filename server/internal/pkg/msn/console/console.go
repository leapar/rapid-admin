// 控制台 SMS sender —— 仅日志打印
// 用途: dev 阶段 / 未配置运营商凭证时 fallback, 验证码直接打印到日志方便自测
package console

import (
	"context"

	"go.uber.org/zap"
)

type ConsoleSender struct{}

func New() *ConsoleSender { return &ConsoleSender{} }

// Send 把验证码打到 zap 日志(Info 级)
func (s *ConsoleSender) Send(_ context.Context, phone, code, scene string) error {
	zap.L().Info("[sms:console] 验证码(未真实下发)",
		zap.String("phone", phone),
		zap.String("code", code),
		zap.String("scene", scene),
	)
	return nil
}

// Provider 标识
func (s *ConsoleSender) Provider() string { return "console" }
