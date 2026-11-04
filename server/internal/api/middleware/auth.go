// Auth 中间件 —— JWT 鉴权 + caller 注入 + 吊销/作废校验
// 文档 §8.6 / §9.6
//
// 三道闸,顺序固定:
//  1. jti 吊销(IsRevoked)—— 登出 / 身份切换 / refresh 轮换,只认"眼前这一张"
//  2. 会话吊销(IsSessionRevoked)—— 登出 / 强制下线,认"这一条会话"(sid)
//  3. 作废线(SessionInvalidated)—— 改密码 / 重置密码,按用户整体作废
//
// 顺序无所谓对错,但三道都必须有:
//   - 只留 1:改密码踢不掉盗号者的会话(漏了 3)
//   - 只留 1+3:登出后那张 refresh 还能续期(漏了 2,而 refresh 走公开路由
//     不过中间件,所以 RefreshToken 那边还要自己再查一遍)
//   - 只留 1+2:强制下线后,被踢的设备手里那张 access 还能用 8 小时
//     (漏了 3)—— 因为前两道闸都不认"这条 access 是哪条会话发的"
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

// PublicPaths 无需登录的路径前缀。
//
// ⚠️ **必须与 router.go 里 pub 组实际注册的路由保持一致**。
//
// 当前 Auth() 只挂在受保护组(/rapid-admin/v1)上,pub 组的路由根本不会走到这里,
// 所以这份清单**今天是防御性的**:它防的是"哪天有人把 Auth() 改成全局挂载"或
// "把它也挂到 pub 组上",那时任何一条漏网的公开路由都会变成 401 ——
// 而最典型的后果是**登录页自己进不去**,现场表现为"服务全挂了"。
//
// 2026-10-21 之前这份清单只有 6 条,漏了 login-sms / reset-password-sms /
// public/config 三条(共 9 条)。它在没有 Auth() 的情况下不影响生产,但它是一份
// 骗人的清单:读的人会以为"列在这儿就是全部公开路由"。宁可它现在就是完整的。
//
// 改 router 的 pub 组时,记得同步这里,并跑
// TestAuth_PublicPathsCoverEveryRegisteredPublicRoute。
var PublicPaths = []string{
	"/healthz",
	"/readyz",
	"/rapid-admin/v1/public/auth/login",
	"/rapid-admin/v1/public/auth/login-sms",
	"/rapid-admin/v1/public/auth/mfa/send",
	"/rapid-admin/v1/public/auth/refresh",
	"/rapid-admin/v1/public/auth/reset-password-sms",
	"/rapid-admin/v1/public/auth/sms/send",
	"/rapid-admin/v1/public/auth/sms/verify",
	// 公开只读 dyn(2026-11-03)—— 集合 public_read 开关放行,闸门在 service
	"/rapid-admin/v1/public/dyn/:collection/schema",
	"/rapid-admin/v1/public/dyn/:collection/records",
	"/rapid-admin/v1/public/dyn/:collection/records/:id",
	"/rapid-admin/v1/public/captcha",
	"/rapid-admin/v1/public/config",
}

