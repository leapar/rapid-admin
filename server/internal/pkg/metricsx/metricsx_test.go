// metricsx 包单测 —— §1.8(M99)
//
// # M99 在这里实证出的 3 个真 bug
//
//  1. `BusinessHistogram` **一次调用就 panic**:
//     结尾 `actual.(prometheus.Observer)` —— `*prometheus.HistogramVec`
//     **没有 Observe 方法**(Observe 在 `*prometheus.Histogram` 上,
//     Vec 只有 WithLabelValues),所以这个断言必 panic。
//     编译器早就说过:`*prometheus.HistogramVec does not implement prometheus.Observer`。
//  2. `ObserveDuration` 拿 `prometheus.Observer` 再断言成 `prometheus.Histogram`
//     —— 同样必 panic;而且它传的 `labelNames` 是**标签名**,Observe 要的是**标签值**,
//     接口本身就不可用。
//  3. `BusinessCounter` / `BusinessGauge` 返回 `prometheus.Counter` /
//     `prometheus.Gauge` —— 这两个 interface **都没有 WithLabelValues**,
//     于是包注释里写的用法**根本编译不过**。
//
// 三个函数的共同点:**从没被真正调用过**。
// 全仓零调用方 + 零测试 = 一段"看起来能用、其实必 panic"的死代码。
//
// # M99(2026-10-26)可逆探针记录
//
//	拆法                                                必须变红
//	──────────────────────────────────────────  ──────────────────────────────────
//	P1 BusinessHistogram 改回返 prometheus.Observer   TestBusinessHistogram_Usable
//	P2 ObserveDuration 改回旧实现                     TestObserveDuration_Records
//	P3 BusinessCounter 改回返 prometheus.Counter       TestBusinessCounter_Usable
//	P4 checkBizMetric 去掉 kind 检查                   TestBusinessSameNameDifferentKind_Panics
//	P5 checkBizMetric 去掉 label 集检查                TestBusinessSameNameDifferentLabels_Panics
//	P6 Handler 改回精确匹配 Accept                     TestHandler_OpenMetricsNegotiation
//	P7 Handler 不调 enc.Close()                        TestHandler_OpenMetricsNegotiation
package metricsx

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	appconfig "rapid-admin/internal/config"
	"rapid-admin/internal/pkg/logger"
)

// TestMain 先 Init —— registry 是包级单例,业务埋点必须在 Init 之后用
func TestMain(m *testing.M) {
	// hertz 默认打 Debug 级路由日志,会把测试输出淹掉
	hlog.SetLevel(hlog.LevelError)
	Init(appconfig.MetricsConfig{
		Enabled:   true,
		Path:      "/metrics",
		Namespace: "app",
	}, "m99-test", "0.0.0-test", "test")
	os.Exit(m.Run())
}

// ── 测试辅助 ──────────────────────────────────────────────────────────

// scrape 抓一次 /metrics,返回 exposition 文本
//
// ⚠️ 为什么用"抓文本"而不是 `Registry().Gather()`:
// Gather 返回的是 `[]*dto.MetricFamily`,dto 来自 client_model ——
// 那是个 **indirect 依赖**,直接用就要动 go.mod。
// 而 exposition 文本本来就是 Prometheus 实际看到的东西,
// 拿它断言反而更贴近"用户能拿到什么"。
func scrape(t *testing.T) string {
	t.Helper()
	e := route.NewEngine(hertzconfig.NewOptions([]hertzconfig.Option{}))
	e.GET("/metrics", Handler())
	w := ut.PerformRequest(e, "GET", "/metrics", &ut.Body{})
	if w.Code != 200 {
		t.Fatalf("/metrics 返 %d,want 200", w.Code)
	}
	return w.Body.String()
}

// sample 取 exposition 文本里某个样本的值
//
// sample 形如 `app_biz_m99_counter{channel="wechat"}` 或 `app_biz_m99_hist_count`
func sample(t *testing.T, body, name string) float64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, name+" ") {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(line[len(name)+1:]), 64)
		if err != nil {
			t.Fatalf("样本 %s 的值解析失败: %v (line=%q)", name, err, line)
		}
		return v
	}
	t.Fatalf("报文里找不到样本 %s", name)
	return 0
}

// hasSample 报文里有没有这个样本
func hasSample(body, name string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, name+" ") {
			return true
		}
	}
	return false
}

// mustPanic 断言 fn 会 panic,且 panic 信息里含 want
func mustPanic(t *testing.T, name, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("%s: 期望 panic,结果没 panic", name)
			return
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, want) {
			t.Errorf("%s: panic 信息 = %q,want 包含 %q", name, msg, want)
		}
	}()
	fn()
}

// ── 1. 基础指标注册 ────────────────────────────────────────────────────

