// /swagger 端点的挂载与静态资产(2026-11-01 开源通用化 · §8.1 G4)
//
// ## 这条网守的是什么
//
// swagger-ui 的静态资产以前来自 Go module(github.com/swaggo/files),
// 也就是说**页面长什么样不由本仓库决定**。改成仓库内嵌后,这里守住两件事:
//
//  1. 资产真的从 fs.FS 里发得出去,而且 **Content-Type 正确**
//     (.js 返 text/plain 会被浏览器拒绝执行 ⇒ 页面一片空白且控制台无明显报错)
//  2. **路径穿越打不穿** —— `GET /swagger/../../etc/passwd` 必须 404
//
// 另外守住"根路径跳 index.html"——浏览器访问 /swagger 不会自动补 index.html。
package swaggerx

import (
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/route/param"
	"github.com/stretchr/testify/assert"
)

// testAssets 一份最小资产集(真实资产由 server/swagger/embed_test.go 验)
var testAssets = fstest.MapFS{
	"index.html":           {Data: []byte("<html><body>ui</body></html>")},
	"swagger-ui.css":       {Data: []byte(".swagger-ui{}")},
	"swagger-ui-bundle.js": {Data: []byte("var SwaggerUIBundle={};")},
	"favicon-32x32.png":    {Data: []byte("\x89PNG")},
}

// driveMount 发一次请求,返回跑完之后的 RequestContext
//
// ⚠️ **必须用 app.NewContext,不能用 &app.RequestContext{}** ——
// Hertz 的 RequestContext.index 是 int8,零值 0;而 Next() 是
//
//	ctx.index++
//	for ctx.index < len(ctx.handlers) { ... }
//
// 也就是说**手工构造的 ctx 从 index=1 开始跑,handlers[0] 被跳过**。
// 生产链路里 ctx 来自 app.NewContext(maxParams)(index = -1),所以线上没事;
// 但这里 /swagger 的三条路由里,鉴权闸那两条链长为 2、静态资产那条链长为 1,
// 一旦 index 起步错,症状是"handler 压根没跑"⇒ 响应体空、状态码停在 Hertz 的
// 默认值(未写过就是 200)⇒ 看起来像"路由没生效",实际是测试驱动方式错了。
//
// ⚠️ 别改回 server.New + &app.RequestContext{}:仓库里 internal/api/router/sse_route_test.go
// 那样写能过,是因为 server.Default 会塞一个 recover 中间件,链长凑够 2 ——
// 那是巧合,不是约定。
func driveMount(t *testing.T, h *server.Hertz, uri string, header map[string]string) *app.RequestContext {
	t.Helper()
	c := app.NewContext(0)
	c.Request.SetRequestURI(uri)
	c.Request.SetMethod("GET")
	c.Request.SetHost("localhost")
	for k, v := range header {
		c.Request.Header.Set(k, v)
	}
	h.ServeHTTP(context.Background(), c)
	return c
}

func mountTestServer(t *testing.T, auth AuthConfig) *server.Hertz {
	t.Helper()
	h := server.New(server.WithHostPorts(":0"))
	Mount(h, "/swagger", Info{Title: "Test API", Version: "1.0.0"}, testAssets, auth)
	return h
}

// TestMount_路由真的会被执行 —— 反向探针。
//
// 上面那条 driveMount 的注释解释了为什么这条网必须存在:
// 如果驱动方式写错(index 起步不是 -1),**所有**用例都会看到
// "handler 没跑 ⇒ 状态码停在 Hertz 默认的 200 ⇒ 断言全红"。
// 这条用例反过来钉住"路由确实命中了":一个不存在的路径必须是 404,
// 而存在的路径必须拿到我们自己的响应头。
func TestMount_路由会被真正执行(t *testing.T) {
	h := mountTestServer(t, AuthConfig{Enabled: false})

	c := driveMount(t, h, "/swagger/swagger-ui.css", nil)
	assert.Equal(t, 200, c.Response.StatusCode())
	assert.NotEmpty(t, c.Response.Body(), "handler 没跑 ⇒ body 是空的")

	// 未注册的路径 ⇒ Hertz 的 404(说明我们没把"什么都返 200"当默认)
	missing := driveMount(t, h, "/nope", nil)
	assert.Equal(t, 404, missing.Response.StatusCode())
}

func TestMount_根路径跳index(t *testing.T) {
	h := mountTestServer(t, AuthConfig{Enabled: false})
	c := driveMount(t, h, "/swagger", nil)

	assert.Equal(t, 302, c.Response.StatusCode())
	assert.Equal(t, "/swagger/index.html", c.Response.Header.Get("Location"))
}

func TestMount_静态资产的ContentType(t *testing.T) {
	h := mountTestServer(t, AuthConfig{Enabled: false})
	cases := []struct {
		uri  string
		want string
	}{
		{"/swagger/index.html", "text/html; charset=utf-8"},
		{"/swagger/swagger-ui.css", "text/css; charset=utf-8"},
		{"/swagger/swagger-ui-bundle.js", "application/javascript; charset=utf-8"},
		{"/swagger/favicon-32x32.png", "image/png"},
	}
	for _, tc := range cases {
		c := driveMount(t, h, tc.uri, nil)
		assert.Equal(t, 200, c.Response.StatusCode(), "%s 应当 200", tc.uri)
		// ⚠️ Header.ContentType() 返的是 []byte —— 直接跟 string 比,
		// testify 走 reflect.DeepEqual("x", []byte("x")) ⇒ 永远不等(类型不同)。
		assert.Equal(t, tc.want, string(c.Response.Header.ContentType()), "%s 的 Content-Type 不对", tc.uri)
	}
}

