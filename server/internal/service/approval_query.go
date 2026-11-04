// 通用审批系统 —— 审批单查询 + 建草稿(M86)
// 文档:docs/通用审批系统设计方案V1.0.md §7.3 / §8.2 / §8.3
//
// ⚠️ **查询层和引擎是分开的**:引擎管"一条单子怎么流转"(M84),
// 本文件管"我怎么找到属于我的那些单子"。两者共用七张表,但生命周期完全不同 ——
// 查询永远只读,引擎永远加锁。
//
// ⚠️ **列表只有三个盒子,没有全量**(§8.2)。每个盒子都由「我和这条单的关系」
// 定义,结构上不可能越权。这是砍掉 `box=all` 换来的好处 ——
// **`box=all` 一旦加回来,广发 `approval:read` 立刻变成越权**(§12 第 9 条)。
package service

import (
	"context"
	"encoding/json"
	"time"

	"rapid-admin/internal/model"
)

// ============================================================================
// 接口
// ============================================================================

// ApprovalService 审批单查询(读) + 建草稿(写)
//
// ⚠️ **写动作(提交/审批/作废/改草稿)不在这里** —— 它们全在 `ApprovalEngine` 上,
// 理由是那四个动作都要走实例行锁(§3.5),而本接口是纯读的。
// 把它们合在一起会出现"一个接口里一半要锁一半不要锁"的怪东西。
type ApprovalService interface {
	// ListApprovals 列表。`box` 必须是 todo / mine / done 之一(§8.2)
	ListApprovals(ctx context.Context, caller *Caller, q ApprovalListQuery) (*model.PageResponse, error)

	// GetApproval 详情(含 rounds[] 与 logs[],§7.5)
	GetApproval(ctx context.Context, caller *Caller, id uint64) (*ApprovalDetail, error)

	// CreateDraft 建草稿(HTTP 通道)
	//
	// ⚠️ **业务记录应当与审批单同事务创建**(§7.3 / D8)—— 那条路是
	// 业务 service 直接调 `ApprovalEngine.CreateDraft(ctx, tx, ...)`,
	// **不是走这个 HTTP 端点**。本方法只服务"业务记录已经存在、单独发起审批"
	// 这种场景(比如补发审批、重新发起)。
	CreateDraft(ctx context.Context, caller *Caller, req CreateApprovalReq) (uint64, error)
}

// ============================================================================
// DTO
// ============================================================================

// ApprovalListQuery 列表查询
type ApprovalListQuery struct {
	model.PageRequest
	// Box todo / mine / done —— **必填,没有默认值**(§8.2)
	//
	// ⚠️ **空 box 直接报错,不退化成"全部"** —— 退化的话,
	// 前端少传一个参数 = 全平台审批单裸奔。
	Box string `json:"box"`
	// BizType 按业务类型过滤(选填)
	BizType string `json:"biz_type"`
	// Status 按实例状态过滤(选填)
	Status string `json:"status"`
	// Keyword 按标题模糊匹配(选填)
	Keyword string `json:"keyword"`
}

