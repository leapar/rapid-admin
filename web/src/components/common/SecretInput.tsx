/**
 * SecretInput —— 敏感值输入组件(Form 适配版)
 *
 * 2026-10-18 收窄:
 *   - 系统配置只走 PATCH update 一条通道,SecretInput 纯前端切明文/密文
 *   - 不调后端 GetConfigPlain(已删),不写 audit view 记录
 *   - 留空 = 不修改,后端 DirectUpdate pointer 风格
 *   - 受控 value/onChange,直接接 <Form.Item name="...">
 *
 * 设计意图(2026-10-18 决策):
 *   - SRS 早期版本要求"查看明文走 SMS 二次验证 + 写 audit",12/15 上线版
 *     改为纯前端切;若 P1+ 真要 SMS 验证,前端加 modal + 调新增 endpoint
 *   - 当前:前端点眼睛 = 切 value/text 与 value/'********',后端完全无感
 *   - 已知限制:同一台设备/浏览器内 value 已在内存,无安全加成;P1+ 加密钥分片
 *
 * 2026-10-26:输入框**不再用 type="password"**(掩码态也是 type="text")
 *   —— 页面里只要有 password 框,Chrome 就会把整页当登录页,
 *   自动把存着的账号填进页面上唯一的文本框(系统配置页的搜索框会凭空
 *   出现 "admin")。掩码靠 display 里的 '********' 实现,不用 password 类型。
 *   详见 JSX 处的注释。
 */
import { useState } from 'react'
import type { FC } from 'react'
import { Input, Button, Tooltip, Space } from 'antd'
import { EyeOutlined, EyeInvisibleOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'

export interface SecretInputProps {
  value?: string
  onChange?: (value: string) => void
  placeholder?: string
  disabled?: boolean
  /** 是否启用眼睛切换(默认 true) */
  toggleable?: boolean
}

export const SecretInput: FC<SecretInputProps> = ({
  value = '',
  onChange,
  placeholder,
  disabled,
  toggleable = true,
}) => {
  const { t } = useI18n()
  const [visible, setVisible] = useState(false)

  // 脱敏态:有原文 → 显示 ********;无原文 → 显示空
  const display = visible ? value : value ? '********' : ''

  return (
    <Space.Compact style={{ width: '100%' }}>
      <Input
        // 2026-10-26:掩码态也用 type="text",不用 type="password"
        //
        //   掩码效果不受影响 —— 藏起来的时候 value 已经被替换成 '********',
        //   DOM 里根本没有明文(见上面的 display)。
        //
        //   换掉的理由:**页面上只要有一个 type="password",Chrome 就把整页
        //   当登录页**,然后把页面上唯一的文本框当"用户名框",自动填它存着的
        //   账号。实测:在系统配置页(本组件唯一的使用方),Chrome 会把
        //   localhost:5173/login 的账号 "admin" 填进搜索框 —— 框里凭空多一个
        //   admin,下面的列表却还是全量(框里的字和过滤状态是两回事)。
        //   页面里没有 password 框,这个"幽灵 admin"就没了(实测 autofill
        //   状态从 autofilled 变回普通)。
        //
        //   ⚠️ 别改回 type="password":autocomplete="off" / name=... /
        //   autocomplete="new-password" 都试过,挡不住 Chrome 的密码管理器
        //   把这个页面当登录页。
        type="text"
        value={display}
        onChange={(e) => onChange?.(e.target.value)}
        placeholder={placeholder ?? t('common.placeholder.keepUnchanged')}
        disabled={disabled}
        autoComplete="off"
      />
      {toggleable && (
        <Tooltip title={visible ? t('component.secretInput.hide') : t('component.secretInput.show')}>
          <Button
            icon={visible ? <EyeInvisibleOutlined /> : <EyeOutlined />}
            onClick={() => setVisible((v) => !v)}
            disabled={disabled}
            aria-label={visible ? t('component.secretInput.hide') : t('component.secretInput.show')}
          />
        </Tooltip>
      )}
    </Space.Compact>
  )
}

export default SecretInput
