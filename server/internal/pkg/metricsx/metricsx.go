// Package metricsx Prometheus 指标 — §1.8 #8 P0
//
// 设计目标:
//  1. 框架自带 4 类基础指标(自动覆盖):
//     - http_requests_total{method,path,status}        请求计数
//     - http_request_duration_seconds{...}             请求耗时直方图
//     - http_requests_in_flight{...}                   当前在飞请求数
//     - http_response_size_bytes{...}                  响应体字节数
//  2. /metrics 端点直接挂,Prometheus scrape 用
//  3. 业务方可以 RegisterCollector 注册自定义 collector(订单/支付/SQL 慢查询等)
//
// 用法:
//
//	// main.go 启动
//	metricsx.Init(cfg.Metrics, appName, version, env)
//	// router
//	h.Use(middleware.Metrics())
//	h.GET(cfg.Metrics.Path, metricsx.Handler())
//
//	// 业务埋点
//	metricsx.BusinessCounter("order_paid_total", "channel").WithLabelValues("wechat").Inc()
//	metricsx.BusinessHistogram("order_amount_yuan", []float64{1, 10, 100, 1000}).WithLabelValues().Observe(99.5)
package metricsx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/common/expfmt"
	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/logger"
)

var (
	registry *prometheus.Registry
	once     sync.Once

	// HTTP 基础指标
	httpRequestsTotal    *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	httpRequestsInFlight prometheus.Gauge
	httpResponseSize     *prometheus.HistogramVec

	// 应用标识
	appInfo *prometheus.GaugeVec
)

// Init 初始化 Prometheus registry + 注册基础 collector
//   - cfg.Path/Enabled 控制是否挂载
//   - 注册 Go runtime + Process 指标(默认 go_* / process_*)
//   - 注册应用元信息 app_info{app,version,env} 恒 1,方便 PromQL 关联
//   - 注册 4 类 HTTP 指标(实际数据由 middleware.Metrics 上报)
func Init(cfg config.MetricsConfig, appName, version, env string) {
	once.Do(func() {
		registry = prometheus.NewRegistry()

		// Go runtime + Process collectors(标准实践)
		registry.MustRegister(collectors.NewGoCollector())
		registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

		ns := cfg.Namespace
		if ns == "" {
			ns = "app"
		}
		// 注:不挂 subsystem,所有 HTTP 指标统一在 namespace 下,PromQL 写 app_http_*

		httpRequestsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: ns,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests processed, labeled by method/path/status.",
			},
			[]string{"method", "path", "status"},
		)
		httpRequestDuration = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: ns,
				Name:      "http_request_duration_seconds",
				Help:      "Histogram of HTTP request latency in seconds.",
				Buckets:   prometheus.DefBuckets, // .005 .01 .025 .05 .1 .25 .5 1 2.5 5 10
			},
			[]string{"method", "path", "status"},
		)
		httpRequestsInFlight = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: ns,
				Name:      "http_requests_in_flight",
				Help:      "Current number of HTTP requests being processed.",
			},
		)
		httpResponseSize = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: ns,
				Name:      "http_response_size_bytes",
				Help:      "Histogram of HTTP response body size in bytes.",
				Buckets:   prometheus.ExponentialBuckets(100, 4, 6), // 100 ~ 102400
			},
			[]string{"method", "path", "status"},
		)
		appInfo = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: ns,
				Name:      "info",
				Help:      "Application info, value is always 1. Labels: app/version/env.",
			},
			[]string{"app", "version", "env"},
		)

		registry.MustRegister(httpRequestsTotal, httpRequestDuration, httpRequestsInFlight, httpResponseSize, appInfo)

		// 一次性设置 app_info(恒 1,只为带 labels)
		appInfo.WithLabelValues(appName, version, env).Set(1)
	})
}

