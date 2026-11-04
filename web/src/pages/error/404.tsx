/**
 * 404 - 页面不存在
 *
 * Phase 5 M10 i18n 化
 *  - title / subTitle / button 全部走 useI18n
 */
import React from 'react'
import { Result, Button, Space } from 'antd'
import { useNavigate } from 'react-router-dom'
import { useI18n } from '@/locales'

const NotFound: React.FC = () => {
  const { t } = useI18n()
  const navigate = useNavigate()
  return (
    <Result
      status="404"
      title={t('error.page404.title')}
      subTitle={
        <Space direction="vertical" size={4}>
          <span>{t('error.page404.subTitle')}</span>
          <span style={{ fontSize: 12, color: 'var(--app-text-tertiary)' }}>{t('error.page404.hint')}</span>
        </Space>
      }
      extra={
        <Button type="primary" onClick={() => navigate('/')}>
          {t('error.page404.backHome')}
        </Button>
      }
    />
  )
}

export default NotFound
