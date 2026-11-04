# smslimit —— 短信发送限流:冷却 / 1 分钟 / 1 天三道闸

> 短信按条计费,而且会被服务商风控。smslimit 用 Redis 做 **per phone + per scene**
> 的三道闸,把"刷验证码"挡在发出去之前。
>
> ⚠️ **Windows 上没有官方 Redis** —— 本包靠 Redis 计数,没 Redis 就退成进程内计数
> (阈值一样生效,但重启清零、多副本不共享)。开发机上要真 Redis 就装
> [Memurai](https://www.memurai.com/get-memurai)(Redis 协议兼容,装成 Windows 服务,
> 默认 `127.0.0.1:6379`、无密码)。**Developer 版仅供开发/测试,不许上生产。**
> 详见 [快速开始](../getting-started/README.md)。

## 功能

smslimit 解决一件事:**同一个手机号在同一个场景下,不能无限发短信**。

判定顺序固定为三道闸:

| 闸 | 机制 | Redis key | 可配的上限 |
|---|---|---|---|
| 冷却期 | `SetNX` + TTL | `sms:cooldown:{phone}:{scene}` | `CooldownSecs`(秒) |
| 1 分钟窗口 | Lua 固定窗口计数 | `sms:win:v2:1m:{phone}:{scene}` | `PerMinute` |
| 1 天窗口 | Lua 固定窗口计数 | `sms:win:v2:1d:{phone}:{scene}` | `PerDay` |

- **冷却期先判**,是为了让"冷却期内重复发送"这条短窗口**不消耗窗口配额**。
  (代价:窗口判超限时冷却期已经被消耗掉了,用户会先看到冷却提示,
  60s 后再看到真正的超限提示 —— 两个都是"太频繁",不影响正确性。)
- **窗口长度是写死的**(1 分钟 / 1 天),**只有上限可配**。
- **被拒的请求也会计数** —— 计数在判超限之前就加了。所以"第 3 次开始被拒"之后
  计数还会继续涨(不影响判定:已经超了就一直超)。

**不解决**:验证码本身的生成 / 校验 / 有效期(那是 `service/auth` 的事,
有效期由 `sms.rate_limit.code_ttl` 控制);IP 维度的限流(那是
[ratelimitx](../security/ratelimitx.md) + `captcha.sms_send_enabled` 那条闸)。

## 配置

### yaml 侧(`sms.rate_limit:` 块)

