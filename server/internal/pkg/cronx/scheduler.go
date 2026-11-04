package cronx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/piiaudit"
	"rapid-admin/internal/pkg/sonyflake"
)

// Config 调度器配置(由 cmd/server 从 config.JobConfig 映射而来)
//
// 单独定义一份而不是直接吃 config.JobConfig:pkg 层不依赖 config 层,
// 跟 kms.Config 的做法一致 —— 映射写在 main,策略留在包内。
//
// ⚠️ 这里**没有**历史保留期字段。清理执行记录的归属是
// pkg/jobs 的 framework.job_history_purge 任务,它直接读
// config.JobConfig.HistoryRetention* —— 不是调度器的内建行为。
// (曾经有过 HistoryKeepDays/HistoryKeepMax 两个字段,声明了但一行都没读过:
// 调度器从不自己清理。留着它们只会让人以为清理逻辑在这里,找半天找不到。)
type Config struct {
	Enabled         bool
	TimeZone        string
	LockMode        string
	LockTTL         time.Duration
	LockRenew       bool
	DefaultTimeout  time.Duration
	ShutdownTimeout time.Duration

	// OnFinish 一次执行落库之后调用(M73 事件推送接入点)
	//
	// ⚠️ **回调,不是直接 import pkg/push**。依赖方向:cmd/server → cronx,
	// 回调把"装配"这件事留在上层。cronx 自己不知道"事件要推给谁、推成什么
	// 形状、哪个 topic 需要哪个权限" —— 那是装配层的事。而且让 cronx 依赖
	// Redis,会让 scheduler 的单测不得不跟着 Redis 一起跑。
	//
	// nil = 不通知(零开销,分支只有一次 nil 判断)。映射成 push 事件的那段
	// 代码在 cmd/server 里,见 jobs_notify.go。
	//
	// ctx 是 **WithoutCancel** 过的:停机 / 超时场景下原 ctx 已取消,
	// 而"这次执行为什么失败"恰恰是此刻最该推出去的信息。
	OnFinish func(ctx context.Context, rec *model.SysJobRun)
}

// 默认值(0 值兜底放 pkg 内,不在 config 包 —— 与 cachex.L1Cap 0→5000 同分工)
const (
	defaultLockTTL      = 30 * time.Second
	defaultTimeout      = 30 * time.Minute
	defaultShutdownWait = 30 * time.Second
	// DefaultLockKeyPrefix 锁 key 前缀。多套系统共用一套 Redis 时靠它隔开
	DefaultLockKeyPrefix = "cronx:lock:"
)

// Scheduler 定时任务调度器
type Scheduler struct {
	cfg    Config
	store  Store
	locker Locker
	cron   *cron.Cron
	loc    *time.Location
	instID string

	// overrides DB 侧覆盖值。运行期会被管理端 PATCH 改,所以要加锁。
	mu        sync.RWMutex
	overrides map[string]Override
	// entries jobKey → cron entry id。
	// robfig 内部有锁保护它自己的 entry,但**从外部按 jobKey 反查 entry id**
	// 得自己维护这张映射 —— 这也是热更新(PATCH 改频率立刻生效)的前提。
	entries map[string]cron.EntryID

	// inflight 运行中任务的 cancel,供 Shutdown 强杀。
	//
	// 键是**每次运行的序号**,不是 jobKey —— 降级模式下(锁不可用)同一个任务
	// 本来就可能同时跑两份,用 jobKey 做键会让后一份覆盖前一份的 cancel,
	// 于是 Shutdown 强杀时漏掉先跑的那份,停机被无限拖住。
	inflightMu  sync.Mutex
	inflight    map[uint64]context.CancelFunc
	inflightSeq uint64

	// wg 统计 TriggerNow 异步起的那批;runs 统计**每一次** Trigger 的进出。
	//
	// 为什么要两个:手动触发是 HTTP handler **同步**调 Trigger 的,不在 wg 里。
	// 只等 wg 的话,"优雅停机完成"会在任务还在跑的时候返回 —— 运维看到日志说
	// 停好了就 kill 容器,任务被腰斩,留下的半截数据比直接崩溃还难查。
	runs      sync.WaitGroup
	wg        sync.WaitGroup
	startOnce sync.Once
	stopOnce  sync.Once
	started   bool

	// closeMu + closing 把"触发入口的 Add"与"Shutdown 的 Wait"隔在两把
	// 顺序固定的锁之间,建立 happens-before:Trigger/TriggerNow 在 closeMu
	// 内查 closing 并 Add,Shutdown 在 closeMu 内置 closing 后才 Wait。
	// 没有这道门,Trigger 的 runs.Add(1) 可以和 Shutdown 的 runs.Wait()
	// 并发(WaitGroup 规则:计数为 0 时的 Add 必须先于 Wait)—— 停机瞬间
	// 进来的手动触发会被腰斩:Wait 看不到刚 Add 的那次,提前返回"已结束"。
	closeMu sync.Mutex
	closing bool
}

