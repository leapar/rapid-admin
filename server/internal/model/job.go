// Package model 定时任务数据模型 —— M67
//
// 设计原则(与字典 / 系统配置同一条规矩):
//
//	handler 只在代码里注册(cronx.Register),DB 里凭空长不出任务。
//	DB(sys_job)只存**运行态覆盖**:启用开关、cron 表达式覆盖值、中文名。
//	删任务 = 从代码里删;DB 残留行由 cronx 启动时 reconcile 检出并置 is_active=0。
package model

import "time"

// JobTriggerType 触发来源
type JobTriggerType string

const (
	// JobTriggerSchedule 由 cron 表达式定时触发
	JobTriggerSchedule JobTriggerType = "schedule"
	// JobTriggerManual 后台管理端手动触发
	JobTriggerManual JobTriggerType = "manual"
)

// JobStatus 执行状态
type JobStatus string

// 只有三态,不设 skipped:抢锁失败**不写记录**(见 cronx/scheduler.go Trigger 注释),
// 一条「我被跳过了」的记录在 N 副本下会把历史表冲垮。
const (
	// JobStatusRunning 执行中
	JobStatusRunning JobStatus = "running"
	// JobStatusSuccess 成功
	JobStatusSuccess JobStatus = "success"
	// JobStatusFailed 失败(含 panic / 超时 / 进程异常退出回收)
	JobStatusFailed JobStatus = "failed"
)

// SysJob 定时任务运行态覆盖表
//
// ⚠️ 这张表**不是任务定义表**,是「代码注册值的运行时覆盖」。
// 没有 job_key 为空的行 —— 一行都没有代码注册,那行就是脏数据。
type SysJob struct {
	BaseModel
	JobKey      string `gorm:"column:job_key;size:64;uniqueIndex;not null" json:"job_key"`
	Name        string `gorm:"column:name;size:128" json:"name"`               // 中文名,可覆盖代码默认值
	Description string `gorm:"column:description;size:512" json:"description"` // 备注
	CronExpr    string `gorm:"column:cron_expr;size:128" json:"cron_expr"`     // 空 = 用代码默认值
	Enabled     bool   `gorm:"column:enabled;default:true" json:"enabled"`     // 与 code.Enabled 做 AND
	Sort        int    `gorm:"column:sort;default:0" json:"sort"`
	IsActive    bool   `gorm:"column:is_active;default:true;index" json:"is_active"` // 软删/停用标记
}

// TableName 表名
func (SysJob) TableName() string { return "sys_job" }

// SysJobRun 定时任务执行记录表
//
// 这张表同时承担两个职责:
//  1. 后台管理端「执行记录」页的数据源
//  2. **分布式去重的 Layer 0** —— `status='running'` 的行天然就是「谁在跑」
//     (cronx.Store.HasRunning)。表本来就要建,去重能力是白送的,
//     且不依赖 Redis。
type SysJobRun struct {
	BaseModel
	JobKey      string         `gorm:"column:job_key;size:64;index:idx_job_started,priority:1" json:"job_key"`
	TriggerType JobTriggerType `gorm:"column:trigger_type;size:16" json:"trigger_type"`
	Status      JobStatus      `gorm:"column:status;size:16;index" json:"status"`
	StartedAt   time.Time      `gorm:"column:started_at;index:idx_job_started,priority:2" json:"started_at"`
	FinishedAt  *time.Time     `gorm:"column:finished_at" json:"finished_at"`
	DurationMs  int64          `gorm:"column:duration_ms" json:"duration_ms"`
	// Error 截断 2KB:一次 panic 的 stack 轻松几十 KB,不截断单条就能把表撑爆
	Error      string `gorm:"column:error;size:2048" json:"error"`
	InstanceID string `gorm:"column:instance_id;size:64" json:"instance_id"`
	// TraceID 接 OTel,便于从历史记录跳到 Grafana
	TraceID  string `gorm:"column:trace_id;size:64" json:"trace_id"`
	Operator string `gorm:"column:operator;size:64" json:"operator"` // 手动触发时的操作人
	// Degraded 本次执行时分布式锁不可用(只有 Layer 0 在挡)
	Degraded bool `gorm:"column:degraded" json:"degraded"`
}

// TableName 表名
func (SysJobRun) TableName() string { return "sys_job_run" }
