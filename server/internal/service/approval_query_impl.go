// 通用审批系统 —— 审批单查询实现(M86)
// 文档:docs/通用审批系统设计方案V1.0.md §8.2(三个盒子)/ §8.3(详情可见性)
//
// 🔴 **本文件最要紧的两条边界**
//
//	① 列表只有三个盒子,**每个盒子的 SQL 条件都带"我和这条单的关系"**
//	   —— 结构上不可能越权,不需要在 Go 里再判一遍。
//	② 详情可见性 = **框架层 assertor 先判 + 审批域 isParty 后兜底**,
//	   顺序不能反(§8.3)。
package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/errcode"
)

// approvalQueryService 审批单查询实现
type approvalQueryService struct {
	db *gorm.DB
	// engine 建草稿用(其余写动作在引擎上)
	engine ApprovalEngine
	// assertor 资源级断言(§8.3 第 ① 步)
	assertor authz.ResourceAssertor
}

var _ ApprovalService = (*approvalQueryService)(nil)

// NewApprovalService 构造
func NewApprovalService(db *gorm.DB, engine ApprovalEngine, assertor authz.ResourceAssertor) ApprovalService {
	return &approvalQueryService{db: db, engine: engine, assertor: assertor}
}

// ============================================================================
// 列表(§8.2)
// ============================================================================

func (s *approvalQueryService) ListApprovals(
	ctx context.Context, caller *Caller, q ApprovalListQuery,
) (*model.PageResponse, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}
	page, size := normApprovalPage(q.Page, q.Size)

	tx := s.db.WithContext(ctx).Model(&model.SysApprovalInstance{})
	switch strings.TrimSpace(q.Box) {
	case string(BoxTodo):
		// ⚠️ **必须带 `status='pending'`**(§8.2)—— 打回后上一轮那条 task 是
		// `stale`,只查"有没有我的 task"会让人在打回后还能再审一遍。
		tx = tx.Where("status = ?", model.ApprovalStatusRunning).
			Where("EXISTS (SELECT 1 FROM sys_approval_task t WHERE t.instance_id = sys_approval_instance.id "+
				"AND t.assignee_user_id = ? AND t.status = ? AND t.deleted_at IS NULL)",
				caller.UserID, model.ApprovalTaskPending)

	case string(BoxMine):
		tx = tx.Where("applicant_user_id = ?", caller.UserID)

	case string(BoxDone):
		tx = tx.Where("EXISTS (SELECT 1 FROM sys_approval_task t WHERE t.instance_id = sys_approval_instance.id "+
			"AND t.assignee_user_id = ? AND t.status IN ? AND t.deleted_at IS NULL)",
			caller.UserID, []string{model.ApprovalTaskApproved, model.ApprovalTaskRejected})

	default:
		// ⚠️ **空 box / 非法 box 一律报错**,不退化成"全部"(§8.2)。
		// 退化 = 前端漏传一个参数 = 全平台审批单裸奔。
		return nil, errcode.New(errcode.ParamInvalid, "box 只能是 todo / mine / done")
	}

	if bt := strings.TrimSpace(q.BizType); bt != "" {
		tx = tx.Where("biz_type = ?", bt)
	}
	if st := strings.TrimSpace(q.Status); st != "" {
		tx = tx.Where("status = ?", st)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		tx = tx.Where("title LIKE ?", "%"+kw+"%")
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批单失败").WithDetail(err.Error())
	}
	var rows []model.SysApprovalInstance
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批单失败").WithDetail(err.Error())
	}

	items, err := s.buildListItems(ctx, caller, rows)
	if err != nil {
		return nil, err
	}
	return &model.PageResponse{List: items, Total: total, Page: page, Size: size}, nil
}

