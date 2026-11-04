// 审计中间件(写操作落 sys_operation_log)
// 文档 §10.6
package middleware

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"go.uber.org/zap"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/service"
)

// Audit 操作审计(只记录写操作:POST/PUT/PATCH/DELETE)
//
// # ⚠️ M99(2026-10-26)补:同时落 audit.log
//
// `pkg/logger` 从一开始就有 audit.log 这个独立通道(`logger.Audit()`),
// 但**全仓零调用方** —— 通道建好了没人往里写,文件连创建都不会发生。
//
// 这里补上写入方:文件轨(便宜、滚动、不依赖 DB)和 DB 轨
// (sys_operation_log,可查询可上报)各留一份,互为备份。
//
// 文件轨**不依赖 AuditSvc** —— DB 审计服务没装配时,文件轨照样有,
// 否则"审计服务没挂"就等于"没有审计"。
func Audit() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		start := time.Now()
		method := string(c.Method())
		isWrite := method != consts.MethodGet && method != consts.MethodHead && method != consts.MethodOptions

		// ⚠️ **必须在 c.Next() 之前采集**(2026-10-30 M126)
		//
		// 两条理由,一条"必须"一条"应该"(完整复盘见
		// audit_capture.go 的文件头):
		//
		// ① **必须**:流式 body(`StreamRequestBody=true`)时 `Request.Body()`
		//    会把流读干 —— 那时 handler 就绑不到了,整个接口 400。
		// ② **应该**:hertz 的 `Request.SetBody` / `SetBodyString` 走
		//    `BodyBuffer()`,而它把 `bodyRaw` 置 nil ⇒ handler 一旦改写
		//    请求体(解密 / 归一化),之后再取到的就是**改写后**的那份,
		//    客户端原始字节已经没了。审计要记"客户端发了什么"。
		//
		// ⚠️ 默认配置(`StreamRequestBody=false`)下两条都不致命 ——
		// body 在跑 handler 之前就缓冲进 `req.bodyRaw`,`Body()` 是只读 getter。
		// 但"通常"不是"一定"(理由 ② 就是反例)⇒ 取在前面。
		var (
			reqBody   string
			reqParams string
			location  string
		)
		if isWrite {
			reqBody = auditRequestBody(c)
			reqParams = auditRequestParams(c)
			location = auditLocation(c)
		}

		// ⚠️ **c.Next() 必须无条件执行一次,而且必须在写审计之前** ——
		//
		// hertz 的 `RequestContext.Next` 是**循环**,不是"调用下一个 handler 一次":
		//
		//	func (ctx *RequestContext) Next(c context.Context) {
		//		ctx.index++
		//		for ctx.index < int8(len(ctx.handlers)) {
		//			ctx.handlers[ctx.index](c, ctx)
		//			ctx.index++
		//		}
		//	}
		//
		// ⇒ 漏调 `c.Next()` **不会**让 handler 不跑(engine 自己的 Next 循环会接着跑),
		//   但会让**这里剩下的代码跑在 handler 之前** ⇒ 审计行写在请求发生之前:
		//   status 恒为 1(默认 200)、response_summary 恒空、error_msg 恒空。
		//
		// ⇒ 所以这行必须在"写审计"**之前**,且读操作那条分支也**照样要调**。
		c.Next(ctx)

		if !isWrite {
			return
		}
		// 1. 文件轨(audit.log)—— 先写,不依赖 DB
		caller, _ := service.FromContext(ctx)
		uid := uint64(0)
		role := ""
		username := ""
		if caller != nil {
			uid = caller.UserID
			role = caller.RoleCode
			username = caller.Username
		}
		logger.Audit().Info("audit",
			zap.Uint64("user_id", uid),
			zap.String("username", username),
			zap.String("role", role),
			zap.String("module", string(c.Path())),
			zap.String("action", method),
			zap.Int("status_code", c.Response.StatusCode()),
			zap.String("ip", c.ClientIP()),
			zap.String("request_id", string(c.GetHeader(response.HeaderRequestID))),
			zap.String("trace_id", string(c.GetHeader(response.HeaderTraceID))),
			zap.Int64("cost_ms", time.Since(start).Milliseconds()),
		)
		// 2. DB 轨(sys_operation_log)
		if AuditSvc == nil {
			return
		}
		// 1=成功 0=失败(基于 HTTP 状态)
		status := 1
		errMsg := ""
		if c.Response.StatusCode() >= 400 {
			status = 0
			errMsg = auditErrorMsg(c)
		}
		_ = AuditSvc.RecordOperation(ctx, &model.SysOperationLog{
			UserID:   uid,
			Username: username,
			UserRole: role,
			Module:   string(c.Path()),
			Action:   method,
			// ⚠️ RequestURL 记的是**请求 URI**(路径 + query),
			//   Module 记的是**请求路径**(c.Path(),不含 query)。
			//   差的就是 query 那一段 —— `?userId=123` / `?force=1` 这种
			//   "这次到底动了哪个对象"的信息只在 query 里。
			//   这一列此前恒空(2026-10-30 补)。
			//
			//   用 `URI().RequestURI()` 而不是 `URI().String()`:
			//   后者带 scheme + host(`http://localhost:8888/...`),
			//   host 在审计里是噪声,还白占 255 字符的额度。
			//   RequestURI 保留**原始转义**(文件名里的 %E6%88%B7 之类不会丢)。
			//
			// ⚠️ **RequestMethod 故意不写** —— 它跟 `Action` 是**同一个值**
			//   (都是 HTTP 方法),写一遍就是往表里塞一份冗余副本。
			//   留着空列 + 这条注释,比写两份一模一样的数据强。
			//
			// ⚠️ **2026-10-30 M126 改:query 走脱敏后的版本**
			//
			//   原来这里落的是**原始 query**。但 `request_url` 是**明文列**,
			//   而 query 里可能有 `keyword=张三` 这种 PII —— 审计表保留 5 年、
			//   可批量导出,明文 PII 落进去等于把高危字段复制到更容易被读的地方。
			//   ⇒ 现在 query 统一走 `desensitize.RedactQuery`,
			//     **脱敏后的 query 落在 request_params 这一列**(结构化、可读),
			//     request_url 里带的是同一份脱敏串(可读性优先)。
			//
			//   ⚠️ 两列因此有重叠 —— 这是**故意的**:request_url 是给人看的
			//     完整地址,request_params 是给机器/检索用的参数表。
			//     两列内容不同(一个带路径一个不带),但 query 部分同源。
			RequestURL: auditRequestURL(c, reqParams),
			// ⚠️ RequestBody / RequestParams 两列此前**恒空**(2026-10-30 M126 补)。
			//   技术方案 §9.5 对 request_body 的定义就是"脱敏后"——
			//   词表与实现见 pkg/desensitize/body.go。
			RequestBody: reqBody,
			// RequestParams = 脱敏后的 query(解析不出来就是空串)
			RequestParams: reqParams,
			// ⚠️ ResponseSummary 落的是**骨架**(code + data 的字段名),
			//   **不是响应体原文** —— 响应体里全是带 PII 的业务数据。
			//   详见 audit_capture.go::auditResponseSummary。
			ResponseSummary: auditResponseSummary(c),
			// ⚠️ Location = 这次动的是哪个对象(路径参数,如 `id=9527`)
			//   没有路径参数的路由(比如 POST 新建)这一列是空的 —— 不猜。
			Location:  location,
			IP:        c.ClientIP(),
			UserAgent: string(c.UserAgent()),
			RequestID: string(c.GetHeader(response.HeaderRequestID)),
			TraceID:   string(c.GetHeader(response.HeaderTraceID)),
			Duration:  int(time.Since(start).Milliseconds()),
			CostMS:    time.Since(start).Milliseconds(),
			Status:    status,
			ErrorMsg:  errMsg,
		})
	}
}

// auditErrorMsg 从失败响应体里取错误文案
//
// 只认框架自己的统一响应体格式(`{"code":..,"message":"..}`,见 pkg/response)。
// 解析不出来就返回空 —— **绝不落原始 body**:
// 原始 body 可能回显用户提交的字段,审计表不该成为 PII 的第二份副本。
//
// ⚠️ 长度先挡一道:失败响应一般很小,但万一某个 handler 写了很大的错误体,
// 不挡就是往审计表里塞一坨二进制。
func auditErrorMsg(c *app.RequestContext) string {
	body := c.Response.Body()
	if len(body) == 0 || len(body) > 4096 {
		return ""
	}
	var r struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return ""
	}
	// error_msg 列是 size:1000 —— 超了会被 MySQL 截断/报错,这里自己收一收
	return truncateRunes(r.Message, 900)
}

// truncateRunes 按**字符**(不是字节)截断
//
// ⚠️ 不能直接切字节 —— 中文是 3 字节 UTF-8,按字节切会把一个汉字劈成两半,落库就是乱码。
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
