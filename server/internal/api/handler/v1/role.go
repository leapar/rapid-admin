// 角色/资源/部门/字典 handler
package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/jsonid"
	"rapid-admin/internal/service"
)

type RoleHandler struct {
	*handler.BaseHandler
}

func NewRoleHandler(r *service.Registry) *RoleHandler {
	return &RoleHandler{BaseHandler: handler.New(r)}
}

func (h *RoleHandler) rc(c *app.RequestContext) *service.Caller { return h.BaseHandler.RequireCaller(c) }

// ========== 角色 ==========
func (h *RoleHandler) ListRoles(ctx context.Context, c *app.RequestContext) {
	q := service.RoleListQuery{Keyword: c.Query("keyword")}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Role.ListRoles(ctx, q)
	if err != nil { h.Fail(c, err); return }
	h.Page(c, res.List, res.Total)
}

func (h *RoleHandler) CreateRole(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	var body model.SysRole
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	id, err := h.Services.Role.CreateRole(ctx, caller, &body)
	if err != nil { h.Fail(c, err); return }
	h.OKID(c, id)
}

func (h *RoleHandler) UpdateRole(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	var body model.SysRole
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	if err := h.Services.Role.UpdateRole(ctx, caller, id, &body); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}

func (h *RoleHandler) DeleteRole(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	if err := h.Services.Role.DeleteRole(ctx, caller, id); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}

// GetRole GET /rapid-admin/v1/system/roles/:id 2026-10-19 M63b
//   单个角色详情,前端角色编辑 Drawer 打开时 pre-fill 用
func (h *RoleHandler) GetRole(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	role, err := h.Services.Role.GetRole(ctx, id)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, role)
}

func (h *RoleHandler) AssignResources(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	// jsonid.IDList 而不是 []uint64:内置的 `,string` 对数组无效,而资源 ID
	// 是 19 位数,裸整数数组一进 JS 就被截断
	var body struct{ ResourceIDs jsonid.IDList `json:"resource_ids"` }
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	if err := h.Services.Role.AssignResources(ctx, caller, id, body.ResourceIDs.Uints()); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}

// GetRoleResources 2026-10-18 分配权限 UI 真实化
//   列出某角色已绑定的资源 ID 集合,供前端 Drawer pre-check
func (h *RoleHandler) GetRoleResources(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	ids, err := h.Services.Role.GetRoleResourceIDs(ctx, id)
	if err != nil { h.Fail(c, err); return }
	// 2026-10-21:key 补成 snake_case;[]uint64 换成 jsonid.IDList ——
	// 内置的 `,string` 对数组无效,而裸整数数组前端一 parse 就被截断。
	h.OK(c, map[string]any{"resource_ids": jsonid.FromUints(ids)})
}

// ========== 资源 ==========
func (h *RoleHandler) ListResources(ctx context.Context, c *app.RequestContext) {
	q := service.ResourceListQuery{Keyword: c.Query("keyword"), Type: c.Query("type")}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Role.ListResources(ctx, q)
	if err != nil { h.Fail(c, err); return }
	h.Page(c, res.List, res.Total)
}

func (h *RoleHandler) ResourceTree(ctx context.Context, c *app.RequestContext) {
	res, err := h.Services.Role.ResourceTree(ctx)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, res)
}

func (h *RoleHandler) CreateResource(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	var body model.SysResource
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	id, err := h.Services.Role.CreateResource(ctx, caller, &body)
	if err != nil { h.Fail(c, err); return }
	h.OKID(c, id)
}

func (h *RoleHandler) UpdateResource(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	// ⚠️ **用 service.ResourceUpdateInput,不用 model.SysResource** ——
	//   model 的字段是值类型,"空串" 分不清"没传" 和 "传了要清空",
	//   于是 method / icon / sort=0 / parent_id=0 这四种"清空类"操作全是死的。
	//   详见 service.ResourceUpdateInput 的注释。
	var body service.ResourceUpdateInput
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	if err := h.Services.Role.UpdateResource(ctx, caller, id, &body); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}