// buildListItems 把实例行装配成列表项
//
// ⚠️ **补充字段是 4 次批量查询,不是每行 4 次** —— 列表页一页 20 条,
// 逐行查就是 80 次往返。
func (s *approvalQueryService) buildListItems(
	ctx context.Context, caller *Caller, rows []model.SysApprovalInstance,
) ([]ApprovalListItem, error) {
	items := make([]ApprovalListItem, 0, len(rows))
	instIDs := make([]uint64, 0, len(rows))
	applicants := make([]uint64, 0, len(rows))
	tplIDs := make([]uint64, 0, len(rows))
	for _, r := range rows {
		instIDs = append(instIDs, r.ID)
		applicants = append(applicants, r.ApplicantUserID)
		tplIDs = append(tplIDs, r.TemplateID)
		items = append(items, ApprovalListItem{
			ID:              strconv.FormatUint(r.ID, 10),
			BizType:         r.BizType,
			BizID:           strconv.FormatUint(r.BizID, 10),
			Title:           r.Title,
			Status:          r.Status,
			CurrentStage:    r.CurrentStage,
			ApplicantUserID: strconv.FormatUint(r.ApplicantUserID, 10),
			ReturnCount:     r.ReturnCount,
			CreatedAt:       r.CreatedAt,
			UpdatedAt:       r.UpdatedAt,
			SubmittedAt:     r.SubmittedAt,
			FinishedAt:      r.FinishedAt,
		})
	}
	if len(rows) == 0 {
		return items, nil
	}

	names, err := s.userNames(ctx, applicants)
	if err != nil {
		return nil, err
	}
	tpls, err := s.templatesByID(ctx, tplIDs)
	if err != nil {
		return nil, err
	}
	stageNames, err := s.stageNames(ctx, tplIDs)
	if err != nil {
		return nil, err
	}
	mine, err := s.myTaskStatuses(ctx, caller.UserID, instIDs)
	if err != nil {
		return nil, err
	}

	for i := range items {
		r := rows[i]
		items[i].ApplicantName = names[strconv.FormatUint(r.ApplicantUserID, 10)]
		tplKey := strconv.FormatUint(r.TemplateID, 10)
		if t, ok := tpls[tplKey]; ok {
			items[i].TemplateCode = t.Code
			items[i].TemplateName = t.Name
		}
		items[i].CurrentStageName = stageNames[stageNameKey(tplKey, r.CurrentStage)]
		items[i].MyTaskStatus = mine[items[i].ID]
	}
	return items, nil
}

// ============================================================================
// 详情(§8.3)
// ============================================================================

func (s *approvalQueryService) GetApproval(
	ctx context.Context, caller *Caller, id uint64,
) (*ApprovalDetail, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}

	var inst model.SysApprovalInstance
	if err := s.db.WithContext(ctx).First(&inst, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "审批单不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询审批单失败").WithDetail(err.Error())
	}

	// 🔴 **可见性:assertor 先,isParty 后兜底**(§8.3)
	//
	// ⚠️ **顺序不能反** —— 先判 isParty 再判 assertor 的话,
	// `ActiveGrants`(委托)那条路就被绕过了。
	if err := s.assertVisible(ctx, caller, &inst); err != nil {
		return nil, err
	}

	items, err := s.buildListItems(ctx, caller, []model.SysApprovalInstance{inst})
	if err != nil {
		return nil, err
	}
	detail := &ApprovalDetail{ApprovalListItem: items[0]}
	detail.Variables = inst.Variables
	detail.TemplateID = strconv.FormatUint(inst.TemplateID, 10)
	detail.ApplicantIdentityID = strconv.FormatUint(inst.ApplicantIdentityID, 10)

	// 模板与关卡
	tpls, err := s.templatesByID(ctx, []uint64{inst.TemplateID})
	if err != nil {
		return nil, err
	}
	if t, ok := tpls[strconv.FormatUint(inst.TemplateID, 10)]; ok {
		detail.TemplateCode = t.Code
		detail.TemplateName = t.Name
	}
	stages, err := s.loadStages(ctx, inst.TemplateID)
	if err != nil {
		return nil, err
	}
	detail.CurrentStageName = stageNameOf(stages, inst.CurrentStage)

	// 关卡 + 每关的审批人与状态
	tasks, err := s.loadTasks(ctx, inst.ID)
	if err != nil {
		return nil, err
	}
	detail.Stages, err = s.buildStageViews(ctx, stages, tasks, inst)
	if err != nil {
		return nil, err
	}

	// 轮次快照
	rounds, err := s.loadRounds(ctx, inst.ID)
	if err != nil {
		return nil, err
	}
	detail.Rounds = rounds

	// 意见流水
	logs, err := s.loadLogs(ctx, inst.ID)
	if err != nil {
		return nil, err
	}
	detail.Logs = logs

	// 我能不能审
	detail.CanAct = inst.Status == model.ApprovalStatusRunning && detail.MyTaskStatus == model.ApprovalTaskPending
	return detail, nil
}

