// 审计采集端 —— 补写 request_body / request_params / response_summary / location
//
// # 为什么单独成文件
//
// audit.go 回答"**什么时候记**",这个文件回答"**记什么、怎么脱敏**"。
// 两者混在一起的后果是:改采集逻辑要去翻"记不记"的判断,改"记不记"又要去翻
// 脱敏词表 —— 迟早有一处改漏。
//
// # ⚠️ **这里的每一条最终都躺在"保留 5 年、可批量导出"的合规表里**
//
// `sys_operation_log` 按技术方案 §9.5 是合规表(保留 5 年、可导出上报)。
// ⇒ 落进去的东西一旦含明文凭证 / PII,等于把最高危字段**复制**到一个
// 更容易被批量读到的地方 —— 而且这份副本**比原始业务表更难撤回**
// (业务表可以改行,合规表按规矩是只写不改)。
//
// ⇒ 三条硬规矩(每条都有对应的可逆探针):
//
//  1. 请求体 / query 一律先过 `desensitize`(词表见 pkg/desensitize/body.go)
//  2. **响应体不落原文** —— 只落"骨架"(code + data 的字段名)
//  3. 解析不出来 / 不是 JSON / 超上限 ⇒ 落**空串**,绝不落原文
//
// # ⚠️ M117-1 那个脱敏器此前是**零生产调用方**的
//
// `pkg/desensitize.RedactJSON` / `RedactQuery` 在 2026-10-30 落地时带了一整套
// 单测,但**全仓没有一处调用** —— 审计表里 request_body / request_params 两列
// 依然是空的,前端抽屉里那两行永远是"空值"。
// ⇒ 这个文件就是那个脱敏器的**第一个生产调用方**。
//
// # ⚠️ 采集 body 必须在 `c.Next()` **之前**
//
// 两条理由,一条是"必须",一条是"应该":
//
// **① 必须**(流式 body):`StreamRequestBody = true` 时 body 是**流**,
// `Request.Body()` 会把它读干(`BodyE()` 里的 `CopyZeroAlloc` +
// `CloseBodyStream`)—— 那时 handler 就绑不到了,整个接口 400。
// ⇒ 这里显式判 `IsBodyStream()` 并跳过。
//
// **② 应该**(handler 改写 body):hertz 的 `Request.SetBody` /
// `SetBodyString` 内部走 `BodyBuffer()`,而 `BodyBuffer()` 会把 `bodyRaw`
// 置 nil(hertz pkg/protocol/request.go:156)。
// ⇒ handler 一旦改写请求体(解密中间件 / 字段归一化),
// `c.Next()` 之后取到的就是**改写后**的那份,客户端原始字节已经没了。
// ⇒ 审计要记的是"客户端发了什么",所以必须在 handler 跑之前取。
//
// ⚠️ **默认配置下两条都不致命**:`StreamRequestBody = false` 时 body 在跑
// handler **之前** 就被读进 `req.bodyRaw`(`protocol/http1/req/request.go`
// 的 `ContinueReadBody` → `SetBodyRaw`),而 `Body()` 是**只读 getter**,
// 读它不会消费掉 —— ⇒ 提前读安全,且"读之后"通常也还读得到。
//
// ⚠️ 但"通常"不是"一定"(理由 ② 就是反例)⇒ 取在前面。
package middleware

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/desensitize"
)

const (
	// maxAuditBodyBytes 请求体采集上限(8KB)
	//
	// ⚠️ **为什么限** —— 审计表是**只写不改**的,写进去就是永久占用。
	// 而 body 大小没有上界:一个 5MB 的 JSON 会被整段塞进一列 TEXT。
	// 8KB 足够放下绝大多数"这次动了什么字段"的审计诉求。
	//
	// ⚠️ 超限**不是错误** —— 落空串就行。审计少一列,不该让请求失败。
	maxAuditBodyBytes = 8 * 1024

	// maxAuditResponseBytes 响应体采集上限(8KB)
	//
	// 同上。而且响应体比请求体更容易超大(列表接口一次返回几千行),
	// 超限就只落 code,不留 data_keys。
	maxAuditResponseBytes = 8 * 1024
)

// auditRequestBody 脱敏后的请求体(非 JSON / 超限 / 解析失败 ⇒ 空串)
//
// ⚠️ **只收 JSON** —— multipart 是文件上传(整个文件就是 body),
// 分片上传是裸字节;把它们当 JSON 解析只会失败,失败就落空串。
// 真正要做的是先按 Content-Type 分流,别让 5MB 的文件去撞 JSON 解析器。
func auditRequestBody(c *app.RequestContext) string {
	if !isJSONContentType(c.Request.Header.ContentType()) {
		return ""
	}
	// ⚠️ 流式 body 取了就把流读干了(handler 随后绑不上)⇒ 不碰
	if c.Request.IsBodyStream() {
		return ""
	}
	raw := c.Request.Body()
	if len(raw) == 0 || len(raw) > maxAuditBodyBytes {
		return ""
	}
	// ⚠️ RedactJSON 解析失败时返 ok=false —— 这时**绝不能把原文写进审计表**
	// (原文可能整个 body 都是明文密码)。
	out, ok := desensitize.RedactJSON(raw)
	if !ok {
		return ""
	}
	return out
}

