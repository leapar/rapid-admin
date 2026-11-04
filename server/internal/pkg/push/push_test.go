// pkg/push 单测 —— M71
//
// 分三块:
//   - consumer 的背压逻辑(丢最旧 / lag 必达 / 永不阻塞)
//     ← 本包最容易写错的一块。deliver 是纯同步的,所以这里**不用 sleep、
//     不用靠调度运气**,断言全部确定性
//   - pump 的 goroutine 外壳(透传 / ctx 取消关 channel / 脏数据跳过)
//   - Publish / Subscribe 的 Transport 契约 + topic/event 命名契约(fake Transport)
//
// 不连真 Redis,与本仓 cachex / cronx 同款纪律(不引 miniredis)。
// 代价:真实 Redis 的 pub/sub 语义(SUBSCRIBE 生效前的丢失窗口等)测不到,
// 已在 redis.go 的 Subscribe 注释里登记为盲区。
package push

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────── 脚手架 ───────────

// jobEv 造一条 job.finished 事件,用 DurationMs 区分先后
func jobEv(seq int64) Event {
	return Event{
		Topic:   TopicJob,
		Event:   EventJobFinished,
		Payload: JobPayload{JobKey: "k", DurationMs: seq},
	}
}

// rawOfEvent 把事件编码成一条 redis.Message
func rawOfEvent(t *testing.T, ev Event) *redis.Message {
	t.Helper()
	b, err := marshalEvent(ev)
	require.NoError(t, err)
	return &redis.Message{Payload: b}
}

// take 从缓冲里取一条,取不到就 fail(证明"应该塞得进去")
//
// ⚠️ 这里**不需要**维护任何计数 —— consumer 的缓冲长度直接读 chan 自己
// (len(c.out)),消费端读走即减。曾经这里有一行 `c.n--`,那是在替生产代码
// 擦屁股:生产代码里的 n 只增不减(没人通知它"事件被读走了"),
// 而这个 helper 每次读走都替它减 —— 于是单测永远绿,真机上第 64 条事件
// 就会把 pump 的 goroutine 永久卡死。见 C9。
func take(t *testing.T, c *consumer) Event {
	t.Helper()
	select {
	case ev := <-c.out:
		return ev
	default:
		t.Fatal("缓冲是空的,但本该有一条")
		return Event{}
	}
}

// peekLen 缓冲里当前的事件数
func peekLen(c *consumer) int { return len(c.out) }

// isLag 判断是不是 lag 事件
func isLag(ev Event) bool { return ev.Event == EventLag }

// durationOf 取事件序号。
//
// ⚠️ 必须同时兼容两种形态:直接 deliver 时 payload 是 JobPayload(结构体),
// 而走完 pump 的 JSON 往返之后它已经退化成 map[string]any —— 只判一种会
// 在另一个测试里 panic,而 panic 会把整个测试二进制带下去,掩盖掉真正的失败。
func durationOf(t *testing.T, ev Event) int64 {
	t.Helper()
	switch p := ev.Payload.(type) {
	case JobPayload:
		return p.DurationMs
	case map[string]any:
		v, ok := p["duration_ms"].(float64)
		require.True(t, ok, "duration_ms 字段类型异常:%T", p["duration_ms"])
		return int64(v)
	default:
		t.Fatalf("payload 形态异常:%T", ev.Payload)
		return 0
	}
}

// lagDropped 取 lag 里的 dropped 计数
func lagDropped(t *testing.T, ev Event) int64 {
	t.Helper()
	switch p := ev.Payload.(type) {
	case map[string]int64:
		return p["dropped"]
	case map[string]any:
		v, ok := p["dropped"].(float64)
		require.True(t, ok, "dropped 字段类型异常:%T", p["dropped"])
		return int64(v)
	default:
		t.Fatalf("lag payload 形态异常:%T", ev.Payload)
		return 0
	}
}

