// 业务错误码
// 文档 §7.5
package errcode

// 1xxxx 基础
const (
	Success          = 0
	BadRequest       = 40000
	ParamMissing     = 40001 // 必填参数缺失
	ParamInvalid     = 40002 // 参数格式错误
	InvalidParam     = 40003 // 参数无效(语义与 ParamInvalid 区分:此处是"我拒绝接受",后者是"格式不对")
	Unauthorized     = 40100
	TokenExpired     = 40101
	TokenInvalid     = 40102
	Forbidden        = 40300
	NoActiveIdentity = 40301 // 身份已停用
	OutOfScope       = 40302 // 超出工作范围
	IdorAttempt      = 40303 // 越权访问他人资源
	NotFound         = 40400
	Conflict         = 40900
	TooManyRequests  = 42900
	PayloadTooLarge  = 41300 // 请求体超过限制(§1.3 #16 BodyLimit)
	Internal         = 50000
)

// 2xxxx 业务
const (
	LoginFailed        = 20100
	UserLocked         = 20101
	CaptchaWrong       = 20102
	CaptchaRequired    = 20103
	SmsTooFrequent     = 20104
	SmsCodeInvalid     = 20105
	SmsSendTooFrequent = 20108 // 同一 phone+scene 冷却期内重复发送
	// MfaRequired 登录二次验证(M150):密码已验对,还要一条发到绑定手机的验证码。
	//
	// ⚠️ 它**不是失败**—— 密码已经对了,流程停在"等验证码"这一步。
	// 前端收到它要切到 MFA 输入码面板,而不是按"登录失败"计数/报错。
	// 登录页的软锁定(fail count)不许对 20109 计数,否则开着 MFA 的用户
	// 每次登录都被当成失败一次,五次之后被前端软锁在门外。
	MfaRequired         = 20109
	PasswordWeak        = 20106
	PasswordExpired     = 20107
	SameAuditor         = 20200 // 提交人与审核人相同
	SelfPromotion       = 20201 // 给自己提权
	OverScope           = 20202 // 越权授予
	StateInvalid        = 20203 // 状态机不允许
	CoOwnerCodeRequired = 20210 // 操作需要全部共有人人脸验证
	AssetNotReady       = 20300
	MaterialNotAudited  = 20400
	HouseNotExists      = 20500
	ProjectArchive      = 20501 // 项目已归档

	// ⚠️ **206xx 是审批系统专段**(M83 新开)。
	// 已占用段:201xx 验证码 / 202xx 权限与审核(20200-20210)/ 20300 资产 /
	// 20400 资料 / 20500 房屋。**不要往 202xx 里插** —— 20203 与 20210 之间
	// 已经被 CoOwnerCodeRequired 占着。
	ApprovalNoTemplate = 20600 // 该 biz_type 没有可用的启用模板
	ApprovalNoAssignee = 20601 // 审批人解析为空 / 解析报错
	// ⚠️ 20602 / 20603 是 M85 审批人解析新开的,见下方同名的两段注释
	//
	// ⚠️ **ApprovalNoAssignee 不许混进 InvalidParam** —— 它是"配置错了",
	// 不是"操作错了":发起人处理不了,得管理员去改审批流程模板。
	// 分类错了前端就会弹"参数有误,请重试",用户永远等不到人来修。

	// ── 206xx 第二段(M85 审批人解析)────────────────────────────
	// ⚠️ **这两个是「组织架构 / 规则配置本身坏了」**,不是「没查到审批人」。
	// 分成两个码是因为**要修的人不一样**:20600-20601 找审批流程管理员,
	// 20602 找 HR(部门树),20603 找流程管理员(规则写错了)。
	ApprovalDeptTreeBroken = 20602 // 部门树成环 / parent_id 悬空(§5.2 第 2 条)
	ApprovalRuleInvalid    = 20603 // 规则本身配错:未知 rule_type / 角色不存在(§5.1)
)

