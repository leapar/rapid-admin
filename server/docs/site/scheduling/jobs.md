# jobs —— 平台内置定时任务(框架任务注册 + 执行记录清理)

> 框架自己也需要定时干活:清理定时任务的执行记录、检查密钥轮转就绪。
> 这些任务**不归任何业务模块**,但它们不能散落在各个业务包里 ——
> 散开的结果是没人知道总共有几个框架任务在跑、它们的 cron 表达式是什么。
> jobs 统一注册、统一命名(`framework.*` 前缀)。

## 功能

jobs 只放**框架层自己**的任务,一共两个:

| job key | 名字 | cron | 干什么 |
|---|---|---|---|
| `framework.job_history_purge` | 定时任务执行记录清理 | `17 4 * * *` | 按保留天数 + 条数上限清理 `sys_job_run` |
| `framework.kms_rotate_check` | 密钥轮转就绪检查 | `23 9 * * *` | 探测各密钥 tier 的加解密往返能力,对"历史版本堆积 / 新 key 没切 Active"告警 |

两条设计约定,继承自 [cronx](cronx.md):

1. **注册是一次性的**,由 main 装配期调 `RegisterAll` 一次。
   `cronx.Register` 撞 key 会 panic,所以它不能出现在任何会被调两次的路径上
   (包括单测)—— 单测直接调导出的 Handler,不碰注册表。
2. **cron 表达式全部避开整点整分**。所有副本都按绝对墙钟触发,
   `0 4 * * *` 意味着每个副本在 04:00:00.000 同时醒来:即使有去重锁,
   同时发起 N 个加锁请求本身也是无谓的压力,而去重窗口
   (Layer 0 的 SELECT + Layer 1 的 SETNX)会在那一刻最拥挤。
   错开到 17 / 23 分让各副本、不同任务自然错开,顺手也让人读日志时能一眼
   认出是哪个任务。

### 两条清理规则是**双保险**,不是二选一

- **按天数**:防"低频任务长尾堆积"—— 一天一条的任务留一年也就 365 行,
  但 5 年就是 1800 行,而且 3 年前那次失败再也查不到了。
- **按条数**:防"高频任务一天几万条"—— 保留 30 天也可能是几百万行。

之所以要双保险,是因为**清理历史的任务本身也是定时任务**:它挂了没人知道,
兜底线必须不依赖它自己。

### 密钥轮转检查检查的是 **key registry,不是业务数据**

它**不扫描任何业务表,不 re-encrypt 任何字段** —— 那些是各业务模块自己的事
(每张表的密文列在哪、怎么批量重写,只有业务自己知道)。这个任务只回答两个问题:

1. "现在加密 / 解密这条路走得通吗?"
2. "轮转流程有没有停在半路上?"(新 key 没切 Active / 旧 key 一直不回收)

它**不解决**:

- 不放业务任务 —— 业务任务由各业务模块在自己的包里 `cronx.Register`
  (约定前缀 `biz.*`)。
- 不做业务数据的批量重加密。
- 不做调度机制本身 —— 那是 [cronx](cronx.md) 的事。

## 配置

### yaml 侧(`job:` 块中的两个键)

```yaml
job:
  # 执行记录(sys_job_run)清理 —— 由内置任务 framework.job_history_purge 执行
  history_retention_days: 30
  history_retention_max: 50000
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `job.history_retention_days` | int | `30` | 执行记录保留天数。`0` = 不按天数清 |
| `job.history_retention_max` | int | `50000` | 执行记录条数上限。`0` = 不按条数清 |

⚠️ **两个都为 0 时任务会返 error**,不是静默什么都不做 —— 见下方注意事项。

`job:` 块的其余键(`enabled` / `timezone` / `lock_mode` / `lock_ttl` /
`lock_renew` / `default_timeout` / `shutdown_timeout`)属于 [cronx](cronx.md)。

### 代码侧

```go
jobs.RegisterAll(db *gorm.DB, cfg config.JobConfig)   // main 装配期调**一次**
```

`RegisterAll` 传 `db` 而不是自己建 store:store 由 `cronx.New` 那条线建,
同一个 `*gorm.DB`。这里只再包一层 `cronx.Store` 传给 Handler ——
让 purge 任务拿到裸 DB 后自己拼删除语句的话,清理规则会在两处各有一份实现,迟早漂移。

## 使用方法

完整示例:`examples/jobs/main.go`。

### 一、注册(必须在 `cronx.New` / `Start` 之前,且只调一次)

```go
cfg := config.JobConfig{
    Enabled:              true,
    TimeZone:             "Asia/Shanghai",
    LockMode:             "memory",
    LockTTL:              5 * time.Minute,
    DefaultTimeout:       10 * time.Minute,
    ShutdownTimeout:      30 * time.Second,
    HistoryRetentionDays: 30,
    HistoryRetentionMax:  2,
}

