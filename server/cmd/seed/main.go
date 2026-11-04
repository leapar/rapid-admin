// rapid-admin 种子数据入口
// 文档 §6.1
// 使用: go run ./cmd/seed -config configs/config.yaml -password Admin@2026
package main

import (
	"flag"
	"fmt"
	"log"

	"rapid-admin/internal/config"
	"rapid-admin/internal/repository"
	"rapid-admin/internal/seed"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	pwd := flag.String("password", "Admin@2026", "超级管理员初始密码")
	flag.Parse()

	if err := config.Load(*cfgPath); err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg := config.Get()
	if _, err := repository.NewDB(cfg.Database, cfg.MySQL); err != nil {
		log.Fatalf("init db: %v", err)
	}
	seed.SetDriver(string(cfg.Database.Driver))
	if err := seed.Run(repository.GetDB(), cfg.Casbin.ModelPath, *pwd); err != nil {
		log.Fatalf("seed: %v", err)
	}
	fmt.Println("[cmd/seed] ok")
}
