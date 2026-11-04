# smsaudit —— 短信发送审计:发了什么、发给谁、成没成

> "这条验证码到底发出去了没有""用户说没收到短信,到底是服务商拒了还是我们根本没发" ——
> 每次发送都要留痕:手机号、场景、验证码、provider、成没成、耗时多少、上游 request_id。

## 功能

smsaudit 解决一件事:**把"每一次短信发送的完整轨迹"变成可查的表**。

能力:

1. **装饰器接入(推荐)** —— `smsaudit.Wrap(realSender)` 包一层,发送结果自动落审计,
   耗时也由装饰器量,业务代码一行不用改。
2. **手工记录** —— 不走装饰器时用 `smsaudit.Record(ctx, smsaudit.SendLog{...})`。
3. **失败也落库** —— `Success=false` + `ErrorMsg` + 服务商返回的原文。
4. **异步攒批** —— 100 条或 5s 落一次库,单次插入按 50 条分片。
5. **ctx 上下文自动带** —— principal / request_id / trace_id / ip / user_agent。

不解决:

- 不拦发送 —— 限流在 [smslimit](../messaging/smslimit.md),模板审核在
  [smstpl](../messaging/smstpl.md)。
- 不负责选服务商 —— 那是 [msn](../messaging/msn.md);本包只是给 sender 包一层。
- 不做脱敏 —— `phone` 和 `code` 都**明文入库**(产品要求:用户报"收到的不是这个"时能
  反查)。代价是这张表本身就是敏感数据,查询接口要收权限。

## 配置

### 代码侧(没有构造选项)

smsaudit 是包级单例:

| 符号 | 说明 |
|---|---|
| `smsaudit.Init(db *gorm.DB)` | 启动期调一次(`sync.Once` 包着)。`db == nil` ⇒ 打一条 Warn(`smsaudit: db is nil, recorder disabled`)并保持关闭(**不起后台 loop**) |
| `smsaudit.Shutdown()` | 退出前调一次,等最后一批 flush 完。**幂等** |
| `smsaudit.Wrap(inner msn.Sender) msn.Sender` | 装饰器。装配期包一层即可 |

攒批参数是**包内常量**,不可配:`defaultBatchSize = 100`、`defaultFlushInterval = 5s`、
`defaultRetryTimes = 3`、单次插入分片 `CreateInBatches(batch, 50)`。

### yaml 侧(`audit:` 块)

`configs/config.example.yaml` 里确实有一个 `audit:` 块:

```yaml
audit:
  batch_size: 100
  flush_interval: 5000   # ms(整数不带单位 —— 写成 "5s" 会让整份配置反序列化失败)
  queue_size: 10000
```

⚠️ **这三个键本包不读**。`config.AuditConfig`(`internal/config/crypto.go`)定义了它们,
但全仓**零消费方** —— 三个审计包的攒批参数都写死在包内常量里。**改 yaml 不生效**。

## 使用方法

### 装配(装饰器,推荐)

真实 sender 的构造在 `cmd/server/main.go` 的 6.5 步,`smsaudit.Wrap` 是最后一步:

```go
rawSmsSender := buildSmsSender(cfg.SMS, ccSvc.ResolveSMSConfig, smsTplRuntime.Guard())
smsSender := smsaudit.Wrap(rawSmsSender)   // 装饰:计时 + 写审计
```

装配期包一层,再把 `smsSender` 传给任何要发短信的地方(它仍是 `msn.Sender` 接口)。
装饰器包一层就够,业务侧还是只认 `msn.Sender`:

```go
var sender msn.Sender = smsaudit.Wrap(real)
err := sender.Send(ctx, "13800001234", "123456", "login")
```

### 手工记录

```go
smsaudit.Record(ctx, smsaudit.SendLog{
    Phone:    "13800001234",
    Scene:    "login",
    Code:     "123456",
    Provider: "aliyun",
    Success:  true,
    CostMs:   234,
})
```

### `SendLog` 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `Phone` | `string` | 手机号(明文入库) |
| `Scene` | `string` | 业务场景(`login` / `reset_password` / ...) |
| `Code` | `string` | 验证码(明文入库,供反查) |
| `Provider` | `string` | `aliyun` / `console` |
| `Success` | `bool` | 成没成 |
| `ErrorMsg` | `string` | 失败原文 |
| `CostMs` | `int` | 下发耗时(ms) |
| `UpstreamReqID` | `string` | 上游 request_id(阿里云那边给的) |

