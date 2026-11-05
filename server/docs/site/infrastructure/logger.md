# logger —— 多 Core 装配:控制台 + info/warn/error/audit/access 分文件

> zap 之上的一层薄封装:把"主日志 / 访问日志 / 审计日志"拆成互不干扰的落盘通道,
> 并把按天分目录 + 按大小滚动 + 压缩 + 过期清理收在一个 rotator 里。

## 功能

logger 解决四件事:

1. **多 Core 装配** —— 一个 `*zap.Logger`,tee 出多个 core:
   - 控制台(stdout)**仅在 `env=dev` / `env=test` 挂载**
   - `info.log`(level ≥ info)、`warn.log`(level ≥ warn)、`error.log`(level ≥ error)
   - `audit.log`、`access.log` —— **独立通道**,按 logger 名路由
2. **落盘策略**(`TimeRotator`):按天分目录 → 按大小滚动 → 可选 gzip → 过期目录清理
   + 同目录历史文件按 `max_backups` 保留。
3. **通道路由**(`route.go`):`Audit()` / `Access()` 拿到具名 logger,只写自己那一份文件,
   结构化字段**原样带过去**(path / method / status / user 都在)。
4. **退出语义**:`SyncFlush()`(fsync 到盘)+ `Close()`(关文件句柄),rotator 关掉后仍可自愈复用。

**不解决**:脱敏不在本包(在 `pkg/desensitize` + `middleware/audit_capture.go`);
链路追踪 / 指标也不在(在 `otelx` / `metricsx`)。

## 配置

### yaml 侧(`logger:` 块)

```yaml
logger:
  level: info             # debug / info / warn / error
  format: json            # json / console
  dir: logs
  max_size_mb: 100
  max_backups: 30
  max_age_days: 90
  compress: true
  env: dev
```

viper 里**没有** `logger.*` 的 `SetDefault` —— 兜底值全在 `logger.New` 内部。

| 键 | 类型 | yaml 示例值 | 代码兜底 | 说明 |
|---|---|---|---|---|
| `level` | string | `info` | `""` ⇒ 等价 `info` | **只作用于 stdout core**,见「注意事项」。文件 core 的级别是写死的 |
| `format` | string | `json` | `""` ⇒ 等价 `json` | `json` / `console`。**audit / access 恒为 JSON**,不受它影响 |
| `dir` | string | `logs` | `""` → `logs` | 日志根目录;实际文件落在 `${dir}/${YYYY-MM-DD}/<level>.log` |
| `max_size_mb` | int | `100` | `0` → `100` | 单文件大小上限,超过即滚动 |
| `max_backups` | int | `30` | `0` → `30` | **同一日期目录内**保留的历史文件数(按 mtime 删最早的) |
| `max_age_days` | int | `90` | `0` → `90` | 按本地日历天保留的天数 |
| `compress` | bool | `true` | `false`(`env=prod` 时强制 `true`) | 滚动后异步 gzip。**prod 下写 `false` 会被强制改回 `true`** |
| `env` | string | `dev` | `""` ⇒ 不挂 stdout core | `dev`/`test` 挂 stdout core;`prod` 强制开启压缩 |

### 代码侧(构造选项)

`logger.Config` 字段与 yaml 一一对应(`Level` / `Format` / `Dir` / `MaxSizeMB` /
`MaxBackups` / `MaxAgeDays` / `Compress` / `Env`),兜底值如上表。

```go
log, err := logger.New(logger.Config{
    Level:      "info",
    Format:     "json",
    Dir:        "logs",
    MaxSizeMB:  100,
    MaxBackups: 30,
    MaxAgeDays: 90,
    Compress:   true,
    Env:        "prod",
})
logger.Default = log      // 全局注入
zap.ReplaceGlobals(log)   // 第三方库走 zap.L()
```

## 使用方法

### 三个通道

```go
// 主日志:进 info.log / warn.log / error.log(按级别分流)
log.Info("服务启动", zap.String("app", "示例"), zap.Int("port", 8080))

// 访问日志:只进 access.log(恒 JSON)
logger.Access().Info("GET /rapid-admin/v1/system/users",
    zap.String("method", "GET"),
    zap.String("path", "/rapid-admin/v1/system/users"),
    zap.Int("status", 200),
    zap.Int64("cost_ms", 12))

// 审计日志:只进 audit.log(恒 JSON)
logger.Audit().Info("删除用户",
    zap.Uint64("actor_id", 1001),
    zap.String("target", "sys_user"),
    zap.Uint64("target_id", 2002))
```

`logger.L()` 是全局 logger 的快捷访问;`logger.Default` 还没注入时它返
`zap.NewNop()`(静默不输出)。

### 错误级别自带堆栈

```go
log.Error("数据库写入失败", zap.String("table", "sys_user"), zap.Error(os.ErrPermission))
```

`zap.AddStacktrace(zap.ErrorLevel)` 是 `New` 里写死的 ⇒ error 及以上自动带 stacktrace。