// drainAll 把缓冲里的事件全读走,返回其中 lag 的条数
func drainAll(t *testing.T, c *consumer) int {
	t.Helper()
	// ⚠️ 长度必须先取出来:take 会缩短缓冲,写成 `i < peekLen(c)` 会在
	// 读掉第一条后就退出 —— 一个静默只读一条的"全量"断言,比没有断言更坏。
	n := peekLen(c)
	lagCount := 0
	for i := 0; i < n; i++ {
		if isLag(take(t, c)) {
			lagCount++
		}
	}
	return lagCount
}

// assertInvariants 检查 consumer 的缓冲没撑破 chan 容量,且为 lag 留了空位。
//
// ⚠️ 这是防"后人改坏 dataCap"的护栏:deliver 里所有"一定不会阻塞"的推理
// 都建立在「缓冲长度 ≤ dataCap+1 ≤ cap(out)」这条不变量上,一旦破了,
// deliver 会静默死锁(而不是报错),而且只在真 Redis + 真慢客户端时才复现。
func assertInvariants(t *testing.T, c *consumer, round int) {
	t.Helper()
	require.LessOrEqual(t, len(c.out), cap(c.out),
		"第 %d 次投递后缓冲撑破了 chan 容量", round)
	require.LessOrEqual(t, len(c.out), c.dataCap+1,
		"第 %d 次投递后缓冲没有为 lag 留出空位,lag 会挤不进去", round)
}

// ─────────── consumer 背压(核心) ───────────

// C1 正常路径:顺序不变,一条不丢
func TestC1_Consumer_Passthrough(t *testing.T) {
	c := newConsumer()
	for i := 1; i <= 5; i++ {
		c.deliver(jobEv(int64(i)))
	}
	require.Equal(t, 5, peekLen(c))
	for i := 1; i <= 5; i++ {
		assert.Equal(t, int64(i), durationOf(t, take(t, c)))
	}
	assert.Equal(t, int64(0), c.dropped, "没满就不该丢任何事件")
}

// C2 **核心**:塞爆后丢的是**最旧的**
//
// 丢错方向(留旧的、丢新的)意味着前端永远停在最老的状态,而那个状态
// 再也等不到后续事件来纠正 —— 表现为"页面怎么刷都是旧数据"。
// 这条测试反过来锁死方向。
//
// ⚠️ 灌 **connBuffer** 条(而不是更多):再多灌,lag 会被后续的丢弃一路挤到队头
// 再挤掉(见 C5),测的就不是"丢最旧"这件事了。
func TestC2_Consumer_DropsOldestNotNewest(t *testing.T) {
	c := newConsumer()
	for i := 0; i < connBuffer; i++ {
		c.deliver(jobEv(int64(i)))
	}

	// 缓冲里 = dataCap 条数据 + 1 条 lag
	require.Equal(t, connBuffer, peekLen(c),
		"缓冲上限 %d 应该是 %d 条数据 + 1 条 lag", connBuffer, connBuffer-1)
	assert.Equal(t, int64(1), c.dropped, "第 %d 条正好把最旧的挤掉", connBuffer)

	// 留下的必须是**最新的** dataCap 条:seq ∈ [1, connBuffer)
	for seq := int64(1); seq < int64(connBuffer); seq++ {
		ev := take(t, c)
		require.False(t, isLag(ev), "seq=%d 那条不该是 lag", seq)
		assert.Equal(t, seq, durationOf(t, ev),
			"留下的必须是最新的一条,顺序不能乱")
	}
	// 最后剩的是 lag(seq 0 被丢掉后补上的那条)
	assert.True(t, isLag(take(t, c)), "队尾应该是 lag")
	assert.Equal(t, 0, peekLen(c))
}