// ⚠️ RegisterAll 只能调**一次** —— 撞 key 会 panic。
// 它必须发生在 cronx.New 之前(注册表要先有东西,调度器才知道要调度谁)。
jobs.RegisterAll(db, cfg)
```

### 二、装配调度器

```go
sched, err := cronx.New(cronx.Config{
    Enabled:         true,
    TimeZone:        cfg.TimeZone,
    LockMode:        cfg.LockMode,
    LockTTL:         cfg.LockTTL,
    DefaultTimeout:  cfg.DefaultTimeout,
    ShutdownTimeout: cfg.ShutdownTimeout,
}, cronx.NewStore(db), nil)
if err != nil {
    fmt.Println("建调度器失败:", err)
    return
}
cronx.SetDefault(sched)
```

### 三、查看注册结果

```go
for _, e := range sched.List() {
    fmt.Printf("%-30s cron=%-12s 超时=%v\n", e.Key, e.CronExpr, e.Timeout)
}
// framework.job_history_purge   cron=17 4 * * *    超时=0s
// framework.kms_rotate_check     cron=23 9 * * *    超时=5m0s
```

### 四、手动触发

```go
rec, err := sched.Trigger(ctx, jobs.KeyJobHistoryPurge, cronx.TriggerManual)
rec2, err := sched.Trigger(ctx, jobs.KeyKMSRotateCheck, cronx.TriggerManual)
```

`jobs.KeyJobHistoryPurge` / `jobs.KeyKMSRotateCheck` 是导出的常量,
seed / 文档 / 测试都引用它们,不要在别处硬编码字符串。

## 注意事项

- ⚠️ **`RegisterAll` 只能调一次**,且**必须早于** `cronx.New` / `Start`。
  `cronx.Register` 撞 key 会 panic;而注册表在 `Start` 里被一次性快照 ——
  快照之后才注册的任务永远没有 cron entry,`NextRunAt` 是空的,而且**没有任何报错**。

- ⚠️ **两个保留参数都为 0 时返 error,不是静默什么都不做**。
  那多半是配置写错了(`history_retention_days: 0`),而一个每天都显示
  "执行成功"、实际什么都没清的任务,正是最难被发现的那种坏。

- ⚠️ **清理任务自己的执行记录也在这张表里** —— 所以每次清理完会多出一行
  (它自己那条)。这不是漏删,是正常现象。

- ⚠️ **`PurgeHistory` 是物理删(`Unscoped`),不是软删**。
  `model.SysJobRun` 带 `gorm.DeletedAt`,默认 `Delete()` 是软删:行还在表里,
  只是 `deleted_at` 被盖了时间戳。而清理的**存在意义就是让表变小** ——
  软删之后表只会越涨越大,任务跑得再勤也一行都不少,而且**没有任何报错**。
  探针实测:删 5 条后 `Count()` 返 0(看不见了),`Unscoped().Count()` 仍返 5
  (物理行还在)—— 典型的假绿灯。软删对这张纯日志表没有任何价值。

- ⚠️ **删了 0 条是常态**(表还没到保留期),记 Debug 而不是 Warn。
  每天一条 Warn 刷屏,会把真正的失败淹掉。

- ⚠️ **ctx 取消与 DB 故障的错误文案不同**:
  `执行记录清理被中断(已删除 N 条)` vs `执行记录清理失败(已删除 N 条)`。
  前者不是"清理失败"而是"清理没做完",排障的人不该以为是 DB 挂了。

- ⚠️ **密钥轮转检查里"tier 没配" ≠ "配坏了"**。只用 PII 加密、不加密 Secret
  的部署完全合法,三个 tier 都没配更是常态(没启 KMS 的极简部署)——
  那就只记一条 Info,别天天报 WARN。**只有"某个已配置的 tier 加解密往返失败"
  才返 error**。

- ⚠️ **"保留版本数超过 2"和"active 不是最高版本"都只告警、不让任务失败**。
  前者说明轮转做完很久没人回收(轮转等于没做),后者可能发生在
  "先把新 key 灌进去、观察几天再切"的轮转流程里 —— 那是**故意的**。
  但必须有人看见,所以告警不吞。

- ⚠️ **"没有 v1 legacy key"也只告警不失败**。新部署从没写过 v1 格式密文,
  没有 legacy key 完全正常;老部署丢了它才是事故 —— 但那种情况下
  业务侧解密的报错会先炸出来。

- ⚠️ **`kms_rotate_check` 不需要声明 `NonIdempotent`**。它是纯读 + 纯内存往返,
  跑两遍结果一样,所以在锁降级时照常跑(非幂等任务在降级模式下会被直接跳过,
  本任务没必要付这个代价)。

- ⚠️ **`framework.job_history_purge` 刻意不给更长的 `Timeout`**(留 0 →
  走 `job.default_timeout`)。这是纯 DB 操作,跑不完的原因是表太大或 DB 卡了,
  给更多时间只会让"卡住的清理"占着锁更久,拖慢其他任务的排查。

- ⚠️ **`framework.kms_rotate_check` 只有一个 5 分钟的 `Timeout`**,
  它拿的是全局锁 —— ctx 检查放在循环里,不让一个已经超时的任务继续占着锁。

## 相关

- 源码:`internal/pkg/jobs/`
- 可运行示例:`examples/jobs/main.go` —— `go run ./examples/jobs`
- 调度机制(注册表 / 两层去重 / 优雅停机)→ [cronx](cronx.md)
- 密钥管理与轮转 → [kms](../security/kms.md)
- 指标:`app_job_runs_total{job,trigger,status}`(见 [metricsx](../infrastructure/metricsx.md))
- 配置体系 → [配置体系](../getting-started/configuration.md)
