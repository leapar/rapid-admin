// Phase 1.2 + Phase 2.5 单测:鉴权决策纯函数 decideAccess
// 文档:后台权限模型设计V1.0.md §6.2 / §8.2
//
// 覆盖核心场景(避免 P0 类风险再次发生):
//   1. super_admin 直通(无需任何策略)
//   2. 角色层 allow 通过
//   3. 用户层 deny 覆盖角色层 allow(policy_effect)
//   4. 角色+用户都无策略 → Forbidden
//   5. projectID 在工作范围 / 不在工作范围 / 0 放行
//   6. caller=nil → Internal
//   7. grant 路径:principal.ActiveGrants 命中 → 放行
//   8. grant 路径:permissions 通配匹配
//   9. grant 路径:scope_project_id 不匹配 → 跳过该 grant
//  10. grant 路径:无 principal → 走纯 role/user
//  11. M147:生产口径的请求形状(module, action)→ 完整资源码
//
// # ⚠️ M147:请求侧的形状必须是**生产的形状**
//
// 生产里 RBAC("user", "list") → decideAccess(module="user", action="list")
// → r.obj = "user:list"(完整资源码)。
// 本文件原来传的是 ("house:read", "GET") —— 那是**测试自己发明的形状**:
// 生产永远不会这么调(act 是资源动作,不是 HTTP 动词)。
// 形状不一样 ⇒ 测出来的匹配行为跟生产不是一回事(§M103 的教训:
// 测试用被测常量构造输入 = 恒绿)。
package middleware

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/casbinx"
	"rapid-admin/internal/service"
)

// resetEnforcer 把 casbinx 包级 enforcer 替换为 test conf 加载出的实例
// 用 t.Cleanup 在用例结束时把 enforcer 置 nil,避免污染其它包的单测
func resetEnforcer(t *testing.T) *casbin.Enforcer {
	t.Helper()
	confPath, err := filepath.Abs("../../../configs/casbin_rbac.conf")
	require.NoError(t, err)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	adapter, err := gormadapter.NewAdapterByDB(db)
	require.NoError(t, err)

	e, err := casbin.NewEnforcer(confPath, adapter)
	require.NoError(t, err)
	require.NoError(t, e.LoadPolicy())

	// 把 e 注入到包级单例 — 用 casbinx 暴露的 setter 没有,直接打 casbinx.enforcer
	// 走 casbinx.Init 重置 once + 替换
	require.NoError(t, casbinx.Init(db, confPath))
	// 二次 LoadPolicy 保证 autoSave 后内存也是空状态
	require.NoError(t, casbinx.Reload())
	// 拿到 init 后的真实 enforcer
	enf := casbinx.Enforcer()
	require.NotNil(t, enf)

	t.Cleanup(func() { casbinx.ResetForTest() })
	return enf
}

func caller(role string, userID uint64, projectIDs ...uint64) *service.Caller {
	return &service.Caller{
		UserID:     userID,
		RoleCode:   role,
		Audience:   "admin",
		ProjectIDs: projectIDs,
	}
}

// 1. super_admin 直通(无任何策略)
func TestDecideAccess_SuperAdminBypass(t *testing.T) {
	enf := resetEnforcer(t)
	_ = enf

	d := decideAccess(caller("role_super_admin", 1), nil, "user", "create", 0)
	assert.Equal(t, AccessAllow, d)
}

// 2. 角色层 allow 通过
func TestDecideAccess_RoleAllow(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_house_admin", "global", "house:*", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	d := decideAccess(caller("role_house_admin", 100), nil, "house", "read", 0)
	assert.Equal(t, AccessAllow, d)
}

// 3. 用户层 deny 覆盖角色层 allow(关键:P0 风险类,必须阻断)
func TestDecideAccess_UserDenyOverridesRoleAllow(t *testing.T) {
	enf := resetEnforcer(t)
	// 角色允许 house:read
	_, err := enf.AddPolicy("role:role_house_admin", "global", "house:*", "*", "allow")
	require.NoError(t, err)
	// 但用户 100 被显式 deny
	_, err = enf.AddPolicy("user:100", "global", "house:*", "*", "deny")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	d := decideAccess(caller("role_house_admin", 100), nil, "house", "read", 0)
	assert.Equal(t, AccessDenyForbidden, d, "用户层 deny 必须赢 — §5.2 核心保证")
}

// 4. 角色+用户都无策略 + 无 principal → Forbidden
func TestDecideAccess_NoPolicy(t *testing.T) {
	enf := resetEnforcer(t)
	_ = enf

	d := decideAccess(caller("role_user_admin", 999), nil, "user", "delete", 0)
	assert.Equal(t, AccessDenyForbidden, d)
}

