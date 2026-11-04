// 阿里云 DirectMail sender —— §1.9 #9 P2
//
// 走阿里云 DirectMail HTTP API(免 SDK,直接 net/http + HMAC-SHA1 签名)
//
// 适用场景:
//   - 国内发信(免备案优势)
//   - 需要退信/打开/点击跟踪
//   - 需要 IP 白名单/发送频率控制
//
// 限制:
//   - 单邮件 1-50 收件人(To/Cc/Bcc 合计)
//   - 单账号默认 500/天,可申请提额
//   - 主题/内容需审核(发信前需在控制台创建发信模板)
//
// 配置:
//
//	aliyun:
//	  access_key: xxx
//	  access_secret: xxx
//	  account_name: noreply@example.com  # 发信地址(需在控制台验证)
//	  from_alias: 平台                    # 发信人显示名
//	  region: cn-hangzhou
//
// 参考:https://help.aliyun.com/document_detail/29444.html (SingleSendMail)
package emailx

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AliyunConfig 阿里云 DirectMail 配置
type AliyunConfig struct {
	AccessKey   string
	AccessSecret string
	AccountName string // 发信地址(From)
	FromAlias   string // 发信人显示名(可选)
	Region      string // 默认 cn-hangzhou
	// ReplyToAddress 回信地址(可选,需在控制台开启)
	ReplyToAddress string
	// AddressType: 0 随机账号 / 1 发信地址(默认 1)
	AddressType int
	// Timeout HTTP 超时(默认 30s)
	Timeout time.Duration
}

// AliyunSender 阿里云 DirectMail
type AliyunSender struct {
	cfg AliyunConfig
	cli *http.Client
}

// NewAliyun 创建阿里云 DirectMail sender
func NewAliyun(cfg AliyunConfig) (*AliyunSender, error) {
	if cfg.AccessKey == "" || cfg.AccessSecret == "" {
		return nil, errors.New("aliyun email: access_key / access_secret 不能为空")
	}
	if cfg.AccountName == "" {
		return nil, errors.New("aliyun email: account_name 不能为空")
	}
	if cfg.Region == "" {
		cfg.Region = "cn-hangzhou"
	}
	if cfg.AddressType == 0 {
		cfg.AddressType = 1
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &AliyunSender{
		cfg: cfg,
		cli: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// Provider 标识
func (s *AliyunSender) Provider() string { return "aliyun" }

// Send 调 SingleSendMail API
//
//	POST https://dm.aliyuncs.com/
//	params: AccountName, FromAlias, AddressType, ToAddress, Subject, HtmlBody, TextBody, ...
//	sig: 公共参数 + HMAC-SHA1
func (s *AliyunSender) Send(ctx context.Context, msg Message) error {
	if err := msg.IsValid(); err != nil {
		return err
	}

	params := url.Values{}
	params.Set("Action", "SingleSendMail")
	params.Set("Format", "JSON")
	params.Set("Version", "2015-11-23")
	params.Set("AccessKeyId", s.cfg.AccessKey)
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("SignatureVersion", "1.0")
	params.Set("SignatureNonce", randNonce())
	// 业务参数
	params.Set("AccountName", s.cfg.AccountName)
	if s.cfg.FromAlias != "" {
		params.Set("FromAlias", s.cfg.FromAlias)
	}
	if msg.FromName != "" && s.cfg.FromAlias == "" {
		params.Set("FromAlias", msg.FromName)
	}
	params.Set("AddressType", fmt.Sprintf("%d", s.cfg.AddressType))
	params.Set("ToAddress", strings.Join(msg.To, ","))
	if len(msg.CC) > 0 {
		params.Set("CcAddress", strings.Join(msg.CC, ","))
	}
	if len(msg.BCC) > 0 {
		params.Set("BccAddress", strings.Join(msg.BCC, ","))
	}
	if s.cfg.ReplyToAddress != "" {
		params.Set("ReplyToAddress", "1")
	}
	params.Set("Subject", msg.Subject)
	if msg.HTML != "" {
		params.Set("HtmlBody", msg.HTML)
	}
	if msg.Text != "" {
		params.Set("TextBody", msg.Text)
	}
	// 签名
	sig := s.sign(params)
	params.Set("Signature", sig)

	// 发起请求
	host := "https://dm." + s.cfg.Region + ".aliyuncs.com/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host, strings.NewReader(params.Encode()))
	if err != nil {
		return &EmailError{Provider: "aliyun", Code: "request", Message: err.Error(), Retry: false}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.cli.Do(req)
	if err != nil {
		return &EmailError{Provider: "aliyun", Code: "network", Message: err.Error(), Retry: true}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return &EmailError{
			Provider: "aliyun",
			Code:     fmt.Sprintf("http_%d", resp.StatusCode),
			Message:  string(body),
			Retry:    resp.StatusCode >= 500,
		}
	}
	// 业务码(阿里云返 {"envId":...} 成功 / 返错误码)
	var env struct {
		EnvID    string `json:"envId"`
		RequestID string `json:"RequestId"`
		Code     string `json:"Code"`
		Message  string `json:"Message"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&env); err == nil && env.Code != "" {
		return &EmailError{
			Provider: "aliyun",
			Code:     env.Code,
			Message:  env.Message,
			Retry:    false, // 业务错误不重试
		}
	}
	return nil
}

// sign 阿里云 API 签名(RPC 风格)
//   - 参数排序(按 key 字典序)
//   - 拼接: HTTPMethod + "&" + encodeURIComponent("/") + "&" + encodeURIComponent(sortedQuery)
//   - HMAC-SHA1(key = AccessSecret + "&"),base64
func (s *AliyunSender) sign(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "Signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	canonicalizedQuery := strings.Builder{}
	for i, k := range keys {
		if i > 0 {
			canonicalizedQuery.WriteString("&")
		}
		canonicalizedQuery.WriteString(percentEncode(k) + "=" + percentEncode(params.Get(k)))
	}
	stringToSign := "POST&" + percentEncode("/") + "&" + percentEncode(canonicalizedQuery.String())
	mac := hmac.New(sha1.New, []byte(s.cfg.AccessSecret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// percentEncode 阿里云签名专用的 URL encode
//   - 不编码: A-Z a-z 0-9 - _ . ~
//   - 其余按 UTF-8 转 %XX(空格 → %20,不编码为 +)
func percentEncode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~' {
			b.WriteRune(r)
		} else {
			// url.QueryEscape 把空格编为 + ,aliyun 要求 %20
			b.WriteString(url.QueryEscape(string(r)))
		}
	}
	// 兜底:url.QueryEscape 的空格是 + ,需替换为 %20
	return strings.ReplaceAll(b.String(), "+", "%20")
}

// randNonce 32 hex 字符
func randNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
