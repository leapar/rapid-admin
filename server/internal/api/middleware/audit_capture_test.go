// 审计采集端单测 —— request_body / request_params / response_summary / location
//
// # 这条网守的是什么
//
// 2026-10-30 查出来的事实:`pkg/desensitize` 那个通用脱敏器
// (`RedactJSON` / `RedactQuery`)落地时带了一整套单测,但**全仓零生产调用方** ——
//
//	⇒ 审计表里 request_body / request_params 两列**恒空**
//	⇒ 前端抽屉里那两行永远是"空值"
//	⇒ 前端注释里"仓库里目前没有通用的请求体脱敏器"这句话**已经不成立**
//
// 这个文件是那个脱敏器的**第一个生产调用方**的测试。
//
// # ⚠️ **最要紧的一条是 TestAudit_HandlerStillBindsBody**
//
// 采集请求体必须在 `c.Next()` **之前**做 —— 但**理由不是"body 是一次性的"**
// (那是最初的写法,已被探针 ③ 推翻:默认 `StreamRequestBody=false` 时 body
// 在跑 handler **之前**就缓冲进 `req.bodyRaw`,`Body()` 是只读 getter,
// `c.Next()` 之后照样读得到)。
//
// 真正的两条理由:
// ① **必须**:流式 body 下 `Request.Body()` 会把流读干,handler 随后绑不上
// ⇒ 整个接口 400。
// ② **应该**:handler 改写请求体(`SetBody` → `BodyBuffer()` → `bodyRaw=nil`)
// 之后,再取到的就是**改写后**的那份 ⇒ 审计记的就不再是"客户端发了什么"。
//
// 而"提前读 body"这件事本身有风险:hertz 的 `Request.Body()` 在
// **流式 body** 下会把流读干,handler 随后绑不上 ⇒ 整个接口 400。
//
// 这条用例就是那个风险的守卫:handler 真的 `c.Bind()` 一次,
// 断言它拿到的是**完整的原始 body**。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	拆法                                              必须变红
//	──────────────────────────────────────────────  ───────────────────────────────
//	把 auditRequestBody 整个删掉                    TestAudit_WritesRequestBodyRedacted
//	把 RedactJSON 换成"原样返回"                     同上(明文密码必须消失)
//	把采集挪到 c.Next() 之后                        TestAudit_RecordsBodyAsClientSentIt
//	把 auditRequestParams 换成"原样返回"              TestAudit_WritesRequestParamsRedacted
//	把 auditResponseSummary 改成落响应体原文          TestAudit_ResponseSummaryIsSkeleton
//	把 auditLocation 删掉                            TestAudit_WritesLocation
//	把 isJSONContentType 改成只认精确的 application/json
//	                                                 TestAudit_JSONBodyWithCharset
//	中间件漏调 c.Next()                             TestAudit_FailedRequestRecordedAsFailure
//	                                                 TestAudit_ResponseSummaryIsSkeleton
package middleware

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/service"
)

// m126AuditRows 起一个只带审计中间件的引擎,打一发请求,返回落库的审计行
//
// ⚠️ **不自己造 AuditSvc** —— 用真的 `service.NewAuditService`,
//
//	免得"自己装配了自己"造成假绿(见 audit_db_test.go 的说明)。
func m126AuditRows(t *testing.T, db *gorm.DB, method, path string, body []byte, contentType string, handler func(ctx context.Context, c *app.RequestContext)) []model.SysOperationLog {
	t.Helper()

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())

	var headers []ut.Header
	var reqBody *ut.Body
	if contentType != "" {
		headers = append(headers, ut.Header{Key: "Content-Type", Value: contentType})
	}
	if body != nil {
		reqBody = &ut.Body{Body: bytes.NewReader(body), Len: len(body)}
	}

	switch method {
	case http.MethodPost:
		e.POST(path, handler)
	case http.MethodPut:
		e.PUT(path, handler)
	case http.MethodPatch:
		e.PATCH(path, handler)
	case http.MethodDelete:
		e.DELETE(path, handler)
	default:
		t.Fatalf("m126AuditRows 不支持的方法: %s", method)
	}
	ut.PerformRequest(e, method, path, reqBody, headers...)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	return rows
}

