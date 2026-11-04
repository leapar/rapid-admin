// 审批系统的案例流程模板 seed
//
// 开源口径:框架不预置任何"默认可用"的业务审批模板(引擎的
// `pickTemplate` 找不到启用模板时报 `ApprovalNoTemplate`,由业务方
// 在「审批流程配置」页配置自己的模板后启用)。
// 这里落的是三条**默认停用**的案例模板(请假 / 报销 / 采购)——
// 是给配置页当起手示例的,不是可用配置;运营把规则改成自己的
// 组织后再启用。规则全用 dept_leader(不依赖角色/人员运行期数据)。
package seed

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

// isRecordNotFound 判断是不是"查不到"
//
// ⚠️ **用 `errors.Is` 不用 `==`** —— gorm 的错误是 wrapped 的,
// `==` 比不过(本仓规矩:判 error 只用 errors.Is)。
func isRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// ============================================================================
// 案例模板(2026-11-03)—— 请假 / 报销 / 采购(或签),默认**停用**
// ============================================================================

// 案例模板的常量(编码与手册《审批使用教程》§5 三个示例一一对应)
const (
	// LeaveCaseCode 请假审批案例(手册示例一)
	LeaveCaseCode    = "leave_approval"
	LeaveCaseName    = "请假审批"
	LeaveCaseBizType = "leave"

	// ExpenseCaseCode 报销审批案例(手册示例二)
	ExpenseCaseCode    = "expense_approval"
	ExpenseCaseName    = "报销审批"
	ExpenseCaseBizType = "expense"

	// PurchaseCaseCode 采购申请案例(手册示例三:会签 + 或签混合)
	PurchaseCaseCode    = "purchase_approval"
	PurchaseCaseName    = "采购申请"
	PurchaseCaseBizType = "purchase"
)

// caseRule 案例规则的最小形状(案例只演示 dept_leader,见函数头注释)
type caseRule struct {
	ruleType string
	levels   int
}

// caseStage 案例关卡的最小形状
type caseStage struct {
	name       string
	reviewType string // model.ApprovalReviewAll / ApprovalReviewAny
	rules      []caseRule
}

// caseTemplate 案例模板的最小形状
type caseTemplate struct {
	code    string
	name    string
	bizType string
	remark  string
	stages  []caseStage
}

// approvalCaseTemplates 三条案例模板的数据
//
// # 为什么规则全部用 dept_leader,不用 role
//
// `role` 规则要 role_id,而角色成员是运行期数据 —— seed 里如果写死
// "role_project_manager" 这类**不存在**的角色 id,规则页打开就是空引用,
// 提交时必然 20601。dept_leader 不依赖任何 id:它的结果取决于发起人所在
// 部门树的负责人配置(1=直属主管,2=上级主管,≤0=爬到顶),
// 部门 seed 已经把树铺好,运营配好负责人后案例就能直接跑。
//
// # 为什么默认 is_active=false
//
// 案例的关卡结构是教学用的,不是你组织的审批流;停用状态保证
// `pickTemplate` 永远捡不到它(发起侧零影响),运营把规则改成
// 自己的组织后再启用。这也是为什么 upsert 时**不碰 is_active** ——
// 运营启用过之后,seed 每次启动把它摁回停用等于悄悄下线一条在用的流程。
func approvalCaseTemplates() []caseTemplate {
	return []caseTemplate{
		{
			code:    LeaveCaseCode,
			name:    LeaveCaseName,
			bizType: LeaveCaseBizType,
			remark: "案例模板(默认停用):部门主管 → 分管领导 → 单位负责人,逐级会签。" +
				"规则按部门层级预配(1=直属主管,2=上级主管,≤0=爬到顶);" +
				"想按角色配置见手册《示例一》,改完规则再启用。",
			stages: []caseStage{
				{name: "部门主管审批", reviewType: model.ApprovalReviewAll,
					rules: []caseRule{{model.ApprovalRuleDeptLeader, 1}}},
				{name: "分管领导审批", reviewType: model.ApprovalReviewAll,
					rules: []caseRule{{model.ApprovalRuleDeptLeader, 2}}},
				{name: "单位负责人备案", reviewType: model.ApprovalReviewAll,
					rules: []caseRule{{model.ApprovalRuleDeptLeader, 0}}},
			},
		},
		{
			code:    ExpenseCaseCode,
			name:    ExpenseCaseName,
			bizType: ExpenseCaseBizType,
			remark: "案例模板(默认停用):三级逐级会签;最后一关放两条规则演示" +
				"同关多规则=并集 —— 两人都批才过(双人会签复核);" +
				"想要任一人复核即过,把第 4 关 review_type 改成 any。" +
				"按角色配置见手册《示例二》。",
			stages: []caseStage{
				{name: "部门主管审批", reviewType: model.ApprovalReviewAll,
					rules: []caseRule{{model.ApprovalRuleDeptLeader, 1}}},
				{name: "分管领导审批", reviewType: model.ApprovalReviewAll,
					rules: []caseRule{{model.ApprovalRuleDeptLeader, 2}}},
				{name: "单位负责人审批", reviewType: model.ApprovalReviewAll,
					rules: []caseRule{{model.ApprovalRuleDeptLeader, 0}}},
				{name: "复核备案(双规则会签)", reviewType: model.ApprovalReviewAll,
					rules: []caseRule{
						{model.ApprovalRuleDeptLeader, 2},
						{model.ApprovalRuleDeptLeader, 0},
					}},
			},
		},
		{
			code:    PurchaseCaseCode,
			name:    PurchaseCaseName,
			bizType: PurchaseCaseBizType,
			remark: "案例模板(默认停用):第 1 关会签逐级推进;第 2 关「归口审核」或签 —— " +
				"两条规则取并集,谁先处理谁说了算,首个动作即整单终局" +
				"(或签只能放最后一关,放中间 = 后面的关卡永远不启动)。见手册《示例三》。",
			stages: []caseStage{
				{name: "部门主管确认需求", reviewType: model.ApprovalReviewAll,
					rules: []caseRule{{model.ApprovalRuleDeptLeader, 1}}},
				{name: "归口审核", reviewType: model.ApprovalReviewAny,
					rules: []caseRule{
						{model.ApprovalRuleDeptLeader, 2},
						{model.ApprovalRuleDeptLeader, 0},
					}},
			},
		},
	}
}