// assertVisible 详情可见性(§8.3)
//
// 🔴 **两条路都走,不互相替代**:
//
//	① 框架层的资源级断言 —— 它负责"所有人"和"委托(ActiveGrants)",不要绕过
//	② 审批域自己的"当事人"判定 —— 只有 approval service 有资格判,
//	   因为它要查 sys_approval_task
//
// ⚠️ **isParty 绝不能反过来给 Assertor 兜底** —— 一旦写成
// `if isParty { return nil }`,`ActiveGrants` 委托路径就被绕过了。
func (s *approvalQueryService) assertVisible(
	ctx context.Context, caller *Caller, inst *model.SysApprovalInstance,
) error {
	var assertErr error
	if s.assertor != nil {
		principal, _ := authz.PrincipalFromContext(ctx)
		assertErr = s.assertor.AssertCan(
			toCallerSummary(caller), principal,
			authz.Resource{
				ResourceType: "approval",
				ResourceID:   inst.ID,
				// ⚠️ **OwnerUserID 填申请人** —— 申请人自己看自己的单子。
				// 填 0 的话申请人会被 assertor 拒,只剩 isParty 兜底,
				// 委托路径就只剩"超管"一种人能走。
				OwnerUserID: inst.ApplicantUserID,
			},
			"approval:read",
		)
	}
	if assertErr == nil {
		return nil
	}
	party, err := s.isParty(ctx, caller, inst)
	if err != nil {
		return err
	}
	if party {
		return nil
	}
	// ⚠️ **原样返回 assertor 的错误**,保留它的 errcode 与审计信息
	return assertErr
}

// isParty 我是不是这条单的当事人
//
// ⚠️ **一条 SQL 搞定**(§8.3),不写三条。
// ⚠️ **不加 `status IN (...)` 过滤** —— 历史轮的 task 也是当事人,
// §9.1 的多轮历史要给他们看。
func (s *approvalQueryService) isParty(
	ctx context.Context, caller *Caller, inst *model.SysApprovalInstance,
) (bool, error) {
	if caller == nil {
		return false, nil
	}
	if inst.ApplicantUserID == caller.UserID {
		return true, nil
	}
	var n int64
	row := s.db.WithContext(ctx).Raw(
		"SELECT COUNT(1) FROM ("+
			"SELECT 1 FROM sys_approval_instance WHERE id = ? AND applicant_user_id = ? AND deleted_at IS NULL "+
			"UNION ALL "+
			"SELECT 1 FROM sys_approval_task WHERE instance_id = ? AND assignee_user_id = ? AND deleted_at IS NULL"+
			") AS party",
		inst.ID, caller.UserID, inst.ID, caller.UserID,
	).Row()
	if err := row.Scan(&n); err != nil {
		return false, errcode.New(errcode.Internal, "校验审批单可见性失败").WithDetail(err.Error())
	}
	return n > 0, nil
}

// ============================================================================
// 建草稿(§7.3)
// ============================================================================