// 5a. project 在工作范围 → Allow
func TestDecideAccess_ProjectInScope(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_house_admin", "global", "house:*", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	d := decideAccess(caller("role_house_admin", 100, 10, 20), nil, "house", "read", 20)
	assert.Equal(t, AccessAllow, d)
}

// 5b. project 不在工作范围 → OutOfScope
func TestDecideAccess_ProjectOutOfScope(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_house_admin", "global", "house:*", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	d := decideAccess(caller("role_house_admin", 100, 10, 20), nil, "house", "read", 99)
	assert.Equal(t, AccessDenyOutOfScope, d)
}

// 6. projectID=0(请求无项目域约束)→ 放行,不查 InProject
func TestDecideAccess_ProjectZeroAllowed(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_house_admin", "global", "house:*", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	// 即使 ProjectIDs 也填了,projectID=0 → 不算越界
	d := decideAccess(caller("role_house_admin", 100, 10, 20), nil, "house", "read", 0)
	assert.Equal(t, AccessAllow, d)
}

// 7. caller=nil → Internal(理论上 auth 中间件已拦住,这里做防御)
func TestDecideAccess_NilCaller(t *testing.T) {
	enf := resetEnforcer(t)
	_ = enf

	d := decideAccess(nil, nil, "user", "read", 0)
	assert.Equal(t, AccessDenyInternal, d)
}

// ===== Phase 2.5 — grant 路径 =====

// 8. principal.ActiveGrants 命中精确权限 → Allow
func TestDecideAccess_GrantExactMatch(t *testing.T) {
	enf := resetEnforcer(t)
	_ = enf
	// 角色无任何权限策略

	p := &authz.Principal{
		UserID:     200,
		RoleCode:   "role_user_admin",
		ActiveGrants: []authz.GrantRef{
			{
				GrantID:     789,
				GrantType:   "user_toggle",
				ResourceType: "house",
				ResourceID:  100,
				Permissions: []string{"house:view"},
			},
		},
	}
	d := decideAccess(caller("role_user_admin", 200), p, "house", "view", 0)
	assert.Equal(t, AccessAllow, d, "精确 grant 命中 → 放行")
}

// 9. principal.ActiveGrants 命中通配 → Allow
func TestDecideAccess_GrantWildcardMatch(t *testing.T) {
	enf := resetEnforcer(t)
	_ = enf

	p := &authz.Principal{
		UserID:     200,
		RoleCode:   "role_user_admin",
		ActiveGrants: []authz.GrantRef{
			{
				GrantID:     790,
				GrantType:   "delegation",
				Permissions: []string{"house:*"},
			},
		},
	}
	d := decideAccess(caller("role_user_admin", 200), p, "house", "edit", 0)
	assert.Equal(t, AccessAllow, d, "通配 grant 命中 → 放行")
}

// 10. grant 限定到项目 A,请求 projectID=B → 该 grant 不命中 → Forbidden
func TestDecideAccess_GrantProjectScopeMismatch(t *testing.T) {
	enf := resetEnforcer(t)
	_ = enf

	exp := time.Now().Add(time.Hour)
	p := &authz.Principal{
		UserID:     200,
		RoleCode:   "role_user_admin",
		ActiveGrants: []authz.GrantRef{
			{
				GrantID:        791,
				ResourceType:   "house",
				ScopeProjectID: 10,
				Permissions:    []string{"house:view"},
				ExpiresAt:      &exp,
			},
		},
	}
	d := decideAccess(caller("role_user_admin", 200), p, "house", "view", 20)
	assert.Equal(t, AccessDenyForbidden, d, "scope 不符 → 该 grant 不命中")
}

// 11. role 策略 + principal 都存在,role allow 优先(grant 不会被显式 deny)
//   验证:doubly layered 决策顺序仍正确
func TestDecideAccess_RoleBeatsGrant(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_house_admin", "global", "house:*", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	p := &authz.Principal{
		UserID:     200,
		RoleCode:   "role_house_admin",
		ActiveGrants: []authz.GrantRef{
			{GrantID: 999, Permissions: []string{"house:view"}},
		},
	}
	d := decideAccess(caller("role_house_admin", 200), p, "house", "view", 0)
	assert.Equal(t, AccessAllow, d)
}

