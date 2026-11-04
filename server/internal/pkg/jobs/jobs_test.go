// 内置任务测试 —— M70
//
// 两个任务都**不碰 cronx 全局注册表**:
//   - purgeHistory / kmsRotateCheck 是导出的,直接调
//   - RegisterAll 只在 main 里调一次(Register 撞 key 会 panic),
//     所以这里用不上它 —— 代价是「注册的定义本身」没被测到,
//     只能靠下面 TestRegistered_KeysAreStable 间接锁住 key / cron 表达式。
//
// purge 用**真的 gorm + 真 cronx.Store(sqlite 内存)**,不 mock:
// 清理逻辑里最容易错的是 SQL 边界(严格小于 cutoff、`id NOT IN (子查询)` 的
// 排序),mock 出来的 fake 数字永远是绿的。
package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/crypto"
)

// ─────────── 测试脚手架 ───────────

func newPurgeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysJobRun{}))
	return db
}

// seedRuns 造 n 条执行记录,started_at 从 now 往前每条递减 step,
// 于是 seedRuns(t, db, idBase, 3, time.Hour, now) 得到的是 0h / 1h / 2h 前各一条。
//
// ⚠️ idBase 必须由调用方串起来传(第一次调 1000,第二次调 1000+第一次的 n):
// BaseModel.ID 是 autoIncrement:false,每次都从 1000 起会直接 UNIQUE constraint failed。
func seedRuns(t *testing.T, db *gorm.DB, idBase uint64, n int, step time.Duration, now time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		started := now.Add(-time.Duration(i) * step)
		r := &model.SysJobRun{
			// ID 是从内嵌的 BaseModel 提升上来的字段,复合字面量必须走 BaseModel:
			// 直接写 ID: 会报 unknown field
			BaseModel:   model.BaseModel{ID: idBase + uint64(i)},
			JobKey:      "framework.probe",
			TriggerType: model.JobTriggerSchedule,
			Status:      model.JobStatusSuccess,
			StartedAt:   started,
		}
		require.NoError(t, db.Create(r).Error)
	}
}

// countRuns 数**可见**行数(软删作用域)
//
// ⚠️ 这里刻意不用 Unscoped。本文件测的是 purgeHistory 这个**包装层**的参数校验 /
// ctx 处理 / 幂等性,不是底层 SQL;「行有没有被物理删掉」由 cronx 的
// store_test.go 用 physCount(Unscoped) 单独锁住 ——
// 那个 bug(软删冒充物理删)骗得过本文件所有断言,但骗不过那里。
func countRuns(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&model.SysJobRun{}).Count(&n).Error)
	return n
}

// remainingAges 返回所有存活记录的 started_at(Unix 秒),按时间升序。
//
// ⚠️ 返回 int64 而不是 time.Time:读回来的时间戳带的是驱动自己的 Location,
// 和构造时用的 time.Local 不是同一个指针,assert.Equal 比 time.Time 结构体
// 会因为 loc 指针不同而失败 —— 明明时刻完全相同。Unix 秒绕开这个坑,
// 且本文件的用例间隔都在分钟级以上,秒精度够用。
func remainingAges(t *testing.T, db *gorm.DB) []int64 {
	t.Helper()
	var rows []model.SysJobRun
	require.NoError(t, db.Order("started_at ASC").Find(&rows).Error)
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.StartedAt.Unix())
	}
	return out
}

// ─────────── purge ───────────

// P1 days 规则:超过保留天数的删,未超的留
func TestP01_Purge_ByDays(t *testing.T) {
	db := newPurgeDB(t)
	now := time.Now()
	// n=1 时 step 不参与计算(started = base + 0),只有基准时刻有意义
	seedRuns(t, db, 1000, 1, 0, now.Add(-3*24*time.Hour)) // 3 天前 → 该删
	seedRuns(t, db, 1001, 1, 0, now.Add(-24*time.Hour))   // 1 天前 → 该留

	require.NoError(t, purgeHistory(context.Background(), cronx.NewStore(db), 2, 0))
	assert.Equal(t, int64(1), countRuns(t, db), "3 天前那条应被删,1 天前那条应保留")
	assert.Equal(t, []int64{now.Add(-24 * time.Hour).Unix()},
		remainingAges(t, db), "活下来的应该是较新那条")
}

