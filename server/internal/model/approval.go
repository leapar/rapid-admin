// 通用审批系统 —— 七张表 + 枚举常量
//
// 文档:docs/通用审批系统设计方案V1.0.md §6.1(数据模型)/ §16.3(枚举常量)
//
// ⚠️ **前缀 `sys_`** —— 框架层既有 `sys_user` / `sys_role` / `sys_file` / `sys_job`。
// **不碰 `biz_` 命名空间**:审批引擎是框架能力,业务表将来挂上来才是 biz_。
//
// ⚠️ **JSON 列一律 `string` + `gorm:"type:json"`,手工 Marshal** —— 本仓 18 处 JSON 列
// 全是这个写法(`activity.go:19` / `asset.go:45` / `permission.go:32` …),
// **引入 `datatypes.JSON` 或 `serializer:json` 会成为全仓唯一破例写法**。
package model

import "time"

// ============================================================================
// 枚举常量
// ============================================================================
//
// ⚠️ **字符串常量 + `const` 块,不用 `iota`** —— 状态会进 DB,数值一改就是数据迁移。
//
// ⚠️ **每个状态值都要在文档里有"谁写它"** —— 没有生产者的状态不给常量(§3.2 的规矩)。
// 本文件里每个常量旁边的注释就是它的生产者。

// 实例状态(§3.1)
const (
	// ApprovalStatusDraft 草稿 —— 生产者:CreateDraft
	ApprovalStatusDraft = "draft"
	// ApprovalStatusRunning 审批中 —— 生产者:Submit(含重提)
	ApprovalStatusRunning = "running"
	// ApprovalStatusReturned 已打回,等申请人改完重提 —— 生产者:Rollback
	ApprovalStatusReturned = "returned"
	// ApprovalStatusApproved 已通过(终态)—— 生产者:Approve(全部关卡通过 / 或签首个动作)
	ApprovalStatusApproved = "approved"
	// ApprovalStatusRejected 已拒绝(终态)—— 生产者:Reject
	ApprovalStatusRejected = "rejected"
	// ApprovalStatusCancelled 已作废(终态)—— 生产者:Cancel
	//
	// ⚠️ **双 l**,与仓库既有词表一致(`activity.go:46` / `seed.go:1081` / `push/event.go:129`)。
	// **不要写成 `canceled`。**
	ApprovalStatusCancelled = "cancelled"
)

// 任务状态(§3.2)
const (
	// ApprovalTaskPending 待处理 —— 生产者:关卡启动时为每个审批人生成一行
	ApprovalTaskPending = "pending"
	// ApprovalTaskApproved 已通过 —— 生产者:该审批人点了通过
	ApprovalTaskApproved = "approved"
	// ApprovalTaskRejected 已拒绝 —— 生产者:该审批人点了拒绝
	ApprovalTaskRejected = "rejected"
	// ApprovalTaskSkipped 跳过 —— 生产者:终局时其余未处理的 pending 全部置此
	ApprovalTaskSkipped = "skipped"
	// ApprovalTaskStale 过期(上一轮,不再计入分母)—— 生产者:打回时本实例所有 task 置此
	//
	// ⚠️ **这是 D4 的机制本体**:轮次不靠日志分界,靠 task 的 stale 状态。
	ApprovalTaskStale = "stale"
)

// 轮次结果(§6.1 ⑦)
const (
	// ApprovalRoundRunning 进行中 —— 生产者:submit 时建行
	ApprovalRoundRunning = "running"
	// ApprovalRoundApproved 本轮通过 —— 生产者:实例终局为 approved
	ApprovalRoundApproved = "approved"
	// ApprovalRoundRejected 本轮被拒 —— 生产者:实例终局为 rejected
	ApprovalRoundRejected = "rejected"
	// ApprovalRoundRolledBack 本轮被打回 —— 生产者:Rollback
	ApprovalRoundRolledBack = "rolled_back"
)

// 关卡审核类型(§4)
const (
	// ApprovalReviewAll 会签 —— 全部审批人都批了才推进
	ApprovalReviewAll = "all"
	// ApprovalReviewAny 或签 —— 首个动作即终局
	ApprovalReviewAny = "any"
)

// 审批人规则类型(§5.1)
const (
	// ApprovalRuleUser 指定人 —— 规则里的 user_id
	ApprovalRuleUser = "user"
	// ApprovalRuleRole 指定角色 —— 规则里的 role_id,展开成该角色下所有用户
	ApprovalRuleRole = "role"
	// ApprovalRuleDeptLeader 部门主管 —— 规则里的 levels,<=0 视作爬到顶
	ApprovalRuleDeptLeader = "dept_leader"
)

