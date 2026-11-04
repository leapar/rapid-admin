// 通用审批系统 —— 流程模板 CRUD 实现(M86)
// 文档:docs/通用审批系统设计方案V1.0.md §7.2 / D7
//
// 🔴 **本文件最重要的一条是「预检在写之前」(§5.3①)。**
// 模板保存之前先解析一遍审批人,解析不出人的关卡**拒绝保存** ——
// 而不是"先存下来,等有人发起单子时才发现这一关没人"。
// 后者的症状是"某个业务的单子发不出去",而排障的人会去看业务代码,不在模板上。
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// flowCodePattern 模板编码格式(§7.2)
//
// ⚠️ **`{1,63}` 是"总长 2~64"** —— 正则里第一个字符已经吃掉了 1 个,
// 所以最小长度是 2。单字符编码("a")会被这条正则拒掉,这是有意的:
// 一位的编码在日志和 DB 里几乎必然和别的撞。
var flowCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// maxFlowStages 关卡数上限(§7.2)
const maxFlowStages = 10

// approvalFlowService 模板 CRUD 实现
type approvalFlowService struct {
	db *gorm.DB
	// prechecker 配置期预检(§5.3①)
	//
	// ⚠️ **它不能是 nil** —— nil 会让"保存前必须预检"这条防线整个消失,
	// 而且症状是"某天有人配了个永远没人审的模板",没人知道是从哪掉下去的。
	prechecker AssigneePrechecker
}

var _ ApprovalFlowService = (*approvalFlowService)(nil)

// NewApprovalFlowService 构造
//
// ⚠️ **传 `ApprovalResolver` 而不是 `AssigneePrechecker`** ——
// 装配处手里拿的就是解析器,再挑一个窄接口出来只是多一次类型声明。
func NewApprovalFlowService(db *gorm.DB, resolver ApprovalResolver) ApprovalFlowService {
	return &approvalFlowService{db: db, prechecker: resolver}
}

// ============================================================================
// 读
// ============================================================================

func (s *approvalFlowService) ListFlows(ctx context.Context, q FlowListQuery) (*model.PageResponse, error) {
	page, size := normApprovalPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysApprovalTemplate{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		tx = tx.Where("code LIKE ? OR name LIKE ?", "%"+kw+"%", "%"+kw+"%")
	}
	if bt := strings.TrimSpace(q.BizType); bt != "" {
		tx = tx.Where("biz_type = ?", bt)
	}
	if q.IsActive != nil {
		tx = tx.Where("is_active = ?", *q.IsActive)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批模板失败").WithDetail(err.Error())
	}
	var rows []model.SysApprovalTemplate
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批模板失败").WithDetail(err.Error())
	}

	items := make([]FlowListItem, 0, len(rows))
	ids := make([]uint64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		items = append(items, flowListItem(r, 0))
	}
	// ⚠️ **一次 GROUP BY 查完这一页的关卡数**,不为每个模板单发一次
	counts, err := s.stageCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].StageCount = counts[items[i].ID]
	}
	return &model.PageResponse{List: items, Total: total, Page: page, Size: size}, nil
}

