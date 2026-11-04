/**
 * 个人中心 —— /profile
 *
 * Phase 5 M7 框架 UI
 *
 * 模块:
 *   - 左侧用户卡片(头像 + 账号 + 角色 + 部门)
 *   - 右侧 Tabs:
 *       1. 基本信息 — 头像/昵称/邮箱/真实姓名(可编辑)
 *       2. 安全设置 — 改密 / 换绑手机(C5 真实现) / MFA 说明
 *       3. 我的身份 — 列出 identities,展示当前主身份(切换走 IdentitySwitcher)
 *       4. 登录设备 — 活跃设备 + 强制下线 + 历史登录记录(会话与设备管理)
 *
 * ⚠️ **M113(2026-10-29)的三处修正**
 *
 *  1. **改密不再要短信验证码** —— 弹窗直接就是"旧密码 / 新密码 / 确认新密码"。
 *     以前这个弹窗**第一屏是短信验证**,验证通过才露出密码表单。
 *     而那条短信链路"从来没能发出去过短信"(SmsCodeGuard 那段注释自己写了)
 *     ⇒ **个人中心实际上改不了密码**。
 *     后端 `POST /auth/password` 从来就只认 `old_password` + `new_password`
 *     (旧密码校验在 service 里),短信这一层是**纯前端的门**,删掉不放松服务端契约。
 *
 *  2. **基本信息表单回填的是真值** —— 以前 `real_name` 读的是
 *     `(userInfo as any)?.real_name`(后端视图压根没这个字段 ⇒ 恒 undefined)
 *     而 `email` 读的是**脱敏后的** `z******@qq.com` ⇒
 *     用户改个昵称点保存,后端就把掩码串和空串一起写回库。
 *     现在后端视图带真实 `real_name`、邮箱不再脱敏(M113 同 commit 改 server)。
 *
 *  3. **头像有默认头像** —— 走 `UserAvatar` 组件:
 *     没有头像 / 图片加载失败 ⇒ 回落成首字母圆牌,而不是浏览器裂图。
 *
 *  4. **「编辑」按钮以前是被自己禁用的** —— 这是"为什么不能修改个人基本信息"
 *     的**真凶**(不是 RBAC、不是后端契约):
 *     antd 5 的 `<Form disabled>` 用 `DisabledContextProvider` 包住子树,
 *     而 antd 5 的 `<Button>` 会从 `DisabledContext` 取禁用态
 *     (`mergedDisabled = customDisabled ?? contextDisabled`)。
 *     「编辑 / 保存 / 取消」三个按钮原本**写在 Form 里面**
 *     ⇒ 只读态(Form disabled)时「编辑」按钮自己也是灰的 ⇒ 点不动
 *     ⇒ 永远进不了编辑态。
 *     现在按钮移到 `<Form>` 外面,`disabled` 只作用于输入框。
 *
 *  5. **只读态也要显示当前值** —— 用户报"没有点击编辑时候,
 *     为什么基本信息里面几个输入框都不显示内容"。
 *     表单值以前**只在点「编辑」那一下**被写入,而 `<Form>` 上没有
 *     `initialValues` ⇒ 首帧是空的 ⇒ 只读态三个框全空。
 *     现在用 effect 把 `userInfo` 的真值同步进表单(编辑态不写,
 *     免得 token 续期换掉 store 时把用户正在改的内容冲掉)。
 *
 *  6. **「换绑手机」不再是禁用按钮**(2026-10-29 C5)——
 *     以前这里是一个 disabled 的 Button + tooltip「Phase 6 真实化」。
 *     现在是真流程:填**新**手机号 → 验证码发到**新号** → 填码 → 确认换绑。
 *
 *     ⚠️ **验证码发到新号,不是旧号** —— 这是后端契约决定的:
 *     换绑要证明的是"用户占着**新**号",而服务端手上只有旧号。
 *     用旧号验证只能证明"用户还占着旧号",证明不了换绑要证明的那件事。
 *     (SRS §2.2.3 的原话:"输入新手机号并完成新号验证")
 *
 *     成功后拿后端返的 CurrentUserView **整份替换** store 的 userInfo ——
 *     左边那张卡片的脱敏手机号要立刻变成新号。
 *
 *  7. **M124(2026-10-30):头像能传了**
 *     以前这页**根本没有头像的入口** —— 用户报"个人中心用户头像现在不能设置"。
 *     根因不是后端不认:`PATCH /auth/profile` 一直收一个 `avatar` 字段(任意 URL),
 *     但**前端从来没提供过这个输入框**,而后端那条路本身还带一个安全问题
 *     (贴外链的头像会渲染在**别人的浏览器**里 ⇒ 那是一个跟踪像素)。
 *     ⇒ 服务端删掉了 `UpdateProfileDTO.Avatar`,新增 `POST /auth/avatar`
 *       (multipart,字段名 `file`),落盘走文件系统(biz = "avatar",
 *       存在**自己 id 底下**),返回**稳定直链**。
 *     前端在这张卡片上:鼠标移到大头像上 → 浮出半透明遮罩 + 铅笔图标,
 *     **点头像区域直接打开文件选择框**(不另外放一个按钮 —— 用户 2026-10-30
 *     明确要求"界面更清爽")。上传成功后 `setUserInfo`
 *     ⇒ 个人中心 + 顶栏 + 用户管理列表三处一起刷新。
 */
import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  Card,
  Tabs,
  Form,
  Input,
  Button,
  Row,
  Col,
  Tag,
  Space,
  App,
  Modal,
  Descriptions,
  Empty,
  Tooltip,
  Alert,
  Upload,
  type UploadProps,
} from 'antd'
import {
  EditOutlined,
  LockOutlined,
  SafetyCertificateOutlined,
  HistoryOutlined,
  CheckCircleOutlined,
  LoadingOutlined,
} from '@ant-design/icons'
import { PageHeader } from '@/components/common/PageHeader'
import { SensitiveField } from '@/components/common/SensitiveField'
import { UserAvatar } from '@/components/common/UserAvatar'
import { useI18n } from '@/locales'
import { useAuth } from '@/hooks/useAuth'
import { useUserStore } from '@/stores/user'
import {
  bindPhone,
  changePassword,
  disableMfa,
  enableMfa,
  sendAccountSms,
  sendBindPhoneCode,
  updateProfile,
  uploadAvatar,
} from '@/api/auth'
import { SmsScene } from '@/constants/sms'
import SessionPanel from './SessionPanel'

