// Package handler 通用工具
package handler

import (
	"context"
	"errors"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/pagination"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/pkg/validatorx"
	"rapid-admin/internal/service"
)

// BaseHandler handler 公共方法
type BaseHandler struct {
	Services *service.Registry
}

// New 构造
func New(r *service.Registry) *BaseHandler {
	return &BaseHandler{Services: r}
}

// OK 成功
//
// # ⚠️ **M163:字段级可见性(列级权限)在这里生效** ——
//
// 这是全仓**唯一**的"把业务数据吐上 wire"的出口(choke point):
// 每个 handler 都经过它,于是"某个接口忘了脱敏"这件事**在结构上不可能发生**。
//
// ⚠️ **没配规则 ⇒ 响应逐字节不变** ——
//
// 走的是原来那条渲染路径(不重新序列化),所以规则层上线不改变任何现有响应。
//
// ⚠️ **脱敏失败 ⇒ 500,不是"原样放行"** ——
//
// 放行 = 一次数据库抖动就把敏感字段原样吐出去,而且日志里一条痕迹都没有。
// (与行级数据范围"落不下去就报错"同一条纪律。)
func (h *BaseHandler) OK(c *app.RequestContext, data any) {
	res, err := h.maskData(c, data)
	if err != nil {
		h.Fail(c, err)
		return
	}
	// ⚠️ **M168:头必须在写 body 之前设** —— Hertz 在 body 落盘时才把头刷出去。
	h.setFieldPolicyHeader(c, res.report)
	if res.changed {
		response.OKWithRawData(c, res.raw, "ok")
		return
	}
	response.OKWithData(c, data)
}

// OKWithMessage
//
// ⚠️ **M163:与 OK 同一套脱敏逻辑** —— 只是多带一句自定义文案。
func (h *BaseHandler) OKWithMessage(c *app.RequestContext, data any, msg string) {
	res, err := h.maskData(c, data)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.setFieldPolicyHeader(c, res.report)
	if res.changed {
		response.OKWithRawData(c, res.raw, msg)
		return
	}
	response.OKWithMessage(c, data, msg)
}

// OKID "新建成功 → 把新行主键返给前端"的统一出口。
//
// 2026-10-21 之前全仓有 31 处写成 h.OK(c, map[string]any{"id": id}),
// 直接把 uint64 裸整数吐上 wire。而 sonyflake 主键是 17 位数(当前 ~9.7e16),
// 超出 JS 的 2^53 安全整数上限,JSON.parse 会静默截断末 6 位:
//
//	POST /houses                     → {"id": 94883951113533126}
//	前端 JSON.parse                  → 94883951113533120
//	GET /houses/94883951113533120    → 404
//
// 这跟身份切换 404 是同一个坑,只是换了个入口 —— 而且它**不经过任何 struct**,
// 所以"扫 json tag"的静态检查天然看不见它(见 internal/pkg/jsonid)。
// 收口成一个函数而不是各自 map:漏一处就得等线上报 404 才知道。
func (h *BaseHandler) OKID(c *app.RequestContext, id uint64) {
	h.OK(c, IDData(id))
}

// IDData 同上,只构造 body 不发送(有些地方要塞进更大的结构里)
func IDData(id uint64) map[string]any {
	return map[string]any{"id": strconv.FormatUint(id, 10)}
}

// Page 分页
//
// ⚠️ **M163:字段级可见性在列表上生效** —— 与 OK 同一套逻辑。
func (h *BaseHandler) Page(c *app.RequestContext, list any, total int64) {
	page, size := h.GetPage(c)
	h.PageWith(c, list, total, page, size)
}

// PageWith 分页(页码与每页条数由调用方给)
//
// ⚠️ **为什么要有它** ——
//
// 动态集合的数据面用的是 service **归一化之后**的页码/每页条数
// (见 handler/v1/dyn.go 的 rawPage 注释:两套归一化规则不同,
// 用重新算的那一套会让响应里的 size 与实际行数对不上,前端分页器会算错总页数)。
// ⇒ 那条路径不能走 h.Page(它自己从 query 重算),只能走这个出口。
//
// ⚠️ **脱敏的是 list,不是整个分页信封** ——
//
// total / page / size 是**元数据**,不是业务行 ⇒ 不该被字段规则动。
func (h *BaseHandler) PageWith(c *app.RequestContext, list any, total int64, page, size int) {
	res, err := h.maskData(c, list)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.setFieldPolicyHeader(c, res.report)
	if res.changed {
		response.PageWithRawList(c, res.raw, total, page, size)
		return
	}
	response.Page(c, list, total, page, size)
}

