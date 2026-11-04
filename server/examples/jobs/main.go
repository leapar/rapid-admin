// jobs —— 平台内置定时任务
//
// ## 这个包解决什么问题
//
// 框架自己也需要定时干活:清理定时任务的执行记录、检查密钥轮转就绪。
// 这些任务**不归任何业务模块**,但它们不能散落在各个业务包里 ——
// 散开的结果是没人知道总共有几个框架任务在跑、它们的 cron 表达式是什么。
//
// 所以框架层单独有个 jobs 包,统一注册、统一命名(`framework.*` 前缀)。
//
// ## 关键点
//
//   - **只放框架层自己的任务**。业务任务由各业务模块在自己的包里
//     `cronx.Register`(约定前缀 `biz.*`)
//   - **注册是一次性的**。`cronx.Register` 撞 key 会 panic,
//     所以 `RegisterAll` 只能由 main 装配期调**一次**
//   - **cron 表达式全部避开整点整分**:两个任务是 `17 4 * * *` 和
//     `23 9 * * *`。所有副本都按绝对墙钟触发,`0 4 * * *` 意味着每个副本
//     在 04:00:00.000 同时醒来 —— 即使有去重锁,同时发起 N 个加锁请求
//     本身也是无谓的压力,而去重窗口在那一刻最拥挤。
//     错开到 17 / 23 分让各副本、不同任务自然错开,顺手也让人读日志时
//     能一眼认出是哪个任务
//   - **执行记录清理是双保险**:按天数(防低频任务长尾堆积)+
//     按条数上限(防高频任务一天几万条)。之所以要双保险,是因为
//     **清理历史的任务本身也是定时任务** —— 它挂了没人知道,
//     兜底线必须不依赖它自己
//   - **两个参数都为 0 时返 error 而不是静默什么都不做**:
//     一个每天都显示"执行成功"、实际什么都没清的任务,
//     是最难被发现的那种坏
//
// ## 跑法
//
//	go run ./examples/jobs
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/jobs"
)

// jobRunDDL 定时任务执行记录表(生产上由 AutoMigrate 建)
const jobRunDDL = `
CREATE TABLE IF NOT EXISTS sys_job_run (
	id            INTEGER PRIMARY KEY,
	created_at    DATETIME,
	updated_at    DATETIME,
	deleted_at    DATETIME,
	job_key       TEXT,
	trigger_type  TEXT,
	status        TEXT,
	started_at    DATETIME,
	finished_at   DATETIME,
	duration_ms   INTEGER,
	error         TEXT,
	instance_id   TEXT,
	trace_id      TEXT,
	operator      TEXT,
	degraded      BOOLEAN
)`

