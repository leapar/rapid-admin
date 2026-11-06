# 包速查表

> `server/internal/pkg/` 下**全部 64 个 Go 包**的一句话索引(58 个一级目录 + 6 个子包)。
> 想知道"有没有我要的能力",先翻这张表;想知道"怎么用",点进对应章节。

命令自查:

```bash
cd server
go list -f '{{.ImportPath}} {{.Doc}}' ./internal/pkg/...
```

## 基础设施(17)

| 包 | 一句话 | 章节 |
|---|---|---|
| `logger` | 多 Core 装配:控制台 + info/warn/error/audit/access 分文件,按天按级别 rotator,脱敏钩子 | [logger](../infrastructure/logger.md) |
| `errcode` | 业务错误码:按业务域分段,`Error` 类型,code → HTTP 状态映射 | [errcode](../infrastructure/errcode.md) |
| `response` | 统一响应体:成功 / 失败 / 分页三种形状 | [response](../infrastructure/response.md) |
| `validatorx` | 统一参数校验:自定义规则(`cn_phone` / `idcard`)+ 中文错误 | [validatorx](../infrastructure/validatorx.md) |
| `pagination` | 分页参数:`PageRequest` / `Normalize` / `Offset`,`size` 上限 100 | [pagination](../infrastructure/pagination.md) |
| `jsonid` | 主键以字符串上 wire;另带一个"找出漏了 `,string` 的字段"的扫描器 | [jsonid](../infrastructure/jsonid.md) |
| `sonyflake` | 分布式 ID 生成(17 位数字,东八区起始时间)+ worker id 分配 | [sonyflake](../infrastructure/sonyflake.md) |
| `timeutil` | 统一东八区的时间工具 | [timeutil](../infrastructure/timeutil.md) |
| `buildinfo` | 编译期构建信息注入(`-ldflags -X`) | [buildinfo](../infrastructure/buildinfo.md) |
| `syncx` | 并发原语集合:单飞 / 优雅关停 / 计数信号量 / worker pool / 计数器 | [syncx](../infrastructure/syncx.md) |
| `randomx` | 安全随机 token 生成(不用 `math/rand`) | [randomx](../infrastructure/randomx.md) |
| `retryx` | 指数退避重试 + 抖动 | [retryx](../infrastructure/retryx.md) |
| `uainfo` | 解析 User-Agent,取出浏览器与操作系统 | [uainfo](../infrastructure/uainfo.md) |
| `metricsx` | Prometheus 指标注册 + `/metrics` handler | [metricsx](../infrastructure/metricsx.md) |
| `otelx` | OpenTelemetry TracerProvider 初始化(关闭时是 noop) | [otelx](../infrastructure/otelx.md) |
| `apiverx` | API 版本协商(`X-API-Version` 头) | [apiverx](../infrastructure/apiverx.md) |
| `grayx` | 灰度发布分组 header 透传 | [grayx](../infrastructure/grayx.md) |

## 存储与缓存(5)

| 包 | 一句话 | 章节 |
|---|---|---|
| `cachex` | 两级缓存(L1 本地 LRU + L2 Redis)+ 三防(单飞 / 空值占位 / TTL 抖动) | [cachex](../storage/cachex.md) |
| `kvstore` | 计数 + 滑动窗口的键值存储;限流与登录锁定都落在它上面 | [kvstore](../storage/kvstore.md) |
| `searchidx` | 加密字段的全文搜索(盲索引) | [searchidx](../storage/searchidx.md) |
| `media` | 媒体探测:ffprobe 读时长/分辨率,ffmpeg 抽封面帧 / 转码 | [media](../storage/media.md) |
| `oss` | 对象存储统一抽象(本地磁盘 / 阿里云 OSS)+ 上传凭证等高级特性 | [oss](../storage/oss.md) |

## 安全与合规(13)

| 包 | 一句话 | 章节 |
|---|---|---|
| `auth` | JWT 签发 / 解析 / 吊销 + 强制下线(`typ` 三态 + JTI + `aud`) | [auth](../security/auth.md) |
| `authz` | 资源级鉴权原语:principal / grant / 数据级断言 | [authz](../security/authz.md) |
| `casbinx` | Casbin 初始化 + 策略行格式 | [casbinx](../security/casbinx.md) |
| `crypto` | AES-256-GCM 字段级加密 + 盲索引 | [crypto](../security/crypto.md) |
| `kms` | KMS 抽象层:启动期选 provider 把 keys 灌进 `crypto.InitKeys` + 轮转 | [kms](../security/kms.md) |
| `desensitize` | 请求体 / 查询串脱敏,供审计采集端使用 | [desensitize](../security/desensitize.md) |
| `signx` | HMAC 接口签名 + 防重放 | [signx](../security/signx.md) |
| `idempotentx` | 幂等键:防重复提交(锁 + 结果缓存) | [idempotentx](../security/idempotentx.md) |
| `ratelimitx` | 进程内限流原语(令牌桶等) | [ratelimitx](../security/ratelimitx.md) |
| `loginlock` | 登录失败计数 + 账号锁定 | [loginlock](../security/loginlock.md) |
| `anomalylogin` | 异常登录检测(并发会话 / 异地 / 不可能旅行) | [anomalylogin](../security/anomalylogin.md) |
| `scope` | 数据范围(行级权限):规则 → 作用域 → WHERE 条件 | [scope](../security/scope.md) |
| `fieldpolicy` | 字段级可见性(列级权限):三态模式 + 逐字段叠加 + 响应出口脱敏 | [fieldpolicy](../security/fieldpolicy.md) |