func (s *approvalQueryService) CreateDraft(
	ctx context.Context, caller *Caller, req CreateApprovalReq,
) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	if s.engine == nil {
		return 0, errcode.New(errcode.Internal, "审批引擎未装配")
	}
	bizType := strings.TrimSpace(req.BizType)
	if bizType == "" {
		return 0, errcode.New(errcode.ParamMissing, "biz_type 必填")
	}
	if req.BizID == 0 {
		return 0, errcode.New(errcode.ParamMissing, "biz_id 必填")
	}
	if strings.TrimSpace(req.Title) == "" {
		return 0, errcode.New(errcode.ParamMissing, "title 必填")
	}

	// ⚠️ **D9:同一条业务记录只允许一个审批实例**(硬唯一索引)。
	// 这里先查一次是为了给出可读的报错 —— 撞唯一索引时数据库只说
	// "UNIQUE constraint failed",用户看不懂。
	//
	// ⚠️ **这只是预检查,不是判定依据**(M88-5):两个请求同时进来会**双双
	// 通过**这一查,后一个 INSERT 才撞索引。真正兜底的是引擎里那一道
	// 唯一约束翻译(approval_engine_impl.go)。两处必须给**同一个**错误码 ——
	// 所以共用 `ErrApprovalInstanceExists`,别各写各的。
	var exist int64
	if err := s.db.WithContext(ctx).Model(&model.SysApprovalInstance{}).
		Where("biz_type = ? AND biz_id = ?", bizType, req.BizID).
		Count(&exist).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "查询审批单失败").WithDetail(err.Error())
	}
	if exist > 0 {
		return 0, ErrApprovalInstanceExists
	}

	// ⚠️ **Variables 原样透传**(§7.4)—— 引擎要逐字节比较
	// "这次重提的内容和本轮快照是否完全一样",中间任何一次
	// map → JSON 的往返都会打乱键序,把"没改"变成"改了"。
	variables := strings.TrimSpace(string(req.Variables))
	if variables == "" {
		variables = "{}"
	}

	var newID uint64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		newID, err = s.engine.CreateDraft(ctx, tx, &CreateDraftInput{
			BizType:             bizType,
			BizID:               req.BizID,
			Title:               strings.TrimSpace(req.Title),
			Variables:           variables,
			TemplateCode:        strings.TrimSpace(req.TemplateCode),
			ApplicantUserID:     caller.UserID,
			ApplicantIdentityID: caller.IdentityID,
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	return newID, nil
}

// ============================================================================
// 装配辅助
// ============================================================================

// userNames 批量取用户显示名
//
// ⚠️ **返回 map 的 key 是字符串形式的 ID** —— 调用方手上的就是字符串,
// 转来转去容易在某一处漏掉 strconv,静默查不到名字。
func (s *approvalQueryService) userNames(ctx context.Context, ids []uint64) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	uniq := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	var rows []model.SysUser
	if err := s.db.WithContext(ctx).
		Select("id", "username", "nickname", "real_name").
		Where("id IN ?", uniq).
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	for _, u := range rows {
		name := strings.TrimSpace(u.RealName)
		if name == "" {
			name = strings.TrimSpace(u.Nickname)
		}
		if name == "" {
			name = u.Username
		}
		out[strconv.FormatUint(u.ID, 10)] = name
	}
	return out, nil
}

// templatesByID 批量取模板
func (s *approvalQueryService) templatesByID(
	ctx context.Context, ids []uint64,
) (map[string]model.SysApprovalTemplate, error) {
	out := make(map[string]model.SysApprovalTemplate, len(ids))
	uniq := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	var rows []model.SysApprovalTemplate
	if err := s.db.WithContext(ctx).Where("id IN ?", uniq).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批模板失败").WithDetail(err.Error())
	}
	for _, t := range rows {
		out[strconv.FormatUint(t.ID, 10)] = t
	}
	return out, nil
}

// stageNames 批量取「模板 + 关卡序号 → 关卡名」
func (s *approvalQueryService) stageNames(
	ctx context.Context, templateIDs []uint64,
) (map[string]string, error) {
	out := make(map[string]string, len(templateIDs))
	uniq := make([]uint64, 0, len(templateIDs))
	seen := make(map[uint64]struct{}, len(templateIDs))
	for _, id := range templateIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	var rows []model.SysApprovalStage
	if err := s.db.WithContext(ctx).
		Where("template_id IN ?", uniq).
		Order("stage_index ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批关卡失败").WithDetail(err.Error())
	}
	for _, st := range rows {
		out[stageNameKey(strconv.FormatUint(st.TemplateID, 10), st.StageIndex)] = st.Name
	}
	return out, nil
}

