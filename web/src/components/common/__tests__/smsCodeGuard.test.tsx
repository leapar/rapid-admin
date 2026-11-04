/**
 * SmsCodeGuard 行为回归(2026-10-28 M104)
 *
 * ## 这条网守的是什么
 *
 *   这个组件是"敏感操作前的短信二次确认",M104 之前它有 6 处失效,
 *   每一处都是**静默**的(不崩、不报错,只是这个功能从来没活过):
 *
 *   1. `bizType` 是**死 prop** —— 从来没被传进 `sendSmsCode` / `verifySmsCode`。
 *      → 现在叫 `scene`,而且必填。
 *   2. `onVerified` **不带验证码** —— 调用方只能传 `undefined` 给后端,
 *      后端直接拒「切换到高敏角色需要短信验证码」。
 *   3. **双重消费** —— 后端校验通过即置 `used_at`,所以"本地先验一次、
 *      后面再拿同一个码去用"必然失败 → 用 `mode="delegate"` 表达。
 *   4. `setInterval` **泄漏** —— 倒计时的 timer 从来没被清过。
 *   5. `cloneElement` **覆盖**了 trigger 自己的 `onClick`。
 *   6. 验证码长度校验是 `code.length < 4` —— 4 位、5 位都能过这关。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `scene` 改回不传(`sendSmsCode(phone, undefined)`)→ 本文件立刻变红。
 *   · 把 `onVerified(code)` 改成 `onVerified()` → 本文件立刻变红。
 *   · 把 `mode === 'guard'` 判断去掉(delegate 也本地验)→ delegate 那条变红。
 *   · 把 `useEffect(() => stopCountdown, [])` 删掉 → 定时器那条变红。
 *   · 把 `trigger.props.onClick?.(e)` 删掉 → trigger 那条变红。
 *   · 把 `code.length !== SMS_CODE_LEN` 改回 `< 4` → 长度那条变红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { message } from 'antd'
import { SmsCodeGuard } from '@/components/common/SmsCodeGuard'
import { sendSmsCode, verifySmsCode } from '@/api/auth'
import { SmsScene, SMS_CODE_LEN } from '@/constants/sms'

vi.mock('@/api/auth', () => ({
  sendSmsCode: vi.fn(),
  verifySmsCode: vi.fn(),
}))

const sendMock = vi.mocked(sendSmsCode)
const verifyMock = vi.mocked(verifySmsCode)

const PHONE = '13800138000'
const CODE = '123456'

/** 触发按钮(普通 HTML button —— 避开 antd Button 的两字插空格行为) */
function triggerButton(onClick?: () => void) {
  return (
    <button type="button" onClick={onClick}>
      触发短信
    </button>
  )
}

/** 打开弹窗 */
function openGuard() {
  fireEvent.click(screen.getByRole('button', { name: '触发短信' }))
}

/** 弹窗里的验证码输入框 */
function codeInput(): HTMLInputElement {
  const el = document.querySelector('.ant-modal input')
  if (!el) throw new Error('弹窗没打开:找不到验证码输入框')
  return el as HTMLInputElement
}

/** 弹窗里的「获取验证码」按钮 */
function getCodeButton() {
  return screen.getByRole('button', { name: /获取验证码/ })
}

/** 弹窗里的「确认」按钮(antd 对两字按钮会插空格,所以用正则) */
function confirmButton() {
  return screen.getByRole('button', { name: /确\s*认/ })
}

/** 在弹窗里填验证码 */
function fillCode(value: string) {
  fireEvent.change(codeInput(), { target: { value } })
}

beforeEach(() => {
  sendMock.mockResolvedValue(undefined)
  verifyMock.mockResolvedValue({ verified: true })
  // antd 的 message 是全局单例,toast 会跨用例留在 DOM 里(默认 3s 才消失)。
  // 不清掉的话,上一个用例的提示语会把下一个用例的断言打成"找到多个"。
  message.destroy()
})