// ============================================================================
// ① sys_approval_template —— 审批模板
// ============================================================================

// SysApprovalTemplate 审批模板(定义"这条业务记录要走哪些关卡")
//
// ⚠️ **没有 `version` 列**(D7)。模板一改,历史实例就解释不清了 ——
// 所以有在跑实例时禁止改结构(§7.2),删除时必须同时改写 `code`。
type SysApprovalTemplate struct {
	BaseModel
	// Code 模板编码,业务侧用它引用模板(如 "material_review")
	//
	// ⚠️ **唯一索引不含 `deleted_at`** —— SQL 里 UNIQUE 认为 NULL 互不相等,
	// 而活跃行的 deleted_at 恰好是 NULL,加了它唯一性就完全不生效(本仓
	// `model/file_test.go:76` 有实测)。所以 DELETE 时必须把 code 改成
	// `code#deleted#<id>`,让出这个坑位。
	Code     string `gorm:"size:64;uniqueIndex:uk_app_tpl_code" json:"code"`
	Name     string `gorm:"size:128" json:"name"`
	BizType  string `gorm:"size:64;index" json:"biz_type"`
	IsActive bool   `gorm:"index" json:"is_active"`
	Remark   string `gorm:"size:512" json:"remark"`
}

func (SysApprovalTemplate) TableName() string { return "sys_approval_template" }

// ============================================================================
// ② sys_approval_stage —— 关卡
// ============================================================================

// SysApprovalStage 审批关卡(模板的第 N 关)
type SysApprovalStage struct {
	BaseModel
	// TemplateID 所属模板
	TemplateID uint64 `gorm:"uniqueIndex:uk_app_stage_tpl_idx,priority:1" json:"template_id,string"`
	// StageIndex 关卡序号 —— **从 1 开始且必须连续**
	//
	// ⚠️ `(template_id, stage_index)` 建**唯一**索引:重复序号会让
	// "推进到第 N 关"命中多行,而多一行就意味着这一关的 task 被算两次。
	StageIndex int `gorm:"uniqueIndex:uk_app_stage_tpl_idx,priority:2" json:"stage_index"`
	// Name 关卡名(如 "资料管理员初审")
	Name string `gorm:"size:64" json:"name"`
	// ReviewType 会签(all)还是或签(any)—— 默认 any
	ReviewType string `gorm:"size:16;default:'any'" json:"review_type"`
	//
	// ⚠️ **没有 `rollback_to`**(D6):打回只回发起人,不指定回到哪一关。
}

func (SysApprovalStage) TableName() string { return "sys_approval_stage" }

// ============================================================================
// ③ sys_approval_assignee_rule —— 审批人规则
// ============================================================================

// SysApprovalAssigneeRule 审批人规则(挂在某个关卡上)
//
// ⚠️ **多条规则 = 或的关系** —— 第 1 关配"部门主管 **或** 财务经理",
// 命中任一即可成为该关审批人。
type SysApprovalAssigneeRule struct {
	BaseModel
	// StageID 所属关卡
	StageID uint64 `gorm:"index" json:"stage_id,string"`
	// Seq 展示顺序
	Seq int `json:"seq"`
	// RuleType 规则类型:user / role / dept_leader
	RuleType string `gorm:"size:32" json:"rule_type"`
	// UserID 仅 rule_type=user 时有效
	UserID uint64 `json:"user_id,string"`
	// RoleID 仅 rule_type=role 时有效
	RoleID uint64 `json:"role_id,string"`
	// Levels 仅 rule_type=dept_leader 时有效 —— 向上爬几级,<=0 视作爬到顶
	Levels int `json:"levels"`
}

func (SysApprovalAssigneeRule) TableName() string { return "sys_approval_assignee_rule" }

// ============================================================================
// ④ sys_approval_instance —— 审批实例
// ============================================================================

