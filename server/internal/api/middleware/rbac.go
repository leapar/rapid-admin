// RBAC 资源权限 + 项目域 + IDOR 3 层防御
// 文档 §5.2 / §9.4 / 后台权限模型设计V1.0.md §8
package middleware

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/casbinx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/service"
)

// ⚠️ M147:权限身份 = **完整资源码**(module + ":" + action)
//
//	RBAC("user", "list")  →  r.obj = "user:list"
//
// 这个口径跟三处保持一致(缺一处就会出现"同一个请求两套判法"):
//
//	· 策略行的 obj(seed.buildRolePolicies 写的是资源码)
//	· grant 路径的 wantPerm(authz.MatchPermissionCode 匹配的就是资源码)
//	· 前端的权限码(web/src/constants/permissions.ts)
//
// AccessDecision 鉴权结果(纯函数输出,便于单测)
type AccessDecision int

const (
	AccessAllow       AccessDecision = iota // 通过
	AccessDenyForbidden                     // 403 业务无权限
	AccessDenyOutOfScope                    // 403 数据域越界
	AccessDenyInternal                      // 500 内部错误(casbin 异常)
)

// RBAC 资源权限校验
// 资源路径 = "<module>:<action>", 例如 "user:create"
// 从 URL/Query 取 projectId 验证数据级权限
//
// 鉴权语义(Phase 2.5 — grant 路径已接入):
//   1. super_admin 短路
//   2. 角色 / 用户覆盖 两路 EnforceEx(role: / user: sub)
//   3. 委托授权 grant 路径(Phase 2.5):遍历 principal.ActiveGrants,
//      若某 grant 的 permissions 命中 module:action 且 scope 匹配 → 放行
//   4. 数据级 projectID 域校验
//
// Deny override(§5.2 user_permission_override 表):
//   - role/user 子:由 Casbin 的 eft=deny 策略承担
//   - grant 子:目前是「任何 active grant 都允许」,deny 显式覆盖
//     留待 Phase 3 抽到 assertor 层做(grant 层 deny 较少见,先满足 allow 路径)
func RBAC(module, action string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		caller, ok := service.FromContext(ctx)
		if !ok || caller == nil {
			response.Fail(c, errcode.Unauthorized, "未登录")
			c.Abort()
			return
		}
		// 数据级 projectId 域校验
		projectID := extractProjectID(c)
		// L0.5 注入的 principal(可能为 nil,公开路径 / dev 模式 loader=nil)
		principal, _ := authz.PrincipalFromContext(ctx)
		decision := decideAccess(caller, principal, module, action, projectID)

		switch decision {
		case AccessAllow:
			// ⚠️ **业务域写进 ctx** —— 数据范围(行级权限,M155)按业务域配,
			// 而业务域的权威定义就是 **rbac() 的第一个参数**。
			//
			// 为什么不从资源码反推:code = module + ":" + action,
			// 而 module 与 action **都可能含冒号**
			// (rbac("audit:operation", "list") / rbac("job", "run:read"))
			// ⇒ 反推出来的 module 与运行时注入的对不上,
			// 规则就会静默失配 ⇒ 退化成不过滤 ⇒ 越权数据被看到。
			// 详见 internal/pkg/scope/module.go。
			//
			// 为什么由这一层写:**判定权限的这一层拿到的就是那个字面量**。
			// 让 service 自己在 Apply 时写一遍 "user",那就是两处拼同一个字符串
			// —— 迟早有一处改了另一处没改。
			//
			// ⚠️ **M163:同一份业务域再写进 Hertz 的 KV** ——
			// 字段级可见性在**响应出口**生效,而那个出口(BaseHandler.OK)
			// 只有 RequestContext,读不到上面那个派生 ctx。
			// ⇒ 两处写的是同一个字符串(都取自这个字面量),不会分叉。
			c.Set(scope.ModuleKey, module)
			c.Next(scope.WithModule(ctx, module))
		case AccessDenyOutOfScope:
			response.Fail(c, errcode.OutOfScope, "超出工作范围")
			c.Abort()
		case AccessDenyForbidden:
			response.Fail(c, errcode.Forbidden, "没有权限")
			c.Abort()
		default: // AccessDenyInternal
			response.Fail(c, errcode.Internal, "权限校验失败")
			c.Abort()
		}
	}
}

