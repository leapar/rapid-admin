package eventx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────── Bus:同步发布 / 订阅 ───────────

func TestM173_Bus_SubscribePublish(t *testing.T) {
	bus := NewBus()
	got := make(chan Event, 1)
	bus.Subscribe("approval.todo", func(_ context.Context, ev Event) {
		got <- ev
	})

	bus.Publish(context.Background(), Event{Name: "approval.todo", Payload: "p"})
	select {
	case ev := <-got:
		assert.Equal(t, Name("approval.todo"), ev.Name)
		assert.Equal(t, "p", ev.Payload)
	case <-time.After(time.Second):
		t.Fatal("订阅方没收到事件")
	}
}

func TestM173_Bus_Unsubscribe(t *testing.T) {
	bus := NewBus()
	calls := 0
	unsub := bus.Subscribe("e", func(context.Context, Event) { calls++ })

	bus.Publish(context.Background(), Event{Name: "e"})
	assert.Equal(t, 1, calls)

	unsub()
	unsub() // 幂等,不 panic
	bus.Publish(context.Background(), Event{Name: "e"})
	assert.Equal(t, 1, calls, "取消订阅后不应再收到事件")
}

func TestM173_Bus_OtherNamesNotNotified(t *testing.T) {
	bus := NewBus()
	calls := 0
	bus.Subscribe("a", func(context.Context, Event) { calls++ })
	bus.Publish(context.Background(), Event{Name: "b"})
	assert.Equal(t, 0, calls, "订阅 a 不该收到 b 的事件")
}

// ⚠️ 单个处理器 panic 被隔离:其余处理器照常跑,Publish 不外抛。
func TestM173_Bus_HandlerPanicIsolated(t *testing.T) {
	bus := NewBus()
	survived := make(chan struct{}, 1)
	bus.Subscribe("e", func(context.Context, Event) { panic("处理器故意 panic") })
	bus.Subscribe("e", func(context.Context, Event) { survived <- struct{}{} })

	assert.NotPanics(t, func() {
		bus.Publish(context.Background(), Event{Name: "e"})
	})
	select {
	case <-survived:
	default:
		t.Fatal("panic 之后的处理器没被调用 —— 隔离把整批都拦了")
	}
}

// ─────────── Bus / Async:异步发布的三件套 ───────────

// 🔴 ctx 已取消,处理器拿到的必须是活的(WithoutCancel 的核心断言)。
// 与 M88 publisher 的「SurvivesCancelledContext」同一课:只能直接测,
// 端到端跑不出来(ctx 根本不会死,恒绿假绿)。
func TestM173_Bus_PublishAsync_SurvivesCancelledContext(t *testing.T) {
	bus := NewBus()
	ctxErrs := make(chan error, 1)
	bus.Subscribe("e", func(ctx context.Context, _ Event) { ctxErrs <- ctx.Err() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bus.PublishAsync(ctx, Event{Name: "e"})

	select {
	case err := <-ctxErrs:
		assert.NoError(t, err, "处理器拿到的 ctx 必须是活的")
	case <-time.After(2 * time.Second):
		t.Fatal("异步发布没跑处理器")
	}
}

// 🔴 处理器 panic 不能打死进程(Async 的 recover),其余处理器照常执行。
func TestM173_Bus_PublishAsync_HandlerPanicIsolated(t *testing.T) {
	bus := NewBus()
	survived := make(chan struct{}, 1)
	bus.Subscribe("e", func(context.Context, Event) { panic("异步处理器故意 panic") })
	bus.Subscribe("e", func(context.Context, Event) { survived <- struct{}{} })

	assert.NotPanics(t, func() {
		bus.PublishAsync(context.Background(), Event{Name: "e"})
	})
	select {
	case <-survived:
	case <-time.After(2 * time.Second):
		t.Fatal("panic 之后的处理器没被调用")
	}
}

func TestM173_Bus_PublishAsync_NoSubscribers(t *testing.T) {
	// 没有订阅方 = 纯 no-op,不该起 goroutine,更不该 panic
	assert.NotPanics(t, func() {
		NewBus().PublishAsync(context.Background(), Event{Name: "nobody.listens"})
	})
}

// Async 本体:三件套的唯一实现。拆掉任何一件,这里的用例就该红(见探针)。
func TestM173_Async_FnRunsWithLiveContext(t *testing.T) {
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	Async(ctx, "probe", func(ctx context.Context) { done <- ctx.Err() })
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Async 没 run fn")
	}
}

func TestM173_Async_PanicIsolated(t *testing.T) {
	done := make(chan struct{})
	assert.NotPanics(t, func() {
		Async(context.Background(), "probe", func(context.Context) {
			panic("Async 故意 panic")
		})
		close(done)
	})
	<-done
	// 真正的断言是"测试进程没死" —— recover 失效的话整个包都会崩
}

// ─────────── 并发冒烟:发布与订阅并发不炸 ───────────

func TestM173_Bus_ConcurrentPublishSubscribe(t *testing.T) {
	bus := NewBus()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			unsub := bus.Subscribe(Name("e"+string(rune('a'+i%3))), func(context.Context, Event) {})
			bus.Publish(context.Background(), Event{Name: Name("e" + string(rune('a'+i%3)))})
			unsub()
		}
	}()
	for i := 0; i < 200; i++ {
		bus.Publish(context.Background(), Event{Name: "eb", Payload: i})
	}
	close(stop)
	wg.Wait()
	require.True(t, true)
}
