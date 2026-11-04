# loginlock —— 登录失败计数 + 账号锁定

> 防暴力破解:同一 IP + 同一账号连续登录失败到阈值 ⇒ 临时锁住。
> 计数存在 [kvstore](../storage/kvstore.md) 里(Redis / 内存),本包自己不碰 Redis。
>
> ⚠️ **Windows 上没有官方 Redis** —— `kv.mode=redis` 之前先装一个:
> [Memurai](https://www.memurai.com/get-memurai)(Redis 协议兼容,装成 Windows 服务,
> 默认 `127.0.0.1:6379`、无密码)。**Developer 版仅供开发/测试,不许上生产。**
> 详见 [快速开始](../getting-started/README.md)。

## 功能

三个函数,串在登录链路上:

| 函数 | 时机 | 作用 |
|---|---|---|
| `PreCheck(ctx, username, ip, max)` | 登录**之前** | 读失败次数,达到 `max` 返 `errcode.UserLocked` |
| `AfterFail(ctx, username, ip, lockDur)` | 登录**失败**之后 | 计数 +1,首次建 key 时设 `lockDur` 过期 |
| `Clear(ctx, username, ip)` | 登录**成功**之后 | 清零 |

本包刻意**不依赖 repository** —— 存储由 kvstore 决定。这样 service 和 middleware
两边都能共用,不用为循环依赖发愁。

### 计数存哪

存 kvstore 的 `Counter`,key 是:

```
login:fail:<ip>:<username>
```

⚠️ **两个维度都算**:IP 和 username 拼在一起。

### 它不解决什么

- 不做验证码(验证码是独立模块,配置在 `captcha:` 块里)
- 不做设备指纹、不识别"同一个人换了 IP"
- 不处理 DB 上那条账号级锁(见「注意事项」第 2 条)

## 配置

### yaml 侧

**该包无 yaml 配置**(没有阈值项、没有锁定时长项)。

它只间接依赖 `kv:` 块 —— 存储后端:

```yaml
kv:
  mode: auto          # auto | redis | memory
```

`kv.mode=auto` 且没 Redis 时走进程内计数:阈值一样生效,但**重启后清零、多副本不共享**。
⇒ 生产多副本必须 `kv.mode: redis`。

### 代码侧

| 项 | 值 | 位置 |
|---|---|---|
| 失败阈值 | **10**(硬编码) | `internal/service/auth_impl.go` 调 `loginlock.PreCheck(ctx, req.Username, req.LoginIP, 10)` |
| 锁定时长 | **15 分钟**(硬编码) | `authService.lock = 15 * time.Minute` |
| 错误码 | `errcode.UserLocked` = **20101** | `internal/pkg/errcode` |

⚠️ `UserLocked` 在 `20xxx` 段 ⇒ `errcode.HTTPStatus` 把它兜底成 **HTTP 200**,
语义靠 body 里的 `code` 承载(框架的通用约定:HTTP 200 + `code != 0` 是正常失败返回)。

`max <= 0` 表示**显式关闭锁定**。

## 使用方法

```go
// 1. 登录前:还锁着吗
if err := loginlock.PreCheck(ctx, username, ip, 10); err != nil {
    return nil, err          // errcode.UserLocked
}

// 2. 密码不对:计数 +1
if !auth.CheckPassword(u.PasswordHash, req.Password) {
    if err := loginlock.AfterFail(ctx, username, ip, 15*time.Minute); err != nil {
        // 计数写不进去 —— 这次失败不累计,但要让调用方看见
    }
    return nil, errcode.New(errcode.LoginFailed, "账号或密码错误")
}

// 3. 登录成功:清零
_ = loginlock.Clear(ctx, username, ip)
```

存储装配在 main 里做一次:

```go
store, err := kvstore.Resolve(cfg.KV.Mode, repository.GetRedis())
if err != nil {
    return err
}
kvstore.SetDefault(store)
```

