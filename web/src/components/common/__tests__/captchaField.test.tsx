/**
 * CaptchaField 行为回归(2026-10-28 M106)
 *
 * ## 这条网守的是什么
 *
 *   **拉取失败不能停在"还在加载"**。
 *
 *   旧实现的渲染分支是 `loading || !image ? <Spin/> : <img/>`:
 *   `getCaptcha()` 失败后 `image` 一直是空串 ⇒ 界面**永远转圈** ——
 *   用户看到的是"还在加载",而请求其实早就断了。没有报错、没有重试入口
 *   (能点,但没人知道要点),唯一的出路是刷新整页。
 *
 *   现在失败后显式标 `failed`,渲染成"点我重试"的图标(容器本来就能点),
 *   并且**保留旧图** —— 因为这次失败的是"拉新图",服务端手里那张旧验证码
 *   **并没有被消费**,用户对着旧图输的码仍然有效,清掉等于白白逼用户重填。
 *
 * ## 顺带守的:开关关着时不发请求
 *
 *   两个验证码开关都关时,这个组件压根不该存在;即便挂上了也不该去打
 *   `/public/captcha`(多余的一次往返 + 一张没人用的图)。
 *
 * ## ⚠️ **等的是"渲染出来",不是"请求发出去了"**(2026-10-31 M127)
 *
 *   「失败时保留旧图」那条原本是:
 *
 *   ```ts
 *   await waitFor(() => expect(captchaMock).toHaveBeenCalledTimes(2), WAIT)
 *   expect(screen.getByAltText('captcha')).toBeInTheDocument()   // ← 直接查
 *   ```
 *
 *   而组件在**拉新图期间**(`loading === true`)渲染的是 `<Spin/>`,
 *   `<img alt="captcha">` **整个不在 DOM 里**;
 *   `toHaveBeenCalledTimes(2)` 在请求发出的那一刻就为真 ——
 *   正好落在这个"图没了"的窗口里 ⇒ 满载时必红。
 *
 *   ⇒ 等**旧图重新出现**:失败时 `image` 没被清掉,它一定会回来。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { CaptchaField } from '@/components/common/CaptchaField'
import { getCaptcha, getPublicConfig } from '@/api/auth'
import { usePublicConfigStore } from '@/stores/publicConfig'

vi.mock('@/api/auth', () => ({
  getCaptcha: vi.fn(),
  getPublicConfig: vi.fn(),
}))

const captchaMock = vi.mocked(getCaptcha)
const publicConfigMock = vi.mocked(getPublicConfig)

const WAIT = { timeout: 10_000 }

/** 造一张 1x1 的 png(base64),够渲染 <img> 了 */
const PNG =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

/** 验证码区域(容器是 role=button,aria-label = 刷新图形验证码) */
function captchaArea() {
  return screen.getByRole('button', { name: /刷新图形验证码|Refresh captcha/ })
}

beforeEach(() => {
  vi.clearAllMocks()
  // store 复位成"未知"(null)⇒ 按"开启"处理,组件会真的去拉图
  usePublicConfigStore.setState({
    captchaLoginEnabled: null,
    captchaSmsSendEnabled: null,
    loading: false,
  })
  captchaMock.mockResolvedValue({ id: 'cap-1', image: PNG })
  publicConfigMock.mockResolvedValue({
    captcha_login_enabled: true,
    captcha_sms_send_enabled: true,
  })
})

describe('CaptchaField · 正常路径', () => {
  it('拉成功后渲染出图片', async () => {
    render(<CaptchaField />)
    await waitFor(() => expect(captchaMock).toHaveBeenCalledTimes(1), WAIT)
    await waitFor(() => expect(screen.getByAltText('captcha')).toBeInTheDocument(), WAIT)
  })

  it('拉成功后把 {id, code} 交给父页面(空 code,用户还没输)', async () => {
    const onChange = vi.fn()
    render(<CaptchaField onChange={onChange} />)
    await waitFor(() => expect(onChange).toHaveBeenCalledWith({ id: 'cap-1', code: '' }), WAIT)
  })

  it('点击图片区域 → 重新拉一张', async () => {
    render(<CaptchaField />)
    await waitFor(() => expect(captchaMock).toHaveBeenCalledTimes(1), WAIT)
    fireEvent.click(captchaArea())
    await waitFor(() => expect(captchaMock).toHaveBeenCalledTimes(2), WAIT)
  })
})

describe('CaptchaField · 拉取失败不能停在"还在加载"(M106)', () => {
  it('失败后不再永远转圈', async () => {
    captchaMock.mockRejectedValue(new Error('网络断了'))
    const { container } = render(<CaptchaField />)

    await waitFor(() => expect(captchaMock).toHaveBeenCalledTimes(1), WAIT)
    // ⚠️ 关键断言:**没有 .ant-spin** —— 旧实现这里会永远转圈
    await waitFor(() => expect(container.querySelector('.ant-spin')).toBeNull(), WAIT)
  })

  it('失败后显示"点我重试"的图标(不是空白)', async () => {
    captchaMock.mockRejectedValue(new Error('网络断了'))
    const { container } = render(<CaptchaField />)

    await waitFor(() => expect(container.querySelector('.anticon-reload')).not.toBeNull(), WAIT)
  })

  it('失败后点一下能重试(真的再发一次请求)', async () => {
    captchaMock.mockRejectedValueOnce(new Error('网络断了'))
    render(<CaptchaField />)

    await waitFor(() => expect(captchaMock).toHaveBeenCalledTimes(1), WAIT)
    fireEvent.click(captchaArea())
    await waitFor(() => expect(captchaMock).toHaveBeenCalledTimes(2), WAIT)
  })

  it('失败时保留旧图(这次失败的是"拉新图",旧验证码没被消费)', async () => {
    // 第一次成功
    render(<CaptchaField />)
    await waitFor(() => expect(screen.getByAltText('captcha')).toBeInTheDocument(), WAIT)

    // 第二次失败
    captchaMock.mockRejectedValueOnce(new Error('网络断了'))
    fireEvent.click(captchaArea())
    await waitFor(() => expect(captchaMock).toHaveBeenCalledTimes(2), WAIT)
    // 旧图还在 —— 用户对着它输的码仍然有效,不该被清掉
    //
    // ⚠️ **不能直接查**(2026-10-31 M127):点一下 → `fetch()` 立刻 `setLoading(true)`
    //   → 渲染分支换成 `<Spin/>`,**`<img alt="captcha">` 整个从 DOM 里消失了**;
    //   而上面那句 `toHaveBeenCalledTimes(2)` 在**请求发出那一刻**就为真,
    //   正好落在这个"图没了"的窗口里 ⇒ 满载时必红:
    //   "Unable to find an element with the text: captcha"。
    //   ⇒ 等**旧图重新出现**(失败时 image 没被清掉 ⇒ 它一定会回来)。
    await waitFor(() => expect(screen.getByAltText('captcha')).toBeInTheDocument(), WAIT)
  })
})

describe('CaptchaField · 开关关着时不发请求', () => {
  it('两个开关都关 → 一次都不打 /public/captcha', async () => {
    usePublicConfigStore.setState({
      captchaLoginEnabled: false,
      captchaSmsSendEnabled: false,
      loading: false,
    })
    render(<CaptchaField />)
    await waitFor(() => expect(captchaMock).not.toHaveBeenCalled(), WAIT)
  })
})
