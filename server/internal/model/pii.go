// PII 解密审计 —— 框架级(§1.5 #5 P0)
//
// 与业务级 BizAuditFullInfoView 区分:
//   - BizAuditFullInfoView: 业务级"查看完整敏感信息"(带 SMS 二次验证),仅 user/owner
//   - SysPIIAccessLog:      框架级"所有 PII 字段的解密",任意资源类型任意字段
//
// 用途: 等保 / GDPR / 内部审计 —— "谁在什么时间看了哪个手机号/身份证/银行卡"
//
// 触发:
//   - 业务代码解密 PII 字段后调 piiaudit.Record(ctx, ...)
//   - 框架自动接管:Cipher.DecryptStringAudited(ctx, field, ...) 一行替代
//
// 性能:
//   - 异步批量写(每 100 条或 5s flush 一次)
//   - 不阻塞业务请求
//   - 失败重试 3 次,丢入 dead_letter channel 报警
package model

// SysPIIAccessLog 框架级 PII 字段访问日志
type SysPIIAccessLog struct {
	BaseModel

	// 访问者
	UserID     uint64 `gorm:"index;not null" json:"user_id,string"`
	IdentityID uint64 `gorm:"index" json:"identity_id,string"`
	RoleCode   string `gorm:"size:64" json:"role_code"`

	// 操作
	Action string `gorm:"size:32;index" json:"action"` // read / list / export / decrypt

	// 被访问资源
	ResourceType string `gorm:"size:32;index" json:"resource_type"` // owner / asset / user / contract / ...
	ResourceID   uint64 `gorm:"index" json:"resource_id,string"`

	// 具体字段
	Field string `gorm:"size:32;index" json:"field"` // phone / id_card / email / bank_card / address / ...

	// 上下文
	IP        string `gorm:"size:45" json:"ip"`
	UserAgent string `gorm:"size:255" json:"user_agent"`
	RequestID string `gorm:"size:64;index" json:"request_id"`
	TraceID   string `gorm:"size:64;index" json:"trace_id"`

	// 反查(运营 / 风控)
	Reason string `gorm:"size:500" json:"reason"`
}

func (SysPIIAccessLog) TableName() string { return "sys_pii_access_log" }

// PIIAction 常量
const (
	PIIActionRead    = "read"    // 单条查看
	PIIActionList    = "list"    // 列表(每行算 1 条)
	PIIActionExport  = "export"  // 导出(整批)
	PIIActionDecrypt = "decrypt" // 单纯解密(导出到下游系统等)
)

// PIIField 常量(常用 PII 字段名)
const (
	PIIFieldPhone    = "phone"
	PIIFieldIDCard   = "id_card"
	PIIFieldEmail    = "email"
	PIIFieldBankCard = "bank_card"
	PIIFieldAddress  = "address"
	PIIFieldName     = "name" // 姓名
)