// C3 丢过事件必须**挤出** lag —— 这条是本轮修掉的真 bug
//
// 旧实现里 lag 的发送是 `select { case out <- lag: default: }`,而紧邻其前的
// 丢事件分支刚好把空位填回去了 —— 于是 lag **几乎永远发不出去**
// (只在消费者恰好在两次发送的微秒间隙里读走一条时才侥幸成功)。
// 后果是"页面停在旧状态且没有任何提示",正是包注释第 3 条要防的静默失效。
func TestC3_Consumer_LagAlwaysEscapes(t *testing.T) {
	c := newConsumer()
	for i := 0; i < connBuffer; i++ {
		c.deliver(jobEv(int64(i)))
	}
	assert.Greater(t, c.dropped, int64(0), "这条用例的前提是真的丢过事件")
	assert.Equal(t, 1, drainAll(t, c),
		"第一次丢事件后就必须能读到 lag,读到 0 条就意味着前端永远不知道自己漏了")
}

// C4 lag 的 dropped 计数是真的丢了多少
func TestC4_Consumer_LagCarriesDropCount(t *testing.T) {
	c := newConsumer()
	for i := 0; i < connBuffer; i++ {
		c.deliver(jobEv(int64(i)))
	}

	var lag Event
	found := false
	for i := 0; i < connBuffer; i++ { // 固定长度:缓冲里正好 connBuffer 条
		ev := take(t, c)
		if isLag(ev) {
			lag, found = ev, true
		}
	}
	require.True(t, found)
	assert.Equal(t, int64(1), lagDropped(t, lag),
		"只丢过 1 条,lag 里就该写 1")
	assert.Equal(t, c.dropped, lagDropped(t, lag), "lag 里的计数必须和实际丢弃数一致")
}

// C5 lag 是**合并**的:狂灌 640 条,缓冲里也**最多**一条 lag
//
// ⚠️ 这里允许读到 0 条 —— 持续过载时 lag 自己也会被挤掉(它同样会随队头轮换
// 出队)。这不是缺陷:前端已经落后到读什么都不对,再补一条"你漏了"毫无意义;
// 等它追上来、下一次再丢事件时 lag 会重新生成(C6 覆盖这一点)。
// 不能接受的是 > 1 条 —— 那说明 lag 在刷缓冲。
func TestC5_Consumer_LagIsCoalesced(t *testing.T) {
	c := newConsumer()
	for i := 0; i < connBuffer*10; i++ {
		c.deliver(jobEv(int64(i)))
		assertInvariants(t, c, i)
	}
	assert.LessOrEqual(t, drainAll(t, c), 1, "lag 必须合并成一条")
	assert.Equal(t, int64(connBuffer*10-(connBuffer-1)), c.dropped)
}

// C6 缓冲腾空后,再丢事件仍能重新生成 lag(不是一次性发完就没了)
func TestC6_Consumer_LagRegeneratesAfterCatchUp(t *testing.T) {
	c := newConsumer()
	for i := 0; i < connBuffer; i++ {
		c.deliver(jobEv(int64(i)))
	}
	require.Equal(t, 1, drainAll(t, c), "第一轮应产生一条 lag")

	// 缓冲已清空,再灌一轮 → 又丢事件 → 应再产生一条 lag
	for i := 0; i < connBuffer; i++ {
		c.deliver(jobEv(int64(1000 + i)))
	}
	assert.Equal(t, 1, drainAll(t, c), "第二轮丢事件后应再补一条 lag")
}

// C7 **永不阻塞**:deliver 必须在有限时间内返回
//
// 这是 §3.5 的核心承诺 —— 一个卡住的浏览器不能拖住整个 Redis 订阅循环。
// 如果 deliver 里有任何一处阻塞式发送,这里会直接超时。
func TestC7_Consumer_NeverBlocks(t *testing.T) {
	c := newConsumer()
	// 消费端**一个都不读**,deliver 必须全部立刻返回
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < connBuffer*20; i++ {
			c.deliver(jobEv(int64(i)))
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("deliver 阻塞了 —— 慢消费者会因此拖住整个订阅循环")
	}
}

