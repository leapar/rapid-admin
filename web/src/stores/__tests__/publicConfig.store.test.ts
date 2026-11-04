/**
 * publicConfig store 行为回归(2026-10-28 M106)
 *
 * ## 这条网守的是什么
 *
 *   这个 store 决定**登录页渲不渲染图形验证码框** —— 它挂在全局链上
 *   (main.tsx 启动时拉一次,登录页 / 找回密码页 / CaptchaField 都读它)。
 *
 *   它的取值语义是**三态**而不是布尔:
 *     true  = 后端说开着 → 要验证码
 *     false = 后端说关着 → 不要
 *     null  = 还没拉到 / 拉失败 → **按 true 处理**
 *
 *   为什么"未知按开":后端无论如何都会再校验一次,前端多显示一个输入框
 *   只是麻烦一点;反过来"未知就不显示"会造出**前端不显示、后端报必填**
 *   的死结。**失败方向要选烦人的那个。**
 *
 *   这条网之前是**零覆盖** —— 而"渲染错了验证码框"是那种**上线后
 *   用户投诉才发现**的故障(登录页多一个框 / 或者卡在"看不见验证码
 *   但服务端说必填")。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@/api/auth', () => ({
  getPublicConfig: vi.fn(),
}))

import { getPublicConfig } from '@/api/auth'
import { usePublicConfigStore } from '@/stores/publicConfig'

const publicConfigMock = vi.mocked(getPublicConfig)

beforeEach(() => {
  vi.clearAllMocks()
  usePublicConfigStore.setState({
    captchaLoginEnabled: null,
    captchaSmsSendEnabled: null,
    appName: '',
    appTitle: '',
    loading: false,
  })
  publicConfigMock.mockResolvedValue({
    captcha_login_enabled: true,
    captcha_sms_send_enabled: true,
  })
})

describe('stores/publicConfig · 三态语义', () => {
  it('初始(还没拉)按"开启"处理 —— 失败方向要选烦人的那个', () => {
    const s = usePublicConfigStore.getState()
    expect(s.captchaLoginEnabled).toBeNull()
    expect(s.isLoginCaptchaEnabled()).toBe(true)
    expect(s.isSmsCaptchaEnabled()).toBe(true)
    expect(s.isAnyCaptchaEnabled()).toBe(true)
  })

  it('后端说关 → 真的关掉', async () => {
    publicConfigMock.mockResolvedValue({
      captcha_login_enabled: false,
      captcha_sms_send_enabled: false,
    })
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.captchaLoginEnabled).toBe(false)
    expect(s.isLoginCaptchaEnabled()).toBe(false)
    expect(s.isSmsCaptchaEnabled()).toBe(false)
    expect(s.isAnyCaptchaEnabled()).toBe(false)
  })

  it('登录关但发短信开着 → 短信流程仍要验证码', async () => {
    publicConfigMock.mockResolvedValue({
      captcha_login_enabled: false,
      captcha_sms_send_enabled: true,
    })
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.isLoginCaptchaEnabled()).toBe(false)
    expect(s.isSmsCaptchaEnabled()).toBe(true)
    expect(s.isAnyCaptchaEnabled()).toBe(true)
  })

  it('登录开着但发短信关着 → 短信流程仍要验证码(短信登录提交那一步受 login 管)', async () => {
    publicConfigMock.mockResolvedValue({
      captcha_login_enabled: true,
      captcha_sms_send_enabled: false,
    })
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.isSmsCaptchaEnabled()).toBe(true)
  })
})

describe('stores/publicConfig · 失败方向', () => {
  it('拉失败 → 保持 null(按"开启"渲染),不把校验"意外关掉"', async () => {
    publicConfigMock.mockRejectedValue(new Error('网络断了'))
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.captchaLoginEnabled).toBeNull()
    expect(s.captchaSmsSendEnabled).toBeNull()
    expect(s.isLoginCaptchaEnabled()).toBe(true)
    expect(s.isAnyCaptchaEnabled()).toBe(true)
    expect(s.loading).toBe(false)
  })

  it('响应里字段缺失 → 也按"开启"(宁可多显示一个框)', async () => {
    publicConfigMock.mockResolvedValue({} as never)
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.captchaLoginEnabled).toBe(true)
    expect(s.captchaSmsSendEnabled).toBe(true)
  })

  it('字段类型不对(字符串 "false")→ 仍然按"开启",不因为类型怪就关掉校验', async () => {
    publicConfigMock.mockResolvedValue({
      captcha_login_enabled: 'false',
      captcha_sms_send_enabled: 'false',
    } as never)
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.captchaLoginEnabled).toBe(true)
  })

  it('拉失败后可以重试(失败不该把开关锁死)', async () => {
    publicConfigMock.mockRejectedValueOnce(new Error('网络断了'))
    await usePublicConfigStore.getState().fetch()
    expect(publicConfigMock).toHaveBeenCalledTimes(1)

    await usePublicConfigStore.getState().fetch()
    expect(publicConfigMock).toHaveBeenCalledTimes(2)
  })
})

describe('stores/publicConfig · 重复拉取', () => {
  it('成功之后不再重复拉(开关在一次会话里只取一次)', async () => {
    await usePublicConfigStore.getState().fetch()
    await usePublicConfigStore.getState().fetch()
    expect(publicConfigMock).toHaveBeenCalledTimes(1)
  })

  it('并发调用只发一次请求(loading 守卫)', async () => {
    await Promise.all([
      usePublicConfigStore.getState().fetch(),
      usePublicConfigStore.getState().fetch(),
    ])
    expect(publicConfigMock).toHaveBeenCalledTimes(1)
  })
})

/**
 * 品牌信息(2026-11-01 开源通用化)
 *
 * 同一个接口顺带下发平台名 —— 登录页标题 / 顶栏品牌名 / 浏览器标签页标题
 * 都取自这里。后端由 `branding:` 配置块驱动(改 yaml 即换名字)。
 *
 * **空串 = 没配** → 调用方回退到本地语言包(中文界面显示中文名)。
 */
