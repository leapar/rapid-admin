// 数据范围(行级权限)规则 —— 服务层接口 —— M155(2026-11-02)
//
// # 这一层与"能调接口"是两件事
//
// 本框架的鉴权只回答"这个角色能不能调这个接口"(RBAC + casbin)。
// 数据范围回答的是"接口放行之后,他能**看到哪些行**"。
//
// 两者必须分开:
//   - 能调接口 ≠ 能看到全部数据
//   - 看不到某行 ≠ 不能调接口(列表页看不到,详情页按 id 可能打得开)
//
// 混在一起的后果是:管理员在角色配置页配了"这个角色只能看本部门",
// 却以为"没配 = 不限制",于是线上数据边界形同虚设。
//
// # 规则存在哪、怎么算、怎么落到 SQL
//
//   - 存:model.SysDataScope(唯一键 role_id + module + scope_type)
//   - 算:internal/pkg/scope(纯函数,不碰 DB)
//   - 落:internal/pkg/scope.Apply(翻译成 WHERE)
//
// 本文件只定**契约**(接口 + DTO),实现在 scope_impl.go。

package service

import (
	"context"

	"gorm.io/gorm"

	"rapid-admin/internal/pkg/scope"
)

// ScopeRuleInput 保存一条数据范围规则的入参
//
// ⚠️ **前端发的是"这一组规则",不是"增量改一条"** ——
// 写路径是**全量替换**:发来的这一组就是该业务域上的全部规则。
// 增量语义(POST 一条加一条)在这个模型里没有位置 ——
// 唯一键是 (role_id, module, scope_type),同一角色的同一业务域上
// "本部门"只能有一条,增量加进来会撞唯一索引。
type ScopeRuleInput struct {
	// ScopeType 范围类型(all / project / dept / dept_and_child / self / custom)
	ScopeType string `json:"scope_type" binding:"required"`
	// CustomColumn 自定义范围的列名(仅 scope_type=custom 时有意义)
	CustomColumn string `json:"custom_column"`
	// CustomValues 自定义范围的允许取值(仅 scope_type=custom 时有意义)
	CustomValues []string `json:"custom_values"`
	// Remark 备注(给管理员看的)
	Remark string `json:"remark"`
}

// ScopeRuleView 一条数据范围规则(读)
//
// ⚠️ **字段名是 snake_case** —— 与仓库其余 DTO 一致(见 memory:
// "snake_case 字段统一"那条迁移),前端 types/permission.Role 同款。
type ScopeRuleView struct {
	// Module 业务域(空串 = 默认规则)
	Module string `json:"module"`
	// ScopeType 范围类型
	ScopeType string `json:"scope_type"`
	// CustomColumn 自定义范围的列名
	CustomColumn string `json:"custom_column,omitempty"`
	// CustomValues 自定义范围的允许取值
	CustomValues []string `json:"custom_values,omitempty"`
	// Remark 备注
	Remark string `json:"remark,omitempty"`
}

// ScopeModuleView 一个可选的业务域(后台下拉框的候选项)
type ScopeModuleView struct {
	// Module 业务域字符串(存库/传参都用它)
	Module string `json:"module"`
	// Label 中文名(资源名或集合名)
	Label string `json:"label"`
	// Source 候选来源:api(接口权限码)/dyn(动态业务集合)
	Source string `json:"source"`
	// SupportedTypes 这个业务域支持的数据范围类型
	//
	// ⚠️ **前端必须按它过滤维度下拉框** —— M161 起,后端在**配置期**
	// 就拒"这一维在这张表上落不下去"的组合(house 表没有归属人列 ⇒
	// 「仅本人」配不上去)。前端不按它过滤,就是让管理员配一个
	// 注定被拒的组合 —— 用户看到的是"我明明能选",点保存却报错。
	//
	// ⚠️ **空列表 = 全部支持**(老后端没这个字段时的兼容口径)——
	// 前端拿到空数组时不该把下拉框清空,那会让"没这个字段"变成"什么都配不了"。
	SupportedTypes []string `json:"supported_types,omitempty"`
}

// ScopeService 数据范围规则
type ScopeService interface {
	// ListRoleScopes 列出某角色配的全部数据范围规则(所有业务域)
	//
	// ⚠️ **返回的是"配置",不是"生效结果"** ——
	// 生效结果取决于调用方是谁(项目/部门不同 ⇒ 可见行不同),
	// 那要问 EffectiveScope。
	ListRoleScopes(ctx context.Context, roleID uint64) ([]ScopeRuleView, error)

	// ReplaceRoleScopes 全量替换某角色在某业务域上的规则
	//
	// ⚠️ **module 为空串 = 默认规则组** ——
	// 某业务域没单独配规则时,回落到默认组(module 为空的那一组)。
	// ⚠️ **某业务域单独配了规则,就完全用这一组,默认组不参与** ——
	// 两组不是叠加关系,是"就近覆盖"。
	ReplaceRoleScopes(ctx context.Context, roleID uint64, module string, rules []ScopeRuleInput) error

	// ListModules 列出可配置的业务域候选(后台下拉框用)
	//
	// ⚠️ **候选由后端推导,不许前端手打** ——
	// 业务域字符串配错一个字符,规则就静默不生效(见 model.SysDataScope 的注释)。
	// 推导来源:接口权限码的模块段 + 已建动态业务集合。
	ListModules(ctx context.Context) ([]ScopeModuleView, error)

	// EffectiveScope 解析出某调用方在某业务域上的有效作用域
	//
	// 没配任何规则 ⇒ 返回零值 Scope(不限���,等于"全部数据")——
	// **这是现状行为,保持不变**:规则层上线不改变任何现有查询的结果。
	EffectiveScope(ctx context.Context, caller *Caller, module string) (scope.Scope, error)

	// Apply 把作用域落到查询上
	//
	// ⚠️ **业务域从 ctx 里读,不在参数里传** ——
	// 业务域字符串由"判定权限的那一层"写进 ctx(RBAC 中间件 / dyn handler),
	// service 只管读。两处拼同一个字符串必然漂移。
	Apply(ctx context.Context, tx *gorm.DB, cols scope.Columns) (*gorm.DB, error)
}
