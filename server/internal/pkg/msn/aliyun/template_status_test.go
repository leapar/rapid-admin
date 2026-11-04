// 阿里云模板审核状态查询的测试 —— M115
//
// # 这层最容易写错、也最该被网住的三处
//
//  1. **状态码映射表** —— 把"审核取消(10)"错当成"驳回(2)",
//     页面会叫运维去改模板内容(没用,号本身的事);反过来把 2 当成 10 更糟。
//  2. **不认识的码当成"通过"** —— 服务商加了新状态,映射表漏了,
//     页面显示"已通过"而模板从来没审过 ⇒ 用户永远收不到验证码,且没有任何告警。
//  3. **"模板不存在"当成同步失败** —— 运营敲错模板号,页面什么都不显示,
//     只有日志里一行,等于这个功能白做。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· template_status.go:case 10 改成 case 2  → 「审核取消 ≠ 驳回」变红
//	· template_status.go:default 分支返回 approved → 「不认识的码不许瞎猜」变红
//	· template_status.go:isTemplateNotFound 永远返 false → 「模板不存在要显示出来」变红
//	· template_status.go:把 TemplateStatus 当字符串硬解 → 「数字形态也要认」变红
package aliyun

import (
	"context"
	"errors"
	"strings"
	"testing"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/smstpl"
)

// configZero 空配置(单测只需要它的形状)
func configZero() config.SMSConfig { return config.SMSConfig{} }

// fakeCaller 打桩的 RPC 调用
type fakeCaller struct {
	body   map[string]interface{}
	err    error
	action string
	query  map[string]*string
}

func (f *fakeCaller) CallApi(params *openapi.Params, request *openapi.OpenApiRequest, _ *util.RuntimeOptions) (map[string]interface{}, error) {
	f.action = tea.StringValue(params.Action)
	f.query = request.Query
	return f.body, f.err
}

// bodyOf 造一个服务商响应
func bodyOf(status interface{}, audit map[string]interface{}) map[string]interface{} {
	b := map[string]interface{}{"Code": "OK", "TemplateCode": "SMS_1"}
	if status != nil {
		b["TemplateStatus"] = status
	}
	if audit != nil {
		b["AuditInfo"] = audit
	}
	return b
}

// ---- 状态码映射 ----

func TestMapTemplateStatusTable(t *testing.T) {
	cases := []struct {
		name       string
		status     interface{}
		wantStatus smstpl.Status
		wantReason string // 必须包含的片段
	}{
		{"审核中", "0", smstpl.StatusReviewing, "审核中"},
		{"审核通过", "1", smstpl.StatusApproved, "通过"},
		{"审核驳回", "2", smstpl.StatusRejected, "驳回"},
		{"审核取消", "10", smstpl.StatusCancelled, "取消"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, err := mapTemplateStatus(bodyOf(c.status, nil))
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if st.Status != c.wantStatus {
				t.Fatalf("状态应为 %q, got %q", c.wantStatus, st.Status)
			}
			if !strings.Contains(st.Reason, c.wantReason) {
				t.Fatalf("理由应包含 %q, got %q", c.wantReason, st.Reason)
			}
		})
	}
}

// TestMapTemplateStatusCancelledIsNotRejected 审核取消 ≠ 驳回
//
// ⚠️ **这两个混在一起,运维就会去改模板内容** ——
// 而"审核取消"多半是模板被删了或者要重新送审,改内容没用。
func TestMapTemplateStatusCancelledIsNotRejected(t *testing.T) {
	st, err := mapTemplateStatus(bodyOf("10", nil))
	if err != nil {
		t.Fatal(err)
	}
	if st.Status == smstpl.StatusRejected {
		t.Fatal("审核取消不能映射成驳回 —— 运维会因此去改模板内容(没用)")
	}
	if st.Status != smstpl.StatusCancelled {
		t.Fatalf("审核取消应映射成 cancelled, got %q", st.Status)
	}
	// 框架层必须认识这个状态,否则登记册会拒收(Apply 会报非法流转)
	if !st.Status.Known() {
		t.Fatalf("框架层不认识 %q —— 登记册会拒收这个状态", st.Status)
	}
}

