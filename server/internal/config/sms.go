// 短信配置
package config

type SMSTemplate struct {
	Login         string `mapstructure:"login"`
	ViewFullPhone string `mapstructure:"view_full_phone"`
	ResetPassword string `mapstructure:"reset_password"`
	ChangePhone   string `mapstructure:"change_phone"`
}

type SMSRateLimit struct {
	PerMinute int `mapstructure:"per_minute"`
	PerDay    int `mapstructure:"per_day"`
	// WindowSeconds 单条短信发送冷却秒数(默认 60s)
	WindowSeconds int `mapstructure:"window_seconds"`
	// CodeTTL 短信验证码有效期(秒,默认 600)
	//
	// ⚠️ **600 = 10 分钟,是跟着阿里云验证码模板的文案走的**:模板正文写着
	// 「该验证码10分钟内有效」——如果自报备模板改了文案,这里要跟着改。
	// 服务端只活 5 分钟的话,用户按短信上的提示在第 6 分钟重试就会失败。
	CodeTTL int `mapstructure:"code_ttl"`
}

type SMSConfig struct {
	Provider     string       `mapstructure:"provider"`
	AccessKey    string       `mapstructure:"access_key"`
	AccessSecret string       `mapstructure:"access_secret"`
	SignName     string       `mapstructure:"sign_name"`
	Region       string       `mapstructure:"region"`
	Templates    SMSTemplate  `mapstructure:"templates"`
	RateLimit    SMSRateLimit `mapstructure:"rate_limit"`

	// TemplateGuard 短信模板审核守卫策略(off / warn / block)—— M115
	//
	// ⚠️ **默认 block,写错也落到 block** —— 见 smstpl.ParsePolicy 的注释:
	//	"没检查"意味着整套模板审核流静默失效,而"检查"最坏结果是拦掉
	//	一张确定不可用的模板。**失效比误拦危险**。
	//
	// ⚠️ **刻意不进配置中心** —— 配置中心的 aliyun.sms.* 是"凭证 / 模板号"这类
	//	运营必须能改的东西;守卫策略是**安全开关**,能从面板一键关掉的话,
	//	它就等于没有。改它只能改部署配置(改完重启)。
	//
	// 取值:
	//   off   —— 完全不检查(不推荐)
	//   warn  —— 检查但只告警,不拦(用于"状态可能陈旧、怕误拦"的场景)
	//   block —— 拦下并明确报出是哪张模板、为什么(默认)
	TemplateGuard string `mapstructure:"template_guard"`
}