func (h *RoleHandler) DeleteResource(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	if err := h.Services.Role.DeleteResource(ctx, caller, id); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}

// GetResource GET /rapid-admin/v1/system/resources/:id 2026-10-19 M63c
//   单个资源详情,前端资源编辑 Drawer 打开时 pre-fill 用
func (h *RoleHandler) GetResource(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	res, err := h.Services.Role.GetResource(ctx, id)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, res)
}

// ========== 部门 ==========
func (h *RoleHandler) ListDepartments(ctx context.Context, c *app.RequestContext) {
	q := service.DepartmentListQuery{Keyword: c.Query("keyword")}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Role.ListDepartments(ctx, q)
	if err != nil { h.Fail(c, err); return }
	h.Page(c, res.List, res.Total)
}

func (h *RoleHandler) DepartmentTree(ctx context.Context, c *app.RequestContext) {
	res, err := h.Services.Role.DepartmentTree(ctx)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, res)
}

func (h *RoleHandler) CreateDepartment(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	var body model.SysDepartment
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	id, err := h.Services.Role.CreateDepartment(ctx, caller, &body)
	if err != nil { h.Fail(c, err); return }
	h.OKID(c, id)
}

func (h *RoleHandler) UpdateDepartment(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	// ⚠️ **用 service.DepartmentUpdateInput,不用 model.SysDepartment** ——
	//   model 的字段是值类型,"0" 分不清"没传" 和 "传了要清空",
	//   于是「清空负责人」「把部门提到顶级」「sort 归 0」这三种操作全是死的;
	//   旧代码里 parent_id 的判断还是恒真式 ⇒ 只想改名字的 PATCH
	//   会把部门**静默搬到根**。详见 service.DepartmentUpdateInput 的注释。
	var body service.DepartmentUpdateInput
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	if err := h.Services.Role.UpdateDepartment(ctx, caller, id, &body); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}

func (h *RoleHandler) DeleteDepartment(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	if err := h.Services.Role.DeleteDepartment(ctx, caller, id); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}

// GetDepartment GET /rapid-admin/v1/system/departments/:id 2026-10-19 M63d
//   单个部门详情,前端部门编辑 Drawer 打开时 pre-fill 用
func (h *RoleHandler) GetDepartment(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	dept, err := h.Services.Role.GetDepartment(ctx, id)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, dept)
}

// ========== 字典 ==========
// GET /rapid-admin/v1/system/dicts?groupCode=xxx
// 2026-10-18:参数 type → groupCode,与前端 useDict / api/config.ts:40 对齐
func (h *RoleHandler) ListDict(ctx context.Context, c *app.RequestContext) {
	res, err := h.Services.Role.ListDict(ctx, c.Query("groupCode"))
	if err != nil { h.Fail(c, err); return }
	h.OK(c, res)
}

// ListDictGroups 2026-10-18: 返回所有有数据的 group_code 列表
// 用途: 字典管理页"业务类型"下拉数据源(替代前端硬编码 GROUP_OPTIONS)
func (h *RoleHandler) ListDictGroups(ctx context.Context, c *app.RequestContext) {
	res, err := h.Services.Role.ListDictGroups(ctx)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, res)
}

func (h *RoleHandler) CreateDict(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	var body model.SysDict
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	id, err := h.Services.Role.CreateDict(ctx, caller, &body)
	if err != nil { h.Fail(c, err); return }
	h.OKID(c, id)
}

func (h *RoleHandler) UpdateDict(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	var body model.SysDict
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	if err := h.Services.Role.UpdateDict(ctx, caller, id, &body); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}

func (h *RoleHandler) DeleteDict(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c); if caller == nil { return }
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	if err := h.Services.Role.DeleteDict(ctx, caller, id); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}
