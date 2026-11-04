/**
 * 鉴权相关 API
 *
 * 路径约定(与 server/internal/api/router/router.go 保持一致):
 *   /rapid-admin/v1/public/*   公开(无需登录)
 *   /rapid-admin/v1/auth/*     受保护(需 Bearer token)
 *
 * public 下细分子路径: /public/auth/{login,refresh,sms/send,sms/verify}, /public/captcha
 */
import { http } from './client'
import {
  LoginRequest,
  LoginResponse,
  UserInfo,
  UpdateProfileRequest,
  SessionOverview,
  type AvatarView,
} from '@/types/user'
import type { SmsScene } from '@/constants/sms'

/* ====================== 公开接口 ====================== */

/** 账号密码登录 */
export function login(data: LoginRequest) {
  return http.post<LoginResponse>('/public/auth/login', data)
}

/** 刷新 token(后端放在 public 下, refresh token 自身可证明身份)
 *
 *  ⚠️ 响应类型就是 LoginResponse,不是另一个结构 —— 2026-10-21 之前后端
 *  RefreshToken 直吐 *model.SysUser,**不带 identities 字段**,用户续期之后
 *  身份列表静默消失(切换身份入口突然没了,不报错)。后端现在三个端点统一返
 *  service.AuthTokens,前端也只有一个类型。
 *
 *  ⚠️ access token 的字段名是 `token` 不是 `access_token`(后端 json tag)。
 *  前端曾按 access_token 取值,而 client.ts 的 http.post 只做 `r.data.data`
 *  透传、不改名 → 续期成功时拿到的 access_token 恒为 undefined,下一次请求发出
 *  `Bearer undefined` → 401 → 被踢回登录页。**自动续期从上线起就没成功过一次。**
 *  改字段名时务必和后端 tag 一起看。 */
export function refreshToken(refresh_token: string) {
  return http.post<LoginResponse>('/public/auth/refresh', { refresh_token })
}

/**
 * 发送短信验证码(公开接口)
 *
 * ⚠️ **scene 是必填的,而且只能是后端白名单里的那 4 个**
 * (`login` / `reset_password` / `change_phone` / `view_full_phone`)。
 *
 * 2026-10-28(M104)之前这里是 `scene?: string`,前端各处传的是
 * `'login-sms'` / `'forgot-password'` / `'switch-identity'` —— **后端一个都不认**,
 * `validScene()` 直接返 400「未知 scene」,于是**短信一条都没发出去过**。
 * 现在类型层面就把它锁死,传错编译期就红。
 *
 * @param phone 11 位手机号
 * @param scene 业务场景(后端日志分流 & 冷却策略 & 模板选择都用它)
 * @param captcha 图形验证码 { id, code };captcha.sms_send_enabled=true 时必填
 *         (SmsSendButton 的 beforeSend 钩子负责拦),=false 时不传,后端跳过校验
 */
export function sendSmsCode(phone: string, scene: SmsScene, captcha?: { id: string; code: string }) {
  return http.post<void>('/public/auth/sms/send', {
    phone,
    scene,
    captcha_id: captcha?.id ?? '',
    captcha_code: captcha?.code ?? '',
  })
}

/**
 * 校验短信验证码(公开接口)
 *
 * ⚠️ scene 同样必填 —— 后端按 `phone + scene + code` 三者一起查,
 * 少一个就查不到(而且 scene 错了等于拿别的场景的码来用)。
 */
export function verifySmsCode(phone: string, code: string, scene: SmsScene) {
  return http.post<{ verified: boolean }>('/public/auth/sms/verify', { phone, code, scene })
}

/**
 * 给**当前账号登记的手机号**发验证码
 *
 * 后端 POST /rapid-admin/v1/auth/sms/account/send
 *
 * ⚠️ **为什么不走上面的公开接口**:前端手上只有脱敏手机号
 * (/auth/me 返 `138****5678`),而公开接口要求 11 位纯数字
 * (后端 validPhone)—— 拿脱敏号去发,后端直接「手机号格式错误」。
 *
 * 所以:**手机号由服务端从 sys_user 取**,前端不传。
 * 顺带把验证码**绑死在账号自己的手机上**(见后端 consumeSmsCode)。
 *
 * @param scene 必须是后端白名单里的自助场景
 *        (change_phone / reset_password / view_full_phone)
 */
export function sendAccountSms(scene: SmsScene) {
  return http.post<void>('/auth/sms/account/send', { scene })
}

/**
 * 校验**发给当前账号登记手机号**的验证码
 *
 * 后端 POST /rapid-admin/v1/auth/sms/account/verify
 *
 * 与公开的 verifySmsCode 的区别只有一个:手机号由服务端取,前端不传。
 * 校验规则完全一致(code + scene + phone 三者全等 + 未用过 + 未过期),
 * 通过即消费。
 */
export function verifyAccountSms(code: string, scene: SmsScene) {
  return http.post<{ verified: boolean }>('/auth/sms/account/verify', { code, scene })
}

