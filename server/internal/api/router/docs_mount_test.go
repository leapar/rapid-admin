// docs_mount_test.go —— /docs(框架手册)路由的 wiring 测试(2026-11-01)
//
// ## 这条网守的是什么
//
// 手册是**内嵌进二进制**的(server/docs/docs.go 的 //go:embed all:site),
// 挂载由 router.Register 完成。这条网守住两件事:
//
//  1. Register 之后 **/docs 真的在路由表里**(没挂上 = 手册永远 404,
//     而"手册挂不上"这件事本身没有任何报错)
//  2. 它**能真的把首页发出去**,而且过的是**完整中间件链**
//     (Recover / Gzip / AccessLog / Metrics —— 挂在这条链之后才有这些)
//
// ⚠️ 手册的静态资产本身由 server/docs 的包内单测守着(Content-Type、路径穿越、
// 相对链接);这里守的是**接线**。
package router

import (
	"context"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/stretchr/testify/assert"

	"rapid-admin/internal/config"
)

// manualCfg 手册端点不鉴权的配置(swagger.auth.mode=none)。
//
// ⚠️ **手册与 /swagger 共用 swagger.auth 的凭据** —— 所以这里用 mode=none
// 把闸拆掉,才能在测试里取到内容。
func manualCfg() *config.Server {
	cfg := minimalCfg()
	cfg.Swagger = config.SwaggerConfig{
		Enabled: true,
		Auth:    config.SwaggerAuthConfig{Mode: "none"},
	}
	return cfg
}

// TestRegister_手册路由已挂载 —— 路由表里必须有 /docs 与 /docs/*any。
func TestRegister_手册路由已挂载(t *testing.T) {
	h := server.Default(server.WithHostPorts(":0"))
	Register(h, manualCfg(), nil, nil, nil)

	paths := make(map[string]bool)
	for _, r := range h.Routes() {
		paths[r.Method+" "+r.Path] = true
	}
	assert.True(t, paths["GET /docs"], "手册根路径没挂上 —— 访问 /docs 会 404")
	assert.True(t, paths["GET /docs/*any"], "手册静态资产路由没挂上")
}

// TestRegister_手册首页能取到 —— 端到端:过完整中间件链拿到 index.html。
func TestRegister_手册首页能取到(t *testing.T) {
	h := server.Default(server.WithHostPorts(":0"))
	Register(h, manualCfg(), nil, nil, nil)

	c := app.NewContext(0)
	c.Request.SetRequestURI("/docs/index.html")
	c.Request.SetMethod("GET")
	c.Request.SetHost("localhost")
	h.ServeHTTP(context.Background(), c)

	assert.Equal(t, 200, c.Response.StatusCode())
	assert.Contains(t, string(c.Response.Body()), "<html", "拿到的不是手册首页")
	assert.Equal(t, "text/html; charset=utf-8", string(c.Response.Header.ContentType()))
}

// TestRegister_手册根路径跳index —— 浏览器访问 /docs 不会自动补 index.html。
func TestRegister_手册根路径跳index(t *testing.T) {
	h := server.Default(server.WithHostPorts(":0"))
	Register(h, manualCfg(), nil, nil, nil)

	c := app.NewContext(0)
	c.Request.SetRequestURI("/docs")
	c.Request.SetMethod("GET")
	c.Request.SetHost("localhost")
	h.ServeHTTP(context.Background(), c)

	assert.Equal(t, 302, c.Response.StatusCode())
	assert.Equal(t, "/docs/index.html", c.Response.Header.Get("Location"))
}
