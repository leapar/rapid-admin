// JobService 定时任务管理 —— M68
//
// 分工:cronx 管"怎么跑",本 service 只管"怎么问 / 怎么改"。
// 唯一的写通道是 UpdateOverride,而且**改完必须立刻 Reload** ——
// 只写 DB 的话 robfig 里注册的还是旧表达式,改频率要等重启才生效,
// 而且没有报错、没有日志(见 cronx.Scheduler.Reload)。
//
// 与字典 / 系统配置同一条规矩:**没有新建 / 删除任务的口子**。
// 任务本体只在代码里 cronx.Register,后台只能改它的运行态覆盖
// (启停 + cron 表达式 + 中文名 + 排序)。理由是 DB 里凭空长出来的任务
// 没有代码,重启即消失,而且没人知道它曾经存在过。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/sonyflake"
)

// JobListQuery 任务列表查询
type JobListQuery struct {
	// Keyword 模糊匹配 job_key / 中文名
	Keyword string `json:"keyword"`
	// EnabledOnly 只看生效中的任务(前端默认不勾:停用的也要看得见,否则
	// "任务怎么不跑了"这个问题在列表里根本无从查起)
	EnabledOnly bool `json:"enabled_only"`
}

// JobRunListQuery 执行记录查询
type JobRunListQuery struct {
	model.PageRequest
	// JobKey 按任务过滤,空 = 全部
	JobKey string `json:"job_key"`
	// Status running / success / failed
	Status string `json:"status"`
	// TriggerType schedule / manual
	TriggerType string `json:"trigger_type"`
	// DegradedOnly 只看降级执行过的(排查"Redis 掉了有没有影响任务"用)
	DegradedOnly bool   `json:"degraded_only"`
	StartAt      *int64 `json:"start_at"`
	EndAt        *int64 `json:"end_at"`
}

// JobService 定时任务管理
type JobService interface {
	// ===== 任务列表 / 详情 =====
	ListJobs(ctx context.Context, q JobListQuery) ([]JobItemView, error)
	GetJob(ctx context.Context, jobKey string) (*JobItemView, error)

	// ===== 运行态健康 =====
	Runtime(ctx context.Context) (cronx.RuntimeInfo, error)

	// ===== 执行记录 =====
	ListJobRuns(ctx context.Context, q JobRunListQuery) (*model.PageResponse, error)
	CronPreview(ctx context.Context, expr string, n int) ([]string, error)

	// ===== 写通道(唯一一条)=====
	// UpdateOverride 改启停 / cron 覆盖值 / 中文名 / 排序,并立刻热更新 cron。
	// resetToDefault=true 时清掉所有「代码可覆盖」字段,回到代码默认值。
	UpdateOverride(ctx context.Context, caller *Caller, jobKey string,
		pu JobOverridePatch, resetToDefault bool) (*JobItemView, error)

	// TriggerNow 手动触发一次。**同步**执行并返回本次执行记录。
	//
	// 不用 202 异步是有代价的(管理页请求会挂到任务跑完),但换成异步的代价更大:
	// 定时任务动辄几十秒到几分钟,点了没反应会立刻变成"是不是没生效"的重启冲动。
	//
	// ⚠️ **任务自身失败时返回 nil error + status="failed" 的记录,不是 error。**
	// 只有"没能跑起来"(任务不存在 / 已停用 / 正在执行 / 去重降级)才返 error。
	// 理由:执行记录里的 duration_ms / trace_id / instance_id 是排障的关键信息,
	// 换成 500 就全丢了;而且"任务跑了但失败"和"你无权触发"混成一个 500,
	// 监控上分不开。前端**必须**判 status !== 'success' 当失败处理(M69 已接)。
	TriggerNow(ctx context.Context, caller *Caller, jobKey string) (*JobRunView, error)
}

// JobOverridePatch 允许改的字段。**全部是指针**,区分"没传"和"传了零值" ——
// 用值类型的话 enabled=false 和"不改 enabled"分不开,前端表单就没法只改频率。
type JobOverridePatch struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	CronExpr    *string `json:"cron_expr"`
	Enabled     *bool   `json:"enabled"`
	Sort        *int    `json:"sort"`
}

// ─────────── View DTO ───────────
//
// 视图不是多余的间接层,是因为 Effective 里嵌着 cronx.Job(含 Handler 函数指针),
// 直接序列化会炸;而且前端需要「代码默认值」和「当前生效值」并排显示,
// 排查时才能知道现在跑的到底是哪一个。

