/**
 * 续期失败后的跨标签页恢复链(2026-10-25 M8)
 *
 * 这一层是 M8 的**主战场**:续期失败到底该不该登出。
 * 旧行为是一律登出 → 用户开两个标签页就有一个被踢下线。
 * 新行为是先把"能不能从兄弟标签页找回"走完,全落空才登出。
 *
 * ⚠️ 这里的断言全是**行为断言**(会话还在不在 / token 换了没 / 发了几次请求),
 *   不是内部函数调用计数 —— 行为才是用户能感知的东西。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

vi.mock('@/api/auth', () => ({ refreshToken: vi.fn() }))
vi.mock('@/api/client', () => ({ handleUnauthorized: vi.fn() }))

import { refreshToken } from '@/api/auth'
import { handleUnauthorized } from '@/api/client'
import { useUserStore, getToken, getRefreshToken } from '@/stores/user'
import { activeBackend } from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'
import { doRefresh, scheduleTokenRefresh, cancelTokenRefresh, setTokenSyncForTest } from '@/utils/tokenScheduler'
import { TokenSync } from '@/utils/tokenSync'
import type { TokenChannel, TokenMessage, TokenPayload } from '@/utils/tokenSync'
import type { UserInfo } from '@/types/user'

const USER: UserInfo = {
  id: 'u1',
  username: 'admin',
  nickname: '管理员',
  phone: '138****0000',
  avatar: '',
  identities: [],
  current_identity_id: '',
}

const OTHER_USER: UserInfo = { ...USER, id: 'u2', username: 'other' }

/** 假的跨标签页总线(语义对齐真 BroadcastChannel:不回送给自己) */
class FakeBus {
  private peers: Array<{ deliver: (m: TokenMessage) => void }> = []

  create(): TokenChannel {
    const handlers = new Set<(m: TokenMessage) => void>()
    const peer = {
      deliver: (m: TokenMessage) => {
        for (const h of [...handlers]) h(m)
      },
    }
    this.peers.push(peer)
    return {
      post: (message) => {
        for (const p of [...this.peers]) if (p !== peer) p.deliver(message)
      },
      subscribe: (handler) => {
        handlers.add(handler)
        return () => {
          handlers.delete(handler)
        }
      },
      close: () => {
        this.peers = this.peers.filter((p) => p !== peer)
        handlers.clear()
      },
    }
  }
}

function newGroup(over: Partial<TokenPayload> = {}): TokenPayload {
  return {
    userId: 'u1',
    accessToken: 'A2',
    refreshToken: 'R2',
    expiresAt: Date.now() + 3600_000,
    expiresIn: 3600,
    refreshExpiresIn: 7200,
    ...over,
  }
}

/** 本标签页的同步器(装到 tokenScheduler 的单例里) */
function installLocalSync(bus: FakeBus): void {
  setTokenSyncForTest(
    new TokenSync(bus.create(), {
      getUserId: () => useUserStore.getState().userInfo?.id ?? null,
      getLocalRefreshToken: () => getRefreshToken(),
      getLocal: () => {
        const expiresAt = useUserStore.getState().getAccessTokenExpiresAt() ?? 0
        if (!expiresAt) return null
        return newGroup({
          accessToken: getToken() ?? '',
          refreshToken: getRefreshToken() ?? '',
          expiresAt,
        })
      },
      onTokens: () => {
        /* 本地这一侧不主动采用 —— 采用由恢复链自己做 */
      },
    }),
  )
}

/** 兄弟标签页:手里是更新的一组(R2) */
function installSibling(bus: FakeBus, group: TokenPayload = newGroup()): void {
  new TokenSync(bus.create(), {
    getUserId: () => group.userId,
    getLocalRefreshToken: () => group.refreshToken,
    getLocal: () => group,
    onTokens: () => {},
  })
}

/** 登录一个会话 */
function login(remember: boolean, user: UserInfo = USER): void {
  useUserStore.getState().setLogin({
    userInfo: user,
    accessToken: 'A1',
    refreshToken: 'R1',
    remember,
    expiresIn: 3600,
    refreshExpiresIn: 7200,
  })
}

beforeEach(() => {
  cancelTokenRefresh()
  setTokenSyncForTest(null)
  vi.mocked(refreshToken).mockReset()
  vi.mocked(handleUnauthorized).mockReset()
  useUserStore.getState().reset()
})

afterEach(() => {
  cancelTokenRefresh()
  setTokenSyncForTest(null)
  useUserStore.getState().reset()
})

describe('doRefresh · 正常路径', () => {
  it('续期成功 → 写盘新的一组,不登出', async () => {
    login(false)
    vi.mocked(refreshToken).mockResolvedValue({
      token: 'A2',
      refresh_token: 'R2',
      expires_in: 3600,
      refresh_expires_in: 7200,
      user: USER,
    })

    const ok = await doRefresh()

    expect(ok).toBe(true)
    expect(getToken()).toBe('A2')
    expect(getRefreshToken()).toBe('R2')
    expect(handleUnauthorized).not.toHaveBeenCalled()
  })
})

