/**
 * 登录页
 *
 * 两个登录入口 (双 tab):
 * 1. 账号密码登录 (Form: username + password + "记住我" 勾选)
 * 2. 短信验证码登录 (Form: phone + smsCode, 配 SmsSendButton 60s 倒计时)
 *
 * "忘记密码?" -> 跳独立路由 /forgot-password, 不在本页做 Modal.
 *
 * "记住我" 语义:
 *   - 勾选: token 走 localStorage, 关浏览器/重启都不丢
 *   - 不勾: 走 sessionStorage, 关浏览器/重启即登出
 *
 * Phase 5 M9 polish:
 *   - 5 次失败软锁定 60s(前端可观察,真实锁定走后端)
 *   - 倒计时显示在按钮 + 文案
 *   - 错误码差异化(后端返回 "ACCOUNT_LOCKED" 字符串时,按钮禁用 + 显式倒计时)
 *   - 锁定期间给出"账号已锁定,请 Ns 后重试"
 */
import React, { useState, useEffect, useRef } from 'react'
import {
  Card, Form, Input, Button, Checkbox, Tabs, Typography, Space, App, Alert,
} from 'antd'
import {
  UserOutlined, LockOutlined, SafetyOutlined, MobileOutlined,
} from '@ant-design/icons'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { login, loginBySms, sendMfaLoginCode } from '@/api/auth'
import { useUserStore } from '@/stores/user'
import { usePublicConfigStore } from '@/stores/publicConfig'
import { preHashPassword } from '@/utils/crypto'
import { SmsSendButton, CaptchaField } from '@/components/common'
import { ROUTES } from '@/constants/app'
import { SmsScene } from '@/constants/sms'
import { BizCode } from '@/types/api'
import type { LoginResponse } from '@/types/user'
import { useI18n } from '@/locales'

const { Title, Text } = Typography

const PHONE_RE = /^1[3-9]\d{9}$/
const SMS_CODE_LEN = 6

/* ====================== 软锁定常量 ====================== */

/** 失败次数阈值 */
const MAX_ATTEMPTS = 5
/** 锁定时长(秒) */
const LOCKOUT_SECONDS = 60
/** localStorage key */
const FAIL_COUNT_KEY = 'login_fail_count'
const LOCKOUT_UNTIL_KEY = 'login_lockout_until'

interface LockoutState {
  failCount: number
  remaining: number
}

/** 读软锁定状态 */
function readLockout(): LockoutState {
  const failCount = Number(localStorage.getItem(FAIL_COUNT_KEY) || '0')
  const until = Number(localStorage.getItem(LOCKOUT_UNTIL_KEY) || '0')
  const remaining = until > 0 ? Math.max(0, Math.ceil((until - Date.now()) / 1000)) : 0
  return { failCount, remaining }
}

function clearLockout() {
  localStorage.removeItem(FAIL_COUNT_KEY)
  localStorage.removeItem(LOCKOUT_UNTIL_KEY)
}

function bumpFailCount(): LockoutState {
  const cur = readLockout()
  const next = cur.failCount + 1
  localStorage.setItem(FAIL_COUNT_KEY, String(next))
  if (next >= MAX_ATTEMPTS) {
    const until = Date.now() + LOCKOUT_SECONDS * 1000
    localStorage.setItem(LOCKOUT_UNTIL_KEY, String(until))
    return { failCount: next, remaining: LOCKOUT_SECONDS }
  }
  return { failCount: next, remaining: 0 }
}

/* ====================== 表单值类型 ====================== */

interface PasswordFormValues {
  username: string
  password: string
  /** 图形验证码 { id, code }(后端 LoginRequest.captcha_id/captcha_code)
   *  可选:captcha.login_enabled=false 时字段不渲染,values.captcha 为 undefined */
  captcha?: { id: string; code: string }
  /** 勾选: token 走 localStorage; 不勾: 走 sessionStorage(关浏览器即登出) */
  remember?: boolean
}

interface SmsFormValues {
  phone: string
  smsCode: string
  /** 图形验证码 { id, code },可选,理由同上(captcha_sms_send_enabled 关闭时同样不渲染) */
  captcha?: { id: string; code: string }
  /** 勾选"记住我" —— 短信登录此前写死 true,用户根本没有这个选项 */
  remember?: boolean
}