func (s *approvalFlowService) GetFlow(ctx context.Context, id uint64) (*FlowDetail, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var tpl model.SysApprovalTemplate
	if err := s.db.WithContext(ctx).First(&tpl, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "审批模板不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询审批模板失败").WithDetail(err.Error())
	}

	var stages []model.SysApprovalStage
	if err := s.db.WithContext(ctx).
		Where("template_id = ?", tpl.ID).
		Order("stage_index ASC, id ASC").
		Find(&stages).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批关卡失败").WithDetail(err.Error())
	}
	stageIDs := make([]uint64, 0, len(stages))
	for _, st := range stages {
		stageIDs = append(stageIDs, st.ID)
	}
	var rules []model.SysApprovalAssigneeRule
	if len(stageIDs) > 0 {
		if err := s.db.WithContext(ctx).
			Where("stage_id IN ?", stageIDs).
			Order("seq ASC, id ASC").
			Find(&rules).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询审批人规则失败").WithDetail(err.Error())
		}
	}
	rulesByStage := make(map[uint64][]model.SysApprovalAssigneeRule, len(stages))
	for _, r := range rules {
		rulesByStage[r.StageID] = append(rulesByStage[r.StageID], r)
	}

	detail := &FlowDetail{
		FlowListItem: flowListItem(tpl, len(stages)),
		Stages:       make([]FlowStageView, 0, len(stages)),
	}
	for _, st := range stages {
		sv := FlowStageView{
			ID:         strconv.FormatUint(st.ID, 10),
			StageIndex: st.StageIndex,
			Name:       st.Name,
			ReviewType: st.ReviewType,
			Rules:      make([]FlowRuleView, 0, len(rulesByStage[st.ID])),
		}
		for _, r := range rulesByStage[st.ID] {
			sv.Rules = append(sv.Rules, FlowRuleView{
				ID:       strconv.FormatUint(r.ID, 10),
				Seq:      r.Seq,
				RuleType: r.RuleType,
				UserID:   strconv.FormatUint(r.UserID, 10),
				RoleID:   strconv.FormatUint(r.RoleID, 10),
				Levels:   r.Levels,
			})
		}
		detail.Stages = append(detail.Stages, sv)
	}
	return detail, nil
}

// ============================================================================
// 写
// ============================================================================

func (s *approvalFlowService) CreateFlow(ctx context.Context, caller *Caller, req FlowSaveReq) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	norm, err := s.normalizeFlow(req, true)
	if err != nil {
		return 0, err
	}

	var newID uint64
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// ① code 唯一(唯一索引不含 deleted_at,软删的行也占坑位)
		if err := s.assertCodeFree(tx, norm.Code, 0); err != nil {
			return err
		}
		// ② 预检(§5.3①)—— **在写之前**
		if err := s.precheck(ctx, tx, norm); err != nil {
			return err
		}
		// ③ 模板
		tpl := &model.SysApprovalTemplate{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			Code:      norm.Code,
			Name:      norm.Name,
			BizType:   norm.BizType,
			IsActive:  norm.IsActive,
			Remark:    norm.Remark,
		}
		if err := tx.WithContext(ctx).Create(tpl).Error; err != nil {
			return errcode.New(errcode.Internal, "创建审批模板失败").WithDetail(err.Error())
		}
		// ④ 关卡 + 规则
		if err := s.writeStages(tx, tpl.ID, norm.Stages); err != nil {
			return err
		}
		newID = tpl.ID
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newID, nil
}

func (s *approvalFlowService) UpdateFlow(ctx context.Context, caller *Caller, id uint64, req FlowSaveReq) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	norm, err := s.normalizeFlow(req, false)
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tpl model.SysApprovalTemplate
		if err := tx.WithContext(ctx).First(&tpl, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errcode.New(errcode.NotFound, "审批模板不存在")
			}
			return errcode.New(errcode.Internal, "查询审批模板失败").WithDetail(err.Error())
		}
		// 🔴 **D7:有非终态实例时禁止改结构。**
		// 改了之后,正在跑的实例的"第 2 关"会指向一个完全不同的关卡 ——
		// 症状是单子卡死,而没人会想到是模板被改过。
		n, err := s.activeInstanceCount(tx, tpl.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return errcode.New(errcode.StateInvalid, fmt.Sprintf(
				"该模板有 %d 条在跑的审批,请新建一条模板代替修改", n))
		}
		if norm.Code != "" && norm.Code != tpl.Code {
			if err := s.assertCodeFree(tx, norm.Code, tpl.ID); err != nil {
				return err
			}
		}
		if err := s.precheck(ctx, tx, norm); err != nil {
			return err
		}

		// ⚠️ **旧关卡与旧规则是硬删,不是软删** —— 详见 `purgeStages` 的注释。
		if err := s.purgeStages(tx, tpl.ID); err != nil {
			return err
		}
		upd := map[string]any{"name": norm.Name, "biz_type": norm.BizType, "remark": norm.Remark}
		if norm.Code != "" {
			upd["code"] = norm.Code
		}
		if norm.IsActiveSet {
			upd["is_active"] = norm.IsActive
		}
		if err := tx.WithContext(ctx).Model(&model.SysApprovalTemplate{}).
			Where("id = ?", tpl.ID).Updates(upd).Error; err != nil {
			return errcode.New(errcode.Internal, "更新审批模板失败").WithDetail(err.Error())
		}
		return s.writeStages(tx, tpl.ID, norm.Stages)
	})
}