// Registry 暴露内部 registry(给业务方用 MustRegister)
func Registry() *prometheus.Registry {
	if registry == nil {
		// 未 Init:返 nil,业务方 MustRegister 前必须先 Init
		return nil
	}
	return registry
}

// HTTPRequestsTotal 暴露给 middleware 用(避免 import 链路过深)
func HTTPRequestsTotal() *prometheus.CounterVec     { return httpRequestsTotal }
func HTTPRequestDuration() *prometheus.HistogramVec { return httpRequestDuration }
func HTTPRequestsInFlight() prometheus.Gauge        { return httpRequestsInFlight }
func HTTPResponseSize() *prometheus.HistogramVec    { return httpResponseSize }

// Handler 返回 Hertz 适配的 /metrics handler
//   - 内容协商:按 Accept 头选 OpenMetrics / text/plain(prometheus 官方同款)
//   - 不走 AccessLog / Metrics middleware(否则 self-reporting 循环)
func Handler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if registry == nil {
			c.String(http.StatusServiceUnavailable, "metrics: registry not initialized")
			return
		}
		// 1. 内容协商
		//
		// ⚠️ M99(2026-10-26)修:**这一段此前是死代码,而且就算走到也是坏的**。
		//
		// (a) 原来写的是 `accept == "application/openmetrics-text"` **精确匹配**。
		//     真实 Prometheus 抓取时发的是复合头:
		//
		//	Accept: application/openmetrics-text; version=1.0.0,text/plain;version=0.0.4;q=0.5,*/*;q=0.1
		//
		//     精确匹配永远不成立 → 永远走 text/plain 0.0.4。
		//     注释写着"内容协商",实现里那个协商从来没发生过。
		//
		// (b) 即便走进 OpenMetrics 分支,**也没调 `enc.Close()`** ——
		//     OpenMetrics 报文必须以 `# EOF` 收尾,缺了这一行客户端会判定
		//     报文非法(`expfmt.Closer.Close()` 就是写这一行的)。
		//
		// 现在用 prometheus 官方同款 `expfmt.NegotiateIncludingOpenMetrics`:
		//   - Prometheus 的复合头 → application/openmetrics-text; version=1.0.0
		//   - `Accept: application/openmetrics-text`(不带 version)→ openmetrics 0.0.1
		//   - `*/*` / 空 / 浏览器 → text/plain; version=0.0.4
		format := expfmt.NegotiateIncludingOpenMetrics(requestHeader(c))
		enc := expfmt.NewEncoder(c.Response.BodyWriter(), format)

		// 2. 收集
		mfs, err := registry.Gather()
		if err != nil {
			c.String(http.StatusInternalServerError, "metrics: gather: %v", err)
			return
		}

		// 3. 写(Content-Type 与 format 走 expfmt 标准的字符串,直接用 Format 字符串即可)
		c.Response.Header.Set("Content-Type", string(format))
		c.SetStatusCode(http.StatusOK)
		for _, mf := range mfs {
			if err := enc.Encode(mf); err != nil {
				// 中途写失败(连接断了):记 warn 即可
				logger.L().Warn("metrics: encode failed",
					zap.String("metric", mf.GetName()),
					zap.String("err", err.Error()))
				return
			}
		}
		// 4. 收尾:OpenMetrics 必须写 "# EOF"(text/plain 下是 no-op)
		if closer, ok := enc.(expfmt.Closer); ok {
			if err := closer.Close(); err != nil {
				logger.L().Warn("metrics: encoder close failed", zap.String("err", err.Error()))
			}
		}
	}
}

// requestHeader 把 hertz 的请求头转成 net/http.Header(expfmt.Negotiate 要这个类型)
func requestHeader(c *app.RequestContext) http.Header {
	h := make(http.Header, 8)
	c.Request.Header.VisitAll(func(k, v []byte) {
		h.Add(string(k), string(v))
	})
	return h
}

