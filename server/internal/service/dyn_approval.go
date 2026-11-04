// 动态业务集合 × 通用审批系统(2026-11-03)
//
// # 这一层做什么
//
// 集合可绑定一条审批模板(sys_dyn_collection.approval_template_id);
// 绑定后,集合的记录可以「送审」—— 引擎接管审批,记录本身不动。
//
// # 关键设计决定
//
// ① **biz_type 恒为 "dyn",不按集合拆**(区别于 digital_material)——
//    引擎的 handler 注册表在**启动时**装配,而集合是**运行时**建的,
//    "每集合一个 biz_type" 就得动态注册,注册表那把锁就成了所有审批动作的
//    争用点。恒用 "dyn" 后注册表静态:main.go 注册一次,全集合通用。
//    模板歧义靠 CreateDraftInput.TemplateCode **显式传绑定的模板 code** 解决
//    (引擎对"同一 biz_type 多条启用模板"拒绝猜,必须显式给 code —— 正好)。
//
// ② **审批结果不回写业务表** —— dyn 记录的数据本来就已经在 dyn_<code> 表里,
//    审批状态以 sys_approval_instance 为准,记录列表/详情按 (biz_type, biz_id)
//    连出来(RecordApprovalBadges)。要"通过后把某字段写成某值"的回写语义,
//    需要先设计字段级契约(哪个字段、写什么值),留给后续版本。
//
// ③ **送审 = 建草稿 + 提交一步走**(接口层面一步,内部两步)——
//	material 是两步(建草稿后给"预览审批链"窗口),因为它的数据提交后还要改;
//	dyn 记录的数据在送审前就已经落库定型,草稿窗口没有东西可补,
//	两步只是多一次点击。**这不是偷懒,是两个业务形态的差别。**
//	⚠️ 内部实现是「草稿事务先落袋,再调 Submit」,**不能**把 Submit 包进
//	草稿的外层事务 —— Submit 自己再开事务,嵌套 = SQLite 整库死锁
//	(2026-11-03 实测,见 SubmitRecordApproval 里的注释与回归用例)。
//
// ④ handler 是**无状态空转**(四个方法都 no-op)——
//    业务数据不用动,引擎的 D9(同记录唯一审批单)已经挡住重复送审。
//    之所以必须注册,是因为引擎对未注册 biz_type 连草稿都不给建(R11 的硬防线,
//    见 approval_engine_impl.CreateDraft)—— 这道防线不能为 dyn 开洞。
package service

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// DynBizType 动态集合记录的审批业务类型(恒为 "dyn",见文件头 ①)
const DynBizType = "dyn"

// ============================================================================
// BizHandler(注册进引擎)
// ============================================================================

// dynApprovalHandler dyn 的业务处理器 —— 四个钩子全部 no-op(见文件头 ④)
type dynApprovalHandler struct{}

var _ BizHandler = (*dynApprovalHandler)(nil)

// NewDynApprovalHandler 构造(main.go 装配期注册)
func NewDynApprovalHandler() BizHandler { return &dynApprovalHandler{} }

// ValidateBiz 校验 —— 记录存在性在 SubmitRecordApproval 里已经查过
// (它认识集合,handler 不认识),这里无事可做。
func (h *dynApprovalHandler) ValidateBiz(
	ctx context.Context, tx *gorm.DB, bizType string, bizID uint64,
) error {
	if bizType != DynBizType {
		return errcode.New(errcode.ParamInvalid, "不支持的业务类型:"+bizType)
	}
	return nil
}

// OnApproved / OnRejected / OnCancelled 审批终局不回写业务表(见文件头 ②)。
// 记日志而不是静默 —— 排查"这单子批了怎么没反应"时,日志里得能找到答案。
func (h *dynApprovalHandler) OnApproved(ctx context.Context, tx *gorm.DB, inst *model.SysApprovalInstance) error {
	log.Printf("[dyn-approval] 实例 %d(biz_id=%d)审批通过 —— dyn 记录不回写,状态见审批单", inst.ID, inst.BizID)
	return nil
}

func (h *dynApprovalHandler) OnRejected(ctx context.Context, tx *gorm.DB, inst *model.SysApprovalInstance) error {
	log.Printf("[dyn-approval] 实例 %d(biz_id=%d)审批被拒 —— dyn 记录不回写,状态见审批单", inst.ID, inst.BizID)
	return nil
}

func (h *dynApprovalHandler) OnCancelled(ctx context.Context, tx *gorm.DB, inst *model.SysApprovalInstance) error {
	log.Printf("[dyn-approval] 实例 %d(biz_id=%d)已作废 —— 记录可重新送审", inst.ID, inst.BizID)
	return nil
}

// ============================================================================
// 送审
// ============================================================================

// validateApprovalTemplate 集合绑定的模板必须存在且启用(0 = 未绑定,放行)
func (s *dynService) validateApprovalTemplate(ctx context.Context, id uint64) error {
	if id == 0 {
		return nil
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.SysApprovalTemplate{}).
		Where("id = ? AND is_active = ?", id, true).
		Count(&n).Error; err != nil {
		return errcode.New(errcode.Internal, "查询审批模板失败").WithDetail(err.Error())
	}
	if n == 0 {
		return errcode.New(errcode.ParamInvalid, "绑定的审批模板不存在或已停用")
	}
	return nil
}

