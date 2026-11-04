/**
 * 用户 / 身份 / 角色 类型
 * 与后端 §5 / §13.3 对齐
 */
// ⚠️ 2026-10-21 全仓 ID 改字符串
//
// 后端主键是 sonyflake 19 位数(~9.5e16),超出 JS 的 2^53 安全整数上限。
// JSON.parse 会静默截断末 6 位 —— 回传后端必然 404(身份切换就是这么死的),
// 而且**不报错**:拿到的是一个"看起来合法"的错 ID,排查成本极高。
//
// 所以 wire 上所有 ID 一律是 string。Go 侧用 `json:",string"` 保持 uint64
// 不变(slice 除外,内置选项对数组无效,改用 jsonid.IDList)。
// 本文件里的 id / *_id 一律 string,**不要**再做算术或隐式转换。

/** 身份(用户可以在多个项目中拥有不同角色) */
export interface Identity {
  id: string
  role_code: string
  role_name: string
  project_id: string
  project_name: string
  /** 资源码列表 ['user:list', 'user:create', ...] */
  permissions: string[]
  valid_from: string
  valid_to: string
  is_active: boolean
  is_primary: boolean
}

/** 用户信息
 *
 *  ⚠️ 这个形状对应后端 `service.CurrentUserView`(M113 起是**唯一真源**):
 *  login / refresh / switch-identity / auth/me / **auth/profile** 五个端点
 *  返的都是它。改字段要 server + web + doc 同 commit。
 *
 *  ⚠️ `email` 是**真邮箱**,不是掩码串 —— 后端 M113 起不再脱敏自己的邮箱。
 *  以前脱敏(`z******@qq.com`),而个人中心把这个掩码值回填进可编辑的输入框,
 *  用户改个昵称点保存就把掩码串写回库 ⇒ 真邮箱被覆盖。
 *  (手机号仍然脱敏:它的口径跟邮箱不一样。)
 */
export interface UserInfo {
  id: string
  username: string
  nickname: string
  /** 真实姓名(M113 新增;此前后端视图里压根没这个字段) */
  real_name?: string
  /** 已脱敏:138****1234 */
  phone: string
  email?: string
  avatar: string
  identities: Identity[]
  current_identity_id: string
  /**
   * **当前生效身份**的角色码(M149-b 新增;服务端权威值)
   *
   * ⚠️ 前端以前是**猜**的:`identities.find(i => i.id === current_identity_id)`
   *   ⇒ 猜错在两处:
   *   ① 身份被管理员改过(后端全量重建身份行 ⇒ ID 全变)⇒ 旧 session 的
   *      `current_identity_id` 在列表里**找不到** ⇒ roleCodes 变空
   *      ⇒ 角色门控(AuthButton 的 roleAnyOf)整个失效;
   *   ② 库里角色改了但 token 还是旧角色 ⇒ 前端显示新角色、后端按旧角色判。
   *
   * ⇒ 服务端直接下发(= token claims 里那个 role_code,
   *   **与 RBAC 中间件判权限同一个来源**)⇒ 界面显示的 = 实际能做的。
   *
   * 可选:老后端不返这个字段 ⇒ 前端回退到"找当前身份"的老办法。
   */
  current_role_code?: string
  /** 平台超管标记 */
  is_platform_super_admin?: boolean
  /** 平台运维标记 */
  is_platform_ops?: boolean
  /**
   * 登录二次验证开关(M150)。五个端点(login/refresh/switch/me/mfa 开关)
   * 都下发最新值,前端不自己维护这个状态 —— 整份替换 userInfo 即可。
   */
  mfa_enabled?: boolean
}

/** 登录请求 */
export interface LoginRequest {
  username: string
  password: string
  /**
   * MFA 短信验证码(M150)。开启二次验证的账号:第一次不带 → 后端返
   * 20109(MFA_REQUIRED)并自动发码;填上码**连同密码一起重发**(挑战无状态)。
   *
   * ⚠️ 旧字段 `sms_code` 已删:后端 LoginRequest 从来没有过 sms_code,
   * 那是个前端自造的死字段。
   */
  mfa_code?: string
  /** 图形验证码 id (来自后端 CaptchaGenerateResult.id)
   *  对应开关为 false 时后端不校验,传空串即可(见 api/auth.ts getPublicConfig) */
  captcha_id: string
  /** 图形验证码值 (用户输入),同上 */
  captcha_code: string
}

