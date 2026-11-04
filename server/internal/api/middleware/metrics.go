// Prometheus metrics 中间件 — §1.8 #8 P0
//
// 在 AccessLog 之后挂(Otel 之前/之后都行,推荐 AccessLog 后):
//
//	Trace → Otel → Recover → AccessLog → Metrics → ...
//
// 行为:
//  1. 进入:InFlight +1
//  2. 业务 handler 执行
//  3. 退出:用 ctx 计时 + body 大小 → 4 类指标(metricx 提供的 registry)
//  4. 异常路径:被 panic 也能落 status=500(Recover 之后 Metrics 自动覆盖)
//
// 注意事项:
//   - path 标签必须**收敛**,否则每次带 ID 的 URL 都会创建新 series,打爆 Prom
//     用 route 模板("/rapid-admin/v1/biz/owners/:id" 而非 "/rapid-admin/v1/biz/owners/123")
//   - 4xx 5xx 都算正常 status(不算 error),用 histogram 看延迟分布
package middleware

import (
	"context"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/metricsx"
)

// Metrics 记录每个 HTTP 请求的 4 类指标
// 无配置开关:挂了就生效;若 metricsx 未 Init,中间件 no-op
func Metrics() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		// 未 Init:跳过(不报错,允许 metrics 模块未启用的场景)
		if metricsx.HTTPRequestsTotal() == nil {
			c.Next(ctx)
			return
		}

		start := time.Now()
		metricsx.HTTPRequestsInFlight().Inc()
		defer metricsx.HTTPRequestsInFlight().Dec()

		c.Next(ctx)

		// 标签:method + 路由模板 + status
		method := string(c.Method())
		path := routePath(c)
		status := strconv.Itoa(c.Response.StatusCode())

		metricsx.HTTPRequestsTotal().WithLabelValues(method, path, status).Inc()
		metricsx.HTTPRequestDuration().WithLabelValues(method, path, status).Observe(time.Since(start).Seconds())
		// ⚠️ M99(2026-10-26)修:响应体大小此前用的是 `Header.ContentLength()`,
		// 而 hertz 只在**文件下载**(app/fs.go)和测试辅助里写这个字段 ——
		// 普通 `c.JSON(...)` 走的是 render → AppendBody,**从不设置它**。
		// 于是 ContentLength() 恒为 0,`app_http_response_size_bytes`
		// 这个指标记录的永远是 0 —— 挂在 P0 特性上的一个恒零指标。
		//
		// 真实大小取 body 缓冲区的长度。
		metricsx.HTTPResponseSize().WithLabelValues(method, path, status).Observe(float64(len(c.Response.Body())))
	}
}

// unmatchedPath 未匹配到路由时用的**固定** path 标签
//
// # ⚠️ M99(2026-10-26)补:兜底**不能**用原始 path
//
// Hertz 的 `c.FullPath()` 对**未匹配路由**(404 / 扫描器乱打)返回空串 ——
// 此前兜底是 `c.Path()`,于是每一个探活请求都会生成一条**新 series**
// (`path="/.env"`、`path="/wp-admin/x"`、`path="/api/v1/users/9182"` …)
// → Prometheus 基数爆炸、内存被打爆;同一份问题在 OTel 那边表现为
// 每个 404 一个新 span 名。
//
// 指标标签要的是"**可聚合的维度**",不是"这一次的原始 URL";
// 原始 URL 在 access.log / audit.log 里(那两个是日志,不是标签)。
const unmatchedPath = "/unmatched"

// routePath 取路由模板路径("/rapid-admin/v1/biz/owners/:id" 而非带 ID 的实际 URL)
// 关键:这是 Prometheus 指标基数控制的核心
//
// # ⚠️ M99(2026-10-26):otel.go 的 span 名也改用本函数
//
// 此前 otel.go 里有一份**同名同职责但行为不同**的拷贝
// (`otelRoutePattern`),它用**原始 path** —— 于是每个带 ID 的 URL 都产生
// 一个新的 span 名,trace 后端基数爆炸,而 metrics 这边收敛得好好的。
// 两份拷贝一份对一份错,已合并成这一份。
//
// 兜底规则:
//  1. 优先 c.FullPath()(routing 之后填,就是路由模板)
//  2. 未匹配路由 → 固定标签 unmatchedPath(**不用**原始 path,见上面的说明)
func routePath(c *app.RequestContext) string {
	// Hertz c.FullPath() 在 routing 后填,等同于 Gin 的 FullPath
	if p := c.FullPath(); p != "" {
		return p
	}
	return unmatchedPath
}
