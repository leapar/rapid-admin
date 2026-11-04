package cronx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rapid-admin/internal/model"
)

// ─────────── fakes ───────────
//
// 纪律:单测不连真 DB / 真 Redis(与本仓 cachex 测试同一条)。
// 另外**绝不靠 time.Sleep 等 cron tick** —— Trigger 是导出的、且不依赖
// robfig 内部状态,所以直接调它就能验证全部业务逻辑。
// WeKnora 的 scheduler_test.go:199 靠 sleep(2500ms) 等一次触发,
// 慢、且在负载高的 CI 上会 flaky。

// fakeStore 记录调用,并可编程地返回"上一次还在跑"
type fakeStore struct {
	mu sync.Mutex

	running bool // HasRunning 返回值
	runErr  error
	loadErr error
	// overrides 是 LoadOverrides 的返回值。Start 会**整体替换** s.overrides,
	// 所以用例要注入覆盖值必须走这里,不能直接写 s.overrides ——
	// Start 之前写的那份会被 LoadOverrides 的结果覆盖掉。
	overrides map[string]Override

	created  []*model.SysJobRun
	finished []*model.SysJobRun

	// hasRunningCalls 记录 Layer 0 被查了几次 —— 用来证明"抢锁失败时
	// 到底有没有查库"(这是本模块最容易写反的一条顺序)
	hasRunningCalls int
}

func (f *fakeStore) HasRunning(context.Context, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hasRunningCalls++
	return f.running, f.runErr
}

// ⚠️ CreateRun / FinishRun 存的是**值拷贝**而不是指针。真实 gorm 在调用
// 那一刻就把行序列化出去了;存指针的话 fake 会看到 Trigger 事后改写的最终态,
// 「创建时是 running」这条断言就永远测不出东西。
func (f *fakeStore) CreateRun(_ context.Context, r *model.SysJobRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *r
	f.created = append(f.created, &cp)
	return nil
}
func (f *fakeStore) FinishRun(_ context.Context, r *model.SysJobRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *r
	f.finished = append(f.finished, &cp)
	return nil
}
func (f *fakeStore) LoadOverrides(context.Context) (map[string]Override, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if f.overrides != nil {
		return f.overrides, nil
	}
	return map[string]Override{}, nil
}
func (f *fakeStore) DeactivateStale(context.Context, []string) error { return nil }
func (f *fakeStore) RecoverStaleRuns(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (f *fakeStore) PurgeHistory(context.Context, int, int) (int64, error) { return 0, nil }

func (f *fakeStore) runs() (created, finished []*model.SysJobRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*model.SysJobRun(nil), f.created...), append([]*model.SysJobRun(nil), f.finished...)
}

// fakeLocker 可编程的锁
type fakeLocker struct {
	mu sync.Mutex

	avail   bool
	tryOk   bool
	tryErr  error
	renewOk bool

	locked   []string // 成功持锁的 key(按顺序)
	unlocked []string
	renewed  int
}

func newFakeLocker() *fakeLocker {
	return &fakeLocker{avail: true, tryOk: true, renewOk: true}
}
func (l *fakeLocker) Available() bool { return l.avail }
func (l *fakeLocker) TryLock(_ context.Context, key, _ string, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.tryErr != nil {
		return false, l.tryErr
	}
	if l.tryOk {
		l.locked = append(l.locked, key)
	}
	return l.tryOk, nil
}
func (l *fakeLocker) Renew(context.Context, string, string, time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.renewed++
	return l.renewOk, nil
}
func (l *fakeLocker) Unlock(_ context.Context, key, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.unlocked = append(l.unlocked, key)
	return nil
}
func (l *fakeLocker) stat() (locked, unlocked []string, renewed int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.locked...), append([]string(nil), l.unlocked...), l.renewed
}

// ─────────── helpers ───────────

// newTestSched 造一个未启动的调度器。⚠️ 不调 Start —— Start 会真的
// s.cron.Start() 把 ticker 跑起来,测试要的就是"只调 Trigger"的确定性。
func newTestSched(t *testing.T, store Store, locker Locker, mut ...func(*Config)) *Scheduler {
	t.Helper()
	cfg := Config{
		Enabled:         true,
		TimeZone:        "Asia/Shanghai",
		LockMode:        LockModeAuto,
		LockTTL:         30 * time.Second,
		LockRenew:       false, // 默认关:看门狗会起 goroutine + ticker,单测里没意义
		DefaultTimeout:  30 * time.Minute,
		ShutdownTimeout: 5 * time.Second,
	}
	for _, m := range mut {
		m(&cfg)
	}
	s, err := New(cfg, store, locker)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// resetForUnit 清全局注册表与单例 —— 每个用例开头调。
// 生产代码永远不调(见 resetRegistry 注释)。
func resetForUnit(t *testing.T) {
	t.Helper()
	resetRegistry()
	ClearDefault()
	t.Cleanup(func() { resetRegistry(); ClearDefault() })
}

// okHandler 什么都不干的成功 handler
func okHandler(context.Context) error { return nil }

// ═══════════ T1 未注册 / 停用 ═══════════

func TestT1_Trigger_NotRegistered(t *testing.T) {
	resetForUnit(t)
	s := newTestSched(t, &fakeStore{}, newFakeLocker())

	if _, err := s.Trigger(context.Background(), "nope", TriggerManual); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("want ErrJobNotFound, got %v", err)
	}
}

func TestT1_Trigger_DisabledInCode(t *testing.T) {
	resetForUnit(t)
	called := false
	Register(Job{Key: "t.off", Name: "停用", Cron: "* * * * *", Enabled: false,
		Handler: func(context.Context) error { called = true; return nil }})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())

	// code.Enabled=false → 不该跑,更不该写执行记录
	if _, err := s.Trigger(context.Background(), "t.off", TriggerManual); !errors.Is(err, ErrJobDisabled) {
		t.Fatalf("want ErrJobDisabled, got %v", err)
	}
	if called {
		t.Fatal("已停用任务的 Handler 被执行了")
	}
}

func TestT1_Trigger_DisabledByDB(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.db.off", Name: "DB 停用", Cron: "* * * * *", Enabled: true})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())
	// DB 行 enabled=false → 生效值 = code.True && DB.False = false
	s.overrides["t.db.off"] = Override{JobKey: "t.db.off", Enabled: false, IsActive: true}

	if _, err := s.Trigger(context.Background(), "t.db.off", TriggerManual); !errors.Is(err, ErrJobDisabled) {
		t.Fatalf("want ErrJobDisabled, got %v", err)
	}
}

func TestT1_Trigger_InactiveRowDisabled(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.inactive", Name: "残留行", Cron: "* * * * *", Enabled: true})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())
	// is_active=false(代码里删过任务又加回来的场景)
	s.overrides["t.inactive"] = Override{JobKey: "t.inactive", Enabled: true, IsActive: false}

	if _, err := s.Trigger(context.Background(), "t.inactive", TriggerManual); !errors.Is(err, ErrJobDisabled) {
		t.Fatalf("is_active=false 应等于停用, got %v", err)
	}
}

// ═══════════ T2 正常路径:记录 / 状态 / 释放锁 ═══════════

