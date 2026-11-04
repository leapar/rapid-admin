// Phase 3.3 — ResourceAssertor 单测
// 文档:后台权限模型设计V1.0.md §8.4
//
// 覆盖决策流程的所有 6 个分支 + 边界:
//
//	1. caller=nil → Internal
//	2. super_admin(caller 路径)→ 允许
//	3. super_admin(principal 路径)→ 允许
//	4. 资源所有人 → 允许
//	5. 资源无所有人 → 不应触发 owner 路径
//	6. 显式 deny override 优先于 grant
//	7. deny 已过期 → 不应生效
//	8. grant 精确匹配:resourceID 一致
//	9. grant ResourceID 不匹配 → 拒绝
//	10. grant ResourceID=0(通配)→ 允许
//	11. grant ResourceType 不匹配 → 拒绝
//	12. 已过期 grant → 不应生效
//	13. principal=nil + 非 owner → 拒绝
//	14. principal=nil + 是 owner → 允许
//	15-19. 便捷构造器 / String / 内部 matchResourceGrant / CallerSummary
package authz

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/errcode"
)

// errIs 判断 err 是否为 errcode code(避免 import service 包,打破 import cycle)
func errIs(err error, code int) bool {
	var e *errcode.Error
	return errors.As(err, &e) && e.Code == code
}

// caller 简化的 caller
func mkCaller(userID uint64, role string) *CallerSummary {
	return &CallerSummary{UserID: userID, RoleCode: role}
}

// principal 简化 principal
func mkPrincipal(userID, identityID uint64, role string, grants []GrantRef, overrides []Override) *Principal {
	p := &Principal{
		UserID:       userID,
		IdentityID:   identityID,
		RoleCode:     role,
		FeatureFlags: map[string]bool{},
	}
	if grants != nil {
		p.ActiveGrants = grants
	}
	if overrides != nil {
		p.Overrides = overrides
	}
	return p
}

// 1. caller=nil → Internal
func TestAssertCan_NilCaller(t *testing.T) {
	a := NewGenericAssertor()
	err := a.AssertCan(nil, nil, HouseResource(1, 100), "house:view")
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.Internal))
}

// 2. super_admin(caller 路径)→ 允许
func TestAssertCan_SuperAdminByCaller(t *testing.T) {
	a := NewGenericAssertor()
	err := a.AssertCan(mkCaller(999, "role_super_admin"), nil, HouseResource(1, 100), "house:view")
	assert.NoError(t, err)
}

// 3. super_admin(principal 路径)→ 允许(身份切换后 RoleCode 已变)
func TestAssertCan_SuperAdminByPrincipal(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(999, "role_user_admin")                  // caller 不是 super
	principal := mkPrincipal(999, 1, "role_super_admin", nil, nil) // principal 是
	err := a.AssertCan(caller, principal, HouseResource(1, 100), "house:view")
	assert.NoError(t, err)
}

// 4. 资源所有人 → 允许
func TestAssertCan_OwnerSelfAccess(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	resource := HouseResource(1, 100) // owner_user_id=100
	err := a.AssertCan(caller, nil, resource, "house:view")
	assert.NoError(t, err, "资源所有人可访问自己的资源")
}

// 5. 资源无所有人(OwnerUserID=0)+ 平台共管 → 不应触发 owner 路径
func TestAssertCan_NoOwnerNotMatched(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	resource := HouseResource(1, 0) // 无 owner(平台共管)
	err := a.AssertCan(caller, nil, resource, "house:view")
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.Forbidden))
}

// 6. 显式 deny override 优先于 grant
func TestAssertCan_DenyOverrideWinsOverGrant(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	exp := time.Now().Add(time.Hour)
	principal := mkPrincipal(100, 1, "role_user_admin",
		[]GrantRef{
			{GrantID: 1, ResourceType: "house", ResourceID: 1, Permissions: []string{"house:view"}},
		},
		[]Override{
			{PermissionCode: "house:view", Effect: "deny", ExpiresAt: &exp},
		},
	)
	err := a.AssertCan(caller, principal, HouseResource(1, 999), "house:view")
	require.Error(t, err, "deny 覆盖 grant")
	assert.True(t, errIs(err, errcode.Forbidden))
}

