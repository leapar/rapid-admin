package querycache

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/pkg/cachex"
)

// demoItem 演示表(内存 sqlite,migrate 建表)。
type demoItem struct {
	ID     uint64 `gorm:"primaryKey"`
	Name   string
	Active bool
}

// newEnv 每个测试独立:内存 sqlite + 内存缓存 + SELECT 计数回调。
// count 数的是真实打到库上的 SELECT 次数 —— 命中缓存不执行 SQL,计数不涨。
func newEnv(t *testing.T) (db *gorm.DB, safe *cachex.Safe, count *atomic.Int32) {
	t.Helper()
	// ⚠️ 每个测试独立命名库:file::memory:?cache=shared 是**全局一份**,
	// 多测试套跑会互相看见对方的行 —— 按测试名隔离。
	dsn := "file:memdb_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatalf("打开 sqlite 失败: %v", err)
	}
	if err := db.AutoMigrate(&demoItem{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}

	count = &atomic.Int32{}
	if err := db.Callback().Query().After("gorm:query").Register("test:count", func(tx *gorm.DB) {
		// DryRun(建键指纹)只构建 SQL 不执行,不算真实打库
		if !tx.DryRun {
			count.Add(1)
		}
	}); err != nil {
		t.Fatalf("注册计数回调失败: %v", err)
	}

	safe = cachex.NewWithBackend(cachex.NewMemoryBackend())
	Use(safe)
	t.Cleanup(func() { Use(nil) })

	if err := Install(db); err != nil {
		t.Fatalf("Install 失败: %v", err)
	}
	return db, safe, count
}

func seed(t *testing.T, db *gorm.DB, items ...demoItem) {
	t.Helper()
	for i := range items {
		if err := db.Create(&items[i]).Error; err != nil {
			t.Fatalf("造数据失败: %v", err)
		}
	}
}

func find(t *testing.T, db *gorm.DB, ctx context.Context) []demoItem {
	t.Helper()
	var rows []demoItem
	q := On(db.Where("active = ?", true), 5*time.Minute, "demo_items")
	if err := q.Find(ctx, &rows); err != nil {
		t.Fatalf("Find 失败: %v", err)
	}
	return rows
}

func TestFind_SecondHitSkipsDB(t *testing.T) {
	db, _, count := newEnv(t)
	seed(t, db, demoItem{ID: 1, Name: "a", Active: true}, demoItem{ID: 2, Name: "b", Active: false})

	ctx := context.Background()
	rows := find(t, db, ctx)
	if len(rows) != 1 || rows[0].Name != "a" {
		t.Fatalf("首次结果错误: %+v", rows)
	}
	if n := count.Load(); n != 1 {
		t.Fatalf("首次查询应打库一次,count=%d", n)
	}

	rows2 := find(t, db, ctx)
	if len(rows2) != 1 || rows2[0].Name != "a" {
		t.Fatalf("命中结果错误: %+v", rows2)
	}
	if n := count.Load(); n != 1 {
		t.Fatalf("第二次应命中缓存不打库,count=%d", n)
	}
}

func TestWriteInvalidates(t *testing.T) {
	db, _, count := newEnv(t)
	seed(t, db, demoItem{ID: 1, Name: "a", Active: true})

	ctx := context.Background()
	_ = find(t, db, ctx) // 打库并缓存
	_ = find(t, db, ctx) // 命中
	if n := count.Load(); n != 1 {
		t.Fatalf("前置缓存失败,count=%d", n)
	}

	// GORM 写路径 → 回调失效
	if err := db.Model(&demoItem{}).Where("id = ?", 1).Update("name", "a2").Error; err != nil {
		t.Fatalf("写失败: %v", err)
	}
	rows := find(t, db, ctx)
	if len(rows) != 1 || rows[0].Name != "a2" {
		t.Fatalf("失效后读到旧数据: %+v", rows)
	}
	if n := count.Load(); n != 2 {
		t.Fatalf("失效后应重新打库,count=%d", n)
	}
}

