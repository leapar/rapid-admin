// 通用审批系统 —— 引擎实现(M84)
//
// 文档:docs/通用审批系统设计方案V1.0.md §3(状态机)/ §4(会签或签)/ §16.5(锁骨架)
//
// ⚠️ **这个文件里没有一个业务表的名字** —— 它只认 `biz_type` + `biz_id`,
// 业务侧的事全走 `BizHandler` SPI。换一条业务线不用改这里一行代码。
//
// ⚠️ **每个写动作的第一行都是"锁住实例行"**(§3.5)。会签是并发审批,A 和 B
// 同时点通过时,只锁各自的 task 行两人会同时进去、都判定"还没到齐",单子永远
// 停在最后一关。锁实例行强制串行。
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// 锁等待的应用层超时(§3.5 第 3 条)
//
// ⚠️ MySQL 的 `lock_wait_timeout` 默认 50s,比这长 —— 不设应用层超时的话,
// 一次锁竞争会让 HTTP 请求挂 50s,网关先超时,而数据库那边的锁还在等。
// 超时**不重试、不排队**:用户重试的成本远低于一个挂死的请求。
const defaultApprovalLockTimeout = 30 * time.Second

// approvalEngine 审批引擎实现
type approvalEngine struct {
	db        *gorm.DB
	resolver  AssigneeResolver
	handlers  map[string]BizHandler
	publisher ApprovalPublisher

	lockTimeout time.Duration
}

var _ ApprovalEngine = (*approvalEngine)(nil)

// NewApprovalEngine 构造。
//
// ⚠️ **四个参数全部有调用方,没有一个是为将来准备的**:
// `resolver` submit / 推进时解析审批人;`handlers` 三个 SPI;
// `publisher` COMMIT 后发事件。`resolver` 与 `publisher` 传 nil 会在用到的
// 路径上明确报错,不会 panic。
func NewApprovalEngine(
	db *gorm.DB,
	resolver AssigneeResolver,
	handlers map[string]BizHandler,
	publisher ApprovalPublisher,
) ApprovalEngine {
	return &approvalEngine{
		db:          db,
		resolver:    resolver,
		handlers:    handlers,
		publisher:   publisher,
		lockTimeout: defaultApprovalLockTimeout,
	}
}

// ============================================================================
// 事务与行锁(§16.5)
// ============================================================================

// withLockedInstance 开事务 → 锁实例行 → 跑 fn。
//
// ⚠️ **fn 返回 error = 整个事务回滚**,fn 里的一切落库都作废。
// 这就是 D8 能成立的原因:`OnApproved` 在同一个 tx 上写业务表。
func (e *approvalEngine) withLockedInstance(
	ctx context.Context,
	id uint64,
	fn func(tx *gorm.DB, inst *model.SysApprovalInstance) error,
) error {
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// ⚠️ 30s 超时挂在**锁的 ctx** 上,不是事务的 ctx 上 —— 后者会被 GORM
		// 在 COMMIT 之后 cancel,挂在它上面的超时会在 COMMIT 那一刻失效。
		// (这句话的"理由"在 gorm@v1.31.1 上查无实据,见 approval.go:267-269;
		//  结论仍然成立:tx 的 ctx 一旦随 COMMIT 失效,超时也随之失效。)
		lockCtx, cancel := context.WithTimeout(ctx, e.lockTimeout)
		defer cancel()

		var inst model.SysApprovalInstance
		// ⚠️ 锁在**实例行**上,不是 task 行上(§3.5 / D10)
		err := tx.WithContext(lockCtx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).
			First(&inst).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errcode.New(errcode.NotFound, "审批单不存在")
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return ErrApprovalLocked
			}
			return err
		}
		return fn(tx, &inst)
	})
}

// publish COMMIT 之后发事件(§9.2 / §16.5)
//
// ⚠️ **`context.WithoutCancel` 不是可选的**。
//
// ⚠️ **设计文档 §9.2 的原话是「GORM 在 COMMIT 之后会 cancel 事务的 ctx」——
// 这一句在 gorm@v1.31.1 上不成立**(核实过:整个 gorm@v1.31.1 没有一处
// `WithoutCancel`,也没有对事务 ctx 的 cancel 调用)。**所以那条理由是假的,
// 但结论仍然成立** —— 真正会让 ctx 死掉的是**调用方**:HTTP 请求超时、
// 上游断开,`ctx.Err() != nil`,拿它写 Redis 直接失败,表现是
// 「审批成功了但前端不刷新」。
//
// 📌 **正因为原理由是假的,端到端断言测不出来**(见 approval_engine_test.go
// 里「publish 用的 ctx 必须是活的」那条:它直接调 publish 传一个已取消的
// ctx,而不是走一遍 Act —— 后者在当前 GORM 版本上恒绿)。
//
// ⚠️ **只在事务成功返回后才调**(每个入口方法自己控制),事务回滚不发事件。
func (e *approvalEngine) publish(ctx context.Context, evs []ApprovalEvent) {
	if e.publisher == nil || len(evs) == 0 {
		return
	}
	safeCtx := context.WithoutCancel(ctx)
	for _, ev := range evs {
		e.publisher.PublishApproval(safeCtx, ev)
	}
}

// ============================================================================
// ① CreateDraft —— 建草稿
// ============================================================================

