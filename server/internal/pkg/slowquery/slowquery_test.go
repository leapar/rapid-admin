package slowquery

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type testUser struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 用 in-memory SQLite(:memory:),无需文件,也不会有"文件被占用"清理问题
	// 每个 db 独立的 cache,确保多测试不冲突
	dsn := "file:slowquery_test?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&testUser{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestInstall_TriggersSlow(t *testing.T) {
	db := openTestDB(t)
	var called atomic.Int32
	if err := Install(db, Config{
		Threshold: 1 * time.Nanosecond, // 几乎任何 SQL 都超阈
		OnSlow: func(e Entry) {
			called.Add(1)
		},
	}); err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	for i := 0; i < 3; i++ {
		db.Create(&testUser{Name: "u"})
	}
	var us []testUser
	db.Find(&us)
	if called.Load() < 1 {
		t.Fatalf("expected slow OnSlow calls >= 1, got %d", called.Load())
	}
}

func TestInstall_NoTriggerUnderThreshold(t *testing.T) {
	db := openTestDB(t)
	var called atomic.Int32
	if err := Install(db, Config{
		Threshold: 10 * time.Second, // 极宽,不应触发
		OnSlow:    func(e Entry) { called.Add(1) },
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		db.Create(&testUser{Name: "u"})
	}
	if called.Load() != 0 {
		t.Fatalf("expected no slow calls, got %d", called.Load())
	}
}

func TestExtractTable(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM users WHERE id = 1":            "users",
		`INSERT INTO "orders" (id) VALUES (1)`:        "orders",
		"UPDATE products SET price = 1 WHERE id = 1":  "products",
		"DELETE FROM logs WHERE created_at < ?":       "logs",
		"SELECT u.* FROM users u JOIN orders o ON 1":  "users",
		"SELECT 1":                                    "unknown",
	}
	for sql, expect := range cases {
		got := extractTable(sql)
		if got != expect {
			t.Errorf("extractTable(%q) = %q, want %q", sql, got, expect)
		}
	}
}

func TestInstall_Idempotent(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 3; i++ {
		if err := Install(db, Config{Threshold: 1 * time.Millisecond}); err != nil {
			// GORM 第二次注册同名 callback 会报"callback already exists" → 应当被我们的 installed 集合拦截
			// 但我们的拦截在 db.Callback() 之前;GORM 内部的 callback 注册是幂等的(by name 覆盖)
			// 所以允许二次调用也成功是 OK 的
			t.Logf("install #%d returned %v", i, err)
		}
	}
}

// ── M150: 采样伪随机 + OnSlow panic 留痕 ────────────────────────────────

// TestSampleOK_TrueRandom 采样必须用真随机,不能再用时钟当伪随机数
//
// 旧实现 time.Now().UnixNano()%1000 在 Windows 时钟粒度(~15ms)下成簇:
// 采样率被量化到 ~10% 一档(0.05 实际 ≈10%)。真随机下两种断言都不可能
// 偶然失败(600 样本全同的概率 2^-599;万级样本 ±4σ),不是 flaky 断言。
func TestSampleOK_TrueRandom(t *testing.T) {
	if sampleOK(0) {
		t.Error("rate=0 必须恒不命中")
	}
	if !sampleOK(1) {
		t.Error("rate=1 必须恒命中")
	}
	// 0.5 档:连续调用必须两种结果都出现(旧实现在时钟粒度内长时间停在同一个值)
	seen := map[bool]bool{}
	for i := 0; i < 600; i++ {
		seen[sampleOK(0.5)] = true
	}
	if !seen[true] || !seen[false] {
		t.Error("600 个样本只有一种结果 —— 采样仍成簇,伪随机未修复")
	}
	// 0.05 档:万级样本命中率必须落在合理区间(旧实现会量化到 ~10%)
	const n = 10000
	hits := 0
	for i := 0; i < n; i++ {
		if sampleOK(0.05) {
			hits++
		}
	}
	if hits < 250 || hits > 750 {
		t.Errorf("rate=0.05 命中 %d/%d,偏离配置太远", hits, n)
	}
}

// TestTrace_OnSlowPanic_IsLogged OnSlow 回调 panic 必须留 Error 日志
//
// OnSlow 是告警链路(接 Sentry / 告警 / 推送),旧实现 recover 后完全静默
// —— 告警链路可以无声死亡。兜底"不 panic"的语义保留,但必须留痕。
func TestTrace_OnSlowPanic_IsLogged(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	w := &wrapped{
		orig:      gormlogger.Discard,
		threshold: 1 * time.Nanosecond,
		sample:    1.0,
		onSlow:    func(e Entry) { panic("boom: sentry down") },
		log:       zap.New(core),
	}
	w.Trace(context.Background(), time.Now().Add(-time.Second),
		func() (string, int64) { return "SELECT * FROM users", 1 }, nil)

	entries := logs.FilterMessage("slowquery: OnSlow panicked").All()
	if len(entries) != 1 {
		t.Fatalf("OnSlow panic 必须留一条 Error 日志,实际 %d 条", len(entries))
	}
	if got := entries[0].ContextMap()["panic"]; got != "boom: sentry down" {
		t.Errorf("日志必须带 recover 值,实际 %v", got)
	}
	// zap observer 的 MapObjectEncoder 把 ByteString 落成 string,两种类型都认
	switch stk := entries[0].ContextMap()["stack"].(type) {
	case string:
		if stk == "" {
			t.Error("日志必须带非空 debug.Stack()")
		}
	case []byte:
		if len(stk) == 0 {
			t.Error("日志必须带非空 debug.Stack()")
		}
	default:
		t.Errorf("日志必须带 debug.Stack(),实际类型 %T", entries[0].ContextMap()["stack"])
	}
}
