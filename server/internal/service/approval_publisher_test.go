// 审批事件生产者单测(M88)
//
// 文档:docs/通用审批系统设计方案V1.0.md §9.2
//
// ⚠️ **这里测的是"接缝",不是引擎** —— 引擎发什么事件在 M84 的
// `approval_engine_test.go` 里已经断言过了。这里断言的是:
// ① 动作 → 事件名的映射对不对;
// ② 投递出去的 payload 是不是白名单(没夹带 comment 之类);
// ③ **拿一个已取消的 ctx 进去,transport 收到的 ctx 仍然是活的**;
// ④ transport panic 不会打死进程。
//
// ⚠️ **③ 必须是"直接测 publish",不能走端到端** —— §9.2 明写:
// 「正因为原理由是假的,端到端断言测不出这条 …… M84 的做法是直接测
// publish:传一个已取消的 ctx 进去,断言 publisher 拿到的是活的。
// **别改回端到端断言**,那条是假绿。」
package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/push"
)

// ─────────── fake Transport ───────────

// captureTransport 抓投递事件 + 记下 transport 拿到的 ctx
type captureTransport struct {
	mu       sync.Mutex
	events   []push.Event
	topics   []string
	ctxErr   []error
	panicOn  bool
	notified chan struct{}
}

func newCaptureTransport() *captureTransport {
	return &captureTransport{notified: make(chan struct{}, 8)}
}

func (c *captureTransport) Available() bool { return true }

func (c *captureTransport) Publish(ctx context.Context, topics []string, ev push.Event) error {
	c.mu.Lock()
	c.events = append(c.events, ev)
	c.topics = append(c.topics, topics...)
	c.ctxErr = append(c.ctxErr, ctx.Err())
	panicking := c.panicOn
	c.mu.Unlock()
	if panicking {
		// ⚠️ 故意 panic —— 验证 recover 把它隔离开了。
		// 没有 recover 的话,这个 panic 会**打死整个测试进程**,
		// 测试会以"整个包崩了"的形式失败,而不是一条干净的断言失败。
		panic("transport 故意 panic")
	}
	select {
	case c.notified <- struct{}{}:
	default:
	}
	return nil
}

func (c *captureTransport) Subscribe(context.Context, []string) (<-chan push.Event, func(), error) {
	ch := make(chan push.Event)
	return ch, func() {}, nil
}

func (c *captureTransport) wait(t *testing.T) {
	t.Helper()
	select {
	case <-c.notified:
	case <-time.After(2 * time.Second):
		t.Fatal("等待事件投递超时 —— goroutine 没跑?")
	}
}

func (c *captureTransport) snapshot() ([]push.Event, []string, []error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]push.Event(nil), c.events...),
		append([]string(nil), c.topics...),
		append([]error(nil), c.ctxErr...)
}

// withPushTransport 临时替换 push 包的全局 transport
func withPushTransport(t *testing.T, tr push.Transport) {
	t.Helper()
	push.Init(tr)
	t.Cleanup(func() { push.Init(nil) })
}

// ─────────── ① 动作 → 事件名 的映射 ───────────

func TestM88_Publisher_ActionToEvent(t *testing.T) {
	cases := []struct {
		action   string
		wantEvt  string
		wantKind string
	}{
		{EvApprovalAssigned, push.EventApprovalTodo, "todo"},
		{EvApprovalApproved, push.EventApprovalDone, "todo"},
		{EvApprovalRejected, push.EventApprovalDone, "todo"},
		{EvApprovalRolledBack, push.EventApprovalDone, "todo"},
		{EvApprovalCancelled, push.EventApprovalDone, "todo"},
	}
	for _, c := range cases {
		evt, payload, ok := buildApprovalEvent(ApprovalEvent{InstanceID: 7, Action: c.action})
		require.True(t, ok, "动作 %q 应当被认得", c.action)
		assert.Equal(t, c.wantEvt, evt, "动作 %q 的事件名", c.action)
		assert.Equal(t, c.wantKind, payload.Kind)
		assert.Equal(t, "7", payload.TodoID, "todo_id 就是 instance_id(§9.2)")
		assert.Equal(t, c.action, payload.Action)
	}
}