// New 构造调度器(不启动)
func New(cfg Config, store Store, locker Locker) (*Scheduler, error) {
	if store == nil {
		store = nopStore{}
	}
	if locker == nil {
		locker = nopLocker{}
	}
	if cfg.LockTTL <= 0 {
		cfg.LockTTL = defaultLockTTL
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = defaultTimeout
	}

	loc := time.Local
	if cfg.TimeZone != "" {
		l, err := time.LoadLocation(cfg.TimeZone)
		if err != nil {
			// 时区配错必须启动失败:`0 3 * * *` 到底在哪跑,不能靠猜。
			// 而且配错时症状是"任务跑了但时间不对",极难从日志发现。
			return nil, fmt.Errorf("cronx: 解析 timezone %q 失败: %w", cfg.TimeZone, err)
		}
		loc = l
	}

	s := &Scheduler{
		cfg:       cfg,
		store:     store,
		locker:    locker,
		loc:       loc,
		instID:    newInstanceID(),
		overrides: make(map[string]Override),
		entries:   make(map[string]cron.EntryID),
		inflight:  make(map[uint64]context.CancelFunc),
	}
	s.cron = cron.New(
		// ⚠️ 必须 WithLocation,不能用 time.Local:容器里 TZ 没设时 time.Local == UTC,
		// `0 3 * * *` 会在北京时间 11:00 跑,而配置文件里明明写着 Asia/Shanghai。
		cron.WithLocation(loc),
		cron.WithParser(cron.NewParser(cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor)),
		cron.WithChain(
			cron.Recover(cronLogger{}),
			// 单副本内防堆积。跨副本靠 Layer 0/1,与这个无关。
			cron.SkipIfStillRunning(cronLogger{}),
		),
	)
	return s, nil
}

// ParseCron 用与调度器**完全相同**的解析器校验表达式。
//
// ⚠️ 必须是同一个 Parser:校验用一套、调度用另一套,会出现"保存时通过、
// 但实际没注册上"的静默失败(WeKnora 就在 scheduler.go:124 踩过这个坑的位置)。
func ParseCron(expr string) (cron.Schedule, error) {
	if expr == "" {
		return nil, ErrEmptyCron
	}
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	return p.Parse(expr)
}

