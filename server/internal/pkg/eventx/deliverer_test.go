package eventx

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// ─────────── 夹具 ───────────

// newDelivererFixture 建库(newOutboxDB,见 outbox_test.go)+ 插 n 条
// pending 行,返回 db 和行 id 列表。
func newDelivererFixture(t *testing.T, n int) (*gorm.DB, []uint64) {
	t.Helper()
	db := newOutboxDB(t)
	ids := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		id, err := Enqueue(context.Background(), db, OutboxMessage{
			EventName: "e", Topic: "topic", Payload: map[string]int{"i": i},
		})
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return db, ids
}

// ─────────── Tick:成功路径 ───────────

func TestM173_Deliverer_TickDeliversDueRows(t *testing.T) {
	db, ids := newDelivererFixture(t, 2)

	var got []Message
	var mu sync.Mutex
	d := NewDeliverer(db, func(_ context.Context, m Message) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, m)
		return nil
	}, DelivererConfig{BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	n, err := d.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	require.Len(t, got, 2)
	assert.Equal(t, "e", got[0].EventName)
	assert.Equal(t, "topic", got[0].Topic)
	assert.Zero(t, got[0].Attempts)
	assert.NotZero(t, got[0].ID)
	assert.NotZero(t, got[0].Payload)

	// 行被标记 delivered
	for _, id := range ids {
		var row model.SysEventOutbox
		require.NoError(t, db.First(&row, id).Error)
		assert.Equal(t, model.OutboxStatusDelivered, row.Status)
	}
}

func TestM173_Deliverer_FutureRetryNotDue(t *testing.T) {
	db, ids := newDelivererFixture(t, 1)
	future := time.Now().Add(time.Hour)
	require.NoError(t, db.Model(&model.SysEventOutbox{}).Where("id = ?", ids[0]).
		Update("next_retry_at", future).Error)

	d := NewDeliverer(db, func(context.Context, Message) error { return nil }, DelivererConfig{})
	n, err := d.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "退避未到期的行不该被投")

	var row model.SysEventOutbox
	require.NoError(t, db.First(&row, ids[0]).Error)
	assert.Equal(t, model.OutboxStatusPending, row.Status)
}

// ─────────── Tick:失败 / 重试 / 死信 ───────────

