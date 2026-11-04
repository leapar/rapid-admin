// M147 —— 「角色-资源绑定矩阵」必须在**强制执行层**真的生效
//
// # 这个洞长什么样
//
// `buildRolePolicies` 给**每一个角色**加了一条通配行:
//
//	role:<role_code>, global, *, *, allow
//
// casbin 的 `keyMatch2(r.obj, "*")` **恒真** —— keyMatch2 把 "*" 编译成
// 正则 `^*$`,匹配任何字符串 ⇒ **任何角色都能访问任何接口**。
//
// 表现:前端按 13×394 的绑定矩阵渲染菜单(别人的菜单少了、按钮少了),
// 后端却**谁都能调**。矩阵只在"显示"这一层是真的。
// 安全上这是**全站越权**:物业工作人员改一下请求路径就能删用户。
//
// # 为什么"只把通配行删掉"是**更糟**的做法
//
// 通配行删掉之后还有第二个洞:绑定行的 obj 是**资源码**("user:list"),
// 而中间件传进去的 obj 是**模块名**("user")⇒
// `keyMatch2("user", "user:list")` = false ⇒ **一条绑定行都命中不了**。
// 结果 = "删了通配行,谁也访问不了任何接口"(连 auth:me 都不行 ⇒ 所有人被踢下线)。
//
// # 第三个洞:`:` 被当成了 URL 路径参数
//
// keyMatch2 是给 **URL 路径**写的:`/rapid-admin/v1/house/:id` 里的 `:id` 是参数。
// 而资源码命名空间里 `:` 是**字面量分隔符**("user" + ":" + "list")。
// 于是 "user:list" 被编译成正则 `^user[^/]+$` ⇒
// **绑了 user:list 的角色也能过 user:delete 的检查**(模块内权限互相串味);
// 同一条也让 `house:*` 这种 deny 规则**永不生效**(keyMatch2("house","house:*") = false)。
//
// # ⇒ 三处必须**配套**改,缺一不可
//
//  1. `configs/casbin_rbac.conf` 的 matcher:`keyMatch2(r.obj, p.obj)` → `keyMatch(r.obj, p.obj)`
//     (资源码命名空间里 ":" 是字面量;"*" 仍然只做后缀通配)
//  2. `buildRolePolicies`:删掉通配行
//  3. `internal/api/middleware/rbac.go`:请求 obj 传**完整资源码**(module + ":" + action)
//
// # 请求侧的契约(本文件与 middleware 包共用同一套口径)
//
//	r.obj = 资源码(module + ":" + action),例如 "user:list"
//	r.act = action 原样透传(策略行的 act 恒为 "*",不参与筛选)
//
// 这跟 grant 路径**本来就是一套**(`matchGrant` 里的 `wantPerm := module + ":" + action`),
// 之前是 casbin 路径与 grant 路径各用各的,同一个请求在两条路上判出两个结果。
package seed

import (
	"path/filepath"
	"testing"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// matrixCasbinModelPath 定位仓库里的 casbin 模型文件
func matrixCasbinModelPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "configs", "casbin_rbac.conf"))
	require.NoError(t, err)
	return p
}

// newSeededMatrixDB 跑一遍真 seed,返回装好角色/资源/绑定/策略的库
func newSeededMatrixDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newSeedTestDB(t)
	SetDriver("sqlite")
	casbinPath := matrixCasbinModelPath(t)
	require.FileExists(t, casbinPath, "casbin model 文件必须存在")
	require.NoError(t, Run(db, casbinPath, ""))
	return db
}

// newMatrixEnforcer 用**真的 adapter + 真的 enforcer** 读回同一个库的策略
//
// ⚠️ 不用包级 casbinx 单例 —— 那是 sync.Once,绑的是**第一次** Init 的那个库,
// 在单测里不可靠(别的包/别的用例先跑过就绑到别的库去了)。
func newMatrixEnforcer(t *testing.T, db *gorm.DB) *casbin.Enforcer {
	t.Helper()
	adapter, err := gormadapter.NewAdapterByDB(db)
	require.NoError(t, err)
	enf, err := casbin.NewEnforcer(matrixCasbinModelPath(t), adapter)
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())
	return enf
}