func (e *approvalEngine) CreateDraft(ctx context.Context, tx *gorm.DB, in *CreateDraftInput) (uint64, error) {
	// ⚠️ 必须传 tx。业务记录建了而审批单没建,就是一条永远没人审的孤儿数据(D8)。
	// nil 检查是为了把这个约定变成一条能读懂的报错,而不是一个 nil 解引用 panic。
	if tx == nil {
		return 0, errcode.New(errcode.Internal,
			"CreateDraft 必须收到事务句柄 tx(D8):业务记录与审批单要同生共死,不能分两次写")
	}
	if in == nil {
		return 0, errcode.New(errcode.ParamMissing, "入参为空")
	}

	bizType := strings.TrimSpace(in.BizType)
	title := strings.TrimSpace(in.Title)
	if bizType == "" || in.BizID == 0 || title == "" {
		return 0, errcode.New(errcode.ParamMissing, "biz_type / biz_id / title 必填")
	}

	// ⚠️ **未注册的 biz_type 直接拒**。放过去等于"审批通过了但业务状态永远不变"
	// —— R11 说这是要在启动时 WARN,这里更硬:连草稿都不给建。
	h := e.handlers[bizType]
	if h == nil {
		return 0, errcode.New(errcode.NotImplemented,
			"业务类型 "+bizType+" 未注册审批处理器,不能发起审批")
	}
	if err := h.ValidateBiz(ctx, tx, bizType, in.BizID); err != nil {
		return 0, err
	}

	tpl, err := e.pickTemplate(tx, bizType, strings.TrimSpace(in.TemplateCode))
	if err != nil {
		return 0, err
	}

	inst := &model.SysApprovalInstance{
		BaseModel:           model.BaseModel{ID: sonyflake.Next()},
		BizType:             bizType,
		BizID:               in.BizID,
		Title:               title,
		Variables:           in.Variables,
		TemplateID:          tpl.ID,
		ApplicantUserID:     in.ApplicantUserID,
		ApplicantIdentityID: in.ApplicantIdentityID,
		Status:              model.ApprovalStatusDraft,
		CurrentStage:        0,
		ReturnCount:         0,
	}
	if err := tx.WithContext(ctx).Create(inst).Error; err != nil {
		// ⚠️ **唯一约束冲突必须翻译成业务错误码**(D9 / §13 M86-2h,M88-5)。
		//
		// 撞 `uk_app_inst_biz` = "这条业务记录已经在审批流里了"。原样把
		// 数据库错误抛出去,handler 会把它当 500,前端弹"服务器开小差了",
		// 而用户真正该看到的是"这条已经发过审批了"。
		//
		// ⚠️ **这里是权威的那一道** —— HTTP 通道虽然有一层预检查,但预检查
		// 有 TOCTOU 窗口(两个请求同时进来都通过预检查,后一个 INSERT 才撞
		// 索引)。业务线直接调引擎(`SubmitMaterial`)更是**根本没有预检查**。
		// 所以翻译必须落在这层,不能指望调用方先查一遍。
		//
		// ⚠️ **不吞掉别的错** —— 只有确认是重复键才翻译,其余(连接断了、
		// 字段超长……)原样上抛,不然真故障会被误报成"你重复发起了"。
		if isDuplicateErr(err) {
			return 0, ErrApprovalInstanceExists
		}
		return 0, err
	}
	return inst.ID, nil
}

// pickTemplate 按 code 取模板;没给 code 就按 biz_type 取该类型唯一启用的模板。
//
// ⚠️ **两条路都必须 `is_active`** —— 停用的模板不该还能接新单。
func (e *approvalEngine) pickTemplate(tx *gorm.DB, bizType, code string) (*model.SysApprovalTemplate, error) {
	q := tx.Model(&model.SysApprovalTemplate{}).Where("is_active = ?", true)
	if code != "" {
		q = q.Where("code = ?", code)
	} else {
		q = q.Where("biz_type = ?", bizType)
	}

	var tpls []model.SysApprovalTemplate
	if err := q.Order("id ASC").Find(&tpls).Error; err != nil {
		return nil, err
	}
	switch len(tpls) {
	case 0:
		return nil, ErrApprovalNoTemplate
	case 1:
		return &tpls[0], nil
	default:
		// ⚠️ 同一 biz_type 有多条启用模板时**不猜**。猜错 = 每条业务记录走错
		// 审批链,而且没人会发现。逼调用方显式给 code。
		return nil, errcode.New(errcode.ApprovalNoTemplate,
			fmt.Sprintf("业务类型 %s 配了 %d 条启用模板,请显式指定 template_code", bizType, len(tpls)))
	}
}

// ============================================================================
// ② UpdateDraft —— 改还没送审的东西(§3.3)
// ============================================================================

func (e *approvalEngine) UpdateDraft(ctx context.Context, caller *Caller, id uint64, req UpdateDraftReq) error {
	return e.withLockedInstance(ctx, id, func(tx *gorm.DB, inst *model.SysApprovalInstance) error {
		// 守卫:**只有两个**,没有别的(§7.4)
		if err := guardApplicant(inst, caller); err != nil {
			return err
		}
		if inst.Status != model.ApprovalStatusDraft && inst.Status != model.ApprovalStatusReturned {
			return ErrApprovalStateInvalid
		}

		upd := map[string]any{}
		if req.Title != nil {
			t := strings.TrimSpace(*req.Title)
			if t == "" {
				return errcode.New(errcode.ParamInvalid, "标题不能为空")
			}
			upd["title"] = t
		}
		if req.Variables != nil {
			// ⚠️ **整体覆盖,不 merge**(§7.4)。合并语义会让"用户删掉了一个字段"
			// 永远删不掉 —— 这是所有合并式编辑的通病。
			upd["variables"] = *req.Variables
		}
		if req.TemplateCode != nil && strings.TrimSpace(*req.TemplateCode) != "" {
			tpl, err := e.pickTemplate(tx, inst.BizType, strings.TrimSpace(*req.TemplateCode))
			if err != nil {
				return err
			}
			upd["template_id"] = tpl.ID
		}
		if len(upd) == 0 {
			return nil
		}

		// ⚠️ **不写 log**(§3.3)。它不是审批动作,是"改还没送审的东西",
		// 记进意见流里全是"提交人修改了标题"的噪音。
		// 它的可见性只体现在**下一次 submit 写出的 round 快照**上。
		return tx.Model(&model.SysApprovalInstance{}).
			Where("id = ?", inst.ID).
			Updates(upd).Error
	})
}

