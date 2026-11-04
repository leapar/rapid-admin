// JobService 测试 —— M68
//
// 这里刻意用**真的 cronx.Scheduler + 真的 gorm store(sqlite 内存)**,
// 而不是把 Scheduler 换成 mock。理由:本 service 最有价值的断言全在
// 「service 的写操作有没有真的落到 cron 的运行时状态上」——
// 改了 DB 但 robfig 里还是旧表达式(或者反过来),mock 一个都看不见。
//
// ⚠️ env 里**真的调了 Start()**,而且**任务必须在 Start 之前注册**:
//   - 覆盖值(LoadOverrides / reconcile)只在 Start 里做,不调就等于永远测
//     「没有 DB 覆盖行」这一条路径;
//   - cron entry 也是 Start 时注册的,晚注册的任务压根没有 entry,
//     NextRunAt 会是空的 —— 那测的是「没注册」,不是「注册了但没跑」。
//   代价是 robfig 的 ticker 真跑起来了,所以所有测试表达式都写成
//   `0 H 1 1 *`(每年 1 月 1 日),测试期间不可能自然触发。
//
// cronx 的全局注册表跨用例不清(ResetRegistry 是 pkg 内部函数),
// 且 Register 撞 key 会 panic —— 所以每个用例用带序号的 jobKey。

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/errcode"
)

// neverCron 永不自然触发的表达式(1 月 1 日 03:00)
const neverCron = "0 3 1 1 *"

// newJobTestEnv 造 sqlite + 真调度器,装进 cronx.Default()
//
// regs 在 Start 之前执行 —— 见文件头说明。每个用例独立 DB:
// UpdateOverride 会写 sys_job,共用会让"没有 DB 行"的前置被别的用例破坏。
func newJobTestEnv(t *testing.T, regs ...func(*testing.T)) (*jobService, *gorm.DB) {
	t.Helper()
	db := newJobDB(t)
	for _, r := range regs {
		r(t)
	}
	// 调度器装进 cronx.Default() 即可,service 每次调用时自己取
	startJobSched(t, db)
	return &jobService{db: db}, db
}

func newJobDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysJob{}, &model.SysJobRun{}))
	return db
}

func startJobSched(t *testing.T, db *gorm.DB) *cronx.Scheduler {
	t.Helper()
	locker, err := cronx.ResolveLocker(cronx.LockModeMemory, nil, "test:")
	require.NoError(t, err)
	s, err := cronx.New(cronx.Config{
		Enabled:         true,
		TimeZone:        "Asia/Shanghai",
		LockMode:        cronx.LockModeMemory,
		LockTTL:         30 * time.Second,
		LockRenew:       false, // 看门狗会起 goroutine,单测里没意义
		DefaultTimeout:  30 * time.Second,
		ShutdownTimeout: 2 * time.Second,
	}, cronx.NewStore(db), locker)
	require.NoError(t, err)
	require.NoError(t, s.Start(context.Background()))
	cronx.SetDefault(s)
	t.Cleanup(func() {
		cronx.ClearDefault()
		s.Shutdown(context.Background())
	})
	return s
}

// regJob 注册任务
func regJob(t *testing.T, key, cronExpr string, enabled bool, run func(context.Context) error) {
	t.Helper()
	if run == nil {
		run = func(context.Context) error { return nil }
	}
	cronx.Register(cronx.Job{
		Key: key, Name: "任务 " + key, Description: "测试用",
		Cron: cronExpr, Enabled: enabled, Timeout: 5 * time.Second,
		Handler: run,
	})
}

func pick(t *testing.T, list []JobItemView, key string) JobItemView {
	t.Helper()
	for _, v := range list {
		if v.JobKey == key {
			return v
		}
	}
	t.Fatalf("列表里找不到 %s", key)
	return JobItemView{}
}

func assertCode(t *testing.T, err error, want int) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, want, errcode.CodeOf(err), "错误码不符,实际错误: %v", err)
}

// ═══════════ 未装配 cronx ═══════════

