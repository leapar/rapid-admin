/**
 * 500 - 服务器错误
 *
 * Phase 5 M10 i18n 化 + 增"复制错误码"按钮
 *  - 错误码从 sessionStorage / url 拿,提供给运维查日志
 *  - 默认显示 server timestamp 作为占位
 */
import React from 'react'
import { Result, Button, Space, App } from 'antd'
import { useI18n } from '@/locales'

const ServerError: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  // 错误码:url ?code=xxx > sessionStorage > 当前 timestamp
  const code =
    new URLSearchParams(window.location.search).get('code') ||
    sessionStorage.getItem('last_error_code') ||
    String(Date.now())

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      message.success(t('error.page500.copied'))
    } catch {
      // 降级
      const ta = document.createElement('textarea')
      ta.value = code
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
      message.success(t('error.page500.copied'))
    }
  }

  return (
    <Result
      status="500"
      title={t('error.page500.title')}
      subTitle={t('error.page500.subTitle')}
      extra={
        <Space direction="vertical" align="center">
          <Space>
            <Button type="primary" onClick={() => window.location.reload()}>
              {t('error.page500.retry')}
            </Button>
            <Button onClick={copy}>
              {t('error.page500.codeLabel', undefined, { code: code.slice(0, 8) })}
            </Button>
          </Space>
        </Space>
      }
    />
  )
}

export default ServerError
