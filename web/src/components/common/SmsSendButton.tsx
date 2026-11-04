/**
 * SmsSendButton —— "发送验证码" 按钮
 *
 * 关键设计: 倒计时时间戳持久化到 localStorage, 不只在内存里.
 *   - 用户刷新页面, 倒计时能从剩余秒数继续走完 (而不是白白再等 60s)
 *   - 用户切走标签页几分钟后回来, 也仍在原剩余秒数
 *
 * 存储 key: sms:cooldown:{scene}:{phone}, value = { until: 截止 epoch ms }
 * TTL 给到 seconds+5 (留 5s 时钟偏差缓冲, 阻止临界点 race).
 *
 * ⚠️ **scene 是必填的,而且必须是后端白名单里的那 4 个**
 * (`login` / `reset_password` / `change_phone` / `view_full_phone`)。
 *
 * 2026-10-28(M104)之前这个 prop 叫 `bizType`,前端传的是
 * `'login-sms'` / `'forgot-password'` —— **后端一个都不认**,
 * `validScene()` 返 400「未知 scene」,**短信一条都没发出去过**。
 * 现在 prop 改成 `scene: SmsScene`,类型层面就锁死,传错编译期就红。
 *
 * 用法:
 *   <SmsSendButton phone={phone} scene={SmsScene.LOGIN} />   // 默认 60s
 *   <SmsSendButton phone={phone} scene={SmsScene.LOGIN} seconds={90} />
 */
import React, { useEffect, useRef, useState } from 'react'
import { Button, message } from 'antd'
import type { SizeType } from 'antd/es/config-provider/SizeContext'
import { sendSmsCode } from '@/api/auth'
import { SMS_SCENE_LABEL_KEY, type SmsScene } from '@/constants/sms'
import { storage } from '@/utils/storage'
import { useI18n } from '@/locales'

const PHONE_RE = /^1[3-9]\d{9}$/

interface StoredUntil {
  /** 截止时间戳(epoch ms) */
  until: number
}

/** 拼 localStorage key, 按 scene+phone 隔离, 互不干扰 */
const cooldownKey = (scene: SmsScene, phone: string) =>
  `sms:cooldown:${scene}:${phone}`

export interface SmsSendButtonProps {
  /** 接收验证码的手机号; 为空时按钮点击会提示 */
  phone?: string
  /** 点击前是否校验手机号格式,默认 true */
  validatePhone?: boolean
  /** 倒计时秒数,默认 60 */
  seconds?: number
  size?: SizeType
  /**
   * 业务场景(后端白名单里的 4 个之一)
   *
   * ⚠️ 不再是可选的:以前叫 `bizType`、类型是 `string`、而且**根本没传进
   * `sendSmsCode`** —— 发出去的请求里 scene 是 undefined,后端返
   * 「未知 scene: 」。
   */
  scene: SmsScene
  /**
   * 发送短信前调用, 用于父页面插入额外校验(典型: 图形验证码必填).
   * - 返回 { ok: false, error }  → 中止发送, 用 message.warning 弹 error
   * - 返回 { ok: true, captcha } → 透传 captcha 给 sendSmsCode
   * - 不传 → 跳过此钩子, 与旧行为一致
   */
  beforeSend?: () => Promise<{
    ok: boolean
    error?: string
    captcha?: { id: string; code: string }
  }>
  /**
   * **发送尝试结束后**回调(成功 / 失败都调)—— 父页面据此换一张新验证码图。
   *
   * ## 为什么必须有这个出口(2026-10-28 M106)
   *
   * 后端的图形验证码是**严格单次性**的:
   * `captchaStore.Verify(id, answer, clear=true)` → `Get(id, true)`
   * 里**先 `delete(s.data, id)` 再比对**,所以**无论验证码对不对,
   * 这个 id 都已经从服务端 store 里被删掉了**。
   * 换句话说:**"发短信"这个动作一定会消耗掉当前这张验证码**。
   *
   * ## 不换图会怎样(两个都是真故障)
   *
   * 1. **发送成功** → 后端接着拿同一张验证码去校验下一步
   *    (短信登录 `/public/auth/login-sms`、短信重置密码
   *    `/public/auth/reset-password-sms`,这两个接口的 scene 是
   *    `login`,受 `captcha.login_enabled` 管)⇒ 拿的是一张**已经作废**的验证码
   *    ⇒ 「图形验证码错误或已过期」⇒ **短信登录 / 短信重置密码 100% 走不通**。
   *    ⚠️ 这条只在 `login_enabled` 与 `sms_send_enabled` **同时为 true** 时发作 ——
   *    而那正是 `configs/config.example.yaml` 模板里的默认值。
   * 2. **发送失败**(验证码填错)→ 验证码同样已被消费,页面却还显示旧图 ⇒
   *    用户对着旧图重输旧码 → 再失败 ⇒ **永远发不出去**(卡死),
   *    除非用户自己知道要点图片刷新。
   *
   * ## 什么时候不调
   *
   * 只有**真的带了验证码**才调(本次请求没带 captcha → 没什么可换的)。
   * `beforeSend` 被拒(校验没过,压根没发请求)→ 也不调 —— 那时验证码没被消费。
   */
  onCaptchaConsumed?: () => void
}