`kvstore.Resolve` 显式 `redis` 模式但没配 `redis.addr` ⇒ **直接报错,不静默退回内存**
(多副本部署下用内存限流等于把阈值放大 N 倍)。

完整可运行示例见 `examples/loginlock/main.go`。

## 注意事项

- ⚠️ **key 是 (ip, username) 两个维度**。⇒ 攻击者**换 IP 刷同一账号**,计数不涨;
  **同一 IP 撞不同账号**,计数也不涨 —— 单维度都挡不住分布式撞库。
- ⚠️ **登录流程里有两条锁,口径不同**:
  1. **kvstore 这条**:按 `(ip, username)`,阈值 10 / 15 分钟
  2. **DB 上那条**:`sys_users.failed_count` + `sys_users.locked_until`,**纯按账号**,
     不看 IP,阈值同样是 10 / 时长同样是 15 分钟(两处都硬编码,靠人工保持一致)
  ⇒ 同一个账号从 10 个 IP 各失败 10 次:kvstore 那把锁一把都没锁,但 DB 那把锁锁死了。
- ⚠️ **计数存 kvstore,不在 DB**。`kv.mode=auto` 没 Redis 时走进程内 ⇒ **重启后清零、
  多副本不共享**(每个副本各数各的,实际阈值 = 10 × 副本数)。生产多副本必须
  `kv.mode: redis`。
- ⚠️ **阈值和锁定时长都不在 yaml 里**。改它们要改 `internal/service/auth_impl.go`
  的代码(两个数字:`PreCheck` 的第三个参数、`authService.lock`)。
- ⚠️ **解锁后计数是否清零?**分两层:
  - **窗口**:`AfterFail` 用 `Counter.Incr(key, lockDur)`,Redis 版 `Incr` 的 ttl
    **只在首次创建时生效**(INCR 不重置 TTL)⇒ 连续失败**不会**把窗口无限续长。
    这是刻意的 —— 否则爆破就有了免死金牌。
  - **计数**:只有**登录成功**才调 `Clear`。锁定到期后 key 自然过期,新的失败从头计;
    但用户**换了个 IP** 登录成功 ⇒ 清的是另一个 key,原 key 的计数还在(不过它
    也已经过期了)。
- ⚠️ **`PreCheck` 出错放行、`AfterFail` 出错返错**,方向相反是刻意的:
  - `PreCheck` 出错 → **放行**(可用性优先)。Redis 抖一下就把所有账号锁死,比放行更糟。
  - `AfterFail` 出错 → **返回错误**(不要假装成功)。登录已经失败了,计数写不进去
    意味着这次失败不累计 —— 但调用方要能看见它。
- ⚠️ **`max <= 0` = 显式关闭锁定**。不这么判的话,一旦有人把阈值改成配置项并默认
  填 0,`n >= max` 会变成"失败一次就锁死所有账号"。
- ⚠️ **`Clear` 的错误被登录流程忽略**(`_ = loginlock.Clear(...)`)⇒ 计数没清掉
  的话,用户密码改对了还是进不去(直到窗口过期)。
- ⚠️ **`PreCheck` 拦下来时不再比对密码** —— 省掉一次 bcrypt。
- ⚠️ **2026-10-21 之前,本地 dev 里登录锁定整个不存在**。三个函数开头都是
  `if repository.RDB == nil { return nil }` ⇒ 没 Redis 时暴力破解可以无限试 ——
  而这恰恰是最需要它的地方(生产有 Redis 挡着,本地反而裸奔)。现在走 kvstore 的
  内存后端,阈值一样生效。

## 相关

- 源码:`internal/pkg/loginlock/`
- 可运行示例:`examples/loginlock/main.go` —— `go run ./examples/loginlock`
- 计数 / 滑动窗口存储(锁定的落地处) → [kvstore](../storage/kvstore.md)
- 异常登录检测(同一账号多端 / 异地) → [anomalylogin](anomalylogin.md)
- 错误码分段 → [errcode](../infrastructure/errcode.md)
