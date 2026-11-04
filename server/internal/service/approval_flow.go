// 通用审批系统 —— 流程模板 CRUD(M86)
// 文档:docs/通用审批系统设计方案V1.0.md §7.2 / §16.1
//
// ⚠️ **模板 CRUD 不在 `ApprovalEngine` 上**(M84 落地时拆开的)。
// 引擎管"一条单子怎么流转",模板管"一类业务要走哪些关卡"——两件事的生命周期
// 和失败模式都不一样,混在一个接口上会让"改模板要锁实例吗"这种问题没有答案。
//
// ⚠️ **本接口的写操作全部走一个事务**(§7.2):
// 模板 + 关卡 + 规则三层是一次性写进去的,任何一层失败整体回滚。
// 半截模板(有模板没关卡)比没有模板更糟 —— 它能被选中、能发起单子,然后卡死。
package service

import (
	"context"
	"time"

	"rapid-admin/internal/model"
)

// ============================================================================
// 接口
// ============================================================================

// ApprovalFlowService 审批流程模板 CRUD(§7.2)
//
// ⚠️ **不挂独立权限码的判定在 handler 层** —— 本接口只管数据,
// "谁能改模板"由 `flow:create/update/delete` 三个资源码决定(§8.1)。
type ApprovalFlowService interface {
	// ListFlows 模板列表
	ListFlows(ctx context.Context, q FlowListQuery) (*model.PageResponse, error)
	// GetFlow 模板详情(含关卡与规则)
	GetFlow(ctx context.Context, id uint64) (*FlowDetail, error)
	// CreateFlow 建模板
	CreateFlow(ctx context.Context, caller *Caller, req FlowSaveReq) (uint64, error)
	// UpdateFlow 改模板(整体替换关卡与规则)
	UpdateFlow(ctx context.Context, caller *Caller, id uint64, req FlowSaveReq) error
	// DeleteFlow 删模板(软删 + 改写 code 让出唯一坑位,D7)
	DeleteFlow(ctx context.Context, caller *Caller, id uint64) error
}

// ============================================================================
// DTO
// ============================================================================

// FlowListQuery 模板列表查询
type FlowListQuery struct {
	model.PageRequest
	// Keyword 按 code / name 模糊匹配
	Keyword string `json:"keyword"`
	// BizType 按业务类型过滤
	BizType string `json:"biz_type"`
	// IsActive nil = 不过滤
	IsActive *bool `json:"is_active"`
}

// FlowRuleInput 一条审批人规则(§5.1)
//
// ⚠️ **按 rule_type 只填对应的那一个 ID** —— 另两个必须留 0。
// 留了非 0 的那个会被 precheck 判成"规则配错"(§5.1 的三种来源互斥)。
type FlowRuleInput struct {
	// RuleType user / role / dept_leader
	RuleType string `json:"rule_type"`
	// UserID 仅 rule_type=user 时有效
	UserID uint64 `json:"user_id,string"`
	// RoleID 仅 rule_type=role 时有效
	RoleID uint64 `json:"role_id,string"`
	// Levels 仅 rule_type=dept_leader 时有效,<=0 视作爬到顶
	Levels int `json:"levels"`
}

// FlowStageInput 一个关卡(§7.2)
type FlowStageInput struct {
	// Name 关卡名
	Name string `json:"name"`
	// ReviewType all(会签) / any(或签)—— 空视作 any
	ReviewType string `json:"review_type"`
	// Rules 该关的审批人规则(**多条是「或」,非空**)
	Rules []FlowRuleInput `json:"rules"`
}

// FlowSaveReq 建 / 改模板的入参
//
// ⚠️ **`IsActive` 是指针**:nil = 不改(改模板时),非 nil = 显式设置。
// 值类型分不清"不传"和"传 false",而模板停用是一件有后果的事。
type FlowSaveReq struct {
	// Code 模板编码,格式 `^[a-z][a-z0-9_]{1,63}$`
	Code string `json:"code"`
	// Name 模板名
	Name string `json:"name"`
	// BizType 业务类型(如 digital_material)
	BizType string `json:"biz_type"`
	// Remark 备注
	Remark string `json:"remark"`
	// IsActive nil = 不改 / 建模板时默认 true
	IsActive *bool `json:"is_active"`
	// Stages 关卡列表,长度 1~10,序号从 1 连续
	Stages []FlowStageInput `json:"stages"`
}

// FlowListItem 模板列表项
type FlowListItem struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	BizType  string `json:"biz_type"`
	IsActive bool   `json:"is_active"`
	Remark   string `json:"remark"`
	// StageCount 关卡数 —— 列表页要显示"几关",不该为此再查一次详情
	StageCount int       `json:"stage_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// FlowDetail 模板详情
type FlowDetail struct {
	FlowListItem
	// Stages 关卡(按 stage_index 升序)
	Stages []FlowStageView `json:"stages"`
}

// FlowStageView 关卡视图
type FlowStageView struct {
	ID         string         `json:"id"`
	StageIndex int            `json:"stage_index"`
	Name       string         `json:"name"`
	ReviewType string         `json:"review_type"`
	Rules      []FlowRuleView `json:"rules"`
}

// FlowRuleView 规则视图
type FlowRuleView struct {
	ID       string `json:"id"`
	Seq      int    `json:"seq"`
	RuleType string `json:"rule_type"`
	// UserID / RoleID 都是 string —— 雪花 ID 19 位,超出 JS 的 2^53(§7.5)
	UserID string `json:"user_id"`
	RoleID string `json:"role_id"`
	Levels int    `json:"levels"`
}
