# notifyx —— 多通道通知分发

> 把"要通知某人"这件事,从"用哪个通道发"里拆开。
> 调用方只描述 **给谁、说什么**;分发器决定 **走哪几条通道**、
> 以及 **某条通道挂了怎么办**。

## 功能

notifyx 解决的是"通知通道各自为政、失败语义没统一"这件事:

1. **通道抽象**:`Channel` 接口 —— `Name()` + `Send(ctx, Recipient, Message) error`。
   自定义通道就是实现两个方法(示例里有一个 `logChannel`)。
2. **一次通知多通道**:`Dispatcher` 按注册顺序逐条投递,收件人 / 内容只描述一次。
3. **失败隔离**:单条通道失败不牵连其它通道,失败收进 `Result.Failed`(map,一个通道对一条错误)。
4. **panic 隔离**:每条通道的调用都 `recover`,panic 记成该通道 failed。
5. **组合投递器 `Fanout`**:站内信(记录)+ 额外交付通道(送达)拼在一起,
   **它实现了 `inbox.Notifier`** —— 装配处把它塞进原来 `inboxMgr` 的位置,
   所有既有的站内信投递自动多出一条通道,调用方一行都不用改。

**不解决什么**:

- 不认识审批 / 站内信的业务语义 —— 只认 `Recipient` + `Message`;
- 不做投递幂等;
- 不做权限判定。

## 配置

### yaml 侧

**该包自身无 yaml 配置块。** 它没有 `notifyx:` 段。

生产装配在 `cmd/server` 里,读的是 **`email:` 块**(邮件通道要发件地址和站点外链前缀):

```yaml
email:
  provider: ""            # "" / console / smtp / aliyun
  from: ""                # 发件邮箱(缺省取 smtp.username / aliyun.account_name)
  from_name: Admin Console
  base_url: ""            # 站点外链前缀 —— 邮件里的链接要拼成绝对地址
  timeout: 30             # 单次发送超时(秒)
```

⚠️ `email.provider` 为空串 = **这条通道不存在**(不是"配好了但关着")。
此时 `buildNotifierDispatcher` 一个通道都不注册,`Fanout` 就只剩站内信。
"配置了但没生效"比"没配"更难查 —— 运维看着 yaml 里有 email 段,以为在发邮件。

### 代码侧(构造选项)

```go
d := notifyx.NewDispatcher(channels ...Channel)   // 可预置通道
d.Register(c)                                     // 运行时注册(同名覆盖)

fanout := notifyx.NewFanout(inner, d, resolver)  // 额外交付异步(生产)
fanout := notifyx.NewSyncFanout(inner, d, resolver) // 同步(只给测试用)
```

- `inner`:`inbox.Notifier`(站内信那一份,必填)
- `dispatcher`:`*notifyx.Dispatcher`(额外交付通道,可空 = 只有站内信)
- `resolver`:`notifyx.Resolver`(`UserID → Recipient`,可空 = 不投递)

## 使用方法

### 只用分发器(纯多通道)

```go
d := notifyx.NewDispatcher(
    &logChannel{name: "email"},
    &logChannel{name: "sms"},
)
res := d.Dispatch(ctx, notifyx.Recipient{
    UserID: 1001,
    Email:  "zhangwei@example.com",
    Phone:  "13800001234",
}, notifyx.Message{
    Title:   "您有一条新的审批待办",
    Content: "请在 24 小时内处理",
    Link:    "/approval/8801",
    RefType: "approval",
    RefID:   8801,
})

res.Delivered  // []string  成功投递的通道名
res.Skipped    // []string  收件人不适用(比如没邮箱)
res.Failed     // map[string]error  失败通道 → 错误
res.OK()       // bool  至少有一条通道投递成功
res.Err()      // error  所有失败用 errors.Join 汇总
```

### 自定义通道

```go
type logChannel struct{ name string }

func (c *logChannel) Name() string { return c.name }

func (c *logChannel) Send(ctx context.Context, r notifyx.Recipient, m notifyx.Message) error {
    // 没有邮箱 = skipped,不算失败
    if r.Email == "" {
        return fmt.Errorf("%w: 用户 %d 没填邮箱", notifyx.ErrSkip, r.UserID)
    }
    return nil
}
```

⚠️ **用 `errors.Is(err, notifyx.ErrSkip)` 判跳过,不要用 `==`** ——
通道实现可能在 `ErrSkip` 上再包一层(带上下文),`==` 会漏判,把"跳过"记成"失败"。

### 组合投递器(站内信 + 邮件)

```go
notifier := notifyx.NewFanout(
    inboxMgr,                              // 站内信(记录)
    buildNotifierDispatcher(emailSender, cfg.Email), // 额外交付(送达)
    notifyx.NewDBResolver(db),             // UserID → 联系方式
)
// 装配进 Registry:InboxNotifier: notifier —— 业务侧照旧调 Notify
```

