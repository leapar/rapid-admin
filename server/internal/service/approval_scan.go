// 通用审批系统 —— 超时巡检(cronx 任务,M88)
//
// 文档:docs/通用审批系统设计方案V1.0.md §9.4(超时巡检)
//
// 🔴 **只告警,绝不自动改单子**(§1 第 2、6 条)
// 「审批人离职了 → 自动转办」「单子卡了 → 自动降级」这类"聪明的"修复
// 会把一条正在被人认真审的单子改掉,而当事人根本不知道。V1 只报不改。
//
// ⚠️ **注册在 main 装配期调一次,不是 init()** —— §9.4 允许"包 init 或
// main 装配期",本仓的既有惯例是后者(`pkg/jobs.RegisterAll`)。
// 选它的理由:**依赖能进闭包**。init() 注册的话,db 和站内信通道只能
// 靠包级变量事后注入 —— 那是全局可变状态 + 竞态面,而这里完全不需要。
// 代价:`RegisterApprovalScanJob` 必须在 `cronx.New / Start` **之前**调,
// 否则任务永远没有 entry(NextRunAt 是空的,而且没有任何报错)。
//
// ⚠️ **cron 表达式是 5 段,不是 6 段** —— cronx 的解析器是
// `cron.NewParser(cron.Minute|Hour|Dom|Month|Dow|Descriptor)`,
// **没有 Seconds**。设计稿 §9.4 的示例写的是 `"0 10 3 * * *"`(6 段),
// 那个表达式在本仓解析器上**直接报错**。见 §13 的 M88 偏离表。
//
// ⚠️ **`NonIdempotent` 留零值(false)** —— 本任务只发告警、不改单,
// 跑两遍结果一样。填成 true 反而会在 Redis 锁降级时被整个跳过,
// 告警就静默不发了(§9.4 的四条硬约束之一)。
//
// ⚠️ **Handler 必须尊重 ctx** —— cronx 的锁到期后第二个副本会抢走同一批
// 数据(`cronx.go:121`)。不检查 ctx 的任务会一直跑,两个副本并发写。
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/logger"
)

// KeyApprovalTimeoutScan 巡检任务的 key
//
// ⚠️ **前缀必须是 `framework.*`** —— 审批已被设计稿定为框架层,
// 用 `biz.*` 会让运维按业务任务分类,排查时找不到(§9.4)。
const KeyApprovalTimeoutScan = "framework.approval.timeout_scan"

// approvalStuckDays 停留超过多少天算"卡住"(§9.4 第 2 条)
const approvalStuckDays = 7

// 告警的 ref_type —— 站内信用它做去重键(§9.4 幂等)
//
// ⚠️ **两类告警必须用不同的 ref_type** —— 同一张单可能同时命中两条
// (审批人全离职 + 停了 8 天),共用一个 ref_type 会让第二条被第一条的
// 去重记录挡掉。
const (
	refTypeApprovalNoApprover = "approval_no_approver"
	refTypeApprovalStalled    = "approval_stalled"
)

// RegisterApprovalScanJob 注册审批超时巡检任务(main 装配期调**一次**)
//
// ⚠️ **必须在 `cronx.New / Start` 之前调** —— cronx 的注册表是"启动时
// 一次性快照"(`scheduler.go:227` 的 registeredKeys())。
// 晚注册的任务永远没有 entry,NextRunAt 是空的,而且**没有任何报错**。
//
// ⚠️ **重复注册同一个 key 会 panic**(`registry.go:33`)—— 这是故意的:
// 复制粘贴出一个重复 key 应该在启动时炸,而不是让后一个悄悄覆盖前一个。
// 写测试时**不要**调这个函数(同一个进程里调两次照样炸)—— 直接调
// `scanStaleApprovals` 测逻辑。
func RegisterApprovalScanJob(db *gorm.DB, notifier inbox.Notifier) {
	cronx.Register(cronx.Job{
		Key:  KeyApprovalTimeoutScan,
		Name: "审批超时巡检",
		Description: "扫描长时间停留的审批实例并告警,不自动改单。" +
			"两类告警:本关审批人全部停用/离职 → 告警平台管理员;" +
			"同一单连续停留超过 7 天 → 告警发起人。",
		// 03:10 —— 避开整点(全仓约定,见 pkg/jobs 的包注释「错开整点」),
		// 也与已有的 04:17 / 09:23 两个框架任务错开。
		Cron:    "10 3 * * *",
		Enabled: true,
		// ⚠️ **NonIdempotent 留零值** —— 只发告警不改单,天然幂等。
		NonIdempotent: false,
		Handler: func(ctx context.Context) error {
			return scanStaleApprovals(ctx, db, notifier)
		},
	})
}

