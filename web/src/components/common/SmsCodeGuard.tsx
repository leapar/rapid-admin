/**
 * SmsCodeGuard —— 短信验证码二次确认
 *
 * 用于敏感操作前的二次验证(切换身份、修改密码等)
 *
 * ## M104(2026-10-28)修的 6 处
 *
 *  1. **`bizType` 是死 prop** —— 它从来没被传进 `sendSmsCode` / `verifySmsCode`,
 *     发出去的请求 scene 是 undefined → 后端「未知 scene: 」→ **短信一条都没发出去过**。
 *     现在叫 `scene`,而且是**必填**的 `SmsScene`(后端白名单里的 4 个之一)。
 *  2. **`onVerified` 不带验证码** —— 调用方只能传 `undefined` 给后端,
 *     后端直接拒「切换到高敏角色需要短信验证码」。
 *     现在 `onVerified(code)` 把验证码交回去。
 *  3. **双重消费** —— 后端 `VerifySms` / `SwitchIdentity` 校验通过即置 `used_at`,
 *     所以"本地先验一次、后面再拿同一个码去用"必然失败。
 *     用 `mode="delegate"` 表达"这个码由后面的后端调用消费"。
 *  4. **`setInterval` 泄漏** —— 倒计时的 timer 从来没被清过,组件卸载后还在跑。
 *  5. **`cloneElement` 覆盖了 trigger 自己的 `onClick`** —— 触发按钮上挂的任何
 *     点击逻辑都被冲掉。现在是**组合**而不是替换。
 *  6. **验证码长度校验是 `code.length < 4`** —— 4 位、5 位都能过;
 *     后端是 6 位(`generateCode` 恒 `fmt.Sprintf("%06d")`)。现在严格等于 6。
 */
import React, { useEffect, useRef, useState } from 'react'
import { Modal, Form, Input, Button, message, Space, Typography } from 'antd'
import { SafetyOutlined } from '@ant-design/icons'
import { sendSmsCode, verifySmsCode } from '@/api/auth'
import { SMS_CODE_LEN, SMS_SCENE_LABEL_KEY, type SmsScene } from '@/constants/sms'
import { useI18n } from '@/locales'

const { Text } = Typography
const COOLDOWN_SECONDS = 60

/**
 * 校验方式
 *
 * - `guard`(默认):先本地校验(`verifyCode`),通过后才回调 `onVerified(code)`
 * - `delegate`:**不**本地校验,直接把验证码交给 `onVerified(code)`
 *
 * ⚠️ `delegate` 只能在"验证码由后续那个后端调用消费"时用。
 * 验证码是**一次性**的(后端校验通过即置 `used_at`),本地先验一次,
 * 后续那次必然拿到「短信验证码已使用」。
 */
export type SmsCodeGuardMode = 'guard' | 'delegate'

export interface SmsCodeGuardProps {
  /**
   * 业务场景(后端白名单里的 4 个之一)
   *
   * ⚠️ 必填 —— 以前这个 prop 叫 `bizType`、可选、而且**根本没被用**。
   */
  scene: SmsScene
  /** 接收验证码的手机号(展示用;脱敏值也可以,原样脱敏后显示) */
  phone: string
  /**
   * 发送验证码。
   * 不传 → 用默认的 `sendSmsCode(phone, scene)`(公开接口,需要明文手机号)。
   * 传了 → 用它(典型:服务端代发 `sendAccountSms(scene)`,手机号由服务端取)。
   */
  sendCode?: () => Promise<void>
  /**
   * 校验验证码,返回是否通过。
   * 不传且 mode='guard' → 用默认的 `verifySmsCode(phone, code, scene)`。
   * 不传且 mode='delegate' → 不做本地校验。
   */
  verifyCode?: (code: string) => Promise<boolean>
  /** 校验方式,见 SmsCodeGuardMode */
  mode?: SmsCodeGuardMode
  /** 验证通过回调(**带验证码**) */
  onVerified: (code: string) => void | Promise<void>
  /** Modal 标题 */
  title?: string
  /** 触发按钮 */
  trigger: React.ReactElement
}

