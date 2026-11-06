# examples —— 每个框架模块一个最小可运行示例

别人拿到 `pkg/cachex` 要自己读 200 行源码、拼出正确的初始化顺序、猜哪些参数不配会静默降级 —— 示例是开源框架降低上手成本最有效的一件工具。

## 约定

```
examples/<包名>/main.go     一包一个目录,package main,可 go run ./examples/<包名>
```

每个示例的**文件头注释就是它的说明书**:这个包解决什么问题、关键点是什么、跑法是什么。GitBook 文档(§8 #3)里引用的片段全部来自这里 —— **文档写的示例如果跑不起来,比没有文档更糟**。

## 怎么跑

```bash
cd server
go run ./examples/cachex
```

示例之间互不依赖,可以单独跑。需要外部基础设施的(Redis / DB / OSS)在示例里用内存实现或 `miniredis` 顶替,**不真连** —— 没配也能跑通,不会 panic。

## 硬约束(有测试守着)

| 约束 | 谁守 |
|---|---|
| `internal/pkg` 下每个顶层包都要有同名示例目录 | `examples_test.go` 的漂移网 |
| 示例目录不能有框架包对不上的多余目录 | 同上(反向漂移) |
| 每个示例必须是 `package main` + `func main()` | 同上(`go/parser` 校验) |
| 示例**不许 import 业务包**(`internal/service` / `internal/model` / `internal/api` / `cmd`) | 同上 —— 别人 clone 下来要能编译 |
| 示例必须是**能跑的代码**,不是伪代码 | 人工 review + CI 里逐个 `go run` 冒烟 |

`internal/config` **不在禁用名单里** —— 它是框架的配置层,框架自己的公开 API 就会用到它。

## 索引

### 基础设施

| 包 | 示例演示什么 |
|---|---|
| [logger](logger/main.go) | 结构化日志 + 字段约定 + 脱敏钩子 |
| [errcode](errcode/main.go) | 业务错误码的分类与统一响应 |
| [response](response/main.go) | 统一响应体(成功/失败/分页三种形状) |
| [validatorx](validatorx/main.go) | 统一参数校验(自定义规则 + 中文错误) |
| [pagination](pagination/main.go) | 分页参数解析与上限保护 |
| [jsonid](jsonid/main.go) | 以字符串形式上 wire 的主键(雪花 ID 不能用数字) |
| [sonyflake](sonyflake/main.go) | 分布式 ID 生成 + worker id 分配 |
| [timeutil](timeutil/main.go) | 统一东八区的时间工具 |
| [buildinfo](buildinfo/main.go) | 编译期构建信息注入 |
| [syncx](syncx/main.go) | 并发原语(单flight / 优雅关停 / 计数器) |
| [randomx](randomx/main.go) | 安全随机 token 生成 |
| [retryx](retryx/main.go) | 指数退避重试 |
| [uainfo](uainfo/main.go) | 解析 User-Agent |
| [metricsx](metricsx/main.go) | Prometheus 指标 + `/metrics` handler |
| [otelx](otelx/main.go) | OpenTelemetry TracerProvider 初始化 |
| [apiverx](apiverx/main.go) | API 版本协商 |
| [grayx](grayx/main.go) | 灰度发布分组透传 |

### 存储与缓存

| 包 | 示例演示什么 |
|---|---|
| [cachex](cachex/main.go) | 两级缓存(L1 本地 LRU + L2 Redis)+ 失效语义 |
| [kvstore](kvstore/main.go) | 计数 + 滑动窗口(Redis / 内存两种实现) |
| [searchidx](searchidx/main.go) | 加密字段的全文搜索(盲索引) |
| [media](media/main.go) | 音视频处理(ffmpeg 探测与转码) |
| [oss](oss/main.go) | 对象存储统一抽象(本地 / OSS) |

### 动态数据建模

| 包 | 示例演示什么 |
|---|---|
| [dynx](dynx/main.go) | 动态业务集合的字段类型注册表(DDL 类型 / 值归一化 / 反归一化) |

### 安全与合规

| 包 | 示例演示什么 |
|---|---|
| [auth](auth/main.go) | JWT 签发/解析/吊销 + 密码哈希 |
| [authz](authz/main.go) | 资源级鉴权原语(principal / grant) |
| [casbinx](casbinx/main.go) | Casbin 初始化 + 策略行格式 |
| [crypto](crypto/main.go) | 字段级加密 + 盲索引 |
| [kms](kms/main.go) | 密钥管理服务(KMS)抽象层 + 轮转 |
| [desensitize](desensitize/main.go) | 敏感字段脱敏 |
| [signx](signx/main.go) | HMAC 接口签名 + 防重放 |
| [idempotentx](idempotentx/main.go) | 幂等键(防重复提交) |
| [ratelimitx](ratelimitx/main.go) | 限流原语 |
| [loginlock](loginlock/main.go) | 登录失败计数 + 账号锁定 |
| [anomalylogin](anomalylogin/main.go) | 异常登录检测 |
| [scope](scope/main.go) | 数据范围(行级权限)原语:规则 → 作用域 → WHERE |
| [fieldpolicy](fieldpolicy/main.go) | 字段级可见性(列级权限)原语:规则 → 打码/删键 → 响应 |

### 审计

| 包 | 示例演示什么 |
|---|---|
| [piiaudit](piiaudit/main.go) | PII 访问审计(等保 P0:"谁在什么时候看了手机号") |
| [cryptaudit](cryptaudit/main.go) | 密钥使用审计(哪个 key 解不开) |
| [smsaudit](smsaudit/main.go) | 短信发送审计(装饰器接入) |

### 消息与通知

| 包 | 示例演示什么 |
|---|---|
| [inbox](inbox/main.go) | 站内信 + 通知中心(未读数 / 幂等 / 防 IDOR) |
| [notifyx](notifyx/main.go) | 多通道通知分发 |
| [push](push/main.go) | 服务端事件推送通道(SSE 的传输层地基) |
| [pushx](pushx/main.go) | App 推送(抽象 Provider) |
| [permnotify](permnotify/main.go) | 权限失效广播(改权限后让前端重拉) |
| [msn](msn/main.go) | 运行时可切换的短信 sender |
| [smslimit](smslimit/main.go) | 短信发送限流(冷却 / 1 分钟 / 1 天三道闸) |
| [smstpl](smstpl/main.go) | 短信模板审核流 |
| [emailx](emailx/main.go) | 邮件下发 |

### 调度与可观测

| 包 | 示例演示什么 |
|---|---|
| [cronx](cronx/main.go) | 定时任务调度(代码注册 + DB 覆盖 + 两层去重) |
| [jobs](jobs/main.go) | 平台内置定时任务(框架任务注册 + 执行记录清理) |
| [slowquery](slowquery/main.go) | 慢 SQL 报警(两种接入方式 + 指标) |

### 语音与媒体

| 包 | 示例演示什么 |
|---|---|
| [voicex](voicex/main.go) | DashScope 音色克隆 + TTS 合成(凭据校验 / https 闸门 / ID 归一) |

### 接口文档

| 包 | 示例演示什么 |
|---|---|
| [swaggerx](swaggerx/main.go) | Swagger UI + 自动生成的 OpenAPI 3.0 spec |
