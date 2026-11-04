# Rapid Admin —— 后台快速开发平台 API

> **框架手册**: [`docs/`](docs/README.md) —— GitBook 56 页,讲框架层每个模块的
> 功能 / 配置 / 使用方法。本地预览 `cd docs && npm install && npm run serve`;
> 服务起来之后也能直接看 `http://127.0.0.1:8888/docs/index.html`
> (手册是用 `go:embed` 打进二进制的,与 `/swagger` 共用 BasicAuth 凭据)。

## 1. 简介

- **角色**: 通用后台管理 API(鉴权 / RBAC / 数据范围 / 文件 / 字典 / 配置 / 审计 / 消息通知 / 动态表单 / 通用审批)
- **技术栈**:
  - HTTP: [CloudWeGo Hertz](https://github.com/cloudwego/hertz)
  - ORM: [GORM](https://gorm.io) + MySQL 8
  - 缓存: [go-redis v9](https://github.com/redis/go-redis)
  - 配置: [Viper](https://github.com/spf13/viper)
  - 日志: [Uber Zap](https://github.com/uber-go/zap) + 自研按天按级别 rotator
  - 鉴权: [Casbin](https://github.com/casbin/casbin) `rbac_with_domains`
  - JWT: [golang-jwt v5](https://github.com/golang-jwt/jwt)
  - ID: [Sonyflake](https://github.com/sony/sonyflake)
- **Go 版本**: 1.22+

## 2. 仓库结构

```
server/
├── cmd/
│   ├── server/
│   │   └── main.go              # 启动入口
│   └── docs/                    # 手册构建工具(honkit → docs/site)
├── configs/
│   ├── config.example.yaml      # 配置样例
│   └── casbin_rbac.conf         # Casbin RBAC with domains 模型
├── internal/
│   ├── api/
│   │   ├── handler/             # 通用 handler 工具
│   │   │   └── v1/              # 各模块 handler
│   │   ├── middleware/          # Trace / Recover / Auth / RBAC / Audit / RateLimit
│   │   └── router/              # 路由总入口
│   ├── config/                  # viper 加载
│   ├── guard/                   # 框架原语可达性守卫(每个 internal/pkg 包都得有调用方)
│   ├── model/                   # GORM 实体
│   ├── pkg/                     # 框架层原语(49 个一级目录,每个一章手册)
│   │   ├── auth/                # JWT + bcrypt + 吊销
│   │   ├── authz/               # 资源级鉴权断言
│   │   ├── casbinx/             # Casbin 封装
│   │   ├── cachex/              # 两级缓存(L1 LRU + L2 Redis)
│   │   ├── crypto/              # 字段级加解密
│   │   ├── desensitize/         # 数据脱敏
│   │   ├── errcode/             # 业务错误码
│   │   ├── logger/              # 按天分目录按级别分文件
│   │   ├── msn/                 # 短信 provider(Sender 抽象 + aliyun/console)
│   │   ├── oss/                 # OSS provider
│   │   ├── pagination/          # 分页
│   │   ├── response/            # 统一响应
│   │   ├── smslimit/            # 短信限流(Redis per phone + per scene)
│   │   ├── sonyflake/           # 分布式 ID
│   │   ├── swaggerx/            # OpenAPI 3.0 生成 + UI 挂载
│   │   ├── timeutil/            # 东八区时间
│   │   └── …                    # 其余 33 个,见手册「包速查表」
│   ├── repository/              # DB / Redis 初始化 + cache helper
│   └── service/                 # service 层(用户 / 角色 / 审批 / 文件 / 动态表单 …)
├── examples/                    # 每个框架包一个最小可运行示例
│   ├── README.md                # 索引表(49 个包 → 这个示例演示什么)
│   └── <包名>/main.go           # go run ./examples/<包名>
├── docs/                        # 框架手册(GitBook / HonKit 源)
│   ├── SUMMARY.md               # 目录(57 页)
│   ├── book.json                # GitBook 配置(插件 / 标题 / 样式)
│   ├── package.json             # honkit 构建依赖
│   ├── <章节>/<模块>.md         # 每个框架模块一章:功能 / 配置 / 使用方法
│   ├── site/                    # 构建产物(**提交进仓库**,go:embed 进二进制)
│   └── docs.go                  # 手册包:go:embed + /docs 路由挂载
├── scripts/
│   └── seed/                    # 13 角色 + 资源 + Casbin policy 初始化
├── deploy/
│   ├── systemd/                 # systemd unit
│   ├── scripts/                 # 备份 / 恢复脚本
│   └── prometheus/              # 告警规则(5 组)
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

> **框架包示例**:`internal/pkg/` 下**每个包**在 [`examples/`](examples/README.md) 都有一个
> 最小可运行示例 —— `go run ./examples/cachex` 直接看效果,不用读源码拼初始化顺序。
> 需要 Redis / DB 的示例用内存实现顶替,**不真连**,没配也能跑通。

> **框架手册**:`docs/` 是一份完整的 GitBook(56 页),讲**框架层**每个模块的
> 功能 / 配置 / 使用方法,附 49 个可运行示例的索引。
> 本地预览:`cd server/docs && npm install && npm run serve` → http://localhost:4000
> 构建静态站:`npm run build` → server/_book/(GitBook CLI 也行:`gitbook build`)。
> 源文件就是普通 Markdown,不想装工具也能直接读。

## 3. 启动

```bash
# 1. 准备配置
cp configs/config.example.yaml configs/config.yaml
# 按需修改 MySQL / Redis / JWT / Casbin 路径

# 2. 拉依赖
go mod tidy

# 3. 建表 + 灌种子数据(13 角色 + 资源 + Casbin + 字典)
go run ./cmd/migrate -config configs/config.yaml

# 4. 启动
go run ./cmd/server -c configs/config.yaml

# 或编译后启动
make build
./bin/rapid-admin -c configs/config.yaml
```

> ⚠️ **`cmd/server` 的配置参数是 `-c`,不是 `-config`**(`cmd/migrate` / `cmd/seed`
> / `scripts/seed` 才是 `-config`)。写错会直接 `flag provided but not defined: -config`
> **启动失败**,不会退到默认配置。
> `cmd/migrate` 已含 seed;只想重灌种子(不建表)用 `go run ./scripts/seed -config configs/config.yaml`。

默认账号 `admin / Admin@2026`。⚠️ 登录时密码要**前置哈希**:`sha256("rapid-admin::" + 明文)`
的 hex —— 服务端拿你发过去的这串直接比 bcrypt,不会自己再哈希一遍。

### Redis(可选;Windows 上用 Memurai)

Redis 是**可选**的 —— 没配 `redis.addr` 时,框架层所有依赖 Redis 的能力
(`cachex` 的 L2、`kvstore` 的限流计数、`idempotentx` 的幂等键、`cronx` 的跨副本锁)
全部退到进程内实现。

⚠️ **Redis 官方不发布 Windows 版**。Windows 上用
[Memurai](https://www.memurai.com/get-memurai)(Redis 协议兼容,装成 Windows 服务,
默认 `127.0.0.1:6379`、无密码)—— go-redis v9 连上去就能用,**一行代码都不用改**。

```yaml
redis:
  addr: 127.0.0.1:6379
  password: ""        # Memurai 默认不带密码 ⇒ 留空
  db: 0
```

⚠️ **Developer 版仅供开发/测试,不许上生产**(还有"闲置 10 天自动关机"的行为)。
完整的坑位(快照策略 / 重启要管理员权限)见手册
[快速开始](docs/getting-started/README.md)。

## 4. 路由分组

所有路由挂在 `/rapid-admin/v1` 下,分组如下:

| 组 | 前缀 | 免登录 | 内容(接在前缀后面) |
| --- | --- | --- | --- |
| 公开 | `/rapid-admin/v1/public/*` | ✅ | `/auth/login` `/auth/refresh` `/auth/sms/*` `/captcha` `/config` |
| 鉴权 | `/rapid-admin/v1/auth/*` | ❌ | `/me` `/logout` `/password` `/password/reset` `/switch-identity` `/sessions` `/profile` `/avatar` `/phone` |
| 系统管理 | `/rapid-admin/v1/system/*` | ❌ | `/users` `/roles` `/resources` `/resource/accessible` `/departments` `/dicts` `/audit/*` `/flows` `/approvals` |
| 功能 | `/rapid-admin/v1/biz/*` | ❌ | `/folders` `/files` `/guides`(示例模块)` `/inbox` |
| 平台运营 | `/rapid-admin/v1/platform/*` | ❌ | `/configs` `/jobs` `/dashboard` `/sms-templates` `/wx-templates` |
| 事件流 | `/rapid-admin/v1/events` | ❌ | SSE 推送(`?access_token=`) |

> ⚠️ **「内容」列里每一项都带一个前导 `/`** —— 它是**前缀之后的那一段**,
> 拼起来才是完整路径(`/rapid-admin/v1/public/*` + `/auth/login` = `/rapid-admin/v1/public/auth/login`)。
>
> ⚠️ **别把这里的前导 `/` 去掉** —— 去掉之后这种写法会被 `internal/guard`
> 的"文档指路牌"扫描器当成**仓库文件路径**(`server/internal/pkg/auth/`
> 正好是个目录)⇒ 启动期自检报"路标断了"。这是**真事故**:守卫报的是
> "指向不存在的路径",而读者确实会去找那个文件。


另有框架级端点:`/healthz` `/readyz` `/metrics` `/swagger/*` `/docs/*`
(后者是这一本手册,构建产物用 `go:embed` 嵌在二进制里)。

### 手册怎么进二进制

```bash
make build        # = make docs + go build
make docs         # 源变了才重建手册(honkit);没变直接跳过,不需要 node
make docs-force   # 强制重建
make docs-check   # 只检查产物是否与源同步(落后则退出码 1)
```

⚠️ **手册产物 `docs/site/` 是提交进仓库的** —— 它是发布物,不是垃圾:
二进制里嵌的就是它。改了 `docs/` 下的任何 `.md` 之后要 `make docs` 重建并一起提交,
否则 `go test ./docs/` 会红(源文件指纹 vs 产物里的 `buildinfo.json`)。

## 5. 安全要点

- **SQL 注入**: 全部使用 GORM 参数化,禁止字符串拼接
- **XSS**: 响应统一 JSON,前端不直接渲染 HTML
- **CSRF**: API 无状态,使用 JWT + Authorization Bearer
- **越权**: 3 级权限(接口 RBAC + 数据级 InProject + 字段级脱敏)
- **审批强制分人**: 审批提交人 ≠ 审批人(`errcode.SameAuditor`,通用审批引擎强制)
- **敏感字段加密**: 手机号 / 邮箱等字段级 AES-256-GCM(带盲索引支持等值查询)
- **敏感查看审计**: 查看完整敏感值会写 `biz_audit_full_info_view`
- **日志脱敏**: zap 自定义 Encoder 跳过 phone/email 等字段
- **限流**: 登录/短信/上传场景基于 Redis 滑动窗口
- **审计**: 写操作自动落 `sys_operation_log` + `sys_data_change_log`
- **平台配置中心**: 唯一写通道是 `PATCH /rapid-admin/v1/platform/configs/:id`,权限码 `config:update`
  (双签工作流已移除;旧的 `SysConfigChangeRequest` 字段保留但恒为空)
- **日志按天按级别分文件**:`logs/{YYYY-MM-DD}/{info|warn|error|audit}.log`

## 6. 中间件链(执行顺序)

全局链(`internal/api/router/router.go`):

```
CORS → Trace → SecHeaders → Otel → Recover → BodyLimit → RequestGzip → Gzip
      → Idempotency → APIVersion → AccessLog → Metrics
```

受保护组(`/rapid-admin/v1`)再叠一层:

```
Auth → Principal → CSRF → Audit → [per-route: RBAC / RateLimit / Breaker / Timeout] → Handler
```

## 7. 部署

部署走 **systemd 单机**(不用 docker / k8s):unit 见
[`deploy/systemd/rapid-admin.service`](deploy/systemd/rapid-admin.service),
备份 / 恢复见 [`deploy/scripts/backup.sh`](deploy/scripts/backup.sh) 与
[`deploy/scripts/restore.sh`](deploy/scripts/restore.sh)。

> 🔴 **2026-10-31(M130)订正**:本节此前指着 `deploy/aliyun/README.md` —— **已删**
> (commit `037e2f8`,2026-10-17 改走 systemd 单机时删的)。链接一直指着一个不存在的东西。
> 阿里云那套 ECS/RDS/SLB/terraform 部署稿**现在不在本仓**(要写按单机 systemd 这条口径重写)。

## 8. 状态

- ✅ 仓库骨架
- ✅ 13 角色 + 资源 + Casbin 模型
- ✅ 配置 / 日志 / 鉴权 / 中间件 / 路由
- ✅ service 层实现(`internal/service/`;残留的 `NotImplemented` 是
  "当前 provider 不支持"这类**运行时**判定,不是待补的桩)
- ✅ 单元测试 / 集成测试(`go test ./...`)
- ✅ OpenAPI 文档生成(`internal/pkg/swaggerx`,挂 `/swagger`)
- ✅ 框架包示例(`examples/`,49 个,`go run ./examples/<包名>`)
- ✅ 框架手册(`docs/`,GitBook 57 页;`make docs` 构建 → `go:embed` 进二进制 → 挂 `/docs`)