```yaml
sms:
  rate_limit:
    per_minute: 1         # 同手机号每分钟 1 条
    per_day: 10           # 同手机号每天 10 条
    window_seconds: 60    # 同手机号再次发送冷却秒数
    code_ttl: 600         # 验证码有效期(秒)—— 不归本包管
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `sms.rate_limit.per_minute` | int | `1` | 1 分钟窗口内同 phone+scene 最多 N 条 |
| `sms.rate_limit.per_day` | int | `10` | 1 天窗口内同 phone+scene 最多 N 条 |
| `sms.rate_limit.window_seconds` | int | `60` | 冷却期秒数 |
| `sms.rate_limit.code_ttl` | int | `600` | 验证码有效期 —— **由 `service/auth` 消费,本包不读** |

⚠️ **`window_seconds` 是"冷却秒数",不是"窗口长度"**。窗口长度(1 分钟 / 1 天)
在代码里写死;这个键映射到的是 `smslimit.Config.CooldownSecs`。

### 代码侧(构造选项)

```go
smslimit.Init(repository.GetRedis(), smslimit.Config{
    PerMinute:    cfg.SMS.RateLimit.PerMinute,
    PerDay:       cfg.SMS.RateLimit.PerDay,
    CooldownSecs: cfg.SMS.RateLimit.WindowSeconds,
})
```

| 字段 | `<= 0` 时的行为 |
|---|---|
| `Config.PerMinute` | **退回默认 1**(不是"不限") |
| `Config.PerDay` | **退回默认 10**(不是"不限") |
| `Config.CooldownSecs` | **退回默认 60** |

## 使用方法

### 装配(启动期调一次)

```go
smslimit.Init(repository.GetRedis(), smslimit.Config{
    PerMinute:    1,
    PerDay:       10,
    CooldownSecs: 60,
})
```

`rdb` 传 nil ⇒ 关闭限流(本地 dev 没 Redis 时所有 `Allow` 返 nil,直接放行)。

### 判定(发送入口)

```go
if err := smslimit.Allow(ctx, phone, scene); err != nil {
    return err   // *errcode.Error: SmsSendTooFrequent / SmsTooFrequent
}
```

两个错误码的区别:

| 错误码 | 含义 | 触发点 |
|---|---|---|
| `errcode.SmsSendTooFrequent` | 冷却期内重复发送 | 冷却期那一闸 |
| `errcode.SmsTooFrequent` | 超过 1 分钟 / 1 天窗口上限 | 两个窗口闸 |

调用方(`service/auth` 的 `SendSms`)在 `smslimit.Allow` 之后还有一层 **DB 兜底**:
查 `sys_sms_code` 里最近一条发送记录,冷却期内也拒。Redis 挂掉时是这层在挡。

### 全局单例

包级函数 `Allow` 只是委托给全局实例:

```go
func Allow(ctx context.Context, phone, scene string) error {
    if global == nil {
        return nil // 未初始化 → 放行
    }
    return global.Allow(ctx, phone, scene)
}
```

⚠️ **`Limiter` 的字段全是未导出的** —— 包外 `&smslimit.Limiter{}` 编译得过,
但拿到的是 `rdb == nil` 的零值实例,`Allow` 一律放行。**包外只能用 `Init` + `Allow`**;
包内单测则直接构造实例(这是 `Allow` 拆成"包级函数 → 实例方法"的原因:
全局变量版只能靠改 `global` 来测,用例之间会互相污染)。

## 注意事项

- ⚠️ **`Config` 字段 `<= 0` 会退回默认值,不是"不限"**。
  旧注释写的是"0 = 不限",但代码是 `if cfg.PerMinute <= 0 { cfg.PerMinute = 1 }` ——
  `checkWindow` 里那条 `limit <= 0 → 放行` 的分支**永远走不到**。
  现在的口径选**代码的行为**(限住),理由:限流是安全阀,配置漏配 / 写错时必须落在
  "限住"这一侧;而且 `configs/config.yaml` 是 gitignore 的,部署方很容易漏配,
  漏配 ⇒ `per_minute=0` ⇒ 若解释成"不限"就是**短信接口裸奔**。
  这与 `slowquery` 的 `Threshold <= 0` 是同一条口径。
- ⚠️ **`Init` 只认第一次调用**(`global != nil` 直接 return)。重复装配不会改配置 ——
  改限流参数只能改配置后重启进程。
- ⚠️ **Redis 故障 ⇒ 放行**,不阻塞主流程。日志里一条 Warn,
  DB 层 `sys_sms_code` 仍兜底(只是慢)。这是有意的取舍:把用户卡在"发送失败"上更糟。
  `rdb == nil` 同样放行。
- ⚠️ **固定窗口的 TTL 只在计数器刚创建时设一次**(`windowScript` 里的 `if c == 1`)。
  M112 之前的旧实现是"每次计数都无条件 `EXPIRE key ttl`" ⇒ TTL 每次被往后推 ⇒
  **计数器永不过期**;加上"被拒的请求也 INCR",实际后果是
  **这个手机号在这个场景下被永久拉黑**(每次重试都续一次 TTL)。
  用 Lua 而不是"INCR 之后在 Go 里判断 count == 1 再 EXPIRE"是因为后者是两次往返,
  中间进程挂掉 ⇒ 计数器建好了却没 TTL ⇒ 又变成永不过期,而且是静默的。
- ⚠️ **窗口 key 带 `v2` 版本段**(`sms:win:v2:1m:` / `sms:win:v2:1d:`)。
  旧 key(`sms:window:1m:` / `sms:window:1d:`)里的计数是被推爆的,TTL 最长 24h ——
  不换命名空间的话,升级后这些手机号会带着旧计数继续被拒,最长 24h。
  旧 key 没人再碰,自然过期即可。
- ⚠️ **每个场景各有各的额度**。key 是 `{phone}:{scene}`,所以
  `login` 和 `reset_password` 互不影响 —— 调用方**传错 scene 名**
  (大小写不同、多一个后缀、拼错)就会换一份**全新额度**,
  "限流"形同虚设。scene 的合法取值只有四个:
  `login` / `reset_password` / `change_phone` / `view_full_phone`。
- ⚠️ **日志里不打 key** —— key 里含手机号(明文 PII)。窗口计数失败时只打
  `limit` 和 error,不打 key。
- ⚠️ **计数存的是 Redis,不是 kvstore**。本包自己拼 key、自己跑 Lua,
  不经过 `kvstore` 的 Counter / Window(那是给限流中间件和登录锁定用的另一套原语,
  见 [kvstore](../storage/kvstore.md))。
- ⚠️ **Redis 未配置时 `Init` 不会报错**,只是限流整个失效(全部放行)。
  启动日志里有一行 `smslimit ready`,那行是判断限流有没有真的开着的唯一依据。

## 相关

- 源码:`internal/pkg/smslimit/`
- 可运行示例:`examples/smslimit/main.go` —— `go run ./examples/smslimit`
  (用 `miniredis` 起内存 Redis,不用真连)
- 短信通道本身 → [msn](msn.md)
- 通用计数 / 滑动窗口原语 → [kvstore](../storage/kvstore.md)
- 错误码定义 → [errcode](../infrastructure/errcode.md)
- IP 维度的接口限流 → [ratelimitx](../security/ratelimitx.md)
