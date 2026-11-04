# inbox —— 站内信 + 通知中心

> 业务模块(审批、订单、系统通知……)要给某个用户发一条站内信时,
> 只依赖一个接口:`Notifier.Notify(ctx, Message)` —— 不依赖具体实现、
> 不依赖 DB、不依赖 Redis。
> 通知中心那一侧(列表 / 未读数 / 标已读)也在这里。

## 功能

inbox 提供四件事:

1. **投递**:`Notifier` 接口(`Notify` / `NotifyBatch`)。
   业务模块依赖**接口**而不是 `Manager`,换存储(比如换成 MQ)时业务代码一行不用改。
2. **通知中心查询**:`Manager.List` / `UnreadCount` / `MarkRead` / `MarkAllRead` / `Delete`。
3. **未读变更信号**:投递 / 标已读之后,经 [push](push.md) 推 `push:user:<id>` 频道,
   前端 SSE 实时刷角标。Redis 不可用 ⇒ 推送静默丢弃,前端轮询 `unread-count` 兜底。
4. **防 IDOR**:`List` / `MarkRead` / `MarkAllRead` / `Delete` 全部带 `user_id` 条件 ——
   不属于自己的那条**返 nil(不报错,也不动)**。

**不解决什么**:

- 不做投递幂等(见「注意事项」第 1 条 —— 同一业务事件调两次 `Notify` 会写两条);
- 不做权限判定(RBAC 码挂在 handler 上,不在这个包里);
- 不做离线推送(App 侧离线通知是 [pushx](pushx.md));
- 不做多通道分发(那层是 [notifyx](notifyx.md))。

## 配置

### yaml 侧

**该包无 yaml 配置。** 没有 `inbox:` 块,也没有任何配置项 —— 它只有一个 `*gorm.DB` 依赖,
表 `sys_inbox_message` 由 GORM AutoMigrate 建(`internal/migrate/migrate.go` 里注册了
`&model.InboxMessage{}`)。示例里手写 DDL,是因为框架示例不许 import `internal/model`。

### 代码侧(构造选项)

```go
mgr := inbox.New(db)   // 唯一构造函数,没有 opts
```

生产上由 `cmd/server` 装配(`inboxMgr := inbox.New(db)`),
业务模块拿的是 `registry.InboxNotifier`(`inbox.Notifier` 接口)。

## 使用方法

### 投递

```go
// 业务模块依赖的是 Notifier 接口,不是 Manager
var notifier inbox.Notifier = mgr

err := notifier.Notify(ctx, inbox.Message{
    UserID:   userID,
    MsgType:  "biz",
    Title:    "订单确认单待确认",
    Content:  "您的订单已生成,请在 7 天内确认。",
    Link:     "/order/confirm/20260301",
    RefType:  "order",
    RefID:    20260301,
    SenderID: 1,
})
```

批量投递(同事务,失败整批回滚;`msgs` 里允许不同 `UserID`):

```go
err := notifier.NotifyBatch(ctx, []inbox.Message{
    {UserID: userID, MsgType: "system", Title: "系统公告", Content: "本周六 02:00-04:00 系统维护。"},
    {UserID: userID, MsgType: "notice", Title: "费用提醒", Content: "第三季度账单将于 10 月 15 日开始计收。"},
})
```

### 未读数 / 列表

```go
n, err := mgr.UnreadCount(ctx, userID, "")    // 全部未读;msgType 非空则按类型过滤
views, total, err := mgr.List(ctx, inbox.ListFilter{
    UserID:     userID,
    MsgType:    "biz",   // 空 = 全部
    OnlyUnread: false,
    Page:       1,
    Size:       20,
})
```

⚠️ **角标数字以 `UnreadCount` 为准,推送里只带 delta(信号)**。

### 标已读 / 删除

```go
err := mgr.MarkRead(ctx, userID, msgID)          // 幂等:已读的不报错
affected, err := mgr.MarkAllRead(ctx, userID, "") // 返回影响条数
err := mgr.Delete(ctx, userID, msgID)            // 防 IDOR:只能删自己的
```

