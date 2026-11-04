// 会话绑定(sid)在**中间件**这一层的接线守卫。
//
// # 为什么这条要单独测
//
// service 层的会话测试(`internal/service/auth_session_test.go`)是**手工**造
// Caller 的 —— 它直接 `&service.Caller{SessionID: c.SessionID}`。
//
// 于是有一条链路是那些测试**碰不到**的:
//
//	token 里的 sid claim → 中间件解析 → Caller.SessionID → service.Logout
//
// 中间件要是漏了 `SessionID: claims.SessionID` 这一行,service 层的测试**照样全绿**
// (它们不走中间件),而真机上登出会静默地"只吊销一半":Logout 拿到的
// caller.SessionID 是空串,RevokeSession 是个 no-op,那张 refresh 还能续命
// 720h(30 天)。**没有报错,没有日志**,只是那个洞又回来了。
//
// 这条测试钉的就是那一行。
//
// # 与 service 层测试的分工
//
// 两条合起来才覆盖整条链,单条都不够:
//
//   - 本文件:middleware 解析出 sid 并**把它放进 Caller**
//   - service/auth_session_test.go:拿着 Caller.SessionID 登出,refresh 真的死
//
// 任何一段断了,至少有一条会红。
package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/service"
)

// TestAuth_InjectsSessionIDIntoCaller 中间件必须把 sid 传下去。
//
// 这是整条链的**接头**。中间件是唯一把 token 变成 Caller 的地方 ——
// service 层拿到的 Caller 一律来自这里(测试里手工造的那些是例外)。
func TestAuth_InjectsSessionIDIntoCaller(t *testing.T) {
	multi := setupAuthMW(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
		SessionID: "sid-mw-test-0001",
	})
	require.NoError(t, err)

	c := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)
	require.True(t, allowed(c), "合法 access token 被拒了")

	raw, ok := c.Get(service.CallerKey)
	require.True(t, ok, "caller 没注入")
	caller, ok := raw.(*service.Caller)
	require.True(t, ok, "caller 类型不对:%T", raw)

	assert.Equal(t, "sid-mw-test-0001", caller.SessionID,
		"中间件没把 sid 传下去 —— Logout 会拿到空 sid,RevokeSession 成了 no-op,"+
			"登出又变回「只吊销 access、refresh 继续续命 720h」")
}

// TestAuth_OldTokenWithoutSessionID_LeavesSessionIDEmpty 老 token 没有 sid claim。
//
// 本轮上线前签发的 token 没有 `sid`(omitempty)。那种 token 登出时退回到
// "只吊销 access 的 jti"(本轮之前的行为,**不是**新引入的缺陷)。
//
// 这里钉住的是**降级的方向**:Caller.SessionID 必须是**空串**,而不是被填成
// 别的什么(比如 token id、用户 id)。
//
// ⚠️ 反过来更危险:如果中间件在 sid 缺失时填了某个**非空**的默认值,那所有
// 老 token 就会共享同一个"会话",登出一次就把**别人**的会话一起吊了。
// 所以这条断言是双向的:既要是空串,也不能是任何别的值。
func TestAuth_OldTokenWithoutSessionID_LeavesSessionIDEmpty(t *testing.T) {
	multi := setupAuthMW(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
		// 不设 SessionID —— 模拟本轮上线前签发的 token
	})
	require.NoError(t, err)

	c := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)
	require.True(t, allowed(c), "老 token 应该照样能过(本轮不做强制下线)")

	raw, ok := c.Get(service.CallerKey)
	require.True(t, ok, "caller 没注入")
	caller, ok := raw.(*service.Caller)
	require.True(t, ok, "caller 类型不对:%T", raw)

	assert.Empty(t, caller.SessionID,
		"老 token 没有 sid,Caller.SessionID 必须是空串 —— 填任何非空默认值都会"+
			"让所有老 token 共享一条会话,登出一次连带吊掉别人的会话")
	assert.NotEqual(t, "sid", caller.SessionID, "SessionID 不能是字面量 'sid'")
	assert.NotEqual(t, caller.JTI, caller.SessionID,
		"SessionID 不能退化成 jti —— 那样每张 token 都是一条独立会话,会话绑定就没了")
}

// TestAuth_RejectsRevokedSessionAccessToken 第三道闸:会话被吊销后,
// 那条会话里的 access token 必须立刻作废。
//
// # 为什么前两道闸不够
//
//   - jti 吊销:认的是**这张 access 自己的 jti**。强制下线吊的是**会话**,
//     这张 access 的 jti 没变 → 前一道闸放行
//   - 作废线:认的是**用户 + 签发时刻**。这张 access 是作废线推进**之前**
//     签发的 → 第二道闸也放行
//
// 于是少了第三道闸,被"强制下线"的那台设备能拿着手里那张 access 继续用
// 8 小时(到它自己过期)。用户点了"下线",那台设备毫无变化 —— 功能是假的。
//
// # 这条断言钉的是**中间件**这一层
//
// service 层的测试只验"refresh 换不动了";access 这一侧只有中间件管。
func TestAuth_RejectsRevokedSessionAccessToken(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
		SessionID: "sid-mw-revoked-0001",
	})
	require.NoError(t, err)

	// 前置条件:吊销之前,这张 token 是好的
	c := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(ctx, c)
	require.True(t, allowed(c), "吊销之前这张 access 必须能过 —— 不然后面的断言没有对照")

	// 吊销**会话**(不是 jti)
	require.NoError(t, auth.RevokeSession(ctx, "sid-mw-revoked-0001", time.Hour))

	c2 := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(ctx, c2)
	assert.False(t, allowed(c2),
		"会话已被吊销,那张 access 还能用 —— 强制下线在 8 小时内形同虚设")
}

// TestAuth_RevokedSessionDoesNotBlockOtherSessions 吊销一条会话
// **不能**连带吊掉同一用户的另一条会话。
//
// 会话是 per-session 的:用户在 A 设备上点"下线其他设备",B 设备上的
// 另一条会话不该跟着死(那是"推进作废线"的全有或全无行为,形状相反)。
func TestAuth_RevokedSessionDoesNotBlockOtherSessions(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	accessA, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
		SessionID: "sid-mw-a-0001",
	})
	require.NoError(t, err)
	accessB, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
		SessionID: "sid-mw-b-0001",
	})
	require.NoError(t, err)

	require.NoError(t, auth.RevokeSession(ctx, "sid-mw-a-0001", time.Hour))

	cA := ctxWithBearer(accessA, "/rapid-admin/v1/system/users")
	Auth()(ctx, cA)
	assert.False(t, allowed(cA), "被吊销的那条会话的 access 必须被拒")

	cB := ctxWithBearer(accessB, "/rapid-admin/v1/system/users")
	Auth()(ctx, cB)
	assert.True(t, allowed(cB),
		"吊销 A 的会话把 B 也连带吊了 —— 会话吊销必须是 per-session 的")
}
