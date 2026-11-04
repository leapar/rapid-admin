// 阿里云 EMNS (原 MPush) sender —— §1.9 #10 P2
//
// 走阿里云 EMNS OpenAPI:Push / MassPush / CancelPush
//
// 覆盖通道:
//   - iOS APNs
//   - Android 厂商(华为/小米/OPPO/vivo/魅族)
//   - Android 系统
//   - 鸿蒙 HMS
//   - Web Push
//
// 配置:
//
//	aliyun_emns:
//	  access_key: xxx
//	  access_secret: xxx
//	  app_key: xxx        # EMNS 控制台 AppKey
//	  region: cn-hangzhou
//	  default_apns_env: PROD  # DEV / PROD
//
// 参考:https://help.aliyun.com/document_detail/39056.html (Push)
package pushx

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
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// AliyunConfig 阿里云 EMNS 配置
type AliyunConfig struct {
	AccessKey    string
	AccessSecret string
	AppKey       string
	Region       string // cn-hangzhou
	// DefaultAPNSEnv: DEV / PROD(默认 PROD)
	DefaultAPNSEnv string
	// Timeout HTTP 超时(默认 30s)
	Timeout time.Duration
	// TargetMax 单次推送 target 上限(EMNS 限制 100)
	TargetMax int
}

// AliyunEMNS 阿里云 EMNS 推送
type AliyunEMNS struct {
	cfg AliyunConfig
	cli *http.Client
}

