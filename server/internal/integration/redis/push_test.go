package redisint

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"rapid-admin/internal/pkg/push"
)

// push 的 Redis pub/sub 在真 Redis 上的往返。
//
// ⚠️ push 包自己**不引 miniredis**(与 cachex 同款纪律),
// 所以整条 pub/sub 链路以前只在"单进程 mock"层面测过:
// `pkg/push/push_test.go` 直接构造 `&redis.Message{}` 喂给 pump。
// 真 Redis 这条路上有三处 miniredis/mock 根本测不到:
//
//  1. **订阅确认** —— `Subscribe` 里 `ps.Receive(ctx)` 等的那一帧。
//     少了它,Subscribe 返回到订阅真正生效之间有个窗口,窗口里发的事件永久丢失
//     (Redis pub/sub 不补发)。
//  2. **真 Redis 的频道语义** —— 同一连接订阅多个频道,消息按频道分发。
//  3. **投递顺序与并发** —— Publisher 与 Subscriber 是两条连接。
func TestPush_真Redis上多频道分发(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx30(t)
	tr := push.NewRedisTransport(rdb)

	topics := []string{"push:test:a", "push:test:b"}
	ch, cancel, err := tr.Subscribe(ctx, topics)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(cancel)
	// 等订阅真正生效(Redis pub/sub 不补发,这里发晚了就永久丢)
	time.Sleep(300 * time.Millisecond)

	evA := push.Event{Topic: "a", Event: "job.finished", Payload: map[string]string{"n": "1"}, At: time.Now().UnixMilli()}
	evB := push.Event{Topic: "b", Event: "dict.changed", Payload: map[string]string{"n": "2"}, At: time.Now().UnixMilli()}

	// 一次 Publish 投两个频道(Pipeline 实现)
	if err := tr.Publish(ctx, topics, evA); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := tr.Publish(ctx, []string{"push:test:b"}, evB); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case ev := <-ch:
			if ev.Event == "" {
				t.Fatal("收到空事件")
			}
			if ev.Payload == nil {
				t.Fatal("payload 丢了")
			}
			seen[ev.Event] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("只收到 %d 条事件,期望 2 条(%v)", len(seen), seen)
		}
	}
	if !seen["job.finished"] || !seen["dict.changed"] {
		t.Fatalf("两个频道的事件没都收到:%v", seen)
	}
}

// 频道隔离:订阅了 a 就不该收到 b 的事件。
//
// ⚠️ 这是 Redis pub/sub 最基本的一条,但**从来没有被验证过** ——
// 真 Redis 里如果订阅命令没写对,消息会串频道,
// 而事件是"该去重拉数据"的信号,串频道会让前端重拉一堆不该拉的东西。
func TestPush_频道隔离(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx30(t)
	tr := push.NewRedisTransport(rdb)

	ch, cancel, err := tr.Subscribe(ctx, []string{"push:test:only"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(cancel)
	time.Sleep(300 * time.Millisecond)

	// 往别的频道发
	other := push.Event{Topic: "other", Event: "should.not.arrive", At: time.Now().UnixMilli()}
	if err := tr.Publish(ctx, []string{"push:test:other"}, other); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case ev := <-ch:
		t.Fatalf("收到了不该收到的频道的事件:%+v", ev)
	case <-time.After(500 * time.Millisecond):
		// 正确:没串频道
	}
}

// 跟得上的消费端在第 64 条事件上不该被卡死。
//
// ⚠️ **这是 f6d4225 修的那个真 bug 的回归网**(M-fix):
// 旧实现自己维护一个 `n int` 计数,消费端把事件读走时**没有任何人通知这个计数**
// ⇒ n 只增不减 ⇒ 跟得上的消费端在第 64 条事件上走进"丢最旧"分支,
// 而那时缓冲早被读空了 ⇒ 阻塞式 `<-c.out` **永久阻塞** ⇒
// pump 的 goroutine 卡死 ⇒ 这条 SSE 连接从此再收不到任何事件,而且没有任何报错。
//
// 真 Redis 才有意义:这条链路一次都没在真的 pub/sub 上跑过。
func TestPush_跟得上的消费端不被卡死(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx30(t)
	tr := push.NewRedisTransport(rdb)

	ch, cancel, err := tr.Subscribe(ctx, []string{"push:test:many"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(cancel)
	time.Sleep(300 * time.Millisecond)

	const total = 200
	var got atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < total; i++ {
			select {
			case <-ch:
				got.Add(1)
			case <-ctx.Done():
				return
			}
		}
	}()

	for i := 0; i < total; i++ {
		ev := push.Event{Topic: "many", Event: "tick", Payload: i, At: time.Now().UnixMilli()}
		if err := tr.Publish(ctx, []string{"push:test:many"}, ev); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		// 让消费端跟得上 —— 这正是旧 bug 的触发条件
		time.Sleep(time.Millisecond)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("消费端被卡死了:只收到 %d/%d 条事件。"+
			"旧 bug 是缓冲计数只增不减 ⇒ 第 64 条之后阻塞在接收上,永久收不到后续事件。",
			got.Load(), total)
	}
	if got.Load() != total {
		t.Fatalf("应收 %d 条,got %d", total, got.Load())
	}
}

// Publish 到没人订阅的频道:不能报错。
//
// 事件是"尽力而为的旁路",投递方是业务写完库的请求 ——
// 不该因为"没人听"就让主流程失败。
func TestPush_无人订阅时Publish不报错(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	tr := push.NewRedisTransport(rdb)

	ev := push.Event{Topic: "nobody", Event: "tick", At: time.Now().UnixMilli()}
	if err := tr.Publish(ctx, []string{"push:test:nobody-listens"}, ev); err != nil {
		t.Fatalf("无人订阅时 Publish 不该报错,got %v", err)
	}
}

// Redis 未注入时的降级语义。
//
// ⚠️ **两层的行为不一样,别混**:
//
//   - Transport 层(`NewRedisTransport(nil)`):Publish / Subscribe 都返
//     `ErrNoRedis`。这是"我确实没法干活"的诚实回答。
//   - 包级 `push.Subscribe`:把那个 error **吞掉**,返 nil channel ——
//     因为 /events handler 靠 nil channel 发 degraded 事件让前端切轮询;
//     把 error 往上抛会让前端无限重连,而那永远也不会成功。
func TestPush_无Redis时降级(t *testing.T) {
	tr := push.NewRedisTransport(nil)
	if tr.Available() {
		t.Fatal("rdb=nil 时 Available 应为 false")
	}
	if err := tr.Publish(context.Background(), []string{"x"}, push.Event{}); err == nil {
		t.Fatal("rdb=nil 时 Publish 应返 ErrNoRedis")
	}
	if _, _, err := tr.Subscribe(context.Background(), []string{"x"}); err == nil {
		t.Fatal("rdb=nil 时 Transport.Subscribe 应返 ErrNoRedis")
	}

	// 包级:注入一个没有 Redis 的传输层
	push.Init(tr)
	t.Cleanup(func() { push.Init(nil) })

	if push.Available() {
		t.Fatal("注入 rdb=nil 的传输层后 Available 应为 false")
	}
	ch, cancel := push.Subscribe(context.Background(), []string{"x"})
	if ch != nil {
		t.Fatal("包级 Subscribe 在 Redis 不可用时应返 nil channel(上层据此发 degraded)")
	}
	cancel()
}
