// CORS 白名单 —— X-Field-Policy 必须在 ExposeHeaders 里(M168)
//
// # 这条用例守的是一个**静默失效**的洞
//
// 跨域时浏览器**只把白名单里的响应头暴露给 JS**。
// `X-Field-Policy` 不在 `ExposeHeaders` 里 ⇒
//
//	前端读 resp.headers['X-Field-Policy'] ⇒ undefined
//
// **不报任何错** —— 后端好好地发了头,前端好好地收不到,
// 于是"字段级可见性的前端消费那一半"整个不工作,
// 而且从两边都看不出哪里出了问题。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	拆法                                                必须变红
//	──────────────────────────────────────────────  ──────────────────────────
//	ExposeHeaders 里删掉 "X-Field-Policy"            TestCORSConfig_暴露字段可见性响应头
//	把 handler.FieldPolicyHeader 改成别的名字         TestCORSConfig_暴露的头名与出口常量一致
//	  (只改一处 ⇒ 两边对不上)
package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/config"
)

func testCorsConfig() *config.Server {
	cfg := &config.Server{}
	cfg.Cors.AllowOrigins = []string{"http://localhost:5173"}
	cfg.Cors.AllowMethods = []string{"GET", "POST"}
	cfg.Cors.AllowHeaders = []string{"Authorization"}
	return cfg
}

func TestCORSConfig_暴露字段可见性响应头(t *testing.T) {
	c := corsConfig(testCorsConfig())
	assert.Contains(t, c.ExposeHeaders, "X-Field-Policy",
		"这个头不在白名单里 ⇒ 前端读不到 ⇒ 字段级可见性的前端那一半静默失效")
}

func TestCORSConfig_暴露的头名与出口常量一致(t *testing.T) {
	c := corsConfig(testCorsConfig())
	require.NotEmpty(t, handler.FieldPolicyHeader)
	assert.Contains(t, c.ExposeHeaders, handler.FieldPolicyHeader,
		"白名单里的头名必须与响应出口写的那个字面量一致 —— 两边各写一份就一定会漂")
}

// TestCORSConfig_原有的三个头没被弄丢
//
// ⚠️ **这是防"修一个坏一个"的用例** ——
// ExposeHeaders 里的 X-Request-Id / X-Trace-Id 都有在用的调用方
// (前端下载文件名要读 Content-Disposition,链路追踪要读 trace id)。
func TestCORSConfig_原有的头没被弄丢(t *testing.T) {
	c := corsConfig(testCorsConfig())
	for _, h := range []string{"X-Request-Id", "X-Trace-Id", "Content-Disposition"} {
		assert.Contains(t, c.ExposeHeaders, h, "既有白名单项不该被删")
	}
}
