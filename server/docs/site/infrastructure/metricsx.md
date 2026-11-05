# metricsx —— Prometheus 指标 + `/metrics` 端点

> 框架自带 4 类 HTTP 基础指标,业务方再挂自己的业务指标,全部从同一个端点暴露。

## 功能

metricsx 干三件事:

1. **一个 registry**(`prometheus.Registry`,不是全局 default registry)
   + Go runtime / process collector + 应用元信息 `app_info{app,version,env}`
2. **4 类 HTTP 基础指标**(数据由 `middleware.Metrics()` 上报,业务代码不用管):

   | 指标 | 类型 | 标签 |
   |---|---|---|
   | `<ns>_http_requests_total` | counter | `method,path,status` |
   | `<ns>_http_request_duration_seconds` | histogram | `method,path,status` |
   | `<ns>_http_requests_in_flight` | gauge | — |
   | `<ns>_http_response_size_bytes` | histogram | `method,path,status` |

3. **业务埋点**:`BusinessCounter` / `BusinessGauge` / `BusinessHistogram` /
   `ObserveDuration`,指标名统一加 `app_biz_` 前缀。
4. **`/metrics` 端点**:`Handler()` 返回 Hertz handler,`Mount()` 一行挂上。

**不解决什么**:不做告警、不做看板、不管采集端。告警规则在 Prometheus 侧
(见 [slowquery](../scheduling/slowquery.md) 的做法)。

## 配置

### yaml 侧(`metrics:` 块)

```yaml
metrics:
  enabled: true        # 总开关
  path: /metrics       # 暴露路径(默认 /metrics)
  namespace: app       # 指标前缀(默认 app)→ app_http_requests_total
  subsystem: ""        # ⚠️ 当前**没有任何代码读它**(见下)
```

`metrics.namespace` 决定 HTTP 指标的命名前缀:`namespace: app` ⇒
`app_http_requests_total`;`namespace: demo` ⇒ `demo_http_requests_total`。

### 代码侧

```go
metricsx.Init(cfg.Metrics, appName, version, env string)
metricsx.Mount(h *server.Hertz, cfg config.MetricsConfig)
metricsx.Handler() app.HandlerFunc
```

`Init` 的四个参数里,`appName` / `version` / `env` 落到 `app_info` 标签上
(恒为 1 的 gauge),方便 PromQL 关联。

## 使用方法

### 启动时装配

```go
// cmd/server/main.go
metricsx.Init(cfg.Metrics, cfg.System.AppName, cfg.System.AppVersion, cfg.OTel.Env)

// router.go
h.Use(middleware.Metrics())    // 4 类 HTTP 指标的采集点
metricsx.Mount(h, cfg.Metrics) // 挂 /metrics
```

### 业务埋点

```go
metricsx.BusinessCounter("order_paid_total", "channel").WithLabelValues("wechat").Inc()
metricsx.BusinessGauge("login_online_total").WithLabelValues().Set(42)
metricsx.BusinessHistogram("order_amount_yuan",
    []float64{1, 10, 100, 1000}, "channel").
    WithLabelValues("wechat").Observe(99.5)

// 无标签的耗时快捷方式
metricsx.ObserveDuration("order_process_seconds", 150*time.Millisecond)
```

### 注册自定义 collector

```go
metricsx.MustRegister(myCollector)   // Init 之后才能用
reg := metricsx.Registry()            // 也能拿到 registry 自己注册
```

### 命名约定

| 指标 | 实际暴露的名字 |
|---|---|
| `BusinessCounter("order_paid_total", ...)` | `app_biz_order_paid_total` |
| `BusinessGauge("login_online_total")` | `app_biz_login_online_total` |
| HTTP 基础指标(`namespace: app`) | `app_http_requests_total` 等 |

即:**HTTP 指标走 `namespace`,业务指标走固定的 `app_biz_`** —— 两套前缀,
业务指标**不受** `metrics.namespace` 影响。

## 注意事项

- ⚠️ **`Init` 是 `sync.Once` —— 一个进程只能初始化一次**。
  第二次调是 no-op:换 `namespace` 不会生效,想重置 registry **必须换进程**
  (测试里尤其要注意)。
