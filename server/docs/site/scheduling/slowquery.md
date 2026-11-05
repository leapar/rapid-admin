# slowquery —— 慢 SQL 报警(包装 GORM logger + Prometheus 指标)

> "这个接口最近慢了 3 秒,SQL 到底慢在哪一条"—— 慢查询报警。
> 不用自己写 GORM callback(要克隆 Statement、还要挑注册位置,坑很多),
> 包一层 logger 就完事。

## 功能

slowquery 做三件事:

1. **包装 GORM 的 `logger.Interface`**,复用内建的 `Trace(ctx, begin, fc, err)` 钩子
   —— 每条 SQL 必走这个钩子。比对 `time.Since(begin) > Threshold`,超了就触发回调。
   比 callback chain 简单可靠(避开 Statement 克隆 + 注册位置坑)。
2. **两个入口**:
   - `Install(db, cfg)` —— 给**已有** db 包一层(替换 `db.Logger`)
   - `NewLogger(orig, cfg)` —— 直接造一个 `logger.Interface`,塞进
     `gorm.Config{Logger: ...}`
3. **Prometheus 指标** `app_slow_sql_total{table,op,is_error}` ——
   Grafana 上"哪张表最慢"的面板数据源。

**原 logger 不会被顶掉**:`Info` / `Warn` / `Error` / `LogMode` 全是**透传**,
只有 `Trace` 多走一步(比对耗时)。`LogMode` 还会顺手把阈值同步给原 logger
(如果它实现了 `SetSlowThreshold`)。

它**不解决**:

- 不做 SQL 优化、不做慢查询归因分析(没有 EXPLAIN、没有索引建议)。
- **不采集响应体 schema**(那是 [swaggerx](../api-docs/swaggerx.md) 的事)。
- 不记录"哪些接口慢" —— 它只知道 SQL 本身。

## 配置

### yaml 侧

**该包无 yaml 配置块。**

`configs/config.example.yaml` 里没有 `slowquery:` 段 —— 阈值、采样率、日志开关
全部是**代码侧**的构造选项。本仓的装配点在 `cmd/server/main.go`:
阈值写死 `500 * time.Millisecond`、`LogRawSQL: false`、`SampleRate: 1.0`。
想改阈值就改那一行(或改成从配置中心读),没有 yaml 键可填。

### 代码侧(构造选项)

```go
slowquery.Install(db *gorm.DB, cfg Config) error
slowquery.NewLogger(orig logger.Interface, cfg Config) logger.Interface
```

| `Config` 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `Threshold` | `time.Duration` | `500ms` | 单条 SQL 超过这个时间算"慢" |
| `OnSlow` | `func(Entry)` | nil ⇒ 默认回调 | 自定义慢查询回调(接 Sentry / 告警 / 推送) |
| `LogRawSQL` | bool | `false` | 默认回调要不要把完整 SQL 打出来 |
| `SampleRate` | float64 | `1.0` | 采样率 0.0~1.0 |
| `Logger` | `*zap.Logger` | nil | 默认回调用的 zap logger;nil ⇒ **只走 metrics,一条日志都不打** |

`Entry` 字段:`SQL` / `Table` / `Op` / `Duration` / `Rows` / `Error`。

## 使用方法

完整示例:`examples/slowquery/main.go`。

### 一、给已有 db 包一层

```go
var hits []slowquery.Entry
if err := slowquery.Install(db, slowquery.Config{
    Threshold: 10 * time.Millisecond,
    OnSlow: func(e slowquery.Entry) {
        hits = append(hits, e)
    },
}); err != nil {
    fmt.Println("装 slowquery 失败:", err)
    return
}
```

`Install` 是**幂等**的:同一个 db 装第二次直接返 `nil`
(靠 `db.InstanceSet("slowquery:installed", true)` 判定)。

### 二、直接造一个 logger

```go
gl := slowquery.NewLogger(gormlogger.Default.LogMode(gormlogger.Silent), slowquery.Config{
    Threshold: demoThreshold,
    OnSlow: func(e slowquery.Entry) {
        fmt.Printf("命中:表=%s 操作=%s 耗时=%v\n", e.Table, e.Op, e.Duration.Round(time.Millisecond))
    },
})
db2 := openDB("file:demo?mode=memory&cache=shared", gl)
```

### 三、用默认回调(打一条 zap warn)

```go
core, logs := observer.New(zapcore.WarnLevel)
slowquery.Install(db, slowquery.Config{
    Threshold: demoThreshold,
    Logger:    zap.New(core),
    // 不传 OnSlow ⇒ 用默认的:一条 zap warn 日志
})
```

默认回调的字段:`table` / `op` / `duration` / `rows`,失败时多一个 `error`;
`LogRawSQL` 为 true 时再加 `sql`(截断到 2000 字符)。

### 四、读指标

```go
families, err := prometheus.DefaultGatherer.Gather()
// app_slow_sql_total{table="widgets", op="query", is_error="false"}
```

label 三个:`table`(哪张表)/ `op`(`query|create|update|delete`,其它算 `other`)/
`is_error`(`"true"` / `"false"`)。

