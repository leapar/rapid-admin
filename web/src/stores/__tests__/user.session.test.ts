/**
 * stores/user.ts · 会话持久化生命周期(2026-10-28 M108)
 *
 * `stores/__tests__/user.store.test.ts` 锁的是 `setUserInfoFromServer` 的**内存态**;
 * 这一份锁的是**落盘 / 恢复**这一侧 —— 同样是"框架原语零覆盖"的高危区
 * (挂在全局中间件链上的那一层,清单里标着 ✅ 不代表有人读过它)。
 *
 * 锁住的四条:
 *   ① 登录 → 空闲基线对齐到现在(新会话的空闲计时重新起算);
 *   ② 登出 → 空闲基线被清掉(不留给下一段会话);
 *   ③ token 全过期后,光有一份 USER blob **不算已登录**(旧实现会置 true,
 *      于是首页闪一下再被 401 踢回登录页);
 *   ④ 恢复会话时把空闲基线重置到现在(浏览器关掉的那段时间不算空闲)。
 *
 * 反向探针(见 scripts/probe_m108.mjs):
 *   · hydrate 不再检查凭证 → 「光有 blob 不算已登录」用例变红;
 *   · setLogin 不再对齐空闲基线 → 「登录对齐基线」用例变红;
 *   · hydrate 不再重置空闲基线 → 「恢复时重置基线」用例变红。
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { useUserStore, hydrateUserStore } from '@/stores/user'
import { storage, session, clearSessionFrom } from '@/utils/storage'
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

/** 一份"上次登录时写下的"USER blob */
function writeUserBlob(): void {
  storage.set(STORAGE_KEYS.USER, { userInfo: USER, currentIdentity: null, isLoggedIn: true })
}

beforeEach(() => {
  clearSessionFrom()
  localStorage.clear()
  sessionStorage.clear()
  useUserStore.setState({ userInfo: null, currentIdentity: null, isLoggedIn: false })
})

describe('user store · 空闲基线跟着会话走', () => {
  it('setLogin 把空闲基线对齐到现在(新会话的空闲计时重新起算)', () => {
    const before = Date.now()

    useUserStore.getState().setLogin({
      userInfo: USER,
      accessToken: 'A1',
      refreshToken: 'R1',
      remember: true,
      expiresIn: 3600,
      refreshExpiresIn: 86400,
    })

    const at = storage.get<number>(STORAGE_KEYS.LAST_ACTIVE_AT)
    expect(at).not.toBeNull()
    expect(at as number).toBeGreaterThanOrEqual(before)
  })

  it('logout 会把空闲基线一起清掉(不留给下一段会话)', () => {
    useUserStore.getState().setLogin({
      userInfo: USER,
      accessToken: 'A1',
      refreshToken: 'R1',
      remember: true,
    })
    expect(storage.get<number>(STORAGE_KEYS.LAST_ACTIVE_AT)).not.toBeNull()

    useUserStore.getState().logout()

    expect(storage.get<number>(STORAGE_KEYS.LAST_ACTIVE_AT)).toBeNull()
  })
})

describe('user store · hydrate 的可信度判据', () => {
  it('token 全过期后,光有一份 USER blob 不算已登录', () => {
    // 会话早就过期了(两个 token 都被 TTL 收走),但 blob 还在盘上。
    // 旧实现只看 blob 里的 isLoggedIn ⇒ 内存态"已登录",实际一个凭证都没有。
    writeUserBlob()

    hydrateUserStore()

    expect(useUserStore.getState().isLoggedIn).toBe(false)
    // 顺手把孤儿 blob 清掉(里面有用户资料,留着还会让下一次 hydrate 再误判)
    expect(storage.get(STORAGE_KEYS.USER)).toBeNull()
  })

  it('refresh token 还活着 → 恢复会话(该续期,不是掉线)', () => {
    storage.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    writeUserBlob()

    hydrateUserStore()

    expect(useUserStore.getState().isLoggedIn).toBe(true)
    expect(useUserStore.getState().userInfo?.id).toBe('u1')
  })

  it('恢复会话时把空闲基线重置到现在(浏览器关掉的那段时间不算空闲)', () => {
    storage.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    writeUserBlob()
    storage.set(STORAGE_KEYS.LAST_ACTIVE_AT, Date.now() - 4 * 60 * 60 * 1000)

    hydrateUserStore()

    const at = storage.get<number>(STORAGE_KEYS.LAST_ACTIVE_AT)
    expect(at).not.toBeNull()
    expect(at as number).toBeGreaterThan(Date.now() - 60_000)
  })
})

describe('user store · 身份落点', () => {
  it('恢复会话时按已选身份定位(不把人打回第一个身份)', () => {
    const identity = {
      id: 'id-2',
      role_code: 'supplier',
      role_name: '供应商',
      project_id: 'p1',
      project_name: '项目一',
      permissions: [],
      valid_from: '',
      valid_to: '',
      is_active: true,
      is_primary: false,
    }
    storage.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    storage.set(STORAGE_KEYS.USER, {
      userInfo: { ...USER, identities: [identity] },
      currentIdentity: identity,
      isLoggedIn: true,
    })

    hydrateUserStore()

    expect(useUserStore.getState().currentIdentity?.id).toBe('id-2')
  })

  it('会话落在 sessionStorage 时,读的是 session 那一侧', () => {
    session.set(STORAGE_KEYS.REFRESH_TOKEN, 'R2')
    session.set(STORAGE_KEYS.USER, {
      userInfo: USER,
      currentIdentity: null,
      isLoggedIn: true,
    })

    hydrateUserStore()

    expect(useUserStore.getState().isLoggedIn).toBe(true)
    expect(useUserStore.getState().userInfo?.id).toBe('u1')
  })
})