// TestMapTemplateStatusAcceptsStringAndNumber 阿里云这个字段的类型不一致
//
// 官方文档写的是 string("2"),但 JSON 解出来也可能是数字(2 / 2.0)。
// 只认一种 = 同一个状态在两种返回里走两条路,其中一条必然是错的。
func TestMapTemplateStatusAcceptsStringAndNumber(t *testing.T) {
	for _, raw := range []interface{}{"2", 2, float64(2), int32(2), int64(2)} {
		st, err := mapTemplateStatus(bodyOf(raw, nil))
		if err != nil {
			t.Fatalf("TemplateStatus=%v(%T)不该报错: %v", raw, raw, err)
		}
		if st.Status != smstpl.StatusRejected {
			t.Fatalf("TemplateStatus=%v(%T)应映射成 rejected, got %q", raw, raw, st.Status)
		}
	}
}

// TestMapTemplateStatusUnknownCodeIsError 不认识的码必须报错
//
// ⚠️ 这是本模块最重要的一条 —— 猜成"通过"= 页面说"已通过"而模板从没审过;
// 猜成"驳回" = 一个映射表漏项把全平台登录验证码锁死。
func TestMapTemplateStatusUnknownCodeIsError(t *testing.T) {
	for _, raw := range []interface{}{"3", "5", "99", "-1", "wat"} {
		st, err := mapTemplateStatus(bodyOf(raw, nil))
		if err == nil {
			t.Fatalf("TemplateStatus=%v 必须报错(不许瞎猜), got %+v", raw, st)
		}
		if st.Status == smstpl.StatusApproved || st.Status == smstpl.StatusRejected {
			t.Fatalf("TemplateStatus=%v 报错时不能带任何状态, got %q", raw, st.Status)
		}
	}
}

func TestMapTemplateStatusMissingFieldIsError(t *testing.T) {
	// 响应里根本没有 TemplateStatus
	if _, err := mapTemplateStatus(map[string]interface{}{"Code": "OK"}); err == nil {
		t.Fatal("缺 TemplateStatus 必须报错 —— 不能当成『通过』")
	}
	// TemplateStatus 是 nil
	if _, err := mapTemplateStatus(bodyOf(nil, nil)); err == nil {
		t.Fatal("TemplateStatus 为 nil 必须报错")
	}
}

func TestMapTemplateStatusRejectReason(t *testing.T) {
	audit := map[string]interface{}{
		"RejectInfo": "模板内容中包含错别字。",
		"AuditDate":  "2024-06-03 11:20:34",
	}
	st, err := mapTemplateStatus(bodyOf("2", audit))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.Reason, "错别字") {
		t.Fatalf("驳回理由要原样带给运维, got %q", st.Reason)
	}
	if !strings.Contains(st.Reason, "2024-06-03") {
		t.Fatalf("审核时间也要带上, got %q", st.Reason)
	}

	// 没有 AuditInfo 时也要有个能看懂的默认理由(不能是空串)
	st2, err := mapTemplateStatus(bodyOf("2", nil))
	if err != nil {
		t.Fatal(err)
	}
	if st2.Reason == "" {
		t.Fatal("驳回但没有理由时也要给一句人话")
	}
}

func TestStatusCodeOfInvalid(t *testing.T) {
	for _, raw := range []interface{}{"", "  ", "abc", 1.5, true, nil, []interface{}{1}} {
		if _, err := statusCodeOf(raw); err == nil {
			t.Fatalf("TemplateStatus=%v(%T)必须报错", raw, raw)
		}
	}
}

// ---- 错误分类 ----

func TestIsTemplateNotFound(t *testing.T) {
	notFound := &tea.SDKError{
		Code:       tea.String("TemplateNotFound"),
		StatusCode: tea.Int(404),
		Message:    tea.String("模板不存在"),
	}
	if !isTemplateNotFound(notFound) {
		t.Fatal("404 TemplateNotFound 应被认出来")
	}

	// 包装过的也要认(errors.As 得能穿透 %w)
	wrapped := errors.New("外层: " + notFound.Error())
	if !isTemplateNotFound(wrapped) {
		t.Fatal("错误文本里带 TemplateNotFound 的也应被认出来")
	}

	forbidden := &tea.SDKError{
		Code:       tea.String("ForbiddenAction"),
		StatusCode: tea.Int(400),
		Message:    tea.String("账号无权限"),
	}
	if isTemplateNotFound(forbidden) {
		t.Fatal("ForbiddenAction(没权限)不是模板不存在")
	}
	// ⚠️ **光看 HTTP 404 就该认** —— 阿里云在别的版本/别的接口里
	// 404 时不保证带 TemplateNotFound 这个码,只看码会漏判。
	only404 := &tea.SDKError{
		Code:       tea.String("SomethingElse"),
		StatusCode: tea.Int(404),
		Message:    tea.String("not found"),
	}
	if !isTemplateNotFound(only404) {
		t.Fatal("404 就该认成模板不存在,不能只看错误码")
	}
	if isTemplateNotFound(nil) {
		t.Fatal("nil 不是模板不存在")
	}
	if isTemplateNotFound(errors.New("网络超时")) {
		t.Fatal("网络错误不是模板不存在")
	}
}

