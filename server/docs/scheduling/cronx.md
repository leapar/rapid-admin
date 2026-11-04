# cronx —— 定时任务调度(代码注册 + DB 覆盖 + 两层去重)

> 定时任务要处理两件事:任务本体在代码里(后台只能改频率和启停),
> 运行侧保证多副本下**同一个任务只有一个实例在跑**。
> cronx 提供**代码注册表**、**DB 覆盖**、**两层去重**、**执行记录**与**优雅停机**。
>
> ⚠️ **Windows 上没有官方 Redis** —— `job.lock_mode=redis`(生产多副本必须)之前
> 先装一个:[Memurai](https://www.memurai.com/get-memurai)(Redis 协议兼容,
> 装成 Windows 服务,默认 `127.0.0.1:6379`、无密码)。
> **Developer 版仅供开发/测试,不许上生产。** 详见 [快速开始](../getting-started/README.md)。

## 功能

cronx 解决五件事:

1. **代码注册表**:任务本体只写在代码里(`cronx.Register`)。
   DB(`sys_job`表)**只能覆盖 cron 表达式和启停**,凭空长不出任务 ——
   那样长出来的任务没有代码,重启即消失,而且没人知道它曾经存在过。
2. **DB 覆盖**:后台改频率 / 改启停 / 改备注,写 `sys_job` 行;
   运行期热更新(`Reload`)会**立刻重注册 cron entry**,不用重启进程。
3. **两层去重**,各挡各的,只上一层都不够:
   - **Layer 0 查库**:`SELECT ... FROM sys_job_run WHERE job_key=? AND status='running'`
     挡住"上一次还没跑完就又跑"。不依赖 Redis —— DB 是所有副本都有的东西,
     比任何外部锁服务都少一个失效点,执行记录表本来就要建,去重是白送的。
   - **Layer 1 加锁**:Redis `SETNX` + Lua CAS 释放 + 看门狗续期。
     挡住"两个副本在同一毫秒同时开跑" —— robfig 按**绝对墙钟**触发,
     所有副本在同一瞬间醒来,Layer 0 是 check-then-act,关不掉这个窗口。
4. **降级 fail-open,但降级后仍剩 Layer 0**:锁不可用时**继续跑** + 大声告警 +
   执行记录写 `degraded` 标记。定时任务停摆比偶尔重复执行更难发现
   (同步不更新、过期授权不下榜,都是静默的),但重复执行会写坏数据。
   折中是把安全性建立在"**handler 必须幂等**"这条硬契约上,
   显式声明 `NonIdempotent` 的任务在降级模式下被**主动跳过**。
5. **执行记录 + 崩溃回收 + 优雅停机**:每次执行落一条 `sys_job_run`;
   启动时回收进程崩溃遗留的 `running` 行;停机时先停止触发、等在跑的任务、
   预算耗尽再强杀。

它**不解决**:

- 不提供"业务定时任务"。业务任务由各业务模块在自己的包里
  `cronx.Register`(约定前缀 `biz.*`),框架层只管调度机制。
- 不做分布式事务、不做任务编排(DAG / 依赖 / 重试补偿)。
- 不管"执行记录保留多久" —— 那是 [jobs](jobs.md) 里
  `framework.job_history_purge` 任务的职责。

## 配置

### yaml 侧(`job:` 块)

