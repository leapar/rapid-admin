// 交付指南 handler
// 文档 §6.2.2 / §7.2.2
package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/model"
	"rapid-admin/internal/service"
)

type GuideHandler struct{ *handler.BaseHandler }

func NewGuideHandler(r *service.Registry) *GuideHandler {
	return &GuideHandler{BaseHandler: handler.New(r)}
}

// List GET /rapid-admin/v1/biz/guides?keyword=&category=&subCategory=&projectId=&status=&isTop=&tag=&page=&size=
func (h *GuideHandler) List(ctx context.Context, c *app.RequestContext) {
	q := service.GuideListQuery{
		Keyword:     c.Query("keyword"),
		Category:    c.Query("category"),
		SubCategory: c.Query("subCategory"),
		ProjectID:   h.MustUint(c, "projectId", 0),
		Status:      c.Query("status"),
	}
	if v := c.Query("isTop"); v != "" {
		b := v == "true" || v == "1"
		q.IsTop = &b
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Guide.ListGuides(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// Get GET /rapid-admin/v1/biz/guides/:id
func (h *GuideHandler) Get(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.Guide.GetGuide(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// Create POST /rapid-admin/v1/biz/guides
func (h *GuideHandler) Create(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var body model.Guide
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	id, err := h.Services.Guide.CreateGuide(ctx, caller, &body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, id)
}

// Update PATCH /rapid-admin/v1/biz/guides/:id
func (h *GuideHandler) Update(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body model.Guide
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Guide.UpdateGuide(ctx, caller, id, &body); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Delete DELETE /rapid-admin/v1/biz/guides/:id
func (h *GuideHandler) Delete(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Guide.DeleteGuide(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}
