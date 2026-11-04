// cachex 监控埋点(§1.7 #9 P2)
//
// 设计目标:
//  1. 框架自带 4 类缓存指标(自动覆盖):
//     - app_cache_ops_total{layer,op,result}     操作计数(L1/L2 × 读/写/删 × 命中/未命中/错/...)
//     - app_cache_op_duration_seconds{op}         L2 操作耗时直方图(Redis RTT 监测)
//     - app_cache_l1_size                        L1 LRU 当前条目数(Gauge,每次 L1 写后更新)
//     - app_cache_loader_total{result}            loader 调用计数(ok/notfound/error)
//  2. 业务可走 InitMetrics(reg) 一次性注册到现有 metricsx.Registry()
//  3. InitMetrics 未调 → 所有 recordOp/setL1Size 自动 no-op(本包仍可单测 / 不依赖 Prom)
//
// 用法:
//
//	// main.go
//	cachex.InitMetrics(metricsx.Registry())
//
//	// 业务侧无需手动调,框架内 Get/GetOrLoad/Set/Del 自动埋
//
// PromQL 示例:
//   - L1 命中率:    sum(rate(app_cache_ops_total{layer="l1",op="get",result="hit"}[5m])) by (layer)
//                   / sum(rate(app_cache_ops_total{layer="l1",op="get"}[5m])) by (layer)
//   - L2 慢查询:    histogram_quantile(0.99, rate(app_cache_op_duration_seconds_bucket{op="get"}[5m]))
//   - Loader 失败率: sum(rate(app_cache_loader_total{result="error"}[5m]))
//                   / sum(rate(app_cache_loader_total[5m]))
package cachex

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// 指标 collector + 初始化闸
var (
	promMu        sync.Mutex
	promInited    bool // InitMetrics 已调过(reg 已锁定)

	cacheOpsTotal    *prometheus.CounterVec
	cacheOpDuration  *prometheus.HistogramVec
	cacheL1SizeGauge prometheus.Gauge
	cacheLoaderTotal *prometheus.CounterVec
)

// InitMetrics 注册 cachex 指标到给定 registry
//   - reg = nil → no-op(便于业务方关停 / 单测跳过)
//   - 重复调用 → panic(避免业务方误调;prometheus 不允许重名注册)
//   - 推荐:metricsx.Registry() 拿全局 registry
func InitMetrics(reg prometheus.Registerer) {
	if reg == nil {
		return
	}
	promMu.Lock()
	defer promMu.Unlock()
	if promInited {
		panic("cachex: InitMetrics already called (prometheus does not allow re-register)")
	}
	cacheOpsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "app",
			Name:      "cache_ops_total",
			Help:      "cache operations counter (layer x op x result).",
		},
		[]string{"layer", "op", "result"},
	)
	// L2 耗时直方图:L1 命中是纯内存(< 1µs)不入桶,只 L2 走
	cacheOpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "app",
			Name:      "cache_op_duration_seconds",
			Help:      "cache operation latency in seconds (L2 only, L1 is sub-microsecond).",
			// 0.1ms..819ms 指数桶,覆盖本地 Redis 99% 场景;RTT > 1s 当 slow log
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 14),
		},
		[]string{"op"},
	)
	cacheL1SizeGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "app",
			Name:      "cache_l1_size",
			Help:      "current L1 LRU entry count.",
		},
	)
	cacheLoaderTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "app",
			Name:      "cache_loader_total",
			Help:      "loader invocation counter (ok / notfound / error).",
		},
		[]string{"result"},
	)
	// 逐个注册,失败立即 panic + 知道哪个失败
	for _, c := range []prometheus.Collector{cacheOpsTotal, cacheOpDuration, cacheL1SizeGauge, cacheLoaderTotal} {
		reg.MustRegister(c)
	}
	promInited = true
}

// resetForTest 仅供单测使用:重置 Init 状态,让每个 test 用自己的 registry
func resetForTest() {
	promMu.Lock()
	defer promMu.Unlock()
	cacheOpsTotal = nil
	cacheOpDuration = nil
	cacheL1SizeGauge = nil
	cacheLoaderTotal = nil
	promInited = false
}

// recordOp 记录一次缓存操作(nil-safe)
//   - layer:l1 / l2
//   - op:get / set / del
//   - result:hit / miss / null / error / ok
//   - dur:L2 操作用 wall time,L1 传 0(不计入耗时桶)
func recordOp(layer, op, result string, dur time.Duration) {
	if cacheOpsTotal == nil {
		return
	}
	cacheOpsTotal.WithLabelValues(layer, op, result).Inc()
	if cacheOpDuration != nil {
		// 只 L2(dur > 0)入耗时桶;L1 命中是 sub-µs 纯内存,入桶会污染分位数
		// 但还是调 WithLabelValues(op) 一次,确保 series 在 Gather 暴露,便于监控看见"有没有 op=get 的调用"
		if dur > 0 {
			cacheOpDuration.WithLabelValues(op).Observe(dur.Seconds())
		} else {
			cacheOpDuration.WithLabelValues(op) // 触发 series 创建,不 Observe(0) 避免污染 0 桶
		}
	}
}

// recordLoader 记录 loader 调用结果(nil-safe)
//   - result:ok / notfound / error
func recordLoader(result string) {
	if cacheLoaderTotal == nil {
		return
	}
	cacheLoaderTotal.WithLabelValues(result).Inc()
}

// setL1Size 写 L1 当前 size(Gauge,L1 任何 Add/Remove 后调一次)
func setL1Size(size int) {
	if cacheL1SizeGauge == nil {
		return
	}
	cacheL1SizeGauge.Set(float64(size))
}