/**
 * 换绑手机号:发码后的冷却秒数
 *
 * ⚠️ **必须跟后端的冷却一致**(smslimit per phone+scene = 60s)。
 * 前端只是"友好的拦截",真拦在后端 —— 前端写 30s 的话,
 * 用户在第 31 秒再点一次,后端照样返「短信发送太频繁」。
 */
const BIND_CODE_COOLDOWN = 60

/**
 * 头像上传的前端约束(M124)
 *
 * ⚠️ **这两条只是"友好拦截",不是安全边界**
 *   真正的闸在服务端:
 *     · 大小上限 5 MB(server/internal/service/user_avatar.go maxAvatarBytes)
 *     · 类型白名单按**扩展名 + 文件头魔数**双重判定
 *       (只按浏览器上报的 Content-Type 判定是能被伪造的)
 *   前端拦的是"用户明显传错了"(拍成了 PDF / 传了几十 MB 的原图),
 *   目的是**当场给一句人话**,而不是传上去等后端报错。
 *
 * ⚠️ **两边的数字必须一致** —— 前端放行、后端拒,用户看到的是
 *   "点了没反应";前端拦、后端放,用户看到的是"莫名其妙"。
 */
const AVATAR_MAX_BYTES = 5 * 1024 * 1024

/** 允许的 MIME(与后端 allowedAvatarTypes 对齐;svg 不在其中 —— svg 能带脚本) */
const AVATAR_ACCEPT_TYPES = [
  'image/png',
  'image/jpeg',
  'image/jpg',
  'image/gif',
  'image/webp',
  'image/bmp',
]