// SysApprovalInstance 审批实例(一条业务记录的一次审批)
type SysApprovalInstance struct {
	BaseModel
	// BizType 业务类型(如 "digital_material")
	BizType string `gorm:"size:64;uniqueIndex:uk_app_inst_biz,priority:1" json:"biz_type"`
	// BizID 业务记录 ID
	//
	// ⚠️ **(biz_type, biz_id) 是硬唯一**(D9):同一条业务记录只允许有一个审批实例。
	// 这不是"一个人只能发一条公告" —— biz_id 是**那条业务记录自己的 ID**,
	// 每条公告有自己的 id,各占各的坑位。D9 挡的是**同一条记录被审两遍**。
	//
	// ⚠️ 本仓早就知道 MySQL 没有条件唯一索引(`model/user.go:89` 写着
	// "唯一性由 service 层事务保证"),**D9 用硬唯一是顺着仓库既有立场走的**。
	BizID uint64 `gorm:"size:64;uniqueIndex:uk_app_inst_biz,priority:2" json:"biz_id,string"`
	// Title 标题
	Title string `gorm:"size:256" json:"title"`
	// Variables JSON 字符串,只放详情页渲染需要的只读上下文(金额、房号、材料名)
	//
	// ⚠️ **它不受权限过滤**,前端不能拿它做展示判断(§11 R10)。
	Variables string `gorm:"type:json" json:"variables"`
	// TemplateID 使用的模板
	TemplateID uint64 `json:"template_id,string"`
	// ApplicantUserID 发起人
	ApplicantUserID uint64 `gorm:"index:idx_app_inst_applicant,priority:1" json:"applicant_user_id,string"`
	// ApplicantIdentityID 发起时的身份
	ApplicantIdentityID uint64 `json:"applicant_identity_id,string"`
	// Status 实例状态,见 ApprovalStatus* 常量
	//
	// ⚠️ **这个字段挂两个索引**(GORM 支持一个字段多个 `index:` tag):
	//   · idx_app_inst_applicant 的 priority:2 —— 服务 `box=mine`
	//   · idx_app_inst_status_submitted 的 priority:1 —— 服务 cronx 超时巡检
	Status string `gorm:"size:16;index:idx_app_inst_applicant,priority:2;index:idx_app_inst_status_submitted,priority:1" json:"status"`
	// CurrentStage 当前关卡序号(1 起)
	CurrentStage int `json:"current_stage"`
	// ReturnCount 累计被打回次数,上限 5(§3.3)
	ReturnCount int `json:"return_count"`
	// SubmittedAt 提交时间
	SubmittedAt *time.Time `gorm:"index:idx_app_inst_status_submitted,priority:2" json:"submitted_at"`
	// FinishedAt 终局时间
	FinishedAt *time.Time `json:"finished_at"`
	//
	// ⚠️ **本表没有 `Version` 字段** —— §3.5 用悲观锁(实例行 FOR UPDATE)。
	// 加一个 `version` 会让人以为可以走乐观锁,是个误导性字段。
	//
	// ⚠️ **没有 `active_key`** —— D9 改成硬唯一,V1.1 的 NULL 技巧就不需要了。
}

func (SysApprovalInstance) TableName() string { return "sys_approval_instance" }

// ============================================================================
// ⑤ sys_approval_task —— 审批任务
// ============================================================================

// SysApprovalTask 审批任务(某人某关的一次待办)
type SysApprovalTask struct {
	BaseModel
	// InstanceID 所属实例
	InstanceID uint64 `gorm:"index:idx_app_task_inst,priority:1" json:"instance_id,string"`
	// StageIndex 关卡序号
	StageIndex int `gorm:"index:idx_app_task_inst,priority:2" json:"stage_index"`
	// AssigneeUserID 审批人
	//
	// ⚠️ **必须挂复合索引而不是单列** —— 这是全库增长最快的表(每个动作都写),
	// 第一个爆。而 `GET /approvals?box=todo` 是最高频的查询。
	AssigneeUserID uint64 `gorm:"index:idx_app_task_assignee_status,priority:1" json:"assignee_user_id,string"`
	// Status 任务状态,见 ApprovalTask* 常量
	//
	// ⚠️ **守卫查 task 时必须带 `status='pending'`**(§3.4)—— 只查"有没有我的
	// task"的话,`stale` 的上一轮 task 会让打回后的人还能再审一遍。
	Status string `gorm:"size:16;index:idx_app_task_assignee_status,priority:2" json:"status"`
	// ActionedAt 处理时间
	ActionedAt *time.Time `json:"actioned_at"`
	//
	// ⚠️ **没有计数器列**(D10):会签分母在锁内重算,不落库。
	// 计数器一旦漏了锁/事务/Where 条件,就会**静默丢票**(§14.2)。
	//
	// ⚠️ **没有 `comment` 列** —— 意见是 log 的事。详情页一次
	// `WHERE instance_id=?` 就能拿到全部意见,不需要冗余一份。
}

func (SysApprovalTask) TableName() string { return "sys_approval_task" }

// ============================================================================
// ⑥ sys_approval_log —— 审批日志(只 append)
// ============================================================================

