# 配置项全表

> yaml 配置路径 → 消费它的框架模块 → 章节。
> **逐字段的说明在 [`configs/config.example.yaml`](../../configs/config.example.yaml)** ——
> 那是唯一权威,本表只做"这个块归谁管"的索引,避免两处漂移。

## 怎么读这张表

- **块**:`config.example.yaml` 里的顶层键。
- **归属**:哪几个框架模块读它。
- **漏配后果**:这个块不配会怎样 —— 大部分都**有安全默认值**,少数字段的零值
  是"陷阱"(见 [配置体系](../getting-started/configuration.md) 里的纪律一)。

## 索引

| 块 | 归属模块 | 章节 | 漏配后果 |
|---|---|---|---|
| `system` | 路由 / 框架 | [快速开始](../getting-started/README.md) | 端口 8888,无大碍 |
| `branding` | swaggerx / 前端 | [swaggerx](../api-docs/swaggerx.md) | 前端回退到语言包(**不是**故障) |
| `swagger` | swaggerx | [swaggerx](../api-docs/swaggerx.md) | ✅ 有默认值 `enabled=true` / `auth.mode=auto` |
| `logger` | logger | [logger](../infrastructure/logger.md) | 无大碍 |
| `database` | repository | — | 无大碍 |
| `mysql` | repository | — | 连不上数据库(启动失败,响亮) |
| `redis` | cachex / kvstore | [cachex](../storage/cachex.md) | 自动降级到进程内实现 |
| `auth` | auth / anomalylogin | [auth](../security/auth.md) | ✅ `revocation_mode=auto`;`anomaly.enabled=true` |
| `kv` | kvstore | [kvstore](../storage/kvstore.md) | ✅ 默认 `auto` |
| `cache` | cachex | [cachex](../storage/cachex.md) | ✅ 默认 `auto`(只变慢,不出安全事故) |
| `job` | cronx / jobs | [cronx](../scheduling/cronx.md) | ✅ `enabled=true` / `timezone=Asia/Shanghai` / `lock_renew=true` |
| `jwt` | auth | [auth](../security/auth.md) | ⚠️ **`secret` 必须改** —— 默认值是占位值 |
| `casbin` | casbinx | [casbinx](../security/casbinx.md) | 策略加载失败(启动失败,响亮) |
| `cors` | 路由 | — | 浏览器跨域被拦(响亮) |
| `captcha` | 登录 / 发短信 | — | ✅ `login_enabled=true` / `sms_send_enabled=true` |
| `sms` | msn / smslimit / smstpl | [msn](../messaging/msn.md) | provider 留空 ⇒ 短信发不出去 |
| `email` | emailx / notifyx | [emailx](../messaging/emailx.md) | provider 留空 ⇒ **不装配**邮件通道 |
| `push` | pushx | [pushx](../messaging/pushx.md) | provider 留空 ⇒ **不装配**推送通道 |
| `oss` | oss | [oss](../storage/oss.md) | 连不上 OSS(响亮) |
| `file` | oss / media | [oss](../storage/oss.md) / [media](../storage/media.md) | ✅ `path_root=rapid-admin` / `ffmpeg_path=ffmpeg` |
| `voice` | voicex / asrx | [voicex](../voice/voicex.md) / [asrx](../voice/asrx.md) | ⚠️ `api_key` 留空 ⇒ 克隆 / 合成 / 识别**全不装配**(调用时才报错);Edge 朗读零配置可用 |
| `crypto` | crypto | [crypto](../security/crypto.md) | ⚠️ **不配就没有字段加密** —— 明文入库 |
| `kms` | kms | [kms](../security/kms.md) | 轮转检查**跳过**(不是失败) |
| `ratelimit` | ratelimitx / kvstore | [ratelimitx](../security/ratelimitx.md) | 无大碍 |
| `audit` | piiaudit / cryptaudit / smsaudit | [piiaudit](../audit/piiaudit.md) | 无大碍 |
| `otel` | otelx | [otelx](../infrastructure/otelx.md) | 不初始化(noop),链路断了 |
| `breaker` | 路由 | — | 熔断不生效 |
| `client_ip` | 中间件 | — | 全部客户端视为同一 IP(限流误伤) |
| `timeout` | 路由 | — | 无大碍 |
| `metrics` | metricsx | [metricsx](../infrastructure/metricsx.md) | `/metrics` 不挂载 |
| `crypto_keys` | crypto | [crypto](../security/crypto.md) | ⚠️ 密钥告警开关 |

> ⚠️ **Windows 上没有官方 Redis** —— `redis.addr` 要指一台真 Redis 时,Windows 上用
> [Memurai](https://www.memurai.com/get-memurai)(Redis 协议兼容,装成 Windows 服务,
> 默认 `127.0.0.1:6379`、无密码)。**Developer 版仅供开发/测试,不许上生产。**
> 详见 [快速开始](../getting-started/README.md)。

## 子块

有几块内部还有子块,单独列一下:

| 路径 | 归属 | 说明 |
|---|---|---|
| `database.sqlite` | repository | `driver=sqlite` 时用(免装 MySQL,本地开发首选) |
| `auth.anomaly` | [anomalylogin](../security/anomalylogin.md) | `enabled` / `max_concurrent` / `active_window` / `impossible_travel_window` |
| `sms.rate_limit` | [smslimit](../messaging/smslimit.md) | 三道闸的阈值 |
| `sms.template_guard` | [smstpl](../messaging/smstpl.md) | 模板发送前守卫策略 |
| `sms.templates` | [msn](../messaging/msn.md) | 各场景的验证码模板 |
| `email.smtp` | [emailx](../messaging/emailx.md) | SMTP 账号 / 端口 / TLS |
| `email.aliyun` | [emailx](../messaging/emailx.md) | 阿里云 DirectMail |
| `push.aliyun_emns` | [pushx](../messaging/pushx.md) | 阿里云 EMNS 凭证 |
| `swagger.auth` | [swaggerx](../api-docs/swaggerx.md) | `mode` / `username` / `password` |
| `file.allowed_biz` | [oss](../storage/oss.md) | 允许的业务类型白名单 |
| `voice.edge` | [voicex](../voice/voicex.md) | Edge 朗读:全空走公共通道;`token` / `wss_url` / 超时 |
| `voice.asr_model` / `asr_timeout_seconds` | [asrx](../voice/asrx.md) | 识别模型(空 = paraformer-realtime-v2)/ 任务兜底超时 |

## 密钥类配置:一律走环境变量

以下键**不要写进 yaml**(yaml 会被复制、会被备份、会被提交):

```
jwt.admin_secret / jwt.owner_secret / jwt.supplier_secret
mysql.password
redis.password
sms.access_key / sms.access_secret
voice.api_key
oss.access_key / oss.access_secret
kms.access_key / kms.secret_key
crypto.field_key / crypto.blind_key
swagger.auth.password
email.smtp.password / email.aliyun.access_secret
push.aliyun_emns.access_key / access_secret
```

写法(yaml 里引用环境变量):

```yaml
jwt:
  admin_secret: ${JWT_ADMIN_SECRET}
```

或者直接用环境变量名覆盖(viper `AutomaticEnv` + `.` → `_`):

```
JWT_ADMIN_SECRET=xxx
```

## 相关

- [配置体系](../getting-started/configuration.md) —— 加载顺序 / 路径解析 / 默认值纪律
- [框架约定](../getting-started/conventions.md)
