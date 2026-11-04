# emailx —— 邮件下发:SMTP / 阿里云 DirectMail / Console / Mock

> 邮件是站内信之外的**额外交付通道**。emailx 把四种下发方式统一成一个
> `Sender` 接口,业务代码不关心用的是哪一种。

## 功能

emailx 提供一个 `Sender` 接口和四个实现:

| 实现 | 构造 | `Provider()` | 用途 |
|---|---|---|---|
| `SMTPSender` | `NewSMTP(SMTPConfig)` | `smtp` | 通用 SMTP(自建 postfix / 腾讯企业邮 / Gmail / Outlook) |
| `AliyunSender` | `NewAliyun(AliyunConfig)` | `aliyun` | 阿里云 DirectMail(HTTP REST,免 SDK) |
| `ConsoleSender` | `NewConsole()` | `console` | 只打到 stdout,dev 用 |
| `MockSender` | `NewMock()` | `mock` | 测试用:记录所有发送、可选失败 |

统一接口:

```go
type Sender interface {
    Send(ctx context.Context, msg Message) error
    Provider() string
}
```

`Message` 是自由内容(不是短信那种"模板号 + 参数"):`From` / `FromName` / `To` /
`CC` / `BCC` / `ReplyTo` / `Subject` / `HTML` / `Text` / `Headers` / `Attachments`。

**不解决**:邮件模板与渲染(正文由业务给)、退信 / 打开跟踪(阿里云侧的能力)、
验证码 TTL(短信才有那个概念)。

## 配置

### yaml 侧(`email:` 块)

```yaml
email:
  provider: ""            # "" / console / smtp / aliyun
  from: ""                # 发件邮箱(缺省取 smtp.username / aliyun.account_name)
  from_name: Admin Console
  base_url: ""            # 站点外链前缀 —— 邮件里的链接要拼成绝对地址
  timeout: 30             # 单次发送超时(秒)
  smtp:
    host: ""              # smtp.example.com
    port: 465             # 465 = use_tls / 587 = use_starttls
    username: ""
    password: ${EMAIL_SMTP_PASSWORD}
    use_tls: true
    use_starttls: false
  aliyun:
    access_key: ${EMAIL_ALIYUN_ACCESS_KEY}
    access_secret: ${EMAIL_ALIYUN_ACCESS_SECRET}
    account_name: ""      # 发信地址(需在阿里云控制台验证过)
    from_alias: ""
    region: cn-hangzhou
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `email.provider` | string | `""` | `""` / `console` / `smtp` / `aliyun`。**空串 = 不装配通道** |
| `email.from` | string | — | 发件邮箱。优先级高于 provider 自带的值 |
| `email.from_name` | string | — | 发信人显示名 |
| `email.base_url` | string | — | 站点外链前缀;邮件里的相对链接必须拼成绝对地址 |
| `email.timeout` | int | `30` | 单次发送超时(秒);`0` = 用 provider 默认 30s |
| `email.smtp.host` | string | — | SMTP 服务器 |
| `email.smtp.port` | int | `465` | `465` / `587` / `25`。代码侧 `0` ⇒ `25`(明文,只在自建内网用) |
| `email.smtp.username` / `password` | string | — | 认证凭据 |
| `email.smtp.use_tls` | bool | — | 声明为"直接 TLS(465)" |
| `email.smtp.use_starttls` | bool | — | `STARTTLS(587)` |
| `email.aliyun.access_key` / `access_secret` | string | — | DirectMail 凭据 |
| `email.aliyun.account_name` | string | — | 发信地址(需在阿里云控制台验证过) |
| `email.aliyun.from_alias` | string | — | 发信人显示名 |
| `email.aliyun.region` | string | `cn-hangzhou` | 端点 `https://dm.<region>.aliyuncs.com/` |