// auditRequestParams 脱敏后的 query string
//
// ⚠️ **与 request_body 用同一份词表** —— 否则同一个 `token` 参数
// 在 body 里被遮、在 query 里没被遮,就是一个洞。
// (词表是 pkg/desensitize 里那一份,这里不另立一套。)
func auditRequestParams(c *app.RequestContext) string {
	return desensitize.RedactQuery(string(c.URI().QueryString()))
}

// auditRequestURL 完整请求 URL:路径 + **脱敏后**的 query
//
// ⚠️ **query 走的是脱敏后的版本**,不是原始串 ——
// 原始 query 里可能有 `keyword=张三` 这种 PII,而这一列是明文列。
// 审计表要有"这次带了什么参数",不需要"参数里那个人叫什么"。
//
// ⚠️ **路径里可能有文件名**(`/rapid-admin/{uid}/{biz}/{fid}/{name}`)——
// 文件名是用户自己起的,算业务内容,不算 PII,不遮。
func auditRequestURL(c *app.RequestContext, params string) string {
	p := string(c.Path())
	if params == "" {
		return truncateRunes(p, 255)
	}
	return truncateRunes(p+"?"+params, 255)
}

// auditLocation 这次动的是**哪个对象**(路径参数)
//
// 形如 `id=9527`。
//
// ⚠️ **只有路由带路径参数时才有值** ——
// `PATCH /users/:id` 有,`POST /users`(新建)没有。
// 新建的对象 id 在**响应**里,而响应体不落原文 ⇒ 这一列对新建操作是空的。
// 这是"宁可不写也不猜"的取舍:猜错了比空着更坏。
func auditLocation(c *app.RequestContext) string {
	// ⚠️ `c.Params` 是**字段**不是方法(hertz 的 RequestContext.Params 是
	// param.Params 切片),写成 c.Params() 编译不过。
	params := c.Params
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, p.Key+"="+p.Value)
	}
	// location 列是 size:64
	return truncateRunes(strings.Join(parts, "&"), 64)
}

// auditResponseSummary 响应的**骨架**,不是响应体本身
//
// 形如 `{"code":0,"data_keys":["list","page","size","total"]}`。
//
// # ⚠️ **为什么不落响应体原文**
//
// 响应体里全是业务数据:用户列表带解密后的手机号、业主信息带身份证号。
// 而技术方案 §9.5 对这一列的定义只写了一个词:"**摘要**"。
// ⇒ 落"字段名骨架"正好落在"摘要"这个词上:它回答"这次操作返回了什么形状",
// 又不把 PII 复制进合规表。
//
// # ⚠️ **为什么连 message 都不落**
//
// `error_msg` 那一列已经落了失败响应里的 message(见 audit.go::auditErrorMsg),
// 再落一份就是同一句话在表里存两遍 —— 而且 message 是 handler 拼的,
// 有可能把用户输入拼进去。
//
// # ⚠️ **只收 JSON**
//
// 文件下载、验证码图片这些响应不是 JSON,解析会失败 ⇒ 落空串。
func auditResponseSummary(c *app.RequestContext) string {
	if !isJSONContentType(c.Response.Header.ContentType()) {
		return ""
	}
	body := c.Response.Body()
	if len(body) == 0 || len(body) > maxAuditResponseBytes {
		return ""
	}
	var env struct {
		// ⚠️ 用指针:**响应体里没有 code 时**要能分辨"没有"和"是 0"
		// (框架响应体 code=0 表示成功,但没有 code 字段是另一回事)
		Code *int `json:"code"`
		Data any  `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	// ⚠️ UseNumber:不让数字变成 float64(19 位 ID 会变成 1.23e+18)
	dec.UseNumber()
	if err := dec.Decode(&env); err != nil {
		return ""
	}
	out := map[string]any{}
	if env.Code != nil {
		out["code"] = *env.Code
	}
	if env.Data != nil {
		switch d := env.Data.(type) {
		case map[string]any:
			keys := make([]string, 0, len(d))
			for k := range d {
				keys = append(keys, k)
			}
			// 排序 ⇒ 输出稳定(同一次操作的两次审计记录可以 diff)
			sort.Strings(keys)
			out["data_keys"] = keys
		case []any:
			out["data_type"] = "array"
			out["data_count"] = len(d)
		default:
			out["data_type"] = "scalar"
		}
	}
	if len(out) == 0 {
		return ""
	}
	buf, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return truncateRunes(string(buf), 2000)
}

// isJSONContentType 判断是不是 JSON
//
// ⚠️ **要认 `application/json; charset=utf-8`** ——
// 带参数的 Content-Type 非常常见,直接比 `== "application/json"` 会全部落空。
//
// ⚠️ **要认 `*+json`** ——
// `application/vnd.api+json` / `application/problem+json` 这类也是 JSON。
func isJSONContentType(ct []byte) bool {
	s := strings.ToLower(strings.TrimSpace(string(ct)))
	if s == "" {
		return false
	}
	// 去掉 ;charset=utf-8 之类参数
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s == "application/json" || strings.HasSuffix(s, "+json")
}