// JobItemView 列表项
type JobItemView struct {
	JobKey      string `json:"job_key"`
	Name        string `json:"name"`
	Description string `json:"description"` // 备注

	// CronExpr **当前生效**的表达式
	CronExpr string `json:"cron_expr"`
	// DefaultCronExpr 代码里的默认值。运营改坏了要能一眼看出"原来是什么",
	// 否则只能去翻代码发版记录。
	DefaultCronExpr string `json:"default_cron_expr"`
	// CronOverridden 生效值来自 DB 覆盖而非代码默认
	CronOverridden bool `json:"cron_overridden"`

	// EnabledEffective 最终生效的启停(code && DB)
	EnabledEffective bool `json:"enabled_effective"`
	// EnabledCode 代码侧开关
	EnabledCode bool `json:"enabled_code"`
	// EnabledDB DB 侧开关(没有 DB 行 / 行已失效时为 true)
	EnabledDB bool `json:"enabled_db"`
	// Active DB 覆盖行是否仍有效。false = 代码里已把这个任务删掉,
	// sys_job 里只剩一行没人认领的残留(cronx.reconcile 置的)。
	Active bool `json:"active"`

	// TimeoutMs 单次执行超时
	TimeoutMs int64 `json:"timeout_ms"`
	// NonIdempotent 声明为非幂等的任务在去重降级时会被跳过
	NonIdempotent bool `json:"non_idempotent"`

	Sort int `json:"sort"`

	// NextRunAt 下次触发时间(RFC3339)。停用 / 未注册时为空字符串,
	// 前端显示"未调度" —— 空字符串而不是零值时间,是因为 1970 那一眼很吓人。
	NextRunAt string `json:"next_run_at"`
	// Overridden 该任务当前有没有 DB 覆盖行(决定"恢复默认"按钮是否可点)
	Overridden bool `json:"overridden"`
}

// JobRunView 执行记录
type JobRunView struct {
	// ⚠️ ,string 不能省:ID 是 sonyflake 生成的 uint64,超出 JS 的 2^53 安全整数,
	// 序列化成裸数字会被前端静默截断(末几位变 0),而且截断后的 ID 回传后
	// 会查到错误的那条记录。仓库有 jsonid 扫描测试守着这条。
	ID          uint64 `json:"id,string"`
	JobKey      string `json:"job_key"`
	TriggerType string `json:"trigger_type"`
	Status      string `json:"status"`
	StartedAt   int64  `json:"started_at"`  // Unix 秒
	FinishedAt  int64  `json:"finished_at"` // Unix 秒,未完成为 0
	DurationMs  int64  `json:"duration_ms"`
	Error       string `json:"error"`
	InstanceID  string `json:"instance_id"`
	TraceID     string `json:"trace_id"`
	Operator    string `json:"operator"` // 仅手动触发有值
	Degraded    bool   `json:"degraded"`
}

// ─────────── 实现 ───────────

type jobService struct{ db *gorm.DB }

func NewJobService(db *gorm.DB) JobService { return &jobService{db: db} }

// sched 取全局调度器。返回 nil 表示 cronx 没装配(理论上不会:
// main.go 无条件 SetDefault),调用方一律当"未启用"处理而不是 panic ——
// 单元测试正是靠这个 nil 路径跑通整个 service。
func (s *jobService) sched() *cronx.Scheduler { return cronx.Default() }

