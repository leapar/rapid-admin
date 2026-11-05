# push —— 服务端事件推送通道(SSE 的传输层地基)

> 后台要在权限变更、任务跑完、字典改了之后**立刻**刷新界面。
> SSE(而不是 WebSocket)是因为它走普通 HTTP,浏览器自带断线重连,
> 而且是单向的 —— 这个场景本来就只需要服务端 → 客户端。

## 功能

push 是 SSE 的**传输层地基**,只做三件事:

1. **订阅 / 发布**:`Publish(ctx, topics, ev)` 投递、`Subscribe(ctx, topics)` 订阅。
   传输层是 `Transport` 接口,当前**只有 Redis pub/sub 一个实现在跑**
   (`NewRedisTransport`)。留接口的唯一理由是将来换 WebSocket 时新增一个实现即可,
   业务侧的 `Publish` 调用一行都不用改。
2. **topic 命名空间**:`TopicPerm` / `TopicJob` / `TopicApproval` / `TopicDict` /
   `TopicUser(userID)`。一条 SSE 连接承载全部 topic,前端**不能自选**订阅范围
   (服务端按 RBAC 算,见 `internal/api/handler/v1/event.go`)。
3. **慢消费者背压**:每条连接一个 64 格缓冲,满了**丢最旧**而不是阻塞,
   并补一条 `lag` 事件告诉前端"你漏了,去补拉"。

**三条设计(包注释里的原话)**:

- **事件是信号,不是数据**。payload 里没有业务正文,前端收到后重新调 REST 拿最新状态。
  这样推送不扩大数据可见范围,也不会产生"REST DTO 一份、推送 payload 一份"的双份契约漂移。
- **单路径投递**。Redis pub/sub 是唯一通道,没有"进程内 Hub + Redis"双投递 ——
  双投递会让**同实例**的连接收到两份事件(长列表页反复重拉),
  而且这个 bug 只在单实例部署下出现,多实例部署恰好正常。
- **不静默**。Redis 不可用时 `/events` 会发 `degraded` 事件让前端切轮询。

**不解决什么**:不鉴权(订阅范围由 handler 判)、不做消息持久化(Redis pub/sub 不补发)、
不推 App 离线消息(那是 [pushx](pushx.md))。

## 配置

### yaml 侧

**该包自身无 yaml 配置块** —— 没有 `push:` 段。

它依赖的是 `redis:` 块(Redis pub/sub 是唯一通道):

```yaml
redis:
  addr: 127.0.0.1:6379
  password: ${REDIS_PASSWORD}
  db: 0
  pool_size: 50
  min_idle_conns: 10
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s
```

⚠️ 别把 `push:` 段(那是 [pushx](pushx.md) 的 App 推送)和本包搞混 ——
两个都叫"推送",但通道、投递对象、失败语义完全不同。

### 代码侧(构造选项)

```go
push.InitRedis(rdb)          // 用 redis.Client 构造默认传输层
push.Init(transport)          // 注入任意 Transport 实现
push.NewRedisTransport(rdb)   // 显式构造 redis 传输层
```

`InitRedis(nil)` 直接 return(不覆盖已注入的传输层);`Init(nil)` 显式清空。

## 使用方法

### 装配(启动期一次)

```go
push.InitRedis(repository.GetRedis())
if push.Available() {
    logger.L().Info("push transport ready")
}
```

### 发布(业务侧)

```go
push.Publish(ctx, []string{push.TopicUser(userID)}, push.Event{
    Topic:   push.TopicUser(userID),
    Event:   push.EventInboxUnread,
    Payload: inbox.UnreadEvent{UserID: userID, Delta: +1},
})
```

⚠️ **`Publish` 永不返回错误** —— 推送是尽力而为的旁路,
投递方是"刚写完库的请求"或"cronx 刚跑完任务的 goroutine",
不该因为"推送没发出去"就让主流程失败。失败只记 Warn 日志。

多 topic 一次投递(pipeline 发,一次 RTT):

```go
push.Publish(ctx, []string{push.TopicPerm, push.TopicJob}, push.Event{
    Topic:   push.TopicJob,
    Event:   push.EventJobFinished,
    Payload: push.JobPayload{JobKey: "framework.job_history_purge", Status: "success", DurationMs: 12},
})
```

### 订阅

```go
evs, cancel := push.Subscribe(ctx, []string{push.TopicPerm})
defer cancel()
for ev := range evs {
    // ev.Topic / ev.Event / ev.Payload / ev.At
}
```

### 事件名与 payload 对照

| 事件名常量 | wire 上的名字 | payload 类型 | 含义 |
|---|---|---|---|
| `EventConnected` | `connected` | `{topics, degraded}` | 连接建立 |
| `EventDegraded` | `degraded` | `{reason, retry_ms}` | Redis 不可用,前端切轮询 |
| `EventLag` | `lag` | `{dropped}` | 本连接被丢过事件,前端补拉 |
| `EventPermChanged` | `perm_changed` | `permnotify.Message` | 权限变了 |
| `EventJobFinished` / `EventJobFailed` | `job.finished` / `job.failed` | `push.JobPayload` | 定时任务状态 |
| `EventInboxUnread` | `inbox.unread` | `inbox.UnreadEvent` | 站内信未读数变了 |
| `EventApprovalTodo` / `EventApprovalDone` | `approval.todo` / `approval.done` | `push.ApprovalPayload` | 待办 / 办完了 |
| `EventDictChanged` | `dict.changed` | `push.DictChangedPayload` | 某个字典组被改了 |