func TestM88_Publisher_UnknownActionDropped(t *testing.T) {
	// ⚠️ 未知动作必须**被丢掉并报错**,不是塞进事件里发出去 ——
	// 前端拿到一个不认识的 action,只能原样忽略,现场没有任何线索。
	_, _, ok := buildApprovalEvent(ApprovalEvent{InstanceID: 1, Action: "not_a_real_action"})
	assert.False(t, ok, "未知动作不能被映射成事件")
}

func TestM88_Publisher_AssigneeOnlyOnTodo(t *testing.T) {
	// §16.2:只有"新待办"才带 assignee_id
	_, p1, _ := buildApprovalEvent(ApprovalEvent{InstanceID: 1, Action: EvApprovalAssigned, AssigneeID: 42})
	assert.Equal(t, "42", p1.AssigneeID)

	_, p2, _ := buildApprovalEvent(ApprovalEvent{InstanceID: 1, Action: EvApprovalApproved, AssigneeID: 42})
	assert.Empty(t, p2.AssigneeID, "审批动作不是发给某个人的,不该带 assignee_id")
}

// ─────────── ② 端到端投递 + ③ ctx 必须活着 ───────────

func TestM88_Publisher_PublishesToApprovalTopic(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	p := NewApprovalPublisher()
	p.PublishApproval(context.Background(), ApprovalEvent{
		InstanceID: 1234567890123456789,
		Action:     EvApprovalAssigned,
		AssigneeID: 99,
	})
	tr.wait(t)

	events, topics, _ := tr.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, push.TopicApproval, events[0].Topic)
	assert.Equal(t, push.EventApprovalTodo, events[0].Event)
	assert.Contains(t, topics, push.TopicApproval)

	payload, ok := events[0].Payload.(push.ApprovalPayload)
	require.True(t, ok, "payload 必须是 push.ApprovalPayload")
	assert.Equal(t, "1234567890123456789", payload.TodoID)
	assert.Equal(t, "99", payload.AssigneeID)
	assert.Equal(t, "assigned", payload.Action)
}

// 🔴 **ctx 已取消也必须投出去** —— §9.2 的核心断言。
//
// 这一条**只能直接测 publish**:端到端跑一遍 Act 时 ctx 根本不会死,
// 那条断言恒绿,是假绿。
func TestM88_Publisher_SurvivesCancelledContext(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟"HTTP 请求已超时 / 上游已断开"

	p := NewApprovalPublisher()
	p.PublishApproval(ctx, ApprovalEvent{InstanceID: 1, Action: EvApprovalApproved})
	tr.wait(t)

	events, _, ctxErrs := tr.snapshot()
	require.Len(t, events, 1, "ctx 死了也必须把事件投出去")
	require.Len(t, ctxErrs, 1)
	assert.NoError(t, ctxErrs[0],
		"transport 拿到的 ctx 必须是活的 —— 拿已取消的 ctx 写 Redis 会直接失败,"+
			"症状是「审批成功了但前端不刷新」")
}

func TestM88_Publisher_TransportPanicIsolated(t *testing.T) {
	tr := newCaptureTransport()
	tr.panicOn = true
	withPushTransport(t, tr)

	p := NewApprovalPublisher()
	// ⚠️ 这一条如果 recover 没生效,整个测试进程会被 panic 打死 ——
	// 那正是我们要验的行为:一条推送事件不该有能力把审批服务带走。
	p.PublishApproval(context.Background(), ApprovalEvent{InstanceID: 1, Action: EvApprovalApproved})

	// 给 goroutine 一点时间跑完(它会 panic → 被 recover)
	time.Sleep(200 * time.Millisecond)
	events, _, _ := tr.snapshot()
	assert.Len(t, events, 1, "panic 之前事件已经进了 transport")
}

func TestM88_Publisher_NoTransportIsNoop(t *testing.T) {
	// push 没 Init(Redis 没连)时,push.Publish 内部记 Debug 后丢弃 ——
	// **不能 panic**,也不能让审批主流程失败。
	push.Init(nil)
	p := NewApprovalPublisher()
	assert.NotPanics(t, func() {
		p.PublishApproval(context.Background(), ApprovalEvent{InstanceID: 1, Action: EvApprovalApproved})
	})
	time.Sleep(100 * time.Millisecond)
}