// TestJ1_NoScheduler_ListIsEmptyNotNil
//
// cronx 没装配时返 [] 而不是 nil:nil 序列化成 JSON null,前端 map 上去直接抛。
func TestJ1_NoScheduler_ListIsEmptyNotNil(t *testing.T) {
	cronx.ClearDefault()
	s := NewJobService(nil)

	list, err := s.ListJobs(context.Background(), JobListQuery{})
	require.NoError(t, err)
	require.NotNil(t, list)
	assert.Empty(t, list)

	rt, err := s.Runtime(context.Background())
	require.NoError(t, err)
	assert.False(t, rt.Enabled)

	_, err = s.GetJob(context.Background(), "whatever")
	assertCode(t, err, errcode.NotFound)
}

// ═══════════ 列表 / 详情 ═══════════

func TestJ2_ListJobs_ExposesCodeDefaultAndEffective(t *testing.T) {
	s, _ := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j2.basic", neverCron, true, nil) })

	list, err := s.ListJobs(context.Background(), JobListQuery{})
	require.NoError(t, err)

	v := pick(t, list, "j2.basic")
	assert.Equal(t, neverCron, v.CronExpr)
	assert.Equal(t, neverCron, v.DefaultCronExpr)
	assert.False(t, v.CronOverridden, "没有 DB 覆盖行时不该是 overridden")
	assert.False(t, v.Overridden)
	assert.True(t, v.EnabledEffective)
	assert.True(t, v.Active)
	assert.Equal(t, int64(5000), v.TimeoutMs)

	// robfig 的 Start 只是 `go c.run()`,Next 是在那个 goroutine 里算的,
	// 所以严格说存在一个"刚 Start 完、Next 还是零值"的窗口。
	// 生产上页面是几秒后才打开的,看不见;但断言必须轮询,不能一上来就查。
	assert.Eventually(t, func() bool {
		l, err := s.ListJobs(context.Background(), JobListQuery{Keyword: "j2.basic"})
		return err == nil && len(l) == 1 && l[0].NextRunAt != ""
	}, 2*time.Second, 5*time.Millisecond, "NextRunAt 应在 Start 后很快被算出")

	l, err := s.ListJobs(context.Background(), JobListQuery{Keyword: "j2.basic"})
	require.NoError(t, err)
	assert.NotEqual(t, "1970-01-01T00:00:00Z", l[0].NextRunAt, "不能是零值时间")
}

func TestJ3_ListJobs_KeywordMatchesKeyAndName(t *testing.T) {
	s, _ := newJobTestEnv(t,
		func(t *testing.T) { regJob(t, "j3.alpha", neverCron, true, nil) },
		func(t *testing.T) { regJob(t, "j3.beta", neverCron, true, nil) },
	)

	// 大小写不敏感
	byKey, err := s.ListJobs(context.Background(), JobListQuery{Keyword: "ALPHA"})
	require.NoError(t, err)
	require.Len(t, byKey, 1)
	assert.Equal(t, "j3.alpha", byKey[0].JobKey)

	// 中文名也要能搜到(注册名叫 "任务 j3.beta")
	byName, err := s.ListJobs(context.Background(), JobListQuery{Keyword: "任务 j3.beta"})
	require.NoError(t, err)
	assert.Len(t, byName, 1)

	// 纯空白 keyword 当不过滤
	all, err := s.ListJobs(context.Background(), JobListQuery{Keyword: "   "})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)
}

func TestJ4_ListJobs_EnabledOnlyFiltersDisabled(t *testing.T) {
	s, _ := newJobTestEnv(t,
		func(t *testing.T) { regJob(t, "j4.on", neverCron, true, nil) },
		func(t *testing.T) { regJob(t, "j4.off", neverCron, false, nil) },
	)

	all, err := s.ListJobs(context.Background(), JobListQuery{})
	require.NoError(t, err)

	on := pick(t, all, "j4.on")
	assert.True(t, on.EnabledEffective)
	assert.True(t, on.EnabledCode)
	assert.True(t, on.EnabledDB)

	// 代码侧就关掉的任务:enabled_code=false,不是 DB 关的
	off := pick(t, all, "j4.off")
	assert.False(t, off.EnabledEffective)
	assert.False(t, off.EnabledCode, "是代码侧关的")
	assert.True(t, off.EnabledDB, "DB 侧没有覆盖行,默认开")
	assert.Empty(t, off.NextRunAt, "没注册进 robfig → 没有下次触发时间")

	only, err := s.ListJobs(context.Background(), JobListQuery{EnabledOnly: true})
	require.NoError(t, err)
	for _, v := range only {
		assert.True(t, v.EnabledEffective, "EnabledOnly 结果里混进了停用任务 %s", v.JobKey)
	}
}