// C8 内部计数与实际缓冲长度始终一致(防后续改动打破不变量)
func TestC8_Consumer_CountMatchesChanLen(t *testing.T) {
	c := newConsumer()
	for i := 0; i < connBuffer*3; i++ {
		c.deliver(jobEv(int64(i)))
		assertInvariants(t, c, i)
	}
}

// C9 **消费端跟得上时,deliver 也不能阻塞** —— 这条是本轮修掉的真 bug
//
// 旧实现用 `n int` 自己维护"缓冲里有多少条",但**消费端把事件读走时
// 没有人通知这个计数**(读走的那一端是 SSE handler / 浏览器,只管 `<-evs`)。
// 于是 n 只增不减 —— 一个**跟得上**的消费端(正常情况!)在第 64 条事件上
// 就会走进"丢最旧"分支,而那时缓冲早被读空了,`<-c.out` 于是**永久阻塞**。
//
// 后果:pump 的 goroutine 卡死 ⇒ 这条 SSE 连接从此收不到任何事件,
// 而心跳还在发(那是 handler 另一个 goroutine 写的)⇒ 连接看起来是活的,
// 但页面就是不再刷新,且**没有任何报错**。
//
// ⚠️ 这条用例跟 C7 是一对:C7 是"消费端不读"(慢客户端),C9 是"消费端跟得上"。
// 只测前者的话,死锁分支永远走不到 —— 那正是这个 bug 藏了这么久的原因。
//
// ⚠️ **消费端必须在同一个 goroutine 里"发一条读一条"** ——
// 用一个独立的读 goroutine 的话,它跟投递方赛跑:缓冲碰巧非空时
// `<-c.out` 就不会卡住,用例变成**假绿**(实测:旧实现下有时红有时绿)。
// 同一个 goroutine 里"投递完立刻读走",缓冲恒为空,死锁是确定的。
func TestC9_Consumer_KeepingUpConsumerDoesNotBlock(t *testing.T) {
	c := newConsumer()

	const total = connBuffer + 5
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < total; i++ {
			c.deliver(jobEv(int64(i)))
			// 消费端立刻读走 —— 真实 SSE handler 就是这么干的
			<-c.out
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deliver 阻塞了 —— 跟得上的消费端被当成了慢客户端," +
			"pump 的 goroutine 会永久卡死,这条 SSE 连接从此收不到事件")
	}

	assert.Equal(t, int64(0), c.dropped,
		"消费端跟得上时不该丢任何事件")
}

// ─────────── pump 外壳(goroutine) ───────────

