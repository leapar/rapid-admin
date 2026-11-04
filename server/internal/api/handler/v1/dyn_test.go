// 动态业务集合 · 数据面权限判定单测 —— M151
//
// ⚠️ 这里守的是本模块最重要的一条安全边界:
//
//	**数据面不挂 rbac() 中间件,权限在 handler 里按集合判。**
//
// 那就意味着"集合编码拼错了 / 权限码算错了 / 中间件忘了挂"这三件事
// 都不会被任何现成的机制拦下来 ——
// 数据面的路由是 `rbacExemptRoutes` 里的豁免项,启动期一致性检查只数个数,
// 不看它判的是哪个码。
//
// ⇒ 所以下面这几条的重点不是"有权限的人能访问",
// 而是"**没有权限的人访问不了**"。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· dynGuard 里把 CanAccess 换成恒 true      → D1 / D3 变红
//	· dynGuard 里把校验编码那段删掉             → D5 变红
//	· dynGuard 里把"未登录"那段删掉             → D6 变红
//	· CanAccess 换成"只看角色不看具体码"        → D3 变红(集合隔离失效)
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
	"rapid-admin/internal/service"
)

// dynGuardEngine 起一个只挂 dynGuard 的引擎
//
// ⚠️ **不接真 service** —— 守卫失败时根本走不到 service 层,
// 而本文件要验的恰恰全是"守卫拦下来"的那几路。
//
// caller == nil 表示"不注入 Caller"(模拟未登录)。
// action == "" 时用清单里的第一个动作(list)。
func dynGuardEngine(t *testing.T, caller *service.Caller, action string) *route.Engine {
	t.Helper()
	if action == "" {
		action = dynx.ActionList
	}
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(func(ctx context.Context, c *app.RequestContext) {
		if caller == nil {
			c.Next(ctx)
			return
		}
		c.Next(service.WithCaller(ctx, caller))
	})
	dh := NewDynHandler(nil)
	e.GET("/biz/dyn/:collection/records", func(ctx context.Context, c *app.RequestContext) {
		// ⚠️ 守卫现在**返回新的 ctx**(M156):业务域 `dyn:<集合编码>` 写在里面。
		// 本文件只验"守卫拦不拦得住",业务域由 dyn_scope_test.go 单独验。
		_, code, ok := dh.dynGuard(ctx, c, action)
		if !ok {
			return
		}
		c.String(http.StatusOK, "ok:"+code)
	})
	return e
}

// D1 **核心**:没有任何策略 → 403(默认拒绝)
//
// 动态业务集合的记录接口默认拒绝:没有 `dyn:<集合编码>:list` 就进不去。
// 一旦有人把 dynGuard 里的 CanAccess 去掉,这条立刻红。
func TestDynGuard_NoPolicy_Forbidden(t *testing.T) {
	resetEnforcer(t)
	e := dynGuardEngine(t, &service.Caller{UserID: 1, RoleCode: "role_ops"}, "")

	w := ut.PerformRequest(e, "GET", "/biz/dyn/inspection/records", nil)
	assert.Equal(t, http.StatusForbidden, w.Code, "没有集合权限码就不该进数据面")
}

