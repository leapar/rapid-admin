// swaggerx —— 挂载 Swagger UI + 自动生成 OpenAPI 3.0 doc.json
//
// 用法:
//
//	import "rapid-admin/internal/pkg/swaggerx"
//
//	swaggerx.Mount(h, "/swagger", swaggerx.Info{
//		Title:   "我的平台 - 后台 API",
//		Version: "1.0.0",
//		BaseURL: "https://api.example.com/rapid-admin/v1",
//	}, assets, swaggerx.AuthConfig{Enabled: true, Username: "u", Password: "p"})
//
// 访问 http://host:port/swagger/index.html 看 UI
// 访问 http://host:port/swagger/doc.json 拿 OpenAPI 3.0 原始 JSON
//
// ⚠️ **UI 资产由调用方提供** —— 本包不内置 swagger-ui 文件。
// 仓库里那份在 server/swagger/ui/(嵌入方式见 server/swagger/embed.go)。
// 为什么不内置:swagger-ui 是一份 ~4MB 的第三方产物,塞进 internal/pkg 会让这个
// "框架原语"背上一个没法审计的依赖;放仓库根的 server/swagger/ 则一眼能看见
// 它是什么、什么版本、什么许可 —— 这正是《框架层未完成项清单》§8.1 G4 要的。
//
// ⚠️ **这层必须挂在全局中间件链之前** —— router.go 里它是第一件注册的事,
// 所以拿不到 middleware.Recover();本包自己兜 panic(见 safeHandler)。
package swaggerx

import (
	"context"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// packageHolder 全局持有当前 hertz 实例和 info,供生成 spec 时读取。
//
// 为什么用包级变量:spec 是从运行期的路由表(h.Routes())推出来的,
// 而路由表只有 Hertz 实例自己知道。
var (
	packageHolder = &holder{}
	mountPath     = "/swagger"
	mountOnce     sync.Once
	mountedHertz  *server.Hertz
)

// holder 持有 Hertz 实例与文档元信息
type holder struct {
	mu   sync.RWMutex
	h    *server.Hertz
	info Info
}

// Mount 把 Swagger UI + doc.json 挂到 Hertz 路由。
//   - 必须早于 Auth/RBAC 中间件调用,或单独绕开:在 router.go 里第一行调用
//   - assets 为 nil 时只挂 doc.json(UI 页面 404)
//   - auth.Enabled 为 false 时不挂鉴权闸(仅建议本地开发用)
//   - 幂等:同一进程多次调用(单测场景)只挂一次路由
func Mount(h *server.Hertz, mountAt string, info Info, assets fs.FS, auth AuthConfig) {
	if h == nil {
		return
	}
	packageHolder.mu.Lock()
	packageHolder.h = h
	packageHolder.info = info
	packageHolder.mu.Unlock()
	mountPath = mountAt

	authMw := BasicAuth(auth)

	// 幂等:同一 h 实例只挂一次;换 h(单测场景)则再挂一份
	mountOnce.Do(func() {
		mountedHertz = h
		doMount(h, mountAt, assets, authMw)
	})
	if mountedHertz != h {
		doMount(h, mountAt, assets, authMw)
	}
}

// doMount 实际挂 UI + doc.json 路由
func doMount(h *server.Hertz, mountAt string, assets fs.FS, authMw app.HandlerFunc) {
	// 根路径 → 跳 index.html
	// (浏览器访问 /swagger 不会自动补 index.html,直接给 404 的话用户一脸懵)
	h.GET(mountAt, withAuth(authMw, func(ctx context.Context, c *app.RequestContext) {
		c.Redirect(consts.StatusFound, []byte(mountAt+"/index.html"))
	})...)

	// 暴露 doc.json 端点(主 spec 给 UI 拉)
	h.GET(mountAt+"/doc.json", withAuth(authMw, safeHandler(docJSONHandler))...)

	// 其余路径 → swagger-ui 静态资产
	h.GET(mountAt+"/*any", withAuth(authMw, safeHandler(assetHandler(assets)))...)
}

// withAuth 把鉴权中间件拼到 handler 前面;没有鉴权时只返回 handler。
func withAuth(mw app.HandlerFunc, h app.HandlerFunc) app.HandlersChain {
	if mw == nil {
		return app.HandlersChain{h}
	}
	return app.HandlersChain{mw, h}
}

// safeHandler 包一层 panic 兜底。
//
// ⚠️ /swagger 路由是**在全局中间件链之前挂的**(router.go 里第一件事),
// 所以它拿不到 middleware.Recover() —— 一次 panic 会直接带走整个进程。
// 而 BuildSpec 遍历的是运行期路由表,什么怪路由都可能出现。
func safeHandler(h app.HandlerFunc) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		defer func() {
			if r := recover(); r != nil {
				c.SetStatusCode(consts.StatusInternalServerError)
				c.Response.SetBodyString(`{"code":500,"message":"swagger spec build failed"}`)
			}
		}()
		h(ctx, c)
	}
}

// docJSONHandler 直接吐构建好的 spec JSON
func docJSONHandler(_ context.Context, c *app.RequestContext) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.SetStatusCode(consts.StatusOK)
	c.Response.SetBodyString(packageHolder.readDoc())
}

// assetHandler 从 fs.FS 里取 swagger-ui 静态资产。
//
// ⚠️ **路径必须过 fs.ValidPath** —— 它拒绝 ".." / "." 段和绝对路径。
// 没有这道闸,`GET /swagger/../../etc/passwd` 就能读到文件系统的别的东西
// (embed.FS 自己也会挡一部分,但那是"碰巧",不能当成防线)。
func assetHandler(assets fs.FS) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		if assets == nil {
			c.SetStatusCode(consts.StatusNotFound)
			c.Response.SetBodyString("not found")
			return
		}
		name := strings.TrimPrefix(c.Param("any"), "/")
		if name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			c.SetStatusCode(consts.StatusNotFound)
			c.Response.SetBodyString("not found")
			return
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			c.SetStatusCode(consts.StatusNotFound)
			c.Response.SetBodyString("not found")
			return
		}
		c.Header("Content-Type", contentTypeOf(name))
		c.SetStatusCode(consts.StatusOK)
		c.Response.SetBodyRaw(data)
	}
}

// contentTypeOf 按扩展名给静态资产定 Content-Type。
//
// ⚠️ **.js 必须是 application/javascript** —— swagger-ui 靠 <script src> 加载。
// 服务端返 text/plain 会被浏览器拒绝执行(控制台报 MIME 错),页面一片空白。
func contentTypeOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".map":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}

// readDoc 构建并序列化 spec。
//
// 每次调用都重新构建(路由表可能动态变化 —— 比如热加载场景),
// 但通常只在 UI 刷新时才调,频次低,无性能压力。
func (h *holder) readDoc() string {
	h.mu.RLock()
	hz := h.h
	info := h.info
	h.mu.RUnlock()

	if hz == nil {
		return `{"openapi":"3.0.3","info":{"title":"uninitialized"}}`
	}
	spec := BuildSpec(hz, info, GetRBACCodes())
	SortPathsStable(spec)
	data, err := marshalJSON(spec)
	if err != nil {
		return `{"openapi":"3.0.3","info":{"title":"marshal error"}}`
	}
	return string(data)
}
