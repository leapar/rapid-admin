/**
 * 用户管理 API
 *
 * 路径规则: 后端 /rapid-admin/v1/system/users (复数)
 * 详见 server/internal/api/router/router.go:126
 *
 * 2026-10-25 修正请求体字段名(用户报"用户编辑界面无法设置角色"时挖出来的):
 *   请求体一直发的是 camelCase(realName / departmentIds / primaryDepartmentId /
 *   initialPassword / status),而后端 service.CreateUserRequest /
 *   UpdateUserRequest 绑的是 **snake_case**:
 *     username / password / nickname / phone / email
 *     department_ids / primary_department_id / identities / is_active
 *   Go 的 encoding/json 只做大小写不敏感匹配、**不认下划线** → 这些字段
 *   全被静默丢弃(不报错!):
 *     · 新增用户缺 password → 后端直接返"密码不能为空"(新增功能整个是坏的)
 *     · 改部门 / 改状态 → 看着保存成功了,其实一点没改
 *   现在按后端的真实契约发字段(项目规则:改 web 适配 server,不动 server)。
 *   字段表见技术方案 §7.7.7。
 */
import { http } from './client'
import { PageRequest, PageResponse } from '@/types/api'
import { SysUser, IdentityItem } from '@/types/permission'

/**
 * 用户列表的 query 条件
 *
 * ⚠️ **筛选参数是 camelCase,跟 body 的 snake_case 不一样** ——
 *    列表 handler 是**手写取 query** 的(server/internal/api/handler/v1/user.go:29):
 *      Keyword:      c.Query("keyword")
 *      DepartmentID: h.MustUint(c, "departmentId", 0)   ← camelCase
 *      RoleCode:     c.Query("roleCode")                 ← camelCase
 *    没走 struct 绑定,所以 service.UserListQuery 上的 json tag 对 query 不起作用。
 *    写错的后果是**静默忽略**(传了等于没传)。
 *
 * ⚠️ `is_active` / `identity_id` 在 service 的 struct 上有,但 **handler 没绑** ——
 *    按状态 / 按身份筛选目前不生效,别写进筛选器。
 */
export interface UserListQuery extends PageRequest {
  keyword?: string
  /** 部门 ID(字符串形式,19 位数字不能转 Number) */
  departmentId?: string
  /** 角色 code(如 role_user_admin),不是 role id */
  roleCode?: string
}

/**
 * 新增用户请求体(后端 service.CreateUserRequest,1:1)
 *
 * ⚠️ password 必须是 **64 位 hex**:前端要用 preHashPassword() 先哈希
 *    (sha256("rapid-admin::" + 明文)),后端只做 bcrypt(明文)哈希,
 *    长度不对直接返"密码格式错误(需前端 pre-hash)"。
 */
export interface CreateUserRequest {
  username: string
  /** 64 位 hex(前端 pre-hash 结果) */
  password: string
  nickname: string
  phone: string
  email?: string
  /** 多选部门 ID(后端 sys_user_department 写多行) */
  department_ids: string[]
  /** 主部门 ID(必须在 department_ids 内,后端校验) */
  primary_department_id?: string
  /** 多身份(每身份一角色 + 项目 + 部门 + 有效期) */
  identities: IdentityItem[]
}

/**
 * 编辑用户请求体(后端 service.UpdateUserRequest,1:1)
 *
 * ⚠️ **全是可选的,语义是 PATCH**:
 *   · 指针字段 nil = 不改
 *   · identities 非 nil = **全量替换**(后端先清空该用户全部身份再重建)
 *   · department_ids 非 nil = 同理,全量替换该用户的部门
 * 所以前端只发"真的改了"的字段 —— 详见 pages/system/user/userForm.ts 的
 * buildUpdatePayload(把没改的字段省掉,免得手机号/邮箱/部门被洗白)。
 */
export interface UpdateUserRequest {
  nickname?: string
  phone?: string
  email?: string
  /** 非空数组 = 全量替换(含清空,空数组=清空所有部门);undefined = 不改 */
  department_ids?: string[]
  primary_department_id?: string
  /** 非空数组 = 全量替换;undefined = 不改 */
  identities?: IdentityItem[]
  /** true = 启用,false = 停用;undefined = 不改 */
  is_active?: boolean
}

export const userApi = {
  /** 后端 GET /system/users, query 传参(非 body) */
  list: (q: UserListQuery) =>
    http.get<PageResponse<SysUser>>('/system/users', q as unknown as Record<string, unknown>),

  /**
   * 用户详情 — 后端 GET /system/users/:id
   *
   * ⚠️ 返回的是 *service.UserDetail(§7.7.2 说"禁止 handler 直接返 *model.Xxx",
   *    但目前只有 auth / platform-config 两个模块做了 DTO 包装,用户模块还没):
   *   · identities  ✅ 有(而且按 is_primary DESC 排序)→ 角色的回填数据就是它
   *   · departments ✅ 有(2026-10-25 M5d 补的)→ 部门的回填数据就是它
   *     ⚠️ 2026-10-25 之前**没有**这个字段 —— 前端编辑弹窗只能把"部门"置为只读
   *       (department_ids 是"清空重建"式全量替换,发一份不完整的清单
   *        就等于把这个人的次要部门洗掉)。补上之后部门就能正常编辑了。
   *   · phone/email ❌ 没有(model.SysUser 这两个字段是 json:"-",密文不外发)
   * 手机号/邮箱只能从列表行拿(列表在有 user:read-sensitive 权限时才给明文)。
   */
  detail: (id: string) => http.get<SysUser>(`/system/users/${id}`),

  create: (data: CreateUserRequest) => http.post<void>('/system/users', data),

  /** 后端 PATCH(非 PUT) */
  update: (id: string, data: UpdateUserRequest) =>
    http.patch<void>(`/system/users/${id}`, data),

  remove: (id: string) => http.delete<void>(`/system/users/${id}`),

  /**
   * 批量删除 — 后端 POST /system/users/batch-delete(router.go:258,已注册)
   * 后端要求 ids 非空、单次最多 200 个、原子执行(任一失败整体回滚)
   */
  batchRemove: (ids: string[]) =>
    http.post<void>('/system/users/batch-delete', { ids }),

  /**
   * 启用/停用 — 后端 endpoint 是 POST /users/:id/toggle, body 为 {active: bool}
   * (前端原用 PATCH /status + {status} 与后端签名不一致)
   */
  toggleStatus: (id: string, active: boolean) =>
    http.post<void>(`/system/users/${id}/toggle`, { active }),

  unlock: (id: string) => http.post<void>(`/system/users/${id}/unlock`),
}
