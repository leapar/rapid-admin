/**
 * OfflineBanner — 顶部网络断开提示
 * 2026-10-19 M64:断网时顶部黄色 banner 提示,网络恢复自动消失
 *
 * 设计:
 *  - 固定顶部(top:0, z-index 高于 layout 头部)
 *  - 仅 offline 时显示(不影响正常布局)
 *  - 网络恢复时调 useAuth 的 refresh 或 useTable 的 reload 是组件外的事
 *    此组件只负责"提示",不做事
 */
import React from 'react'
import { useOnline } from '@/hooks/useOnline'
import { WifiOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'

export const OfflineBanner: React.FC = () => {
  const online = useOnline()
  const { t } = useI18n()
  if (online) return null
  return (
    <div
      role="alert"
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        zIndex: 9999,
        background: '#faad14',
        color: '#fff',
        padding: '6px 16px',
        textAlign: 'center',
        fontSize: 13,
        fontWeight: 500,
        boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 8,
      }}
    >
      <WifiOutlined />
      {t('component.offline.banner')}
    </div>
  )
}
