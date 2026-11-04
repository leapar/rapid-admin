// docs_test.go —— 手册的"同步网"与"静态资产网"(2026-11-01)
//
// ## 这两条网各守什么
//
//  1. **同步网**:源(md / book.json / 插件版本)改了但没重新构建 ⇒ 线上手册是旧的。
//     这个坑**没有任何报错**:编译过、测试过、页面照开,只是内容停在上一版。
//     判据是源文件指纹(buildinfo.json 里那串),见 IsStale。
//
//  2. **静态资产网**:手册是纯静态站,发出去的时候不能出三件事 ——
//     路径穿越拿到嵌入物以外的东西 / Content-Type 不对导致页面空白 /
//     链接写死绝对路径导致换前缀就裂。
//
// ## 为什么"产物过期"要在测试里守,而不是只在 build 里提示
//
// build 那条是"编的时候顺手提一句",CI 跑 `go test` 时不会执行;
// 测试这条每次都在,而且**红得明确**(告诉你要跑哪条命令)。
package docs

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/route/param"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/docsbuild"
)

// TestManual_产物不落后于源 —— 同步网本体。
//
// ⚠️ **改了 server/docs/ 下的任何 .md / book.json / package.json 之后
// 必须跑 `make docs` 重新构建并把 site/ 提交上来**,否则这条会红。
//
// ⚠️ 指纹算法本身在 internal/docsbuild(那边有"指纹对变化敏感"的反向探针);
//
//	本条守的是"仓库里这份产物 == 当前源"。
func TestManual_产物不落后于源(t *testing.T) {
	stale, bi, err := docsbuild.IsStale(".")
	require.NoError(t, err, "读手册构建信息失败")
	if stale {
		t.Fatalf("手册源已改动但没重新构建 —— 跑 `make docs`(go run ./cmd/docs build),"+
			"然后把 server/docs/site/ 一起提交。当前产物指纹=%s", bi.SourceHash)
	}
}

// TestManual_核心资产齐全 —— 产物没被删干净 / 没构建全。
func TestManual_核心资产齐全(t *testing.T) {
	assets := Assets()
	for _, name := range []string{
		"index.html",
		"gitbook/style.css",
		"gitbook/gitbook.js",
		"search_plus_index.json",
	} {
		data, err := fs.ReadFile(assets, name)
		require.NoError(t, err, "手册缺少 %s", name)
		assert.NotEmpty(t, data, "%s 是空文件", name)
	}
}

// TestManual_链接全是相对路径 —— 保证能挂在任意前缀下。
//
// ⚠️ 这条守的是"手册挂在 /docs 下能用,挂到 /foo/bar 下也能用":
// GitBook 生成的链接全是相对的(href="storage/cachex.html"),
// 但只要有一处写成 href="/storage/cachex.html",换前缀就全裂。
func TestManual_链接全是相对路径(t *testing.T) {
	assets := Assets()
	err := fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		data, err := fs.ReadFile(assets, p)
		if err != nil {
			return err
		}
		body := string(data)
		for _, bad := range []string{`href="/`, `src="/`} {
			if strings.Contains(body, bad) {
				t.Errorf("%s 里出现了绝对路径引用 %q —— 手册挂到子路径下会裂", p, bad)
			}
		}
		return nil
	})
	require.NoError(t, err)
}

// driveMount 发一次请求,返回跑完之后的 RequestContext。
//
// ⚠️ **必须用 app.NewContext,不能用 &app.RequestContext{}** ——
// Hertz 的 RequestContext.index 是 int8,零值 0;而 Next() 是
//
//	ctx.index++
//	for ctx.index < len(ctx.handlers) { ... }
//
// 手工构造的 ctx 从 index=1 开始跑,handlers[0] 被跳过。
// 生产链路里 ctx 来自 app.NewContext(maxParams)(index = -1),所以线上没事。
func driveMount(t *testing.T, h *server.Hertz, uri string) *app.RequestContext {
	t.Helper()
	c := app.NewContext(0)
	c.Request.SetRequestURI(uri)
	c.Request.SetMethod("GET")
	c.Request.SetHost("localhost")
	h.ServeHTTP(context.Background(), c)
	return c
}