// TestRun_CasbinPolicies_NoWildcardRow ★ M147 回归线(第一层)
//
// 守的是"写进 casbin_rule 的行里没有通配行"。
//
// ⚠️ **为什么这条不能省**:后面那条"没绑的码必须被拒"是**行为**断言,
// 而行为断言依赖 matcher 的语义 —— 一旦有人把 matcher 改回 keyMatch2,
// "user:list" 会变成正则 `^user[^/]+$`,于是 user:delete 也命中 ⇒
// 行为断言**变红**但读起来像"绑定矩阵坏了",而真正的原因是 matcher 变了。
// 这条把"行"钉住,让三层改动各自有独立的责任人。
func TestRun_CasbinPolicies_NoWildcardRow(t *testing.T) {
	db := newSeededMatrixDB(t)

	var rows []gormadapter.CasbinRule
	require.NoError(t, db.Find(&rows).Error)
	require.NotEmpty(t, rows, "策略表不能是空的 —— 空表 = 除超管外所有人 403")

	for _, r := range rows {
		assert.NotEqual(t, "*", r.V2,
			"策略行 %q 的 obj 不能是 \"*\" —— 通配行让**每个角色**都能访问一切,绑定矩阵形同虚设", r.V0)
		assert.Equal(t, "*", r.V3,
			"策略行 %q 的 act 恒为 \"*\" —— 权限身份是**整个资源码**,act 这一维不参与筛选", r.V0)
	}

	// 资源码形态:obj 必须是一个**真实资源码**(带 ":" 或点分),不是路径也不是通配
	byCode := map[string]bool{}
	var pool []string
	require.NoError(t, db.Model(&model.SysResource{}).Pluck("code", &pool).Error)
	for _, c := range pool {
		byCode[c] = true
	}
	for _, r := range rows {
		assert.True(t, byCode[r.V2],
			"策略行的 obj %q 必须是资源池里的真实资源码", r.V2)
	}
}

// TestRun_CasbinPolicies_EnforceBindingMatrix ★ M147 回归线(第二层)
//
// 用**真的 enforcer** enforce,断言"绑定矩阵"真的在强制执行层生效。
//
// # 三个断言各自钉一件事
//
//	① 绑了的码 → 放行(否则就是"全站 403",比原来的洞更难查)
//	② 没绑的码 → 拒绝(这才是本模块要修的东西)
//	③ 同模块内不同 action 不串味(user:list 不放行 user:delete)
//	   —— 这一条专门盯 keyMatch2 把 ":" 当路径参数的后果
func TestRun_CasbinPolicies_EnforceBindingMatrix(t *testing.T) {
	db := newSeededMatrixDB(t)
	enf := newMatrixEnforcer(t, db)

	// 只读员:filterByRead 给的是所有 ":read"/":list"/":tree" 的码
	// ⇒ 能列用户(user:list),但**没有** user:delete。
	require.Contains(t, roleResourceCodes(t, db, model.RoleReadOnly), "user:list",
		"前置条件:只读员确实绑了 user:list(否则本用例测的是别的东西)")
	require.NotContains(t, roleResourceCodes(t, db, model.RoleReadOnly), "user:delete",
		"前置条件:只读员确实没绑 user:delete")

	t.Run("绑了的码放行", func(t *testing.T) {
		ok, rules, err := enf.EnforceEx("role:"+model.RoleReadOnly, "global", "user:list", "list")
		require.NoError(t, err)
		assert.True(t, ok, "只读员绑了 user:list ⇒ 必须放行(实际规则:%v)", rules)
	})

	t.Run("没绑的码拒绝", func(t *testing.T) {
		ok, rules, err := enf.EnforceEx("role:"+model.RoleReadOnly, "global", "user:delete", "delete")
		require.NoError(t, err)
		assert.False(t, ok,
			"只读员**没绑** user:delete ⇒ 必须拒绝(实际规则:%v)—— 放行就是全站越权", rules)
	})

	t.Run("同模块不同 action 不串味", func(t *testing.T) {
		// ⚠️ 这一条盯的是 keyMatch2 把 ":" 当 URL 路径参数的后果:
		//   "user:list" 被编译成 ^user[^/]+$ ⇒ user:delete 也会命中。
		//
		// 挑只读查看员当样本:filterByRead 按 ":list"/":read"/":tree" 后缀捞码,
		// 所以它绑了 dict:list,**没有** dict:delete(写通道只归平台运营员 / 超管)。
		require.Contains(t, roleResourceCodes(t, db, model.RoleReadOnly), "dict:list")
		require.NotContains(t, roleResourceCodes(t, db, model.RoleReadOnly), "dict:delete")

		ok, _, err := enf.EnforceEx("role:"+model.RoleReadOnly, "global", "dict:list", "list")
		require.NoError(t, err)
		assert.True(t, ok, "只读查看员绑了 dict:list ⇒ 放行")

		ok, _, err = enf.EnforceEx("role:"+model.RoleReadOnly, "global", "dict:delete", "delete")
		require.NoError(t, err)
		assert.False(t, ok, "只读查看员没绑 dict:delete ⇒ 拒绝(同模块的兄弟 action 不该串味)")
	})

	t.Run("超管的绑定矩阵覆盖全部资源", func(t *testing.T) {
		// 超管在中间件里是**代码短路**(HasRole("role_super_admin")),
		// 但策略矩阵本身也该是全的 —— 否则换个入口(非中间件的 CanAccess)就露馅。
		for _, code := range []string{"config:update", "file:delete", "user:delete"} {
			ok, _, err := enf.EnforceEx("role:"+model.RoleSuperAdmin, "global", code, "x")
			require.NoError(t, err)
			assert.True(t, ok, "超管应能访问 %s", code)
		}
	})
}

