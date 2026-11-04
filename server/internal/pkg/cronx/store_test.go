// gormStore 的 DB 级测试 —— 补 cronx_test.go 完全没有覆盖的一半
//
// cronx_test.go 里 45 条用例全跑在 fakeStore 上(不连 DB),
// 好处是去重/锁/热加载这些逻辑可以被单独拆开测;代价是 **gormStore 的 SQL
// 一行都没被测过** —— 而 PurgeHistory 的 bug 恰恰藏在 SQL 里:
//
//	model.BaseModel 带 DeletedAt gorm.DeletedAt → 默认 Delete() 是**软删**。
//	探针实测(修复前):删 5 条后 Count() 返 0(看不见),Unscoped().Count() 仍返 5。
//
// 而且这个 bug 骗得过**所有**软删作用域的断言 —— 只有数物理行数才能抓住它。
// 所以本文件的删除断言统一走 s.count()(Unscoped),不用软删作用域的 Count。
package cronx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// seeder 每个测试独占一套 sqlite 内存库 + id 分配器。
//
// 为什么要自己分配 id:BaseModel.ID 是 autoIncrement:false,不给值全是 0 → 主键冲突。
type seeder struct {
	t    *testing.T
	db   *gorm.DB
	next uint64
}

func newSeeder(t *testing.T) *seeder {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysJobRun{}))
	return &seeder{t: t, db: db, next: 1000}
}

// runs 造 n 条记录,started_at 从 now 往前每条递减 step。
// 多次调用 id 自动接续,不会撞主键。
func (s *seeder) runs(n int, step time.Duration, now time.Time) {
	s.t.Helper()
	for i := 0; i < n; i++ {
		require.NoError(s.t, s.db.Create(&model.SysJobRun{
			BaseModel:   model.BaseModel{ID: s.next},
			JobKey:      "t",
			TriggerType: model.JobTriggerSchedule,
			Status:      model.JobStatusSuccess,
			StartedAt:   now.Add(-time.Duration(i) * step),
		}).Error)
		s.next++
	}
}

// count 数**物理行数**(Unscoped 绕过软删作用域)。
//
// ⚠️ 本文件所有删除断言都必须用它,用 Model().Count() 等于没断言 ——
// 被软删的行对 Count() 是隐形的,断言会假绿。
func (s *seeder) count() int64 {
	s.t.Helper()
	var n int64
	require.NoError(s.t, s.db.Unscoped().Model(&model.SysJobRun{}).Count(&n).Error)
	return n
}

// ids 返回全部物理行的 id,按 started_at 升序
func (s *seeder) ids() []uint64 {
	s.t.Helper()
	var ids []uint64
	require.NoError(s.t, s.db.Unscoped().Model(&model.SysJobRun{}).
		Order("started_at ASC, id ASC").Pluck("id", &ids).Error)
	return ids
}

func (s *seeder) purge(days, max int) int64 {
	s.t.Helper()
	n, err := NewStore(s.db).PurgeHistory(context.Background(), days, max)
	require.NoError(s.t, err)
	return n
}

// S1 核心回归:PurgeHistory 必须**物理**删行
//
// 这条锁的正是上面那个软删 bug。修复前:物理行数一直是 5。
func TestS1_Purge_PhysicallyRemovesRows(t *testing.T) {
	s := newSeeder(t)
	s.runs(5, time.Minute, time.Now().Add(-100*24*time.Hour))

	assert.Equal(t, int64(5), s.purge(30, 0))
	assert.Equal(t, int64(0), s.count(),
		"保留期外的行必须物理消失;软删只让它们对 Count() 隐形,表照样涨")
}

