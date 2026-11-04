// emailx —— 邮件下发
//
// ## 这个包解决什么问题
//
// 邮件通道有三种:SMTP、阿里云、Console(只打日志)。
// 业务不该关心用的是哪种 ⇒ 统一成 `Sender` 接口:
//
//	Send(ctx, msg) error   // nil = 服务商已接受(不代表到达收件箱)
//	Provider() string      // provider 名,给 smsaudit / 监控用
//
// ## 关键点
//
//   - **Send 返回 nil 只代表服务商收下了**,不代表进了收件箱 ——
//     业务侧的"发送成功"文案要按这个口径写
//   - Message.IsValid() 前置校验:From 必填、至少 1 个收件人、
//     Subject 必填、HTML/Text 至少 1 个
//   - MockSender 给测试用:能数发送次数、能取最后一封
//   - ConsoleSender 给本地开发用:只打日志,不真发
//
// ## 跑法
//
//	go run ./examples/emailx
package main

import (
	"context"
	"fmt"

	"rapid-admin/internal/pkg/emailx"
)

func main() {
	ctx := context.Background()

	// ── 一、消息校验 ────────────────────────────────────────────
	fmt.Println("=== 消息校验 ===")
	ok := emailx.Message{
		From:    "noreply@example.com",
		To:      []string{"zhangwei@example.com"},
		Subject: "您的验证码",
		Text:    "您的验证码是 123456,5 分钟内有效。",
	}
	fmt.Printf("合法消息校验 = %v\n", ok.IsValid())

	bad := emailx.Message{From: "noreply@example.com", Subject: "缺收件人"}
	fmt.Printf("缺收件人校验 = %v\n", bad.IsValid())

	noBody := emailx.Message{
		From:    "noreply@example.com",
		To:      []string{"zhangwei@example.com"},
		Subject: "缺正文",
	}
	fmt.Printf("缺正文校验   = %v\n", noBody.IsValid())

	// ── 二、MockSender:测试用 ───────────────────────────────────
	fmt.Println()
	fmt.Println("=== MockSender ===")
	mock := emailx.NewMock()
	fmt.Printf("provider = %s\n", mock.Provider())

	if err := mock.Send(ctx, ok); err != nil {
		fmt.Println("发送失败:", err)
		return
	}
	_ = mock.Send(ctx, emailx.Message{
		From:    "noreply@example.com",
		To:      []string{"lisi@example.com"},
		Subject: "第二封",
		Text:    "正文",
	})
	fmt.Printf("已发送 %d 封\n", mock.Count())
	last, has := mock.Last()
	if has {
		fmt.Printf("最后一封: to=%v subject=%q\n", last.To, last.Subject)
	}
	mock.Reset()
	fmt.Printf("Reset 后已发送 %d 封\n", mock.Count())

	// ── 三、ConsoleSender:本地开发用 ───────────────────────────
	fmt.Println()
	fmt.Println("=== ConsoleSender(只打日志,不真发)===")
	console := emailx.NewConsole()
	fmt.Printf("provider = %s\n", console.Provider())
	if err := console.Send(ctx, ok); err != nil {
		fmt.Println("发送失败:", err)
		return
	}

	// ── 四、生产环境用 SMTP / 阿里云 ───────────────────────────
	//
	// smtp, err := emailx.NewSMTP(emailx.SMTPConfig{
	// 	Host: "smtp.exmail.qq.com", Port: 465,
	// 	Username: "noreply@example.com", Password: "***",
	// 	From: "noreply@example.com", FromName: "Rapid Admin 后台快速开发平台",
	// 	Timeout: 30 * time.Second,
	// })
	// if err != nil { return err }
	// aliyun, err := emailx.NewAliyun(emailx.AliyunConfig{...})
	//
	// 两者都实现 Sender ⇒ 业务代码不用改。
	fmt.Println()
	fmt.Println("生产环境:NewSMTP(...) / NewAliyun(...) 都实现 Sender 接口,业务代码不用改。")
}
