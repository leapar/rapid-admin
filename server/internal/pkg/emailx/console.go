// Console / Mock sender —— §1.9 #9 P2
//
// dev 模式用:控制台打印 / 测试断言
package emailx

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// ConsoleSender 控制台打印(只发到 stdout)
type ConsoleSender struct {
	Out io.Writer // 默认 os.Stdout
}

// NewConsole 创建控制台 sender
func NewConsole() *ConsoleSender {
	return &ConsoleSender{Out: os.Stdout}
}

// Provider 标识
func (c *ConsoleSender) Provider() string { return "console" }

// Send 控制台打印
func (c *ConsoleSender) Send(ctx context.Context, msg Message) error {
	if err := msg.IsValid(); err != nil {
		return err
	}
	fmt.Fprintf(c.Out, "[email/console] %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(c.Out, "  From:    %s <%s>\n", msg.FromName, msg.From)
	fmt.Fprintf(c.Out, "  To:      %v\n", msg.To)
	if len(msg.CC) > 0 {
		fmt.Fprintf(c.Out, "  Cc:      %v\n", msg.CC)
	}
	if len(msg.BCC) > 0 {
		fmt.Fprintf(c.Out, "  Bcc:     %v\n", msg.BCC)
	}
	fmt.Fprintf(c.Out, "  Subject: %s\n", msg.Subject)
	if msg.Text != "" {
		fmt.Fprintf(c.Out, "  Text:    %s\n", truncate(msg.Text, 200))
	}
	if msg.HTML != "" {
		fmt.Fprintf(c.Out, "  HTML:    %s\n", truncate(msg.HTML, 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// MockSender 测试用 mock(记录所有 send 调用,可选断言)
type MockSender struct {
	mu       sync.Mutex
	Sent     []Message
	FailOn   error // 设置后 Send 返此错误
}

// NewMock 创建 mock sender
func NewMock() *MockSender {
	return &MockSender{}
}

// Provider 标识
func (m *MockSender) Provider() string { return "mock" }

// Send 记录 + 可选失败
func (m *MockSender) Send(ctx context.Context, msg Message) error {
	if m.FailOn != nil {
		return m.FailOn
	}
	m.mu.Lock()
	m.Sent = append(m.Sent, msg)
	m.mu.Unlock()
	return nil
}

// Reset 清空
func (m *MockSender) Reset() {
	m.mu.Lock()
	m.Sent = nil
	m.FailOn = nil
	m.mu.Unlock()
}

// Count 已发送数
func (m *MockSender) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Sent)
}

// Last 最近一封
func (m *MockSender) Last() (Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Sent) == 0 {
		return Message{}, false
	}
	return m.Sent[len(m.Sent)-1], true
}
