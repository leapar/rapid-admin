/**
 * 权限 Store
 *
 * 包含:
 * 1. 当前身份可访问的菜单(后端返回,前端按 parent_id 构造成树)
 * 2. 当前身份的所有权限码(用于按钮级控制)
 * 3. 路由级权限判断
 */
import { create } from 'zustand'
import type { Resource } from '@/types/permission'
import { getCurrentUser } from '@/api/auth'
import { resourceApi } from '@/api/role'
import { buildMenuTree } from '@/utils/resourceTree'

interface PermissionState {
  /** 菜单树(顶级,带 children 嵌套) */
  menus: Resource[]
  /** 扁平化权限码集合(模块:资源:动作) */
  permissionCodes: Set<string>
  /** 当前角色编码集合 */
  roleCodes: string[]

  setPermissions: (data: { menus: Resource[]; permissionCodes: string[]; roleCodes: string[] }) => void
  /**
   * 重新拉取当前用户可访问资源(用于切换身份后刷新菜单/权限码)
   * - 菜单: resourceApi.accessible('menu')
   * - api:   resourceApi.accessible('api') 合并到 permissionCodes
   * - 角色:  user.current_role_code(M149-b;服务端权威值 = token claims 的 role_code)
   *         —— **不是** identities[].role_code(那是"所有身份"的并集)
   */
  fetchAccessible: () => Promise<void>
  reset: () => void

  /* -------- 工具方法(对外暴露,不存 state) -------- */
  has: (code: string) => boolean
  hasAny: (codes: string[]) => boolean
  hasAll: (codes: string[]) => boolean
  hasRole: (roleCode: string) => boolean
}


export const usePermissionStore = create<PermissionState>((set, get) => ({
  menus: [],
  permissionCodes: new Set(),
  roleCodes: [],

  setPermissions: ({ menus, permissionCodes, roleCodes }) =>
    set({
      menus: buildMenuTree(menus),
      permissionCodes: new Set(permissionCodes),
      roleCodes,
    }),

  fetchAccessible: async () => {
    // 1. 当前用户 —— 要的是「**当前身份**」(current_identity_id),不是身份列表
    //   2026-10-18: getCurrentUser() 现在返 { user, resources },需解 user
    //
    // ⚠️ **M149(2026-11-01):以前这里取的是"所有身份"的 role_code / permissions**
    //
    //   roleCodes = user.identities.map(i => i.role_code)
    //
    //   那是**账号属性**("这个人有哪几个身份"),不是"我现在以什么身份行事"。
    //   ⇒ 多身份用户(超管 + 普通身份)从超管切到普通身份后:
    //     · `hasRole('role_super_admin')` 仍然是 true
    //       ⇒ RoleGuard / AuthButton 的 roleAnyOf / 路由守卫里的
    //         `has(code) || hasRole(code)` 全部按超管放行
    //       ⇒ **超管专属的按钮还在**(用户报的现象之一)
    //     · identities[].permissions 也把所有身份的并了进来(同上)
    //
    //   现在只认**当前身份** —— 与后端执行层(RBAC 中间件用 caller.RoleCode)
    //   同一口径:**界面显示的 = 实际能做的**。
    //
    // ⚠️ **M149-b:当前角色的取值不再靠"猜"**
    //
    //   优先用服务端下发的 `user.current_role_code` —— 它就是 token claims 里
    //   那个 role_code,**与 RBAC 中间件判权限是同一个来源**。
    //   回退到"找当前身份"的老办法只是为了兼容老后端;那个办法有两处会猜错:
    //     ① 身份被管理员改过(后端全量重建身份行 ⇒ ID 全变)⇒
    //        `current_identity_id` 在 identities 里**找不到** ⇒ roleCodes 空
    //        ⇒ 角色门控(AuthButton 的 roleAnyOf)整个失效;
    //     ② 库里角色改了但 token 还是旧角色 ⇒ 前端显示新角色、
    //        后端按旧角色判 ⇒ 看得见的点了 403。
    //
    //   ⚠️ 两条路都拿不到角色码 ⇒ roleCodes 空 —— 那是**拒绝**的方向,
    //     安全侧倒向。
    const { user } = await getCurrentUser()
    const currentIdentity = user.identities?.find((i) => i.id === user.current_identity_id)
    const currentRoleCode = user.current_role_code || currentIdentity?.role_code || ''
    const roleCodes = currentRoleCode ? [currentRoleCode] : []

    // 2. 三种资源并行拉:菜单 / api / button(2026-10-18 加 button,详见 issue:button 粒度 perm)
    //   - menu:   只用于 Sidebar 渲染
    //   - api:    路由级 / 大粒度(暂保留,等业务彻底迁完再下架)
    //   - button: RowAction / 工具栏按钮粒度(usePermission(perm) 核心判断源)
    //
    //   ⚠️ M149:这三个端点后端已改成「按当前身份」算(M149 同批),
    //   所以这里拿到的就是**当前身份**那一份,不用再在前端过滤。
    const [menuResources, apiResources, buttonResources] = await Promise.all([
      resourceApi.accessible('menu'),
      resourceApi.accessible('api'),
      resourceApi.accessible('button'),
    ])

    // 3. 合并 → permissionCodes
    const permissionCodes = new Set<string>()
    // ⚠️ M149:只并**当前身份**的 permissions(以前并所有身份的)
    for (const code of currentIdentity?.permissions || []) permissionCodes.add(code)
    for (const r of apiResources) if (r.code) permissionCodes.add(r.code)
    for (const r of buttonResources) if (r.code) permissionCodes.add(r.code)

    // ⚠️ M102:统一走 `setPermissions` —— 以前这里是**第二份**写 store 的代码
    // (建树 / 建 Set / 写 roleCodes 三件事各写一遍),而 `setPermissions`
    // 自己**零调用方**。两条写路径迟早会分叉(改了其中一条忘了另一条)。
    // 现在只有一条,而且它有调用方。
    get().setPermissions({
      menus: menuResources,
      permissionCodes: [...permissionCodes],
      roleCodes,
    })
  },

  reset: () =>
    set({
      menus: [],
      permissionCodes: new Set(),
      roleCodes: [],
    }),

  has: (code) => get().permissionCodes.has(code),
  hasAny: (codes) => {
    const set = get().permissionCodes
    return codes.some((c) => set.has(c))
  },
  hasAll: (codes) => {
    const set = get().permissionCodes
    return codes.every((c) => set.has(c))
  },
  hasRole: (roleCode) => get().roleCodes.includes(roleCode),
}))
