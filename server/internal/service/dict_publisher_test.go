// 字典事件生产者单测(M94)
//
// ⚠️ **这里测的是"接缝",不是字典管理页** —— 字典改了会发什么事件,
// 在 `role_dict_test.go` 里断言(那边有真库)。
// 这里断言的是"接缝"的三件事:
// ① 事件名 / topic / payload 形状对不对;
// ② **拿一个已取消的 ctx 进去,transport 收到的 ctx 仍然是活的**;
// ③ transport panic 不会打死进程。
//
// ⚠️ **② 必须是"直接测 publish",不能走端到端** —— 端到端跑一遍
// UpdateDict 时 ctx 根本不会死,那条断言恒绿,是假绿。
// (与 `approval_publisher_test.go` 同一条纪律,照抄。)
//
// ⚠️ **helper 复用 `approval_publisher_test.go` 里的 captureTransport** ——
// 同一个包,再抄一份会编译不过(重复定义)。
package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/push"
)

// ─────────── ① 事件形状 ───────────

func TestM94_Publisher_PublishesToDictTopic(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	p := NewDictPublisher()
	p.PublishDictChanged(context.Background(), "issue_status")
	tr.wait(t)

	events, topics, _ := tr.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, push.TopicDict, events[0].Topic)
	assert.Equal(t, push.EventDictChanged, events[0].Event)
	assert.Contains(t, topics, push.TopicDict)

	payload, ok := events[0].Payload.(push.DictChangedPayload)
	require.True(t, ok, "payload 必须是 push.DictChangedPayload")
	assert.Equal(t, "issue_status", payload.GroupCode)
}

// ⚠️ **payload 白名单**:事件只带 group_code,不带字典内容。
//
// 带内容等于造出第二份数据契约 —— 后端改了字典项的字段,前端会拿着
// 旧 payload 里的字段渲染,而且**两边会漂**。信号只说"变了"。
func TestM94_Publisher_PayloadCarriesNoDictContent(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	p := NewDictPublisher()
	p.PublishDictChanged(context.Background(), "issue_status")
	tr.wait(t)

	events, _, _ := tr.snapshot()
	require.Len(t, events, 1)
	payload, ok := events[0].Payload.(push.DictChangedPayload)
	require.True(t, ok)
	// 结构体只有一个字段 —— 这条断言的价值在于:有人加了 label 字段进来,
	// 这里立刻红(而不是等到某天发现前端在用广播里的 label)。
	assert.Equal(t, 1, reflect.TypeOf(payload).NumField(),
		"DictChangedPayload 只允许带 group_code 一个字段")
}

// ⚠️ **空 group_code 不发** —— 前端拿到它不知道该作废哪个组。
// 数据库里 group_code 是 NOT NULL,理论上不会是空;
// 这里挡一道是为了不让"上游传了个空串"变成"前端全量重拉"。
func TestM94_Publisher_EmptyGroupCodeDropped(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	p := NewDictPublisher()
	p.PublishDictChanged(context.Background(), "")

	// 给 goroutine 一点时间(如果它该发的话)
	time.Sleep(200 * time.Millisecond)
	events, _, _ := tr.snapshot()
	assert.Len(t, events, 0, "空 group_code 不该发出事件")
}

// 🔴 **ctx 已取消也必须投出去** —— 与 M88 审批 publisher 同一课。
func TestM94_Publisher_SurvivesCancelledContext(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟"HTTP 请求已超时 / 上游已断开"

	p := NewDictPublisher()
	p.PublishDictChanged(ctx, "issue_status")
	tr.wait(t)

	events, _, ctxErrs := tr.snapshot()
	require.Len(t, events, 1, "ctx 死了也必须把事件投出去")
	require.Len(t, ctxErrs, 1)
	assert.NoError(t, ctxErrs[0],
		"transport 拿到的 ctx 必须是活的 —— 拿已取消的 ctx 写 Redis 会直接失败,"+
			"症状是「字典改了但前端不刷新」")
}

func TestM94_Publisher_TransportPanicIsolated(t *testing.T) {
	tr := newCaptureTransport()
	tr.panicOn = true
	withPushTransport(t, tr)

	p := NewDictPublisher()
	// ⚠️ 这一条如果 recover 没生效,整个测试进程会被 panic 打死 ——
	// 那正是我们要验的行为:一条推送事件不该有能力把字典服务带走。
	p.PublishDictChanged(context.Background(), "issue_status")

	time.Sleep(200 * time.Millisecond)
	events, _, _ := tr.snapshot()
	assert.Len(t, events, 1, "panic 之前事件已经进了 transport")
}
