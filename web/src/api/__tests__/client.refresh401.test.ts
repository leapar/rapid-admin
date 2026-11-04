/**
 * 401 拦截器与续期的分工(2026-10-25 M8)
 *
 * M8 把"续期失败要不要登出"的决定权从**拦截器**挪到了 **doRefresh**:
 * 拦截器只负责把 401 变成 reject,登不登出由 doRefresh 走完恢复链再定。
 *
 * 这条分工如果不成立,M8 的恢复链就是死代码 —— 拦截器会在 doRefresh
 * 有机会恢复之前就把会话清掉。所以这里专门锁住它。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { AxiosError, AxiosHeaders } from 'axios'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'

vi.mock('@/api/auth', () => ({ refreshToken: vi.fn() }))

import instance from '@/api/client'
import { refreshToken } from '@/api/auth'
import { useUserStore, getToken } from '@/stores/user'
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

/**
 * 让所有请求都返 401
 *
 * ⚠️ **自定义 adapter 必须自己 reject** —— axios 的 settle()(validateStatus
 *   判定 + 抛 AxiosError)是**内建 adapter 内部**做的事;自定义 adapter
 *   直接 resolve 一个 401 响应,axios 会当成成功响应交给"成功拦截器",
 *   于是 401 分支根本没被走到(实测:响应被当成 code=40102 的业务错误处理)。
 *   所以这里照内建 adapter 的做法:抛一个带 response 的 AxiosError。
 */
function mockAdapterStatus(status: number, code: number) {
  const adapter: AxiosAdapter = async (config: InternalAxiosRequestConfig) => {
    const response = {
      status,
      statusText: 'Unauthorized',
      data: { code, message: 'refresh token 已失效,请重新登录', request_id: 'r-1' },
      headers: new AxiosHeaders(),
      config,
    }
    throw new AxiosError(
      `Request failed with status code ${status}`,
      AxiosError.ERR_BAD_RESPONSE,
      config,
      undefined,
      response,
    )
  }
  instance.defaults.adapter = adapter
}

beforeEach(() => {
  useUserStore.getState().reset()
  vi.mocked(refreshToken).mockReset()
})

afterEach(() => {
  useUserStore.getState().reset()
  // 还原 axios 默认适配器(undefined = 用内建的那套)
  instance.defaults.adapter = undefined
})

describe('401 拦截器 · refresh 请求自身 401', () => {
  it('**不清会话** —— 登不登出由 doRefresh 走完恢复链再定', async () => {
    useUserStore.getState().setLogin({
      userInfo: USER,
      accessToken: 'A1',
      refreshToken: 'R1',
      remember: false,
      expiresIn: 3600,
      refreshExpiresIn: 7200,
    })
    mockAdapterStatus(401, 40102)

    await expect(
      instance.post('/public/auth/refresh', { refresh_token: 'R1' }),
    ).rejects.toBeTruthy()

    // ⚠️ 旧行为:这里会 handleUnauthorized() → 会话被清 → 用户被踢回登录页
    expect(useUserStore.getState().isLoggedIn).toBe(true)
    expect(getToken()).toBe('A1')
  })
})

describe('401 拦截器 · 普通请求 401', () => {
  it('续期也失败且恢复链落空 → 照常登出(会话被清)', async () => {
    useUserStore.getState().setLogin({
      userInfo: USER,
      accessToken: 'A1',
      refreshToken: 'R1',
      remember: false,
      expiresIn: 3600,
      refreshExpiresIn: 7200,
    })
    mockAdapterStatus(401, 40102)
    vi.mocked(refreshToken).mockRejectedValue(new Error('refresh token 已失效'))

    await expect(instance.get('/rapid-admin/v1/users')).rejects.toBeTruthy()

    expect(useUserStore.getState().isLoggedIn).toBe(false)
  })
})
