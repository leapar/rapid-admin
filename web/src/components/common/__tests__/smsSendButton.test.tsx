/**
 * SmsSendButton 行为回归(2026-10-28 M104)
 *
 * ## 这条网守的是什么
 *
 *   1. **scene 必须传下去** —— 2026-10-28 之前这个 prop 叫 `bizType`、
 *      类型是 `string`、而且**根本没传进 `sendSmsCode`**。发出去的请求里
 *      scene 是 undefined,后端 `validScene()` 返 400「未知 scene」,
 *      **短信一条都没发出去过**。
 *   2. **冷却必须按 scene 分桶** —— 登录页和找回密码页用的是同一个手机号。
 *      不分桶的话,在登录页点一次发送,找回密码页就进入冷却(反之亦然),
 *      用户明明想收另一条短信却被告知「N s 后重发」。
 *   3. **提示语里是场景的人话,不是内部标识** —— 也顺带守住了
 *      「模板占位符变量名与字典不一致」这一类(占位符替换不上时,
 *      界面上会出现字面量「验证码已发送({scene})」)。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { message } from 'antd'
import { SmsSendButton } from '@/components/common/SmsSendButton'
import { sendSmsCode } from '@/api/auth'
import { SmsScene } from '@/constants/sms'

vi.mock('@/api/auth', () => ({
  sendSmsCode: vi.fn(),
}))

const sendMock = vi.mocked(sendSmsCode)

const PHONE = '13800138000'

/** localStorage 里 cooldown 的真实 key(前缀由 utils/storage 拼) */
const cooldownKey = (scene: string, phone: string) =>
  `rapid-admin:sms:cooldown:${scene}:${phone}`

function sendButton() {
  return screen.getByRole('button', { name: /发送验证码/ })
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  sendMock.mockResolvedValue(undefined)
  // antd 的 message 是全局单例,toast 会跨用例留在 DOM 里(默认 3s 才消失)。
  // 不清掉的话,上一个用例的提示语会把下一个用例的断言打成"找到多个"。
  message.destroy()
})

describe('SmsSendButton · scene 传下去', () => {
  it('发送时带上 scene(以前是 undefined → 后端 400「未知 scene」)', async () => {
    render(<SmsSendButton phone={PHONE} scene={SmsScene.LOGIN} />)
    fireEvent.click(sendButton())

    await waitFor(() => expect(sendMock).toHaveBeenCalled())
    expect(sendMock.mock.calls[0]![0]).toBe(PHONE)
    expect(sendMock.mock.calls[0]![1]).toBe('login')
    // 没有图形验证码时第三参是 undefined(不是空串,后端按"跳过校验"处理)
    expect(sendMock.mock.calls[0]![2]).toBeUndefined()
  })

  it('换个场景就是换个 scene(不是同一个 prop 传两遍)', async () => {
    render(<SmsSendButton phone={PHONE} scene={SmsScene.RESET_PASSWORD} />)
    fireEvent.click(sendButton())

    await waitFor(() => expect(sendMock).toHaveBeenCalled())
    expect(sendMock.mock.calls[0]![1]).toBe('reset_password')
  })

  it('图形验证码透传给后端', async () => {
    render(
      <SmsSendButton
        phone={PHONE}
        scene={SmsScene.LOGIN}
        beforeSend={async () => ({ ok: true, captcha: { id: 'cap-1', code: 'ABCD' } })}
      />,
    )
    fireEvent.click(sendButton())

    await waitFor(() => expect(sendMock).toHaveBeenCalled())
    expect(sendMock.mock.calls[0]![2]).toEqual({ id: 'cap-1', code: 'ABCD' })
  })

  it('beforeSend 拒绝 → 不发短信', async () => {
    render(
      <SmsSendButton
        phone={PHONE}
        scene={SmsScene.LOGIN}
        beforeSend={async () => ({ ok: false, error: '图形验证码错误' })}
      />,
    )
    fireEvent.click(sendButton())

    await waitFor(() => expect(screen.getByText(/图形验证码错误/)).toBeInTheDocument())
    expect(sendMock).not.toHaveBeenCalled()
  })
})

describe('SmsSendButton · 手机号校验', () => {
  it('手机号为空 → 不发', async () => {
    render(<SmsSendButton phone="" scene={SmsScene.LOGIN} />)
    fireEvent.click(sendButton())

    await waitFor(() => expect(screen.getByText(/请输入手机号/)).toBeInTheDocument())
    expect(sendMock).not.toHaveBeenCalled()
  })

  it('手机号格式不对 → 不发', async () => {
    render(<SmsSendButton phone="138abc" scene={SmsScene.LOGIN} />)
    fireEvent.click(sendButton())

    await waitFor(() => expect(screen.getByText(/11 位手机号/)).toBeInTheDocument())
    expect(sendMock).not.toHaveBeenCalled()
  })
})