// scanStaleApprovals 巡检主流程
//
// ⚠️ **入参而不是包级变量** —— 依赖进参数,测试就能直接调它,
// 不用装配全局状态。
func scanStaleApprovals(ctx context.Context, db *gorm.DB, notifier inbox.Notifier) error {
	if db == nil {
		return errors.New("approval: 巡检缺少 db")
	}
	if notifier == nil {
		return errors.New("approval: 巡检缺少站内信通道(notifier)")
	}
	s := &approvalScanner{db: db, notifier: notifier}

	var insts []model.SysApprovalInstance
	if err := db.WithContext(ctx).
		Where("status = ?", model.ApprovalStatusRunning).
		Order("id ASC").
		Find(&insts).Error; err != nil {
		return err
	}

	var noApprover, stalled int
	for i := range insts {
		inst := &insts[i]
		// ⚠️ **每条都查一次 ctx** —— cronx 的锁到期后第二个副本会接手,
		// 不看 ctx 就会两个副本同时往下发告警。
		if err := ctx.Err(); err != nil {
			return err
		}

		assignees, err := s.currentStageAssignees(ctx, inst)
		if err != nil {
			return err
		}
		// ⚠️ **0 个待办人 ≠ "全部离职"** —— 空集上做"全部"判断是恒真,
		// 会给每一条数据异常的实例都发告警。空集直接跳过。
		if len(assignees) > 0 {
			off, err := s.allAssigneesInactive(ctx, assignees)
			if err != nil {
				return err
			}
			if off {
				sent, err := s.alertNoApprover(ctx, inst, assignees)
				if err != nil {
					return err
				}
				if sent {
					noApprover++
				}
			}
		}

		days, ok := s.stuckDays(inst)
		if ok && days >= approvalStuckDays {
			sent, err := s.alertStalled(ctx, inst, days, assignees)
			if err != nil {
				return err
			}
			if sent {
				stalled++
			}
		}
	}

	logger.L().Info("approval: 超时巡检完成",
		zap.Int("running_instances", len(insts)),
		zap.Int("alert_no_approver", noApprover),
		zap.Int("alert_stalled", stalled))
	return nil
}

// approvalScanner 巡检器
type approvalScanner struct {
	db       *gorm.DB
	notifier inbox.Notifier
}