// TestJ4b_EnabledOnly_FiltersDBDisabledToo
//
// 代码开着、DB 关掉的也要被 EnabledOnly 过滤掉 —— 这是前端"只看运行中"的主路径。
func TestJ4b_EnabledOnly_FiltersDBDisabledToo(t *testing.T) {
	s, _ := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j4b.a", neverCron, true, nil) })

	off := false
	_, err := s.UpdateOverride(context.Background(), nil, "j4b.a",
		JobOverridePatch{Enabled: &off}, false)
	require.NoError(t, err)

	list, err := s.ListJobs(context.Background(), JobListQuery{})
	require.NoError(t, err)
	v := pick(t, list, "j4b.a")
	assert.True(t, v.EnabledCode)
	assert.False(t, v.EnabledDB, "DB 侧关了")
	assert.False(t, v.EnabledEffective)

	only, err := s.ListJobs(context.Background(), JobListQuery{EnabledOnly: true})
	require.NoError(t, err)
	for _, it := range only {
		assert.NotEqual(t, "j4b.a", it.JobKey)
	}
}

func TestJ5_GetJob_UnknownKey(t *testing.T) {
	s, _ := newJobTestEnv(t)
	_, err := s.GetJob(context.Background(), "j5.nope")
	assertCode(t, err, errcode.NotFound)
}

// ═══════════ Cron 预览 ═══════════

func TestJ6_CronPreview(t *testing.T) {
	s, _ := newJobTestEnv(t)

	list, err := s.CronPreview(context.Background(), "0 3 * * *", 3)
	require.NoError(t, err)
	require.Len(t, list, 3)
	// 人类可读格式,不是 RFC3339 —— 这是给运营看的
	assert.Contains(t, list[0], "03:00:00")
	assert.NotContains(t, list[0], "T")

	_, err = s.CronPreview(context.Background(), "not a cron", 3)
	assertCode(t, err, errcode.BadRequest)
}

func TestJ7_CronPreview_ClampsN(t *testing.T) {
	s, _ := newJobTestEnv(t)
	// n=0 → 兜底 5,不是空数组(前端点了"预览"要有东西)
	list, err := s.CronPreview(context.Background(), "0 * * * *", 0)
	require.NoError(t, err)
	assert.Len(t, list, 5)

	// n=999 → 封顶 20,防手滑把接口打成全量计算
	list, err = s.CronPreview(context.Background(), "0 * * * *", 999)
	require.NoError(t, err)
	assert.Len(t, list, 20)
}

// ═══════════ UpdateOverride —— 唯一写通道 ═══════════

func TestJ8_UpdateOverride_PersistsAndHotReloads(t *testing.T) {
	s, db := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j8.a", neverCron, true, nil) })

	expr := "0 5 1 1 *"
	en := true
	v, err := s.UpdateOverride(context.Background(), &Caller{Username: "admin"},
		"j8.a", JobOverridePatch{CronExpr: &expr, Enabled: &en}, false)
	require.NoError(t, err)

	assert.Equal(t, expr, v.CronExpr)
	assert.True(t, v.CronOverridden)
	assert.True(t, v.Overridden)
	assert.Equal(t, neverCron, v.DefaultCronExpr, "代码默认值不该被改动")

	var row model.SysJob
	require.NoError(t, db.Where("job_key = ?", "j8.a").First(&row).Error)
	assert.Equal(t, expr, row.CronExpr)
	assert.True(t, row.IsActive)

	// 热更新的真正验收:robfig 里那个 entry 真的换掉了。
	// 只断言 DB 的话,"改了表达式但 robfig 还跑旧的"这种最贵的 bug 会漏过去。
	got, err := s.GetJob(context.Background(), "j8.a")
	require.NoError(t, err)
	assert.Equal(t, expr, got.CronExpr)
}