// ApprovalListItem 列表项
type ApprovalListItem struct {
	ID      string `json:"id"`
	BizType string `json:"biz_type"`
	// BizID 业务记录 ID —— 前端拿它跳回业务详情(§9.1 顶部)
	BizID  string `json:"biz_id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// CurrentStage 当前关卡序号(0 = 未进入任何关卡)
	CurrentStage int `json:"current_stage"`
	// CurrentStageName 当前关卡名 —— 列表页要显示"卡在谁那儿"
	CurrentStageName string `json:"current_stage_name"`
	// ApplicantUserID 发起人
	ApplicantUserID string `json:"applicant_user_id"`
	// ApplicantName 发起人姓名(冗余一份,省一次 join)
	ApplicantName string `json:"applicant_name"`
	// ReturnCount 累计被打回次数
	ReturnCount int `json:"return_count"`
	// MyTaskStatus 我和这条单的关系:pending / approved / rejected / 空
	//
	// ⚠️ **空 = 我不是审批人**(比如在"我发起的"盒子里)。
	// 前端靠它决定要不要渲染审批按钮 —— **按钮可见性由数据决定,不由 RBAC 决定**(§8.1)。
	MyTaskStatus string `json:"my_task_status"`
	// TemplateCode 模板编码
	TemplateCode string `json:"template_code"`
	// TemplateName 模板名
	TemplateName string     `json:"template_name"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	SubmittedAt  *time.Time `json:"submitted_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

// ApprovalDetail 详情
//
// ⚠️ **`rounds[]` 和 `logs[]` 一次返回,不为轮次单开端点**(§7.5)——
// 它只有一个消费者(详情页),单开端点等于多一处路由/权限码/前端请求要同步。
type ApprovalDetail struct {
	ApprovalListItem
	// Variables 事由 / 上下文(JSON 文本)
	//
	// ⚠️ **它不受权限过滤** —— 前端不能拿它做展示判断(§11 R10)。
	Variables string `json:"variables"`
	// TemplateID / ApplicantIdentityID
	TemplateID          string `json:"template_id"`
	ApplicantIdentityID string `json:"applicant_identity_id"`
	// Stages 模板的关卡 + 每关的审批人与状态(§9.1 中部进度条)
	Stages []ApprovalStageView `json:"stages"`
	// Rounds 轮次快照(§6.1 ⑦)
	Rounds []ApprovalRoundView `json:"rounds"`
	// Logs 意见流水(§6.1 ⑥)
	Logs []ApprovalLogView `json:"logs"`
	// CanAct 我能不能在这条单上做审批动作
	//
	// ⚠️ **这是给前端隐藏按钮用的,不是安全边界** —— 后端 `Act` 会再算一遍。
	// 前端算一遍是为了体验(不该出现的按钮别出现),后端算一遍是为了安全。
	CanAct bool `json:"can_act"`
}

// ApprovalStageView 关卡视图
type ApprovalStageView struct {
	StageIndex int    `json:"stage_index"`
	Name       string `json:"name"`
	ReviewType string `json:"review_type"`
	// Status 本关的展示状态:done / current / rejected / pending
	//
	// ⚠️ **纯展示用,不是判定依据** —— 判定一律看 task 表(§4.1)。
	Status string `json:"status"`
	// TotalCount 本关审批人总数
	TotalCount int `json:"total_count"`
	// ApprovedCount 本关已通过人数(§9.1 硬要求 2:"共 N 人,已批 M 人")
	ApprovedCount int `json:"approved_count"`
	// Assignees 本关审批人及各自状态
	Assignees []ApprovalAssigneeView `json:"assignees"`
}

// ApprovalAssigneeView 审批人视图
type ApprovalAssigneeView struct {
	UserID string `json:"user_id"`
	// Name 姓名
	Name string `json:"name"`
	// Status pending / approved / rejected / skipped
	Status string `json:"status"`
	// ActionedAt 处理时间
	ActionedAt *time.Time `json:"actioned_at"`
}

// ApprovalRoundView 轮次快照
type ApprovalRoundView struct {
	RoundNo int `json:"round_no"`
	// Title 本轮提交时的标题快照
	Title string `json:"title"`
	// Variables 本轮提交时的事由快照
	Variables string `json:"variables"`
	// SubmitterID 本轮提交人
	SubmitterID string `json:"submitter_id"`
	// SubmittedAt 提交时间
	SubmittedAt time.Time `json:"submitted_at"`
	// Outcome running / approved / rejected / rolled_back
	Outcome string `json:"outcome"`
	// OutcomeBy 终局操作人
	OutcomeBy string `json:"outcome_by"`
	// OutcomeAt 终局时间
	OutcomeAt *time.Time `json:"outcome_at"`
}

// ApprovalLogView 意见流水
type ApprovalLogView struct {
	ID string `json:"id"`
	// StageIndex 发生在第几关
	StageIndex int `json:"stage_index"`
	// RoundNo 第几轮
	RoundNo int `json:"round_no"`
	// Action submit / approve / reject / rollback / cancel
	Action string `json:"action"`
	// OperatorUserID 操作人
	OperatorUserID string `json:"operator_user_id"`
	// OperatorName 操作人姓名
	OperatorName string `json:"operator_name"`
	// Comment 意见
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateApprovalReq 建草稿入参(HTTP 通道)
type CreateApprovalReq struct {
	// BizType 业务类型
	BizType string `json:"biz_type"`
	// BizID 业务记录 ID
	BizID uint64 `json:"biz_id,string"`
	// Title 标题
	Title string `json:"title"`
	// TemplateCode 模板编码(选填;不给则取该 biz_type 唯一启用的模板)
	TemplateCode string `json:"template_code"`
	// Variables 事由 / 上下文(**原始 JSON 文本**)
	//
	// ⚠️ **收 `json.RawMessage` 而不是 `map[string]any`** —— 引擎要**逐字节**
	// 比较"这次重提的内容和本轮快照是否完全一样"(§3.3),而 `map` 序列化
	// 的键序是 Go 的随机序,同一份内容两次序列化可能不一样。
	// 那会让"没改内容"的重提被误判成"改了内容",或者反过来。
	Variables json.RawMessage `json:"variables"`
}