// currentStageAssignees 当前关的待办人
func (s *approvalScanner) currentStageAssignees(
	ctx context.Context, inst *model.SysApprovalInstance,
) ([]uint64, error) {
	var ids []uint64
	err := s.db.WithContext(ctx).
		Model(&model.SysApprovalTask{}).
		// ⚠️ **`status='pending'` 不能少** —— 上一轮打回后 task 是 `stale`,
		// 把它算进"当前关的审批人"会得出"这些人全离职了"的错误结论。
		Where("instance_id = ? AND stage_index = ? AND status = ?",
			inst.ID, inst.CurrentStage, model.ApprovalTaskPending).
		Distinct().
		Pluck("assignee_user_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// allAssigneesInactive 这批人是不是全都停用/离职了
func (s *approvalScanner) allAssigneesInactive(ctx context.Context, ids []uint64) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	// ⚠️ **数"还在的人"而不是数"走了的人"** —— 少一个还在的人就不算
	// 全部离职,判据是"一个都不剩"。
	var alive int64
	err := s.db.WithContext(ctx).
		Model(&model.SysUser{}).
		Where("id IN ?", ids).
		Where("is_active = ? AND is_leave = ?", true, false).
		Count(&alive).Error
	if err != nil {
		return false, err
	}
	return alive == 0, nil
}

// stuckDays 这条单子已经停了几天
//
// ⚠️ **submitted_at 为空时返回 false** —— 拿零值时间算天数会得到一个
// 巨大的数字,把每一条数据异常的实例都当成"卡了 20000 天"。
func (s *approvalScanner) stuckDays(inst *model.SysApprovalInstance) (int, bool) {
	if inst.SubmittedAt == nil || inst.SubmittedAt.IsZero() {
		return 0, false
	}
	d := time.Since(*inst.SubmittedAt)
	if d < 0 {
		// submitted_at 在未来:时钟漂移或数据写错。不当成"卡了 0 天"。
		return 0, false
	}
	return int(d.Hours() / 24), true
}

// alertNoApprover 告警:本关审批人全部停用/离职
//
// 收件人:超管 + 平台运营员
func (s *approvalScanner) alertNoApprover(
	ctx context.Context, inst *model.SysApprovalInstance, assignees []uint64,
) (bool, error) {
	// ⚠️ **去重先于发信** —— 这个任务每天跑一次,不去重的话一条卡住的
	// 单子会给管理员发 7 遍同样的告警。去重键 = (ref_type, ref_id,
	// 本轮提交时间之后)。
	if s.alreadyAlerted(ctx, refTypeApprovalNoApprover, inst) {
		return false, nil
	}

	recipients, err := s.usersWithRole(ctx, model.RoleSuperAdmin, model.RolePlatformOps)
	if err != nil {
		return false, err
	}
	if len(recipients) == 0 {
		// ⚠️ **没人可告警不是错误** —— 可能是没配这两个角色。
		// 记一条 Warn 就走,不能因此让整个任务失败(那会让运维以为
		// 巡检坏了,实际上只是没有收件人)。
		logger.L().Warn("approval: 无人可接收「审批人全离职」告警",
			zap.Uint64("instance_id", inst.ID))
		return false, nil
	}

	days, _ := s.stuckDays(inst)
	content := fmt.Sprintf(
		"审批单 #%d(%s / %d)当前第 %d 关的审批人 %s 全部停用或离职,已停留 %d 天。"+
			"单子不会有人处理,请人工介入(转办或调整流程配置)。",
		inst.ID, inst.BizType, inst.BizID, inst.CurrentStage,
		formatUserIDs(assignees), days)

	msgs := make([]inbox.Message, 0, len(recipients))
	for _, uid := range recipients {
		msgs = append(msgs, inbox.Message{
			UserID:  uid,
			MsgType: "system",
			Title:   "审批待办人全部离职",
			Content: content,
			Link:    "/approval/" + strconv.FormatUint(inst.ID, 10),
			RefType: refTypeApprovalNoApprover,
			RefID:   inst.ID,
		})
	}
	if err := s.notifier.NotifyBatch(ctx, msgs); err != nil {
		return false, err
	}
	return true, nil
}

// alertStalled 告警:同一单连续停留超过 7 天
//
// 收件人:发起人(§9.4 第 2 条)
func (s *approvalScanner) alertStalled(
	ctx context.Context, inst *model.SysApprovalInstance, days int, assignees []uint64,
) (bool, error) {
	if s.alreadyAlerted(ctx, refTypeApprovalStalled, inst) {
		return false, nil
	}
	if inst.ApplicantUserID == 0 {
		return false, nil
	}

	content := fmt.Sprintf(
		"你发起的审批单 #%d(%s / %d)已停留 %d 天,当前在第 %d 关,待办人 %s。"+
			"如果内容需要修改,可以直接改完重新提交。",
		inst.ID, inst.BizType, inst.BizID, days, inst.CurrentStage,
		formatUserIDs(assignees))

	err := s.notifier.Notify(ctx, inbox.Message{
		UserID:  inst.ApplicantUserID,
		MsgType: "system",
		Title:   "审批单长时间未处理",
		Content: content,
		Link:    "/approval/" + strconv.FormatUint(inst.ID, 10),
		RefType: refTypeApprovalStalled,
		RefID:   inst.ID,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// alreadyAlerted 本轮是不是已经告警过了
//
// ⚠️ **问的是「有没有告警发在「本次提交」之后」** —— 有 = 这一轮已经提醒过
// 了,跳过。**方向不能反**:写成「有没有告警早于本次提交」的话,
// 上一轮的告警会把这一轮也挡掉,一条单子一辈子只能被提醒一次。
//
// ⚠️ **发起人改完重提后 submitted_at 会刷新** —— 新一轮的 submitted_at 晚于
// 上一轮的告警时间,于是「告警在提交之后」不成立,那一轮就能重新提醒。
//
// ⚠️ **submitted_at 为空时不加时间下界** —— 拿 Go 的零值时间(公元 1 年)
// 去比一列 datetime,能不能比出正确结果取决于驱动的序列化格式,
// 那是拿"能不能查到"赌驱动行为。宁可只按 (ref_type, ref_id) 去重:
// 这种实例本来就是坏数据,提醒一次也就够了。
func (s *approvalScanner) alreadyAlerted(
	ctx context.Context, refType string, inst *model.SysApprovalInstance,
) bool {
	q := s.db.WithContext(ctx).
		Model(&model.InboxMessage{}).
		Where("ref_type = ? AND ref_id = ?", refType, inst.ID)
	if inst.SubmittedAt != nil && !inst.SubmittedAt.IsZero() {
		q = q.Where("created_at >= ?", *inst.SubmittedAt)
	}
	var n int64
	err := q.Count(&n).Error
	if err != nil {
		// ⚠️ **查不到就当"没告警过"** —— 去重失败时多发一次告警,
		// 远好过因为一次查询错误把告警静默吞掉。
		logger.L().Warn("approval: 告警去重查询失败,按未告警处理",
			zap.String("ref_type", refType),
			zap.Uint64("instance_id", inst.ID),
			zap.Error(err))
		return false
	}
	return n > 0
}

// usersWithRole 找出拥有这些角色的用户(去重、只取在职的)
//
// ⚠️ **抄 `approval_resolver_impl.go:174-207` 的写法** —— JOIN sys_user 而不是
// 用子查询。两点原因:
// ① JOIN 侧要自己写 `u.deleted_at IS NULL` —— GORM 只给**主 model** 加
// 软删条件,JOIN 进来的表它管不着;
// ② **身份有效期(valid_from / valid_to)在 Go 里判** —— 这两列零值是
// 公元 1 年,写成 SQL 条件 `valid_to >= now` 会让所有人一个都查不出来。
//
// ⚠️ **不用 Pluck** —— GORM 的 `Pluck` 会**立刻执行查询**,写在它后面的
// Where 条件不会生效(条件加在已经跑完的查询上)。
func (s *approvalScanner) usersWithRole(ctx context.Context, roleCodes ...string) ([]uint64, error) {
	if len(roleCodes) == 0 {
		return nil, nil
	}
	type identityRow struct {
		UserID    uint64
		ValidFrom model.Time
		ValidTo   model.Time
	}
	var rows []identityRow
	err := s.db.WithContext(ctx).
		Model(&model.SysUserIdentity{}).
		Select("sys_user_identity.user_id AS user_id, "+
			"sys_user_identity.valid_from AS valid_from, "+
			"sys_user_identity.valid_to AS valid_to").
		Joins("JOIN sys_user u ON u.id = sys_user_identity.user_id AND u.deleted_at IS NULL").
		Where("sys_user_identity.role_code IN ? AND sys_user_identity.is_active = ?"+
			" AND u.is_active = ? AND u.is_leave = ?",
			roleCodes, true, true, false).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	now := time.Now()
	seen := make(map[uint64]struct{}, len(rows))
	out := make([]uint64, 0, len(rows))
	for _, row := range rows {
		if row.ValidFrom.Time.After(now) {
			continue // 未到生效时间
		}
		if !row.ValidTo.IsZero() && row.ValidTo.Time.Before(now) {
			continue // 已过期
		}
		if _, dup := seen[row.UserID]; dup {
			continue // 一个人可能同时是两个角色的成员,只收一次
		}
		seen[row.UserID] = struct{}{}
		out = append(out, row.UserID)
	}
	return out, nil
}

// formatUserIDs 把 user id 列表拼成人能看的样子
func formatUserIDs(ids []uint64) string {
	if len(ids) == 0 {
		return "(无)"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, "#"+strconv.FormatUint(id, 10))
	}
	return strings.Join(parts, "、")
}