// ============================================================================
// ③ Submit —— 提交 / 打回后重提
// ============================================================================

func (e *approvalEngine) Submit(ctx context.Context, caller *Caller, id uint64, req SubmitReq) error {
	var evs []ApprovalEvent

	err := e.withLockedInstance(ctx, id, func(tx *gorm.DB, inst *model.SysApprovalInstance) error {
		if err := guardApplicant(inst, caller); err != nil {
			return err
		}

		// ⚠️ 首次提交和重提走同一个方法,但守卫完全不同(§3.3 的 7 行表)
		var resubmit bool
		switch inst.Status {
		case model.ApprovalStatusDraft:
			resubmit = false
		case model.ApprovalStatusReturned:
			if inst.ReturnCount >= MaxReturnCount {
				return ErrApprovalTooManyReturns
			}
			resubmit = true
		default:
			return ErrApprovalStateInvalid
		}

		prevRound, err := e.latestRound(tx, inst.ID)
		if err != nil {
			return err
		}
		// 🔴 **重提必须改内容**(§3.3)。被驳回却原样重提,会让所有审批人把上一轮
		// 做过的事重做一遍,纯浪费,而且每次都在烧 return_count 直到锁死。
		//
		// ⚠️ **比的是"本轮"不是"第 1 轮"** —— 连续两次重提、第二次相对第一次
		// 有改动就该放行,拿第 1 轮比会把合法的第二次重提误拒。
		// ⚠️ **逐字节比**,不做 trim / 不做 JSON 归一化:归一化过的比较会出现
		// "改了个键序"也算改动的假阳性,反而放行了实质没改的单子。
		if resubmit && prevRound != nil &&
			inst.Title == prevRound.Title && inst.Variables == prevRound.Variables {
			return ErrApprovalNoChange
		}

		stages, err := e.loadStages(tx, inst.TemplateID)
		if err != nil {
			return err
		}
		if len(stages) == 0 || stages[0].StageIndex != 1 {
			// ⚠️ 模板在,但一个关卡都没有 —— 配置坏了。不是"没配流程",是"配坏了"
			return ErrApprovalCorrupt
		}
		st1 := stages[0]

		// ⚠️ **重提时旧 task 已经是 `stale`,不动**(§3.3)。分母靠 `stale` 排除
		// 上一轮(§4.1 的 WHERE 条件),所以这里只生成新的。
		assignees, err := e.resolveAssignees(ctx, tx, inst, st1)
		if err != nil {
			return err
		}
		if err := e.createTasks(tx, inst.ID, st1.StageIndex, assignees); err != nil {
			return err
		}

		// 轮次号 = **上一轮 + 1**,不拿 `return_count` 推。
		// 两者理论上相等,但 round 表才是轮次的真相源;拿 count 推会让
		// 「有人手工改过 return_count」变成一条静默错位的快照。
		roundNo := 1
		if prevRound != nil {
			roundNo = prevRound.RoundNo + 1
		}
		now := time.Now()
		if err := tx.Create(&model.SysApprovalRound{
			BaseModel:   model.BaseModel{ID: sonyflake.Next()},
			InstanceID:  inst.ID,
			RoundNo:     roundNo,
			Title:       inst.Title,
			Variables:   inst.Variables,
			SubmitterID: caller.UserID,
			SubmittedAt: now,
			Outcome:     model.ApprovalRoundRunning,
		}).Error; err != nil {
			return err
		}

		if err := tx.Model(&model.SysApprovalInstance{}).
			Where("id = ?", inst.ID).
			Updates(map[string]any{
				"status":        model.ApprovalStatusRunning,
				"current_stage": st1.StageIndex,
				"submitted_at":  now,
			}).Error; err != nil {
			return err
		}

		if err := e.writeLog(tx, &model.SysApprovalLog{
			InstanceID:         inst.ID,
			StageIndex:         st1.StageIndex,
			RoundNo:            roundNo,
			Action:             ActSubmit,
			OperatorUserID:     caller.UserID,
			OperatorIdentityID: caller.IdentityID,
			Comment:            strings.TrimSpace(req.Comment),
		}); err != nil {
			return err
		}

		evs = assignedEvents(inst.ID, assignees)
		return nil
	})
	if err != nil {
		return err
	}
	e.publish(ctx, evs)
	return nil
}

// ============================================================================
// ④ Act —— 通过 / 拒绝 / 打回
// ============================================================================

