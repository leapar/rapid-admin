// emailx 单测(§1.9 #9 P2)
package emailx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"strings"
	"testing"
)

func validMsg() Message {
	return Message{
		From:    "noreply@example.com",
		To:      []string{"user@example.com"},
		Subject: "Test",
		HTML:    "<h1>Hi</h1>",
		Text:    "Hi",
	}
}

func TestMessage_IsValid(t *testing.T) {
	cases := []struct {
		name    string
		mut     func(m *Message)
		wantErr bool
	}{
		{"valid", func(m *Message) {}, false},
		{"no_from", func(m *Message) { m.From = "" }, true},
		{"no_to", func(m *Message) { m.To = nil }, true},
		{"no_subject", func(m *Message) { m.Subject = "" }, true},
		{"no_body", func(m *Message) { m.HTML = ""; m.Text = "" }, true},
		{"only_html_ok", func(m *Message) { m.Text = "" }, false},
		{"only_text_ok", func(m *Message) { m.HTML = "" }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := validMsg()
			c.mut(&m)
			err := m.IsValid()
			if (err != nil) != c.wantErr {
				t.Errorf("IsValid err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}

func TestConsoleSender_Send(t *testing.T) {
	var buf bytes.Buffer
	s := &ConsoleSender{Out: &buf}
	err := s.Send(context.Background(), validMsg())
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "noreply@example.com") {
		t.Errorf("missing From in output: %s", out)
	}
	if !strings.Contains(out, "Test") {
		t.Errorf("missing Subject in output: %s", out)
	}
	if !strings.Contains(out, "user@example.com") {
		t.Errorf("missing To in output: %s", out)
	}
}

func TestConsoleSender_Invalid(t *testing.T) {
	s := NewConsole()
	if err := s.Send(context.Background(), Message{Subject: "x"}); err == nil {
		t.Error("expected invalid err, got nil")
	}
}

func TestMockSender_Record(t *testing.T) {
	m := NewMock()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := m.Send(ctx, validMsg()); err != nil {
			t.Fatalf("Send err: %v", err)
		}
	}
	if m.Count() != 3 {
		t.Errorf("Count = %d, want 3", m.Count())
	}
	last, ok := m.Last()
	if !ok || last.Subject != "Test" {
		t.Errorf("Last Subject = %q", last.Subject)
	}
	m.Reset()
	if m.Count() != 0 {
		t.Errorf("after Reset Count = %d, want 0", m.Count())
	}
}

func TestMockSender_FailOn(t *testing.T) {
	m := NewMock()
	m.FailOn = errors.New("forced fail")
	if err := m.Send(context.Background(), validMsg()); err == nil {
		t.Error("expected err, got nil")
	}
	if m.Count() != 0 {
		t.Errorf("Count after fail = %d, want 0", m.Count())
	}
}

func TestNewSMTP_Validation(t *testing.T) {
	// 缺 host
	if _, err := NewSMTP(SMTPConfig{}); err == nil {
		t.Error("expected host required")
	}
	// port 默认 25
	s, err := NewSMTP(SMTPConfig{Host: "smtp.example.com"})
	if err != nil {
		t.Fatalf("NewSMTP err: %v", err)
	}
	if s.cfg.Port != 25 {
		t.Errorf("default port = %d, want 25", s.cfg.Port)
	}
	// From 默认 Username
	s2, _ := NewSMTP(SMTPConfig{Host: "smtp.example.com", Username: "u@x.com"})
	if s2.cfg.From != "u@x.com" {
		t.Errorf("From = %q, want u@x.com", s2.cfg.From)
	}
}

func TestNewAliyun_Validation(t *testing.T) {
	// 缺 access_key
	if _, err := NewAliyun(AliyunConfig{}); err == nil {
		t.Error("expected access_key required")
	}
	// 缺 account_name
	if _, err := NewAliyun(AliyunConfig{AccessKey: "k", AccessSecret: "s"}); err == nil {
		t.Error("expected account_name required")
	}
	// 完整
	s, err := NewAliyun(AliyunConfig{
		AccessKey: "k", AccessSecret: "s", AccountName: "noreply@x.com",
	})
	if err != nil {
		t.Fatalf("NewAliyun err: %v", err)
	}
	if s.cfg.Region != "cn-hangzhou" {
		t.Errorf("Region = %q, want cn-hangzhou", s.cfg.Region)
	}
	if s.cfg.AddressType != 1 {
		t.Errorf("AddressType = %d, want 1", s.cfg.AddressType)
	}
}

func TestAliyun_PercentEncode(t *testing.T) {
	cases := []struct{ in, out string }{
		{"abc", "abc"},
		{"a b", "a%20b"},
		{"a+b", "a%2Bb"},
		{"中", "%E4%B8%AD"},
		{"a.b-c_d~e", "a.b-c_d~e"},
	}
	for _, c := range cases {
		got := percentEncode(c.in)
		if got != c.out {
			t.Errorf("percentEncode(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestBuildMIME(t *testing.T) {
	msg := validMsg()
	out := buildMIME("noreply@example.com", msg)
	s := string(out)
	for _, want := range []string{
		"From: noreply@example.com",
		"To: user@example.com",
		"Subject: Test",
		"MIME-Version: 1.0",
		"multipart/alternative",
		"text/plain",
		"text/html",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("MIME missing %q in:\n%s", want, s)
		}
	}
}

func TestBuildMIME_CC(t *testing.T) {
	m := validMsg()
	m.CC = []string{"cc1@x.com", "cc2@x.com"}
	out := string(buildMIME("noreply@example.com", m))
	if !strings.Contains(out, "Cc: cc1@x.com, cc2@x.com") {
		t.Errorf("CC header missing: %s", out)
	}
}

func TestRandNonce_Length(t *testing.T) {
	for i := 0; i < 100; i++ {
		if len(randNonce()) != 32 {
			t.Errorf("randNonce length != 32")
			return
		}
	}
}

func TestSMTPSender_InvalidMsg(t *testing.T) {
	s, _ := NewSMTP(SMTPConfig{Host: "smtp.example.com"})
	if err := s.Send(context.Background(), Message{Subject: "x"}); err == nil {
		t.Error("expected invalid err, got nil")
	}
}

func TestAliyunSender_InvalidMsg(t *testing.T) {
	s, _ := NewAliyun(AliyunConfig{AccessKey: "k", AccessSecret: "s", AccountName: "n@x.com"})
	if err := s.Send(context.Background(), Message{Subject: "x"}); err == nil {
		t.Error("expected invalid err, got nil")
	}
}

func TestEmailError_Error(t *testing.T) {
	e := &EmailError{Provider: "aliyun", Code: "InvalidArgument", Message: "ToAddress invalid"}
	if e.Error() != "aliyun: InvalidArgument: ToAddress invalid" {
		t.Errorf("Error() = %q", e.Error())
	}
}

// mimeHeader 取 raw 报文里指定头的值(不含续行折叠,够测试用)
func mimeHeader(t *testing.T, raw, key string) string {
	t.Helper()
	for _, line := range strings.Split(raw, "\r\n") {
		if strings.HasPrefix(line, key+": ") {
			return strings.TrimPrefix(line, key+": ")
		}
	}
	t.Fatalf("header %q not found in:\n%s", key, raw)
	return ""
}

// mimePartBody 取指定 content-type part 的正文(QP 编码后的原始形态)
func mimePartBody(t *testing.T, raw, contentType string) string {
	t.Helper()
	i := strings.Index(raw, "Content-Type: "+contentType)
	if i < 0 {
		t.Fatalf("part %q not found in:\n%s", contentType, raw)
	}
	rest := raw[i:]
	j := strings.Index(rest, "\r\n\r\n")
	if j < 0 {
		t.Fatalf("part %q header/body separator not found", contentType)
	}
	body := rest[j+4:]
	if k := strings.Index(body, "\r\n--"); k >= 0 {
		body = body[:k]
	}
	return body
}

func TestBuildMIME_ChineseSubjectRFC2047(t *testing.T) {
	m := validMsg()
	m.Subject = "验证码订单通知"
	raw := string(buildMIME("noreply@example.com", m))

	subject := mimeHeader(t, raw, "Subject")
	// 非 ASCII 头必须包成 encoded-word,不能裸 UTF-8
	if !strings.HasPrefix(subject, "=?utf-8?") {
		t.Errorf("Subject not RFC2047 encoded: %q", subject)
	}
	if strings.Contains(raw, "验证码") {
		t.Errorf("raw UTF-8 subject leaked into headers:\n%s", raw)
	}
	// 收端解码还原原义
	got, err := new(mime.WordDecoder).DecodeHeader(subject)
	if err != nil {
		t.Fatalf("DecodeHeader err: %v", err)
	}
	if got != "验证码订单通知" {
		t.Errorf("decoded subject = %q, want %q", got, "验证码订单通知")
	}
}

func TestBuildMIME_ASCIISubjectNotWrapped(t *testing.T) {
	// 纯 ASCII 时 QEncoding 原样返回,不该多包 =?utf-8?...?=
	m := validMsg()
	raw := string(buildMIME("noreply@example.com", m))
	if got := mimeHeader(t, raw, "Subject"); got != "Test" {
		t.Errorf("Subject = %q, want %q", got, "Test")
	}
}

func TestBuildMIME_FromNameRFC2047(t *testing.T) {
	m := validMsg()
	m.FromName = "平台客服"
	raw := string(buildMIME("noreply@example.com", m))
	from := mimeHeader(t, raw, "From")
	if !strings.HasPrefix(from, "=?utf-8?") {
		t.Errorf("FromName not RFC2047 encoded: %q", from)
	}
	// addr-spec 部分保持裸地址,不能被编码
	if !strings.HasSuffix(from, "<noreply@example.com>") {
		t.Errorf("From addr-spec broken: %q", from)
	}
}

func TestBuildMIME_HeaderInjectionStripped(t *testing.T) {
	m := validMsg()
	m.Subject = "hi\r\nBcc: victim@x.com"
	m.FromName = "名\nReply-To: evil@x.com\r"
	m.ReplyTo = "reply@x.com\r\nBcc: victim@x.com"
	m.Headers = map[string]string{"X-Clean": "safe\r\nBcc: victim@x.com"}
	raw := string(buildMIME("noreply@example.com", m))
	// CRLF 注入的本质 = 换行后新起一个头行;逐行断言不存在被注入的头
	for _, line := range strings.Split(raw, "\r\n") {
		for _, evil := range []string{"Bcc:", "X-Injected:", "Reply-To: evil@x.com"} {
			if strings.HasPrefix(line, evil) {
				t.Errorf("injected header line %q:\n%s", line, raw)
			}
		}
	}
	// 合法位置不受影响:ReplyTo 只 strip 不编码(注入片段被剥离换行后粘在本行,
	// 不再是独立头行),自定义头本体保留
	if got := mimeHeader(t, raw, "Reply-To"); got != "reply@x.comBcc: victim@x.com" {
		t.Errorf("Reply-To = %q, want stripped-glue value", got)
	}
	if !strings.Contains(raw, "X-Clean: safe") {
		t.Errorf("custom header lost after strip:\n%s", raw)
	}
	// Subject strip 后解码还原不再含换行
	sub, err := new(mime.WordDecoder).DecodeHeader(mimeHeader(t, raw, "Subject"))
	if err != nil {
		t.Fatalf("DecodeHeader err: %v", err)
	}
	if strings.ContainsAny(sub, "\r\n") {
		t.Errorf("Subject still contains CRLF after strip: %q", sub)
	}
}

func TestBuildMIME_QuotedPrintableBody(t *testing.T) {
	m := validMsg()
	m.Text = "?page=20"
	m.HTML = `<a href="/x?page=20&next=30">y</a>`
	raw := string(buildMIME("noreply@example.com", m))

	// "=" 必须转义为 =3D,否则收件端解码时 "=20" 变空格
	if !strings.Contains(raw, "?page=3D20") {
		t.Errorf("body not quoted-printable escaped:\n%s", raw)
	}
	// 收端解码还原原义
	for _, part := range []struct{ ctype, want string }{
		{"text/plain", "?page=20"},
		{"text/html", `<a href="/x?page=20&next=30">y</a>`},
	} {
		body := mimePartBody(t, raw, part.ctype)
		got, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
		if err != nil {
			t.Fatalf("decode %s err: %v", part.ctype, err)
		}
		if string(got) != part.want {
			t.Errorf("decoded %s = %q, want %q", part.ctype, got, part.want)
		}
	}
}

func TestBuildMIME_QuotedPrintableChineseBody(t *testing.T) {
	// 中文正文也走 QP,非 ASCII 字节按 =XX 转义
	m := validMsg()
	m.Text = "你好"
	m.HTML = ""
	raw := string(buildMIME("noreply@example.com", m))
	body := mimePartBody(t, raw, "text/plain")
	if strings.Contains(body, "你好") {
		t.Errorf("raw UTF-8 body leaked despite QP declaration:\n%s", body)
	}
	got, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("decode err: %v", err)
	}
	if string(got) != "你好" {
		t.Errorf("decoded body = %q, want %q", got, "你好")
	}
}