func TestT2_Trigger_Success_WritesRunningThenFinished(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.ok", Name: "正常", Cron: "* * * * *", Enabled: true, Handler: okHandler})
	st := &fakeStore{}
	lk := newFakeLocker()
	s := newTestSched(t, st, lk)

	rec, err := s.Trigger(context.Background(), "t.ok", TriggerSchedule)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Fatalf("want success, got %s", rec.Status)
	}
	if rec.TriggerType != model.JobTriggerSchedule {
		t.Fatalf("trigger_type 没落库: %q", rec.TriggerType)
	}
	if rec.FinishedAt == nil {
		t.Fatal("finished_at 为空")
	}
	if rec.Degraded {
		t.Fatal("锁可用时不该标 degraded")
	}

	created, finished := st.runs()
	if len(created) != 1 {
		t.Fatalf("CreateRun 调了 %d 次,期望 1", len(created))
	}
	if created[0].Status != model.JobStatusRunning {
		t.Fatalf("创建时状态应为 running,实际 %s", created[0].Status)
	}
	if len(finished) != 1 {
		t.Fatalf("FinishRun 调了 %d 次,期望 1", len(finished))
	}
	if finished[0].Status != model.JobStatusSuccess {
		t.Fatalf("终态应为 success,实际 %s", finished[0].Status)
	}

	locked, unlocked, _ := lk.stat()
	if len(locked) != 1 || len(unlocked) != 1 {
		t.Fatalf("锁应成对加/放: locked=%v unlocked=%v", locked, unlocked)
	}
}

func TestT2_Trigger_NilHandler_FailsLoudly(t *testing.T) {
	// 忘写 Handler 是最常见的注册错误。静默当成功会让"任务跑没跑"
	// 完全看不出来 —— 所以必须判 failed。
	resetForUnit(t)
	Register(Job{Key: "t.noimpl", Name: "没实现", Cron: "* * * * *", Enabled: true})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())

	rec, err := s.Trigger(context.Background(), "t.noimpl", TriggerSchedule)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if rec.Status != model.JobStatusFailed {
		t.Fatalf("没 Handler 应判 failed,实际 %s", rec.Status)
	}
	if !strings.Contains(rec.Error, "Handler") {
		t.Fatalf("错误信息应点明是缺 Handler: %q", rec.Error)
	}
}

func TestT2_Trigger_HandlerError_Recorded(t *testing.T) {
	resetForUnit(t)
	want := errors.New("上游接口 502")
	Register(Job{Key: "t.err", Name: "失败", Cron: "* * * * *", Enabled: true,
		Handler: func(context.Context) error { return want }})
	st := &fakeStore{}
	s := newTestSched(t, st, newFakeLocker())

	rec, err := s.Trigger(context.Background(), "t.err", TriggerManual)
	// Handler 出错**不是** Trigger 的错误:执行本身"完成了"(记录已落),
	// 只是业务失败。用 status 表达,而不是把 error 往外抛。
	if err != nil {
		t.Fatalf("Trigger 不应返回 error,got %v", err)
	}
	if rec.Status != model.JobStatusFailed {
		t.Fatalf("want failed, got %s", rec.Status)
	}
	if rec.Error != "上游接口 502" {
		t.Fatalf("error 未落库: %q", rec.Error)
	}
}

func TestT2_Trigger_Panic_BecomesFailedRecord(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.panic", Name: "panic", Cron: "* * * * *", Enabled: true,
		Handler: func(context.Context) error { panic("nil map 写崩了") }})
	st := &fakeStore{}
	s := newTestSched(t, st, newFakeLocker())

	// safeInvoke 必须兜住 —— 手动触发走 HTTP,panic 冒上去会打成 500 连接中断
	rec, err := s.Trigger(context.Background(), "t.panic", TriggerManual)
	if err != nil {
		t.Fatalf("panic 不该从 Trigger 冒出: %v", err)
	}
	if rec.Status != model.JobStatusFailed {
		t.Fatalf("want failed, got %s", rec.Status)
	}
	if !strings.Contains(rec.Error, "nil map 写崩了") {
		t.Fatalf("panic 信息没落库: %q", rec.Error)
	}
	if !strings.Contains(rec.Error, "goroutine") {
		t.Fatalf("stack 没落库 —— 只记 panic 文本排障等于没记: %q", rec.Error)
	}
}

func TestT2_Trigger_HandlerError_StillReleasesLock(t *testing.T) {
	// 失败也必须释放锁。只在成功时释放的话,一个天天失败的任务会把锁
	// 攥在手里到 TTL 过期 —— 之后每个周期都抢锁失败,任务彻底停摆,
	// 而且现场只有"任务一直在跑"这一条线索,极难定位。
	resetForUnit(t)
	Register(Job{Key: "t.errlock", Name: "失败仍放锁", Cron: "* * * * *", Enabled: true,
		Handler: func(context.Context) error { return errors.New("boom") }})
	lk := newFakeLocker()
	s := newTestSched(t, &fakeStore{}, lk)

	if _, err := s.Trigger(context.Background(), "t.errlock", TriggerSchedule); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	locked, unlocked, _ := lk.stat()
	if len(locked) != 1 {
		t.Fatalf("应尝试加锁: %v", locked)
	}
	if len(unlocked) != 1 {
		t.Fatalf("失败后没释放锁: locked=%v unlocked=%v", locked, unlocked)
	}
}

func TestT2_Trigger_ErrorTruncatedTo2048(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.big", Name: "超长错误", Cron: "* * * * *", Enabled: true,
		Handler: func(context.Context) error { return errors.New(strings.Repeat("x", 9000)) }})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())

	rec, _ := s.Trigger(context.Background(), "t.big", TriggerManual)
	if len(rec.Error) > 2048+len("...(truncated)") {
		t.Fatalf("error 未截断,长度 %d", len(rec.Error))
	}
}

func TestT2_Trigger_OperatorFromContext(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.op", Name: "操作人", Cron: "* * * * *", Enabled: true})
	st := &fakeStore{}
	s := newTestSched(t, st, newFakeLocker())

	ctx := WithOperator(context.Background(), "zhangsan")
	rec, _ := s.Trigger(ctx, "t.op", TriggerManual)
	if rec.Operator != "zhangsan" {
		t.Fatalf("operator 未落库: %q", rec.Operator)
	}
	// 定时触发没有操作人 —— 留空是对的,不该写 instance id 之类的东西
	rec2, _ := s.Trigger(context.Background(), "t.op", TriggerSchedule)
	if rec2.Operator != "" {
		t.Fatalf("定时触发不该有 operator: %q", rec2.Operator)
	}
}

// ═══════════ T3 Layer 0 去重 ═══════════

func TestT3_Layer0_SkipsWhenPreviousStillRunning(t *testing.T) {
	resetForUnit(t)
	calls := int32(0)
	Register(Job{Key: "t.l0", Name: "重叠", Cron: "* * * * *", Enabled: true,
		Handler: func(context.Context) error { atomic.AddInt32(&calls, 1); return nil }})
	st := &fakeStore{running: true}
	lk := newFakeLocker()
	s := newTestSched(t, st, lk)

	if _, err := s.Trigger(context.Background(), "t.l0", TriggerSchedule); !errors.Is(err, ErrJobRunning) {
		t.Fatalf("want ErrJobRunning, got %v", err)
	}
	if calls != 0 {
		t.Fatal("Layer 0 拦下了,Handler 却仍被执行")
	}
	// Layer 0 命中就**不该**再去抢 Layer 1 的锁 —— 否则无谓地在 Redis
	// 上留一个键,而且 degraded 判定会被锁状态污染
	if locked, _, _ := lk.stat(); len(locked) != 0 {
		t.Fatalf("Layer 0 命中后仍抢了锁: %v", locked)
	}
	if _, finished := st.runs(); len(finished) != 0 {
		t.Fatal("被 Layer 0 拦下却写了执行记录 —— 一条「我被跳过了」的记录" +
			"在 N 副本下会把历史表冲垮")
	}
}