⚠️ **配置加载器不做 `${VAR}` 展开**(viper 没这功能)。yaml 里写的
`${EMAIL_SMTP_PASSWORD}` 只有在**对应的环境变量真的被设置**时才有意义 ——
viper 的 `AutomaticEnv` + `.`→`_` 替换会让环境变量**优先于** yaml;
环境变量没设时,这个占位符会**原样**成为密码(连接失败,而不是"没配密码")。
环境变量名:`EMAIL_SMTP_HOST` / `EMAIL_SMTP_PORT` / `EMAIL_SMTP_USERNAME` /
`EMAIL_SMTP_PASSWORD` / `EMAIL_ALIYUN_ACCESS_KEY` / `EMAIL_ALIYUN_ACCESS_SECRET` /
`EMAIL_ALIYUN_ACCOUNT_NAME`。

### 代码侧(构造选项)

```go
smtpSender, err := emailx.NewSMTP(emailx.SMTPConfig{
    Host:     "smtp.exmail.qq.com",
    Port:     465,
    Username: "noreply@example.com",
    Password: "***",
    From:     "noreply@example.com",
    Timeout:  30 * time.Second,
})

aliyun, err := emailx.NewAliyun(emailx.AliyunConfig{
    AccessKey:    "...",
    AccessSecret: "...",
    AccountName:  "noreply@example.com",
    FromAlias:    "平台",
    Region:       "cn-hangzhou",
    Timeout:      30 * time.Second,
})
```

| 配置字段 | 校验 | 默认 |
|---|---|---|
| `SMTPConfig.Host` | 空 ⇒ 构造报错 | — |
| `SMTPConfig.Port` | — | `25` |
| `SMTPConfig.From` | — | 空 ⇒ 取 `Username` |
| `SMTPConfig.Timeout` | — | `30s` |
| `AliyunConfig.AccessKey` / `AccessSecret` | 空 ⇒ 构造报错 | — |
| `AliyunConfig.AccountName` | 空 ⇒ 构造报错 | — |
| `AliyunConfig.Region` | — | `cn-hangzhou` |
| `AliyunConfig.AddressType` | — | `0` 会被改成 `1`(发信地址) |
| `AliyunConfig.Timeout` | — | `30s` |

## 使用方法

### 发送

```go
msg := emailx.Message{
    From:     "noreply@example.com",
    FromName: "Rapid Admin 后台快速开发平台",
    To:       []string{"zhangwei@example.com"},
    Subject:  "您的验证码",
    Text:     "您的验证码是 123456,10 分钟内有效。",
    HTML:     "<h1>您的验证码是 123456</h1>",
}
err := sender.Send(ctx, msg)
```

`msg.IsValid()` 是所有真 sender 的前置校验:
`From` 必填、至少 1 个收件人、`Subject` 必填、`HTML` / `Text` 至少 1 个。

### 测试用 Mock

```go
mock := emailx.NewMock()
_ = mock.Send(ctx, msg)
mock.Count()      // 已发送数
last, has := mock.Last()
mock.Reset()      // 清空记录 + 清掉 FailOn
```

### dev 用 Console

```go
console := emailx.NewConsole()
_ = console.Send(ctx, msg)   // 只打 stdout
```

### 通道分发(通知中心那条路)

邮件不是独立调用的,而是站内信的**额外交付**通道:

```go
d := notifyx.NewDispatcher()
d.Register(notifyx.NewEmailChannel(emailSender, from, fromName, baseURL))
res := d.Dispatch(ctx, recipient, msg)
```

## 注意事项

- ⚠️ **邮件发送失败**不**阻断主流程**。邮件是站内信落库之后的**异步额外交付**:
  站内信已经写进 `inbox` 表了,邮件失败只记一条 Warn
  (`notifyx: 额外交付部分失败`),**不会**让整个通知失败。
  分发器把每条通道的失败收进 `Result.Failed`,**其它通道照发**
  (`Result.OK()` = 至少一条通道投递成功)。但如果只挂了邮件一个通道,
  `Result.OK()` 就是 false —— 调用方能看出来。
  ⚠️ 与 [notifyx](notifyx.md) 的口径要对齐:通道 `Send` 返回 `ErrSkip`
  表示"这个收件人不适用这条通道"(没填邮箱),**不算失败**。
- ⚠️ **`Send` 返回 nil 只代表"服务商收下了"**,不代表邮件进了收件箱。
  业务侧的"发送成功"文案要按这个口径写。
