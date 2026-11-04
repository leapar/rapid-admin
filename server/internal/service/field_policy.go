// 字段级可见性(列级权限)规则 —— 服务层接口 —— M162(2026-11-02)
//
// # 这一层与"能调接口"、"能看哪些行"是三件事
//
//	① 能调接口吗        → RBAC + casbin(§1.1)
//	② 能看哪些行        → 数据范围 scope(§1.13 #2)
//	③ 能看一行里的哪些字段 → **本层**(§1.13 #4)
//
// 三者互相独立:能调接口 ≠ 能看到全部行;能看到某行 ≠ 能看到那行里的每个字段。
//
// # 规则存在哪、怎么算、怎么生效
//
//   - 存:model.SysFieldPolicy(唯一键 role_id + module + field)
//   - 算:internal/pkg/fieldpolicy(纯函数,不碰 DB)
//   - 生效:响应出口统一脱敏(internal/api/handler 的 OK / Page)
//
// 本文件只定**契约**(接口 + DTO),实现在 field_policy_impl.go。
//
// # ⚠️ **只收窄,不放宽**
//
// 本层能把已经在响应里的字段打码或删掉;
// 它**不能**把本来就不在响应里的字段(如 `json:"-"` 的加密手机号)变出来。
// ⇒ 要让某个角色看到某个字段,得业务侧先把值放进响应。

package service

import (
	"context"

	"rapid-admin/internal/pkg/fieldpolicy"
)

// FieldPolicyInput 保存一条字段级可见性规则的入参
//
// ⚠️ **前端发的是"这一组规则",不是"增量改一条"** ——
// 写路径是**全量替换**:发来的这一组就是该业务域上的全部字段规则。
// 与数据范围(ScopeRuleInput)同一套语义。
type FieldPolicyInput struct {
	// Field 字段名(响应 JSON 里的键名)
	Field string `json:"field" binding:"required"`
	// Mode 可见性模式(visible / masked / hidden)
	Mode string `json:"mode" binding:"required"`
	// MaskType 打码方式(phone / id_card / name / email / bank_card /
	// contract_no / full);空串 = 按字段名自动推断
	MaskType string `json:"mask_type"`
	// Remark 备注(给管理员看的)
	Remark string `json:"remark"`
}

// FieldPolicyView 一条字段级可见性规则(读)
//
// ⚠️ **字段名是 snake_case** —— 与仓库其余 DTO 一致。
type FieldPolicyView struct {
	// Module 业务域(空串 = 默认规则组)
	Module string `json:"module"`
	// Field 字段名
	Field string `json:"field"`
	// Mode 可见性模式
	Mode string `json:"mode"`
	// MaskType 打码方式(空串 = 按字段名自动推断)
	MaskType string `json:"mask_type,omitempty"`
	// Remark 备注
	Remark string `json:"remark,omitempty"`
}

// FieldPolicyFieldView 一个可配置的字段候选项
type FieldPolicyFieldView struct {
	// Field 字段名(响应 JSON 里的键名)
	Field string `json:"field"`
	// Label 中文名(动态集合取字段标签;业务表没有标签来源,就是字段名本身)
	Label string `json:"label"`
	// MaskType 这个字段**推荐**的打码方式(空串 = 猜不出来,配 masked 时必须显式指定)
	MaskType string `json:"mask_type"`
	// Sensitive 是否命中内置的敏感字段表
	//
	// ⚠️ **前端可以据此排版** —— 但**它不是安全边界**:
	// 真正的边界是后端配的规则。
	Sensitive bool `json:"sensitive"`
}

// FieldPolicyModuleView 一个可配置的业务域(后台下拉框的候选项)
type FieldPolicyModuleView struct {
	// Module 业务域字符串(存库/传参都用它)
	Module string `json:"module"`
	// Label 中文名
	Label string `json:"label"`
	// Source 候选来源:api(业务表)/dyn(动态业务集合)/default(默认规则组)
	Source string `json:"source"`
	// Fields 这个业务域的字段候选
	//
	// ⚠️ **候选由后端推导,不许前端手打** ——
	// 字段名配错 ⇒ 规则作用在一个不存在的键上 ⇒ **静默不生效**。
	Fields []FieldPolicyFieldView `json:"fields"`
}

// FieldPolicyService 字段级可见性规则
type FieldPolicyService interface {
	// ListRoleFieldPolicies 列出某角色配的全部字段规则(所有业务域)
	//
	// ⚠️ **返回的是"配置",不是"生效结果"** ——
	// 生效结果还要看默认规则组,见 EffectiveFieldPolicy。
	ListRoleFieldPolicies(ctx context.Context, roleID uint64) ([]FieldPolicyView, error)

	// ReplaceRoleFieldPolicies 全量替换某角色在某业务域上的字段规则
	//
	// ⚠️ **module 为空串 = 默认规则组** ——
	// "这个角色在所有业务域上的默认字段规则"。
	//
	// ⚠️ **字段必须在这个业务域的候选里,否则配置期就拒** ——
	// 配一个不存在的字段名 ⇒ 规则作用不到任何键上 ⇒ 静默不生效。
	ReplaceRoleFieldPolicies(ctx context.Context, roleID uint64, module string, policies []FieldPolicyInput) error

	// ListFieldPolicyModules 列出可配置的业务域候选(含每个域的字段候选)
	//
	// ⚠️ **候选由后端推导,不许前端手打** —— 同上。
	ListFieldPolicyModules(ctx context.Context) ([]FieldPolicyModuleView, error)

	// EffectiveFieldPolicy 解析出某调用方在某业务域上的有效字段策略
	//
	// ⚠️ **默认规则组与业务域规则是"逐字段叠加",不是"整组覆盖"** ——
	// 默认组的限制不会被某个业务域的其它规则悄悄顶掉(见 pkg/fieldpolicy.Overlay)。
	//
	// ⚠️ **没配任何规则 ⇒ 返回空策略**(等于什么都不做 = 响应原样返回)。
	EffectiveFieldPolicy(ctx context.Context, caller *Caller, module string) (fieldpolicy.Policy, error)
}