// ---- 端到端(打桩 RPC) ----

// TestGetTemplateStatusNotFoundBecomesRejected 模板不存在要显示出来
//
// ⚠️ **这是整个模块最值钱的一条** ——
// 运营在配置中心把模板号敲错一位,页面上必须显示"模板号不存在",
// 而不是"同步失败"(那等于什么都没发生)。
func TestGetTemplateStatusNotFoundBecomesRejected(t *testing.T) {
	caller := &fakeCaller{err: &tea.SDKError{
		Code:       tea.String("TemplateNotFound"),
		StatusCode: tea.Int(404),
		Message:    tea.String("模板不存在"),
	}}
	st, err := getTemplateStatus(caller, "SMS_XXX")
	if err != nil {
		t.Fatalf("模板不存在不该是错误(它是一个结论), got %v", err)
	}
	if st.Status != smstpl.StatusRejected {
		t.Fatalf("模板不存在应映射成 rejected, got %q", st.Status)
	}
	if !strings.Contains(st.Reason, "不存在") {
		t.Fatalf("理由要说清楚是『不存在』, got %q", st.Reason)
	}
}

// TestGetTemplateStatusForbiddenIsError 没权限是故障,不是结论
func TestGetTemplateStatusForbiddenIsError(t *testing.T) {
	caller := &fakeCaller{err: &tea.SDKError{
		Code:       tea.String("ForbiddenAction"),
		StatusCode: tea.Int(400),
		Message:    tea.String("账号无权限"),
	}}
	st, err := getTemplateStatus(caller, "SMS_1")
	if err == nil {
		t.Fatalf("没权限必须报错(故障), got %+v", st)
	}
	if st.Status != "" {
		t.Fatalf("报错时不能带状态, got %q", st.Status)
	}
}

// TestGetTemplateStatusBusinessErrorCode HTTP 200 但业务码不是 OK
func TestGetTemplateStatusBusinessErrorCode(t *testing.T) {
	caller := &fakeCaller{body: map[string]interface{}{
		"Code":    "isv.SMS_TEMPLATE_ILLEGAL",
		"Message": "模板不存在",
	}}
	if _, err := getTemplateStatus(caller, "SMS_1"); err == nil {
		t.Fatal("业务码不是 OK 必须报错")
	}
}

// TestCallGetSmsTemplateRequestShape 请求长什么样
//
// ⚠️ **Action / Version / Style 写错,阿里云直接不认** ——
// 而这类错误在真网环境下表现为一句含糊的 400,排查起来很费时间。
func TestCallGetSmsTemplateRequestShape(t *testing.T) {
	caller := &fakeCaller{body: bodyOf("1", nil)}
	if _, err := callGetSmsTemplate(caller, "SMS_123"); err != nil {
		t.Fatal(err)
	}
	if caller.action != "GetSmsTemplate" {
		t.Fatalf("Action 应为 GetSmsTemplate, got %q", caller.action)
	}
	if got := tea.StringValue(caller.query["TemplateCode"]); got != "SMS_123" {
		t.Fatalf("TemplateCode 应为 SMS_123, got %q", got)
	}
}

// ---- FetchTemplateStatus 入口 ----

func TestFetchTemplateStatusEmptyCode(t *testing.T) {
	f := NewTemplateStatusFetcher(nil, configZero())
	if _, err := f.FetchTemplateStatus(context.Background(), ""); err == nil {
		t.Fatal("空模板号必须报错")
	}
}

func TestFetchTemplateStatusNilFetcher(t *testing.T) {
	var f *TemplateStatusFetcher
	if _, err := f.FetchTemplateStatus(context.Background(), "SMS_1"); err == nil {
		t.Fatal("未初始化的查询器必须报错,不能 panic")
	}
}

func TestFetchTemplateStatusCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := NewTemplateStatusFetcher(NewDeferred(configZero()), configZero())
	// ctx 已取消 ⇒ 不该发请求(这版 SDK 不吃 ctx,只能进门时拦一道)
	_, err := f.FetchTemplateStatus(ctx, "SMS_1")
	if err == nil {
		t.Fatal("ctx 已取消必须报错")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回 ctx 的错误, got %v", err)
	}
}

