// 短信验证码
package model

// SysSmsCode 短信验证码记录
// 业务约束:
//   - 同一 (Phone, Scene) 同时只有一条"未使用"记录
//   - 验证码一次性: Verify 后 UsedAt 被设置, 不可再用
//   - TTL 由 sms.rate_limit.code_ttl 控制(默认 600 秒 = 10 分钟)
//
// ⚠️ **M129(2026-10-31)订正**:此前这里写的是"默认 5 分钟" —— 那是
// **已删除的 `pkg/sms.VerifyStore`**(Redis 版)的口径。
// 真正在用的是这张表 + `service/auth_impl.go:VerifySms`,
// TTL 取 `config.SMS.RateLimit.CodeTTL`,配置模板里是 `600`。
// 口径写错会让运维按 5 分钟去排查一个其实是 10 分钟的行为。
type SysSmsCode struct {
	BaseModel
	Phone  string `gorm:"size:16;not null;index:idx_phone_scene,priority:1" json:"phone"`
	Scene  string `gorm:"size:32;not null;index:idx_phone_scene,priority:2" json:"scene"`
	Code   string `gorm:"size:8;not null" json:"code"` // 6 位数字
	SendAt Time   `gorm:"index" json:"send_at"`
	UsedAt *Time  `json:"used_at,omitempty"`
}

// TableName
func (SysSmsCode) TableName() string { return "sys_sms_code" }

// Scene 枚举
const (
	SmsSceneLogin         = "login"           // 短信登录
	SmsSceneResetPassword = "reset_password"  // 自助重置密码
	SmsSceneChangePhone   = "change_phone"    // 修改手机号
	SmsSceneViewFullPhone = "view_full_phone" // 查完整手机号(脱敏反查)
	// SmsSceneMFA 登录二次验证(M150)。
	//
	// ⚠️ **必须是独立 scene,不能复用 login** ——
	// 复用的话,MFA 登录流程发出的码同时能被 LoginBySms(按 scene=login 查)消费:
	// 拿到密码的攻击者触发一次 MFA 发码,再把同一条码从"短信登录"入口用掉,
	// MFA 就被整条绕过了。独立 scene ⇒ MFA 的码只有"密码登录第二步"这一个消费者。
	SmsSceneMFA = "mfa"
)
