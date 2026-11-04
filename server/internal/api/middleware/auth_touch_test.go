// 会话活动心跳(M117-4)中间件侧的单测
//
// # 这一层为什么要有测试
//
// 心跳是"纯记账":写失败不影响任何放行判定。
// 正因为它**不影响正确性**,它才特别容易变成**没人看的死代码** ——
// 中间件里那行 `touchSession(ctx, caller)` 被谁删掉,全仓没有一条测试会红,
// 而线上表现是"审计中心「最后活动」那一列永远是空的"。
//
// 所以这里锁的是**中间件确实调了它**、以及**在不该调的时候确实没调**。
//
// # 反向用例同样重要
//
// 只写"调了"这一条,那把调用挪到 `c.Next` 之前、或者挪进 public 分支里,
// 测试都还是绿的。所以这里同时锁:
//   - 没带 sid 的老 token ⇒ 不调(没有可定位的行)
//   - 鉴权失败(abort)⇒ 不调(请求都没过,不该记活动)
package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/service"
)

// fakeToucher 假的心跳写入方 —— 只记调用
type fakeToucher struct {
	calls []*service.Caller
	err   error
}

// TouchLoginSession 实现 middleware.SessionToucher
func (f *fakeToucher) TouchLoginSession(_ context.Context, caller *service.Caller) error {
	f.calls = append(f.calls, caller)
	return f.err
}

// withToucher 装上假实现,并在用例结束后拆掉
//
// ⚠️ AuthSvc 是**包级全局** —— 不还原的话会污染同包的其它用例
// (本包没有 t.Parallel(),顺序执行是安全的)。
func withToucher(t *testing.T) *fakeToucher {
	t.Helper()
	f := &fakeToucher{}
	AuthSvc = f
	t.Cleanup(func() { AuthSvc = nil })
	return f
}

// TestAuth_TouchesSessionActivity 已认证的请求必须记一次会话活动
func TestAuth_TouchesSessionActivity(t *testing.T) {
	multi := setupAuthMW(t)
	f := withToucher(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
		SessionID: "sid-xyz",
	})
	require.NoError(t, err)

	c := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)
	require.True(t, allowed(c), "前置条件:合法 access token 应放行")

	require.Len(t, f.calls, 1,
		"已认证的请求没记会话活动 —— 审计中心「最后活动」那一列还是空的")
	require.Equal(t, "sid-xyz", f.calls[0].SessionID)
	require.Equal(t, uint64(42), f.calls[0].UserID)
}

// TestAuth_SkipsTouchWithoutSessionID 没有 sid 的 token 不记
//
// 老 token(本轮上线前签发的)没有 sid claim ⇒ 没有可定位的登记簿行。
// 这时候去写库只会打一次"更新 0 行"的空查询,纯浪费。
func TestAuth_SkipsTouchWithoutSessionID(t *testing.T) {
	multi := setupAuthMW(t)
	f := withToucher(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, Username: "u", RoleCode: "role_user_admin",
		// 刻意不给 SessionID
	})
	require.NoError(t, err)

	c := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)
	require.True(t, allowed(c))

	require.Empty(t, f.calls, "没有 sid 的 token 不该触发心跳")
}

// TestAuth_SkipsTouchWhenAuthFails 鉴权失败不记
//
// 请求都被 abort 了,记它"有活动"是假的。
func TestAuth_SkipsTouchWhenAuthFails(t *testing.T) {
	setupAuthMW(t)
	f := withToucher(t)

	c := ctxWithBearer("", "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)
	require.False(t, allowed(c), "前置条件:无 token 应被拒")

	require.Empty(t, f.calls, "鉴权失败的请求不该记会话活动")
}

// TestAuth_PublicPathSkipsTouch public 路径不记
func TestAuth_PublicPathSkipsTouch(t *testing.T) {
	setupAuthMW(t)
	f := withToucher(t)

	c := ctxWithBearer("", "/rapid-admin/v1/public/auth/login")
	Auth()(context.Background(), c)
	require.True(t, allowed(c))

	require.Empty(t, f.calls, "公开路径不该记会话活动")
}

// TestTouchSession_NilServiceIsNoop 没装配时不 panic、不影响请求
//
// 心跳是可选的记账 —— 缺了它不能把请求判失败。
func TestTouchSession_NilServiceIsNoop(t *testing.T) {
	AuthSvc = nil
	t.Cleanup(func() { AuthSvc = nil })

	assert.NotPanics(t, func() {
		touchSession(context.Background(), &service.Caller{UserID: 1, SessionID: "sid-a"})
	})
}

// TestTouchSession_WriteErrorDoesNotBreakRequest 写失败不影响请求
//
// 这条是"纯记账"这个定位的**可执行版本**:写库炸了,请求照样过。
func TestTouchSession_WriteErrorDoesNotBreakRequest(t *testing.T) {
	multi := setupAuthMW(t)
	f := withToucher(t)
	f.err = errors.New("db down")

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, Username: "u", RoleCode: "role_user_admin",
		SessionID: "sid-xyz",
	})
	require.NoError(t, err)

	c := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	assert.NotPanics(t, func() {
		Auth()(context.Background(), c)
	})
	require.True(t, allowed(c), "心跳写失败把请求判失败了 —— 它不该有任何判据能力")
	require.Len(t, f.calls, 1, "写失败也要调(失败要被记下来,不能静默吞掉)")
}

// ===== SSE 长连接 =====

// TestSSEAuth_TouchesSessionActivity SSE 也要记会话活动
//
// SSE 是**长连接**:一条连接可以挂几十分钟,期间用户一直在用。
// 只在 Auth() 上记心跳的话,SSE 用户的那一行永远是空的。
//
// 这里走 `?access_token=`(浏览器 EventSource 的实际用法),不是 header。
func TestSSEAuth_TouchesSessionActivity(t *testing.T) {
	multi := setupAuthMW(t)
	f := withToucher(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, Username: "u", RoleCode: "role_user_admin",
		SessionID: "sid-sse",
	})
	require.NoError(t, err)

	c := &app.RequestContext{}
	c.Request.URI().SetPath("/rapid-admin/v1/auth/perm-stream")
	c.Request.URI().SetQueryString("access_token=" + access)
	SSEAuth()(context.Background(), c)
	require.True(t, allowed(c), "前置条件:合法 token 应放行")

	require.Len(t, f.calls, 1, "SSE 长连接没记会话活动")
	require.Equal(t, "sid-sse", f.calls[0].SessionID)
}

// TestSSEAuth_SkipsTouchWhenAuthFails SSE 鉴权失败不记
func TestSSEAuth_SkipsTouchWhenAuthFails(t *testing.T) {
	setupAuthMW(t)
	f := withToucher(t)

	c := &app.RequestContext{}
	c.Request.URI().SetPath("/rapid-admin/v1/auth/perm-stream")
	SSEAuth()(context.Background(), c)
	require.False(t, allowed(c), "前置条件:无 token 应被拒")

	require.Empty(t, f.calls, "SSE 鉴权失败的请求不该记会话活动")
}
