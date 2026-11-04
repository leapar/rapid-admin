// AuditService 审计日志查询 + 供应商
// 文档 §10.4 + §6.7
package service

import (
	"context"

	"rapid-admin/internal/model"
)

// OperationLogQuery 操作日志
type OperationLogQuery struct {
	model.PageRequest
	UserID  uint64 `json:"user_id,string"`
	Module  string `json:"module"`
	Action  string `json:"action"`
	StartAt *int64 `json:"start_at"`
	EndAt   *int64 `json:"end_at"`
	IP      string `json:"ip"`
	// Keyword 关键字 —— 命中 用户名 / 模块 / 动作 / 描述 任一列
	//
	// ⚠️ **2026-10-30 新增**:此前前端"搜索"框发的是 keyword,
	//   而这里根本没有这个字段 —— **后端完全不读**,
	//   页面表现是"输了关键字没反应"(而且不报错、HTTP 200)。
	Keyword string `json:"keyword"`
	// Status 1 成功 / 0 失败
	//
	// ⚠️ **必须用指针**:0 在 model 里是"失败",在"筛不筛"里却是"不筛" ——
	//   用 int 就没法表达"只看失败"了(跟 LoginLogQuery.Success 同一个道理)。
	Status *int `json:"status"`
}

// DataChangeLogQuery 数据变更日志
type DataChangeLogQuery struct {
	model.PageRequest
	TableName string `json:"table_name"`
	RecordID  uint64 `json:"record_id,string"`
	UserID    uint64 `json:"user_id,string"`
	StartAt   *int64 `json:"start_at"`
	EndAt     *int64 `json:"end_at"`
	// Keyword 关键字 —— 命中 表名(2026-10-30 新增,理由同 OperationLogQuery.Keyword)
	Keyword string `json:"keyword"`
	// Operation 1 新增 / 2 修改 / 3 删除;nil = 不筛
	//
	// ⚠️ 同样必须用指针:0 不是合法操作类型,但 int 的零值却是"不筛"。
	Operation *int `json:"operation"`
}

// LoginLogQuery 登录日志
type LoginLogQuery struct {
	model.PageRequest
	UserID  uint64 `json:"user_id,string"`
	Success *bool  `json:"success"`
	IP      string `json:"ip"`
	StartAt *int64 `json:"start_at"`
	EndAt   *int64 `json:"end_at"`
	// Keyword 关键字 —— 命中 用户名(2026-10-30 新增,理由同 OperationLogQuery.Keyword)
	Keyword string `json:"keyword"`
}

// SystemLogQuery 系统运行日志查询(慢 SQL / 慢接口 / 5xx / panic)
type SystemLogQuery struct {
	model.PageRequest
	Level  string `json:"level"` // info / warn / error(字典 sys_log_level)
	Source string `json:"source"` // slow_sql / slow_http / http_error / panic
	// Keyword 关键字 —— 命中 path / message 任一列(口径同 OperationLogQuery.Keyword)
	Keyword string `json:"keyword"`
	StartAt *int64 `json:"start_at"`
	EndAt   *int64 `json:"end_at"`
}

// SystemLogEntry 系统运行日志写入项
//
// 由三处钩子统一投喂:slowquery(慢 SQL)、AccessLog(慢接口 / 5xx)、Recover(panic)。
// 约束见 RecordSystemLog:异步、超时兜底、绝不影响业务请求。
type SystemLogEntry struct {
	Level   string // info / warn / error
	Source  string // slow_sql / slow_http / http_error / panic
	Method  string // HTTP 方法(SQL 类为空)
	Path    string // 接口路径(SQL 类为表名)
	Message string // 摘要
	Detail  string // 详情(完整 SQL / panic 栈)
	TraceID string
	UserID  uint64
	DurationMs int64
}

// FullInfoViewQuery 敏感信息查看审计
type FullInfoViewQuery struct {
	model.PageRequest
	UserID  uint64 `json:"user_id,string"`
	OwnerID uint64 `json:"owner_id,string"`
	Field   string `json:"field"`
	StartAt *int64 `json:"start_at"`
	EndAt   *int64 `json:"end_at"`
}

// DataExportLogQuery 业务数据导出审计查询
type DataExportLogQuery struct {
	model.PageRequest
	UserID       uint64 `json:"user_id,string"`
	BusinessType string `json:"business_type"`
	StartAt      *int64 `json:"start_at"`
	EndAt        *int64 `json:"end_at"`
}

