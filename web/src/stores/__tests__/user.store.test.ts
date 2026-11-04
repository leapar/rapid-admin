/**
 * user store · 启动路径的登录态(2026-10-25)
 *
 * 这组用例锁的是 `setUserInfoFromServer` 的**内存态**。
 *
 * 背景(2026-10-25 修的真 bug):
 *   `setUserInfoFromServer` 只可能在 `/auth/me` **成功**之后被调用(唯一调用方
 *   PermissionLoader)—— 即"服务端已确认这个 token 有效"。但它此前只写盘
 *   (`isLoggedIn: true`),没同步内存态,于是**内存和落盘自相矛盾**。
 *
 *   触发条件:落盘那份 USER blob 来自**旧版本**(没有 isLoggedIn 字段)或被人清过
 *   → hydrate 出来的内存态是 false → PermissionLoader 拿到 200 之后,内存态
 *   仍然是 false →
 *     · AuthGuard 把一个**有效会话**踢回登录页;
 *     · tokenScheduler(`if (!store.isLoggedIn) return`)永远不排续期。
 *
 * 反向探针:把 `isLoggedIn: true` 从内存态那行删掉 → 用例 1、2 变红
 * (用例 3 验的是 token 不变式,本来就与 isLoggedIn 无关,理应保持绿)。
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { useUserStore, hydrateUserStore } from '@/stores/user'
import { clearSessionFrom, storage, session } from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'
import type { UserInfo } from '@/types/user'

const USER: UserInfo = {
  id: 'u1',
  username: 'admin',
  nickname: '管理员',
  phone: '138****0000',
  email: '',
  avatar: '',
  identities: [],
  current_identity_id: '',
}

/** 写一个「旧版本格式」的 USER blob —— 没有 isLoggedIn 字段 */
function writeLegacyBlob(): void {
  localStorage.setItem(
    STORAGE_KEYS.USER,
    JSON.stringify({ value: { userInfo: USER, currentIdentity: null } }),
  )
}

beforeEach(() => {
  clearSessionFrom()
  localStorage.clear()
  sessionStorage.clear()
  useUserStore.setState({ userInfo: null, currentIdentity: null, isLoggedIn: false })
})

describe('setUserInfoFromServer · 内存态', () => {
  it('/auth/me 成功后,内存态必须是「已登录」', () => {
    useUserStore.getState().setUserInfoFromServer(USER)

    expect(useUserStore.getState().isLoggedIn).toBe(true)
    expect(useUserStore.getState().userInfo?.id).toBe('u1')
  })

  it('落盘 blob 来自旧版本(没有 isLoggedIn)时,也能把内存态拉回 true', () => {
    // 旧版本写下的 blob:没有 isLoggedIn 字段
    writeLegacyBlob()
    // refresh token 在(会话有效)—— activeBackend 才会选中 localStorage
    storage.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')

    // hydrate:读到的 blob 没有 isLoggedIn → 内存态保持 false
    hydrateUserStore()
    expect(useUserStore.getState().isLoggedIn).toBe(false)

    // PermissionLoader 的动作:服务端认了这个 token
    useUserStore.getState().setUserInfoFromServer(USER)

    // ← 修之前这里仍是 false:AuthGuard 会把有效会话踢回登录页,
    //   tokenScheduler 也不会排续期
    expect(useUserStore.getState().isLoggedIn).toBe(true)
  })

  it('只更新 userInfo,不动 token、不改持久化后端(「记住我」不变式)', () => {
    // 未勾「记住我」的登录:token 落在 sessionStorage
    session.set(STORAGE_KEYS.TOKEN, 'A1')
    session.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    storage.set(STORAGE_KEYS.USER, { userInfo: USER, currentIdentity: null, isLoggedIn: true })

    useUserStore.getState().setUserInfoFromServer(USER)

    // token 仍在 sessionStorage,没有被搬进 localStorage
    expect(session.get<string>(STORAGE_KEYS.TOKEN)).toBe('A1')
    expect(storage.get<string>(STORAGE_KEYS.TOKEN)).toBeNull()
  })
})