/**
 * 给**新手机号**发验证码(换绑手机号用)
 *
 * 后端 POST /rapid-admin/v1/auth/phone/code
 *
 * ⚠️ **跟 sendAccountSms 的方向正好相反**:
 *   `sendAccountSms` 发给**账号登记的号**(服务端从 sys_user 取,前端不传);
 *   这里发给**用户想换绑的新号** —— 所以手机号由前端传。
 *
 * 为什么必须这样:换绑要证明的是"用户占着**新**号",而服务端手上只有旧号。
 * 用旧号验证只能证明"用户还占着旧号",证明不了换绑真正要证明的那件事。
 *
 * 后端会把码落在 sys_sms_code(phone=新号, scene=change_phone),
 * 换绑时按**新号**查 —— 于是这条码在"切高敏身份"那条链路上用不了。
 */
export function sendBindPhoneCode(phone: string) {
  return http.post<void>('/auth/phone/code', { phone })
}

/**
 * 换绑手机号
 *
 * 后端 PATCH /rapid-admin/v1/auth/phone
 *
 * @param phone 新手机号(11 位)
 * @param code  发到**新号**上的 6 位验证码
 * @returns 最新的用户视图 —— 前端整份替换 store 的 userInfo,
 *          少返一个字段 = 把那个字段从界面上洗空(同 updateProfile)
 */
export function bindPhone(phone: string, code: string) {
  return http.patch<UserInfo>('/auth/phone', { phone, code })
}

/** 获取图形验证码
 *  服务端 CaptchaGenerateResult.ID json tag = "id", image 字段为 data:image/png;base64,...
 *
 *  只有在相关验证码开关为 true 时才需要调 —— 见 getPublicConfig()。 */
export function getCaptcha() {
  return http.get<{ id: string; image: string }>('/public/captcha')
}

/** 匿名运行时开关 + 品牌信息(登录前可用)
 *  后端: GET /rapid-admin/v1/public/config
 *    captcha_login_enabled    → 密码登录 / 短信登录 / 短信重置密码
 *    captcha_sms_send_enabled → 发短信(防刷量闸门)
 *    app_name                 → 平台全名(登录页标题 / 顶栏品牌名)
 *    app_title                → 浏览器标签页标题
 *  只返白名单开关和品牌名,不返配置值。刻意不复用 configApi.list —— 那个走
 *  /platform/configs,挂在 Auth() 下,未登录拿不到。
 *  组合逻辑(短信流程取"或")留在前端 stores/publicConfig.ts,理由见那里。
 *
 *  ⚠️ app_name / app_title **可选**:老后端不返这两个字段(开源通用化之前的老版本),
 *    而且"没配 branding"时后端返的就是空串 —— 两种情况都回退到本地语言包。 */
export function getPublicConfig() {
  return http.get<{
    captcha_login_enabled: boolean
    captcha_sms_send_enabled: boolean
    app_name?: string
    app_title?: string
  }>('/public/config')
}

/* ====================== 短信登录 / 短信重置密码 ======================
 * 后端 handler 在 service 之前做验证码校验。
 * captcha 参数是**可选**的:对应开关为 false 时后端不校验,前端此时也不渲染
 * 验证码框,硬传 captcha.id 会在字段不存在时直接 TypeError。
 * 统一用 ?. + ?? '' —— 关与开都能跑通,后端对空串的处理是"跳过校验"。
 */
export function loginBySms(phone: string, smsCode: string, captcha?: { id: string; code: string }) {
  return http.post<LoginResponse>('/public/auth/login-sms', {
    phone,
    sms_code: smsCode,
    captcha_id: captcha?.id ?? '',
    captcha_code: captcha?.code ?? '',
  })
}
export function resetPasswordBySms(phone: string, smsCode: string, newPassword: string, captcha?: { id: string; code: string }) {
  return http.post<void>('/public/auth/reset-password-sms', {
    phone,
    sms_code: smsCode,
    new_password: newPassword,
    captcha_id: captcha?.id ?? '',
    captcha_code: captcha?.code ?? '',
  })
}

/* ====================== 登录二次验证 MFA(M150)====================== */

/**
 * 登录 MFA 步骤的"重发验证码"
 *
 * 后端 POST /rapid-admin/v1/public/auth/mfa/send
 *
 * ⚠️ **密码先验,才发码** —— 发码权 = "知道密码"。所以参数就是
 * username + password(前端 SHA-256 后的 hex,与 login 同一口径),
 * 不需要 captcha(后端靠 loginlock + IP 限流兜底,见 handler 注释)。
 *
 * 与 login 自动发码的区别:login 首次挑战会自动发一条;这条给
 * "码没收到 / 手滑关了弹窗"的用户手动重发用。
 */
export function sendMfaLoginCode(username: string, password: string) {
  return http.post<void>('/public/auth/mfa/send', { username, password })
}

/**
 * 开启登录二次验证
 *
 * 后端 POST /rapid-admin/v1/auth/mfa/enable
 *
 * @param smsCode 发到**账号登记手机号**上的 scene=mfa 验证码
 *                (用 sendAccountSms(SmsScene.MFA) 发)
 * @returns 最新的用户视图(mfa_enabled=true),前端整份替换 userInfo
 */