// S2 天数规则:边界是严格小于,正好等于保留天数的**不删**
func TestS2_Purge_BoundaryIsStrictlyLess(t *testing.T) {
	s := newSeeder(t)
	now := time.Now()
	s.runs(1, time.Minute, now.Add(-30*24*time.Hour-time.Minute)) // 该删
	s.runs(1, time.Minute, now.Add(-29*24*time.Hour))             // 该留

	assert.Equal(t, int64(1), s.purge(30, 0))
	assert.Equal(t, []uint64{1001}, s.ids(), "29 天前那条在保留期内")
}

// S3 条数上限:留最新 max 条,且也是物理删
func TestS3_Purge_ByMaxKeepsNewest(t *testing.T) {
	s := newSeeder(t)
	s.runs(10, time.Minute, time.Now()) // 0..9 分钟前,id 1000..1009

	assert.Equal(t, int64(6), s.purge(0, 4))
	// ids 按 started_at 升序:留的是最新 4 条 = id 1000..1003,倒着排
	assert.Equal(t, []uint64{1003, 1002, 1001, 1000}, s.ids(),
		"留的必须是 started_at 最新的 4 条")
}

// S4 双规则:days 先删超期的,max 再删超量的
func TestS4_Purge_BothRulesCompose(t *testing.T) {
	s := newSeeder(t)
	now := time.Now()
	s.runs(5, time.Hour, now.Add(-100*24*time.Hour)) // 全超期
	s.runs(5, time.Minute, now)                      // 全新

	// days=30 删 5 条 → 剩 5 条;max=3 再删 2 条 → 剩 3 条
	assert.Equal(t, int64(7), s.purge(30, 3))
	assert.Equal(t, int64(3), s.count())
}

// S5 max 大于现存条数 → 不动任何东西(不能把表删空)
func TestS5_Purge_MaxAboveCountIsNoop(t *testing.T) {
	s := newSeeder(t)
	s.runs(3, time.Minute, time.Now())

	assert.Equal(t, int64(0), s.purge(0, 500))
	assert.Equal(t, int64(3), s.count())
}

// S6 跨批边界(purgeBatch=1000):第二批 50 条也必须删掉,不能死循环
func TestS6_Purge_BatchesPastPurgeBatch(t *testing.T) {
	s := newSeeder(t)
	s.runs(purgeBatch+50, time.Minute, time.Now().Add(-100*24*time.Hour))

	assert.Equal(t, int64(purgeBatch+50), s.purge(30, 0))
	assert.Equal(t, int64(0), s.count())
}

// S7 幂等:第二次跑没有可删的,返回 0 且不报错
func TestS7_Purge_Idempotent(t *testing.T) {
	s := newSeeder(t)
	s.runs(3, time.Minute, time.Now().Add(-100*24*time.Hour))

	require.Equal(t, int64(3), s.purge(30, 0))
	assert.Equal(t, int64(0), s.purge(30, 0), "第二次不该报错")
	assert.Equal(t, int64(0), s.count())
}

// S8 nil db(极简部署模式)→ 返回 0 而不是 panic
func TestS8_Purge_NilDB(t *testing.T) {
	n, err := NewStore(nil).PurgeHistory(context.Background(), 30, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

// S9 已被业务侧软删的行:不被 purge 算进 kept,也不被物理删误伤
func TestS9_Purge_IgnoresAlreadySoftDeletedRows(t *testing.T) {
	s := newSeeder(t)
	s.runs(4, time.Minute, time.Now()) // id 1000..1003

	// 业务侧手动软删最新那条(id 1000)
	require.NoError(t, s.db.Where("id = ?", 1000).Delete(&model.SysJobRun{}).Error)

	// 保留 2 条 → 可见的 3 条里删掉最旧的 1 条(id 1003)
	assert.Equal(t, int64(1), s.purge(0, 2))

	// 活着的 2 条 + 之前软删的 1 条 = 物理 3 行
	assert.Equal(t, int64(3), s.count())
	// ids 按 started_at 升序:1002(最旧)→ 1000(最新)
	assert.Equal(t, []uint64{1002, 1001, 1000}, s.ids(), "1003 最旧,已被 purge")
}