/* ====================== Login 主组件 ====================== */

/**
 * 短信 Form 子树内的手机号监听桥。
 * Form.useWatch 必须放在 Form **已挂载**的子树里 —— Form 未挂载时它内部调
 * getFieldsValue 会触发 rc-field-form 的 warningUnhooked(见 smsPhone 处注释)。
 * 渲染 null,只负责把值经 onChange 上提到页面级 state(smsPhone)。
 */
const SmsPhoneBridge: React.FC<{ onChange: (v: string) => void }> = ({ onChange }) => {
  const phone = Form.useWatch('phone')
  React.useEffect(() => {
    onChange(typeof phone === 'string' ? phone : '')
  }, [phone, onChange])
  return null
}

const Login: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  // 两个开关分别对应两类接口:
  //   密码 tab  → login_enabled(挡自动化撞库,本地最常关的就是它)
  //   短信 tab  → login || sms_send —— 短信流程要先过 /public/auth/sms/send,
  //               只要发短信闸门还开着,验证码框就得留着
  // 未拉到时都按 true(见 stores/publicConfig.ts)。
  const loginCaptchaOn = usePublicConfigStore((s) => s.isLoginCaptchaEnabled())
  const smsCaptchaOn = usePublicConfigStore((s) => s.isSmsCaptchaEnabled())
  // 平台名(后端 branding 配置下发,开源通用化)。空串 = 没配 → 落回语言包。
  const appName = usePublicConfigStore((s) => s.appName)
  const [tab, setTab] = useState<'password' | 'sms'>('password')
  const [loading, setLoading] = useState(false)
  const [lockout, setLockout] = useState<LockoutState>({ failCount: 0, remaining: 0 })
  const [lockedMessage, setLockedMessage] = useState<string | null>(null)
  const tickRef = useRef<number | null>(null)

  const [passwordForm] = Form.useForm<PasswordFormValues>()
  const [smsForm] = Form.useForm<SmsFormValues>()

  // 触发 CaptchaField 重新拉图: 登录失败 / 短信发送失败 时调用
  const [pwdCaptchaKey, setPwdCaptchaKey] = useState(0)
  const [smsCaptchaKey, setSmsCaptchaKey] = useState(0)

  const navigate = useNavigate()
  const [params] = useSearchParams()
  const redirect = params.get('redirect') || '/'
  const reason = params.get('reason')
  const setLogin = useUserStore((s) => s.setLogin)

  // 监听 SMS tab 的手机号, 实时给到 SmsSendButton (校验后才能发送)
  // 短信 tab 里手机号的实时值,由 <SmsPhoneBridge>(挂在短信 Form 子树内)上提。
  // ⚠️ 不能在页面级 `Form.useWatch('phone', smsForm)` —— 短信 tab 未激活时 Form
  //   没挂载,useWatch 走 getFieldsValue → rc-field-form warningUnhooked,
  //   每次进登录页控制台必报 "Instance created by useForm is not connected"。
  const [smsPhone, setSmsPhone] = React.useState('')

  // ===== MFA 挑战(M150)=====
  // 密码已验对(后端返 20109),等一条发到绑定手机的短信码。
  // 挑战是**无状态**的:提交时把 username + password(已 hash)+ mfa_code
  // 一起重发,不引入第三种短期凭证。null = 不在 MFA 步骤。
  const [mfa, setMfa] = useState<{ username: string; hashedPwd: string; remember: boolean } | null>(null)
  const [mfaCode, setMfaCode] = useState('')
  const [mfaSending, setMfaSending] = useState(false)
  const [mfaCountdown, setMfaCountdown] = useState(0)
  const mfaTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (mfaCountdown <= 0) return
    mfaTimerRef.current = setTimeout(() => setMfaCountdown((s) => s - 1), 1000)
    return () => {
      if (mfaTimerRef.current) clearTimeout(mfaTimerRef.current)
    }
  }, [mfaCountdown])

  // 初始读锁定状态 + 倒计时
  useEffect(() => {
    setLockout(readLockout())
    if (readLockout().remaining > 0) {
      setLockedMessage(t('login.locked', undefined, { n: readLockout().remaining }))
    }
  }, [t])

  useEffect(() => {
    if (lockout.remaining > 0) {
      tickRef.current = window.setInterval(() => {
        const cur = readLockout()
        setLockout(cur)
        if (cur.remaining > 0) {
          setLockedMessage(t('login.locked', undefined, { n: cur.remaining }))
        } else {
          setLockedMessage(null)
          if (tickRef.current) {
            clearInterval(tickRef.current)
            tickRef.current = null
          }
        }
      }, 1000)
    }
    return () => {
      if (tickRef.current) {
        clearInterval(tickRef.current)
        tickRef.current = null
      }
    }
  }, [lockout.remaining, t])

  const isLocked = lockout.remaining > 0

  /** 登录成功的统一落点(密码登录 / MFA 复验 / 短信登录三个入口共用) */
  const completeLogin = (resp: LoginResponse, remember: boolean) => {
    clearLockout()
    setLockout({ failCount: 0, remaining: 0 })
    setLockedMessage(null)
    setLogin({
      userInfo: resp.user,
      accessToken: resp.token,
      refreshToken: resp.refresh_token,
      // 勾选框 initialValues={remember:true},所以这里恒有明确布尔值。
      // 用 === true 而不是 !== false:后者在字段缺失时又会退回"记住我",
      // 那正是本轮要消掉的那种静默默认。
      remember,
      expiresIn: resp.expires_in,
      // 2026-10-21:refresh token 的本地 TTL 同样来自服务端(后台口径 720h)。
      // 不传的话 store 会用 8h 兜底 —— 用户明明还有 30 天,浏览器却在 8 小时
      // 后把 refresh 扔了,表现为"用着用着被踢回登录页"。
      refreshExpiresIn: resp.refresh_expires_in,
    })
    message.success(t('message.loginSuccess'))
    navigate(redirect, { replace: true })
  }

  const onPasswordFinish = async (values: PasswordFormValues) => {
    if (isLocked) {
      message.warning(t('login.lockedWarning', undefined, { n: lockout.remaining }))
      return
    }
    setLoading(true)
    // hashed 提到 try 外:MFA 挑战在 catch 里拦截,要把 hash 存进挑战状态
    let hashed = ''
    try {
      hashed = await preHashPassword(values.password)
      const resp = await login({
        username: values.username,
        password: hashed,
        // 验证码开关关闭时字段没渲染,values.captcha 是 undefined,
        // 直接 .id 会 TypeError;空串由后端按"跳过校验"处理。
        captcha_id: values.captcha?.id ?? '',
        captcha_code: values.captcha?.code ?? '',
      })
      completeLogin(resp, values.remember === true)
    } catch (err: any) {
      // 后端验证 captcha 是单次性: 成功 / 失败 都会 clear; 失败时强制换图
      setPwdCaptchaKey(Date.now())
      // 后端返回 ACCOUNT_LOCKED / PASSWORD_EXPIRED 时单独处理
      const code = err?.code ?? err?.bizCode
      const msg = err?.message ?? ''
      // ⚠️ 下面这个正则里的中文**不是文案**,是**匹配后端 message 的模式**:
      //   后端的 message 是服务端生成的固定中文,不受界面语言影响
      //   (i18n 的是"我们说什么",不是"我们匹配什么"),所以这里不能 i18n。
      // ⚠️ `code` 用的是**后端真实业务码**(errcode.UserLocked = 20101),
      //   不是字符串 'ACCOUNT_LOCKED' —— 后端从来没发过那个字符串,
      //   那个判断以前**一次都没命中过**,全靠上面的正则兜着。
      // ⚠️ MFA 挑战(20109)必须在软锁定计数**之前**拦下 ——
      //   它不是失败(密码已验对),bumpFailCount 对它计数会把开了 MFA
      //   的用户第一次登录就往软锁定推一步。
      if (code === BizCode.MFA_REQUIRED) {
        setMfa({ username: values.username, hashedPwd: hashed, remember: values.remember === true })
        setMfaCode('')
        // 首条码服务端已自动发出;重发按钮从 60s 冷却开始倒计时
        setMfaCountdown(60)
        return
      }
      if (code === BizCode.USER_LOCKED || /锁定|locked/i.test(msg)) {
        // ⚠️ 以前是 `setLockedMessage(msg || t('login.lockedByServer'))` ——
        //   msg 优先,于是英文用户看到的是**后端那句中文**。
        //   后端 message 永远是中文的,不能直接甩给用户;统一走字典。
        setLockedMessage(t('login.lockedByServer'))
        // 后端锁定时间可能更长,这里用 600s 兜底
        const until = Date.now() + 600 * 1000
        localStorage.setItem(LOCKOUT_UNTIL_KEY, String(until))
        setLockout({ failCount: MAX_ATTEMPTS, remaining: 600 })
        return
      }
      // 正常失败计数 + 软锁定
      const next = bumpFailCount()
      setLockout(next)
      if (next.remaining > 0) {
        setLockedMessage(t('login.locked', undefined, { n: next.remaining }))
      } else {
        const left = MAX_ATTEMPTS - next.failCount
        if (left > 0 && left <= 3) {
          message.warning(t('login.attemptsLeft', undefined, { n: left }))
        }
      }
    } finally {
      setLoading(false)
    }
  }

  const onSmsFinish = async (values: SmsFormValues) => {
    setLoading(true)
    try {
      const resp = await loginBySms(values.phone, values.smsCode, values.captcha)
      // 此前这里写死 remember: true —— 短信登录的用户压根没有"记住我"
      // 这个选项,关掉浏览器也一定在。现在跟密码登录走同一个勾选框。
      completeLogin(resp, values.remember === true)
    } catch {
      // SMS 登录失败: captcha 已被服务端 clear, 强制换图
      setSmsCaptchaKey(Date.now())
    } finally {
      setLoading(false)
    }
  }

  // ===== MFA 步骤(M150)=====

  /** 带码复验:密码 + mfa_code 一起重发(挑战无状态),成功即登录 */
  const onMfaSubmit = async () => {
    if (!mfa) return
    if (!/^\d{6}$/.test(mfaCode)) {
      message.warning(t('validation.smsCodeLen', undefined, { n: SMS_CODE_LEN }))
      return
    }
    setLoading(true)
    try {
      const resp = await login({
        username: mfa.username,
        password: mfa.hashedPwd,
        mfa_code: mfaCode,
        // MFA 复验那次调用后端跳过图形码校验(见 handler Login 注释)——
        // 这里首条图形码在首次挑战时已被消费,不能再造一张
        captcha_id: '',
        captcha_code: '',
      })
      completeLogin(resp, mfa.remember)
    } catch (err) {
      // 错码 = 真失败,与密码登录同一套软锁定计数
      const e = err as { code?: number; bizCode?: number; message?: string }
      const code = e?.code ?? e?.bizCode
      if (code === BizCode.USER_LOCKED || /锁定|locked/i.test(e?.message ?? '')) {
        setMfa(null)
        setLockedMessage(t('login.lockedByServer'))
        localStorage.setItem(LOCKOUT_UNTIL_KEY, String(Date.now() + 600 * 1000))
        setLockout({ failCount: MAX_ATTEMPTS, remaining: 600 })
        return
      }
      const next = bumpFailCount()
      setLockout(next)
    } finally {
      setLoading(false)
    }
  }

  /** 重发 MFA 验证码(密码先验,服务端职责;这里只管按钮冷却) */
  const onMfaResend = async () => {
    if (!mfa || mfaCountdown > 0) return
    setMfaSending(true)
    try {
      await sendMfaLoginCode(mfa.username, mfa.hashedPwd)
      setMfaCountdown(60)
      message.success(t('login.mfaResent'))
    } catch {
      // 错误 toast 由 client 层统一弹(密码错 / 限流 / 未开 MFA)
    } finally {
      setMfaSending(false)
    }
  }

  return (
    <div style={styles.page}>
      <Card style={styles.card} hoverable>
        <Space direction="vertical" size="large" style={{ width: '100%' }}>
          <div style={{ textAlign: 'center' }}>
            <Title level={3} style={{ margin: 0 }}>{appName || t('login.title')}</Title>
            <Text type="secondary">{t('login.subtitle')}</Text>
          </div>

          {reason === 'idle' && (
            <Alert
              type="warning"
              showIcon
              message={t('login.idleTimeout')}
            />
          )}

          {reason === 'password-changed' && (
            <Alert
              type="success"
              showIcon
              message={t('login.passwordChanged')}
            />
          )}

          {lockedMessage && (
            <Alert type="error" showIcon message={lockedMessage} />
          )}

          {mfa ? (
            // ===== MFA 步骤面板(M150)=====
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              <Alert
                type="info"
                showIcon
                message={t('login.mfaTitle')}
                description={t('login.mfaHint')}
              />
              <Input
                prefix={<SafetyOutlined />}
                size="large"
                placeholder={t('validation.smsCodePlaceholder', undefined, { n: SMS_CODE_LEN })}
                maxLength={SMS_CODE_LEN}
                value={mfaCode}
                onChange={(e) => setMfaCode(e.target.value.replace(/\D/g, ''))}
                onPressEnter={onMfaSubmit}
                disabled={loading}
              />
              <Button type="primary" size="large" loading={loading} block onClick={onMfaSubmit}>
                {t('login.mfaSubmit')}
              </Button>
              <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                <Button
                  type="link"
                  size="small"
                  disabled={mfaCountdown > 0}
                  loading={mfaSending}
                  onClick={onMfaResend}
                >
                  {mfaCountdown > 0
                    ? t('login.mfaResendIn', undefined, { n: mfaCountdown })
                    : t('login.mfaResend')}
                </Button>
                <Button type="link" size="small" onClick={() => setMfa(null)}>
                  {t('login.mfaBack')}
                </Button>
              </Space>
            </Space>
          ) : (
            <>
          <Tabs
            activeKey={tab}
            onChange={(k) => setTab(k as 'password' | 'sms')}
            items={[
              { key: 'password', label: t('login.tabPassword') },
              { key: 'sms', label: t('login.tabSms') },
            ]}
            centered
          />

          {tab === 'password' ? (
            <Form<PasswordFormValues>
              key="pwd"
              form={passwordForm}
              layout="vertical"
              onFinish={onPasswordFinish}
              initialValues={{ remember: true }}
              requiredMark={false}
            >
              <Form.Item
                name="username"
                rules={[{ required: true, message: t('validation.usernameRequired') }]}
              >
                <Input
                  prefix={<UserOutlined />}
                  size="large"
                  placeholder={t('login.usernamePlaceholder')}
                  autoComplete="username"
                  disabled={isLocked}
                />
              </Form.Item>
              <Form.Item
                name="password"
                rules={[{ required: true, message: t('validation.passwordRequired') }]}
              >
                <Input.Password
                  prefix={<LockOutlined />}
                  size="large"
                  placeholder={t('login.passwordPlaceholder')}
                  autoComplete="current-password"
                  disabled={isLocked}
                />
              </Form.Item>
              {loginCaptchaOn && (
                <Form.Item
                  name="captcha"
                  rules={[
                    {
                      validator: (_, v: { id?: string; code?: string } | undefined) =>
                        v?.code ? Promise.resolve() : Promise.reject(new Error(t('validation.captchaRequired'))),
                    },
                  ]}
                >
                  <CaptchaField size="large" refreshKey={pwdCaptchaKey} />
                </Form.Item>
              )}
              <Form.Item>
                <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                  <Form.Item name="remember" valuePropName="checked" noStyle>
                    <Checkbox>{t('login.remember')}</Checkbox>
                  </Form.Item>
                  <Link to={ROUTES.FORGOT_PASSWORD}>{t('login.forgot')}</Link>
                </Space>
              </Form.Item>
              <Form.Item>
                <Button
                  type="primary"
                  htmlType="submit"
                  size="large"
                  loading={loading}
                  disabled={isLocked}
                  block
                >
                  {isLocked
                    ? t('login.lockedButton', undefined, { n: lockout.remaining })
                    : t('login.submit')}
                </Button>
              </Form.Item>
            </Form>
          ) : (
            <Form<SmsFormValues>
              key="sms"
              form={smsForm}
              layout="vertical"
              onFinish={onSmsFinish}
              requiredMark={false}
            >
              <Form.Item
                name="phone"
                rules={[
                  { required: true, message: t('validation.phoneRequired') },
                  { pattern: PHONE_RE, message: t('validation.phoneInvalid') },
                ]}
              >
                <Input
                  prefix={<MobileOutlined />}
                  size="large"
                  placeholder={t('validation.phonePlaceholder')}
                  maxLength={11}
                />
              </Form.Item>
              {/* 手机号监听桥:必须在 Form 子树内,Form 卸载时它跟着不存在(useWatch 不会打警告) */}
              <SmsPhoneBridge onChange={setSmsPhone} />
              <Form.Item style={{ marginBottom: 16 }}>
                <div style={{ display: 'flex', gap: 8, width: '100%' }}>
                  <Form.Item
                    name="smsCode"
                    noStyle
                    rules={[
                      { required: true, message: t('validation.smsCodeRequired') },
                      { len: SMS_CODE_LEN, message: t('validation.smsCodeLen', undefined, { n: SMS_CODE_LEN }) },
                    ]}
                  >
                    <Input
                      prefix={<SafetyOutlined />}
                      size="large"
                      placeholder={t('validation.smsCodePlaceholder', undefined, { n: SMS_CODE_LEN })}
                      maxLength={SMS_CODE_LEN}
                      style={{ flex: 1 }}
                    />
                  </Form.Item>
                  <div style={{ width: 140 }}>
                    <SmsSendButton
                      phone={smsPhone}
                      scene={SmsScene.LOGIN}
                      // 2026-10-28 M106:发短信会**消费掉**后端那张图形验证码
                      //   (后端先删后比,成功失败都消费)。
                      //   短信登录提交那一步(POST /public/auth/login-sms,scene=login)
                      //   要再校验一次 —— 不换图的话它拿到的是一张**已作废**的验证码,
                      //   「图形验证码错误或已过期」⇒ 短信登录 100% 走不通。
                      onCaptchaConsumed={() => setSmsCaptchaKey(Date.now())}
                      beforeSend={async () => {
                        try {
                          // 验证码关闭时字段根本没注册,validateFields(['captcha'])
                          // 不会报错但也校验不到东西 —— 只校验 phone,否则
                          // "手机号没填就点发送"的拦截会跟着一起消失。
                          await smsForm.validateFields(smsCaptchaOn ? ['phone', 'captcha'] : ['phone'])
                          const c = smsCaptchaOn
                            ? (smsForm.getFieldValue('captcha') as { id: string; code: string })
                            : undefined
                          return { ok: true, captcha: c }
                        } catch (e: any) {
                          return { ok: false, error: e?.errorFields?.[0]?.errors?.[0] ?? t('validation.captchaRequired') }
                        }
                      }}
                    />
                  </div>
                </div>
              </Form.Item>
              {smsCaptchaOn && (
                <Form.Item
                  name="captcha"
                  rules={[
                    {
                      validator: (_, v: { id?: string; code?: string } | undefined) =>
                        v?.code ? Promise.resolve() : Promise.reject(new Error(t('validation.captchaRequired'))),
                    },
                  ]}
                >
                  <CaptchaField size="large" refreshKey={smsCaptchaKey} />
                </Form.Item>
              )}
              <Form.Item style={{ marginBottom: 12 }}>
                <Form.Item name="remember" valuePropName="checked" noStyle initialValue>
                  <Checkbox>{t('login.remember')}</Checkbox>
                </Form.Item>
              </Form.Item>
              <Form.Item>
                <Button type="primary" htmlType="submit" size="large" loading={loading} block>
                  {t('login.submit')}
                </Button>
              </Form.Item>
            </Form>
          )}
            </>
          )}
        </Space>
      </Card>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: '100vh',
    background: 'linear-gradient(135deg, var(--app-primary) 0%, var(--app-primary-hover) 100%)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 16,
  },
  card: {
    width: 420,
    maxWidth: '100%',
    boxShadow: '0 8px 32px rgba(0,0,0,0.12)',
    borderRadius: 8,
  },
}

export default Login