func TestJ9_UpdateOverride_UnknownJob(t *testing.T) {
	s, db := newJobTestEnv(t)
	_, err := s.UpdateOverride(context.Background(), nil, "j9.nope", JobOverridePatch{}, false)
	assertCode(t, err, errcode.NotFound)

	var n int64
	db.Model(&model.SysJob{}).Count(&n)
	assert.Zero(t, n, "404 不能留下任何行")
}

func TestJ10_UpdateOverride_RejectsBadCronAndLeavesNothingBehind(t *testing.T) {
	s, db := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j10.a", neverCron, true, nil) })

	bad := "99 99 99"
	_, err := s.UpdateOverride(context.Background(), nil, "j10.a", JobOverridePatch{CronExpr: &bad}, false)
	assertCode(t, err, errcode.BadRequest)

	// 关键:校验失败**不能留下 sys_job 行** —— 留着的话下次启动
	// LoadOverrides 会把一条永远注册不上的覆盖值灌进去
	var n int64
	db.Model(&model.SysJob{}).Count(&n)
	assert.Zero(t, n)

	v, err := s.GetJob(context.Background(), "j10.a")
	require.NoError(t, err)
	assert.Equal(t, neverCron, v.CronExpr, "非法输入不该改动生效值")
}

// TestJ11_UpdateOverride_EmptyPatchDoesNotPinCodeDefaults
//
// 最隐蔽的一个:基底必须取 DB 原始行,不能取 effective。
// 用 effective 当基底的话,只改排序也会把代码里的 cron / name 写进 DB,
// 于是这个任务从此变成 overridden,后续发版改代码默认值对它彻底无效。
func TestJ11_UpdateOverride_EmptyPatchDoesNotPinCodeDefaults(t *testing.T) {
	s, db := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j11.a", neverCron, true, nil) })

	sort := 7
	v, err := s.UpdateOverride(context.Background(), nil, "j11.a", JobOverridePatch{Sort: &sort}, false)
	require.NoError(t, err)

	var row model.SysJob
	require.NoError(t, db.Where("job_key = ?", "j11.a").First(&row).Error)
	assert.Empty(t, row.CronExpr, "只改排序不该把代码默认 cron 固化进 DB")
	assert.Empty(t, row.Name, "只改排序不该把代码默认名称固化进 DB")
	assert.Equal(t, 7, row.Sort)

	assert.False(t, v.CronOverridden)
	assert.Equal(t, neverCron, v.CronExpr)
}

func TestJ12_UpdateOverride_PartialPatchKeepsOtherFields(t *testing.T) {
	s, db := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j12.a", neverCron, true, nil) })

	e1 := "0 5 1 1 *"
	nm := "自定义名"
	_, err := s.UpdateOverride(context.Background(), nil, "j12.a",
		JobOverridePatch{CronExpr: &e1, Name: &nm}, false)
	require.NoError(t, err)

	// 第二次只改 enabled —— 前两个字段必须还在
	off := false
	_, err = s.UpdateOverride(context.Background(), nil, "j12.a",
		JobOverridePatch{Enabled: &off}, false)
	require.NoError(t, err)

	var row model.SysJob
	require.NoError(t, db.Where("job_key = ?", "j12.a").First(&row).Error)
	assert.Equal(t, "0 5 1 1 *", row.CronExpr, "上次设的 cron 被冲掉了")
	assert.Equal(t, "自定义名", row.Name)
	assert.False(t, row.Enabled)
}

func TestJ13_UpdateOverride_ResetToDefault(t *testing.T) {
	s, db := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j13.a", neverCron, true, nil) })

	e1 := "0 5 1 1 *"
	nm := "改过的名"
	_, err := s.UpdateOverride(context.Background(), nil, "j13.a",
		JobOverridePatch{CronExpr: &e1, Name: &nm}, false)
	require.NoError(t, err)

	// 恢复默认:清 cron + 名称 + 备注,但**不动 enabled**
	off := false
	v, err := s.UpdateOverride(context.Background(), nil, "j13.a",
		JobOverridePatch{Enabled: &off}, true)
	require.NoError(t, err)

	var row model.SysJob
	require.NoError(t, db.Where("job_key = ?", "j13.a").First(&row).Error)
	assert.Empty(t, row.CronExpr)
	assert.Empty(t, row.Name)
	assert.False(t, row.Enabled, "恢复默认不该顺手把任务启用了")

	assert.Equal(t, neverCron, v.CronExpr)
	assert.False(t, v.CronOverridden)
}