export const SmsSendButton: React.FC<SmsSendButtonProps> = ({
  phone = '',
  validatePhone = true,
  seconds = 60,
  size = 'large',
  scene,
  beforeSend,
  onCaptchaConsumed,
}) => {
  const { t } = useI18n()
  const [sending, setSending] = useState(false)
  const [countdown, setCountdown] = useState(0)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const stop = () => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }

  /**
   * 把当前 (phone, scene) 的剩余冷却时间同步到 UI:
   *   1. 清掉旧 timer
   *   2. 从 localStorage 读截止时间戳, 算出剩余秒数
   *   3. 若剩余 > 0, 启动 1s 间隔 timer 倒计时
   */
  const syncWithStorage = () => {
    stop()
    if (!phone) {
      setCountdown(0)
      return
    }
    const stored = storage.get<StoredUntil>(cooldownKey(scene, phone))
    if (!stored) {
      setCountdown(0)
      return
    }
    const remaining = Math.ceil((stored.until - Date.now()) / 1000)
    if (remaining <= 0) {
      setCountdown(0)
      return
    }
    setCountdown(remaining)
    timerRef.current = setInterval(() => {
      setCountdown((c) => {
        if (c <= 1) {
          stop()
          return 0
        }
        return c - 1
      })
    }, 1000)
  }

  // mount / phone 变化 / scene 变化 时同步
  useEffect(() => {
    syncWithStorage()
    return stop
    // syncWithStorage 内使用了 get/set/clearInterval — 都引用稳定,
    // 这里只把外部依赖列出来就够
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phone, scene])

  const handleClick = async (): Promise<void> => {
    if (timerRef.current) return // 倒计时中(或本地 storage 还有效)
    if (validatePhone) {
      if (!phone) {
        message.warning(t('validation.phoneRequired'))
        return
      }
      if (!PHONE_RE.test(phone)) {
        message.warning(t('validation.phoneInvalid'))
        return
      }
    }
    // 父页面可注入额外校验 (典型: 图形验证码必填). 返回 {ok:false} 中止, 不进倒计时.
    let captcha: { id: string; code: string } | undefined
    if (beforeSend) {
      const r = await beforeSend()
      if (!r.ok) {
        if (r.error) message.warning(r.error)
        return
      }
      captcha = r.captcha
    }
    setSending(true)
    try {
      // scene 同时承担三件事:后端冷却策略 + 日志分流 + 短信模板选择
      await sendSmsCode(phone, scene, captcha)
      // 写时间戳到 localStorage. TTL 比窗口略长, 避免临界 race 读不到
      const until = Date.now() + seconds * 1000
      storage.set(cooldownKey(scene, phone), { until }, seconds + 5)
      // ⚠️ 变量名必须叫 `scene` —— 字典模板是 '验证码已发送({scene})'。
      //   传 `{ bizType: ... }` 的话占位符替换不上,用户看到的是
      //   字面量「验证码已发送({scene})」。
      message.success(t('sms.codeSent', undefined, { scene: t(SMS_SCENE_LABEL_KEY[scene]) }))
      syncWithStorage()
    } catch {
      /* axios 拦截器已统一提示 */
    } finally {
      setSending(false)
      // ⚠️ **成功和失败都要换图** —— 后端是先删后比,失败也消费掉了这张验证码。
      //   不换图的话:成功 → 下一步拿废验证码必失败;失败 → 用户卡死。
      //   (详见 onCaptchaConsumed 的注释)
      if (captcha) onCaptchaConsumed?.()
    }
  }

  return (
    <Button
      size={size}
      loading={sending}
      disabled={countdown > 0}
      onClick={handleClick}
      block
    >
      {countdown > 0 ? t('sms.resendAfter', undefined, { n: countdown }) : t('sms.send')}
    </Button>
  )
}

export default SmsSendButton
