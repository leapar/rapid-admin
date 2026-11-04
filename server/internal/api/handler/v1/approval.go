// 通用审批系统 —— 审批单 handler(M86)
// 文档:docs/通用审批系统设计方案V1.0.md §7.1 / §7.3 / §7.4
//
// ⚠️ **后四个端点(draft/submit/cancel/act)不挂独立权限码**(§7.1)——
// 守卫靠"你是不是当事人",`middleware.RBAC` 表达不了"这条数据属于我吗"。
// 它们仍挂 `approval:read`,让完全没有审批权限的角色根本进不来。
package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/service"
)

// ApprovalHandler 审批单 handler
type ApprovalHandler struct{ *handler.BaseHandler }

// NewApprovalHandler 构造
func NewApprovalHandler(r *service.Registry) *ApprovalHandler {
	return &ApprovalHandler{BaseHandler: handler.New(r)}
}

// ListApprovals GET /rapid-admin/v1/system/approvals?box=todo
//
// ⚠️ **`box` 必填**(§8.2)—— 没有 `box=all`,空 box 服务端直接报错。
// 少传一个参数不该等于"给我看全部"。
func (h *ApprovalHandler) ListApprovals(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	q := service.ApprovalListQuery{
		Box:     c.Query("box"),
		BizType: c.Query("biz_type"),
		Status:  c.Query("status"),
		Keyword: c.Query("keyword"),
	}
	q.Page, q.Size = h.GetPage(c)

	res, err := h.Services.Approval.ListApprovals(ctx, caller, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// GetApproval GET /rapid-admin/v1/system/approvals/:id
//
// ⚠️ **可见性在 service 层判**(§8.3)—— assertor + isParty 两条路,
// handler 不重复实现。
func (h *ApprovalHandler) GetApproval(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	res, err := h.Services.Approval.GetApproval(ctx, caller, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// CreateApproval POST /rapid-admin/v1/system/approvals
//
// ⚠️ **这条端点不是业务记录创建的主路**(§7.3)——
// 业务记录与审批单必须同事务,那条路是业务 service 直接调
// `ApprovalEngine.CreateDraft(ctx, tx, ...)`。
// 走 HTTP 端点 = 业务记录已经落库了,审批单建失败就留下一条没人审的记录。
func (h *ApprovalHandler) CreateApproval(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var req service.CreateApprovalReq
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	id, err := h.Services.Approval.CreateDraft(ctx, caller, req)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, id)
}

// UpdateDraft PUT /rapid-admin/v1/system/approvals/:id/draft
//
// ⚠️ **改草稿不写意见流水**(§3.3)—— 它不是审批动作,
// 记进意见流里全是"提交人修改了标题"的噪音。
func (h *ApprovalHandler) UpdateDraft(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var req service.UpdateDraftReq
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.ApprovalEngine.UpdateDraft(ctx, caller, id, req); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Submit POST /rapid-admin/v1/system/approvals/:id/submit
//
// ⚠️ **首次提交与打回后重提走同一个端点**,守卫不同(§3.3 的 7 行表)。
func (h *ApprovalHandler) Submit(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var req service.SubmitReq
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.ApprovalEngine.Submit(ctx, caller, id, req); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Cancel POST /rapid-admin/v1/system/approvals/:id/cancel
//
// ⚠️ **running 状态不给作废**(D9)—— 已经送审的单子可能有人正在审,
// 悄悄作废掉会让他审批到一条已经不存在的单。
func (h *ApprovalHandler) Cancel(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.ApprovalEngine.Cancel(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Act POST /rapid-admin/v1/system/approvals/:id/act
//
// ⚠️ **approve / reject / rollback 三合一**(§3.3)——
// 三者的守卫完全一样(是不是本关待办人),拆成三个端点只会多出两处
// 路由和两处权限码同步点。
func (h *ApprovalHandler) Act(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var req service.ActReq
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.ApprovalEngine.Act(ctx, caller, id, req); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}
