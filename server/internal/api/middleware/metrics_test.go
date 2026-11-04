// metrics 中间件单测 —— §1.8(M99)
//
// # M99 在这里实证出的 2 个真 bug
//
//  1. **响应体大小恒为 0**
//     中间件此前用 `c.Response.Header.ContentLength()` 取大小,
//     而 hertz 只在**文件下载**(app/fs.go)和测试辅助里写这个字段 ——
//     普通 `c.JSON(...)` 走 render → AppendBody,**从不设置它**。
//     于是 `app_http_response_size_bytes` 记录的永远是 0:
//     挂在 P0 特性上的一个恒零指标,谁也看不出它坏了。
//  2. **未匹配路由(404)用原始 path 当标签**
//     `routePath` 拿 `c.FullPath()`,而 hertz 对**没匹配上**的路由返回空串
//     → 兜底用了 `c.Path()`(原始请求路径)。
//     于是每个扫描器/探活请求都生成一条**新 series**
//     (path="/.env"、path="/wp-admin/x"、path="/api/v1/users/9182" …)
//     → Prometheus 基数爆炸、内存被打爆。
//     修法:兜底改成固定标签 "/unmatched"。
//
// # M99(2026-10-26)可逆探针记录
//
//	拆法                                                     必须变红
//	─────────────────────────────────────────────  ──────────────────────────────────
//	P3 响应大小改回 Header.ContentLength()          TestMetrics_ResponseSizeRecorded
//	P13 routePath 兜底改回 c.Path()                 TestMetrics_UnmatchedPathUsesFixedLabel
//	P13b routePath 兜底改回 c.Path()                TestMetrics_PathLabelIsRouteTemplate
//
//	⚠️ **P3 的坑**:响应大小此前恒为 0 —— 断言"值 == 0"是**绿**的,
//	必须断言"**大于** 0",否则探针拆了也不红。
package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

const obsMetricsMetric = "app_http_response_size_bytes"

// TestMetrics_ResponseSizeRecorded 响应体大小必须真的记上
//
// # M99 修的第一个 bug
//
// 旧实现取 `c.Response.Header.ContentLength()`,普通 JSON 响应恒为 0。
// 修法:取 `len(c.Response.Body())`。
//
// ⚠️ 断言必须盯住"**大于 0**":旧值就是 0,断言 == 0 的话旧实现照样绿。
func TestMetrics_ResponseSizeRecorded(t *testing.T) {
	e := obsNewEngine()
	e.Use(Metrics())
	e.GET("/m99/metrics/size", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(http.StatusOK, map[string]string{"hello": "world", "padding": "0123456789"})
	})

	w := ut.PerformRequest(e, "GET", "/m99/metrics/size", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := obsScrapeMetrics(t)
	labels := map[string]string{
		"method": "GET",
		"path":   "/m99/metrics/size",
		"status": "200",
	}
	sum, ok := obsFindSample(body, obsMetricsMetric+"_sum", labels)
	if !ok {
		t.Fatalf("报文里没有 %s_sum 样本(path=/m99/metrics/size)", obsMetricsMetric)
	}
	if sum <= 0 {
		t.Errorf("响应体大小 = %v,want > 0(旧实现恒为 0 —— ContentLength() 对普通 JSON 响应从不设置)", sum)
	}
	if sum > float64(len(w.Body.String())) {
		t.Errorf("响应体大小 = %v,超过了实际响应长度 %d", sum, len(w.Body.String()))
	}
	// 样本数也应该是 1
	if cnt, ok := obsFindSample(body, obsMetricsMetric+"_count", labels); !ok || cnt != 1 {
		t.Errorf("%s_count = %v(找到=%v),want 1", obsMetricsMetric, cnt, ok)
	}
}

// TestMetrics_PathLabelIsRouteTemplate path 标签必须是**路由模板**
//
// 带 ID 的 URL("/m99/metrics/users/42")必须收敛成模板("/m99/metrics/users/:id"),
// 否则每条业务数据都会生成一条新 series。
//
// ⚠️ 这条同时是 B11(otel 的 span 名用原始 path)的对照:
// 同一件事,metrics 这边收敛、trace 那边不收敛 —— 两份拷贝一份对一份错。
func TestMetrics_PathLabelIsRouteTemplate(t *testing.T) {
	e := obsNewEngine()
	e.Use(Metrics())
	e.GET("/m99/metrics/users/:id", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})

	ut.PerformRequest(e, "GET", "/m99/metrics/users/42", nil)

	body := obsScrapeMetrics(t)
	if !obsHasMetricLabel(body, "app_http_requests_total", "path", "/m99/metrics/users/:id") {
		t.Error("指标里没有路由模板 path=/m99/metrics/users/:id —— 标签没收敛")
	}
	if obsHasMetricLabel(body, "app_http_requests_total", "path", "/m99/metrics/users/42") {
		t.Error("原始路径 /m99/metrics/users/42 进了指标标签 —— 基数会爆")
	}
}

