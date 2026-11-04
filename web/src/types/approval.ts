/**
 * 通用审批系统 —— 类型定义(M87)
 *
 * 文档:docs/通用审批系统设计方案V1.0.md §7.5 / §8.2 / §9.1
 *
 * ⚠️ **字段名全部 snake_case** —— 后端 DTO 的 json tag 就是 snake_case。
 *   Go 的 `encoding/json` 大小写不敏感但**认不出下划线**,前端再写驼峰
 *   就是"绑不上"(仓库历史踩过:commit f91b5a3 / 90b771d)。
 *
 * ⚠️ **所有 ID 是 string** —— 雪花 ID 是 19 位,超出 JS 的 2^53,
 *   裸数字会被 JSON.parse 静默截断(§7.5 硬要求)。
 */
import { PageRequest } from './api'

/* ====================== 枚举 ====================== */

/** 列表的三个盒子(§8.2)—— **没有 `all`**
 *
 * ⚠️ **前端不要加 `box=all`** —— 服务端会直接报错。加了它,
 *   广发 `approval:read` 立刻变成越权(§12 第 9 条)。
 */
export type ApprovalBox = 'todo' | 'mine' | 'done'

/** 实例状态(§16.3) */
export type ApprovalStatus =
  | 'draft'
  | 'running'
  | 'returned'
  | 'approved'
  | 'rejected'
  | 'cancelled'

/** 任务状态(§16.3) */
export type ApprovalTaskStatus =
  | 'pending'
  | 'approved'
  | 'rejected'
  | 'skipped'
  | 'stale'

/** 关卡的通过方式(§5.1) */
export type ApprovalReviewType = 'all' | 'any'

/** 审批人来源(§5.1)—— 三种互斥 */
export type ApprovalRuleType = 'user' | 'role' | 'dept_leader'

/** 审批动作(§3.3)—— **端点的 action 只有这三个**
 *
 * ⚠️ `submit` / `cancel` 不在这里 —— 它们是**另外的端点**,
 *   只是 log 里也用这两个名字记流水。
 */
export type ApprovalAction = 'approve' | 'reject' | 'rollback'

/** 轮次结果(§6.1 ⑦) */
export type ApprovalRoundOutcome =
  | 'running'
  | 'approved'
  | 'rejected'
  | 'rolled_back'

/** 流水里的动作名(含 submit / cancel) */
export type ApprovalLogAction = ApprovalAction | 'submit' | 'cancel'

/* ====================== 事件(§9.2) ====================== */

/**
 * 推送事件的 action —— **与 `pkg/push.ApprovalPayload.Action` 同名同值**
 *
 * ⚠️ **`rolled_back` 是 M88 加的** —— 打回是一种"办完了",但单子回到
 *   发起人手里,不是终局。混进 `approved` 会让待办角标减错。
 *
 * ⚠️ **两侧必须同一个 commit 改**:服务端 `pkg/push/event.go` 的注释 +
 *   本文件 + 设计文档 §9.2。事件是广播契约,少改一边就成两套词表。
 */
export type ApprovalEventAction =
  | 'assigned'
  | 'approved'
  | 'rejected'
  | 'rolled_back'
  | 'cancelled'

/** 推送事件负载(§9.2)
 *
 * ⚠️ **白名单** —— 服务端只带"够前端判断要不要重拉"的字段,
 *   事由 / 意见 / 金额一律不带,前端自己回查详情接口。
 */
export interface ApprovalEventPayload {
  /** 审批单 ID —— 前端跳 `/approval/{todo_id}` */
  todo_id: string
  /** todo / approval —— 同一通道上区分两类 */
  kind: 'todo' | 'approval'
  action: ApprovalEventAction
  /** 当前处理人(user_id,string)—— **只有"新待办"才非零** */
  assignee_id?: string
}

/* ====================== 审批单 ====================== */

/** 列表项(§7.5) */
export interface ApprovalListItem {
  id: string
  biz_type: string
  /** 业务记录 ID —— 前端拿它跳回业务详情 */
  biz_id: string
  title: string
  status: ApprovalStatus
  /** 当前关卡序号(0 = 未进入任何关卡) */
  current_stage: number
  /** 当前关卡名 —— 列表页要显示"卡在谁那儿" */
  current_stage_name: string
  applicant_user_id: string
  applicant_name: string
  /** 累计被打回次数 */
  return_count: number
  /** 我和这条单的关系:pending / approved / rejected / 空
   *
   * ⚠️ **空 = 我不是审批人**。按钮可见性由这个字段决定,
   *   **不由 RBAC 决定**(§8.1)—— 有权限不等于该你审。
   */
  my_task_status: ApprovalTaskStatus | ''
  template_code: string
  template_name: string
  created_at: string
  updated_at: string
  submitted_at: string | null
  finished_at: string | null
}