### 初始化失败不想处理 error 时

```go
logger.Must(l, err)   // err != nil ⇒ panic("init logger: ...")
```

### 退出前:刷盘 + 关句柄

```go
defer func() {
    _ = logger.SyncFlush()   // 1. fsync 所有 rotator
    _ = log.Sync()           // 2. 刷 tee
    _ = logger.Close()       // 3. 关文件句柄
}()
```

完整示例见 `examples/logger/main.go`。

## 注意事项

- ⚠️ **`logger.level` 只管 stdout core,不管文件**。三个文件 core 的级别写死在
  `logger.go`:`info.log` ≥ Info、`warn.log` ≥ Warn、`error.log` ≥ Error。
  dev 下 `level: debug` 能看到 debug 打到 stdout;但 **prod 根本没有 stdout core**
  ⇒ `level: debug` 在生产**完全无效**,`info.log` 里永远不会有 debug 行。
  想在生产看 debug,得改代码里的 core 级别,不是改 yaml。
- ⚠️ **audit / access 是 core 级路由,不是 hook**。zap 的 hook 签名是
  `func(zapcore.Entry) error`,拿不到每次调用的结构化字段 ——
  `zapcore.hooked.Write(ent, _ []Field)` 把 fields 直接丢了。
  早期版本因此让 audit.log / access.log 里 path / method / status / user **全部消失**。
  现在改用 `namedCore`(`route.go`):`Check` 阶段按 `LoggerName` 选核心,
  `Write(ent, fields)` 时字段原样带过去。
- ⚠️ **空名 logger 两条独立通道都不进**。只有 `logger.Audit()` / `logger.Access()`
  (或自己 `Named("audit")` / `Named("access")`)才会路由过去;
  `log.Info(...)` 走的是三个文件 core。路由规则是**精确等值**匹配。
- ⚠️ **audit / access 恒为 JSON**,`format: console` 也不影响它们 ——
  这两份是给机器读的(审计检索 / 访问日志分析),人读的 console 只给 dev/test。
- ⚠️ **`log.Sync()` 刷不到 audit / access**。`zap.Logger.Sync()` 只刷 **tee 里的 core**;
  两个独立 rotator 不在 tee 里 ⇒ 进程退出时最后几条审计/访问日志可能丢。
  `logger.SyncFlush()` 专门兜这两个。**退出三步的顺序不能改**:
  `SyncFlush()` → `log.Sync()` → `Close()`。写成三个独立 `defer` 会因 LIFO 把顺序搞反
  (关句柄抢在刷盘前面跑)。
- ⚠️ **`Close()` 之后 rotator 仍可自愈**:下次 `Write` 发现 `currentFile == nil`
  会重新 rotate 打开。但 `Write` 的判据**必须**是
  `currentDay != today || currentFile == nil` —— 少了后半边,重开文件失败后
  `currentFile == nil` 但 `currentDay` 仍是今天 ⇒ 跳过 rotate ⇒ 每次写都返
  `ErrInvalid`(`os.File` 对 nil 接收者不 panic)⇒ **日志从此静默全丢**,无任何告警。
- ⚠️ **过期目录按日期串比较,不解析成 `time.Time`**。`time.Parse("2006-01-02", …)`
  对只有日期的布局按 **UTC** 解析,而 cutoff 来自本地时间 ⇒ 边界行为随"当天几点"漂移
  (东八区 vs 美西最多差 14 小时)。现在的规则(`expiredDir`):目录名 `<= cutoff`
  (今天 − maxAge 天)即删 ⇒ **今天、昨天、……、前 (maxAge-1) 天保留**。
  名字解析不出日期的目录**不删**(别的子目录不能被误伤)。
- ⚠️ **`max_backups` 是"同一天目录内"的计数**,不是全局备份数。
  匹配规则:文件名以 `<level>.` 开头且以 `.log` 结尾 —— `rotateSize` 产出的
  `info.150405.000.log` 匹配得上。
- ⚠️ **压缩是异步的**(`go compressFile(...)`)⇒ 进程退出瞬间可能还有 `.gz` 没写完;
  `compressFile` 成功后才删源文件。
- ⚠️ **`logger.L()` 在 `Default` 未注入时返 Nop logger** —— 静默不输出。
  所以 init 阶段的告警会被丢掉(`sonyflake` 因此把机号告警推迟到第一次 `Next()`)。
- ⚠️ **本包没有脱敏钩子**。审计日志里的请求体/查询串脱敏在
  `pkg/desensitize` + `internal/api/middleware/audit_capture.go` 里做,
  由 audit 中间件调,不在 logger 里。

## 相关

- 源码:`internal/pkg/logger/`(`logger.go` 装配 / `rotator.go` 滚动 / `route.go` 通道路由)
- 可运行示例:`examples/logger/main.go` —— `go run ./examples/logger`
- 脱敏词表 → [desensitize](../security/desensitize.md)
- 链路追踪(header 同源) → [otelx](otelx.md)
- 指标 → [metricsx](metricsx.md)
