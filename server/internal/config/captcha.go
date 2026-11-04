package config

// CaptchaConfig 图形验证码开关
//
// 拆成两个独立开关而不是一个总开关,是因为 4 个受影响的接口里验证码的**作用不同**:
//
//	登录类(密码登录 / 短信登录 / 短信重置密码)—— 验证码的作用是挡自动化撞库,
//	  开发联调时纯负担,关掉不影响任何安全边界以外的东西。
//	发短信(/public/auth/sms/send)—— 验证码在这里是**防短信刷量的闸门**,
//	  关掉等于把一个可被用来打钱(短信按条计费)的接口放开。
//
// 两个开关平级、没有优先级规则,比"总开关 + 分项覆盖"少一类用错。
//
// 为什么默认全是 true:
//
//	viper 的 bool 零值是 false,而 Load() 除了本项以外全不设默认值 ——
//	yaml 漏写 captcha 块,零值会让校验静默消失,把"配置遗漏"当成安全策略,
//	是最坏的失败方向。所以默认值必须由 Load() 用 SetDefault 显式给成 true
//	(见 config.go),只有显式写 false 才关。
type CaptchaConfig struct {
	// LoginEnabled 登录类接口的验证码。true=校验, false=跳过且前端不显示。
	// 覆盖:POST /public/auth/{login, login-sms, reset-password-sms}
	//
	// 开发联调最常关的就是这个 —— 登录不再需要 OCR 验证码。
	LoginEnabled bool `mapstructure:"login_enabled"`

	// SmsSendEnabled 发短信接口的验证码(防刷量闸门)。
	// 覆盖:POST /public/auth/sms/send
	//
	// **不要因为本地联调方便就关掉它** —— 即便 sms.provider=mock 不真发短信,
	// 这也是生产环境唯一的刷量闸门;关掉后只剩该接口自身 1 次/60 秒/IP 的限流。
	// 本地 mock 模式下嫌烦,可以只关 LoginEnabled。
	SmsSendEnabled bool `mapstructure:"sms_send_enabled"`
}

// CaptchaScene 验证码校验场景 —— 决定用哪个开关
type CaptchaScene int

const (
	// CaptchaSceneLogin 登录类:密码登录 / 短信登录 / 短信重置密码
	CaptchaSceneLogin CaptchaScene = iota
	// CaptchaSceneSmsSend 发短信(防刷量闸门)
	CaptchaSceneSmsSend
)

// String 便于日志里看得懂
func (s CaptchaScene) String() string {
	if s == CaptchaSceneSmsSend {
		return "sms_send"
	}
	return "login"
}

// Enabled 该场景当前是否要求验证码
//
// config 未加载时(单测直接 new handler)AppConfig 为 nil,直接取字段会 panic。
// 这种情况按"开"处理 —— 宁可多要求一次输入,也不在未初始化状态下放开校验。
func (c CaptchaConfig) Enabled(scene CaptchaScene) bool {
	if scene == CaptchaSceneSmsSend {
		return c.SmsSendEnabled
	}
	return c.LoginEnabled
}

// CaptchaEnabled 该场景是否要求验证码(全局兜底版,见 CaptchaConfig.Enabled)
func CaptchaEnabled(scene CaptchaScene) bool {
	if AppConfig == nil {
		return true
	}
	return AppConfig.Captcha.Enabled(scene)
}