func TestT3_Layer0_CheckBeforeLock(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.order", Name: "顺序", Cron: "* * * * *", Enabled: true})
	st := &fakeStore{running: true}
	s := newTestSched(t, st, newFakeLocker())

	s.Trigger(context.Background(), "t.order", TriggerManual)

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.hasRunningCalls != 1 {
		t.Fatalf("HasRunning 调了 %d 次,期望恰好 1 次", st.hasRunningCalls)
	}
}

func TestT3_Layer0_ErrorFailsOpenButMarksDegraded(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.l0err", Name: "查库失败", Cron: "* * * * *", Enabled: true, Handler: okHandler})
	st := &fakeStore{runErr: errors.New("db 掉了")}
	s := newTestSched(t, st, newFakeLocker())

	// fail-open:定时任务停摆比偶尔重复更难发现,所以照跑
	rec, err := s.Trigger(context.Background(), "t.l0err", TriggerSchedule)
	if err != nil {
		t.Fatalf("查库失败不该阻断执行: %v", err)
	}
	if !rec.Degraded {
		t.Fatal("查库失败后没标 degraded —— 降级会变成静默的")
	}
}

func TestT3_Layer0_ErrorStillTakesLock(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.l0err2", Name: "查库失败", Cron: "* * * * *", Enabled: true, Handler: okHandler})
	st := &fakeStore{runErr: errors.New("db 掉了")}
	lk := newFakeLocker()
	s := newTestSched(t, st, lk)

	rec, _ := s.Trigger(context.Background(), "t.l0err2", TriggerSchedule)
	// Layer 0 挂了,但 Layer 1 还活着 → 仍然受跨副本互斥保护,
	// 只是记录里诚实标了 degraded
	if !rec.Degraded {
		t.Fatal("应标 degraded")
	}
	locked, unlocked, _ := lk.stat()
	if len(locked) != 1 || len(unlocked) != 1 {
		t.Fatalf("Layer 0 失效后仍应走 Layer 1: locked=%v unlocked=%v", locked, unlocked)
	}
}

// ═══════════ T4 Layer 1 去重 + 降级 ═══════════

func TestT4_Layer1_SkipsWhenOtherReplicaHolds(t *testing.T) {
	resetForUnit(t)
	calls := int32(0)
	Register(Job{Key: "t.l1", Name: "跨副本", Cron: "* * * * *", Enabled: true,
		Handler: func(context.Context) error { atomic.AddInt32(&calls, 1); return nil }})
	lk := newFakeLocker()
	lk.tryOk = false // 别人拿着
	s := newTestSched(t, &fakeStore{}, lk)

	if _, err := s.Trigger(context.Background(), "t.l1", TriggerSchedule); !errors.Is(err, ErrJobRunning) {
		t.Fatalf("want ErrJobRunning, got %v", err)
	}
	if calls != 0 {
		t.Fatal("抢锁失败却仍执行了 Handler")
	}
}

func TestT4_Layer1_NotAvailable_DegradedButRuns(t *testing.T) {
	resetForUnit(t)
	calls := int32(0)
	Register(Job{Key: "t.degraded", Name: "降级", Cron: "* * * * *", Enabled: true,
		Handler: func(context.Context) error { atomic.AddInt32(&calls, 1); return nil }})
	lk := newFakeLocker()
	lk.avail = false
	s := newTestSched(t, &fakeStore{}, lk)

	rec, err := s.Trigger(context.Background(), "t.degraded", TriggerSchedule)
	if err != nil {
		t.Fatalf("锁不可用不该阻断幂等任务: %v", err)
	}
	if calls != 1 {
		t.Fatal("降级后幂等任务没执行")
	}
	if !rec.Degraded {
		t.Fatal("锁不可用没标 degraded")
	}
}

func TestT4_Layer1_TryLockError_DegradedButRuns(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.lockerr", Name: "加锁报错", Cron: "* * * * *", Enabled: true, Handler: okHandler})
	lk := newFakeLocker()
	lk.tryErr = errors.New("redis 超时")
	s := newTestSched(t, &fakeStore{}, lk)

	rec, err := s.Trigger(context.Background(), "t.lockerr", TriggerSchedule)
	if err != nil || !rec.Degraded {
		t.Fatalf("加锁报错应 fail-open 且标 degraded: err=%v degraded=%v", err, rec.Degraded)
	}
}

func TestT4_NonIdempotent_SkippedWhenDegraded(t *testing.T) {
	resetForUnit(t)
	calls := int32(0)
	Register(Job{Key: "t.nonidem", Name: "非幂等", Cron: "* * * * *", Enabled: true,
		NonIdempotent: true,
		Handler:       func(context.Context) error { atomic.AddInt32(&calls, 1); return nil }})
	lk := newFakeLocker()
	lk.avail = false
	s := newTestSched(t, &fakeStore{}, lk)

	// 降级下非幂等任务**不跑** —— 这是"fail-open 但不冒险重复"的落点
	_, err := s.Trigger(context.Background(), "t.nonidem", TriggerSchedule)
	if !errors.Is(err, ErrDegraded) {
		t.Fatalf("want ErrDegraded, got %v", err)
	}
	if calls != 0 {
		t.Fatal("降级下非幂等任务被执行了 —— 会写坏数据")
	}
}

func TestT4_NonIdempotent_RunsNormallyWhenLockAvailable(t *testing.T) {
	resetForUnit(t)
	calls := int32(0)
	Register(Job{Key: "t.nonidem2", Name: "非幂等", Cron: "* * * * *", Enabled: true,
		NonIdempotent: true,
		Handler:       func(context.Context) error { atomic.AddInt32(&calls, 1); return nil }})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())

	if _, err := s.Trigger(context.Background(), "t.nonidem2", TriggerManual); err != nil {
		t.Fatalf("锁可用时非幂等任务应正常跑: %v", err)
	}
	if calls != 1 {
		t.Fatal("锁可用时非幂等任务没跑")
	}
}

func TestT4_OwnerTokenDiffersPerRun(t *testing.T) {
	// 同一副本连跑两次必须拿到不同 owner:固定 owner 会让第一次的 CAS
	// 释放误删第二次的锁(见 lock.go Unlock 注释)
	a, b := newOwnerToken("host-1"), newOwnerToken("host-1")
	if a == b {
		t.Fatalf("owner token 重复: %q", a)
	}
	if !strings.HasPrefix(a, "host-1:") {
		t.Fatalf("owner token 应带实例 id: %q", a)
	}
}

// ═══════════ T5 看门狗 ═══════════

func TestT5_Watchdog_RenewsWhileRunning(t *testing.T) {
	resetForUnit(t)
	release := make(chan struct{})
	entered := make(chan struct{})
	Register(Job{Key: "t.watch", Name: "长任务", Cron: "* * * * *", Enabled: true,
		Handler: func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}})
	lk := newFakeLocker()
	// LockTTL 3s → 续期间隔 1s(下限),任务跑 2.2s 至少续 1~2 次
	s := newTestSched(t, &fakeStore{}, lk, func(c *Config) {
		c.LockTTL = 3 * time.Second
		c.LockRenew = true
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := s.Trigger(context.Background(), "t.watch", TriggerSchedule); err != nil {
			t.Errorf("Trigger: %v", err)
		}
	}()

	<-entered
	time.Sleep(2200 * time.Millisecond)
	close(release)
	<-done

	_, _, renewed := lk.stat()
	if renewed == 0 {
		t.Fatal("长任务期间一次都没续期 —— 锁会自然过期,别的副本会抢进来并发写")
	}
}

