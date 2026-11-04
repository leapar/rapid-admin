// 数据范围接线 drift 测试 —— M161(2026-11-02)
//
// # 目标
//
// 路由层登记的业务域,每一个都必须能被 `service.CheckScopeWiring()` 认领 ——
// 要么它在 `bizScopeSpecs` 里(受某张业务表管),要么在 `bizScopeCtxWired` 里
// (走 ctx 业务域那条路),要么在 `bizScopeUnwired` 里(明确没有行级范围 + 原因)。
//
// ⚠️ **为什么这条测试住在 router 包而不是 service 包** ——
// service 包里查不到"路由层登记了哪些业务域"(那是路由层的产出),
// 而 router 包正好能拿到 `scope.RegisteredModules()`。
// 反过来 service 不能 import router(会成环)⇒ 这条测试只能在 router 包。
//
// ⚠️ **漏登记的后果是"静默不过滤"** ——
// 路由层新加一个 `rbac("news", "list")`,业务查询里没接数据范围 ⇒
// 管理员在后台给 "news" 配的规则**永远不生效**,而且没有任何报错。
// 那正是 §1.13 #2 记的那句"不声明 ⇒ 启动失败"要治的病。

package router

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/service"
)

// TestRegister_每个业务域都在数据范围的账本里
//
// ⚠️ **这是 M161 的启动期 fail-fast 的前置测试** ——
// cmd/server/main.go 里 `service.CheckScopeWiring()` 不通过就 log.Fatal。
// 这条测试保证那条检查**平时就是绿的**(不是"一上线就红")。
func TestRegister_每个业务域都在数据范围的账本里(t *testing.T) {
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)

	h := server.Default(server.WithHostPorts(":0"))
	Register(h, minimalCfg(), nil, nil, nil)

	require.NotEmpty(t, scope.RegisteredModules(), "路由注册完,业务域登记表不该是空的")

	err := service.CheckScopeWiring()
	require.NoError(t, err,
		"路由层登记的每个业务域都必须在数据范围的三张账本里登记一次 —— "+
			"漏登记的后果是后台配的规则对它永远不生效,而且没有任何报错")
}