describe('doRefresh · 续期被拒(竞态)', () => {
  it('兄弟标签页有更新的一组 → 自愈,**不登出**', async () => {
    login(false)
    const bus = new FakeBus()
    installLocalSync(bus)
    installSibling(bus)

    vi.mocked(refreshToken).mockRejectedValue(new Error('refresh token 已失效,请重新登录'))

    const ok = await doRefresh()

    expect(ok).toBe(true)
    expect(handleUnauthorized).not.toHaveBeenCalled()
    // 会话被兄弟的那一组接管
    expect(getRefreshToken()).toBe('R2')
    expect(getToken()).toBe('A2')
  })

  it('兄弟标签页没有可用的组 → 才登出', async () => {
    login(false)
    const bus = new FakeBus()
    installLocalSync(bus)
    // 没有兄弟 → 求助超时

    vi.mocked(refreshToken).mockRejectedValue(new Error('refresh token 已失效,请重新登录'))

    const ok = await doRefresh()

    expect(ok).toBe(false)
    expect(handleUnauthorized).toHaveBeenCalledTimes(1)
  })

  it('没有通道(老浏览器)时 → 退回旧行为:登出', async () => {
    login(false)
    // 降级:没有通道
    setTokenSyncForTest(
      new TokenSync(null, {
        getUserId: () => 'u1',
        getLocalRefreshToken: () => getRefreshToken(),
        getLocal: () => null,
        onTokens: () => {},
      }),
    )

    vi.mocked(refreshToken).mockRejectedValue(new Error('refresh token 已失效,请重新登录'))

    const ok = await doRefresh()

    expect(ok).toBe(false)
    expect(handleUnauthorized).toHaveBeenCalledTimes(1)
  })

  it('共享存储里已被兄弟换新 → 自愈,且**不再发第二次请求**', async () => {
    // 勾了"记住我" → 存储是 localStorage,兄弟标签页写进来本标签页看得见
    login(true)
    vi.mocked(refreshToken).mockImplementation(async () => {
      // 模拟兄弟刚好刷新成功:新的一组已经落进同一个存储
      useUserStore.getState().setTokens({
        accessToken: 'A2',
        refreshToken: 'R2',
        expiresIn: 3600,
        refreshExpiresIn: 7200,
      })
      throw new Error('refresh token 已失效,请重新登录')
    })

    const ok = await doRefresh()

    expect(ok).toBe(true)
    expect(handleUnauthorized).not.toHaveBeenCalled()
    // ⚠️ 关键:不能再发一次 —— 那会把兄弟刚拿到的 refresh token 又消费掉
    expect(refreshToken).toHaveBeenCalledTimes(1)
    expect(getRefreshToken()).toBe('R2')
  })

  it('共享存储里换成了**别人**的会话 → 不捡(防串号),照常登出', async () => {
    login(true)
    /**
     * ⚠️ 时序很关键:兄弟标签页换人登录发生在**本次请求飞行途中**。
     *   如果在调 doRefresh 之前就把存储换掉,doRefresh 读到的"待用 token"
     *   就是新写进去的那个了 —— 于是"刚失败的那个 token"跟"存储里的那个"
     *   是同一个,adoptFromSharedStorage 在第一道闸(current === failedToken)
     *   就返回 false,测的根本不是防串号那一道。
     */
    vi.mocked(refreshToken).mockImplementation(async () => {
      // 另一个标签页登出后换了个账号登录:存储里的会话变成别人
      activeBackend().set(STORAGE_KEYS.USER, {
        userInfo: OTHER_USER,
        currentIdentity: null,
        isLoggedIn: true,
      })
      useUserStore.getState().setTokens({
        accessToken: 'A2',
        refreshToken: 'R2',
        expiresIn: 3600,
        refreshExpiresIn: 7200,
      })
      throw new Error('refresh token 已失效,请重新登录')
    })

    const ok = await doRefresh()

    expect(ok).toBe(false)
    expect(handleUnauthorized).toHaveBeenCalledTimes(1)
  })
})

describe('调度 · 别的标签页已经换过 token 时不重复发请求', () => {
  it('到点发现 token 换过了 → 直接重排,不发 refresh', async () => {
    vi.useFakeTimers()
    try {
      login(false)
      vi.mocked(refreshToken).mockResolvedValue({
        token: 'A3',
        refresh_token: 'R3',
        expires_in: 3600,
        refresh_expires_in: 7200,
        user: USER,
      })

      // 排一次 10 分钟后的刷新
      scheduleTokenRefresh(10 * 60 * 1000)

      // 兄弟标签页抢先换了 token
      useUserStore.getState().setTokens({
        accessToken: 'A2',
        refreshToken: 'R2',
        expiresIn: 3600,
        refreshExpiresIn: 7200,
      })

      // 走到点(delay = 10min - 5min 提前量)
      await vi.advanceTimersByTimeAsync(5 * 60 * 1000)

      expect(refreshToken).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })
})