// D2 有该集合的 list 码 → 放行
func TestDynGuard_HasCollectionPolicy_Allowed(t *testing.T) {
	enf := resetEnforcer(t)
	// 策略形状与生产一致(casbinx.PolicyRow 的 act 是 "*")
	_, err := enf.AddPolicy("role:role_ops", "global", "dyn:inspection:list", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	e := dynGuardEngine(t, &service.Caller{UserID: 1, RoleCode: "role_ops"}, "")
	w := ut.PerformRequest(e, "GET", "/biz/dyn/inspection/records", nil)
	assert.Equal(t, http.StatusOK, w.Code, "有该集合的 list 码就该放行")
	assert.Equal(t, "ok:inspection", w.Body.String())
}

// D3 **集合隔离**:有**别的**集合的码 → 403
//
// ⚠️ 这是本模块最容易被改坏的一条 ——
// 权限码是 `dyn:<集合编码>:<动作>`,如果判定时只按"角色有没有 dyn: 前缀的码"
// 来放行,那有 A 集合权限的人就能看 B 集合的数据。
func TestDynGuard_OtherCollectionPolicy_Forbidden(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_ops", "global", "dyn:repair_order:list", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	e := dynGuardEngine(t, &service.Caller{UserID: 1, RoleCode: "role_ops"}, "")
	w := ut.PerformRequest(e, "GET", "/biz/dyn/inspection/records", nil)
	assert.Equal(t, http.StatusForbidden, w.Code, "有别的集合的码不该看到这个集合")
}

// D4 super_admin 直通
func TestDynGuard_SuperAdmin_Allowed(t *testing.T) {
	resetEnforcer(t)
	e := dynGuardEngine(t, &service.Caller{UserID: 1, RoleCode: "role_super_admin"}, "")

	w := ut.PerformRequest(e, "GET", "/biz/dyn/inspection/records", nil)
	assert.Equal(t, http.StatusOK, w.Code, "超管绕过绑定,应当直通")
}

// D5 非法集合编码 → 400(不是 403)
//
// ⚠️ 集合编码会拼进资源码,不校验就等于"请求方决定判哪个权限"。
// 校验在判权限**之前**,所以非法编码不会去查任何权限。
func TestDynGuard_InvalidCollectionCode_BadRequest(t *testing.T) {
	resetEnforcer(t)
	// 超管直通 ⇒ 只有"编码校验"这一关能拦住它
	e := dynGuardEngine(t, &service.Caller{UserID: 1, RoleCode: "role_super_admin"}, "")

	w := ut.PerformRequest(e, "GET", "/biz/dyn/Inspection/records", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, "非法集合编码应当 400,而不是放行或 403")
}

// D6 未登录 → 401
func TestDynGuard_NoCaller_Unauthorized(t *testing.T) {
	resetEnforcer(t)
	e := dynGuardEngine(t, nil, "")

	w := ut.PerformRequest(e, "GET", "/biz/dyn/inspection/records", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code, "没登录就该 401")
}

// D7 清单里的每个动作都能走通守卫(资源码按动作逐条生效)
//
// ⚠️ **每个动作各判一次权限** ——
// 守卫是按 `dyn:<集合编码>:<动作>` 判的,动作不同码不同。
// 只测 list 的话,"create 用的码拼错了"这类问题没人看得见。
//
// ⚠️ **同一用例里不能调两次 resetEnforcer** ——
// casbinx.Init 走 sync.Once,第二次调用**不会重建 enforcer**
// (只是 Reload 一次)⇒ 上一半加的策略会漏进下一半。
// ⇒ 这里改用 RemovePolicy 清场。
func TestDynGuard_EveryListedAction_PermissionIsPerAction(t *testing.T) {
	for _, action := range dynx.Actions() {
		t.Run(action, func(t *testing.T) {
			enf := resetEnforcer(t)
			// 清场:这个集合的全部动作码都先摘掉
			for _, a := range dynx.Actions() {
				_, _ = enf.RemovePolicy("role:role_ops", "global", dynx.RecordCode("inspection", a), "*", "allow")
			}
			// 只给这个动作的码
			_, err := enf.AddPolicy("role:role_ops", "global", dynx.RecordCode("inspection", action), "*", "allow")
			require.NoError(t, err)

			e := dynGuardEngine(t, &service.Caller{UserID: 1, RoleCode: "role_ops"}, action)
			w := ut.PerformRequest(e, "GET", "/biz/dyn/inspection/records", nil)
			assert.Equal(t, http.StatusOK, w.Code, "有 %s 的码就该放行 %s", action, action)

			// 换成别的动作的码 → 不该放行(权限是逐动作的,不是逐集合的)
			other := dynx.ActionRead
			if action == dynx.ActionRead {
				other = dynx.ActionDelete
			}
			_, _ = enf.RemovePolicy("role:role_ops", "global", dynx.RecordCode("inspection", action), "*", "allow")
			_, err = enf.AddPolicy("role:role_ops", "global", dynx.RecordCode("inspection", other), "*", "allow")
			require.NoError(t, err)

			e2 := dynGuardEngine(t, &service.Caller{UserID: 1, RoleCode: "role_ops"}, action)
			w2 := ut.PerformRequest(e2, "GET", "/biz/dyn/inspection/records", nil)
			assert.Equal(t, http.StatusForbidden, w2.Code,
				"只有 %s 的码不该放行 %s", other, action)
		})
	}
}

// D8 动作不在清单里 → 500(而不是安静地 403 给所有人)
//
// ⚠️ 这条守的是"配置漏了当场暴露" ——
// 资源码 `dyn:<集合编码>:<动作>` 只有在权限资源池里存在一行才绑得动。
// 动作写错 ⇒ 那条码不存在 ⇒ 谁也绑不上 ⇒
// 该接口对所有非超管 403 **且日志里一条错误都没有**
// (权限判定返 false 是完全正常的一条路径,排查时根本想不到是"码没登记")。
// ⇒ 宁可当场 500,也不要留一个安静坏掉的接口。
func TestDynGuard_ActionNotInList_InternalError(t *testing.T) {
	resetEnforcer(t)
	e := dynGuardEngine(t, &service.Caller{UserID: 1, RoleCode: "role_super_admin"}, "schema")

	w := ut.PerformRequest(e, "GET", "/biz/dyn/inspection/records", nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"清单外的动作应当当场 500 —— 它在权限资源池里不存在,绑给谁都没用")
}