/** 审批人视图 */
export interface ApprovalAssigneeView {
  user_id: string
  name: string
  status: ApprovalTaskStatus
  actioned_at: string | null
}

/** 关卡视图 */
export interface ApprovalStageView {
  stage_index: number
  name: string
  review_type: ApprovalReviewType
  /** 展示状态:done / current / rejected / pending
   *
   * ⚠️ **纯展示用,不是判定依据** —— 判定一律看 task 表(§4.1)。
   */
  status: 'done' | 'current' | 'rejected' | 'pending'
  total_count: number
  /** 已通过人数(§9.1 硬要求 2:"共 N 人,已批 M 人") */
  approved_count: number
  assignees: ApprovalAssigneeView[]
}

/** 轮次快照(§6.1 ⑦) */
export interface ApprovalRoundView {
  round_no: number
  /** 本轮提交时的标题快照 */
  title: string
  /** 本轮提交时的事由快照 */
  variables: string
  submitter_id: string
  submitted_at: string
  outcome: ApprovalRoundOutcome
  outcome_by: string
  outcome_at: string | null
}

/** 意见流水(§6.1 ⑥) */
export interface ApprovalLogView {
  id: string
  stage_index: number
  round_no: number
  action: ApprovalLogAction
  operator_user_id: string
  operator_name: string
  comment: string
  created_at: string
}

/** 详情(§7.5)—— `rounds[]` 和 `logs[]` 一次返回 */
export interface ApprovalDetail extends ApprovalListItem {
  /** 事由 / 上下文(JSON 文本)
   *
   * ⚠️ **它不受权限过滤** —— 前端不能拿它做展示判断(§11 R10)。
   */
  variables: string
  template_id: string
  applicant_identity_id: string
  stages: ApprovalStageView[]
  rounds: ApprovalRoundView[]
  logs: ApprovalLogView[]
  /** 我能不能在这条单上做审批动作
   *
   * ⚠️ **给前端隐藏按钮用的,不是安全边界** —— 后端 `Act` 会再算一遍。
   */
  can_act: boolean
}

/** 列表查询 */
export interface ApprovalListQuery extends PageRequest {
  /** 必填:todo / mine / done */
  box: ApprovalBox
  biz_type?: string
  status?: string
  keyword?: string
}

/** 动作入参(§7.4) */
export interface ActReq {
  action: ApprovalAction
  /** 驳回/打回必填,且**不少于 5 个字**(§3.4) */
  comment?: string
}

/** 改草稿入参(§7.4)—— **三个字段都是指针式语义,nil = 不改** */
export interface UpdateDraftReq {
  title?: string
  variables?: string
  template_code?: string
}

/* ====================== 流程模板 ====================== */

/** 模板列表项 */
export interface FlowListItem {
  id: string
  code: string
  name: string
  biz_type: string
  is_active: boolean
  remark: string
  stage_count: number
  created_at: string
  updated_at: string
}

/** 规则视图 */
export interface FlowRuleView {
  id: string
  seq: number
  rule_type: ApprovalRuleType
  user_id: string
  role_id: string
  levels: number
}

/** 关卡视图 */
export interface FlowStageView {
  id: string
  stage_index: number
  name: string
  review_type: ApprovalReviewType
  rules: FlowRuleView[]
}

/** 模板详情 */
export interface FlowDetail {
  id: string
  code: string
  name: string
  biz_type: string
  is_active: boolean
  remark: string
  stages: FlowStageView[]
}

/** 模板列表查询 */
export interface FlowListQuery extends PageRequest {
  keyword?: string
  biz_type?: string
  /** 三态:不传 = 不过滤,传 false = 只看停用的 */
  is_active?: boolean
}

/* ====================== 模板保存入参 ====================== */

/** 一条审批人规则(§5.1)
 *
 * ⚠️ **按 rule_type 只填对应的那一个 ID** —— 另两个必须留 0。
 *   留了非 0 的那个会被服务端判成"规则配错"(三种来源互斥)。
 */
export interface FlowRuleInput {
  rule_type: ApprovalRuleType
  user_id?: string
  role_id?: string
  /** dept_leader 往上爬几级(<=0 视作爬到顶) */
  levels?: number
}

/** 一个关卡 */
export interface FlowStageInput {
  name: string
  review_type?: ApprovalReviewType
  rules: FlowRuleInput[]
}

/** 模板保存入参(§7.2)
 *
 * ⚠️ **这是"整棵树一次写进去"** —— 后端 `PUT /flows/:id` 会
 *   **硬删旧关卡再整棵重写**(不是增量改某一级)。
 */
export interface FlowSaveReq {
  /** 建模板时必填;小写字母/数字/下划线,必须以字母开头 */
  code?: string
  name: string
  biz_type: string
  remark?: string
  is_active?: boolean
  /** 1~10 个关卡,每个关卡至少一条规则 */
  stages: FlowStageInput[]
}