// Auth 校验 JWT,注入 caller 到 ctx + Hertz 上下文
func Auth() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())
		if isPublicPath(path) {
			c.Next(ctx)
			return
		}
		token := ExtractToken(c)
		if token == "" {
			response.Fail(c, errcode.Unauthorized, "未登录")
			c.Abort()
			return
		}
		// 三受众解析(从 token claim 选 manager)+ 强制 access 类型
		// ParseAnyAccess 会拒绝 refresh / short / 无 typ 的老 token ——
		// 所以拿着 refresh token 打受保护路由会被挡在门外,而不是拿 720h 的
		// 有效期畅通无阻。
		claims, err := JWTs.ParseAnyAccess(token)
		if err != nil {
			// 用 sentinel 而不是 strings.Contains(err.Error(), "expired")。
			// 字符串匹配依赖 jwt 库的错误文案,升级就可能失配,而失配的后果是
			// 过期 token 被报成"令牌无效" —— 前端拿到 TokenInvalid 而不是
			// TokenExpired,刷新流程走错分支。jwt v5 把多个校验错误 errors.Join
			// 到一起,errors.Is 仍然正确。
			if errors.Is(err, jwt.ErrTokenExpired) {
				response.Fail(c, errcode.TokenExpired, "登录已过期")
			} else {
				response.Fail(c, errcode.TokenInvalid, "令牌无效")
			}
			c.Abort()
			return
		}
		// 吊销校验(jti 失效 / 强制登出)
		// 两层旧守卫全删(W5):
		//   `if bl != nil`      —— DefaultRevocation 保证非 nil,之前这层判空
		//                          正是"Redis 没配就静默失去吊销"的成因
		//   `if claims.JTI != ""` —— sign() 无条件写 JTI(W3 起),恒真
		// 留着判空只会让人以为"存储没配 = 免检",等于给将来留一条绕过吊销的口子。
		//
		// 查询出错按"未吊销"放行:吊销存储故障不应该把人全踢下线,
		// 但这条降级是有代价的(见 ResolveRevocation 的注释),所以只兜底启动期。
		if revoked, err := auth.DefaultRevocation().IsRevoked(ctx, claims.JTI()); err != nil {
			L().Warn("revocation 查询失败,按未吊销放行",
				zap.String("jti", claims.JTI()), zap.Error(err))
		} else if revoked {
			response.Fail(c, errcode.TokenInvalid, "令牌已失效")
			c.Abort()
			return
		}

		// 会话吊销校验(第三道闸)—— 让"强制下线"在被踢的那台设备上**立刻**生效。
		//
		// 前两道闸里,jti 吊销认的是"这张 access 自己的 jti",作废线认的是
		// "用户 + 签发时刻" —— 都不认"这张 access 是哪条会话发的"。
		// 而"强制下线其他设备"吊的恰恰是**会话**(sid)。
		//
		// 少了这道闸,"强制下线"就是假的:被踢的设备手里那张 access 的 jti 没变、
		// 签发时刻也在作废线之前,前两道闸全放行 —— 它能继续用 8 小时。
		// 用户点完按钮发现那台设备还好端端地登着,只会认为功能没实现。
		//
		// 查不到(存储故障)按未吊销放行,与前两道闸同一取向:存储抖一下不该
		// 把人全踢下线。代价是"这次强制下线可能没生效",必须留痕。
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

		// 作废线校验(改密码 / 重置密码触发的强制下线,见 pkg/auth/invalidate.go)。
		//
		// 与上面那次 IsRevoked 是**两道独立的闸**,不能互相替代:
		//   - jti 吊销只认"眼前这一张",改密码只 revoke caller.JTI,于是同一用户的
		//     其它会话(另一台设备、另一个浏览器、以及盗号者)纹丝不动;
		//   - 作废线按用户整体作废,补的正是这一块。
		//
		// 错误码沿用 TokenInvalid(与 jti 吊销同一码):前端对 40102 的处理是
		// 清会话 + 回登录页,这正是"改完密码后本设备被踢"该有的表现。
		// 另起一个码就得同步改前端分支,收益只是错误文案更精确,不划算。
		invalid, err := auth.SessionInvalidated(ctx, claims)
		if err != nil {
			// 与上面同向降级:查不到就放行。存储故障不该把人全踢下线,
			// 代价是这一次强制下线可能没生效 —— 必须留痕,否则查不出来。
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
			SessionID:  claims.SessionID,
		}
		// 同时挂到 Hertz 请求上下文(handler 用)与 context.Context(service 用)
		c.Set(service.CallerKey, caller)
		authCtx := service.WithCaller(ctx, caller)
		// ⚠️ **M163:把 ctx 也存进 Hertz 的 KV** ——
		//
		// 响应出口(BaseHandler.OK / Page)手上只有一个 RequestContext,
		// 而 `*app.RequestContext` **不是** context.Context
		// (Hertz v0.9.6:没有 Deadline / Done / Err),
		// 中间件链上派生出来的 ctx 也**取不回来**。
		//
		// 字段级可见性(M163)在响应出口查规则需要一个 context.Context
		// (GORM 的 WithContext 与 cachex 的加载器都收它)
		// ⇒ 由这一层提前存进 KV,响应出口再取。
		//
		// 为什么存**这一层**而不是 RBAC 层 ——
		// 动态集合的数据面(dyn:<集合编码>)不挂 rbac(),
		// 它由 handler 自己的 dynGuard 判权限;
		// 而 Auth 是**所有**受保护路由的共同前置,存这里覆盖面最全。
		c.Set(service.CtxKey, authCtx)
		c.Next(authCtx)

		// 会话活动心跳(M117-4):这次请求打在一条已认证的会话上
		// = 这条会话还活着 ⇒ 记一次 last_active_at。
		//
		// 放在 c.Next **之后**:它是记账,不是判据 —— 不该挡在业务逻辑前面。
		touchSession(ctx, caller)
	}
}

// touchSession 记一次会话活动(Auth / SSEAuth 共用)
//
// ⚠️ **纯记账**:写失败只影响"活跃设备列表"的显示精度,
// 不影响任何放行判定(放行看的是吊销存储的三道闸,与这一列无关)。
//
// AuthSvc 为 nil(单测 / 没装配)时静默跳过 —— 心跳是可选的,
// 不能因为它没装配就把请求判失败。
//
// sid 为空(本轮上线前签发的老 token 没有 sid)也跳过 —— 没有可定位的行。
func touchSession(ctx context.Context, caller *service.Caller) {
	if AuthSvc == nil || caller == nil || caller.SessionID == "" {
		return
	}
	if err := AuthSvc.TouchLoginSession(ctx, caller); err != nil {
		L().Warn("记录会话活动失败(仅影响活跃设备列表精度)",
			zap.String("sid", caller.SessionID), zap.Error(err))
	}
}

// ExtractToken 从 Authorization header 取 Bearer token
//
// # ⚠️ M105(2026-10-28):删掉了 `?access_token=` 兜底
//
// 原来这里有一句"兼容 query(仅供调试)":
//
//	if q := string(c.Query("access_token")); q != "" { return q }
//
// 它挂在**所有受保护路由**的鉴权路径上,不是"调试开关":
//
//   - token 进了 URL → 进 nginx access log、进浏览器历史、进 Referer
//     (跳到第三方站点时 Referer 会带上完整 URL);
//   - 本项目自己的 SSE 端点**根本不需要它** —— SSEAuth 有自己的三级取值
//     (Authorization → ?access_token= → cookie),见 sseauth.go;
//     也就是说这条兜底的实际受益者是"手工拿 URL 调接口"的人,
//     代价是全站多一个 token 泄漏面。
//
// 文档 `docs/事件推送框架层设计方案V1.0.md` 也把 `?access_token=`
// 明确写成 **SSE 专属**的兜底手段,不是通用鉴权方式。
func ExtractToken(c *app.RequestContext) string {
	h := string(c.GetHeader("Authorization"))
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

func isPublicPath(p string) bool {
	for _, pp := range PublicPaths {
		if strings.HasPrefix(p, pp) {
			return true
		}
	}
	return false
}
