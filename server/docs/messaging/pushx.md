# pushx —— App 推送:抽象 Provider + 阿里云 EMNS

> 服务端 → **App 客户端**的离线通知(APNs / Android 厂商通道 / 鸿蒙 / Web Push)。
> 跟 [push](push.md) 完全不是一个东西 —— 那是服务端 → 浏览器的实时事件。

## 功能

pushx 把"给手机推一条消息"这件事抽象成一个 `Sender` 接口,当前有三个实现:

1. **`pushx.MockSender`**(`NewMock`)—— 记录已推送的 `Notification`,给单测用。
2. **`pushx.ConsoleSender`**(`NewConsolePush`)—— 只打到标准输出,dev 用。
3. **`pushx.AliyunEMNS`**(`NewAliyunEMNS`)—— 阿里云 EMNS(原 MPush)OpenAPI,
   覆盖 iOS APNs / Android 厂商(华为小米 OPPO vivo 魅族)/ Android 系统 / 鸿蒙 HMS / Web Push。

除此之外它提供:

- **`pushx.Notification`** —— 一条通知的内容(标题 / 正文 / 角标 / 自定义 data /
  离线定时 / Android 通道 / iOS 环境 / 静默推送)。
- **`pushx.Target`** —— 推送目标,`Type` 取 `account` / `device` / `alias` / `tag` /
  `tagAnd` / `tagNot`,**至少 1 个**(API 限制)。
- **`pushx.PushError`** —— 三段式错误(provider / code / message + `Retry` 标志),
  排障时能直接定位是平台拒了还是参数错了。

**不解决什么**:

- 不管设备令牌从哪来(平台侧**还没有那张设备登记册**);
- 不做推送结果回执 / 到达率统计(那是推送平台的事);
- 不做送达保证(`Push` 返 nil 只表示**平台已接受**)。

## 配置

### yaml 侧(`push:` 块)

