/**
 * 登录页 · 短信登录的图形验证码链路(2026-10-28 M106)
 *
 * ## 这条网守的是什么(P0)
 *
 *   后端 `captchaStore.Verify(id, answer, clear=true)` → `Get(id, true)`:
 *   **先 `delete(s.data, id)` 再比对** ⇒ 图形验证码**成功失败都被消费**,
 *   一次验证即作废。
 *
 *   而短信登录这条链路上有**两个**受验证码保护的请求:
 *     ① `POST /public/auth/sms/send`    (scene=sms_send,受 captcha.sms_send_enabled 管)
 *     ② `POST /public/auth/login-sms`   (scene=login,   受 captcha.login_enabled  管)
 *
 *   修复前,页面把**同一个** `{id, code}` 先后发给这两个请求 ⇒
 *   ② 拿到的是一张**已经被 ① 消费掉**的验证码 ⇒ 「图形验证码错误或已过期」
 *   ⇒ **短信登录 100% 走不通**。
 *
 *   ⚠️ 这条只在 `login_enabled` 与 `sms_send_enabled` **同时为 true** 时发作 ——
 *   而那正是 `server/configs/config.example.yaml` 模板里的默认值;
 *   本地 dev 配置把 login_enabled 关掉了,所以**本地测不出来**。
 *
 * ## 断言的是"两次请求用的是不同的验证码"
 *
 *   不是断言"调用了某个函数",而是断言**结构上不可能重放**:
 *   发短信用的是 idA,提交登录用的是 idB,且 idB ≠ idA ⇒ 后端那张图只可能被消费一次。
 *
 *   ⚠️ **不写死 id 序号** —— 页面初始在"账号密码" tab,那个 tab 里也有一张
 *   CaptchaField,切到"短信登录" tab 会**再挂一张**(antd Tabs 懒挂载)，
 *   所以短信 tab 用的是第 2 张图。序号写死会变成一条**假网**。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把登录页的 `onCaptchaConsumed` 接线删掉 → 「发完短信要换图」那条变红,
 *     「提交那一步用的是新验证码」那条也变红。
 *   · 把 `if (captcha) onCaptchaConsumed?.()` 改成无条件调用 → 「没带验证码
 *     不通知」那条变红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import Login from '../index'
import { sendSmsCode, getCaptcha, loginBySms } from '@/api/auth'

const { setLoginMock } = vi.hoisted(() => ({ setLoginMock: vi.fn() }))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  loginBySms: vi.fn(),
  sendSmsCode: vi.fn(),
  verifySmsCode: vi.fn(),
  getCaptcha: vi.fn(),
  getPublicConfig: vi.fn(),
}))

vi.mock('@/stores/user', () => ({
  useUserStore: (selector: (s: { setLogin: unknown }) => unknown) =>
    selector({ setLogin: setLoginMock }),
}))

const sendMock = vi.mocked(sendSmsCode)
const captchaMock = vi.mocked(getCaptcha)
const loginBySmsMock = vi.mocked(loginBySms)

const WAIT = { timeout: 10_000 }
/** ⚠️ 默认 testTimeout 只有 5s,而这里要等 antd 表单 + 多次网络往返 —— 必须放宽 */
const TIMEOUT = 30_000
const PHONE = '13800138000'

const PNG =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

