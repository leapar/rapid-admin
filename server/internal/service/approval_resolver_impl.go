// 通用审批系统 —— 审批人解析实现(M85)
//
// 文档:docs/通用审批系统设计方案V1.0.md §5.1(三种来源)/ §5.2(三条硬规则)/
// §5.3(空审批人三层拦截)
//
// 🔴 **这个文件里最重要的一条是「error ≠ 空集合」**(§5.2 第 1 条)。
// 每一条 SQL 出错都往上抛,**没有一条把 err 当成"没人"往下走**。
// 理由在 §5.3:静默把单子发给一个无关的人(或干脆放行)比报错糟糕得多 ——
// 报错至少让申请人知道该找谁。所以下面每处 `if err != nil` 都是 return,
// 没有一处 `return nil, nil` 紧跟着 err 分支。
package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// maxDeptWalkSteps 部门链向上爬的硬上限(§5.2 第 2 条"走满 10 步")
//
// ⚠️ **真正的判据是访问集合,不是这个数。** 访问集合命中 100% 是成环,
// 不会有假阳性;这个上限是二道兜底 —— 它先触发说明链比 10 级还深,
// 那本身就该有人来看一眼,所以报同一个错(而不是静默截断)。
const maxDeptWalkSteps = 10

// approvalResolver 审批人解析器实现
type approvalResolver struct {
	db *gorm.DB
}

var (
	_ ApprovalResolver = (*approvalResolver)(nil)
)

// NewApprovalResolver 构造。**无状态** —— 每次解析都现查库,不缓存。
//
// ⚠️ **不缓存是 §5.2 的要求**,不是偷懒:模板一改 / 组织架构一调,缓存就得有失效
// 通道,而审批单是"发起那一瞬间的快照"语义(§5.2 开头),中途换人本来就是允许的。
// 真要缓存,先想清楚「部门调整后在跑的流程怎么办」(§9.4 的告警兜底)。
func NewApprovalResolver(db *gorm.DB) ApprovalResolver {
	return &approvalResolver{db: db}
}

// ============================================================================
// 引擎侧:解析某一关(§5.1)
// ============================================================================

// ResolveStage 按 §5.1 的三种来源解析一关的审批人。
//
// ⚠️ **返回的列表不去重、不剔发起人** —— 那两件事归引擎
// (`resolveAssignees`:D12 按 user_id 去重 / D11 提交人撞审批人直接报错)。
// 解析器在这里做一遍的话,配置期预检会跟着被污染(预检只看"有没有 ≥1 人",
// 而 D11 是运行期的错误,不该在配置期就报)。
func (r *approvalResolver) ResolveStage(
	ctx context.Context, db *gorm.DB, req ResolveStageRequest,
) ([]uint64, error) {
	if db == nil {
		return nil, errcode.New(errcode.Internal,
			"解析审批人必须传 db —— 用解析器自带的连接会绕开审批事务(D8)")
	}
	if req.StageID == 0 {
		return nil, errcode.New(errcode.Internal, "解析审批人缺少关卡 ID")
	}

	var rules []model.SysApprovalAssigneeRule
	err := db.WithContext(ctx).
		Where("stage_id = ?", req.StageID).
		Order("seq ASC, id ASC").
		Find(&rules).Error
	if err != nil {
		// 🔴 **查不到 ≠ 没人**(§5.2 第 1 条)。这条 err 必须往上抛。
		return nil, errcode.New(errcode.Internal, "查询审批人规则失败").WithDetail(err.Error())
	}
	if len(rules) == 0 {
		// 查得到、结果为空 —— 数据常态(这一关确实没配规则),归空集合
		return nil, nil
	}

	out := make([]uint64, 0, len(rules))
	for _, rule := range rules {
		ids, err := r.resolveRule(ctx, db, req, rule)
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
	}
	return out, nil
}