// seedApprovalCaseTemplates 落三条案例模板(请假 / 报销 / 采购)
//
// ⚠️ **create-if-missing,比 seedApprovalTemplates 更收敛** ——
// 已存在的模板**什么都不改**(name / remark / is_active / 关卡 / 规则
// 一概不碰):material_review 是系统模板,名字和启用状态由代码说了算;
// 案例从落库那一刻起就是运营的 —— 他改的名、启的用、删的关,
// seed 每次启动重跑一遍都不能冲掉(测试
// TestSeed_CaseTemplates_UpsertKeepsUserState 钉的就是这条)。
//
// ⚠️ **创建时 is_active=false** —— 案例不是可用配置,见
// approvalCaseTemplates 的头注释。
func seedApprovalCaseTemplates(db *gorm.DB) error {
	for _, ct := range approvalCaseTemplates() {
		var tpl model.SysApprovalTemplate
		err := db.Where("code = ?", ct.code).First(&tpl).Error
		if err == nil {
			// 已存在 = 运营的案例(他可能改了名、启用了、改了流程)——
			// **什么都不改**,只报一行日志。案例模板与 material_review 的
			// 区别就在这:material_review 是系统模板,名字/启用状态由代码
			// 说了算;案例从落库那一刻起就是运营的。
			fmt.Println("[seed] approval case template exists, left untouched:", ct.code)
			continue
		}
		if !isRecordNotFound(err) {
			return fmt.Errorf("query approval case template %s: %w", ct.code, err)
		}

		tpl = model.SysApprovalTemplate{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			Code:      ct.code,
			Name:      ct.name,
			BizType:   ct.bizType,
			IsActive:  false,
			Remark:    ct.remark,
		}
		if err := db.Create(&tpl).Error; err != nil {
			return fmt.Errorf("create approval case template %s: %w", ct.code, err)
		}
		for i, st := range ct.stages {
			stage := model.SysApprovalStage{
				BaseModel:  model.BaseModel{ID: sonyflake.Next()},
				TemplateID: tpl.ID,
				StageIndex: i + 1,
				Name:       st.name,
				ReviewType: st.reviewType,
			}
			if err := db.Create(&stage).Error; err != nil {
				return fmt.Errorf("create approval case stage %s#%d: %w", ct.code, i+1, err)
			}
			for j, rl := range st.rules {
				rule := model.SysApprovalAssigneeRule{
					BaseModel: model.BaseModel{ID: sonyflake.Next()},
					StageID:   stage.ID,
					Seq:       j + 1,
					RuleType:  rl.ruleType,
					Levels:    rl.levels,
				}
				if err := db.Create(&rule).Error; err != nil {
					return fmt.Errorf("create approval case rule %s#%d#%d: %w", ct.code, i+1, j+1, err)
				}
			}
		}
		fmt.Println("[seed] approval case template created (inactive):", ct.code)
	}
	return nil
}
