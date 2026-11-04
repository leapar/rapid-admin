// 通用审批系统 —— 审批人解析:配置期预检 SPI(M85)
//
// 文档:docs/通用审批系统设计方案V1.0.md §5(审批人解析)/ §16.1(M85 文件清单)
//
// ⚠️ **本文件只放"引擎不调、模板 CRUD 调"的那一半 SPI。**
// 引擎依赖的另一半(`AssigneeResolver`)在 M84 已经跟引擎接口一起落在
// `approval.go` —— 两者挨着是因为引擎读它、模板 CRUD 也读它,不是偷懒。
//
// ⚠️ **为什么 `AssigneePrechecker` 不并进 `AssigneeResolver`**:引擎一次都不
// 调它。把引擎用不到的方法塞进引擎的 SPI,读代码的人会去找"引擎在哪儿调",
// 找不到就得重新判断一遍。并成一个接口省下一个类型,不值。
package service

import (
	"context"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// ============================================================================
// §5.3① 配置期预检
// ============================================================================

// StageRuleCheck 一个关卡的全部审批人规则(§5.3① 的预检单元)
type StageRuleCheck struct {
	// StageIndex 关卡序号 —— **只为了报错时说得出"第几关"**,解析过程不靠它
	StageIndex int
	// Rules 该关挂着的规则,按 `seq ASC` 排好序
	//
	// ⚠️ **多条规则是「或」**(§5.1)—— 所以预检的判据是
	//「**至少一条能解析出 ≥1 人**」而不是「每条都能」。前者对,后者会让
	// "配了 3 条部门主管规则、其中 2 条当前无人"的合法配置保存不了。
	Rules []model.SysApprovalAssigneeRule
}

// TemplatePrecheckInput 一个待保存模板的预检入参
type TemplatePrecheckInput struct {
	// TemplateCode 模板编码 —— 只出现在错误信息里
	TemplateCode string
	// Stages 全部关卡,按 `stage_index` 排好序
	Stages []StageRuleCheck
}

// AssigneePrechecker 配置期预检(§5.3①)。
//
// ⚠️ **它收的是"待保存的规则",不是"按 stage_id 查出来的规则"。**
// 模板 CRUD 是「先在内存里拼好整棵树、校验、再一次性落库」,预检必须校验
// **即将写进去的那份**;按 id 反查只能校验已经写好的,而正在写的那批根本查不到。
type AssigneePrechecker interface {
	// PrecheckTemplate 逐关预检。任一关解析不到人 → 返回带关号的错误,拒绝保存
	PrecheckTemplate(ctx context.Context, db *gorm.DB, in TemplatePrecheckInput) error
}

// ApprovalResolver 解析器的完整能力面(引擎 + 模板 CRUD 各取所需)。
//
// ⚠️ **构造器返回它而不是 `AssigneeResolver`** —— M86 的 `POST /flows` 要在
// 同一次装配里既解析又预检,返回窄接口就得写一句类型断言把宽接口再捞回来,
// 那是在给"接口拆得太碎"擦屁股。
type ApprovalResolver interface {
	AssigneeResolver
	AssigneePrechecker
}
