/**
 * PermissionLoader —— 启动时加载用户 / 权限(2026-10-27 M102)
 *
 * ## 这条网守的是什么
 *
 *   `PermissionLoader` 是**整个应用启动的第一道门**:它校验 token、拉 `/auth/me`、
 *   拉权限、写 store。写坏了的表现是"一进去就白屏"或"一进去就掉登录页"。
 *
 *   M102 之前它的 `catch {}` 是这样的:
 *
 *     ```ts
 *     } catch (e) {
 *       reset()                       // ← 任何失败都当"登录态失效"
 *       usePermissionStore.getState().reset()
 *     }
 *     ```
 *
 *   两个问题:
 *     ① **网络闪断 / 后端 500 → 清 token + 跳登录页**,用户得重新输密码。
 *        一次瞬时抖动不该把人踢下线。
 *     ② 错误被 `catch {}` **整个吞掉**,日志里一个字都没有 —— 事后查无可查。
 *
 *   修法:按错误性质分开
 *     · **会话失效(401 / 40100 / 40101 / 40102)** → 立刻清登录态,**不重试**;
 *     · **其他错误(网络 / 5xx)** → 退避 800ms 后**重试一次**,还失败才清登录态;
 *     · 两条路径都要 `console.error` 留痕。
 *
 * ## 用例里那些"为什么"
 *
 *   · 断言 `getCurrentUser` 的**调用次数**而不是"最终有没有登出":
 *     「401 不该重试」和「500 该重试一次」都是次数语义。
 *   · 断言 `console.error` 被调用:老代码是 `catch {}`,一个日志都没有。
 *   · 重试分支自带 800ms 延迟,所以 `waitFor` 的超时给到 5s。
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

vi.mock('@/api/auth', () => ({ getCurrentUser: vi.fn() }))
vi.mock('@/api/role', () => ({ resourceApi: { accessible: vi.fn() } }))

import { getCurrentUser } from '@/api/auth'
import { resourceApi } from '@/api/role'
import { PermissionLoader } from '@/router/guards'
import { useUserStore, getToken, hydrateUserStore } from '@/stores/user'
import { usePermissionStore } from '@/stores/permission'
import { clearSessionFrom, session } from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'
import { BizCode } from '@/types/api'

const meMock = vi.mocked(getCurrentUser)
const accessibleMock = vi.mocked(resourceApi.accessible)

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
      permissions: ['user:list'],
      valid_from: '',
      valid_to: '',
      is_active: true,
      is_primary: true,
    },
  ],
}

/** 造一个"会话还在"的环境 */
function seedSession(): void {
  session.set(STORAGE_KEYS.TOKEN, 'ACCESS')
  session.set(STORAGE_KEYS.REFRESH_TOKEN, 'REFRESH')
}

/**
 * 造一个**真实启动**的环境:会话 + 落盘的 USER blob,然后 hydrate。
 *
 * ⚠️ 只写 token 不 hydrate 是不够的 —— 真实启动时 `main.tsx` 会先调
 *   `hydrateUserStore()`,把内存态恢复成"已登录"。不模拟这一步的话,
 *   "403 之后 isLoggedIn 还是不是 true"这条断言恒为 false(它在测的是
 *   beforeEach 里那个手动 setState,不是被测代码),等于没测。
 */
function seedRestoredSession(): void {
  seedSession()
  session.set(STORAGE_KEYS.USER, {
    userInfo: USER,
    currentIdentity: USER.identities[0],
    isLoggedIn: true,
  })
  hydrateUserStore()
}

let errorSpy: ReturnType<typeof vi.spyOn>

/**
 * 只挑 loader 自己打的错误日志。
 *
 * antd / React Router 自己也会往 console.error 上写警告(比如 React Router
 * 的 future flag 提示),把整个 console.error 都断言成"没调用"会被框架噪声
 * 绊倒。真正要盯的是 `[PermissionLoader]` 这一条。
 */
function loaderErrors(): unknown[][] {
  return errorSpy.mock.calls.filter((c) => String(c[0]).includes('[PermissionLoader]'))
}

beforeEach(() => {
  clearSessionFrom()
  sessionStorage.clear()
  localStorage.clear()
  seedSession()
  useUserStore.setState({ userInfo: null, currentIdentity: null, isLoggedIn: false })
  usePermissionStore.setState({ menus: [], permissionCodes: new Set(), roleCodes: [] })
  meMock.mockReset()
  accessibleMock.mockReset()
  accessibleMock.mockResolvedValue([])
  errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
})

