// Principal 中间件 —— L0.5 加载应用层鉴权画像
// 文档:后台权限模型设计V1.0.md §8.1
//
// 位置: Auth(L0) → Principal(L0.5) → RBAC(L1) → ProjectScope(L2) → Handler
// 职责:
//   - 从 ctx.caller 拿 (UserID, IdentityID) → 调 loader.Load(...)
//   - 注入 ctx.principal(失败/无 caller 走降级:warn + 跳过,RBAC 退化为只做 role/user 判定)
//   - 公开路由(public/auth/login)不进 L0.5,principal = nil
//
// 失败降级策略(为何不直接 500):
//   - DB 抖动时全平台 500 比「grant 检查退化为纯 role/user」更糟
//   - L1 RBAC 仍能保护(角色策略 + Casbin user: deny override 够用)
//   - grant 类授权(场景 B/C/D)在缓存恢复前会暂时失效,降级而非熔断
package middleware

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/service"
)

// Principal 构造 L0.5 中间件(由 router.Register 在启动时创建并装载)
//
// loader 一般是 CachedLoader(Redis + singleflight);允许传 nil(完全跳过 L0.5)。
// router 在没有 Redis 的本地 dev 模式可传 nil,所有路由退化为 L0 + L1 纯 casbin 路径。
func Principal(loader authz.Loader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		// 公开路径(已被 Auth 短路)不会有 caller,跳过 principal 加载
		caller, ok := service.FromContext(ctx)
		if !ok || caller == nil {
			c.Next(ctx)
			return
		}
		// 显式禁用 loader(本地 dev)→ 跳过
		if loader == nil {
			c.Next(ctx)
			return
		}
		// 给 loader 留 1.5s 上限,超过就降级(避免慢 DB 把请求拖死)
		loadCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		p, err := loader.Load(loadCtx, caller.UserID, caller.IdentityID)
		if err != nil {
			// 降级:warn 后放行,RBAC 走纯 role/user 路径
			zap.L().Warn("principal loader failed, degraded to role/user only",
				zap.Uint64("user_id", caller.UserID),
				zap.Uint64("identity_id", caller.IdentityID),
				zap.String("err", err.Error()),
			)
			c.Next(ctx)
			return
		}
		if p == nil {
			// 确认没有这个身份(2026-10-25 M90:null 占位命中,或身份确实不存在)。
			//
			// 与"加载失败"同等对待 —— 降级,不挂 principal。区别只在于确定性:
			// 这里不是故障,是"这个身份压根不存在"这个**事实**。
			//
			// 刻意**不**往 ctx 里塞一个 nil principal:那会造出第三种状态
			// ("挂着一个空 principal"),与"根本没挂"混在一起,下游
			// PrincipalFromContext 的 ok 值就不再能说明任何事。
			zap.L().Warn("principal not found, degraded to role/user only",
				zap.Uint64("user_id", caller.UserID),
				zap.Uint64("identity_id", caller.IdentityID),
			)
			c.Next(ctx)
			return
		}
		c.Set(authzPrincipalKey, p)
		c.Next(authz.WithPrincipal(ctx, p))
	}
}

// authzPrincipalKey Hertz RequestContext 上挂 principal 的 key(handler 取)
const authzPrincipalKey = "authz.Principal"

// PrincipalFromRequest 同时从 Hertz ctx 和标准 ctx 取 principal(优先 Hertz)
// 业务 handler 想用 principal 时调这个,避免每次都 import authz
func PrincipalFromRequest(c *app.RequestContext, ctx context.Context) *authz.Principal {
	if v, ok := c.Get(authzPrincipalKey); ok {
		if p, ok2 := v.(*authz.Principal); ok2 {
			return p
		}
	}
	if p, ok := authz.PrincipalFromContext(ctx); ok {
		return p
	}
	return nil
}

// 当前未使用,保留响应式 resp 包风格的对称(后续中间件复用)
var _ = response.Fail
var _ = errcode.Internal