func (e *approvalEngine) Act(ctx context.Context, caller *Caller, id uint64, req ActReq) error {
	var evs []ApprovalEvent

	err := e.withLockedInstance(ctx, id, func(tx *gorm.DB, inst *model.SysApprovalInstance) error {
		// —— §3.4 守卫,缺一不可 ——

		// 1. 实例存在且 running(存在性由 withLockedInstance 保证)
		if inst.Status != model.ApprovalStatusRunning {
			return ErrApprovalStateInvalid
		}
		// 2. caller 在**本关**有 status='pending' 的 task
		//
		// 🔴 **`status='pending'` 不能少**(§3.4)。只查"有没有我的 task"的话,
		// 打回后上一轮那条 `stale` 会让人**再审一遍** —— 最容易写出来也最容易漏。
		var task model.SysApprovalTask
		err := tx.Where("instance_id = ? AND stage_index = ? AND assignee_user_id = ? AND status = ?",
			inst.ID, inst.CurrentStage, caller.UserID, model.ApprovalTaskPending).
			First(&task).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrApprovalForbidden
			}
			return err
		}
		// 3. task 的 stage_index == 实例的 current_stage —— 上面那条 Where 已经
		//    带上了,这里保留一次显式断言,防止以后有人把 Where 改宽了。

		// 4. 意见校验。⚠️ **校验在 service 层,不在 binding tag 层**(§3.4)——
		//    `action` 是动态的,`required_if` 表达不了。
		if err := validateComment(req.Action, req.Comment); err != nil {
			return err
		}

		roundNo, err := e.currentRoundNo(tx, inst)
		if err != nil {
			return err
		}
		now := time.Now()

		switch req.Action {
		case ActReject:
			if err := tx.Model(&model.SysApprovalTask{}).
				Where("id = ?", task.ID).
				Updates(map[string]any{
					"status":      model.ApprovalTaskRejected,
					"actioned_at": now,
				}).Error; err != nil {
				return err
			}
			// 🔴 **拒绝必须写意见流水**(§6① 的 action 词表里有 `reject`)。
			//
			// ⚠️ **2026-10-24(M86-2)补的** —— 原来这一支不写 log,后果是
			// **驳回理由被静默丢弃**:§3.4 强制驳回必须写 ≥5 个字的意见,
			// 结果那条意见哪儿也没去 —— 详情页看不到,业务侧的
			// `review_comment` 也是空的。approve / rollback 两支都写了,只有
			// reject 漏了,八成是当初照着"终局就不再记流水"写的。
			//
			// ⚠️ **写在 finish 之前** —— `finish` 里会调 `OnRejected`,
			// 业务侧要靠这条 log 拿审核人和意见(意见流水的唯一真相源)。
			// 顺序反了业务侧就只读到上一条 log。
			if err := e.writeLog(tx, &model.SysApprovalLog{
				InstanceID:         inst.ID,
				StageIndex:         inst.CurrentStage,
				RoundNo:            roundNo,
				Action:             ActReject,
				OperatorUserID:     caller.UserID,
				OperatorIdentityID: caller.IdentityID,
				Comment:            strings.TrimSpace(req.Comment),
			}); err != nil {
				return err
			}
			// ⚠️ **reject 立即终局**(D5),不等其他人
			if err := e.finish(tx, inst, roundNo, model.ApprovalStatusRejected,
				model.ApprovalRoundRejected, caller.UserID, now,
				func(tx *gorm.DB, i *model.SysApprovalInstance) error {
					return e.notify(ctx, tx, i, false)
				}); err != nil {
				return err
			}
			// 🔴 **Result: true** —— reject 当场终局(D5),发起人必须知道结果。
			evs = []ApprovalEvent{{InstanceID: inst.ID, Action: EvApprovalRejected, Result: true}}

		case ActRollback:
			// 🔴 **本实例所有 task → `stale`**(§3.3 / D4)。这就是"轮次"的
			// 实现方式 —— 不靠日志分界,靠 task 状态。打回之后没有任何一条
			// task 还算数,重提时生成的是全新的一批。
			if err := tx.Model(&model.SysApprovalTask{}).
				Where("instance_id = ? AND status <> ?", inst.ID, model.ApprovalTaskStale).
				Updates(map[string]any{
					"status":      model.ApprovalTaskStale,
					"actioned_at": now,
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.SysApprovalInstance{}).
				Where("id = ?", inst.ID).
				Updates(map[string]any{
					"status":        model.ApprovalStatusReturned,
					"return_count":  gorm.Expr("return_count + 1"),
					"current_stage": 0,
					// ⚠️ **清掉 submitted_at**:本单现在不在审了。这个字段是
					// cronx 超时巡检的输入(§9.4),留着会让一个没在审的单子被
					// 巡检当成"审了很久没人管"报警。
					"submitted_at": nil,
				}).Error; err != nil {
				return err
			}
			if err := e.writeLog(tx, &model.SysApprovalLog{
				InstanceID:         inst.ID,
				StageIndex:         inst.CurrentStage,
				RoundNo:            roundNo,
				Action:             ActRollback,
				OperatorUserID:     caller.UserID,
				OperatorIdentityID: caller.IdentityID,
				Comment:            strings.TrimSpace(req.Comment),
			}); err != nil {
				return err
			}
			// ⚠️ 本轮 outcome 必须是 `rolled_back`,不是留着 `running` ——
			// §10.1 用例 12 点名了这个假绿:详情页上会出现三轮全是"进行中"。
			if err := e.closeRound(tx, inst.ID, roundNo, model.ApprovalRoundRolledBack,
				caller.UserID, now); err != nil {
				return err
			}
			// 🔴 **Result: true** —— 打回是"结果",发起人必须知道要去改。
			// (打回不是终局,但它同样是一个"该通知发起人的结果"。)
			evs = []ApprovalEvent{{InstanceID: inst.ID, Action: EvApprovalRolledBack, Result: true}}

		case ActApprove:
			if err := tx.Model(&model.SysApprovalTask{}).
				Where("id = ?", task.ID).
				Updates(map[string]any{
					"status":      model.ApprovalTaskApproved,
					"actioned_at": now,
				}).Error; err != nil {
				return err
			}
			if err := e.writeLog(tx, &model.SysApprovalLog{
				InstanceID:         inst.ID,
				StageIndex:         inst.CurrentStage,
				RoundNo:            roundNo,
				Action:             ActApprove,
				OperatorUserID:     caller.UserID,
				OperatorIdentityID: caller.IdentityID,
				Comment:            strings.TrimSpace(req.Comment),
			}); err != nil {
				return err
			}
			evs, err = e.advanceStage(ctx, tx, inst, roundNo, caller)
			if err != nil {
				return err
			}

		default:
			// ⚠️ **白名单校验**。理论上 `validateComment` 已经挡掉了(它也
			// switch 一遍 action),这里留着是因为将来给 act 加第四个动作时,
			// 忘了改 validateComment 而这边也忘了,会静默地什么都不做。
			return errcode.New(errcode.ParamInvalid,
				"action 只能是 approve / reject / rollback")
		}
		return nil
	})
	if err != nil {
		return err
	}
	e.publish(ctx, evs)
	return nil
}