// m126Echo 读 body 并回显(验证 handler 仍能绑到 body)
func m126Echo(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		c.String(http.StatusBadRequest, "bind failed: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, response.Response{
		Code:    0,
		Message: "ok",
		Data:    map[string]any{"username": req.Username, "password": req.Password},
	})
}

// TestAudit_WritesRequestBodyRedacted request_body 要写进去,而且凭证要脱敏
func TestAudit_WritesRequestBodyRedacted(t *testing.T) {
	db := m117AuditDB(t)

	payload := []byte(`{"username":"alice","password":"s3cr3t-plain","phone":"13800000000"}`)
	rows := m126AuditRows(t, db, http.MethodPost, "/m126/body", payload, "application/json", m126Echo)
	require.Len(t, rows, 1)

	body := rows[0].RequestBody
	require.NotEmpty(t, body, "request_body 此前恒空 —— 现在必须有值")
	require.Contains(t, body, "alice", "业务字段要留着(遮蔽业务内容等于把审计记录废掉)")
	require.NotContains(t, body, "s3cr3t-plain", "⚠️ 明文密码绝不能进审计表")
	require.NotContains(t, body, "13800000000", "⚠️ 手机号也要遮")
	require.Contains(t, body, "***", "遮蔽后要有标记")
}

// TestAudit_HandlerStillBindsBody ⚠️ **提前读 body 不能把 body 吃掉**
//
// 这是本次改动**最锋利的边**:采集必须在 c.Next() 之前,
// 而流式 body 下 `Body()` 会把流读干 ⇒ handler 绑不上 ⇒ 接口 400。
func TestAudit_HandlerStillBindsBody(t *testing.T) {
	db := m117AuditDB(t)

	payload := []byte(`{"username":"alice","password":"s3cr3t"}`)
	rows := m126AuditRows(t, db, http.MethodPost, "/m126/bind", payload, "application/json", m126Echo)
	require.Len(t, rows, 1)

	// handler 绑到了 body(它回显了 username)⇒ 审计表里也该有
	require.Contains(t, rows[0].RequestBody, "alice")
	require.Equal(t, 1, rows[0].Status, "handler 绑不到 body 时会返 400,那一行 status 就是 0")
}

// TestAudit_RecordsBodyAsClientSentIt 审计要记**客户端发的原文**
//
// ⚠️ **这条是"采集必须在 c.Next() 之前"唯一真正说得清的理由**
//
// hertz 的 `Request.SetBody` / `SetBodyString` 内部走 `BodyBuffer()`,
// 而 `BodyBuffer()` 会把 `bodyRaw` 置 nil(见 hertz pkg/protocol/request.go:156)。
//
// ⇒ handler 一旦改写请求体(解密中间件 / 字段归一化 / 补默认值),
// `c.Next()` 之后取到的 body 就是**改写后**的那份,原始字节已经没了。
//
// ⇒ 审计记的必须是"客户端发了什么",不是"handler 把 body 改成了什么"——
// 否则事后复盘看到的是被中间件改过的版本,原始请求凭空消失。
func TestAudit_RecordsBodyAsClientSentIt(t *testing.T) {
	db := m117AuditDB(t)

	rows := m126AuditRows(t, db, http.MethodPost, "/m126/rewrite",
		[]byte(`{"username":"alice"}`), "application/json",
		func(ctx context.Context, c *app.RequestContext) {
			// 模拟一个改写请求体的中间件(解密 / 归一化)
			c.Request.SetBodyString(`{"username":"mallory"}`)
			c.String(http.StatusOK, "ok")
		})
	require.Len(t, rows, 1)

	require.Contains(t, rows[0].RequestBody, "alice", "审计要记客户端发的原文,不是 handler 改写后的版本")
	require.NotContains(t, rows[0].RequestBody, "mallory", "⚠️ handler 改写后的内容不该顶替原始请求")
}