describe('SmsCodeGuard · 场景必须传下去(死 prop 回归)', () => {
  it('发送验证码时带上 scene(以前是 undefined → 后端 400「未知 scene」)', async () => {
    render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton()}
        onVerified={vi.fn()}
      />,
    )
    openGuard()
    fireEvent.click(getCodeButton())

    await waitFor(() => expect(sendMock).toHaveBeenCalled())
    expect(sendMock.mock.calls[0]![0]).toBe(PHONE)
    // ⚠️ 第二个参数必须是后端白名单里的 scene —— 不是 undefined,也不是 bizType
    expect(sendMock.mock.calls[0]![1]).toBe('login')
  })

  it('校验验证码时也带上 scene(后端按 phone+scene+code 三者一起查)', async () => {
    render(
      <SmsCodeGuard
        scene={SmsScene.RESET_PASSWORD}
        phone={PHONE}
        trigger={triggerButton()}
        onVerified={vi.fn()}
      />,
    )
    openGuard()
    fillCode(CODE)
    fireEvent.click(confirmButton())

    await waitFor(() => expect(verifyMock).toHaveBeenCalled())
    expect(verifyMock.mock.calls[0]![0]).toBe(PHONE)
    expect(verifyMock.mock.calls[0]![1]).toBe(CODE)
    expect(verifyMock.mock.calls[0]![2]).toBe('reset_password')
  })

  it('弹窗里显示的是场景的人话,不是 scene 原值', async () => {
    render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton()}
        onVerified={vi.fn()}
      />,
    )
    openGuard()

    await waitFor(() => expect(screen.getByText(/短信登录/)).toBeInTheDocument())
    // 界面里不该出现内部标识
    expect(document.body.textContent).not.toContain('login')
  })
})

describe('SmsCodeGuard · 验证码必须交回调用方', () => {
  it('校验通过后 onVerified 拿到的是验证码本身(以前只能传 undefined)', async () => {
    const onVerified = vi.fn()
    render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton()}
        onVerified={onVerified}
      />,
    )
    openGuard()
    fillCode(CODE)
    fireEvent.click(confirmButton())

    await waitFor(() => expect(onVerified).toHaveBeenCalled())
    expect(onVerified.mock.calls[0]![0]).toBe(CODE)
  })

  it('校验不通过 → 不回调 onVerified', async () => {
    verifyMock.mockResolvedValue({ verified: false })
    const onVerified = vi.fn()
    render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton()}
        onVerified={onVerified}
      />,
    )
    openGuard()
    fillCode(CODE)
    fireEvent.click(confirmButton())

    await waitFor(() => expect(screen.getByText(/验证码错误/)).toBeInTheDocument())
    expect(onVerified).not.toHaveBeenCalled()
  })

  it('自定义 verifyCode 优先于默认的 verifySmsCode', async () => {
    const verifyCode = vi.fn().mockResolvedValue(true)
    render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton()}
        verifyCode={verifyCode}
        onVerified={vi.fn()}
      />,
    )
    openGuard()
    fillCode(CODE)
    fireEvent.click(confirmButton())

    await waitFor(() => expect(verifyCode).toHaveBeenCalledWith(CODE))
    expect(verifyMock).not.toHaveBeenCalled()
  })
})

