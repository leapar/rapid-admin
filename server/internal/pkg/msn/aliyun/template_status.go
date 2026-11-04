// 阿里云模板审核状态查询 —— M115
//
// # 这个文件解决什么
//
// 平台此前**完全不知道**模板在阿里云那边过没过审:
//
//   - 运营在配置中心填了一个模板号,面板上一切正常;
//   - 用户点「获取验证码」,短信没到;
//   - 服务端日志只有一句 isv.SMS_TEMPLATE_ILLEGAL —— 阿里云的通用业务错误码,
//     它不告诉你"是模板没过审"还是"模板号写错了"还是"签名没报备"。
//
// 这个文件是**唯一**能把真实审核态拉回来的地方:
// 调 GetSmsTemplate,把服务商的原始状态码翻译成 smstpl.Status。
//
// # ⚠️ 为什么用裸 RPC 而不是 SDK
//
// dysmsapi-20170525/v3@v3.0.6 的 SDK 里**没有 GetSmsTemplate** ——
// 它只有 QuerySmsTemplate,而那个接口阿里云已经**下线**了
// (官方文档标注「该接口已下线」,建议改用 GetSmsTemplate)。
// ⇒ 只能照抄 SDK 生成代码的调用姿势(Params + OpenApiRequest + CallApi)自己发。
//
// # ⚠️ 为什么"不认识的状态码"必须报错而不是瞎猜
//
// 服务商加了一个新状态,我们的映射表漏了 ——
//
//	猜成"通过" ⇒ 页面显示"已通过",而这张模板从来没审过,用户永远收不到验证码;
//	猜成"驳回" ⇒ 一个映射表漏项把全平台的登录验证码锁死。
//
// ⇒ 两种都是灾难,所以老老实实报错:同步任务记 Failed、状态保持原样,
// 下轮同步自动重试(或者等人来改映射表)。
//
// # ⚠️ 为什么"模板不存在"不算故障
//
// 模板号不存在(运营在配置中心敲错一位)是**确定发不出去**的结论,
// 它必须在页面上显示出来 —— 当成"同步失败"藏进日志,等于白做这个功能。
// ⇒ 404 TemplateNotFound 直接映射成 rejected + 理由"模板号在阿里云不存在"。
//
// # ⚠️ ctx 只能"进门时"用
//
// 这版 SDK 的 CallApi **不接受 context**(签名里只有 runtime),
// 中途取消不了。所以 ctx 只能:
//
//	进门先查一次 ctx.Err()(已经取消就别发这个请求了)
//	靠 runtime 的超时兜底(读 10s / 连 5s),不让同步任务被拖死
package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/smstpl"
)

// TemplateStatusFetcher 查阿里云侧的模板审核状态(实现 smstpl.Fetcher)
//
// ⚠️ **每轮同步构造一个新的** —— 配置中心换过凭证之后,
// 抱着旧 fetcher 去查,查的是旧账号的模板(而且会一直查错下去)。
// 构造时传入"这次生效的配置",内部按凭证指纹复用 client。
type TemplateStatusFetcher struct {
	sender *AliyunSender
	cfg    config.SMSConfig
}

// NewTemplateStatusFetcher 构造
func NewTemplateStatusFetcher(sender *AliyunSender, cfg config.SMSConfig) *TemplateStatusFetcher {
	return &TemplateStatusFetcher{sender: sender, cfg: cfg}
}

// FetchTemplateStatus 查模板审核状态
//
// ⚠️ **返回 error ⇒ 状态保持原样** —— 拉不到 ≠ 被驳回(见 smstpl.Syncer 的注释)。
// 唯一例外是"模板号不存在":那是一个**确定**的结论,映射成 rejected 返回。
func (f *TemplateStatusFetcher) FetchTemplateStatus(ctx context.Context, templateCode string) (smstpl.ProviderStatus, error) {
	if templateCode == "" {
		return smstpl.ProviderStatus{}, errors.New("aliyun sms: 模板号为空")
	}
	if f == nil || f.sender == nil {
		return smstpl.ProviderStatus{}, errors.New("aliyun sms: 模板状态查询器未初始化")
	}
	// ⚠️ 这版 SDK 的 CallApi 不吃 ctx,只能进门时看一眼(见文件头注释)
	if err := ctx.Err(); err != nil {
		return smstpl.ProviderStatus{}, err
	}
	client, err := f.sender.clientFor(f.cfg)
	if err != nil {
		return smstpl.ProviderStatus{}, err
	}
	return getTemplateStatus(client, templateCode)
}

