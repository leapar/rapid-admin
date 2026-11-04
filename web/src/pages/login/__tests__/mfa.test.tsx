/**
 * 登录页 · MFA 二次验证(M150)
 *
 * ## 这条网守的是什么
 *
 *   后端开启 MFA 的账号,密码登录的第一步不再直接发 token,而是返
 *   **20109(MFA_REQUIRED)+ 自动发一条 scene=mfa 的短信码**。前端要:
 *
 *     1. 把 20109 拦在软锁定计数**之前** —— 它不是失败(密码已验对),
 *        bumpFailCount 对它计数会把开了 MFA 的用户第一次登录就往
 *        "5 次软锁"推一步(5 次挑战 = 一次真锁,纯假账)。
 *     2. 切换到 MFA 面板,等用户填码。
 *     3. 复验提交 = **username + password + mfa_code 一起重发**(挑战无状态),
 *        且 captcha 字段为空 —— 首条图形码已在第一步被消费,后端对
 *        mfa_code 非空的调用跳过图形码校验(见 handler Login 注释)。
 *     4. 复验成功走与普通登录同一个 completeLogin(remember 语义保留)。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 onPasswordFinish catch 里的 `code === BizCode.MFA_REQUIRED` 分支删掉
 *     → 「20109 不进软锁定计数」变红(fail count 会被计 1)。
 *   · 复验提交里把 `captcha_id: ''` 改成带上旧 captcha → 「复验不带图形码」变红。
 *   · 复验提交漏掉 password → 「密码一起重发」变红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import Login from '../index'
import { login } from '@/api/auth'
import { preHashPassword } from '@/utils/crypto'

const { setLoginMock } = vi.hoisted(() => ({ setLoginMock: vi.fn() }))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  loginBySms: vi.fn(),
  sendSmsCode: vi.fn(),
  verifySmsCode: vi.fn(),
  sendMfaLoginCode: vi.fn(),
  getCaptcha: vi.fn(),
  getPublicConfig: vi.fn(),
}))

vi.mock('@/stores/user', () => ({
  useUserStore: (selector: (s: { setLogin: unknown }) => unknown) =>
    selector({ setLogin: setLoginMock }),
}))

// 图形验证码开关按"关"处理 —— 本用例只关心 MFA 链路,不渲染验证码框,
// 提交才不会被「请先输入图形验证码」拦住
vi.mock('@/stores/publicConfig', () => ({
  usePublicConfigStore: (selector: (s: Record<string, unknown>) => unknown) =>
    selector({
      isLoginCaptchaEnabled: () => false,
      isSmsCaptchaEnabled: () => false,
      appName: '',
    }),
}))

const loginMock = vi.mocked(login)

const WAIT = { timeout: 10_000 }
const TIMEOUT = 30_000
const USERNAME = 'zhangsan'
const PASSWORD = 'Pwd@1234567'

const LOGIN_OK = {
  token: 'tk',
  refresh_token: 'rt',
  expires_in: 3600,
  refresh_expires_in: 86400,
  user: {},
} as never

/** 后端返 MFA 挑战的形状:HTTP 层 reject,err.code = 20109 */
const MFA_CHALLENGE = { code: 20109, message: '该账号已开启二次验证,请输入短信验证码' }

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
})

function renderLogin() {
  return render(
    <App>
      <MemoryRouter initialEntries={['/login']}>
        <Login />
      </MemoryRouter>
    </App>,
  )
}

/** 在密码 tab 填账号密码并提交 */
async function submitPassword() {
  fireEvent.change(screen.getByPlaceholderText('用户名 / 工号'), {
    target: { value: USERNAME },
  })
  fireEvent.change(screen.getByPlaceholderText('密码'), {
    target: { value: PASSWORD },
  })
  fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))
}

describe('登录页 · MFA 二次验证(M150)', () => {
  it('20109 → 切到 MFA 面板,且**不进软锁定计数**', async () => {
    loginMock.mockRejectedValueOnce(MFA_CHALLENGE)
    renderLogin()
    await submitPassword()

    // MFA 面板出现
    await waitFor(() => expect(screen.getByText('二次验证')).toBeInTheDocument(), WAIT)
    // 关键断言:失败计数没动(20109 不是失败)
    expect(localStorage.getItem('login_fail_count')).toBeNull()
    // 首条码服务端已自动发出,重发按钮在冷却中
    expect(screen.getByText(/60s 后可重发/)).toBeInTheDocument()
  }, TIMEOUT)

  it('复验提交 = username + password + mfa_code 一起重发,不带图形码;成功即登录', async () => {
    loginMock.mockRejectedValueOnce(MFA_CHALLENGE).mockResolvedValueOnce(LOGIN_OK)
    renderLogin()
    await submitPassword()
    await waitFor(() => expect(screen.getByText('二次验证')).toBeInTheDocument(), WAIT)

    fireEvent.change(screen.getByPlaceholderText('6 位短信验证码'), {
      target: { value: '654321' },
    })
    fireEvent.click(screen.getByRole('button', { name: /完成登录/ }))

    await waitFor(() => expect(loginMock.mock.calls).toHaveLength(2), WAIT)
    const submitted = loginMock.mock.calls[1]![0]!
    expect(submitted.username).toBe(USERNAME)
    // 密码是 SHA-256 摘要后的 hex(与后端 bcrypt 比对口径一致)
    expect(submitted.password).toBe(await preHashPassword(PASSWORD))
    expect(submitted.mfa_code).toBe('654321')
    // 首条图形码已被第一步消费 —— 复验不能也不需要再带
    expect(submitted.captcha_id).toBe('')
    expect(submitted.captcha_code).toBe('')

    // 成功走 completeLogin:token + remember 语义保留
    expect(setLoginMock).toHaveBeenCalledWith(
      expect.objectContaining({
        accessToken: 'tk',
        refreshToken: 'rt',
        remember: true,
      }),
    )
  }, TIMEOUT)

  it('复验带错码 = 真失败,照常计数', async () => {
    loginMock
      .mockRejectedValueOnce(MFA_CHALLENGE)
      .mockRejectedValueOnce({ code: 20105, message: '短信验证码错误或已过期' })
    renderLogin()
    await submitPassword()
    await waitFor(() => expect(screen.getByText('二次验证')).toBeInTheDocument(), WAIT)

    fireEvent.change(screen.getByPlaceholderText('6 位短信验证码'), {
      target: { value: '000000' },
    })
    fireEvent.click(screen.getByRole('button', { name: /完成登录/ }))

    await waitFor(() => expect(loginMock.mock.calls).toHaveLength(2), WAIT)
    await waitFor(
      () => expect(localStorage.getItem('login_fail_count')).toBe('1'),
      WAIT,
    )
  }, TIMEOUT)
})
