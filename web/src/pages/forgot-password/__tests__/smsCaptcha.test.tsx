/**
 * 找回密码页 · 图形验证码链路(2026-10-28 M106)
 *
 * ## 这条网守的是什么(P0)
 *
 *   同登录页:后端图形验证码**成功失败都被消费**
 *   (`captchaStore.Verify` → `Get(id, true)`,先删后比)。
 *
 *   找回密码这条链路上有**两个**受验证码保护的请求:
 *     ① `POST /public/auth/sms/send`           (scene=sms_send)
 *     ② `POST /public/auth/reset-password-sms` (scene=login)
 *
 *   修复前,页面把**同一个** `{id, code}` 先后发给这两个请求 ⇒
 *   ② 拿的是一张**已被 ① 消费掉**的验证码 ⇒ 「图形验证码错误或已过期」
 *   ⇒ **短信重置密码 100% 走不通**。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把本页的 `onCaptchaConsumed` 接线删掉 → 「发完短信要换图」那条立刻变红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import ForgotPassword from '../index'
import { sendSmsCode, getCaptcha } from '@/api/auth'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  loginBySms: vi.fn(),
  resetPasswordBySms: vi.fn(),
  sendSmsCode: vi.fn(),
  verifySmsCode: vi.fn(),
  getCaptcha: vi.fn(),
  getPublicConfig: vi.fn(),
}))

const sendMock = vi.mocked(sendSmsCode)
const captchaMock = vi.mocked(getCaptcha)

const WAIT = { timeout: 10_000 }
/** ⚠️ 默认 testTimeout 只有 5s,这里要等 antd 表单 + 多次往返 —— 必须放宽 */
const TIMEOUT = 30_000
const PHONE = '13800138000'

const PNG =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

let captchaSeq = 0
beforeEach(() => {
  vi.clearAllMocks()
  // ⚠️ 必须清 localStorage —— 冷却时间戳落在里面,不清的话按钮直接是「60s 后重发」
  localStorage.clear()
  captchaSeq = 0
  captchaMock.mockImplementation(async () => {
    captchaSeq += 1
    return { id: `cap-${captchaSeq}`, image: PNG }
  })
  sendMock.mockResolvedValue(undefined)
})

function renderPage() {
  return render(
    <App>
      <MemoryRouter initialEntries={['/forgot-password']}>
        <ForgotPassword />
      </MemoryRouter>
    </App>,
  )
}

describe('找回密码页 · 图形验证码(M106)', () => {
  it('发短信用当前这张验证码,发完必须换一张新图', async () => {
    renderPage()

    fireEvent.change(screen.getByPlaceholderText('请输入注册时使用的手机号'), {
      target: { value: PHONE },
    })
    await waitFor(() => expect(captchaMock).toHaveBeenCalled(), WAIT)
    const callsBefore = captchaMock.mock.calls.length

    const captchaInput = screen.getByPlaceholderText('请输入图形验证码') as HTMLInputElement
    fireEvent.change(captchaInput, { target: { value: 'ABCD' } })

    fireEvent.click(screen.getByRole('button', { name: /发送验证码/ }))
    await waitFor(() => expect(sendMock).toHaveBeenCalled(), WAIT)

    const used = sendMock.mock.calls[0]![2] as { id: string; code: string }
    expect(used.code).toBe('ABCD')
    expect(used.id).toBeTruthy()

    // ⚠️ 关键断言:换图了(= 后端那张已被消费,不能再用)
    await waitFor(() => expect(captchaMock.mock.calls.length).toBeGreaterThan(callsBefore), WAIT)
  }, TIMEOUT)
})
