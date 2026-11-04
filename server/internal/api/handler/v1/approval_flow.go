// 通用审批系统 —— 流程模板 CRUD handler(M86)
// 文档:docs/通用审批系统设计方案V1.0.md §7.1 / §7.2
//
// ⚠️ **这五个端点挂在 `/rapid-admin/v1/system/flows` 下** ——
// 路由一致性检查(`router/consistency.go`)要求"受保护路由数 == rbac() 调用数",
// 而受保护前缀只有 system / biz / platform / auth 四个。挂在 system 下
// 也和前端已有的 `/system/dicts` 惯例一致。
package v1

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/service"
)

// ApprovalFlowHandler 审批流程模板 handler
type ApprovalFlowHandler struct{ *handler.BaseHandler }

// NewApprovalFlowHandler 构造
func NewApprovalFlowHandler(r *service.Registry) *ApprovalFlowHandler {
	return &ApprovalFlowHandler{BaseHandler: handler.New(r)}
}

// ListFlows GET /rapid-admin/v1/system/flows
func (h *ApprovalFlowHandler) ListFlows(ctx context.Context, c *app.RequestContext) {
	q := service.FlowListQuery{
		Keyword: c.Query("keyword"),
		BizType: c.Query("biz_type"),
	}
	// ⚠️ **is_active 是三态** —— 不传 = 不过滤,传了 false = 只看停用的。
	// 用 `*bool` 而不是 `bool`,否则"不传"和"传 false"分不开。
	if v := c.Query("is_active"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			h.FailCode(c, errcode.ParamInvalid, "is_active 只能是 true / false")
			return
		}
		q.IsActive = &b
	}
	q.Page, q.Size = h.GetPage(c)

	res, err := h.Services.ApprovalFlow.ListFlows(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// GetFlow GET /rapid-admin/v1/system/flows/:id
func (h *ApprovalFlowHandler) GetFlow(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	res, err := h.Services.ApprovalFlow.GetFlow(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// CreateFlow POST /rapid-admin/v1/system/flows
//
// ⚠️ **权限码 `flow:create` 只绑超管**(§8.4 D 组)——
// 配错一个审批模板 = 全平台每一条业务记录都走错审批链。
func (h *ApprovalFlowHandler) CreateFlow(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var req service.FlowSaveReq
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	id, err := h.Services.ApprovalFlow.CreateFlow(ctx, caller, req)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, id)
}

// UpdateFlow PUT /rapid-admin/v1/system/flows/:id
//
// ⚠️ **有非终态实例时服务端会拒**(D7)—— 报错文案里带"N 条在跑",
// 让操作的人知道该去处理哪些单子,而不是只看到一句"不允许修改"。
func (h *ApprovalFlowHandler) UpdateFlow(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var req service.FlowSaveReq
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.ApprovalFlow.UpdateFlow(ctx, caller, id, req); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// DeleteFlow DELETE /rapid-admin/v1/system/flows/:id
func (h *ApprovalFlowHandler) DeleteFlow(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.ApprovalFlow.DeleteFlow(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}
