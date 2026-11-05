# 通用后台管理框架手册

> 这是一份**框架级**手册 —— 讲的是"这个框架提供了哪些模块、每个模块怎么配、怎么用",
> 不讲业务。你把自己的业务模块接上来,就是你的后台。

## 开源地址

本项目已开源: **[github.com/leapar/rapid-admin](https://github.com/leapar/rapid-admin)** ——
包含 Go 后端、React 管理端、JS SDK 与这本手册的完整源码,欢迎 Star / Issue / PR。

## 这份手册是什么

`server/` 下的后台 API 是一套**通用后台管理框架**:鉴权、权限、缓存、审计、通知、
定时任务、可观测性这些"每个后台都要有、又每个后台都要重写一遍"的东西,
框架层已经写好并且配好了默认值。

框架层一共有 **51 个模块**(`internal/pkg/` 下一级目录),本文档给每个模块一份
**功能 / 配置 / 使用方法**三段式说明,外加 **51 个可以直接跑起来的示例**。

## 主要功能

51 个模块归成八组:

| 组 | 覆盖的能力 | 章节 |
|---|---|---|
| 基础设施(17) | 统一响应体 / 错误码 / 参数校验 / 分页 / 分布式 ID / 时间 / 日志 / 指标 / 链路 / 版本协商 / 灰度 | [基础设施](reference/packages.md) |
| 存储与缓存(5) | 两级缓存(L1 LRU + L2 Redis)、KV 计数与滑动窗口、加密字段盲索引、对象存储、音视频转码 | [存储与缓存](reference/packages.md) |
| 安全与合规(14) | JWT 签发与吊销、资源级鉴权、Casbin 策略、字段级加密、KMS、请求脱敏、HMAC 防重放、幂等键、限流、登录锁定、异常登录检测、登录二次验证(MFA)、数据范围(行级权限)、字段级可见性 | [安全与合规](reference/packages.md) |
| 审计(3) | PII 访问审计(谁看了完整手机号)、密钥使用审计、短信发送审计 | [审计](reference/packages.md) |
| 消息与通知(9) | 站内信与通知中心、SSE 推送、App 推送、短信、邮件、权限失效广播、模板审核、发送限流 | [消息与通知](reference/packages.md) |
| 调度与可观测(3) | 定时任务(代码注册 + DB 覆盖 + 分布式锁)、平台内置任务、慢 SQL 报警 | [调度与可观测](reference/packages.md) |
| 业务扩展(1) | 动态业务集合:用户自己设计业务表,菜单 / 接口 / 权限码 / 列表页全自动 | [dynx](business/dyn.md) |
| 接口文档(2) | OpenAPI 3.0 自动生成 + Swagger UI、这一本手册的构建与内嵌 | [Swagger UI](api-docs/swaggerx.md) / [手册](api-docs/manual.md) |

落到日常开发里,它替你把这些"每个后台都要有、又每个后台都要重写一遍"的东西做完了:

- **登录态** —— JWT 签发 / 续期 / 吊销 / 强制下线;access 与 refresh 类型隔离
  (refresh token 拿到业务接口上会被拒);改密码即踢下线。
- **权限** —— 接口级 RBAC + 资源级 ReBAC + 多身份切换;权限码从数据库下发,
  前端按同一份数据渲染按钮与菜单,不必两边各写一遍。
- **数据安全** —— 手机号 / 身份证 / 银行卡字段级 AES-256-GCM 加密;
  盲索引让**加密字段**也能精确查询与模糊搜索;日志与响应按需脱敏。
- **合规** —— 谁在什么时候看了完整手机号,有审计记录可查。
- **可靠性** —— 幂等键防重复提交、滑动窗口限流、短信三道闸、登录失败锁定、
  熔断与超时(中间件链自带)。
- **可观测** —— 分天分级别日志、Prometheus 指标、OpenTelemetry 链路、慢 SQL 报警;
  一个 trace id 把请求、SQL、日志串起来。
- **交付形态** —— OpenAPI 自动生成;Swagger UI 与这一本手册都 `go:embed` 进二进制,
  部署不需要额外进程、不需要外网。

## 优势

1. **不接业务就能用。** 框架层不 import 任何业务包(有测试守着这条硬约束):
   你把自己的业务包删掉,框架层照样编译得过、跑得起来。
2. **配错会响。** 每个配置项要么有安全默认值,要么漏配即报错 / 显式降级 ——
   没有"静默走一条你不知道的路"的开关(`job.timezone` 这种影响时间语义的除外,
   它有"必须显式配"的检查)。
3. **每个模块都有能跑的示例。** 51 个 `go run ./examples/<模块名>` 直接看效果,
   不用读源码拼初始化顺序;手册里引用的代码段就是这些示例。
4. **机械化的"文档不撒谎"。** 手册结构漂移有 `scripts/check_docs_links.py` 守着
   (SUMMARY 收录 / 链接可解析 / 引用的包与示例都真实存在);
   框架原语的可达性有 `internal/guard` 守着(写了但没人调用的包会红)。
5. **换名字就能用。** 品牌名、文档标题从 `branding:` 配置下发,不在代码里写死 ——
   这个框架的设计前提就是"改个 yaml 就能变成你的平台"。
6. **不装中间件也能跑。** Redis 可选(自动降级到进程内实现)、MySQL 可换 SQLite、
   测试与示例用内存库;单机开发、跑示例、跑单测都不需要额外服务。
7. **嵌入式交付。** Swagger UI 与手册都进二进制:离线可读、版本由仓库说了算,
   不多一个第三方 Go 依赖。

## 怎么读

| 你是谁 | 从哪读起 |
|---|---|
| 第一次接触这个框架 | [快速开始](getting-started/README.md) → [框架约定](getting-started/conventions.md) |
| 要接一个新业务模块 | [框架约定](getting-started/conventions.md) → [权限](security/authz.md) → [审计](audit/piiaudit.md) |
| 要改某个框架模块的行为 | 直接跳到该模块那一章 |
| 只是想知道"有没有我要的东西" | [包速查表](reference/packages.md) |
| 想看代码怎么跑 | [示例目录](reference/examples.md) |

## 文档与代码的关系

**文档里出现的每一段代码,都是能跑的。**

- 每章的「使用方法」小节引用的完整示例在 `server/examples/<模块名>/main.go`,
  `go run ./examples/<模块名>` 就能看到输出。
- 本手册写作时的纪律:**先有能跑的示例,再有文档**。示例本身由
  `server/examples/examples_test.go` 守着(新增框架模块却忘了写示例 ⇒ 测试红)。

```
server/
├── internal/pkg/<模块名>/     # 框架模块本体
├── examples/<模块名>/main.go   # 该模块的最小可运行示例(本文档引用它)
└── docs/                       # ← 你正在读的手册
```

## 三个贯穿全书的前提

1. **配置全部有默认值。** 不配也能跑;配错了会**显式报错或显式降级**,
   不会静默走一条你不知道的路。(例外:`job.timezone` 这种,配错会让任务在
   错误的时间跑 —— 所以它有"必须显式配"的检查。)
2. **框架模块不依赖业务。** `internal/pkg/` 里没有任何一个包 import
   `internal/service` / `internal/model` / `internal/api` / `cmd` ——
   这是 `examples/examples_test.go` 里的漂移网在守的硬约束。
   你把自己的业务包删掉,框架层照样编译得过、跑得起来。
3. **平台名是配置项,不是常量。** 品牌名、Swagger 标题都从
   `branding:` 配置下发,不在代码里写死 —— 这个框架是要被"换个名字就能用"的。

## 环境要求

| 项 | 版本 |
|---|---|
| Go | 1.22+ |
| 数据库 | MySQL 8(示例与测试用 SQLite,不真连) |
| 缓存 | Redis 7+(没有也能跑,框架层自动降级到进程内) |
| 媒体处理 | ffmpeg / ffprobe(只有 `media` 模块需要) |

## 怎么本地读这本手册

用 [GitBook](https://github.com/GitbookIO/gitbook-cli) 或
[HonKit](https://github.com/honkit/honkit) 都能构建:

```bash
cd server/docs
npm ci                            # 装 honkit 与插件(第一次要装)
npm run build                     # = honkit build . site  → 产物在 site/
npm run serve                     # 本地预览,带热重载 → http://localhost:4000
```

在 `server/` 下还有一层封装(源变了才重建、跟二进制联动):

```bash
make docs                         # 源变了才重建(honkit);没变直接跳过
make docs-force                   # 强制重建
make docs-check                   # 只检查产物是否与源同步(落后则退出码 1)
```

不想装工具也行 —— 这些 `.md` 就是普通 Markdown,任何编辑器都能读。

服务跑起来之后,手册也在 `http://127.0.0.1:8888/docs/index.html`:
它是用 `go:embed` 打进二进制的,部署时不需要 node 也不需要外网。
细节见 [框架手册](api-docs/manual.md)。
