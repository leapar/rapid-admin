// 邮件通道 —— 把 notifyx.Message 翻译成 emailx.Message
//
// ⚠️ **邮件正文是 HTML,内容来自业务** —— 一律 html.EscapeString 转义,
//
//	不转义的话,标题里一个 "<script>" 就会在邮件客户端里执行
//	(邮件客户端会禁脚本,但 HTML 注入照样能改样式、伪造钓鱼链接)。
package notifyx

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"

	"rapid-admin/internal/pkg/emailx"
)

// EmailChannel 邮件投递通道
type EmailChannel struct {
	sender   emailx.Sender
	from     string
	fromName string
	// baseURL 站点外链前缀(如 https://admin.example.com)
	baseURL string
}

// NewEmailChannel 构造邮件通道
//
// ⚠️ **sender 为 nil = 这条通道不该存在** —— 调用方(main.go)应该在
//
//	provider 为空时**根本不构造**它,而不是构造一个空通道再靠 Send 报错。
//	少注册一条通道比多注册一条空通道好:后者会让 Result 里出现一个
//	永远 failed 的通道名,监控上看着像坏了。
func NewEmailChannel(sender emailx.Sender, from, fromName, baseURL string) *EmailChannel {
	return &EmailChannel{
		sender:   sender,
		from:     from,
		fromName: fromName,
		baseURL:  strings.TrimRight(baseURL, "/"),
	}
}

// Name 通道名
func (c *EmailChannel) Name() string { return "email" }

// Send 发一封邮件
//
// ⚠️ **没有邮箱 = skipped,不是 failed** —— 后台账号大量不填邮箱,
//
//	那是常态不是错误。记成 failed 会让"投递成功率"这个指标永远掉。
func (c *EmailChannel) Send(ctx context.Context, r Recipient, m Message) error {
	if c.sender == nil {
		return fmt.Errorf("%w: 邮件通道没有 sender", ErrSkip)
	}
	if strings.TrimSpace(r.Email) == "" {
		return fmt.Errorf("%w: 收件人没有邮箱", ErrSkip)
	}
	if strings.TrimSpace(c.from) == "" {
		// ⚠️ **这是配置问题,不是收件人的问题** —— 不 skip,
		//   因为 skip 会让"配错了却没人发现"。
		return fmt.Errorf("notifyx: 邮件通道没有配置发件地址(from)")
	}
	if strings.TrimSpace(m.Title) == "" {
		return fmt.Errorf("notifyx: 邮件标题为空")
	}

	link := c.absoluteLink(m.Link)
	msg := emailx.Message{
		From:     c.from,
		FromName: c.fromName,
		To:       []string{r.Email},
		Subject:  m.Title,
		Text:     plainText(m, link),
		HTML:     htmlBody(m, link),
	}
	return c.sender.Send(ctx, msg)
}

// absoluteLink 把相对链接拼成绝对地址
//
// ⚠️ **邮件里的相对链接是死的** —— 收件人的邮件客户端不知道"当前站点"
//
//	是哪个,点 "/approval/123" 会被解析成本地文件路径或直接报错。
//	baseURL 没配时:绝对链接照用,相对链接**丢掉**(好过给一个点不开的链接)。
func (c *EmailChannel) absoluteLink(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return link
	}
	if c.baseURL == "" {
		return ""
	}
	if !strings.HasPrefix(link, "/") {
		link = "/" + link
	}
	u, err := url.Parse(c.baseURL + link)
	if err != nil {
		return ""
	}
	return u.String()
}

// plainText 纯文本正文
//
// ⚠️ **Text 不是可有可无的** —— 相当一部分收件人(以及很多网关的
//
//	预览)是纯文本渲染的,只给 HTML 会看到一坨标签。
func plainText(m Message, link string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(m.Content))
	if link != "" {
		b.WriteString("\n\n")
		b.WriteString(link)
	}
	return b.String()
}

// htmlBody HTML 正文
func htmlBody(m Message, link string) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;font-size:14px;line-height:1.6;color:#1f2329">`)
	if title := strings.TrimSpace(m.Title); title != "" {
		b.WriteString(`<h2 style="margin:0 0 12px;font-size:18px;font-weight:600">`)
		b.WriteString(html.EscapeString(title))
		b.WriteString(`</h2>`)
	}
	if content := strings.TrimSpace(m.Content); content != "" {
		b.WriteString(`<div style="white-space:pre-wrap">`)
		b.WriteString(html.EscapeString(content))
		b.WriteString(`</div>`)
	}
	if link != "" {
		b.WriteString(`<p style="margin:20px 0 0"><a href="`)
		b.WriteString(html.EscapeString(link))
		b.WriteString(`" style="color:#1677ff">查看详情</a></p>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}
