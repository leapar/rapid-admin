// push —— 服务端事件推送通道(SSE 的传输层地基)
//
// ## 这个包解决什么问题
//
// 后台要在权限变更、任务跑完、字典改了之后**立刻**刷新界面,而不是等用户
// 手点刷新。SSE(而不是 WebSocket)是因为它走普通 HTTP,浏览器自带断线重连,
// 而且是单向的 —— 这个场景本来就只需要服务端 → 客户端。
//
// ## 三条设计
//
//  1. **事件是信号,不是数据。** payload 里没有业务正文,前端收到后重新调
//     REST 拿最新状态。这样推送不扩大数据可见范围,也不会产生"REST DTO 一份、
//     推送 payload 一份"的双份契约漂移
//  2. **单路径投递。** Redis pub/sub 是唯一通道,没有"进程内 Hub + Redis"
//     双投递 —— 双投递会让同实例的连接收到两份事件(长列表页反复重拉),
//     而且这个 bug 只在单实例部署下出现,多实例部署恰好正常
//  3. **不静默。** Redis 不可用时前端会收到 degraded 事件切轮询;
//     推送消失但没人知道 ⇒ 对任务状态来说那意味着"页面永远不更新",
//     比报错更难查
//
// ## 关键点
//
//   - **Publish 永不返错**。推送是尽力而为的旁路,不该因为"推送没发出去"
//     就让主流程失败;失败只记 Warn 日志
//   - **Subscribe 在 Redis 不可用时返 nil channel,不返 error**。返回 error
//     会让前端看到"连接失败"然后无限重连 —— 而那永远也不会成功
//   - **慢消费者策略是"丢最旧"不是阻塞**。丢一条旧的代价是"这次刷新看到
//     稍旧的状态",下一个事件立刻纠正;阻塞整个订阅循环的代价是**该实例所有
//     SSE 连接都停摆**。两者差三个数量级
//   - **缓冲的最后一格永久留给 lag 事件**。否则数据事件把缓冲占满后,lag
//     永远挤不进去 ⇒ 前端不知道自己漏了 ⇒ 页面停在旧状态且无任何提示
//   - **Transport 是接口,不是具体类型**。将来换 WebSocket 时新增一个实现,
//     业务侧的 Publish 调用一行都不用改
//
// ## 跑法
//
//	go run ./examples/push
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"rapid-admin/internal/pkg/permnotify"
	"rapid-admin/internal/pkg/push"
)