// TestMount_根路径跳index —— 浏览器访问 /docs 不会自动补 index.html。
func TestMount_根路径跳index(t *testing.T) {
	h := server.New(server.WithHostPorts(":0"))
	Mount(h, "/docs", nil)

	c := driveMount(t, h, "/docs")
	assert.Equal(t, 302, c.Response.StatusCode())
	assert.Equal(t, "/docs/index.html", c.Response.Header.Get("Location"))
}

// TestMount_静态资产的ContentType —— Content-Type 不对 = 页面一片空白。
func TestMount_静态资产的ContentType(t *testing.T) {
	h := server.New(server.WithHostPorts(":0"))
	Mount(h, "/docs", nil)

	cases := []struct {
		uri  string
		want string
	}{
		{"/docs/index.html", "text/html; charset=utf-8"},
		{"/docs/gitbook/style.css", "text/css; charset=utf-8"},
		{"/docs/gitbook/gitbook.js", "application/javascript; charset=utf-8"},
		{"/docs/search_plus_index.json", "application/json; charset=utf-8"},
	}
	for _, tc := range cases {
		c := driveMount(t, h, tc.uri)
		assert.Equal(t, 200, c.Response.StatusCode(), "%s 应当 200", tc.uri)
		assert.Equal(t, tc.want, string(c.Response.Header.ContentType()), "%s 的 Content-Type 不对", tc.uri)
	}
}

// TestMount_资产处理器自己挡住路径穿越 —— 直接打 assetHandler,绕开路由。
//
// ⚠️ 为什么必须绕开路由:Hertz 的 URI 层会把 ".." 归一化掉
// (/docs/../secret.txt → /secret.txt),请求压根到不了 handler
// ⇒ 端到端用例测不到 fs.ValidPath 闸。
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
	// 真实资产(embed.FS)自己就会拒绝 "..",所以"请求没拿到内容"这件事
	// 它们也能做到 —— 那种绿是假的(被别的门挡住了,不是我们的闸)。
	for _, asked := range assets.asked {
		assert.NotContains(t, asked, "..", "含 .. 的路径 %q 被递给了底层 FS —— ValidPath 闸没生效", asked)
	}
}

// TestMount_端到端的穿越请求拿不到资产 —— 守住用户可见的结果。
func TestMount_端到端的穿越请求拿不到资产(t *testing.T) {
	h := server.New(server.WithHostPorts(":0"))
	Mount(h, "/docs", nil)

	for _, uri := range []string{
		"/docs/../secret.txt",
		"/docs/../../etc/passwd",
		"/docs/%2e%2e/secret.txt",
		"/docs/nope.html",
	} {
		c := driveMount(t, h, uri)
		assert.NotEqual(t, 200, c.Response.StatusCode(), "%s 竟然返回了 200", uri)
	}
}

// TestMount_挂了鉴权中间件时资产要凭据 —— 手册里有默认账号与配置键名。
func TestMount_挂了鉴权中间件时资产要凭据(t *testing.T) {
	h := server.New(server.WithHostPorts(":0"))
	Mount(h, "/docs", func(ctx context.Context, c *app.RequestContext) {
		c.SetStatusCode(401)
		c.Abort()
	})

	c := driveMount(t, h, "/docs/index.html")
	assert.Equal(t, 401, c.Response.StatusCode())
}

// worstCaseFS 一个"最坏情况"的 fs.FS —— 只在探针里用。
//
// 它的行为是:**凡是名字里带 ".." 的,一律照给**(返回 LEAKED);
// 别的名字一律说没有。
//
// 为什么需要它:真实资产是 embed.FS,它们**自己**就会拒绝含 ".." 的路径。
// 也就是说,把 assetHandler 里的 fs.ValidPath 闸拆掉,用真实资产测**照样绿**:
// 拦住请求的不是我们的闸,是 embed.FS 的实现。那种绿不能证明闸存在。
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
