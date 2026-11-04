// 邮件 / 推送 配置块的特征测试
//
// 存在的理由同 file_test.go,而且**比 file 那条更隐蔽**:
//
//	yaml 里写错一个键名 → viper 解析成空 → 落到默认值 → **零症状**。
//	而邮件这一段的默认值是"provider = 空串 = 不装配通道",
//	于是"键名写错了"和"运维忘了配"产生的日志**一模一样**
//	(启动时都只说"邮件通道未配置")。
//
//	换句话说:拼错 `email.smtp.user_name`(真名 username)之后,
//	平台会安静地**不发邮件**,而且没有任何地方会告诉你原因。
//	所以这里断言的**不是解析后的值,而是"这个键必须真的写在文件里"**
//	(AppViper.InConfig 只认配置文件本身,不看 SetDefault)。
//
//	值的那一遍也留着 —— 它是第二道,专抓 mapstructure tag 与 yaml 键对不上
//	(键在文件里、tag 拼错 → 同样是空值)。
package config

import (
	"path/filepath"
	"testing"
)

// notifyFileKeys email / push 块里**必须显式出现在 yaml 里**的键。
//
// 为什么必须显式而不是靠 SetDefault:默认值的存在是为了"漏配也能跑",
// 但它同时把"写错了键名"变成一件查不出来的事。
var notifyFileKeys = []string{
	"email.provider",
	"email.from",
	"email.from_name",
	"email.base_url",
	"email.timeout",
	"email.smtp.host",
	"email.smtp.port",
	"email.smtp.username",
	"email.smtp.password",
	"email.smtp.use_tls",
	"email.smtp.use_starttls",
	"email.aliyun.access_key",
	"email.aliyun.access_secret",
	"email.aliyun.account_name",
	"email.aliyun.from_alias",
	"email.aliyun.region",
	"push.provider",
	"push.timeout",
	"push.target_max",
	"push.aliyun_emns.access_key",
	"push.aliyun_emns.access_secret",
	"push.aliyun_emns.app_key",
	"push.aliyun_emns.region",
	"push.aliyun_emns.default_apns_env",
}

// TestLoad_NotifyKeysAreDeclaredInYaml 两份模板都过一遍 ——
// 免得"改了一个忘了另一个"(dev 模板和生产模板各写一遍是本仓的既有做法)。
func TestLoad_NotifyKeysAreDeclaredInYaml(t *testing.T) {
	for _, name := range []string{"config.dev.yaml.example", "config.example.yaml"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join("..", "..", "configs", name)
			if err := Load(p); err != nil {
				t.Fatalf("load %s: %v", p, err)
			}
			for _, k := range notifyFileKeys {
				if !AppViper.InConfig(k) {
					t.Errorf("%s 里没有 %s —— 键名写错会静默落回默认值", name, k)
				}
			}
		})
	}
}

// TestLoad_NotifyBlockIsFullyParsed 对齐生产口径(config.example.yaml)。
//
// 这一遍抓的是**另一个方向**:键在文件里,但 mapstructure tag 拼错了
// (比如 yaml 写 use_tls、tag 写 usetls)—— 键检查会过,值却是零值。
func TestLoad_NotifyBlockIsFullyParsed(t *testing.T) {
	p := filepath.Join("..", "..", "configs", "config.example.yaml")
	if err := Load(p); err != nil {
		t.Fatalf("load %s: %v", p, err)
	}
	e := AppConfig.Email
	push := AppConfig.Push

	// 生产模板的口径:两个 provider 都是空串 = **两条通道都不装配**。
	// 这是本仓的默认值策略,改模板时如果顺手改成了 console,
	// 生产环境就会"看起来在发邮件"而实际一封都没发出去。
	if e.Provider != "" {
		t.Errorf("email.provider = %q, want 空串(生产模板不装配邮件通道)", e.Provider)
	}
	if push.Provider != "" {
		t.Errorf("push.provider = %q, want 空串(生产模板不装配推送通道)", push.Provider)
	}

	if e.Timeout != 30 || push.Timeout != 30 {
		t.Errorf("timeout = email:%d push:%d, want 30 / 30", e.Timeout, push.Timeout)
	}
	if push.TargetMax != 100 {
		t.Errorf("push.target_max = %d, want 100(EMNS 硬限制)", push.TargetMax)
	}
	if e.FromName == "" {
		t.Error("email.from_name 为空 —— 邮件发件人会显示成裸地址")
	}
	if e.SMTP.Port != 465 || !e.SMTP.UseTLS || e.SMTP.UseSTARTTLS {
		t.Errorf("smtp = port:%d use_tls:%v use_starttls:%v, want 465 / true / false",
			e.SMTP.Port, e.SMTP.UseTLS, e.SMTP.UseSTARTTLS)
	}
	if e.Aliyun.Region != "cn-hangzhou" || push.AliyunEMNS.Region != "cn-hangzhou" {
		t.Errorf("region = email:%q push:%q, want cn-hangzhou / cn-hangzhou",
			e.Aliyun.Region, push.AliyunEMNS.Region)
	}
	if push.AliyunEMNS.DefaultAPNSEnv != "PROD" {
		t.Errorf("push.aliyun_emns.default_apns_env = %q, want PROD(生产模板)",
			push.AliyunEMNS.DefaultAPNSEnv)
	}
	// base_url 留空 = 邮件里的相对链接**会被丢掉**(好过给一个点不开的链接)。
	if e.BaseURL != "" {
		t.Errorf("email.base_url = %q, want 空串(生产模板不写站点地址)", e.BaseURL)
	}
}

// TestLoad_DevNotifyBlockIsConsole 锁定 dev 模板的口径。
//
// ⚠️ dev 用 console 是**故意的**:dev 环境本来就没有真 SMTP,
//
//	邮件打到日志里正好用来验证"投递链路走通了"。
//	改回空串会让 dev 完全看不到邮件链路,改非 console 会让人以为
//	dev 真的能收到邮件。
func TestLoad_DevNotifyBlockIsConsole(t *testing.T) {
	p := filepath.Join("..", "..", "configs", "config.dev.yaml.example")
	if err := Load(p); err != nil {
		t.Fatalf("load %s: %v", p, err)
	}
	e := AppConfig.Email
	push := AppConfig.Push

	if e.Provider != "console" {
		t.Errorf("email.provider = %q, want console(dev 模板用 console 验证链路)", e.Provider)
	}
	if e.From == "" {
		t.Error("email.from 为空 —— dev 模板给了发件地址,解析却没拿到")
	}
	if e.BaseURL != "http://localhost:5173" {
		t.Errorf("email.base_url = %q, want http://localhost:5173", e.BaseURL)
	}
	// ⚠️ **推送在 dev 也不装配** —— 推送要设备令牌,平台侧没有那张登记册。
	//   dev 配成 console 只会让人以为"推送通了"。
	if push.Provider != "" {
		t.Errorf("push.provider = %q, want 空串(推送至今没有调用方)", push.Provider)
	}
	if push.AliyunEMNS.DefaultAPNSEnv != "DEV" {
		t.Errorf("push.aliyun_emns.default_apns_env = %q, want DEV(dev 模板)",
			push.AliyunEMNS.DefaultAPNSEnv)
	}
}
