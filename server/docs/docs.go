// Package docs 内嵌并发布「通用后台管理框架手册」。
//
// # 它是什么
//
// server/docs/ 是一份 GitBook(HonKit)源(56 页,每个框架模块一章),
// 构建产物在 server/docs/site/。本包用 go:embed 把产物打进二进制,
// 再用一条路由把它读出去 —— 和 server/swagger/(swagger-ui)完全同一套路子:
//
//	server/swagger/  →  //go:embed ui       →  挂 /swagger
//	server/docs/     →  //go:embed all:site →  挂 /docs
//
// # 为什么产物要进仓库
//
// 跟 server/swagger/ui/ 一个理由:页面长什么样、版本是多少,应该由本仓库说了算,
// 而不是 transitively 由 node_modules 决定;而且部署时不必带 node、不必联网。
//
// ⚠️ **产物是"生成物",但它同时是"发布物"** —— 二进制里嵌的那一份就是它,
// 所以"改了手册没重建"= "线上手册是旧的"。这条漂移由 docs_test.go 盯着。
//
// # 产物怎么保持新鲜
//
// 构建由 cmd/docs 负责(见 server/Makefile 的 make docs):
//
//	go run ./cmd/docs build   # 源变了才重建(比对源文件指纹)
//	go run ./cmd/docs force   # 强制重建
//	go run ./cmd/docs check   # 源变了但没重建 ⇒ 退出码 1
//
// 指纹写在 site/buildinfo.json 里;源文件指纹见 SourceHash。
package docs

import (
	"context"
	"embed"
	"io/fs"
	"path"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// assets 内嵌 site/ 目录下的全部手册静态资源。
//
//go:embed all:site
var assets embed.FS

// Assets 返回手册静态资源的 fs.FS(根就是 index.html 那一层)。
func Assets() fs.FS {
	sub, err := fs.Sub(assets, "site")
	if err != nil {
		// embed 的目录在编译期就定死了,Sub 失败只可能是"目录名写错" ——
		// 那时编译根本过不去。这里返回原始 FS 而不是 panic:
		// 手册挂不上不该把整个服务带崩。
		return assets
	}
	return sub
}

// Mount 把手册挂到 Hertz 路由。
//
//   - mountAt:挂载点(如 "/docs");访问 /docs 会 302 到 /docs/index.html
//   - authMw:鉴权中间件;nil = 不鉴权(仅本地开发用)
//
// ⚠️ **手册里的页面全部是相对链接**(href="storage/cachex.html"、href="gitbook/style.css"),
// 所以挂在任意前缀下都能正常跳 —— 不用改一处链接。
//
// ⚠️ **路径必须过 fs.ValidPath** —— 它拒绝 ".." / "." 段和绝对路径。
// 没有这道闸,`GET /docs/../../etc/passwd` 就能读到嵌入物以外的东西
// (embed.FS 自己也会挡一部分,但那是"碰巧",不能当成防线)。
func Mount(h *server.Hertz, mountAt string, authMw app.HandlerFunc) {
	if h == nil {
		return
	}
	mountAt = strings.TrimSuffix(mountAt, "/")
	// 根路径 → 跳 index.html(浏览器访问 /docs 不会自动补 index.html,直接 404 一脸懵)
	h.GET(mountAt, chain(authMw, func(ctx context.Context, c *app.RequestContext) {
		c.Redirect(consts.StatusFound, []byte(mountAt+"/index.html"))
	})...)
	// 其余路径 → 手册静态资源
	h.GET(mountAt+"/*any", chain(authMw, safeHandler(assetHandler(Assets())))...)
}

// chain 把鉴权中间件拼到 handler 前面;没有鉴权时只返回 handler。
func chain(mw, h app.HandlerFunc) app.HandlersChain {
	if mw == nil {
		return app.HandlersChain{h}
	}
	return app.HandlersChain{mw, h}
}

// safeHandler 包一层 panic 兜底。
//
// ⚠️ /docs 路由挂在全局中间件链**之后**(router.go 里在 middleware 链下面),
// 理论上拿得到 middleware.Recover();但这条路由也可能被单独挂到别的地方,
// 一次 panic 不该带走整个进程 —— 兜底成本极低。
func safeHandler(h app.HandlerFunc) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		defer func() {
			if r := recover(); r != nil {
				c.SetStatusCode(consts.StatusInternalServerError)
				c.Response.SetBodyString(`{"code":500,"message":"manual asset failed"}`)
			}
		}()
		h(ctx, c)
	}
}

// assetHandler 从 fs.FS 里取手册静态资源。
func assetHandler(assets fs.FS) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		name := strings.TrimPrefix(c.Param("any"), "/")
		if name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			notFound(c)
			return
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			notFound(c)
			return
		}
		c.Header("Content-Type", contentTypeOf(name))
		c.SetStatusCode(consts.StatusOK)
		c.Response.SetBodyRaw(data)
	}
}

// notFound 统一 404 出口。
func notFound(c *app.RequestContext) {
	c.SetStatusCode(consts.StatusNotFound)
	c.Response.SetBodyString("not found")
}

// contentTypes 扩展名 → Content-Type。
//
// ⚠️ **.js 必须是 application/javascript** —— 浏览器靠 <script src> 加载,
// 服务端返 text/plain 会被拒绝执行(控制台报 MIME 错),页面一片空白。
//
// ⚠️ **字体那几个不能漏** —— GitBook 主题带 .woff2/.woff/.ttf/.otf/.eot,
// 返 application/octet-stream 时 Chrome 会拒收(控制台报 "Failed to decode
// downloaded font"),图标和代码块字重会掉。
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "application/javascript; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".map":  "application/json; charset=utf-8",
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".ico":  "image/x-icon",
	// 字体(GitBook 主题自带)
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".eot":   "application/vnd.ms-fontobject",
	".txt":   "text/plain; charset=utf-8",
}

// contentTypeOf 按扩展名给静态资产定 Content-Type。
func contentTypeOf(name string) string {
	if ct, ok := contentTypes[strings.ToLower(path.Ext(name))]; ok {
		return ct
	}
	return "application/octet-stream"
}