`DBResolver` 从 `sys_user` 读 `email` / `phone`,并做格式校验:
不像邮箱的值一律当"没有邮箱",不像手机号(11 位 / 1 开头 / 全数字)的值一律当"没有手机号"。

## 注意事项

- ⚠️ **两个方向的失败语义是相反的,别混**:
  - **站内信是"记录"**:写库失败 ⇒ `Notify` **返 error**,业务层据此决定是否回滚主事务;
  - **额外交付是"送达"**:邮件 / 推送失败 ⇒ **只记 Warn 日志,不返 error**。
    审批单已经落库,不该因为"邮件没发出去"让审批失败。
  两者混成一个返回值,调用方就没法区分"没发出去"和"发出去但没收到"。
- ⚠️ **"没有某个联系方式"是 skipped,不是 failed** —— 后台账号大量不填邮箱,那是常态。
  记成 failed 会让"投递成功率"这个指标永远掉。
  ⚠️ 但**"通道自己没配好"是 failed,不是 skipped**:邮件通道 `from` 为空返的是
  普通 error(不是 `ErrSkip`),因为 skip 会让"配错了却没人发现"。
  这就是 email 通道内部两种"没发出去"方向相反的地方。
- ⚠️ **分发器里目前只有邮件一条通道** —— 推送通道(`pushx`)**故意没挂**:
  推送要的是**设备令牌**(APNs device token / 厂商 registration id),全仓没有那张登记册,
  挂上去每条通知都会 skip,看着像"接上了"其实没发。
  `pushx` 的 sender 走 `registry.PushSender` 交给业务侧,登记册落地后再接。
- ⚠️ **异步投递必须脱离请求 ctx**。`Fanout.deliverExtra` 用
  `context.WithoutCancel(ctx)` + 独立 30s deadline:请求超时后 ctx 已经死了,
  拿它发邮件必然失败,表现是"审批成功了但收件人没收到邮件"。
  ⚠️ 30s 不是随便取的 —— 站内信是写库 3s 够,邮件要跟外网 SMTP 握手 + 传输,
  3s 会把正常投递判成超时。
- ⚠️ **`NewSyncFanout` 只给测试用**。同步投递会把一次 SMTP 往返挂在调用方的关键路径上,
  而调用方可能正处在 HTTP 请求里。
- ⚠️ **分发是串行的,不是并发**。三个理由:通知不是热路径;串行时 `Result` 的通道顺序
  = 注册顺序,**可复现**(并发时每次都不一样,排查时对不上);邮件(SMTP)本身有连接成本,
  并发发反而更容易被对方限流。
- ⚠️ **`Register` 同名通道直接覆盖**,不并存。重复注册会让 `Result` 里的通道名出现两次,
  排查时根本分不清是哪一次投递。
- ⚠️ **`Result.Failed` 是 map 不是 slice** —— 多个通道可能同时失败,
  map 才能一个通道对一条错误;slice 只能记"有失败",查不出是哪条。
  `Result.Err()` 用 `errors.Join` 汇总,多个失败都要看得见,只留最后一个是丢信息。
- ⚠️ **邮件正文是 HTML,内容来自业务 —— 一律 `html.EscapeString` 转义**。
  不转义的话,标题里一个 `<script>` 就会在邮件客户端里执行
  (邮件客户端会禁脚本,但 HTML 注入照样能改样式、伪造钓鱼链接)。
- ⚠️ **邮件里的相对链接是死的**。收件人的邮件客户端不知道"当前站点"是哪个,
  点 `/approval/123` 会被解析成本地文件路径。`baseURL` 没配时:绝对链接照用,
  相对链接**丢掉**(好过给一个点不开的链接)。
- ⚠️ **站内信批量是"要么全成要么全败"**(inbox 侧一个事务),
  **额外交付是逐条尽力而为** —— 一封邮件发不出去不该让另外十九封也不发。
- ⚠️ **`notifyx.Message` 不重复定义站内信字段**。`Title` / `Content` / `Link` /
  `RefType` / `RefID` 的语义跟 `inbox.Message` 完全一致,重复一份就是漂移的起点。
  `Fanout` 的做法是直接复用 `inbox.Message`,只搬有对应语义的字段 ——
  `inbox` 里的 `MsgType` / `SenderID` / `ExpiredAt` 在"投递"这一层没有对应物。

## 相关

- 源码:`internal/pkg/notifyx/`
- 可运行示例:`examples/notifyx/main.go` —— `go run ./examples/notifyx`
- 站内信那一半 → [inbox](inbox.md)
- 邮件通道的实现 → [emailx](emailx.md)
- App 推送(另一条通道,目前没挂)→ [pushx](pushx.md)
