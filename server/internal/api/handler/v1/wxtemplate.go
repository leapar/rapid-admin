// 微信模板消息 handler
// 文档 §6.6 — 模板 CRUD + 发送
package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/model"
	"rapid-admin/internal/service"
)

type WxTemplateHandler struct{ *handler.BaseHandler }

func NewWxTemplateHandler(r *service.Registry) *WxTemplateHandler {
	return &WxTemplateHandler{BaseHandler: handler.New(r)}
}

// List GET /rapid-admin/v1/platform/wx-templates?keyword=&channel=&scene=&status=&page=&size=
func (h *WxTemplateHandler) List(ctx context.Context, c *app.RequestContext) {
	q := service.WxTemplateListQuery{
		Keyword: c.Query("keyword"),
		Channel: c.Query("channel"),
		Scene:   c.Query("scene"),
		Status:  c.Query("status"),
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.WxTemplate.ListTemplates(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// Get GET /rapid-admin/v1/platform/wx-templates/:id
func (h *WxTemplateHandler) Get(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.WxTemplate.GetTemplate(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// Create POST /rapid-admin/v1/platform/wx-templates
func (h *WxTemplateHandler) Create(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var body model.WxTemplate
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	id, err := h.Services.WxTemplate.CreateTemplate(ctx, caller, &body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, id)
}

// Update PATCH /rapid-admin/v1/platform/wx-templates/:id
func (h *WxTemplateHandler) Update(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body model.WxTemplate
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.WxTemplate.UpdateTemplate(ctx, caller, id, &body); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Delete DELETE /rapid-admin/v1/platform/wx-templates/:id
func (h *WxTemplateHandler) Delete(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.WxTemplate.DeleteTemplate(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Send POST /rapid-admin/v1/platform/wx-templates/:id/send
//   body: { code, openId, receiverType, userId, data, url, pagepath }
//   注:code 在 body 中必填,:id 是 path 模板 ID(校验一致性,留待后续)
func (h *WxTemplateHandler) Send(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var body service.WxTemplateSendRequest
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	id, err := h.Services.WxTemplate.Send(ctx, caller, body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, id)
}