func TestT5_Watchdog_NotStartedWhenRenewDisabled(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.nowatch", Name: "不长", Cron: "* * * * *", Enabled: true})
	lk := newFakeLocker()
	s := newTestSched(t, &fakeStore{}, lk, func(c *Config) { c.LockRenew = false })

	if _, err := s.Trigger(context.Background(), "t.nowatch", TriggerManual); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if _, _, renewed := lk.stat(); renewed != 0 {
		t.Fatalf("lock_renew=false 时不该起看门狗,却续了 %d 次", renewed)
	}
}

func TestT5_Watchdog_LockLost_CancelsTask(t *testing.T) {
	// 丢锁后必须**真的取消** ctx。只 return 的实现看起来没错,实际 handler
	// 拿不到任何信号会照跑到底 —— 两个副本真并发写同一批数据。
	// 这是本模块最锋利的一条边界。
	resetForUnit(t)
	var sawCancel atomic.Bool
	entered := make(chan struct{})
	Register(Job{Key: "t.lost", Name: "丢锁", Cron: "* * * * *", Enabled: true,
		Handler: func(ctx context.Context) error {
			close(entered)
			<-ctx.Done() // 只有被取消才会返回
			sawCancel.Store(true)
			return ctx.Err()
		}})
	lk := newFakeLocker()
	lk.renewOk = false // 看门狗第一次续期就发现锁没了
	s := newTestSched(t, &fakeStore{}, lk, func(c *Config) {
		c.LockTTL = 3 * time.Second
		c.LockRenew = true
	})

	done := make(chan struct{})
	var rec *model.SysJobRun
	go func() {
		defer close(done)
		r, _ := s.Trigger(context.Background(), "t.lost", TriggerSchedule)
		rec = r
	}()

	<-entered
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("丢锁后任务没被取消,handler 会一直跑 —— 并发写已经发生了")
	}
	if !sawCancel.Load() {
		t.Fatal("handler 没收到 ctx 取消")
	}
	if rec.Status != model.JobStatusFailed {
		t.Fatalf("丢锁后应记 failed,实际 %s", rec.Status)
	}
}

// ═══════════ T6 热更新(改频率立刻生效)═══════════

func TestT6_Reload_ReregistersCronEntry(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.reload", Name: "改频率", Cron: "0 3 * * *", Enabled: true})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())

	// 必须真的 cron.Start():robfig 是在 run() 里才给 entry 算 Next 的,
	// 没 Start 的话 NextRunAt 恒为零 —— 那正是 robfig 的既定行为,
	// 不是我们的 bug。用 0 3 * * * 保证测试期间不会真触发。
	if err := s.register("t.reload"); err != nil {
		t.Fatalf("register: %v", err)
	}
	s.started = true
	s.cron.Start()
	defer s.cron.Stop()
	before := s.NextRunAt("t.reload")
	if before.IsZero() {
		t.Fatal("注册后没有下一次触发时间")
	}

	// 改 cron 表达式
	if err := s.Reload("t.reload", Override{
		JobKey: "t.reload", CronExpr: "*/5 * * * *", Enabled: true, IsActive: true,
	}); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	after := s.NextRunAt("t.reload")
	if after.Equal(before) {
		t.Fatalf("Reload 后下一次触发时间没变(%v)—— 只写 DB 不重注册,"+
			"改频率要等重启才生效,而且没有报错", before)
	}
	if eff, _ := s.Get("t.reload"); !eff.CronOverridden || eff.CronExpr != "*/5 * * * *" {
		t.Fatalf("生效表达式没更新: %+v", eff)
	}
}

func TestT6_Reload_DisablingRemovesEntry(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.off2", Name: "停用", Cron: "0 3 * * *", Enabled: true})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())
	if err := s.register("t.off2"); err != nil {
		t.Fatalf("register: %v", err)
	}
	s.started = true
	s.cron.Start()
	defer s.cron.Stop()

	if err := s.Reload("t.off2", Override{JobKey: "t.off2", Enabled: false, IsActive: true}); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !s.NextRunAt("t.off2").IsZero() {
		t.Fatal("停用后 cron entry 没被移除 —— robfig 会继续按旧表达式触发," +
			"然后在 Trigger 里被 EnabledEffective 挡下并记 ErrJobDisabled," +
			"留下一堆没必要的「已停用」告警")
	}
}

func TestT6_Reload_InvalidCronRollsBack(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.bad", Name: "非法表达式", Cron: "0 3 * * *", Enabled: true})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())
	if err := s.register("t.bad"); err != nil {
		t.Fatalf("register: %v", err)
	}
	s.started = true
	s.cron.Start()
	defer s.cron.Stop()
	good := s.NextRunAt("t.bad")

	bad := Override{JobKey: "t.bad", CronExpr: "不是表达式", Enabled: true, IsActive: true}
	if err := s.Reload("t.bad", bad); err == nil {
		t.Fatal("非法表达式应返回错误")
	}
	// 回滚要求:不能留下「DB 说启用、实际没注册」的状态
	if s.NextRunAt("t.bad").IsZero() {
		t.Fatal("注册失败后没有回滚,任务从此静默消失直到重启")
	}
	if !s.NextRunAt("t.bad").Equal(good) {
		t.Fatalf("回滚后应按旧表达式重建: 期望 %v,实际 %v", good, s.NextRunAt("t.bad"))
	}
	// 覆盖值也必须退回旧的 —— 否则下次 Reload / 重启会再撞一次同一个错
	if eff, _ := s.Get("t.bad"); eff.CronExpr == "不是表达式" || eff.CronOverridden {
		t.Fatalf("失败的 Reload 把非法表达式留在了生效配置里: %+v", eff)
	}
}

func TestT6_Reload_UnknownKey(t *testing.T) {
	resetForUnit(t)
	s := newTestSched(t, &fakeStore{}, newFakeLocker())
	if err := s.Reload("ghost", Override{JobKey: "ghost"}); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("want ErrJobNotFound, got %v", err)
	}
}

// ═══════════ T7 表达式解析 / 预览 ═══════════

func TestT7_ParseCron(t *testing.T) {
	ok := []string{
		"0 3 * * *",   // 每天 03:00
		"*/5 * * * *", // 每 5 分钟
		"0 0 1 * *",   // 每月 1 号
		"0 9 * * 1-5", // 工作日 09:00
		"@daily",      // Descriptor
		"@every 1h",   // Descriptor
		"0 3 1 1 *",   // 每年元旦
	}
	for _, e := range ok {
		if _, err := ParseCron(e); err != nil {
			t.Errorf("ParseCron(%q) 应成功: %v", e, err)
		}
	}
	bad := []string{
		"",
		"   ",
		"不是表达式",
		"0 3 * *",     // 4 段
		"60 3 * * *",  // 分钟越界
		"0 25 * * *",  // 小时越界
		"0 0 32 * *",  // 日越界
		"*/0 * * * *", // 步长为 0
	}
	for _, e := range bad {
		if _, err := ParseCron(e); err == nil {
			t.Errorf("ParseCron(%q) 应失败,但通过了", e)
		}
	}
}

