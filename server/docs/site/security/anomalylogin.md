# anomalylogin —— 异常登录检测(多端 / 异地同时活跃)

> 同一账号在多个 IP / 多台设备同时活跃时识别可疑会话。
> 本包只做**检测 + 通知**,具体写库 / 告警由注入的 Notifier 决定 ——
> 与登录 service 完全解耦。

## 功能

### Detector:纯函数、无状态、线程安全

历史会话由 service 查库传入,**本包不读 DB**。

| 接口 | 作用 |
|---|---|
| `anomalylogin.New()` | 默认检测器(`MaxConcurrent=3`,不开"不可能旅行") |
| `(*Detector).WithMaxConcurrent(n int)` | 允许的同时活跃会话数(含本次登录),超过才报 |
| `(*Detector).WithImpossibleTravelWindow(w)` | 开启"不可能旅行"检测;`0` = 不检测 |
| `(*Detector).Detect(current, history)` | 跑所有规则,返 `[]Anomaly` |

### 三类异常

| 常量 | 值 | 严重度 | 触发条件 |
|---|---|---|---|
| `ConcurrentDifferentIP` | `concurrent_different_ip` | **High** | 活跃会话数超阈值,且存在 IP 不同的其它会话 |
| `ConcurrentDifferentDevice` | `concurrent_different_device` | **Medium** | 同上,Device 不同 |
| `ImpossibleTravel` | `impossible_travel` | **High** | 两个不同 IP 的登录时间差 < `ImpossibleTravelWindow` |

严重度三级 `Low` / `Medium` / `High`,`Severity.String()` 返 `"low"` / `"medium"` /
`"high"`(未知值返 `"unknown"`)。

⚠️ **`Low` 级别定义了但 `Detect` 从不产生它** —— 规则 1 出 High、规则 2 出 Medium、
规则 3 出 High,没有出 Low 的路径。

### 规则 3 不受并发阈值门控

规则 1 / 2 判"**数量**"(活跃会话数是否超过 `MaxConcurrent`),规则 3 判"**时间差**"。
两者正交 —— 即使只有两个会话,10 分钟内换了 IP 也会报 `ImpossibleTravel`。

### Notifier:通知靠注入

```go
type Notifier interface {
    Notify(ctx context.Context, a Anomaly)
}
```

默认实现 `LogNotifier{Hook func(ctx, a)}`:记一条 **WARN** 日志 + 调 Hook。
业务可以换成 SMS / 邮件 / 强制登出。

### 它不解决什么

- **不阻断登录**。它是提醒,不是准入闸门(准入闸门是 loginlock / captcha / RBAC)。
- 不做地理判断(没有 IP 地理库)
- 不查库、不写库

## 配置

### yaml 侧(`auth.anomaly:` 块)

```yaml
auth:
  anomaly:
    enabled: true                 # 是否启用检测
    max_concurrent: 3             # 允许的同时活跃会话数(含本次登录)
    active_window: 30m            # "多久之内有过活动"才算同时在线
    impossible_travel_window: 0s  # 多短时间换 IP 算疑似盗号;0 = 关
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `auth.anomaly.enabled` | bool | `true` | 关掉后登录链路**完全不做检测**(连查库都不查),不是"检测但不报警" |
| `auth.anomaly.max_concurrent` | int | `3` | 超过才报并发类异常;`≤0` 会被兜底成 **1**(见下) |
| `auth.anomaly.active_window` | duration | `30m` | "同时在线"的口径;`0` = 不判 ⇒ 并发类异常**永不触发** |
| `auth.anomaly.impossible_travel_window` | duration | `0s` | `0` = 关(默认,见下) |

对应的配置结构体是 `config.AnomalyConfig`(字段 `Enabled` / `MaxConcurrent` /
`ActiveWindow` / `ImpossibleTravelWindow`)。

⚠️ **`active_window` 不是 Detector 的字段** —— `Detector` 结构体里只有
`MaxConcurrent` 和 `ImpossibleTravelWindow`。`active_window` 是**登录 service 层**
的口径(它决定查哪些历史会话算"还活跃"),不参与 `Detect` 的判定。

## 使用方法

```go
d := anomalylogin.New().
    WithMaxConcurrent(2).
    WithImpossibleTravelWindow(30 * time.Minute)

current := anomalylogin.LoginInfo{
    SessionID: "sess-4", UserID: 1001,
    IP: "5.6.7.8", Device: "微信/iOS",
    LoginAt: now,
    // LogoutAt 为 nil = 仍活跃
}

history := []anomalylogin.LoginInfo{ /* 由 service 查库传入 */ }

anomalies := d.Detect(current, history)
for _, a := range anomalies {
    notifier.Notify(ctx, a)   // a.Type / a.Severity / a.Reason / a.Current / a.Existing
}
```

注入额外副作用(发短信 / 站内信 / 吊销旧 jti):

```go
notifier := &anomalylogin.LogNotifier{
    Hook: func(ctx context.Context, a anomalylogin.Anomaly) {
        // 日志与副作用**都要**,别用 Hook 取代日志
    },
}
```

`LoginInfo` / `Anomaly` 的形状:

```go
type LoginInfo struct {
    SessionID string
    UserID    uint64
    IP        string
    Device    string     // User-Agent 截断
    LoginAt   time.Time
    LogoutAt  *time.Time // nil = 仍活跃
}