```yaml
job:
  enabled: true          # 总开关。false ⇒ 一个 cron entry 都不注册
  timezone: Asia/Shanghai  # 调度时区。配错的名字会**启动直接失败**

  # 去重层 1(分布式锁)的存储:
  #   auto   — 有 Redis 用 Redis,没有退进程内锁 + 降级告警(单副本安全)
  #   redis  — 强制 Redis,没配直接**拒绝启动**(生产多副本必须用这个)
  #   memory — 强制进程内锁,即使配了 Redis 也不连(仅单副本)
  #   off    — 完全不锁,仅本地开发
  lock_mode: auto
  lock_ttl: 30s          # 锁租约。长任务靠看门狗每 lock_ttl/3 续期
  lock_renew: true       # 关掉看门狗 = 长任务跑到一半锁过期,别的副本抢进来并发写
  default_timeout: 30m   # 单次执行超时,任务未单独指定时的兜底
  shutdown_timeout: 30s  # 优雅停机预算,用完强杀在跑任务(记 failed)

  # 执行记录(sys_job_run)清理 —— 由内置任务 framework.job_history_purge 执行
  history_retention_days: 30
  history_retention_max: 50000
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `job.enabled` | bool | `true` | 调度总开关。关掉后**既不注册也不触发** |
| `job.timezone` | string | `Asia/Shanghai` | 调度时区。**必须显式配**,见下方注意事项 |
| `job.lock_mode` | string | `auto` | `auto` / `redis` / `memory` / `off` |
| `job.lock_ttl` | duration | `30s` | 锁的**故障恢复时间**(持锁副本崩溃后别人最多等这么久),不是任务时长上限 |
| `job.lock_renew` | bool | `true` | 看门狗续期开关 |
| `job.default_timeout` | duration | `30m` | `Job.Timeout` 为 0 时用它 |
| `job.shutdown_timeout` | duration | `30s` | 停机预算,用完强杀 |
| `job.history_retention_days` | int | `30` | 执行记录保留天数(归 [jobs](jobs.md) 用) |
| `job.history_retention_max` | int | `50000` | 执行记录条数上限(同上) |

### 代码侧(构造选项)

```go
cronx.Register(job Job)                      // 注册任务(包 init 或 main 装配期调)
cronx.New(cfg Config, store Store, locker Locker) (*Scheduler, error)
cronx.ResolveLocker(mode string, rdb *redis.Client, prefix string) (Locker, error)
cronx.NewStore(db *gorm.DB) Store            // gorm 实现;db 为 nil ⇒ 无记录、无 Layer 0
cronx.SetDefault(s *Scheduler)               // 登记全局单例
cronx.Default() *Scheduler                   // 取全局单例;未装配返 nil(不 panic)
cronx.InitMetrics(reg prometheus.Registerer) // 注册指标(重复调用 panic)
```

`Config` 字段:`Enabled` / `TimeZone` / `LockMode` / `LockTTL` / `LockRenew` /
`DefaultTimeout` / `ShutdownTimeout` / `OnFinish`。

⚠️ `Config` **刻意没有**历史保留期字段 —— 清理执行记录不是调度器的内建行为,
曾经声明过 `HistoryKeepDays` / `HistoryKeepMax` 两个字段,一行都没读过。

`OnFinish func(ctx context.Context, rec *model.SysJobRun)` 是**回调**,
不是直接 import `pkg/push`:依赖方向是 `cmd/server → cronx`,
cronx 自己不知道"事件要推给谁、推成什么形状、哪个 topic 需要哪个权限"。
传 `nil` = 不通知(零开销)。

## 使用方法

完整示例:`examples/cronx/main.go`。

### 一、注册任务(必须早于 `cronx.New` / `Start`)

```go
cronx.Register(cronx.Job{
    Key:         "framework.demo_purge",
    Name:        "示例:清理过期数据",
    Description: "演示用的框架任务",
    Cron:        "0 3 * * *",
    Enabled:     true,
    Handler: func(ctx context.Context) error {
        // 真实任务在这里做清理
        return nil
    },
})
```

`Job` 的字段:`Key` / `Name` / `Description` / `Cron` / `Timeout` / `Enabled` /
`NonIdempotent` / `Handler`。

⚠️ **`Handler` 必须尊重 `ctx`** —— ctx 到 deadline 会取消;不检查 ctx 的任务会一直跑,
而它的锁到期后会被第二个副本抢走,两个副本并发写同一批数据。

### 二、装配 + 启动 + 停机

```go
// 生产装配:
locker, err := cronx.ResolveLocker(cfg.Job.LockMode, repository.GetRedis(), cronx.DefaultLockKeyPrefix)
if err != nil {
    log.Fatal("cronx: 锁模式装配失败,拒绝启动", zap.Error(err))
}
sched, err := cronx.New(cronx.Config{
    Enabled:         true,
    TimeZone:        "Asia/Shanghai",
    LockMode:        "redis",
    LockTTL:         5 * time.Minute,
    LockRenew:       true,
    DefaultTimeout:  10 * time.Minute,
    ShutdownTimeout: 30 * time.Second,
}, cronx.NewStore(db), locker)
if err != nil {
    log.Fatal("cronx: 调度器装配失败,拒绝启动", zap.Error(err))
}
cronx.SetDefault(sched)
if err := sched.Start(context.Background()); err != nil {
    log.Error("cronx: 启动失败,定时任务未运行", zap.Error(err))
}
defer sched.Shutdown(ctx)
```

`Start` 做三件事:读 DB 覆盖值 + reconcile 残留行 → 回收崩溃遗留的 `running` 行 →
逐个注册 cron entry。

### 三、表达式校验与预览

```go
schedule, err := cronx.ParseCron("0 3 * * *")             // 用与调度器完全相同的 parser
runs, err := cronx.NextRuns("0 3 * * *", 3, time.Local)   // 未来 3 次触发时间
```

`NextRuns` 是纯计算,成本极低,用来在后台"改频率"时实时预览"下一次是几点"。

### 四、触发

```go
// 同步(手动触发,HTTP handler 直接调):
run, err := sched.Trigger(ctx, "framework.demo_purge", cronx.TriggerManual)