// TestJ14_ResidualRow_IsVisibleButNotEditable
//
// reconcile 把「代码里已删掉」的残留行置成 is_active=false。
// 视图要能把它标出来(active=false),而且**不允许经后台改回来** ——
// 放行等于凭空造出一个没有代码的任务,重启即消失。
func TestJ14_ResidualRow_IsVisibleButNotEditable(t *testing.T) {
	db := newJobDB(t)
	// ⚠️ 必须用 map 形式造这行:struct + `default:true` tag 会把 IsActive:false
	// 悄悄翻成 true(见 saveOverride 的注释)。这里要是用 struct,
	// 测的就不是"残留行"而是一个正常行,断言全绿但什么都没证明。
	require.NoError(t, db.Model(&model.SysJob{}).Create(map[string]any{
		"id": 1, "created_at": time.Now(), "updated_at": time.Now(),
		"job_key": "j14.a", "name": "", "description": "",
		"cron_expr": "0 6 1 1 *", "enabled": true, "sort": 0, "is_active": false,
	}).Error)
	// 代码里仍然注册着(模拟"代码还在,但 DB 行被 reconcile 标死"这种组合)
	regJob(t, "j14.a", neverCron, true, nil)
	startJobSched(t, db)

	s := &jobService{db: db}
	v, err := s.GetJob(context.Background(), "j14.a")
	require.NoError(t, err)
	assert.False(t, v.Active, "残留行 active=false 必须体现在视图里")
	assert.False(t, v.EnabledDB, "残留行在 merge 里算成 DBEnabled=false")
	assert.True(t, v.EnabledCode, "代码里在,所以 code 侧是开的")
	assert.True(t, v.Overridden, "DB 里确实有行")

	nm := "复活"
	_, err = s.UpdateOverride(context.Background(), nil, "j14.a",
		JobOverridePatch{Name: &nm}, false)
	assertCode(t, err, errcode.BadRequest)
}

// ═══════════ 手动触发 ═══════════

func TestJ15_TriggerNow_RunsAndReturnsRecord(t *testing.T) {
	ran := 0
	s, db := newJobTestEnv(t, func(t *testing.T) {
		regJob(t, "j15.a", neverCron, true, func(context.Context) error {
			ran++
			return nil
		})
	})

	v, err := s.TriggerNow(context.Background(), &Caller{Username: "张三"}, "j15.a")
	require.NoError(t, err)
	assert.Equal(t, 1, ran)
	assert.Equal(t, "manual", v.TriggerType)
	assert.Equal(t, "success", v.Status)
	assert.NotZero(t, v.StartedAt)
	assert.NotZero(t, v.FinishedAt)
	assert.Equal(t, "张三", v.Operator, "手动触发必须记下操作人")
	assert.NotEmpty(t, v.InstanceID)

	var n int64
	db.Model(&model.SysJobRun{}).Count(&n)
	assert.Equal(t, int64(1), n)
}

func TestJ15b_TriggerNow_NilCallerDoesNotPanic(t *testing.T) {
	ran := 0
	s, _ := newJobTestEnv(t, func(t *testing.T) {
		regJob(t, "j15b.a", neverCron, true, func(context.Context) error {
			ran++
			return nil
		})
	})
	v, err := s.TriggerNow(context.Background(), nil, "j15b.a")
	require.NoError(t, err)
	assert.Equal(t, 1, ran)
	assert.Empty(t, v.Operator)
}

// TestJ16_TriggerNow_HandlerErrorIsRecordNotError
//
// 契约:任务跑了但失败 → nil error + status="failed"。
// 这样 duration / trace_id / instance_id 都还在,前端必须自己判 status。
func TestJ16_TriggerNow_HandlerErrorIsRecordNotError(t *testing.T) {
	s, _ := newJobTestEnv(t, func(t *testing.T) {
		regJob(t, "j16.a", neverCron, true, func(context.Context) error {
			return errors.New("boom")
		})
	})

	v, err := s.TriggerNow(context.Background(), nil, "j16.a")
	require.NoError(t, err, "任务自身失败不返 error —— 记录里已经带上了原因")
	assert.Equal(t, "failed", v.Status, "前端靠这个字段判失败")
	assert.Contains(t, v.Error, "boom")
	assert.NotZero(t, v.FinishedAt)
}