// NewAliyunEMNS 创建
func NewAliyunEMNS(cfg AliyunConfig) (*AliyunEMNS, error) {
	if cfg.AccessKey == "" || cfg.AccessSecret == "" {
		return nil, errors.New("aliyun emns: access_key / access_secret 不能为空")
	}
	if cfg.AppKey == "" {
		return nil, errors.New("aliyun emns: app_key 不能为空")
	}
	if cfg.Region == "" {
		cfg.Region = "cn-hangzhou"
	}
	if cfg.DefaultAPNSEnv == "" {
		cfg.DefaultAPNSEnv = "PROD"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.TargetMax == 0 {
		cfg.TargetMax = 100
	}
	return &AliyunEMNS{
		cfg: cfg,
		cli: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// Provider 标识
func (s *AliyunEMNS) Provider() string { return "aliyun-emns" }

// Push 调 Push API
//
//	POST https://cloudpush.aliyuncs.com/
//	Action=Push
func (s *AliyunEMNS) Push(ctx context.Context, n Notification) error {
	if err := n.IsValid(); err != nil {
		return err
	}
	// target 切块(EMNS 单次 ≤ 100)
	chunks := chunkTargets(n.Targets, s.cfg.TargetMax)
	var lastErr error
	for _, chunk := range chunks {
		if err := s.pushOne(ctx, n, chunk); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// pushOne 单批推送
func (s *AliyunEMNS) pushOne(ctx context.Context, n Notification, targets []Target) error {
	params := url.Values{}
	params.Set("Action", "Push")
	params.Set("Format", "JSON")
	params.Set("Version", "2016-08-01")
	params.Set("AccessKeyId", s.cfg.AccessKey)
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("SignatureVersion", "1.0")
	params.Set("SignatureNonce", randNonceEMNS())
	// 业务
	params.Set("AppKey", s.cfg.AppKey)
	params.Set("Target", joinTargetTypes(targets))
	params.Set("TargetValue", joinTargetValues(targets))
	params.Set("Title", n.Title)
	if n.Body != "" {
		params.Set("Body", n.Body)
	}
	if n.SubTitle != "" {
		params.Set("SubTitle", n.SubTitle)
	}
	if n.Sound != "" {
		params.Set("Sound", n.Sound)
	}
	if n.Badge > 0 {
		params.Set("Badge", fmt.Sprintf("%d", n.Badge))
	}
	// iOS
	if n.IOSApnsEnv != "" {
		params.Set("iOSApnsEnv", n.IOSApnsEnv)
	} else {
		params.Set("iOSApnsEnv", s.cfg.DefaultAPNSEnv)
	}
	// Android
	if n.AndroidChannel != "" {
		params.Set("AndroidNotificationChannel", n.AndroidChannel)
	}
	if n.AndroidOpenType != "" {
		params.Set("AndroidOpenType", n.AndroidOpenType)
	}
	if n.AndroidOpenURL != "" {
		params.Set("AndroidPopupActivity", n.AndroidOpenActivity)
		params.Set("AndroidPopUrl", n.AndroidOpenURL)
	}
	// 离线
	if n.PushTime != "" {
		params.Set("PushTime", n.PushTime)
	}
	if n.ExpireTime != "" {
		params.Set("ExpireTime", n.ExpireTime)
	}
	storeOffline := "true"
	if n.StoreOffline != nil {
		storeOffline = fmt.Sprintf("%t", *n.StoreOffline)
	}
	params.Set("StoreOffline", storeOffline)
	// 自定义数据(JSON 字符串)
	if len(n.Data) > 0 {
		b, _ := json.Marshal(n.Data)
		params.Set("ExtParameters", string(b))
	}
	// 签名
	sig := s.signEMNS(params)
	params.Set("Signature", sig)

	host := "https://cloudpush." + s.cfg.Region + ".aliyuncs.com/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host, strings.NewReader(params.Encode()))
	if err != nil {
		return &PushError{Provider: "aliyun-emns", Code: "request", Message: err.Error(), Retry: false}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.cli.Do(req)
	if err != nil {
		return &PushError{Provider: "aliyun-emns", Code: "network", Message: err.Error(), Retry: true}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return &PushError{
			Provider: "aliyun-emns",
			Code:     fmt.Sprintf("http_%d", resp.StatusCode),
			Message:  string(body),
			Retry:    resp.StatusCode >= 500,
		}
	}
	var env struct {
		RequestID string          `json:"RequestId"`
		// ⚠️ Code 用 RawMessage 而不是 int:body 非 JSON(LB/WAF 注入 HTML)时
		// 整体 unmarshal 失败,下面单独报错;Code 本身成功时是数字,失败时
		// 厂商也可能给字符串错误码 —— 固定 int 会把字符串码的 unmarshal 失败
		// 误当成"Code=0 成功"。
		Code      json.RawMessage `json:"Code"`
		Message   string          `json:"Message"`
		MessageID string          `json:"MessageId"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		// ⚠️ HTTP 200 但 body 不是 JSON ≠ 推送成功。以前这里 return nil,
		// 一旦网关/LB/WAF 吐回 HTML 错误页,推送就静默丢失,连一条日志都没有。
		// 按不可重试失败报出来:网关返回 200 通常是策略性拦截而非瞬时故障,
		// 重试同样会被拦。
		return &PushError{
			Provider: "aliyun-emns",
			Code:     "unparseable_response",
			Message:  fmt.Sprintf("响应不是 JSON(可能被网关/WAF 拦截),body 共 %d 字节,开头: %s", len(body), truncateForLog(body)),
			Retry:    false,
		}
	}
	// EMNS 业务错误(非网络)
	// - Code=0 或 200:成功
	// - Code=其他(数字或字符串):失败
	bizCode := ""
	var codeNum int
	if err := json.Unmarshal(env.Code, &codeNum); err == nil {
		if codeNum != 0 && codeNum != 200 {
			bizCode = fmt.Sprintf("biz_%d", codeNum)
		}
	} else {
		var codeStr string
		if err := json.Unmarshal(env.Code, &codeStr); err == nil && codeStr != "" {
			bizCode = "biz_" + codeStr
		}
	}
	if bizCode != "" {
		return &PushError{
			Provider: "aliyun-emns",
			Code:     bizCode,
			Message:  env.Message,
			Retry:    false,
		}
	}
	return nil
}

// truncateLen 日志里 body 摘要的最大字节数(够认出 HTML 错误页即可,别刷屏)
const truncateLen = 200

// truncateForLog 把响应 body 截成日志可用的摘要
func truncateForLog(b []byte) string {
	if len(b) > truncateLen {
		return string(b[:truncateLen]) + "..."
	}
	return string(b)
}

// chunkTargets 把 targets 拆成多批
func chunkTargets(targets []Target, size int) [][]Target {
	if size <= 0 {
		size = 100
	}
	var out [][]Target
	for i := 0; i < len(targets); i += size {
		end := i + size
		if end > len(targets) {
			end = len(targets)
		}
		out = append(out, targets[i:end])
	}
	return out
}

// joinTargetTypes 把 targets 的 type 用逗号串起来
func joinTargetTypes(targets []Target) string {
	parts := make([]string, len(targets))
	for i, t := range targets {
		parts[i] = t.Type
	}
	return strings.Join(parts, ",")
}

// joinTargetValues 把 targets 的 value 用逗号串起来
func joinTargetValues(targets []Target) string {
	parts := make([]string, len(targets))
	for i, t := range targets {
		parts[i] = t.Value
	}
	return strings.Join(parts, ",")
}

// signEMNS 阿里云 EMNS 签名(同 DirectMail 风格 HMAC-SHA1)
func (s *AliyunEMNS) signEMNS(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "Signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	canonical := strings.Builder{}
	for i, k := range keys {
		if i > 0 {
			canonical.WriteString("&")
		}
		canonical.WriteString(percentEncodeEMNS(k) + "=" + percentEncodeEMNS(params.Get(k)))
	}
	stringToSign := "POST&" + percentEncodeEMNS("/") + "&" + percentEncodeEMNS(canonical.String())
	mac := hmac.New(sha1.New, []byte(s.cfg.AccessSecret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func percentEncodeEMNS(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~' {
			b.WriteRune(r)
		} else {
			b.WriteString(url.QueryEscape(string(r)))
		}
	}
	return strings.ReplaceAll(b.String(), "+", "%20")
}

func randNonceEMNS() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ConsoleSender 控制台打印(dev 模式)
type ConsoleSender struct {
	// Out 打印目标(默认 os.Stdout)
	//
	// 🔴 **2026-10-25 修**:原来是 `Out *bytes.Buffer`,默认值是个**空的
	//   内存 buffer** —— 于是 dev 模式下的 console 推送**打印到 nowhere**:
	//   日志里一行都看不到,而单测因为只读 `Output()`(同一个 buffer)，
	//   一直是绿的 —— 假信心。
	//
	//   改成 io.Writer(默认 stdout),同时**另存一份**到内部 buffer,
	//   `Output()` 照旧能拿到内容(单测不用改)。
	Out io.Writer
	// buf 内部留档(供 Output 断言)
	//
	// ⚠️ **要加锁** —— 通知分发器的额外交付是**异步**的,两条通知可能
	//   同时打进同一个 sender。buf 是共享可变状态,不加锁就是数据竞争
	//   (emailx.Mock / pushx.Mock 都用 sync.Mutex 守着同一类状态,同一套规矩)。
	buf bytes.Buffer
	// mu 守住 buf(Out 交给调用方的 io.Writer,可能是并发安全的,也可能不是)
	mu sync.Mutex
}

// NewConsolePush 控制台 push
func NewConsolePush() *ConsoleSender {
	return &ConsoleSender{Out: os.Stdout}
}

// Provider 标识
func (c *ConsoleSender) Provider() string { return "console" }

// Push 打印
func (c *ConsoleSender) Push(ctx context.Context, n Notification) error {
	if err := n.IsValid(); err != nil {
		return err
	}
	out := c.Out
	if out == nil {
		out = io.Discard
	}
	// ⚠️ **锁要盖住整段写入** —— 分多次 Fprintf 写同一个 buf,
	//   只锁最后一句的话,两条并发推送仍然会交错。
	c.mu.Lock()
	defer c.mu.Unlock()
	w := io.MultiWriter(out, &c.buf)
	fmt.Fprintf(w, "[push/console] %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(w, "  Title:   %s\n", n.Title)
	fmt.Fprintf(w, "  Body:    %s\n", n.Body)
	for _, t := range n.Targets {
		fmt.Fprintf(w, "  Target:  %s=%s\n", t.Type, t.Value)
	}
	if len(n.Data) > 0 {
		fmt.Fprintf(w, "  Data:    %v\n", n.Data)
	}
	return nil
}

// Output 拿到打印内容
func (c *ConsoleSender) Output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}