// 3xxxx 平台配置中心
const (
	ConfigNotFound      = 30100
	ConfigReadOnly      = 30101
	ConfigChangePending = 30200
	ConfigNotAudited    = 30201
	ConfigSelfReview    = 30202 // 配置变更自审(申请人与审批人相同)
	ConfigInvalidState  = 30203 // 配置变更状态机不允许
)

// 5xxxx 框架
const (
	NotImplemented       = 50100 // 业务尚未实现
	ServiceUnavailable   = 50300 // 服务暂时不可用(熔断 open / 超时)
	GatewayTimeout       = 50400 // 网关超时(handler 超时未写入响应)
	VersionNotSupported  = 50500 // 请求的 API 版本不被支持(X-API-Version 超出 MaxMajor)
	SignatureMissing     = 50600 // 缺少 X-Signature header
	SignatureExpired     = 50601 // 签名 ts 超出允许的时钟漂移(防重放)
	SignatureInvalid     = 50602 // 签名 sig 不匹配(secret 错 / 请求被篡改)
	PayloadMalformed     = 50700 // 请求体格式错误(gzip 解压失败 / JSON 损坏 / 编码不支持)
	UnsupportedMediaType = 50701 // Content-Encoding 不支持(仅支持 gzip/identity)
)

// 错误码 -> 文案(默认)
var defaultMsg = map[int]string{
	Success:          "ok",
	BadRequest:       "请求参数错误",
	ParamMissing:     "必填参数缺失",
	ParamInvalid:     "参数格式错误",
	InvalidParam:     "参数无效",
	Unauthorized:     "未登录或登录已过期",
	TokenExpired:     "登录已过期",
	TokenInvalid:     "令牌无效",
	Forbidden:        "没有权限",
	NoActiveIdentity: "当前身份已停用",
	OutOfScope:       "超出工作范围",
	IdorAttempt:      "无权访问该资源",
	NotFound:         "资源不存在",
	Conflict:         "数据冲突",
	TooManyRequests:  "请求过于频繁",
	PayloadTooLarge:  "请求体超过限制",
	Internal:         "服务内部错误",

	LoginFailed:         "登录失败",
	UserLocked:          "账号已锁定",
	CaptchaWrong:        "图形验证码错误",
	CaptchaRequired:     "请先通过图形验证码",
	SmsTooFrequent:      "短信发送过于频繁",
	SmsCodeInvalid:      "短信验证码错误或已过期",
	SmsSendTooFrequent:  "短信发送过于频繁,请稍后再试",
	MfaRequired:         "该账号已开启二次验证,请输入短信验证码",
	PasswordWeak:        "密码强度不足",
	PasswordExpired:     "密码已过期,请修改",
	SameAuditor:         "提交人与审核人不能为同一人",
	SelfPromotion:       "不能给自己授予更高权限",
	OverScope:           "不能授予超出自身范围的权限",
	StateInvalid:        "当前状态不允许此操作",
	CoOwnerCodeRequired: "此操作需要全部共有人人脸验证",
	AssetNotReady:       "资产未就绪",
	MaterialNotAudited:  "资料未通过审核",
	HouseNotExists:      "房屋不存在",
	ProjectArchive:      "项目已归档,只读",

	ApprovalNoTemplate: "该业务类型没有配置可用的审批流程",
	ApprovalNoAssignee: "审批流程配置有误:解析不出审批人,请联系管理员检查流程配置",

	ApprovalDeptTreeBroken: "部门架构数据异常(成环或上级不存在),审批无法定位主管,请联系管理员修复组织架构",
	ApprovalRuleInvalid:    "审批人规则配置有误,请联系管理员检查审批流程配置",

	ConfigNotFound:      "配置项不存在",
	ConfigReadOnly:      "配置项为只读",
	ConfigChangePending: "存在未审批的变更",
	ConfigNotAudited:    "变更未审核",
	ConfigSelfReview:    "申请人与审批人不能为同一人",
	ConfigInvalidState:  "配置变更状态不允许此操作",

	NotImplemented:       "功能未实现",
	ServiceUnavailable:   "服务暂时不可用,请稍后重试",
	GatewayTimeout:       "请求处理超时",
	VersionNotSupported:  "请求的 API 版本不被支持",
	SignatureMissing:     "缺少接口签名",
	SignatureExpired:     "接口签名已过期,请重试",
	SignatureInvalid:     "接口签名校验失败",
	PayloadMalformed:     "请求体格式错误",
	UnsupportedMediaType: "不支持的 Content-Encoding(仅支持 gzip/identity)",
}