func TestJ17_TriggerNow_ConcurrentSecondCallGetsConflict(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	s, _ := newJobTestEnv(t, func(t *testing.T) {
		regJob(t, "j17.a", neverCron, true, func(context.Context) error {
			entered <- struct{}{}
			<-release
			return nil
		})
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.TriggerNow(context.Background(), nil, "j17.a")
	}()

	<-entered // 第一轮已在跑
	_, err := s.TriggerNow(context.Background(), nil, "j17.a")
	close(release)
	<-done

	assertCode(t, err, errcode.Conflict)
}

func TestJ18_TriggerNow_DisabledJob(t *testing.T) {
	s, _ := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j18.a", neverCron, false, nil) })
	_, err := s.TriggerNow(context.Background(), nil, "j18.a")
	assertCode(t, err, errcode.Forbidden)
}

func TestJ19_TriggerNow_UnknownJob(t *testing.T) {
	s, _ := newJobTestEnv(t)
	_, err := s.TriggerNow(context.Background(), nil, "j19.nope")
	assertCode(t, err, errcode.NotFound)
}

// ═══════════ 执行记录 ═══════════

func TestJ20_ListJobRuns_FiltersAndPaginates(t *testing.T) {
	s, _ := newJobTestEnv(t,
		func(t *testing.T) { regJob(t, "j20.a", neverCron, true, nil) },
		func(t *testing.T) { regJob(t, "j20.b", neverCron, true, nil) },
	)

	_, err := s.TriggerNow(context.Background(), &Caller{Username: "u1"}, "j20.a")
	require.NoError(t, err)
	_, err = s.TriggerNow(context.Background(), &Caller{Username: "u2"}, "j20.b")
	require.NoError(t, err)

	all, err := s.ListJobRuns(context.Background(), JobRunListQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), all.Total)
	assert.Equal(t, 1, all.Page, "分页归一后必须回填 Page/Size")
	assert.Equal(t, 20, all.Size)

	byKey, err := s.ListJobRuns(context.Background(), JobRunListQuery{JobKey: "j20.a"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), byKey.Total)

	byStatus, err := s.ListJobRuns(context.Background(), JobRunListQuery{Status: "success"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), byStatus.Total)

	byMiss, err := s.ListJobRuns(context.Background(), JobRunListQuery{Status: "running"})
	require.NoError(t, err)
	assert.Zero(t, byMiss.Total)

	byTrigger, err := s.ListJobRuns(context.Background(), JobRunListQuery{TriggerType: "schedule"})
	require.NoError(t, err)
	assert.Zero(t, byTrigger.Total, "两条都是手动触发")

	// 时间窗:起点在更早 → 两条都落在窗内
	cut := time.Now().Add(-time.Hour).Unix()
	inWindow, err := s.ListJobRuns(context.Background(), JobRunListQuery{StartAt: &cut})
	require.NoError(t, err)
	assert.Equal(t, int64(2), inWindow.Total)

	// 起点在未来 → 一条都不落
	future := time.Now().Add(time.Hour).Unix()
	none, err := s.ListJobRuns(context.Background(), JobRunListQuery{StartAt: &future})
	require.NoError(t, err)
	assert.Zero(t, none.Total)

	// 终点在过去 → 一条都不落
	past := time.Now().Add(-time.Hour).Unix()
	none2, err := s.ListJobRuns(context.Background(), JobRunListQuery{EndAt: &past})
	require.NoError(t, err)
	assert.Zero(t, none2.Total)

	deg, err := s.ListJobRuns(context.Background(), JobRunListQuery{DegradedOnly: true})
	require.NoError(t, err)
	assert.Zero(t, deg.Total)
}

func TestJ21_ListJobRuns_NoDB(t *testing.T) {
	s := NewJobService(nil)
	res, err := s.ListJobRuns(context.Background(), JobRunListQuery{})
	require.NoError(t, err)
	require.NotNil(t, res.List, "List 必须是 [] 而不是 nil")
	assert.Zero(t, res.Total)
}

