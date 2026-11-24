// querycache —— GORM 查询结果缓存(SELECT 缓存 + 写回调自动失效)
//
// ## 这个包解决什么问题
//
// cachex 是"按点缓存"(GetOrLoad 显式 loader),列表 / 树 / 报表类 SELECT
// 每次都打库。本包借鉴 GoFrame gdb 的 Model.Cache,把"这条查询的结果缓存
// 下来、写到相关表就自动失效"收敛成显式两行:
//
//	querycache.Install(db)
//	querycache.On(db.Where(...), 5*time.Minute, "表标签").Find(ctx, &rows)
//
// ## 关键点
//
//   - opt-in:没有 On(...) 的查询行为完全不变
//   - 事务内不缓存(gdb 同款短路);TTL 兜底索引失效的漏网
//   - 裸 SQL 写路径打不着回调(db.Exec),必要时显式 Invalidate
//   - 缓存客户端无后端时自动退化直查 —— 缓存配置不改变业务语义
//
// ## 跑法
//
//	go run ./examples/querycache
//
// 看点:GORM logger 开着,SELECT 打到 stdout —— 第二次查询没有 SELECT
// 日志 = 命中缓存;UPDATE 之后又出现 SELECT = 写路径自动失效。
package main

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/pkg/cachex"
	"rapid-admin/internal/pkg/querycache"
)

// demoItem 演示表(真实项目里由 migrate 建)
type demoItem struct {
	ID     uint64 `gorm:"primaryKey"`
	Name   string
	Active bool
}

func main() {
	// 内存 sqlite 当演示库;logger 开 Info 让 SELECT 可见
	db, err := gorm.Open(sqlite.Open("file:querycache_demo?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(&demoItem{}); err != nil {
		panic(err)
	}
	seed := []demoItem{{ID: 1, Name: "菜单A", Active: true}, {ID: 2, Name: "按钮B", Active: true}, {ID: 3, Name: "停用C", Active: false}}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			panic(err)
		}
	}

	// 一、注入缓存客户端。真实项目在 main 装配 cachex 后:
	//	querycache.Use(cachePool.Safe())   // 有 Redis 走共享 L2
	// 这里用内存后端演示(零依赖)
	querycache.Use(cachex.NewWithBackend(cachex.NewMemoryBackend()))
	defer querycache.Use(nil)

	// 二、注册失效回调(Create/Update/Delete 之后按表标签清缓存)
	if err := querycache.Install(db); err != nil {
		panic(err)
	}

	ctx := context.Background()

	// 三、第一次查询:未命中,打库(stdout 会出现 SELECT)
	var rows []demoItem
	q := querycache.On(db.Where("active = ?", true), 5*time.Minute, "demo_items")
	if err := q.Find(ctx, &rows); err != nil {
		panic(err)
	}
	fmt.Printf("\n第一次查询:%d 行 hit=%v(有 SELECT = 打了库)\n", len(rows), q.Hit())

	// 四、第二次查询:命中缓存,不再打库(没有 SELECT)
	q = querycache.On(db.Where("active = ?", true), 5*time.Minute, "demo_items")
	if err := q.Find(ctx, &rows); err != nil {
		panic(err)
	}
	fmt.Printf("第二次查询:%d 行 hit=%v(没有 SELECT = 命中缓存)\n", len(rows), q.Hit())

	// 五、写路径 → GORM 回调自动失效 → 下次查询重新打库
	if err := db.Model(&demoItem{}).Where("id = ?", 1).Update("name", "菜单A-改名").Error; err != nil {
		panic(err)
	}
	fmt.Println("已 UPDATE demo_items(写回调自动失效)")

	q = querycache.On(db.Where("active = ?", true), 5*time.Minute, "demo_items")
	if err := q.Find(ctx, &rows); err != nil {
		panic(err)
	}
	fmt.Printf("写后查询:%d 行 hit=%v,第一条名=%q(有 SELECT = 失效后重新打库,读到新值)\n",
		len(rows), q.Hit(), rows[0].Name)
}
