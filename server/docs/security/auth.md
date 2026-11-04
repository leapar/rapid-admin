# auth —— JWT 签发 / 解析 / 吊销 + 强制下线

> 令牌体系的三件事放在一个包里,因为它们共享同一条信任链:
> 签出来的 token 能不能被认、能不能被作废,以及口令怎么存。
>
> ⚠️ **Windows 上没有官方 Redis** —— `revocation_mode=redis`(生产多副本必须)之前
> 先装一个:[Memurai](https://www.memurai.com/get-memurai)(Redis 协议兼容,
> 装成 Windows 服务,默认 `127.0.0.1:6379`、无密码)。
> **Developer 版仅供开发/测试,不许上生产。** 详见 [快速开始](../getting-started/README.md)。

## 功能

auth 提供四组能力:

1. **JWT 签发与解析(三受众独立 secret)**
   - `Audience` 三态:`admin` / `owner` / `supplier`,每个受众一把自己的 secret
   - `TokenType` 三态:`access` / `refresh` / `short` —— **声明用途,解析时强制比对**
   - `Claims` 装业务 claim(uid / iid / usr / role / pj / sid / typ),标准 claim
     走嵌入的 `jwt.RegisteredClaims`
2. **令牌吊销**
   - 逐 jti 吊销(`RevocationStore.Revoke`)
   - **按会话吊销**(`RevokeSession` / `IsSessionRevoked`):一次结束这条会话的
     access + refresh 两张 token
   - 存储三选一:`RedisStore`(多副本) / `MemoryStore`(进程内) / 自动挑选
3. **强制下线(按用户整体作废)**
   - `InvalidateFrom` 推进"作废线",`SessionInvalidated` 判定某张令牌是否落在线之前
   - 用于改密码 / 重置密码:踢掉该用户**全部**在途令牌,不只是当前这一张
4. **密码哈希与强度校验**
   - bcrypt(cost=12)+ `VerifyPasswordStrength`(≥10 字符,大小写 + 数字 + 特殊字符)

**不解决什么**:不存用户 / 角色 / 令牌黑名单以外的数据;不做接口级权限判定
(那是 [casbinx](casbinx.md) 与 `middleware.RBAC`);不管理会话列表本身
(登记簿 `sys_login_log` 在 service 层)。

## 配置

### yaml 侧(`jwt:` 块)

```yaml
jwt:
  admin_secret: ${JWT_ADMIN_SECRET}      # admin 后台
  owner_secret: ${JWT_OWNER_SECRET}      # 第二受众:小程序端
  supplier_secret: ${JWT_SUPPLIER_SECRET}# 第三受众:合作方端
  issuer: rapid-admin
  access_ttl: 8h          # access token 有效期
  refresh_ttl: 720h       # ⚠️ 是"整条会话链的总预算",不是"一张 token 能用多久"
  short_ttl: 5m           # 用于"查看完整手机号"等临时凭证
```

⚠️ **refresh token 是单次使用的**:续期时旧的那张会被立即吊销(吊销 TTL 取它
自己的剩余寿命)。所以 `refresh_ttl` 是整条会话链的总预算。

### yaml 侧(`auth:` 块)

```yaml
auth:
  revocation_mode: auto   # auto | redis | memory
  principal_cache_ttl: 5m # 权限主体缓存 TTL(属 authz,见 [authz](authz.md))
```

`revocation_mode` 三态:

| 值 | 行为 |
|---|---|
| `auto` | 有 Redis 用 Redis,没有退内存(默认)。只保证"能吊销",**不保证多副本一致** |
| `redis` | 强制 Redis。没配 `redis.addr` **启动直接失败** —— 生产多副本必须用这个 |
| `memory` | 强制内存,即使配了 Redis 也不连。仅调试用 |

把它做成显式配置,是因为"我现在到底是不是安全的"不该隐式继承"有没有 Redis"。

### 代码侧(构造选项)

```go
auth.NewManager(secret, issuer string, accessTTL, refreshTTL, shortTTL time.Duration, aud Audience) *auth.Manager
auth.NewMulti(adminSecret, ownerSecret, supplierSecret, issuer string, ...) *auth.Multi
auth.ResolveRevocation(mode string, rdb *redis.Client) (auth.RevocationStore, error)
auth.SetDefault(p *auth.Multi)             // 注入全局 Multi
auth.SetDefaultRevocation(rs auth.RevocationStore) // 注入全局吊销存储
```

## 使用方法

### 签发与解析

```go
mgr := auth.NewManager(
    "admin-secret-at-least-32-bytes-long!!",
    "rapid-admin",
    8*time.Hour, 720*time.Hour, 15*time.Minute,
    auth.AudienceAdmin,
)

claims := &auth.Claims{
    UserID:     1001,
    IdentityID: 2001,
    Username:   "zhangwei",
    RoleCode:   "role_project_manager",
    ProjectIDs: []uint64{7, 9},
    SessionID:  auth.NewSessionID(), // 一次登录一个,access / refresh 共用
}

access, err := mgr.SignAccess(claims)
parsed, err := mgr.ParseAccess(access)   // 只认 access
```

### 三受众

```go
multi := auth.NewMulti(adminSecret, ownerSecret, supplierSecret, "rapid-admin",
    8*time.Hour, 720*time.Hour, 15*time.Minute)

tok, err := multi.SignByAudience(auth.AudienceAdmin, claims)
c, err := multi.ParseAnyAccess(tok)      // 受保护路由中间件用
```

### 吊销

