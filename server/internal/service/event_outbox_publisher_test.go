// 领域事件 outbox 接缝单测(M173)
//
// ⚠️ 这里测的是「publisher ↔ outbox ↔ 投递器」的接缝,不是 eventx 原语
// (那边在 internal/pkg/eventx 自己的包里测)。核心断言有两条:
//
// ①线字节兼容:outbox 通道发出去的 payload 字节,与直推通道(M88/M94 原版
// 行为)逐字节相同 —— topic / event 名 / payload 字段一个都不能漂,前端
// 角标依赖「广播后重拉 total」,语义变了它会错。
//
// ②落表失败不丢事件:DB 抖动时退回直推,行为与无 outbox 部署一致,且
// 永不阻断业务(PublishApproval / PublishDictChanged 不返回错误)。
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/eventx"
	"rapid-admin/internal/pkg/push"
)

// outboxRows 数 outbox 行(默认作用域)。
func outboxRows(t *testing.T, db *gorm.DB) []model.SysEventOutbox {
	t.Helper()
	var rows []model.SysEventOutbox
	require.NoError(t, db.Order("id").Find(&rows).Error)
	return rows
}

// ─────────── ① outbox 通道:落表形状 ───────────

func TestM173_ApprovalPublisherOutbox_EnqueuesRow(t *testing.T) {
	db := newTestDB(t)
	p := NewApprovalPublisherWithOutbox(db)

	p.PublishApproval(context.Background(), ApprovalEvent{
		InstanceID: 1234567890123456789, Action: EvApprovalAssigned, AssigneeID: 99,
	})

	rows := outboxRows(t, db)
	require.Len(t, rows, 1, "落表是同步的,断言不需要等 goroutine")
	assert.Equal(t, push.EventApprovalTodo, rows[0].EventName)
	assert.Equal(t, push.TopicApproval, rows[0].Topic)
	assert.Equal(t, model.OutboxStatusPending, rows[0].Status)

	var payload push.ApprovalPayload
	require.NoError(t, json.Unmarshal([]byte(rows[0].Payload), &payload))
	assert.Equal(t, "1234567890123456789", payload.TodoID)
	assert.Equal(t, "assigned", payload.Action)
	assert.Equal(t, "99", payload.AssigneeID)
}

func TestM173_DictPublisherOutbox_EnqueuesRow(t *testing.T) {
	db := newTestDB(t)
	p := NewDictPublisherWithOutbox(db)

	p.PublishDictChanged(context.Background(), "issue_status")

	rows := outboxRows(t, db)
	require.Len(t, rows, 1)
	assert.Equal(t, push.EventDictChanged, rows[0].EventName)
	assert.Equal(t, push.TopicDict, rows[0].Topic)

	var payload push.DictChangedPayload
	require.NoError(t, json.Unmarshal([]byte(rows[0].Payload), &payload))
	assert.Equal(t, "issue_status", payload.GroupCode)
}

// ⚠️ 契约守护:空 group_code 不发(与直推通道同一条规则)。
func TestM173_DictPublisherOutbox_EmptyGroupCodeDropped(t *testing.T) {
	db := newTestDB(t)
	p := NewDictPublisherWithOutbox(db)
	p.PublishDictChanged(context.Background(), "")
	assert.Len(t, outboxRows(t, db), 0, "空 group_code 不落表")
}

// ⚠️ 契约守护:未知动作不落表(与直推通道同一条规则)。
func TestM173_ApprovalPublisherOutbox_UnknownActionDropped(t *testing.T) {
	db := newTestDB(t)
	p := NewApprovalPublisherWithOutbox(db)
	p.PublishApproval(context.Background(), ApprovalEvent{InstanceID: 1, Action: "not_real"})
	assert.Len(t, outboxRows(t, db), 0, "未知动作不落表")
}

// ─────────── ① 线字节兼容:outbox 通道 = 直推通道 ───────────