// P2 max 规则:全部都是新的,但超出条数上限 → 删最旧的
func TestP02_Purge_ByMax(t *testing.T) {
	db := newPurgeDB(t)
	now := time.Now()
	seedRuns(t, db, 1000, 10, time.Minute, now) // 0..9 分钟前

	require.NoError(t, purgeHistory(context.Background(), cronx.NewStore(db), 0, 4))
	assert.Equal(t, int64(4), countRuns(t, db), "10 条留最新 4 条")
	// 留的是 0/1/2/3 分钟前那四条(最新的),按时间升序排列
	assert.Equal(t, []int64{
		now.Add(-3 * time.Minute).Unix(), now.Add(-2 * time.Minute).Unix(),
		now.Add(-time.Minute).Unix(), now.Unix(),
	}, remainingAges(t, db))
}

// P3 双保险:days 删掉超期的,max 再删掉超量的
func TestP03_Purge_BothRules(t *testing.T) {
	db := newPurgeDB(t)
	now := time.Now()
	seedRuns(t, db, 1000, 5, time.Hour, now.Add(-100*24*time.Hour)) // 100~104 小时前,days 全删
	seedRuns(t, db, 1005, 5, time.Minute, now)                      // 0~4 分钟前,全部保留期内

	require.NoError(t, purgeHistory(context.Background(), cronx.NewStore(db), 30, 3))
	// days 规则删 5 条 → 剩 5 条;max=3 再删 2 条 → 剩 3 条
	assert.Equal(t, int64(3), countRuns(t, db))
	assert.Len(t, remainingAges(t, db), 3)
}

// P4 days=0 只按 max,days<0 也视作"跳过天数规则"而不是"删光"
func TestP04_Purge_NonPositiveDaysSkipsDayRule(t *testing.T) {
	for _, days := range []int{0, -1} {
		db := newPurgeDB(t)
		now := time.Now()
		seedRuns(t, db, 1000, 5, time.Minute, now)

		require.NoError(t, purgeHistory(context.Background(), cronx.NewStore(db), days, 3))
		assert.Equal(t, int64(3), countRuns(t, db), "days=%d 时不应触发删光", days)
	}
}

// P5 两个参数都 <= 0 → 报错,而且**什么都没删**
// 这条锁的是"配置写错 → 任务失败",不是"静默什么都不做"
func TestP05_Purge_BothZero_IsError(t *testing.T) {
	db := newPurgeDB(t)
	seedRuns(t, db, 1000, 5, time.Minute, time.Now())

	err := purgeHistory(context.Background(), cronx.NewStore(db), 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "history_retention_days")
	assert.Equal(t, int64(5), countRuns(t, db), "报错时不应删除任何记录")
}

// P6 幂等:第二次跑没有可删的,返回成功
func TestP06_Purge_Idempotent(t *testing.T) {
	db := newPurgeDB(t)
	now := time.Now()
	seedRuns(t, db, 1000, 6, 3*24*time.Hour, now)

	st := cronx.NewStore(db)
	require.NoError(t, purgeHistory(context.Background(), st, 1, 0))
	after := countRuns(t, db)
	require.NoError(t, purgeHistory(context.Background(), st, 1, 0), "第二次不该报错")
	assert.Equal(t, after, countRuns(t, db), "第二次不应再删任何东西")
}