// decideAccess 纯鉴权决策函数(HTTP 无关,便于单测)
//   - super_admin 直通
//   - 否则:角色 / 用户两路 EnforceEx,合并规则:
//       1) 任何子含 deny 规则 → Forbidden(deny 优先,跨子生效)
//       2) 否则任一子 allow → 走数据级 project 校验
//       3) 都无 allow → grant 子查(Phase 2.5)
//       4) grant 也无 → Forbidden
//   - 数据级 projectID 不在工作范围 → OutOfScope
//   - casbin 异常 → Internal
//
// 设计取舍:为什么不用单一 sub(user:)+ grouping 让 Casbin 内部处理 deny?
//   - grouping g(user, role, dom) 需要在身份创建/登录时注入,改 seed/auth 流程
//   - 显式 deny 也需要单独 sub(user:),否则 deny 不会和 role: 在同一 sub 里 evaluate
//   - 两路 OR + 显式 deny 短路语义更直观,审计日志可同时看到 role/user 命中规则
func decideAccess(caller *service.Caller, principal *authz.Principal, module, action string, projectID uint64) AccessDecision {
	return decideAccessOpt(caller, principal, module, action, projectID, true)
}

// decideAccessOpt decideAccess + 「拒绝时要不要记日志」开关
//
// ⚠️ **判定逻辑一个字都没变** —— logDeny 只控制那两条 zap.L().Warn。
// (两套实现分叉是本仓明确记过的失败模式,所以这里是同一个函数体加一个开关,不是另写一份。)
//
// ⚠️ 为什么需要这个开关:看板这类「批量探测能不能看」的场景,
// 一次页面加载要问 6 个业务域,每个没权限的域都打一条 WARN
// ⇒ 日志被"权限不足"刷爆,真出问题时反而看不见。
func decideAccessOpt(caller *service.Caller, principal *authz.Principal, module, action string, projectID uint64, logDeny bool) AccessDecision {
	if caller == nil {
		return AccessDenyInternal
	}
	// super_admin 直接放行
	if caller.HasRole("role_super_admin") {
		return AccessAllow
	}

	roleSub := "role:" + caller.RoleCode
	userSub := "user:" + strconv.FormatUint(caller.UserID, 10)

	// ⚠️ M147:obj 传**完整资源码**(module + ":" + action),不是模块名。
	//
	// 策略行的 obj 是资源码("user:list"),而这里以前传的是模块名("user")
	// ⇒ keyMatch2("user", "user:list") = false ⇒ **一条绑定行都命中不了**。
	// 通配策略行(`role:X, global, *, *, allow`)把这个洞盖住了 ——
	// 删掉通配行(M147)之后,粒度必须同时对齐,否则从"人人能访问"变成"人人访问不了"。
	//
	// 这跟 grant 路径本来就在用的口径**本来就是一套**
	// (matchGrant 里的 `module + ":" + action`),之前是两条路径各用各的:
	// 同一个请求,走 casbin 和走 grant 会判出两个结果。
	code := module + ":" + action

	enf := casbinx.Enforcer()
	if enf == nil {
		return AccessDenyInternal
	}
	okRole, rulesRole, err := enf.EnforceEx(roleSub, "global", code, action)
	if err != nil {
		return AccessDenyInternal
	}
	okUser, rulesUser, err := enf.EnforceEx(userSub, "global", code, action)
	if err != nil {
		return AccessDenyInternal
	}

	// 1) 任何一路出现 deny 规则 → 一票否决(deny override 跨子生效,§5.2)
	if hasDenyRule(rulesRole) || hasDenyRule(rulesUser) {
		if logDeny {
			zap.L().Warn("rbac denied by deny-rule",
				zap.String("user_id", userSub),
				zap.String("role", roleSub),
				zap.String("module", module),
				zap.String("action", action),
				zap.Strings("role_rules", rulesRole),
				zap.Strings("user_rules", rulesUser),
			)
		}
		return AccessDenyForbidden
	}

	// 2) 无 deny → 任一 allow 通过(role/user 直通)
	if okRole || okUser {
		if projectID > 0 && !caller.InProject(projectID) {
			return AccessDenyOutOfScope
		}
		return AccessAllow
	}

	// 3) role/user 都不 allow → 查 grant 路径(Phase 2.5)
	//    principal 可能为 nil:公开路径 / dev 模式 loader=nil → 直接 Forbidden
	if principal != nil {
		if grantHit, grantID := matchGrant(principal, code, projectID); grantHit {
			zap.L().Info("rbac allowed via grant",
				zap.Uint64("user_id", caller.UserID),
				zap.String("module", module),
				zap.String("action", action),
				zap.Uint64("grant_id", grantID),
			)
			return AccessAllow
		}
	}

	if logDeny {
		zap.L().Warn("rbac denied (no policy match)",
			zap.String("user_id", userSub),
			zap.String("role", roleSub),
			zap.String("module", module),
			zap.String("action", action),
		)
	}
	return AccessDenyForbidden
}

