// 字段级可见性(列级权限)规则 handler —— M162(2026-11-02)
//
// # 这三个端点回答什么问题
//
//	GET /rapid-admin/v1/system/field-policy-modules        → 有哪些业务域可以配、每个域有哪些字段
//	GET /rapid-admin/v1/system/roles/:id/field-policies    → 这个角色配了什么
//	PUT /rapid-admin/v1/system/roles/:id/field-policies    → 全量替换这个角色在某业务域上的规则
//
// # ⚠️ **为什么复用 role:read / role:assign,不开新的资源码**
//
// 与数据范围(scope.go)同一套理由:
// 字段级可见性是**角色配置的一部分**,它跟"这个角色能调哪些接口"
// 摆在同一个页面上、受同一批人管。
//
// 新开 `field_policy:*` 一族码要进 seed 的资源表 + 13 个角色的绑定矩阵,
// 漏绑一个就是那个角色 403(而且红 toast 说的是另一个码,极难排查)。
//
// # ⚠️ **候选由后端推导,前端不许手打业务域与字段名**
//
// 业务域字符串配错 ⇒ 规则落到默认规则组 ⇒ 静默失配。
// 字段名配错 ⇒ 规则作用在一个不存在的键上 ⇒ 静默不生效。
//
// ⇒ 两者都在配置期就拒(见 service/field_policy_impl.go)。

package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/service"
)

type FieldPolicyHandler struct {
	*handler.BaseHandler
}

func NewFieldPolicyHandler(r *service.Registry) *FieldPolicyHandler {
	return &FieldPolicyHandler{BaseHandler: handler.New(r)}
}

// ListFieldPolicyModules GET /rapid-admin/v1/system/field-policy-modules
//
// 可配置的业务域候选 + 每个域的字段候选(下拉框用)。
func (h *FieldPolicyHandler) ListFieldPolicyModules(ctx context.Context, c *app.RequestContext) {
	mods, err := h.Services.FieldPolicy.ListFieldPolicyModules(ctx)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, mods)
}

// GetRoleFieldPolicies GET /rapid-admin/v1/system/roles/:id/field-policies
//
// 列出该角色配的全部字段规则(所有业务域)。
func (h *FieldPolicyHandler) GetRoleFieldPolicies(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	rules, err := h.Services.FieldPolicy.ListRoleFieldPolicies(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, rules)
}

// ReplaceRoleFieldPolicies PUT /rapid-admin/v1/system/roles/:id/field-policies
//
// ⚠️ **全量替换,不是增量** —— body 里给的那一组就是该业务域上的全部字段规则。
// 发空数组 = 清空该业务域的规则。
//
// body:
//
//	{
//	  "module": "owner",
//	  "policies": [{"field": "real_name", "mode": "masked", "mask_type": "name"}]
//	}
func (h *FieldPolicyHandler) ReplaceRoleFieldPolicies(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body struct {
		Module   string                     `json:"module"`
		Policies []service.FieldPolicyInput `json:"policies"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.FieldPolicy.ReplaceRoleFieldPolicies(ctx, id, body.Module, body.Policies); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}