const ProfilePage: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  const { userInfo, logout } = useAuth()
  const setUserInfo = useUserStore((s) => s.setUserInfo)

  /* ----- Tab 1: 基本信息编辑 ----- */
  const [editingProfile, setEditingProfile] = useState(false)
  const [profileForm] = Form.useForm()
  const [savingProfile, setSavingProfile] = useState(false)

  /**
   * 表单要显示的**当前值**(= 用户资料真值)
   *
   * ⚠️ 这三个字段的来源在 M113 之前全是坏的:
   *   · `real_name` 读的是后端视图压根没有的字段 ⇒ 恒 undefined
   *   · `email` 读的是**脱敏后的** "z\*\*\*\*\*\*\*@qq.com" ⇒ 掩码串会被写回库
   * 现在后端视图带真实 `real_name`、邮箱不再脱敏(M113 同 commit 改 server)。
   */
  const profileValues = useMemo(
    () => ({
      nickname: userInfo?.nickname ?? '',
      email: userInfo?.email ?? '',
      real_name: userInfo?.real_name ?? '',
    }),
    [userInfo],
  )

  /**
   * ⚠️ **M113-b(2026-10-29):只读态也要显示当前值**
   *
   * 用户报:"没有点击编辑时候,为什么基本信息里面几个输入框都不显示内容"。
   *
   * 真凶:`profileForm` 的值**只在 `startEditProfile`(点「编辑」那一下)被写入**,
   * 而 `<Form>` 上**没有 `initialValues`**
   * ⇒ **首帧表单里是空的** ⇒ 只读态三个输入框全是空的
   * ⇒ 用户看到的是"这个账号没有昵称 / 没有真实姓名 / 没有邮箱",
   *    而不是"这里显示的是我的资料"。
   *
   * ⚠️ **为什么不用 `initialValues` 解决**:
   *   antd 的 `initialValues` **只在 Form 挂载那一刻读一次**,
   *   之后 userInfo 到了 / 变了都**不会再读**。
   *   而 `userInfo` 是登录后由 `/auth/me` **异步**灌进 store 的 ——
   *   F5 刷新个人中心时,首帧它多半还是 undefined
   *   ⇒ `initialValues` 那一刻拿到的就是空。
   *   (用 `initialValues` 修这个 bug 会得到"刷新一次就好了、再刷又空了"。)
   *
   * ⇒ 改成 **effect 同步**:userInfo 变化时把真值写进表单。
   *   ⚠️ **编辑态不写** —— 否则 token 续期把 store 换掉(userInfo 换新对象)时,
   *     会把**用户正在改的内容冲掉**。
   *   ⚠️ `setFieldsValue` 走的是 rc-field-form 的 `source: 'external'` 分支
   *     (rc-field-form/lib/Field.js:148):它**只清错、不触发校验**
   *     ⇒ 只读态不会冒出一片红色的"必填"提示。
   */
  useEffect(() => {
    if (editingProfile) return
    profileForm.setFieldsValue(profileValues)
  }, [editingProfile, profileValues, profileForm])

  const startEditProfile = () => {
    // 每次进编辑态都重新灌一遍真值:
    // ① 只读态已经显示着这些值了,这里保证"编辑态 = 当前真值",不会带出上一次的残留
    // ② 用户在只读态改不了东西,所以这里灌的就是 userInfo 本身
    profileForm.setFieldsValue(profileValues)
    setEditingProfile(true)
  }

  const saveProfile = async () => {
    let vals: { nickname?: string; email?: string; real_name?: string }
    try {
      vals = await profileForm.validateFields()
    } catch {
      // 校验没过:字段自己会标红,这里不弹 toast(免得两条提示叠在一起)
      return
    }
    setSavingProfile(true)
    try {
      const updated = await updateProfile({
        nickname: vals.nickname,
        real_name: vals.real_name,
        // ⚠️ 空邮箱 = "不改"(后端 `dto.Email != nil && *dto.Email != ""` 会跳过)
        //   ⇒ 这里直接不传,免得发一个空串过去让人以为清空了。
        ...(vals.email ? { email: vals.email } : {}),
      })
      // 后端 M113 起返的是 CurrentUserView(带 identities / 超管标记 / 手机号),
      // 所以这里可以整份替换 —— 以前返的是裸 SysUser,一替换就把这些全洗空了。
      setUserInfo(updated)
      message.success(t('common.success'))
      setEditingProfile(false)
    } catch {
      // 错误提示由 http 层统一弹,这里不重复弹
    } finally {
      setSavingProfile(false)
    }
  }

  /* ----- 头像上传(M124)----- */
  const [uploadingAvatar, setUploadingAvatar] = useState(false)
  /** 鼠标是否悬在头像上 —— 悬上去时头像上浮出"点击更换"的遮罩 */
  const [hoverAvatar, setHoverAvatar] = useState(false)

  /**
   * 上传并设置自己的头像
   *
   * ⚠️ **上传成功后必须更新 store** ——
   *   左边这张卡片读的是 store 的 `userInfo.avatar`,
   *   顶栏那个头像读的也是同一份 ⇒ 一次 `setUserInfo` 把**两处**一起刷了。
   *   忘了刷 ⇒ 个人中心显示新头像、顶栏还是旧的(或者反过来)。
   *
   * ⚠️ **只更新 avatar 这一个字段**,不要整份换:
   *   `setUserInfo` 是**整份替换**语义(见 stores/user.ts)——
   *   把后端返的 UserView 整份灌进去会把 identities / 超管标记一起换掉。
   *   这里服务端只返 AvatarView(不含 user),所以只能自己在前端拼。
   */
  const handleAvatarUpload = async (file: File) => {
    if (file.size > AVATAR_MAX_BYTES) {
      message.error(t('page.profile.avatarTooLarge'))
      return
    }
    if (!AVATAR_ACCEPT_TYPES.includes(file.type)) {
      message.error(t('page.profile.avatarTypeError'))
      return
    }
    setUploadingAvatar(true)
    try {
      const view = await uploadAvatar(file)
      if (userInfo) {
        setUserInfo({ ...userInfo, avatar: view.url })
      }
      message.success(t('page.profile.avatarSuccess'))
    } catch {
      // 错误提示由 http 层统一弹,这里不重复弹
    } finally {
      setUploadingAvatar(false)
    }
  }

  /**
   * antd Upload 的配置
   *
   * ⚠️ **`beforeUpload` 返回 false** = 不走 antd 自己的上传通道,
   *   我们在 `handleAvatarUpload` 里自己发 —— 因为要拿到 `userInfo`、
   *   要控制提示文案、要上传完刷新 store。
   *   (返回 false 时 antd 不会发请求,但**会把文件塞进它内部的 fileList**;
   *   `showUploadList: false` 让它不显示 —— 那个列表只用来显示进度条,
   *   我们不用。)
   *
   * ⚠️ **不要手写 `Content-Type: multipart/form-data`** ——
   *   少了 axios 自动补的 boundary,后端解析不出来,
   *   报错是「缺少文件字段 file」—— 看着像字段名写错了,实际是 boundary 丢了。
   */
  const avatarUploadProps: UploadProps = {
    accept: AVATAR_ACCEPT_TYPES.join(','),
    showUploadList: false,
    /**
     * ⚠️ **`hasControlInside={false}`** ——
     *   antd 的 Upload 默认 `hasControlInside = true`,意思是
     *   "我的 children 里已经有一个控件了,别再给外层加 role=button / tabIndex"。
     *   那是给 `<Upload><Button/></Upload>` 这种写法准备的。
     *   我们的 children 是**一张头像**(不是控件)⇒ 必须传 false,
     *   rc-upload 才会给包裹 span 加 `role="button"` + `tabIndex=0`
     *   ⇒ 读屏软件当它是个按钮,键盘 Tab 过去按 Enter 也能打开文件框
     *   (rc-upload 的 onKeyDown)。
     */
    hasControlInside: false,
    beforeUpload: (file) => {
      void handleAvatarUpload(file)
      return false
    },
  }

  /* ----- Tab 2: 改密 ----- */
  const [changePwdOpen, setChangePwdOpen] = useState(false)
  const [pwdForm] = Form.useForm()
  const [submitting, setSubmitting] = useState(false)

  /**
   * 改密提交
   *
   * ⚠️ **M113:不再需要短信验证码**
   *   以前第一屏是 SmsCodeGuard(发短信 + 校验),通过后才露出密码表单。
   *   而那条短信链路实际**发不出去**(SmsCodeGuard 注释里写着"这个弹窗从来没能
   *   发出去过短信")⇒ 个人中心实际上改不了密码。
   *   后端 `POST /auth/password` 本来就只认 old_password + new_password,
   *   旧密码由 service 校验 ⇒ 去掉前端这道门不放松任何服务端约束。
   */
  const handleChangePwdOk = async () => {
    let vals: { old_password: string; new_password: string; confirm: string }
    try {
      vals = await pwdForm.validateFields()
    } catch {
      return
    }
    setSubmitting(true)
    try {
      await changePassword(vals.old_password, vals.new_password)
      message.success(t('page.profile.pwdChanged'))
      setChangePwdOpen(false)
      setTimeout(() => {
        logout()
        window.location.href = '/login?reason=password-changed'
      }, 800)
    } catch {
      // 错误提示由 http 层统一弹
    } finally {
      setSubmitting(false)
    }
  }

  /* ----- Tab 2: 换绑手机号(C5,2026-10-29)----- */
  const [bindOpen, setBindOpen] = useState(false)
  const [bindForm] = Form.useForm()
  const [bindSubmitting, setBindSubmitting] = useState(false)
  const [bindSending, setBindSending] = useState(false)
  const [bindCountdown, setBindCountdown] = useState(0)

  /**
   * ⚠️ **倒计时的 timer 必须能被清掉**(同 SmsCodeGuard 的做法)
   *
   * 组件卸载之后那个 timer 还在每秒跑一次 setState —— 内存泄漏 + 卸载后 setState。
   * 所以从一开始就管起来:start 前先 stop,卸载时 cleanup。
   */
  const bindTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const stopBindCountdown = () => {
    if (bindTimerRef.current) {
      clearInterval(bindTimerRef.current)
      bindTimerRef.current = null
    }
  }

  // 卸载时清 timer
  useEffect(() => stopBindCountdown, [])

  const startBindCountdown = () => {
    stopBindCountdown()
    setBindCountdown(BIND_CODE_COOLDOWN)
    bindTimerRef.current = setInterval(() => {
      setBindCountdown((c) => {
        if (c <= 1) {
          stopBindCountdown()
          return 0
        }
        return c - 1
      })
    }, 1000)
  }

  /* ----- Tab 2: 登录二次验证 MFA(M150)----- */
  //
  // 流程与换绑手机号同构:sendAccountSms(scene=mfa)把码发到**账号登记手机号**
  // (前端手上只有脱敏号,发码权在服务端),用户填码,enableMfa/disableMfa
  // 带码提交,后端消费码并返最新的 CurrentUserView ⇒ 整份替换 userInfo。
  //
  // ⚠️ **关 MFA 也要码** —— 不验码 = 偷到会话的人第一件事就是把 MFA 关掉,
  // 之后用偷来的密码无阻登录。开关动作都验一次"持机"。
  const [mfaModalOpen, setMfaModalOpen] = useState(false)
  /** enable = 开启;disable = 关闭(同一个弹窗,文案和提交动作按它分流) */
  const [mfaModalMode, setMfaModalMode] = useState<'enable' | 'disable'>('enable')
  const [mfaForm] = Form.useForm<{ code: string }>()
  const [mfaSubmitting, setMfaSubmitting] = useState(false)
  const [mfaSending, setMfaSending] = useState(false)
  const [mfaCountdown, setMfaCountdown] = useState(0)
  const mfaTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const stopMfaCountdown = () => {
    if (mfaTimerRef.current) {
      clearInterval(mfaTimerRef.current)
      mfaTimerRef.current = null
    }
  }

  // 卸载时清 timer(同 bindCountdown 的做法)
  useEffect(() => stopMfaCountdown, [])

  const startMfaCountdown = () => {
    stopMfaCountdown()
    setMfaCountdown(BIND_CODE_COOLDOWN)
    mfaTimerRef.current = setInterval(() => {
      setMfaCountdown((c) => {
        if (c <= 1) {
          stopMfaCountdown()
          return 0
        }
        return c - 1
      })
    }, 1000)
  }

  const openMfaModal = (mode: 'enable' | 'disable') => {
    mfaForm.resetFields()
    stopMfaCountdown()
    setMfaCountdown(0)
    setMfaModalMode(mode)
    setMfaModalOpen(true)
  }

  const closeMfaModal = () => {
    setMfaModalOpen(false)
    mfaForm.resetFields()
    stopMfaCountdown()
    setMfaCountdown(0)
  }

  /** 发码到账号登记手机号(scene=mfa;手机号由服务端取,前端不传) */
  const handleSendMfaCode = async () => {
    setMfaSending(true)
    try {
      await sendAccountSms(SmsScene.MFA)
      message.success(t('sms.codeSentSimple'))
      startMfaCountdown()
    } catch {
      // 错误提示由 http 层统一弹(没绑手机号会在这里报 InvalidParam)
    } finally {
      setMfaSending(false)
    }
  }

  /** 确认开/关 MFA */
  const handleMfaOk = async () => {
    let vals: { code: string }
    try {
      vals = await mfaForm.validateFields()
    } catch {
      return
    }
    setMfaSubmitting(true)
    try {
      const updated =
        mfaModalMode === 'enable'
          ? await enableMfa(vals.code)
          : await disableMfa(vals.code)
      // 返的是最新的 CurrentUserView ⇒ 整份替换,卡片状态立刻翻面
      setUserInfo(updated)
      message.success(t(mfaModalMode === 'enable' ? 'page.profile.mfaEnabled' : 'page.profile.mfaDisabled'))
      closeMfaModal()
    } catch {
      // 错误提示由 http 层统一弹
    } finally {
      setMfaSubmitting(false)
    }
  }

  /** 打开换绑弹窗(每次打开都清掉上一次的残留) */
  const openBindModal = () => {
    bindForm.resetFields()
    stopBindCountdown()
    setBindCountdown(0)
    setBindOpen(true)
  }

  const closeBindModal = () => {
    setBindOpen(false)
    bindForm.resetFields()
    stopBindCountdown()
    setBindCountdown(0)
  }

  /**
   * 发验证码(发给**新号**)
   *
   * ⚠️ 只校验 `phone` 一个字段 —— `validateFields(['phone'])`。
   *   用 `validateFields()`(全部字段)的话,还没填验证码时点「发送验证码」
   *   会被验证码那条规则拦住,提示"请输入验证码" —— 而用户此刻
   *   正想发码,这个提示驴唇不对马嘴。
   */
  const handleSendBindCode = async () => {
    let phone: string
    try {
      const vals = (await bindForm.validateFields(['phone'])) as { phone: string }
      phone = vals.phone
    } catch {
      // 手机号没填 / 格式不对:字段自己标红,这里不弹 toast
      return
    }
    setBindSending(true)
    try {
      await sendBindPhoneCode(phone)
      message.success(t('sms.codeSentSimple'))
      startBindCountdown()
    } catch {
      // 错误提示由 http 层统一弹,这里不重复弹
    } finally {
      setBindSending(false)
    }
  }

  /** 确认换绑 */
  const handleBindPhoneOk = async () => {
    let vals: { phone: string; code: string }
    try {
      vals = (await bindForm.validateFields()) as { phone: string; code: string }
    } catch {
      // 校验没过:字段自己会标红,这里不弹 toast
      return
    }
    setBindSubmitting(true)
    try {
      const updated = await bindPhone(vals.phone, vals.code)
      // 后端返的是最新的 CurrentUserView(带 identities / 超管标记 / 手机号)
      // ⇒ 整份替换,左边卡片上的脱敏手机号立刻变成新号
      setUserInfo(updated)
      message.success(t('page.profile.bindPhoneSuccess'))
      closeBindModal()
    } catch {
      // 错误提示由 http 层统一弹
    } finally {
      setBindSubmitting(false)
    }
  }

  if (!userInfo) {
    return <Empty description={t('page.profile.notLoggedIn')} />
  }

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        minHeight: 0,
      }}
    >
      {/* 页头:固定高度,不参与伸缩 */}
      <div style={{ flexShrink: 0 }}>
        <PageHeader description={t('page.profile.description')} />
      </div>

      {/*
        ⚠️⚠️ **M113(2026-10-29):整页的高度链**
        用户报"个人中心整个界面存在竖向滚动,内容看不全"。
        这一页以前**没有高度链** —— Row → Col → Card → Tabs → TabPane
        每一层都是"内容撑开",内容多高页面就多高,于是永远有滚动条。

        现在补上 ManagementTable 那条同款的链:
          根 div (height:100% / flex column)
            → Row (flex:1 / min-height:0)
              → Col (min-height:0)
                → Card (maxHeight:100% / body flex:1 / min-height:0)
                  → Tabs (flex:1 / min-height:0)
                    → .ant-tabs-content-holder (flex:1 1 0% / overflow:hidden)

        ⚠️ **minHeight:0 是 flex 子项能收缩的前提** —— 没有它,
        flex 子项的 min-height 默认是 auto(= 内容高度),内容多高就多高,
        整条链会被内容顶开(overflow:hidden 也拦不住,因为是它自己变高了)。

        ⚠️ **两张 Card 都用 `maxHeight: '100%'`(2026-10-29 用户手改)**
          · `height:100%` ⇒ 卡片被拉成整列高,内容不够时底下留一大片空白
            (用户报"左卡片显示不对"就是它)
          · `maxHeight:100%` ⇒ 卡片贴着内容高度;内容超过上限时才被截断,
            而 body 是被截断那个盒子的**直接子元素** ⇒ 它自己滚,内容不会丢

        ⚠️⚠️ **具体尺寸不再由代码钉死 —— 归用户手调**
        用户 2026-10-29 明确:"这个界面大小那些等下我手动改样式"。
        ⇒ 代码这边只保证**结构**对(min-height:0 / flex 链 / overflow 归属),
          不替用户定高度,也**不在注释里写"实测数字"**。

        ⚠️ **之前那组数字是复制件上量出来的,不能当依据(已删)**
        之前写过"列高 400px / holder 368 / 底下 232px 被裁掉"这类数字,
        那是**手搭的 flex 链复制件**(临时 HTML)上的测量结果 —— 不是真实页面
        (antd 的真 DOM + 布局壳层)。真页面上这条链的百分比高度能不能解析,
        取决于壳层给不给**确定高度**,复制件给不出这个前提
        ⇒ 那组数字既证明不了"对",也证明不了"错",只是**看起来很精确**。
        教训与"用被测常量构造输入 = 恒绿"同一类:
        **手搭的替身 ≠ 被测对象**,数字越精确越有欺骗性。
      */}
      <Row gutter={16} style={{ flex: 1, minHeight: 0 }}>
        {/* 左侧用户卡片 */}
        <Col xs={24} md={8} style={{ minHeight: 0 }}>
          <Card
            // ⚠️ **M113-c(2026-10-29):`maxHeight` 而不是 `height`** —— 用户手改的,
            //   `height:100%` ⇒ 卡片被拉成整列高,内容不够时底下留一大片空白
            //   `maxHeight:100%` ⇒ 卡片贴着内容高度;超了才被截断,
            //     而 body 是被截断那个盒子的**直接子元素** ⇒ 它自己滚,内容不会丢
            style={{ maxHeight: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
            styles={{ body: { flex: 1, minHeight: 0, overflowY: 'auto' } }}
          >
            <div style={{ textAlign: 'center', padding: '16px 0' }}>
              {/* ⚠️ M113:走 UserAvatar —— 以前是 <Avatar src={userInfo.avatar} icon={...} />,
                  而 avatar 为空串时 antd 会渲染 `<img src="">` = **浏览器裂图**,
                  且空 src 不触发 onError ⇒ icon 兜底永远等不到。 */}
              {/*
                ⚠️ **M124(2026-10-30):上传头像是这页唯一的头像写入口**
                以前 `PATCH /auth/profile` 收一个 `avatar` 字段(任意 URL),
                但前端**从来没提供过这个输入框** ⇒ 用户"不能设置头像"。
                早期那条路还带着一个安全问题:贴外链的头像会渲染在
                **别人的浏览器**里(管理端用户列表 / 顶栏)⇒ 那是一个跟踪像素。
                ⇒ 服务端删掉了 `UpdateProfileDTO.Avatar`(M124),
                  头像只能**上传**,落盘走文件系统(biz = "avatar"),
                  存在**自己 id 底下**。

                ⚠️ **不另外放一个「更换头像」按钮**(用户 2026-10-30 明确要求):
                多一个按钮把这一块撑得很挤。改成
                  **鼠标移上去 → 头像上浮出半透明遮罩 + 铅笔图标**
                  **点头像(整个区域)直接打开文件选择框**
                ⇒ antd 的 `<Upload>` 把头像当 children,
                  它自己的包裹 span 带了 `role="button"` + `tabIndex=0`
                  + onClick(打开文件框)⇒ 键盘也能用(Enter 触发)。
              */}
              <Upload {...avatarUploadProps}>
                <span
                  className="avatar-upload-trigger"
                  onMouseEnter={() => setHoverAvatar(true)}
                  onMouseLeave={() => setHoverAvatar(false)}
                >
                  <UserAvatar
                    size={80}
                    src={userInfo.avatar}
                    name={userInfo.nickname || userInfo.username}
                  />
                  {/*
                    ⚠️ **遮罩用 opacity 而不是 display** ——
                      display:none 会把点击事件一起带走;这里要的是"盖住但不挡点击"。
                    ⚠️ **pointer-events: none 在 CSS 里**(见 styles/global.css)——
                      遮罩只是装饰,真正的点击目标是它下面的头像。
                  */}
                  <span
                    className="avatar-upload-overlay"
                    style={{ opacity: hoverAvatar || uploadingAvatar ? 1 : 0 }}
                    aria-hidden="true"
                  >
                    {uploadingAvatar ? <LoadingOutlined /> : <EditOutlined />}
                  </span>
                  {/* 读屏用的文字:视觉上不可见,但要说清"这个按钮是干什么的" */}
                  <span className="avatar-upload-sr">{t('page.profile.avatarUpload')}</span>
                </span>
              </Upload>
              <div style={{ marginTop: 12, fontSize: 18, fontWeight: 500 }}>
                {userInfo.nickname || userInfo.username}
              </div>
              <div style={{ color: 'var(--app-text-tertiary)', marginTop: 4 }}>@{userInfo.username}</div>
              <Space style={{ marginTop: 12 }} wrap>
                {/*
                  ⚠️ **M113-c(2026-10-29):平台运维不该同时挂「普通用户」**
                  以前第三个条件只判 `!is_platform_super_admin` ⇒ 运维账号
                  (is_platform_ops=true、is_platform_super_admin=false)
                  会**同时**亮出「平台运维」和「普通用户」两个互斥的标签。
                  ⇒ 三个标签现在是互斥的:超管 / 运维 / 普通用户。
                */}
                {userInfo.is_platform_super_admin && (
                  <Tag color="red">{t('role.superAdmin')}</Tag>
                )}
                {userInfo.is_platform_ops && <Tag color="orange">{t('role.platformOps')}</Tag>}
                {!userInfo.is_platform_super_admin && !userInfo.is_platform_ops && (
                  <Tag color="blue">{t('role.normalUser')}</Tag>
                )}
              </Space>
            </div>
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label={t('field.account')}>{userInfo.username}</Descriptions.Item>
              <Descriptions.Item label={t('common.phone')}>
                <SensitiveField type="phone" value={userInfo.phone} />
              </Descriptions.Item>
              {/*
                ⚠️ **M113-c(2026-10-29):空串也要兜底成 "-"**
                以前写的是 `userInfo.email ?? '-'` —— `??` **只在 null/undefined 时兜底**,
                空串(账号压根没填邮箱,超管 seed 就是这样)会原样渲染成
                **一个空白单元格**,和旁边有值的行对不齐 —— 这正是
                "左卡片显示不对"的一种(有邮箱的账号看不出来,没邮箱的账号看得出)。
                ⇒ 改用 `||`:空串和 undefined 一起兜底。
              */}
              <Descriptions.Item label={t('common.email')}>{userInfo.email || '-'}</Descriptions.Item>
              <Descriptions.Item label={t('page.profile.identityCount')}>
                {t('page.profile.identityCountValue', undefined, {
                  n: userInfo.identities?.length ?? 0,
                })}
              </Descriptions.Item>
              <Descriptions.Item label={t('page.profile.currentIdentity')}>
                {userInfo.identities?.find((i) => i.id === userInfo.current_identity_id)?.role_name ||
                  '-'}
              </Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>

        {/* 右侧 Tabs */}
        <Col xs={24} md={16} style={{ minHeight: 0 }}>
          <Card
            // ⚠️ **M113-c(2026-10-29):跟左卡片一样用 `maxHeight: '100%'`(用户手改)**
            //   页签区那条链(holder overflow:hidden → content height:100%)
            //   要求父高是**确定值**;卡片到底给多高由用户按实际效果手调,
            //   这里只保证结构(min-height:0 / overflow 归属)对。理由见上面那段注释。
            style={{ maxHeight: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
            styles={{
              body: {
                flex: 1,
                minHeight: 0,
                display: 'flex',
                flexDirection: 'column',
                overflow: 'hidden',
              },
            }}
          >
            <Tabs
              // ⚠️ M113:高度链的最后一环 —— Tabs 根节点要 flex:1 + min-height:0,
              //   里面的 .ant-tabs-content-holder 才有确定的可用高度
              //   (对应的 CSS 在 styles/global.css 的 .profile-page-tabs 段)
              className="profile-page-tabs"
              style={{ flex: 1, minHeight: 0 }}
              defaultActiveKey="profile"
              items={[
                {
                  key: 'profile',
                  label: (
                    <span>
                      <EditOutlined /> {t('page.profile.tabBasic')}
                    </span>
                  ),
                  children: (
                    <div style={{ height: '100%', overflowY: 'auto', padding: '8px 0' }}>
                      {/*
                        ⚠️⚠️ **M113(2026-10-29):按钮必须放在 `<Form>` 外面**
                        这不是排版偏好,是"为什么不能修改个人基本信息"的**真凶**:

                          · antd 5 的 `Form` 会用 `DisabledContextProvider` 包住自己的
                            子树(antd/es/form/Form.js:140)
                          · antd 5 的 `Button` 从 `DisabledContext` 取禁用态:
                            `mergedDisabled = customDisabled ?? contextDisabled`
                            (antd/es/button/button.js:119-120)

                        而这个表单是 `<Form disabled={!editingProfile}>`,
                        「编辑 / 保存 / 取消」三个按钮又**写在 Form 里面** ⇒

                          editingProfile === false(初始态)⇒ Form 禁用
                          ⇒ **「编辑」按钮自己也被禁用了** ⇒ 点不动
                          ⇒ 永远进不了编辑态 ⇒ **基本信息根本改不了**

                        —— 这跟 RBAC 无关(那个端点对所有后台角色都是通的),
                        也不是后端不认(后端契约一直是通的),就是**前端把
                        "只读" 这个状态连按钮一起禁用了**。

                        现在按钮移到 Form 外面:`disabled` 只作用于三个输入框
                        (只读态确实该灰),动作按钮永远可点。
                      */}
                      <Form
                        form={profileForm}
                        layout="vertical"
                        disabled={!editingProfile}
                      >
                        <Form.Item
                          name="nickname"
                          label={t('field.nickname')}
                          rules={[{ required: true, message: t('common.placeholder.nickname') }]}
                        >
                          <Input placeholder={t('common.placeholder.nickname')} />
                        </Form.Item>
                        <Form.Item
                          name="real_name"
                          label={t('field.realName')}
                          rules={[{ max: 32, message: t('page.profile.max32') }]}
                        >
                          <Input placeholder={t('common.placeholder.realName')} />
                        </Form.Item>
                        <Form.Item
                          name="email"
                          label={t('common.email')}
                          rules={[{ type: 'email', message: t('validation.emailInvalid') }]}
                          extra={t('page.profile.emailHint')}
                        >
                          <Input placeholder={t('common.placeholder.email')} />
                        </Form.Item>
                        {/*
                          ⚠️ **M113(2026-10-29):手机号从「基本信息」里删掉了**
                          用户要求:"安全设置里面有手机绑定,基本信息 tab 页就不要显示手机号"。
                          以前这里是一个**只读**的 Input(带一句"要换手机号请到安全设置"),
                          跟「安全设置 → 手机绑定」那张卡片**显示的是同一个值**
                          —— 同一个信息在两个页签露两次,改一个地方另一个就过期了。
                          现在手机号**只有「安全设置」一个出处**(敏感字段 + 眼睛按钮),
                          基本信息只管"姓名 / 昵称 / 邮箱"这些跟账号无关的东西。
                        */}
                      </Form>
                      <Space>
                        {!editingProfile ? (
                          <Button
                            type="primary"
                            icon={<EditOutlined />}
                            onClick={startEditProfile}
                          >
                            {t('common.edit')}
                          </Button>
                        ) : (
                          <>
                            <Button type="primary" onClick={saveProfile} loading={savingProfile}>
                              {t('common.save')}
                            </Button>
                            <Button onClick={() => setEditingProfile(false)}>
                              {t('common.cancel')}
                            </Button>
                          </>
                        )}
                      </Space>
                    </div>
                  ),
                },
                {
                  key: 'security',
                  label: (
                    <span>
                      <LockOutlined /> {t('page.profile.tabSecurity')}
                    </span>
                  ),
                  children: (
                    <Space
                      direction="vertical"
                      style={{ width: '100%', height: '100%', overflowY: 'auto' }}
                      size="middle"
                    >
                      <Card type="inner" title={t('page.profile.loginPassword')}>
                        <Row justify="space-between" align="middle">
                          <Col>
                            <Space direction="vertical" size={4}>
                              <span>{t('page.profile.pwdTip')}</span>
                              <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>
                                {t('page.profile.pwdReloginTip')}
                              </span>
                            </Space>
                          </Col>
                          <Col>
                            <Button
                              type="primary"
                              icon={<LockOutlined />}
                              onClick={() => {
                                pwdForm.resetFields()
                                setChangePwdOpen(true)
                              }}
                            >
                              {t('page.profile.changePassword')}
                            </Button>
                          </Col>
                        </Row>
                      </Card>
                      <Card type="inner" title={t('page.profile.phoneBinding')}>
                        <Row justify="space-between" align="middle">
                          <Col>
                            <Space>
                              <span>{t('page.profile.boundPrefix')}</span>
                              <SensitiveField type="phone" value={userInfo.phone} />
                            </Space>
                          </Col>
                          <Col>
                            {/*
                              ⚠️ **C5(2026-10-29):这个按钮以前是禁用的**
                              tooltip 写着「Phase 6 真实化」—— 换绑手机这条链路
                              **从来没通过过**。现在是真流程(见下面那个弹窗)。
                            */}
                            <Button onClick={openBindModal}>{t('page.profile.changePhone')}</Button>
                          </Col>
                        </Row>
                      </Card>
                      {/*
                        ⚠️ **M150(2026-11-03):MFA 从"说明卡片"变成真功能**
                        M113 那版只有说明 + 禁用按钮。现在:
                          · 状态读 userInfo.mfa_enabled(服务端权威值,前端不自存)
                          · 开/关都走「发码(scene=mfa)→ 填码 → 提交」弹窗
                          · 前提 = 已绑手机号(phone 为空 = 未绑),未绑先去换绑手机
                      */}
                      <Card type="inner" title={t('page.profile.mfa')}>
                        <Space direction="vertical" style={{ width: '100%' }} size="small">
                          <Alert
                            type="info"
                            showIcon
                            message={t('page.profile.mfaWhat')}
                            description={t('page.profile.mfaWhy')}
                          />
                          {!userInfo.phone && (
                            <Alert
                              type="warning"
                              showIcon
                              message={t('page.profile.mfaNeedPhone')}
                            />
                          )}
                          <Row justify="space-between" align="middle">
                            <Col>
                              <Space>
                                <span>{t('page.profile.statusPrefix')}</span>
                                {userInfo.mfa_enabled ? (
                                  <Tag color="success">{t('page.profile.enabled')}</Tag>
                                ) : (
                                  <Tag color="default">{t('page.profile.notEnabled')}</Tag>
                                )}
                              </Space>
                            </Col>
                            <Col>
                              <Button
                                icon={<SafetyCertificateOutlined />}
                                onClick={() => openMfaModal(userInfo.mfa_enabled ? 'disable' : 'enable')}
                              >
                                {userInfo.mfa_enabled
                                  ? t('page.profile.disableMfa')
                                  : t('page.profile.enableMfa')}
                              </Button>
                            </Col>
                          </Row>
                        </Space>
                      </Card>
                    </Space>
                  ),
                },
                {
                  key: 'identities',
                  label: (
                    <span>
                      <CheckCircleOutlined /> {t('page.profile.tabIdentities')}
                    </span>
                  ),
                  children: (
                    <Space
                      direction="vertical"
                      style={{ width: '100%', height: '100%', overflowY: 'auto' }}
                      size="small"
                    >
                      {userInfo.identities?.map((i) => {
                        const isCurrent = i.id === userInfo.current_identity_id
                        return (
                          <Card
                            key={i.id}
                            size="small"
                            style={{
                              borderColor: isCurrent ? 'var(--app-selected-border-strong)' : undefined,
                              background: isCurrent ? 'var(--app-selected-bg)' : undefined,
                            }}
                          >
                            <Row justify="space-between" align="middle">
                              <Col>
                                <Space direction="vertical" size={2}>
                                  <Space>
                                    <span style={{ fontWeight: 500 }}>{i.role_name}</span>
                                    {isCurrent && <Tag color="blue">{t('page.profile.current')}</Tag>}
                                    {i.is_primary && <Tag color="purple">{t('page.profile.primaryIdentity')}</Tag>}
                                    {i.is_active ? (
                                      <Tag color="green">{t('common.enabled')}</Tag>
                                    ) : (
                                      <Tag color="red">{t('common.disabled')}</Tag>
                                    )}
                                  </Space>
                                  <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>
                                    {i.project_name} · {t('page.profile.validPeriod')} {i.valid_from?.slice(0, 10) ?? '-'} ~ {i.valid_to?.slice(0, 10) ?? '-'}
                                  </span>
                                </Space>
                              </Col>
                              <Col>
                                <Tooltip title={t('page.profile.switchHint')}>
                                  <Button size="small" disabled={isCurrent}>
                                    {t('common.switch')}
                                  </Button>
                                </Tooltip>
                              </Col>
                            </Row>
                          </Card>
                        )
                      })}
                    </Space>
                  ),
                },
                {
                  key: 'sessions',
                  label: (
                    <span>
                      <HistoryOutlined /> {t('page.profile.tabSessions')}
                    </span>
                  ),
                  children: <SessionPanel />,
                },
              ]}
            />
          </Card>
        </Col>
      </Row>

      {/* 改密弹窗 — 短信验证 + 旧/新密码 + 确认 */}
      <Modal
        title={t('page.profile.changePassword')}
        open={changePwdOpen}
        onCancel={() => setChangePwdOpen(false)}
        onOk={handleChangePwdOk}
        confirmLoading={submitting}
        okText={t('page.profile.confirmChange')}
        cancelText={t('common.cancel')}
        destroyOnHidden
        maskClosable={false}
      >
        {/*
          ⚠️ **M113(2026-10-29):整个弹窗不再需要短信验证码**
          以前是"第一屏短信验证 → 通过后才露出密码表单"。
          现在直接就是密码表单:旧密码 / 新密码 / 确认新密码。

          为什么这样安全:后端 `POST /rapid-admin/v1/auth/password` 从来就只认
          `old_password` + `new_password`,旧密码由 service 层校验
          (bcrypt 比对)。短信那一层是**纯前端的门**,而且那道门是**坏的**
          —— SmsCodeGuard 依赖的短信链路当时压根发不出去,
          结果是"个人中心改不了密码"。删掉它不放松任何服务端约束,
          只是把一个把人挡在门外的假门拆掉。
        */}
        <Form form={pwdForm} layout="vertical">
          <Form.Item
            name="old_password"
            label={t('field.oldPassword')}
            rules={[{ required: true, message: t('common.placeholder.oldPassword') }]}
          >
            <Input.Password autoComplete="current-password" placeholder={t('common.placeholder.oldPassword')} />
          </Form.Item>
          <Form.Item
            name="new_password"
            label={t('field.newPassword')}
            rules={[
              { required: true, message: t('validation.newPasswordRequired') },
              { min: 8, message: t('validation.passwordMinLength') },
              { pattern: /[A-Za-z]/, message: t('page.profile.pwdNeedLetter') },
              { pattern: /\d/, message: t('page.profile.pwdNeedDigit') },
            ]}
          >
            <Input.Password autoComplete="new-password" placeholder={t('common.placeholder.newPassword')} />
          </Form.Item>
          <Form.Item
            name="confirm"
            label={t('field.confirmPassword')}
            dependencies={['new_password']}
            rules={[
              { required: true, message: t('validation.confirmPasswordRequired') },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('new_password') === value) {
                    return Promise.resolve()
                  }
                  return Promise.reject(new Error(t('validation.passwordMismatch')))
                },
              }),
            ]}
          >
            <Input.Password autoComplete="new-password" placeholder={t('common.placeholder.confirmPassword')} />
          </Form.Item>
        </Form>
      </Modal>

      {/* 换绑手机号弹窗(C5,2026-10-29)—— 语义见模块头第 6 条说明 */}
      <Modal
        title={t('page.profile.bindPhoneTitle')}
        open={bindOpen}
        onCancel={closeBindModal}
        onOk={handleBindPhoneOk}
        confirmLoading={bindSubmitting}
        okText={t('page.profile.bindPhoneConfirm')}
        cancelText={t('common.cancel')}
        destroyOnHidden
        maskClosable={false}
      >
        <Alert
          type="info"
          showIcon
          message={t('page.profile.bindPhoneHint')}
          style={{ marginBottom: 16 }}
        />
        <Form form={bindForm} layout="vertical">
          <Form.Item
            name="phone"
            label={t('page.profile.bindPhoneNewLabel')}
            rules={[
              { required: true, message: t('validation.phoneRequired') },
              { pattern: /^1[3-9]\d{9}$/, message: t('validation.phoneInvalid') },
            ]}
          >
            <Input
              placeholder={t('page.profile.bindPhoneNewPlaceholder')}
              maxLength={11}
              autoComplete="tel"
            />
          </Form.Item>
          {/*
            ⚠️ **两层 Form.Item:外层管 label/布局,内层(name + noStyle)贴着 <Input>** ——
            antd 的 Form.Item 把 value/onChange 注入给它的**直接子元素**,
            所以**带 name 的那一层必须紧挨着 <Input>**,中间不能隔 <Space.Compact>。

            ⚠️ **实测(2026-10-29,antd 5.29.3):把 name 提到外层,提交时 code 并不会变 undefined**
            —— Space.Compact 把注入的 onChange 原样透传给自己的根 div,
            而 React 的 onChange 是**会冒泡的合成事件**(原生 input 事件),
            输入框里的值照样能冒泡到 div → 进 form store。
            但那样输入框会退化成**非受控**组件(value 落在 div 上,输入框收不到),
            resetFields / setFieldValue 清不掉它显示的内容。
            ⇒ 仍按 antd 官方给 Space.Compact 的两层写法来(受控 + 清得掉)。
          */}
          <Form.Item label={t('page.profile.bindPhoneCodeLabel')} required>
            <Space.Compact style={{ width: '100%' }}>
              <Form.Item
                name="code"
                noStyle
                rules={[
                  { required: true, message: t('page.profile.bindPhoneCodeRequired') },
                  { len: 6, message: t('page.profile.bindPhoneCodeLen') },
                ]}
              >
                <Input
                  placeholder={t('page.profile.bindPhoneCodePlaceholder')}
                  maxLength={6}
                  autoComplete="one-time-code"
                />
              </Form.Item>
              <Button
                onClick={handleSendBindCode}
                loading={bindSending}
                disabled={bindCountdown > 0}
              >
                {bindCountdown > 0
                  ? t('sms.resendAfter', undefined, { n: bindCountdown })
                  : t('sms.send')}
              </Button>
            </Space.Compact>
          </Form.Item>
        </Form>
      </Modal>

      {/* MFA 开/关弹窗(M150)—— 码发到**账号登记手机号**,与换绑弹窗的方向不同:
          这里要证明"还占着旧号"(安全设置变更),不是"占着新号" */}
      <Modal
        title={t(mfaModalMode === 'enable' ? 'page.profile.mfaEnableTitle' : 'page.profile.mfaDisableTitle')}
        open={mfaModalOpen}
        onCancel={closeMfaModal}
        onOk={handleMfaOk}
        confirmLoading={mfaSubmitting}
        okText={t(mfaModalMode === 'enable' ? 'page.profile.mfaEnableConfirm' : 'page.profile.mfaDisableConfirm')}
        cancelText={t('common.cancel')}
        destroyOnHidden
        maskClosable={false}
      >
        <Alert
          type="info"
          showIcon
          message={t(mfaModalMode === 'enable' ? 'page.profile.mfaEnableHint' : 'page.profile.mfaDisableHint')}
          style={{ marginBottom: 16 }}
        />
        <Form form={mfaForm} layout="vertical">
          <Form.Item label={t('page.profile.mfaCodeLabel')} required>
            <Space.Compact style={{ width: '100%' }}>
              <Form.Item
                name="code"
                noStyle
                rules={[
                  { required: true, message: t('page.profile.bindPhoneCodeRequired') },
                  { len: 6, message: t('page.profile.bindPhoneCodeLen') },
                ]}
              >
                <Input
                  placeholder={t('page.profile.bindPhoneCodePlaceholder')}
                  maxLength={6}
                  autoComplete="one-time-code"
                />
              </Form.Item>
              <Button
                onClick={handleSendMfaCode}
                loading={mfaSending}
                disabled={mfaCountdown > 0}
              >
                {mfaCountdown > 0
                  ? t('sms.resendAfter', undefined, { n: mfaCountdown })
                  : t('sms.send')}
              </Button>
            </Space.Compact>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

export default ProfilePage
