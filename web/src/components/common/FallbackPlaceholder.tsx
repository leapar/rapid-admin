/**
 * FallbackPlaceholder — 轻量级降级占位
 * 2026-10-19 M66:组件级降级 UI
 * 2026-10-25 M7:文案走 i18n(此前写死中文,英文环境下会出现中文)
 *
 * 用法:
 *   - 网络挂了,卡片/区块显示 "加载失败,点重试" 而非整页白屏
 *   - 父组件 catch 错误后渲染 <FallbackPlaceholder onRetry={reload} />
 *   - 或配合 <ErrorBoundary> 局部包裹
 *
 * vs ErrorBoundary:
 *   - ErrorBoundary = React 渲染错误的自动兜底
 *   - FallbackPlaceholder = 业务方显式 catch 后手动渲染(粒度更细)
 */
import type React from 'react'
import { Empty, Button } from 'antd'
import { ReloadOutlined, DisconnectOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'

export interface FallbackPlaceholderProps {
  /** 错误信息(显示在描述里) */
  error?: Error | string
  /** 重试回调 */
  onRetry?: () => void
  /** 风格:'network' 强调断网(灰底 wifi 图标),'empty' 通用空白(默认) */
  variant?: 'network' | 'empty'
  /** 自定义高度(默认 200) */
  height?: number
  /** 自定义文案 */
  text?: string
}

export const FallbackPlaceholder: React.FC<FallbackPlaceholderProps> = ({
  error,
  onRetry,
  variant = 'empty',
  height = 200,
  text,
}) => {
  const { t } = useI18n()
  // ⚠️ 2026-10-25 M7:默认文案走 i18n
  //   此前这里写死 "加载失败" / "重试",英文环境下整块降级 UI 是中文。
  const desc = text || (error ? String((error as Error).message || error) : t('common.loadFailed'))
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight: height,
        width: '100%',
      }}
    >
      <Empty
        image={variant === 'network' ? Empty.PRESENTED_IMAGE_SIMPLE : Empty.PRESENTED_IMAGE_DEFAULT}
        description={
          <div>
            <div style={{ color: 'var(--app-text-tertiary)', marginBottom: 4 }}>
              {variant === 'network' ? <DisconnectOutlined style={{ marginRight: 6 }} /> : null}
              {desc}
            </div>
            {onRetry && (
              <Button type="link" icon={<ReloadOutlined />} onClick={onRetry}>
                {t('common.retry')}
              </Button>
            )}
          </div>
        }
      />
    </div>
  )
}
