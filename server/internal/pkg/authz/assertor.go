// Phase 3.1 — ResourceAssertor 通用接口 + GenericAssertor 实现
// 文档:后台权限模型设计V1.0.md §8.4
//
// 用途:资源级访问控制(L3,在 L1 RBAC 通过后进一步判断"该 caller 对该资源 X 是否有权")
//
// 调用模式: 每个 service 在获取资源后第一件事调 assertor.AssertCan(...)
//   func (s *HouseService) Get(ctx, id) (*House, error) {
//       h := s.repo.FindByID(ctx, id)
//       caller, _ := service.FromContext(ctx)
//       principal, _ := authz.PrincipalFromContext(ctx)
//       if err := s.assertor.AssertCan(caller, principal, authz.HouseResource(h.ID, h.OwnerID), "house:view"); err != nil {
//           return nil, err
//       }
//       return h, nil
//   }
//
// 决策流程(按顺序短路):
//   1. caller == nil              → Internal(中间件已挡住,这里防御)
//   2. caller.IsSuperAdmin()      → Allow
//   3. principal.HasDenyOverride(perm)       → Forbidden(显式 deny,场景 F 优先级)
//   4. resource.OwnerUserID == caller.UserID → Allow(资源所有人)
//   5. principal.ActiveGrants 命中
//      (ResourceType 匹配 && ResourceID 匹配 [0=全部,=resource.ID] && Permissions 含 perm)
//      → Allow
//   6. 其他 → Forbidden
//
// 优先级口径:deny > owner > grant。deny 必须排在 owner 直通之前 ——
// 否则"是资源所有人"会绕过显式 deny,风控封禁(场景 F)对 owner 失效。
//
// 设计取舍: 为什么不让 L1 RBAC 直接覆盖?
//   - L1 RBAC 是「接口级」,只到"你能调这个 API",不知道具体资源
//   - L3 assertor 是「资源级」,需要"这条记录属于谁"的信息
//   - 双层防御:L1 挡住「无权限的人」,L3 挡住「有权限但不是这条资源的人」(IDOR)
//
// 不依赖 service 包:本包不导入 service(避免 cmd/migrate → repository → authz → service →
// loginlock → repository 的 import cycle)。Caller 字段由调用方从 service.Caller 拆解传入。
package authz

import (
	"fmt"

	"rapid-admin/internal/pkg/errcode"
)

// Resource 资源级断言的最小信息(三元组足够)
//
//	ResourceType: "house" / "owner" / "asset" / "delivery" / ...
//	              与 casbin obj + permission_grant.resource_type 保持一致
//	ResourceID:   资源主键;0 视为「该类型全部」(与 grant.ResourceID=0 语义对齐)
//	OwnerUserID:  资源所有人;0 = 平台共管(无所有者概念,如 sys_user / sys_role)
type Resource struct {
	ResourceType string
	ResourceID   uint64
	OwnerUserID  uint64
}

// String 便于日志/审计
func (r Resource) String() string {
	return fmt.Sprintf("%s:%d(owner=%d)", r.ResourceType, r.ResourceID, r.OwnerUserID)
}

// HasOwner 资源是否有「所有人」概念(OwnerUserID > 0)
func (r Resource) HasOwner() bool { return r.OwnerUserID > 0 }

// CallerSummary 资源级断言所需 caller 最小信息(避免 import service 包)
type CallerSummary struct {
	UserID   uint64
	RoleCode string
}

// IsSuperAdmin 同 service.Caller.HasRole("role_super_admin")
func (c *CallerSummary) IsSuperAdmin() bool {
	return c != nil && c.RoleCode == "role_super_admin"
}

// ResourceAssertor 资源级访问控制接口
//
// 实现:GenericAssertor(默认,纯内存,不依赖外部);可换实现如带审计日志的 Assertor
type ResourceAssertor interface {
	// AssertCan 检查 caller 是否可对 resource 执行 permissionCode 代表的操作
	// 返回 nil = 允许;非 nil = 拒绝(带 errcode)
	AssertCan(caller *CallerSummary, principal *Principal, resource Resource, permissionCode string) error
}