// rpcCaller 抽象"发一个阿里云 RPC"(单测打桩用)
//
// ⚠️ **生产只有一个实现**(*dysmsapi.Client)——
// 它存在的唯一理由是让"404 → 模板不存在 → rejected"这条最关键的分支**能被测到**:
// 那一分支如果只能靠真网才能走到,实际上就等于**没被测过**。
type rpcCaller interface {
	CallApi(params *openapi.Params, request *openapi.OpenApiRequest, runtime *util.RuntimeOptions) (map[string]interface{}, error)
}

// getTemplateStatus 查一个模板并把结果翻译成 smstpl.Status
func getTemplateStatus(client rpcCaller, templateCode string) (smstpl.ProviderStatus, error) {
	body, err := callGetSmsTemplate(client, templateCode)
	if err != nil {
		if isTemplateNotFound(err) {
			// 模板号在阿里云不存在 ⇒ 确定发不出去,但**不是**内容被驳回
			// (理由里写清楚,免得运维去改模板内容 —— 改内容没用,号本身就是错的)
			return smstpl.ProviderStatus{
				Status: smstpl.StatusRejected,
				Reason: fmt.Sprintf("模板号在阿里云不存在(%s)", templateCode),
			}, nil
		}
		return smstpl.ProviderStatus{}, err
	}
	return mapTemplateStatus(body)
}

// callGetSmsTemplate 裸调 GetSmsTemplate(2017-05-25)
//
// ⚠️ **照抄 SDK 生成代码的姿势**(Params + OpenApiRequest + CallApi) ——
// 签名算法、RPC 风格、BodyType 都得跟生成代码一致,否则服务端不认。
func callGetSmsTemplate(client rpcCaller, templateCode string) (map[string]interface{}, error) {
	if client == nil {
		return nil, errors.New("aliyun sms: client 未初始化")
	}
	query := map[string]interface{}{"TemplateCode": templateCode}
	req := &openapi.OpenApiRequest{
		Query: openapiutil.Query(query),
	}
	params := &openapi.Params{
		Action:      tea.String("GetSmsTemplate"),
		Version:     tea.String("2017-05-25"),
		Protocol:    tea.String("HTTPS"),
		Pathname:    tea.String("/"),
		Method:      tea.String("POST"),
		AuthType:    tea.String("AK"),
		Style:       tea.String("RPC"),
		ReqBodyType: tea.String("formData"),
		BodyType:    tea.String("json"),
	}
	runtime := &util.RuntimeOptions{
		// ⚠️ 超时是**必需的** —— 这版 SDK 不吃 ctx,
		// 没有超时就可能把同步任务挂在那儿直到 TCP 层放弃。
		ConnectTimeout: tea.Int(5000),
		ReadTimeout:    tea.Int(10000),
		// ⚠️ **不重试** —— 同步任务每 10 分钟自己跑一轮,
		// SDK 内部再重试 3 次 = 一次失败拖 30 秒,毫无意义。
		Autoretry:   tea.Bool(false),
		MaxAttempts: tea.Int(1),
	}
	body, err := client.CallApi(params, req, runtime)
	if err != nil {
		logger.L().Warn("aliyun GetSmsTemplate failed",
			zap.String("template_code", templateCode), zap.Error(err))
		return nil, fmt.Errorf("aliyun sms: GetSmsTemplate 失败(%s): %w", templateCode, err)
	}
	// ⚠️ **必须剥掉 RPC 外层信封** —— 业务字段在 body 里面,不在顶层(见 unwrapPayload)
	payload := unwrapPayload(body)
	// HTTP 200 但业务码不是 OK —— 阿里云有两种报错方式,这里都要接住
	if code := stringField(payload, "Code"); code != "" && code != "OK" {
		logger.L().Warn("aliyun GetSmsTemplate business error",
			zap.String("template_code", templateCode),
			zap.String("biz_code", code),
			zap.String("biz_msg", stringField(payload, "Message")))
		return nil, fmt.Errorf("aliyun sms: GetSmsTemplate 业务错误(%s): %s",
			code, stringField(payload, "Message"))
	}
	return payload, nil
}