// resolveRule 单条规则 → 人
func (r *approvalResolver) resolveRule(
	ctx context.Context, db *gorm.DB, req ResolveStageRequest, rule model.SysApprovalAssigneeRule,
) ([]uint64, error) {
	switch rule.RuleType {
	case model.ApprovalRuleUser:
		return r.resolveUser(ctx, db, rule)
	case model.ApprovalRuleRole:
		return r.resolveRole(ctx, db, rule)
	case model.ApprovalRuleDeptLeader:
		return r.resolveDeptLeader(ctx, db, req, rule)
	default:
		// 🔴 未知 rule_type = 库里进了垃圾,不是"没人"。报错,别当空集合。
		// (合法词表就三个:model/approval.go:80-87)
		return nil, ErrApprovalRuleInvalid.WithDetail(
			"第 " + strconv.Itoa(req.StageIndex) + " 关规则 #" +
				strconv.FormatUint(rule.ID, 10) + " 的 rule_type=" + rule.RuleType + " 不在词表内")
	}
}

// resolveUser `user` 规则:就是这个人(§5.1)
//
// ⚠️ **在职过滤在这里,不在引擎里**(§5.2 第 3 条)。停用/离职/已删的人不生成
// task —— 注意是「查到了但不算数」,不是报错:配模板时他还在职,提交时已经离职,
// 这属于数据常态。
func (r *approvalResolver) resolveUser(
	ctx context.Context, db *gorm.DB, rule model.SysApprovalAssigneeRule,
) ([]uint64, error) {
	if rule.UserID == 0 {
		return nil, ErrApprovalRuleInvalid.WithDetail("user 规则没有填 user_id")
	}
	var u model.SysUser
	err := db.WithContext(ctx).
		Select("id").
		Where("id = ? AND is_active = ? AND is_leave = ?", rule.UserID, true, false).
		First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 人不在 / 停用 / 离职 / 已软删 —— 空集合,不是故障
		}
		return nil, errcode.New(errcode.Internal, "查询指定审批人失败").WithDetail(err.Error())
	}
	return []uint64{u.ID}, nil
}

// resolveRole `role` 规则:该角色下所有在职用户(§5.1)
func (r *approvalResolver) resolveRole(
	ctx context.Context, db *gorm.DB, rule model.SysApprovalAssigneeRule,
) ([]uint64, error) {
	if rule.RoleID == 0 {
		return nil, ErrApprovalRuleInvalid.WithDetail("role 规则没有填 role_id")
	}
	var role model.SysRole
	err := db.WithContext(ctx).
		Select("id, code, is_active").
		First(&role, "id = ?", rule.RoleID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 🔴 角色不存在 = 规则指向了一个空的东西。**这不是"该角色没人"**,
			// 是配置坏了 —— 报错比空集合有用得多(空集合只会变成 §5.3② 的一句
			//「解析不到审批人」,把锅甩给流程管理员而不是指出角色被删了)。
			return nil, ErrApprovalRuleInvalid.WithDetail(
				"role 规则指向的角色不存在:role_id=" + strconv.FormatUint(rule.RoleID, 10))
		}
		return nil, errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	if !role.IsActive {
		// 角色被停用 —— 同上,配置坏了(人还在,只是这个角色不生效了)
		return nil, ErrApprovalRuleInvalid.WithDetail(
			"role 规则指向的角色已停用:role=" + role.Code)
	}

	type identityRow struct {
		UserID    uint64
		ValidFrom model.Time
		ValidTo   model.Time
	}
	var rows []identityRow
	err = db.WithContext(ctx).
		Model(&model.SysUserIdentity{}).
		Select("sys_user_identity.user_id AS user_id, "+
			"sys_user_identity.valid_from AS valid_from, "+
			"sys_user_identity.valid_to AS valid_to").
		// ⚠️ JOIN 侧的软删得自己写 —— GORM 只给**主 model** 加
		// `deleted_at IS NULL`,`sys_user` 是 JOIN 进来的,它管不着。
		Joins("JOIN sys_user u ON u.id = sys_user_identity.user_id AND u.deleted_at IS NULL").
		Where("sys_user_identity.role_code = ? AND sys_user_identity.is_active = ?"+
			" AND u.is_active = ? AND u.is_leave = ?",
			role.Code, true, true, false).
		Find(&rows).Error
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询角色成员失败").WithDetail(err.Error())
	}
	if len(rows) == 0 {
		return nil, nil // 查得到、结果为空:这个角色下没人(数据常态)
	}

	// 🔴 **有效期在 Go 里判,不在 SQL 里判**(§5.2 第 3 条)。
	//
	// 抄的是 `auth_impl.go:797-800` 的既有写法(切身份时判的就是这两行)。
	// **不能写成 `valid_to >= now` 的 SQL 条件** —— `valid_to` 零值是公元 1 年
	// (本仓所有 identity 都可能没有有效期),SQL 里比出来永远是"已过期",
	// 于是**所有人一个都查不出来**,而症状是"该财务审的单子没人审"。
	now := time.Now()
	out := make([]uint64, 0, len(rows))
	for _, row := range rows {
		if row.ValidFrom.Time.After(now) {
			continue // 未到生效时间
		}
		if !row.ValidTo.IsZero() && row.ValidTo.Time.Before(now) {
			continue // 已过期
		}
		out = append(out, row.UserID)
	}
	return out, nil
}