// NextRuns 返回未来 n 次触发时间 —— 管理端"改频率"时实时预览用。
//
// 成本极低(纯计算)、体验提升极大:运维改完表达式立刻看到"下一次是明天 03:00",
// 而不是提交后等到 03:00 才发现表达式写错、任务根本没注册。
func NextRuns(expr string, n int, loc *time.Location) ([]time.Time, error) {
	sched, err := ParseCron(expr)
	if err != nil {
		return nil, err
	}
	if loc == nil {
		loc = time.Local
	}
	if n <= 0 {
		n = 5
	}
	out := make([]time.Time, 0, n)
	t := time.Now().In(loc)
	for i := 0; i < n; i++ {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out, nil
}

// Start 加载覆盖值 + 回收崩溃行 + 注册 cron
func (s *Scheduler) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		logger.L().Info("cronx: job.enabled=false,定时任务整体关闭(不注册任何 entry)",
			zap.String("lock_mode", s.cfg.LockMode))
		return nil
	}

	// 1) 读 DB 覆盖 + reconcile 残留行
	ovs, err := s.store.LoadOverrides(ctx)
	if err != nil {
		// 覆盖值读不到不该启动失败:退化成"全用代码默认值"继续跑。
		// 定时任务停摆比"频率没按 DB 设置"更难发现。
		logger.L().Warn("cronx: 加载 sys_job 覆盖值失败,退化为全部使用代码默认配置", zap.Error(err))
		ovs = map[string]Override{}
	}
	s.mu.Lock()
	s.overrides = ovs
	s.mu.Unlock()

	stale := reconcile(ovs, func(key string) bool { _, ok := lookup(key); return ok })
	if len(stale) > 0 {
		if err := s.store.DeactivateStale(ctx, stale); err != nil {
			logger.L().Warn("cronx: 置残留行为 is_active=false 失败", zap.Error(err))
		}
	}

	// 2) 回收崩溃遗留的 running 行。
	//    阈值取「本进程最长任务 Timeout + 宽限」,而不是固定值 ——
	//    固定 1 小时会把一个合法的跑了两小时的任务误判成"进程死了"。
	before := time.Now().Add(-(s.longestTimeout() + StaleGrace))
	n, err := s.store.RecoverStaleRuns(ctx, before)
	logStaleRecovered(n, err)

	// 3) 注册
	keys := registeredKeys()
	failed := 0
	for _, key := range keys {
		if err := s.register(key); err != nil {
			logger.L().Error("cronx: 任务注册失败,该任务本次不会执行",
				zap.String("job_key", key), zap.Error(err))
			failed++
		}
	}
	s.started = true
	s.cron.Start()
	logger.L().Info("cronx ready",
		zap.String("timezone", s.loc.String()),
		zap.String("instance_id", s.instID),
		zap.String("lock_mode", s.cfg.LockMode),
		zap.Bool("lock_available", s.locker.Available()),
		zap.Int("registered", len(keys)-failed),
		zap.Int("failed", failed))
	return nil
}

// Shutdown 优雅停机:不再触发 → 等在跑的任务 → 超时强杀
func (s *Scheduler) Shutdown(ctx context.Context) {
	// ctx 只用来"比配置的预算更早收手"(比如整个进程只剩 5 秒了)。
	// 调用方不传、或传的 ctx 没有 deadline 时,**用 cfg.ShutdownTimeout** ——
	// 让 job.shutdown_timeout 这个配置项真的说了算,而不是只在 main.go
	// 恰好传对了才生效。
	budget := s.cfg.ShutdownTimeout
	if budget <= 0 {
		budget = defaultShutdownWait
	}
	var cancel context.CancelFunc
	if ctx == nil {
		ctx, cancel = context.WithTimeout(context.Background(), budget)
		defer cancel()
	} else if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}
	s.stopOnce.Do(func() {
		// robfig 的 Stop() 返回的 ctx 在所有已触发的任务跑完后 Done
		<-s.cron.Stop().Done()
	})

	// 先关门再等:置 closing 之后不会再有新的 runs/wg Add,已经 Add 的
	// 必然 Done —— waitGroups 的 Wait 与触发入口的 Add 之间由此有
	// happens-before,不会再漏看任何一次在途触发。
	s.closeMu.Lock()
	s.closing = true
	s.closeMu.Unlock()

	if waitGroups(ctx, &s.wg, &s.runs) {
		logger.L().Info("cronx: 所有在跑任务已结束")
		return
	}

	// 停机预算用完:强杀在跑的任务。它们会被 ctx 取消,记 failed。
	logger.L().Warn("cronx: 停机预算耗尽,强制取消在跑任务",
		zap.Int("inflight", s.inflightCount()),
		zap.Duration("budget", budget))
	s.cancelAllInflight()
	// 这里不再设期限:ctx 已经取消过了,handler 一定会收尾。
	// 再套一个 ctx 只是把"停不下来"变成"停了一半"。
	s.wg.Wait()
	s.runs.Wait()
}

