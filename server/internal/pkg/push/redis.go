package push

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// connBuffer 每条连接的事件缓冲深度。
//
// 为什么需要缓冲:SSE writer(往 socket 写)比 Redis 投递慢,中间必须有个解耦,
// 否则一个网速慢的浏览器会把订阅 goroutine 卡住,而那个 goroutine 是**该实例
// 所有 SSE 连接共用一条 pub/sub 流** —— 一个慢客户端就能拖垮所有人。
const connBuffer = 64

// redisTransport Redis pub/sub 实现。**本包唯一在跑的 Transport**
type redisTransport struct {
	rdb *redis.Client
}

// NewRedisTransport 构造 Redis 传输层(cli 为 nil 时 Available() 返 false)
func NewRedisTransport(cli *redis.Client) Transport {
	return &redisTransport{rdb: cli}
}

func (t *redisTransport) Available() bool { return t.rdb != nil }

// Publish 往多个 topic 各发一份。
//
// 用 Pipeline 而不是循环 Publish:一次 RTT 发完所有频道,而且避免部分频道
// 成功部分失败的中间态(那种情况没法判断该不该重试)。
func (t *redisTransport) Publish(ctx context.Context, topics []string, ev Event) error {
	if t.rdb == nil {
		return ErrNoRedis
	}
	payload, err := marshalEvent(ev)
	if err != nil {
		return err
	}
	pipe := t.rdb.Pipeline()
	for _, topic := range topics {
		pipe.Publish(ctx, topic, payload)
	}
	_, err = pipe.Exec(ctx)
	return err
}

// Subscribe 订阅 topic,返回带缓冲的事件流。
//
// ⚠️ 慢消费者策略是**丢最旧**而不是阻塞。理由:事件是"该去拉数据了"的信号,
// 丢一条旧的代价是"这次刷新看到的是稍旧的状态",而下一个事件立刻就把状态
// 拉回来了;反过来,阻塞整个订阅循环的代价是**该实例所有连接都停摆**。
// 两者代价差三个数量级。
func (t *redisTransport) Subscribe(ctx context.Context, topics []string) (<-chan Event, func(), error) {
	if t.rdb == nil {
		return nil, func() {}, ErrNoRedis
	}

	ps := t.rdb.Subscribe(ctx, topics...)
	// 等订阅确认。跳过这步的话,Subscribe 返回后到 Redis 真正生效之间有一个
	// 窗口,这期间发布的事件会永久丢失(Redis pub/sub 不补发)
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, func() {}, err
	}

	out := pump(ctx, ps.Channel())
	cancel := func() { _ = ps.Close() }
	return out, cancel, nil
}

// pump 把原始消息流转成带缓冲的 Event 流,并负责慢消费者策略。
//
// ⚠️ 抽成独立函数是为了**可测**:真 Redis 的 Subscribe 在单测里造不出来(本仓不引
// miniredis,与 cachex 同款纪律)。背压是本包最容易写错的逻辑(丢错方向 = 变成阻塞 =
// 拖垮整个实例),却是唯一一条在不接 Redis 时测不到的 —— 必须留这个口子。
//
// 入参是 `<-chan *redis.Message` 而不是 `<-chan string`:虽然后者更"干净",
// 但 *redis.Message 只是个带 Payload 字段的普通 struct,单测里
// `&redis.Message{Payload: ...}` 直接造,**不需要 Redis 服务器**。
func pump(ctx context.Context, raw <-chan *redis.Message) <-chan Event {
	c := newConsumer()

	go func() {
		defer close(c.out)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-raw:
				if !ok {
					return
				}
				var ev Event
				if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
					// 单条消息解析失败不该拖垮整个连接(比如有人往频道里发了脏数据)
					logger.L().Debug("push: 事件解析失败,跳过",
						zap.String("payload", truncateForLog(msg.Payload)), zap.Error(err))
					continue
				}
				c.deliver(ev)
			}
		}
	}()

	return c.out
}

