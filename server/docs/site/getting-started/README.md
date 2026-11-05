# 快速开始

> 目标:十分钟内让这套框架在你机器上跑起来,并看到一个真实的接口响应。

## 0. 前置

| 项 | 版本 | 没有会怎样 |
|---|---|---|
| Go | 1.22+ | 装不上 |
| MySQL | 8.x | **可以用 SQLite 代替**(见下) |
| Redis | 7+ | **可以没有** —— 框架自动降级到进程内缓存 / 内存计数 |

Redis 是**可选**的:框架层所有依赖 Redis 的能力(`cachex` 的 L2、`kvstore`
的限流计数、`idempotentx` 的幂等键)在没有 Redis 时都会退到进程内实现。
单机开发、跑示例、跑单测都不需要它。

### Windows 上装 Redis:用 Memurai

⚠️ **Redis 官方不发布 Windows 版**。Windows 上要用 Redis 兼容实现,最常用的是
[Memurai](https://www.memurai.com/get-memurai) —— 它讲的就是 Redis 协议,
本框架用的 go-redis v9 **连上去就能用,一行代码都不用改**。

| 项 | 值 |
|---|---|
| 下载 | <https://www.memurai.com/get-memurai> |
| 装完是什么 | Windows 服务,服务名 `Memurai`,默认开机自启 |
| 默认地址 | `127.0.0.1:6379`,**默认无密码**(`requirepass` 不配) |
| 自带客户端 | `memurai-cli.exe`(用法与 `redis-cli` 完全一样) |
| 配置文件 | `<安装目录>\memurai.conf`(默认 `C:\Program Files\Memurai\`) |
| 版本口径 | Memurai 8.2.0 ⇄ Redis 8.2 兼容 |

装完在配置里指一下地址就能用上:

```yaml
redis:
  addr: 127.0.0.1:6379
  password: ""        # Memurai 默认不带密码 ⇒ 留空
  db: 0
```

⚠️ **Windows 上用 Memurai 的三条坑**:

1. **Developer 版仅供开发/测试,不许上生产** —— 生产用 Linux 上正经的 Redis
   (或云厂商的托管 Redis)。Developer 版还有"闲置 10 天自动关机"的行为,
   本地环境突然连不上时先看这个。
2. **配置里把 `save` 注释掉 ≠ 关闭了持久化** —— Redis 的默认快照规则是
   **编译进去的**,注释掉配置里的 `save` 只是"没到阈值",不是"不落盘"
   (装完目录里那个 `dump.rdb` 就是证据)。真要调快照策略,改的是
   `stop-writes-on-bgsave-error`(默认 `yes` ⇒ **快照失败就拒写一切**,
   磁盘一满整个 Redis 就变成只读)。
3. **重启服务要管理员权限** —— `net stop Memurai` / `net start Memurai`
   在普通终端里会"拒绝访问",要用**管理员身份**打开 PowerShell。

## 1. 拿到代码

```bash
git clone <repo-url>
cd rapid-admin/server
```

## 2. 准备配置

```bash
cp configs/config.example.yaml configs/config.yaml
```

`configs/config.yaml` 是**本地配置**,已被 `.gitignore` 忽略 —— 里面会有你的
数据库密码、阿里云 AK/SK 这类东西,**不要提交**。

最小可用配置(其它项都有默认值):

```yaml
database:
  driver: sqlite          # 想用 MySQL 就改成 mysql
mysql:
  host: 127.0.0.1
  port: 3306
  username: root
  password: your_password
  dbname: rapid-admin
redis:
  addr: ""                # 留空 = 不连 Redis,框架走内存实现
jwt:
  admin_secret: "换成你自己的随机串,至少 32 字节"
```

⚠️ `jwt.admin_secret` **必须配**。它**没有默认值** —— 不配就是空字符串,
框架**不会报错、不会告警**,直接拿空密钥签 token(等于把签发权交给任何人)。
后台 / 小程序端 / 合作方端是三个独立密钥(`admin_secret` / `owner_secret` /
`supplier_secret`),用哪个端就配哪个。

完整的配置项说明见 [配置体系](configuration.md),全表见
[配置项全表](../reference/config-index.md)。

## 3. 建表 + 灌种子数据

```bash
go run ./cmd/migrate -config configs/config.yaml
```

`migrate` 会做三件事:AutoMigrate 建表 / 灌种子数据(13 个角色 + 资源树 +
Casbin 策略 + 业务字典)/ 校验字典完整性。

```bash
# 只想建表,先不灌数据
go run ./cmd/migrate -config configs/config.yaml --skip-seed

# 只想看看会执行什么 DDL,不真跑
go run ./cmd/migrate -config configs/config.yaml --dry-run
```

## 4. 起服务

```bash
go run ./cmd/server -c configs/config.yaml
```

> ⚠️ 注意是 `-c`,不是 `-config`(`cmd/server` 只认 `-c`;`cmd/migrate` 和
> `cmd/seed` 才是 `-config`)。

起来之后:

| 端点 | 说明 |
|---|---|
| `http://127.0.0.1:8888/healthz` | 存活检查 |
| `http://127.0.0.1:8888/readyz` | 就绪检查(探依赖) |
| `http://127.0.0.1:8888/metrics` | Prometheus 指标(`metrics.enabled=true` 时) |
| `http://127.0.0.1:8888/swagger/index.html` | API 文档(见 [swaggerx](../api-docs/swaggerx.md)) |
| `http://127.0.0.1:8888/docs/index.html` | **这一本手册**(见 [框架手册](../api-docs/manual.md)) |
| `http://127.0.0.1:8888/rapid-admin/v1/public/auth/login` | 登录(**公开**组,不带 token) |

路由前缀是 `/rapid-admin/v1`,下面分四组:`public`(免登录)/ `system`(用户、角色、
字典等系统管理)/ `biz`(业务)/ `platform`(平台运营)。

默认账号 `admin / Admin@2026`(由 `cmd/migrate` 的 seed 写入;改初始密码用
`go run ./cmd/migrate -config configs/config.yaml --password '你的新密码'`)。

## 5. 打一个真实的接口

```bash
# ⚠️ 密码是**前置哈希**过的:sha256("rapid-admin::" + 明文) 的 hex。
#    服务端直接拿你发过去的这串去比 bcrypt(hash),不会自己再哈希一次。
#    (前端 web/src/utils/crypto.ts 的 preHashPassword 做的就是这件事)
PWD_HEX=$(printf 'rapid-admin::Admin@2026' | sha256sum | cut -d' ' -f1)

# 登录,拿 token
curl -s -X POST http://127.0.0.1:8888/rapid-admin/v1/public/auth/login \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PWD_HEX\"}"

# 带 token 访问
curl -s http://127.0.0.1:8888/rapid-admin/v1/system/users \
  -H "Authorization: Bearer <上一步的 token>"
```

响应体是**统一形状**(见 [框架约定](conventions.md)):

```json
{
  "code": 0,
  "data": { "...": "..." },
  "message": "ok",
  "timestamp": 1761878400000
}
```

`code == 0` 是成功,非 0 是业务错误码(见 [errcode](../infrastructure/errcode.md))。

## 6. 跑测试

```bash
go test ./...
```

不需要 MySQL,不需要 Redis —— 测试用 SQLite 内存库和内存实现。

## 7. 看框架模块怎么用

每个框架模块都有一个**能直接跑**的最小示例:

```bash
go run ./examples/cachex      # 两级缓存
go run ./examples/authz        # 资源级鉴权
go run ./examples/cronx        # 定时任务
```

全部 50 个见 [示例目录](../reference/examples.md)。

## 8. 看这一本手册

两种看法:

```bash
# 1) 直接读源 —— 都是普通 Markdown,不想装工具也能读
cd server/docs && ls

# 2) 本地预览(编成 GitBook 站点)
cd server/docs && npm ci && npm run serve   # http://localhost:4000
```

服务起来之后,手册也在 `http://127.0.0.1:8888/docs/index.html`
—— 它是用 `go:embed` 打进二进制的,部署时不需要 node 也不需要外网。
(与 `/swagger` 共用 BasicAuth 凭据,见 [框架手册](../api-docs/manual.md)。)

## 下一步

- 要接业务模块:先读 [框架约定](conventions.md)(响应体 / 错误码 / 分页 / 字段命名)
- 要理解分层:看 [配置体系](configuration.md)
- 要找现成能力:看 [包速查表](../reference/packages.md)
- 要改这一本手册:看 [框架手册](../api-docs/manual.md)(构建 / 内嵌 / 访问)