func (s *approvalFlowService) DeleteFlow(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tpl model.SysApprovalTemplate
		if err := tx.WithContext(ctx).First(&tpl, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errcode.New(errcode.NotFound, "审批模板不存在")
			}
			return errcode.New(errcode.Internal, "查询审批模板失败").WithDetail(err.Error())
		}
		n, err := s.activeInstanceCount(tx, tpl.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return errcode.New(errcode.StateInvalid, fmt.Sprintf(
				"该模板有 %d 条在跑的审批,请先处理完再删除", n))
		}
		if err := s.purgeStages(tx, tpl.ID); err != nil {
			return err
		}
		// 🔴 **D7:code 必须改写,否则软删的行还占着唯一坑位。**
		// 唯一索引 `uk_app_tpl_code` 不含 deleted_at(SQL 里 NULL 互不相等),
		// 所以软删之后同名模板再也建不出来。
		upd := map[string]any{
			"code": deletedFlowCode(tpl.Code, tpl.ID),
			// ⚠️ **同时停用** —— 留着一条 is_active=true 的幽灵模板,
			// `pickTemplate` 会把它当成"该 biz_type 唯一启用的模板"选中。
			"is_active": false,
		}
		if err := tx.WithContext(ctx).Model(&model.SysApprovalTemplate{}).
			Where("id = ?", tpl.ID).Updates(upd).Error; err != nil {
			return errcode.New(errcode.Internal, "删除审批模板失败").WithDetail(err.Error())
		}
		if err := tx.WithContext(ctx).Delete(&model.SysApprovalTemplate{}, tpl.ID).Error; err != nil {
			return errcode.New(errcode.Internal, "删除审批模板失败").WithDetail(err.Error())
		}
		return nil
	})
}

// ============================================================================
// 内部
// ============================================================================

// normalizedFlow 规整后的模板入参
type normalizedFlow struct {
	Code    string
	Name    string
	BizType string
	Remark  string
	// IsActiveSet true = 本次调用显式设置了 is_active
	IsActiveSet bool
	IsActive    bool
	Stages      []FlowStageInput
}