// SysApprovalLog 审批日志(意见流水)
//
// 🔴 **这张表只 append,永不 UPDATE / DELETE。`DeletedAt` 字段继承自 BaseModel,
// 但在本表上永远不该有值。**
//
// 原因不是"不会有人删",而是:**`gorm.DeletedAt` 存在,就意味着 GORM 给该 model 的
// 每一条查询自动加 `deleted_at IS NULL`。只要有任何一处代码对 log 调了 `Delete()`,
// 全表意见立刻从详情页消失,而且没有任何报错。**
//
// 防护:① 本注释写死禁令;② `model/approval_test.go` 有一条单测 ——
// `COUNT(*)` 与该实例的 log 总数断言一致,不等就红。
type SysApprovalLog struct {
	BaseModel
	// InstanceID 所属实例
	InstanceID uint64 `gorm:"index:idx_app_log_inst,priority:1" json:"instance_id,string"`
	// StageIndex 发生在第几关
	StageIndex int `json:"stage_index"`
	// RoundNo 第几轮
	//
	// ⚠️ **进了复合索引的 priority:2** —— 详情页是一次
	// `WHERE instance_id=? ORDER BY round_no, id`。InnoDB 的二级索引会隐式带上
	// 主键,所以这个索引实际能供 `(instance_id, round_no, id)` 整条排序用,
	// 不必再排一次。
	//
	// ⚠️ **`CreatedAt` 秒级精度内同刻写入的 log 很多**(一次会签就写一批),
	// 所以排序必须以 `round_no, id` 为主,**不能只按 `created_at`** ——
	// 否则同轮意见顺序会飘。
	RoundNo int `gorm:"index:idx_app_log_inst,priority:2" json:"round_no"`
	// Action 动作:submit / approve / reject / rollback / cancel
	//
	// ⚠️ **`update` 不写 log** —— 它是"改还没送审的东西",记进去意见流里
	// 全是"提交人修改了标题"的噪音(§3.3)。
	Action string `gorm:"size:16" json:"action"`
	// OperatorUserID 操作人
	OperatorUserID uint64 `json:"operator_user_id,string"`
	// OperatorIdentityID 操作时的身份
	OperatorIdentityID uint64 `json:"operator_identity_id,string"`
	// Comment 意见 / 备注
	Comment string `gorm:"size:1000" json:"comment"`
	//
	// ⚠️ **没有 `seq`**(D4):轮次不靠日志分界,靠 task 的 stale 状态。
	// ⚠️ **没有 `attachments`**(§1 第 11 条)。
}

func (SysApprovalLog) TableName() string { return "sys_approval_log" }

// ============================================================================
// ⑦ sys_approval_round —— 审批轮次(纯展示表)
// ============================================================================

// SysApprovalRound 审批轮次快照
//
// ⚠️ **V2.1 新增。它是纯展示表**:判定分母、状态推进一律不读它
// (§4.1 照旧只看 task)。
//
// **三个设计要点**:
//
//  1. **写入时机只有一处**:每次 submit(含重提)开一行。`draft` 阶段改来改去
//     **不产生 round** —— 没送审的东西没有"第几轮"可言。
//  2. **不存 `comment`** —— 打回/拒绝的理由仍然只存在于 `sys_approval_log`
//     (唯一真相源)。前端拿 `rounds[]` 拿结构、拿 `logs[]` 拿意见,按 round_no 拼。
//     **冗余一份评论就要有两个可能写错的地方,而这里省下来的只是一个 join。**
//  3. **打回时把本轮 outcome 写成 `rolled_back`,不改实例历史** —— 之后 submit
//     开 `round_no+1`。`UNIQUE(instance_id, round_no)` 保证并发重提撞不出两行。
type SysApprovalRound struct {
	BaseModel
	// InstanceID 所属实例
	InstanceID uint64 `gorm:"uniqueIndex:uk_app_round_inst,priority:1" json:"instance_id,string"`
	// RoundNo 轮次序号 —— **从 1 开始,连续,不跳号**
	RoundNo int `gorm:"uniqueIndex:uk_app_round_inst,priority:2" json:"round_no"`
	// Title 本轮提交时的标题快照
	Title string `gorm:"size:256" json:"title"`
	// Variables 本轮提交时的内容快照
	Variables string `gorm:"type:json" json:"variables"`
	// SubmitterID 本轮提交人
	SubmitterID uint64 `gorm:"index" json:"submitter_id,string"`
	// SubmittedAt 本轮提交时间
	SubmittedAt time.Time `json:"submitted_at"`
	// Outcome 本轮结果:running / approved / rejected / rolled_back
	Outcome string `gorm:"size:16;index" json:"outcome"`
	// OutcomeBy 终局操作人
	OutcomeBy uint64 `json:"outcome_by,string"`
	// OutcomeAt 终局时间
	OutcomeAt *time.Time `json:"outcome_at"`
	//
	// ⚠️ **`DeletedAt` 在本表同样不该有值**,理由同 ⑥。
}

func (SysApprovalRound) TableName() string { return "sys_approval_round" }