// 12. 已过期 grant 不应命中
func TestDecideAccess_GrantExpired(t *testing.T) {
	enf := resetEnforcer(t)
	_ = enf

	exp := time.Now().Add(-time.Hour)
	p := &authz.Principal{
		UserID:     200,
		RoleCode:   "role_user_admin",
		ActiveGrants: []authz.GrantRef{
			{
				GrantID:     792,
				Permissions: []string{"house:view"},
				ExpiresAt:   &exp, // 已过期
			},
		},
	}
	d := decideAccess(caller("role_user_admin", 200), p, "house", "view", 0)
	assert.Equal(t, AccessDenyForbidden, d, "过期 grant 不应放行")
}

// 13. 多个 grant 中,首个命中即放行(审计:取最早一个)
func TestDecideAccess_GrantFirstHitWins(t *testing.T) {
	enf := resetEnforcer(t)
	_ = enf

	p := &authz.Principal{
		UserID:     200,
		RoleCode:   "role_user_admin",
		ActiveGrants: []authz.GrantRef{
			{GrantID: 1, Permissions: []string{"user:view"}},  // 不命中
			{GrantID: 2, Permissions: []string{"house:view"}}, // 命中
			{GrantID: 3, Permissions: []string{"house:edit"}},
		},
	}
	d := decideAccess(caller("role_user_admin", 200), p, "house", "view", 0)
	assert.Equal(t, AccessAllow, d)
}