// resolveDeptLeader `dept_leader` 规则:从发起人所在部门逐级向上找负责人(§5.1)
//
// ⚠️ **`levels` 的定义**:`levels=1` 即**自己所在部门**的主管(直属主管),
// `levels=2` 是上级部门的主管,`levels<=0` 视作**一直爬到根部门**。
// 也就是「先爬 `levels-1` 级,再取那一级的主管」。
//
// ⚠️ **爬不到那么多级 = 空集合,不是报错**(§5.2 第 1 条白纸黑字列了"超出层级")。
// 但**爬的过程中撞见环或 parent_id 悬空 = 报错**(§5.2 第 2 条)。
// 这两件事必须分开:前者是正常组织形态,后者是数据坏了。
func (r *approvalResolver) resolveDeptLeader(
	ctx context.Context, db *gorm.DB, req ResolveStageRequest, rule model.SysApprovalAssigneeRule,
) ([]uint64, error) {
	startID, err := r.applicantDeptID(ctx, db, req)
	if err != nil {
		return nil, err
	}
	if startID == 0 {
		// 申请人没部门 = 查得到、结果为空(数据常态)。这一条规则贡献 0 人,
		// 整关没有别人兜底的话,§5.3② 的提交期拦截会报出来。
		return nil, nil
	}

	depts, err := r.deptMap(ctx, db)
	if err != nil {
		return nil, err
	}

	target, ok, err := walkUpDept(depts, startID, rule.Levels)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil // 层级不够(§5.2 第 1 条的"超出层级")
	}
	if target.LeaderID == 0 {
		return nil, nil // 这一级没配负责人 —— 空集合,不是故障
	}
	return r.resolveUser(ctx, db, model.SysApprovalAssigneeRule{
		RuleType: model.ApprovalRuleUser,
		UserID:   target.LeaderID,
	})
}

// applicantDeptID 定位申请人所在部门。
//
// ⚠️ **优先用 `sys_user_identity.dept_id`** —— 审批单记的就是发起时那个身份,
// 而身份带部门;退路是 `sys_user_department` 的主部门。
func (r *approvalResolver) applicantDeptID(
	ctx context.Context, db *gorm.DB, req ResolveStageRequest,
) (uint64, error) {
	if req.ApplicantIdentityID != 0 {
		var id model.SysUserIdentity
		err := db.WithContext(ctx).Select("dept_id").
			First(&id, "id = ?", req.ApplicantIdentityID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errcode.New(errcode.Internal, "查询申请人身份失败").WithDetail(err.Error())
		}
		if err == nil && id.DeptID != 0 {
			return id.DeptID, nil
		}
	}

	// 退路:主部门优先,没标主部门就取第一条(与 `is_primary` 的语义一致:
	// 「主部门没维护」是常态,不是错误)
	var row model.SysUserDepartment
	err := db.WithContext(ctx).Select("dept_id").
		Where("user_id = ? AND is_primary = ?", req.ApplicantUserID, true).
		Order("id ASC").First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, errcode.New(errcode.Internal, "查询申请人部门失败").WithDetail(err.Error())
	}
	if err == nil && row.DeptID != 0 {
		return row.DeptID, nil
	}

	err = db.WithContext(ctx).Select("dept_id").
		Where("user_id = ?", req.ApplicantUserID).
		Order("id ASC").First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil // 没部门 —— 空,不是故障
		}
		return 0, errcode.New(errcode.Internal, "查询申请人部门失败").WithDetail(err.Error())
	}
	return row.DeptID, nil
}

