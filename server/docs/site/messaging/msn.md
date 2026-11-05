# msn —— 运行时可切换的短信 sender

> 短信通道必须能在**不重启服务**的前提下换 provider / 换凭证 / 换模板号 ——
> 运维在配置中心面板上改完就生效。msn 把这件事拆成"每次发送前解析一次生效配置"的三段式。

## 功能

msn 解决一件事:**让 provider 不再是启动时定死的东西**。

1. **`Sender` 抽象** —— 上层只调 `Send(ctx, phone, code, scene)`,不关心运营商;
   `Provider()` 报出实现方(给审计和监控用)。
2. **`DynamicSender` 运行时切换** —— 三段拆分:
   - `Resolver` 每次发送前解析一次"当前生效的 `config.SMSConfig`"
     (yaml 打底 + 配置中心覆盖,规则见 `service.ResolveSMSConfig`)
   - `DynamicSender` 按解析出来的 `Provider` 选后端
   - `AliyunBackend` 把"这次生效的配置"交给阿里云后端
     (凭证变了重建 client,没变就复用)
3. **发送前模板守卫(可选)** —— `TemplateGuard` 窄接口,判定逻辑全在
   [smstpl](smstpl.md) 里,本包只负责"在正确的位置问一次"。

两个后端实现:

| 后端 | 包 | 行为 |
|---|---|---|
| `aliyun` | `internal/pkg/msn/aliyun` | 阿里云 dysmsapi SDK,`New`(严格)/ `NewDeferred`(宽松) |
| `console` | `internal/pkg/msn/console` | **只把验证码打进 zap 日志**,不真实下发 |

**不解决**:验证码的生成 / 校验 / 限流(那是 `service/auth` + [smslimit](smslimit.md));
模板的审核状态(那是 [smstpl](smstpl.md))。

## 配置

### yaml 侧(`sms:` 块)

```yaml
sms:
  provider: aliyun        # aliyun / console
  access_key: ""
  access_secret: ""
  sign_name: RapidAdmin
  region: cn-hangzhou
  templates:
    login: SMS_244570823
    view_full_phone: SMS_244570823
    reset_password: SMS_244570823
    change_phone: SMS_244570823
  rate_limit:            # ← 归 smslimit 管,见该章
    per_minute: 1
    per_day: 10
    window_seconds: 60
    code_ttl: 600
  template_guard: block  # off / warn / block —— 归 smstpl 管,见该章
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `sms.provider` | string | — | `aliyun` / `console`。**空串或未知值 ⇒ fallback 到 console** |
| `sms.access_key` / `sms.access_secret` | string | — | 阿里云凭证。运行时可被配置中心覆盖 |
| `sms.sign_name` | string | — | 短信签名,必须是阿里云那边**已审核通过**的 |
| `sms.region` | string | `cn-hangzhou` | `aliyun` 后端在 region 为空时用这个 |
| `sms.templates.*` | string | — | 场景 → 模板号。**唯一真源在配置中心**,yaml 只是打底 |

⚠️ **配置加载器不做 `${VAR}` 展开**(viper 没这功能)。yaml 里写
`access_key: ${SMS_ACCESS_KEY}` 会把**字面量**当 AK 发给阿里云。
正确做法是走环境变量(viper `AutomaticEnv` + `.`→`_` 替换,环境变量优先于 yaml):
`SMS_ACCESS_KEY` / `SMS_ACCESS_SECRET` / `SMS_PROVIDER` / `SMS_SIGN_NAME`。

⚠️ **`sms.template_guard` 刻意不进配置中心** —— 守卫策略是**安全开关**,
能从面板一键关掉就等于没有。改它只能改部署配置(改完重启)。

### 代码侧(构造选项)

```go
sender := msn.NewDynamicSender(base, resolve, nil, nil, guard)
```

`nil` = 不装配阿里云后端(此时 `provider=aliyun` 会明确报错)。
生产装配见 `cmd/server/main.go` 的 `buildSmsSender`:
`msn.NewDynamicSender(base, resolve, aliyun.NewDeferred(eff), console.New(), guard)`。

| 参数 | 类型 | 说明 |
|---|---|---|
| `base` | `config.SMSConfig` | 配置文件里那一份(打底) |
| `resolve` | `msn.Resolver` | 解析生效配置。**为 nil 时发送直接报错**,不会静默用旧配置 |
| `aliyun` | `msn.AliyunBackend` | 阿里云后端。为 nil 且生效配置要求 aliyun 时,发送返回明确错误 |
| `console` | `msn.Sender` | 控制台后端。为 nil 时退化为内置的 console 实现 |
| `guard` | `msn.TemplateGuard` | 发送前的模板守卫。为 nil 时不做任何检查 |

⚠️ **`guard` 是必填位置参数,不是可选的 setter** —— 守卫是安全开关,
做成"忘了调就不生效"的 setter,等于默认失效。不想检查就显式传 `nil`。

## 使用方法

### 装配

```go
// yaml 打底
base := config.SMSConfig{
    Provider:  "console",
    SignName:  "RapidAdmin",
    Templates: config.SMSTemplate{Login: "SMS_244570823"},
}