## 注意事项

- ⚠️ **`Threshold <= 0` 会退回 500ms,不是"不限"**。这与 `smslimit` 的
  `Config<=0 用默认值` 是同一条口径。想"什么 SQL 都报"**不能靠把阈值调到 0** ——
  0 会被当成"没配"。

- ⚠️ **阈值必须大于时钟粒度**。一条跑得极快的 SQL,实测耗时可能**正好是 0**
  (Windows 上尤其明显),那它当然不算慢查询(`0 < 阈值`)。
  阈值调到 1ms 可能一条都报不出来。

- ⚠️ **`LogRawSQL` 默认 false ⇒ 默认回调不打 SQL**。但 `Entry.SQL` **永远有值** ——
  它只是没进日志字段。生产期 SQL 长,默认不打是对的。

- ⚠️ **但 `LogRawSQL=false` 管不到原 logger**。`Trace` 里第一件事就是
  `w.orig.Trace(ctx, begin, fc, err)` —— 原 logger 会不会把 SQL 打进日志,
  取决于它自己的 LogLevel。GORM 默认 logger 在 Info 级别会把**每条** SQL 都打出来。
  想彻底不打,得把原 logger 设成 `Silent`(`gormlogger.Default.LogMode(gormlogger.Silent)`)。

- ⚠️ **GORM 自带的 logger 也会报慢 SQL,阈值是 200ms**(`logger.Default` 的
  `SlowThreshold: 200ms` / `LogLevel: Warn`,慢的会打一行 `SLOW SQL >= 200ms`)。
  也就是说日志里可能同时存在**两条独立的慢 SQL 口径**:GORM 的 200ms 和
  slowquery 的 500ms。要只看自己这一套,得先把原 logger 设成 `Silent`。

- ⚠️ **`LogMode` 里那段"把阈值同步给原 logger"对 GORM 自带 logger 是死代码**。
  它靠 type assertion 找 `SetSlowThreshold(time.Duration)` 接口,
  而 gorm v1.31.1 的 logger **根本没这个方法**(全模块搜不到),
  所以断言永远失败、阈值永远同步不过去 —— GORM 那条 200ms 的口径不会被改。
  只有你自己实现的、实现了 `SetSlowThreshold` 的 logger 才会被同步。

- ⚠️ **不传 `Logger` 也不传 `OnSlow` ⇒ 一条日志都没有,只剩指标**。
  默认回调在 `log == nil` 时直接 `return`(注释写的是"nil 用 noop(只走 metrics,不打 log)")。
  排查时别以为是回调没触发。

- ⚠️ **`OnSlow` 里的 panic 会被吞掉**。慢查询报警是**旁路**,不该把业务请求带崩 ——
  但反过来说,**回调里出的错你收不到**,它只会被 `recover` 静默吃掉。
  回调里要发告警的话,自己处理错误。

- ⚠️ **表名是从 SQL 里正则抠出来的**,拿不到时是 `"unknown"`;
  操作类型看 SQL 开头(`select` / `insert` / `update` / `delete`,其它算 `other`)。
  所以带子查询的 SQL 抠出来的表名可能是**子表**而不是主表 ——
  指标上的 `table` label 是启发式的,不是权威的。

- ⚠️ **采样粒度只有千分之一**。`SampleRate` 的判定是
  `float64(time.Now().UnixNano() % 1000) / 1000.0 < rate`,所以小于 0.001 的
  采样率无法按比例兑现。`SampleRate <= 0` 会被当成"没配" ⇒ 退回 `1.0`(全量)。

- ⚠️ **`Install` 会替换 `db.Logger`**。装完之后 `db.Config.Logger` 变成 wrapper,
  之后你自己再 `db.Logger = 别的` 会把慢查询监控整个顶掉,而且没有任何提示。

- ⚠️ **指标注册走 `sync.Once`**:`metricsx.Registry()` 非空时注册到它,
  否则注册到 prometheus 默认 registry。所以"在别处读不到指标"通常是
  `metricsx` 没初始化,而不是这个包坏了。

- ⚠️ **`NewLogger` 不接 `*gorm.DB`**,它拿不到 `db.InstanceSet` 的幂等标记 ——
  同一条链路上 `Install` 和 `NewLogger` 混用,可能出现两层 wrapper
  (慢查询会被回调两次)。

- ⚠️ **失败的 SQL 同样会进回调**,`Entry.Error` 有值 ——
  "慢"和"错"是两个维度,指标上分别落在 `is_error=true` / `false`。
  一条又慢又错的 SQL 会同时打两条记录(错误计数 + 慢查询计数)。

## 相关

- 源码:`internal/pkg/slowquery/`
- 可运行示例:`examples/slowquery/main.go` —— `go run ./examples/slowquery`
- 指标注册与 `/metrics` handler → [metricsx](../infrastructure/metricsx.md)
- 缓存预热前后 SQL 量的对比(慢查询下降往往说明缓存生效)→ [cachex](../storage/cachex.md)