// ---- RPC 外层信封(2026-10-30 修的真 bug) ----
//
// # 背景:这个 bug 的表现极具误导性
//
// 用户报:"同步短信模版定时器记录显示成功,短信模版列表还是显示未同步"。
// 查下来:阿里云 RPC 风格接口的响应是**套了两层**的 ——
//
//	{ "body": {"Code":"OK","TemplateStatus":"1",...}, "headers": {...}, "statusCode":"200" }
//
// 业务字段在 body 里面。旧代码读顶层 ⇒ 永远读不到 TemplateStatus
// ⇒ 每个场景都判成"拉取失败" ⇒ 登记册永远停在 unknown
// ⇒ 页面永远显示"未同步";而定时任务因为"单个场景失败不算任务失败"照样记 success。
//
// ⇒ **一个纯粹的"取错了层级"的问题,表现成了"同步功能好像没生效"。**

// envelopeOf 把载荷包进 RPC 外层信封
func envelopeOf(payload map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"body":       payload,
		"headers":    map[string]interface{}{"content-type": "application/json"},
		"statusCode": "200",
	}
}

// TestGetTemplateStatusUnwrapsRPCEnvelope 信封包着也要读得出状态
func TestGetTemplateStatusUnwrapsRPCEnvelope(t *testing.T) {
	caller := &fakeCaller{body: envelopeOf(bodyOf("1", nil))}
	st, err := getTemplateStatus(caller, "SMS_1")
	if err != nil {
		t.Fatalf("包在信封里的正常响应必须能读出状态, got %v", err)
	}
	if st.Status != smstpl.StatusApproved {
		t.Fatalf("TemplateStatus=1 应映射成 approved, got %q", st.Status)
	}
}

// TestCallGetSmsTemplateReturnsInnerPayload 返回的应该是剥过信封的载荷
func TestCallGetSmsTemplateReturnsInnerPayload(t *testing.T) {
	caller := &fakeCaller{body: envelopeOf(bodyOf("0", nil))}
	got, err := callGetSmsTemplate(caller, "SMS_1")
	if err != nil {
		t.Fatal(err)
	}
	if _, has := got["body"]; has {
		t.Fatal("返回的应该是剥过信封的载荷,不该再套一层")
	}
	if _, has := got["TemplateStatus"]; !has {
		t.Fatalf("载荷里应有 TemplateStatus, got %#v", got)
	}
}

// TestCallGetSmsTemplateBusinessErrorInsideEnvelope 信封里的业务错码也要接住
//
// ⚠️ 旧代码查的是**顶层**的 Code —— 信封里的业务错码会被漏掉,
//
//	一个"HTTP 200 + 业务失败"的响应会被当成"没有状态字段"处理掉。
func TestCallGetSmsTemplateBusinessErrorInsideEnvelope(t *testing.T) {
	caller := &fakeCaller{body: envelopeOf(map[string]interface{}{
		"Code":    "isv.SMS_TEMPLATE_ILLEGAL",
		"Message": "模板不存在",
	})}
	if _, err := callGetSmsTemplate(caller, "SMS_1"); err == nil {
		t.Fatal("信封里的业务错码必须报错")
	}
}

// TestUnwrapPayloadKeepsFlatBody 扁平载荷不能被乱剥
func TestUnwrapPayloadKeepsFlatBody(t *testing.T) {
	flat := bodyOf("1", nil)
	got := unwrapPayload(flat)
	if got["TemplateStatus"] != "1" {
		t.Fatalf("扁平载荷应原样返回, got %#v", got)
	}
	// 载荷自己带 body 字段时,也不能被剥(那不是信封)
	withBody := map[string]interface{}{"Code": "OK", "TemplateStatus": "1", "body": "文本"}
	if got := unwrapPayload(withBody); got["TemplateStatus"] != "1" {
		t.Fatalf("自带 body 字段的载荷应原样返回, got %#v", got)
	}
}

// TestUnwrapPayloadIdempotent 剥两次结果不变
//
// ⚠️ mapTemplateStatus 里也调了一次 unwrapPayload —— 必须幂等,
//
//	否则第二次会把载荷里的东西当成信封再剥一次。
func TestUnwrapPayloadIdempotent(t *testing.T) {
	once := unwrapPayload(envelopeOf(bodyOf("1", nil)))
	twice := unwrapPayload(once)
	if twice["TemplateStatus"] != "1" {
		t.Fatalf("剥两次应得到同一个载荷, got %#v", twice)
	}
}