// MustRegister 业务侧注册自定义 collector(Init 后才能用)
// Init 未调用时直接 panic(早期发现配置问题,不要让指标悄悄丢)
func MustRegister(cs ...prometheus.Collector) {
	if registry == nil {
		panic("metricsx: Init must be called before MustRegister")
	}
	registry.MustRegister(cs...)
}

/* ====================== 业务埋点便捷方法 ====================== */

// # ⚠️ M99(2026-10-26)修:这一段此前**每一次调用都 panic**
//
// 不是"某些调用会炸",是**三个函数没有一个能跑通** —— 把 HEAD 版的实现原样抄出来
// 实测(panic 消息逐字):
//
//	BusinessCounter("x")     → interface conversion: *prometheus.CounterVec is not prometheus.Counter: missing method Add
//	BusinessGauge("x")       → interface conversion: *prometheus.GaugeVec is not prometheus.Gauge: missing method Add
//	BusinessHistogram("x")   → interface conversion: *prometheus.HistogramVec is not prometheus.Observer: missing method Observe
//
// 根因是同一处:三个函数都声明返回 `prometheus.Counter` / `prometheus.Gauge` /
// `prometheus.Observer`,可它们缓存进 `bizCollectors` 的是 **Vec 类型**
// (`*CounterVec` / `*GaugeVec` / `*HistogramVec`)——
// Vec **只有 `WithLabelValues`**,没有 `Add` / `Set` / `Observe`,
// 所以结尾那句 `actual.(prometheus.Counter)` 断言**必失败**。
// 编译器早就说过:`*prometheus.CounterVec does not implement prometheus.Counter`。
//
// 顺带两个问题:
//   - `ObserveDuration` 拿 `prometheus.Observer` 再断言成 `prometheus.Histogram`
//     —— 同样必 panic;而且就算不断言,它传的 `labelNames` 是**标签名**,
//     Observe 要的是**标签值**,接口本身就不可用。
//   - `BusinessCounter` / `BusinessGauge` 返回的那两个 interface
//     **都没有 WithLabelValues**,于是包注释里写的用法
//     `BusinessCounter("order_paid_total", "channel").WithLabelValues("wechat").Inc()`
//     **根本编译不过**。
//
// ⚠️ 因此"同名冲突"在旧实现里其实**走不到** Prometheus 的注册 panic ——
// 接口转换那一炸更早。同名冲突是**新实现**才需要处理的问题(见 checkBizMetric)。
//
// # 修法
//
// 三个函数一律返回**具名 Vec 类型**(`*CounterVec` / `*GaugeVec` / `*HistogramVec`),
// 调用方自己 `WithLabelValues(...)`。这与 Prometheus 官方用法一致,
// 也把"标签"这件事从隐式变成显式。
//
// 同名冲突(此前共用一个 `bizCollectors` 键空间)改成**注册前**就拦:
//   - 同名但种类不同 → "指标 %q 已按 %s 注册,不能改成 %s"
//   - 同名但 label 集不同 → "指标 %q 的 label 集不一致(已注册 [%s],新来 [%s])"
// 换成人话,而不是 Prometheus 的裸 panic。

// 业务指标种类
const (
	bizKindCounter   = "counter"
	bizKindGauge     = "gauge"
	bizKindHistogram = "histogram"
)

// bizMeta 已注册业务指标的元信息
type bizMeta struct {
	kind     string // counter / gauge / histogram
	labelSig string // 标签名集合签名
}

var (
	// bizMu 保护下面两张表
	//
	// 用互斥锁而不是 sync.Map:业务埋点的注册路径是**一次性**的,
	// 但"查缓存 + 查元信息 + 注册"这三步必须**原子**完成
	// (否则两个 goroutine 同时注册同名指标会双注册)。
	bizMu         sync.Mutex
	bizCollectors = map[string]interface{}{} // cacheKey → collector
	bizMetrics    = map[string]bizMeta{}     // Prometheus 指标名 → 元信息
	// histogram 的 buckets 不在缓存键里(见 BusinessHistogram),单独记账,
	// 用于二次注册不同桶时 Warn 留痕
	bizHistogramBuckets = map[string][]float64{} // cacheKey → buckets
)

