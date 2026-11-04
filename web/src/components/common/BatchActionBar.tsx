/**
 * BatchActionBar —— 批量操作条(§2.8 #14,P1)
 *
 * 选中行时出现在工具栏里:左边"已选 N 项",右边是业务给的批量动作。
 *
 * 用法:
 *   const sel = useTableSelection<SysUser>({ data })
 *   <BatchActionBar count={sel.selectedCount} onClear={sel.clear}>
 *     <AuthButton danger perm={PermButton.USER_DELETE} onClick={...}>批量删除</AuthButton>
 *   </BatchActionBar>
 *
 * 设计:
 *   · count <= 0 → 整个组件不渲染(业务页不用写条件判断,少一处 state 漏清)
 *   · **权限过滤不归这里管** —— children 里放 AuthButton / RowActions,
 *     组件本身不感知权限(跟 RowActions 一样的分工)
 *   · 文案走 i18n(common.selected / common.clearSelection)
 */
import type React from 'react'
import { Button, Space, Typography } from 'antd'
import { useI18n } from '@/locales'

const { Text } = Typography

export interface BatchActionBarProps {
  /** 已选数量(<= 0 不渲染) */
  count: number
  /** 清除选择 */
  onClear: () => void
  /** 批量动作按钮(通常是 AuthButton,权限自己判) */
  children?: React.ReactNode
  /** 左侧文案(默认"已选 {n} 项") */
  text?: string
  /** 额外内容(放在"取消选择"之前) */
  extra?: React.ReactNode
}

export const BatchActionBar: React.FC<BatchActionBarProps> = ({
  count,
  onClear,
  children,
  text,
  extra,
}) => {
  const { t } = useI18n()

  if (count <= 0) return null

  return (
    <Space
      size={8}
      wrap
      style={{
        background: 'var(--app-selected-bg)',
        border: '0.5px solid var(--app-selected-border)',
        borderRadius: 6,
        padding: '2px 12px',
      }}
    >
      <Text strong>{text ?? t('common.selected', undefined, { n: count })}</Text>
      {extra}
      {children}
      <Button type="link" size="small" onClick={onClear}>
        {t('common.clearSelection')}
      </Button>
    </Space>
  )
}

export default BatchActionBar
