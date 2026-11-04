// Mock push sender —— §1.9 #10 P2
package pushx

import (
	"context"
	"sync"
)

// MockSender 测试用 mock
type MockSender struct {
	mu     sync.Mutex
	Sent   []Notification
	FailOn error
}

// NewMock 创建 mock
func NewMock() *MockSender {
	return &MockSender{}
}

// Provider 标识
func (m *MockSender) Provider() string { return "mock" }

// Push 记录 + 可选失败
func (m *MockSender) Push(ctx context.Context, n Notification) error {
	if m.FailOn != nil {
		return m.FailOn
	}
	if err := n.IsValid(); err != nil {
		return err
	}
	m.mu.Lock()
	m.Sent = append(m.Sent, n)
	m.mu.Unlock()
	return nil
}

// Count 已 push 数
func (m *MockSender) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Sent)
}

// Last 最近一条
func (m *MockSender) Last() (Notification, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Sent) == 0 {
		return Notification{}, false
	}
	return m.Sent[len(m.Sent)-1], true
}

// Reset 清空
func (m *MockSender) Reset() {
	m.mu.Lock()
	m.Sent = nil
	m.FailOn = nil
	m.mu.Unlock()
}