// seedBaseMetrics 通过**暴露给中间件的访问器**造出 4 类 HTTP 指标的样本
//
// ⚠️ 为什么不直接查"注册表里有没有这个指标":
//
// Prometheus 的 Vec(`CounterVec` / `HistogramVec` / `GaugeVec`)**没有子标签时
// 不会出现在 Gather 结果里** —— 空 Vec 是"已注册但无样本",
// 报文中连 `# TYPE` 行都没有。
//
// 所以这里换成"用访问器造一个子标签,再看报文中有没有" ——
// 顺带把"访问器指向的到底是不是注册进去的那个 collector"也验了。
func seedBaseMetrics() {
	HTTPRequestsTotal().WithLabelValues("GET", "/ping", "200").Inc()
	HTTPRequestDuration().WithLabelValues("GET", "/ping", "200").Observe(0.01)
	HTTPRequestsInFlight().Set(1)
	HTTPResponseSize().WithLabelValues("GET", "/ping", "200").Observe(12)
}

// TestInit_RegistersBaseMetrics 4 类 HTTP 指标 + app_info 必须都注册上
//
// app_info 是恒 1 的 gauge,用来在 PromQL 里关联 app/version/env。
func TestInit_RegistersBaseMetrics(t *testing.T) {
	seedBaseMetrics()
	body := scrape(t)
	for _, name := range []string{
		"app_http_requests_total",
		"app_http_request_duration_seconds",
		"app_http_requests_in_flight",
		"app_http_response_size_bytes",
		"app_info",
	} {
		if !strings.Contains(body, "# TYPE "+name+" ") {
			t.Errorf("指标 %s 没注册(报文里没有它的 TYPE 行)", name)
		}
	}
}

// ── 2. 业务埋点:三个函数都必须真的能用 ─────────────────────────────────

// TestBusinessCounter_Usable 业务计数器要能 WithLabelValues().Inc()
//
// # M99 修的第一个 bug
//
// 旧签名返回 `prometheus.Counter` —— 这个 interface **没有 WithLabelValues**,
// 所以包注释里写的用法根本编译不过,业务方只能绕开这个函数或者自己造 Vec。
//
// 现在返回 `*prometheus.CounterVec`,和 Prometheus 官方用法一致。
// TestBusinessCounter_Usable 业务计数器要能 WithLabelValues().Inc()
//
// ⚠️ 旧实现里这个函数**每一次调用都 panic**:它声明返回 `prometheus.Counter`,
// 可 `bizCollectors` 里存的是 `*prometheus.CounterVec` ——
// Vec 只有 `WithLabelValues`,没有 `Add`,断言必失败。
// 实测 panic 消息:`interface conversion: *prometheus.CounterVec is not prometheus.Counter: missing method Add`
func TestBusinessCounter_Usable(t *testing.T) {
	c := BusinessCounter("m99_counter", "channel")
	c.WithLabelValues("wechat").Inc()
	c.WithLabelValues("wechat").Inc()
	c.WithLabelValues("alipay").Inc()

	body := scrape(t)
	if got := sample(t, body, `app_biz_m99_counter{channel="wechat"}`); got != 2 {
		t.Errorf("wechat 计数 = %v,want 2", got)
	}
	if got := sample(t, body, `app_biz_m99_counter{channel="alipay"}`); got != 1 {
		t.Errorf("alipay 计数 = %v,want 1", got)
	}
}

// TestBusinessGauge_Usable 业务 gauge 要能 WithLabelValues().Set()
//
// ⚠️ `*prometheus.GaugeVec` 也**没有** Set 方法 —— 必须先 WithLabelValues。
// 这点旧注释写错过(写成 `.Set(42)`),编译不过。
func TestBusinessGauge_Usable(t *testing.T) {
	g := BusinessGauge("m99_gauge", "queue")
	g.WithLabelValues("default").Set(42)

	body := scrape(t)
	if got := sample(t, body, `app_biz_m99_gauge{queue="default"}`); got != 42 {
		t.Errorf("gauge 值 = %v,want 42", got)
	}
}

// TestBusinessHistogram_Usable 业务直方图要能 WithLabelValues().Observe()
//
// # M99 修的第二个 bug —— 这个函数此前**一次调用就 panic**
//
// 旧实现结尾 `actual.(prometheus.Observer)`:`*prometheus.HistogramVec`
// 没有 Observe 方法 → 运行时断言必 panic。
//
// 这条测试同时是"旧代码会 panic"的证据:旧代码连 `BusinessHistogram(...)`
// 这一行都过不去,更别说 Observe。
func TestBusinessHistogram_Usable(t *testing.T) {
	h := BusinessHistogram("m99_hist", []float64{1, 10, 100})
	h.WithLabelValues().Observe(5)
	h.WithLabelValues().Observe(50)

	body := scrape(t)
	if got := sample(t, body, "app_biz_m99_hist_count"); got != 2 {
		t.Errorf("样本数 = %v,want 2", got)
	}
	if got := sample(t, body, "app_biz_m99_hist_sum"); got != 55 {
		t.Errorf("样本和 = %v,want 55", got)
	}
}