// TestAudit_NonJSONBodyNotCaptured 非 JSON 的 body 一律不落
//
// multipart 是文件上传(整个文件就是 body)、text/plain 是裸文本 ——
// 把它们当 JSON 解析只会失败,而"解析失败"绝不能退化成"落原文"。
func TestAudit_NonJSONBodyNotCaptured(t *testing.T) {
	db := m117AuditDB(t)

	rows := m126AuditRows(t, db, http.MethodPost, "/m126/plain",
		[]byte("raw-not-json"), "text/plain", m126Echo)
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].RequestBody, "非 JSON body 不该落库")
	require.NotContains(t, rows[0].RequestBody, "raw-not-json")
}

// TestAudit_InvalidJSONBodyNotCaptured Content-Type 说谎时也不落原文
func TestAudit_InvalidJSONBodyNotCaptured(t *testing.T) {
	db := m117AuditDB(t)

	rows := m126AuditRows(t, db, http.MethodPost, "/m126/badjson",
		[]byte("this is not json at all"), "application/json", m126Echo)
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].RequestBody, "解析失败 ⇒ 落空串")
	require.NotContains(t, rows[0].RequestBody, "not json")
}

// TestAudit_JSONBodyWithCharset Content-Type 带 charset 也要认
//
// `application/json; charset=utf-8` 是最常见的写法 ——
// 只认精确的 "application/json" 的话,这一整类请求的 body 全都落空。
func TestAudit_JSONBodyWithCharset(t *testing.T) {
	db := m117AuditDB(t)

	rows := m126AuditRows(t, db, http.MethodPost, "/m126/charset",
		[]byte(`{"username":"alice"}`), "application/json; charset=utf-8", m126Echo)
	require.Len(t, rows, 1)
	require.Contains(t, rows[0].RequestBody, "alice")
}

// TestAudit_LargeBodyNotCaptured 超大 body 不落库
func TestAudit_LargeBodyNotCaptured(t *testing.T) {
	db := m117AuditDB(t)

	big := []byte(`{"username":"` + strings.Repeat("x", 9000) + `"}`)
	rows := m126AuditRows(t, db, http.MethodPost, "/m126/big", big, "application/json", m126Echo)
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].RequestBody, "超上限的 body 不该整段进表")
}

// TestAudit_WritesRequestParamsRedacted query 要落进去,而且要脱敏
func TestAudit_WritesRequestParamsRedacted(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.POST("/m126/params", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, http.MethodPost, "/m126/params?page=2&token=raw-token-value", nil)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)

	params := rows[0].RequestParams
	require.NotEmpty(t, params, "request_params 此前恒空")
	require.Contains(t, params, "page=2", "普通参数要留着")
	require.NotContains(t, params, "raw-token-value", "⚠️ token 不能明文进表")
}

// TestAudit_RequestURLUsesRedactedQuery request_url 里也不能有明文 token
//
// ⚠️ request_url 是**明文列** —— 它要是带原始 query,
//
//	脱敏就白做了(token 还在表里,只是旁边多了一列遮过的)。
func TestAudit_RequestURLUsesRedactedQuery(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.POST("/m126/url", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, http.MethodPost, "/m126/url?force=1&password=hunter2", nil)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)

	require.Contains(t, rows[0].RequestURL, "/m126/url", "路径要在")
	require.Contains(t, rows[0].RequestURL, "force=1", "query 也要在(这是 request_url 存在的意义)")
	require.NotContains(t, rows[0].RequestURL, "hunter2", "⚠️ 明文密码不能出现在 request_url 里")
}