export const SmsCodeGuard: React.FC<SmsCodeGuardProps> = ({
  scene,
  phone,
  sendCode,
  verifyCode,
  mode = 'guard',
  onVerified,
  title,
  trigger,
}) => {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const [code, setCode] = useState('')
  const [sending, setSending] = useState(false)
  const [verifying, setVerifying] = useState(false)
  const [countdown, setCountdown] = useState(0)
  const [form] = Form.useForm()

  // ⚠️ 倒计时的 timer 必须能被清掉 —— 以前它在 sendCode 里 setInterval 之后
  // 就没人管了:组件卸载(setOpen(false) + destroyOnHidden,或者父组件把整个
  // Modal 拆掉)之后,那个 timer 还在每秒跑一次 setState。
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const stopCountdown = () => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }

  // 卸载时清 timer
  useEffect(() => stopCountdown, [])

  const startCountdown = () => {
    stopCountdown()
    setCountdown(COOLDOWN_SECONDS)
    timerRef.current = setInterval(() => {
      setCountdown((c) => {
        if (c <= 1) {
          stopCountdown()
          return 0
        }
        return c - 1
      })
    }, 1000)
  }

  const sendCodeAndStartCountdown = async () => {
    if (sendCode) {
      await sendCode()
    } else {
      if (!phone) {
        message.warning(t('smsGuard.phoneMissing'))
        return
      }
      await sendSmsCode(phone, scene)
    }
    message.success(t('sms.codeSentSimple'))
    startCountdown()
  }

  const handleSend = async () => {
    setSending(true)
    try {
      await sendCodeAndStartCountdown()
    } catch {
      /* 错误已被拦截器处理 */
    } finally {
      setSending(false)
    }
  }

  const verify = async () => {
    // ⚠️ 必须是**恰好 6 位** —— 以前是 `code.length < 4`,4 位、5 位都能过这关,
    // 然后被后端拒掉(用户已经输了,提示却来得莫名其妙)。
    if (!code || code.length !== SMS_CODE_LEN) {
      message.warning(t('smsGuard.codeInvalid'))
      return
    }
    setVerifying(true)
    try {
      if (mode === 'guard') {
        const ok = verifyCode
          ? await verifyCode(code)
          : (await verifySmsCode(phone, code, scene)).verified
        if (!ok) {
          message.error(t('smsGuard.codeWrong'))
          return
        }
      }
      // delegate 模式:验证码交给 onVerified,由后续的后端调用消费它
      await onVerified(code)
      setOpen(false)
      setCode('')
      stopCountdown()
    } catch {
      /* 错误已被拦截器处理 */
    } finally {
      setVerifying(false)
    }
  }

  // 脱敏:已经是脱敏值(138****1234)的,原样脱敏还是它;明文的则脱敏后显示
  const maskedPhone = phone ? `${phone.slice(0, 3)}****${phone.slice(-4)}` : ''

  // ⚠️ **组合**而不是替换 trigger 自己的 onClick。
  // 以前是 `cloneElement(trigger, { onClick: () => setOpen(true) })` ——
  // 触发按钮上挂的任何点击逻辑(埋点、埋单、disabled 判断)都被冲掉。
  const triggerEl = React.cloneElement(trigger, {
    onClick: (e: React.MouseEvent) => {
      trigger.props.onClick?.(e)
      setOpen(true)
    },
  })

  return (
    <>
      {triggerEl}
      <Modal
        title={title || t('smsGuard.defaultTitle')}
        open={open}
        onCancel={() => setOpen(false)}
        footer={null}
        destroyOnHidden
        maskClosable={false}
      >
        <Form form={form} layout="vertical">
          <Text type="secondary">
            <SafetyOutlined />{' '}
            {t('smsGuard.sentTo', undefined, {
              phone: maskedPhone,
              scene: t(SMS_SCENE_LABEL_KEY[scene]),
            })}
          </Text>
          <Form.Item style={{ marginTop: 16 }}>
            <Space.Compact style={{ width: '100%' }}>
              <Input
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder={t('validation.smsCodePlaceholder', undefined, { n: SMS_CODE_LEN })}
                maxLength={SMS_CODE_LEN}
                size="large"
              />
              <Button
                size="large"
                loading={sending}
                disabled={countdown > 0}
                onClick={handleSend}
              >
                {countdown > 0
                  ? t('sms.resendAfterRetry', undefined, { n: countdown })
                  : t('sms.getCode')}
              </Button>
            </Space.Compact>
          </Form.Item>
          <Form.Item>
            <Button
              type="primary"
              block
              size="large"
              loading={verifying}
              onClick={verify}
            >
              {t('common.confirm')}
            </Button>
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}

export default SmsCodeGuard