// TestObserveDuration_Records ObserveDuration 要能记录耗时
//
// # M99 修的第三个 bug
//
// 旧实现:
//
//	obs := BusinessHistogram(name, prometheus.DefBuckets, labelNames...)
//	obs.(prometheus.Histogram).Observe(d.Seconds())
//
// 第一行就 panic(`*HistogramVec` 不是 `prometheus.Observer`)。
// 就算不断言,它传的也是**标签名**而不是标签值,接口不可用。
//
// 现在是无标签版本:`ObserveDuration(name, d)`。
func TestObserveDuration_Records(t *testing.T) {
	ObserveDuration("m99_duration", 250*time.Millisecond)

	body := scrape(t)
	if got := sample(t, body, "app_biz_m99_duration_count"); got != 1 {
		t.Errorf("样本数 = %v,want 1", got)
	}
	if got := sample(t, body, "app_biz_m99_duration_sum"); got < 0.24 || got > 0.26 {
		t.Errorf("样本和 = %v,want ≈0.25(秒)", got)
	}
}

// ── 3. 同名冲突:必须给一句人话,而不是 Prometheus 的裸 panic ────────────

// TestBusinessSameNameDifferentKind_Panics 同名不同种类要拦下来
//
// # M99 修的第四个 bug
//
// 旧实现里三个函数共用一个 `bizCollectors` 键空间(counter/gauge 用 name,
// histogram 用 "h:"+name),而且三个函数**每次调用都 panic**
// (结尾 `actual.(prometheus.Counter)` 断言一个 `*CounterVec` —— 必失败)。
// 所以旧实现里根本走不到 Prometheus 的注册冲突那一步。
//
// 现在在注册前查 `bizMetrics`,给出可读的 panic。
func TestBusinessSameNameDifferentKind_Panics(t *testing.T) {
	BusinessCounter("m99_conflict", "kind")
	mustPanic(t, "counter→gauge", "种类必须一致", func() {
		BusinessGauge("m99_conflict", "kind")
	})
	mustPanic(t, "counter→histogram", "种类必须一致", func() {
		BusinessHistogram("m99_conflict", nil, "kind")
	})
}

// TestBusinessSameNameDifferentLabels_Panics 同名但 label 集不同要拦下来
//
// Prometheus 自己也会拦(panic "inconsistent label cardinality"),
// 但那条消息同样不说是哪个指标 —— 而且旧实现里压根走不到那一步
// (接口转换 panic 先炸)。现在提前拦,并把两边的 label 集都报出来。
func TestBusinessSameNameDifferentLabels_Panics(t *testing.T) {
	BusinessCounter("m99_labels", "a")
	mustPanic(t, "label 集不同", "label 集不一致", func() {
		BusinessCounter("m99_labels", "b")
	})
}

// TestBusinessSameNameSameLabels_ReusesInstance 同名同 label 集要复用同一个实例
//
// 重复调用不能重复注册(Prometheus 会 panic "duplicate collector registration")。
func TestBusinessSameNameSameLabels_ReusesInstance(t *testing.T) {
	a := BusinessCounter("m99_reuse", "x")
	b := BusinessCounter("m99_reuse", "x")
	if a != b {
		t.Error("同名同 label 集应该返回同一个实例")
	}
	a.WithLabelValues("v").Inc()
	body := scrape(t)
	if got := sample(t, body, `app_biz_m99_reuse{x="v"}`); got != 1 {
		t.Errorf("计数 = %v,want 1", got)
	}
}

// TestBusinessCounter_EmptyNamePanics 名字必填
func TestBusinessCounter_EmptyNamePanics(t *testing.T) {
	mustPanic(t, "空名字", "name required", func() {
		BusinessCounter("")
	})
	mustPanic(t, "空名字", "name required", func() {
		BusinessGauge("")
	})
	mustPanic(t, "空名字", "name required", func() {
		BusinessHistogram("", nil)
	})
}

// ── 4. /metrics 端点的内容协商 ─────────────────────────────────────────

