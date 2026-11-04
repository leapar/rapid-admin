// Package emailx 邮件发送 —— 抽象 Provider(§1.9 #9 P2)
//
// 目的:
//   - 统一邮件下发通道(注册验证 / 通知 / 营销邮件)
//   - 跟 sms/ 平行,接口相似便于混用
//   - 0 业务耦合:框架只提供"通道",业务按"场景+模板"投递
//
// 与 sms/ 的区别:
//   - 短信是模板化(template code + params),邮件是"自由内容"(subject + html + text)
//   - 邮件走 SMTP / HTTP / 控制台,短信走 SDK
//   - 邮件不强制 5min TTL 验证码(邮件验证码常见 10-30min)
//
// 选型:
//   - 通用 SMTP:任何 SMTP 服务都接(gmail / outlook / 自建 postfix / 阿里云邮箱推送 SMTP)
//   - 阿里云 DirectMail:HTTP REST,适合国内发信 + 退信/打开跟踪
//   - Console:dev 模式打印,无真实发送
//   - Mock:测试用
//
// 用法:
//
//	// 1. 选 provider
//	smtpSender, _ := emailx.NewSMTP(emailx.SMTPConfig{Host: "smtp.example.com", Port: 465, Username: "...", Password: "..."})
//	consoleSender := emailx.NewConsole()
//	mockSender := emailx.NewMock()
//
//	// 2. 业务组装 Message 并 Send
//	_ = smtpSender.Send(ctx, emailx.Message{
//	    From:    "noreply@example.com",
//	    FromName: "平台",
//	    To:      []string{"user@example.com"},
//	    Subject: "欢迎注册",
//	    HTML:    "<h1>欢迎</h1>",
//	    Text:    "欢迎",
//	})
package emailx

import (
	"context"
	"time"
)

// Sender 邮件下发接口
//
//	Send 返回 error 表示发送失败(biz_error / network_error)
type Sender interface {
	// Send 发送邮件
	//   - 返回 nil:发送成功(provider 已接受,不代表到达收件箱)
	//   - 返回 non-nil:发送失败,调用方应降级(重试 / 走备用通道 / 写 audit)
	Send(ctx context.Context, msg Message) error

	// Provider 返回 provider 名称(用于 smsaudit / 监控)
	Provider() string
}

// Message 邮件内容
//
//	From 必填,FromName 可选(显示名)
//	To 至少 1 个,CC/BCC 可选
//	Subject 必填,HTML/Text 至少 1 个(优先 HTML)
type Message struct {
	From     string   // 发件邮箱
	FromName string   // 发件人显示名(可选)
	To       []string // 收件人(至少 1)
	CC       []string // 抄送(可选)
	BCC      []string // 密送(可选)
	ReplyTo  string   // 回复地址(可选)
	Subject  string   // 主题
	HTML     string   // HTML 正文(可选,与 Text 至少 1)
	Text     string   // 纯文本正文(可选,与 HTML 至少 1)
	// Headers 额外邮件头(可选,如 X-Mailer / X-Priority)
	//  key 推荐用 "X-" 前缀
	Headers map[string]string
	// Attachments 附件(可选,file path 二选一;暂未实现,留接口)
	Attachments []Attachment
}

// Attachment 附件
type Attachment struct {
	Filename    string
	ContentType string
	// Data 原始字节;FilePath 文件路径(任一非空)
	Data     []byte
	FilePath string
}

// IsValid 校验必填字段
func (m Message) IsValid() error {
	if m.From == "" {
		return errInvalid("from 不能为空")
	}
	if len(m.To) == 0 {
		return errInvalid("to 不能为空")
	}
	if m.Subject == "" {
		return errInvalid("subject 不能为空")
	}
	if m.HTML == "" && m.Text == "" {
		return errInvalid("html/text 至少 1 个")
	}
	return nil
}

// ============== Errors ==============

// EmailError 邮件发送错误
type EmailError struct {
	Provider string
	Code     string // provider 业务码 / "network" / "config"
	Message  string
	Retry    bool   // 是否值得重试
}

func (e *EmailError) Error() string { return e.Provider + ": " + e.Code + ": " + e.Message }

// errInvalid 参数校验错误(不会重试)
func errInvalid(msg string) *EmailError {
	return &EmailError{Code: "invalid", Message: msg, Retry: false}
}

// ============== 通用选项 ==============

// Option 可选配置
type Option struct {
	// Timeout 单次发送超时(默认 30s)
	Timeout time.Duration
}