describe('router/guards · PermissionLoader 失败语义(M102)', () => {
  it('会话失效(HTTP 401)→ 立刻清登录态,**不重试**', async () => {
    meMock.mockRejectedValue({ code: 401, message: 'unauthorized' })

    render(
      <PermissionLoader>
        <span>应用内容</span>
      </PermissionLoader>,
    )

    await waitFor(() => expect(getToken()).toBeNull())
    expect(meMock).toHaveBeenCalledTimes(1)
    // 错误要留痕(老代码是 catch {},日志里什么都没有)
    expect(loaderErrors().length).toBeGreaterThan(0)
  })

  it('会话失效(业务码 40100 / 40101 / 40102)→ 同样不重试', async () => {
    for (const code of [BizCode.UNAUTHORIZED, BizCode.TOKEN_EXPIRED, BizCode.TOKEN_INVALID]) {
      meMock.mockReset()
      errorSpy.mockClear()
      meMock.mockRejectedValue({ code, message: 'x' })

      const { unmount } = render(
        <PermissionLoader>
          <span>应用内容</span>
        </PermissionLoader>,
      )

      await waitFor(() => expect(getToken()).toBeNull())
      expect(meMock, `业务码 ${code} 不该重试`).toHaveBeenCalledTimes(1)
      unmount()
      // 下一轮重新种一个会话
      seedSession()
    }
  })

  it('权限不足(HTTP 403)→ **保持登录态**,不重试、不清 token(M146b)', async () => {
    seedRestoredSession()
    meMock.mockRejectedValue({ code: 403, message: 'forbidden' })

    render(
      <PermissionLoader>
        <span>应用内容</span>
      </PermissionLoader>,
    )

    // 应用照常渲染(没有被踢回登录页)
    await waitFor(() => expect(screen.getByText('应用内容')).toBeInTheDocument())
    // ⚠️ **会话没被清** —— 403 是"这个身份没权限",不是"这个会话没了"。
    //   历史上两者混在一起,造成了「右上角切身份 → 切完就退出」。
    expect(getToken()).toBe('ACCESS')
    expect(useUserStore.getState().isLoggedIn).toBe(true)
    // 403 是确定性的,重试一百次结果一样 ⇒ 不进重试分支
    expect(meMock).toHaveBeenCalledTimes(1)
    // 留痕(不能像老代码那样 catch {} 整个吞掉)
    expect(loaderErrors().length).toBeGreaterThan(0)
  })

  it('权限不足(业务码 40300)→ 同样保持登录态', async () => {
    seedRestoredSession()
    meMock.mockRejectedValue({ code: BizCode.FORBIDDEN, message: 'forbidden' })

    render(
      <PermissionLoader>
        <span>应用内容</span>
      </PermissionLoader>,
    )

    await waitFor(() => expect(screen.getByText('应用内容')).toBeInTheDocument())
    expect(getToken()).toBe('ACCESS')
    expect(useUserStore.getState().isLoggedIn).toBe(true)
    expect(meMock).toHaveBeenCalledTimes(1)
  })

  it('网络 / 5xx → 退避后**重试一次**,重试成功就继续用,不踢登录页', async () => {
    // 第一次失败,第二次成功
    meMock.mockRejectedValueOnce({ code: BizCode.SERVER_ERROR, message: 'boom' })
    meMock.mockResolvedValue({ user: USER as never, resources: [] })

    render(
      <PermissionLoader>
        <span>应用内容</span>
      </PermissionLoader>,
    )

    // 重试成功后应用内容渲染出来
    await waitFor(() => expect(screen.getByText('应用内容')).toBeInTheDocument(), { timeout: 5000 })
    // 会话**没被清** —— 一次瞬时抖动不该把人踢回登录页
    expect(getToken()).toBe('ACCESS')
    expect(useUserStore.getState().isLoggedIn).toBe(true)
    // /auth/me 至少打了两次(失败 + 重试);fetchAccessible 内部还会再打一次
    expect(meMock.mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  it('网络 / 5xx 重试一次还是失败 → 才清登录态(共 2 次尝试)', async () => {
    meMock.mockRejectedValue({ code: BizCode.SERVER_ERROR, message: 'boom' })

    render(
      <PermissionLoader>
        <span>应用内容</span>
      </PermissionLoader>,
    )

    await waitFor(() => expect(getToken()).toBeNull(), { timeout: 5000 })
    // 首次 + 一次重试 = 2 次,不再多了
    expect(meMock).toHaveBeenCalledTimes(2)
    expect(loaderErrors().length).toBeGreaterThan(0)
  })

  it('没有 token 时什么都不做(不请求、不报错)', async () => {
    clearSessionFrom()

    render(
      <PermissionLoader>
        <span>应用内容</span>
      </PermissionLoader>,
    )

    await waitFor(() => expect(screen.getByText('应用内容')).toBeInTheDocument())
    expect(meMock).not.toHaveBeenCalled()
    expect(loaderErrors()).toHaveLength(0)
  })

  it('启动加载期间要显示加载文案(antd 的 tip 在默认模式下**不渲染**)', async () => {
    // /auth/me 永不返回 → loader 一直停在 loading 态
    meMock.mockImplementation(() => new Promise(() => {}))

    render(
      <PermissionLoader>
        <span>应用内容</span>
      </PermissionLoader>,
    )

    // ⚠️ 这里原来写的是 `<Spin size="large" tip={t('common.loading')} />` ——
    // antd 5 的 `tip` **只在 nest / fullscreen 模式下才渲染成文案**,
    // 默认模式下它是死代码:"加载中..."从来没显示过,取而代之的是每次启动
    // 都打一条 antd 警告。改成自己渲染文案,这条用例把它钉住。
    expect(await screen.findByText('加载中...')).toBeInTheDocument()
  })
})
