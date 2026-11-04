/**
 * ColumnSettings —— 列设置面板(§2.8 #11 P2)
 *
 * 用于 useTablePro 的列设置 UI:
 *  - 显示/隐藏列(checkbox)
 *  - 调整列宽(number input)
 *  - 一键 reset
 */
import React from 'react'
import { Modal, Checkbox, InputNumber, Button, Space, Divider } from 'antd'
import { TableProColumn } from '@/hooks/useTablePro'
import { useI18n } from '@/locales'

export interface ColumnSettingsProps {
  open: boolean
  onClose: () => void
  columns: TableProColumn[]
  states: Array<{ key: string; visible: boolean; width: number }>
  onVisibleChange: (key: string, visible: boolean) => void
  onWidthChange: (key: string, width: number) => void
  onReset: () => void
}

export const ColumnSettings: React.FC<ColumnSettingsProps> = ({
  open,
  onClose,
  columns,
  states,
  onVisibleChange,
  onWidthChange,
  onReset,
}) => {
  const stateMap = new Map(states.map((s) => [s.key, s]))
  const { t } = useI18n()
  return (
    <Modal
      title={t('component.columnSettings.title')}
      open={open}
      onCancel={onClose}
      footer={[
        <Button key="reset" onClick={onReset}>
          {t('common.resetDefault')}
        </Button>,
        <Button key="close" type="primary" onClick={onClose}>
          {t('component.columnSettings.done')}
        </Button>,
      ]}
      width={520}
    >
      <div style={{ maxHeight: 480, overflowY: 'auto' }}>
        {columns.map((c) => {
          const s = stateMap.get(c.key) || { visible: true, width: c.width || 120 }
          const hideable = c.hideable !== false
          return (
            <div
              key={c.key}
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                padding: '8px 0',
                borderBottom: '0.5px dashed var(--app-border-secondary)',
              }}
            >
              <Checkbox
                /**
                 * ⚠️ **M111 修正:不可隐藏的列一律显示为勾选**
                 *   不可隐藏的列复选框是**禁用**的 ⇒ 用户改不了它。
                 *   以前这里直接用存储里的 `s.visible` ⇒ 一旦旧版本的
                 *   localStorage 把它存成 `visible: false`,面板显示"未勾选",
                 *   而表格里(useTablePro 现在)却**照常显示**这一列 ——
                 *   面板和表格对不上,用户看着一个"没勾上但确实在"的列,
                 *   只能干瞪眼。
                 *   现在统一:不可隐藏 ⇒ 面板显示勾选 + 禁用,跟表格一致。
                 */
                checked={hideable ? s.visible : true}
                disabled={!hideable}
                onChange={(e) => onVisibleChange(c.key, e.target.checked)}
              >
                {c.title}
              </Checkbox>
              <Space>
                <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>
                  {t('component.columnSettings.width')}
                </span>
                <InputNumber
                  size="small"
                  min={60}
                  max={600}
                  step={10}
                  value={s.width}
                  onChange={(v) => v != null && onWidthChange(c.key, v)}
                  style={{ width: 80 }}
                />
              </Space>
            </div>
          )
        })}
      </div>
      <Divider style={{ margin: '12px 0' }} />
      <div style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>{t('component.columnSettings.storageHint')}</div>
    </Modal>
  )
}
