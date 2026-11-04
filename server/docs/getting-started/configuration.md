# 配置体系

> 一句话:配置是 **yaml + 环境变量**两层,所有相对路径在加载期钉成绝对路径,
> 所有"漏配"都被显式处理成"照常跑"而不是"静默变弱"。

## 配置从哪来

```
configs/config.yaml          ← 本地配置(gitignore,含密码 / AK-SK)
        ↓  viper
环境变量                       ← 优先级更高,用于注入密钥
        ↓  config.Load()
config.Server                ← 内存里的强类型结构体
```

- **加载入口**:`config.Load(path)` —— 只认 `-config` 指向的那个 yaml 文件。
- **全局单例**:`config.Get()` 返回 `*config.Server`,`config.AppViper` 是底层 viper。
- **热重载**:yaml 改了会重新 Unmarshal 并重新解析路径。

⚠️ **Windows 上改 yaml 不会热生效**。viper 的热重载依赖 `fsnotify`,而 Windows
上编辑器的"原子替换"(写临时文件再 rename)会把 watcher 打瞎。**改配置请重启进程。**

## 三个必须知道的概念

### ① 相对路径一律相对 `rootDir`,不是相对 cwd

```go
config.Root()      // 根目录:APP_ROOT 环境变量 > 进程 cwd
config.ConfigFile() // 实际加载的配置文件绝对路径
```

同一个 server 从不同目录启动(`go run` / VSCode F5 / systemd `WorkingDirectory`),
落盘位置必须一样 —— 所以 `logger.dir`、`casbin.model_path`、`database.sqlite.path`
这些都在 `Load()` 里被钉成绝对路径。

想显式指定根目录:

```bash
APP_ROOT=/opt/rapid-admin go run ./cmd/server -c configs/config.yaml
```

### ② 环境变量覆盖:`${VAR}` 与自动映射

```yaml
database:
  mysql:
    password: ${MYSQL_PASSWORD}
```

- yaml 里的 `${VAR}` 会被 viper 展开成环境变量的值。
- `AutomaticEnv()` + `SetEnvKeyReplacer("." → "_")` 让**每个配置键**都能被环境变量覆盖:
  `database.mysql.password` → 环境变量 `DATABASE_MYSQL_PASSWORD`。
- **密钥建议一律走环境变量**,别写进 yaml(yaml 会被复制、会被备份、会被提交)。

### ③ "漏配"必须等于"照常跑"

这是本框架配置层的第一纪律,也是最容易踩的地方。

Go 的 bool 零值是 `false`。如果"没配"和"配成 false"是同一件事,那么:

- 漏配 `captcha.login_enabled` → 验证码校验**静默消失**;
- 漏配 `job.enabled` → 一个定时任务都不跑,而且**没有任何症状**(页面能开、接口能通);
- 漏配 `swagger.enabled` → 文档端点静默 404。

所以配置层给这些键**显式设了安全默认值**:

| 键 | 默认值 | 漏配的后果(如果不设默认) |
|---|---|---|
| `captcha.login_enabled` | `true` | 登录不校验验证码 |
| `captcha.sms_send_enabled` | `true` | 发短信不校验验证码 |
| `auth.revocation_mode` | `auto` | token 吊销判定行为不明 |
| `auth.anomaly.enabled` | `true` | 异常登录检测静默不跑 |
| `auth.anomaly.max_concurrent` | `3` | 兜底成 1 ⇒ 多人多端登录全被判异常 |
| `auth.anomaly.active_window` | `30m` | 兜底成 0 ⇒ 永远不判同时在线 |
| `job.enabled` | `true` | 一个定时任务都不跑 |
| `job.lock_renew` | `true` | 长任务跑到一半锁过期,副本并发写 |
| `job.timezone` | `Asia/Shanghai` | 容器里 TZ 没设 ⇒ 任务在北京时间 11:00 跑 |
| `swagger.enabled` | `true` | `/swagger` 静默 404 |
| `swagger.auth.mode` | `auto` | 空串 = "没配凭据" ⇒ 文档端点裸奔 |

> ⚠️ `swagger.auth` **同时管着 `/docs`(框架手册)** —— 两者共用同一套 BasicAuth
> 凭据(见 [框架手册](../api-docs/manual.md))。`swagger.enabled` 只管 `/swagger`:
> 关掉它,手册照旧在 `/docs` 上。

反过来,**默认值必须是"安全的那一侧"**:

- `cache.mode` 默认 `auto`(有 Redis 用 Redis,没有退纯 L1)——
  缓存失效只会变慢,不会出安全事故,所以漏配无所谓。
