// metricsx —— Prometheus 指标
//
// ## 这个包解决什么问题
//
// 框架自带 4 类 HTTP 基础指标(自动覆盖),业务方再自己注册
// 业务指标(订单 / 支付 / SQL 慢查询……),全部从同一个 /metrics 端点暴露。
//
// ## 关键点
//
//   - **Init 只能调一次**(sync.Once)。第二次调是 no-op —— 所以测试里
//     想重置 registry,得换进程
//   - **HTTP 指标挂在 cfg.Namespace 下**(留空落 "app"),不挂 subsystem:
//     PromQL 写 app_http_* / demo_http_*
//   - **业务指标用固定前缀 `app_biz_`**(不走 namespace):
//     `BusinessCounter("order_paid_total")` → `app_biz_order_paid_total`
//   - **业务指标的 label 不要放高基数值**(用户 id / 订单 id)——
//     Prometheus 会被撑爆。要放就放可枚举的维度(渠道 / 状态)
//   - **Handler() 返回的是 Hertz handler**,直接挂在路由上就行
//
// ## 跑法
//
//	go run ./examples/metricsx
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/metricsx"
)

func main() {
	// ── 一、初始化 ──────────────────────────────────────────────
	fmt.Println("=== 初始化 ===")
	metricsx.Init(config.MetricsConfig{
		Enabled:   true,
		Path:      "/metrics",
		Namespace: "demo",
	}, "demo-app", "1.0.0", "dev")
	fmt.Printf("registry 已建: %v\n", metricsx.Registry() != nil)

	// Init 用 sync.Once,再调一次是 no-op
	metricsx.Init(config.MetricsConfig{Namespace: "另一个"}, "demo-app", "1.0.0", "dev")
	fmt.Println("  ⇒ Init 是 sync.Once,第二次调不改配置")

	// ── 二、业务埋点 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 业务埋点 ===")
	metricsx.BusinessCounter("order_paid_total", "channel").WithLabelValues("wechat").Inc()
	metricsx.BusinessCounter("order_paid_total", "channel").WithLabelValues("wechat").Inc()
	metricsx.BusinessCounter("order_paid_total", "channel").WithLabelValues("alipay").Inc()

	metricsx.BusinessHistogram("order_amount_yuan",
		[]float64{1, 10, 100, 1000}, "channel").
		WithLabelValues("wechat").Observe(99.5)

	metricsx.BusinessGauge("login_online_total").WithLabelValues().Set(42)

	metricsx.ObserveDuration("order_process_seconds", 150*time.Millisecond)

	fmt.Println("  order_paid_total{channel=wechat} = 2")
	fmt.Println("  order_paid_total{channel=alipay} = 1")
	fmt.Println("  login_online_total = 42")
	fmt.Println("  ⇒ label 只放可枚举的维度;放用户 id 会把 Prometheus 撑爆")

	// ── 三、HTTP 基础指标 ───────────────────────────────────────
	fmt.Println()
	fmt.Println("=== HTTP 基础指标 ===")
	metricsx.HTTPRequestsTotal().WithLabelValues("GET", "/api/v1/houses", "200").Inc()
	metricsx.HTTPRequestsTotal().WithLabelValues("GET", "/api/v1/houses", "500").Inc()
	metricsx.HTTPRequestDuration().WithLabelValues("GET", "/api/v1/houses", "200").Observe(0.12)
	metricsx.HTTPRequestsInFlight().Inc()
	metricsx.HTTPResponseSize().WithLabelValues("GET", "/api/v1/houses", "200").Observe(2048)

	fmt.Println("  framework 自带 4 类:请求计数 / 请求耗时 / 在飞请求数 / 响应体大小")
	fmt.Println("  它们由 middleware.Metrics() 自动埋,业务代码不用管")

	// ── 四、/metrics 端点 ───────────────────────────────────────
	fmt.Println()
	fmt.Println("=== /metrics 端点 ===")
	c := app.NewContext(0)
	metricsx.Handler()(context.Background(), c)
	body := string(c.Response.Body())
	fmt.Printf("  状态码 = %d\n", c.Response.StatusCode())
	fmt.Printf("  正文行数 = %d\n", len(strings.Split(strings.TrimSpace(body), "\n")))

	// 只把业务指标那几行打出来,HTTP 指标太多
	fmt.Println("  业务指标片段:")
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "app_biz_order") || strings.HasPrefix(line, "app_biz_login") {
			fmt.Printf("    %s\n", line)
		}
	}
	fmt.Println("  ⇒ Prometheus 直接 scrape 这个端点")
}