### wire 契约

`List` 返回 `[]inbox.MessageView`(`id` / `ref_id` / `sender_id` 是 **string**,
`read_at` 未读为 `null`)。完整字段说明见 [框架约定 §4](../getting-started/conventions.md)。

## 注意事项

- ⚠️ **投递侧没有幂等键**。`inbox.Message` 里没有去重字段,同一个业务事件调两次
  `Notify` 会**写两条**。要"同一事件只发一次"得靠调用方自己带业务键去重
  (或走 [idempotentx](../security/idempotentx.md) 的幂等键)。包里唯一的幂等是
  **标已读**:已读再标一次返 nil、不动数据。
- ⚠️ **`Message.Validate()` 里的"默认 system"是无效的**。它的签名是值接收者
  `func (m Message) Validate() error`,函数体里的 `m.MsgType = "system"` 改的是**副本**,
  调用方看不到 —— `Notify` 里 `msg` 原样落库,`msg_type` 是**空串**而不是 `"system"`。
  后果:按 `msg_type` 过滤(`ListFilter.MsgType` / `UnreadCount` 的 msgType 参数)
  永远筛不到这些消息。要按类型分类,**调用方必须自己填 `MsgType`**。
- ⚠️ **推的 delta 恒为 +1,连"标已读"也是 +1**。`publishUnread` 不区分方向,
  因为并发投递 / 标已读会让 delta 累加漂移。所以前端**必须重拉 `unread-count` 拿绝对值**,
  拿 delta 做 +1/-1 会在标已读之后让角标反而上涨。
  前端 `useEventStream.refreshUnread` 就是这么做的。
- ⚠️ **动作完成后前端要自己拉一次角标,不等广播**。广播是"状态变了"的信号,
  不是"当前状态";连接建立之前发生的事件收不到。`InboxBell` 在
  `markRead` / `markAllRead` 之后都直接调了一次 `refreshUnread()`。
- ⚠️ **雪花 ID 在应用侧生成,不能交给 DB 自增**。`BaseModel.ID` 的 tag 是
  `primaryKey;autoIncrement:false`,列是 `bigint unsigned` 且**没有 AUTO_INCREMENT** ——
  MySQL 严格模式下直接报 1364「Field 'id' doesn't have a default value」,一条都投不出去;
  非严格模式更糟:全部写成 `id=0`,而 `MarkRead(userID, 0)` 会把**这个人的所有消息**
  一起标成已读。`newRow` 是 `Notify` / `NotifyBatch` 唯一共用的地方,别在别处再抄一份 map。
- ⚠️ **`List` 扫 `[]model.InboxMessage` 而不是 map**。GORM 的软删除作用域、
  `model.Time` 的时间归一化、`uint64` 的 string tag 三件事都只在 typed 扫描下才生效 ——
  早先返回 `[]map[string]any` 时,wire 上是裸数字 ID(JS 的 2^53 会静默截断),
  时间格式还出现了"本地时间带纳秒"和"UTC 秒级"两种。
- ⚠️ **两个方向的失败处理是相反的,刻意的**:DB 写失败 ⇒ **返 error**(业务层决定是否回滚主事务);
  Redis 不可用 ⇒ **推送静默丢弃**(只是少了实时性,前端轮询兜底)。
- ⚠️ **列表过滤掉已过期,但保留已读**;未读判定是 `read_at IS NULL`,
  **不是"比时间戳"** —— 未读没有时间戳可比。
- ⚠️ **`RefID` / `SenderID` 为 0 时输出空串**,不是 `"0"` —— 0 的含义是"无关联 / 系统发的"。

## 相关

- 源码:`internal/pkg/inbox/`
- 可运行示例:`examples/inbox/main.go` —— `go run ./examples/inbox`
- 传输层(未读信号走它)→ [push](push.md)
- 多通道分发(站内信 + 邮件的组合投递)→ [notifyx](notifyx.md)
- ID 用字符串上 wire → [jsonid](../infrastructure/jsonid.md)
