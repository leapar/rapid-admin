// 平台配置中心 4 张表
// 文档 §6.6 / §14.5
package model

// SysConfig 平台配置主表
type SysConfig struct {
	BaseModel

	// 业务标识
	ConfigKey string `gorm:"size:128;uniqueIndex;not null" json:"config_key"` // aliyun.oss.access_key
	ConfigGroup string `gorm:"size:32;index;not null" json:"config_group"`  // aliyun / wechat / sms / etc.
	ConfigName  string `gorm:"size:64;not null" json:"config_name"`
	Description string `gorm:"size:500" json:"description"`

	// 类型与值
	ValueType    ConfigValueType `gorm:"size:16;not null" json:"value_type"`
	ValueEncrypted string `gorm:"type:text" json:"-"`                     // AES-256-GCM(Base64)
	ValuePreview  string `gorm:"size:255" json:"value_preview"`          // 脱敏回显
	IsSensitive   bool `gorm:"default:false" json:"is_sensitive"`

	// 校验
	RegexValidate string `gorm:"size:255" json:"regex_validate"`
	MinLength     int    `json:"min_length"`
	MaxLength     int    `json:"max_length"`

	// 元数据
	DefaultValueEncrypted string `gorm:"type:text" json:"-"`
	IsRequired bool `gorm:"default:false" json:"is_required"`
	IsReadonly bool `gorm:"default:false" json:"is_readonly"`
	EnvVarName string `gorm:"size:128" json:"env_var_name"`
	Sort       int    `gorm:"default:0" json:"sort"`
	IsActive   bool   `gorm:"default:true;index" json:"is_active"`
	Remarks    string `gorm:"size:500" json:"remarks"`

	// 审计
	Version        int    `gorm:"default:1" json:"version"`
	LastChangedBy  uint64 `gorm:"index" json:"last_changed_by,string"`
	LastChangedAt  *Time  `json:"last_changed_at"`
	LastAuditID    uint64 `gorm:"index" json:"last_audit_id,string"`
}

func (SysConfig) TableName() string { return "sys_config" }

// SysConfigChangeRequest 配置变更申请(双签)
type SysConfigChangeRequest struct {
	BaseModel
	RequestNo string `gorm:"size:32;uniqueIndex;not null" json:"request_no"` // CHG-20261016-0001

	// 变更目标
	ConfigID  uint64 `gorm:"index;not null" json:"config_id,string"`
	ConfigKey string `gorm:"size:128;not null" json:"config_key"`

	// 变更内容
	OldValueEncrypted string `gorm:"type:text" json:"-"` // 用于回滚与对比
	NewValueEncrypted string `gorm:"type:text;not null" json:"-"`
	NewValuePreview   string `gorm:"size:255" json:"new_value_preview"`
	DiffSummary       string `gorm:"type:text" json:"diff_summary"`

	// 流程 - 发起
	ApplicantID      uint64 `gorm:"index;not null" json:"applicant_id,string"`
	ApplicantRole    string `gorm:"size:64;not null" json:"applicant_role"`
	ApplicantSmsCode string `gorm:"size:6" json:"-"` // 已用即焚
	ApplicantReason  string `gorm:"size:500;not null" json:"applicant_reason"`
	ApplicantIP      string `gorm:"size:45" json:"applicant_ip"`
	ApplicantUA      string `gorm:"size:255" json:"applicant_ua"`
	AppliedAt        Time   `json:"applied_at"`

	// 流程 - 复核
	ReviewerID    uint64 `gorm:"index" json:"reviewer_id,string"`
	ReviewerRole  string `gorm:"size:64" json:"reviewer_role"`
	ReviewSmsCode string `gorm:"size:6" json:"-"`
	ReviewComment string `gorm:"size:500" json:"review_comment"`
	ReviewedAt    *Time  `json:"reviewed_at"`

	// 生效
	EffectiveType ConfigEffectiveType `gorm:"size:16;not null" json:"effective_type"`
	EffectiveAt   *Time  `json:"effective_at"`
	ActivatedAt   *Time  `json:"activated_at"`

	// 灰度(可选,文档 §14.3)
	GrayPercent int    `gorm:"default:100" json:"gray_percent"` // 1-100
	GrayHosts   string `gorm:"type:json" json:"gray_hosts"`     // 灰度实例列表

	// 通知
	NotifyWebhook string `gorm:"size:500" json:"notify_webhook"`
	NotifySentAt  *Time  `json:"notify_sent_at"`

	// 状态机
	Status          ConfigChangeStatus `gorm:"size:16;default:'pending';index" json:"status"`
	RejectionReason string             `gorm:"size:500" json:"rejection_reason"`

	// 回滚
	RollbackToRequestID uint64 `gorm:"index" json:"rollback_to_request_id,string"`
	RolledBackAt        *Time  `json:"rolled_back_at"`
}

func (SysConfigChangeRequest) TableName() string { return "sys_config_change_request" }

// SysConfigAudit 配置审计(全量记录)
//   2026-10-18 收窄:
//     - 删除 Reason 字段 — UI 没输入框,前端硬塞"批量保存 xxx"无意义
//     - 删除 ChangeReqID 字段 — 双签工作流移除后,此字段永远 = 0,前端无意义显示"关联工单ID"
//     - action 枚举只剩 "update" — create/delete/view 后端接口已全删,前端 UI 只触发 PATCH /configs/:id
//   schema 保留 action 字段(老数据仍可能是 create/delete/view 之一,前端 Tag 兜底渲染)
type SysConfigAudit struct {
	BaseModel
	ConfigID  uint64 `gorm:"index;not null" json:"config_id,string"`
	ConfigKey string `gorm:"size:128;index" json:"config_key"`
	Action    string `gorm:"size:32;index" json:"action"`
	UserID    uint64 `gorm:"index" json:"user_id,string"`
	UserRole  string `gorm:"size:64" json:"user_role"`
	IP        string `gorm:"size:45" json:"ip"`
	Before    string `gorm:"type:text" json:"before"` // 脱敏后
	After     string `gorm:"type:text" json:"after"`
}

func (SysConfigAudit) TableName() string { return "sys_config_audit" }

// SysConfigExportLog 配置导入/导出日志(替换原 HotReload 表)
type SysConfigExportLog struct {
	BaseModel
	OperatorID   uint64 `gorm:"index;not null" json:"operator_id,string"`
	OperatorName string `gorm:"size:64" json:"operator_name"`
	ExportType   string `gorm:"size:16;index" json:"export_type"` // export / import / preview
	ExportFormat string `gorm:"size:16" json:"export_format"`     // json / yaml / xlsx
	RecordCount  int    `json:"record_count"`
	FileURL      string `gorm:"size:500" json:"file_url"`
	IP           string `gorm:"size:45" json:"ip"`
	UA           string `gorm:"size:255" json:"ua"`
	Status       string `gorm:"size:16;index" json:"status"` // success / failed
	ErrorMessage string `gorm:"size:500" json:"error_message"`
}

func (SysConfigExportLog) TableName() string { return "sys_config_export_log" }

// ConfigAuditAction 审计动作枚举(用于 sys_config_audit.action)
//   2026-10-18 收窄:
//     - 系统配置只走 PATCH /configs/:id 一条写通道(用户不会创建/删除配置项,无明文查看 endpoint)
//     - 唯一活 action = "update";create/delete/view 后端接口全删,seed/前端不会再写这 3 种
//     - DB schema 保留 action 字段(老数据兼容),前端 audit.tsx 的 Tag 颜色 map 兜底渲染未知值
const (
	ConfigAuditActionUpdate = "update"
)
