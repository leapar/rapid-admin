# smstpl —— 短信模板审核流 + 发送前守卫

> 模板在阿里云那边过没过审,平台此前**完全不知道** —— 服务端日志只有一句
> `isv.SMS_TEMPLATE_ILLEGAL`,而驳回理由躺在阿里云控制台里没人看。
> smstpl 做三件事:**知道状态、提前失败、留痕**。

## 功能

### 三条硬规则

1. **只有服务商能判"通过 / 驳回"**。人工只能"停用"或"标记为待审核"。
   反过来的话,运营在面板上点一下"已通过"就绕过了阿里云的审核 ——
   而模板没过审时阿里云会拒发,于是面板显示"正常"、用户永远收不到验证码。
2. **人工停用的模板,服务商同步不能自动复活**。停用是止血手段,
   同步任务每 10 分钟跑一次,不能因为服务商说"通过"就把运维刚停用的东西又打开。
3. **换了模板号,原来的审核状态立即作废**。审核的是**模板号**,不是场景;
   换了号就是另一张模板,拿旧号的通过状态去担保新号 = 假保证。

### 三个角色

| 角色 | 类型 | 职责 |
|---|---|---|
| `Registry` 登记册 | 运行期权威状态 | 谁在用哪张模板、这张模板过没过审、流转历史 |
| `Store` 持久化 | 快照(DB) | `sys_sms_template` 表 + `sys_sms_template_log` 表 |
| `Syncer` 同步器 | 拉服务商状态 | 把服务商的真实审核态拉回来写进登记册,再落库 |
| `Guard` 守卫 | 发送前闸门 | 确定发不出去时,在发出去之前拦下并说清原因 |
| `Runtime` 运行期装配 | 捆住上面四样 | 让"后台页面 / 定时同步 / 发送链路"三处共用同一份装配 |

### 状态机

7 个状态(`smstpl.Status`):

| 状态 | 含义 | `Usable()` | `Blocked()` |
|---|---|---|---|
| `StatusUnknown` | 没登记 / 还没同步过 | false | false |
| `StatusPending` | 已登记,还没送审 | false | false |
| `StatusReviewing` | 服务商侧正在审 | false | false |
| `StatusApproved` | 审核通过 | **true** | false |
| `StatusRejected` | 服务商驳回 | false | **true** |
| `StatusCancelled` | 服务商撤回了这次审核 | false | **true** |
| `StatusDisabled` | 人工停用 | false | **true** |

⚠️ **`Usable()` 和 `Blocked()` 的口径不同是故意的**:`Usable()` 只用于**展示**
(页面要如实显示"还在审"),`Blocked()` 是守卫判据(守卫不能因为"还在审"
就把验证码通道锁死)。

**不解决**:模板正文与参数(那是阿里云侧的事,平台只认模板号);
谁在用哪个场景(模板号的**唯一真源在配置中心** `aliyun.sms.template.*`,
本包只记"这张模板过没过审")。

## 配置

### yaml 侧(`sms.template_guard:`)

```yaml
sms:
  template_guard: block  # off / warn / block
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `sms.template_guard` | string | `block` | 守卫策略。`off` 不检查 / `warn` 检查但只告警 / `block` 拦下 |

⚠️ **`sms.template_guard` 刻意不进配置中心** —— 配置中心的 `aliyun.sms.*` 是
"凭证 / 模板号"这类运营必须能改的东西;守卫策略是**安全开关**,
能从面板一键关掉就等于没有。改它只能改部署配置(改完重启)。

⚠️ **写错值也落到 `block`**(`smstpl.ParsePolicy` 的 `default` 分支)。
理由:配置写错(比如 `"blcok"`)时,"没检查"意味着整套模板审核功能**静默失效**,
而"检查"最坏结果是拦掉一张确定不可用的模板。**失效比误拦危险**。

### 代码侧(装配选项)

```go
rt := smstpl.NewRuntime(smstpl.RuntimeOptions{
    Store:     smstpl.NewSQLStore(db),
    Resolve:   func(ctx context.Context) (config.SMSConfig, error) { return ccSvc.ResolveSMSConfig(ctx, cfg.SMS) },
    NewFetcher: buildSmsTemplateFetcher,
    Policy:    smstpl.ParsePolicy(cfg.SMS.TemplateGuard),
})
```

| `RuntimeOptions` 字段 | 说明 |
|---|---|
| `Registry` | 登记册;为 nil 时自动建一个空的 |
| `Store` | 持久化;为 nil 时只留内存(不落库) |
| `Resolve` | 取"当前生效"的短信配置;为 nil 时没有模板号可用(场景码表为空) |
| `NewFetcher` | 造服务商状态查询器;为 nil 时同步不了(返 `ErrNoFetcher`) |
| `Policy` | 守卫策略;默认 `block` |

## 使用方法

### 登记册 + 状态机(纯内存)

```go
reg := smstpl.NewRegistry()