- `auth.anomaly.impossible_travel_window` 默认 `0s`(**关闭**)——
  没有 IP 地理库,开着必然误报。这是有意关掉的,不是漏配。

## 配置分块

`config.Server` 一共 30 个块(见 `internal/config/` 下一个文件一个块):

| 块 | 归属模块 | 作用 |
|---|---|---|
| `system` | — | 端口、模式、时区、超时、请求体上限 |
| `branding` | [swaggerx](../api-docs/swaggerx.md) | 平台名 / 浏览器标题(**换名字改这里**) |
| `swagger` | [swaggerx](../api-docs/swaggerx.md) | 文档端点开关 + 鉴权方式 |
| `logger` | [logger](../infrastructure/logger.md) | 级别、格式、目录、按天/按级别分文件、轮转 |
| `database` / `mysql` | — | driver 选择 + 连接池 |
| `redis` | [cachex](../storage/cachex.md) / [kvstore](../storage/kvstore.md) | 连接地址、DB 号、密码 |
| `jwt` | [auth](../security/auth.md) | 签名密钥、access / refresh 有效期、签发方受众 |
| `auth` | [auth](../security/auth.md) / [anomalylogin](../security/anomalylogin.md) | 吊销存储模式、异常登录检测 |
| `kv` | [kvstore](../storage/kvstore.md) | 限流 / 登录锁定的存储模式(`auto`/`redis`/`memory`) |
| `cache` | [cachex](../storage/cachex.md) | 两级缓存:L1 容量 / TTL、空值 TTL、抖动 |
| `job` | [cronx](../scheduling/cronx.md) / [jobs](../scheduling/jobs.md) | 调度开关、时区、锁、执行记录保留 |
| `casbin` | [casbinx](../security/casbinx.md) | 模型文件路径、自动加载策略 |
| `cors` | — | 允许的 Origin、方法、Header |
| `captcha` | — | 登录 / 发短信两个独立开关 |
| `sms` | [msn](../messaging/msn.md) | provider(aliyun/console)、签名、模板 |
| `email` | [emailx](../messaging/emailx.md) | provider、账号、域名 |
| `push` | [pushx](../messaging/pushx.md) | provider、APP 凭证 |
| `oss` | [oss](../storage/oss.md) | provider(local/aliyun)、bucket、endpoint |
| `file` | [oss](../storage/oss.md) / [media](../storage/media.md) | key 根、大小上限、ffmpeg 路径、会话 TTL |
| `crypto` | [crypto](../security/crypto.md) | 字段加密开关、算法 |
| `kms` | [kms](../security/kms.md) | provider(local)、轮转周期 |
| `ratelimit` | [ratelimitx](../security/ratelimitx.md) | 限流开关与配额 |
| `audit` | [piiaudit](../audit/piiaudit.md) | 审计开关与批量大小 |
| `otel` | [otelx](../infrastructure/otelx.md) | 开关、endpoint、采样率 |
| `breaker` | — | 熔断阈值与恢复窗口 |
| `client_ip` | — | 可信反向代理网段(决定 `ClientIP` 怎么取) |
| `timeout` | — | 各类请求超时 |
| `metrics` | [metricsx](../infrastructure/metricsx.md) | Prometheus 开关、路径 |
| `crypto_keys` | [crypto](../security/crypto.md) | 各业务字段用哪把 key |

每个块的字段级说明见 `configs/config.example.yaml`(那是**唯一**逐字段的说明文件,
本手册不重复抄,免得两边漂移)。

> ⚠️ **Windows 上没有官方 Redis** —— `redis.addr` 要指一台真 Redis 时,Windows 上用
> [Memurai](https://www.memurai.com/get-memurai)(Redis 协议兼容,装成 Windows 服务,
> 默认 `127.0.0.1:6379`、无密码)。**Developer 版仅供开发/测试,不许上生产。**
> 详见 [快速开始](README.md)。

## 加一个新配置块的步骤

1. 在 `internal/config/` 加一个文件,定义 `XxxConfig` struct,字段带 `mapstructure` tag;
2. 在 `config.Server` 里加一个字段,tag 是 yaml 里的块名;
3. 在 `configs/config.example.yaml` 写默认值 + 注释(**注释就是文档**);
4. 如果漏配会有危险,在 `config.Load()` 里 `v.SetDefault(...)` 显式钉住;
5. 消费方**只用 `config.Get().Xxx`**,不要自己再读一遍 yaml。

## 相关

- [配置项全表](../reference/config-index.md) —— yaml 路径 → 框架模块对照
- [框架约定](conventions.md) —— 约定层面(响应体 / 错误码 / 字段命名)