// 7. deny 已过期 → 不应生效
func TestAssertCan_ExpiredDenyIgnored(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	exp := time.Now().Add(-time.Hour) // 已过期
	principal := mkPrincipal(100, 1, "role_user_admin",
		[]GrantRef{
			{GrantID: 1, ResourceType: "house", ResourceID: 1, Permissions: []string{"house:view"}},
		},
		[]Override{
			{PermissionCode: "house:view", Effect: "deny", ExpiresAt: &exp},
		},
	)
	err := a.AssertCan(caller, principal, HouseResource(1, 999), "house:view")
	assert.NoError(t, err, "过期 deny 不应生效")
}

// 8. grant 精确匹配:resourceID 一致
func TestAssertCan_GrantExactResourceID(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	exp := time.Now().Add(time.Hour)
	principal := mkPrincipal(100, 1, "role_user_admin",
		[]GrantRef{
			{
				GrantID: 1, ResourceType: "house", ResourceID: 100,
				Permissions: []string{"house:view"}, ExpiresAt: &exp,
			},
		}, nil,
	)
	err := a.AssertCan(caller, principal, HouseResource(100, 999), "house:view")
	assert.NoError(t, err)
}

// 9. grant ResourceID 不匹配(grant 是 100,resource 是 200)→ 拒绝
func TestAssertCan_GrantResourceIDMismatch(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	exp := time.Now().Add(time.Hour)
	principal := mkPrincipal(100, 1, "role_user_admin",
		[]GrantRef{
			{
				GrantID: 1, ResourceType: "house", ResourceID: 100,
				Permissions: []string{"house:view"}, ExpiresAt: &exp,
			},
		}, nil,
	)
	err := a.AssertCan(caller, principal, HouseResource(200, 999), "house:view")
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.Forbidden))
}

// 10. grant ResourceID=0(通配该 type 全部)→ 允许
func TestAssertCan_GrantWildcardResourceID(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	exp := time.Now().Add(time.Hour)
	principal := mkPrincipal(100, 1, "role_user_admin",
		[]GrantRef{
			{
				GrantID: 1, ResourceType: "house", ResourceID: 0, // 0 = 全部
				Permissions: []string{"house:*"}, ExpiresAt: &exp,
			},
		}, nil,
	)
	err := a.AssertCan(caller, principal, HouseResource(999, 888), "house:edit")
	assert.NoError(t, err, "ResourceID=0 + 权限通配 → 通配该 type 全部")
}

// 11. grant ResourceType 不匹配(grant 是 house,resource 是 owner)→ 拒绝
func TestAssertCan_GrantResourceTypeMismatch(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	exp := time.Now().Add(time.Hour)
	principal := mkPrincipal(100, 1, "role_user_admin",
		[]GrantRef{
			{
				GrantID: 1, ResourceType: "house", ResourceID: 0,
				Permissions: []string{"owner:view"}, ExpiresAt: &exp,
			},
		}, nil,
	)
	err := a.AssertCan(caller, principal, OwnerResource(1, 999), "owner:view")
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.Forbidden))
}

// 12. 已过期 grant → 不应生效
func TestAssertCan_ExpiredGrantIgnored(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	exp := time.Now().Add(-time.Hour) // 已过期
	principal := mkPrincipal(100, 1, "role_user_admin",
		[]GrantRef{
			{
				GrantID: 1, ResourceType: "house", ResourceID: 0,
				Permissions: []string{"house:view"}, ExpiresAt: &exp,
			},
		}, nil,
	)
	err := a.AssertCan(caller, principal, HouseResource(1, 999), "house:view")
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.Forbidden))
}

// 13. principal=nil(loader 跳过场景)+ 非 owner → 拒绝
func TestAssertCan_NilPrincipalNoOwnerNoGrant(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	err := a.AssertCan(caller, nil, HouseResource(1, 999), "house:view")
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.Forbidden))
}