export function enableMfa(smsCode: string) {
  return http.post<UserInfo>('/auth/mfa/enable', { sms_code: smsCode })
}

/**
 * 关闭登录二次验证
 *
 * 后端 POST /rapid-admin/v1/auth/mfa/disable
 *
 * ⚠️ **关也要验证码** —— 不验码 = 偷到会话的人第一件事就是把 MFA 关掉。
 * @returns 最新的用户视图(mfa_enabled=false),前端整份替换 userInfo
 */
export function disableMfa(smsCode: string) {
  return http.post<UserInfo>('/auth/mfa/disable', { sms_code: smsCode })
}

/* ====================== 受保护接口 ====================== */

/** 获取当前用户信息(包含身份列表)
 *  2026-10-18 修复:后端 /auth/me 返 { user, resources },http wrapper 已经解掉一层
 *    (resp.data.data) 拿到的就是 { user: UserInfo, resources: SysResource[] }
 *    之前类型签名骗 TS 返 UserInfo,实际是 wrapper,setLogin({ userInfo: wrapper })
 *    导致 userInfo.is_platform_super_admin / userInfo.identities 永远 undefined
 *    → 修复:显式标注返 { user, resources },caller 自行解 user
 */
export function getCurrentUser() {
  return http.get<{ user: UserInfo; resources: unknown[] }>('/auth/me')
}

/** 登出 */
export function logout() {
  return http.post<void>('/auth/logout')
}

/** 切换身份
 *
 *  ⚠️ 2026-10-21:返回类型此前声明为 `{ token, identity }`,而后端从来不返
 *  `identity` 字段 —— 它返的是完整的 AuthTokens(user.identities 数组)。
 *  于是 IdentitySwitcher 里 `resp.identity ?? target` 永远走 ?? 分支,
 *  一直靠调用方传进来的 target 兜底。声明在骗人,调用方在骗自己。
 *  现在按真实契约声明:用 user.current_identity_id 定位,别再猜。 */
export function switchIdentity(identityId: string, smsCode?: string) {
  return http.post<LoginResponse>(
    '/auth/switch-identity',
    { identity_id: identityId, sms_code: smsCode }
  )
}

/** 修改密码(用户自助) */
export function changePassword(oldPwd: string, newPwd: string) {
  return http.post<void>('/auth/password', { old_password: oldPwd, new_password: newPwd })
}

/** 重置密码(管理员操作) */
export function resetPassword(userId: string, newPwd: string) {
  return http.post<void>('/auth/password/reset', { user_id: userId, new_password: newPwd })
}

/**
 * 更新个人资料
 * - 仅本人调用,后端不做 IDOR 校验
 * - 字段缺失表示"不改";空串表示"改空"(后端会拒绝 nickname/realName 空串)
 * - phone 变更不在本接口范围
 * - ⚠️ **没有 avatar** —— 头像是**上传**出来的,见 uploadAvatar
 * @see server/internal/service/auth_impl.go UpdateProfile
 */
export function updateProfile(dto: UpdateProfileRequest) {
  return http.patch<UserInfo>('/auth/profile', dto)
}

/**
 * 上传头像(M124)
 *
 * 后端 POST /rapid-admin/v1/auth/avatar,multipart/form-data,字段名固定 `file`。
 * 落盘走文件系统(biz = "avatar",落在**自己 id 底下**),返回的是**稳定直链**。
 *
 * ⚠️ **不要手写 `Content-Type: multipart/form-data`** ——
 * 少了 axios 自动补的 boundary,后端 `c.FormFile("file")` 解析失败,
 * 报错是"缺少文件字段 file" —— 看着像字段名写错了,实际是 boundary 丢了。
 *
 * ⚠️ **前端只做"大小 + 类型"的友好拦截,真正的闸在服务端**
 * (服务端还查文件头魔数,前端查不了)。
 */
export function uploadAvatar(file: File) {
  const form = new FormData()
  form.append('file', file, file.name)
  return http.post<AvatarView>('/auth/avatar', form, { timeout: 60000 })
}

/* ====================== 会话与设备管理 ====================== */

/**
 * 我的活跃会话 + 历史登录记录(SRS §2.2.1 #6)
 *
 * 只能看自己的 —— 后端这个接口**不接 userId**,没有可越权的入口。
 *
 * @param page 历史记录那部分的页码(活跃设备不分页)
 */
export function listSessions(params?: { page?: number; size?: number }) {
  return http.get<SessionOverview>('/auth/sessions', params as Record<string, unknown>)
}

/**
 * 强制下线指定会话(必须属于我自己 —— 归属校验在后端 service 层)
 *
 * 传空数组 = 什么都不做(后端返回 revoked: 0,不是错误)。
 *
 * ⚠️ 别把"我当前这条"传进去:后端会跳过它,而且是**故意的** ——
 * 自己这条要走 /auth/logout(它连带把 access 的 jti 也吊了,语义完整)。
 *
 * @returns 真正被吊销的条数。传进去的 sid 可能已失效,这个数是如实反映的。
 */
export function revokeSessions(sessionIds: string[]) {
  return http.post<{ revoked: number }>('/auth/sessions/revoke', {
    session_ids: sessionIds,
  })
}
