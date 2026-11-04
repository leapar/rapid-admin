/**
 * 短信验证码场景枚举 —— **必须与后端逐字一致**
 *
 * ## 这份枚举为什么是"契约"而不是"常量"
 *
 * 后端 `server/internal/service/auth_impl.go` 的 `validScene()` 是**白名单**:
 * 不在白名单里的 scene,`POST /public/auth/sms/send` 直接返
 * 「未知 scene: xxx」(400)。
 *
 * 而 2026-10-28(M104)之前,前端发短信用的是:
 *
 *   · `bizType="login-sms"`      (登录页)
 *   · `bizType="forgot-password"`(找回密码页)
 *   · `bizType="switch-identity"`(身份切换弹窗)
 *   · `bizType="change-password"`(个人中心改密)
 *
 * **这四个 scene 后端一个都不认** —— `validScene()` 只认
 * `login` / `reset_password` / `change_phone` / `view_full_phone`。
 * 于是**短信一条都没发出去过**:登录页点「发送验证码」必得一个
 * 「未知 scene: login-sms」。
 *
 * 错在哪一边?后端的 4 个 scene 与阿里云的 4 个短信模板
 * (`aliyun.sms.template.{login,view_full_phone,reset_password,change_phone}`,
 * 见 docs/配置项清单.md)一一对应 —— **后端是对的,技术方案 §2310 那份
 * 5-scene 清单是过期的**(它多了一个 `switch_identity`,而短信服务商
 * 根本没有那个模板)。所以对齐方向是**前端改向后端**。
 *
 * `web/src/constants/__tests__/smsScene.test.ts` 直接解析后端源码比对,
 * 后端加/改 scene 时那张网立刻变红。
 */

/** 短信场景(与后端 `model.SmsScene*` 逐字一致) */
export const SmsScene = {
  /** 短信登录 */
  LOGIN: 'login',
  /** 自助重置密码 */
  RESET_PASSWORD: 'reset_password',
  /** 修改手机号(切高敏身份也复用它 —— 见下方注释) */
  CHANGE_PHONE: 'change_phone',
  /** 查完整手机号(脱敏反查) */
  VIEW_FULL_PHONE: 'view_full_phone',
  /**
   * 登录二次验证(M150)。
   *
   * ⚠️ **独立 scene,不许复用 `login`** —— LoginBySms 按 `login` scene 消费码,
   * 复用的话"短信登录"就能烧掉 MFA 的码,等于给 MFA 开了个总后门。
   * 后端模板侧复用 login 的阿里云模板(零新增配置),但码的存取按 `mfa` 隔离。
   */
  MFA: 'mfa',
} as const

export type SmsScene = (typeof SmsScene)[keyof typeof SmsScene]

/** 全部合法 scene(给表单校验 / 参数白名单用) */
export const SMS_SCENES: readonly SmsScene[] = Object.values(SmsScene)

/**
 * scene → i18n key
 *
 * 用途:提示语里要告诉用户"这条验证码是干什么用的"。
 * 以前拼的是 `bizType`(`login-sms` 这种内部标识),用户看到的是
 * "验证码已发送 (login-sms)" —— 内部标识不该出现在用户界面上。
 */
export const SMS_SCENE_LABEL_KEY: Record<SmsScene, string> = {
  [SmsScene.LOGIN]: 'smsScene.login',
  [SmsScene.RESET_PASSWORD]: 'smsScene.resetPassword',
  [SmsScene.CHANGE_PHONE]: 'smsScene.changePhone',
  [SmsScene.VIEW_FULL_PHONE]: 'smsScene.viewFullPhone',
  [SmsScene.MFA]: 'smsScene.mfa',
}

/** 短信验证码长度(后端 generateCode 恒 6 位) */
export const SMS_CODE_LEN = 6

/**
 * ⚠️ **切高敏身份用的 scene 是 `change_phone`,不是 `switch_identity`。**
 *
 * 后端 `SwitchIdentity` 校验验证码时查的就是 `scene = change_phone`
 * (权限模型设计V1.0.md §1494),而阿里云也只有这 4 个模板 ——
 * 没有 `switch_identity` 模板,凭空造一个只会让短信发不出去。
 *
 * 前后端必须一致:前端发 `change_phone`,后端查 `change_phone`。
 */
export const SWITCH_IDENTITY_SMS_SCENE: SmsScene = SmsScene.CHANGE_PHONE

// ⚠️ M113(2026-10-29):**删掉了 `CHANGE_PASSWORD_SMS_SCENE`** ——
//   个人中心改密**不再要短信验证码**(用户 2026-10-29 明确要求),
//   前端那道短信门是纯前端的、而且是**坏的**(短信压根发不出去),
//   ⇒ 那条常量描述的流程已经不存在了,留着只会骗下一个人
//   ("改密要发短信吗?看常量是 RESET_PASSWORD……")。
//   后端 `POST /auth/password` 从来就只认 old_password + new_password。