// normJobPage 分页归一。独立于 configCenterService.normPage:那个是方法,
// 这里是本 service 的私有方法,规则一致(page 默认 1、size 默认 20、上限 100)。
func (s *jobService) normPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func (s *jobService) ListJobs(_ context.Context, q JobListQuery) ([]JobItemView, error) {
	sc := s.sched()
	if sc == nil {
		return []JobItemView{}, nil
	}
	out := make([]JobItemView, 0)
	for _, e := range sc.List() {
		v := s.jobItemView(sc, e)
		if q.EnabledOnly && !v.EnabledEffective {
			continue
		}
		if q.Keyword != "" && !matchJobKeyword(v, q.Keyword) {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *jobService) GetJob(_ context.Context, jobKey string) (*JobItemView, error) {
	sc := s.sched()
	if sc == nil {
		return nil, errcode.New(errcode.NotFound, "定时任务未启用")
	}
	e, err := sc.Get(jobKey)
	if err != nil {
		return nil, errcode.New(errcode.NotFound, "任务不存在:"+jobKey)
	}
	v := s.jobItemView(sc, e)
	return &v, nil
}

func matchJobKeyword(v JobItemView, kw string) bool {
	kw = strings.ToLower(strings.TrimSpace(kw))
	if kw == "" {
		return true
	}
	return strings.Contains(strings.ToLower(v.JobKey), kw) ||
		strings.Contains(strings.ToLower(v.Name), kw)
}

// jobItemView 把 cronx 的 Effective 转成前端可序列化的视图。
func (s *jobService) jobItemView(sc *cronx.Scheduler, e cronx.Effective) JobItemView {
	ov, overridden := sc.Override(e.Key)
	v := JobItemView{
		JobKey:           e.Key,
		Name:             e.Name,
		Description:      e.Description,
		CronExpr:         e.CronExpr,
		DefaultCronExpr:  e.Cron,
		CronOverridden:   e.CronOverridden,
		EnabledEffective: e.EnabledEffective(),
		EnabledCode:      e.Enabled,
		EnabledDB:        e.DBEnabled,
		Active:           true,
		NonIdempotent:    e.NonIdempotent,
		Overridden:       overridden,
	}
	if e.Timeout > 0 {
		v.TimeoutMs = e.Timeout.Milliseconds()
	}
	if overridden {
		v.Sort = ov.Sort
		// reconcile 把「代码里已删掉」的残留行置成 is_active=false。
		// Effective 里 EnabledDB 已经把它体现成 false,这里再单独给出 Active,
		// 是为了让前端能区分"是我停用的"和"它自己没人认领了" —— 两种情况
		// 处置完全不同:前者去启停,后者是发版时漏了清理。
		v.Active = ov.IsActive
	}
	if next := sc.NextRunAt(e.Key); !next.IsZero() {
		v.NextRunAt = next.Format(time.RFC3339)
	}
	return v
}

func (s *jobService) Runtime(_ context.Context) (cronx.RuntimeInfo, error) {
	sc := s.sched()
	if sc == nil {
		return cronx.RuntimeInfo{}, nil
	}
	return sc.Runtime(), nil
}

func (s *jobService) ListJobRuns(ctx context.Context, q JobRunListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	if s.db == nil {
		return &model.PageResponse{List: []JobRunView{}, Page: page, Size: size}, nil
	}
	db := s.db.WithContext(ctx).Model(&model.SysJobRun{})
	if q.JobKey != "" {
		db = db.Where("job_key = ?", q.JobKey)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.TriggerType != "" {
		db = db.Where("trigger_type = ?", q.TriggerType)
	}
	if q.DegradedOnly {
		db = db.Where("degraded = ?", true)
	}
	if q.StartAt != nil {
		db = db.Where("started_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		db = db.Where("started_at <= ?", time.Unix(*q.EndAt, 0))
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询执行记录失败").WithDetail(err.Error())
	}
	var rows []model.SysJobRun
	if err := db.Order("started_at DESC, id DESC").
		Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询执行记录失败").WithDetail(err.Error())
	}
	list := make([]JobRunView, 0, len(rows))
	for _, r := range rows {
		list = append(list, jobRunView(r))
	}
	return &model.PageResponse{List: list, Total: total, Page: page, Size: size}, nil
}

func jobRunView(r model.SysJobRun) JobRunView {
	v := JobRunView{
		ID:          r.ID,
		JobKey:      r.JobKey,
		TriggerType: string(r.TriggerType),
		Status:      string(r.Status),
		DurationMs:  r.DurationMs,
		Error:       r.Error,
		InstanceID:  r.InstanceID,
		TraceID:     r.TraceID,
		Operator:    r.Operator,
		Degraded:    r.Degraded,
	}
	if !r.StartedAt.IsZero() {
		v.StartedAt = r.StartedAt.Unix()
	}
	if r.FinishedAt != nil {
		v.FinishedAt = r.FinishedAt.Unix()
	}
	return v
}

// CronPreview 返回未来 n 次触发时间(人类可读,按调度器时区)。
//
// 成本是纯计算,收益是运维改完表达式立刻看到"下一次是明天 03:00",
// 而不是提交后等到 03:00 才发现表达式写错、任务压根没注册。
func (s *jobService) CronPreview(_ context.Context, expr string, n int) ([]string, error) {
	if n < 1 {
		n = 5
	}
	if n > 20 {
		n = 20
	}
	var loc *time.Location
	if sc := s.sched(); sc != nil {
		// 复用调度器自己的时区,而不是让前端传时区过来 ——
		// 前端传的话,页面时区和服务端调度时区一旦不一致,
		// 预览出来的 03:00 和实际触发的 03:00 就会差一整个时区,
		// 那样这个功能反而比没有更害人。
		if l, err := time.LoadLocation(sc.Runtime().TimeZone); err == nil {
			loc = l
		}
	}
	runs, err := cronx.NextRuns(expr, n, loc)
	if err != nil {
		return nil, errcode.New(errcode.BadRequest, "cron 表达式非法:"+expr).WithDetail(err.Error())
	}
	out := make([]string, 0, len(runs))
	for _, r := range runs {
		out = append(out, r.Format("2006-01-02 15:04:05"))
	}
	return out, nil
}

func (s *jobService) UpdateOverride(ctx context.Context, _ *Caller, jobKey string,
	pu JobOverridePatch, resetToDefault bool) (*JobItemView, error) {
	sc := s.sched()
	if sc == nil {
		return nil, errcode.New(errcode.NotFound, "定时任务未启用")
	}
	if _, err := sc.Get(jobKey); err != nil {
		return nil, errcode.New(errcode.NotFound, "任务不存在:"+jobKey)
	}

	// ⚠️ 基底是 **DB 原始行**,不是 effective 值。用 effective 当基底的话,
	// 任何一次无关的编辑(比如只改排序)都会把代码里的 cron / name 固化进 DB,
	// 于是这个任务从此变成 cron_overridden=true,而后续发版改代码默认值对它无效 ——
	// 一个"改了个名字结果改了调度频率来源"的静默行为变更。
	prev, hadRow := sc.Override(jobKey)
	ov := cronx.Override{JobKey: jobKey, Enabled: true, IsActive: true}
	if hadRow {
		ov = prev
		ov.JobKey = jobKey
	}
	// 没有 DB 行时基底刻意留空:merge 里 ov.Name == "" / ov.CronExpr == ""
	// 表示"用代码默认值",这样新建覆盖行不会把代码默认值钉死成 DB 值。
	if pu.Name != nil {
		ov.Name = *pu.Name
	}
	if pu.Description != nil {
		ov.Description = *pu.Description
	}
	if pu.CronExpr != nil {
		ov.CronExpr = *pu.CronExpr
	}
	if pu.Enabled != nil {
		ov.Enabled = *pu.Enabled
	}
	if pu.Sort != nil {
		ov.Sort = *pu.Sort
	}
	// 恢复默认 = 清掉「代码可覆盖」的三个字段(cron / 名称 / 备注)。
	// Enabled 和 Sort 是运维自己定的,不动 —— 恢复默认不该顺手把任务停了。
	if resetToDefault {
		ov.CronExpr = ""
		ov.Name = ""
		ov.Description = ""
	}

	// 先校验再落库。cronx.ParseCron 与调度器注册用的是同一个 parser,
	// 所以这里过了就一定注册得上(校验和调度必须是同一个 parser,
	// 两套的话就会出现"校验通过但注册失败")。
	if ov.CronExpr != "" {
		if _, err := cronx.ParseCron(ov.CronExpr); err != nil {
			return nil, errcode.New(errcode.BadRequest, "cron 表达式非法:"+ov.CronExpr).WithDetail(err.Error())
		}
	}
	// is_active=false 的残留行不允许经由后台复活 —— 它的存在意味着代码里
	// 已经没人注册这个任务了,这里放行等于凭空造出一个没有代码的任务。
	if hadRow && !prev.IsActive {
		return nil, errcode.New(errcode.BadRequest, "该任务已从代码中移除,残留配置不可修改")
	}

	if s.db != nil {
		if err := s.saveOverride(ctx, ov); err != nil {
			return nil, errcode.New(errcode.Internal, "保存任务配置失败").WithDetail(err.Error())
		}
	}
	// ⚠️ 顺序:先落库再 Reload。反过来的话 Reload 失败时 DB 和内存不一致,
	// 而 DB 是下次启动的真相 —— 重启后行为变成另一个样子,极难排查。
	// cronx.Reload 内部已回滚到旧覆盖值,这里把 DB 也写回去。
	if err := sc.Reload(jobKey, ov); err != nil {
		logger.L().Warn("cronx: 热更新失败,已回滚",
			zap.String("job_key", jobKey), zap.Error(err))
		if s.db != nil && hadRow {
			if rerr := s.saveOverride(ctx, prev); rerr != nil {
				logger.L().Error("cronx: 回滚 DB 失败,重启后行为将与当前不一致",
					zap.String("job_key", jobKey), zap.Error(rerr))
			}
		}
		return nil, errcode.New(errcode.BadRequest, "任务热更新失败").WithDetail(err.Error())
	}

	e, _ := sc.Get(jobKey)
	v := s.jobItemView(sc, e)
	return &v, nil
}

// saveOverride upsert sys_job 行(按 job_key 唯一索引)
//
// ⚠️ ⚠️ Insert 必须用 map 形式,**不能用 struct**。
//
// SysJob 的 Enabled / IsActive 带 `default:true` tag,而 GORM 对"带 default 的
// 零值字段"会**替换成默认值而不是写入你给的值**(实测:Create(&SysJob{Enabled:false})
// 落库是 enabled=true,Select 显式列也救不了,只有 map 形式才写 false)。
//
// 后果非常隐蔽:停用一个**从未配置过**的任务时,内存里 Reload 的是 enabled=false
// (当次生效),但 DB 里是 true —— 重启后 LoadOverrides 又把它读回来,任务自己复活了。
// 这正是本模块设计上最想避免的"重启前后行为不一致"。
func (s *jobService) saveOverride(ctx context.Context, ov cronx.Override) error {
	now := time.Now()
	var existing model.SysJob
	err := s.db.WithContext(ctx).Where("job_key = ?", ov.JobKey).First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return s.db.WithContext(ctx).Model(&model.SysJob{}).Create(map[string]any{
			"id": sonyflake.Next(), "created_at": now, "updated_at": now,
			"job_key": ov.JobKey, "name": ov.Name, "description": ov.Description,
			"cron_expr": ov.CronExpr, "enabled": ov.Enabled,
			"sort": ov.Sort, "is_active": ov.IsActive,
		}).Error
	case err != nil:
		return err
	default:
		// Update 也用 map:struct 形式同样会跳过零值字段,「把 enabled 改成 false」
		// 用 struct 会被静默丢弃。
		return s.db.WithContext(ctx).Model(&model.SysJob{}).
			Where("job_key = ?", ov.JobKey).
			Updates(map[string]any{
				"name": ov.Name, "description": ov.Description,
				"cron_expr": ov.CronExpr, "enabled": ov.Enabled,
				"sort": ov.Sort, "is_active": ov.IsActive, "updated_at": now,
			}).Error
	}
}

func (s *jobService) TriggerNow(ctx context.Context, caller *Caller, jobKey string) (*JobRunView, error) {
	sc := s.sched()
	if sc == nil {
		return nil, errcode.New(errcode.NotFound, "定时任务未启用")
	}
	// 操作人靠 ctx 传:cronx 是 pkg 层,让它 import service 会形成依赖环。
	if caller != nil {
		ctx = cronx.WithOperator(ctx, caller.Username)
	}
	rec, err := sc.Trigger(ctx, jobKey, cronx.TriggerManual)
	if err != nil {
		return nil, mapTriggerError(err)
	}
	v := jobRunView(*rec)
	return &v, nil
}

// mapTriggerError 把 cronx 的哨兵错误映射到本仓 errcode。
//
// ⚠️ 区分 ErrJobRunning 和 ErrDegraded 是必须的:两者都是"没跑",
// 但运维的处置完全不同 —— 前者"上一轮还没完"是正常的,后者"Redis 掉了"
// 要立刻去查。都返 409 的话,监控上就是一片 409,看不出哪个是真问题。
// 降级走 500 而不是 409,就是为了让它在告警里跳出来。
func mapTriggerError(err error) error {
	switch {
	case errors.Is(err, cronx.ErrJobNotFound):
		return errcode.New(errcode.NotFound, "任务不存在")
	case errors.Is(err, cronx.ErrJobDisabled):
		return errcode.New(errcode.Forbidden, "任务已停用,请先启用")
	case errors.Is(err, cronx.ErrJobRunning):
		return errcode.New(errcode.Conflict, "任务正在执行中,请等待本轮结束")
	case errors.Is(err, cronx.ErrDegraded):
		return errcode.Newf(errcode.Internal,
			"去重能力降级(分布式锁不可用),已跳过本次触发:%v", err)
	case errors.Is(err, cronx.ErrEmptyCron):
		return errcode.New(errcode.BadRequest, "cron 表达式为空")
	case errors.Is(err, cronx.ErrInvalidCron):
		return errcode.New(errcode.BadRequest, "cron 表达式非法")
	default:
		return errcode.New(errcode.Internal, "触发失败").WithDetail(err.Error())
	}
}