// bizCacheKey 缓存键 = 种类 + 指标名 + 标签名集合
//
// ⚠️ 标签名集合**必须**进键 —— 同一个指标名配两套不同的 label 名,
// Prometheus 那边会直接 panic("inconsistent label cardinality")。
func bizCacheKey(kind, name, sig string) string {
	return kind + "|" + name + "|" + sig
}

// labelSig 标签名集合的稳定签名
func labelSig(labelNames []string) string {
	return strings.Join(labelNames, ",")
}

// checkBizMetric 同名指标的种类 / 标签集必须一致,否则给一句人话
//
// (必须在 bizMu 保护下调用)
func checkBizMetric(metricName, kind, sig string) {
	if m, ok := bizMetrics[metricName]; ok {
		if m.kind != kind {
			panic(fmt.Sprintf(
				"metricsx: 指标 %q 已按 %s 注册,不能改成 %s —— 同名指标的种类必须一致",
				metricName, m.kind, kind))
		}
		if m.labelSig != sig {
			panic(fmt.Sprintf(
				"metricsx: 指标 %q 的 label 集不一致(已注册 [%s],新来 [%s])—— 同名指标的标签必须一致",
				metricName, m.labelSig, sig))
		}
	}
}

// BusinessCounter 业务计数器
//
//	name 必填;labelNames 是**标签名**,取值在调用时给
//	例: BusinessCounter("order_paid_total", "channel").WithLabelValues("wechat").Inc()
//
// 同一 (name, labelNames) 重复调用返回同一实例(不会重复注册)。
func BusinessCounter(name string, labelNames ...string) *prometheus.CounterVec {
	if name == "" {
		panic("metricsx: BusinessCounter name required")
	}
	sig := labelSig(labelNames)
	key := bizCacheKey(bizKindCounter, name, sig)

	bizMu.Lock()
	defer bizMu.Unlock()
	if c, ok := bizCollectors[key]; ok {
		return c.(*prometheus.CounterVec)
	}
	metricName := "app_biz_" + name
	checkBizMetric(metricName, bizKindCounter, sig)
	cv := prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: metricName, Help: "business counter: " + name},
		labelNames,
	)
	MustRegister(cv)
	bizCollectors[key] = cv
	bizMetrics[metricName] = bizMeta{kind: bizKindCounter, labelSig: sig}
	return cv
}

// BusinessGauge 业务 gauge
//
//	例: BusinessGauge("queue_depth").WithLabelValues().Set(42)
//
//	⚠️ `*prometheus.GaugeVec` **没有** Set 方法(和 CounterVec/HistogramVec
//	一样,取值必须先 WithLabelValues)—— 别写成 `.Set(42)`,编译不过。
func BusinessGauge(name string, labelNames ...string) *prometheus.GaugeVec {
	if name == "" {
		panic("metricsx: BusinessGauge name required")
	}
	sig := labelSig(labelNames)
	key := bizCacheKey(bizKindGauge, name, sig)

	bizMu.Lock()
	defer bizMu.Unlock()
	if c, ok := bizCollectors[key]; ok {
		return c.(*prometheus.GaugeVec)
	}
	metricName := "app_biz_" + name
	checkBizMetric(metricName, bizKindGauge, sig)
	gv := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: metricName, Help: "business gauge: " + name},
		labelNames,
	)
	MustRegister(gv)
	bizCollectors[key] = gv
	bizMetrics[metricName] = bizMeta{kind: bizKindGauge, labelSig: sig}
	return gv
}

