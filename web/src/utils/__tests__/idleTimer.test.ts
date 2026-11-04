/**
 * utils/idleTimer.ts 回归网(2026-10-28 M108)
 *
 * 这一层此前**一条网都没有**。空闲超时是"用户还在不在"的唯一裁决者,
 * 而它同时握着三个敏感动作:吊销后端会话、清本地凭证、跳登录页。
 *
 * 锁住的五条:
 *   ① 判定边界(29 分 59 秒不超时 / 正好 30 分钟超时 / 从来没记录过不超时);
 *   ② 活动事件会刷新时间戳(节流);
 *   ③ 超时处理**不重入**(15s 轮询在网络往返期间不会重复发登出);
 *   ④ 超时后本地凭证被清干净;
 *   ⑤ 跳登录页**带 redirect**(和 401 路径行为一致)。
 *
 * ⭐ 最值钱的一条是文件末尾那条 **P1 回归**:
 *   "关掉浏览器几小时再打开"曾经会在 15 秒内把刚恢复的会话踢掉 ——
 *   「记住我」的 30 天持久会话实际只值 30 分钟。
 *
 * 反向探针(见 scripts/probe_m108.mjs):
 *   · 去掉 handleIdleTimeout 的重入守卫 → 「不重入」用例变红;
 *   · 去掉 buildIdleLoginUrl 的 redirect → 「带 redirect」用例变红;
 *   · hydrate 不再重置空闲基线 → 「P1 回归」用例变红。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// ⚠️ 必须用 vi.hoisted:vi.mock 的工厂会被提前到 import 之前执行,
//   直接引用模块顶层的 const 会拿到 undefined。
const { logoutMock } = vi.hoisted(() => ({ logoutMock: vi.fn() }))

vi.mock('@/api/auth', () => ({ logout: logoutMock }))
vi.mock('@/utils/tokenScheduler', () => ({ cancelTokenRefresh: vi.fn() }))

import {
  IDLE_TIMEOUT_MS,
  isIdleExpired,
  getLastActiveAt,
  startIdleTimer,
  stopIdleTimer,
  handleIdleTimeout,
  buildIdleLoginUrl,
} from '@/utils/idleTimer'
import { hydrateUserStore } from '@/stores/user'
import { useUserStore } from '@/stores/user'
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

beforeEach(() => {
  clearSessionFrom()
  localStorage.clear()
  sessionStorage.clear()
  logoutMock.mockReset()
  logoutMock.mockResolvedValue(undefined)
  useUserStore.setState({ userInfo: null, currentIdentity: null, isLoggedIn: false })
  // 停在 /login —— 免得 handleIdleTimeout 真去改 window.location.href
  // (jsdom 会打印 "Not implemented: navigation")。
  window.history.pushState({}, '', '/login')
})

afterEach(() => {
  stopIdleTimer()
  vi.useRealTimers()
})

describe('idleTimer · 判定', () => {
  it('29 分 59 秒不超时,正好 30 分钟超时', () => {
    const now = 1_800_000_000_000
    expect(isIdleExpired(now - IDLE_TIMEOUT_MS + 1000, now)).toBe(false)
    expect(isIdleExpired(now - IDLE_TIMEOUT_MS, now)).toBe(true)
    expect(isIdleExpired(now - IDLE_TIMEOUT_MS - 60_000, now)).toBe(true)
  })

  it('从来没有活动记录(0)不判定超时 —— 刚打开的页面不该被踢', () => {
    expect(isIdleExpired(0, Date.now())).toBe(false)
  })

  it('活动事件会刷新时间戳(节流内只记一次)', () => {
    startIdleTimer()
    window.dispatchEvent(new Event('mousemove'))
    expect(getLastActiveAt()).toBeGreaterThan(0)
  })
})

describe('idleTimer · 超时处理', () => {
  it('不重入:一次处理没结束前,再来一次不会再发登出', async () => {
    let release!: () => void
    logoutMock.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        release = resolve
      }),
    )

    const first = handleIdleTimeout()
    // 第一个还在 await logoutApi()(模拟网络往返)
    const second = handleIdleTimeout()

    expect(logoutMock).toHaveBeenCalledTimes(1)

    release()
    await first
    await second

    expect(logoutMock).toHaveBeenCalledTimes(1)
  })

  it('超时后本地凭证被清干净', async () => {
    storage.set(STORAGE_KEYS.TOKEN, 'A1')
    session.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    storage.set(STORAGE_KEYS.LAST_ACTIVE_AT, Date.now())

    await handleIdleTimeout()

    expect(storage.get(STORAGE_KEYS.TOKEN)).toBeNull()
    expect(session.get(STORAGE_KEYS.REFRESH_TOKEN)).toBeNull()
    expect(storage.get(STORAGE_KEYS.LAST_ACTIVE_AT)).toBeNull()
  })

  it('跳登录页带 redirect(和 401 路径行为一致,别把用户丢回首页)', () => {
    window.history.pushState({}, '', '/system/user?page=2')

    expect(buildIdleLoginUrl()).toBe('/login?reason=idle&redirect=%2Fsystem%2Fuser%3Fpage%3D2')
  })
})

describe('idleTimer · 轮询', () => {
  it('未登录时不触发超时处理', async () => {
    vi.useFakeTimers()
    startIdleTimer()
    storage.set(STORAGE_KEYS.LAST_ACTIVE_AT, Date.now() - IDLE_TIMEOUT_MS - 1000)

    await vi.advanceTimersByTimeAsync(20_000)

    expect(logoutMock).not.toHaveBeenCalled()
  })

  it('已登录且空闲超阈值 → 触发登出', async () => {
    vi.useFakeTimers()
    useUserStore.setState({ isLoggedIn: true })
    startIdleTimer()
    storage.set(STORAGE_KEYS.LAST_ACTIVE_AT, Date.now() - IDLE_TIMEOUT_MS - 1000)

    await vi.advanceTimersByTimeAsync(20_000)

    expect(logoutMock).toHaveBeenCalledTimes(1)
  })
})

describe('idleTimer · P1 回归(跨会话 / 跨浏览器重启)', () => {
  it('关掉浏览器几小时再打开,不会被立刻踢下线', () => {
    // 昨天 18:00 的会话:refresh token 还在(服务端让它活 30 天),
    // 空闲时间戳停在 4 小时前。旧实现里,启动后第一次轮询(15s)就会把
    // 刚恢复的会话踢掉 ⇒「记住我」的持久会话实际只值 30 分钟。
    storage.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    storage.set(STORAGE_KEYS.USER, { userInfo: USER, currentIdentity: null, isLoggedIn: true })
    storage.set(STORAGE_KEYS.LAST_ACTIVE_AT, Date.now() - 4 * 60 * 60 * 1000)

    hydrateUserStore()

    expect(useUserStore.getState().isLoggedIn).toBe(true)
    // 空闲判定不成立 —— 浏览器关掉的那段时间不算"用户没在用"
    expect(isIdleExpired(getLastActiveAt(), Date.now())).toBe(false)
  })
})
