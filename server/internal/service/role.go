// RoleService 角色/资源/部门
// 文档 §6.1 #3
package service

import (
	"context"

	"rapid-admin/internal/model"
	"rapid-admin/internal/seed"
)

// RoleListQuery 角色列表
type RoleListQuery struct {
	model.PageRequest
	Keyword string `json:"keyword"`
}

// RoleListItem 列表条目 DTO(snake_case,与前端 types/permission.Role 对齐)
//
// Phase 5 修复(2026-10-17):
//   - 原 ListRoles 直接返 []SysRole(camelCase + 缺 type/enabled)
//   - 改 snake_case + 派生 enabled (= isActive,前端 switch 用)
type RoleListItem struct {
	ID          uint64 `json:"id,string"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`                  // platform | business
	Sort        int    `json:"sort"`
	IsBuiltin   bool   `json:"is_builtin"`
	Enabled     bool   `json:"enabled"`               // 派生:isActive
	CreatedAt   string `json:"created_at"`
}

// ResourceListQuery 资源列表
type ResourceListQuery struct {
	model.PageRequest
	Keyword string `json:"keyword"`
	Type string `json:"type"`
	ParentID uint64 `json:"parent_id,string"`
}

// DepartmentListQuery 部门列表
type DepartmentListQuery struct {
	model.PageRequest
	Keyword string `json:"keyword"`
	ParentID uint64 `json:"parent_id,string"`
}

// ResourceUpdateInput 资源更新入参
//
// # 为什么是 DTO 而不是直接用 model.SysResource
//
// 原来的 UpdateResource 直接收 *model.SysResource,靠"**空串 = 不改**"来判断
// 哪些字段要更新。这么写有三个洞,全都出在"清空"上:
//
//  1. method 清不掉 —— 前端想选「全部」(= 不限定),发 method:"" ,
//     后端 `if res.Method != ""` 直接跳过 ⇒ **改了没生效**,界面上还是老值。
//  2. icon 清不掉 —— 同上。
//  3. sort 归 0 存不进去 —— `if res.Sort != 0`。
//  4. parent_id 归 0(提到顶级)存不进去 —— `if res.ParentID > 0`。
//     ⇒ 父资源下拉里的「— 顶级 —」选项对**已有资源是死的**(选了没反应)。
//
// ⇒ 指针字段是解药:**nil = 没传(不动),非 nil = 传了(哪怕是空串/0也要写进去)**。
//
// # parent_id 为什么是 *string 而不是 *uint64
//
//   · wire 上 parent_id 是**带引号的数字串**("0" / "1234567890123456789"):
//     sonyflake 19 位 ID 超出 JS 的 2^53 安全整数上限,前端只能传字符串;
//   · Go 的 `json:",string"` **只对非指针类型生效**(encoding/json 的 typeFields
//     只在 Bool/Int*/Uint*/Float*/String 上置 quoted 标志)⇒ *uint64 + ,string
//     会变成"只收裸数字",前端发 "0" 直接被拒(整个请求 400)。
//
// 所以这里收字符串,自己 parse。
type ResourceUpdateInput struct {
	// 资源名称(必填,不允许清空)
	Name *string `json:"name"`
	// 前端路径 / API 路径(允许清空)
	Path *string `json:"path"`
	// HTTP 方法(允许清空 = 不限定)
	Method *string `json:"method"`
	// 图标 key(允许清空 = 不显示图标)
	Icon *string `json:"icon"`
	// 排序(允许 0)
	Sort *int `json:"sort"`
	// 父资源 ID;"0" = 顶级
	ParentID *string `json:"parent_id"`
	// 启用 / 停用
	IsActive *bool `json:"is_active"`
}

