/**
 * 权限 / 角色 / 资源 类型
 * 与后端 §5 RBAC + Casbin 对齐
 */
// ⚠️ 2026-10-21 全仓 ID 改字符串
//
// 后端主键是 sonyflake 19 位数(~9.5e16),超出 JS 的 2^53 安全整数上限。
// JSON.parse 会静默截断末 6 位 —— 回传后端必然 404(身份切换就是这么死的),
// 而且**不报错**:拿到的是一个"看起来合法"的错 ID,排查成本极高。
//
// 所以 wire 上所有 ID 一律是 string。Go 侧用 `json:",string"` 保持 uint64
// 不变(slice 除外,内置选项对数组无效,改用 jsonid.IDList)。
// 本文件里的 id / *_id 一律 string,**不要**再做算术或隐式转换。

/** 角色 */
export interface Role {
  id: string
  code: string
  name: string
  description: string
  sort: number
  /** 角色类型: business 业务 / platform 平台 */
  type: 'business' | 'platform'
  is_builtin: boolean
  enabled: boolean
  created_at: string
  updated_at: string
}

/** 资源 */
export interface Resource {
  id: string
  code: string // e.g. user:list
  name: string
  type: 'menu' | 'button' | 'api' | 'data'
  /** 父资源 ID。根节点后端返 "0"。
   *
   *  ⚠️ 这里历史上还留着一个 camelCase 的 parentId,注释写着"后端实际返 camelCase"。
   *  那条注释是**反的**:2026-10-18 全仓统一 snake_case 之后,后端 SysResource
   *  的 tag 就是 `json:"parent_id,string"`,从来没发过 parentId。
   *  老代码靠 `r.parentId ?? r.parent_id` 的兜底才碰巧能用 —— 也就是每次
   *  取值都走 undefined 分支。留着只为兼容旧 mock,新代码一律读 parent_id。 */
  parent_id?: string
  /** @deprecated 后端从不发送,仅为兼容旧 mock 保留 */
  parentId?: string
  path?: string
  icon?: string
  /** HTTP 方法 —— **纯标注,不参与鉴权**。
   *
   *  ⚠️ RBAC 只认 `module:action` 编码(`middleware.RBAC` 从不读 HTTP 方法),
   *     所以 method 填 GET 还是留空,**权限行为完全一样**,
   *     它只是"这个权限点对应哪个接口"的说明。
   *  ⚠️ 以前前端类型里漏了这个字段(表格里靠 any 渲染),
   *     资源编辑弹窗要回填它 ⇒ 2026-10-30 补上。 */
  method?: string
  sort: number
  /** 启用 —— 后端 SysResource 的 tag 是 `json:"is_active"`,不是 isActive。
   *  同上,之前声明成必填 isActive,而 wire 上根本没有这个 key。 */
  is_active?: boolean
  /** @deprecated 后端从不发送 */
  isActive?: boolean
  /** @deprecated 更老的一版字段名 */
  enabled?: boolean
  /** 子资源(由前端树化函数填,后端扁平返回不带此字段) */
  children?: Resource[]
  /** 是否在 Sidebar 等位置可见(默认 true)
   *
   *  ⚠️ **后端现在没有这个字段** —— `model.SysResource` 只有 `is_active`,
   *  没有 `visible` 列(`server/internal/model/user.go:114`)。
   *  所以 Sidebar 里那句 `.filter((r) => r.visible !== false)` **今天恒为真**,
   *  是个空过滤 —— 菜单的可见性目前只由 `is_active` 决定。
   *  留着这个字段是因为:①它是"资源启用但菜单隐藏"这种需求的预留位;
   *  ②真要上线这个能力,后端加一列 + seed 填值,前端不用再改。
   *  但**别把"运营可以隐藏单个菜单"当成已实现的能力** —— 那需要后端加列。 */
  visible?: boolean
}

/** 角色-资源绑定 */
export interface RoleResource {
  role_id: string
  resource_id: string
  /** 动作: read / create / update / delete / approve / export */
  actions: string[]
}

/** 部门 */
export interface Department {
  id: string
  code: string
  name: string
  parent_id: string
  path: string // 1,2,3,
  sort: number
  leader_id?: string
  leader_name?: string
  member_count: number
  is_active: boolean
  children?: Department[]
}

/** 用户-部门-角色(用户列表条目) */
export interface SysUser {
  id: string
  username: string
  nickname: string
  phone: string
  email?: string
  avatar: string
  /** 主部门(单数,后端 ListItems 投影自 sys_user_department.is_primary=true) */
  department_id: string
  department_name: string
  /** 2026-10-19:多部门(后端 Get 返,List 暂未返);前端用于 ViewDrawer 展示 */
  departments?: { id: string; name: string; is_primary: boolean }[]
  /** 2026-10-19:多身份(后端 Get 返,List 暂未返) */
  identities?: SysUserIdentity[]
  /**
   * 2026-10-30 M122:这个用户**当前有效**的角色(后端 List 返)
   *
   * ⚠️ **不是身份清单** —— 身份(含项目 / 部门 / 有效期 / 主身份)在详情抽屉里。
   *   这里只是"这个人有什么权限",按 role_code 去重。
   *   后端保证**不是 null**:没有角色就是空数组。
   */
  roles?: SysUserRole[]
  status: 'active' | 'disabled' | 'locked'
  last_login_at?: string
  last_login_ip?: string
  mfa_enabled: boolean
  is_platform_super_admin: boolean
  created_at: string
}

/** 用户-身份(后端 sys_user_identity 1:1 投影) */
export interface SysUserIdentity {
  id: string
  user_id: string
  role_code: string
  role_name: string
  project_id: string
  dept_id: string
  /** RFC3339 字符串;后端 model.Time 零值序列化成 null(valid_to = 永久) */
  valid_from: string | null
  valid_to: string | null
  is_active: boolean
  is_primary: boolean
}

/**
 * 用户-角色摘要(后端 UserListItem.Roles,2026-10-30 M122)
 *
 * ⚠️ **不是身份** —— 身份(SysUserIdentity)含项目 / 部门 / 有效期 / 主身份;
 *   这里只是"这个人有什么权限",按 role_code 去重。
 */
export interface SysUserRole {
  role_code: string
  role_name: string
}

/** 身份编辑项(IdentityEditorDrawer 用) */
export interface IdentityItem {
  role_code: string
  role_name?: string
  project_id?: string
  dept_id?: string
  valid_from?: string | null
  valid_to?: string | null
  is_primary?: boolean
}