type Anomaly struct {
    Type       AnomalyType
    Severity   Severity
    UserID     uint64
    Current    LoginInfo // 本次登录
    Existing   LoginInfo // 冲突源(已存在的活跃登录)
    Reason     string
    DetectedAt time.Time
}
```

完整可运行示例见 `examples/anomalylogin/main.go`。

## 注意事项

- ⚠️ **`impossible_travel_window` 默认 0(关闭),这是刻意的**。这条规则只看 IP,
  **不看地理位置**:用户从家里 WiFi 切到手机流量(几秒换 IP)⇒ 开着必然误报。
  真正的"不可能旅行"需要 IP 地理库(两地相距多远),**本仓没有那个库**。
  ⇒ 框架层把旋钮留好,要不要开由运维按自己的误报容忍度决定。
- ⚠️ **`enabled` 漏配 ≠ 不跑,而是照常跑**。`config.Load` 里有
  `v.SetDefault("auth.anomaly.enabled", true)` ⇒ yaml 里漏写整个 `auth.anomaly` 块,
  检测**照常执行**。
  ⚠️ 但**零值结构体不一样**:`config.AnomalyConfig{}` 直接构造时 `Enabled` 是 `false`
  ⇒ 检测**静默不跑**(连查库都不查),症状是"日志里一条异常都没有" ——
  与"跑了但没发现异常"**完全无法区分**。
- ⚠️ **`max_concurrent` 填 0 会被兜底成 1**。`Detector.maxConcurrent()` 里 `≤0 → 1`,
  等于"只允许单端在线" ⇒ 正常多端登录的用户**每次登录都收到提醒**。yaml 注释里
  明写了"别填 0"。
- ⚠️ **`active_window` 填 0 = 不判"同时在线"**。登录 service 的
  `concurrentSessions` 里 `window <= 0` 直接返 nil ⇒ 并发类异常**永远不会触发**,
  而且没有任何报错。
- ⚠️ **"同时在线"的口径是 `active_window`,不是会话 TTL**。仓里有两个"会话还活着"
  的口径,回答的是不同问题:
  1. `sessionExpired`(`COALESCE(last_active_at, login_at) + refresh_ttl <= now`)——
     答"这条会话还算不算数"。`refresh_ttl` 默认 720h(30 天)⇒ 一个月前登录、
     没登出、token 还能续期的会话,在它眼里**全都还活着**。
  2. 异常登录用的"同时在线"—— 活动时刻落在 `active_window` 之内,答
     "**这个人现在在不在用**"。
  ⚠️ 拿 1 当 2 ⇒ 一个 30 天里在 3 台设备上登录过的人,第 4 次登录时"同时活跃会话数"
  就是 4 > 3 ⇒ **每次登录都报异常**,阈值形同虚设。
- ⚠️ **`MaxConcurrent` 曾经是个死旋钮**。M128 之前 `Detect` 硬编码
  `len(active) > 1`,这个字段**从来没被任何一行代码读过** ⇒ 配 `MaxConcurrent = 10`
  实际第 2 端就报 High。清单上标着 "P2 ✅" 描述的是**字段注释**,不是行为。
- ⚠️ **`LogNotifier` 曾经是个空壳**。M128 之前 `Notify` 的函数体只有
  `if n.Hook != nil { n.Hook(...) }` —— 注释写着"落 stderr",实现里**一行日志都没有**。
  ⇒ "用默认通知器" = 什么都不发生。现在补了 WARN 日志(异常登录是**安全事件**,
  不是调试信息)。
- ⚠️ **规则 1 / 2 需要 IP 或 Device 非空**。判定是
  `if a.IP != "" && a.IP != current.IP` ⇒ IP 为空的会话**检测不出来**。
- ⚠️ **Device 是 UA 原文按 128 截断**(`uainfo.Clip(req.UserAgent, 128)`),
  必须与 `sys_login_log.device` **用同样规则截断** —— 两边长度不一致会让
  "设备相同"的判断失真,进而误报 `ConcurrentDifferentDevice`。
- ⚠️ **只有 High 会发站内信**。Medium(只是设备不同)太常见 —— 一个"手机 + 电脑"的
  正常人每次登录都会命中一次。发站内信 ⇒ 用户被反复骚扰 ⇒ 关掉通知 ⇒
  真异常也没人知道。日志则三种严重度都记(日志是给运维看的)。
- ⚠️ **检测失败绝不影响登录**。`detectLoginAnomaly` **不返回错误**:查库失败、
  通知失败、站内信投递失败全部只记日志。登录链路已经过了密码校验与 token 签发,
  不该被一个"安全增强"拖垮。
- ⚠️ **检测必须放在登录成功之后**。放在之前:密码还没校验对就查会话,等于给撞库的
  人送情报。
- ⚠️ **历史会话查询有 50 条上限**(`Limit(50)`)。并发会话数超过这个量级时,
  阈值早就超了,再往下翻也没有增量信息。

## 相关

- 源码:`internal/pkg/anomalylogin/`
- 接线(service 层,含"同时在线"口径与站内信) → `internal/service/auth_anomaly.go`
- 可运行示例:`examples/anomalylogin/main.go` —— `go run ./examples/anomalylogin`
- 登录失败计数 + 账号锁定(准入闸门那一侧) → [loginlock](loginlock.md)
- JWT 签发 / 解析 / 吊销 → [auth](auth.md)
- 配置项全表 → [configuration](../getting-started/configuration.md)
