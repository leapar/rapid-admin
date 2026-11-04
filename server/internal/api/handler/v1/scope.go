// 数据范围(行级权限)规则 handler —— M155(2026-11-02)
//
// # 这三个端点回答什么问题
//
//	GET /rapid-admin/v1/system/scope-modules       → 有哪些业务域可以配
//	GET /rapid-admin/v1/system/roles/:id/scopes     → 这个角色配了什么
//	PUT /rapid-admin/v1/system/roles/:id/scopes     → 全量替换这个角色在某业务域上的规则
//
// # 为什么复用 role:read / role:assign,不开新的资源码
//
// 数据范围就是**角色配置的一部分** —— 它跟"这个角色能调哪些接口"
// 摆在同一个页面上,受同一批人管。
//
// ⚠️ **新开 `scope:*` 一族资源码的代价** ——
// 每个新码都要进 seed 的资源表 + 13 个角色的绑定矩阵,
// 漏绑一个角色就是那个角色 403(而且红 toast 说的是另一个码,极难排查)。
// 复用 `role:read` / `role:assign` 则**天然继承既有绑定**,不需要动矩阵。
//
// ⚠️ **语义上也说得通** ——
// `role:assign` = "分配角色权限",数据范围就是权限的一部分。

package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/service"
)

type ScopeHandler struct {
	*handler.BaseHandler
}

func NewScopeHandler(r *service.Registry) *ScopeHandler {
	return &ScopeHandler{BaseHandler: handler.New(r)}
}

// ListScopeModules GET /rapid-admin/v1/system/scope-modules
//
// 可配置的业务域候选(下拉框用)。
//
// ⚠️ **候选由后端推导** —— 前端不许手打业务域字符串:
// 配错一个字符,规则静默不生效(退化成"全部数据")。
func (h *ScopeHandler) ListScopeModules(ctx context.Context, c *app.RequestContext) {
	mods, err := h.Services.Scope.ListModules(ctx)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, mods)
}

// GetRoleScopes GET /rapid-admin/v1/system/roles/:id/scopes
//
// 列出该角色配的全部数据范围规则(所有业务域)。
func (h *ScopeHandler) GetRoleScopes(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	rules, err := h.Services.Scope.ListRoleScopes(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, rules)
}

// ReplaceRoleScopes PUT /rapid-admin/v1/system/roles/:id/scopes
//
// ⚠️ **全量替换,不是增量** —— body 里给的那一组就是该业务域上的全部规则。
// 发空数组 = 清空该业务域的规则(回到默认组/不限制)。
//
// body:
//
//	{
//	  "module": "user",
//	  "rules": [{"scope_type": "dept", "remark": "只看本部门"}]
//	}
func (h *ScopeHandler) ReplaceRoleScopes(ctx context.Context, c *app.RequestContext) {
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
		Module string                   `json:"module"`
		Rules  []service.ScopeRuleInput `json:"rules"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Scope.ReplaceRoleScopes(ctx, id, body.Module, body.Rules); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}