// 登记模板号:第一次登记不记历史
changed, tr, err := reg.Register(smstpl.SceneLogin, "SMS_244570823")

// 服务商同步回来的结论
changed, tr, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusApproved,
    smstpl.SourceProvider, "阿里云审核通过", "")

// 人工动作:只能停用 / 标记为待审核
changed, tr, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusDisabled,
    smstpl.SourceManual, "模板内容涉嫌违规,先止血", "zhangsan")
```

返回值里的 `tr` 是本次记进历史的流转;**没记历史时是零值**(`tr.At.IsZero()` 为 true)。

### 发送前守卫

```go
guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)

d := guard.Evaluate(smstpl.SceneLogin, "SMS_244570823")
if d.Blocked {
    return d.Err     // 错误原文带"是哪张模板、为什么"
}
// 只想要一个 error:
err := guard.Check(smstpl.SceneLogin, "SMS_244570823")
```

守卫的返回值 `smstpl.Decision`:

| 字段 | 含义 |
|---|---|
| `Blocked` | 是否拦下 |
| `Status` | 判定时登记册里的状态(未登记时是 `unknown`) |
| `Reason` | 人类可读的原因 —— **非空就该记一条日志**(两种策略下都有值) |
| `Err` | 拦下时的错误(`blocked=false` 时为 nil) |

⚠️ **`warn` 策略下 `Check` 拿不到错误** —— 想记日志就用 `Evaluate`,
它的 `Reason` 在两种策略下都有值。

### 运行期装配(三处共用一份)

```go
rt := smstpl.NewRuntime(smstpl.RuntimeOptions{ /* 见上 */ })

// ⚠️ 启动时必须调一次
_ = rt.Load(ctx)

// 定时任务:跑一轮同步(每 10 分钟)
res, err := rt.SyncOnce(ctx)

