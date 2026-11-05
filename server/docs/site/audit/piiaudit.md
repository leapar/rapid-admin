# piiaudit —— PII 访问审计:谁在什么时候看了手机号

> 等保 P0 项(§1.5 #5)。任何地方解了 PII(手机号 / 身份证 / 银行卡 / 住址),
> 就调一行 `piiaudit.Record(ctx, ...)`;攒批、落库、重试由框架负责。

## 功能

piiaudit 解决一件事:**把"谁在什么时候看了哪个 PII 字段"变成可查的表**。

能力:

1. **一行记录** —— 业务代码只给 payload(`Action` / `ResourceType` / `ResourceID` /
   `Field` / `Reason`);主体和请求上下文(principal / request_id / trace_id / ip /
   user_agent)框架从 ctx 里提。
2. **异步攒批** —— 100 条或 5s 落一次库,单次插入按 50 条分片(`CreateInBatches`)。
   业务请求不阻塞。
3. **失败重试 3 次** —— 仍失败打一条 Error 日志并丢弃整批。
4. **列表 / 导出按行计** —— `RecordMany` 里一条 `Access` 落一行("每行算 1 条"是产品意图)。
5. **trace_id 兜底** —— 读不到自己的 ctx key 时,退到 OTel span context 取。

不解决:

- 不做**写操作**审计 —— 那是 `middleware.Audit()` 写 `sys_operation_log` /
  `sys_data_change_log` 的事。
- 不做**字段级脱敏 / 加密** —— 那是 [crypto](../security/crypto.md) /
  [desensitize](../security/desensitize.md)。piiaudit 只记"看过",不记"看到的是什么"。
- 不做鉴权,也不拦访问 —— 它是旁路,记录既成事实。

## 配置

### 代码侧(没有构造选项)

piiaudit 是包级单例,只有两个生命周期函数:

| 符号 | 说明 |
|---|---|
| `piiaudit.Init(db *gorm.DB)` | 启动期调一次(`sync.Once` 包着)。`db == nil` ⇒ 打一条 Warn(`piiaudit: db is nil, recorder disabled`)并保持关闭(部署形态可以整体关停 PII 审计) |
| `piiaudit.Shutdown()` | 退出前调一次,等最后一批 flush 完。**幂等** |

攒批参数是**包内常量**,不可配:

| 常量 | 值 |
|---|---|
| `defaultBatchSize` | 100 |
| `defaultFlushInterval` | 5s |
| `defaultRetryTimes` | 3 |
| 单次插入分片 | `CreateInBatches(batch, 50)` |

### yaml 侧(`audit:` 块)

`configs/config.example.yaml` 里确实有一个 `audit:` 块:

```yaml
audit:
  batch_size: 100
  flush_interval: 5000   # ms(整数不带单位 —— 写成 "5s" 会让整份配置反序列化失败)
  queue_size: 10000
```

⚠️ **这三个键本包不读**。`config.AuditConfig`(`internal/config/crypto.go`)定义了它们,
但全仓**零消费方** —— piiaudit / cryptaudit / smsaudit 的攒批参数都写死在包内常量里。
**改 yaml 不生效**,要调攒批节奏只能改代码里的常量。

## 使用方法

### 装配

```go
piiaudit.Init(repository.GetDB())
defer piiaudit.Shutdown()
```

### 记一次

```go
piiaudit.Record(ctx, piiaudit.Access{
    Action:       model.PIIActionRead,
    ResourceType: "user",
    ResourceID:   user.ID,
    Field:        model.PIIFieldPhone,
    Reason:       "客服来电查询",
})
```

`Action` 留空默认 `model.PIIActionRead`。

### 列表 / 导出(每行算 1 条)

```go
piiaudit.RecordMany(ctx, []piiaudit.Access{
    {Action: model.PIIActionExport, ResourceType: "user", ResourceID: 20260302, Field: model.PIIFieldIDCard},
    {Action: model.PIIActionExport, ResourceType: "user", ResourceID: 20260303, Field: model.PIIFieldIDCard},
})
```

### ctx 里要有什么

piiaudit **只读 ctx,不写 ctx**。生产上这些值由中间件注入:

| ctx 值 | 注入方 | 读不到时 |
|---|---|---|
| principal(user / identity / role) | `authz.WithPrincipal` —— principal 中间件 | 主体三列全 0 |
| request_id | `piiaudit.InjectRequestID` —— `middleware.Trace()` | 空串 |
| trace_id | `piiaudit.InjectTraceID`,或 OTel span context | 空串 |
| ip | `piiaudit.InjectIP` —— `middleware.Trace()` | 空串 |
| user_agent | `piiaudit.InjectUA` —— `middleware.Trace()` | 空串 |

测试 / 示例里自己注入:

```go
ctx = authz.WithPrincipal(ctx, &authz.Principal{UserID: 8801, IdentityID: 8801, RoleCode: "ops_manager"})
ctx = piiaudit.InjectRequestID(ctx, "req-20261101-0002")
ctx = piiaudit.InjectTraceID(ctx, "trace-123456abcdef")
ctx = piiaudit.InjectIP(ctx, "10.0.0.8")
ctx = piiaudit.InjectUA(ctx, "Mozilla/5.0")
```

### 完整可运行示例

`examples/piiaudit/main.go` —— `go run ./examples/piiaudit`

示例里手写 DDL 建表(框架示例**不许 import 业务模型**,`internal/model` 不在范围内),
`action` / `field` 写的是字面量;真实业务代码请用 `model.PIIActionRead` 这类常量。

## 注意事项

- ⚠️ **主键必须自己填(雪花)**。`model.BaseModel.ID` 的 tag 是
  `primaryKey;autoIncrement:false` —— GORM 因此**不会**让库自增,零值 0 原样入库。
  而 id 是主键 ⇒ **第二批往后的插入全部撞 "UNIQUE constraint failed"** ⇒ 整批重试
  3 次后被丢掉(只留一条 Warn)。后果是 **`sys_pii_access_log` 永远只能存下 1 行** ——
  而它是 §1.5 #5 的 P0 等保项:表在、行有,但**查不出东西**。
  本包用 `model.BaseModel{ID: sonyflake.Next()}` 补上了(M100 修的),改这段代码时别删。
- ⚠️ **攒到第 100 条时是同步 flush**(不是 `go flush()`)。这条业务请求会被
  `CreateInBatches` 阻塞几十毫秒(兄弟包 cryptaudit 用的是 `go flush()`,异步)。
  绝大多数请求碰不到这个阈值,但批量导出场景每 100 条会卡一次。
- ⚠️ **落库是异步的**。`Record` 只往内存缓冲里塞。进程退出前不调 `Shutdown()`,
  最后一批(最多 99 条)会丢。
- ⚠️ **`Shutdown()` 是幂等的**(M100 改的)。原来第二次调用会 `close` 已关闭的 channel →
  `panic("close of closed channel")`;`defer piiaudit.Shutdown()` + 显式调一次是很容易
  写出来的组合,现在可以放心写。
- ⚠️ **`Shutdown()` 之后还能 `Record`,但大多数数据不会落库**。后台 flush loop 已经退出,
  不再有 5s 定时 flush;只有把缓冲攒到 100 条、触发 `append()` 里的同步 flush 分支才会
  写下去。退出路径上 `Shutdown()` 之后不要再记审计。
- ⚠️ **`ctx` 传 nil 不 panic,但也不会有上下文**。自动降级成 `context.Background()`,
  所有上下文列都是空串 —— 审计路径不该把业务请求带崩(后台 goroutine 里 panic = 进程退出)。
- ⚠️ **忘了 `Init` ⇒ `Record` 静默 no-op**。`if global == nil { return }`,连日志都没有。
  只有 `Init(nil)` 才会打那条 Warn。排查"审计表里没数据"时先确认 `Init` 被调过。
- ⚠️ **`Init` 只能生效一次**(`sync.Once` 包着)。测试里想换个 db 重新 `Init` 是无效的,
  静默 no-op。三个兄弟包都一样。
- ⚠️ **`flush()` 不能复用同一块底层数组**。现在写的是 `r.batch = nil` 而不是 `r.batch[:0]`:
  flush 释放锁之后才去 `CreateInBatches`(那一段要几十毫秒),这期间 `append()` 会往
  **同一块数组**里写新行 ⇒ 正在被 GORM 读的那批数据被**就地改写**。后果有两个 ——
  数据竞争(-race 一抓一个准),以及落库的行**内容可能是别的行的**(审计记录写着"张三
  看了手机号",字段却是李四那行的)。改这块代码时别"优化"回 `[:0]`。
- ⚠️ **trace_id 有两套键**。本包的 ctx key 和 OTel span context 是**两套完全不同的键**,
  所以做了兜底:先读自己的 key(显式注入优先),读不到再从
  `trace.SpanContextFromContext(ctx)` 取。`InjectTraceID` 全仓零调用方,生产里的
  trace_id 实际来自 otel 中间件起的那条链。
- ⚠️ **`action` / `field` 的取值是字符串常量,在 `internal/model` 里**:
  `model.PIIActionRead` / `PIIActionList` / `PIIActionExport` / `PIIActionDecrypt`,
  `model.PIIFieldPhone` / `PIIFieldIDCard` / `PIIFieldEmail` / `PIIFieldBankCard` /
  `PIIFieldAddress` / `PIIFieldName`。业务代码用常量 —— 写字面量不会编译报错,只会让
  等保报表按 `action` 聚合时对不上。
- ⚠️ **`RecordMany` 是"每行 1 条"**,不是"1 条批量"。列表页导出了 5000 行就写 5000 行,
  这张表的增长速度比想象快,要看数据保留策略。

## 相关

- 源码:`internal/pkg/piiaudit/`
- 可运行示例:`examples/piiaudit/main.go` —— `go run ./examples/piiaudit`
- 兄弟包:[cryptaudit](cryptaudit.md) / [smsaudit](smsaudit.md)
- 字段加密与脱敏:[crypto](../security/crypto.md) / [desensitize](../security/desensitize.md)
- 主体来源:[authz](../security/authz.md)