// Msg 取得错误码默认文案
func Msg(code int) string {
	if s, ok := defaultMsg[code]; ok {
		return s
	}
	return "未知错误"
}

// HTTPStatus 业务码 -> HTTP 状态码
// 文档 §7.5
//
// # ⚠️ 顺序:必须从窄到宽
//
// 401xx / 403xx / 404xx / 409xx / 413xx / 429xx **全都落在 40000-49999 这个
// 大桶里**。所以细分桶必须排在"40000-49999 → 400"**之前**,否则它们会被大桶
// 先吃掉、全部返 400。
//
// # 这一条**真的错过**,而且是"三份拷贝"造成的(2026-10-26 M98 收口)
//
// 同一个映射曾经有**三份**实现,各错各的:
//
//  1. `response.httpStatus`(私有,**活着的**那份)—— 缺 413 桶(`41300` → 400)、缺 501(`50100` → 500)
//  2. 本函数(导出的)—— 大桶 `40000-41000 → 400` 排在细分桶**前面**,于是 40100/40300/40400/40900 全被吃掉返 400;50300/50400 兜底成 500
//  3. `cmd/gen-errcode-doc` 里的一份拷贝(给生成文档用)—— 4xxxx 对,但缺 501/503/504 → `docs/error-codes.md` 里这三个码全写 500
//
// 2026-10-18 修过一次(把 `response` 那份的 401/403/404/409 修对),但**只修了
// 活着的那一份** —— 本函数一直保留着错顺序,而且它**零调用方**:没人用它,
// 自然没人发现它错。
//
// M98 把三份合成**这一份**:本函数是唯一实现,`response` 与文档生成器都调它。
func HTTPStatus(code int) int {
	switch {
	case code == Success:
		return 200
	// ── 4xxxx 细分桶(必须在大桶之前)─────────────────────────────
	case code >= 40100 && code < 40200:
		return 401
	case code >= 40300 && code < 40400:
		return 403
	case code >= 40400 && code < 40500:
		return 404
	case code >= 40900 && code < 41000:
		return 409
	case code >= 41300 && code < 41400:
		return 413
	case code >= 42900 && code < 43000:
		return 429
	// ── 5xxxx 特殊码(同样必须在大桶之前)─────────────────────────
	//
	// ⚠️ **NotImplemented 必须是 501,不是 500**:"这个 provider 没有这个能力"
	// (501,该换条路走)和"它炸了"(500,该重试 / 该告警)在客户端是完全不同的两种
	// 处理。混成 500 的代价很具体:前端 `api/client.ts` 的 `switch (status)` 里
	// 500 走"服务器内部错误"通用文案、**根本不看 body 里的业务码** ——
	// 用户看到的是"服务器内部错误"而不是"该功能尚未实现"。
	case code == NotImplemented:
		return 501
	case code == ServiceUnavailable:
		return 503
	case code == GatewayTimeout:
		return 504
	// ── 大桶 ─────────────────────────────────────────────────────
	case code >= 40000 && code < 50000:
		return 400
	case code >= 50000 && code < 60000:
		return 500
	default:
		// 业务码 2xxxx / 3xxxx:HTTP 200,语义由 body 里的 code + i18n 承载
		return 200
	}
}

// HTTPRaw 已废弃 —— 503 / 504 的细分已并入 HTTPStatus,两者现在**完全等价**。
//
// 保留它只是为了不破坏外部调用方(它是导出 API);新代码直接用 HTTPStatus。
//
// ⚠️ 它跟 HTTPStatus 曾经是**两份不同的实现**:HTTPRaw 里 503/504 走精确值,
// 而 HTTPStatus 把它们兜底成 500 —— 同一个码在两个函数里两个状态码。
// M98 合并成一份。
func HTTPRaw(code int) int { return HTTPStatus(code) }