// unwrapPayload 把 RPC 风格的外层信封剥掉,返回真正的业务载荷
//
// # ⚠️ 这是一个**真 bug 的修复**,不是洁癖
//
// 阿里云 RPC 风格接口的响应是**套了两层**的:
//
//	{
//	  "body":       { "Code":"OK", "TemplateStatus":"1", ... },  ← 真正的业务载荷
//	  "headers":    { ... },
//	  "statusCode": "200"
//	}
//
// 业务字段在 **body 里面**,不在顶层。
//
// 之前直接读顶层 ⇒ 永远读不到 TemplateStatus ⇒ 每个场景都判成"拉取失败"
// ⇒ 登记册永远停在 unknown ⇒ 页面上永远是"未同步";
// 而定时任务因为"单个场景失败不算任务失败"照样记 success ——
// **一个纯粹的"取错了层级"的问题,表现成了"同步功能好像没生效"**。
//
// # ⚠️ 不能无脑剥
//
// 载荷自己也有可能带 "body" 字段(不同接口的载荷结构不一样)。
// 判据:**外层已经有业务字段(Code / TemplateStatus)时,它本身就是载荷**,别再剥。
//
// 幂等:剥过一次之后再剥一次,结果不变(载荷自己有 Code / TemplateStatus)。
func unwrapPayload(body map[string]interface{}) map[string]interface{} {
	if body == nil {
		return nil
	}
	inner, ok := body["body"].(map[string]interface{})
	if !ok || len(inner) == 0 {
		return body
	}
	if _, has := body["TemplateStatus"]; has {
		return body
	}
	if _, has := body["Code"]; has {
		return body
	}
	return inner
}

// mapTemplateStatus 把服务商的响应翻译成 smstpl.Status
//
// ⚠️ **这是全模块最容易写错的一张映射表** ——
// 服务商的 TemplateStatus 取值(官方文档):
//
//	 0 = 审核中
//	 1 = 审核通过
//	 2 = 审核未通过(理由在 AuditInfo.RejectInfo)
//	10 = 取消审核
//
// ⚠️ **10 不是 2** —— "取消审核"既不是"内容被驳回"也不是"还在审",
// 运维看到"已驳回"会去改模板内容,看到"审核取消"要办的是另一件事。
// ⇒ 框架层为它准备了独立状态 smstpl.StatusCancelled(见 smstpl 包)。
//
// ⚠️ **不认识的值一律报错** —— 见文件头「为什么必须报错而不是瞎猜」。
func mapTemplateStatus(body map[string]interface{}) (smstpl.ProviderStatus, error) {
	// ⚠️ 再剥一次 —— 这个函数可能被直接调用(单测 / 别的调用方),
	//   不能假设传进来的已经是载荷。unwrapPayload 是幂等的。
	body = unwrapPayload(body)
	raw, ok := body["TemplateStatus"]
	if !ok || raw == nil {
		return smstpl.ProviderStatus{}, fmt.Errorf(
			"aliyun sms: GetSmsTemplate 响应里没有 TemplateStatus(原始响应:%s)", previewBody(body))
	}
	code, err := statusCodeOf(raw)
	if err != nil {
		return smstpl.ProviderStatus{}, err
	}
	switch code {
	case 0:
		return smstpl.ProviderStatus{
			Status: smstpl.StatusReviewing,
			Reason: "服务商审核中",
		}, nil
	case 1:
		return smstpl.ProviderStatus{
			Status: smstpl.StatusApproved,
			Reason: "服务商审核通过",
		}, nil
	case 2:
		reason := auditRejectReason(body)
		if reason == "" {
			reason = "服务商驳回(未给出理由)"
		}
		return smstpl.ProviderStatus{
			Status: smstpl.StatusRejected,
			Reason: reason,
		}, nil
	case 10:
		return smstpl.ProviderStatus{
			Status: smstpl.StatusCancelled,
			Reason: "服务商取消了这次审核(需重新送审)",
		}, nil
	default:
		return smstpl.ProviderStatus{}, fmt.Errorf(
			"aliyun sms: 不认识的 TemplateStatus=%d(映射表漏了新状态,不许瞎猜)", code)
	}
}