describe('SmsCodeGuard · delegate 模式(验证码由后续的后端调用消费)', () => {
  it('delegate 模式不本地校验 —— 同一个码不能被消费两次', async () => {
    const onVerified = vi.fn()
    render(
      <SmsCodeGuard
        scene={SmsScene.CHANGE_PHONE}
        phone={PHONE}
        mode="delegate"
        trigger={triggerButton()}
        onVerified={onVerified}
      />,
    )
    openGuard()
    fillCode(CODE)
    fireEvent.click(confirmButton())

    await waitFor(() => expect(onVerified).toHaveBeenCalledWith(CODE))
    // ⚠️ 本地验过一次 → 后端 switchIdentity 再用同一个码就是「已使用」
    expect(verifyMock).not.toHaveBeenCalled()
  })

  it('delegate 模式下自定义 verifyCode 也不该被调用', async () => {
    const verifyCode = vi.fn().mockResolvedValue(true)
    render(
      <SmsCodeGuard
        scene={SmsScene.CHANGE_PHONE}
        phone={PHONE}
        mode="delegate"
        trigger={triggerButton()}
        verifyCode={verifyCode}
        onVerified={vi.fn()}
      />,
    )
    openGuard()
    fillCode(CODE)
    fireEvent.click(confirmButton())

    await waitFor(() => expect(verifyMock).not.toHaveBeenCalled())
    expect(verifyCode).not.toHaveBeenCalled()
  })
})

describe('SmsCodeGuard · 验证码长度', () => {
  it('4 位验证码被拦下(以前 `length < 4` 放行 4/5 位)', async () => {
    const onVerified = vi.fn()
    render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton()}
        onVerified={onVerified}
      />,
    )
    openGuard()
    fillCode('1234')
    fireEvent.click(confirmButton())

    await waitFor(() => expect(screen.getByText(/请输入 6 位验证码/)).toBeInTheDocument())
    expect(verifyMock).not.toHaveBeenCalled()
    expect(onVerified).not.toHaveBeenCalled()
  })

  it('5 位验证码同样被拦下', async () => {
    const onVerified = vi.fn()
    render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton()}
        onVerified={onVerified}
      />,
    )
    openGuard()
    fillCode('12345')
    fireEvent.click(confirmButton())

    await waitFor(() => expect(screen.getByText(/请输入 6 位验证码/)).toBeInTheDocument())
    expect(onVerified).not.toHaveBeenCalled()
  })

  it('恰好 6 位才放行(长度常量就是 6)', async () => {
    const onVerified = vi.fn()
    render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton()}
        onVerified={onVerified}
      />,
    )
    openGuard()
    fillCode('0'.repeat(SMS_CODE_LEN))
    fireEvent.click(confirmButton())

    await waitFor(() => expect(onVerified).toHaveBeenCalledWith('0'.repeat(SMS_CODE_LEN)))
  })
})

describe('SmsCodeGuard · trigger 的点击逻辑不能被冲掉', () => {
  it('trigger 自己的 onClick 照常触发,弹窗同时打开', async () => {
    const triggerClick = vi.fn()
    render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton(triggerClick)}
        onVerified={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: '触发短信' }))

    expect(triggerClick).toHaveBeenCalled()
    await waitFor(() => expect(document.querySelector('.ant-modal')).toBeInTheDocument())
  })
})

describe('SmsCodeGuard · 定时器不许泄漏', () => {
  it('卸载时把倒计时的 setInterval 清掉', async () => {
    const setSpy = vi.spyOn(globalThis, 'setInterval')
    const clearSpy = vi.spyOn(globalThis, 'clearInterval')

    const { unmount } = render(
      <SmsCodeGuard
        scene={SmsScene.LOGIN}
        phone={PHONE}
        trigger={triggerButton()}
        onVerified={vi.fn()}
      />,
    )
    openGuard()

    const before = setSpy.mock.calls.length
    await act(async () => {
      fireEvent.click(getCodeButton())
    })
    await waitFor(() => expect(sendMock).toHaveBeenCalled())

    // 倒计时定时器是**发码之后**才建的
    const newTimers = setSpy.mock.results.slice(before).map((r) => r.value)
    expect(newTimers.length).toBeGreaterThan(0)

    unmount()
    // 一个都不许留在后台跑
    for (const id of newTimers) {
      expect(clearSpy, `定时器 ${String(id)} 没被清掉`).toHaveBeenCalledWith(id)
    }
  })
})