// deptMap 读全量部门 → id→行。
//
// 🔴 **这里没有 `is_active` 过滤**,与 `roleService.DepartmentTree`
// (`role_impl.go:519`) 不同,那是故意的:
//
//	向上爬要**穿过**停用的中间部门才能到根。一旦把停用部门滤掉,一个
//	「A(启用) → B(停用) → C(根)」的链会在 B 处断掉,于是「爬到顶」返回 A 的
//	主管 —— **返回一个错的、静默的答案**。这正是 §5.3 要避免的那一类事故。
//
//	要停用某个人做审批人,靠 §5.2 第 3 条的 `sys_user` 过滤(他本人停用),
//	不是靠停用他所在的部门。
func (r *approvalResolver) deptMap(ctx context.Context, db *gorm.DB) (map[uint64]model.SysDepartment, error) {
	var rows []model.SysDepartment
	err := db.WithContext(ctx).
		Select("id, parent_id, name, leader_id").
		Find(&rows).Error
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询部门失败").WithDetail(err.Error())
	}
	// ⚠️ **复用 `role_impl.go:558` 的 `buildParentMap` 那套 id→行 查表思路**,
	// 但不调 `DepartmentTree` 本身 —— 它硬编码 `s.db`(会绕开审批事务),
	// 且额外查 leader_name / member_count 两条这里用不上的查询。
	// 部门表是全库最小的表之一(本仓 seed 17 个节点),一次全量读可接受。
	out := make(map[uint64]model.SysDepartment, len(rows))
	for _, d := range rows {
		out[d.ID] = d
	}
	return out, nil
}

// walkUpDept 沿 parent_id 向上爬,返回目标部门。
//
// 返回 `(部门, 命中?, error)`:
//   - `命中=false, err=nil` —— **层级不够**(§5.2 第 1 条「超出层级」= 空集合)
//   - `err != nil` —— 部门树成环 / parent_id 指向不存在的部门(§5.2 第 2 条)
func walkUpDept(
	depts map[uint64]model.SysDepartment, startID uint64, levels int,
) (model.SysDepartment, bool, error) {
	if _, ok := depts[startID]; !ok {
		return model.SysDepartment{}, false, ErrApprovalDeptTreeBroken.WithDetail(
			"申请人所在部门不存在:dept_id=" + strconv.FormatUint(startID, 10))
	}

	toTop := levels <= 0
	// levels=1 即本部门主管 —— 所以先爬 levels-1 级
	remaining := levels - 1

	cur := startID
	visited := make(map[uint64]bool, maxDeptWalkSteps)
	for {
		// 🔴 **环检测用访问集合,不用"跳 N 次"**(§5.2 第 2 条)。
		// `dept-seed` 的 parent_id 第二轮回填确实可能配出 A→B→A,那种链
		// 跳 10 次也回不了顶,只有集合命中才抓得住。
		if visited[cur] {
			return model.SysDepartment{}, false, ErrApprovalDeptTreeBroken.WithDetail(
				"部门树成环:自 dept_id=" + strconv.FormatUint(startID, 10) +
					" 向上走到了重复节点 " + strconv.FormatUint(cur, 10))
		}
		visited[cur] = true
		if len(visited) > maxDeptWalkSteps {
			return model.SysDepartment{}, false, ErrApprovalDeptTreeBroken.WithDetail(
				"部门链超过 " + strconv.Itoa(maxDeptWalkSteps) + " 级仍未到顶,请检查组织架构")
		}

		node := depts[cur]
		isTop := node.ParentID == 0
		switch {
		case toTop && isTop:
			return node, true, nil
		case !toTop && remaining == 0:
			return node, true, nil
		case !toTop && isTop:
			// ⚠️ 到顶了但还没爬够 —— §5.2 第 1 条的"超出层级",空集合不是错误
			return model.SysDepartment{}, false, nil
		}

		next, ok := depts[node.ParentID]
		if !ok {
			// parent_id 指向一个不存在的部门 —— 树不自洽,和成环同一类事故
			return model.SysDepartment{}, false, ErrApprovalDeptTreeBroken.WithDetail(
				"部门 " + node.Name + " 的上级 dept_id=" +
					strconv.FormatUint(node.ParentID, 10) + " 不存在")
		}
		cur = next.ID
		if !toTop {
			remaining--
		}
	}
}