// statusCodeOf 把响应里的 TemplateStatus 归一成 int
//
// ⚠️ **阿里云在不同接口里把这个字段的类型搞得不一致** ——
// GetSmsTemplate 的文档写的是 string("2"),JSON 解出来也可能是数字(2 / 2.0)。
// ⇒ 两种都得认,否则同一个状态在不同的返回里走两条路,其中一个必然是错的。
func statusCodeOf(raw interface{}) (int, error) {
	switch v := raw.(type) {
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, errors.New("aliyun sms: TemplateStatus 是空串")
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("aliyun sms: TemplateStatus=%q 不是数字", s)
		}
		return n, nil
	case float64:
		// JSON 解码出来的数字都是 float64
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("aliyun sms: TemplateStatus=%v 不是整数", v)
		}
		return int(v), nil
	case float32:
		if float64(v) != math.Trunc(float64(v)) {
			return 0, fmt.Errorf("aliyun sms: TemplateStatus=%v 不是整数", v)
		}
		return int(v), nil
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("aliyun sms: TemplateStatus=%q 不是整数", v.String())
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("aliyun sms: TemplateStatus 类型不认识(%T)", raw)
	}
}

// auditRejectReason 取审核信息里的驳回理由
//
// ⚠️ **理由要原样带给运维** —— 阿里云的驳回理由是唯一能定位问题的东西
// ("模板内容中包含错别字"),平台不该把它压成一句"审核未通过"。
func auditRejectReason(body map[string]interface{}) string {
	info, ok := body["AuditInfo"].(map[string]interface{})
	if !ok {
		return ""
	}
	reason := strings.TrimSpace(stringField(info, "RejectInfo"))
	date := strings.TrimSpace(stringField(info, "AuditDate"))
	if reason == "" {
		if date == "" {
			return ""
		}
		return fmt.Sprintf("服务商驳回(审核时间:%s)", date)
	}
	if date == "" {
		return reason
	}
	return fmt.Sprintf("%s(审核时间:%s)", reason, date)
}

// isTemplateNotFound 判断错误是不是"模板号不存在"
//
// ⚠️ **404 与业务码两种都要认** —— 阿里云在不同接口/不同版本里
// 分别用过这两种报错方式(文档里是 HTTP 404 + TemplateNotFound)。
func isTemplateNotFound(err error) bool {
	if err == nil {
		return false
	}
	var se *tea.SDKError
	if errors.As(err, &se) {
		if tea.IntValue(se.StatusCode) == 404 {
			return true
		}
		if isNotFoundCode(tea.StringValue(se.Code)) {
			return true
		}
	}
	// CallApi 之外的路径(比如业务码)也会把码写在错误文本里
	return isNotFoundCode(err.Error())
}

// isNotFoundCode 判断错误码是不是"模板不存在"那一族
func isNotFoundCode(code string) bool {
	if code == "" {
		return false
	}
	c := strings.ToUpper(code)
	return strings.Contains(c, "TEMPLATENOTFOUND") ||
		strings.Contains(c, "TEMPLATE_NOT_EXIST") ||
		strings.Contains(c, "TEMPLATENOTEXIST")
}

// stringField 从 map 里取字符串字段(非字符串一律当空串)
func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// previewBody 把响应体压成一行,给错误信息用
//
// ⚠️ **只给排查用,不要打进日志主流程** —— 响应体里有模板内容(可能含手机号等),
// 只在出错时截断展示。
func previewBody(body map[string]interface{}) string {
	if body == nil {
		return "{}"
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "<无法序列化>"
	}
	s := string(b)
	if len(s) > 500 {
		return s[:500] + "…(截断)"
	}
	return s
}