// 15. ★ M147 回归线:生产口径的请求形状 → 绑定矩阵真的在强制执行
//
// # 守的是什么
//
// M147 之前,seed 给**每个角色**都加了一条通配策略
// (`role:X, global, *, *, allow`),而 casbin 的 keyMatch2(r.obj, "*") 恒真
// ⇒ **任何角色都能访问任何接口**。
// 前端按"角色-资源绑定矩阵"渲染菜单(别人的菜单少了、按钮少了),
// 后端却谁都能调 —— 矩阵只在"显示"这一层是真的。
//
// 这一条用**生产形状**的请求(module, action)复现那个洞:
// 策略行 obj 是**资源码**,请求 obj 也是资源码 ⇒ 绑了哪个就只放行哪个。
//
// ⚠️ 三种越权分别对应三条断言:
//
//	· 同模块的兄弟 action(user:list ⇒ user:delete 不该过)
//	· 别的模块(config:update)
//	· 完全没绑过的码(activity:create)
func TestDecideAccess_ProductionRequestShape_EnforcesBindingMatrix(t *testing.T) {
	enf := resetEnforcer(t)

	// 策略形状 = 生产:obj 是**完整资源码**,act 是资源动作
	_, err := enf.AddPolicy("role:role_readonly", "global", "user:list", "*", "allow")
	require.NoError(t, err)
	_, err = enf.AddPolicy("role:role_readonly", "global", "user:read", "*", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	t.Run("绑了的码放行", func(t *testing.T) {
		d := decideAccess(caller("role_readonly", 100), nil, "user", "list", 0)
		assert.Equal(t, AccessAllow, d)
	})

	t.Run("同模块的兄弟 action 不串味", func(t *testing.T) {
		// ⚠️ 这一条盯的是 keyMatch2 把 ":" 当 URL 路径参数的后果:
		//   "user:list" 被编译成 ^user[^/]+$ ⇒ user:delete 也会命中。
		d := decideAccess(caller("role_readonly", 100), nil, "user", "delete", 0)
		assert.Equal(t, AccessDenyForbidden, d, "只读员没绑 user:delete ⇒ 必须拒")
	})

	t.Run("别的模块不放行", func(t *testing.T) {
		d := decideAccess(caller("role_readonly", 100), nil, "config", "update", 0)
		assert.Equal(t, AccessDenyForbidden, d, "只读员没绑 config:update ⇒ 必须拒")
	})

	t.Run("完全没绑过的码不放行", func(t *testing.T) {
		d := decideAccess(caller("role_readonly", 100), nil, "activity", "create", 0)
		assert.Equal(t, AccessDenyForbidden, d, "只读员没绑 activity:create ⇒ 必须拒")
	})
}

// 16. ★ M147:grant 路径与 casbin 路径判的是**同一个资源码**
//
// # 为什么这条重要
//
// grant 路径的 wantPerm 一直是 `module + ":" + action`(完整资源码),
// 而 casbin 路径以前传的是模块名 —— 同一个请求在两条路上判出两个结果。
// M147 把 casbin 路径也统一到资源码 ⇒ 两条路必须判出**同一个**结论。
//
// 样本:角色**没有** user:list,但有一条 grant 给了 "user:list"
// ⇒ casbin 路径拒、grant 路径放 ⇒ 整体放行(grant 是显式授权,优先级更高)。
// 反过来:grant 给的是 "user:delete"、请求的是 "user:list"
// ⇒ grant 不命中 ⇒ 整体拒(grant 不会"顺带"放开别的码)。
func TestDecideAccess_GrantPathUsesSameResourceCode(t *testing.T) {
	enf := resetEnforcer(t)
	_ = enf

	t.Run("grant 给的正是请求的那个码 ⇒ 放行", func(t *testing.T) {
		p := &authz.Principal{
			UserID: 200, RoleCode: "role_readonly",
			ActiveGrants: []authz.GrantRef{
				{GrantID: 1, Permissions: []string{"user:list"}},
			},
		}
		d := decideAccess(caller("role_readonly", 200), p, "user", "list", 0)
		assert.Equal(t, AccessAllow, d)
	})

	t.Run("grant 给的是别的码 ⇒ 不放行", func(t *testing.T) {
		p := &authz.Principal{
			UserID: 200, RoleCode: "role_readonly",
			ActiveGrants: []authz.GrantRef{
				{GrantID: 2, Permissions: []string{"user:delete"}},
			},
		}
		d := decideAccess(caller("role_readonly", 200), p, "user", "list", 0)
		assert.Equal(t, AccessDenyForbidden, d, "grant 的 user:delete 不该放开 user:list")
	})
}

// 14. permission 匹配:精确 > 通配
func TestMatchPermissionCode(t *testing.T) {
	cases := []struct {
		pattern, target string
		want            bool
	}{
		{"*", "anything", true},
		{"house:view", "house:view", true},
		{"house:view", "house:edit", false},
		{"house:*", "house:view", true},
		{"house:*", "house:edit", true},
		{"house:*", "user:view", false},
		{"*", "*", true},
		{"house:view", "house:view:detail", false}, // 不应跨 : 边界
	}
	for _, tc := range cases {
		got := matchPermissionCode(tc.pattern, tc.target)
		assert.Equalf(t, tc.want, got, "pattern=%q target=%q", tc.pattern, tc.target)
	}
}

// ===== CanAccessQuiet(2026-11-01)=====
//
// ⚠️ **CanAccessQuiet 只是"不记日志"的变体,判定必须一模一样** ——
//	它是看板"批量探测 6 个业务域"的入口(见 handler/v1/dashboard.go)。
//	判定一旦和 CanAccess 分叉,看板就会显示"接口其实调不了"的内容 ——
//	那正是 M149 那一类问题(显示层与执行层两套真相)。
//
// # 可逆探针
//
//	· 把 CanAccessQuiet 改成 `return true` → 本测试红(无权角色被判成有权)
//	· 把 decideAccessOpt 的 logDeny 反过来 → 本测试仍绿(日志开关不影响判定)

func TestCanAccessQuiet_MatchesCanAccess(t *testing.T) {
	enf := resetEnforcer(t)
	_, err := enf.AddPolicy("role:role_house_admin", "global", "house:*", "*", "allow")
	require.NoError(t, err)
	_, err = enf.AddPolicy("user:100", "global", "owner:list", "*", "deny")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	cases := []struct {
		name    string
		c       *service.Caller
		module  string
		action  string
		project uint64
	}{
		{"角色有权限", caller("role_house_admin", 100), "house", "read", 0},
		{"角色没权限", caller("role_house_admin", 100), "owner", "list", 0},
		{"用户层 deny", caller("role_house_admin", 100), "owner", "list", 0},
		{"超管直通", caller("role_super_admin", 1), "anything", "whatever", 0},
		{"越界项目", caller("role_house_admin", 100, 10), "house", "read", 99},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			normal := CanAccess(tc.c, nil, tc.module, tc.action, tc.project)
			quiet := CanAccessQuiet(tc.c, nil, tc.module, tc.action, tc.project)
			assert.Equal(t, normal, quiet,
				"CanAccessQuiet 与 CanAccess 的判定必须一致 —— 只差日志")
		})
	}
}

// CanAccessQuiet 不得因为"安静"而放宽判定:无权角色必须判成 false
func TestCanAccessQuiet_DoesNotWidenAccess(t *testing.T) {
	resetEnforcer(t)
	// 没有任何策略行
	assert.False(t, CanAccessQuiet(caller("role_user_admin", 999), nil, "owner", "list", 0),
		"没有任何策略行 ⇒ 必须拒绝(安静不等于放行)")
	assert.False(t, CanAccessQuiet(nil, nil, "owner", "list", 0),
		"caller=nil ⇒ 必须拒绝")
}