// Fail 失败
func (h *BaseHandler) Fail(c *app.RequestContext, err error) {
	var e *errcode.Error
	if errors.As(err, &e) {
		if e.Detail != nil {
			response.FailWithData(c, e.Code, e.Message, e.Detail)
			return
		}
		response.Fail(c, e.Code, e.Message)
		return
	}
	response.Fail(c, errcode.Internal, "服务器内部错误")
}

// FailCode
func (h *BaseHandler) FailCode(c *app.RequestContext, code int, msg string) {
	response.Fail(c, code, msg)
}

// GetPage 解析分页参数
func (h *BaseHandler) GetPage(c *app.RequestContext) (page, size int) {
	page, _ = strconv.Atoi(c.Query("page"))
	size, _ = strconv.Atoi(c.Query("size"))
	pr := pagination.PageRequest{Page: page, Size: size}
	pr.Normalize()
	return pr.Page, pr.Size
}

// CallerFromContext 取 caller
func (h *BaseHandler) CallerFromContext(ctx context.Context) *service.Caller {
	c, _ := service.FromContext(ctx)
	return c
}

// RequireCaller 取 caller,未登录返回 nil
func (h *BaseHandler) RequireCaller(c *app.RequestContext) *service.Caller {
	if v, ok := c.Get(service.CallerKey); ok {
		if caller, ok := v.(*service.Caller); ok && caller.UserID != 0 {
			return caller
		}
	}
	h.FailCode(c, errcode.Unauthorized, errcode.Msg(errcode.Unauthorized))
	return nil
}

// GetID 路径参数
func (h *BaseHandler) GetID(c *app.RequestContext, name string) (uint64, error) {
	v := c.Param(name)
	if v == "" {
		return 0, errcode.New(errcode.BadRequest, "缺少参数 "+name)
	}
	id, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, errcode.New(errcode.BadRequest, "参数 "+name+" 格式错误")
	}
	return id, nil
}

// BindJSON 解析 body 到 dto
func (h *BaseHandler) BindJSON(c *app.RequestContext, dto any) error {
	if err := c.BindJSON(dto); err != nil {
		return errcode.New(errcode.BadRequest, "请求体格式错误: "+err.Error())
	}
	return nil
}

// BindAndValidate 解析 body + 跑 validatorx(§1.11 #6 P1)
//
//   - 解析失败:errcode.BadRequest "请求体格式错误"
//   - 校验失败:errcode.InvalidParam(40100),Detail 字段 { field, rule, value, message }
//   - 一次性只报第一个错误(避免一次弹一堆吓用户),后续错误在 log 落 detail
//
// DTO 用 `validate:"required,email,min=6,max=32,oneof=foo bar"` 声明规则
//   - 字段名以 json tag 为准(RegisterTagNameFunc 已配)
//   - 业务模块想加自定义规则:在 init() 调 validatorx.Get().RegisterCustom()
func (h *BaseHandler) BindAndValidate(c *app.RequestContext, dto any) error {
	if err := c.BindJSON(dto); err != nil {
		return errcode.New(errcode.BadRequest, "请求体格式错误: "+err.Error())
	}
	if err := validatorx.Get().Struct(dto); err != nil {
		field, msg := validatorx.Get().Explain(err)
		// ⚠️ **M112 修正:field 为空时不要拼 ": "**
		//   以前是无条件 `field+": "+msg` ⇒ Explain 认不出字段时
		//   消息变成 "": 参数校验失败"" —— 前端弹出来带个孤零零的冒号。
		//   (Explain 认不出字段 = err 不是 ValidationErrors,比如 DTO 传的不是 struct)
		text := msg
		if field != "" {
			text = field + ": " + msg
		}
		// 40100 = InvalidParam(已在 errcode.go 登记)
		return errcode.New(errcode.InvalidParam, text).WithDetail(map[string]any{
			"field":   field,
			"message": msg,
		})
	}
	return nil
}

// ReadBody 读原始 body(用于 import/preview 类需传原始字节的场景)
func (h *BaseHandler) ReadBody(c *app.RequestContext) []byte {
	return c.Request.Body()
}

// MustUint 取 uint64 query 参数,缺省值
func (h *BaseHandler) MustUint(c *app.RequestContext, key string, def uint64) uint64 {
	v := c.Query(key)
	if v == "" {
		return def
	}
	id, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return def
	}
	return id
}

// NotImplemented 业务实现占位(返回 501)
//
// ⚠️ **2026-10-26 M98:用 errcode.NotImplemented 常量,不再写死 50100。**
// 写死的数字跟常量脱钩,改段位时这里不会跟着动 —— 而且 HTTP 状态(501)是由
// 业务码推导出来的,码写错就静默降级成 500("该换条路走"变成"它炸了")。
func (h *BaseHandler) NotImplemented(c *app.RequestContext, action string) {
	response.FailWithData(c, errcode.NotImplemented, "功能未实现: "+action, map[string]any{"action": action})
}