// DepartmentUpdateInput 部门更新入参
//
// # 为什么是 DTO 而不是直接用 model.SysDepartment
//
// 原来的 UpdateDepartment 直接收 *model.SysDepartment,靠"**零值 = 不改**"判断
// 哪些字段要更新。这么写有三个洞,全都出在"清空"和"没传"上:
//
//  1. 负责人清不掉 —— `if dept.LeaderID > 0`,前端把负责人清空后发 "0",
//     后端直接跳过 ⇒ **改了没生效**,刷新一下负责人又回来了。
//  2. 父部门那个判断是**恒真的** —— `if dept.ParentID > 0 || dept.ParentID == 0`
//     (uint64 不可能既 >0 又 ==0)⇒ parent_id **永远被写**。
//     后果反过来了:一个只想改名字的 PATCH(没带 parent_id)会把部门**静默搬到根**。
//  3. sort 归 0 存不进去 —— `if dept.Sort != 0`。
//
// ⇒ 指针字段是解药:**nil = 没传(不动),非 nil = 传了(哪怕是 "0" 也要写进去)**。
//   跟 ResourceUpdateInput 是同一套思路(见它的注释),不另立一套规矩。
//
// # parent_id / leader_id 为什么是 *string 而不是 *uint64
//
//   · wire 上这两个 ID 是**带引号的数字串**("0" / "1234567890123456789"):
//     sonyflake 19 位 ID 超出 JS 的 2^53 安全整数上限,前端只能传字符串;
//   · Go 的 `json:",string"` **只对非指针类型生效**(encoding/json 的 typeFields
//     只在 Bool/Int*/Uint*/Float*/String 上置 quoted 标志)⇒ *uint64 + ,string
//     会变成"只收裸数字",前端发 "0" 直接被拒(整个请求 400)。
//
// 所以这里收字符串,自己 parse(见 parseParentID)。
type DepartmentUpdateInput struct {
	// 部门名称(不允许清空)
	Name *string `json:"name"`
	// 部门编码(不允许改成别人的;改了要过唯一性校验)
	Code *string `json:"code"`
	// 父部门 ID;"0" = 顶级部门
	ParentID *string `json:"parent_id"`
	// 负责人(用户)ID;"0" = 无负责人
	LeaderID *string `json:"leader_id"`
	// 排序(允许 0)
	Sort *int `json:"sort"`
	// 启用 / 停用
	IsActive *bool `json:"is_active"`
}

// RoleService 角色/资源/部门
type RoleService interface {
	// ===== 角色 =====
	ListRoles(ctx context.Context, q RoleListQuery) (*model.PageResponse, error)
	GetRole(ctx context.Context, id uint64) (*model.SysRole, error)
	CreateRole(ctx context.Context, caller *Caller, role *model.SysRole) (uint64, error)
	UpdateRole(ctx context.Context, caller *Caller, id uint64, role *model.SysRole) error
	DeleteRole(ctx context.Context, caller *Caller, id uint64) error
	AssignResources(ctx context.Context, caller *Caller, roleID uint64, resourceIDs []uint64) error
	// 2026-10-18:分配权限 UI 真实化需要 — 列出某角色已绑定的资源 ID 集合
	//   前端 Drawer 打开时 pre-check 用,缺则全空(误判为全空绑定,误保存会清空)
	GetRoleResourceIDs(ctx context.Context, roleID uint64) ([]uint64, error)

	// ===== 资源 =====
	ListResources(ctx context.Context, q ResourceListQuery) (*model.PageResponse, error)
	ResourceTree(ctx context.Context) ([]model.SysResource, error) // 树形结构
	GetResource(ctx context.Context, id uint64) (*model.SysResource, error)
	CreateResource(ctx context.Context, caller *Caller, res *model.SysResource) (uint64, error)
	UpdateResource(ctx context.Context, caller *Caller, id uint64, in *ResourceUpdateInput) error
	DeleteResource(ctx context.Context, caller *Caller, id uint64) error

	// ===== 部门 =====
	ListDepartments(ctx context.Context, q DepartmentListQuery) (*model.PageResponse, error)
	DepartmentTree(ctx context.Context) ([]model.SysDepartment, error)
	GetDepartment(ctx context.Context, id uint64) (*model.SysDepartment, error)
	CreateDepartment(ctx context.Context, caller *Caller, dept *model.SysDepartment) (uint64, error)
	UpdateDepartment(ctx context.Context, caller *Caller, id uint64, in *DepartmentUpdateInput) error
	DeleteDepartment(ctx context.Context, caller *Caller, id uint64) error

	// ===== 字典 =====
	ListDict(ctx context.Context, dictType string) ([]model.SysDict, error)
	/** ListDictGroups 2026-10-18: 返回所有有数据的 group_code + 中文 label 列表 */
	ListDictGroups(ctx context.Context) ([]seed.DictGroupKV, error)
	GetDict(ctx context.Context, id uint64) (*model.SysDict, error)
	CreateDict(ctx context.Context, caller *Caller, d *model.SysDict) (uint64, error)
	UpdateDict(ctx context.Context, caller *Caller, id uint64, d *model.SysDict) error
	DeleteDict(ctx context.Context, caller *Caller, id uint64) error
}
