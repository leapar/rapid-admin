// 通用 SMTP sender —— §1.9 #9 P2
//
// 走标准库 net/smtp,任何 SMTP 服务都接:
//   - 自建 postfix / 阿里云邮箱推送 SMTP / Gmail SMTP / Outlook SMTP
//   - 支持明文 / LOGIN / PLAIN auth
//   - 支持 SSL/TLS(465) / STARTTLS(587)
//
// 配置示例:
//
//	smtp://user:pass@smtp.example.com:465
//
// YAML:
//
//	email:
//	  smtp:
//	    host: smtp.example.com
//	    port: 465
//	    username: noreply@example.com
//	    password: ${SMTP_PASSWORD}
//	    from: noreply@example.com
//	    use_tls: true     # 465 走 SSL
//	    use_starttls: false # 587 走 STARTTLS
package emailx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTPConfig SMTP 配置
type SMTPConfig struct {
	Host        string // smtp.example.com
	Port        int    // 465 (SSL) / 587 (STARTTLS) / 25 (明文,慎用)
	Username    string // 认证用户名
	Password    string // 认证密码
	From        string // 发件邮箱(显示在 From:)
	UseTLS      bool   // 直接 TLS(465)
	UseSTARTTLS bool   // STARTTLS(587)
	// Timeout 网络/认证超时(默认 30s)
	Timeout time.Duration
}

// SMTPSender 通用 SMTP 邮件发送器
type SMTPSender struct {
	cfg SMTPConfig
}

// NewSMTP 创建 SMTP sender
func NewSMTP(cfg SMTPConfig) (*SMTPSender, error) {
	if cfg.Host == "" {
		return nil, errors.New("smtp: host 不能为空")
	}
	if cfg.Port == 0 {
		cfg.Port = 25
	}
	if cfg.From == "" {
		cfg.From = cfg.Username
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &SMTPSender{cfg: cfg}, nil
}

// Provider 标识
func (s *SMTPSender) Provider() string { return "smtp" }

// Send 发邮件(走标准库 net/smtp)
//
//	1. 构造 mime 报文
//	2. dial + auth
//	3. 写 From / To + Data
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if err := msg.IsValid(); err != nil {
		return err
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))

	// ctx 取消 + 超时双重保护
	dialer := &dialCtx{addr: addr, timeout: s.cfg.Timeout}
	conn, err := dialer.dial()
	if err != nil {
		return &EmailError{Provider: "smtp", Code: "dial", Message: err.Error(), Retry: true}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(s.cfg.Timeout))

	// UseTLS(465 隐式 TLS):连接建立后立刻升级 TLS,再开始 SMTP 会话。
	// 与 STARTTLS 区分:STARTTLS 是明文连上后按服务器能力可选升级(下面分支),
	// 465 则服务器从第一个字节起就在等 TLS ClientHello,不包装必然握手协议错乱。
	if s.cfg.UseTLS {
		conn = tls.Client(conn, &tls.Config{ServerName: s.cfg.Host})
	}

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return &EmailError{Provider: "smtp", Code: "client", Message: err.Error(), Retry: true}
	}
	defer c.Close()

	// STARTTLS
	if s.cfg.UseSTARTTLS && !s.cfg.UseTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			cfg := &tls.Config{ServerName: s.cfg.Host}
			if err := c.StartTLS(cfg); err != nil {
				return &EmailError{Provider: "smtp", Code: "starttls", Message: err.Error(), Retry: false}
			}
		}
	}

	// Auth
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return &EmailError{Provider: "smtp", Code: "auth", Message: err.Error(), Retry: false}
			}
		}
	}

	// From / To
	if err := c.Mail(s.cfg.From); err != nil {
		return &EmailError{Provider: "smtp", Code: "mail", Message: err.Error(), Retry: false}
	}
	for _, to := range msg.To {
		if err := c.Rcpt(to); err != nil {
			return &EmailError{Provider: "smtp", Code: "rcpt", Message: err.Error(), Retry: false}
		}
	}
	for _, cc := range msg.CC {
		if err := c.Rcpt(cc); err != nil {
			return &EmailError{Provider: "smtp", Code: "rcpt-cc", Message: err.Error(), Retry: false}
		}
	}
	for _, bcc := range msg.BCC {
		if err := c.Rcpt(bcc); err != nil {
			return &EmailError{Provider: "smtp", Code: "rcpt-bcc", Message: err.Error(), Retry: false}
		}
	}

	// Data
	w, err := c.Data()
	if err != nil {
		return &EmailError{Provider: "smtp", Code: "data", Message: err.Error(), Retry: false}
	}
	body := buildMIME(s.cfg.From, msg)
	if _, err := w.Write(body); err != nil {
		_ = w.Close()
		return &EmailError{Provider: "smtp", Code: "write", Message: err.Error(), Retry: true}
	}
	if err := w.Close(); err != nil {
		return &EmailError{Provider: "smtp", Code: "close", Message: err.Error(), Retry: true}
	}
	// QUIT 失败 ≠ 未送达:走到这里 DATA 已被服务器接受(250),返回错误会让上层
	// 重试,造成重复投递。所以 QUIT 的错误忽略,连接由 defer c.Close() 收尾。
	_ = c.Quit()
	return nil
}