// stageNameKey 「模板 ID + 关卡序号」的 map key
func stageNameKey(templateID string, stageIndex int) string {
	return templateID + ":" + strconv.Itoa(stageIndex)
}

// stageNameOf 从已加载的关卡里取名字
func stageNameOf(stages []model.SysApprovalStage, stageIndex int) string {
	if stageIndex <= 0 {
		return ""
	}
	for _, st := range stages {
		if st.StageIndex == stageIndex {
			return st.Name
		}
	}
	return ""
}

// myTaskStatuses 我和这批单子的关系
//
// ⚠️ **优先 pending** —— 同一个��在第 1 轮批过、第 2 轮又轮到我,
// 列表上要显示"待办",不是"已批"。
//
// ⚠️ **排除 `stale`** —— 打回后上一轮那条 task 是 stale,
// 它不是"我办过",是"那一轮作废了"(D4)。
func (s *approvalQueryService) myTaskStatuses(
	ctx context.Context, userID uint64, instanceIDs []uint64,
) (map[string]string, error) {
	out := make(map[string]string, len(instanceIDs))
	if len(instanceIDs) == 0 || userID == 0 {
		return out, nil
	}
	var rows []model.SysApprovalTask
	if err := s.db.WithContext(ctx).
		Where("instance_id IN ? AND assignee_user_id = ? AND status IN ?",
			instanceIDs, userID,
			[]string{model.ApprovalTaskPending, model.ApprovalTaskApproved, model.ApprovalTaskRejected}).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批待办失败").WithDetail(err.Error())
	}
	for _, t := range rows {
		key := strconv.FormatUint(t.InstanceID, 10)
		if t.Status == model.ApprovalTaskPending {
			out[key] = model.ApprovalTaskPending
			continue
		}
		if _, ok := out[key]; !ok {
			out[key] = t.Status
		}
	}
	return out, nil
}

// loadStages 取模板的关卡
func (s *approvalQueryService) loadStages(
	ctx context.Context, templateID uint64,
) ([]model.SysApprovalStage, error) {
	var stages []model.SysApprovalStage
	if err := s.db.WithContext(ctx).
		Where("template_id = ?", templateID).
		Order("stage_index ASC").
		Find(&stages).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批关卡失败").WithDetail(err.Error())
	}
	return stages, nil
}

// loadTasks 取实例的 task
//
// ⚠️ **只取非 stale 的** —— stale 是上一轮的作废票(§3.2 / D4),
// 混进来会让"共 N 人已批 M 人"算错。
func (s *approvalQueryService) loadTasks(
	ctx context.Context, instanceID uint64,
) ([]model.SysApprovalTask, error) {
	var tasks []model.SysApprovalTask
	if err := s.db.WithContext(ctx).
		Where("instance_id = ? AND status <> ?", instanceID, model.ApprovalTaskStale).
		Order("stage_index ASC, id ASC").
		Find(&tasks).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批任务失败").WithDetail(err.Error())
	}
	return tasks, nil
}

// loadRounds 取轮次快照
func (s *approvalQueryService) loadRounds(
	ctx context.Context, instanceID uint64,
) ([]ApprovalRoundView, error) {
	var rows []model.SysApprovalRound
	if err := s.db.WithContext(ctx).
		Where("instance_id = ?", instanceID).
		Order("round_no ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批轮次失败").WithDetail(err.Error())
	}
	out := make([]ApprovalRoundView, 0, len(rows))
	for _, r := range rows {
		out = append(out, ApprovalRoundView{
			RoundNo:     r.RoundNo,
			Title:       r.Title,
			Variables:   r.Variables,
			SubmitterID: strconv.FormatUint(r.SubmitterID, 10),
			SubmittedAt: r.SubmittedAt,
			Outcome:     r.Outcome,
			OutcomeBy:   strconv.FormatUint(r.OutcomeBy, 10),
			OutcomeAt:   r.OutcomeAt,
		})
	}
	return out, nil
}