// normalizeFlow 校验并规整入参
//
// ⚠️ **`forCreate` 决定 `is_active` 缺省行为**:建模板时 nil → true(默认启用),
// 改模板时 nil → 不动(指针语义,§7.4 同一套规矩)。
func (s *approvalFlowService) normalizeFlow(req FlowSaveReq, forCreate bool) (normalizedFlow, error) {
	out := normalizedFlow{
		Code:    strings.TrimSpace(req.Code),
		Name:    strings.TrimSpace(req.Name),
		BizType: strings.TrimSpace(req.BizType),
		Remark:  strings.TrimSpace(req.Remark),
	}
	if out.Name == "" {
		return out, errcode.New(errcode.ParamMissing, "模板名不能为空")
	}
	if out.BizType == "" {
		return out, errcode.New(errcode.ParamMissing, "业务类型 biz_type 不能为空")
	}
	if forCreate && out.Code == "" {
		return out, errcode.New(errcode.ParamMissing, "模板编码不能为空")
	}
	// 🔴 **改模板时也要校验 code 格式**(M89-2 补)—— 原来这段整体挂在
	// `if forCreate` 里面,于是 `PUT /flows/:id` **能把 code 改成任何字符串**:
	// "Bad Code!"、中文、emoji 都收。
	//
	// 后果不只是"编码不好看":
	//   ① `deletedFlowCode` 按**字节**截断(`prefix[:64-len(suffix)]`),
	//      一个中文 code 会被从中间劈开 → 库里躺了半个 UTF-8 字符;
	//   ② `code` 是业务侧引用模板的键(`template_code`),脏值一路传下去,
	//      而 §7.2 的"服务端校验"清单里明写着这条格式。
	//
	// ⚠️ **空 code 不参与校验** —— 改模板时它是"不改"(指针语义,§7.4)。
	if out.Code != "" && !flowCodePattern.MatchString(out.Code) {
		return out, errcode.New(errcode.ParamInvalid,
			"模板编码格式错误:只允许小写字母、数字、下划线,且必须以字母开头")
	}
	if req.IsActive != nil {
		out.IsActiveSet = true
		out.IsActive = *req.IsActive
	} else if forCreate {
		out.IsActiveSet = true
		out.IsActive = true
	}

	// ⚠️ **关卡数 1~10**(§7.2)。0 关卡的模板 = 每张单子提交进去就卡住。
	if len(req.Stages) == 0 {
		return out, errcode.New(errcode.ParamMissing, "至少要配一个关卡")
	}
	if len(req.Stages) > maxFlowStages {
		return out, errcode.New(errcode.ParamInvalid,
			fmt.Sprintf("关卡数 %d 超过上限 %d", len(req.Stages), maxFlowStages))
	}
	for i := range req.Stages {
		st := req.Stages[i]
		st.Name = strings.TrimSpace(st.Name)
		if st.Name == "" {
			return out, errcode.New(errcode.ParamMissing,
				fmt.Sprintf("第 %d 关的关卡名不能为空", i+1))
		}
		switch st.ReviewType {
		case "":
			st.ReviewType = model.ApprovalReviewAny
		case model.ApprovalReviewAll, model.ApprovalReviewAny:
		default:
			return out, errcode.New(errcode.ParamInvalid,
				fmt.Sprintf("第 %d 关的 review_type 只能是 all / any", i+1))
		}
		if len(st.Rules) == 0 {
			return out, errcode.New(errcode.ParamMissing,
				fmt.Sprintf("第 %d 关没有配任何审批人规则", i+1))
		}
		for j := range st.Rules {
			r := st.Rules[j]
			switch r.RuleType {
			case model.ApprovalRuleUser:
				if r.UserID == 0 {
					return out, errcode.New(errcode.ParamInvalid,
						fmt.Sprintf("第 %d 关第 %d 条规则是指定人,但没填 user_id", i+1, j+1))
				}
				// ⚠️ **另两个 ID 必须清零** —— 留着会让"这条规则到底按谁算"变得有歧义
				r.RoleID = 0
			case model.ApprovalRuleRole:
				if r.RoleID == 0 {
					return out, errcode.New(errcode.ParamInvalid,
						fmt.Sprintf("第 %d 关第 %d 条规则是指定角色,但没填 role_id", i+1, j+1))
				}
				r.UserID = 0
			case model.ApprovalRuleDeptLeader:
				r.UserID = 0
				r.RoleID = 0
			default:
				return out, errcode.New(errcode.ParamInvalid,
					fmt.Sprintf("第 %d 关第 %d 条规则的 rule_type=%s 不在词表内(user/role/dept_leader)", i+1, j+1, r.RuleType))
			}
			st.Rules[j] = r
		}
		req.Stages[i] = st
	}
	out.Stages = req.Stages
	return out, nil
}

