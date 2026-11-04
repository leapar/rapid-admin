// Package cronx 定时任务调度框架层 —— M67
//
// 设计:
//
//  1. **handler 只在代码里注册**,DB(sys_job)只能覆盖 cron 表达式和启停。
//     与本仓字典 / 系统配置同一条规矩:DB 里凭空长不出任务,那样长出来的任务
//     没有代码,重启即消失,而且没人知道它曾经存在过。
//
//  2. **两层去重**,各挡各的,只上一层都不够:
//     - Layer 0 查库:SELECT 1 FROM sys_job_run WHERE job_key=? AND status='running'
//     挡住"上一次还没跑完就又跑"。不依赖 Redis —— DB 是所有副本都有的东西,
//     比任何外部锁服务都少一个失效点。执行记录表本来就要建,去重是白送的。
//     - Layer 1 加锁:Redis SETNX + Lua CAS 释放 + 看门狗续期。
//     挡住"两个副本在同一毫秒同时开跑" —— robfig 按绝对墙钟触发,所有副本
//     在同一瞬间醒来,Layer 0 是 check-then-act,关不掉这个窗口。
//
//  3. **降级 fail-open,但降级后仍剩 Layer 0**。定时任务停摆比偶尔重复执行更难发现
//     (明源同步不更新、过期授权不下榜,都是静默的)。但重复执行会写坏数据。
//     折中:继续跑 + 大声告警 + 写 degraded 标记,并把安全性建立在
//     "handler 必须幂等" 这条硬契约上。
//
// 用法:
//
//	cronx.Register(cronx.Job{
//	    Key:     "framework.job_history_purge",
//	    Name:    "定时任务执行记录清理",
//	    Cron:    "0 3 * * *",
//	    Enabled: true,
//	    Handler: purgeJobHistory,
//	})
//	// main.go 启动
//	s := cronx.New(cronx.Config{...}, store, locker)
//	s.Start(ctx)
//	defer s.Shutdown()
package cronx

import (
	"context"
	"errors"
	"time"
)

// 锁模式
const (
	// LockModeAuto 有 Redis 用 Redis,没有退进程内锁 + 降级告警
	LockModeAuto = "auto"
	// LockModeRedis 强制 Redis,没配直接启动失败(生产多副本必须用这个)
	LockModeRedis = "redis"
	// LockModeMemory 强制进程内锁,即使有 Redis 也不连。仅单副本
	LockModeMemory = "memory"
	// LockModeOff 完全不锁。仅本地开发
	LockModeOff = "off"
)

// 触发来源
type TriggerType string

const (
	// TriggerSchedule cron 表达式定时触发
	TriggerSchedule TriggerType = "schedule"
	// TriggerManual 后台管理端手动触发
	TriggerManual TriggerType = "manual"
)

// 执行状态(与 model.JobStatus 对齐,这里定义常量避免 pkg 依赖 model 的枚举)
const (
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// 错误
var (
	// ErrJobNotFound 代码注册表里没有这个 key
	ErrJobNotFound = errors.New("cronx: 任务未注册")
	// ErrJobDisabled 任务被停用(code.Enabled=false,或 DB 覆盖行 enabled=false)
	ErrJobDisabled = errors.New("cronx: 任务已停用")
	// ErrJobRunning 上一次还在跑(Layer 0 或 Layer 1 判定冲突)
	ErrJobRunning = errors.New("cronx: 任务正在执行中")
	// ErrShuttingDown 调度器已进入停机流程,拒绝新的触发。
	// 这是 Shutdown 的 Wait 与 Trigger 的 Add 之间 happens-before 的代价:
	// 不关门就无法保证"Wait 不会漏看在途触发"。
	ErrShuttingDown = errors.New("cronx: 调度器正在停机,拒绝触发")
	// ErrEmptyCron 表达式为空
	ErrEmptyCron = errors.New("cronx: cron 表达式为空")
	// ErrInvalidCron 表达式非法
	ErrInvalidCron = errors.New("cronx: cron 表达式非法")
	// ErrDegraded 去重能力降级(锁不可用),本次触发被主动放弃。
	// 与 ErrJobRunning 分开:两者都是"没跑",但运维要看的处置完全不同 ——
	// ErrJobRunning = 上一轮还没结束(正常);ErrDegraded = Redis 掉了(要去查)
	ErrDegraded = errors.New("cronx: 去重能力降级,已跳过本次触发")
)

// Job 一个定时任务的代码侧定义
type Job struct {
	// Key 全局唯一,snake_case。
	// 约定前缀:framework.* 是框架任务,biz.* 是业务任务。
	Key string

	// Name 中文名(DB 行可覆盖)
	Name string

	// Description 说明,展示在管理端
	Description string

	// Cron 代码默认表达式,DB(sys_job.cron_expr)可覆盖
	Cron string

	// Timeout 单次执行超时,0 → Config.DefaultTimeout
	Timeout time.Duration

	// Enabled 代码侧开关。最终生效 = code.Enabled && DB.Enabled(DB 行不存在时只看 code),
	// 做 AND 而不是只用 DB:这样**从代码里删掉任务一定等于停用**,不会因为 DB 残留行而复活。
	Enabled bool

	// NonIdempotent 声明本任务**跑两遍结果不一样**。
	//
	// ⚠️ 用反向命名(零值 false = 幂等)是为了让默认值落在安全的一侧:
	//    忘填的普通任务照常执行,只有显式声明非幂等的任务在降级模式下被跳过。
	//    大多数框架任务天然幂等:
	//      UPDATE t SET status='expired' WHERE status='active' AND expires_at < NOW()
	NonIdempotent bool

	// Handler 任务逻辑
	//
	// **必须尊重 ctx** —— ctx 到 deadline 会取消,不检查 ctx 的任务会一直跑,
	// 而它的锁到期后会被第二个副本抢走,两个副本并发写同一批数据。
	Handler func(ctx context.Context) error
}

// Override DB 侧对某个任务的运行时覆盖
type Override struct {
	JobKey      string
	Name        string
	Description string
	CronExpr    string
	Enabled     bool
	Sort        int
	IsActive    bool
}

// Effective 合并后的生效定义
type Effective struct {
	Job
	// CronExpr 生效的表达式(DB 覆盖优先)
	CronExpr string
	// CronOverridden 生效值来自 DB 覆盖而非代码默认值
	// 管理端要同时显示「生效值」和「代码默认值」,否则排查时不知道跑的是哪个
	CronOverridden bool
	// DBEnabled DB 侧开关(DB 无行时为 true)
	DBEnabled bool
}

// EnabledEffective 最终启停判定:code.Enabled && DB.Enabled
func (e Effective) EnabledEffective() bool { return e.Enabled && e.DBEnabled }