// ============================================================================
// ⑤ Cancel —— 作废(D9)
// ============================================================================

func (e *approvalEngine) Cancel(ctx context.Context, caller *Caller, id uint64) error {
	var evs []ApprovalEvent

	err := e.withLockedInstance(ctx, id, func(tx *gorm.DB, inst *model.SysApprovalInstance) error {
		if err := guardApplicant(inst, caller); err != nil {
			return err
		}
		// ⚠️ **`running` 不给作废**(D9):已经送审的单子可能有人正在审,
		// 悄悄作废掉会让他审批到一条已经不存在的单。
		if inst.Status != model.ApprovalStatusDraft && inst.Status != model.ApprovalStatusReturned {
			return ErrApprovalStateInvalid
		}
		now := time.Now()

		if err := e.skipPending(tx, inst.ID, now); err != nil {
			return err
		}
		if err := tx.Model(&model.SysApprovalInstance{}).
			Where("id = ?", inst.ID).
			Updates(map[string]any{
				"status":       model.ApprovalStatusCancelled,
				"finished_at":  now,
				"submitted_at": nil,
			}).Error; err != nil {
			return err
		}

		// ⚠️ **用 latestRound 而不是 currentRoundNo** —— `draft` 从没送审过,
		// 一行 round 都没有,`currentRoundNo` 会抛 ErrApprovalCorrupt,把"正常
		// 作废一张草稿"变成 500。round_no=0 表示"还没进入任何一轮",
		// 真实轮次从 1 开始,不会撞号。
		prev, err := e.latestRound(tx, inst.ID)
		if err != nil {
			return err
		}
		roundNo := 0
		if prev != nil {
			roundNo = prev.RoundNo
		}
		if err := e.writeLog(tx, &model.SysApprovalLog{
			InstanceID:         inst.ID,
			StageIndex:         inst.CurrentStage,
			RoundNo:            roundNo,
			Action:             ActCancel,
			OperatorUserID:     caller.UserID,
			OperatorIdentityID: caller.IdentityID,
			Comment:            "",
		}); err != nil {
			return err
		}
		// ⚠️ **不改 round 的 outcome**。从 `returned` 作废时,上一轮的 outcome
		// 已经是 `rolled_back` 了,再覆写成 `cancelled` 会把"为什么被打回"
		// 这条信息抹掉。轮次记录的是**审批**结果,作废不是审批结果。

		// ⚠️ **作废也要回调业务侧**(M86-2 加的第 4 个 SPI 方法)——
		// 作废是终局,业务记录不会自己变回去。不回调的话资料会永远停在"待审核"。
		if err := e.notifyCancelled(ctx, tx, inst); err != nil {
			return err
		}

		evs = []ApprovalEvent{{InstanceID: inst.ID, Action: EvApprovalCancelled}}
		return nil
	})
	if err != nil {
		return err
	}
	e.publish(ctx, evs)
	return nil
}

// ============================================================================
// 关卡推进(§4)
// ============================================================================

