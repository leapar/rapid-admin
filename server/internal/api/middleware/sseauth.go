// SSE / WebSocket 鉴权中间件(§1.3 #18 P1)
//
// 背景:
//   - EventSource(W3C 标准 SSE 客户端)API 限制:不能设置自定义 Header
//   - WebSocket 浏览器 API 同样:握手阶段不能设 Authorization
//   - 因此 /rapid-admin/v1/auth/perm-stream 这类长连接端点不能用普通 Bearer header 鉴权
//
// 解决:SSEAuth 替代 Auth(只挂在 SSE/WS 路由)
//   1. 优先 Authorization header(支持后端/Node EventSource shim)
//   2. 兜底 ?access_token= query(浏览器 EventSource 标准用法)
//   3. 兜底 access_token cookie(HttpOnly + SameSite,降级到非 JS 环境)
//
// 安全:
//   - ?access_token= 会被 nginx access log 记录,前端应只在不可降级到 cookie 时用
//   - Cookie 模式:前端登录成功后,后端 Set-Cookie;后续 EventSource 自动带
//   - 本中间件不适用 POST/PUT/DELETE 等写操作 —— 那类仍走 Auth()
//
// ⚠️ **2026-10-22 更正**:上面第 2 条描述的是**打算**,不是现状 —— 全仓
// (含 web/)grep 不到任何一处 SetCookie / document.cookie,所以 Cookie 那一级
// 目前不可达,实际只有 ?access_token= 能走通。web/src/hooks/useEventStream.ts
// 就是按这个事实写的(它直接带 query token,没指望 cookie)。
// 真要启用 Cookie 模式,得在登录成功处补 Set-Cookie,并处理 SameSite / CSRF ——
// 那会顺带把"登出后 Cookie 还在"变成一个必须处理的问题,所以没有草率加上。
//
// 鉴权流程:复用 Auth() 的 JWT 解析 + **完整三道吊销闸**(jti / 会话 sid / 作废线),
// 差异只在 token 的来源(多两级兜底)
package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/service"
)

// SSEAuth 长连接鉴权中间件(支持 Header / query / cookie)
//   - 仅用于 SSE / WebSocket 端点(浏览器无 Header 能力)
//   - 写操作仍用 Auth(),query token 不被接受(SSEAuth 显式不提这个能力)
func SSEAuth() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		token := extractTokenSSE(c)
		if token == "" {
			response.Fail(c, errcode.Unauthorized, "未登录(SSE 需 ?access_token= 或 Cookie)")
			c.Abort()
			return
		}
		claims, err := JWTs.ParseAnyAccess(token)
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				response.Fail(c, errcode.TokenExpired, "登录已过期")
			} else {
				response.Fail(c, errcode.TokenInvalid, "令牌无效")
			}
			c.Abort()
			return
		}
		// 与 Auth() 中间件保持同一套吊销判定(W5:去掉 nil 守卫)
		if revoked, err := auth.DefaultRevocation().IsRevoked(ctx, claims.JTI()); err != nil {
			L().Warn("revocation 查询失败,按未吊销放行",
				zap.String("jti", claims.JTI()), zap.Error(err))
		} else if revoked {
			response.Fail(c, errcode.TokenInvalid, "令牌已失效")
			c.Abort()
			return
		}

		// ⚠️ M105(2026-10-28):补齐 Auth() 的**另外两道闸**。
		//
		// 原来这里只查了 jti 吊销,注释却写着"与 Auth() 中间件保持同一套吊销判定"
		// —— 那句话是**假的**:Auth() 有三道闸(见 auth.go 的文件头),
		// 这里只有第一道。少掉的两道各对应一个真实场景:
		//
		//   2. 会话吊销(sid)—— "强制下线其他设备"(§1.2 A2)。
		//      被管理员踢掉的那台设备,它手里那张 access 的 jti 没变、
		//      签发时刻也在作废线之前 → jti 吊销查不出来 →
		//      **被踢的设备照样能连 SSE**,还能再活 8 小时。
		//   3. 作废线 —— 改密码 / 重置密码触发的按用户整体作废(§1.2)。
		//      同样只认"签发时刻",对改密之前签发的 token 一律放行。
		//
		// SSE 是长连接,本来就比普通接口活得久 —— 少两道闸的代价更大。
		if claims.SessionID != "" {
			if revoked, err := auth.IsSessionRevoked(ctx, claims.SessionID); err != nil {
				L().Warn("会话吊销查询失败,按未吊销放行(强制下线可能未生效)",
					zap.String("sid", claims.SessionID), zap.Error(err))
			} else if revoked {
				response.Fail(c, errcode.TokenInvalid, "登录状态已失效,请重新登录")
				c.Abort()
				return
			}
		}
		if invalid, err := auth.SessionInvalidated(ctx, claims); err != nil {
			L().Warn("作废线查询失败,按未作废放行(本次强制下线可能未生效)",
				zap.Uint64("user_id", claims.UserID), zap.Error(err))
		} else if invalid {
			response.Fail(c, errcode.TokenInvalid, "登录状态已失效,请重新登录")
			c.Abort()
			return
		}

		caller := &service.Caller{
			UserID:     claims.UserID,
			IdentityID: claims.IdentityID,
			RoleCode:   claims.RoleCode,
			Username:   claims.Username,
			Audience:   string(claims.Audience()),
			IP:         c.ClientIP(),
			UserAgent:  string(c.UserAgent()),
			RequestID:  string(c.GetHeader(response.HeaderRequestID)),
			TraceID:    string(c.GetHeader(response.HeaderTraceID)),
			ProjectIDs: claims.ProjectIDs,
			JTI:        claims.JTI(),
			// ⚠️ M105:SessionID 之前漏了。Auth() 里有,SSEAuth 里没有 ——
			// 于是 service 层拿 caller.SessionID 去判"是不是同一条会话"时,
			// SSE 这条路永远是空串。
			SessionID: claims.SessionID,
		}
		c.Set(service.CallerKey, caller)
		c.Next(service.WithCaller(ctx, caller))

		// 会话活动心跳(M117-4)—— 与 Auth() 同一套(长连接也算一次活动)
		touchSession(ctx, caller)
	}
}

// extractTokenSSE 优先级:Authorization > ?access_token= > Cookie
func extractTokenSSE(c *app.RequestContext) string {
	// 1. Authorization header(后端/Node 客户端)
	if h := string(c.GetHeader("Authorization")); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	// 2. Query(浏览器 EventSource 标准用法)
	if q := string(c.Query("access_token")); q != "" {
		return q
	}
	// 3. Cookie(HttpOnly,前端登录后 Set-Cookie 写入)
	if ck := string(c.Cookie("access_token")); ck != "" {
		return ck
	}
	return ""
}