func TestRawExec_NotInvalidated(t *testing.T) {
	db, _, count := newEnv(t)
	seed(t, db, demoItem{ID: 1, Name: "a", Active: true})

	ctx := context.Background()
	_ = find(t, db, ctx)
	_ = find(t, db, ctx)
	if n := count.Load(); n != 1 {
		t.Fatalf("前置缓存失败,count=%d", n)
	}

	// 裸 SQL 写路径打不着 GORM 回调(包注释如实说明的边界):
	// 不失效,但 TTL 兜底 —— 这里验证"不失效"这一半。
	if err := db.Exec(`UPDATE demo_items SET name = 'raw' WHERE id = 1`).Error; err != nil {
		t.Fatalf("裸 SQL 失败: %v", err)
	}
	rows := find(t, db, ctx)
	if len(rows) != 1 || rows[0].Name != "a" {
		t.Fatalf("裸 SQL 后不应失效(边界如此): %+v", rows)
	}
	if n := count.Load(); n != 1 {
		t.Fatalf("裸 SQL 不应触发失效,count=%d", n)
	}

	// 显式 Invalidate 是裸 SQL 的补救通道
	Invalidate(ctx, "demo_items")
	rows = find(t, db, ctx)
	if rows[0].Name != "raw" {
		t.Fatalf("显式失效后应读到新值: %+v", rows)
	}
	if n := count.Load(); n != 2 {
		t.Fatalf("显式失效后应重新打库,count=%d", n)
	}
}

func TestInTx_SkipsCache(t *testing.T) {
	db, _, count := newEnv(t)
	seed(t, db, demoItem{ID: 1, Name: "a", Active: true})

	ctx := context.Background()
	tx := db.Begin()
	defer tx.Rollback()

	var rows []demoItem
	for i := 0; i < 2; i++ {
		q := On(tx.Where("active = ?", true), 5*time.Minute, "demo_items")
		if err := q.Find(ctx, &rows); err != nil {
			t.Fatalf("事务内 Find 失败: %v", err)
		}
	}
	// 事务内不缓存:两次都打库(gdb 同款短路)
	if n := count.Load(); n != 2 {
		t.Fatalf("事务内不应缓存,count=%d", n)
	}
}

func TestNoTables_NoCache(t *testing.T) {
	db, _, count := newEnv(t)
	seed(t, db, demoItem{ID: 1, Name: "a", Active: true})

	ctx := context.Background()
	var rows []demoItem
	for i := 0; i < 2; i++ {
		// 不传失效标签 = 不缓存(没有失效手段的缓存是裸奔)
		q := On(db.Where("active = ?", true), 5*time.Minute)
		if err := q.Find(ctx, &rows); err != nil {
			t.Fatalf("Find 失败: %v", err)
		}
	}
	if n := count.Load(); n != 2 {
		t.Fatalf("无标签不应缓存,count=%d", n)
	}
}

func TestNoSafe_DirectQuery(t *testing.T) {
	db, _, count := newEnv(t)
	seed(t, db, demoItem{ID: 1, Name: "a", Active: true})
	Use(nil) // 模拟 cachex 未启用

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		find(t, db, ctx)
	}
	if n := count.Load(); n != 2 {
		t.Fatalf("无客户端应全部直查,count=%d", n)
	}
}

func TestEmptyResult_Cached(t *testing.T) {
	db, _, count := newEnv(t)
	seed(t, db, demoItem{ID: 1, Name: "a", Active: false}) // 没有满足条件的行

	ctx := context.Background()
	rows := find(t, db, ctx)
	if len(rows) != 0 {
		t.Fatalf("应为空结果: %+v", rows)
	}
	_ = find(t, db, ctx)
	if n := count.Load(); n != 1 {
		t.Fatalf("空结果也应被缓存(防穿透),count=%d", n)
	}
}

func TestMultiTable_TagInvalidates(t *testing.T) {
	db, _, count := newEnv(t)
	seed(t, db, demoItem{ID: 1, Name: "a", Active: true})

	ctx := context.Background()
	var rows []demoItem
	q := On(db.Where("active = ?", true), 5*time.Minute, "demo_items", "sys_other")
	if err := q.Find(ctx, &rows); err != nil {
		t.Fatalf("Find 失败: %v", err)
	}
	_ = find(t, db, ctx)

	// 写到第二个 tag 表也应失效这条查询
	Invalidate(ctx, "sys_other")
	_ = find(t, db, ctx)
	if n := count.Load(); n != 2 {
		t.Fatalf("任一 tag 表写失效都应生效,count=%d", n)
	}
}