// waitGroups 等所有 WaitGroup 归零。返回 true = 期间干净结束;
// false = ctx 先到,调用方该走强杀分支。
func waitGroups(ctx context.Context, wgs ...*sync.WaitGroup) bool {
	done := make(chan struct{})
	go func() {
		for _, w := range wgs {
			w.Wait()
		}
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// Trigger 触发一次任务。定时和手动都走这一个入口。
//
// 导出且不依赖 robfig 内部状态,是为了让测试**直接调它** ——
// 单测不该为了验证业务逻辑去 sleep 等 tick(WeKnora 的 scheduler_test.go:199
// 靠 time.Sleep(2500ms) 等一次触发,慢且在 CI 上会 flaky)。
func (s *Scheduler) Trigger(ctx context.Context, key string, src TriggerType) (*model.SysJobRun, error) {
	// 打点放在最前面:连"被 Layer 0 挡下"的这次触发也要计数,
	// 否则 Shutdown 可能在它还查库的时候判定"已空"。
	// ⚠️ Add 必须与关门检查在同一把锁内:凡是通过检查的 Add,必然发生在
	// closing=true 之前,Shutdown 的 Wait 就不可能漏看(WaitGroup 的
	// "计数为 0 时的 Add 必须先于 Wait"靠这道门保证)。
	s.closeMu.Lock()
	if s.closing {
		s.closeMu.Unlock()
		return nil, ErrShuttingDown
	}
	s.runs.Add(1)
	s.closeMu.Unlock()
	defer s.runs.Done()

	job, ok := lookup(key)
	if !ok {
		return nil, ErrJobNotFound
	}
	eff := s.effective(job)
	if !eff.EnabledEffective() {
		return nil, ErrJobDisabled
	}

	// ---- 锁是否可用 ----
	// ⚠️ 两层各自独立降级,**不能合成一个 degraded 开关**:
	// 早先把它们合成一个,结果"DB 查不到"会连带把加锁整段跳过 ——
	// 而 Redis 明明好着,跨副本互斥恰恰是最该保住的那一层。
	// 现在 lockUsable 只看锁层,layer0OK 只看查库层,degraded 只是给
	// 执行记录/指标用的**汇总标记**。
	lockUsable := s.locker.Available()
	layer0OK := true

	// ---- Layer 0:查库,挡"上一次还没跑完" ----
	running, err := s.store.HasRunning(ctx, key)
	if err != nil {
		// 查库失败不阻断执行(定时任务停摆比重复执行更难发现),
		// 但要如实标记,不能让它悄悄降级。
		// 注意这里**不**记 lock_skipped:那个指标的含义是"本次被跳过",
		// 而 Layer 0 查库失败时本次照常执行了。降级由 job_degraded 表达。
		logger.L().Warn("cronx: Layer 0 查询失败,本次执行不受重叠保护", zap.Error(err))
		layer0OK = false
	} else if running {
		recordLockSkipped(key)
		logger.L().Debug("cronx: 上一次尚未结束,跳过本次触发", zap.String("job_key", key))
		return nil, ErrJobRunning
	}

	// ---- Layer 1:加锁,挡"同一毫秒双开" ----
	owner := ""
	locked := false
	lockUsable = false
	if s.locker.Available() {
		owner = newOwnerToken(s.instID)
		ok, lerr := s.locker.TryLock(ctx, lockKey(key), owner, s.cfg.LockTTL)
		if lerr != nil {
			logger.L().Warn("cronx: 加锁失败,本次执行不受并发保护", zap.Error(lerr))
		} else if !ok {
			recordLockSkipped(key)
			logger.L().Debug("cronx: 锁被其他副本持有,跳过本次触发", zap.String("job_key", key))
			return nil, ErrJobRunning
		} else {
			locked = true
			lockUsable = true
		}
	}
	degraded := !lockUsable || !layer0OK

	// 声明为非幂等的任务在降级模式下不跑:重复执行会写坏数据,
	// 而这类任务恰恰是运维不敢关掉的那类(数据同步 / 对账)。
	if degraded && job.NonIdempotent {
		logger.L().Warn("cronx: 去重能力降级,跳过非幂等任务(不冒险重复执行)",
			zap.String("job_key", key))
		recordDegraded(key)
		return nil, ErrDegraded
	}

	rec := &model.SysJobRun{
		BaseModel:   model.BaseModel{ID: sonyflake.Next()},
		JobKey:      key,
		TriggerType: model.JobTriggerType(src),
		Status:      model.JobStatusRunning,
		StartedAt:   time.Now(),
		InstanceID:  s.instID,
		TraceID:     piiaudit.TraceIDFromContext(ctx),
		Operator:    operatorFromContext(ctx),
		Degraded:    degraded,
	}
	if cerr := s.store.CreateRun(ctx, rec); cerr != nil {
		logger.L().Warn("cronx: 写执行记录失败", zap.String("job_key", key), zap.Error(cerr))
	}

	timeout := job.Timeout
	if timeout <= 0 {
		timeout = s.cfg.DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runID := s.trackInflight(cancel)
	defer s.untrackInflight(runID)

	stopWatchdog := func() {}
	if locked && s.cfg.LockRenew {
		stopWatchdog = s.startWatchdog(runCtx, key, owner, cancel)
	}
	defer stopWatchdog()

	logger.L().Info("cronx: 任务开始",
		zap.String("job_key", key), zap.String("trigger", string(src)),
		zap.String("trace_id", rec.TraceID), zap.String("instance_id", s.instID),
		zap.Bool("degraded", degraded), zap.Duration("timeout", timeout))

	rerr := safeInvoke(runCtx, job)

	finished := time.Now()
	rec.FinishedAt = &finished
	rec.DurationMs = finished.Sub(rec.StartedAt).Milliseconds()
	rec.Error = truncate(errText(rerr), 2048)
	if rerr != nil {
		rec.Status = model.JobStatusFailed
	} else {
		rec.Status = model.JobStatusSuccess
	}
	rec.UpdatedAt = finished

	// ⚠️ 用 WithoutCancel:停机 / 超时场景下 ctx 已经被取消,
	// 但"这次执行为什么失败"恰恰是此刻最该落库 / 推出去的信息。
	finishCtx := context.WithoutCancel(ctx)
	if ferr := s.store.FinishRun(finishCtx, rec); ferr != nil {
		logger.L().Warn("cronx: 更新执行记录失败", zap.String("job_key", key), zap.Error(ferr))
	}

	if locked {
		if uerr := s.locker.Unlock(finishCtx, lockKey(key), owner); uerr != nil {
			logger.L().Warn("cronx: 释放锁失败", zap.String("job_key", key), zap.Error(uerr))
		}
	}

	logger.L().Info("cronx: 任务结束",
		zap.String("job_key", key), zap.String("status", string(rec.Status)),
		zap.Int64("duration_ms", rec.DurationMs), zap.Bool("degraded", degraded))
	recordRun(key, src, string(rec.Status), rec.DurationMs, degraded)

	// 事件推送(M73)。⚠️ 只在"真的落了执行记录"时通知 ——
	// 被 Layer 0/1 挡下(ErrJobRunning)和降级跳过(ErrDegraded)都**不发**:
	// 那几种情况 DB 里没有新状态,前端收到信号去 refetch 拉回来的还是同一批
	// 数据,纯浪费一次请求。事件的语义是"有东西变了,去看",不是"发生过什么"。
	s.notifyFinish(finishCtx, rec)

	return rec, nil
}

// notifyFinish 调 Config.OnFinish,并吞掉 panic。
//
// ⚠️ 必须吞:回调是**外部**代码,而 Trigger 的调用方可能是 HTTP handler
// (手动触发是同步跑的) —— 回调一 panic,用户看到 500,而任务其实已经跑完、
// 记录已经落库了。推送是尽力而为的附加能力,绝不该反过来把主流程搞挂。
func (s *Scheduler) notifyFinish(ctx context.Context, rec *model.SysJobRun) {
	if s.cfg.OnFinish == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.L().Error("cronx: OnFinish 回调 panic,已忽略(任务本身已跑完并落库)",
				zap.String("job_key", rec.JobKey), zap.Any("panic", r))
		}
	}()
	s.cfg.OnFinish(ctx, rec)
}

// TriggerNow 是异步触发:给 cron 回调和"不想等结果"的调用方用
//
// wg.Add 同样要过关门检查:凡是通过检查的 Add,必然发生在 closing=true
// 之前,Shutdown 的 Wait 不会漏看。停机期间到达的触发直接丢弃(此时
// cron 已 Stop,能到这里的只有和关门赛跑的回调 / 请求)。
func (s *Scheduler) TriggerNow(ctx context.Context, key string, src TriggerType) {
	s.closeMu.Lock()
	if s.closing {
		s.closeMu.Unlock()
		return
	}
	s.wg.Add(1)
	s.closeMu.Unlock()
	go func() {
		defer s.wg.Done()
		if _, err := s.Trigger(ctx, key, src); err != nil && !errors.Is(err, ErrJobRunning) {
			logger.L().Error("cronx: 任务执行失败",
				zap.String("job_key", key), zap.Error(err))
		}
	}()
}

// Reload 应用 DB 覆盖值并**立刻重注册 cron entry**。
//
// ⚠️ 漏掉重注册是最容易犯的错:只写 DB 的话,robfig 里注册的还是旧表达式,
// 改频率要等进程重启才生效 —— 而且没有报错、没有日志,测试也可能绿
// (如果测试只断言 DB 变化、不断言 entry 生效)。
func (s *Scheduler) Reload(key string, ov Override) error {
	if _, ok := lookup(key); !ok {
		return ErrJobNotFound
	}

	// 先校验、后落值。顺序反了的话,非法表达式会短暂地改掉一半状态,
	// 期间若进程挂掉,DB 里就留下一条"永远注册不上"的覆盖行。
	if ov.CronExpr != "" {
		if _, err := ParseCron(ov.CronExpr); err != nil {
			return err
		}
	}

	s.mu.Lock()
	prevOv, hadPrev := s.overrides[key]
	s.overrides[key] = ov
	s.mu.Unlock()

	s.unregister(key)
	if !s.started || !s.cfg.Enabled {
		return nil
	}
	if err := s.register(key); err != nil {
		// 真回滚:不能留下"DB 说启用、实际没注册"的状态 —— 那意味着
		// 重启前行为和重启后行为不一致,是最难排查的一类问题。
		//
		// 注意必须**用旧值重新 register**,而不是把 entry id 塞回去:
		// unregister 已经调过 cron.Remove,那个 id 在 robfig 里已经不存在了。
		s.unregister(key)
		s.mu.Lock()
		if hadPrev {
			s.overrides[key] = prevOv
		} else {
			delete(s.overrides, key)
		}
		s.mu.Unlock()
		if hadPrev {
			if rerr := s.register(key); rerr != nil {
				// 旧值是跑通过的,还注册不上说明有别的问题 —— 必须 loud,
				// 否则任务从此静默消失,直到有人重启服务才发现
				logger.L().Error("cronx: 回滚到旧配置仍然失败,该任务已停止调度直到下次 Reload 或重启",
					zap.String("job_key", key), zap.Error(rerr))
			}
		}
		return err
	}
	return nil
}

// effective 合并代码定义与 DB 覆盖
func (s *Scheduler) effective(job Job) Effective {
	s.mu.RLock()
	ov, has := s.overrides[job.Key]
	s.mu.RUnlock()
	if !has {
		return merge(job, nil)
	}
	return merge(job, &ov)
}

func (s *Scheduler) register(key string) error {
	job, _ := lookup(key)
	eff := s.effective(job)
	if !eff.EnabledEffective() {
		return nil
	}
	if _, err := ParseCron(eff.CronExpr); err != nil {
		return err
	}
	k := key
	id, err := s.cron.AddFunc(eff.CronExpr, func() {
		s.TriggerNow(context.Background(), k, TriggerSchedule)
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.entries[key] = id
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) unregister(key string) {
	s.mu.Lock()
	id, ok := s.entries[key]
	delete(s.entries, key)
	s.mu.Unlock()
	if ok {
		s.cron.Remove(id)
	}
}

// startWatchdog 看门狗:每 ttl/3 续期一次,直到返回的 stop 被调
//
// 没有它,长任务跑到一半锁就自然过期了,第二个副本直接抢进来并发写。
func (s *Scheduler) startWatchdog(ctx context.Context, key, owner string, cancel context.CancelFunc) func() {
	interval := s.cfg.LockTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				// 续期用独立 ctx:ctx 一旦被取消,释放锁和续期就都做不到了
				// ⚠️ 续期 ctx 叫 rcancel,不能叫 cancel —— 同名会把下面
				// "丢锁就取消任务" 那个 cancel 遮蔽掉,结果只取消了这次续期,
				// 真正的 run ctx 毫发无损,handler 照跑到底 = 并发写。
				rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.LockTTL)
				ok, err := s.locker.Renew(rctx, lockKey(key), owner, s.cfg.LockTTL)
				rcancel()
				if err != nil {
					logger.L().Warn("cronx: 锁续期失败", zap.String("job_key", key), zap.Error(err))
					continue
				}
				if !ok {
					// 锁已经不属于我了:说明它超时被别人抢走了。
					// 此时继续跑就是并发写 —— 必须真的把 ctx 取消掉,
					// 只 return 是**不够的**:handler 拿不到任何信号,会照跑到底,
					// 于是两个副本真的并发写同一批数据。
					logger.L().Error("cronx: 锁已丢失(疑似超时被其他副本接管),取消本次执行",
						zap.String("job_key", key))
					cancel()
					return
				}
			}
		}
	}()
	return func() { close(stop) }
}