// CanAccess 供**非中间件**场景复用同一套权限判定,返回是否放行。
//
// ⚠️ 这是 decideAccess 的薄包装,**不是第二套实现**。现成用例:
// GET /events(SSE)需要在**建立连接的瞬间**决定"这个人能订阅哪些 topic",
// 而 SSE 不是普通请求 —— 它不经过 RBAC 中间件(否则一个长连接会占着
// 一次鉴权事务),权限必须在 handler 里现判。
//
// ⚠️ 绝不要在这里重新实现一遍判定。decideAccess 里有 super_admin 短路、
// role/user 两路 EnforceEx、deny override、grant 委托 —— 漏掉任何一条
// 就是权限绕过。两套实现分叉是本仓明确记过的失败模式。
//
// 一律把判定折叠成 bool:调用方只关心"能不能",不关心是 Forbidden 还是
// OutOfScope;真要区分的话中间件那条路径已经报过日志了。
func CanAccess(caller *service.Caller, principal *authz.Principal, module, action string, projectID uint64) bool {
	return decideAccess(caller, principal, module, action, projectID) == AccessAllow
}

// CanAccessQuiet 同 CanAccess,但拒绝时**不记 WARN 日志**。
//
// ⚠️ **判定完全一样**(同一个 decideAccessOpt,只是关掉日志),
// 别在这里重写一遍 —— 漏掉 deny override 或 grant 委托就是权限绕过。
//
// ⚠️ **只给「批量探测」场景用**:
// 看板一次页面加载要问 6 个业务域,没权限的域每个都打一条 WARN
// ⇒ 日志被"权限不足"刷爆,真出事的时候反而看不见。
//
// **单次判定(比如 SSE 建连时判一次)继续用 CanAccess** ——
// 那种场景一次拒绝值得一条日志。
func CanAccessQuiet(caller *service.Caller, principal *authz.Principal, module, action string, projectID uint64) bool {
	return decideAccessOpt(caller, principal, module, action, projectID, false) == AccessAllow
}

// matchGrant 检查 principal.ActiveGrants 中是否有授权能覆盖 (code, projectID)
// 返回 (hit, grantID) — 首个命中即返回(便于日志审计)
//
// ⚠️ M147:入参是**完整资源码**(module + ":" + action),不是 (module, action) ——
// 调用方 decideAccess 已经算好了,这里不再重复拼一遍(两处拼同一个字符串,
// 迟早有一处改了另一处没改)。
//
// 匹配规则:
//   - grant 必须 Active(未过期)
//   - scope_project_id 校验:grant 限定了项目且与请求 projectID 不符 → 跳过
//   - permissions 列表里有任一项匹配 code(支持 * 后缀通配)
func matchGrant(p *authz.Principal, code string, projectID uint64) (bool, uint64) {
	wantPerm := code
	for _, g := range p.ActiveGrants {
		if !g.Active() {
			continue
		}
		// project 域过滤:grant 限定到某项目时,请求 projectID 必须命中
		if g.ScopeProjectID > 0 && projectID > 0 && g.ScopeProjectID != projectID {
			continue
		}
		for _, perm := range g.Permissions {
			if matchPermissionCode(perm, wantPerm) {
				return true, g.GrantID
			}
		}
	}
	return false, 0
}

// matchPermissionCode 简化版的 keyMatch2(委托给 authz.MatchPermissionCode)
//   - "*" 匹配所有
//   - "<prefix>:*" 匹配 "<prefix>:任何"
//   - 其余精确匹配
// 共享:ResourceAssertor 也用同一个匹配函数,避免规则分叉
func matchPermissionCode(pattern, target string) bool {
	return authz.MatchPermissionCode(pattern, target)
}

// hasDenyRule 检查 EnforceEx 返回的规则列表里是否含 eft=deny
// Casbin Go v3 的 EnforceEx 返回 []string,每个元素是规则的一个字段
//   - 规则字段: [sub, dom, obj, act, eft]
//   - 整张规则是一个 []string 整体;不是 [][]string
// 调试输出证实:`["user:100", "global", "house:*", "*", "deny"]`
func hasDenyRule(rules []string) bool {
	if len(rules) < 5 {
		return false
	}
	return rules[len(rules)-1] == "deny"
}

// extractProjectID 从 query / path / header 取 projectId
func extractProjectID(c *app.RequestContext) uint64 {
	if v := c.Query("projectId"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	}
	if v := c.GetHeader("X-Current-Project-Id"); string(v) != "" {
		if n, err := strconv.ParseUint(string(v), 10, 64); err == nil {
			return n
		}
	}
	return 0
}
