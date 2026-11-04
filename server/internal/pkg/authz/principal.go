// Phase 2.4 — Principal 主体(应用层鉴权上下文)
// 文档:后台权限模型设计V1.0.md §7
//
// Principal 是 L0.5 中间件从 DB / cache 加载的「当前用户鉴权画像」,
// 供 L1 RBAC 中间件 / L3 assertor 使用,补齐 casbin_rule 表达不了的几类场景:
//   - ActiveGrants: 资源级授权(场景 B/C)
//   - Overrides:    单点 deny/allow(场景 F)
//   - FeatureFlags: 个人功能开关(场景 E)
//
// 与 Casbin 的边界:casbin_rule 不动,本结构是「应用层附加」,loader 从 DB 拉
//
// 与 service 包的关系:本包不导入 service(避免 cmd/migrate → repository → authz → service → loginlock
// → repository 的 import cycle)。FromCallerBase 接收零散字段,由调用方从 service.Caller 拆解后传入。
package authz

import (
	"context"
	"time"
)

type principalKey struct{}

// WithPrincipal 把 Principal 注入 ctx(供下游 RBAC / service / assertor 取)
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFromContext 从 ctx 取 Principal(无则返回 nil,false)
// 公开路径可能没挂 principal(public 路由不经过 L0.5),调用方需判 nil
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	return p, ok
}

// Principal 当前用户的鉴权画像(由 PrincipalLoader 加载,L0.5 中间件注入 ctx)
type Principal struct {
	// 基础身份
	UserID     uint64
	IdentityID uint64
	RoleCode   string
	ProjectIDs []uint64

	// 应用层附加
	ActiveGrants []GrantRef  // 当前对我有效的授权
	Overrides    []Override  // deny/allow 覆盖
	FeatureFlags map[string]bool

	// 元
	LoadedAt time.Time // 加载时间(用于判断 TTL)
	CacheHit bool      // 是否命中缓存
}

// CallerFields 装载 Principal 基础身份所需字段(避免 import service 包)
// 调用方从 service.Caller 拆出来传入。
type CallerFields struct {
	UserID     uint64
	IdentityID uint64
	RoleCode   string
	ProjectIDs []uint64
}

// FromCallerBase 用 CallerFields 构造 Principal 基础字段
//   - ActiveGrants / Overrides / FeatureFlags 由 loader 填充
func FromCallerBase(c CallerFields) *Principal {
	return &Principal{
		UserID:       c.UserID,
		IdentityID:   c.IdentityID,
		RoleCode:     c.RoleCode,
		ProjectIDs:   c.ProjectIDs,
		LoadedAt:     time.Now(),
		FeatureFlags: map[string]bool{},
	}
}

// IsSuperAdmin 快速判定
func (p *Principal) IsSuperAdmin() bool {
	return p != nil && p.RoleCode == "role_super_admin"
}

// InProject 复用 caller 逻辑(避免重复代码)
//
// ⚠️⚠️ fail-open 警告(2026-11-03 审计):「ProjectIDs 为空 = 放行所有项目」
// 本意是"未配置项目边界 = 不限制",但**当前 loader 并不填充 ProjectIDs**
// ⇒ ProjectIDs 恒为空 ⇒ 本函数恒放行。也就是说:接线项目级鉴权之前,
// 这个判定形同虚设 —— 必须先补 loader 把用户的项目身份真正灌进来,
// 否则任何依赖 InProject 的调用方都会以为有项目隔离而实际没有。
func (p *Principal) InProject(projectID uint64) bool {
	if p == nil {
		return false
	}
	if projectID == 0 {
		return true
	}
	if len(p.ProjectIDs) == 0 {
		return true
	}
	for _, pid := range p.ProjectIDs {
		if pid == projectID || pid == 0 {
			return true
		}
	}
	return false
}

// HasFeature 判断功能开关
func (p *Principal) HasFeature(key string) bool {
	if p == nil {
		return false
	}
	return p.FeatureFlags[key]
}

// HasDenyOverride 检查是否被显式 deny(场景 F 优先级)
func (p *Principal) HasDenyOverride(permCode string) bool {
	if p == nil {
		return false
	}
	for _, o := range p.Overrides {
		if o.PermissionCode == permCode {
			if o.Effect == "deny" {
				if o.ExpiresAt == nil || o.ExpiresAt.After(time.Now()) {
					return true
				}
			}
		}
	}
	return false
}

// GrantRef 资源级授权引用
type GrantRef struct {
	GrantID      uint64
	GrantType    string
	GranterUserID uint64
	ResourceType string
	ResourceID   uint64 // 0 = 全部
	ScopeProjectID uint64
	Permissions  []string
	ExpiresAt    *time.Time
}

// Active 是否当前有效(未过期)
func (g GrantRef) Active() bool {
	if g.ExpiresAt == nil {
		return true
	}
	return g.ExpiresAt.After(time.Now())
}

// Override 单点覆盖(allow / deny)
type Override struct {
	PermissionCode string
	Effect         string // "allow" | "deny"
	ExpiresAt      *time.Time
}
