// 短信发送审计 —— §1.9 #7 P1
//
// SysSmsCode 是"验证码记录"(业务级,频繁 upsert)
// SysSmsSendLog 是"发送尝试审计"(框架级,只追加,等保 / 故障排查)
//
// 区分:
//   - SysSmsCode:  验证码 + 状态(未用/已用/过期),参与登录流程
//   - SysSmsSendLog: 每一次 send 尝试 1 行(成功 + 失败),不参与业务,只读
//
// 用途:
//   - 等保审计:谁在什么时间往哪个手机号发什么场景的短信
//   - 故障排查:阿里云返回错误码、响应时长、request_id
//   - 限流决策依据(per phone+scene 真实发送频率)
package model

// SysSmsSendLog 短信发送审计日志
type SysSmsSendLog struct {
	BaseModel

	// 业务
	Phone  string `gorm:"size:16;not null;index" json:"phone"`
	Scene  string `gorm:"size:32;not null;index" json:"scene"`
	Code   string `gorm:"size:8" json:"code"` // 验证码(失败时为空,成功时为真实下发值,供反查)
	Provider string `gorm:"size:32" json:"provider"` // aliyun / console

	// 结果
	Success   bool   `gorm:"not null;index" json:"success"`
	ErrorMsg  string `gorm:"size:500" json:"error_msg"`
	CostMs    int    `json:"cost_ms"`     // 下发耗时(ms)
	UpstreamReqID string `gorm:"size:128" json:"upstream_req_id"` // 阿里云 request_id

	// 上下文
	IP        string `gorm:"size:45" json:"ip"`
	UserAgent string `gorm:"size:255" json:"user_agent"`
	RequestID string `gorm:"size:64;index" json:"request_id"`
	TraceID   string `gorm:"size:64;index" json:"trace_id"`
	UserID    uint64 `gorm:"index" json:"user_id,string"`
	IdentityID uint64 `gorm:"index" json:"identity_id,string"`
}

func (SysSmsSendLog) TableName() string { return "sys_sms_send_log" }

// SmsProvider 枚举
const (
	SmsProviderAliyun  = "aliyun"
	SmsProviderConsole = "console" // dev 模式 fallback,控制台打印
)