// 🔴 两条通道的 topic / event 名 / payload 字节必须完全一致 ——
// 前端 useEventStream 按名字订阅、按字段重拉,漂了就是静默坏掉。
func TestM173_PublisherOutbox_WireBytesEqualDirectPath(t *testing.T) {
	// ⚠️ 两条腿各用一次 newTestDB —— 它按 t.Name() 建 DSN,所以放在子测试里:
	// 同一个测试里调两次会拿到同一个 shared-cache 内存库。
	approval := func(t *testing.T) {
		ctx := context.Background()
		ev := ApprovalEvent{InstanceID: 42, Action: EvApprovalAssigned, AssigneeID: 7}

		tr := newCaptureTransport()
		withPushTransport(t, tr)
		NewApprovalPublisher().PublishApproval(ctx, ev)
		tr.wait(t)
		directEvents, _, _ := tr.snapshot()
		require.Len(t, directEvents, 1)
		directPayload, err := json.Marshal(directEvents[0].Payload)
		require.NoError(t, err)

		db := newTestDB(t)
		NewApprovalPublisherWithOutbox(db).PublishApproval(ctx, ev)
		rows := outboxRows(t, db)
		require.Len(t, rows, 1)
		assert.Equal(t, push.TopicApproval, rows[0].Topic)
		assert.Equal(t, push.EventApprovalTodo, rows[0].EventName)
		assert.Equal(t, string(directPayload), rows[0].Payload,
			"outbox 通道的 payload 必须与直推通道逐字节相同")
	}
	dict := func(t *testing.T) {
		ctx := context.Background()
		tr := newCaptureTransport()
		withPushTransport(t, tr)
		NewDictPublisher().PublishDictChanged(ctx, "issue_status")
		tr.wait(t)
		directEvents, _, _ := tr.snapshot()
		require.Len(t, directEvents, 1)
		directPayload, err := json.Marshal(directEvents[0].Payload)
		require.NoError(t, err)

		db := newTestDB(t)
		NewDictPublisherWithOutbox(db).PublishDictChanged(ctx, "issue_status")
		rows := outboxRows(t, db)
		require.Len(t, rows, 1)
		assert.Equal(t, push.TopicDict, rows[0].Topic)
		assert.Equal(t, push.EventDictChanged, rows[0].EventName)
		assert.Equal(t, string(directPayload), rows[0].Payload,
			"outbox 通道的 payload 必须与直推通道逐字节相同")
	}
	t.Run("approval", approval)
	t.Run("dict", dict)
}

// ─────────── ② 落表失败:退回直推,不阻断业务 ───────────

// 🔴 DB 抖动(这里用"没有 outbox 表的库"模拟)→ 事件退回直推,
// 语义与无 outbox 部署完全一致 —— 不能静默丢,更不能 panic。
func TestM173_ApprovalPublisherOutbox_EnqueueFailureFallsBackToDirectPush(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	// 一张表都没有的库:Enqueue 必然失败
	bare, err := gorm.Open(sqlite.Open("file:"+t.Name()+"bare?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := bare.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	p := NewApprovalPublisherWithOutbox(bare)
	assert.NotPanics(t, func() {
		p.PublishApproval(context.Background(), ApprovalEvent{InstanceID: 1, Action: EvApprovalApproved})
	})
	tr.wait(t)
	events, _, _ := tr.snapshot()
	require.Len(t, events, 1, "落表失败必须退回直推,事件不能丢")
	assert.Equal(t, push.EventApprovalDone, events[0].Event)
}

// ─────────── 端到端:publisher → outbox → 投递器 → push 通道 ───────────

// 完整走一遍生产链路(投递函数与 main.go 的装配同构):
// 落表的两条事件被投递器推出,行标记 delivered,通道收到的字节与直推一致。
func TestM173_Deliverer_DeliversPublisherRowsEndToEnd(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	NewApprovalPublisherWithOutbox(db).PublishApproval(ctx, ApprovalEvent{
		InstanceID: 42, Action: EvApprovalApproved,
	})
	NewDictPublisherWithOutbox(db).PublishDictChanged(ctx, "issue_status")
	require.Len(t, outboxRows(t, db), 2)

	tr := newCaptureTransport()
	d := eventx.NewDeliverer(db, func(dctx context.Context, m eventx.Message) error {
		// 与 main.go 的 deliver 函数同构(RawMessage 原样透传 + 补 At)
		return tr.Publish(dctx, []string{m.Topic}, push.Event{
			Topic:   m.Topic,
			Event:   m.EventName,
			Payload: json.RawMessage(m.Payload),
			At:      1,
		})
	}, eventx.DelivererConfig{BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	n, err := d.Tick(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	events, topics, _ := tr.snapshot()
	require.Len(t, events, 2)
	assert.ElementsMatch(t, []string{push.TopicApproval, push.TopicDict}, topics)
	byTopic := map[string]push.Event{}
	for _, e := range events {
		byTopic[e.Topic] = e
	}
	// payload 字段与前端契约对齐(todo_id 是 string,不是裸数字 —— JS 2^53)
	var ap push.ApprovalPayload
	require.NoError(t, json.Unmarshal([]byte(byTopic[push.TopicApproval].Payload.(json.RawMessage)), &ap))
	assert.Equal(t, "42", ap.TodoID)
	assert.Equal(t, "approved", ap.Action)

	var dp push.DictChangedPayload
	require.NoError(t, json.Unmarshal([]byte(byTopic[push.TopicDict].Payload.(json.RawMessage)), &dp))
	assert.Equal(t, "issue_status", dp.GroupCode)

	// 行被标记 delivered
	for _, row := range outboxRows(t, db) {
		assert.Equal(t, model.OutboxStatusDelivered, row.Status)
	}
}