func (s *Scheduler) trackInflight(cancel context.CancelFunc) uint64 {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	s.inflightSeq++
	id := s.inflightSeq
	s.inflight[id] = cancel
	return id
}

func (s *Scheduler) untrackInflight(id uint64) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	delete(s.inflight, id)
}

func (s *Scheduler) inflightCount() int {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	return len(s.inflight)
}

func (s *Scheduler) cancelAllInflight() {
	s.inflightMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.inflight))
	for _, c := range s.inflight {
		cancels = append(cancels, c)
	}
	s.inflightMu.Unlock()
	for _, c := range cancels {
		c()
	}
}

func (s *Scheduler) longestTimeout() time.Duration {
	var max time.Duration
	for _, k := range registeredKeys() {
		j, _ := lookup(k)
		t := j.Timeout
		if t <= 0 {
			t = s.cfg.DefaultTimeout
		}
		if t > max {
			max = t
		}
	}
	if max <= 0 {
		max = s.cfg.DefaultTimeout
	}
	return max
}

func lockKey(jobKey string) string { return jobKey }

// safeInvoke 拦住 Handler 的 panic。
//
// cron.Recover 只在 robfig 触发的那条路径上兜底 —— 手动触发走的是我们自己的
// Trigger,panic 会一路冒到 HTTP handler。所以这里必须独立兜一次。
func safeInvoke(ctx context.Context, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	if job.Handler == nil {
		return fmt.Errorf("cronx: 任务 %s 未实现 Handler", job.Key)
	}
	return job.Handler(ctx)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// errText 把 error 转成可存储的字符串(nil → "")
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func newInstanceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

// RuntimeInfo 运行时健康(管理端横幅用)
type RuntimeInfo struct {
	Enabled       bool   `json:"enabled"`
	LockMode      string `json:"lock_mode"`
	LockAvailable bool   `json:"lock_available"`
	// Degraded 锁不可用 → 多副本部署下任务可能重复执行
	Degraded   bool   `json:"degraded"`
	InstanceID string `json:"instance_id"`
	TimeZone   string `json:"timezone"`
	Registered int    `json:"registered"`
}

// Runtime 返回运行时健康
func (s *Scheduler) Runtime() RuntimeInfo {
	s.mu.RLock()
	registered := len(s.entries)
	s.mu.RUnlock()
	return RuntimeInfo{
		Enabled:       s.cfg.Enabled,
		LockMode:      s.cfg.LockMode,
		LockAvailable: s.locker.Available(),
		Degraded:      !s.locker.Available(),
		InstanceID:    s.instID,
		TimeZone:      s.loc.String(),
		Registered:    registered,
	}
}

// List 返回全部任务的生效配置(管理端列表数据源)
func (s *Scheduler) List() []Effective {
	keys := registeredKeys()
	out := make([]Effective, 0, len(keys))
	for _, k := range keys {
		job, _ := lookup(k)
		out = append(out, s.effective(job))
	}
	// 按 DB sort,同 sort 按注册顺序稳定排序
	s.mu.RLock()
	defer s.mu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := 0, 0
		if o, ok := s.overrides[out[i].Key]; ok {
			si = o.Sort
		}
		if o, ok := s.overrides[out[j].Key]; ok {
			sj = o.Sort
		}
		return si < sj
	})
	return out
}