// precheck 配置期预检(§5.3①)
//
// ⚠️ **传的是"即将写进去的那份规则"**,不是按 stage_id 反查出来的 ——
// 模板 CRUD 是「先在内存里拼好整棵树、校验、再一次性落库」。
func (s *approvalFlowService) precheck(ctx context.Context, tx *gorm.DB, norm normalizedFlow) error {
	if s.prechecker == nil {
		// ⚠️ **不该走到这里** —— 构造器强制传了解析器。
		// 真走到(有人传了 nil),宁可拒绝保存,也不能"跳过预检" ——
		// 那等于 §5.3① 这条防线被静默摘掉。
		return errcode.New(errcode.Internal, "审批预检未装配,拒绝保存模板")
	}
	stages := make([]StageRuleCheck, 0, len(norm.Stages))
	for i, st := range norm.Stages {
		rules := make([]model.SysApprovalAssigneeRule, 0, len(st.Rules))
		for _, r := range st.Rules {
			rules = append(rules, model.SysApprovalAssigneeRule{
				RuleType: r.RuleType,
				UserID:   r.UserID,
				RoleID:   r.RoleID,
				Levels:   r.Levels,
			})
		}
		stages = append(stages, StageRuleCheck{StageIndex: i + 1, Rules: rules})
	}
	return s.prechecker.PrecheckTemplate(ctx, tx, TemplatePrecheckInput{
		TemplateCode: norm.Code,
		Stages:       stages,
	})
}

// writeStages 写关卡与规则
func (s *approvalFlowService) writeStages(tx *gorm.DB, templateID uint64, stages []FlowStageInput) error {
	for i, st := range stages {
		stage := &model.SysApprovalStage{
			BaseModel:  model.BaseModel{ID: sonyflake.Next()},
			TemplateID: templateID,
			// ⚠️ **stage_index 从 1 开始且连续**(§6②)—— 唯一索引
			// `(template_id, stage_index)` 会挡住重复,但**挡不住断号**,
			// 而断号的关卡"推进到第 N 关"永远命中不了。
			StageIndex: i + 1,
			Name:       st.Name,
			ReviewType: st.ReviewType,
		}
		if err := tx.WithContext(context.Background()).Create(stage).Error; err != nil {
			return errcode.New(errcode.Internal, "创建审批关卡失败").WithDetail(err.Error())
		}
		for j, r := range st.Rules {
			rule := &model.SysApprovalAssigneeRule{
				BaseModel: model.BaseModel{ID: sonyflake.Next()},
				StageID:   stage.ID,
				Seq:       j + 1,
				RuleType:  r.RuleType,
				UserID:    r.UserID,
				RoleID:    r.RoleID,
				Levels:    r.Levels,
			}
			if err := tx.WithContext(context.Background()).Create(rule).Error; err != nil {
				return errcode.New(errcode.Internal, "创建审批人规则失败").WithDetail(err.Error())
			}
		}
	}
	return nil
}

// purgeStages 删掉模板下的全部关卡与规则
//
// 🔴 **硬删,不是软删** —— 与设计稿 §7.2 写的"软删旧 stage/rule"相反,理由如下:
//
//	`uk_app_stage_tpl_idx(template_id, stage_index)` 的唯一索引**不含 deleted_at**
//	(模型层 `TestApproval_NoUniqueIndexHasDeletedAt` 断言了这一点)。
//	软删的关卡行**仍然占着 (template_id, stage_index) 这个坑位** ——
//	于是"改模板"写回同样的第 1 关时,数据库直接拒绝:
//	「UNIQUE constraint failed: sys_approval_stage.template_id, sys_approval_stage.stage_index」。
//
//	换句话说:照设计稿写,`PUT /flows/:id` **第一次改就报错**,改不动。
//	硬删没有这个问题 —— 这两张表是配置表(不是流水表),没有"历史要留"的要求;
//	真正要留历史的 log / round 两张表才是不许删的(§6① ⑥⑦)。
func (s *approvalFlowService) purgeStages(tx *gorm.DB, templateID uint64) error {
	var stageIDs []uint64
	if err := tx.WithContext(context.Background()).Model(&model.SysApprovalStage{}).
		Unscoped().
		Where("template_id = ?", templateID).
		Pluck("id", &stageIDs).Error; err != nil {
		return errcode.New(errcode.Internal, "清理审批关卡失败").WithDetail(err.Error())
	}
	if len(stageIDs) > 0 {
		if err := tx.WithContext(context.Background()).
			Unscoped().
			Where("stage_id IN ?", stageIDs).
			Delete(&model.SysApprovalAssigneeRule{}).Error; err != nil {
			return errcode.New(errcode.Internal, "清理审批人规则失败").WithDetail(err.Error())
		}
	}
	if err := tx.WithContext(context.Background()).
		Unscoped().
		Where("template_id = ?", templateID).
		Delete(&model.SysApprovalStage{}).Error; err != nil {
		return errcode.New(errcode.Internal, "清理审批关卡失败").WithDetail(err.Error())
	}
	return nil
}

