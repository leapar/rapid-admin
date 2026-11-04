// 短信下发 —— Sender 抽象
//
// 设计要点:
//   - 上层只调 Sender.Send, 不关心运营商
//   - 通过 factory 按 config.SMS.Provider 选择实现
//   - 模板/签名/AccessKey 都从 config.SMS 读
package msn

import "context"

// Scene 业务场景
type Scene string

const (
	SceneLogin         Scene = "login"
	SceneResetPassword Scene = "reset_password"
	SceneChangePhone   Scene = "change_phone"
	SceneViewFullPhone Scene = "view_full_phone"
	// SceneMFA 登录二次验证(M150)。模板复用登录那张(见 smstpl.TemplateCode)。
	SceneMFA Scene = "mfa"
)

// AsScene 接受任意 string 并归一为 Scene
func AsScene(s string) Scene { return Scene(s) }

// Sender 短信下发接口
type Sender interface {
	// Send 发送验证码
	//   phone  11 位手机号
	//   code   验证码(纯数字, 不带单位/说明)
	//   scene  业务场景字符串(见 Scene 常量)
	Send(ctx context.Context, phone, code, scene string) error
	// Provider 标识实现方(aliyun / console),用于 smsaudit 写日志
	Provider() string
}