// SubmitRecordApproval 把一条记录送进审批流
//
// 前置:集合绑定了模板、记录存在(且 caller 按行级权限看得见)、
// 这条记录还没有在审批流里(引擎 D9 兜底,这里预检给个能读懂的错)。
func (s *dynService) SubmitRecordApproval(
	ctx context.Context, caller *Caller, code string, recordID uint64,
) (uint64, error) {
	if _, err := requireUser(caller); err != nil {
		return 0, err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return 0, err
	}
	if rc.col.ApprovalTemplateID == 0 {
		return 0, errcode.New(errcode.ParamInvalid, "该集合未绑定审批模板,无法送审")
	}
	if s.engine == nil {
		return 0, errcode.New(errcode.Internal, "未装配审批引擎,无法送审")
	}
	// 记录必须存在且 caller 看得见(recordScope 里含 owner_scoped 判定)——
	// 别人看不见的记录,送审按钮对他是不可达的,这里再拦一道防直连接口。
	ok, err := s.recordExists(ctx, rc, caller, recordID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errcode.New(errcode.NotFound, "记录不存在")
	}
	// 模板必须还启用着 —— 集合绑定时校验过一次,但模板之后可能被停用
	var tpl model.SysApprovalTemplate
	if err := s.db.WithContext(ctx).
		Where("id = ? AND is_active = ?", rc.col.ApprovalTemplateID, true).
		First(&tpl).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, errcode.New(errcode.ParamInvalid, "绑定的审批模板已停用,请先在集合设置里换一条")
		}
		return 0, errcode.New(errcode.Internal, "查询审批模板失败").WithDetail(err.Error())
	}

	var instID uint64
	// ⚠️ **只有 CreateDraft 在事务里,Submit 必须放在事务外面** ——
	// Submit 的实现在 withLockedInstance 里**自己再开一个事务**(e.db)。
	// 把它包进这里的外层事务 = 两个连接嵌套等锁:外层事务拿着写锁等 Submit
	// 返回,Submit 的新事务反过来等外层事务的锁 —— SQLite 上直接死锁,
	// 整库焊死,server 所有请求一起 30s 超时(2026-11-03 教程 E2E 实测,
	// dev.db-journal 热着只剩重启);Postgres 上也过不了:Submit 读不到
	// 外层未提交的实例行,报「审批单不存在」。资料线(asset_impl.go)一直是
	// 两步走,这里对齐。
	//
	// 草稿建好、提交失败会留一个 draft 实例 —— 重试送审即可,不是孤儿数据:
	// dyn 的业务记录在送审前就已经落库,不存在「先建业务行再建审批单」的
	// 同生共死问题,D8 的本意不破。
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		id, err := s.engine.CreateDraft(ctx, tx, &CreateDraftInput{
			BizType: DynBizType,
			BizID:   recordID,
			// 标题给"集合名 #记录id" —— 审批人的待办列表里得认得出这是哪条
			Title:               fmt.Sprintf("%s #%d", rc.col.Name, recordID),
			TemplateCode:        tpl.Code,
			ApplicantUserID:     caller.UserID,
			ApplicantIdentityID: caller.IdentityID,
		})
		if err != nil {
			return err
		}
		instID = id
		return nil
	}); err != nil {
		return 0, err
	}
	// 一步送审(见文件头 ③):草稿没有可补的内容,直接提交。
	// 引擎自己判状态守卫(草稿才能提交),业务侧不重复判。
	return instID, s.engine.Submit(ctx, caller, instID, SubmitReq{})
}

// ============================================================================
// 审批状态连出
// ============================================================================

// RecordApprovalBadge 一条记录的审批概要(连自 sys_approval_instance)
type RecordApprovalBadge struct {
	// InstanceID 审批单 id(前端跳审批详情用)
	InstanceID uint64 `json:"instance_id,string"`
	// Status 审批单状态(draft / running / approved / rejected / returned / cancelled)
	Status string `json:"status"`
}

// RecordApprovalBadges 一批记录的审批状态
//
// 返回 map[记录id]概要;没有审批单的记录**不在 map 里**(≠ 没送审过)。
// used by ListRecords / GetRecord 给每行挂 `_approval`。
func (s *dynService) RecordApprovalBadges(
	ctx context.Context, recordIDs []uint64,
) (map[uint64]RecordApprovalBadge, error) {
	out := make(map[uint64]RecordApprovalBadge, len(recordIDs))
	if len(recordIDs) == 0 {
		return out, nil
	}
	var rows []model.SysApprovalInstance
	if err := s.db.WithContext(ctx).
		Where("biz_type = ? AND biz_id IN ?", DynBizType, recordIDs).
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批状态失败").WithDetail(err.Error())
	}
	for _, r := range rows {
		out[r.BizID] = RecordApprovalBadge{InstanceID: r.ID, Status: string(r.Status)}
	}
	return out, nil
}

// attachApprovalBadges 给记录视图批量挂 `_approval`(没有审批单的行**不加 key**)
//
// ⚠️ 这是**展示增强**,失败只记日志不拦列表 ——
// 审批状态查不出来时,列表照常出,只是少了徽标。
//
// ⚠️ 没绑定审批模板的集合**一次查询都不发** ——
// dyn 的记录列表是最热的读路径,绝大多数集合根本没绑审批,
// 白发一条 `biz_id IN (...)` 是纯浪费。
func (s *dynService) attachApprovalBadges(ctx context.Context, rc *dynRecordCtx, views []map[string]any) {
	if rc.col.ApprovalTemplateID == 0 {
		return
	}
	ids := make([]uint64, 0, len(views))
	for _, v := range views {
		if id, err := strconv.ParseUint(dynIDString(v["id"]), 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	badges, err := s.RecordApprovalBadges(ctx, ids)
	if err != nil {
		log.Printf("[dyn-approval] 审批徽标查询失败(不影响列表):%v", err)
		return
	}
	for _, v := range views {
		if id, err := strconv.ParseUint(dynIDString(v["id"]), 10, 64); err == nil {
			if b, ok := badges[id]; ok {
				v["_approval"] = b
			}
		}
	}
}
