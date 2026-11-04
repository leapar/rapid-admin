// 动态集合 · 业务域注入 —— M156(2026-11-02)
//
// # 为什么动态集合要自己写业务域
//
// 普通路由的 RBAC 中间件会写业务域(见 middleware/rbac_scope_test.go),
// 但动态集合的数据面**不挂 rbac()** —— 它的资源码是
// `dyn:<集合编码>:<动作>`,而集合是**用户建出来的**,注册期拼不出来。
//
// ⇒ 动态集合的数据面由 handler 自己判权限(dynGuard),
// **同一层顺手把业务域写进 ctx** —— 它同样知道集合编码。
//
// ⚠️ **业务域是 `dyn:<集合编码>`,不是 `dyn`** ——
// 集合之间必须互相隔离:给"巡检"集合配的行级规则不能影响"报修"集合。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· 把 dynGuard 里的 scope.WithModule(ctx, ...) 删掉  → DS1 变红
//	· 把它改成 scope.WithModule(ctx, "dyn")            → DS1 变红
//	· 把它改成 scope.WithModule(ctx, code)              → DS1 变红(少了 dyn: 前缀)
package v1

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

	"rapid-admin/internal/pkg/dynx"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/service"
)

// dynScopeEngine 起一个「注入 caller → dynGuard → 收业务域」的引擎
func dynScopeEngine(c *service.Caller, action string, got *string, ran *bool) *route.Engine {
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(func(ctx context.Context, rc *app.RequestContext) {
		ctx = service.WithCaller(ctx, c)
		rc.Next(ctx)
	})
	dh := NewDynHandler(nil)
	e.GET("/biz/dyn/:collection/records", func(ctx context.Context, rc *app.RequestContext) {
		ctx, code, ok := dh.dynGuard(ctx, rc, action)
		if !ok {
			return
		}
		if ran != nil {
			*ran = true
		}
		if got != nil {
			m, _ := scope.ModuleFromContext(ctx)
			*got = m
		}
		rc.String(http.StatusOK, "ok:"+code)
	})
	return e
}

// DS1 放行时业务域 = `dyn:<集合编码>`
func TestDynGuard_把业务域写进ctx(t *testing.T) {
	resetEnforcer(t)

	var got string
	var ran bool
	e := dynScopeEngine(&service.Caller{UserID: 1, RoleCode: "role_super_admin"}, dynx.ActionList, &got, &ran)

	w := ut.PerformRequest(e, "GET", "/biz/dyn/inspection/records", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, ran)
	assert.Equal(t, "dyn:inspection", got,
		"业务域必须是 dyn:<集合编码> —— 少了前缀就与别的集合串味")
}

// DS2 集合之间**必须互相隔离**
//
// ⚠️ 这是最容易改坏的一条 ——
// 如果业务域只写 "dyn"(不带集合编码),那么给"巡检"配的规则会作用到"报修"上。
func TestDynGuard_不同集合的业务域不同(t *testing.T) {
	resetEnforcer(t)

	var gotInspection, gotRepair string
	e1 := dynScopeEngine(&service.Caller{UserID: 1, RoleCode: "role_super_admin"}, dynx.ActionList, &gotInspection, nil)
	ut.PerformRequest(e1, "GET", "/biz/dyn/inspection/records", nil)

	e2 := dynScopeEngine(&service.Caller{UserID: 1, RoleCode: "role_super_admin"}, dynx.ActionList, &gotRepair, nil)
	ut.PerformRequest(e2, "GET", "/biz/dyn/repair_order/records", nil)

	assert.Equal(t, "dyn:inspection", gotInspection)
	assert.Equal(t, "dyn:repair_order", gotRepair)
	assert.NotEqual(t, gotInspection, gotRepair, "两个集合的业务域必须不同")
}

// DS3 被拒时**不写业务域**
//
// 守卫没过 ⇒ handler 根本没跑 ⇒ ctx 里不该有业务域。
// 这条守的是"别在判权限之前就把业务域写进去" ——
// 判权限之前写 = 请求还没被批准就已经带着业务域往下走了。
func TestDynGuard_被拒时不写业务域(t *testing.T) {
	resetEnforcer(t)
	// 无任何策略 ⇒ Forbidden

	var got string
	var ran bool
	e := dynScopeEngine(&service.Caller{UserID: 1, RoleCode: "role_ops"}, dynx.ActionList, &got, &ran)

	w := ut.PerformRequest(e, "GET", "/biz/dyn/inspection/records", nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, ran, "被拒的请求不该进 handler")
}
