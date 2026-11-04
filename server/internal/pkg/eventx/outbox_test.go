package eventx

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// newOutboxDB 每个用例一个独立内存库 + 建表。
//
// ⚠️ 连接池限 1 条 —— 与 service 夹具同一课:shared-cache 的 sqlite
// 两个连接并发写同一张表直接 SQLITE_LOCKED。
func newOutboxDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.SysEventOutbox{}))
	return db
}

// countRows 数物理行(含软删 —— 软删作用域的 Count 会把软删行藏起来,骗过断言)。
func countRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Unscoped().Model(&model.SysEventOutbox{}).Count(&n).Error)
	return n
}

// countLive 数有效行(默认作用域,软删的看不见)。
func countLive(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&model.SysEventOutbox{}).Count(&n).Error)
	return n
}

func TestM173_Enqueue_InsertsPendingRow(t *testing.T) {
	db := newOutboxDB(t)

	id, err := Enqueue(context.Background(), db, OutboxMessage{
		EventName: "approval.todo",
		Topic:     "approval",
		Payload: struct {
			GroupCode string `json:"group_code"`
		}{GroupCode: "x"},
	})
	require.NoError(t, err)
	assert.NotZero(t, id, "落表成功必须拿到行 id(投递/去重靠它)")

	var row model.SysEventOutbox
	require.NoError(t, db.First(&row, id).Error)
	assert.Equal(t, "approval.todo", row.EventName)
	assert.Equal(t, "approval", row.Topic)
	assert.Equal(t, model.OutboxStatusPending, row.Status)
	assert.Zero(t, row.Attempts)
	assert.Nil(t, row.NextRetryAt)
	assert.Nil(t, row.IdemKey)
	// struct 载荷被 marshal 成 JSON
	var p map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.Payload), &p))
	assert.Equal(t, "x", p["group_code"])
}

// RawMessage 载荷原样存储 —— 这是"线字节兼容"的落表一半。
func TestM173_Enqueue_RawMessageStoredVerbatim(t *testing.T) {
	db := newOutboxDB(t)
	raw := json.RawMessage(`{"todo_id":"7","kind":"todo"}`)
	_, err := Enqueue(context.Background(), db, OutboxMessage{EventName: "e", Topic: "t", Payload: raw})
	require.NoError(t, err)

	var row model.SysEventOutbox
	require.NoError(t, db.First(&row).Error)
	assert.JSONEq(t, string(raw), row.Payload)
	assert.Equal(t, string(raw), row.Payload, "必须是逐字节原样,不是重排过键序的 JSON")
}

func TestM173_Enqueue_NilDB(t *testing.T) {
	_, err := Enqueue(context.Background(), nil, OutboxMessage{EventName: "e"})
	require.ErrorIs(t, err, ErrNoDB)
}

// 幂等键:同一 key 只落一行,第二次返回 ErrDuplicate(不算失败)。
func TestM173_Enqueue_IdemKeyDedups(t *testing.T) {
	db := newOutboxDB(t)

	id1, err := Enqueue(context.Background(), db, OutboxMessage{EventName: "e", Topic: "t", IdemKey: "k1"})
	require.NoError(t, err)

	_, err = Enqueue(context.Background(), db, OutboxMessage{EventName: "e", Topic: "t", IdemKey: "k1"})
	require.ErrorIs(t, err, ErrDuplicate)
	assert.Equal(t, int64(1), countRows(t, db), "同幂等键只落一行")

	_, err = Enqueue(context.Background(), db, OutboxMessage{EventName: "e", Topic: "t", IdemKey: "k2"})
	require.NoError(t, err)

	// 不给幂等键:每次都是新事件(多行 NULL 不冲突)
	for i := 0; i < 3; i++ {
		_, err = Enqueue(context.Background(), db, OutboxMessage{EventName: "e", Topic: "t"})
		require.NoError(t, err)
	}
	assert.Equal(t, int64(5), countRows(t, db))
	_ = id1
}

// 🔴 事务性核心:把业务事务回滚掉,事件必须跟着一起消失 ——
// 这就是 outbox 与"裸 goroutine 直推"的本质区别。
func TestM173_Enqueue_TxRollbackTakesEventWithIt(t *testing.T) {
	db := newOutboxDB(t)

	// ① 事务内落表 + 回滚 ⇒ 事件没发生
	tx := db.Begin()
	_, err := Enqueue(context.Background(), tx, OutboxMessage{EventName: "e", Topic: "t"})
	require.NoError(t, err)
	require.NoError(t, tx.Rollback().Error)
	assert.Equal(t, int64(0), countRows(t, db), "业务回滚,事件必须一起回滚")

	// ② 事务内落表 + commit ⇒ 事件在库里,等投递器
	tx2 := db.Begin()
	_, err = Enqueue(context.Background(), tx2, OutboxMessage{EventName: "e2", Topic: "t"})
	require.NoError(t, err)
	require.NoError(t, tx2.Commit().Error)
	assert.Equal(t, int64(1), countRows(t, db))
}