## 注意事项

- ⚠️ **消费者缓冲长度必须读 `len(c.out)`,不能自己维护计数**。
  原实现有个 `n int` 字段,靠 `deliver` 里的 `n++` / `n--` 维护"缓冲里有多少条",
  但**消费端把事件读走时没有任何人会通知这个计数** —— 读走的那一端是 SSE handler /
  浏览器,它们只管 `<-evs`,不会回调 consumer。
  于是 `n` 只增不减:一个**跟得上**的消费端(正常情况!)在第 64 条事件上就走进"丢最旧"分支,
  而那时缓冲早被读空了 —— `<-c.out` 于是**永久阻塞**,pump 的 goroutine 卡死,
  这条 SSE 连接从此再收不到任何事件,而且**没有任何报错**。
  症状极隐蔽:心跳还在发(那是 handler 另一个 goroutine 写的),连接看起来是活的,
  但页面就是不再刷新。心跳 25s 一跳、job 事件每分钟一条 ⇒ 一个开着后台的标签页
  几小时内必然踩到。(commit `f6d4225` 修的就是这个。)
- ⚠️ **缓冲的最后一格永久留给 `lag`**。数据事件上限恒为 `connBuffer-1`(63),
  任何时刻缓冲里至少有 1 格空位,`lag` 一定塞得进去。
  否则数据事件把缓冲占满后 `lag` 永远挤不进去 ⇒ 前端不知道自己漏了 ⇒
  页面停在旧状态且无任何提示("不静默"直接失效)。
- ⚠️ **慢消费者策略是"丢最旧"不是阻塞**。丢一条旧的代价是"这次刷新看到稍旧的状态",
  下一个事件立刻纠正;阻塞整个订阅循环的代价是**该实例所有 SSE 连接都停摆**。
  两者差三个数量级。丢最新更糟:页面永远停在最老的状态,而那个状态再也等不到后续事件纠正。
- ⚠️ **持续过载时 `lag` 事件自己也会随队头轮换出队** —— 这是**有意接受**的:
  前端已经落后到读什么都不对,再补一条"你漏了"毫无意义;等它追上来、下一次再丢事件时
  `lag` 会重新生成。所以前端不能只靠 `lag` —— **连接建立时前端就应当主动拉一次全量**。
- ⚠️ **`Subscribe` 在 Redis 不可用时返 nil channel,不返 error**。
  返回 error 会让前端看到"连接失败"然后无限重连 —— 而那永远也不会成功。
  `/events` handler 据此发 `degraded` 事件并主动断开,让 `EventSource` 按
  服务端给的 `retry:60000` 慢速重试。
- ⚠️ **Redis pub/sub 不补发**。`Subscribe` 内部必须等一次订阅确认
  (`ps.Receive`)—— 跳过这步,`Subscribe` 返回后到 Redis 真正生效之间有一个窗口,
  这期间发布的事件会**永久丢失**。
- ⚠️ **事件名不许改**。前端 `useEventStream.ts` 的 `EVENT` 常量按名字订阅,
  改名等于让所有已发版的前端收不到事件,而症状是"字典改了不生效" / "权限变了不刷新",
  不报错、很难归因。
- ⚠️ **payload 不带业务正文**。`JobPayload` 刻意不含 `Error` 正文、
  `ApprovalPayload` 不含驳回意见、`DictChangedPayload` 只带 `group_code`。
  收到的人自己回查 —— 那是按权限过滤过的地方。
- ⚠️ **事件是广播 ⇒ 角标必须重拉 total,不能 ±1**。同一 topic 上既有"分给我的"
  也有"我发起的",前端根本判不出"这条该动谁的角标";而且 delta 在并发下会漂移。
  前端 `refreshUnread` / `refreshApprovalTodo` 都是**一律重拉**。
- ⚠️ **订阅范围是建连那一刻的快照**。权限变了之后老连接上仍挂着已经不属于自己的 topic,
  所以收到 `perm_changed` 后**必须重建连接**(`useEventStream` 里延迟 500ms 重连),
  让服务端重判 `subscribableTopics`。
- ⚠️ **`TopicPerm` 的频道名故意不带 `push:` 前缀**。`perm:invalidate:all` 是迁移前就存在的,
  保留它意味着本模块接管订阅端时发布端一行都不用改,没有"双写 / 双订阅"的迁移窗口。

## 相关

- 源码:`internal/pkg/push/`
- 可运行示例:`examples/push/main.go` —— `go run ./examples/push`
- 权限失效广播(本包的一个生产者)→ [permnotify](permnotify.md)
- 站内信未读信号(另一个生产者)→ [inbox](inbox.md)
- Redis 连接配置 → [cachex](../storage/cachex.md)