/** 登录 / 续期 / 切换身份的响应 —— 三个端点返的都是这一个形状
 *
 *  ⚠️ access token 的字段名是 `token` 不是 `access_token`(后端 service.AuthTokens
 *  的 json tag)。前端曾按 access_token 取值,结果自动续期拿到 undefined、发
 *  `Bearer undefined`、401 把人踢回登录页。
 *
 *  ⚠️ 2026-10-21:后端把 refresh 的 user 从 *model.SysUser 换成统一的
 *  CurrentUserView —— 此前那条路径**不带 identities**,用户续期之后身份列表
 *  静默消失(切换身份入口突然没了)。现在三个端点同构,user.identities 永远在。
 */
export interface LoginResponse {
  token: string
  refresh_token: string
  /** access token 剩余秒数 */
  expires_in: number
  /** refresh token 剩余秒数 —— 2026-10-21 新增,前端 TTL 全靠它 */
  refresh_expires_in: number
  user: UserInfo
}

/** 切换身份请求 */
export interface SwitchIdentityRequest {
  identity_id: string
}

/* ⚠️ **M113(2026-10-29)删掉了 `ChangePasswordRequest`**
 *
 * 那个类型写的是 `{ old_password, new_password, sms_code }` ——
 * **第三个字段从来没存在过**:`POST /auth/password` 从上线起就只认
 * old_password + new_password(旧密码由 service 层 bcrypt 比对),
 * 前端那道"短信二次验证"是**纯前端的门**,而且是**坏的**(短信压根发不出去)。
 *
 * 这个类型**零调用方**,留着只会骗下一个人
 * ("改密要发短信吗?看类型是有 sms_code 的……")。
 * ⇒ 需求原文见技术方案 §7.7.4.4。
 */

/**
 * 更新个人资料请求
 * - 字段均为可选;只传需要改的字段
 * - 后端 service.UpdateProfileDTO 用 *string 区分 "不改"(undefined) 与 "改空串"("")
 * - phone 变更需走单独的 /auth/phone 端点 + SMS 二次验证(本期范围外)
 *
 * ⚠️ **字段名是 snake_case(`real_name`),不是 `realName`**
 *   —— 技术方案 §7.7.4.6 那个接口契约此前写的是 `realName`,
 *   与实现(后端 DTO 的 json tag)对不上,已按实现订正(见 M113)。
 *
 * ⚠️ **这里没有 `avatar`**(M124 删掉了)—— 头像只能**上传**
 *   (`POST /auth/avatar`,multipart),不能在这里贴一个任意 URL。
 *   贴外链的头像会渲染在**别人的浏览器**里(管理端用户列表 / 顶栏),
 *   那等于一个跟踪像素。类型层面不给这个字段 = 编译期就写不出来。
 */
export interface UpdateProfileRequest {
  nickname?: string
  email?: string
  real_name?: string
}

/**
 * 头像上传结果(后端 service.AvatarView)
 *
 * ⚠️ `url` 是**稳定直链**(不是签名 URL)—— 签名 URL 过期头像就裂了。
 */
export interface AvatarView {
  url: string
  file_id: string
  file_name: string
  file_size: number
}

/**
 * 会话 / 设备视图(SRS §2.2.1 #6)
 *
 * 对应后端 service.SessionView(见 server/internal/service/auth_sessions.go)。
 * 一次登录 = 一条会话 = 一行 sys_login_log(带 session_id)。
 */
export interface SessionView {
  /** 会话 id(sid)。空串 = 登录失败的记录(失败时压根没有会话) */
  session_id: string
  /** User-Agent 原文 */
  device: string
  browser: string
  os: string
  ip: string
  location: string
  /** 1 成功 / 0 失败 */
  status: number
  /** 登录结果说明("ok" / "密码错误" / ...) */
  message: string
  /** 这一次登录的时刻 */
  login_at: string | null
  /** 最后一次确认这条会话还活着的时刻(续期时更新);null = 还没续期过 */
  last_active_at: string | null
  /** 被登出 / 强制下线的时刻;null = 还没下线 */
  logout_at: string | null
  /** 这条是不是**我当前这条**会话(前端用它标"本机") */
  is_current: boolean
  /** 是否已经自然过期(还能看,但已经不算活跃设备) */
  expired: boolean
}

/** 历史登录记录分页 */
export interface SessionPage {
  list: SessionView[]
  total: number
  page: number
  size: number
}

/** 会话总览:活跃设备 + 历史登录记录 */
export interface SessionOverview {
  active: SessionView[]
  history: SessionPage
}