describe('stores/publicConfig · 品牌信息', () => {
  it('后端下发的 app_name / app_title 原样进 store', async () => {
    publicConfigMock.mockResolvedValue({
      captcha_login_enabled: true,
      captcha_sms_send_enabled: true,
      app_name: 'Acme 交付平台',
      app_title: 'Acme 交付平台 - 后台管理',
    })
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.appName).toBe('Acme 交付平台')
    expect(s.appTitle).toBe('Acme 交付平台 - 后台管理')
  })

  it('后端没配(空串)→ store 里是空串,调用方据此回退语言包', async () => {
    publicConfigMock.mockResolvedValue({
      captcha_login_enabled: true,
      captcha_sms_send_enabled: true,
      app_name: '',
      app_title: '',
    })
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.appName).toBe('')
    expect(s.appTitle).toBe('')
  })

  it('老后端不返这两个字段(undefined)→ 空串,不是 "undefined"', async () => {
    publicConfigMock.mockResolvedValue({
      captcha_login_enabled: true,
      captcha_sms_send_enabled: true,
    } as never)
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.appName).toBe('')
    expect(s.appTitle).toBe('')
  })

  it('字段类型不对(数字 / 对象)→ 空串(否则界面会显示 "[object Object]")', async () => {
    publicConfigMock.mockResolvedValue({
      captcha_login_enabled: true,
      captcha_sms_send_enabled: true,
      app_name: { nested: true },
      app_title: 42,
    } as never)
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.appName).toBe('')
    expect(s.appTitle).toBe('')
  })

  it('拉失败 → 品牌名保持空串(失败方向也是回退语言包,不是崩)', async () => {
    publicConfigMock.mockRejectedValue(new Error('网络断了'))
    await usePublicConfigStore.getState().fetch()

    const s = usePublicConfigStore.getState()
    expect(s.appName).toBe('')
    expect(s.appTitle).toBe('')
  })
})