// advanceStage 本关处理完之后判断走向。全程持有实例行锁(§3.5)。
//
// 返回值是**这一票落地后要发的事件**:进入下一关 = 逐个新审批人的 `assigned`;
// 终局 = 一条终局事件;会签还在等人 = 一条 `approved`(这一票落地了,整单还没过,
// M89-1 补的 —— 原来这里是空,角标和"会签人要互相知道进度"两条都断了)。
func (e *approvalEngine) advanceStage(
	ctx context.Context,
	tx *gorm.DB,
	inst *model.SysApprovalInstance,
	roundNo int,
	caller *Caller,
) ([]ApprovalEvent, error) {
	stages, err := e.loadStages(tx, inst.TemplateID)
	if err != nil {
		return nil, err
	}

	var cur *model.SysApprovalStage
	for i := range stages {
		if stages[i].StageIndex == inst.CurrentStage {
			cur = &stages[i]
			break
		}
	}
	if cur == nil {
		return nil, ErrApprovalCorrupt
	}

	// ⚠️ **本函数只从 approve 分支进来** —— reject 在 `Act` 里当场终局,
	// 根本走不到这里(D5)。所以下面两个终局分支里只有 approved 是"正常路径"。
	finishApproved := func() ([]ApprovalEvent, error) {
		now := time.Now()
		if err := e.finish(tx, inst, roundNo, model.ApprovalStatusApproved,
			model.ApprovalRoundApproved, caller.UserID, now,
			func(tx *gorm.DB, i *model.SysApprovalInstance) error {
				return e.notify(ctx, tx, i, true)
			}); err != nil {
			return nil, err
		}
		// 🔴 **Result: true** —— 整单通过(终局)。
		return []ApprovalEvent{{InstanceID: inst.ID, Action: EvApprovalApproved, Result: true}}, nil
	}

	switch cur.ReviewType {
	case model.ApprovalReviewAny:
		// ⚠️ **或签 = 首个动作终局**(D5)。不必再看别人的票。
		return finishApproved()

	case model.ApprovalReviewAll:
		counts, err := e.stageCounts(tx, inst.ID, cur.StageIndex)
		if err != nil {
			return nil, err
		}
		// 🔴 **空集防御放最前面**(§4.1)。0==0 判"全过"是**静默放行** ——
		// 模板配坏了(规则解析不出人 / task 被误删)却让单子一路绿灯到底。
		if counts[model.ApprovalTaskPending]+counts[model.ApprovalTaskApproved]+
			counts[model.ApprovalTaskRejected] == 0 {
			return nil, ErrApprovalCorrupt
		}
		if counts[model.ApprovalTaskRejected] > 0 {
			// ⚠️ **正常路径走不到这里** —— reject 当场终局了(D5)。留着是一道
			// 防御:将来 §12 第 5 条改成"或签拒绝不定局"时,这一支就是它的落点。
			now := time.Now()
			if err := e.finish(tx, inst, roundNo, model.ApprovalStatusRejected,
				model.ApprovalRoundRejected, caller.UserID, now,
				func(tx *gorm.DB, i *model.SysApprovalInstance) error {
					return e.notify(ctx, tx, i, false)
				}); err != nil {
				return nil, err
			}
			// 🔴 **Result: true** —— 防御分支的终局驳回,同样要通知发起人。
			return []ApprovalEvent{{InstanceID: inst.ID, Action: EvApprovalRejected, Result: true}}, nil
		}
		if counts[model.ApprovalTaskPending] > 0 {
			// ⚠️ **还有人没处理 —— 不动实例**。D10:分母在锁内重算,一条计数器都不落库。
			//
			// 🔴 **但必须发一条 `approved` 事件**(M89-1 补)—— 原来这里是 `return nil`,
			// 于是**会签的这一票谁都不知道落地了**:
			//
			//	① **投票人自己的待办角标不刷新** —— 他那一票已经落成 `approved`,
			//	   `box=todo` 里不再有它,但前端**只有收到事件才会重拉**角标
			//	   (M88 的设计:事件是唯一的刷新信号)。角标就停在错的数上。
			//	② **同一关的其他人不知道有人批了** —— §8.3 写的是"会签人要互相
			//	   知道进度,否则不知道还有几个人没批",详情页那半边做到了,
			//	   但"进度变了"这个信号没推出去。
			//	③ **§9.2 那张表写着 `act(approve)` → `approved`**,这里是空的 = 违约。
			//
			// ⚠️ **这条事件的语义是"这一票记下了",不是"整单通过了"** ——
			// 整单通过由 `finishApproved` 那条终局事件表达(同一条 action,
			// 不同语境)。前端两个事件一视同仁(都只触发"重拉待办数"),
			// 所以区分不出来也不影响。
			// ⚠️ **Result 留 false(不写)** —— 这一票**不是整单的结果**。
			// 实例还在 running,发起人此刻看到"审批已通过"是**假消息**。
			// 真正定局的那一票由 `finishApproved` / `enterStage` 发(Result: true)。
			//
			// ⚠️ **前端区分不出来,也不需要区分** —— 它只当"重拉角标"的信号。
			return []ApprovalEvent{{InstanceID: inst.ID, Action: EvApprovalApproved}}, nil
		}
		// pending==0 → 本关全过,进下一关

	default:
		// ⚠️ 模板里出现第三种 review_type。**必须报错而不是当 any 处理**:
		// 当 any = 一个人批完全关就放行,是静默的越权。
		return nil, ErrApprovalCorrupt
	}

	// 本关全过 → 进下一关;没有下一关就是终局 approved
	return e.enterStage(ctx, tx, inst, roundNo, cur.StageIndex+1, caller)
}

// enterStage 进入第 n 关。
//
// ⚠️ **n 超过最后一个关卡序号 = 全部通过 = 终局**(§4.1 的 `enterStage(n+1)`
// 会自然走到这里,所以"最后一关"和"没有下一关"是同一条路径)。
func (e *approvalEngine) enterStage(
	ctx context.Context,
	tx *gorm.DB,
	inst *model.SysApprovalInstance,
	roundNo, stageIndex int,
	caller *Caller,
) ([]ApprovalEvent, error) {
	stages, err := e.loadStages(tx, inst.TemplateID)
	if err != nil {
		return nil, err
	}
	var next *model.SysApprovalStage
	for i := range stages {
		if stages[i].StageIndex == stageIndex {
			next = &stages[i]
			break
		}
	}
	if next == nil {
		now := time.Now()
		if err := e.finish(tx, inst, roundNo, model.ApprovalStatusApproved,
			model.ApprovalRoundApproved, caller.UserID, now,
			func(tx *gorm.DB, i *model.SysApprovalInstance) error {
				return e.notify(ctx, tx, i, true)
			}); err != nil {
			return nil, err
		}
		// 🔴 **Result: true** —— 没有下一关 = 全部通过 = 终局。
		return []ApprovalEvent{{InstanceID: inst.ID, Action: EvApprovalApproved, Result: true}}, nil
	}

	assignees, err := e.resolveAssignees(ctx, tx, inst, *next)
	if err != nil {
		return nil, err
	}
	if err := e.createTasks(tx, inst.ID, next.StageIndex, assignees); err != nil {
		return nil, err
	}
	if err := tx.Model(&model.SysApprovalInstance{}).
		Where("id = ?", inst.ID).
		Update("current_stage", next.StageIndex).Error; err != nil {
		return nil, err
	}
	return assignedEvents(inst.ID, assignees), nil
}

// notify 调业务侧 SPI(D8)。**必须在审批事务内、用传进来的 tx。**
//
// ⚠️ 传 `approved bool` 而不是两个方法,是为了让"这里是终局点"这件事
// 在调用处一眼可见 —— 终局只有一处会产生这个闭包。
func (e *approvalEngine) notify(
	ctx context.Context,
	tx *gorm.DB,
	inst *model.SysApprovalInstance,
	approved bool,
) error {
	h := e.handlers[inst.BizType]
	if h == nil {
		return errcode.New(errcode.NotImplemented,
			"业务类型 "+inst.BizType+" 未注册审批处理器")
	}
	if approved {
		return h.OnApproved(ctx, tx, inst)
	}
	return h.OnRejected(ctx, tx, inst)
}