// GenericAssertor 默认实现 — 无外部依赖
// 决策流程见包头注释
type GenericAssertor struct{}

// NewGenericAssertor 构造
func NewGenericAssertor() *GenericAssertor { return &GenericAssertor{} }

// AssertCan 实现 ResourceAssertor
func (a *GenericAssertor) AssertCan(caller *CallerSummary, principal *Principal, resource Resource, permissionCode string) error {
	// 1. caller 必填
	if caller == nil {
		return errcode.New(errcode.Internal, "assertor: caller 缺失")
	}

	// 2. super_admin 短路
	if caller.IsSuperAdmin() {
		return nil
	}
	// 也兼容 principal 加载了的情况(身份切换后 RoleCode 可能已变)
	if principal != nil && principal.IsSuperAdmin() {
		return nil
	}

	// 3. 显式 deny override 最高优先(场景 F 风控拒绝)
	//    必须排在 owner 直通之前:deny > owner > grant,
	//    否则资源所有人用自己的资源绕过显式 deny。
	if principal != nil && principal.HasDenyOverride(permissionCode) {
		return errcode.New(errcode.Forbidden,
			fmt.Sprintf("显式拒绝 %s", permissionCode))
	}

	// 4. 资源所有人直通(只对「有所有者概念」的资源生效)
	if resource.HasOwner() && resource.OwnerUserID == caller.UserID {
		return nil
	}

	// 5. grant 路径(场景 B/C/D 资源级授权)
	if principal != nil {
		if hit, grantID := matchResourceGrant(principal, resource, permissionCode); hit {
			_ = grantID // 后续可挂审计日志
			return nil
		}
	}

	// 6. 全部不命中 → 拒绝
	return errcode.New(errcode.Forbidden,
		fmt.Sprintf("无权访问 %s", resource.String()))
}

// matchResourceGrant 检查 principal.ActiveGrants 是否有授权覆盖 (resource, permissionCode)
// 与 rbac.go.matchGrant 的区别:这里还要校验 grant.ResourceID 与 resource.ResourceID 匹配
//   - grant.ResourceID == 0 → 通配该 type 全部
//   - grant.ResourceID == resource.ResourceID → 精确匹配
//   - 其他 → 不命中
func matchResourceGrant(p *Principal, resource Resource, permissionCode string) (bool, uint64) {
	for _, g := range p.ActiveGrants {
		if !g.Active() {
			continue
		}
		if g.ResourceType != resource.ResourceType {
			continue
		}
		// ResourceID 匹配:0 通配,= 精确
		if g.ResourceID != 0 && g.ResourceID != resource.ResourceID {
			continue
		}
		for _, perm := range g.Permissions {
			if matchPermissionCode(perm, permissionCode) {
				return true, g.GrantID
			}
		}
	}
	return false, 0
}

// ===== 便捷构造器 =====

// HouseResource 房屋资源快捷构造
func HouseResource(id, ownerUserID uint64) Resource {
	return Resource{ResourceType: "house", ResourceID: id, OwnerUserID: ownerUserID}
}

// OwnerResource 业主档案资源
func OwnerResource(id, ownerUserID uint64) Resource {
	return Resource{ResourceType: "owner", ResourceID: id, OwnerUserID: ownerUserID}
}

// AssetResource 资产资源
func AssetResource(id, ownerUserID uint64) Resource {
	return Resource{ResourceType: "asset", ResourceID: id, OwnerUserID: ownerUserID}
}

// DeliveryResource 交付资源
func DeliveryResource(id, ownerUserID uint64) Resource {
	return Resource{ResourceType: "delivery", ResourceID: id, OwnerUserID: ownerUserID}
}

// 编译期断言:GenericAssertor 实现 ResourceAssertor
var _ ResourceAssertor = (*GenericAssertor)(nil)
