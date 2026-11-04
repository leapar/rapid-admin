// 邮件配置(§1.9 #9 P2)
//
// ⚠️ **provider 为空串 = 不装配邮件通道** —— 与 sms.provider 的"空串落到 console"
//
//	刻意不同,理由是**失败方向不一样**:
//
//	- sms 验证码**必须**有去处(哪怕只是打日志),空串落 console 是安全方向
//	- 邮件是**额外交付**(站内信之外的补充),没配就是没配;
//	  悄悄落到 console 会让运维以为"邮件发出去了",实际上一封都没发
//
// ⚠️ **"配置了但没生效"是最坏的失败方向** —— 所以未知的 provider 值
//
//	在 buildEmailSender 里会打 Warn 并**拒绝装配**(而不是静默落 console)。
package config

// EmailSMTPConfig 通用 SMTP 配置
//
//	host + port 决定走哪种加密:
//	  465 → use_tls: true
//	  587 → use_starttls: true
//	  25  → 两个都 false(明文,只在自建内网用)
type EmailSMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	// UseTLS 直接 TLS(465)
	UseTLS bool `mapstructure:"use_tls"`
	// UseSTARTTLS STARTTLS(587)
	UseSTARTTLS bool `mapstructure:"use_starttls"`
}

// EmailAliyunConfig 阿里云 DirectMail 配置
type EmailAliyunConfig struct {
	AccessKey    string `mapstructure:"access_key"`
	AccessSecret string `mapstructure:"access_secret"`
	// AccountName 发信地址(需在阿里云控制台验证过)
	AccountName string `mapstructure:"account_name"`
	// FromAlias 发信人显示名(可选)
	FromAlias string `mapstructure:"from_alias"`
	// Region 默认 cn-hangzhou
	Region string `mapstructure:"region"`
}

// EmailConfig 邮件通道配置
type EmailConfig struct {
	// Provider: ""(不装配) / console(仅日志) / smtp / aliyun
	Provider string `mapstructure:"provider"`
	// From 发件邮箱。
	// 缺省时由 provider 决定:smtp 取 smtp.username,aliyun 取 aliyun.account_name
	From string `mapstructure:"from"`
	// FromName 发件人显示名(可选)
	FromName string `mapstructure:"from_name"`
	// Timeout 单次发送超时秒数(0 = 用 provider 默认 30s)
	Timeout int `mapstructure:"timeout"`
	// BaseURL 站点外链前缀。邮件里的链接必须是**绝对地址** ——
	// 收件人的邮件客户端没有"当前站点"的概念,相对路径点开是坏的。
	BaseURL string `mapstructure:"base_url"`

	SMTP   EmailSMTPConfig   `mapstructure:"smtp"`
	Aliyun EmailAliyunConfig `mapstructure:"aliyun"`
}
