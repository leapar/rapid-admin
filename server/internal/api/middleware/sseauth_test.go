// SSEAuth 中间件单测 —— **完整三道吊销闸** + Caller.SessionID
//
// # 本文件存在的理由
//
// M105 审计 §1.3 时发现:SSEAuth 的文件头写着"与 Auth() 中间件保持同一套
// 吊销判定" —— 那句话是**假的**。Auth() 有三道闸:
//
//	1. jti 吊销     —— 登出 / 身份切换 / refresh 轮换
//	2. 会话吊销(sid)—— 强制下线其他设备
//	3. 作废线        —— 改密码 / 重置密码
//
// 而 SSEAuth 只有第 1 道。少掉的第 2、3 道各对应一个真实场景:
//
//   - **强制下线**踢掉的设备,手里的 access 的 jti 没变、签发时刻也在作废线
//     之前 → 两道闸都查不出来 → **被踢的设备照样能连 SSE**,还能再活 8 小时。
//   - **改密码**同理:改密码只推作废线,不动 jti。
//
// SSE 是长连接,本来就比普通接口活得久 —— 少两道闸的代价更大。
//
// 另外一条:SSEAuth 之前**没有**把 SessionID 填进 service.Caller,
// 于是 service 层拿 caller.SessionID 判"是不是同一条会话"时,
// 走 SSE 这条路永远是空串。
package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/service"
)

// sseCtx 构造一个带 SSE token(query 参数)的请求上下文
//
// SSE 走 query 是浏览器 EventSource 的标准用法(不能设自定义头)。
func sseCtx(token string) *app.RequestContext {
	c := &app.RequestContext{}
	c.Request.URI().SetPath("/rapid-admin/v1/auth/perm-stream")
	if token != "" {
		c.Request.URI().SetQueryString("access_token=" + token)
	}
	return c
}

// signSSE 签一张带 sid 的 access token
func signSSE(t *testing.T, multi *auth.Multi, userID uint64, sid string) string {
	t.Helper()
	tok, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID:   userID,
		Username: "u",
		RoleCode: "role_user_admin",
		SessionID: sid,
	})
	if err != nil {
		t.Fatalf("签发失败:%v", err)
	}
	return tok
}

// TestSSEAuth_RejectsRevokedSession 是 M105 修的那条(第 2 道闸)
//
// 强制下线吊的是**会话**(sid),不是某一张 access 的 jti。
// 少了这道闸,被踢的设备手里的 token 照样能连 SSE。
func TestSSEAuth_RejectsRevokedSession(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	token := signSSE(t, multi, 42, "sid-m105-1")

	// 前置:会话还没被吊销时必须放行 —— 否则"吊销后被拒"可能只是别的原因
	c0 := sseCtx(token)
	SSEAuth()(ctx, c0)
	if !allowed(c0) {
		t.Fatal("前置条件:吊销前应该放行")
	}

	if err := auth.RevokeSession(ctx, "sid-m105-1", time.Hour); err != nil {
		t.Fatalf("吊销会话失败:%v", err)
	}

	c1 := sseCtx(token)
	SSEAuth()(ctx, c1)
	if allowed(c1) {
		t.Fatal("会话已被吊销,SSE 请求仍然放行了 —— '强制下线'对长连接无效")
	}
}

// TestSSEAuth_RejectsInvalidatedUser 是 M105 修的那条(第 3 道闸)
//
// 改密码 / 重置密码推的是**作废线**(按用户整体作废),不动 jti。
// 少了这道闸,用户改完密码,盗号者手里的长连接照样能收消息。
func TestSSEAuth_RejectsInvalidatedUser(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	token := signSSE(t, multi, 42, "sid-m105-2")

	c0 := sseCtx(token)
	SSEAuth()(ctx, c0)
	if !allowed(c0) {
		t.Fatal("前置条件:作废线推之前应该放行")
	}

	// 作废线推到"现在之后"—— 越过 60s 的时钟偏移宽限(见 pkg/auth/invalidate.go)
	if err := auth.DefaultRevocation().InvalidateFrom(ctx, 42, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatalf("推作废线失败:%v", err)
	}

	c1 := sseCtx(token)
	SSEAuth()(ctx, c1)
	if allowed(c1) {
		t.Fatal("用户已被整体作废,SSE 请求仍然放行了 —— 改密码踢不掉在途长连接")
	}
}

// TestSSEAuth_PopulatesCallerSessionID Caller.SessionID 之前是空的
func TestSSEAuth_PopulatesCallerSessionID(t *testing.T) {
	multi := setupAuthMW(t)

	token := signSSE(t, multi, 42, "sid-m105-3")

	c := sseCtx(token)
	SSEAuth()(context.Background(), c)
	if !allowed(c) {
		t.Fatal("合法 token 被拒了")
	}

	raw, ok := c.Get(service.CallerKey)
	if !ok {
		t.Fatal("caller 没注入")
	}
	caller, ok := raw.(*service.Caller)
	if !ok {
		t.Fatalf("caller 类型不对:%T", raw)
	}
	if caller.SessionID != "sid-m105-3" {
		t.Fatalf("caller.SessionID = %q, want sid-m105-3 —— SSE 这条路的会话 id 丢了", caller.SessionID)
	}
}

// TestSSEAuth_AcceptsValidToken 对照组:合法 token 必须放行
//
// 上一两条如果是因为"中间件坏了 / 解析不对 / 签名不对"而红,这里也会红。
// 一条证明进不去,一条证明能进,两条一起才是守卫。
func TestSSEAuth_AcceptsValidToken(t *testing.T) {
	multi := setupAuthMW(t)

	token := signSSE(t, multi, 42, "sid-m105-4")

	c := sseCtx(token)
	SSEAuth()(context.Background(), c)
	if !allowed(c) {
		t.Fatal("合法的 access token 被 SSE 鉴权拒了 —— 对照组不成立")
	}
}

// TestSSEAuth_RejectsRevokedJTI 第一道闸(jti 吊销)不能被改坏
func TestSSEAuth_RejectsRevokedJTI(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	token := signSSE(t, multi, 42, "sid-m105-5")

	if err := auth.DefaultRevocation().Revoke(ctx, jtiOf(t, multi, token), time.Hour); err != nil {
		t.Fatalf("吊销 jti 失败:%v", err)
	}

	c := sseCtx(token)
	SSEAuth()(ctx, c)
	if allowed(c) {
		t.Fatal("已吊销的 token 仍然放行了")
	}
}

// TestSSEAuth_NoTokenRejected 没有 token 必须拒
func TestSSEAuth_NoTokenRejected(t *testing.T) {
	setupAuthMW(t)
	c := sseCtx("")
	SSEAuth()(context.Background(), c)
	if allowed(c) {
		t.Fatal("无 token 的 SSE 请求被放行了")
	}
}

// TestSSEAuth_RejectsRefreshToken refresh token 不能当 access 用(与 Auth() 同口径)
func TestSSEAuth_RejectsRefreshToken(t *testing.T) {
	multi := setupAuthMW(t)

	refresh, err := multi.RefreshByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, Username: "u", RoleCode: "role_user_admin",
	})
	if err != nil {
		t.Fatalf("签发 refresh 失败:%v", err)
	}

	c := sseCtx(refresh)
	SSEAuth()(context.Background(), c)
	if allowed(c) {
		t.Fatal("refresh token 当 access 用了 —— 那是一张 720h 的万能通行证")
	}
}