func TestT7_ParseCron_MatchesSchedulerParser(t *testing.T) {
	// 校验器与调度器必须用**同一个 Parser**。用两套会出现
	// 「保存时通过、实际没注册上」的静默失败。
	s := newTestSched(t, &fakeStore{}, newFakeLocker())
	for _, e := range []string{"0 3 * * *", "@daily", "*/10 * * * *"} {
		if _, err := s.cron.AddFunc(e, func() {}); err != nil {
			t.Errorf("调度器拒绝了 ParseCron 接受(%q)的表达式: %v", e, err)
		}
	}
}

func TestT7_NextRuns(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("本机缺 tzdata: %v", err)
	}
	runs, err := NextRuns("0 3 * * *", 3, loc)
	if err != nil {
		t.Fatalf("NextRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("要 3 个时间点,得到 %d", len(runs))
	}
	for i, r := range runs {
		if r.Hour() != 3 || r.Minute() != 0 {
			t.Fatalf("第 %d 个时间点不是 03:00: %s", i, r)
		}
		if r.Location() != loc {
			t.Fatalf("第 %d 个时间点时区不对: %s", i, r)
		}
		if i > 0 && !r.After(runs[i-1]) {
			t.Fatalf("时间点未递增: %s 然后 %s", runs[i-1], r)
		}
	}
}

func TestT7_NextRuns_Invalid(t *testing.T) {
	if _, err := NextRuns("不是表达式", 5, nil); err == nil {
		t.Fatal("非法表达式应报错")
	}
}

func TestT7_TimeZoneIsHonored(t *testing.T) {
	// 容器里 time.Local == UTC 时,`0 3 * * *` 会在北京时间 11:00 跑。
	// WithLocation 必须真的生效。
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("本机缺 tzdata: %v", err)
	}
	s := newTestSched(t, &fakeStore{}, newFakeLocker(), func(c *Config) { c.TimeZone = "Asia/Shanghai" })
	if s.loc.String() != "Asia/Shanghai" {
		t.Fatalf("时区没生效: %s", s.loc)
	}
	runs, err := NextRuns("0 3 * * *", 1, loc)
	if err != nil || len(runs) != 1 {
		t.Fatalf("NextRuns: %v", err)
	}
	if runs[0].Hour() != 3 {
		t.Fatalf("按上海时区应在 03:00 触发,实际 %s", runs[0])
	}
}

func TestT7_BadTimezoneFailsStartup(t *testing.T) {
	// 时区配错必须启动失败 —— 症状是"任务跑了但时间不对",极难从日志发现
	_, err := New(Config{Enabled: true, TimeZone: "Mars/Olympus"}, nil, nil)
	if err == nil {
		t.Fatal("非法时区应返回错误")
	}
	if !strings.Contains(err.Error(), "timezone") {
		t.Fatalf("错误信息应点明是时区问题: %v", err)
	}
}

// ═══════════ T8 注册表 / merge / reconcile ═══════════

func TestT8_Register_DuplicatePanics(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.dup", Cron: "* * * * *", Enabled: true})
	// 重复注册一定是复制粘贴错了 —— 让它启动就炸,
	// 而不是让后一个悄悄覆盖前一个(现场是"任务怎么没跑")
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("重复注册应 panic")
		} else if !strings.Contains(r.(string), "t.dup") {
			t.Fatalf("panic 信息应含 key: %v", r)
		}
	}()
	Register(Job{Key: "t.dup", Cron: "* * * * *", Enabled: true})
}

func TestT8_Merge_Priority(t *testing.T) {
	base := Job{Key: "k", Name: "代码名", Description: "代码说明", Cron: "0 3 * * *", Enabled: true}

	// 无 DB 行 → 全用代码默认值
	e := merge(base, nil)
	if e.CronExpr != "0 3 * * *" || e.Name != "代码名" || e.CronOverridden || !e.DBEnabled {
		t.Fatalf("无 DB 行时不该有任何覆盖: %+v", e)
	}

	// DB 行存在但字段留空 → 不覆盖(允许只改开关不改频率)
	e = merge(base, &Override{JobKey: "k", CronExpr: "", Name: "", Enabled: true, IsActive: true})
	if e.CronExpr != "0 3 * * *" || e.Name != "代码名" || e.CronOverridden {
		t.Fatalf("DB 空字段不该覆盖代码值: %+v", e)
	}

	// DB 有值 → 覆盖,并标记 CronOverridden(管理端要能显示"当前跑的到底是哪个")
	e = merge(base, &Override{JobKey: "k", CronExpr: "*/5 * * * *", Name: "运营改的名",
		Description: "运营写的说明", Enabled: true, IsActive: true})
	if e.CronExpr != "*/5 * * * *" || e.Name != "运营改的名" ||
		e.Description != "运营写的说明" || !e.CronOverridden {
		t.Fatalf("DB 覆盖没生效: %+v", e)
	}
}

func TestT8_Merge_CodeEnabledAndDBEnabled(t *testing.T) {
	// 做 AND:从代码里删掉/关掉任务,DB 残留行不能让它复活
	e := merge(Job{Key: "k", Cron: "* * * * *", Enabled: false},
		&Override{JobKey: "k", Enabled: true, IsActive: true})
	if e.EnabledEffective() {
		t.Fatal("code.Enabled=false 时 DB 残留行不该让它复活")
	}
}

func TestT8_Reconcile(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "live.a", Cron: "* * * * *", Enabled: true})
	Register(Job{Key: "live.b", Cron: "* * * * *", Enabled: true})

	existing := map[string]Override{
		"live.a":   {JobKey: "live.a"},
		"dead.one": {JobKey: "dead.one"},
		"dead.two": {JobKey: "dead.two"},
	}
	stale := reconcile(existing, func(key string) bool { _, ok := lookup(key); return ok })
	if len(stale) != 2 || stale[0] != "dead.one" || stale[1] != "dead.two" {
		t.Fatalf("stale 检出错误: %v", stale)
	}
}

func TestT8_List_StableAndSorted(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "z.job", Name: "Z", Cron: "* * * * *", Enabled: true})
	Register(Job{Key: "a.job", Name: "A", Cron: "* * * * *", Enabled: true})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())
	s.overrides["z.job"] = Override{JobKey: "z.job", Sort: 1, Enabled: true, IsActive: true}
	s.overrides["a.job"] = Override{JobKey: "a.job", Sort: 99, Enabled: true, IsActive: true}

	got := s.List()
	if len(got) != 2 {
		t.Fatalf("List 应返回 2 个任务,得到 %d", len(got))
	}
	// sort=1 的排前面
	if got[0].Key != "z.job" || got[1].Key != "a.job" {
		t.Fatalf("排序错误: %s, %s", got[0].Key, got[1].Key)
	}
	// 再调一次顺序不变(map 随机序会让管理端列表每刷新跳一下)
	again := s.List()
	if again[0].Key != got[0].Key || again[1].Key != got[1].Key {
		t.Fatal("List 顺序不稳定 —— 管理端列表会每次刷新重排")
	}
}

func TestT8_Runtime_DegradedReflectsLock(t *testing.T) {
	resetForUnit(t)
	lk := newFakeLocker()
	lk.avail = false
	s := newTestSched(t, &fakeStore{}, lk)
	rt := s.Runtime()
	if !rt.Degraded || rt.LockAvailable {
		t.Fatalf("锁不可用时 Runtime 应报 degraded: %+v", rt)
	}
	if rt.TimeZone != "Asia/Shanghai" {
		t.Fatalf("Runtime 应回报时区: %+v", rt)
	}
}