```yaml
push:
  provider: ""            # "" / console / aliyun-emns
  timeout: 30             # 单次推送超时(秒)
  target_max: 100         # 单次推送 target 上限(EMNS 硬限制 100)
  aliyun_emns:
    access_key: ${PUSH_ALIYUN_EMNS_ACCESS_KEY}
    access_secret: ${PUSH_ALIYUN_EMNS_ACCESS_SECRET}
    app_key: ""           # EMNS 控制台 AppKey
    region: cn-hangzhou
    default_apns_env: PROD   # DEV / PROD
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `push.provider` | string | `""` | `""` = **不装配推送通道**;`console` = 只打日志;`aliyun-emns` = 真发 |
| `push.timeout` | int(秒) | `0` → provider 默认 30s | 单次推送 HTTP 超时 |
| `push.target_max` | int | `0` → 100 | 单次推送 target 上限(EMNS 的硬限制) |
| `push.aliyun_emns.access_key` / `access_secret` | string | 空 | 阿里云凭证 |
| `push.aliyun_emns.app_key` | string | 空 | EMNS 控制台 AppKey |
| `push.aliyun_emns.region` | string | `cn-hangzhou` | 区域 |
| `push.aliyun_emns.default_apns_env` | string | `PROD` | iOS 环境 `DEV` / `PROD` |

⚠️ **`provider` 为空串 = 这条通道不存在**,不是"配好了但关着"。
理由同邮件:"配置了但没生效"比"没配"更难查 —— 运维看着 yaml 里有 `push:` 段,
以为在推,其实一个推送都没发出去。未知值(如 `foo`)只记 Warn 并**返回 nil**,不落 console 假装工作。

⚠️ **`push:` 段对应的是本包(App 推送),不是 [push](push.md)(SSE 传输层)**。
两者都在 yaml 附近,别挂错。

### 代码侧(构造选项)

```go
emns, err := pushx.NewAliyunEMNS(pushx.AliyunConfig{
    AccessKey:      "...",
    AccessSecret:   "...",
    AppKey:         "...",
    Region:         "cn-hangzhou",
    DefaultAPNSEnv: "PROD",
    Timeout:        30 * time.Second,   // 0 → 30s
    TargetMax:      100,                // 0 → 100
})
mock := pushx.NewMock()
cs := pushx.NewConsolePush()
```

`NewAliyunEMNS` 在 `AccessKey` / `AccessSecret` / `AppKey` 任一为空时返 error;
生产装配(`cmd/server` 的 `buildPushSender`)遇到这个 error 是 `logger.L().Fatal` —— 起不来。

## 使用方法

### 发一条

```go
err := sender.Push(ctx, pushx.Notification{
    Title:    "新消息",
    Body:     "您有一条新的待办事项",
    SubTitle: "RapidAdmin",
    Badge:    3,
    Targets: []pushx.Target{
        {Type: "account", Value: "user_1001"},
        {Type: "device",  Value: "dev_abc123"},
    },
    Data: map[string]string{"biz": "repair", "id": "8801"},
})
```

### 校验

```go
err := n.IsValid()   // 标题 / 正文 / targets 至少 1 个
```

非静默模式下 `Title` / `Body` 都不能为空,每个 `Target` 的 `Type` / `Value` 都不能为空。
**静默推送(`Silent: true`)不校验 `Title` / `Body`** —— 静默消息只带 `Data`,
用来在后台同步数据,不该弹通知。

### Mock(测试)

```go
mock := pushx.NewMock()
mock.FailOn = errors.New("模拟平台故障")   // 可选:让 Push 返错
_ = mock.Push(ctx, n)
mock.Count()      // 已推送条数
mock.Last()       // (Notification, bool)
mock.Reset()
```

### Console(dev)

```go
cs := pushx.NewConsolePush()
_ = cs.Push(ctx, n)
cs.Output()   // 打印内容(另存一份在内部 buffer)
```

## 注意事项

- ⚠️ **推送失败绝不能影响主流程**。这是本包最重要的语义:
  `Push` 返 error 表示**推送平台拒收**(参数错 / 鉴权失败 / 配额用完),
  调用方要把它**降级**(重试 / 走备用通道 / 写审计),
  而不是让"审批单落库"这种主流程失败。
  生产装配里 `registry.PushSender` **可能是 nil**(provider 为空串 / 未知值)——
  业务侧拿到 nil 应当**降级为不投递**,不要 panic。
- ⚠️ **`Push` 返 nil = 推送平台已接受,不代表用户已收到**。到达率由推送平台决定
  (厂商通道、系统省电策略都可能拦)。别把它当"送达成功"写进业务语义。
- ⚠️ **target 超 100 会自动切块,但错误只返回最后一块的**。
  `AliyunEMNS.Push` 把 targets 切成 `TargetMax` 一批依次发,
  `lastErr` 只保留**最后一次**的错误 —— 前面的批失败会被**静默吞掉**,
  调用方看到 nil 就以为全发出去了。要确认全量送达,得自己按批拆开调。
- ⚠️ **静默推送不校验标题 / 正文,但仍然要 targets**。
  `Silent: true` 时 `Title` / `Body` 可以为空,`Targets` 一个都不能少。
- ⚠️ **`ConsoleSender.Out` 必须是 `io.Writer`**。
  2026-10-25 修过一个真 bug:原来 `Out` 是个 `*bytes.Buffer`,默认值是个**空的内存 buffer** ——
  于是 dev 模式下的 console 推送**打印到 nowhere**:日志里一行都看不到,
  而单测因为只读 `Output()`(同一个 buffer)一直是绿的 —— 假信心。
  现在 `Out` 是 `io.Writer`(默认 `os.Stdout`),同时另存一份到内部 buffer,`Output()` 照旧能拿到内容。
- ⚠️ **`ConsoleSender` 的写入是加锁的,锁要盖住整段**。
  通知分发器的额外交付是**异步**的,两条通知可能同时打进同一个 sender;
  buf 是共享可变状态,不加锁就是数据竞争。
- ⚠️ **`pushx.ErrUnsupported` 目前没有任何实现会返回它** ——
  它是个占位符(框架未实现某 provider 时的约定)。别指望能靠 `errors.Is` 判它。
- ⚠️ **推送通道目前没有生产调用方**。推送要的是**设备令牌**
  (APNs device token / 华为小米 OPPO vivo 的 registration id),
  平台侧**还没有那张设备登记册**。所以 `push:` 段配好了也还是没人调它:
  sender 走 `registry.PushSender` 交给业务侧,等登记册落地再接。
- ⚠️ **别为了"看起来接上了"就把 pushx 注册进 [notifyx](notifyx.md) 的分发器** ——
  没有登记册,每条通知都会 skip,看着像接上了其实没发。
- ⚠️ **别和 `pkg/push` 搞混**。`pkg/pushx` 是阿里云 EMNS 的封装
  (APNs / Android 厂商通道 / Web Push),推的是**给 App 客户端**的离线通知;
  `pkg/push` 推的是**服务端到浏览器**的实时事件(SSE)。
  两个都叫"推送",但通道、投递对象、失败语义完全不同。

## 相关

- 源码:`internal/pkg/pushx/`
- 可运行示例:`examples/pushx/main.go` —— `go run ./examples/pushx`
- 服务端事件推送(另一个"推送")→ [push](push.md)
- 多通道分发(要挂 pushx 得先有设备登记册)→ [notifyx](notifyx.md)
