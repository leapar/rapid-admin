/**
 * 权限 Store —— 拉取 / 合并 / 唯一写路径(2026-10-27 M102)
 *
 * ## 这条网守的是什么
 *
 *   `fetchAccessible()` 是**全站权限的唯一来源**:切换身份、启动加载、
 *   SSE `perm_changed` 三个入口都走它。它做三件事:
 *
 *     1. 并行拉 menu / api / button 三类资源;
 *     2. 把 `user.identities[].permissions` ∪ api 码 ∪ button 码 **合并**成一个
 *        扁平权限码集合;
 *     3. 把 menu 平铺列表**建树**,把 `identities[].role_code` 写成 roleCodes。
 *
 *   §2.2 清单上这个 store 之前是**零用例**的,所以上面三件事没有任何回归网。
 *
 * ## 为什么第 4 条要盯"只有一条写路径"
 *
 *   M102 之前,`fetchAccessible` 自己**内联**写了一遍 store(建树 / 建 Set /
 *   写 roleCodes 三件事),而 `setPermissions` —— 一个语义完全相同的公开方法 ——
 *   **零调用方**。两条写路径迟早分叉:改了其中一条忘了另一条,
 *   症状是"有的入口刷新了权限,有的入口没刷新",而且没有任何报错。
 *
 *   现在的写法是 `fetchAccessible` 调 `setPermissions`,只有一条路。
 *   用例 4 直接断言这件事(打桩看有没有被调到)——
 *   探针把 `fetchAccessible` 改回内联写,这条用例变红。
 *
 * ## M149-b:当前角色码改用服务端权威值
 *
 *   M149 之后,前端按 `identities.find(i => i.id === current_identity_id)`
 *   **猜**当前身份 —— 那是 M149 自己引入的回归(见下面 M149-b 那个 describe)。
 *   现在优先用服务端下发的 `user.current_role_code`(= token claims 里那个
 *   role_code,与 RBAC 中间件判权限同一个来源)。
 *
 *   ⚠️ 本文件的基准 fixture `USER` **故意不带** `current_role_code` ——
 *     它代表"老后端"那条兼容路径(回退到 identities 查找)。
 *     M149-b 那个 describe 单独覆盖"新后端"路径,两条路都有回归网。
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'

vi.mock('@/api/auth', () => ({ getCurrentUser: vi.fn() }))
vi.mock('@/api/role', () => ({ resourceApi: { accessible: vi.fn() } }))

import { getCurrentUser } from '@/api/auth'
import { resourceApi } from '@/api/role'
import { usePermissionStore } from '@/stores/permission'
import type { Resource } from '@/types/permission'
import type { UserInfo } from '@/types/user'

const meMock = vi.mocked(getCurrentUser)
const accessibleMock = vi.mocked(resourceApi.accessible)

function menu(id: string, parentId: string, sort: number, name = `m${id}`): Resource {
  return { id, code: `menu.${id}`, name, type: 'menu', parent_id: parentId, sort, is_active: true }
}
function codeRes(code: string, type: 'api' | 'button'): Resource {
  return { id: code, code, name: code, type, parent_id: '0', sort: 0, is_active: true }
}

const USER = {
  id: 'u1',
  username: 'admin',
  nickname: '管理员',
  phone: '',
  avatar: '',
  current_identity_id: 'i1',
  identities: [
    {
      id: 'i1',
      role_code: 'ops',
      role_name: '运维',
      project_id: 'p1',
      project_name: '项目',
      permissions: ['user:list', 'button.user.add'],
      valid_from: '',
      valid_to: '',
      is_active: true,
      is_primary: true,
    },
    {
      id: 'i2',
      role_code: 'auditor',
      role_name: '审计',
      project_id: 'p1',
      project_name: '项目',
      permissions: ['user:list', 'audit:view'],
      valid_from: '',
      valid_to: '',
      is_active: true,
      is_primary: false,
    },
  ],
} as unknown as UserInfo

beforeEach(() => {
  usePermissionStore.setState({ menus: [], permissionCodes: new Set(), roleCodes: [] })
  meMock.mockReset()
  accessibleMock.mockReset()
  meMock.mockResolvedValue({ user: USER, resources: [] })
  accessibleMock.mockImplementation(async (type?: 'menu' | 'api' | 'button') => {
    if (type === 'menu') return [menu('1', '0', 2), menu('2', '0', 1), menu('3', '1', 1)]
    if (type === 'api') return [codeRes('user:list', 'api')]
    if (type === 'button') return [codeRes('button.user.delete', 'button')]
    return []
  })
})

describe('stores/permission · fetchAccessible', () => {
  it('menu 平铺列表必须被建成树(Sidebar 靠 children 渲染层级)', async () => {
    await usePermissionStore.getState().fetchAccessible()

    const menus = usePermissionStore.getState().menus
    // 根节点按 sort 升序:m2(sort=1) 在 m1(sort=2) 前面
    expect(menus.map((m) => m.id)).toEqual(['2', '1'])
    // m3 的 parent 是 m1 → 挂在 m1.children 下
    expect(menus[1]!.children?.map((c) => c.id)).toEqual(['3'])
  })

  it('权限码 = **当前身份**的权限 ∪ api 码 ∪ button 码', async () => {
    await usePermissionStore.getState().fetchAccessible()

    const codes = usePermissionStore.getState().permissionCodes
    // 当前身份(i1)的 permissions 要在
    expect(codes.has('user:list')).toBe(true)
    expect(codes.has('button.user.add')).toBe(true)
    // api / button 两类资源也要在
    expect(codes.has('button.user.delete')).toBe(true)
    // ⚠️ **M149**:别的身份(i2)的权限码**不能**进来 ——
    //   以前这里是"所有身份的 permissions 取并集",于是切到低权限身份后,
    //   高权限身份的那些码还在 ⇒ 按钮该藏的没藏。
    expect(codes.has('audit:view')).toBe(false)
  })

  it('roleCodes = **当前身份**的 role_code(不是所有身份的)', async () => {
    await usePermissionStore.getState().fetchAccessible()

    // ⚠️ M149:以前是所有身份的并集(['ops','auditor']),
    //   于是 `hasRole('ops')` 在切到 auditor 之后仍然是 true
    //   ⇒ RoleGuard / AuthButton 的 roleAnyOf / 路由守卫里的
    //   `has(code) || hasRole(code)` 全部按旧身份放行。
    expect(usePermissionStore.getState().roleCodes).toEqual(['ops'])
  })

  it('**只有一条写路径**:fetchAccessible 必须经过 setPermissions', async () => {
    const original = usePermissionStore.getState().setPermissions
    const spy = vi.fn((d: Parameters<typeof original>[0]) => original(d))
    usePermissionStore.setState({ setPermissions: spy })

    await usePermissionStore.getState().fetchAccessible()

    expect(spy).toHaveBeenCalledTimes(1)
    const payload = spy.mock.calls[0]![0]!
    // 写进去的必须是"扁平权限码数组",不是 Set —— setPermissions 内部再转 Set
    expect(Array.isArray(payload.permissionCodes)).toBe(true)
    expect(payload.roleCodes).toEqual(['ops'])
  })

  it('拉取失败时不能把已有权限清空(错误直接抛给调用方)', async () => {
    await usePermissionStore.getState().fetchAccessible()
    expect(usePermissionStore.getState().permissionCodes.has('user:list')).toBe(true)

    meMock.mockRejectedValueOnce(new Error('boom'))
    await expect(usePermissionStore.getState().fetchAccessible()).rejects.toThrow('boom')

    // 旧权限还在(没有半途清空)
    expect(usePermissionStore.getState().permissionCodes.has('user:list')).toBe(true)
  })
})

// ===== M149:权限可见性必须跟「当前身份」走 =====
//
// 现象(用户报的):从超级管理员切到用户管理员,**界面完全一样** ——
// 菜单还是超管那一套,按钮也一个没少。
//
// 两层原因,这一层管前端那一半:
//
//   后端 AccessibleResources 以前按"所有身份的并集"算(M149 同批已修);
//   前端这里以前也按"所有身份的并集"算 roleCodes / permissions。
//
// ⇒ 前端这半边的后果是:`hasRole('role_super_admin')` 在切走之后仍是 true
// ⇒ RoleGuard / AuthButton 的 roleAnyOf / 路由守卫里的
// `has(code) || hasRole(code)` 全部按超管放行 ⇒ **超管专属按钮还在**。
describe('stores/permission · M149 按当前身份算', () => {
  it('切到另一个身份后,roleCodes 与权限码都跟着换', async () => {
    // 第一次:当前身份 = i1(ops)
    await usePermissionStore.getState().fetchAccessible()
    expect(usePermissionStore.getState().roleCodes).toEqual(['ops'])

    // 切身份:服务端把 current_identity_id 换成 i2(auditor)
    meMock.mockResolvedValue({
      user: { ...USER, current_identity_id: 'i2' },
      resources: [],
    })
    await usePermissionStore.getState().fetchAccessible()

    const s = usePermissionStore.getState()
    expect(s.roleCodes).toEqual(['auditor'])
    expect(s.permissionCodes.has('audit:view')).toBe(true)
    // 旧身份的那条不能还在
    expect(s.permissionCodes.has('button.user.add')).toBe(false)
  })

  it('找不到当前身份时 roleCodes 为空(倒向拒绝,不是倒向放行)', async () => {
    meMock.mockResolvedValue({
      user: { ...USER, current_identity_id: 'i-not-exist' },
      resources: [],
    })
    await usePermissionStore.getState().fetchAccessible()

    expect(usePermissionStore.getState().roleCodes).toEqual([])
  })
})

// ===== M149-b:当前角色码用服务端权威值,不再靠"猜" =====
//
// M149 把 roleCodes 改成"只认当前身份"之后,取值方式是
// `identities.find(i => i.id === current_identity_id)` —— **猜**。
//
// 猜会错在两处:
//
//	① 身份被管理员改过 ⇒ 后端 `writeUserIdentitiesFromItems` 是**全量替换**
//	  (身份行的 ID 全变)⇒ 旧 session 的 `current_identity_id` 在列表里
//	  **找不到** ⇒ roleCodes 变空 ⇒ 角色门控(AuthButton 的 roleAnyOf)
//	  整个失效(按钮凭空消失 —— 从"该藏的没藏"变成"该显的不显",
//	  两个都是同一个洞的两面)。
//
//	② 库里角色改了但 token 还是旧角色 ⇒ 前端显示**新**角色,
//	  后端按 token 里的**旧**角色判 ⇒ 看得见的点了 403。
//
// ⇒ 服务端直接下发 `current_role_code`(= token claims 里那个 role_code,
//   与 RBAC 中间件**同一个来源**)⇒ 前端不用猜。
describe('stores/permission · M149-b 服务端权威角色码', () => {
  it('current_role_code 存在时,roleCodes 用它(不猜 identities)', async () => {
    // ⚠️ 故意造出"两个来源不一致"的情形:
    //   current_identity_id 指向 i2(auditor),但服务端权威值说是 ops。
    //   权威值必须赢 —— 因为执行层(RBAC 中间件)就是按 role_code 判的。
    meMock.mockResolvedValue({
      user: { ...USER, current_identity_id: 'i2', current_role_code: 'ops' },
      resources: [],
    })
    await usePermissionStore.getState().fetchAccessible()

    expect(usePermissionStore.getState().roleCodes).toEqual(['ops'])
  })

  it('身份被管理员改过(ID 全变、找不到当前身份)时,权威值仍让 roleCodes 非空', async () => {
    // 这条在"靠猜"的老实现下是**红的**:roleCodes 会变空。
    meMock.mockResolvedValue({
      user: { ...USER, current_identity_id: 'i-not-exist', current_role_code: 'ops' },
      resources: [],
    })
    await usePermissionStore.getState().fetchAccessible()

    expect(usePermissionStore.getState().roleCodes).toEqual(['ops'])
  })

  it('老后端不返 current_role_code 时,回退到"找当前身份"的老办法', async () => {
    // 基准 fixture 就没有 current_role_code ⇒ 走回退路径
    meMock.mockResolvedValue({
      user: { ...USER, current_identity_id: 'i2' },
      resources: [],
    })
    await usePermissionStore.getState().fetchAccessible()

    expect(usePermissionStore.getState().roleCodes).toEqual(['auditor'])
  })

  it('两条路都拿不到角色码 ⇒ roleCodes 为空(倒向拒绝)', async () => {
    meMock.mockResolvedValue({
      user: { ...USER, current_identity_id: 'i-not-exist', current_role_code: '' },
      resources: [],
    })
    await usePermissionStore.getState().fetchAccessible()

    expect(usePermissionStore.getState().roleCodes).toEqual([])
  })
})

describe('stores/permission · setPermissions / reset / 判定方法', () => {
  it('setPermissions 建树 + 建 Set', () => {
    usePermissionStore.getState().setPermissions({
      menus: [menu('1', '0', 1), menu('2', '1', 1)],
      permissionCodes: ['a', 'b'],
      roleCodes: ['ops'],
    })

    const s = usePermissionStore.getState()
    expect(s.permissionCodes).toBeInstanceOf(Set)
    expect(s.permissionCodes.has('a')).toBe(true)
    expect(s.menus[0]!.children?.map((c) => c.id)).toEqual(['2'])
  })

  it('reset 把三样都清空', () => {
    usePermissionStore.setState({
      menus: [menu('1', '0', 1)],
      permissionCodes: new Set(['a']),
      roleCodes: ['ops'],
    })

    usePermissionStore.getState().reset()

    const s = usePermissionStore.getState()
    expect(s.menus).toEqual([])
    expect(s.permissionCodes.size).toBe(0)
    expect(s.roleCodes).toEqual([])
  })

  it('has / hasAny / hasAll / hasRole 的语义', () => {
    usePermissionStore.setState({
      permissionCodes: new Set(['a', 'b']),
      roleCodes: ['ops'],
    })
    const s = usePermissionStore.getState()

    expect(s.has('a')).toBe(true)
    expect(s.has('zzz')).toBe(false)
    expect(s.hasAny(['zzz', 'a'])).toBe(true)
    expect(s.hasAny(['zzz'])).toBe(false)
    expect(s.hasAll(['a', 'b'])).toBe(true)
    expect(s.hasAll(['a', 'zzz'])).toBe(false)
    expect(s.hasRole('ops')).toBe(true)
    expect(s.hasRole('super_admin')).toBe(false)
  })
})
