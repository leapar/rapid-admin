// rapid-admin 数据库迁移入口
// 文档 §4.2
// 使用:
//   go run ./cmd/migrate -config configs/config.yaml             # AutoMigrate + seed + dict guard
//   go run ./cmd/migrate -config configs/config.yaml -dry-run     # 仅打印 SQL,不跑 seed
//   go run ./cmd/migrate -config configs/config.yaml -v           # 显示每张表
//   go run ./cmd/migrate -skip-seed                              # 跳过 seed(只迁移表结构)
//
// 2026-10-18 起变更:migrate 末尾自动跑 seed + SeedDictGuard
//   - 之前 dev 流程需要先 go run ./cmd/migrate 再 go run ./cmd/seed 两步,
//     一旦漏跑 seed,前端 useDict() 拿不到数据,fallback 显示原始 key(如 role_type 显示
//     'platform' 而不是 '平台级')。这是 5 层字典防御的 L1 漏洞。
//   - 修复:migrate 把 seed + guard 一起跑。生产环境用 -skip-seed,避免误覆盖运营改过的字典。
package main

import (
	"flag"
	"fmt"
	"log"

	"rapid-admin/internal/config"
	"rapid-admin/internal/migrate"
	"rapid-admin/internal/repository"
	"rapid-admin/internal/seed"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	dryRun := flag.Bool("dry-run", false, "只打印 DDL,不在数据库中执行")
	verbose := flag.Bool("v", false, "打印每张表的迁移状态")
	skipSeed := flag.Bool("skip-seed", false, "跳过 seed 和字典完整性检查(只迁移表结构)")
	superPwd := flag.String("password", "Admin@2026", "超级管理员初始密码(仅 seed 时用)")
	flag.Parse()

	if err := config.Load(*cfgPath); err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg := config.Get()
	if _, err := repository.NewDB(cfg.Database, cfg.MySQL); err != nil {
		log.Fatalf("init db: %v", err)
	}
	if err := migrate.Run(repository.GetDB(), *dryRun, *verbose); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if !*dryRun && !*skipSeed {
		// 1) seed 角色 / 资源 / Casbin policy / 字典(upsert,不会覆盖运营改过的 label)
		seed.SetDriver(string(cfg.Database.Driver))
		if err := seed.Run(repository.GetDB(), cfg.Casbin.ModelPath, *superPwd); err != nil {
			log.Fatalf("seed: %v", err)
		}
		// 2) L1 字典完整性 guard:必须字典缺失 → 直接非 0 退出
		if err := seed.SeedDictGuard(repository.GetDB()); err != nil {
			log.Fatalf("❌ 字典完整性检查失败: %v", err)
		}
		fmt.Println("✅ 字典完整性检查通过")
	}
	fmt.Println("[cmd/migrate] ok")
}