describe('SmsSendButton · 冷却按 scene 分桶', () => {
  it('冷却记录写进 localStorage,key 里带 scene', async () => {
    render(<SmsSendButton phone={PHONE} scene={SmsScene.LOGIN} />)
    fireEvent.click(sendButton())

    await waitFor(() => expect(sendMock).toHaveBeenCalled())
    expect(localStorage.getItem(cooldownKey('login', PHONE))).toBeTruthy()
    // 别的场景不该被牵连
    expect(localStorage.getItem(cooldownKey('reset_password', PHONE))).toBeNull()
  })

  it('冷却期内按钮禁用并显示剩余秒数', async () => {
    render(<SmsSendButton phone={PHONE} scene={SmsScene.LOGIN} />)
    fireEvent.click(sendButton())

    await waitFor(() => expect(screen.getByRole('button', { name: /60s 后重发/ })).toBeDisabled())
  })

  it('换个场景 → 冷却互不干扰(登录页发过了,找回密码页照样能发)', async () => {
    render(
      <>
        <div data-testid="login-scene">
          <SmsSendButton phone={PHONE} scene={SmsScene.LOGIN} />
        </div>
        <div data-testid="reset-scene">
          <SmsSendButton phone={PHONE} scene={SmsScene.RESET_PASSWORD} />
        </div>
      </>,
    )
    const loginBtn = within(screen.getByTestId('login-scene')).getByRole('button')
    const resetBtn = within(screen.getByTestId('reset-scene')).getByRole('button')
    fireEvent.click(loginBtn)

    await waitFor(() => expect(sendMock).toHaveBeenCalledTimes(1))
    // 登录场景的按钮进了冷却,找回密码场景的那个没有
    await waitFor(() => expect(loginBtn).toBeDisabled())
    expect(resetBtn).toBeEnabled()
  })

  it('挂载时读回冷却 —— 刷新页面不用白等 60s', async () => {
    // 先发一次,落下冷却记录
    const first = render(<SmsSendButton phone={PHONE} scene={SmsScene.LOGIN} />)
    fireEvent.click(sendButton())
    await waitFor(() => expect(sendMock).toHaveBeenCalledTimes(1))
    first.unmount()

    // 重新挂载(等价于刷新页面)
    render(<SmsSendButton phone={PHONE} scene={SmsScene.LOGIN} />)
    await waitFor(() => expect(screen.getByRole('button', { name: /60s 后重发/ })).toBeDisabled())
  })
})

describe('SmsSendButton · 提示语', () => {
  it('提示里是场景的人话,不是内部标识,也不是没替换上的占位符', async () => {
    render(<SmsSendButton phone={PHONE} scene={SmsScene.LOGIN} />)
    fireEvent.click(sendButton())

    await waitFor(() => expect(screen.getByText(/验证码已发送/)).toBeInTheDocument())
    const toast = screen.getByText(/验证码已发送/)
    expect(toast.textContent).toContain('短信登录')
    // ⚠️ 变量名与字典模板对不上时,界面上会出现字面量「({scene})」
    expect(toast.textContent).not.toContain('{scene}')
    expect(toast.textContent).not.toContain('login')
  })
})

/**
 * 图形验证码被消费后必须换图(2026-10-28 M106)
 *
 * ## 这条网守的是什么
 *
 *   后端 `captchaStore.Verify(id, answer, clear=true)` → `Get(id, true)`
 *   里**先 `delete(s.data, id)` 再比对** ⇒ 这张验证码**成功失败都被消费**。
 *   所以"发短信"这个动作**一定**消耗掉当前这张验证码。
 *
 *   父页面不换图的两个后果:
 *   ① 发送成功 → 提交那一步(短信登录 / 短信重置密码)拿一张**已作废**的验证码
 *      去校验 ⇒ 「图形验证码错误或已过期」⇒ **整条短信流程 100% 走不通**;
 *   ② 发送失败 → 页面还显示旧图 ⇒ 用户重输旧码 → 再失败 ⇒ **卡死**。
 */
describe('SmsSendButton · 验证码被消费后要通知换图(M106)', () => {
  const withCaptcha = (onCaptchaConsumed: () => void) => (
    <SmsSendButton
      phone={PHONE}
      scene={SmsScene.LOGIN}
      beforeSend={async () => ({ ok: true, captcha: { id: 'cap-1', code: 'ABCD' } })}
      onCaptchaConsumed={onCaptchaConsumed}
    />
  )

  it('发送成功 → 通知父页面换图', async () => {
    const onConsumed = vi.fn()
    render(withCaptcha(onConsumed))
    fireEvent.click(sendButton())

    await waitFor(() => expect(sendMock).toHaveBeenCalled())
    await waitFor(() => expect(onConsumed).toHaveBeenCalledTimes(1))
  })

  it('发送失败 → 也要换图(后端先删后比,失败同样消费掉了)', async () => {
    sendMock.mockRejectedValue(new Error('网络断了'))
    const onConsumed = vi.fn()
    render(withCaptcha(onConsumed))
    fireEvent.click(sendButton())

    await waitFor(() => expect(sendMock).toHaveBeenCalled())
    await waitFor(() => expect(onConsumed).toHaveBeenCalledTimes(1))
  })

  it('本次压根没带验证码 → 不通知换图(没东西可换)', async () => {
    const onConsumed = vi.fn()
    render(
      <SmsSendButton
        phone={PHONE}
        scene={SmsScene.LOGIN}
        onCaptchaConsumed={onConsumed}
      />,
    )
    fireEvent.click(sendButton())

    await waitFor(() => expect(sendMock).toHaveBeenCalled())
    expect(onConsumed).not.toHaveBeenCalled()
  })

  it('beforeSend 拒绝(请求根本没发出去)→ 不通知换图(验证码没被消费)', async () => {
    const onConsumed = vi.fn()
    render(
      <SmsSendButton
        phone={PHONE}
        scene={SmsScene.LOGIN}
        beforeSend={async () => ({ ok: false, error: '图形验证码错误' })}
        onCaptchaConsumed={onConsumed}
      />,
    )
    fireEvent.click(sendButton())

    await waitFor(() => expect(screen.getByText(/图形验证码错误/)).toBeInTheDocument())
    expect(sendMock).not.toHaveBeenCalled()
    expect(onConsumed).not.toHaveBeenCalled()
  })
})
