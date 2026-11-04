// 动态集合记录接口的动作清单 —— **唯一真相**
//
// # 这份清单为什么住在原语层
//
// 它同时被两个地方消费:
//
//	service 侧 —— 建集合时按它登记 `dyn:<集合编码>:<动作>` 资源码
//	              (internal/service/dyn_resource.go)
//	handler 侧 —— 守卫时按它拼资源码
//	              (internal/api/handler/v1/dyn.go)
//
// ⚠️ **两边必须同源,否则会静默失权** ——
// 2026-11-01 M151 真的踩过:handler 多写了一个 "schema" 动作
// (表单结构接口要单独一个码),而 service 只登记 5 个码 ⇒
// `dyn:<集合编码>:schema` 这条资源码**永远不存在** ⇒
// 角色配置页里压根没有这一行可勾 ⇒ 谁也绑不上 ⇒
// 表单结构接口对所有非超管一律 403 ——
// 而日志里一条错误都没有(权限判定返回 false 是完全正常的一条路径)。
//
// ⇒ 清单 + 拼码函数都放在这里,两边引用同一份,不可能再分叉。
package dynx

// 记录接口的动作
const (
	// ActionList 列表
	//
	// ⚠️ **表单结构(字段定义)接口也归它管**,不另开一个码 ——
	// 字段定义是"渲染列表页"的前提:能看列表却看不到字段,
	// 页面就是一张空表格 + 一个 403 弹窗。
	// 而表单结构就是"这个集合有哪些字段",对能看记录的人没有信息增量。
	ActionList = "list"
	// ActionRead 单条详情
	ActionRead = "read"
	// ActionCreate 新建
	ActionCreate = "create"
	// ActionUpdate 编辑
	ActionUpdate = "update"
	// ActionDelete 删除(软删)
	ActionDelete = "delete"
	// ActionRestore 恢复(回收站里的软删记录还原)
	ActionRestore = "restore"
	// ActionBatchDelete 批量删除
	ActionBatchDelete = "batch_delete"
	// ActionBatchUpdate 批量修改单字段
	ActionBatchUpdate = "batch_update"
	// ActionExport 导出(CSV,按当前筛选导全量)
	ActionExport = "export"
	// ActionImport 导入(CSV)
	ActionImport = "import"
)

// actions 全部动作(顺序即资源码登记顺序)
//
// ⚠️ **用私有 slice + 拷贝函数,不要导出一个可变的 var** ——
// 导出的 slice 任何人都能 append,清单一旦被改就静默漂移。
var actions = []string{
	ActionList, ActionRead, ActionCreate, ActionUpdate, ActionDelete,
	ActionRestore, ActionBatchDelete, ActionBatchUpdate, ActionExport, ActionImport,
}

// actionSet 动作白名单(查询用,零分配)
var actionSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(actions))
	for _, a := range actions {
		m[a] = struct{}{}
	}
	return m
}()

// Actions 全部动作(返回副本)
func Actions() []string {
	out := make([]string, len(actions))
	copy(out, actions)
	return out
}

// IsAction 动作是否在清单里
//
// ⚠️ **守卫要调它** —— handler 拼资源码时若动作不在清单内,
// 那条资源码在 sys_resource 里根本不存在 ⇒
// 「角色管理」页没有这一行可勾 ⇒ 没人绑得上 ⇒
// 该接口对所有非超管 403 且**没有任何错误日志**。
// ⇒ 宁可在这里当场 500,也不要留一个"看起来有权限、其实谁也用不了"的接口。
func IsAction(action string) bool {
	_, ok := actionSet[action]
	return ok
}

// ActionLabel 动作的中文名(资源管理页显示用)
func ActionLabel(action string) string {
	switch action {
	case ActionList:
		return "列表"
	case ActionRead:
		return "详情"
	case ActionCreate:
		return "新建"
	case ActionUpdate:
		return "编辑"
	case ActionDelete:
		return "删除"
	case ActionRestore:
		return "恢复"
	case ActionBatchDelete:
		return "批量删除"
	case ActionBatchUpdate:
		return "批量修改"
	case ActionExport:
		return "导出"
	case ActionImport:
		return "导入"
	default:
		return action
	}
}

// RecordModule 动态集合记录接口的**资源码模块段**
//
// ⚠️ **中间件的约定是 `module + ":" + action`** ——
// 所以这里给的是"模块段"(末尾不带冒号),
// middleware.CanAccess(caller, principal, dynx.RecordModule(code), action, 0)
// 拼出来的才是 `dyn:<集合编码>:<动作>`。
//
// ⚠️ **不要在这里留尾巴冒号** ——
// "dyn:" + code 与 "dyn:" + code + ":" 两种拼法在模块段里只差一个字符,
// 但拼出来的资源码会变成 `dyn:inspection::list`(多一个冒号)⇒
// 跟登记的码对不上 ⇒ 又是"谁也绑不上"的静默失权。
func RecordModule(collection string) string {
	return "dyn:" + collection
}

// RecordCode 动态集合记录接口的**完整资源码**
//
// 登记 sys_resource 时用这个(与 middleware 拼出来的必须逐字相同)。
func RecordCode(collection, action string) string {
	return RecordModule(collection) + ":" + action
}
