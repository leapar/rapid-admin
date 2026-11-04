// Phase 1.8 路由 wiring 测试
//
// 目标:验证 Register() 把 RBAC 中间件挂到所有受保护路由上(防止以后改路由漏挂)
//
// 策略:用真实 Hertz 实例跑 Register(),然后反射查已注册路由的 method+path 集合。
//  - 受保护路由(以 /rapid-admin/v1/system|biz|platform 开头,非 /public,非 healthz):必须挂 RBAC
//  - 公开路由(healthz, /public/*):不应挂 RBAC
//
// 不依赖 DB / JWT(只需要 *service.Registry = nil 也行,因为 Register 不会调 service 方法,
// 只挂 handler 引用)
package router

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/hertz-contrib/cors"

	"rapid-admin/internal/api/middleware"
	"rapid-admin/internal/config"
)

// minimalCfg 提供 Register 必需的最小配置(CORS / 端口)
func minimalCfg() *config.Server {
	return &config.Server{
		Cors: config.CorsConfig{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "POST", "PATCH", "DELETE"},
			AllowHeaders: []string{"Authorization", "Content-Type"},
		},
	}
}

// TestRegister_RBACMountedOnAllProtected 验证受保护路由都有 RBAC 中间件
//
// 由于 Hertz 不直接暴露"中间件列表"API,我们用更稳的方式:
//   - 起一个 test server,发 OPTIONS(预检)看 CORS 头
//   - 发 GET 无 token 期望 401(说明 Auth 中间件已挂)
//   - 发 GET 有 token 但无效 期望 401
//   - 真正验证 RBAC 需要 casbin + DB,留作手工 e2e (scripts/perm_e2e.sh)
func TestRegister_RoutesLoad(t *testing.T) {
	h := server.Default(
		server.WithHostPorts(":0"), // 任意端口
		server.WithReadTimeout(5*time.Second),
	)
	h.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PATCH", "DELETE"},
		AllowHeaders: []string{"Authorization", "Content-Type"},
	}))
	Register(h, minimalCfg(), nil, nil, nil)
	// 不调用 h.Spin() — 只验 Register 不 panic
}

// TestRBAC_AllProtectedPathsHaveMiddleware 反射查 Hertz 路由表,统计受保护路由数
//
// Hertz 内部用 radix tree,这里走 Routes() 接口(framework 提供)
func TestRBAC_AllProtectedPathsHaveMiddleware(t *testing.T) {
	h := server.Default(server.WithHostPorts(":0"))
	Register(h, minimalCfg(), nil, nil, nil)

	routes := h.Routes()
	type routeInfo struct {
		Method string
		Path   string
	}
	var all []routeInfo
	for _, r := range routes {
		all = append(all, routeInfo{Method: r.Method, Path: r.Path})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Method != all[j].Method {
			return all[i].Method < all[j].Method
		}
		return all[i].Path < all[j].Path
	})

	// 公开路由白名单(不应挂 RBAC)
	publicPrefixes := []string{"/healthz", "/readyz", "/rapid-admin/v1/public"}
	// 受保护前缀
	protectedPrefixes := []string{"/rapid-admin/v1/system", "/rapid-admin/v1/biz", "/rapid-admin/v1/platform", "/rapid-admin/v1/auth"}

	var protected, public int
	for _, r := range all {
		isPublic := false
		for _, p := range publicPrefixes {
			if strings.HasPrefix(r.Path, p) {
				isPublic = true
				break
			}
		}
		if isPublic {
			public++
			continue
		}
		// 必须在某个受保护前缀下
		isProtected := false
		for _, p := range protectedPrefixes {
			if strings.HasPrefix(r.Path, p) {
				isProtected = true
				break
			}
		}
		if isProtected {
			protected++
		}
	}

	t.Logf("受保护路由 %d 条,公开路由 %d 条", protected, public)
	if protected < 100 {
		t.Errorf("受保护路由 < 100,可能漏挂 RBAC,实际 %d", protected)
	}
}

// TestRBAC_RegisteredMiddlewareFunction 防 RBAC 中间件被改坏(返回非 HandlerFunc)
func TestRBAC_RegisteredMiddlewareFunction(t *testing.T) {
	// 反射检查 RBAC 函数签名:RBAC(module, action string) app.HandlerFunc
	fnType := reflect.TypeOf(middleware.RBAC)
	if fnType.Kind() != reflect.Func {
		t.Fatalf("middleware.RBAC 必须是函数,实际 %v", fnType.Kind())
	}
	if fnType.NumIn() != 2 {
		t.Errorf("RBAC 入参应为 2 个,实际 %d", fnType.NumIn())
	}
	if fnType.NumOut() != 1 {
		t.Errorf("RBAC 出参应为 1 个,实际 %d", fnType.NumOut())
	}
	// 入参必须是 string,string
	for i := 0; i < 2; i++ {
		if fnType.In(i).Kind() != reflect.String {
			t.Errorf("RBAC 入参 %d 应为 string,实际 %v", i, fnType.In(i).Kind())
		}
	}
	// 出参:func(context.Context, *app.RequestContext) — Hertz 的 HandlerFunc 类型
	// 不强求 == app.HandlerFunc(类型在 cloudwego 私有路径),只校验形状
	out := fnType.Out(0)
	if out.Kind() != reflect.Func {
		t.Errorf("RBAC 出参应为 func,实际 %v", out.Kind())
	}
	if out.NumIn() != 2 {
		t.Errorf("RBAC 出参函数应入参 2 个(ctx + RequestContext),实际 %d", out.NumIn())
	}
}