func TestM173_Deliverer_FailureBacksOff(t *testing.T) {
	db, ids := newDelivererFixture(t, 1)
	base := 50 * time.Millisecond
	d := NewDeliverer(db, func(context.Context, Message) error {
		return errors.New("redis 抖了")
	}, DelivererConfig{BaseDelay: base, MaxDelay: time.Second})

	n, err := d.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	var row model.SysEventOutbox
	require.NoError(t, db.First(&row, ids[0]).Error)
	assert.Equal(t, model.OutboxStatusPending, row.Status, "失败仍是 pending(等下次重试)")
	assert.Equal(t, 1, row.Attempts)
	assert.NotNil(t, row.NextRetryAt)
	assert.True(t, time.Until(*row.NextRetryAt) > 0, "next_retry_at 必须在未来")
	assert.Contains(t, row.LastError, "redis 抖了")

	// 立即再扫一轮:退避没到,不该重投
	n, err = d.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestM173_Deliverer_DeadAfterMaxAttempts(t *testing.T) {
	db, ids := newDelivererFixture(t, 1)
	// 把 attempts 顶到上限-1:这次失败就是最后一次
	require.NoError(t, db.Model(&model.SysEventOutbox{}).Where("id = ?", ids[0]).
		Update("attempts", 7).Error)

	d := NewDeliverer(db, func(context.Context, Message) error {
		return errors.New("一直失败")
	}, DelivererConfig{MaxAttempts: 8, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	_, err := d.Tick(context.Background())
	require.NoError(t, err)

	var row model.SysEventOutbox
	require.NoError(t, db.First(&row, ids[0]).Error)
	assert.Equal(t, model.OutboxStatusDead, row.Status, "重试耗尽必须进死信")
	assert.Equal(t, 8, row.Attempts)
	assert.Contains(t, row.LastError, "一直失败")

	// 死信行不再被投
	calls := 0
	d2 := NewDeliverer(db, func(context.Context, Message) error { calls++; return nil }, DelivererConfig{})
	_, err = d2.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, calls)
}

// 🔴 通道不可用 ≠ 消息有问题:退避,但不烧尝试次数。
// 拆掉这个特判的探针就是本用例(Redis 宕机把积压事件全部送进死信)。
func TestM173_Deliverer_TransportUnavailableDoesNotBurnAttempts(t *testing.T) {
	db, ids := newDelivererFixture(t, 1)
	d := NewDeliverer(db, func(context.Context, Message) error {
		return ErrTransportUnavailable
	}, DelivererConfig{BaseDelay: time.Hour, MaxDelay: time.Hour})

	_, err := d.Tick(context.Background())
	require.NoError(t, err)

	var row model.SysEventOutbox
	require.NoError(t, db.First(&row, ids[0]).Error)
	assert.Equal(t, model.OutboxStatusPending, row.Status)
	assert.Equal(t, 0, row.Attempts, "通道不可用不该消耗尝试次数")
	assert.NotNil(t, row.NextRetryAt, "但要退避")
}

// 🔴 投递实现 panic 不能打死轮询循环,按一次失败记账。
func TestM173_Deliverer_PanicIsolated(t *testing.T) {
	db, ids := newDelivererFixture(t, 1)
	d := NewDeliverer(db, func(context.Context, Message) error {
		panic("deliver 故意 panic")
	}, DelivererConfig{BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond, MaxAttempts: 8})

	assert.NotPanics(t, func() {
		_, _ = d.Tick(context.Background())
	})
	var row model.SysEventOutbox
	require.NoError(t, db.First(&row, ids[0]).Error)
	assert.Equal(t, 1, row.Attempts, "panic 记作一次失败")
	assert.Contains(t, row.LastError, "panic")
}

// 🔴 停机窗口不烧次数:父 ctx 取消导致的失败不算消息的错。
func TestM173_Deliverer_ShutdownDoesNotBurnAttempt(t *testing.T) {
	db, ids := newDelivererFixture(t, 1)

	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	d := NewDeliverer(db, func(fnCtx context.Context, _ Message) error {
		// 卡住直到父 ctx 取消 —— 模拟"投递进行中进程开始停机"
		<-fnCtx.Done()
		close(release)
		return fnCtx.Err()
	}, DelivererConfig{BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	done := make(chan struct{})
	go func() {
		_, _ = d.Tick(ctx)
		close(done)
	}()
	// 等 fn 真正进入阻塞,再取消父 ctx
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-release
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Tick 没有随 ctx 取消返回")
	}

	var row model.SysEventOutbox
	require.NoError(t, db.First(&row, ids[0]).Error)
	assert.Equal(t, 0, row.Attempts, "停机不算投递失败")
	assert.Equal(t, model.OutboxStatusPending, row.Status)
	assert.Nil(t, row.NextRetryAt, "下一轮立即可投")
}

// ─────────── 退避函数 ───────────

func TestM173_Deliverer_Backoff(t *testing.T) {
	base := 5 * time.Second
	max := time.Minute
	assert.Equal(t, 5*time.Second, backoff(base, max, 1))
	assert.Equal(t, 10*time.Second, backoff(base, max, 2))
	assert.Equal(t, 20*time.Second, backoff(base, max, 3))
	assert.Equal(t, 40*time.Second, backoff(base, max, 4))
	assert.Equal(t, time.Minute, backoff(base, max, 5), "封顶")
	assert.Equal(t, time.Minute, backoff(base, max, 100), "封顶不溢出")
	assert.Equal(t, 5*time.Second, backoff(base, max, 0), "非法入参按第 1 次算")
}

// ─────────── 清理 ───────────

func TestM173_Deliverer_CleansExpiredDeliveredRows(t *testing.T) {
	db, ids := newDelivererFixture(t, 2)
	// 行 1:delivered 且已超保留期 → 清
	require.NoError(t, db.Model(&model.SysEventOutbox{}).Where("id = ?", ids[0]).
		UpdateColumns(map[string]any{"status": model.OutboxStatusDelivered,
			"updated_at": time.Now().Add(-48 * time.Hour)}).Error)
	// 行 2:delivered 但没超期 → 留
	require.NoError(t, db.Model(&model.SysEventOutbox{}).Where("id = ?", ids[1]).
		Update("status", model.OutboxStatusDelivered).Error)
	// 行 3:dead 且超期 → **留**(死信是给人排查的)
	dead, err := Enqueue(context.Background(), db, OutboxMessage{EventName: "e", Topic: "t"})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.SysEventOutbox{}).Where("id = ?", dead).
		UpdateColumns(map[string]any{"status": model.OutboxStatusDead,
			"updated_at": time.Now().Add(-48 * time.Hour)}).Error)

	d := NewDeliverer(db, func(context.Context, Message) error { return nil },
		DelivererConfig{DeliveredRetention: 24 * time.Hour})
	_, err = d.Tick(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(2), countLive(t, db), "只清过期的已投递行(软删)")
	// ⚠️ dest 不能复用同一个已填充的 struct:GORM 会把它带的主键值拼进
	// WHERE(id=A AND id=B),查不到任何行还报 record not found
	var row model.SysEventOutbox
	require.NoError(t, db.First(&row, ids[1]).Error)
	assert.Equal(t, model.OutboxStatusDelivered, row.Status)
	row = model.SysEventOutbox{}
	require.NoError(t, db.First(&row, dead).Error)
	assert.Equal(t, model.OutboxStatusDead, row.Status, "死信不自动清理")
}

// ─────────── Run 循环 ───────────

func TestM173_Deliverer_RunStopsOnCancel(t *testing.T) {
	db, _ := newDelivererFixture(t, 0)
	d := NewDeliverer(db, func(context.Context, Message) error { return nil },
		DelivererConfig{Interval: 5 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run 没有随 ctx 取消退出")
	}
}

func TestM173_Deliverer_NilDB(t *testing.T) {
	d := NewDeliverer(nil, func(context.Context, Message) error { return nil }, DelivererConfig{})
	_, err := d.Tick(context.Background())
	require.ErrorIs(t, err, ErrNoDB)
}

func TestM173_Deliverer_NilFunc(t *testing.T) {
	db, _ := newDelivererFixture(t, 0)
	d := NewDeliverer(db, nil, DelivererConfig{})
	_, err := d.Tick(context.Background())
	require.Error(t, err)
}
