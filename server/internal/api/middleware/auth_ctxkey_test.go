// Auth 中间件 · CtxKey 注入 —— M163(2026-11-02)
//
// # 为什么响应出口需要这份 ctx
//
// `*app.RequestContext` **不是** context.Context(Hertz v0.9.6:
// 没有 Deadline / Done / Err),而字段级可见性在**响应出口**查规则
// 需要一个 context.Context(GORM 的 WithContext、cachex 的加载器都收它)。
// 中间件链上派生出来的 ctx(scope.WithModule 之类)只沿 c.Next 往下传,
// 到了 handler 里**取不回来**。
//
// ⇒ Auth 中间件把 ctx 存进 Hertz 的 KV(service.CtxKey),
// 响应出口(handler.requestCtx)再取出来。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· 把 auth.go 里 `c.Set(service.CtxKey, authCtx)` 删掉          → C1 变红
//	· 把它改成 `c.Set(service.CtxKey, ctx)`(不带 caller 的那份)     → C1 变红
//	· 把它改成存字符串 `c.Set(service.CtxKey, "ctx")`               → C2 变红(类型不对)
package middleware

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/service"
)

// C1 放行时把带 caller 的 ctx 存进 Hertz 的 KV
//
// ⚠️ **存的是"带 caller 的那份"**,不是入参那份 ——
// 响应出口将来若要从这份 ctx 里取 caller / trace 信息,
// 拿到入参那份就什么都没有。
func TestAuth_放行时把ctx存进KV(t *testing.T) {
	multi := setupAuthMW(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
	})
	require.NoError(t, err)

	c := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)

	require.True(t, allowed(c), "前置条件:合法 access token 必须放行")
	raw, ok := c.Get(service.CtxKey)
	require.True(t, ok, "放行后 ctx 必须存进 Hertz 的 KV —— 响应出口全靠它取")
	ctx, ok := raw.(context.Context)
	require.True(t, ok, "存进去的东西必须是 context.Context,实际 %T", raw)
	caller, ok := service.FromContext(ctx)
	require.True(t, ok, "存进去的必须是**带 caller 的那份** ctx")
	assert.Equal(t, uint64(42), caller.UserID)
}

// C2 存进 KV 的东西**类型必须对**
//
// ⚠️ 响应出口的 requestCtx 对"类型不对"是兜底成 Background 的 ——
// 存错类型不会 panic,只会让"取消语义"悄悄失效。
// 这条钉住:存进去的就是一个真 context.Context。
func TestAuth_存进KV的是真context(t *testing.T) {
	multi := setupAuthMW(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 43, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
	})
	require.NoError(t, err)

	c := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)

	raw, ok := c.Get(service.CtxKey)
	require.True(t, ok)
	if _, ok := raw.(context.Context); !ok {
		t.Fatalf("CtxKey 里存的不是 context.Context(是 %T)—— 响应出口会静默退化成 Background", raw)
	}
}

// C3 被拒时**不写**
//
// 请求没被批准 ⇒ 后面没有 handler 会跑 ⇒ 不该留任何东西在 KV 里。
func TestAuth_被拒时不写ctx(t *testing.T) {
	setupAuthMW(t)

	// 没带 token ⇒ Unauthorized
	c := ctxWithBearer("", "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)

	assert.False(t, allowed(c))
	_, ok := c.Get(service.CtxKey)
	assert.False(t, ok, "被拒的请求不该在 KV 里留 ctx")
}