// ═══════════ T9 超时 / Start / Shutdown ═══════════

func TestT9_Timeout_CancelsHandler(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.timeout", Name: "超时", Cron: "* * * * *", Enabled: true,
		Timeout: 50 * time.Millisecond,
		Handler: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}})
	s := newTestSched(t, &fakeStore{}, newFakeLocker(), func(c *Config) {
		c.DefaultTimeout = 30 * time.Minute // 任务自己的 Timeout 应该优先
	})

	start := time.Now()
	rec, err := s.Trigger(context.Background(), "t.timeout", TriggerSchedule)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if rec.Status != model.JobStatusFailed {
		t.Fatalf("超时后应记 failed,实际 %s", rec.Status)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("任务 Timeout 没生效,跑了 %s", el)
	}
	if rec.DurationMs > 5000 {
		t.Fatalf("耗时记录异常: %dms", rec.DurationMs)
	}
}

func TestT9_DefaultTimeout_UsedWhenJobHasNone(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.dt", Name: "用默认", Cron: "* * * * *", Enabled: true,
		Handler: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }})
	s := newTestSched(t, &fakeStore{}, newFakeLocker(), func(c *Config) {
		c.DefaultTimeout = 50 * time.Millisecond
	})
	rec, _ := s.Trigger(context.Background(), "t.dt", TriggerSchedule)
	if rec.Status != model.JobStatusFailed {
		t.Fatalf("默认超时应生效,实际 %s", rec.Status)
	}
}

func TestT9_DisabledScheduler_RegistersNothing(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.globaloff", Cron: "* * * * *", Enabled: true})
	s := newTestSched(t, &fakeStore{}, newFakeLocker(), func(c *Config) { c.Enabled = false })
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !s.NextRunAt("t.globaloff").IsZero() {
		t.Fatal("job.enabled=false 时不该注册任何 entry")
	}
	// 但手动触发仍然可用 —— 运维需要能立刻跑一次验证
	if _, err := s.Trigger(context.Background(), "t.globaloff", TriggerManual); err != nil {
		t.Fatalf("job.enabled=false 时手动触发应仍可用: %v", err)
	}
}

func TestT9_Start_LoadOverridesFailureDegradesToDefaults(t *testing.T) {
	resetForUnit(t)
	calls := int32(0)
	Register(Job{Key: "t.loaddown", Name: "读不到覆盖", Cron: "* * * * *", Enabled: true,
		Handler: func(context.Context) error { atomic.AddInt32(&calls, 1); return nil }})
	st := &fakeStore{loadErr: errors.New("sys_job 读不了")}
	s := newTestSched(t, st, newFakeLocker())

	// 覆盖值读不到不该启动失败:退化成"全用代码默认值"继续跑
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start 应降级而非失败: %v", err)
	}
	if _, err := s.Trigger(context.Background(), "t.loaddown", TriggerSchedule); err != nil {
		t.Fatalf("降级后任务应仍可跑: %v", err)
	}
	if calls != 1 {
		t.Fatal("降级后任务没执行")
	}
}

func TestT9_Start_InvalidCronSkipsThatJobOnly(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.good", Cron: "0 3 * * *", Enabled: true})
	// 覆盖值必须经 fakeStore 注入:Start 会用 LoadOverrides 的结果**整体替换**
	// s.overrides,直接写 s.overrides 的话会被这一下冲掉(测出来 registered=1,
	// 看起来像"非法表达式也能注册上",其实是覆盖值根本没进去)。
	st := &fakeStore{overrides: map[string]Override{
		"t.good": {JobKey: "t.good", CronExpr: "不是表达式", Enabled: true, IsActive: true},
	}}
	s := newTestSched(t, st, newFakeLocker())

	// 一个任务配错不该拖垮全部任务
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if rt := s.Runtime(); rt.Registered != 0 {
		t.Fatalf("表达式非法的任务不该注册上,registered=%d", rt.Registered)
	}
}

func TestT9_Shutdown_DrainsInflight(t *testing.T) {
	resetForUnit(t)
	release := make(chan struct{})
	entered := make(chan struct{})
	Register(Job{Key: "t.drain", Cron: "0 3 * * *", Enabled: true,
		Handler: func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}})
	s := newTestSched(t, &fakeStore{}, newFakeLocker())
	s.started = true
	s.cron.Start()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Trigger(context.Background(), "t.drain", TriggerManual)
	}()
	<-entered

	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Shutdown(ctx)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown 没等到在跑任务结束就返回了")
	}
}

func TestT9_Shutdown_BudgetExhaustedCancelsInflight(t *testing.T) {
	resetForUnit(t)
	entered := make(chan struct{})
	var sawCancel atomic.Bool
	Register(Job{Key: "t.hardstop", Cron: "0 3 * * *", Enabled: true,
		Handler: func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			sawCancel.Store(true)
			return ctx.Err()
		}})
	s := newTestSched(t, &fakeStore{}, newFakeLocker(), func(c *Config) {
		c.ShutdownTimeout = 200 * time.Millisecond
	})
	s.started = true
	s.cron.Start()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Trigger(context.Background(), "t.hardstop", TriggerManual)
	}()
	<-entered

	// 传 nil:验证 job.shutdown_timeout 这个配置项自己说了算,
	// 而不是"只有 main.go 恰好传对了 ctx 才生效"
	begin := time.Now()
	s.Shutdown(nil)

	if el := time.Since(begin); el > 5*time.Second {
		t.Fatalf("Shutdown 卡了 %s,预算没用上", el)
	}
	<-done
	if !sawCancel.Load() {
		t.Fatal("预算耗尽后没取消在跑任务 —— 停机会被无限拖住")
	}
}

func TestT9_Shutdown_CancelsBothConcurrentRunsOfSameJob(t *testing.T) {
	// 降级模式下(锁不可用)同一个任务本来就会同时跑两份 —— 这是
	// "fail-open 但降级后仍剩 Layer 0"的必然结果,不是异常。
	//
	// inflight 如果按 jobKey 做键,第二份会把第一份的 cancel 覆盖掉,
	// 于是 Shutdown 的强制取消只杀到一份,另一份永远不收尾 —— 停机卡死。
	// 这条用例就是那个缺陷的守卫。
	resetForUnit(t)
	entered := make(chan struct{}, 2)
	var sawCancel atomic.Int32
	release := make(chan struct{})
	Register(Job{Key: "t.dupe", Name: "并发两份", Cron: "0 3 * * *", Enabled: true,
		Handler: func(ctx context.Context) error {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				sawCancel.Add(1)
				return ctx.Err()
			}
			return nil
		}})
	lk := newFakeLocker()
	lk.avail = false // 降级 → 不去重,允许并发
	s := newTestSched(t, &fakeStore{}, lk, func(c *Config) {
		c.ShutdownTimeout = 300 * time.Millisecond
	})

	go s.Trigger(context.Background(), "t.dupe", TriggerManual)
	<-entered
	go s.Trigger(context.Background(), "t.dupe", TriggerManual)
	<-entered

	if n := s.inflightCount(); n != 2 {
		t.Fatalf("inflight 应记着 2 份运行,实际 %d", n)
	}

	done := make(chan struct{})
	go func() { defer close(done); s.Shutdown(nil) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown 没结束 —— 有在跑任务没被取消到,停机会无限拖住")
	}
	if got := sawCancel.Load(); got != 2 {
		t.Fatalf("两份运行都该被取消,实际取消了 %d 份", got)
	}
	close(release)
}