### 完整可运行示例

`examples/smsaudit/main.go` —— `go run ./examples/smsaudit`

示例里用一个假 sender 演示"成功 / 失败"两条路径,手写 DDL 建表(框架示例**不许
import 业务模型**)。

## 注意事项

- ⚠️ **ctx key 已统一到 piiaudit(2026-11-01 修的坑)**。本包原来**自己定义了一套私有
  `ctxKeyRequestID{}` / `ctxKeyTraceID{}`**,而全仓唯一注入这两样东西的地方是
  `internal/api/middleware/trace.go`,它用的是 **piiaudit** 的 key
  (`piiaudit.InjectRequestID` / `InjectIP` / `InjectUA`)。两套 key 谁也读不到谁 ⇒
  `sys_sms_send_log.request_id` / `trace_id` 在生产里**恒为空**;ip / user_agent 更是
  **从来没读过**(表里有那两列,兄弟包 cryptaudit 一直在读同一套 key)。
  现在本包的 `InjectRequestID` / `InjectTraceID` / `InjectIP` / `InjectUA` 全部转发给
  piiaudit,读写同一套 key。
  ⚠️ **自建包不要另起一套 ctx key** —— 全仓只有 `middleware/trace.go` 一处注入,
  键名对不上就全空,而且是静默全空。
- ⚠️ **`Shutdown()` 幂等是后补的**。兄弟包 piiaudit / cryptaudit 在 M100 就改成幂等了,
  本包漏了,2026-11-01 才补(原来第二次调用 `close` 已关闭的 channel → panic)。
  幂等的 `sync.Once` 放在 **`Recorder` 上**而不是包级变量 —— 每个 Recorder 各自
  只关一次,换 `global` 也不会误伤别的实例。
- ⚠️ **装饰器不吞错误**。底层 `Send` 返什么错,装饰器原样往上抛 —— 审计只是旁路,
  不是替业务做决定。
- ⚠️ **装饰器会额外打一条 Warn**(`sms send failed`),里面带 **明文 phone**。
  审计表里手机号明文是有意的(合规要求能反查),但日志里最好打码 —— 别让完整手机号
  跟着日志跑到 ELK 里。
- ⚠️ **装饰器不填 `UpstreamReqID`**。阿里云返回的上游 request_id 装饰器拿不到,那一列
  会是空串;需要它就得自己 `Record` 手工填。
- ⚠️ **别 wrapper of wrapper**。包两次就会同一次发送记两条。
- ⚠️ **`code`(验证码)也明文入库**。`sys_sms_send_log.code` 存的是真实下发的值 ——
  这是"用户报收到的不是这个"时唯一的反查依据,但也意味着**这张表里有明文验证码**。
  别给它开放导出接口。
- ⚠️ **本包不记角色**。`SysSmsSendLog` 没有 `role_code` 列(`extractFromCtx` 也不取
  `RoleCode`),查"谁发的"只有 `user_id` / `identity_id`;兄弟包 piiaudit / cryptaudit
  是有 `role_code` 的。
- ⚠️ **落库是异步的**。`Record` 只往内存缓冲里塞。进程退出前不调 `Shutdown()`,最后
  一批(最多 99 条)会丢。
- ⚠️ **`Shutdown()` 之后还能 `Record`,但大多数数据不会落库**。后台 flush loop 已退出,
  不再有 5s 定时 flush;只有攒到 100 条触发 `append()` 里的同步 flush 分支才会写下去。
- ⚠️ **忘了 `Init` ⇒ `Record` 静默 no-op**(`if global == nil { return }`),连日志都没有。
  只有 `Init(nil)` 才会打那条 Warn。
- ⚠️ **`Init` 只能生效一次**(`sync.Once` 包着)。测试里想换个 db 重新 `Init` 是无效的,
  静默 no-op。三个兄弟包都一样。

## 相关

- 源码:`internal/pkg/smsaudit/`
- 可运行示例:`examples/smsaudit/main.go` —— `go run ./examples/smsaudit`
- 兄弟包:[piiaudit](piiaudit.md) / [cryptaudit](cryptaudit.md)
- 短信发送链路:[msn](../messaging/msn.md)(sender 抽象)