// P7 ctx 取消 → 错误要带上 ctx 状态,而不是伪装成 DB 失败
func TestP07_Purge_RespectsContext(t *testing.T) {
	db := newPurgeDB(t)
	seedRuns(t, db, 1000, 5, 24*time.Hour, time.Now().Add(-10*24*time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := purgeHistory(ctx, cronx.NewStore(db), 1, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// P8 空表不报错
func TestP08_Purge_EmptyTable(t *testing.T) {
	db := newPurgeDB(t)
	assert.NoError(t, purgeHistory(context.Background(), cronx.NewStore(db), 30, 50000))
}

// ─────────── kms rotate check ───────────

// key 工厂:32 字节,内容随 seed 变化(不同 seed → 不同 key)
func mkKey(seed byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = seed + byte(i)
	}
	return k
}

// initCrypto 重建 key registry。tier 用指针传,只覆盖非 nil 的,
// 这样"只配 PII"这种场景能表达出来。
func initCrypto(t *testing.T, pii, secret, normal []crypto.KeyEntry, legacy *crypto.KeyEntry) {
	t.Helper()
	require.NoError(t, crypto.InitKeys(crypto.KeysConfig{
		PII: pii, Secret: secret, Normal: normal, Legacy: legacy,
	}))
	// InitKeys 里的 mustBuildTier 失败是 panic,不是 error。
	// 这里恢复 recover,让"配置非法"在测试里表现为断言失败而不是整个包崩掉
	t.Cleanup(func() {
		// 清空 registry,避免用例之间互相污染
		_ = crypto.InitKeys(crypto.KeysConfig{})
	})
}

// swapProbe 临时替换探测函数。生产路径恒等于 crypto.RotationState。
func swapProbe(t *testing.T,
	fn func(crypto.Tier) (crypto.KeyRotationState, error),
	legacy func() bool) {
	origState, origLegacy := rotationStateFn, hasLegacyKeyFn
	rotationStateFn, hasLegacyKeyFn = fn, legacy
	t.Cleanup(func() { rotationStateFn, hasLegacyKeyFn = origState, origLegacy })
}

// K1 一个 tier 都没配 → nil(不是错误)。这是没启 KMS 的部署
func TestK01_RotateCheck_NoTierConfigured(t *testing.T) {
	initCrypto(t, nil, nil, nil, nil)
	assert.NoError(t, kmsRotateCheck(context.Background()))
}

// K2 三个 tier 各一把 active key → nil,且每条都往返通
func TestK02_RotateCheck_SingleKeyPerTier(t *testing.T) {
	initCrypto(t,
		[]crypto.KeyEntry{{KeyID: "pii-1", KeyVersion: 1, Key: mkKey(1), Active: true}},
		[]crypto.KeyEntry{{KeyID: "sec-1", KeyVersion: 1, Key: mkKey(2), Active: true}},
		[]crypto.KeyEntry{{KeyID: "nrm-1", KeyVersion: 1, Key: mkKey(3), Active: true}},
		nil)

	assert.NoError(t, kmsRotateCheck(context.Background()))

	st, err := crypto.RotationState(crypto.TierPII)
	require.NoError(t, err)
	assert.True(t, st.RoundTripOK)
	assert.True(t, st.ActiveIsHighest)
	assert.Empty(t, st.RetainedVersions)
	assert.Equal(t, "pii-1", st.ActiveKeyID)
	assert.Equal(t, 1, st.ActiveVersion)
}

// K3 轮转完成:v2 active + v1 保留 → nil,RetainedVersions=[1]
func TestK03_RotateCheck_CompletedRotation(t *testing.T) {
	initCrypto(t,
		[]crypto.KeyEntry{
			{KeyID: "pii-1", KeyVersion: 1, Key: mkKey(1), Active: false},
			{KeyID: "pii-2", KeyVersion: 2, Key: mkKey(2), Active: true},
		}, nil, nil, nil)

	assert.NoError(t, kmsRotateCheck(context.Background()))

	st, err := crypto.RotationState(crypto.TierPII)
	require.NoError(t, err)
	assert.True(t, st.ActiveIsHighest, "active v2 应是最高版本")
	assert.Equal(t, []byte{1}, st.RetainedVersions)
	// MaxRetained 是**保留版本里的最大号**,不含 active 自己 —— 所以是 1 不是 2。
	// (ActiveIsHighest 拿它和 ActiveVersion 比,所以这个字段从设计上就排除 active)
	assert.Equal(t, 1, st.MaxRetained)
}

// K4 加了新 key 但没切 Active:v1 active + v2 保留 → **仍然 nil**
// (告警不失败 —— "先灌新 key 观察几天再切"可能是故意的),但状态要说对
func TestK04_RotateCheck_ActiveNotHighest(t *testing.T) {
	initCrypto(t,
		[]crypto.KeyEntry{
			{KeyID: "pii-1", KeyVersion: 1, Key: mkKey(1), Active: true},
			{KeyID: "pii-2", KeyVersion: 2, Key: mkKey(2), Active: false},
		}, nil, nil, nil)

	assert.NoError(t, kmsRotateCheck(context.Background()), "active 不是最高版本只告警不失败")

	st, err := crypto.RotationState(crypto.TierPII)
	require.NoError(t, err)
	assert.False(t, st.ActiveIsHighest, "必须能观测到这个状态,否则告警无从谈起")
}

// K5 保留版本堆积(3 把历史)→ nil(告警),状态里能数出来
func TestK05_RotateCheck_RetainedPileup(t *testing.T) {
	initCrypto(t,
		[]crypto.KeyEntry{
			{KeyID: "pii-1", KeyVersion: 1, Key: mkKey(1)},
			{KeyID: "pii-2", KeyVersion: 2, Key: mkKey(2)},
			{KeyID: "pii-3", KeyVersion: 3, Key: mkKey(3)},
			{KeyID: "pii-4", KeyVersion: 4, Key: mkKey(4), Active: true},
		}, nil, nil, nil)

	assert.NoError(t, kmsRotateCheck(context.Background()), "历史堆积只告警不失败")

	st, err := crypto.RotationState(crypto.TierPII)
	require.NoError(t, err)
	assert.Equal(t, []byte{1, 2, 3}, st.RetainedVersions)
	assert.Len(t, st.RetainedVersions, retainedWarnThreshold+1)
}

// K6 往返失败 → error,且消息里带上 tier 名和原因
//
// 这条是本包唯一一条"任务真的会失败"的分支,也是最该被测的。
// 用 swapProbe 而不是真实 registry:真实不变式下造不出这个状态
// (理由见 rotationStateFn 的注释)。
func TestK06_RotateCheck_RoundTripFailureIsError(t *testing.T) {
	swapProbe(t,
		func(tier crypto.Tier) (crypto.KeyRotationState, error) {
			if tier != crypto.TierPII {
				return crypto.KeyRotationState{}, assert.AnError
			}
			return crypto.KeyRotationState{
				Tier: tier, Configured: true, Problem: "解密失败:坏密文",
			}, nil
		},
		func() bool { return true })

	err := kmsRotateCheck(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pii")
	assert.Contains(t, err.Error(), "解密失败")
}

// K7 多个 tier 失败 → 全部列出来,不是只报第一个
func TestK07_RotateCheck_ReportsAllFailures(t *testing.T) {
	swapProbe(t,
		func(tier crypto.Tier) (crypto.KeyRotationState, error) {
			return crypto.KeyRotationState{
				Tier: tier, Configured: true, Problem: "boom",
			}, nil
		},
		func() bool { return true })

	err := kmsRotateCheck(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pii")
	assert.Contains(t, err.Error(), "secret")
	assert.Contains(t, err.Error(), "normal")
}

// K8 tier 没配 = 跳过,不是失败;全都没配时也不提 legacy key
func TestK08_RotateCheck_UnconfiguredTierIsNotFailure(t *testing.T) {
	initCrypto(t,
		[]crypto.KeyEntry{{KeyID: "pii-1", KeyVersion: 1, Key: mkKey(1), Active: true}},
		nil, nil, nil)

	assert.NoError(t, kmsRotateCheck(context.Background()),
		"只配 PII 是合法部署,不能因为 Secret/Normal 没配就失败")
}

// K9 ctx 已取消 → 直接返回 ctx 错误,不继续探测
func TestK09_RotateCheck_RespectsContext(t *testing.T) {
	initCrypto(t,
		[]crypto.KeyEntry{{KeyID: "pii-1", KeyVersion: 1, Key: mkKey(1), Active: true}},
		nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := kmsRotateCheck(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// K10 legacy key 已注册 → 探测函数被问到(没有"有 legacy 就安静跳过"的分支)
func TestK10_RotateCheck_LegacyKeyAccessorWired(t *testing.T) {
	initCrypto(t,
		[]crypto.KeyEntry{{KeyID: "pii-1", KeyVersion: 1, Key: mkKey(1), Active: true}},
		nil, nil,
		&crypto.KeyEntry{KeyID: "legacy", KeyVersion: 1, Key: mkKey(9)})

	assert.True(t, crypto.HasLegacyKey())
	assert.NoError(t, kmsRotateCheck(context.Background()))
}

// K11 legacy key 未注册 → HasLegacyKey=false,但**任务不失败**
func TestK11_RotateCheck_NoLegacyKeyIsNotFailure(t *testing.T) {
	initCrypto(t,
		[]crypto.KeyEntry{{KeyID: "pii-1", KeyVersion: 1, Key: mkKey(1), Active: true}},
		nil, nil, nil)

	assert.False(t, crypto.HasLegacyKey())
	assert.NoError(t, kmsRotateCheck(context.Background()), "缺 legacy key 只告警")
}

// ─────────── 注册定义 ───────────

// 注册的 key / cron 表达式是**文档与 seed 的契约**,改了要同步三处
func TestRegistered_KeysAreStable(t *testing.T) {
	assert.Equal(t, "framework.job_history_purge", KeyJobHistoryPurge)
	assert.Equal(t, "framework.kms_rotate_check", KeyKMSRotateCheck)
}