func TestT9_Shutdown_BeforeStart_ReturnsImmediately(t *testing.T) {
	// main.go 里 Start 可能失败(已 Error 不 Fatal),Shutdown 必须仍能安全调用
	resetForUnit(t)
	s := newTestSched(t, &fakeStore{}, newFakeLocker())
	done := make(chan struct{})
	go func() { defer close(done); s.Shutdown(nil) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("未 Start 就 Shutdown 死锁了")
	}
}

func TestT9_Start_RecoverStaleRuns(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t.stale", Cron: "* * * * *", Enabled: true})

	before := make(chan time.Time, 1)
	st := &recordingStaleStore{fakeStore: fakeStore{}, ch: before}
	s := newTestSched(t, st, newFakeLocker(), func(c *Config) { c.DefaultTimeout = time.Hour })
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case got := <-before:
		// 阈值 = 最长任务 Timeout + 宽限,不能是固定值(会把合法的长任务
		// 误判成「进程死了」)。got 是 before 时间戳,它相对 now 应当 ≈ -want
		// (不是 +want —— 方向写反的话这个断言恒假,等于没测)。
		want := -(time.Hour + StaleGrace)
		d := got.Sub(time.Now())
		if d-want > time.Minute || want-d > time.Minute {
			t.Fatalf("回收阈值 %s,距今 %s,与预期(最长 Timeout + 宽限 = %s)不符",
				got, d, -want)
		}
	default:
		t.Fatal("Start 没调 RecoverStaleRuns —— 崩溃遗留的 running 行永远没人回收," +
			"Layer 0 会一直判成「正在执行中」,把任务永久挡死")
	}
}

// recordingStaleStore 记下 RecoverStaleRuns 收到的阈值
type recordingStaleStore struct {
	fakeStore
	ch chan time.Time
}

func (r *recordingStaleStore) RecoverStaleRuns(_ context.Context, before time.Time) (int64, error) {
	select {
	case r.ch <- before:
	default:
	}
	return 0, nil
}

// ═══════════ T7 OnFinish 事件回调(M73)═══════════
//
// 这里守的是 cronx ↔ 事件推送之间的接缝。四条性质,每一条都有对应的
// 真实事故形态:
//   1. 跑完就调(成功 / 失败都要)—— 不调 = 前端永远不刷新
//   2. 被跳过的**不**调 —— 调了 = 前端白 refetch 一次
//   3. panic 不许外泄 —— 外泄 = 手动触发时用户吃 500,而任务其实跑完了
//   4. ctx 不许是已取消的 —— 停机 / 超时场景下推送会立刻失败,状态永远推不出去

// finishSpy 收集 OnFinish 的调用
type finishSpy struct {
	mu    sync.Mutex
	calls []*model.SysJobRun
	ctxs  []context.Context
}

// ⚠️ 存**值拷贝**而不是指针 —— 和上面 fakeStore 同一个理由,这里踩过一次。
// 存指针的话:回调就算在 status 赋值**之前**被调用,也测不出来 —— 断言时那个
// rec 已经被 Trigger 改写成终态了。于是"推出去的是半截 running 记录"这个
// bug 会伪装成绿灯。存快照才是"回调那一刻看到的东西"。
func (f *finishSpy) fn(ctx context.Context, rec *model.SysJobRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *rec
	f.calls = append(f.calls, &cp)
	f.ctxs = append(f.ctxs, ctx)
}

func (f *finishSpy) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *finishSpy) at(i int) *model.SysJobRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[i]
}

func TestT9_OnFinish_CalledOnSuccess(t *testing.T) {
	resetForUnit(t)
	spy := &finishSpy{}
	Register(Job{Key: "t9.ok", Name: "成功", Cron: "* * * * *", Enabled: true, Handler: okHandler})
	s := newTestSched(t, &fakeStore{}, newFakeLocker(),
		func(c *Config) { c.OnFinish = spy.fn })

	if _, err := s.Trigger(context.Background(), "t9.ok", TriggerManual); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	if spy.count() != 1 {
		t.Fatalf("OnFinish 应被调 1 次,实际 %d —— 不调 = 前端永远不刷新", spy.count())
	}
	rec := spy.at(0)
	if rec.JobKey != "t9.ok" {
		t.Errorf("job_key 错位: %q", rec.JobKey)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status 应为 success,实际 %q", rec.Status)
	}
	// 推的是**终态**记录:前端拿到就能显示,不用再等一次 refetch
	if rec.FinishedAt == nil {
		t.Error("FinishedAt 为 nil —— 传出去的是半截记录")
	}
}

func TestT9_OnFinish_CalledOnFailure(t *testing.T) {
	resetForUnit(t)
	spy := &finishSpy{}
	Register(Job{Key: "t9.fail", Name: "失败", Cron: "* * * * *", Enabled: true,
		Handler: func(context.Context) error { return errors.New("boom") }})
	s := newTestSched(t, &fakeStore{}, newFakeLocker(),
		func(c *Config) { c.OnFinish = spy.fn })

	_, err := s.Trigger(context.Background(), "t9.fail", TriggerManual)
	if err != nil {
		t.Fatalf("Trigger 本身不该返错(失败要落在记录里): %v", err)
	}

	if spy.count() != 1 {
		t.Fatalf("失败也必须通知,实际调了 %d 次", spy.count())
	}
	// 这条最关键:失败**不**以 error 形式从 Trigger 返回,而是记在 rec.Status 上。
	// 装配层就是靠 Status 区分 job.finished / job.failed 的。
	if got := spy.at(0).Status; got != model.JobStatusFailed {
		t.Errorf("status 应为 failed,实际 %q —— 装配层会把它推成 job.finished", got)
	}
}

// 跳过的四种情况一律不通知:DB 里没有新状态,前端 refetch 拉回来的还是同一批
func TestT9_OnFinish_NotCalledWhenSkipped(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) (*Scheduler, string)
	}{
		{"未注册", func(t *testing.T) (*Scheduler, string) {
			return newTestSched(t, &fakeStore{}, newFakeLocker()), "t9.nope"
		}},
		{"代码停用", func(t *testing.T) (*Scheduler, string) {
			Register(Job{Key: "t9.off", Name: "停用", Cron: "* * * * *", Enabled: false})
			return newTestSched(t, &fakeStore{}, newFakeLocker()), "t9.off"
		}},
		{"Layer0 上一次还在跑", func(t *testing.T) (*Scheduler, string) {
			Register(Job{Key: "t9.l0", Name: "重叠", Cron: "* * * * *", Enabled: true, Handler: okHandler})
			return newTestSched(t, &fakeStore{running: true}, newFakeLocker()), "t9.l0"
		}},
		{"Layer1 锁被别人持有", func(t *testing.T) (*Scheduler, string) {
			Register(Job{Key: "t9.l1", Name: "跨副本", Cron: "* * * * *", Enabled: true, Handler: okHandler})
			lk := newFakeLocker()
			lk.tryOk = false // 别人拿着
			return newTestSched(t, &fakeStore{}, lk), "t9.l1"
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spy := &finishSpy{}
			s, key := c.setup(t)
			s.cfg.OnFinish = spy.fn // 直接改,绕开 newTestSched 的 mut

			if _, err := s.Trigger(context.Background(), key, TriggerManual); err == nil {
				t.Fatalf("该场景本应返 error,却成功了")
			}
			if spy.count() != 0 {
				t.Errorf("%s 被跳过的触发不该通知,实际调了 %d 次 —— 前端会白 refetch 一次", c.name, spy.count())
			}
		})
	}
}