```go
store := auth.NewMemoryStore()   // 生产用 auth.NewRedisStore(rdb)
auth.SetDefaultRevocation(store)

// 一次结束整条会话(access + refresh 一起死)
auth.RevokeSession(ctx, claims.SessionID, 720*time.Hour)
revoked, err := auth.IsSessionRevoked(ctx, claims.SessionID)

// 逐 jti
auth.DefaultRevocation().Revoke(ctx, parsed.JTI(), 8*time.Hour)
```

### 强制下线(作废线)

```go
// 改密码 / 重置密码:作废该时刻之前签发的全部令牌
store.InvalidateFrom(ctx, userID, time.Now())

// 中间件侧判定
invalid, err := auth.SessionInvalidated(ctx, claims)

// 纯函数版(边界判定可单测)
auth.TokenInvalidated(issuedAt, before, auth.ClockSkewGrace)
```

### 密码

```go
hash, err := auth.HashPassword(plain)   // bcrypt cost=12
ok := auth.CheckPassword(hash, plain)
strong := auth.VerifyPasswordStrength(plain)
```

## 注意事项

- ⚠️ **`Claims` 复用 = 静默指错对象**。`sign()` 无条件覆写整个
  `RegisteredClaims`(iat / exp / aud / jti)并生成**新的随机 jti**。同一个
  `*Claims` 连签两次,内存里那份最终停留在**后签**的那张上 —— 之后读
  `base.JTI()` 拿到的是新 token 的值。签两次就 `Fork()` 两次
  (`Fork` 必须拷 `SessionID`,漏了会让会话绑定在第一次续期后断掉)。
- ⚠️ **不要在 `Claims` 上再声明 `aud` / `jti` 同名同 tag 的字段**。`encoding/json`
  是**浅层胜出**,自定义字段会把 `RegisteredClaims` 里的那个彻底遮蔽
  (marshal / unmarshal 两边都收不到),留下一段看着能跑、实际是死代码的赋值。
  本包历史上踩过两次,现在统一用 `c.Audience()` / `c.JTI()` 访问器。
- ⚠️ **`typ` 缺失的老 token 一律拒收**(`ErrLegacyToken`),**不提供兼容解析** ——
  兼容分支等于让"refresh 可当 access 用"这个洞一直活着。发版即全员重登。
- ⚠️ **`Parse` 的 `expect` 是必填的**,没有"不校验类型"的旁路。access / refresh /
  short 互不通用,混用返 `ErrTokenTypeMismatch`。
- ⚠️ **`Multi.parse` 先不验签嗅探 `aud`** 来选 secret。嗅探出来的 aud 只用来选
  key,真校验由 `m.Parse` 重读 aud 并比对完成 —— 伪造 aud 只能把请求路由到错误的
  secret 上,结果必然验签失败。
- ⚠️ **`DefaultRevocation()` 保证非 nil**。忘了注入会拿到内存实现(能吊销,但多副本
  不一致);显式配 `redis` 而 Redis 不可用 → `ResolveRevocation` 返 error,
  **启动直接失败**(不静默降级)。
- ⚠️ **`MemoryStore` 重启即清空** —— 吊销记录全丢,重启会"复活"所有本该失效的
  token。而且它**只在本实例有效**:多副本部署下用户换个负载均衡节点,被吊销的
  token 又活了。
- ⚠️ **失败方向是 fail-open**。吊销查询 / 作废线查询出错 ⇒ 按"没吊销 / 没作废"
  放行(只记 WARN)。这是刻意的(误判的代价是"用户被随机踢下线且无法复现"),
  但代价是**吊销失效在监控上不可见** —— 看日志。
- ⚠️ **吊销 TTL 必须取会话里寿命最长的那张 token**(通常是 refresh 的 720h)。
  取 access 的 8h ⇒ 吊销记录先过期、token 还活着 = 等于没吊。
- ⚠️ **作废线判定带 60s 宽限**(`ClockSkewGrace`):判定式是 `iat + 60s < 作废线`。
  多实例时钟不同步时,"刚改完密码、重新登录拿到的 token"不会当场被判失效;
  代价是作废线前后 60 秒内签发的 token 仍然有效。
- ⚠️ **`ResolveRevocation` 的参数是 `*redis.Client` 而不是 `redis.UniversalClient`** ——
  接口会把 typed nil 变成非 nil,`rdb == nil` 恒为 false,于是"显式配了 redis 却
  没配 addr"会照常启动,然后每次吊销都往一台死 Redis 上打。
- ⚠️ **`NewManager` 不校验 secret 长度**(不像 crypto 那样硬校验 32 字节)。
  三套 secret 必须彼此不同 —— secret 相同的话,拿其中一把就能签出**任意 aud** 的
  token(`Multi.parse` 先按 aud 选 manager,aud 相同 ⇒ 选到同一把 secret ⇒ 验签通过)。
- ⚠️ **没有 sid 的老 token 登出时只能吊 access 的 jti**。`RevokeSession` 拿到空 sid
  直接 no-op,那张 refresh 还能再换发一次(窗口随老 token 自然过期关闭,最长 720h)。
  这是升级期的降级语义,不是新引入的缺陷。
- ⚠️ **强制下线是"全有或全无"**。推作废线 = 该用户**所有设备**一起下线;要
  "只踢别的设备"走 service 层的 `RevokeSessions`(sid 粒度)。登出刻意不推作废线 ——
  那会让"手机上退出"把电脑上的会话也一起踢掉。

## 相关

- 源码:`internal/pkg/auth/`(吊销存储实现在同包 `memory_store.go` / `redis_store.go`,
  跨实例广播在 `auth/revocache/`)
- 可运行示例:`examples/auth/main.go` —— `go run ./examples/auth`
- 接口级 RBAC 是另一件事 → [casbinx](casbinx.md)
- 资源级鉴权(IDOR 那一层)→ [authz](authz.md)
