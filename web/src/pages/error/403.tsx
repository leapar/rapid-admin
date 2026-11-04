/**
 * 403 - 无权限
 *
 * Phase 5 M10 i18n 化
 *  - title / subTitle / button 全部走 useI18n
 *  - 仅"返回首页"按钮(2026-10-17 移除"联系管理员": 暂无法发邮件, mailto 形同虚设)
 */
import React from 'react'
import { Result, Button, Space } from 'antd'
import { useNavigate } from 'react-router-dom'
import { useI18n } from '@/locales'

const Forbidden: React.FC = () => {
  const { t } = useI18n()
  const navigate = useNavigate()
  return (
    <Result
      status="403"
      title={t('error.page403.title')}
      subTitle={
        <Space direction="vertical" size={4}>
          <span>{t('error.page403.subTitle')}</span>
          <span style={{ fontSize: 12, color: 'var(--app-text-tertiary)' }}>{t('error.page403.hint')}</span>
        </Space>
      }
      extra={
        <Button type="primary" onClick={() => navigate('/')}>
          {t('error.page403.backHome')}
        </Button>
      }
    />
  )
}

export default Forbidden
