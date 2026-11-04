// 通用审批系统 —— 服务层接口 + DTO
// 文档:docs/通用审批系统设计方案V1.0.md §16.2 / §3.3 / §7
//
// ⚠️ **审批引擎本体不认识任何业务表** —— 它只认 `biz_type` + `biz_id`,
// 业务侧的事由 `BizHandler` SPI 接进来(§16.2)。所以这个包不 import 任何
// 业务 model,换业务线不用改引擎一行代码。
//
// ⚠️ **模板 CRUD 不在本接口里**(§16.2 原稿把 `CreateFlow` / `UpdateFlow` /
// `DeleteFlow` 和实例流转写在同一个 `ApprovalEngine` 上)。落地时拆开:
// 模板 CRUD 归 `ApprovalFlowService`,随 M86 的 API 层一起落。理由是
// **它一个调用方都没有**(M86 之前没人调),而本仓的规矩明确写着
// "给零调用方的导出类型加字段 = 冗余"。等 M86 把它接上再合并不迟。
package service

import (
	"context"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// ============================================================================
// 错误
// ============================================================================

// 审批域错误。**不混进 ErrBadRequest / ErrForbidden** ——
// 「流程配置有误」和「你调错了参数」是两类事故,前端要弹的东西不一样(§7.5)。
//
// ⚠️ **有对应常量的全部引用 errcode 常量,不用裸数字** —— 裸数字哪天
// 被改段位,这里会静默变成另一个语义的错误码,而 20601 这种"配置坏了"
// 被误分类成 40003 的后果是前端弹"参数有误"、用户永远等不到人来修。
var (
	// ErrApprovalStateInvalid 状态机不允许(§3.4 守卫第 1 条)
	ErrApprovalStateInvalid = errcode.New(errcode.StateInvalid, "当前状态不允许此操作")
	// ErrApprovalForbidden 不是本关待办人(§3.4 守卫第 2 条)
	ErrApprovalForbidden = errcode.New(errcode.Forbidden, "你不是本关的待办人")
	// ErrApprovalSameAuditor 提交人 = 审批人(D11)
	ErrApprovalSameAuditor = errcode.New(errcode.SameAuditor)
	// ErrApprovalCommentShort 意见不足 5 个字符(§3.4)
	ErrApprovalCommentShort = errcode.New(errcode.ParamInvalid, "驳回/打回必须写明理由,至少 5 个字")
	// ErrApprovalNoChange 原样重提(§3.3 新守卫)
	ErrApprovalNoChange = errcode.New(errcode.InvalidParam, "重提必须修改内容:与本轮提交完全一致的单子会被退回")
	// ErrApprovalNoTemplate 该 biz_type 没有启用的模板(§7.3)
	ErrApprovalNoTemplate = errcode.New(errcode.ApprovalNoTemplate)
	// ErrApprovalNoAssignee 解析不出审批人(§5.3)。**不降级、不自动升级给管理员**
	ErrApprovalNoAssignee = errcode.New(errcode.ApprovalNoAssignee)
	// ErrApprovalCorrupt 流程自身已损坏(§4.1 空集防御)
	ErrApprovalCorrupt = errcode.New(errcode.Internal, "审批流程数据异常:本关没有有效审批人,请联系管理员")
	// ErrApprovalTooManyReturns 打回次数用尽(§3.3 上限 5)
	ErrApprovalTooManyReturns = errcode.New(errcode.StateInvalid, "本单已被打回 5 次,请重新发起")
	// ErrApprovalLocked 实例行锁等待超时(§3.5 第 3 条)
	ErrApprovalLocked = errcode.New(errcode.StateInvalid, "审批处理中,请稍后重试")
	// ErrApprovalDeptTreeBroken 部门树成环 / parent_id 指向不存在的部门(§5.2 第 2 条,M85)
	ErrApprovalDeptTreeBroken = errcode.New(errcode.ApprovalDeptTreeBroken)
	// ErrApprovalRuleInvalid 规则本身坏了:未知 rule_type / role 规则指向不存在的角色(M85)
	ErrApprovalRuleInvalid = errcode.New(errcode.ApprovalRuleInvalid)
	// ErrApprovalInstanceExists 同一条业务记录已经在审批流里了(D9 硬唯一索引,M88-5)
	//
	// ⚠️ **这条错有两种产生方式,两种必须落成同一个码**:
	//   1. 引擎 INSERT 时撞 `uk_app_inst_biz`(**权威**):两个请求同时建草稿,
	//      预检查都通过了,其中一个 INSERT 撞唯一索引。数据库只说
	//      "Duplicate entry 'x-y' for key 'uk_app_inst_biz'",用户看不懂,
	//      而且它落到 handler 里是 **500** —— 前端弹"服务器开小差了"。
	//   2. HTTP 通道的**预检查**(可读性优先,但有 TOCTOU 窗口)。
	// 所以**引擎里必须把唯一约束冲突翻译成这个码**,否则第 1 种情况永远
	// 漏成 500。§13 M86-2h 记的就是这条。
	//
	// ⚠️ **用 Conflict(409)而不是新开一个 206xx** —— 预检查那条路早就在用
	// Conflict 了,新开一个码会让同一个语义有两个码,前端要判两遍。
	ErrApprovalInstanceExists = errcode.New(errcode.Conflict, "该业务记录已经发过审批了,不能重复发起")
)

// ============================================================================
// 引擎接口
// ============================================================================

// ApprovalEngine 审批引擎 —— **实例生命周期**
//
// ⚠️ **写动作一律收 `id` 不收 `task_id`**(§3.3):待办由服务端按
// `(instance_id, current_stage, caller)` 定位。少一个可被篡改的参数。
type ApprovalEngine interface {
	// CreateDraft 建草稿。
	//
	// ⚠️ **收 `tx` 是故意的**(D8):业务记录和审批单必须同事务 —— 业务记录建了
	// 而审批单没建,就是一条永远没人审的孤儿数据。
	// 带 `tx` 的方法**不能自己开事务,也不能发事件**。
	CreateDraft(ctx context.Context, tx *gorm.DB, in *CreateDraftInput) (uint64, error)

	// UpdateDraft 改还没送审的内容。**不是审批动作,不写 log**(§3.3)
	UpdateDraft(ctx context.Context, caller *Caller, id uint64, req UpdateDraftReq) error

	// Submit 提交。首次提交 / 打回后重提走同一个方法,守卫不同(§3.3 的 7 行表)
	Submit(ctx context.Context, caller *Caller, id uint64, req SubmitReq) error

	// Act 审批动作。approve / reject / rollback 三合一,共用同一个端点(§3.3)
	Act(ctx context.Context, caller *Caller, id uint64, req ActReq) error

	// Cancel 作废。只在 draft / returned 下允许;**running 不给**(D9)
	Cancel(ctx context.Context, caller *Caller, id uint64) error
}

// ============================================================================
// DTO
// ============================================================================

// CreateDraftInput 建草稿入参
type CreateDraftInput struct {
	BizType             string `json:"biz_type"`
	BizID               uint64 `json:"biz_id,string"`
	Title               string `json:"title"`
	Variables           string `json:"variables"` // JSON 字符串,调用方自己序列化
	TemplateCode        string `json:"template_code"`
	ApplicantUserID     uint64 `json:"applicant_user_id,string"`
	ApplicantIdentityID uint64 `json:"applicant_identity_id,string"`
}

// UpdateDraftReq 改草稿
//
// ⚠️ **三个字段都是可选的指针式语义**:零值表示"不改",而不是"清空"。
// `title` 想清空得显式传空串 + 一个哨兵,这一点在 M86 的 handler 里定。
type UpdateDraftReq struct {
	Title        *string `json:"title"`
	Variables    *string `json:"variables"`
	TemplateCode *string `json:"template_code"`
}

// SubmitReq 提交
type SubmitReq struct {
	// Comment 提交说明,**选填**(§3.4)。只有驳回/打回才强制 5 字。
	Comment string `json:"comment"`
}

// ActReq 审批动作
type ActReq struct {
	// Action approve / reject / rollback —— **只有这三个**(§3.3)
	Action string `json:"action"`
	// Comment 意见。reject / rollback 必填且 ≥5 rune;approve 选填
	Comment string `json:"comment"`
}

// 动作常量(与 `model.Approval*` 的状态常量不是一回事,别混)
//
// ⚠️ **前三个是 `ActReq.Action` 的取值(§7.4 白名单),后两个只出现在 log 表里**
// —— log 的 action 词表比端点多两个(`submit` / `cancel`,§6①)。
// 两套词表故意分开:端点的 `action` 里出现 `cancel` 一定是拼错了,
// 而 log 里出现 `cancel` 是正常的。
const (
	ActApprove  = "approve"
	ActReject   = "reject"
	ActRollback = "rollback"

	// ActSubmit 写进 log 的动作名。**它不是端点 action**
	ActSubmit = "submit"
	// ActCancel 写进 log 的动作名
	ActCancel = "cancel"
)

// MaxReturnCount 打回次数上限(§3.3)。再高就没意义了 ——
// 单子被同一个流程打回 5 次说明流程本身配错了,该改模板而不是继续试。
const MaxReturnCount = 5

// MinCommentRune reject / rollback 的意见最少字符数(§3.4)
//
// ⚠️ **按 rune 计不是字节** —— "不同意。"是 4 字符 12 字节,
// 按字节算会把这个合理的意见判成够长。
const MinCommentRune = 5

// ApprovalListBox 列表的三个盒子(§8.2)
type ApprovalListBox string

const (
	// BoxTodo 我有待办
	BoxTodo ApprovalListBox = "todo"
	// BoxMine 我发起的
	BoxMine ApprovalListBox = "mine"
	// BoxDone 我处理过的
	BoxDone ApprovalListBox = "done"
)

// ⚠️ **没有 `box=all`**(§8.2)。每个盒子都由「我和这条单的关系」定义,
// 结构上不可能越权 —— 这是砍掉全量列表换来的好处。
// ⚠️ **加它的那一天,§8.4 的广发 `approval:read` 立刻变成越权**(§12 第 9 条),
// 两件事必须同一个 commit。

// ============================================================================
// BizHandler SPI —— 业务侧注册,框架只定义
// ============================================================================

// BizHandler 业务侧要实现的 SPI。
//
// ⚠️ **由 M86 的 material 那条老路径实现**(替换 `asset_impl.go:436-458`)。
// 框架侧**不注册任何内置实现** —— 引擎不认识业务表,这不是缺陷。
//
// 🔴 **三个方法都收 `tx`,这是对 §16.2 原稿的改签,不是笔误。**
// 原稿是 `OnApproved(ctx, inst) error` —— 那样实现方只能拿自己持有的 `db`
// 去写,那是**另一条连接、另一个事务**:实例回滚了业务写不会回滚,业务写成了
// 实例回滚不了。D8("与审批同事务,失败整体回滚")在这个签名下**根本无法实现**。
// 收 `tx` 是 D8 唯一能落地的写法,代价是实现方必须用它而不是自己那个 db。
type BizHandler interface {
	// ValidateBiz 建草稿前校验业务记录存在且允许发起审批(**在建审批单之前**)
	ValidateBiz(ctx context.Context, tx *gorm.DB, bizType string, bizID uint64) error
	// OnApproved 审批通过。**在审批事务内调用**,返错 = 整体回滚(D8)
	OnApproved(ctx context.Context, tx *gorm.DB, inst *model.SysApprovalInstance) error
	// OnRejected 审批被拒。同样在事务内,同样用传进来的 tx
	OnRejected(ctx context.Context, tx *gorm.DB, inst *model.SysApprovalInstance) error
	// OnCancelled 审批单被发起人作废。同样在事务内。
	//
	// ⚠️ **这是 M86-2 加的第 4 个方法** —— 原稿只有三个。
	// 加它的理由:**作废是一个会改业务状态的终局动作**(§3.2 `cancelled` 是终态),
	// 而业务记录不会因为审批单作废而自己变回去。没有这个钩子的话,
	// 资料会永远停在"待审核"—— 审批单没了,资料还在等一个不会来的审批。
	//
	// ⚠️ **不回调的情形只有一个**:`biz_type` 没注册处理器 —— 那时连草稿都建不出来,
	// 不存在"有实例但没处理器"的状态。
	OnCancelled(ctx context.Context, tx *gorm.DB, inst *model.SysApprovalInstance) error
}

// ============================================================================
// 审批人解析 SPI(M85 实现)
// ============================================================================

// ResolveStageRequest 解析某一关的审批人
type ResolveStageRequest struct {
	TemplateID          uint64
	StageID             uint64
	StageIndex          int
	ApplicantUserID     uint64
	ApplicantIdentityID uint64
}

// AssigneeResolver 审批人解析器。
//
// ⚠️ **收 `db` 而不是只收 `ctx`** —— §5.3① 的配置期预检要在**事务外**
// (保存模板时)解析一次,提交期要在**事务内**再解析一次。给一个只认 ctx
// 的接口,配置期那条路就得自己开事务,反而更容易写错。
type AssigneeResolver interface {
	// ResolveStage 返回该关的审批人 user_id 列表(**可以为空,但不能是"报错"**)
	//
	// ⚠️ **"解析失败"必须用 error 表达,不能用空列表表达** —— 空列表会被
	// §5.3 当成"暂时没人",而 error 才是"配置坏了"。把两者混成空列表,
	// 就回到了"静默把单子发给一个无关的人"这个 V1 明确不采用的策略。
	ResolveStage(ctx context.Context, db *gorm.DB, req ResolveStageRequest) ([]uint64, error)
}

// ============================================================================
// 事件出口(M88 装配 push 实现)
// ============================================================================

// ApprovalEvent 一条审批通知。
//
// ⚠️ **进 wire 的字段与 `pkg/push.ApprovalPayload` 一一对应,不多不少**(§9.2)。
// 事件是广播的,字段一旦上线就是所有人的契约 —— 想带"事由 / 金额 / 意见"就得先
// 想清楚越权边界(§11 R10)。前端拿这个只判断"要不要重拉",正文自己回查详情接口。
//
// ⚠️ **不带 `comment`** —— 与 `push/event.go:117-122` 的白名单纪律一致。
// 审批意见常含内部口径,而"能看列表"和"该看这条的意见"未必同一个权限粒度。
//
// ⚠️ **唯一的例外是 `Result`**(M3 新增)—— 它**不广播**,只活在引擎 → publisher
// 这段进程内接口上。冻结的是 wire 契约,不是内部接口。
type ApprovalEvent struct {
	// InstanceID 审批单 ID,即 `push.ApprovalPayload.TodoID`。前端跳 /approval/{id}
	InstanceID uint64
	// Action 见下方动作常量 —— **直接就是 `ApprovalPayload.Action` 的取值**
	Action string
	// AssigneeID 这条通知针对谁。
	//
	// ⚠️ **只有"新待办"才非零**(逐个审批人各一条,§9.2);
	// 审批动作 / 作废一律 0 —— 那不是"发给某个人"的,谁关心谁自己去拉。
	AssigneeID uint64
	// Result 这条事件是不是一个**结果**(M3 新增)。
	//
	// 🔴 **它解决的是「approved 有两种语境」这个歧义**:会签里"这一票记下了"
	// 与整单通过**用的是同一个 action**(`approved`),事件本身分不出来
	// (§9.2 那张表写的就是同一条)。而"要不要给发起人发一条'审批已通过'"
	// **必须**分得出这两种 —— 会签到一半就报"审批已通过"是**假消息**,
	// 比不报更糟(用户会以为单子已经批完了)。
	//
	// ⚠️ **只有引擎知道答案**:引擎发事件的那一刻就在事务里,它知道这次 approve
	// 是"最后一票"还是"还有别人没批"。旁路(读实例 status 猜)有竞态 ——
	// 会签的中间票与最后一票可能前后脚投递,谁先被读到是随机的,
	// 于是要么漏发要么重发。**这件事不能靠猜。**
	//
	// ⚠️ **这个字段不进 wire** —— `buildApprovalEvent` 不映射它。
	// 广播出去的仍然是 InstanceID / Action / AssigneeID 三个字段,一字不多。
	//
	// 语义:
	//   · `assigned` → false(这是"新待办",收件人是审批人,不是发起人)
	//   · `cancelled` → false(发起人自己做的,不用通知自己)
	//   · 会签中间票的 `approved` → false(还没定局)
	//   · 整单通过的 `approved` → true
	//   · `rejected` / `rolled_back` → true(终局 / 结果,发起人必须知道)
	Result bool
}

// 事件 action 常量 —— 与 `push.ApprovalPayload.Action` 同名同值,
// 前端拿 `action` 分流,两侧必须一起改。
//
// ⚠️ **`rolled_back` 是对 M73 契约的增量**(§9.2),M88 必须同一个 commit 改
// `pkg/push/event.go` 的注释和 `web/src/types/` 的联合类型。
const (
	// EvApprovalAssigned 新待办 —— submit / 重提 / 推进到下一关时**逐个审批人**各发一条
	EvApprovalAssigned = "assigned"
	// EvApprovalApproved 审批通过
	EvApprovalApproved = "approved"
	// EvApprovalRejected 审批拒绝
	EvApprovalRejected = "rejected"
	// EvApprovalRolledBack 打回
	EvApprovalRolledBack = "rolled_back"
	// EvApprovalCancelled 发起人作废
	EvApprovalCancelled = "cancelled"
)

// ApprovalPublisher 审批事件出口。
//
// ⚠️ **引擎只在 COMMIT 之后调用它,且 ctx 一定是 `context.WithoutCancel(ctx)`**
// (§9.2 / §16.5):调用方的 ctx 可能已经死了(HTTP 请求超时 / 上游断开),
// 拿它发事件等于拿一个已取消的 ctx 去写 Redis —— 表现是「审批成功了但前端不刷新」。
//
// ⚠️ **设计文档 §9.2 把原因写成「GORM 在 COMMIT 之后 cancel 事务的 ctx」——
// 这一句在 gorm@v1.31.1 上不成立**(核实过:该版本全仓没有 `WithoutCancel`,
// 也没有对事务 ctx 的 cancel)。结论对、理由假 —— 别照抄理由。
//
// ⚠️ **goroutine + recover 归实现方(M88),不在引擎里。** 引擎同步调用,这样
// 单测能同步断言发了什么;真发网络请求时再由实现方 `go func(){defer recover()}`。
// 反过来(引擎里起 goroutine)会让每条用例都得 sleep 轮询,而且 goroutine 里
// 逃出去的 panic 会**打死整个测试进程**而不是被 recover 掉。
//
// nil = 不发事件(单测与降级部署)。**nil 检查在引擎里**,不甩给调用方。
type ApprovalPublisher interface {
	PublishApproval(ctx context.Context, ev ApprovalEvent)
}
