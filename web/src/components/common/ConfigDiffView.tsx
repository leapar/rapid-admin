/**
 * ConfigDiffView —— 平台配置变更对比
 *
 * 用于配置中心审批流,展示"原值 vs 新值"差异
 */
import React, { useMemo } from 'react'
import { Card, Tag, Typography, Space, Alert } from 'antd'
import { ConfigItem } from '@/types/config'
import { useI18n } from '@/locales'

const { Text } = Typography

export interface ConfigDiffViewProps {
  /** 原配置(变更前快照) */
  before?: ConfigItem | null
  /** 新配置(变更后值) */
  after: Partial<ConfigItem>
  /** 字段名映射(可选) */
  fieldLabels?: Record<string, string>
}

const DEFAULT_FIELDS: Array<keyof ConfigItem> = [
  'config_name',
  'value',
  'description',
  'env_var_name',
  'is_active',
  'is_sensitive',
]

export const ConfigDiffView: React.FC<ConfigDiffViewProps> = ({
  before,
  after,
  fieldLabels,
}) => {
  const { t } = useI18n()
  const fields = useMemo(() => {
    return DEFAULT_FIELDS.map((key) => {
      const beforeVal = before?.[key]
      const afterVal = after[key]
      const changed = String(beforeVal ?? '') !== String(afterVal ?? '')
      return {
        key,
        label: fieldLabels?.[key] || String(key),
        beforeVal,
        afterVal,
        changed,
      }
    }).filter((f) => f.beforeVal !== undefined || f.afterVal !== undefined)
  }, [before, after, fieldLabels])

  const hasSensitive = before?.is_sensitive || after.is_sensitive

  return (
    <Card title={t('component.configDiff.title')} size="small">
      {hasSensitive && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message={t('component.configDiff.sensitiveHint')}
        />
      )}
      {fields.map((f) => {
        const displayBefore = hasSensitive && f.key === 'value'
          ? '******'
          : (f.beforeVal == null ? <Text type="secondary">{t('common.emptyValue')}</Text> : String(f.beforeVal))
        const displayAfter = hasSensitive && f.key === 'value'
          ? '******'
          : (f.afterVal == null ? <Text type="secondary">{t('common.emptyValue')}</Text> : String(f.afterVal))
        return (
          <div
            key={String(f.key)}
            style={{
              display: 'grid',
              gridTemplateColumns: '120px 1fr 24px 1fr',
              alignItems: 'center',
              gap: 12,
              padding: '8px 0',
              borderBottom: '0.5px dashed var(--app-border-secondary)',
            }}
          >
            <Text strong>{f.label}</Text>
            <div style={{ background: 'var(--app-diff-del-bg)', padding: '4px 8px', borderRadius: 4 }}>
              <Space>{f.changed && <Tag color="red">{t('component.configDiff.old')}</Tag>}{displayBefore}</Space>
            </div>
            <span style={{ textAlign: 'center' }}>→</span>
            <div style={{ background: 'var(--app-diff-add-bg)', padding: '4px 8px', borderRadius: 4 }}>
              <Space>{f.changed && <Tag color="green">{t('component.configDiff.new')}</Tag>}{displayAfter}</Space>
            </div>
          </div>
        )
      })}
    </Card>
  )
}

export default ConfigDiffView