// P1 端到端透传:raw → out
func TestP1_Pump_Passthrough(t *testing.T) {
	raw := make(chan *redis.Message, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := pump(ctx, raw)
	for i := 1; i <= 3; i++ {
		raw <- rawOfEvent(t, jobEv(int64(i)))
	}
	close(raw)

	for i := 1; i <= 3; i++ {
		select {
		case ev, ok := <-out:
			require.True(t, ok, "第 %d 条后 channel 提前关闭", i)
			assert.Equal(t, int64(i), durationOf(t, ev))
		case <-time.After(2 * time.Second):
			t.Fatalf("第 %d 条事件没收到", i)
		}
	}
	_, ok := <-out
	assert.False(t, ok, "raw 关闭后 out 必须关闭")
}

// P2 ctx 取消 → out 必须被关闭,否则 SSE writer 永远等不到结束
func TestP2_Pump_ClosesOnContextCancel(t *testing.T) {
	raw := make(chan *redis.Message)
	ctx, cancel := context.WithCancel(context.Background())

	out := pump(ctx, raw)
	// 先塞一条,确保 goroutine 已经起来在 select 上
	raw <- rawOfEvent(t, jobEv(1))
	select {
	case <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("第一条事件都没收到")
	}

	cancel()
	select {
	case _, ok := <-out:
		assert.False(t, ok, "ctx 取消后 channel 必须关闭")
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 out 没有关闭")
	}
}

// P3 脏数据跳过而不是崩掉整条连接
func TestP3_Pump_SkipsMalformedPayload(t *testing.T) {
	raw := make(chan *redis.Message, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := pump(ctx, raw)
	raw <- &redis.Message{Payload: "这不是 JSON"}
	raw <- rawOfEvent(t, jobEv(7))
	close(raw)

	ev := <-out
	assert.Equal(t, int64(7), durationOf(t, ev),
		"脏数据后面的合法事件必须还能收到")
	_, ok := <-out
	assert.False(t, ok)
}

// ─────────── Publish ───────────

// P4 未 Init 时 Publish 静默丢弃,不 panic
func TestP4_Publish_NoTransport(t *testing.T) {
	withTransport(t, nil)
	assert.NotPanics(t, func() {
		Publish(context.Background(), []string{TopicJob}, Event{Event: EventJobFinished})
	})
}

// P5 空 topic 列表 → 不投递(连 fake 都不碰)
func TestP5_Publish_EmptyTopics(t *testing.T) {
	tr := &fakeTransport{avail: true}
	withTransport(t, tr)

	Publish(context.Background(), nil, Event{Event: EventJobFinished})
	Publish(context.Background(), []string{}, Event{Event: EventJobFinished})
	assert.Empty(t, tr.published)
}

// P6 At 缺省时自动补时间戳 —— 前端靠它丢弃乱序到达的旧事件
func TestP6_Publish_FillsTimestamp(t *testing.T) {
	tr := &fakeTransport{avail: true}
	withTransport(t, tr)

	before := time.Now().UnixMilli()
	Publish(context.Background(), []string{TopicJob}, Event{Event: EventJobFinished})
	after := time.Now().UnixMilli()

	require.Len(t, tr.published, 1)
	assert.GreaterOrEqual(t, tr.published[0].At, before)
	assert.LessOrEqual(t, tr.published[0].At, after)
}

// P7 已给的 At 不被覆盖(重放的事件要保留原时间)
func TestP7_Publish_KeepsProvidedTimestamp(t *testing.T) {
	tr := &fakeTransport{avail: true}
	withTransport(t, tr)

	Publish(context.Background(), []string{TopicJob},
		Event{Event: EventJobFinished, At: 1234567890})
	assert.Equal(t, int64(1234567890), tr.published[0].At)
}

// P8 Publish 失败只记日志,**不影响调用方** —— 推送是旁路
func TestP8_Publish_SwallowsTransportError(t *testing.T) {
	withTransport(t, &errTransport{})
	assert.NotPanics(t, func() {
		Publish(context.Background(), []string{TopicJob}, Event{Event: EventJobFailed})
	})
}

// ─────────── Subscribe ───────────

// P9 无 transport → 返回 nil channel 而非 error
//
// 理由:handler 拿到 nil channel 会发 degraded 让前端切轮询(能正常工作);
// 拿到 error 则前端会当成"连接失败"无限重连,而那永远也不会成功。
func TestP9_Subscribe_NoTransportReturnsNilChannel(t *testing.T) {
	withTransport(t, nil)

	ch, cancel := Subscribe(context.Background(), []string{TopicJob})
	require.NotNil(t, cancel, "cancel 必须非 nil,否则调用方 defer cancel() 会 panic")
	assert.Nil(t, ch)
	cancel()
	assert.False(t, Available())
}

// P10 空 topic → 同样返回 nil channel 而不是起一条空订阅
func TestP10_Subscribe_EmptyTopics(t *testing.T) {
	tr := &fakeTransport{avail: true}
	withTransport(t, tr)

	ch, _ := Subscribe(context.Background(), nil)
	assert.Nil(t, ch)
	assert.Empty(t, tr.subscribed, "空 topic 不该真的去订阅")
}

// P11 订阅失败(Redis 挂)→ 返回 nil channel,不是 error
func TestP11_Subscribe_SwallowsTransportError(t *testing.T) {
	withTransport(t, &errTransport{})

	ch, cancel := Subscribe(context.Background(), []string{TopicJob})
	require.NotNil(t, cancel)
	assert.Nil(t, ch, "订阅失败要退化成降级,不是抛错让前端无限重连")
	cancel()
}

// P12 正常订阅把 topic 透传给 transport
func TestP12_Subscribe_PassesTopics(t *testing.T) {
	tr := &fakeTransport{avail: true}
	withTransport(t, tr)

	_, cancel := Subscribe(context.Background(), []string{TopicUser(7), TopicJob})
	cancel()
	assert.Equal(t, []string{TopicUser(7), TopicJob}, tr.subscribed)
}

// ─────────── 命名契约 ───────────

// T1 频道名是对外契约,改了前端和跨实例部署全断
func TestT1_TopicNamesAreStable(t *testing.T) {
	assert.Equal(t, "perm:invalidate:all", TopicPerm,
		"⚠️ 迁移前就存在的频道名,发布端没改过,不能动")
	assert.Equal(t, "job", TopicJob)
	assert.Equal(t, "approval", TopicApproval)
	assert.Equal(t, "push:user:42", TopicUser(42))
}

// T2 事件名前端按字面量注册回调,不能随便改
func TestT2_EventNamesAreStable(t *testing.T) {
	assert.Equal(t, "connected", EventConnected)
	assert.Equal(t, "degraded", EventDegraded)
	assert.Equal(t, "lag", EventLag)
	assert.Equal(t, "job.finished", EventJobFinished)
	assert.Equal(t, "job.failed", EventJobFailed)
	assert.Equal(t, "inbox.unread", EventInboxUnread)
	assert.Equal(t, "perm_changed", EventPermChanged,
		"与迁移前的 perm-stream 事件名保持一致,前端处理逻辑可原样搬")
}

// T3 user topic 之间的隔离:user 频道不能互相收到
func TestT3_TopicUser_IsolatesUsers(t *testing.T) {
	assert.NotEqual(t, TopicUser(1), TopicUser(2))
	// 且不能撞上广播频道
	assert.NotEqual(t, TopicJob, TopicUser(1))
	assert.NotEqual(t, TopicPerm, TopicUser(1))
}

// T4 payload 不含业务正文 —— JobPayload 只有状态元信息
func TestT4_JobPayload_HasNoBusinessBody(t *testing.T) {
	b, err := json.Marshal(JobPayload{
		JobKey: "framework.job_history_purge", Status: "failed",
		DurationMs: 1234, TriggerType: "schedule",
	})
	require.NoError(t, err)

	fields := map[string]any{}
	require.NoError(t, json.Unmarshal(b, &fields))
	// 白名单:多一个字段就是"往推送里塞了业务正文"
	for k := range fields {
		assert.Contains(t,
			[]string{"job_key", "status", "duration_ms", "trigger_type"}, k,
			"JobPayload 出现未登记字段 %q —— 推送不放业务正文", k)
	}
	assert.NotContains(t, fields, "error", "error 正文不进推送,前端回查 REST")
	assert.NotContains(t, fields, "trace_id")
}

// ─────────── T5 审批 / 待办契约(M73 骨架)───────────
//
// 下面几条守的是**契约**,不是行为 —— 审批模块还没写,没有任何发布端。
// 那正是要把契约钉死的时候:事件是广播的,字段一旦上线就成了所有人的约定,
// 事后想收窄就是破坏性变更。所以"这个 payload 能带什么"必须有断言。
// "目前没有发布端"不是不写测试的理由 —— 那只说明还没人用。

// 白名单:审批事件里多一个字段就是"往广播里塞了业务正文"
func TestT5_ApprovalPayload_WhitelistOnly(t *testing.T) {
	b, err := json.Marshal(ApprovalPayload{
		TodoID: "1734567890123456789", Kind: "approval",
		Action: "approved", AssigneeID: "1001",
	})
	require.NoError(t, err)

	fields := map[string]any{}
	require.NoError(t, json.Unmarshal(b, &fields))
	for k := range fields {
		assert.Contains(t,
			[]string{"todo_id", "kind", "action", "assignee_id"}, k,
			"ApprovalPayload 出现未登记字段 %q —— 审批事件是广播的,字段即契约", k)
	}
	// 审批意见 / 事由 / 金额 / 标题 一律不进推送:
	// "能看列表" ≠ "该看这一条的意见",广播出去就是越权
	assert.NotContains(t, fields, "reason")
	assert.NotContains(t, fields, "comment")
	assert.NotContains(t, fields, "amount")
	assert.NotContains(t, fields, "title", "标题常含业务摘要,前端回查 REST")
}

// AssigneeID 是 omitempty:待办刚创建还没指派时就不该有这个 key。
// 前端判"有没有处理人"用 `'assignee_id' in payload`,而不是读一个 undefined。
func TestT5_ApprovalPayload_AssigneeOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(ApprovalPayload{
		TodoID: "1734567890123456789", Kind: "todo", Action: "created",
	})
	require.NoError(t, err)

	fields := map[string]any{}
	require.NoError(t, json.Unmarshal(b, &fields))
	assert.NotContains(t, fields, "assignee_id")
	assert.Contains(t, fields, "todo_id")
}

// 事件名不得重复。两个常量同值的话,前端 addEventListener 会注册两次,
// 同一条事件回调跑两遍 —— 对 refetch 型事件就是重复请求。
func TestT5_AllEventNames_AreDistinct(t *testing.T) {
	all := map[string]string{
		"connected":     EventConnected,
		"degraded":      EventDegraded,
		"lag":           EventLag,
		"perm_changed":  EventPermChanged,
		"inbox.unread":  EventInboxUnread,
		"job.finished":  EventJobFinished,
		"job.failed":    EventJobFailed,
		"approval.todo": EventApprovalTodo,
		"approval.done": EventApprovalDone,
	}
	seen := map[string]string{}
	for name, val := range all {
		require.NotEmpty(t, val, "事件名 %s 不能为空(会广播一条无名事件)", name)
		if prev, dup := seen[val]; dup {
			t.Fatalf("事件名冲突:%q 与 %q 同为 %q —— 前端会注册两次回调", prev, name, val)
		}
		seen[val] = name
	}
}

// ─────────── fake Transport ───────────

type fakeTransport struct {
	mu         sync.Mutex
	avail      bool
	published  []Event
	topics     []string
	subErr     error
	subscribed []string
}

func (f *fakeTransport) Available() bool { return f.avail }

func (f *fakeTransport) Publish(_ context.Context, topics []string, ev Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, ev)
	f.topics = topics
	return nil
}

func (f *fakeTransport) Subscribe(_ context.Context, topics []string) (<-chan Event, func(), error) {
	f.mu.Lock()
	f.subscribed = append(f.subscribed, topics...)
	f.mu.Unlock()
	if f.subErr != nil {
		return nil, func() {}, f.subErr
	}
	ch := make(chan Event)
	return ch, func() {}, nil
}

// withTransport 临时替换全局 transport
func withTransport(t *testing.T, tr Transport) {
	t.Helper()
	orig := defaultTransport
	defaultTransport = tr
	t.Cleanup(func() { defaultTransport = orig })
}

type errTransport struct{}

func (errTransport) Available() bool { return true }
func (errTransport) Publish(context.Context, []string, Event) error {
	return assert.AnError
}
func (errTransport) Subscribe(context.Context, []string) (<-chan Event, func(), error) {
	return nil, func() {}, assert.AnError
}