// TestHandler_OpenMetricsNegotiation Accept 头要真的被协商
//
// # M99 修的第五个 bug
//
// 此前是 `accept == "application/openmetrics-text"` **精确匹配** →
// OpenMetrics 分支是**死代码**;而且即便走进去了,**也没调 `enc.Close()`**,
// OpenMetrics 报文缺少必须的 `# EOF` 收尾行 → 客户端判定报文非法。
//
// Prometheus 真实抓取时发的是复合头:
//
//	Accept: application/openmetrics-text; version=1.0.0,text/plain;version=0.0.4;q=0.5,*/*;q=0.1
//
// 三个断言:
//  1. 复合头 → Content-Type 是 openmetrics
//  2. openmetrics 报文以 "# EOF" 结尾
//  3. 普通请求(浏览器)→ text/plain,且**没有** "# EOF"
func TestHandler_OpenMetricsNegotiation(t *testing.T) {
	e := route.NewEngine(hertzconfig.NewOptions([]hertzconfig.Option{}))
	e.GET("/metrics", Handler())

	// 1. Prometheus 的真实复合头
	w := ut.PerformRequest(e, "GET", "/metrics", &ut.Body{},
		ut.Header{Key: "Accept", Value: "application/openmetrics-text; version=1.0.0,text/plain;version=0.0.4;q=0.5,*/*;q=0.1"})
	if w.Code != 200 {
		t.Fatalf("/metrics 返 %d,want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "openmetrics") {
		t.Errorf("Content-Type = %q,want 含 openmetrics", ct)
	}
	body := w.Body.String()
	if !strings.HasSuffix(strings.TrimSpace(body), "# EOF") {
		t.Errorf("OpenMetrics 报文必须以 \"# EOF\" 结尾(没调 enc.Close()):\n...%s", tailOf(body, 120))
	}

	// 2. 普通客户端(浏览器 / curl)→ text/plain
	w2 := ut.PerformRequest(e, "GET", "/metrics", &ut.Body{},
		ut.Header{Key: "Accept", Value: "text/html,application/xhtml+xml,*/*;q=0.8"})
	if ct := w2.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q,want 含 text/plain", ct)
	}
	if strings.Contains(w2.Body.String(), "# EOF") {
		t.Error("text/plain 报文不该有 \"# EOF\"")
	}
}

// TestHandler_ExposesBaseMetrics 端点里要真的有基础指标
func TestHandler_ExposesBaseMetrics(t *testing.T) {
	seedBaseMetrics()
	body := scrape(t)
	for _, want := range []string{"app_http_requests_total", "app_info"} {
		if !hasSample(body, want) && !strings.Contains(body, "# TYPE "+want+" ") {
			t.Errorf("/metrics 报文里没有 %s", want)
		}
	}
}

// ── 5. 类型断言:三个函数的返回值类型必须是对应的具名 Vec ───────────────

// TestBusinessReturnTypes 返回类型必须带 WithLabelValues
//
// 用编译期断言把"接口有没有 WithLabelValues"这件事钉死 ——
// 这正是旧实现坏掉的地方(返回的 interface 没有这个方法)。
func TestBusinessReturnTypes(t *testing.T) {
	var _ interface {
		WithLabelValues(...string) prometheus.Counter
	} = BusinessCounter("m99_types_counter")
	var _ interface {
		WithLabelValues(...string) prometheus.Gauge
	} = BusinessGauge("m99_types_gauge")
	var _ interface {
		WithLabelValues(...string) prometheus.Observer
	} = BusinessHistogram("m99_types_hist", nil)
}

// tailOf 取字符串末尾 n 个字符
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// ── M150: 同名同标签不同 buckets 的 Warn ───────────────────────────────

// TestBusinessHistogram_DifferentBuckets_Warns 二次注册不同桶必须 Warn 留痕
//
// 缓存键不含 buckets:旧实现第二次注册静默拿到第一套 Vec,新桶配置被无声
// 丢弃(分布全落在错误的桶里)。修法不改缓存行为(把 buckets 加进键 =
// 同一指标重复注册,Prometheus 必 panic),只 Warn。
func TestBusinessHistogram_DifferentBuckets_Warns(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	old := logger.Default
	logger.Default = zap.New(core)
	t.Cleanup(func() { logger.Default = old })

	const name = "m150_hist_buckets_warn"
	// 首次注册 + 同桶复用:不许 Warn
	hv := BusinessHistogram(name, []float64{1, 10, 100}, "ch")
	hv.WithLabelValues("wechat").Observe(5)
	hv = BusinessHistogram(name, []float64{1, 10, 100}, "ch")
	hv.WithLabelValues("wechat").Observe(5)
	if logs.Len() != 0 {
		t.Fatalf("首次注册 / 同桶复用不该 Warn,实际 %d 条: %v", logs.Len(), logs.All())
	}
	// 不同桶:必须 Warn
	BusinessHistogram(name, []float64{1, 10, 1000}, "ch")
	entries := logs.FilterMessage(
		"metricsx: BusinessHistogram buckets changed on re-register, reusing registered buckets").All()
	if len(entries) != 1 {
		t.Fatalf("不同 buckets 二次注册必须产生一条 Warn,实际 %d 条", len(entries))
	}
	if got := entries[0].ContextMap()["metric"]; got != "app_biz_"+name {
		t.Errorf("Warn 必须带 metric 字段指向指标名,实际 %v", got)
	}
}