// TestAudit_ResponseSummaryIsSkeleton response_summary 是**骨架**,不是响应体原文
//
// ⚠️ 响应体里全是带 PII 的业务数据(手机号 / 身份证)。
//
//	把原文落进保留 5 年的合规表 = 把高危字段复制一份出去。
func TestAudit_ResponseSummaryIsSkeleton(t *testing.T) {
	db := m117AuditDB(t)

	rows := m126AuditRows(t, db, http.MethodPost, "/m126/resp",
		[]byte(`{"username":"alice"}`), "application/json", m126Echo)
	require.Len(t, rows, 1)

	summary := rows[0].ResponseSummary
	require.NotEmpty(t, summary, "response_summary 此前恒空")
	require.Contains(t, summary, "data_keys", "骨架里要有 data 的字段名")
	require.Contains(t, summary, "username", "字段名要留着(它回答「返回了什么形状」)")
	require.NotContains(t, summary, "s3cr3t", "⚠️ 响应里的值一个都不能落")
}

// TestAudit_ResponseSummaryEmptyForNonJSON 非 JSON 响应 ⇒ 空
func TestAudit_ResponseSummaryEmptyForNonJSON(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.POST("/m126/plainresp", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "plain text body")
	})
	ut.PerformRequest(e, http.MethodPost, "/m126/plainresp", nil)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].ResponseSummary)
}

// TestAudit_WritesLocation 路径参数要落进 location
func TestAudit_WritesLocation(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.PUT("/m126/items/:id", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, http.MethodPut, "/m126/items/9527", nil)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "id=9527", rows[0].Location, "这次动的是哪个对象")
}

// TestAudit_LocationEmptyWithoutPathParam 没有路径参数 ⇒ 空(不猜)
func TestAudit_LocationEmptyWithoutPathParam(t *testing.T) {
	db := m117AuditDB(t)

	rows := m126AuditRows(t, db, http.MethodPost, "/m126/new", nil, "", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].Location, "没有路径参数就不该编一个出来")
}

// TestAudit_ReadMethodHandlerStillRuns 读请求的 handler 必须照常跑
//
// # ⚠️ **这条用例曾经写错过,值得留个疤**
//
// 2026-10-30 第一版写的是"中间件漏调 c.Next() ⇒ handler 根本不会被调用",
// 并把它当成 M126 唯一的"不会被现有用例盖住"的风险点。
//
// **那个说法是错的**,而且是**可逆探针实测出来的**:
//
//	hertz 的 `RequestContext.Next` 是**循环**,不是"调用下一个 handler 一次":
//
//	    ctx.index++
//	    for ctx.index < int8(len(ctx.handlers)) { ctx.handlers[ctx.index](c, ctx); ctx.index++ }
//
// ⇒ 漏调 `c.Next()` **不会**让 handler 不跑 —— engine 自己的 Next 循环接着跑。
//
//	真正会坏的是**顺序**:中间件剩下的代码跑在 handler **之前**。
//
// ⇒ 探针 H(删掉 c.Next())打红的是 `TestAudit_FailedRequestRecordedAsFailure`
//
//	(status 恒为 1)和 `TestAudit_ResponseSummaryIsSkeleton`(摘要恒空),
//	**不是**这条用例 —— 它在链断的情况下**照样是绿的**。
//
// ⇒ 这条用例现在的作用降级成:守住"读请求不被中间件拦掉"这一条(比如
//
//	哪天有人给读分支加了 Abort / 提前 return,这里会响)。
//	**它守不住链的顺序** —— 顺序由上面那两条守着。
func TestAudit_ReadMethodHandlerStillRuns(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	called := false
	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.GET("/m126/read", func(ctx context.Context, c *app.RequestContext) {
		called = true
		c.String(http.StatusOK, "ok")
	})
	w := ut.PerformRequest(e, http.MethodGet, "/m126/read", nil)

	require.True(t, called, "⚠️ 读请求的 handler 没被调用 —— 中间件把读请求拦掉了")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok", w.Body.String())
}