// 异步(cron 回调用):
sched.TriggerNow(ctx, "framework.demo_purge", cronx.TriggerSchedule)
```

`Trigger` 返回 `*model.SysJobRun`(本次执行记录),错误有四种语义**不同**的状态:

| 错误 | 含义 | 运维处置 |
|---|---|---|
| `ErrJobNotFound` | 代码注册表里没这个 key | 查注册 |
| `ErrJobDisabled` | `code.Enabled && DB.Enabled` 为 false | 查启停 |
| `ErrJobRunning` | 上一次还在跑 / 锁被别人持有 | 正常 |
| `ErrDegraded` | 去重能力降级,本次主动放弃(仅 `NonIdempotent` 任务) | **要去查 Redis** |

### 五、查状态 / 热更新

```go
list := sched.List()                        // 全部任务的生效配置
eff, err := sched.Get("framework.demo_purge")
sched.NextRunAt("framework.demo_purge")     // 下次触发时间(未注册则返零值)
sched.Runtime()                             // RuntimeInfo:锁是否可用、实例 ID、已注册数
sched.Override("framework.demo_purge")      // 当前 DB 覆盖行(后台"恢复默认"用)
sched.Reload("framework.demo_purge", ov)    // 应用 DB 覆盖 + 立刻重注册 cron entry
```

`Effective` 同时带**生效值**与**代码默认值**(`CronExpr` / `CronOverridden` / `DBEnabled`),
后台要同时显示两者 —— 否则排查时不知道跑的是哪个。

### 六、给执行记录带上操作人

```go
ctx = cronx.WithOperator(ctx, "zhangwei")   // API 层拿到 Caller 后塞进来
```

cronx 是 pkg 层,不能 import `internal/service`(会形成反向依赖环),
操作人只能靠 ctx 传。

## 注意事项

- ⚠️ **`cronx.Register` 撞 key 会 panic**,而且注册**必须早于** `cronx.New` / `Start`。
  注册表在 `Start` 里被**一次性快照**(`registeredKeys()`):快照之后才注册的任务
  永远没有 cron entry,`NextRunAt` 是零值,而且**没有任何报错、没有日志**。
  全局只能注册一次 —— 所以 `RegisterAll` 这类"注册多个任务"的入口只能由
  main 装配期调一次。

- ⚠️ **cron 表达式全部避开整点整分**。所有副本按**绝对墙钟**触发,
  `0 4 * * *` 意味着每个副本在 04:00:00.000 同时醒来:即使有去重锁,
  同时发起 N 个加锁请求本身也是无谓压力,而去重窗口(Layer 0 的 SELECT +
  Layer 1 的 SETNX)会在那一刻最拥挤。本仓两个框架任务错开在
  `17 4 * * *` 和 `23 9 * * *`。

- ⚠️ **`job.timezone` 必须显式配**。容器里 TZ 没设时 `time.Local == UTC`,
  `0 3 * * *` 会在**北京时间 11:00** 跑,而配置文件里明明写着 `Asia/Shanghai` ——
  **看起来配置生效了,实际没生效**,因为任务确实跑了,只是跑在了错误的时间。
  配错的名字(时区名写错)会**启动直接失败**,不会带着错时区继续跑。

- ⚠️ **`job.enabled: false` ⇒ 一个 entry 都不注册**,`Start` 打一行 Info 就返回 nil。
  没有错误、没有告警级别,症状是"所有任务永远不跑"。
  (yaml 里**漏配**不会触发这个 —— viper 有 `job.enabled` 默认 `true`。)

- ⚠️ **启动日志里的 `registered` 数字会把"已停用的任务"也算进去**。
  `Start` 里 `failed` 只统计**注册出错**的任务,`EnabledEffective()` 为 false 的任务
  `register()` 直接返 nil、不进 `entries`,但它仍算在 `len(keys)-failed` 里。
  真实注册数看 `sched.Runtime().Registered`(= `len(s.entries)`)。

- ⚠️ **最终生效 = `code.Enabled && DB.Enabled`**(AND,不是只看 DB)。
  这样做是刻意的:**从代码里删掉任务一定等于停用**,不会因为 DB 残留行而"复活"。
  反过来说,DB 里存在但代码里没有的 `job_key` 会在启动时被 `reconcile` 置
  `is_active=false` 并 WARN —— **不是直接删**:删掉会丢掉运维当初设的 cron 覆盖值和备注。

- ⚠️ **`lock_mode=redis` 而没配 Redis ⇒ 启动失败**(`ResolveLocker` 返 error,
  `cmd/server` 直接 Fatal)。`auto` 没配 Redis 则退进程内锁 + 降级告警 ——
  多副本部署下这意味着**同一个任务会被多个副本同时跑**。
  未知 `lock_mode` 也直接报错,不静默当 `auto` 处理("我配了 X,系统跑的是 Y"
  是最难查的一类配置问题)。

- ⚠️ **锁的释放与续期必须是 CAS(Lua)**。裸 `DEL` 的路径是:
  A 跑超时 → 锁 TTL 到期 → B 抢到锁开始跑 → A 结束时裸 `DEL`
  → **把 B 的锁删了** → C 立刻抢到并发跑。CAS 是这条路径上唯一的防线。

- ⚠️ **owner token 必须每次运行都不同**。用固定的 instanceID 的话,
  同一副本重试两次会拿到同一个 owner,第一次的 CAS 释放会误删第二次的锁。

- ⚠️ **看门狗发现锁丢了必须真的 `cancel()` 掉 run ctx**,只 `return` 不够 ——
  handler 拿不到任何信号,会照跑到底,于是两个副本真的并发写同一批数据。

- ⚠️ **两层各自独立降级,不能合成一个 degraded 开关**。早先把它们合成一个,
  结果"DB 查不到"会连带把加锁整段跳过 —— 而 Redis 明明好着,
  跨副本互斥恰恰是最该保住的那一层。现在 `lockUsable` 只看锁层,
  `layer0OK` 只看查库层,`degraded` 只是给执行记录 / 指标用的**汇总标记**。

- ⚠️ **`Reload` 漏掉重注册是最容易犯的错**:只写 DB 的话,robfig 里注册的还是旧表达式,
  改频率要等进程重启才生效 —— 而且**没有报错、没有日志**,测试也可能绿
  (如果测试只断言 DB 变化、不断言 entry 生效)。`Reload` 失败时会**真回滚**:
  撤销 entry、用旧值重新 `register`,不允许留下"DB 说启用、实际没注册"的状态。

- ⚠️ **DB 覆盖行的 `Description` 必须在 `LoadOverrides` 里读出来**。漏掉的话:
  运营在后台写备注,当次生效(`Reload` 拿到的是完整 ov),重启后读回来的
  `Description` 是空 → `merge` 判 `ov.Description != ""` 不成立 →
  悄悄回退到代码默认值。症状是"**备注自己消失了**",没有任何报错。

- ⚠️ **`cronx.InitMetrics` 重复调用会 panic**(prometheus 不允许重名注册)。
  这是刻意的:让"初始化两次"在启动时炸,而不是让第二个 registry 拿到空指标。

- ⚠️ **`Default()` 未装配时返 `nil` 而不是 panic**。API 层要能在
  `job.enabled=false` 时也把页面正常渲染出来(告诉运维"调度器没开"),
  而不是整个后台 500。

- ⚠️ **`cronx.Config` 里没有历史保留期字段**。清理执行记录是
  [jobs](jobs.md) 的 `framework.job_history_purge` 任务的事,
  别在 `cronx.Config` 里找它。

- ⚠️ **`Shutdown` 的预算用 `cfg.ShutdownTimeout`**,调用方传的 ctx **没有 deadline 时也一样生效**
  —— 不是只在 main.go 恰好传对时才有预算。预算耗尽后强杀在跑任务(记 failed)。

- ⚠️ **`Handler` 返回的 error 会被截断到 2KB**(`truncate`),panic 的 stack 也是
  (走 `safeInvoke` 兜住,记成 failed)。手动触发走的是自己的 `Trigger` 路径,
  `cron.Recover` 只兜 robfig 那条路,所以 `safeInvoke` 是独立的第二道防线。

## 相关

- 源码:`internal/pkg/cronx/`
- 可运行示例:`examples/cronx/main.go` —— `go run ./examples/cronx`
- 平台内置任务(执行记录清理 / 密钥轮转检查)→ [jobs](jobs.md)
- 指标:`app_job_runs_total{job,trigger,status}` / `app_job_duration_seconds{job}` /
  `app_job_lock_skipped_total{job}` / `app_job_degraded_total{job}`
  (见 [metricsx](../infrastructure/metricsx.md))
- 配置体系与"漏配必须等于照常跑"→ [配置体系](../getting-started/configuration.md)