// consumer 单条连接的事件缓冲 + 慢消费者策略。
//
// ⚠️ **拆成独立类型不是为了好看,是为了可测 + 正确**。
//
// 正确性:背压策略和"告知前端丢了事件"两件事必须同时成立,而它们会互相打架。
// 如果数据事件能把缓冲占满,lag 事件就永远挤不进去 —— 于是"丢过事件"这个事实
// 永远发不出去,前端不知道自己漏了,页面停在旧状态且没有任何提示(包注释第 3 条
// "不静默"直接失效)。所以这里把 chan 的**最后一格永久留给 lag**:数据事件上限
// 恒为 connBuffer-1,任何时刻缓冲里至少有 1 格空位,lag 一定塞得进去。
//
// 可测性:deliver 是纯同步的,不依赖 goroutine 调度。原先这段逻辑内联在 pump 的
// select 里,要测「丢的是最旧的」就得靠 sleep 和运气,测试本身会假绿。
//
// ⚠️ 非线程安全:只有一个 goroutine(pump 的那个)会调用 deliver。
type consumer struct {
	out chan Event

	dataCap   int // 数据事件上限 = connBuffer-1,那之后的几格永远空着给 lag
	dropped   int64
	lagQueued bool // 有事件被丢过但 lag 还没挤出去
}

// newConsumer 造一条连接的缓冲
func newConsumer() *consumer {
	return &consumer{
		out:     make(chan Event, connBuffer),
		dataCap: connBuffer - 1,
	}
}

// deliver 把一条事件投递给消费者。**永不阻塞。**
//
// 满了就丢最旧的一条给新事件腾位置。
//
// ⚠️ **缓冲长度直接读 chan(len(c.out)),本类型不自己维护计数** ——
//
//	原实现有个 `n int` 字段,靠 deliver 里的 n++ / n-- 维护"缓冲里有多少条"。
//	但**消费端把事件读走时没有任何人会通知这个计数** —— 读走的那一端是
//	SSE handler / 浏览器,它们只管 `<-evs`,不会回调 consumer。
//	于是 n 只增不减:一个**跟得上**的消费端(正常情况!)在第 64 条事件上
//	就会走进"丢最旧"分支,而那时缓冲早被读空了 ——
//	`<-c.out` 于是**永久阻塞**,pump 的 goroutine 卡死,
//	这条 SSE 连接从此再收不到任何事件,而且**没有任何报错**。
//
//	症状极隐蔽:心跳还在发(那是 handler 另一个 goroutine 写的),
//	连接看起来是活的,但页面就是不再刷新。
//	心跳 25s 一跳、job 事件每分钟一条 ⇒ 一个开着后台的标签页几小时内必然踩到。
//
//	len(c.out) 是 chan 自己的长度,读走即减,永远是真值。
func (c *consumer) deliver(ev Event) {
	if len(c.out) >= c.dataCap {
		// 缓冲满 → 丢最旧的一条。
		// ⚠️ 丢最旧而不是丢最新:丢最新的等于让页面永远停在最老的状态,
		// 而那个状态再也等不到后续事件来纠正(表现为"怎么刷都是旧数据")。
		// 丢最旧的代价只是"少看了一次中间态",下一个事件立刻就纠正了。
		//
		// ⚠️ **必须是非阻塞接收**:判断(len)和接收之间,消费端可能刚好把缓冲
		// 读空了 —— 阻塞式 `<-c.out` 会把 pump 卡死,而那正是本函数要防的死锁。
		select {
		case <-c.out:
			c.dropped++
			c.lagQueued = true
		default:
			// 消费端刚好读空了 ⇒ 没有东西可丢,直接往下走
		}
	}

	// 走到这里 len(c.out) ≤ dataCap < cap(c.out),一定有空位
	c.out <- ev

	if c.lagQueued {
		// 缓冲里至多 dataCap+1 条,lag 塞得进去。
		// 仍留 default 兜底:万一将来有人改了 dataCap,这里是**静默丢 lag**
		// 而不是阻塞投递方 —— 宁可少一条提示,不能卡住整条链路。
		select {
		case c.out <- lagEvent(c.dropped):
			c.lagQueued = false
		default:
		}
	}
}

func lagEvent(n int64) Event {
	return Event{
		Topic:   "",
		Event:   EventLag,
		Payload: map[string]int64{"dropped": n},
		At:      time.Now().UnixMilli(),
	}
}

// truncateForLog 日志里截断 payload,避免把整个连接日志撑爆
func truncateForLog(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