// BusinessHistogram 业务直方图
//
//	buckets 为空时用 prometheus.DefBuckets
//	例: BusinessHistogram("order_amount_yuan", []float64{1, 10, 100, 1000}).WithLabelValues().Observe(99.5)
func BusinessHistogram(name string, buckets []float64, labelNames ...string) *prometheus.HistogramVec {
	if name == "" {
		panic("metricsx: BusinessHistogram name required")
	}
	if len(buckets) == 0 {
		buckets = prometheus.DefBuckets
	}
	sig := labelSig(labelNames)
	key := bizCacheKey(bizKindHistogram, name, sig)
	metricName := "app_biz_" + name

	bizMu.Lock()
	defer bizMu.Unlock()
	if c, ok := bizCollectors[key]; ok {
		// ⚠️ 缓存键不含 buckets:同名同标签二次注册不同桶时,这里会静默复用
		// 第一套桶,新桶配置被无声丢弃(分布全落在错误的桶里)。不能把 buckets
		// 加进键 —— 那会导致同一指标重复注册,Prometheus 直接 panic;这里只
		// Warn 留痕、缓存行为不变(与 checkBizMetric 对同名冲突的"给一句人话"
		// 口径一致,只是不 panic:Vec 已注册,再注册必炸,Warn 是唯一选项)。
		if prev := bizHistogramBuckets[key]; !float64sEqual(prev, buckets) {
			logger.L().Warn("metricsx: BusinessHistogram buckets changed on re-register, reusing registered buckets",
				zap.String("metric", metricName),
				zap.Float64s("registered", prev),
				zap.Float64s("requested", buckets))
		}
		return c.(*prometheus.HistogramVec)
	}
	checkBizMetric(metricName, bizKindHistogram, sig)
	hv := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: metricName, Help: "business histogram: " + name, Buckets: buckets},
		labelNames,
	)
	MustRegister(hv)
	bizCollectors[key] = hv
	bizMetrics[metricName] = bizMeta{kind: bizKindHistogram, labelSig: sig}
	bizHistogramBuckets[key] = buckets
	return hv
}

// float64sEqual 逐值比较两个 buckets(顺序敏感:桶序决定分布区间)
func float64sEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ObserveDuration 便捷:记录某段代码耗时(无标签)
//
//	start := time.Now()
//	... do something ...
//	metricsx.ObserveDuration("order_pay_seconds", time.Since(start))
//
// # ⚠️ M99(2026-10-26)重写:此前**必 panic**
//
// 旧签名带 `labelNames ...string`,但 Observe 要的是**标签值**不是标签名,
// 接口本身就不可能用对。现在只保留无标签版本;
// 要带标签请直接用:
//
//	metricsx.BusinessHistogram("order_pay_seconds", nil, "channel").
//	    WithLabelValues("wechat").Observe(d.Seconds())
func ObserveDuration(name string, d time.Duration) {
	BusinessHistogram(name, prometheus.DefBuckets).WithLabelValues().Observe(d.Seconds())
}

// ErrNoRegistry Init 未调用时返错
var ErrNoRegistry = errors.New("metricsx: registry not initialized, call Init first")

// Mount 把 /metrics 挂到 hertz 实例(在 cfg.Path,默认 /metrics)
// 同时绕过 Metrics 中间件(self-reporting 循环)
func Mount(h *server.Hertz, cfg config.MetricsConfig) {
	if !cfg.Enabled {
		return
	}
	if registry == nil {
		logger.L().Warn("metricsx: registry is nil, call Init before Mount")
		return
	}
	path := cfg.Path
	if path == "" {
		path = "/metrics"
	}
	h.GET(path, Handler())
	logger.L().Info("metricsx: /metrics mounted", zap.String("path", path))
}

// String 健康检查 + 调试用
func String() string {
	if registry == nil {
		return "metricsx: not initialized"
	}
	return fmt.Sprintf("metricsx: %d collectors registered", len(collectorsNames(registry)))
}

func collectorsNames(reg *prometheus.Registry) []string {
	mfs, _ := reg.Gather()
	names := make([]string, 0, len(mfs))
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	return names
}
