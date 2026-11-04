/**
 * CaptchaField —— 图形验证码输入框
 *
 * 设计目标: 通用组件, 父页面只关心 { id, code } 这一对值.
 *
 * 内部状态:
 *   - id / image: 来自后端 CaptchaService.Generate 的 base64 PNG
 *   - 加载中: 切换时 loading
 *
 * 父页面交互:
 *   - 受控: value (用户输入的 code) + onChange({id, code})
 *   - 触发重拉: 改 refreshKey (e.g. setRefreshKey(Date.now())) — 用于登录失败后强制换图
 *   - 点击图片: 自动重拉
 *
 * 集成到 antd Form:
 *   <Form.Item name="captcha" rules={[{ required: true }]}>
 *     <CaptchaField size="large" refreshKey={refreshKey} />
 *   </Form.Item>
 *   form.getFieldsValue().captcha => { id, code } (整对象)
 *
 * 为什么不走 getValueFromEvent: onChange({id, code}) 直接当 Form 字段值,
 *   父页面的 form.validateFields() / getFieldsValue() 拿到的就是对象, 不需拆字段.
 *
 * 布局:
 *   输入框 (flex:1) + 图片 (120x40 视觉 4 位数字) 同一行, image cursor: pointer
 *   login 页 420 卡片容得下; 大屏自动拉宽
 */
import React, { useEffect, useState } from 'react'
import { Input, Spin } from 'antd'
import type { SizeType } from 'antd/es/config-provider/SizeContext'
import { ReloadOutlined } from '@ant-design/icons'
import { getCaptcha } from '@/api/auth'
import { usePublicConfigStore } from '@/stores/publicConfig'
import { useI18n } from '@/locales'

export interface CaptchaValue {
  id: string
  code: string
}

export interface CaptchaFieldProps {
  /**
   * 受控值. 两种形态都接受:
   *   - 简单用法: 直接传 string (用户输入的 code)
   *   - 配合 antd Form: Form 会把整对象 {id, code} 回灌, 内部自动剥出 .code 渲染
   */
  value?: string | CaptchaValue | null
  /** 变化时回调, 父页面/Form 拿到的就是 {id, code} */
  onChange?: (v: CaptchaValue) => void
  size?: SizeType
  disabled?: boolean
  /** 变化时重新拉取, 典型用法: setRefreshKey(Date.now()) */
  refreshKey?: number | string
  placeholder?: string
}

export const CaptchaField: React.FC<CaptchaFieldProps> = ({
  value,
  onChange,
  size = 'large',
  disabled,
  refreshKey,
  placeholder,
}) => {
  const { t } = useI18n()
  const [id, setId] = useState<string>('')
  const [image, setImage] = useState<string>('')
  const [loading, setLoading] = useState(false)
  // 2026-10-28 M106:拉取失败标记 —— 以前失败后 image 一直是空,
  //   渲染分支 `loading || !image` 会永远显示 Spin ⇒ **界面在撒谎**
  //   ("还在加载"),而请求其实早就断了,用户只能干等。
  const [failed, setFailed] = useState(false)

  // 两个开关都关时整个组件不该存在。父页面已按此条件渲染,
  // 这里再兜一层:store 是异步的,若开关值在 mount 之后才变成 false,
  // 已排队的 fetch 会被这次守卫挡掉,不多打一次 /public/captcha 请求。
  const captchaEnabled = usePublicConfigStore((s) => s.isAnyCaptchaEnabled())

  // 兼容两种 value 形态: 直接 string / Form 回灌的 {id, code}
  // Input 必须是 string, 不然会渲染成 [object Object]
  const code = typeof value === 'object' && value !== null
    ? (value as CaptchaValue).code ?? ''
    : (value as string | undefined) ?? ''

  const fetch = async () => {
    if (!usePublicConfigStore.getState().isAnyCaptchaEnabled()) return
    setLoading(true)
    setFailed(false)
    try {
      const r = await getCaptcha()
      setId(r.id)
      setImage(r.image)
      // 拉新图时, 已输入的 code 必定失效 (id 已变), 主动清空
      onChange?.({ id: r.id, code: '' })
    } catch {
      // 拦截器已统一提示; 不阻塞 UI
      //
      // ⚠️ **不清 image、不清输入框** —— 这次失败的是"拉新图",
      //   不是"校验验证码":服务端手里那张旧验证码**并没有被消费**,
      //   用户看着旧图输的码仍然有效。把它清掉等于白白逼用户重填。
      //   但必须把 failed 标出来,否则界面会一直转圈(见 failed 的注释)。
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }

  // mount / refreshKey 变化 → 重新拉
  useEffect(() => {
    fetch()
    // fetch 内部使用了 onChange (受控), 但父页面的 onChange 在 props 里引用稳定
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshKey])

  // 开关关掉的瞬间把已渲染的验证码抹掉,避免用户填了却不被校验的错觉
  useEffect(() => {
    if (!captchaEnabled) {
      setId('')
      setImage('')
      onChange?.({ id: '', code: '' })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [captchaEnabled])

  return (
    <div style={{ display: 'flex', gap: 8, alignItems: 'stretch', width: '100%' }}>
      <Input
        size={size}
        value={code}
        onChange={(e) => onChange?.({ id, code: e.target.value })}
        disabled={disabled}
        maxLength={4}
        placeholder={placeholder ?? t('common.placeholder.captcha')}
        style={{ flex: 1 }}
      />
      <div
        role="button"
        aria-label={t('component.captcha.refreshAria')}
        onClick={() => !loading && !disabled && fetch()}
        style={{
          width: 120,
          height: size === 'large' ? 40 : 32,
          border: '0.5px solid var(--app-border, #d9d9d9)',
          borderRadius: 6,
          background: 'var(--app-surface-bg)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          cursor: loading || disabled ? 'not-allowed' : 'pointer',
          overflow: 'hidden',
          flexShrink: 0,
          position: 'relative',
        }}
        title={t('component.captcha.refreshTitle')}
      >
        {loading ? (
          <Spin size="small" />
        ) : failed && !image ? (
          // 2026-10-28 M106:拉取失败且手里没有旧图 —— 显示"点我重试",
          //   不再永远转圈(以前 `loading || !image` 让界面一直假装"还在加载")
          <ReloadOutlined style={{ fontSize: 16, color: 'var(--app-text-tertiary)' }} />
        ) : image ? (
          <img
            src={image}
            alt="captcha"
            draggable={false}
            style={{ width: '100%', height: '100%', objectFit: 'contain', userSelect: 'none', pointerEvents: 'none' }}
          />
        ) : (
          <Spin size="small" />
        )}
        {!loading && image && (
          <ReloadOutlined
            style={{
              position: 'absolute',
              right: 4,
              bottom: 2,
              fontSize: 10,
              color: 'var(--app-text-tertiary)',
              pointerEvents: 'none',
            }}
          />
        )}
      </div>
    </div>
  )
}

export default CaptchaField
