// 业务域登记表 wiring 测试 —— M155(2026-11-02)
//
// # 目标
//
// 数据范围规则按**业务域**配,而业务域的权威定义是 `rbac(module, action)`
// 的第一个参数。路由层必须把它登记到 `internal/pkg/scope` 的登记表里,
// 否则后台页面的业务域下拉框是空的 ——
// 管理员配不了任何规则,而**没有任何报错**。
//
// ⚠️ **这层零覆盖时最容易出事的不是"少登记一个"** ——
// 而是有人把 `scope.RegisterModule(module)` 那行删掉(看着像无用代码),
// 整个功能静默失效:下拉框空、规则配不进去、页面报错但看不出原因。
//
// 所以这里钉住"Register 跑完之后登记表里有东西"。

package router

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/scope"
)

// TestRegister_业务域登记表被填上
func TestRegister_业务域登记表被填上(t *testing.T) {
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)

	h := server.Default(server.WithHostPorts(":0"))
	Register(h, minimalCfg(), nil, nil, nil)

	mods := scope.RegisteredModules()
	require.NotEmpty(t, mods, "路由注册完,业务域登记表不该是空的")

	// ⚠️ **必须含冒号的业务域原样保留** ——
	// rbac("audit:operation", "list") 的业务域是 "audit:operation",
	// 不是 "audit"。切错了规则就与运行时注入的对不上 ⇒ 静默不过滤。
	assert.Contains(t, mods, "audit:operation")
	assert.Contains(t, mods, "user")
	assert.Contains(t, mods, "role")

	// ⚠️ **"audit" 不是业务域** —— 它只是资源码 "audit:operation:list" 的前缀。
	// 把它列进候选 = 管理员能配上一条永远不生效的规则。
	assert.NotContains(t, mods, "audit")
}

// TestRegister_重复注册不累积上一轮的残留
//
// ⚠️ **单测多次 Register 会累积** —— 生产只注册一次,但单测会。
// 残留的业务域会让"这个业务域存在"变成假阳性。
func TestRegister_重复注册不累积上一轮的残留(t *testing.T) {
	scope.ResetModules()
	t.Cleanup(scope.ResetModules)

	h1 := server.Default(server.WithHostPorts(":0"))
	Register(h1, minimalCfg(), nil, nil, nil)
	first := len(scope.RegisteredModules())
	require.NotZero(t, first)

	// 手工塞一个上一轮残留的业务域
	scope.RegisterModule("上一轮的残留")

	h2 := server.Default(server.WithHostPorts(":0"))
	Register(h2, minimalCfg(), nil, nil, nil)

	assert.NotContains(t, scope.RegisteredModules(), "上一轮的残留",
		"第二次 Register 必须先清空登记表")
	assert.Len(t, scope.RegisteredModules(), first)
}