// 降级跳过(非幂等任务)同样不通知
func TestT9_OnFinish_NotCalledWhenDegradedSkip(t *testing.T) {
	resetForUnit(t)
	spy := &finishSpy{}
	Register(Job{Key: "t9.ni", Name: "非幂等", Cron: "* * * * *", Enabled: true,
		NonIdempotent: true, Handler: okHandler})
	// nil locker → LockModeOff 路径,Available()=false → degraded
	s := newTestSched(t, &fakeStore{}, nil, func(c *Config) { c.OnFinish = spy.fn })

	if _, err := s.Trigger(context.Background(), "t9.ni", TriggerManual); !errors.Is(err, ErrDegraded) {
		t.Fatalf("want ErrDegraded, got %v", err)
	}
	if spy.count() != 0 {
		t.Errorf("降级跳过的触发不该通知,实际 %d 次", spy.count())
	}
}

// panic 不许外泄:手动触发是同步的,外泄 = 用户吃 500 而任务其实跑完了
func TestT9_OnFinish_PanicDoesNotEscape(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t9.panic", Name: "回调 panic", Cron: "* * * * *", Enabled: true, Handler: okHandler})
	s := newTestSched(t, &fakeStore{}, newFakeLocker(), func(c *Config) {
		c.OnFinish = func(context.Context, *model.SysJobRun) { panic("装配层炸了") }
	})

	rec, err := s.Trigger(context.Background(), "t9.panic", TriggerManual)
	if err != nil {
		t.Fatalf("回调 panic 绝不该让 Trigger 返错: %v", err)
	}
	if rec == nil || rec.Status != model.JobStatusSuccess {
		t.Fatalf("任务本身应当正常完成并落库,实际 rec=%+v", rec)
	}
}

// 停机 / 超时场景:原 ctx 已取消,OnFinish 拿到的必须还能用
//
// 这条最容易写反:如果直接把 Trigger 的 ctx 传下去,超时场景下
// push.Publish 会立刻拿到一个已取消的 ctx —— 而失败恰恰最常发生在
// 超时场景,于是"最该推的时候永远推不出去"。
func TestT9_OnFinish_ReceivesUsableContext(t *testing.T) {
	resetForUnit(t)
	spy := &finishSpy{}
	Register(Job{Key: "t9.ctx", Name: "带 ctx", Cron: "* * * * *", Enabled: true,
		Handler: func(ctx context.Context) error {
			<-ctx.Done() // 模拟"任务被超时取消"
			return ctx.Err()
		}})
	s := newTestSched(t, &fakeStore{}, newFakeLocker(),
		func(c *Config) { c.OnFinish = spy.fn })

	// 调用方传一个**已取消**的 ctx(等价于停机预算耗尽的场景)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Trigger(cancelled, "t9.ctx", TriggerManual); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if spy.count() != 1 {
		t.Fatalf("OnFinish 应被调 1 次,实际 %d", spy.count())
	}
	spy.mu.Lock()
	got := spy.ctxs[0]
	spy.mu.Unlock()
	if got == nil {
		t.Fatal("ctx 为 nil")
	}
	if got.Err() != nil {
		t.Errorf("OnFinish 收到已取消的 ctx(%v)—— 推送会立刻失败,状态永远推不出去", got.Err())
	}
	// 不许是同一个 ctx 对象:必须是 WithoutCancel 派生出来的新实例
	if got == cancelled {
		t.Error("OnFinish 收到的就是调用方那个 ctx,没做 WithoutCancel")
	}
}

// nil OnFinish 不许 panic(默认路径:所有没配推送的部署)
func TestT9_OnFinish_NilIsNoOp(t *testing.T) {
	resetForUnit(t)
	Register(Job{Key: "t9.nil", Name: "无回调", Cron: "* * * * *", Enabled: true, Handler: okHandler})
	s := newTestSched(t, &fakeStore{}, newFakeLocker()) // 不配 OnFinish

	if _, err := s.Trigger(context.Background(), "t9.nil", TriggerManual); err != nil {
		t.Fatalf("OnFinish 为 nil 时 Trigger 不该受影响: %v", err)
	}
}

// TestT10_Shutdown_NotCutTriggeredRun 并发 Trigger + Shutdown,被腰斩的窗口必须消除。
//
// 曾经 Trigger 的 runs.Add(1) 与 Shutdown 的 runs.Wait() 之间没有 happens-before:
// Wait 在计数为 0 时返回 ⇒ 停机"正常完成",而刚 Add 的任务还在跑 ⇒ 进程退出,
// 留下半截数据。现在关门(closing)+ 同锁 Add 保证:凡是通过关门检查的触发,
// Shutdown 必须等它跑完;被关门拒绝的触发收 ErrShuttingDown。
//
// 判据:每轮 Shutdown 返回后,started 必须全部落账为 finished —— handler 要么
// 已跑完、要么还没开始,不允许"还在跑"。handler 自己带 50ms 兜底超时,
// 保证极端时序(停机预算耗尽时还没 trackInflight)也能收敛,不会死等。
func TestT10_Shutdown_NotCutTriggeredRun(t *testing.T) {
	resetForUnit(t)
	var started, finished atomic.Int32
	const rounds = 50
	for i := 0; i < rounds; i++ {
		key := fmt.Sprintf("t10.shutdown.race.%d", i)
		release := make(chan struct{})
		Register(Job{
			Key:     key,
			Name:    key,
			Cron:    "0 0 1 * * *", // 测试期间不会到点,只走手动触发
			Enabled: true,
			Handler: func(ctx context.Context) error {
				started.Add(1)
				defer finished.Add(1)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(50 * time.Millisecond):
					return nil
				}
			},
		})
		s := newTestSched(t, &fakeStore{}, newFakeLocker(),
			func(c *Config) { c.ShutdownTimeout = 5 * time.Millisecond })

		trigRes := make(chan error, 1)
		go func() {
			_, err := s.Trigger(context.Background(), key, TriggerManual)
			trigRes <- err
		}()
		shutdownDone := make(chan struct{})
		go func() {
			s.Shutdown(context.Background())
			close(shutdownDone)
		}()
		select {
		case <-shutdownDone:
		case <-time.After(5 * time.Second):
			t.Fatalf("第 %d 轮:Shutdown 未收敛", i)
		}
		// Shutdown 返回 ⇒ 通过关门检查的触发必然已整跑完(handler 已结束)
		if got := started.Load() - finished.Load(); got > 0 {
			t.Fatalf("第 %d 轮:Shutdown 返回时任务被腰斩(在跑 %d 个)", i, got)
		}
		// 收尾:放行可能仍挂在 release 上的 handler(修复正确时不该走到),
		// 并等 Trigger 退出,避免泄漏到下一轮
		close(release)
		<-trigRes
	}
	if started.Load() == 0 {
		t.Fatal("50 轮里一次都没触发过,测试没测到东西")
	}
}

// ═══════════ 注册表重置 ═══════════

func TestMain(m *testing.M) {
	// 注册表是全局的,任何用例漏调 resetForUnit 都会污染后续用例。
	// 这里兜一道:每个用例结束后强制清空。
	code := m.Run()
	resetRegistry()
	os.Exit(code)
}