- ⚠️ **失败方向与短信刻意相反**:
  - 短信 `provider` 为空 ⇒ **fallback 到 console**(验证码必须有去处);
  - 邮件 `provider` 为空 ⇒ **不装配通道**(返回 nil),未知值也**只 Warn 不装配**,
    **绝不落 console**。理由:邮件是额外交付,"配错了但看起来在工作"才是最坏的
    失败方向 —— 运维看着日志里一封封 `[email/console]` 会以为邮件通了,
    实际上收件人一封都没收到,而"没发出去"根本不会报错。
  - 邮件通道**配错 ⇒ 启动失败**(`Fatal`),而不是静默降级成"不发邮件"。
- ⚠️ **⚠️ `SMTPConfig.UseTLS` 声明了但没有实现**。`SMTPSender.Send` 里
  `UseTLS` 只出现在 `UseSTARTTLS && !UseTLS` 这个判断里 ——
  **代码里从来没有 `tls.Dial`**,连接是 `net.DialTimeout` 起的明文 TCP。
  所以 yaml 里 `port: 465` + `use_tls: true` **并不会**建立隐式 TLS。
  目前只有 STARTTLS 那条路是真加密的(`use_starttls: true` + 587)。
- ⚠️ **`Message.From` 会被校验,但两个真 sender 都不用它**:
  SMTP 用的是 `SMTPConfig.From`(发 `MAIL FROM` 头),阿里云用的是
  `AliyunConfig.AccountName`(发 `AccountName` 参数)。业务层看到的 `From`
  只是"必填字段",真正发出去的发信地址由配置决定。
- ⚠️ **`Message.ReplyTo` 只有 SMTP 用**(写成 `Reply-To:` 头);
  阿里云 sender 完全不读它 —— `AliyunConfig.ReplyToAddress` 非空时只往请求里塞了
  一个字符串 `"1"`(标记位),**真实地址没被送出**。要回信地址得走阿里云控制台那侧。
- ⚠️ **`Message.Attachments` 是"留接口",没有任何实现**。SMTP 的 MIME 构造
  (`buildMIME`)只拼 `multipart/alternative`(text/plain + text/html),
  阿里云 sender 也不带附件。传了附件**会被静默丢掉**,不报错。
- ⚠️ **`emailx.Option` 是个死类型**(里面只有 `Timeout`),全仓零调用方 ——
  超时由各 sender 自己的配置字段决定。
- ⚠️ **`MockSender` 不做 `IsValid()` 校验**(其余三个都做)。测试里用 mock
  断言"参数不合法应该报错"会得到 nil —— 换成真 sender 才会报错。
- ⚠️ **`EmailError.Retry` 决定值不值得重试**:网络错误 `Retry: true`,
  业务错误 `Retry: false`(阿里云业务码不重试),HTTP 5xx `Retry: true`,
  参数校验错误 `Retry: false`。
- ⚠️ **阿里云 DirectMail 的硬限制**:单邮件 1~50 个收件人(To/Cc/Bcc 合计)、
  单账号默认 500 封/天(可申请提额)、**主题与内容需审核**
  (发信前要在控制台创建发信模板)。
- ⚠️ **阿里云 `FromAlias` 的优先级**:`AliyunConfig.FromAlias` 优先,
  `Message.FromName` 只在 `cfg.FromAlias == ""` 时才被采用。
- ⚠️ **邮件正文里的链接必须是绝对地址**。收件人的邮件客户端没有"当前站点"的
  概念,相对路径点开是坏的。`notifyx` 那层用 `email.base_url` 拼;
  `base_url` 没配时**相对链接直接丢掉**(好过给一个点不开的链接)。

## 相关

- 源码:`internal/pkg/emailx/`(`smtp.go` `aliyun.go` `console.go`)
- 可运行示例:`examples/emailx/main.go` —— `go run ./examples/emailx`
- 通道分发(邮件作为额外交付通道)→ [notifyx](notifyx.md)
- 站内信 → [inbox](inbox.md)
- 短信通道(与邮件平行的另一条交付路径)→ [msn](msn.md)
- 配置体系(环境变量覆盖规则)→ [配置体系](../getting-started/configuration.md)
