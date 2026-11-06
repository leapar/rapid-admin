<!-- 本文件由 scripts/gen_ai_docs.py 生成 —— 手改会被覆盖;数据源见脚本文件头 -->

# module-list —— 框架模块速查

> 共 57 个框架模块(`server/internal/pkg/`),每个模块都有可运行示例与手册章节。数据源:SUMMARY.md × internal/pkg × examples 三边对齐。

| 组 | 模块 | 一句话 | 手册 | 示例 |
|---|---|---|---|---|
| 基础设施 | logger | 多 Core 装配:控制台 + info/warn/error/audit/access 分文件 | [手册](../../server/docs/infrastructure/logger.md) | `go run ./examples/logger` |
| 基础设施 | errcode | 分类、HTTP 映射、错误对象 | [手册](../../server/docs/infrastructure/errcode.md) | `go run ./examples/errcode` |
| 基础设施 | response | 成功 / 失败 / 分页三种形状 | [手册](../../server/docs/infrastructure/response.md) | `go run ./examples/response` |
| 基础设施 | validatorx | 统一参数校验:自定义规则 + 中文错误 | [手册](../../server/docs/infrastructure/validatorx.md) | `go run ./examples/validatorx` |
| 基础设施 | pagination | 分页参数解析与上限保护 | [手册](../../server/docs/infrastructure/pagination.md) | `go run ./examples/pagination` |
| 基础设施 | jsonid | 以字符串形式上 wire 的主键(雪花 ID 不能用数字) | [手册](../../server/docs/infrastructure/jsonid.md) | `go run ./examples/jsonid` |
| 基础设施 | sonyflake | 分布式 ID 生成 + worker id 分配 | [手册](../../server/docs/infrastructure/sonyflake.md) | `go run ./examples/sonyflake` |
| 基础设施 | timeutil | 统一东八区的时间工具 | [手册](../../server/docs/infrastructure/timeutil.md) | `go run ./examples/timeutil` |
| 基础设施 | buildinfo | 编译期构建信息注入 | [手册](../../server/docs/infrastructure/buildinfo.md) | `go run ./examples/buildinfo` |
| 基础设施 | syncx | 单飞 / 优雅关停 / 计数信号量 / worker pool | [手册](../../server/docs/infrastructure/syncx.md) | `go run ./examples/syncx` |
| 基础设施 | randomx | 安全随机 token 生成 | [手册](../../server/docs/infrastructure/randomx.md) | `go run ./examples/randomx` |
| 基础设施 | retryx | 指数退避重试 | [手册](../../server/docs/infrastructure/retryx.md) | `go run ./examples/retryx` |
| 基础设施 | uainfo | 解析 User-Agent,取出浏览器与操作系统 | [手册](../../server/docs/infrastructure/uainfo.md) | `go run ./examples/uainfo` |
| 基础设施 | metricsx | Prometheus 指标 + `/metrics` handler | [手册](../../server/docs/infrastructure/metricsx.md) | `go run ./examples/metricsx` |
| 基础设施 | otelx | OpenTelemetry TracerProvider 初始化 | [手册](../../server/docs/infrastructure/otelx.md) | `go run ./examples/otelx` |
| 基础设施 | apiverx | API 版本协商 | [手册](../../server/docs/infrastructure/apiverx.md) | `go run ./examples/apiverx` |
| 基础设施 | grayx | 灰度发布 header 透传 | [手册](../../server/docs/infrastructure/grayx.md) | `go run ./examples/grayx` |
| 存储与缓存 | cachex | 两级缓存(L1 本地 LRU + L2 Redis)+ 三防 | [手册](../../server/docs/storage/cachex.md) | `go run ./examples/cachex` |
| 存储与缓存 | kvstore | 计数 + 滑动窗口:Redis / 内存两种实现 | [手册](../../server/docs/storage/kvstore.md) | `go run ./examples/kvstore` |
| 存储与缓存 | searchidx | 加密字段的全文搜索(盲索引) | [手册](../../server/docs/storage/searchidx.md) | `go run ./examples/searchidx` |
| 存储与缓存 | media | 音视频处理:ffprobe 探测与 ffmpeg 转码 | [手册](../../server/docs/storage/media.md) | `go run ./examples/media` |
| 存储与缓存 | oss | 对象存储统一抽象:本地磁盘 / 阿里云 OSS | [手册](../../server/docs/storage/oss.md) | `go run ./examples/oss` |
| 安全与合规 | auth | JWT 签发 / 解析 / 吊销 + 强制下线 | [手册](../../server/docs/security/auth.md) | `go run ./examples/auth` |
| 安全与合规 | authz | 资源级鉴权原语:principal / grant | [手册](../../server/docs/security/authz.md) | `go run ./examples/authz` |
| 安全与合规 | casbinx | Casbin 初始化 + 策略行格式 | [手册](../../server/docs/security/casbinx.md) | `go run ./examples/casbinx` |
| 安全与合规 | crypto | 字段级加密(AES-256-GCM)+ 盲索引 | [手册](../../server/docs/security/crypto.md) | `go run ./examples/crypto` |
| 安全与合规 | kms | 密钥管理服务抽象层 + 轮转 | [手册](../../server/docs/security/kms.md) | `go run ./examples/kms` |
| 安全与合规 | desensitize | 请求体 / 查询串脱敏 | [手册](../../server/docs/security/desensitize.md) | `go run ./examples/desensitize` |
| 安全与合规 | signx | HMAC 接口签名 + 防重放 | [手册](../../server/docs/security/signx.md) | `go run ./examples/signx` |
| 安全与合规 | idempotentx | 幂等键:防重复提交 | [手册](../../server/docs/security/idempotentx.md) | `go run ./examples/idempotentx` |
| 安全与合规 | ratelimitx | 进程内限流原语 | [手册](../../server/docs/security/ratelimitx.md) | `go run ./examples/ratelimitx` |
| 安全与合规 | loginlock | 登录失败计数 + 账号锁定 | [手册](../../server/docs/security/loginlock.md) | `go run ./examples/loginlock` |
| 安全与合规 | anomalylogin | 异常登录检测 | [手册](../../server/docs/security/anomalylogin.md) | `go run ./examples/anomalylogin` |
| 安全与合规 | scope | 数据范围(行级权限):规则 → 作用域 → WHERE | [手册](../../server/docs/security/scope.md) | `go run ./examples/scope` |
| 安全与合规 | fieldpolicy | 字段级可见性(列级权限):三态模式 + 逐字段叠加 + 响应出口脱敏 | [手册](../../server/docs/security/fieldpolicy.md) | `go run ./examples/fieldpolicy` |
| 审计 | piiaudit | PII 访问审计:谁在什么时候看了手机号 | [手册](../../server/docs/audit/piiaudit.md) | `go run ./examples/piiaudit` |
| 审计 | cryptaudit | 密钥使用审计:哪个 key 被用过 | [手册](../../server/docs/audit/cryptaudit.md) | `go run ./examples/cryptaudit` |
| 审计 | smsaudit | 短信发送审计 | [手册](../../server/docs/audit/smsaudit.md) | `go run ./examples/smsaudit` |
| 审计 | auditx | 白名单表 GORM 变更审计:同事务 before/after 快照落 sys_data_change_log | [手册](../../server/docs/audit/auditx.md) | `go run ./examples/auditx` |
| 消息与通知 | inbox | 站内信 + 通知中心:未读数 / 幂等 / 防 IDOR | [手册](../../server/docs/messaging/inbox.md) | `go run ./examples/inbox` |
| 消息与通知 | notifyx | 多通道通知分发 | [手册](../../server/docs/messaging/notifyx.md) | `go run ./examples/notifyx` |
| 消息与通知 | push | 服务端事件推送通道(SSE 的传输层地基) | [手册](../../server/docs/messaging/push.md) | `go run ./examples/push` |
| 消息与通知 | pushx | App 推送:抽象 Provider + 阿里云 EMNS | [手册](../../server/docs/messaging/pushx.md) | `go run ./examples/pushx` |
| 消息与通知 | permnotify | 权限失效广播:改权限后让前端重拉 | [手册](../../server/docs/messaging/permnotify.md) | `go run ./examples/permnotify` |
| 消息与通知 | msn | 运行时可切换的短信 sender | [手册](../../server/docs/messaging/msn.md) | `go run ./examples/msn` |
| 消息与通知 | smslimit | 短信发送限流:冷却 / 1 分钟 / 1 天三道闸 | [手册](../../server/docs/messaging/smslimit.md) | `go run ./examples/smslimit` |
| 消息与通知 | smstpl | 短信模板审核流 + 发送前守卫 | [手册](../../server/docs/messaging/smstpl.md) | `go run ./examples/smstpl` |
| 消息与通知 | emailx | 邮件下发:阿里云 DirectMail | [手册](../../server/docs/messaging/emailx.md) | `go run ./examples/emailx` |
| 消息与通知 | eventx | 领域事件总线 + 事务性 outbox + 消费侧幂等 | [手册](../../server/docs/messaging/eventx.md) | `go run ./examples/eventx` |
| 调度与可观测 | cronx | 定时任务调度:代码注册 + DB 覆盖 + 两层去重 | [手册](../../server/docs/scheduling/cronx.md) | `go run ./examples/cronx` |
| 调度与可观测 | jobs | 平台内置定时任务:框架任务注册 + 执行记录清理 | [手册](../../server/docs/scheduling/jobs.md) | `go run ./examples/jobs` |
| 调度与可观测 | slowquery | 慢 SQL 报警:两种接入方式 + Prometheus 指标 | [手册](../../server/docs/scheduling/slowquery.md) | `go run ./examples/slowquery` |
| 调度与可观测 | sysmetrics | CPU / 内存 / 磁盘一次采样:工作台服务器监控卡片数据源 | [手册](../../server/docs/scheduling/sysmetrics.md) | `go run ./examples/sysmetrics` |
| 语音与媒体 | voicex | DashScope 音色克隆 + TTS 文本合成(整段 / 流式两套形态,不用 WebSocket) | [手册](../../server/docs/voice/voicex.md) | `go run ./examples/voicex` |
| 语音与媒体 | asrx | 百炼 Paraformer WS 实时识别 + 一句话识别(与 voicex 共用凭据) | [手册](../../server/docs/voice/asrx.md) | `go run ./examples/asrx` |
| 业务扩展 | dynfilter | 记录筛选表达式 → 参数化 SQL(结构性防注入) | [手册](../../server/docs/business/dynfilter.md) | `go run ./examples/dynfilter` |
| 接口文档 | swaggerx | Swagger UI + 自动生成的 OpenAPI 3.0 spec | [手册](../../server/docs/api-docs/swaggerx.md) | `go run ./examples/swaggerx` |

跑某模块示例:`cd server && go run ./examples/<模块名>`;新增模块的流程见 [module-add.md](module-add.md)。
