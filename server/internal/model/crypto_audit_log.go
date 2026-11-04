package model

// SysCryptoAuditLog 密钥使用审计(§1.5 #8 P1)
//
// 落库字段:
//   - KeyID / KeyVersion:用了哪个密钥/版本(密钥轮转后能区分"老 key 还在被用")
//   - Algorithm:加密算法(aes-256-gcm / hmac-sha256 / etc.)
//   - Operation:操作(encrypt / decrypt / blind-index)
//   - ResourceType / ResourceID:被加密/解密的资源(冗余索引,便于排查)
//   - ByteCount:密文/明文长度(看异常大流量)
//   - Success / ErrorMsg:失败也落库(看到底哪个 key 解不开)
//   - RequestID / TraceID:跟 access_log 串联
//   - UserID / IdentityID / RoleCode:谁在用
//   - IP / UserAgent:谁的网络
//
// 索引:
//   - idx_user_time (user_id, created_at):按人按时间查
//   - idx_key_time  (key_id, created_at):按 key 排查异常
//   - idx_resource  (resource_type, resource_id):按资源排查
//   - idx_time      (created_at):兜底按时间
type SysCryptoAuditLog struct {
	BaseModel
	KeyID        string `gorm:"size:64;index:idx_key_time" json:"key_id"`
	KeyVersion   int    `gorm:"index:idx_key_time" json:"key_version"`
	Algorithm    string `gorm:"size:32" json:"algorithm"`
	Operation    string `gorm:"size:16;index:idx_op_time" json:"operation"`
	ResourceType string `gorm:"size:32;index:idx_resource" json:"resource_type"`
	ResourceID   uint64 `gorm:"index:idx_resource" json:"resource_id,string"`
	ByteCount    int    `json:"byte_count"`
	Success      bool   `gorm:"index:idx_op_time" json:"success"`
	ErrorMsg     string `gorm:"size:255" json:"error_msg"`
	IP           string `gorm:"size:64" json:"ip"`
	UserAgent    string `gorm:"size:255" json:"user_agent"`
	RequestID    string `gorm:"size:64;index" json:"request_id"`
	TraceID      string `gorm:"size:64;index" json:"trace_id"`
	UserID       uint64 `gorm:"index:idx_user_time" json:"user_id,string"`
	IdentityID   uint64 `json:"identity_id,string"`
	RoleCode     string `gorm:"size:32" json:"role_code"`
}

// TableName 显式表名
func (SysCryptoAuditLog) TableName() string { return "sys_crypto_audit_log" }