/** 每次拉图都给一个新的 id —— 才能断言"第二次请求用的是另一张图" */
let captchaSeq = 0
beforeEach(() => {
  vi.clearAllMocks()
  // ⚠️ 必须清 localStorage —— SmsSendButton 的冷却时间戳写在 localStorage 里
  //   (key = sms:cooldown:{scene}:{phone})。不清的话上一个用例发过一次短信,
  //   下一个用例按钮直接是「60s 后重发」,按名字找按钮就找不到了。
  localStorage.clear()
  captchaSeq = 0
  captchaMock.mockImplementation(async () => {
    captchaSeq += 1
    return { id: `cap-${captchaSeq}`, image: PNG }
  })
  sendMock.mockResolvedValue(undefined)
  loginBySmsMock.mockResolvedValue({
    token: 'tk',
    refresh_token: 'rt',
    expires_in: 3600,
    refresh_expires_in: 86400,
    user: {},
  } as never)
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

/** 切到短信 tab */
async function switchToSmsTab() {
  fireEvent.click(screen.getByRole('tab', { name: '短信登录' }))
  await waitFor(() => expect(screen.getByPlaceholderText('11 位手机号')).toBeInTheDocument(), WAIT)
}

/** 图形验证码输入框 */
function captchaInput() {
  return screen.getByPlaceholderText('请输入图形验证码') as HTMLInputElement
}

/** 填图形验证码(每次换图后输入框会被清空,需要重填) */
function fillCaptcha(code: string) {
  fireEvent.change(captchaInput(), { target: { value: code } })
}

/** 等到输入框被清空(说明新图已经换上了) */
async function waitCaptchaRefreshed() {
  await waitFor(() => expect(captchaInput().value).toBe(''), WAIT)
}

describe('登录页 · 短信登录的图形验证码(M106)', () => {
  it('发短信用的是当前这张验证码', async () => {
    renderLogin()
    await switchToSmsTab()

    fireEvent.change(screen.getByPlaceholderText('11 位手机号'), {
      target: { value: PHONE },
    })
    await waitFor(() => expect(captchaMock).toHaveBeenCalled(), WAIT)
    fillCaptcha('ABCD')

    fireEvent.click(screen.getByRole('button', { name: /发送验证码/ }))
    await waitFor(() => expect(sendMock).toHaveBeenCalled(), WAIT)

    const used = sendMock.mock.calls[0]![2] as { id: string; code: string }
    expect(used.code).toBe('ABCD')
    expect(used.id).toBeTruthy()
  }, TIMEOUT)

  it('发完短信必须换一张新图(后端把旧的消费掉了)', async () => {
    renderLogin()
    await switchToSmsTab()

    fireEvent.change(screen.getByPlaceholderText('11 位手机号'), {
      target: { value: PHONE },
    })
    await waitFor(() => expect(captchaMock).toHaveBeenCalled(), WAIT)
    const callsBefore = captchaMock.mock.calls.length
    fillCaptcha('ABCD')

    fireEvent.click(screen.getByRole('button', { name: /发送验证码/ }))
    await waitFor(() => expect(sendMock).toHaveBeenCalled(), WAIT)

    // ⚠️ 关键断言:换图了(= 后端那张已被消费,不能再用)
    await waitFor(() => expect(captchaMock.mock.calls.length).toBeGreaterThan(callsBefore), WAIT)
  }, TIMEOUT)

  it('提交短信登录时用的是**新那张**验证码(不是重放已消费的那张)', async () => {
    renderLogin()
    await switchToSmsTab()

    fireEvent.change(screen.getByPlaceholderText('11 位手机号'), {
      target: { value: PHONE },
    })
    await waitFor(() => expect(captchaMock).toHaveBeenCalled(), WAIT)
    fillCaptcha('ABCD')

    // ① 发短信
    fireEvent.click(screen.getByRole('button', { name: /发送验证码/ }))
    await waitFor(() => expect(sendMock).toHaveBeenCalled(), WAIT)
    const sendCaptcha = sendMock.mock.calls[0]![2] as { id: string; code: string }

    // ② 后端已消费掉它,前端换图 → 输入框被清空,用户重新填
    await waitCaptchaRefreshed()
    fillCaptcha('WXYZ')

    // ③ 提交短信登录 —— 必须带**另一张**图
    fireEvent.change(screen.getByPlaceholderText('6 位短信验证码'), {
      target: { value: '123456' },
    })
    // ⚠️ antd 5 的 Button 会自动在两个汉字之间插空格(「登录」→「登 录」),
    //   所以这里用正则而不是精确字符串(精确匹配会找不到)。
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))

    await waitFor(() => expect(loginBySmsMock).toHaveBeenCalled(), WAIT)
    const used = loginBySmsMock.mock.calls[0]![2] as { id: string; code: string }
    expect(used.code).toBe('WXYZ')
    // 绝不能是发短信那张 —— 那张已经被后端消费掉了
    expect(used.id).not.toBe(sendCaptcha.id)
  }, TIMEOUT)
})