## 审计(4)

| 包 | 一句话 | 章节 |
|---|---|---|
| `piiaudit` | PII 访问审计(等保 P0):谁在什么时候看了完整手机号 | [piiaudit](../audit/piiaudit.md) |
| `cryptaudit` | 密钥使用审计:哪个 key 被用过 / 解不开 | [cryptaudit](../audit/cryptaudit.md) |
| `smsaudit` | 短信发送审计:发了什么、谁发的、哪次请求 | [smsaudit](../audit/smsaudit.md) |
| `auditx` | 数据变更审计(sys_data_change_log)自动写入器:表级钩子记 diff | [auditx](../audit/auditx.md) |

## 消息与通知(10)

| 包 | 一句话 | 章节 |
|---|---|---|
| `inbox` | 站内信 + 通知中心:未读数 / 幂等 / 防 IDOR | [inbox](../messaging/inbox.md) |
| `notifyx` | 多通道通知分发 | [notifyx](../messaging/notifyx.md) |
| `push` | 服务端事件推送通道(SSE 的传输层地基) | [push](../messaging/push.md) |
| `pushx` | App 推送:抽象 Provider + 阿里云 EMNS | [pushx](../messaging/pushx.md) |
| `permnotify` | 权限失效广播:改权限后让前端重拉 | [permnotify](../messaging/permnotify.md) |
| `msn` | 运行时可切换的短信 sender(`aliyun` / `console`) | [msn](../messaging/msn.md) |
| `smslimit` | 短信发送限流:冷却 / 1 分钟 / 1 天三道闸 | [smslimit](../messaging/smslimit.md) |
| `smstpl` | 短信模板审核流 + 发送前守卫 | [smstpl](../messaging/smstpl.md) |
| `emailx` | 邮件下发:阿里云 DirectMail | [emailx](../messaging/emailx.md) |
| `eventx` | 领域事件总线 + 事务性 outbox + 消费侧幂等(Deduper) | [eventx](../messaging/eventx.md) |

## 调度与可观测(4)

| 包 | 一句话 | 章节 |
|---|---|---|
| `cronx` | 定时任务调度:代码注册 + DB 覆盖 + 两层去重 | [cronx](../scheduling/cronx.md) |
| `jobs` | 平台内置定时任务:框架任务注册 + 执行记录清理 | [jobs](../scheduling/jobs.md) |
| `slowquery` | 慢 SQL 报警:包装 GORM logger + Prometheus 指标 | [slowquery](../scheduling/slowquery.md) |
| `sysmetrics` | 服务器资源采样(CPU / 内存 / 磁盘),供系统监控面板 | [sysmetrics](../scheduling/sysmetrics.md) |

## 语音与媒体(2)

| 包 | 一句话 | 章节 |
|---|---|---|
| `voicex` | 百炼 CosyVoice 音色克隆 + 双通道 TTS(百炼网关 / Edge 朗读,整段 / 流式) | [voicex](../voice/voicex.md) |
| `asrx` | 百炼 Paraformer 实时语音识别(WS 逐句)+ 一句话识别,与 voicex 共用凭据 | [asrx](../voice/asrx.md) |

## 业务扩展(2)

| 包 | 一句话 | 章节 |
|---|---|---|
| `dynx` | 动态业务集合的字段类型注册表(DDL 类型 + 归一化 + 反归一化)+ 命名规则 + 动作清单 | [dynx](../business/dyn.md) |
| `dynfilter` | 动态记录筛选表达式 → 参数化 SQL(对标 PocketBase fexpr 的安全子集) | [dynfilter](../business/dynfilter.md) |

## 接口文档(1)

| 包 | 一句话 | 章节 |
|---|---|---|
| `swaggerx` | Swagger UI + 自动生成的 OpenAPI 3.0 spec(挂 `/swagger`) | [swaggerx](../api-docs/swaggerx.md) |

## 子包(6)

这些是上面某些包的 provider 实现,一般**不直接用**,由上层包装配。

| 包 | 归属 | 说明 |
|---|---|---|
| `auth/revocache` | `auth` | 跨实例 token 撤销广播 + 本地缓存 |
| `kms/local` | `kms` | Local KMS provider:从环境变量装载密钥 |
| `msn/aliyun` | `msn` | 阿里云短信 sender(依赖 alidysms SDK) |
| `msn/console` | `msn` | 控制台短信 sender:验证码直接打进日志,**仅限 dev** |
| `oss/aliyun` | `oss` | 阿里云 OSS 高级特性实现 |
| `oss/local` | `oss` | 本地磁盘 Provider |

## `internal/` 下的其它目录(不是框架原语)

| 目录 | 作用 |
|---|---|
| `internal/api` | Hertz 路由 / 中间件 / handler —— **业务接口层** |
| `internal/service` | 业务 service —— **业务代码** |
| `internal/model` | GORM 实体 —— **业务表结构** |
| `internal/repository` | DB / Redis 初始化 + cache helper |
| `internal/config` | 配置层(viper 加载 + 30 个配置块) |
| `internal/seed` | 种子数据(13 角色 / 资源树 / Casbin / 字典) |
| `internal/migrate` | AutoMigrate + 字典完整性检查 |
| `internal/guard` | 框架原语可达性守卫:每个 `internal/pkg` 包都得有调用方 |

⚠️ **`internal/pkg/` 里没有任何一个包 import `internal/service` / `internal/model` /
`internal/api` / `cmd`** —— 框架层不依赖业务层。这条由
`server/examples/examples_test.go` 的漂移网守着。