// notifyCancelled 作废时回调业务侧(M86-2)。
//
// ⚠️ **与 `notify` 分开写而不是加个 bool 参数** —— `notify(approved bool)`
// 那种写法迟早被人写成 `notify(ctx, tx, inst, false)` 然后分不清哪个是哪个。
// 三个终局动作三个函数名,编译器帮你查。
func (e *approvalEngine) notifyCancelled(
	ctx context.Context,
	tx *gorm.DB,
	inst *model.SysApprovalInstance,
) error {
	h := e.handlers[inst.BizType]
	if h == nil {
		return errcode.New(errcode.NotImplemented,
			"业务类型 "+inst.BizType+" 未注册审批处理器")
	}
	return h.OnCancelled(ctx, tx, inst)
}

// finish 终局:实例落终态 + 剩余 pending 置 skipped + 本轮 outcome 收口
// + 调 BizHandler(D8,同一个 tx)。
//
// ⚠️ **`OnApproved` 在这里调,不是在调用方调** —— 终局只有这一条路径,
// 放在别处迟早漏掉一处,而漏掉的后果是"审批通过了但业务状态没变"(R11)。
func (e *approvalEngine) finish(
	tx *gorm.DB,
	inst *model.SysApprovalInstance,
	roundNo int,
	status, outcome string,
	operatorID uint64,
	now time.Time,
	onBiz func(tx *gorm.DB, inst *model.SysApprovalInstance) error,
) error {
	if err := tx.Model(&model.SysApprovalInstance{}).
		Where("id = ?", inst.ID).
		Updates(map[string]any{
			"status":        status,
			"current_stage": 0,
			"finished_at":   now,
			"submitted_at":  nil,
		}).Error; err != nil {
		return err
	}
	inst.Status = status
	inst.CurrentStage = 0

	// ⚠️ **其余未处理的 pending 全部 skipped**(§3.2)。不清的话,"我已办理"
	// 列表里会一直挂着一条永远等不到我处理的待办。
	if err := e.skipPending(tx, inst.ID, now); err != nil {
		return err
	}
	if err := e.closeRound(tx, inst.ID, roundNo, outcome, operatorID, now); err != nil {
		return err
	}
	if onBiz != nil {
		return onBiz(tx, inst)
	}
	return nil
}

// skipPending 终局时把剩余待办置 skipped
func (e *approvalEngine) skipPending(tx *gorm.DB, instanceID uint64, now time.Time) error {
	return tx.Model(&model.SysApprovalTask{}).
		Where("instance_id = ? AND status = ?", instanceID, model.ApprovalTaskPending).
		Updates(map[string]any{
			"status":      model.ApprovalTaskSkipped,
			"actioned_at": now,
		}).Error
}

// ============================================================================
// 审批人(§4.2 / §5)
// ============================================================================

