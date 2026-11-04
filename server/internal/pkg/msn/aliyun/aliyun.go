// 阿里云短信 sender —— 依赖 alidysms SDK
// 配置: config.SMS { provider: aliyun, access_key, access_secret, sign_name, region, templates }
// 参考: D:/gocodes/hertz-antd-admin-saas/admin-saas/server/utils/msn/aliyun/index.go
//
// ⚠️ **凭证可以热替换(C6,2026-10-29)**
//
// 原来 client 和 cfg 在 New() 里就定死了 —— 配置中心里改了 AccessKey / 签名 /
// 模板也没人再读,面板成了摆设。现在:
//
//   - SendWith:调用方把"这次生效的配置"传进来,内部按凭证指纹决定复用还是重建 client
//   - 指纹不变 ⇒ 复用同一个 client(不重复握手)
//   - 指纹变了 ⇒ 重建 client 并替换(下次发送即生效,无需重启)
package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/logger"
)

// AliyunSender 阿里云短信下发器
//
// 线程安全: client 的替换走 mutex;同一时刻只有一个 client 在用,
// 换凭证时新 client 建好再替换(不会把正在进行的请求打断)。
type AliyunSender struct {
	mu     sync.Mutex
	client *dysmsapi.Client
	credFP string // 当前 client 对应的凭证指纹(AK|SK|Region)

	// cfg 静态模式下生效的配置(Send 走它)
	cfg config.SMSConfig
}

// New 构造(严格模式)
//
// ⚠️ 这里**建 client 是为了启动即失败**:凭证缺失/region 写错在启动时炸,
// 比第一次发短信时才炸好排查得多。运行期换凭证走 SendWith。
func New(cfg config.SMSConfig) (*AliyunSender, error) {
	if err := validate(cfg); err != nil {
		return nil, err
	}
	s := &AliyunSender{cfg: cfg}
	if _, err := s.clientFor(cfg); err != nil {
		return nil, err
	}
	return s, nil
}

// NewDeferred 构造(宽松模式):**构造时不校验凭证**,等第一次发送时再校验
//
// 用途:provider 由配置中心在**运行时**决定 —— 启动时可能是 console(根本没配 AK),
// 但运营随时可能在面板上把它切成 aliyun。
// 如果启动时就因为"没凭证"而 Fatal,面板就永远切不过去。
//
// 校验并没有被推迟到"第一次发送才炸",SendWith 每次都会先 validate(cfg) ——
// 只是**启动时**不炸。
func NewDeferred(cfg config.SMSConfig) *AliyunSender {
	return &AliyunSender{cfg: cfg}
}

// validate 校验凭证/签名齐不齐
func validate(cfg config.SMSConfig) error {
	if cfg.AccessKey == "" || cfg.AccessSecret == "" {
		return fmt.Errorf("aliyun sms: access_key / access_secret 不能为空")
	}
	if cfg.SignName == "" {
		return fmt.Errorf("aliyun sms: sign_name 不能为空")
	}
	return nil
}

// clientFor 取(或重建)client
//   指纹(AK|SK|Region)不变 ⇒ 复用;变了 ⇒ 重建
func (s *AliyunSender) clientFor(cfg config.SMSConfig) (*dysmsapi.Client, error) {
	fp := credFingerprint(cfg)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil && s.credFP == fp {
		return s.client, nil
	}
	region := cfg.Region
	if region == "" {
		region = "cn-hangzhou"
	}
	api := &openapi.Config{
		AccessKeyId:     tea.String(cfg.AccessKey),
		AccessKeySecret: tea.String(cfg.AccessSecret),
		RegionId:        tea.String(region),
		Endpoint:        tea.String("dysmsapi.aliyuncs.com"),
		// ⚠️ 超时是**必需的** —— 这版 SDK 不吃 ctx,没有超时的话 TCP 挂死
		// 就会把验证码请求(SendSms)一直吊在那儿,同 template_status.go 的口径。
		ConnectTimeout: tea.Int(5000),
		ReadTimeout:    tea.Int(10000),
	}
	c, err := dysmsapi.NewClient(api)
	if err != nil {
		return nil, fmt.Errorf("aliyun sms: new client: %w", err)
	}
	s.client = c
	s.credFP = fp
	return c, nil
}

// credFingerprint 凭证指纹(只在内存里,不落日志)
func credFingerprint(cfg config.SMSConfig) string {
	return cfg.AccessKey + "|" + cfg.AccessSecret + "|" + cfg.Region
}

// Send 发送短信验证码(静态模式:用构造时的配置)
//   1. 选模板: scene -> template_code
//   2. 模板参数: { "code": "123456" }
//   3. 调 SendSms
func (s *AliyunSender) Send(ctx context.Context, phone, code, scene string) error {
	return s.SendWith(ctx, s.cfg, phone, code, scene)
}

// SendWith 按"这次生效的配置"发送(运行期换凭证/换模板走这里)
func (s *AliyunSender) SendWith(ctx context.Context, cfg config.SMSConfig, phone, code, scene string) error {
	if err := validate(cfg); err != nil {
		return err
	}
	client, err := s.clientFor(cfg)
	if err != nil {
		return err
	}
	tplCode, err := resolveTemplate(cfg, scene)
	if err != nil {
		return err
	}
	if tplCode == "" {
		// 空模板号发给阿里云只会得到一句含糊的 isv.SMS_TEMPLATE_ILLEGAL,
		// 这里直接说清楚"哪个场景没配模板"。
		return fmt.Errorf("aliyun sms: scene=%q 的模板号未配置", scene)
	}
	param, _ := json.Marshal(map[string]string{"code": code})
	req := &dysmsapi.SendSmsRequest{
		PhoneNumbers:  tea.String(phone),
		SignName:      tea.String(cfg.SignName),
		TemplateCode:  tea.String(tplCode),
		TemplateParam: tea.String(string(param)),
	}
	resp, err := client.SendSms(req)
	if err != nil {
		logger.L().Error("aliyun SendSms network error",
			zap.String("phone", phone), zap.String("scene", scene), zap.Error(err))
		return fmt.Errorf("aliyun sms: %w", err)
	}
	body := resp.Body
	if tea.StringValue(body.Code) != "OK" {
		msg := tea.StringValue(body.Message)
		logger.L().Warn("aliyun SendSms business error",
			zap.String("phone", phone), zap.String("scene", scene),
			zap.String("biz_code", tea.StringValue(body.Code)), zap.String("biz_msg", msg))
		return fmt.Errorf("aliyun sms biz error: %s", msg)
	}
	logger.L().Info("aliyun SendSms ok",
		zap.String("phone", phone), zap.String("scene", scene),
		zap.String("request_id", tea.StringValue(body.RequestId)))
	return nil
}

// Provider 标识(§1.9 #7 smsaudit)
func (s *AliyunSender) Provider() string { return "aliyun" }

// resolveTemplate scene -> template code
func resolveTemplate(cfg config.SMSConfig, scene string) (string, error) {
	switch scene {
	case "login":
		return cfg.Templates.Login, nil
	case "reset_password":
		return cfg.Templates.ResetPassword, nil
	case "change_phone":
		return cfg.Templates.ChangePhone, nil
	case "view_full_phone":
		return cfg.Templates.ViewFullPhone, nil
	case "mfa":
		// 登录二次验证(M150)复用登录模板 —— 与 smstpl.TemplateCode 同一口径,
		// 守卫判的号和实际下发的号必须一致。
		return cfg.Templates.Login, nil
	default:
		return "", fmt.Errorf("aliyun sms: 未知 scene=%q", scene)
	}
}
