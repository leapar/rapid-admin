# otelx —— OpenTelemetry TracerProvider 初始化

> 链路追踪的"起点":把 TracerProvider 建起来、把资源信息挂上、把 trace context
> 的传播格式定下来。

## 功能

otelx 做四件事:

1. **建 TracerProvider** —— OTLP exporter(grpc / http)+ 批处理 + 采样器
2. **挂 resource** —— 只有 `service.name` 和 `deployment.environment.name` 两个属性
3. **定传播格式** —— W3C `traceparent` + `baggage` 复合传播器
4. **给一个关闭函数** —— `ShutdownFunc`,先 `ForceFlush` 再 `Shutdown`

**不解决什么**:不产生 span。span 由 `middleware.Otel(serviceName)` 开
(每个 HTTP 请求一个 server span),业务侧要自己 `otel.Tracer(...)` 开子 span。

## 配置

### yaml 侧(`otel:` 块)

```yaml
otel:
  enabled: false               # 总开关;false → main 走 InitNoop,不发任何网络
  endpoint: arms-cn-hangzhou.aliyuncs.com:4317  # 只写 host:port(http:// 前缀会被剥掉)
  protocol: grpc               # grpc(默认)| http
  insecure: false              # true = 跳过 TLS(内网 collector)
  sample_ratio: 1.0            # 0.0~1.0,会被夹到 [0,1]
  service_name: rapid-admin
  env: dev
```

- `endpoint` **只写 `host:port`**。scheme 会被剥掉 —— 走 http 还是 https
  **只由 `insecure` 决定**,不由 endpoint 里写的字符串决定。
- `protocol: http` 时可以带 path(如 `localhost:4318/v1/traces`),不写就用默认
  `/v1/traces`。
- `sample_ratio` 是**比例**,`1.0` 全采样、`0.1` 采 10%。

### 代码侧

```go
otelx.Init(ctx context.Context, serviceName, env, protocol, endpoint string,
    insecure bool, sampleRatio float64) (ShutdownFunc, error)
otelx.InitNoop() ShutdownFunc
```

`ShutdownFunc` 的类型是 `func(context.Context) error` —— main 用 `defer` 调。

## 使用方法

```go
// cmd/server/main.go
var otelShutdown otelx.ShutdownFunc = otelx.InitNoop()
if cfg.OTel.Enabled {
    otelShutdown, err = otelx.Init(context.Background(),
        cfg.OTel.ServiceName,
        cfg.OTel.Env,
        cfg.OTel.Protocol,
        cfg.OTel.Endpoint,
        cfg.OTel.Insecure,
        cfg.OTel.SampleRatio,
    )
    if err != nil {
        log.Fatal("init otel failed", zap.Error(err))
    }
}
defer func() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    _ = otelShutdown(ctx)
}()
```

`InitNoop()` 返回的关闭函数**不是 nil**,调用它返 `nil` error ——
所以 main 的 `defer` 可以统一调,不用分支。

## 注意事项

- ⚠️ **`Init` 本身没有 `enabled` 参数**。源码注释写的"cfg.Enabled = false →
  直接返 noop"描述的是 `cmd/server/main.go` 的行为,不是 `Init` 的行为 ——
  **调了 `Init` 就一定建真 exporter**。开关判断在 main 里。
- ⚠️ **不初始化时确实是 noop,但 noop 的范围比想象的大**。`otel.enabled=false`
  时 main 走 `InitNoop()`,于是:
  - `otel.SetTracerProvider` 从没被调 ⇒ 全局 TracerProvider 是 OTel SDK 的
    **默认 noop** ⇒ span 被丢弃,不发网络;
  - `otel.SetTextMapPropagator` 也从没被调 ⇒ 全局传播器是 **noop** ⇒
    上游的 `traceparent` **既不会被提取也不会被注入**,
    `middleware.TraceIDFromContext` 返 `""`。
- ⚠️ **`endpoint` 里的 scheme 会被剥掉**。这是 M99 修的坑:OTLP 的
  `WithEndpoint` 只接受 `host:port`,而配置注释曾写"http: http://host:port",
  运营照填后 exporter 的实际地址变成 `http://http://collector:4318/v1/traces`
  ⇒ **每次导出都失败**,span 被静默丢弃(业务侧毫无感知),只有 stderr 里一行报错。
  现在统一处理:剥 scheme → 拆出 path → `WithURLPath`。
  **http 还是 https 由 `insecure` 说了算,只有这一个开关。**
- ⚠️ **`protocol` 只认 `grpc` 和 `http`,写错直接报错**(不会静默走默认);
  但**空字符串默认走 grpc**。
- ⚠️ **exporter 是懒连接的**:`Init` 成功**不代表** OTLP 后端可达。
  真正连不上时是每次导出时报错,span 静默丢弃。
- ⚠️ **`Init` 会覆盖全局 TracerProvider**(`otel.SetTracerProvider(tp)`)。
  调第二次会把前一个换掉 —— 前一个的 in-flight span 归属会乱,
  而且它的 shutdown 要自己单独调。示例里为了演示采样率夹取就连着调了多次。
- ⚠️ **`sampleRatio` 会被夹到 [0,1]**(配成 5 或 -1 不会让采样器炸掉,
  但也意味着**配错了不会有人知道** —— 5 和 1 的行为完全一样)。
- ⚠️ **关闭必须走返回的 `ShutdownFunc`**:它先 `ForceFlush`(5s 超时)再
  `Shutdown`。直接 `tp.Shutdown()` 会丢掉缓冲区里还没导出的 span。
- ⚠️ **resource 刻意不带 `resource.Default()`** —— 不会有 host / process /
  `telemetry.sdk.*` 属性。要不要带由 collector 侧决定,这里保持最小集。
- ⚠️ **`serviceName` 为空会兜底成 `rapid-admin`**(不是报错)。

## 相关

- 源码:`internal/pkg/otelx/`
- 可运行示例:`examples/otelx/main.go` —— `go run ./examples/otelx`
- yaml 全表 → [配置体系](../getting-started/configuration.md)
- 同一套可观测体系里的日志侧 → [logger](logger.md)
