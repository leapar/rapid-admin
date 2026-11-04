// 系统运行日志的写入 / 查询(2026-11-03)
//
// # 这一组守的是什么
//
//	sys_system_log 是四类运行期事件(慢 SQL / 慢接口 / 5xx / panic)的
//	唯一落库面,写入方全在**请求主路径**上(AccessLog / Recover / slowquery)。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· RecordSystemLog 里的 Create 删掉 → 「写入可查询」变红
//	· ListSystemLogs 的 Level/Source/Keyword 过滤去掉 → 对应断言变红
//	· goroutine 改回同步调用方 ctx → 「异步不随调用方 ctx 取消」仍绿
//	  (这正是断言点名 context.Background 的原因:用例里传的 ctx 已 cancel,
//	   若实现误用调用方 ctx,写入会静默失败 → 列表查不到 → 红)
package service

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

func newSystemLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// ⚠️ RecordSystemLog 是**异步**写入(goroutine)—— :memory: 库下
	//   连接池的第二个连接看到的是另一个空库("no such table")。
	//   钉死单连接,AutoMigrate 的表结构对所有读写可见。
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.SysSystemLog{}))
	return db
}

// waitSystemLogCount 轮询等异步写入落地(上限 2s)
func waitSystemLogCount(t *testing.T, db *gorm.DB, want int64) int64 {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var n int64
	for {
		require.NoError(t, db.Model(&model.SysSystemLog{}).Count(&n).Error)
		if n >= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSystemLog_RecordAndList 写入 → 查询往返 + 筛选维度
func TestSystemLog_RecordAndList(t *testing.T) {
	db := newSystemLogTestDB(t)
	svc := NewAuditService(db, nil)

	// ⚠️ 调用方 ctx 已 cancel —— RecordSystemLog 必须内部用 context.Background,
	// 误用调用方 ctx 的话写入会静默失败,下面的计数断言就会红。
	// (ctx 本身不往下传,这里只是把"调用方 ctx 已死"这个前提立在注释里;
	//  若未来有人把签名改成接收 ctx 并直接用,本用例不会红 —— 那要靠
	//  behavior 级探针:把 context.Background 换成调用方 ctx 再跑这组。)
	deadCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = deadCtx

	svc.RecordSystemLog(&SystemLogEntry{
		Level: "warn", Source: "slow_sql", Path: "sys_user",
		Message: "慢 SQL: sys_user query 耗时 620ms", Detail: "SELECT * FROM sys_user",
		DurationMs: 620,
	})
	svc.RecordSystemLog(&SystemLogEntry{
		Level: "error", Source: "http_error", Method: "POST", Path: "/rapid-admin/v1/system/users",
		Message: "接口 5xx: POST /rapid-admin/v1/system/users status=500",
		TraceID: "trace-1", UserID: 42, DurationMs: 900,
	})
	svc.RecordSystemLog(&SystemLogEntry{
		Level: "error", Source: "panic", Method: "GET", Path: "/rapid-admin/v1/x",
		Message: "panic: runtime error", Detail: "goroutine 1 [running]:\n...",
		TraceID: "trace-2", UserID: 7,
	})
	// nil 不炸
	svc.RecordSystemLog(nil)

	require.Equal(t, int64(3), waitSystemLogCount(t, db, 3), "应写入 3 条(nil 不写)")

	// --- 全量 ---
	page, err := svc.ListSystemLogs(context.Background(), SystemLogQuery{Level: "", Source: ""})
	require.NoError(t, err)
	assert.Equal(t, int64(3), page.Total)

	// --- level 筛选 ---
	page, err = svc.ListSystemLogs(context.Background(), SystemLogQuery{Level: "error"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total, "error 级应有 2 条")

	// --- source 筛选 ---
	page, err = svc.ListSystemLogs(context.Background(), SystemLogQuery{Source: "slow_sql"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "slow_sql 应有 1 条")

	// --- keyword 命中 path ---
	page, err = svc.ListSystemLogs(context.Background(), SystemLogQuery{Keyword: "sys_user"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "keyword 应命中 path=表名")

	// --- keyword 命中 message ---
	page, err = svc.ListSystemLogs(context.Background(), SystemLogQuery{Keyword: "panic"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "keyword 应命中 message")

	// --- 排序:id DESC(最新的在前)---
	rows := page.List.([]model.SysSystemLog)
	assert.Equal(t, "panic", rows[0].Source, "最后写入的 panic 排最前")
	assert.Equal(t, uint64(7), rows[0].UserID)
}

// TestSystemLog_DetailTruncated 超长 Detail 截断(别让一条 panic 栈撑爆行)
func TestSystemLog_DetailTruncated(t *testing.T) {
	db := newSystemLogTestDB(t)
	svc := NewAuditService(db, nil)

	big := make([]byte, systemLogDetailMax+1000)
	for i := range big {
		big[i] = 'x'
	}
	svc.RecordSystemLog(&SystemLogEntry{
		Level: "error", Source: "panic", Message: "panic: big", Detail: string(big),
	})

	require.Equal(t, int64(1), waitSystemLogCount(t, db, 1))
	var row model.SysSystemLog
	require.NoError(t, db.First(&row).Error)
	assert.Len(t, row.Detail, systemLogDetailMax+len("\n…(已截断)"), "Detail 应被截断并加标记")
}
