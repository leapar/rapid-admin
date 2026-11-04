// 系统配置审计日志 seed(给前端看真实数据 + 演示用)
//
// 2026-10-18 收窄:
//   - UI 只走 PATCH /configs/:id 一条写通道,action 唯一 = "update"
//   - create/delete/view 3 种后端接口已全删,seed 不再生成
//   - 旧数据可能仍有 4 种 action(schema 字段保留),前端 Tag 颜色 map 兜底
//
// 覆盖场景(全部 update):覆盖 17 个 sys_config key 中高频改动的 6 个,演示
//   super_admin 在过去 2 小时内调过 region / ttl / sign_name / template / cdn / endpoint
//
// 运行: go run ./cmd/seed_audit -config configs/config.dev.yaml
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
	"rapid-admin/internal/repository"
)

type record struct {
	key    string
	before string
	after  string
	ip     string
	agoMin int
}

func main() {
	cfgPath := flag.String("config", "configs/config.dev.yaml", "配置文件路径")
	flag.Parse()

	if err := config.Load(*cfgPath); err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg := config.Get()
	if _, err := repository.NewDB(cfg.Database, cfg.MySQL); err != nil {
		log.Fatalf("init db: %v", err)
	}
	db := repository.GetDB()

	// 1. 拿 super_admin user
	var admin model.SysUser
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		log.Fatalf("find super_admin: %v", err)
	}
	fmt.Printf("[seed_audit] super_admin: id=%d name=%s\n", admin.ID, admin.RealName)

	// 2. 拿 17 个 sys_config,按 key 索引
	var configs []model.SysConfig
	if err := db.Find(&configs).Error; err != nil {
		log.Fatalf("list sys_config: %v", err)
	}
	if len(configs) == 0 {
		log.Fatal("no sys_config rows, run seed first")
	}
	idx := map[string]model.SysConfig{}
	for _, c := range configs {
		idx[c.ConfigKey] = c
	}
	fmt.Printf("[seed_audit] sys_config rows: %d\n", len(configs))

	// 3. 先清空旧 audit 记录(避免重复 seed 累积)
	if err := db.Where("1 = 1").Delete(&model.SysConfigAudit{}).Error; err != nil {
		log.Fatalf("clear audit: %v", err)
	}

	// 4. 构造记录(从 90 分钟前到现在,符合真实运营节奏)
	//   2026-10-18 收窄:全部 action=update
	now := time.Now()
	mk := func(r record) model.SysConfigAudit {
		cfg, ok := idx[r.key]
		if !ok {
			log.Fatalf("config key not found: %s", r.key)
		}
		return model.SysConfigAudit{
			BaseModel: model.BaseModel{
				ID:        sonyflake.Next(),
				CreatedAt: now.Add(-time.Duration(r.agoMin) * time.Minute),
				UpdatedAt: now.Add(-time.Duration(r.agoMin) * time.Minute),
			},
			ConfigID:  cfg.ID,
			ConfigKey: r.key,
			Action:    model.ConfigAuditActionUpdate,
			UserID:    admin.ID,
			UserRole:  "role_super_admin",
			IP:        r.ip,
			Before:    r.before,
			After:     r.after,
		}
	}

	records := []record{
		// 过去 90 分钟内的 6 次 super_admin 运维调整
		{key: "aliyun.sms.region", before: "cn-hangzhou", after: "cn-shanghai", ip: "192.168.10.45", agoMin: 90},
		{key: "aliyun.oss.upload_token_ttl", before: "3600", after: "7200", ip: "192.168.10.45", agoMin: 75},
		{key: "aliyun.sms.sign_name", before: "示例集团", after: "示例数字科技", ip: "192.168.10.45", agoMin: 60},
		{key: "aliyun.sms.template.login", before: "SMS_0001", after: "SMS_2026_LOGIN", ip: "192.168.10.45", agoMin: 45},
		{key: "aliyun.oss.cdn_domain", before: "", after: "https://cdn-dev.example.com", ip: "10.0.0.12", agoMin: 30},
		{key: "aliyun.oss.endpoint", before: "https://oss-cn-hangzhou.aliyuncs.com", after: "https://oss-cn-shanghai.aliyuncs.com", ip: "10.0.0.12", agoMin: 15},
	}

	// 5. 批量插入
	audits := make([]model.SysConfigAudit, 0, len(records))
	for _, r := range records {
		audits = append(audits, mk(r))
	}
	if err := db.Create(&audits).Error; err != nil {
		log.Fatalf("insert audit: %v", err)
	}

	fmt.Printf("[seed_audit] inserted %d audit records (action=update)\n", len(audits))

	// 6. 验证:模拟 service.ListConfigAudit 的 join 逻辑,看 user_name 能否拿到
	fmt.Println("\n[verify] LEFT JOIN sys_user 看 user_name:")
	type row struct {
		model.SysConfigAudit
		UserName string `gorm:"column:user_name"`
	}
	var rows []row
	if err := db.Model(&model.SysConfigAudit{}).
		Select("sys_config_audit.*, u.real_name AS user_name").
		Joins("LEFT JOIN sys_user u ON u.id = sys_config_audit.user_id").
		Order("sys_config_audit.id DESC").
		Limit(5).
		Scan(&rows).Error; err != nil {
		log.Fatalf("verify join: %v", err)
	}
	for _, r := range rows {
		fmt.Printf("  ID=%d  action=%-7s  key=%-40s  user_name=%s\n",
			r.ID, r.Action, r.ConfigKey, r.UserName)
	}

	// 7. 验证:确认 DB 里只剩 update 一种 action
	fmt.Println("\n[verify] action distribution:")
	type count struct {
		Action string
		N      int
	}
	var counts []count
	db.Model(&model.SysConfigAudit{}).
		Select("action, COUNT(*) as n").
		Group("action").
		Scan(&counts)
	for _, c := range counts {
		fmt.Printf("  %-8s %d\n", c.Action, c.N)
	}
}