// Resolver:每次发送前跑一次,返回"这次生效的配置"
// 生产实现 = yaml 打底 + 配置中心覆盖(service.ResolveSMSConfig)
panel := base   // 模拟"运维在面板上改了 provider"
resolve := func(ctx context.Context, b config.SMSConfig) (config.SMSConfig, error) {
    out := b
    out.Provider = panel.Provider
    out.Templates = panel.Templates
    return out, nil
}

// 模板守卫:登记册 + 策略
reg := smstpl.NewRegistry()
_, _, _ = reg.Register(smstpl.SceneLogin, "SMS_244570823")
_, _, _ = reg.Apply(smstpl.SceneLogin, smstpl.StatusApproved, smstpl.SourceProvider, "阿里云审核通过", "")
guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)

sender := msn.NewDynamicSender(base, resolve, nil, nil, guard)
```

### 发送

```go
err := sender.Send(ctx, "13800001234", "123456", smstpl.SceneLogin)
fmt.Println(sender.Provider())   // 本次用的 provider(审计用)
```

场景常量在 `msn`(`msn.SceneLogin` / `SceneResetPassword` / `SceneChangePhone` /
`SceneViewFullPhone`),`msn.AsScene(s)` 是任意 string 到 `msn.Scene` 的归一入口。

### 运行时切换

`provider` 是**每次发送时解析**的,不是启动时定死的:

```go
panel.Provider = "aliyun"          // 运维在面板上改
sender.Send(ctx, phone, code, scene)   // 下一次发送即生效,不用重启
```

## 注意事项

- ⚠️ **`console` 实现是"把验证码直接打进日志"**。`console.ConsoleSender.Send` 走
  `zap.L().Info("[sms:console] 验证码(未真实下发)", phone, code, scene)` ——
  **验证码明文落在日志文件里**。只能用于 dev;生产误配的后果就是"验证码出现在日志文件里"。
- ⚠️ **`provider` 为空串 / 未知值 ⇒ fallback 到 console**。会打一条 Warn
  (不是静默降级),但**方向仍然是"验证码进日志"**。生产上 `provider` 必须显式配成
  `aliyun`。
- ⚠️ **`provider=aliyun` 但没装配阿里云后端 ⇒ 明确报错**
  (`sms: provider=aliyun 但没有可用的阿里云后端`),**不会**退到 console ——
  这是与上一条刻意相反的方向。
- ⚠️ **`Resolver` 返回错误 ⇒ 放弃这次发送**(而不是退回默认配置)。
  配置读不出来说明环境有问题,静默用旧配置发短信会掩盖故障。
  `resolve` 为 nil 时同样直接报错(`sms: 没有配置解析器`)。
- ⚠️ **守卫用的模板号必须来自"这次解析出来的同一份 cfg"**。模板号从
  `cfg` 里取,而不是另找一份配置再解析一次 —— 否则会出现"按 A 号的审核状态放行、
  实际发出去的是 B 号"的**拦错对象**错配。
- ⚠️ **`Provider()` 返回的是"最近一次解析出来的 provider"**,不是"这次成功用到的"。
  provider 落到 console 时它照样返回 `console` —— 审计行里看到 `console`
  就是"这次没真发"。
- ⚠️ **阿里云后端两种构造模式**:`aliyun.New(cfg)` 是**严格模式** ——
  构造时就建 client,凭证缺失 / region 写错在启动时炸(比第一次发短信才炸好排查);
  `aliyun.NewDeferred(cfg)` 是**宽松模式** —— 构造时不校验,等第一次发送时再校验。
  provider 由配置中心在运行期决定时必须用宽松模式,否则启动时因为"没凭证"就 Fatal,
  面板永远切不过去。
- ⚠️ **阿里云 client 的复用靠凭证指纹**(`AccessKey|AccessSecret|Region`):
  指纹不变 ⇒ 复用同一个 client(不重复握手);指纹变了 ⇒ 重建并替换。
  `AliyunSender.SendWith` 每次调用都会先 `validate(cfg)`。
- ⚠️ **模板号为空时阿里云只回一句含糊的 `isv.SMS_TEMPLATE_ILLEGAL`**,
  所以 `aliyun` 后端在模板号为空时**提前**报 `aliyun sms: scene=%q 的模板号未配置`。
- ⚠️ **`msn/factory.go` 里没有工厂函数** —— 它只是一个包注释文件,
  说明"factory 与 aliyun / console 分包是为了避开 `msn → aliyun → msn` 的 import cycle"。
  按 provider 选实现的代码在 `cmd/server/main.go` 的 `buildSmsSender` 里。
- ⚠️ **守卫要放在 `smsaudit` 装饰器的里面** —— 装配顺序是
  `smsaudit.Wrap(dynamicSender)`:守卫拦下的这次发送同样要落 `sys_sms_send_log`
  (带错误原因),否则"发不出去"这件事在审计表里查不到。

## 相关

- 源码:`internal/pkg/msn/`(后端子包 `internal/pkg/msn/aliyun/`、`internal/pkg/msn/console/`)
- 可运行示例:`examples/msn/main.go` —— `go run ./examples/msn`
- 模板审核流与发送前守卫 → [smstpl](smstpl.md)
- 发送限流(冷却 / 1 分钟 / 1 天三道闸)→ [smslimit](smslimit.md)
- 发送审计(`sys_sms_send_log`)→ [smsaudit](../audit/smsaudit.md)
- 配置中心覆盖规则 → [配置体系](../getting-started/configuration.md)