// 14. principal=nil + 是 owner → 允许(走 owner 路径,不依赖 principal)
func TestAssertCan_NilPrincipalOwnerOK(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	err := a.AssertCan(caller, nil, HouseResource(1, 100), "house:view")
	assert.NoError(t, err)
}

// 15. 多种 resource 类型的便捷构造器
func TestResourceBuilders(t *testing.T) {
	assert.Equal(t, "house", HouseResource(1, 100).ResourceType)
	assert.Equal(t, "owner", OwnerResource(1, 100).ResourceType)
	assert.Equal(t, "asset", AssetResource(1, 100).ResourceType)
	assert.Equal(t, "delivery", DeliveryResource(1, 100).ResourceType)
}

// 16. Resource.String() 含全部字段
func TestResourceString(t *testing.T) {
	r := HouseResource(42, 100)
	s := r.String()
	assert.Contains(t, s, "house")
	assert.Contains(t, s, "42")
	assert.Contains(t, s, "100")
}

// 17. matchResourceGrant 内部:多 grant 中首个命中即返回
func TestMatchResourceGrant_FirstHit(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	p := mkPrincipal(100, 1, "role_user_admin",
		[]GrantRef{
			{GrantID: 1, ResourceType: "house", ResourceID: 999, Permissions: []string{"user:view"}, ExpiresAt: &exp},
			{GrantID: 2, ResourceType: "house", ResourceID: 0, Permissions: []string{"house:view"}, ExpiresAt: &exp},
			{GrantID: 3, ResourceType: "house", ResourceID: 0, Permissions: []string{"house:edit"}, ExpiresAt: &exp},
		}, nil,
	)
	hit, gid := matchResourceGrant(p, HouseResource(5, 888), "house:view")
	assert.True(t, hit)
	assert.Equal(t, uint64(2), gid, "首个命中 grant_id")
}

// 18. CallerSummary.IsSuperAdmin
func TestCallerSummary_IsSuperAdmin(t *testing.T) {
	assert.True(t, mkCaller(1, "role_super_admin").IsSuperAdmin())
	assert.False(t, mkCaller(1, "role_user_admin").IsSuperAdmin())
	assert.False(t, (*CallerSummary)(nil).IsSuperAdmin())
}

// 19. 多个 grant 都命中 — 取首个(用于审计:按时间/grantID 稳定)
func TestMatchResourceGrant_MultipleAllMatch(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	p := mkPrincipal(100, 1, "role_user_admin",
		[]GrantRef{
			{GrantID: 100, ResourceType: "house", ResourceID: 0, Permissions: []string{"house:*"}, ExpiresAt: &exp},
			{GrantID: 200, ResourceType: "house", ResourceID: 0, Permissions: []string{"house:view"}, ExpiresAt: &exp},
		}, nil,
	)
	_, gid := matchResourceGrant(p, HouseResource(5, 888), "house:view")
	assert.Equal(t, uint64(100), gid, "通配在前先命中")
}

// 20. deny > owner:用户是资源所有人且被显式 deny → 必须拒
// (deny 判定排在 owner 直通之前;曾经 owner 直通在前,显式 deny 对 owner 失效)
func TestAssertCan_DenyOverrideBeatsOwner(t *testing.T) {
	a := NewGenericAssertor()
	caller := mkCaller(100, "role_user_admin")
	exp := time.Now().Add(time.Hour)
	principal := mkPrincipal(100, 1, "role_user_admin", nil,
		[]Override{
			{PermissionCode: "house:view", Effect: "deny", ExpiresAt: &exp},
		},
	)
	// resource.owner = 100 = caller.UserID:旧实现会在这里直通放行
	err := a.AssertCan(caller, principal, HouseResource(1, 100), "house:view")
	require.Error(t, err, "显式 deny 必须拦住资源所有人")
	assert.True(t, errIs(err, errcode.Forbidden))
}