// loadLogs 取意见流水
//
// ⚠️ **排序按 `round_no, id`,不按 `created_at`** —— 同一轮里秒级精度内
// 可能写了一批 log,只按 created_at 排顺序会飘(§6.1 ⑥ 的注释)。
func (s *approvalQueryService) loadLogs(
	ctx context.Context, instanceID uint64,
) ([]ApprovalLogView, error) {
	var rows []model.SysApprovalLog
	if err := s.db.WithContext(ctx).
		Where("instance_id = ?", instanceID).
		Order("round_no ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询审批意见失败").WithDetail(err.Error())
	}
	ops := make([]uint64, 0, len(rows))
	for _, l := range rows {
		ops = append(ops, l.OperatorUserID)
	}
	names, err := s.userNames(ctx, ops)
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalLogView, 0, len(rows))
	for _, l := range rows {
		opKey := strconv.FormatUint(l.OperatorUserID, 10)
		out = append(out, ApprovalLogView{
			ID:             strconv.FormatUint(l.ID, 10),
			StageIndex:     l.StageIndex,
			RoundNo:        l.RoundNo,
			Action:         l.Action,
			OperatorUserID: opKey,
			OperatorName:   names[opKey],
			Comment:        l.Comment,
			CreatedAt:      l.CreatedAt,
		})
	}
	return out, nil
}

// buildStageViews 装配关卡视图
//
// ⚠️ **关卡状态是"展示口径",不是判定依据** —— 判定一律看 task 表(§4.1)。
// 前端拿它画进度条,后端不拿它做任何决定。
func (s *approvalQueryService) buildStageViews(
	ctx context.Context,
	stages []model.SysApprovalStage,
	tasks []model.SysApprovalTask,
	inst model.SysApprovalInstance,
) ([]ApprovalStageView, error) {
	// 按关卡序号归拢 task
	byStage := make(map[int][]model.SysApprovalTask, len(stages))
	for _, t := range tasks {
		byStage[t.StageIndex] = append(byStage[t.StageIndex], t)
	}
	// 用户名
	ids := make([]uint64, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.AssigneeUserID)
	}
	names, err := s.userNames(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make([]ApprovalStageView, 0, len(stages))
	for _, st := range stages {
		sv := ApprovalStageView{
			StageIndex: st.StageIndex,
			Name:       st.Name,
			ReviewType: st.ReviewType,
			Assignees:  make([]ApprovalAssigneeView, 0, len(byStage[st.StageIndex])),
		}
		for _, t := range byStage[st.StageIndex] {
			sv.Assignees = append(sv.Assignees, ApprovalAssigneeView{
				UserID:     strconv.FormatUint(t.AssigneeUserID, 10),
				Name:       names[strconv.FormatUint(t.AssigneeUserID, 10)],
				Status:     t.Status,
				ActionedAt: t.ActionedAt,
			})
			if t.Status == model.ApprovalTaskApproved {
				sv.ApprovedCount++
			}
		}
		sv.TotalCount = len(sv.Assignees)
		sv.Status = stageDisplayStatus(st.StageIndex, byStage[st.StageIndex], inst)
		out = append(out, sv)
	}
	return out, nil
}

// stageDisplayStatus 某关的展示状态
//
// ⚠️ **四个分支的顺序是有讲究的**:
//
//	① 本关有人被拒 → "rejected"(单子在这一关就停了)
//	② 实例已终局且通过 → 全部 "done"
//	③ current_stage == 0(草稿 / 终局) → 其余 "pending"
//	④ 按序号比:过了的 done、当前关 current、后面的 pending
func stageDisplayStatus(
	stageIndex int, tasks []model.SysApprovalTask, inst model.SysApprovalInstance,
) string {
	for _, t := range tasks {
		if t.Status == model.ApprovalTaskRejected {
			return "rejected"
		}
	}
	if inst.Status == model.ApprovalStatusApproved {
		return "done"
	}
	if inst.CurrentStage == 0 {
		return "pending"
	}
	switch {
	case stageIndex < inst.CurrentStage:
		return "done"
	case stageIndex == inst.CurrentStage:
		return "current"
	default:
		return "pending"
	}
}