// ============================================================================
// §5.3① 配置期预检
// ============================================================================

// PrecheckTemplate 模板保存前的逐关预检。
//
// ⚠️ **`dept_leader` 规则在这里一律跳过,判定不了。** 它的结果本质依赖发起人
// (§5.1),而保存模板时**还不知道发起人是谁** —— 拿一个假的发起人去解析,
// 只能得到一个假的结论。所以 `dept_leader` 的可解析性由 §5.3② 的提交期兜。
// 这不是漏做,是这条规则在配置期**客观上不可判定**。
func (r *approvalResolver) PrecheckTemplate(
	ctx context.Context, db *gorm.DB, in TemplatePrecheckInput,
) error {
	if db == nil {
		return errcode.New(errcode.Internal, "预检审批人必须传 db")
	}
	for _, stage := range in.Stages {
		if len(stage.Rules) == 0 {
			return precheckErr(in.TemplateCode, stage.StageIndex, "这一关没有配任何审批人规则")
		}
		// 至少一条「本机能判定」的规则解析出人 —— 多条规则是「或」(§5.1)
		judgeableCount, okAny := 0, false
		for _, rule := range stage.Rules {
			ids, judgeable, err := r.precheckRule(ctx, db, rule)
			if err != nil {
				return err
			}
			if !judgeable { // dept_leader —— 不可判定,跳过
				continue
			}
			judgeableCount++
			if len(ids) > 0 {
				okAny = true
			}
		}
		if judgeableCount == 0 {
			// 全是 dept_leader —— 判不了就放行,交给提交期
			continue
		}
		if !okAny {
			return precheckErr(in.TemplateCode, stage.StageIndex,
				"这一关的审批人规则当前都解析不出人(指定的人已停用/离职,或该角色下没有在职用户)")
		}
	}
	return nil
}

// precheckRule 预检单条规则。
//
// 🔴 **第二个返回值是「本层判不判得了」,不能用 `ids == nil` 代替** ——
// 第一版就是这么写的,当场抓到一个真 bug:`user` 规则指向一个已停用的人时
// `resolveUser` 返回 `(nil, nil)`,和 `dept_leader` 的 `(nil, nil)` 一模一样,
// 于是「这一关指定的人全停用了」被当成「不可判定」放过去 ——
// **§5.3① 这条防线整个失效,而且用例是绿的**。
func (r *approvalResolver) precheckRule(
	ctx context.Context, db *gorm.DB, rule model.SysApprovalAssigneeRule,
) ([]uint64, bool, error) {
	switch rule.RuleType {
	case model.ApprovalRuleUser:
		ids, err := r.resolveUser(ctx, db, rule)
		return ids, true, err
	case model.ApprovalRuleRole:
		ids, err := r.resolveRole(ctx, db, rule)
		return ids, true, err
	case model.ApprovalRuleDeptLeader:
		return nil, false, nil // 不可判定
	default:
		return nil, false, ErrApprovalRuleInvalid.WithDetail(
			"规则 #" + strconv.FormatUint(rule.ID, 10) +
				" 的 rule_type=" + rule.RuleType + " 不在词表内")
	}
}

func precheckErr(tplCode string, stageIndex int, reason string) error {
	prefix := "审批模板 " + tplCode + " 第 " + strconv.Itoa(stageIndex) + " 关:"
	if tplCode == "" {
		prefix = "审批模板第 " + strconv.Itoa(stageIndex) + " 关:"
	}
	return errcode.New(errcode.ApprovalNoAssignee, prefix+reason)
}
