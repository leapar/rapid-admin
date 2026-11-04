// 数据范围 · 业务域注入 —— M156(2026-11-02)
//
// # 这一层是整条链的"起点"
//
// 数据范围(行级权限,M155)的规则是**按业务域**配的:
// 同一个角色,在"用户"域可以是"仅本人",在"审计日志"域可以是"全部"。
//
// 而业务域这个字符串,**判权限的那一层本来就知道** ——
// 它就是 `rbac(module, action)` 的第一个参数。
// ⇒ 中间件放行时把它写进 ctx,service 层只管读。
//
// # 为什么中间件写、不从资源码反推
//
// 资源码是 `module + ":" + action`,而 **module 与 action 都可能含冒号**:
//
//	rbac("audit:operation", "list") → 码 "audit:operation:list" → module 是 "audit:operation"
//	rbac("job", "run:read")         → 码 "job:run:read"         → module 是 "job"
//
// 从码反推 module 只能猜 ⇒ 猜错时**规则静默失配** ⇒
// 退化成"不过滤" ⇒ **越权数据被看到,而且日志里一条都没有**。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· 把 scope.WithModule(ctx, module) 改成 scope.WithModule(ctx, "") → S1/S2 变红
//	· 把 scope.WithModule(ctx, module) 改成 scope.WithModule(ctx, "x") → S1/S2 变红
//	· 把 AccessAllow 分支里的 c.Next(ctx) 改回不带 ctx 的 c.Next(ctx) → S1 变红
//	· 把 c.Next(scope.WithModule(ctx, module)) 挪到 deny 分支 → S3 变红(不该跑 handler)
package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/service"
)

// rbacScopeEngine 起一个「注入 caller → RBAC 中间件 → 收业务域」的引擎
//
// ⚠️ **收业务域的 handler 挂在 RBAC 后面** ——
// 中间件写进 ctx 的值只有下游才看得见,这是它唯一的验收方式。
func rbacScopeEngine(c *service.Caller, module, action string, got *string, ran *bool) *route.Engine {
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(func(ctx context.Context, rc *app.RequestContext) {
		ctx = service.WithCaller(ctx, c)
		rc.Next(ctx)
	})
	e.GET("/x", RBAC(module, action), func(ctx context.Context, rc *app.RequestContext) {
		if ran != nil {
			*ran = true
		}
		if got != nil {
			m, _ := scope.ModuleFromContext(ctx)
			*got = m
		}
		rc.String(http.StatusOK, "ok")
	})
	return e
}

// S1 放行时业务域被写进 ctx
func TestRBAC_放行时把业务域写进ctx(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_ops", "global", "user:list", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	var got string
	var ran bool
	e := rbacScopeEngine(caller("role_ops", 1), "user", "list", &got, &ran)

	w := ut.PerformRequest(e, "GET", "/x", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, ran, "有权限就该进 handler")
	assert.Equal(t, "user", got, "业务域必须原样是 rbac() 的第一个参数")
}

// S2 ★ 业务域**带冒号**时必须原样保留
//
// ⚠️ 这是本模块最容易被改坏的一条 ——
// 从资源码反推 module 时,`audit:operation:list` 会被切成 "audit",
// 而运行时注入的是 "audit:operation" ⇒ 规则失配 ⇒ 不过滤 ⇒ 越权可见。
//
// ⇒ 这条断言的是"**原样**",不是"某个以它开头的字符串"。
func TestRBAC_带冒号的业务域原样保留(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_ops", "global", "audit:operation:list", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	var got string
	var ran bool
	e := rbacScopeEngine(caller("role_ops", 1), "audit:operation", "list", &got, &ran)

	w := ut.PerformRequest(e, "GET", "/x", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, ran)
	assert.Equal(t, "audit:operation", got,
		"业务域必须逐字保留 —— 从资源码反推会把 audit:operation 切成 audit")
}

// S2b action 带冒号时,业务域**不受影响**
//
// rbac("job", "run:read") → 码 "job:run:read" ⇒ module 是 "job",
// 不是 "job:run"。这条钉住"业务域 = 第一个参数的整个字面量"。
func TestRBAC_动作带冒号时业务域不受影响(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_ops", "global", "job:run:read", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	var got string
	var ran bool
	e := rbacScopeEngine(caller("role_ops", 1), "job", "run:read", &got, &ran)

	w := ut.PerformRequest(e, "GET", "/x", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, ran)
	assert.Equal(t, "job", got, "业务域只是 rbac() 的第一个参数,动作不参与")
}

// S3 拒绝时**不进 handler**,也不写业务域
func TestRBAC_拒绝时不写业务域(t *testing.T) {
	resetEnforcer(t)
	// 没有任何策略行 ⇒ Forbidden

	var got string
	var ran bool
	e := rbacScopeEngine(caller("role_ops", 1), "user", "list", &got, &ran)

	w := ut.PerformRequest(e, "GET", "/x", nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, ran, "被拒的请求不该进 handler")
}

// S4 超管直通时**同样要写业务域**
//
// ⚠️ super_admin 走的是 decideAccess 的短路分支,最终落到同一个
// AccessAllow 分支 ⇒ 业务域照样有。
// 这一条守的是"将来有人给超管加一条独立分支时,别把业务域漏掉" ——
// 超管看不到自己部门的数据,那是个很难查的谜案。
func TestRBAC_超管直通也写业务域(t *testing.T) {
	resetEnforcer(t)

	var got string
	var ran bool
	e := rbacScopeEngine(caller("role_super_admin", 1), "audit:operation", "list", &got, &ran)

	w := ut.PerformRequest(e, "GET", "/x", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, ran)
	assert.Equal(t, "audit:operation", got, "超管也必须有业务域")
}
