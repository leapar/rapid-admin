/**
 * SensitiveField —— 敏感字段展示(自动脱敏 + 显隐切换)
 *
 * 配合"用户信息脱敏"安全要求
 */
import React, { useState } from 'react'
import { Space, Button, Tooltip } from 'antd'
import { EyeOutlined, EyeInvisibleOutlined } from '@ant-design/icons'
import { mask } from '@/utils/format'
import { useI18n } from '@/locales'

export type SensitiveType = 'phone' | 'idCard' | 'bankCard' | 'email' | 'name' | 'auto'

export interface SensitiveFieldProps {
  value?: string
  type?: SensitiveType
  /** 是否有权限看明文(后端判断) */
  canView?: boolean
  /** 默认是脱敏还是明文 */
  defaultVisible?: boolean
  placeholder?: string
}

export const SensitiveField: React.FC<SensitiveFieldProps> = ({
  value,
  type = 'auto',
  canView = false,
  defaultVisible = false,
  placeholder = '-',
}) => {
  const { t } = useI18n()
  const [visible, setVisible] = useState(defaultVisible)

  if (!value) return <span style={{ color: 'var(--app-text-quaternary)' }}>{placeholder}</span>

  // 非敏感或没权限看明文:直接脱敏
  if (!canView || !visible) {
    const masked =
      type === 'auto'
        ? /**
           * ⚠️ **M111 修正:先看 '@',再看长度**
           *   旧顺序是"11 位手机 → 18 位身份证 → 含 '@' 邮箱"——
           *   而**恰好 18 个字符的邮箱**会先撞上"18 位身份证"分支,
           *   被脱成 `zhangs********.com`(身份证样式),而不是
           *   `z***@exam.com`(邮箱样式)。用户看到的是一串星号,
           *   既认不出是邮箱,也保留了域名的形状 —— 脱敏反而泄露了信息。
           *   邮箱是**最确定的信号**(`@` 不可能出现在手机号/身份证里),
           *   所以放第一位判断。
           */
          value.includes('@')
          ? mask.email(value)
          : value.length === 11 && /^1[3-9]/.test(value)
            ? mask.phone(value)
            : value.length === 18
              ? mask.idCard(value)
              : mask.name(value)
        : mask[type](value)
    return (
      <Space>
        <span>{masked}</span>
        {canView && (
          <Tooltip title={t('component.sensitiveField.viewPlain')}>
            <Button
              type="text"
              size="small"
              aria-label={t('component.sensitiveField.viewPlain')}
              icon={<EyeOutlined />}
              onClick={() => setVisible(true)}
            />
          </Tooltip>
        )}
      </Space>
    )
  }

  // 可见明文
  return (
    <Space>
      <span style={{ fontFamily: 'monospace' }}>{value}</span>
      <Tooltip title={t('component.sensitiveField.hide')}>
        <Button
          type="text"
          size="small"
          aria-label={t('component.sensitiveField.hide')}
          icon={<EyeInvisibleOutlined />}
          onClick={() => setVisible(false)}
        />
      </Tooltip>
    </Space>
  )
}

export default SensitiveField