func TestMount_docJSON是合法OpenAPI(t *testing.T) {
	h := mountTestServer(t, AuthConfig{Enabled: false})
	c := driveMount(t, h, "/swagger/doc.json", nil)

	assert.Equal(t, 200, c.Response.StatusCode())
	var spec map[string]any
	assert.NoError(t, json.Unmarshal(c.Response.Body(), &spec), "doc.json 不是合法 JSON")
	assert.Equal(t, "3.0.3", spec["openapi"])
	assert.NotNil(t, spec["info"])
}

// TestMount_端到端的穿越请求拿不到资产
//
// ⚠️ **这条网守住的是"结果",不是"哪道门拦的"** ——
// 实测:Hertz 的 URI 解析器会把 "/swagger/../secret.txt" 归一成 "/secret.txt"
// (编码形式 %2e%2e / ..%2f 也一样),所以请求**根本进不了 /swagger 路由**。
//
// ⇒ 也就是说:端到端这条用例**证明不了** assetHandler 里的 fs.ValidPath 闸
// 是否存在(把它拆掉,这条用例照样绿)。闸本身由下面那条单测守。
// 这里留着它,是为了守住"整个端到端链路拿不到穿越内容"这个用户可见的结果。
func TestMount_端到端的穿越请求拿不到资产(t *testing.T) {
	assets := &worstCaseFS{}
	h := server.New(server.WithHostPorts(":0"))
	Mount(h, "/swagger", Info{Title: "Test API", Version: "1.0.0"}, assets, AuthConfig{Enabled: false})

	for _, uri := range []string{
		"/swagger/../secret.txt",
		"/swagger/../../etc/passwd",
		"/swagger/%2e%2e/secret.txt",
		"/swagger/nope.txt",
	} {
		c := driveMount(t, h, uri, nil)
		assert.NotEqual(t, 200, c.Response.StatusCode(), "%s 竟然返回了 200", uri)
		assert.NotContains(t, string(c.Response.Body()), "LEAKED", "%s 把资产内容发出来了", uri)
	}
}

// TestMount_资产处理器自己挡住路径穿越 —— 直接打 assetHandler,绕开路由。
//
// 为什么必须绕开路由:见上面那条的注释,Hertz 的 URI 层会把 ".." 归一化掉,
// 请求压根到不了 handler ⇒ 端到端用例测不到 ValidPath 闸。
//
// 这条闸不是装饰:Hertz 有个 `UseRawPath` 选项,开了之后路由匹配用的是
// **未解码的原始路径**(URI().PathOriginal()),那时 "%2e%2e/" 会原样落进
// catch-all 参数;再叠上 `UnescapePathValues`,参数里就真的会出现 ".."。
// 那是合法配置,不是假想。
func TestMount_资产处理器自己挡住路径穿越(t *testing.T) {
	assets := &worstCaseFS{}
	h := assetHandler(assets)

	for _, name := range []string{
		"../secret.txt",
		"../../etc/passwd",
		"a/../../b.txt",
		"/../secret.txt",
	} {
		c := app.NewContext(0)
		c.Params = param.Params{{Key: "any", Value: name}}

		h(context.Background(), c)

		assert.Equal(t, 404, c.Response.StatusCode(), "%q 竟然不是 404", name)
		assert.NotContains(t, string(c.Response.Body()), "LEAKED", "%q 把资产内容发出来了", name)
	}

	// ⚠️ 关键断言:含 ".." 的路径**连底层 FS 都不该碰到**。
	// 真实资产(embed.FS / MapFS)自己就会拒绝 "..",所以"请求没拿到内容"
	// 这件事它们也能做到 —— 那种绿是假的(被别的门挡住了,不是我们的闸)。
	for _, asked := range assets.asked {
		assert.NotContains(t, asked, "..", "含 .. 的路径 %q 被递给了底层 FS —— ValidPath 闸没生效", asked)
	}
}

// worstCaseFS 一个"最坏情况"的 fs.FS —— 只在探针里用。
//
// 它的行为是:**凡是名字里带 ".." 的,一律照给**(返回 LEAKED);
// 别的名字一律说没有。
//
// 为什么需要它:真实资产是 embed.FS, fstest.MapFS —— 它们**自己**就会拒绝
// 含 ".." 的路径。也就是说,把 mount.go 里的 fs.ValidPath 闸拆掉,
// 用真实资产测**照样绿**:拦住请求的不是我们的闸,是 fstest 的实现。
// 那种绿不能证明闸存在。
//
// worstCaseFS 把"别的门"拆掉,只留下我们自己的闸 ⇒ 闸拆了就会漏。
type worstCaseFS struct {
	asked []string
}

func (w *worstCaseFS) Open(name string) (fs.File, error) {
	w.asked = append(w.asked, name)
	if !strings.Contains(name, "..") {
		return nil, fs.ErrNotExist
	}
	return fstest.MapFS{"probe": &fstest.MapFile{Data: []byte("LEAKED")}}.Open("probe")
}

func TestMount_开了鉴权时全路径都要凭据(t *testing.T) {
	h := mountTestServer(t, AuthConfig{Enabled: true, Username: "u", Password: "p"})

	// 没有凭据 ⇒ 401(UI、doc.json、静态资产一个都不能漏)
	for _, uri := range []string{"/swagger/index.html", "/swagger/doc.json", "/swagger/swagger-ui.css"} {
		c := driveMount(t, h, uri, nil)
		assert.Equal(t, 401, c.Response.StatusCode(), "%s 竟然没要凭据", uri)
	}

	// 有凭据 ⇒ 放行
	c := driveMount(t, h, "/swagger/doc.json", map[string]string{
		"Authorization": basicHeader("u", "p"),
	})
	assert.Equal(t, 200, c.Response.StatusCode())
}
