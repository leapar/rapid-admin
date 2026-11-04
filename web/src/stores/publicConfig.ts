/**
 * 匿名运行时开关 Store(登录前可用)
 *
 * 为什么不能直接用 stores/config.ts:
 *   它打的是 GET /platform/configs,在 router.go 里挂在 Auth() 受保护组下,
 *   未登录时 401 —— 而登录页恰恰是最需要知道"验证码开没开"的地方。
 *   所以另开 GET /rapid-admin/v1/public/config,后端只返白名单开关,不返配置值。
 *
 * 读取约定 —— 两个标志都是三态而不是布尔:
 *   true  = 后端说开着 → 需要验证码
 *   false = 后端说关着 → 不需要
 *   null  = 还没拉到 / 拉失败 → **按 true 处理**
 *
 *   为什么未知按 true:后端无论如何都会再校验一次,前端多显示一个输入框
 *   只是麻烦一点;反过来"未知就不显示"会让后端开着校验时用户卡在
 *   "看不见验证码但服务端说必填"的死结里。失败方向要选烦人的那个。
 *
 * 页面该用哪个选择器:
 *   密码登录 tab        → isLoginCaptchaEnabled()   (只看 login 开关)
 *   短信登录 tab        → isSmsCaptchaEnabled()     (login || sms_send)
 *   找回密码页          → isSmsCaptchaEnabled()     (同上,短信是唯一入口)
 *   CaptchaField 自身   → isAnyCaptchaEnabled()     (任一开就不发请求)
 *
 * 短信流程取"或"是因为它要先过 /public/auth/sms/send:哪怕登录侧关了,
 * 只要发短信闸门还开着,验证码框就得留着。
 *
 * ## 品牌信息(2026-11-01 开源通用化新增)
 *
 *   同一个接口顺带下发**平台名**(`app_name` / `app_title`):
 *   登录页标题、顶栏品牌名、浏览器标签页标题都取自这里,
 *   后端由 `branding:` 配置块驱动(改 yaml 即换名字,不用改前端)。
 *
 *   **空串 = 后端没配** —— 此时调用方回退到本地语言包
 *   (中文界面显示中文名,英文界面显示英文名)。
 *   ⚠️ 失败方向也选"回退到语言包":品牌名缺失只是界面上一行字,
 *   不该让页面报错或空着 —— 语言包永远有值。
 */
import { create } from 'zustand'
import { getPublicConfig } from '@/api/auth'

interface PublicConfigState {
  /** null = 未知(未拉到或拉失败),按"开启"处理,见文件头说明 */
  captchaLoginEnabled: boolean | null
  /** 发短信防刷量闸门,同上 */
  captchaSmsSendEnabled: boolean | null
  /** 平台全名(后端 branding.app_name);空串 = 没配 → 调用方回退到语言包 */
  appName: string
  /** 浏览器标签页标题(后端 branding.app_title / app_name);空串 = 没配 */
  appTitle: string
  loading: boolean
  /** 拉取;已在拉或已成功过则直接返回 */
  fetch: () => Promise<void>
  /** 登录类要不要验证码 —— 密码登录 tab 用这个 */
  isLoginCaptchaEnabled: () => boolean
  /** 短信流程要不要验证码 —— 短信登录 tab / 找回密码页用这个 */
  isSmsCaptchaEnabled: () => boolean
  /** 任一场景要验证码 —— CaptchaField 判断要不要发请求 */
  isAnyCaptchaEnabled: () => boolean
}

export const usePublicConfigStore = create<PublicConfigState>((set, get) => ({
  captchaLoginEnabled: null,
  captchaSmsSendEnabled: null,
  appName: '',
  appTitle: '',
  loading: false,

  fetch: async () => {
    const { loading, captchaLoginEnabled } = get()
    if (loading || captchaLoginEnabled !== null) return
    set({ loading: true })
    try {
      const r = await getPublicConfig()
      // 兜底成 true:接口返回形状不对时也不要把校验"意外关掉"
      const on = (v: unknown) => v !== false
      // 品牌名同理:类型不对 / 字段缺失 → 当成"没配"(空串)→ 调用方回退语言包。
      // ⚠️ **不能把非字符串直接塞进 state** —— 那会让 `appName || t(...)` 这种
      //   回退判断失效(非空对象/数字都是 truthy),界面就会显示 "[object Object]"。
      const str = (v: unknown) => (typeof v === 'string' ? v : '')
      set({
        captchaLoginEnabled: on(r?.captcha_login_enabled),
        captchaSmsSendEnabled: on(r?.captcha_sms_send_enabled),
        appName: str(r?.app_name),
        appTitle: str(r?.app_title),
      })
    } catch {
      // 拦截器已提示;保持 null = 按开启渲染,不阻塞登录页
    } finally {
      set({ loading: false })
    }
  },

  isLoginCaptchaEnabled: () => get().captchaLoginEnabled !== false,

  // 未知(null)按 true,所以 sms 未知时这个或运算天然也是 true —— 与"未知按开"一致
  isSmsCaptchaEnabled: () => get().captchaLoginEnabled !== false || get().captchaSmsSendEnabled !== false,

  isAnyCaptchaEnabled: () => get().captchaLoginEnabled !== false || get().captchaSmsSendEnabled !== false,
}))