// dialCtx 简单 ctx-aware dial(标准库 net/smtp 不支持 ctx)
type dialCtx struct {
	addr    string
	timeout time.Duration
}

func (d *dialCtx) dial() (net.Conn, error) {
	// 用 DialTimeout 替代;ctx 取消要靠后续 conn.SetDeadline
	conn, err := net.DialTimeout("tcp", d.addr, d.timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", d.addr, err)
	}
	return conn, nil
}

// stripCRLF 去掉文本里的 \r 和 \n。
// 邮件头按 RFC 5322 遇到换行即开新头,不 strip 的话业务文本里混入的换行
// 就是现成的 CRLF 注入入口(可注入 Bcc 等任意头)。
func stripCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "\n", "")
}

// encodeHeaderText 对可能含非 ASCII 的文本头做 RFC 2047 encoded-word 编码
// (邮件头按 RFC 5322 是 7-bit ASCII,裸 UTF-8 中文会被拒收或显示乱码)。
// 纯 ASCII 时 QEncoding 原样返回,不会多余地包 =?utf-8?...?=。
func encodeHeaderText(s string) string {
	return mime.QEncoding.Encode("utf-8", stripCRLF(s))
}

// buildMIME 构造 RFC 5322 + multipart/alternative 邮件
//
//	Headers:
//	  From / To / Cc / Subject / MIME-Version / Date / Message-ID
//	Body:
//	  multipart/alternative { text/plain, text/html }
func buildMIME(from string, msg Message) []byte {
	var b strings.Builder
	fromHeader := from
	if msg.FromName != "" {
		fromHeader = encodeHeaderText(msg.FromName) + " <" + from + ">"
	}
	b.WriteString("From: " + fromHeader + "\r\n")
	b.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	if len(msg.CC) > 0 {
		b.WriteString("Cc: " + strings.Join(msg.CC, ", ") + "\r\n")
	}
	if msg.ReplyTo != "" {
		// 回复地址是 addr-spec,不能做 RFC 2047 编码(地址部分必须裸 ASCII),只 strip CRLF
		b.WriteString("Reply-To: " + stripCRLF(msg.ReplyTo) + "\r\n")
	}
	b.WriteString("Subject: " + encodeHeaderText(msg.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + time.Now().Format("20060102T150405.000") + "@rapid-admin>\r\n")
	// 额外 headers(key 是头名不能编码,value 可能是任意文本,同样 strip 防注入)
	for k, v := range msg.Headers {
		b.WriteString(stripCRLF(k) + ": " + stripCRLF(v) + "\r\n")
	}

	boundary := "=_Mail_" + time.Now().Format("20060102150405")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("\r\n")

	// text/plain part
	if msg.Text != "" {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		// 头里声明了 quoted-printable 就必须真编码:URL 里的 "?page=20" 不转义,
		// 收件端解码时 "=20" 会被还原成空格,链接语义被破坏
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		writeQuotedPrintable(&b, msg.Text)
	}
	// text/html part
	if msg.HTML != "" {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		writeQuotedPrintable(&b, msg.HTML)
	}
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// writeQuotedPrintable 把 s 按 quoted-printable 编码写入 b
// ("=" 转义为 "=3D",非 ASCII 字节转义为 "=XX",声明与实际编码保持一致)
func writeQuotedPrintable(b *strings.Builder, s string) {
	qw := quotedprintable.NewWriter(b)
	// strings.Builder 的 Write 永不返回错误,这里错误实际不可能发生
	_, _ = qw.Write([]byte(s))
	_ = qw.Close()
	// QP writer 不补尾换行,不补的话正文会和下一行 boundary 粘在一起,
	// boundary 不在行首按 RFC 2046 就不再是边界
	b.WriteString("\r\n")
}
