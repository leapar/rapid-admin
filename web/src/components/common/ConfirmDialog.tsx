/**
 * ConfirmDialog —— 通用确认弹窗
 *
 * 替代 Modal.confirm 散落用法 (因为 App.useApp().modal.confirm 调用栈深,业务页直接用 this hook 麻烦)
 * 统一用受控 state: <ConfirmDialog open={...} onCancel={...} onOk={...} />
 *
 * 用法:
 *   const [del, setDel] = useState<{ id: string; name: string } | null>(null)
 *   <ConfirmDialog
 *     open={!!del}
 *     title={`删除 ${del?.name}?`}
 *     description="删除后将无法恢复,关联的 XX 记录也会被清理"
 *     danger
 *     onCancel={() => setDel(null)}
 *     onOk={async () => {
 *       await api.remove(del!.id)
 *       setDel(null)
 *       reload()
 *     }}
 *   />
 */
import React from 'react'
import { Modal, Button, Space, Typography } from 'antd'
import { ExclamationCircleFilled } from '@ant-design/icons'
import { useI18n } from '@/locales'

const { Text } = Typography

export interface ConfirmDialogProps {
  open: boolean
  title: string
  description?: React.ReactNode
  danger?: boolean
  okText?: string
  cancelText?: string
  /**
   * 确定按钮的额外属性
   *
   * ⚠️ **用来做"条件不满足就不让点确定"** —— 比如驳回意见必须写够 5 个字
   * (设计稿 §3.4),与其让用户点了之后被后端拒,不如直接把按钮置灰。
   * ⚠️ 组件自己渲染 footer,不用 antd 的 `okButtonProps` 那套。
   */
  okButtonProps?: { disabled?: boolean }
  /** 异步 onOk(loading 由组件管理) */
  onOk: () => Promise<void> | void
  onCancel: () => void
}

export const ConfirmDialog: React.FC<ConfirmDialogProps> = ({
  open,
  title,
  description,
  danger,
  okText,
  cancelText,
  okButtonProps,
  onOk,
  onCancel,
}) => {
  const { t } = useI18n()
  const [loading, setLoading] = React.useState(false)

  const handleOk = async () => {
    setLoading(true)
    try {
      await onOk()
    } finally {
      setLoading(false)
    }
  }

  return (
    <Modal
      open={open}
      title={
        <Space>
          {danger && <ExclamationCircleFilled style={{ color: '#ff4d4f' }} />}
          <span>{title}</span>
        </Space>
      }
      onOk={handleOk}
      onCancel={onCancel}
      maskClosable={false}
      footer={
        <Space>
          <Button onClick={onCancel} disabled={loading}>
            {cancelText ?? t('common.cancel')}
          </Button>
          <Button
            type="primary"
            danger={danger}
            loading={loading}
            disabled={okButtonProps?.disabled}
            onClick={handleOk}
          >
            {okText ?? t('common.confirm')}
          </Button>
        </Space>
      }
      destroyOnHidden
    >
      {description && <Text type="secondary">{description}</Text>}
    </Modal>
  )
}

export default ConfirmDialog
