// 看板可见性 —— 按业务域的权限码决定"看得到哪几块"
//
// # 谁来判
//
//	**不在 service 层判** —— service 层没有 casbin,也不该有。
//	判定在 handler 层用 middleware.CanAccess 做完
//	(那是**唯一一份**判定实现:super_admin 短路 / role+user 两路 EnforceEx /
//	deny 一票否决 / grant 委托),service 只负责"按判定结果决定算不算"。
//
// # 为什么不用绑定矩阵(sys_role_resource)直接查
//
//	绑定矩阵是**显示层**的真相(前端菜单/按钮就是读它),
//	casbin 才是**执行层**的真相。M149 的规矩是
//	「显示层不许自己算,必须读执行层那个权威字段」——
//	看板就是显示层,所以读 casbin。
//
//	(绑定矩阵和 casbin 在 M147-f 之后是同一份东西的物化副本,
//	但运行时改绑定到 casbin 同步之间有一个窗口 —— 那个窗口里两者会分叉,
//	看板宁可跟**执行层**一致:接口 403 的东西,看板也不该显示。)
package service

// DashboardDomain 看板上的一个业务域
type DashboardDomain struct {
	// Code 权限码(如 "owner:list")
	Code string
	// Module 权限模块名(判定时传给 middleware.CanAccess)
	Module string
	// Action 权限动作
	Action string
}

// dashboardDomains 看板涉及的业务域
//
// ⚠️ **一个域一个权限码,不多不少** —— 判定一次就够,
//
//	漏一个域 ⇒ 那个域永远不显示(且不报错)。
//	TestDashboard_DomainsCoverAllPerms 钉住"域表 ⊇ 指标表用到的所有权限码"。
//
// ⚠️ 这里的 Module/Action 拼出来的资源码**必须与 seed 的 resourceList 一致**
//
//	(否则 casbin 永远匹配不到 ⇒ 那个域对所有人都不显示)。
//	TestDashboard_DomainCodesExistInSeed 钉住这条。
var dashboardDomains = []DashboardDomain{
	// 文件系统(M80)—— 2026-11-03 存储统计加入看板
	{Code: "file:list", Module: "file", Action: "list"},
	{Code: "folder:list", Module: "folder", Action: "list"},
	// 当前在线人数 —— 纯计数,能打开工作台(dashboard:read)就能看(2026-11-03)
	{Code: "dashboard:read", Module: "dashboard", Action: "read"},
}

// DashboardDomains 返回看板涉及的业务域(副本)
func DashboardDomains() []DashboardDomain {
	out := make([]DashboardDomain, len(dashboardDomains))
	copy(out, dashboardDomains)
	return out
}

// DashboardVisibility 权限码 → 是否可见
type DashboardVisibility map[string]bool

// Allows 该权限码对应的域是否可见
//
// ⚠️ nil map 上调用是安全的(读 nil map 返回零值)⇒
//
//	"没判定过"一律落在**不可见**这一侧(安全侧倒向)。
func (v DashboardVisibility) Allows(code string) bool { return v[code] }