func main() {
	ctx := context.Background()

	// ── 一、装配 ────────────────────────────────────────────────
	fmt.Println("=== 装配 ===")
	db, err := gorm.Open(sqlite.Open("file:jobs_demo?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		fmt.Println("开库失败:", err)
		return
	}
	if err := db.Exec(jobRunDDL).Error; err != nil {
		fmt.Println("建表失败:", err)
		return
	}

	// 清理规则:保留 30 天,最多留 2 条
	cfg := config.JobConfig{
		Enabled:              true,
		TimeZone:             "Asia/Shanghai",
		LockMode:             "memory",
		LockTTL:              5 * time.Minute,
		DefaultTimeout:       10 * time.Minute,
		ShutdownTimeout:      30 * time.Second,
		HistoryRetentionDays: 30,
		HistoryRetentionMax:  2,
	}

	// ⚠️ RegisterAll 只能调**一次** —— 撞 key 会 panic。
	// 它必须发生在 cronx.New 之前(注册表要先有东西,调度器才知道要调度谁)。
	jobs.RegisterAll(db, cfg)
	fmt.Println("jobs.RegisterAll 已调用(框架任务注册进 cronx 注册表)")

	sched, err := cronx.New(cronx.Config{
		Enabled:         true,
		TimeZone:        cfg.TimeZone,
		LockMode:        cfg.LockMode,
		LockTTL:         cfg.LockTTL,
		DefaultTimeout:  cfg.DefaultTimeout,
		ShutdownTimeout: cfg.ShutdownTimeout,
	}, cronx.NewStore(db), nil)
	if err != nil {
		fmt.Println("建调度器失败:", err)
		return
	}
	cronx.SetDefault(sched)

	// ── 二、注册了什么 ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 注册结果 ===")
	for _, e := range sched.List() {
		fmt.Printf("  %-30s cron=%-12s 超时=%v\n", e.Key, e.CronExpr, e.Timeout)
	}
	fmt.Println("  ⇒ 两个任务的 cron 都在 17 分 / 23 分,不在整点 ——")
	fmt.Println("    所有副本按绝对墙钟触发,整点会让它们同时醒来抢锁")
	fmt.Println("  ⇒ framework.* 是框架任务,biz.* 是业务任务(业务模块自己注册)")

	// ── 三、造点脏数据 ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 造点脏数据 ===")
	now := time.Now()
	// 3 条 40 天前的(超过保留期)+ 4 条刚跑完的(在保留期内)
	for i := 0; i < 3; i++ {
		seedRun(db, int64(1000+i), now.AddDate(0, 0, -40))
	}
	for i := 0; i < 4; i++ {
		seedRun(db, int64(2000+i), now.Add(-time.Duration(i)*time.Minute))
	}
	printCount(db, "  清理前")

	// ── 四、触发清理任务 ────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 触发清理任务 ===")
	rec, err := sched.Trigger(ctx, jobs.KeyJobHistoryPurge, cronx.TriggerManual)
	if err != nil {
		fmt.Println("触发失败:", err)
		return
	}
	fmt.Printf("  %s → 状态=%s 耗时=%dms\n", rec.JobKey, rec.Status, rec.DurationMs)
	printCount(db, "  清理后")
	fmt.Println("  ⇒ 天数规则:40 天前那 3 条被清掉")
	fmt.Println("  ⇒ 条数规则:剩下的最多留 2 条")
	fmt.Println("  ⇒ 清理任务自己的执行记录也在这张表里 —— 所以最后多出来的那 1 行是它自己")

	// ── 五、密钥轮转检查 ────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 密钥轮转检查 ===")
	rec2, err := sched.Trigger(ctx, jobs.KeyKMSRotateCheck, cronx.TriggerManual)
	if err != nil {
		fmt.Println("触发失败:", err)
		return
	}
	fmt.Printf("  %s → 状态=%s\n", rec2.JobKey, rec2.Status)
	fmt.Println("  ⇒ 本示例没配 KMS,所以它**跳过**而不是失败")
	fmt.Println("    (「tier 没配」≠「配坏了」—— 只用 PII 加密、不加密 Secret 的部署完全合法)")
	fmt.Println("  ⇒ 这个任务只回答两件事:加解密这条路走得通吗?轮转流程停在半路了吗?")
	fmt.Println("    它**不扫业务表、不重加密任何字段** —— 那些只有业务自己知道")
}

// seedRun 插一条执行记录(裸 SQL,不 import internal/model)
func seedRun(db *gorm.DB, id int64, startedAt time.Time) {
	err := db.Exec(`INSERT INTO sys_job_run
		(id, created_at, updated_at, job_key, trigger_type, status, started_at,
		 duration_ms, instance_id, degraded)
		VALUES (?, ?, ?, 'framework.job_history_purge', 'schedule', 'success', ?, 12, 'demo-instance', 0)`,
		id, startedAt, startedAt, startedAt).Error
	if err != nil {
		fmt.Println("  插脏数据失败:", err)
	}
}

// printCount 打印表里还剩多少行(软删的行 Count 看不见,所以另查一次物理行数)
func printCount(db *gorm.DB, prefix string) {
	var visible int64
	db.Raw(`SELECT COUNT(*) FROM sys_job_run WHERE deleted_at IS NULL`).Scan(&visible)
	var physical int64
	db.Raw(`SELECT COUNT(*) FROM sys_job_run`).Scan(&physical)
	fmt.Printf("%s:可见 %d 行 / 物理 %d 行\n", prefix, visible, physical)
}