// resolveAssignees 解析一关的审批人:调 SPI → 查重 → 剔除发起人(D11)。
//
// ⚠️ **error ≠ 空集合**(§5.2 第 1 条)。查不到(DB 报错)是故障,整个 submit
// 失败;查得到但结果是空是数据常态。混成空集合的后果是走降级,而本模块
// **任何降级路径都不存在**(§5.3)—— 报错至少让申请人知道要找谁。
func (e *approvalEngine) resolveAssignees(
	ctx context.Context,
	tx *gorm.DB,
	inst *model.SysApprovalInstance,
	st model.SysApprovalStage,
) ([]uint64, error) {
	if e.resolver == nil {
		return nil, errcode.New(errcode.Internal,
			"未装配审批人解析器(AssigneeResolver),无法解析第 "+strconv.Itoa(st.StageIndex)+" 关审批人")
	}

	ids, err := e.resolver.ResolveStage(ctx, tx, ResolveStageRequest{
		TemplateID:          inst.TemplateID,
		StageID:             st.ID,
		StageIndex:          st.StageIndex,
		ApplicantUserID:     inst.ApplicantUserID,
		ApplicantIdentityID: inst.ApplicantIdentityID,
	})
	if err != nil {
		// ⚠️ 保留原始 error 便于排障,但业务码是 500 —— **不能降级**
		return nil, fmt.Errorf("解析第 %d 关审批人失败: %w", st.StageIndex, err)
	}

	// ⚠️ **按 `user_id` 去重,不按 identity**(D12)。审批是"这个人干的",
	// 不是"这个身份干的";按 identity 去重会让同一人两个身份拿到两条 task,
	// 会签永远凑不齐。
	uniq := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, uid := range ids {
		if uid == 0 {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		uniq = append(uniq, uid)
	}

	if len(uniq) == 0 {
		return nil, errcode.New(errcode.ApprovalNoAssignee,
			"第 "+strconv.Itoa(st.StageIndex)+" 关解析不到审批人,请联系管理员检查审批流程配置")
	}

	// ⚠️ **D11:解析结果含发起人直接报错,不是过滤掉**。小部门里负责人就是自己,
	// 这不是配置错误而是数据常态 —— 过滤掉的话"这一关没人审"会被当成
	// "这关不需要审",单子静默滑到下一关。
	for _, uid := range uniq {
		if uid == inst.ApplicantUserID {
			return nil, ErrApprovalSameAuditor
		}
	}
	return uniq, nil
}

// createTasks 给一关的每个审批人生成一条 pending task
func (e *approvalEngine) createTasks(tx *gorm.DB, instanceID uint64, stageIndex int, assignees []uint64) error {
	if len(assignees) == 0 {
		return nil
	}
	rows := make([]model.SysApprovalTask, 0, len(assignees))
	for _, uid := range assignees {
		rows = append(rows, model.SysApprovalTask{
			BaseModel:      model.BaseModel{ID: sonyflake.Next()},
			InstanceID:     instanceID,
			StageIndex:     stageIndex,
			AssigneeUserID: uid,
			Status:         model.ApprovalTaskPending,
		})
	}
	return tx.Create(&rows).Error
}

// ============================================================================
// 落库小工具
// ============================================================================

func (e *approvalEngine) loadStages(tx *gorm.DB, templateID uint64) ([]model.SysApprovalStage, error) {
	var stages []model.SysApprovalStage
	err := tx.Where("template_id = ?", templateID).
		Order("stage_index ASC").
		Find(&stages).Error
	return stages, err
}

func (e *approvalEngine) latestRound(tx *gorm.DB, instanceID uint64) (*model.SysApprovalRound, error) {
	var r model.SysApprovalRound
	err := tx.Where("instance_id = ?", instanceID).
		Order("round_no DESC").
		First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// currentRoundNo 当前轮号。running 状态下必然有一行 —— 没有就是数据坏了。
func (e *approvalEngine) currentRoundNo(tx *gorm.DB, inst *model.SysApprovalInstance) (int, error) {
	r, err := e.latestRound(tx, inst.ID)
	if err != nil {
		return 0, err
	}
	if r == nil {
		return 0, ErrApprovalCorrupt
	}
	return r.RoundNo, nil
}

func (e *approvalEngine) closeRound(tx *gorm.DB, instanceID uint64, roundNo int, outcome string, operatorID uint64, now time.Time) error {
	return tx.Model(&model.SysApprovalRound{}).
		Where("instance_id = ? AND round_no = ?", instanceID, roundNo).
		Updates(map[string]any{
			"outcome":    outcome,
			"outcome_by": operatorID,
			"outcome_at": now,
		}).Error
}

// 🔴 **这张表只 append**(§6① 的禁令)。这里没有 Update、没有 Delete。
func (e *approvalEngine) writeLog(tx *gorm.DB, l *model.SysApprovalLog) error {
	l.BaseModel = model.BaseModel{ID: sonyflake.Next()}
	return tx.Create(l).Error
}

// stageCounts 会签分母 —— **在实例行锁内重算**(D10 / §4.1)。
//
// 🔴 **WHERE 的三个成员各司其职,少一个都错**:
//
//	pending  —— "还有人没处理"是唯一推进判据,漏了第一票就推进
//	approved —— 空集防御要用,漏了 0==0 被当成"全过"静默放行
//	rejected —— 一票否决判定要用,漏了有人拒绝仍继续等,单子卡死
//	stale    —— **必须排除**(D4)。上一轮的行混进来,分母翻倍永远凑不齐
//	skipped  —— **必须排除**(§3.2)。终局标记不是票
//
// ⚠️ **本实现用「pending 归零」当推进判据,不用「总数 - 已办」** —— 所以
// `stale` / `skipped` 这两个排除项在当前路径上**观察不到差别**(它们进的是
// map 里没人读的桶)。**它们是防御,不是当前生效的逻辑**:D4 真正在起作用
// 的地方是**打回时把旧 task 置 stale**(那一步做对了,旧行就永远进不了
// `pending` 桶)。留着这两个排除项是因为将来若把判据改成"分母 = 本关总数",
// 少了它们立刻单子卡死 —— 但那天的探针必须重新打,不能假设今天测过。
//
// (可逆探针实证:把 `stale` 加进 IN 列表后 20 条用例**全绿** —— 这就是
// "这行是防御不是逻辑"的实证。P01 探针改打真正生效的那一步:打回时置 stale。)
func (e *approvalEngine) stageCounts(tx *gorm.DB, instanceID uint64, stageIndex int) (map[string]int64, error) {
	type row struct {
		Status string
		Cnt    int64
	}
	var rows []row
	err := tx.Model(&model.SysApprovalTask{}).
		Select("status, COUNT(*) AS cnt").
		Where("instance_id = ? AND stage_index = ? AND status IN ?",
			instanceID, stageIndex,
			[]string{model.ApprovalTaskPending, model.ApprovalTaskApproved, model.ApprovalTaskRejected}).
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Status] = r.Cnt
	}
	return out, nil
}

// ============================================================================
// 守卫
// ============================================================================

// guardApplicant 是不是发起人自己
func guardApplicant(inst *model.SysApprovalInstance, caller *Caller) error {
	if caller == nil {
		return errcode.New(errcode.Unauthorized, "缺少调用方身份")
	}
	if caller.UserID != inst.ApplicantUserID {
		return errcode.New(errcode.IdorAttempt, "只有发起人本人可以做这个操作")
	}
	return nil
}

// validateComment 意见校验(§3.4 / §7.4)
//
// ⚠️ **按 rune 计不是字节** —— "不同意。"是 4 字符 12 字节,按字节算会把这个
// 合理的意见判成够长。
// ⚠️ **先 TrimSpace** —— 比文档 §7.4 的示例多一步。五个空格满足"5 个字符"
// 却没有任何内容,放过去等于把守卫变成摆设。这只会**多拒**不会少拒。
func validateComment(action, comment string) error {
	switch action {
	case ActApprove:
		return nil // 选填,不校验长度
	case ActReject, ActRollback:
		if utf8.RuneCountInString(strings.TrimSpace(comment)) < MinCommentRune {
			return ErrApprovalCommentShort
		}
		return nil
	default:
		// ⚠️ 白名单校验,不能直接拿 action 去 switch 更新(§7.4)
		return errcode.New(errcode.ParamInvalid, "action 只能是 approve / reject / rollback")
	}
}

func assignedEvents(instanceID uint64, assignees []uint64) []ApprovalEvent {
	evs := make([]ApprovalEvent, 0, len(assignees))
	for _, uid := range assignees {
		evs = append(evs, ApprovalEvent{
			InstanceID: instanceID,
			Action:     EvApprovalAssigned,
			AssigneeID: uid,
		})
	}
	return evs
}