func main() {
	ctx := context.Background()

	// ── 一、没注入传输层 ────────────────────────────────────────
	fmt.Println("=== 没注入传输层 ===")
	fmt.Printf("  Available() = %v\n", push.Available())
	push.Publish(ctx, []string{push.TopicPerm}, push.Event{Event: push.EventPermChanged})
	fmt.Println("  ⇒ Publish 静默丢弃(只记 Debug 日志),不返回错误")
	ch, cancel := push.Subscribe(ctx, []string{push.TopicPerm})
	fmt.Printf("  Subscribe 返回的 channel == nil ? %v\n", ch == nil)
	cancel()
	fmt.Println("  ⇒ 返 nil channel(不是 error)—— 前端据此发 degraded 事件切轮询")

	// ── 二、自定义 Transport(接口可替换)──────────────────────
	fmt.Println()
	fmt.Println("=== 自定义 Transport ===")
	mem := newMemTransport()
	push.Init(mem)
	fmt.Printf("  Available() = %v\n", push.Available())
	fmt.Println("  ⇒ 将来换 WebSocket 时新增一个实现即可,业务侧 Publish 调用一行不用改")

	// 订阅
	evCh, cancel2 := push.Subscribe(ctx, []string{push.TopicPerm, push.TopicJob})
	defer cancel2()

	// ── 三、permnotify:权限失效广播 ───────────────────────────
	fmt.Println()
	fmt.Println("=== permnotify.PublishInvalidateAll ===")
	permnotify.PublishInvalidateAll(ctx, 8801, "role_resource_changed")
	select {
	case ev := <-evCh:
		fmt.Printf("  收到事件:topic=%s event=%s payload=%+v\n", ev.Topic, ev.Event, ev.Payload)
	case <-time.After(time.Second):
		fmt.Println("  超时没收到事件")
	}
	fmt.Println("  ⇒ permnotify 只是把权限失效这件事包成一条 push 事件")
	fmt.Println("    它不自己持有 redis.Client —— 依赖方向是 permnotify → push")

	// ── 四、多 topic 一次投递 ─────────────────────────────────
	fmt.Println()
	fmt.Println("=== 多 topic ===")
	push.Publish(ctx, []string{push.TopicPerm, push.TopicJob}, push.Event{
		Topic:   push.TopicJob,
		Event:   push.EventJobFinished,
		Payload: push.JobPayload{JobKey: "framework.job_history_purge", Status: "success", DurationMs: 12},
	})
	for i := 0; i < 2; i++ {
		select {
		case ev := <-evCh:
			fmt.Printf("  收到:topic=%s event=%s payload=%+v\n", ev.Topic, ev.Event, ev.Payload)
		case <-time.After(time.Second):
			fmt.Println("  超时")
		}
	}
	fmt.Println("  ⇒ 一次投递,每个频道各一份,payload 完全相同")
	fmt.Println("    所以前端按 event 名分发,不按 topic —— 同一事件投多个频道时")
	fmt.Println("    收方无从分辨自己是从哪个频道收到的")

	// ── 五、TopicUser:只给本人的频道 ─────────────────────────
	fmt.Println()
	fmt.Println("=== TopicUser ===")
	fmt.Printf("  TopicUser(8801) = %s\n", push.TopicUser(8801))
	fmt.Println("  ⇒ 站内信未读、审批指派给我这类「只给本人」的事件走这个频道")

	// ── 六、真 Redis 传输层 ────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 真 Redis 传输层(miniredis)===")
	mr, err := miniredis.Run()
	if err != nil {
		fmt.Println("启动内存 Redis 失败:", err)
		return
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	push.Init(push.NewRedisTransport(rdb))
	fmt.Printf("  Available() = %v\n", push.Available())

	redisCh, cancel3 := push.Subscribe(ctx, []string{push.TopicDict})
	defer cancel3()
	// 等订阅确认落地,否则紧接着发布的事件会永久丢失
	// (Redis pub/sub 不补发,包内 ps.Receive 就是干这个的)
	time.Sleep(200 * time.Millisecond)

	push.Publish(ctx, []string{push.TopicDict}, push.Event{
		Event:   push.EventDictChanged,
		Payload: push.DictChangedPayload{GroupCode: "issue_status"},
	})
	select {
	case ev := <-redisCh:
		fmt.Printf("  收到:topic=%s event=%s payload=%+v\n", ev.Topic, ev.Event, ev.Payload)
	case <-time.After(2 * time.Second):
		fmt.Println("  超时没收到事件")
	}
	fmt.Println("  ⇒ payload 只带 group_code,不带字典内容 —— 收到的人自己去重拉")

	// ── 七、慢消费者:丢最旧 + lag 提示 ───────────────────────
	fmt.Println()
	fmt.Println("=== 慢消费者(第一轮:刚溢出)===")
	// 缓冲 64 格,连发 64 条但不读 —— 第 64 条正好把最旧的挤掉,
	// 于是缓冲 = 63 条数据 + 1 条 lag
	for i := 0; i < 64; i++ {
		push.Publish(ctx, []string{push.TopicDict}, push.Event{
			Event:   push.EventDictChanged,
			Payload: push.DictChangedPayload{GroupCode: fmt.Sprintf("g%d", i)},
		})
	}
	time.Sleep(500 * time.Millisecond)
	lagSeen, got := 0, 0
	for {
		select {
		case ev := <-redisCh:
			got++
			if ev.Event == push.EventLag {
				lagSeen++
				fmt.Printf("  收到 lag 事件:payload=%+v\n", ev.Payload)
			}
			continue
		default:
		}
		break
	}
	fmt.Printf("  缓冲里 %d 条,其中 lag %d 条\n", got, lagSeen)
	fmt.Println("  ⇒ 缓冲满 ⇒ 丢最旧的(不是丢最新:丢最新页面会永远停在最老状态)")
	fmt.Println("    并且必须补一条 lag 告诉前端「你漏了,去补拉」")

	fmt.Println()
	fmt.Println("=== 慢消费者(第二轮:持续过载)===")
	// 再连发 200 条,仍不读 —— 缓冲一直满着,队头不断轮换,
	// 那条 lag 迟早也被挤出去
	for i := 0; i < 200; i++ {
		push.Publish(ctx, []string{push.TopicDict}, push.Event{
			Event:   push.EventDictChanged,
			Payload: push.DictChangedPayload{GroupCode: fmt.Sprintf("g%d", i)},
		})
	}
	time.Sleep(500 * time.Millisecond)
	lagSeen, got = 0, 0
	for {
		select {
		case ev := <-redisCh:
			got++
			if ev.Event == push.EventLag {
				lagSeen++
			}
			continue
		default:
		}
		break
	}
	fmt.Printf("  缓冲里 %d 条,其中 lag %d 条\n", got, lagSeen)
	fmt.Println("  ⇒ 持续过载时 lag 自己也随队头轮换出队 —— 这是**有意接受**的:")
	fmt.Println("    前端已经落后到读什么都不对,再补一条「你漏了」毫无意义;")
	fmt.Println("    等它追上来、下一次再丢事件时 lag 会重新生成")
	fmt.Println("  ⇒ 所以前端不能只靠 lag:连接建立时前端就应当主动拉一次全量")
}

// memTransport 内存实现 —— 演示 Transport 接口可以自己换
type memTransport struct {
	mu     sync.Mutex
	subs   map[string][]chan push.Event
	closed bool
}

func newMemTransport() *memTransport {
	return &memTransport{subs: map[string][]chan push.Event{}}
}

func (t *memTransport) Available() bool { return !t.closed }

func (t *memTransport) Publish(ctx context.Context, topics []string, ev push.Event) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ev.At == 0 {
		ev.At = time.Now().UnixMilli()
	}
	for _, topic := range topics {
		for _, ch := range t.subs[topic] {
			select {
			case ch <- ev:
			default: // 满了就丢 —— 和 Redis 传输层同一个策略
			}
		}
	}
	return nil
}

func (t *memTransport) Subscribe(ctx context.Context, topics []string) (<-chan push.Event, func(), error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(chan push.Event, 16)
	for _, topic := range topics {
		t.subs[topic] = append(t.subs[topic], out)
	}
	return out, func() {}, nil
}