func TestJ22_ListJobRuns_SizeCapped(t *testing.T) {
	s, _ := newJobTestEnv(t)
	res, err := s.ListJobRuns(context.Background(),
		JobRunListQuery{PageRequest: model.PageRequest{Page: 1, Size: 9999}})
	require.NoError(t, err)
	assert.Equal(t, 100, res.Size)
}

func TestJ23_Runtime_ReportsLockAndTimezone(t *testing.T) {
	s, _ := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j23.a", neverCron, true, nil) })

	rt, err := s.Runtime(context.Background())
	require.NoError(t, err)
	assert.True(t, rt.Enabled)
	assert.Equal(t, "Asia/Shanghai", rt.TimeZone)
	assert.True(t, rt.LockAvailable)
	assert.False(t, rt.Degraded, "内存锁是可用的")
	assert.NotEmpty(t, rt.InstanceID)
}

// ═══════════ 重启后行为必须一致 ═══════════
//
// 下面两条守的是本模块最贵的一类 bug:「当次改对了,重启后又变回去」。
// 共同成因都是"写进去的 DB 行 ≠ 内存里 Reload 的那份 ov"。

// TestJ24_DisableFreshJob_SurvivesRestart
//
// SysJob.Enabled 带 `default:true` tag,而 GORM 对带 default 的零值字段
// 会**替换成默认值**而不是写你给的值 —— Create(&SysJob{Enabled:false}) 落库
// 是 true(实测过,Select 显式列也救不了)。
// 于是:停用一个从未配置过的任务 → 内存里是关的、当次也确实没跑,
// 但重启后 LoadOverrides 读回 true,任务自己复活。
func TestJ24_DisableFreshJob_SurvivesRestart(t *testing.T) {
	s, db := newJobTestEnv(t, func(t *testing.T) { regJob(t, "j24.a", neverCron, true, nil) })

	// 第一次配置就走 Create 分支(没有已存在的行)
	off := false
	v, err := s.UpdateOverride(context.Background(), nil, "j24.a",
		JobOverridePatch{Enabled: &off}, false)
	require.NoError(t, err)
	assert.False(t, v.EnabledEffective, "当次就该关掉")

	// DB 真相
	var row model.SysJob
	require.NoError(t, db.Where("job_key = ?", "j24.a").First(&row).Error)
	assert.False(t, row.Enabled, "DB 里必须是 false —— gorm default:true 会把它翻成 true")

	// 模拟重启:同一个 DB 上重新 New + Start,只靠 LoadOverrides 恢复状态
	v2, err := reloadOnly(t, db, "j24.a")
	require.NoError(t, err)
	assert.False(t, v2.EnabledEffective, "重启后任务不能自己复活")
	assert.False(t, v2.EnabledDB)
}

// TestJ25_DescriptionAndSort_SurviveRestart
//
// LoadOverrides 曾经漏读 Description,后果是运营写的备注重启后静默消失
// (merge 判 ov.Description != "" 不成立 → 退回代码默认值)。
func TestJ25_DescriptionAndSort_SurviveRestart(t *testing.T) {
	db := newJobDB(t)
	regJob(t, "j25.a", neverCron, true, nil)
	startJobSched(t, db)
	s := &jobService{db: db}

	desc := "每天凌晨清理"
	nm := "历史清理"
	sort := 3
	_, err := s.UpdateOverride(context.Background(), nil, "j25.a",
		JobOverridePatch{Description: &desc, Name: &nm, Sort: &sort}, false)
	require.NoError(t, err)

	v, err := reloadOnly(t, db, "j25.a")
	require.NoError(t, err)
	assert.Equal(t, desc, v.Description, "备注重启后不能消失")
	assert.Equal(t, nm, v.Name)
	assert.Equal(t, 3, v.Sort)
}

// reloadOnly 在既有 DB 上重新起一个调度器(等价于进程重启),返回该任务的视图
func reloadOnly(t *testing.T, db *gorm.DB, key string) (JobItemView, error) {
	t.Helper()
	prev := cronx.Default()
	cronx.ClearDefault()
	startJobSched(t, db)
	s := &jobService{db: db}
	v, err := s.GetJob(context.Background(), key)
	cronx.ClearDefault()
	if prev != nil {
		cronx.SetDefault(prev)
	}
	if err != nil {
		return JobItemView{}, err
	}
	return *v, nil
}