/**
 * switchIdentity · 切换身份后的 userInfo(M146b)
 *
 * ⚠️ **这个洞是"切身份"这条链上最后一处错位。**
 *
 *   `switch-identity` 的响应里带着**服务端权威的 user**(identities 全量 +
 *   新的 current_identity_id)。前端此前只拿它去**取**新身份对象,取完就丢:
 *   store 的 switchIdentity 只 `set({ currentIdentity })`,**不碰 userInfo**。
 *
 *   后果:
 *     · `userInfo.current_identity_id` 停在切之前的那个 ⇒
 *       个人中心("当前身份:XXX")显示的是**切之前的角色名**;
 *     · `is_platform_super_admin` 也是旧值 ⇒ 界面按错的身份渲染。
 *
 *   而且它**只存在内存里**对不上还好说 —— 落盘那份 blob 同样没更新,
 *   F5 之后 hydrate 读回来的还是旧值,错位就变成了持久状态。
 */
describe('switchIdentity · 服务端权威 user', () => {
  const SERVER_USER: UserInfo = {
    ...USER,
    current_identity_id: 'i2',
    identities: [
      { id: 'i1', role_code: 'r1', role_name: '角色一', project_id: '0', project_name: '', permissions: [], valid_from: '', valid_to: '', is_active: true, is_primary: true },
      { id: 'i2', role_code: 'r2', role_name: '角色二', project_id: '0', project_name: '', permissions: [], valid_from: '', valid_to: '', is_active: true, is_primary: false },
    ],
  }

  const IDENTITY_I2 = SERVER_USER.identities[1]!

  it('给了服务端 user ⇒ 内存态与落盘 blob 的 userInfo 都换成它', () => {
    session.set(STORAGE_KEYS.TOKEN, 'A1')
    session.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    session.set(STORAGE_KEYS.USER, { userInfo: USER, currentIdentity: null, isLoggedIn: true })

    useUserStore.getState().switchIdentity(
      IDENTITY_I2,
      { accessToken: 'A2', refreshToken: 'R2', expiresIn: 3600, refreshExpiresIn: 86400 },
      SERVER_USER,
    )

    // 内存态
    expect(useUserStore.getState().userInfo?.current_identity_id).toBe('i2')
    expect(useUserStore.getState().currentIdentity?.id).toBe('i2')
    // 落盘 blob 也必须换 —— 否则 F5 之后 hydrate 读回来的还是旧身份
    const blob = session.get<{ userInfo: UserInfo; currentIdentity: { id: string } | null }>(
      STORAGE_KEYS.USER,
    )
    expect(blob?.userInfo.current_identity_id).toBe('i2')
    expect(blob?.currentIdentity?.id).toBe('i2')
    // 新 token 也换了
    expect(session.get<string>(STORAGE_KEYS.TOKEN)).toBe('A2')
    expect(session.get<string>(STORAGE_KEYS.REFRESH_TOKEN)).toBe('R2')
  })

  it('切换身份只写会话所在的那个后端,不动另一个(「记住我」不变式)', () => {
    // 会话在 sessionStorage(未勾「记住我」)
    session.set(STORAGE_KEYS.TOKEN, 'A1')
    session.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    session.set(STORAGE_KEYS.USER, { userInfo: USER, currentIdentity: null, isLoggedIn: true })
    // 另一个后端里躺着一份陈旧 token —— 切身份不许去动它
    storage.set(STORAGE_KEYS.TOKEN, 'OTHER_BACKEND')

    useUserStore.getState().switchIdentity(
      IDENTITY_I2,
      { accessToken: 'A2', refreshToken: 'R2' },
      SERVER_USER,
    )

    expect(session.get<string>(STORAGE_KEYS.TOKEN)).toBe('A2')
    expect(storage.get<string>(STORAGE_KEYS.TOKEN)).toBe('OTHER_BACKEND')
  })
})