// assertCodeFree 断言 code 没被占用
func (s *approvalFlowService) assertCodeFree(tx *gorm.DB, code string, selfID uint64) error {
	if code == "" {
		return nil
	}
	var n int64
	// ⚠️ **Unscoped**:软删的行也占着唯一坑位(唯一索引不含 deleted_at),
	// 所以查"有没有被占用"必须连软删的算上。
	if err := tx.WithContext(context.Background()).Model(&model.SysApprovalTemplate{}).
		Unscoped().
		Where("code = ? AND id <> ?", code, selfID).
		Count(&n).Error; err != nil {
		return errcode.New(errcode.Internal, "检查模板编码失败").WithDetail(err.Error())
	}
	if n > 0 {
		return errcode.New(errcode.Conflict, "模板编码已存在:"+code)
	}
	return nil
}

// activeInstanceCount 该模板下"非终态"的实例数(D7)
func (s *approvalFlowService) activeInstanceCount(tx *gorm.DB, templateID uint64) (int64, error) {
	var n int64
	if err := tx.WithContext(context.Background()).Model(&model.SysApprovalInstance{}).
		Where("template_id = ? AND status IN ?", templateID, []string{
			model.ApprovalStatusDraft,
			model.ApprovalStatusRunning,
			model.ApprovalStatusReturned,
		}).
		Count(&n).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "检查在跑的审批失败").WithDetail(err.Error())
	}
	return n, nil
}

// stageCounts 一次 GROUP BY 统计多个模板的关卡数
func (s *approvalFlowService) stageCounts(ctx context.Context, templateIDs []uint64) (map[string]int, error) {
	out := make(map[string]int, len(templateIDs))
	if len(templateIDs) == 0 {
		return out, nil
	}
	type row struct {
		TemplateID uint64
		N          int
	}
	var rows []row
	if err := s.db.WithContext(ctx).Model(&model.SysApprovalStage{}).
		Select("template_id, COUNT(*) AS n").
		Where("template_id IN ?", templateIDs).
		Group("template_id").
		Scan(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "统计审批关卡失败").WithDetail(err.Error())
	}
	for _, r := range rows {
		out[strconv.FormatUint(r.TemplateID, 10)] = r.N
	}
	return out, nil
}

// flowListItem model → view
func flowListItem(tpl model.SysApprovalTemplate, stageCount int) FlowListItem {
	return FlowListItem{
		ID:         strconv.FormatUint(tpl.ID, 10),
		Code:       tpl.Code,
		Name:       tpl.Name,
		BizType:    tpl.BizType,
		IsActive:   tpl.IsActive,
		Remark:     tpl.Remark,
		StageCount: stageCount,
		CreatedAt:  tpl.CreatedAt,
		UpdatedAt:  tpl.UpdatedAt,
	}
}

// deletedFlowCode 软删时改写后的 code(D7)
//
// ⚠️ **必须截断原 code** —— `code` 列是 `size:64`,而
// `"#deleted#" + 19 位雪花 ID` 本身就要 27 个字符,原 code 最多只能留 37 个。
// 不截断就是"删模板"这一步自己把 code 撑爆,报的是数据库截断错误。
func deletedFlowCode(code string, id uint64) string {
	suffix := "#deleted#" + strconv.FormatUint(id, 10)
	prefix := code
	if len(prefix)+len(suffix) > 64 {
		prefix = prefix[:64-len(suffix)]
	}
	return prefix + suffix
}

// normApprovalPage 分页规整
func normApprovalPage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}