- ⚠️ **`metrics.enabled: false` 并不停止采集**。`Init` 里**没有**读
  `cfg.Enabled` —— 它照样建 registry、注册全部 collector;只有 `Mount`
  读 `Enabled` 并因此**不挂 `/metrics` 端点**。而 `middleware.Metrics()`
  在 router 里是无条件挂的,它只看 `metricsx.HTTPRequestsTotal() != nil`
  (Init 过了就非 nil)⇒ **`enabled: false` 时指标照样在采,只是没人能 scrape 到**。
  yaml 里"Init 内部空实现、中间件也 no-op"那句注释与实现对不上。
- ⚠️ **`metrics.subsystem` 是死配置** —— `MetricsConfig` 里有这个字段,
  但 `metricsx` 里没有任何代码读它。填什么都不会生效。
- ⚠️ **`Registry()` 在 `Init` 之前返 `nil`**;`MustRegister` 在 `Init` 之前
  直接 **panic**(`metricsx: Init must be called before MustRegister`),
  这是有意的 —— 早期暴露配置问题,而不是让指标悄悄丢。
- ⚠️ **`BusinessGauge` / `BusinessCounter` / `BusinessHistogram` 返回的是
  `*Vec` 类型,取值必须先 `WithLabelValues(...)`**。
  `*prometheus.GaugeVec` **没有** `Set` 方法 —— 写 `.Set(42)` 编译不过。
- ⚠️ **同名指标的"种类"和"标签集"必须完全一致,否则 panic**。
  `BusinessCounter("x", "a")` 之后再调 `BusinessGauge("x", "a")` 或
  `BusinessCounter("x", "b")` 都会 panic(消息是人话,不是 Prometheus 的裸 panic)。
  标签名集合进缓存键就是为了拦这个 —— Prometheus 那边会直接
  `inconsistent label cardinality`。
- ⚠️ **`ObserveDuration(name, d)` 没有标签**。要带标签请直接用
  `BusinessHistogram(name, nil, "channel").WithLabelValues("wechat").Observe(d.Seconds())`。
  (旧版 `ObserveDuration` 带 `labelNames ...string`,但 `Observe` 要的是
  **标签值**不是标签名,接口本身就不可用 —— 那一版每次调用都 panic。)
- ⚠️ **label 里不要放高基数值**(用户 id / 订单 id)—— Prometheus 会被撑爆。
  要放就放可枚举的维度(渠道 / 状态)。
- ⚠️ **`path` 标签用的是路由模板**(`c.FullPath()`,如
  `/rapid-admin/v1/biz/orders/:id`),不是实际 URL;未匹配到路由时固定用
  `/unmatched`,**不用**原始 path —— 否则扫描器每打一个随机 URL 就多一条 series。
- ⚠️ **`/metrics` 的内容协商必须写 `# EOF`**。OpenMetrics 报文要以 `# EOF`
  收尾,否则客户端判定报文非法。实现用的是 prometheus 官方同款的
  `expfmt.NegotiateIncludingOpenMetrics`(早期版本是精确匹配 `Accept` 头,
  真实 Prometheus 发的是复合头,永远协商不中)。
- ⚠️ **在当前 router 的挂载顺序下,`/metrics` 的抓取请求本身会被记一条
  `path="/metrics"` 的 series**。Hertz 是在**路由注册时**把全局中间件拼进
  路由链的,而 `metricsx.Mount` 在 `h.Use(middleware.Metrics())` **之后**执行 ——
  所以源码注释里"绕过 Metrics 中间件(self-reporting 循环)"这句在当前装配下
  **并没有兑现**。这不是死循环(只多一条 series),但看板里会多出一个指标项。
- ⚠️ `ErrNoRegistry` 这个导出变量**当前无人使用**(声明了但没有任何返回它的地方)。

## 相关

- 源码:`internal/pkg/metricsx/`
- 可运行示例:`examples/metricsx/main.go` —— `go run ./examples/metricsx`
- 采集中间件 → [框架约定](../getting-started/conventions.md)
- 同 registry 上的缓存指标 `app_cache_ops_total` → [cachex](../storage/cachex.md)
- 定时任务指标 `app_job_runs_total` → [cronx](../scheduling/cronx.md)