// TestMetrics_UnmatchedPathUsesFixedLabel 404 要落到固定标签上
//
// # M99 修的第二个 bug
//
// hertz 的 `FullPath()` 对**未匹配路由**返回空串 —— 旧兜底用 `c.Path()`,
// 于是每个 404 都带一条新 series。
//
// 修法:固定标签 "/unmatched"。
//
// ⚠️ 注意 hertz 的 `Use()` 语义:"Even 404, 405, static files..." ——
// 全局中间件对 404 **也会跑**,所以这条路径确实会被记录。
func TestMetrics_UnmatchedPathUsesFixedLabel(t *testing.T) {
	e := obsNewEngine()
	e.Use(Metrics())
	e.GET("/m99/metrics/exists", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})

	// 一个**没有**注册过的路径,后面还带随机后缀(模拟扫描器)
	w := ut.PerformRequest(e, "GET", "/wp-admin/probe-9182", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	body := obsScrapeMetrics(t)
	if !obsHasMetricLabel(body, "app_http_requests_total", "path", "/unmatched") {
		t.Error("404 没有落到固定标签 path=/unmatched")
	}
	if obsHasMetricLabel(body, "app_http_requests_total", "path", "/wp-admin/probe-9182") {
		t.Error("原始路径进了指标标签 —— 每个 404 一条 series,基数会爆")
	}
}

// TestMetrics_CountsRequest 请求计数与状态码标签要对
func TestMetrics_CountsRequest(t *testing.T) {
	e := obsNewEngine()
	e.Use(Metrics())
	e.GET("/m99/metrics/count", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusTeapot, "teapot")
	})

	ut.PerformRequest(e, "GET", "/m99/metrics/count", nil)

	body := obsScrapeMetrics(t)
	v, ok := obsFindSample(body, "app_http_requests_total", map[string]string{
		"method": "GET",
		"path":   "/m99/metrics/count",
		"status": "418",
	})
	if !ok {
		t.Fatal("报文里没有 app_http_requests_total{method=GET,path=/m99/metrics/count,status=418}")
	}
	if v != 1 {
		t.Errorf("计数 = %v,want 1", v)
	}
}

// TestMetrics_InFlightBackToZero 请求结束后在飞数要回到 0
//
// 在飞 gauge 用来发现"请求进得来出不去"(连接池打满之类)。
// 如果中间件只加不减,它会单调上涨 —— 那还不如没有。
func TestMetrics_InFlightBackToZero(t *testing.T) {
	e := obsNewEngine()
	e.Use(Metrics())
	e.GET("/m99/metrics/inflight", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})

	ut.PerformRequest(e, "GET", "/m99/metrics/inflight", nil)

	body := obsScrapeMetrics(t)
	v, ok := obsFindSample(body, "app_http_requests_in_flight", nil)
	if !ok {
		t.Fatal("报文里没有 app_http_requests_in_flight")
	}
	if v != 0 {
		t.Errorf("在飞请求数 = %v,want 0(请求结束后必须归零)", v)
	}
}

// TestMetrics_DurationRecorded 耗时要记进直方图
func TestMetrics_DurationRecorded(t *testing.T) {
	e := obsNewEngine()
	e.Use(Metrics())
	e.GET("/m99/metrics/duration", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})

	ut.PerformRequest(e, "GET", "/m99/metrics/duration", nil)

	body := obsScrapeMetrics(t)
	cnt, ok := obsFindSample(body, "app_http_request_duration_seconds_count", map[string]string{
		"method": "GET",
		"path":   "/m99/metrics/duration",
		"status": "200",
	})
	if !ok {
		t.Fatal("报文里没有 app_http_request_duration_seconds_count")
	}
	if cnt != 1 {
		t.Errorf("耗时样本数 = %v,want 1", cnt)
	}
}
