// auditx —— 数据变更审计的白名单回调 + 显式落库
//
// ## 这个包解决什么问题
//
// 「谁在什么时候把 X 改成了 Y」要当证据用,但让每个业务 service 手写
// before/after 快照必然漏。auditx 对**白名单表**注册 GORM 回调
// (create/update/delete),操作人从 ctx 取,before/after 快照落
// sys_data_change_log —— 业务写与审计写**同一个连接/事务**,业务回滚
// 审计行也不存在(证据一致性)。
//
// ## 关键点
//
//   - Install 只应对**服务运行时**的 db 调;cmd/migrate 用自己的 db
//     实例不 Install,建表灌种子天然不被审计
//   - 动态表等**裸 SQL 写路径**打不着 GORM 回调,用 RecordChange 显式落
//   - 白名单不是所有表:流水类表再记一层是套娃
//
// ## 跑法
//
//	go run ./examples/auditx
package main

import (
	"context"
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/pkg/auditx"
)

func main() {
	// 内存 sqlite 当演示库;sys_data_change_log 在真实项目里由 migrate 建
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		panic(err)
	}
	if err := db.Exec(`CREATE TABLE sys_data_change_log (
		id INTEGER PRIMARY KEY,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME,
		table_name VARCHAR(64),
		record_id INTEGER,
		operation INTEGER,
		before TEXT, after TEXT,
		user_id INTEGER, username VARCHAR(64)
	)`).Error; err != nil {
		panic(err)
	}

	// 一、Install:注册白名单回调。LookupCaller 从 ctx 取操作人,
	//    nil 或取不到 ⇒ 记 system(seed / cron 后台写没有请求方,记 system 是诚实的)
	err = auditx.Install(db, auditx.Config{
		LookupCaller: func(ctx context.Context) (uint64, string) { return 1001, "demo_admin" },
		Rules: []auditx.TableRule{
			{Table: "demo_user"},
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("已对 demo_user 注册变更审计回调")

	// 二、RecordChange:显式通道,给裸 SQL 写路径用(GORM 回调打不着的地方)
	err = auditx.RecordChange(context.Background(), db, "demo_user", auditx.OpUpdate, 42,
		map[string]any{"status": "pending"},
		map[string]any{"status": "approved"})
	if err != nil {
		panic(err)
	}

	// 三、回读刚落的那条证据
	var row struct {
		TableName string
		RecordID  int64
		Operation int
		Before    string
		After     string
		Username  string
	}
	if err := db.Raw(`SELECT table_name, record_id, operation, before, after, username
		FROM sys_data_change_log ORDER BY id DESC LIMIT 1`).
		Scan(&row).Error; err != nil {
		panic(err)
	}
	fmt.Printf("落库结果: table=%s record=%d op=%d 操作人=%s\n", row.TableName, row.RecordID, row.Operation, row.Username)
	fmt.Printf("  before = %s\n  after  = %s\n", row.Before, row.After)
}
