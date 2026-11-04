// 兼容入口 —— 转发到 internal/seed
// 文档 §6.1
// 运行: go run ./scripts/seed -config configs/config.yaml
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
	pwd := flag.String("password", "Admin@2026", "超级管理员初始密码(空字符串=使用默认)")
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
	fmt.Println("[seed] ok")
}
