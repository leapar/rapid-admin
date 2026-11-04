// Package jobs 平台内置定时任务 —— M70
//
// 这里只放**框架层自己**需要的任务(命名空间 framework.*),不放任何业务任务。
// 业务任务由各业务模块在自己的包里 cronx.Register(约定前缀 biz.*)。
//
// 两条设计约定,继承自 pkg/cronx:
//
//  1. **注册是一次性的**,由 main 装配期调 RegisterAll 一次。
//     Register 撞 key 会 panic,所以它不能出现在任何会被调两次的路径上
//     (包括单测)—— 单测直接调导出的 Handler,不碰注册表。
//
//  2. **cron 表达式全部避开整点整分**。所有副本都按绝对墙钟触发,
//     `0 4 * * *` 意味着每个副本在 04:00:00.000 同时醒来:
//     即使有去重锁,同时发起 N 个加锁请求本身也是无谓的压力,而去重窗口
//     (Layer 0 的 SELECT + Layer 1 的 SETNX)会在那一刻最拥挤。
//     错开到 17 / 23 分让各副本、不同任务自然错开,顺手也让人读日志时能一眼
//     认出是哪个任务。
package jobs

import (
	"context"
	"time"

	"gorm.io/gorm"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/cronx"
)

// 注册的 job key(外部,尤其 seed / 文档 / 测试用)
const (
	// KeyJobHistoryPurge 执行记录清理
	KeyJobHistoryPurge = "framework.job_history_purge"
	// KeyKMSRotateCheck 密钥轮转就绪检查
	KeyKMSRotateCheck = "framework.kms_rotate_check"
)

// RegisterAll 注册全部内置任务(main 装配期调**一次**)
//
// 传 db 而不是把 store 也建在这:store 由 cronx.New 那条线建,同一个 *gorm.DB。
// 这里只再包一层 cronx.Store 传给 Handler —— 让 purge 任务拿到裸 DB 后自己拼
// 删除语句的话,清理规则会在两处各有一份实现,迟早漂移。
func RegisterAll(db *gorm.DB, cfg config.JobConfig) {
	store := cronx.NewStore(db)

	cronx.Register(cronx.Job{
		Key:         KeyJobHistoryPurge,
		Name:        "定时任务执行记录清理",
		Description: "按保留天数 + 条数上限清理 sys_job_run,防止执行记录表无限增长。两条规则双保险,是为了让兜底不依赖本任务自己。",
		// 04:17 而非 04:00 —— 见包注释「错开整点」
		Cron:    "17 4 * * *",
		Enabled: true,
		// Timeout 留 0 → 走 job.default_timeout。刻意不给更长的:
		// 这是纯 DB 操作,跑不完的原因是表太大或 DB 卡了,给更多时间
		// 只会让「卡住的清理」占着锁更久,拖慢其他任务的排查
		Handler: func(ctx context.Context) error {
			return purgeHistory(ctx, store, cfg.HistoryRetentionDays, cfg.HistoryRetentionMax)
		},
	})

	cronx.Register(cronx.Job{
		Key:         KeyKMSRotateCheck,
		Name:        "密钥轮转就绪检查",
		Description: "探测每个密钥 tier 的加解密往返能力,并对「历史版本堆积 / 新 key 没切 Active」发出告警。只检查,不重加密业务数据。",
		// 09:23 —— 与 purge 错开,两个任务不会挤在同一分钟
		Cron:    "23 9 * * *",
		Enabled: true,
		Timeout: 5 * time.Minute,
		// 幂等:纯读 + 纯内存往返,跑两遍结果一样,所以不需要 NonIdempotent。
		// 非幂等的任务在锁降级时会被直接跳过,本任务没必要付这个代价
		Handler: kmsRotateCheck,
	})
}
