/**
 * 角色 / 资源 / 部门 API
 *
 * 路径规则: 后端 /rapid-admin/v1/system/{roles,resources,departments} (复数)
 * 详见 server/internal/api/router/router.go:122
 *
 * 例外: /resource/accessible 后端是单数(router.go:143),前端保持单数
 */
import { http } from './client'
import { PageRequest, PageResponse } from '@/types/api'
import { Role, Resource, RoleResource, Department } from '@/types/permission'

/* ====================== 角色 ====================== */

export interface RoleListQuery extends PageRequest {
  keyword?: string
  roleType?: number
  status?: number
}

export interface CreateRoleRequest {
  code: string
  name: string
  roleType: number
  description?: string
  resourceIds: string[]
}

export const roleApi = {
  /** 后端 GET /roles, query 传参(非 body) */
  list: (q: RoleListQuery) =>
    http.get<PageResponse<Role>>('/system/roles', q as unknown as Record<string, unknown>),

  /**
   * 角色详情 — 后端 GET /system/roles/:id
   * 2026-10-19 M63b:供前端编辑 Drawer pre-fill
   */
  detail: (id: string) => http.get<Role>(`/system/roles/${id}`),

  create: (data: CreateRoleRequest) => http.post<void>('/system/roles', data),

  /** 后端 PATCH(非 PUT) */
  update: (id: string, data: Partial<CreateRoleRequest>) =>
    http.patch<void>(`/system/roles/${id}`, data),

  remove: (id: string) => http.delete<void>(`/system/roles/${id}`),

  /**
   * 分配资源
   *
   * ⚠️ 2026-10-21 修一个**数据销毁级**的 bug:这里一直发的是 camelCase
   * `resourceIds`,而后端绑的是 `json:"resource_ids"`。Go 的 encoding/json
   * 只做大小写不敏感匹配、**不认下划线**,所以后端收到的是空切片 ——
   * 而 AssignResources 的实现是"事务内先 DELETE 全部旧绑定再写新的",
   * 空切片 = 把这个角色的权限**全清空**。
   * 换句话说:打开"分配权限"抽屉点保存,角色的权限就没了。
   * 同一处的 GET 返回值 key 也一样对不上,所以抽屉打开时 pre-check 恒为空。
   * 同 commit 修 server + web(项目规则:改字段必须同 commit 改 server+web+doc)。
   */
  assignResources: (id: string, resourceIds: string[]) =>
    http.post<void>(`/system/roles/${id}/resources`, { resource_ids: resourceIds }),

  /**
   * 角色已分配资源 — 后端 GET /system/roles/:id/resources
   * 返回 { resource_ids: string[] }(snake_case,ID 字符串,同 assignResources)
   * 用于分配权限 Drawer 打开时 pre-check
   */
  getResources: (id: string) =>
    http.get<{ resource_ids: string[] }>(`/system/roles/${id}/resources`),

  /**
   * 角色已分配用户 — 后端暂无对应 endpoint
   * TODO: 后续在 server 注册 GET /system/roles/:id/users
   */
  getUsers: (id: string) => http.get<string[]>(`/system/roles/${id}/users`),
}

/* ====================== 资源(权限点) ====================== */

/**
 * ⚠️ 后端 GET /system/resources/tree 返的是**扁平列表**,不是树:
 *   model.SysResource 没有 children 字段,后端也从不填。
 *   层级要靠前端 buildMenuTree(按 parent_id)自己建 —— 见 @/utils/resourceTree。
 *   以前这里声明成 `ResourceTreeNode[]`(带 children),害得两个页面都以为
 *   拿到的就是树,`n.children` 恒为 undefined → 层级整个丢了。
 *   2026-10-25 改成诚实的 Resource[]。
 */
export const resourceApi = {
  /** 全量资源点(菜单 + 按钮 + API),**扁平列表** */
  tree: () => http.get<Resource[]>('/system/resources/tree'),

  /** 当前用户可访问的资源(默认全部 type)
   *  typeFilter: 'menu' 仅返菜单(供 Sidebar)/ 'api' 仅返 api(供按钮级)
   *  ⚠️ 后端此 endpoint 是单数 /resource/accessible(router.go:143) */
  accessible: (typeFilter?: 'menu' | 'api' | 'button') =>
    http.get<Resource[]>(
      `/system/resource/accessible${typeFilter ? `?type=${typeFilter}` : ''}`
    ),

  create: (data: Partial<Resource>) => http.post<void>('/system/resources', data),

  /**
   * 资源详情 — 后端 GET /system/resources/:id
   * 2026-10-19 M63c:供前端资源编辑 Drawer pre-fill
   */
  detail: (id: string) => http.get<Resource>(`/system/resources/${id}`),

  /** 后端 PATCH(非 PUT) */
  update: (id: string, data: Partial<Resource>) =>
    http.patch<void>(`/system/resources/${id}`, data),

  remove: (id: string) => http.delete<void>(`/system/resources/${id}`),

  /**
   * 资源列表(平铺,非树) — 后端已实现:GET /system/resources(router.go:272)
   */
  list: () => http.get<Resource[]>(`/system/resources`),
}

/* ====================== 部门 ====================== */

export interface DepartmentListQuery extends PageRequest {
  keyword?: string
  parentId?: string
  status?: number
}

export interface DepartmentTreeNode extends Department {
  children?: DepartmentTreeNode[]
}

export const departmentApi = {
  tree: () => http.get<DepartmentTreeNode[]>('/system/departments/tree'),

  /** 后端 GET /departments, query 传参(非 body) */
  list: (q: DepartmentListQuery) =>
    http.get<PageResponse<Department>>(
      '/system/departments',
      q as unknown as Record<string, unknown>
    ),

  /**
   * 部门详情 — 后端 GET /system/departments/:id
   * 2026-10-19 M63d:供前端部门编辑 Drawer pre-fill
   */
  detail: (id: string) => http.get<Department>(`/system/departments/${id}`),

  create: (data: Partial<Department>) => http.post<void>('/system/departments', data),

  /** 后端 PATCH(非 PUT) */
  update: (id: string, data: Partial<Department>) =>
    http.patch<void>(`/system/departments/${id}`, data),

  remove: (id: string) => http.delete<void>(`/system/departments/${id}`),
}
