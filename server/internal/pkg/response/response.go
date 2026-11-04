// 统一响应
// 文档 §7.4
package response

import (
	"encoding/json"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/errcode"
)

// Response 统一响应体
type Response struct {
	Code      int         `json:"code"`               // 0 = 成功
	Data      interface{} `json:"data,omitempty"`
	Message   string      `json:"message"`
	Timestamp int64       `json:"timestamp"`
	TraceID   string      `json:"trace_id,omitempty"`
}

// PageData 分页
type PageData struct {
	List  interface{} `json:"list"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Size  int         `json:"size"`
}

const (
	HeaderRequestID = "X-Request-Id"
	HeaderTraceID   = "X-Trace-Id"
)

// OK 成功(无数据)
func OK(c *app.RequestContext) {
	c.JSON(200, Response{Code: 0, Message: "ok", Timestamp: Now(), TraceID: TraceIDFrom(c)})
}

// OKWithData 成功(带数据)
func OKWithData(c *app.RequestContext, data interface{}) {
	c.JSON(200, Response{
		Code:      0,
		Data:      data,
		Message:   "ok",
		Timestamp: Now(),
		TraceID:   TraceIDFrom(c),
	})
}

// OKWithMessage 成功 + 自定义文案
func OKWithMessage(c *app.RequestContext, data interface{}, msg string) {
	c.JSON(200, Response{
		Code:      0,
		Data:      data,
		Message:   msg,
		Timestamp: Now(),
		TraceID:   TraceIDFrom(c),
	})
}

// Page 成功(分页)
func Page(c *app.RequestContext, list interface{}, total int64, page, size int) {
	OKWithData(c, PageData{List: list, Total: total, Page: page, Size: size})
}

// OKWithRawData 成功(带**已序列化**的数据)
//
// # ⚠️ **给"要先改内容再上 wire"的场景用** ——
//
//	字段级可见性(M163)脱敏后交过来的是一段已经改好的 JSON,
//	不用再让渲染器把原值序列化一遍。
//
// # ⚠️ **raw 必须是合法 JSON** ——
//
//	空 / nil 会被当成 "null"(json.RawMessage 空切片序列化会报错)。
//
// # ⚠️ **响应体外壳仍然是这一份** ——
//
//	Data 是 `interface{}`,装进去的是 json.RawMessage ⇒
//	渲染器走它的 MarshalJSON ⇒ **原样输出,不重排键、不二次转义**。
//	(外壳的定义只有 response.Response 这一处,没有第二份。)
func OKWithRawData(c *app.RequestContext, raw json.RawMessage, msg string) {
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	c.JSON(200, Response{
		Code:      0,
		Data:      raw,
		Message:   msg,
		Timestamp: Now(),
		TraceID:   TraceIDFrom(c),
	})
}

// PageWithRawList 成功(分页,列表已序列化)
//
// ⚠️ **与 Page 同一形状** —— list / total / page / size 四个字段一个不少,
// 差别只在 list 是已经脱敏过的字节。
//
// ⚠️ **脱敏的是 list,不是整个信封** ——
// total / page / size 是元数据(有多少条、翻到第几页),
// 不是业务行的投影 ⇒ 字段级规则作用不到它们身上,
// 也不该作用(把 total 藏了,前端分页器直接算错总页数)。
func PageWithRawList(c *app.RequestContext, raw json.RawMessage, total int64, page, size int) {
	if len(raw) == 0 {
		raw = json.RawMessage("[]")
	}
	// PageData.List 是 interface{} —— 装进去的 json.RawMessage
	// 会被渲染器原样输出(它实现了 json.Marshaler)。
	OKWithData(c, PageData{List: raw, Total: total, Page: page, Size: size})
}

// Fail 失败
func Fail(c *app.RequestContext, code int, message string) {
	c.JSON(errcode.HTTPStatus(code), Response{
		Code:      code,
		Message:   message,
		Timestamp: Now(),
		TraceID:   TraceIDFrom(c),
	})
}

// FailWithData 失败(带数据,例如校验错误明细)
func FailWithData(c *app.RequestContext, code int, message string, data interface{}) {
	c.JSON(errcode.HTTPStatus(code), Response{
		Code:      code,
		Data:      data,
		Message:   message,
		Timestamp: Now(),
		TraceID:   TraceIDFrom(c),
	})
}

// FailError 从 error 取 code + msg
func FailError(c *app.RequestContext, err error) {
	Fail(c, errcode.CodeOf(err), err.Error())
}

// Now 毫秒时间戳
func Now() int64 { return time.Now().UnixMilli() }

// TraceIDFrom 从 ctx header 取 traceId
func TraceIDFrom(c *app.RequestContext) string {
	if v := c.GetHeader(HeaderTraceID); len(v) > 0 {
		return string(v)
	}
	if v := c.GetHeader(HeaderRequestID); len(v) > 0 {
		return string(v)
	}
	return ""
}

// ── M98(2026-10-26)记录:这个文件原来有一份私有的 httpStatus ──────────
//
// **这个函数已被删除**,映射统一到 `errcode.HTTPStatus`。
//
// 原来这里有一份**私有拷贝**,与 `errcode.HTTPStatus`(导出的那份)、以及
// `cmd/gen-errcode-doc` 里的第三份拷贝**三处各写一遍**,而且各错各的:
//
//	这份(活着的)  41300 → 400  ✗  50100 → 500  ✗  50300 → 503 ✓  50400 → 504 ✓
//	errcode 那份    401/403/404/409/413 全部 → 400 ✗(大桶排在细分桶前面)
//	文档生成器那份  4xxxx 对 ✓,但 50100/50300/50400 → 500 ✗
//
// 于是 `docs/error-codes.md` 里写着 50100 → 500,而代码返的也是 500 ——
// 文档和代码"一致地错",没人发现。真正受害的是**前端**:501 被塞进 500 那档,
// `api/client.ts` 的 `case 500` 只弹"服务器内部错误"的通用文案,**不看 body
// 里的业务码**,于是已经登记了 i18n 译文的"该功能尚未实现"永远显示不出来。
//
// 现在:`errcode.HTTPStatus` 是唯一实现,本包、文档生成器都调它。
// 新增码只要改 errcode 一处,三边同时生效。