// PIIAccessLogQuery 框架级 PII 解密访问日志查询 (§1.5 #5 P0)
//
// 与 FullInfoViewQuery 区分:
//   - FullInfoView: 业务级"查看完整手机/身份证"(带 SMS 二次验证),仅 owner
//   - PIIAccess:    框架级"任何 PII 字段解密",任意资源任意字段
type PIIAccessLogQuery struct {
	model.PageRequest
	UserID       uint64 `json:"user_id,string"`
	ResourceType string `json:"resource_type"`
	ResourceID   uint64 `json:"resource_id,string"`
	Field        string `json:"field"`
	Action       string `json:"action"`
	StartAt      *int64 `json:"start_at"`
	EndAt        *int64 `json:"end_at"`
}

// SmsSendLogQuery 短信发送审计查询 (§1.9 #7 P1)
type SmsSendLogQuery struct {
	model.PageRequest
	Phone    string `json:"phone"`
	Scene    string `json:"scene"`
	Provider string `json:"provider"`
	Success  *bool  `json:"success"`
	StartAt  *int64 `json:"start_at"`
	EndAt    *int64 `json:"end_at"`
}

// CryptoAuditLogQuery 密钥使用审计查询 (§1.5 #8 P1)
type CryptoAuditLogQuery struct {
	model.PageRequest
	UserID       uint64 `json:"user_id,string"`
	KeyID        string `json:"key_id"`
	Operation    string `json:"operation"` // encrypt / decrypt / blind-index
	Algorithm    string `json:"algorithm"`
	ResourceType string `json:"resource_type"`
	ResourceID   uint64 `json:"resource_id,string"`
	Success      *bool  `json:"success"`
	StartAt      *int64 `json:"start_at"`
	EndAt        *int64 `json:"end_at"`
}

// AuditService 审计
type AuditService interface {
	// 查询
	ListOperationLogs(ctx context.Context, q OperationLogQuery) (*model.PageResponse, error)
	GetOperationLog(ctx context.Context, id uint64) (*model.SysOperationLog, error)

	ListDataChangeLogs(ctx context.Context, q DataChangeLogQuery) (*model.PageResponse, error)
	GetDataChangeLog(ctx context.Context, id uint64) (*model.SysDataChangeLog, error)

	ListLoginLogs(ctx context.Context, q LoginLogQuery) (*model.PageResponse, error)

	// 系统运行日志(2026-11-03)—— 慢 SQL / 慢接口 / 5xx / panic
	ListSystemLogs(ctx context.Context, q SystemLogQuery) (*model.PageResponse, error)

	ListFullInfoViews(ctx context.Context, q FullInfoViewQuery) (*model.PageResponse, error)

	ListDataExports(ctx context.Context, q DataExportLogQuery) (*model.PageResponse, error)

	// 框架级 PII 解密访问日志(§1.5 #5 P0)
	ListPIIAccessLogs(ctx context.Context, q PIIAccessLogQuery) (*model.PageResponse, error)
	// 密钥使用审计(§1.5 #8 P1)
	ListCryptoAuditLogs(ctx context.Context, q CryptoAuditLogQuery) (*model.PageResponse, error)

	// 短信发送审计(§1.9 #7 P1)
	ListSmsSendLogs(ctx context.Context, q SmsSendLogQuery) (*model.PageResponse, error)

	// 写入(由中间件统一调用,业务侧不直接调)
	RecordOperation(ctx context.Context, log *model.SysOperationLog) error
	RecordDataChange(ctx context.Context, log *model.SysDataChangeLog) error
	RecordLogin(ctx context.Context, log *model.SysLoginLog) error
	// RecordSystemLog 系统运行日志写入 —— 实现层保证异步 + 不 panic 不阻塞,
	// 调用方(中间件钩子 / slowquery)可放心在请求主路径上调用。
	RecordSystemLog(entry *SystemLogEntry)
	RecordFullInfoView(ctx context.Context, log *model.BizAuditFullInfoView) error

	// 2026-10-19 M62:身份/角色切换审计(高危操作,业务专用,handler 显式调)
	RecordRoleSwitch(ctx context.Context, log *model.RoleSwitchAudit) error
	RecordDataExport(ctx context.Context, log *model.BizAuditDataExportLog) error

	// 导出
	ExportOperationLogs(ctx context.Context, q OperationLogQuery) (string, error) // 返回 OSS key
}