// 后台页面:人工停用 / 恢复
tpl, err := rt.ApplyManual(ctx, smstpl.SceneLogin, smstpl.StatusDisabled, "先止血", "zhangsan")
```

`SyncResult` 的字段:

| 字段 | 含义 |
|---|---|
| `Registered` | 模板号变了(旧状态被作废)的场景 |
| `Updated` | 状态变了(拿到新结论)的场景 |
| `Unchanged` | 状态没变的场景数 |
| `Attempted` | 本轮**真正去查了**的场景数 |
| `Failed` | 拉取失败 / 状态被拒绝的场景(**状态保持原样**) |
| `Undetermined` | 没能确定状态的场景数 |
| `Failures` | 每个失败场景的**原因**(人话) |

`res.AllFailed()` 的判据是 `Attempted > 0 && Undetermined == Attempted` ——
"这一轮一个状态都没定下来"才算系统性失败。

## 注意事项

- ⚠️ **模板没审核通过 ⇒ 拒发,而不是"用默认模板糊过去"**。
  `rejected` / `cancelled` / `disabled` 三种状态会被守卫**当场拦下**,
  返回的错误原文带着场景和原因(上层会把它拼进给用户的提示,运营看到的是
  "模板被服务商驳回"而不是"发送失败")。
- ⚠️ **模板号没配 ⇒ 也发不出去,但不是守卫拦的**。守卫对"没登记的场景"放行
  (不知道 ≠ 确定不行),真正拦住的是阿里云后端:
  `aliyun sms: scene=%q 的模板号未配置`。
  `smstpl.TemplateCode(cfg, scene)` 取不到时返回**空串**,**不会编一个默认模板号** ——
  编出来的号会带着"没配模板"的假状态进登记册,页面上显示成一个像模像样的模板。
- ⚠️ **"不确定"一律放行**:`unknown` / `pending` / `reviewing` 都不拦。
  理由:同步任务一挂,状态就永远停在 `pending`,此时拦掉 = **平台自己把登录验证码
  通道锁死,而且没人知道为什么**。
- ⚠️ **登记册里的号 ≠ 这次要发的号 ⇒ 放行**(不是拦)。配置中心刚换了号、
  同步还没跑到,此时拿旧号的"已驳回"去拦新号 = **拦错对象**。
  这种情况 `Decision.Reason` 会有值("模板号已变更(登记册=X,本次=Y),审核状态待同步"),
  调用方该记一条日志。
- ⚠️ **换号 ⇒ 旧状态立即作废,回到 `unknown`**(`Registry.Register` 里做的)。
  因为 `unknown` 不拦,所以**换号到同步跑完之前,这张新模板是放行的** ——
  守卫拦不住"新号还没审"这件事,那是服务商侧的事。
- ⚠️ **`Runtime.Load` 必须在启动时调一次**。不调的话,进程重启后登记册是空的,
  人工停用的模板会被下一轮同步"复活"(服务商说通过 → `approved`)——
  硬规则 2 在重启后就失效了。装载失败不退出进程,但要**大声记一条 error**:
  登记册空了 ⇒ 守卫放行一切 ⇒ 人工停用形同虚设。
- ⚠️ **`ErrNoFetcher` 不是故障,是"这个 provider 没这个能力"**。
  `console` 通道只把验证码打到日志,压根没有模板审核状态可言。
  调用方要能区分"服务商挂了"(该重试)和"这个通道不查审核"(别重试了),
  所以单独一个哨兵,而不是塞进通用 error。
  `Runtime.SyncOnce` 里**先问能力、再问有没有东西要查** ——
  顺序反了的话,`console` 通道 + 没配模板号会给出一句"成功,更新 0 个",
  运维看着像"同步过了、没问题"。
- ⚠️ **`Failed` 与 `Undetermined` 不是一回事**。`Failed` 里混着"状态机按规则拒绝流转":
  服务商**已经给了结论**(比如"审核通过"),只是规则不让写
  (人工停用的模板不许被服务商复活)⇒ 那一行状态是**定下来了**的。
  判"这一轮是不是全挂了"要看 `Undetermined`,不是 `len(Failed)`。
- ⚠️ **拉不到状态 ≠ 被驳回**。拉取失败时**状态保持原样**,绝不改成
  `rejected` / `unknown`,下一轮自动重试。
- ⚠️ **服务商给了一个不认识的状态码 ⇒ 返回 error,不许瞎猜**
  ("不认识就算通过"的后果是:一个新状态被当成 `approved` 写进登记册,
  页面显示"已通过",而实际上那张模板从来没通过审核)。
- ⚠️ **内存里的流转历史有上限(500 条)**,而且**进程重启就没了**。
  落库的完整历史在 `sys_sms_template_log` 表里;`Runtime.History` 优先读 DB,
  没配 `Store` 时才退回内存历史。
- ⚠️ **状态没变且理由没变 ⇒ 不记历史**。服务商每次同步都会报同一个状态,
  每次都记一条的话历史会被"已通过 → 已通过"刷满,真正的那次变化反而找不到。
  理由变了要记(那是新信息)。
- ⚠️ **判据只有一处**(`blockedInfo`)。`Status.Blocked()` 从它派生,
  守卫拦截时的文案也用它。以前是两份表(一个 switch 判状态、另一个 switch 决定
  拦不拦说什么话),加新状态时漏改一处 ⇒ 一张确定不可用的模板照发不误。
  **加新状态时只改 `blockedInfo` 一处就够了。**
- ⚠️ **`smstpl` 刻意不依赖 `msn`** —— 场景常量在 `smstpl` 里定义了一份
  (`smstpl.SceneLogin` 等),因为依赖方向反了会成环。
  场景 → 模板号的映射**只有 `smstpl.TemplateCode` 一处** ——
  以前 aliyun 包一份、msn 一份、配置中心一份,三份真相,合并过。
- ⚠️ **`ParseStatus` 遇未知值报错,不静默归到 `unknown`** ——
  静默归一的话,DB 里存进一个手写错的 `"approvd"`,页面显示"未知",
  而守卫会**放行** ⇒ 一个拼写错误就绕过了审核态检查。

## 相关

- 源码:`internal/pkg/smstpl/`(`guard.go` `machine.go` `registry.go` `runtime.go` `store.go` `sqlstore.go`)
- 可运行示例:`examples/smstpl/main.go` —— `go run ./examples/smstpl`
- 守卫的调用方(短信通道)→ [msn](msn.md)
- 服务商侧状态查询器(`smstpl.Fetcher` 的实现)在 `internal/pkg/msn/aliyun/`
- 通知通道分发(邮件 / 站内信)→ [notifyx](notifyx.md)
- 模板号的唯一真源在配置中心 → [配置体系](../getting-started/configuration.md)