// TestRun_BindsResourceAccessibleToAllRoles ★ M147 配套
//
// # 为什么这条跟着 M147 一起改
//
// `/system/resource/accessible`(rbac("resource", "accessible"))是**自服务端点**:
// 返回"当前用户自己能访问哪些资源",前端拿它渲染菜单和按钮。
//
// 强制执行层一旦真的按矩阵判,**没绑这个码的角色就拉不到自己的权限清单**
// ⇒ 菜单和按钮全空(前端不报错,只是"什么都没有")。
// 修矩阵之前这件事被通配行盖住了 —— 谁都能调,所以没人发现。
//
// ⇒ 这跟 M93 补 `dict:list` 是**同一类洞、同一处理方式**:
// 自服务能力进 `authBaseCodes`,人人有份;写通道仍然只归管理员。
//
// ⚠️ 这里只加 **read 侧的 accessible**,不给 resource:list / resource:tree
// (那是"权限管理页"的能力,归用户管理员 / 超管)。
func TestRun_BindsResourceAccessibleToAllRoles(t *testing.T) {
	db := newSeededMatrixDB(t)

	for _, r := range roleList() {
		codes := roleResourceCodes(t, db, r.Code)
		assert.Contains(t, codes, "resource:accessible",
			"角色 %s(%s)必须有 resource:accessible —— 否则前端拉不到自己的权限清单,菜单/按钮全空",
			r.Code, r.Name)
	}

	// 权限管理页的能力**不**跟着放开
	for _, r := range roleList() {
		codes := roleResourceCodes(t, db, r.Code)
		for _, w := range []string{"resource:create", "resource:update", "resource:delete"} {
			if r.Code == model.RoleSuperAdmin || r.Code == model.RolePlatformOps {
				continue
			}
			assert.NotContains(t, codes, w,
				"角色 %s 不该有 %s —— 资源管理是平台运营员 / 超管的专属", r.Code, w)
		}
	}
}

// TestRun_CasbinPolicies_AuditMenusHaveApiCodes ★ §1.1 #18(M169,2026-11-02)
//
// # 守的洞
//
// 平台运营员侧边栏有四个审计菜单(menu.audit + operation / dataChange / login),
// 但 seed 的 api 侧此前一个 audit: 都没绑 ⇒ 四个入口点进去全 403
// —— 与 M154 那三个死菜单**同形状**(菜单给了、接口没给)。
//
// # 锁什么
//
//   - 绑定的必须**恰好**是各页面实际调用的只读码
//     (web/src/api/audit.ts 的 operationLogs / dataChangeLogs / loginLogs / systemLogs)
//   - export:create(写通道)与没配菜单的审计页(pii / sms / crypto / full-info)不绑
//   - 强制执行层(casbin)对三个码**放行** —— 绑定不能只在显示层是真的(M147 的教训)
func TestRun_CasbinPolicies_AuditMenusHaveApiCodes(t *testing.T) {
	db := newSeededMatrixDB(t)
	enf := newMatrixEnforcer(t, db)

	// 前置条件:审计菜单真的给了(否则本用例测的是别的东西)
	require.Contains(t, roleResourceCodes(t, db, model.RolePlatformOps), "menu.audit.operation",
		"前置条件:平台运营员确实绑了审计菜单(否则本用例测的是别的东西)")

	for _, code := range []string{"audit:operation:list", "audit:data:list", "audit:login:list", "audit:system:list"} {
		require.Contains(t, roleResourceCodes(t, db, model.RolePlatformOps), code,
			"平台运营员必须绑 %s —— 有菜单没接口 = 点进去 403(死菜单)", code)
		ok, rules, err := enf.EnforceEx("role:"+model.RolePlatformOps, "global", code, "list")
		require.NoError(t, err)
		assert.True(t, ok, "平台运营员绑了 %s ⇒ 强制执行层必须放行(实际规则:%v)", code, rules)
	}

	// 写通道与没配菜单的审计页不给码
	for _, code := range []string{"audit:export:create", "audit:pii:list", "audit:sms:list", "audit:crypto:list", "audit:full-info:list"} {
		assert.NotContains(t, roleResourceCodes(t, db, model.RolePlatformOps), code,
			"%s 不该绑给平台运营员 —— 没配菜单的能力不给码,写通道只归管理员", code)
	}
}