// Get 返回单个任务的生效配置
func (s *Scheduler) Get(key string) (Effective, error) {
	job, ok := lookup(key)
	if !ok {
		return Effective{}, ErrJobNotFound
	}
	return s.effective(job), nil
}

// NextRunAt 返回任务下一次触发时间,未注册则返零值
func (s *Scheduler) NextRunAt(key string) time.Time {
	s.mu.RLock()
	id, ok := s.entries[key]
	s.mu.RUnlock()
	if !ok {
		return time.Time{}
	}
	e := s.cron.Entry(id)
	if e.ID == 0 || e.Schedule == nil {
		return time.Time{}
	}
	return e.Next
}

// Override 返回任务当前的 DB 覆盖行(管理端"恢复默认"用)
func (s *Scheduler) Override(key string) (Override, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.overrides[key]
	return o, ok
}

// cronLogger 把 robfig 的日志接到 zap
type cronLogger struct{}

func (cronLogger) Info(msg string, keysAndValues ...any) {
	fields := zapFields(keysAndValues)
	logger.L().Info("cronx: "+msg, fields...)
}
func (cronLogger) Error(err error, msg string, keysAndValues ...any) {
	fields := zapFields(keysAndValues)
	fields = append(fields, zap.Error(err))
	logger.L().Error("cronx: "+msg, fields...)
}

func zapFields(kv []any) []zap.Field {
	out := make([]zap.Field, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		out = append(out, zap.Any(k, kv[i+1]))
	}
	return out
}

// ─────────── 手动触发的操作人 ───────────
//
// cronx 是 pkg 层,不能 import internal/service(会被 service 反向 import 形成环),
// 所以操作人靠 ctx 传:API 层拿到 Caller 后 WithOperator 塞进来。

type operatorKey struct{}

// WithOperator 把操作人(用户名)挂到 ctx,执行记录会记下来
func WithOperator(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, operatorKey{}, name)
}

func operatorFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(operatorKey{}).(string); ok {
		return v
	}
	return ""
}
