// authz —— 资源级鉴权原语
//
// ## 这个包解决什么问题
//
// RBAC 回答"这个角色能不能调这个接口"(粗粒度、按路由)。
// 但业务里还有一类问题:"这个业主能不能看**这一套**房子的交付单" ——
// 细到具体资源、具体人。这类判断本包做:
//
//   - Principal:当前用户的鉴权画像(角色 + 项目范围 + 授权 + 覆盖)
//   - Resource:被访问资源的最小信息(类型 + 主键 + 所有人)
//   - GenericAssertor:断言"caller 能不能对 resource 做 permissionCode"
//
// ## 关键点
//
//   - 本包**不 import service 包** —— 用 CallerFields / CallerSummary 承载
//     最小信息,避免框架层反过来依赖业务层
//   - Principal 走 context 流转,中间件加载一次、下游随便读
//   - 权限码支持通配:"*" / "house:*" / "house:view" / "user:delete"
//
// ## 跑法
//
//	go run ./examples/authz
package main

import (
	"context"
	"fmt"

	"rapid-admin/internal/pkg/authz"
)

func main() {
	// ── 一、权限码匹配 ────────────────────────────────────────────
	fmt.Println("=== 权限码匹配 ===")
	cases := [][2]string{
		{"*", "user:delete"},
		{"house:*", "house:view"},
		{"house:*", "house:delete"},
		{"house:view", "house:view"},
		{"house:view", "house:delete"},
	}
	for _, c := range cases {
		fmt.Printf("  pattern=%-12q target=%-14q → %v\n", c[0], c[1], authz.MatchPermissionCode(c[0], c[1]))
	}

	// ── 二、Principal:当前用户的鉴权画像 ──────────────────────────
	fmt.Println()
	fmt.Println("=== Principal ===")
	p := authz.FromCallerBase(authz.CallerFields{
		UserID:     1001,
		IdentityID: 2001,
		RoleCode:   "role_project_manager",
		ProjectIDs: []uint64{7, 9},
	})
	p.ActiveGrants = []authz.GrantRef{{
		GrantID:      5001,
		GrantType:    "project_delivery",
		ResourceType: "house",
		ResourceID:   0, // 0 = 该类型全部
		Permissions:  []string{"house:view", "house:export"},
	}}
	p.Overrides = []authz.Override{{PermissionCode: "house:delete", Effect: "deny"}}
	p.FeatureFlags = map[string]bool{"new_dashboard": true}

	fmt.Printf("  超管?      = %v\n", p.IsSuperAdmin())
	fmt.Printf("  在项目 7?  = %v\n", p.InProject(7))
	fmt.Printf("  在项目 8?  = %v\n", p.InProject(8))
	fmt.Printf("  有 new_dashboard 特性? = %v\n", p.HasFeature("new_dashboard"))
	fmt.Printf("  被 deny 了 house:delete? = %v\n", p.HasDenyOverride("house:delete"))

	// 超管是另一条路
	su := authz.FromCallerBase(authz.CallerFields{RoleCode: "role_super_admin"})
	fmt.Printf("  超管 IsSuperAdmin() = %v\n", su.IsSuperAdmin())

	// ── 三、Resource:被访问资源 ──────────────────────────────────
	fmt.Println()
	fmt.Println("=== Resource ===")
	house := authz.HouseResource(8801, 1001)
	fmt.Printf("  %s\n", house)
	fmt.Printf("  有明确所有人? = %v\n", house.HasOwner())
	asset := authz.AssetResource(9001, 0)
	fmt.Printf("  %s\n", asset)
	fmt.Printf("  有明确所有人? = %v(0 = 平台共管,无所有者概念)\n", asset.HasOwner())

	// ── 四、断言:caller 能不能对这个资源做这个操作 ────────────────
	//
	// ⚠️ 决策顺序是**按优先级短路**的,不是"逐条累加":
	//
	//	1. caller 缺失           → 内部错误
	//	2. 超管                   → 允许
	//	3. 资源所有人 == caller   → 允许(直通,后面的 deny/grant 都不看了)
	//	4. 显式 deny override     → 拒绝(优先级高于 grant)
	//	5. 授权命中(grant)       → 允许
	//	6. 都不命中               → 拒绝
	//
	// 换句话说:**资源所有人直通排在 deny 检查之前** ⇒ 显式 deny 对资源本人无效。
	fmt.Println()
	fmt.Println("=== 资源级断言 ===")
	assertor := authz.NewGenericAssertor()
	caller := &authz.CallerSummary{UserID: 1001, RoleCode: "role_project_manager"}

	// p 的 grant 是 ResourceID=0(通配 house 全部),所以别人的房子也放行
	fmt.Println("grant = house 类型全部(ResourceID=0):")
	for _, perm := range []string{"house:view", "house:export"} {
		err := assertor.AssertCan(caller, p, house, perm)
		fmt.Printf("  %-14s → %s\n", perm, verdict(err))
	}

	// 换成资源级授权:只授权 8801 这一套
	scoped := authz.FromCallerBase(authz.CallerFields{
		UserID:     1001,
		IdentityID: 2001,
		RoleCode:   "role_project_manager",
	})
	scoped.ActiveGrants = []authz.GrantRef{{
		GrantID:      5001,
		GrantType:    "project_delivery",
		ResourceType: "house",
		ResourceID:   8801, // 只这一套
		Permissions:  []string{"house:view"},
	}}
	// house 是别人的(owner=2002),caller 不是 owner ⇒ 只能靠 grant
	otherHouse := authz.HouseResource(8801, 2002)
	ungranted := authz.HouseResource(8802, 2002)

	fmt.Println()
	fmt.Println("grant = 只授权 house:8801 一套:")
	fmt.Printf("  8801 house:view  → %s\n", verdict(assertor.AssertCan(caller, scoped, otherHouse, "house:view")))
	fmt.Printf("  8802 house:view  → %s\n", verdict(assertor.AssertCan(caller, scoped, ungranted, "house:view")))

	// 显式 deny 优先级高于 grant
	scoped.Overrides = []authz.Override{{PermissionCode: "house:view", Effect: "deny"}}
	fmt.Println()
	fmt.Println("加上显式 deny house:view 后(deny 优先于 grant):")
	fmt.Printf("  8801 house:view  → %s\n", verdict(assertor.AssertCan(caller, scoped, otherHouse, "house:view")))

	// 资源所有人直通
	fmt.Println()
	fmt.Println("资源所有人直通:")
	fmt.Printf("  自己名下的 8801 → %s\n", verdict(assertor.AssertCan(caller, scoped, house, "house:view")))

	// 超管短路
	fmt.Println()
	suCaller := &authz.CallerSummary{UserID: 999, RoleCode: "role_super_admin"}
	fmt.Printf("  超管访问任意资源 → %s\n", verdict(assertor.AssertCan(suCaller, nil, ungranted, "house:delete")))

	// ── 五、Principal 走 context 流转 ─────────────────────────────
	fmt.Println()
	fmt.Println("=== ctx 流转 ===")
	ctx := authz.WithPrincipal(context.Background(), p)
	got, ok := authz.PrincipalFromContext(ctx)
	fmt.Printf("  从 ctx 取到 Principal = %v(uid=%d)\n", ok, got.UserID)

	_, ok = authz.PrincipalFromContext(context.Background())
	fmt.Printf("  空 ctx 取 Principal    = %v\n", ok)
}

// verdict 把断言结果翻成一句话
func verdict(err error) string {
	if err == nil {
		return "允许"
	}
	return "拒绝:" + err.Error()
}
