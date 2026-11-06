/**
 * 克隆台账 tab —— sys_voice_clone(每行是一次克隆,或一次失败尝试)
 *
 * ⚠️ **懒加载**:台账只在 tab 激活时才拉 —— 克隆是低频动作,
 * 大多数人打开页面只看音色列表,没必要每次都多打一枪 voice_clone:list
 * (menuRequiredAPIs 的最小集口径也是这么登记的)。
 */
import { useCallback, useEffect, useState } from 'react'
import type { FC } from 'react'
import { Button, Tag, App } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { ManagementTable } from '@/components/common/ManagementTable'
import { RowActions } from '@/components/common/RowActions'
import type { RowAction } from '@/components/common/RowActions'
import { useI18n } from '@/locales'
import { voiceApi } from '@/api/voice'
import { PermVoice } from '@/constants/permissions'
import type { VoiceClone } from '@/types/voice'

const ClonesTab: FC<{ active: boolean }> = ({ active }) => {
  const { t } = useI18n()
  const { message, modal } = App.useApp()

  const [rows, setRows] = useState<VoiceClone[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size] = useState(20)
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const load = useCallback(
    async (p = page, kw = keyword) => {
      setLoading(true)
      try {
        const res = await voiceApi.listClones({ page: p, size, keyword: kw || undefined })
        setRows(res.list)
        setTotal(res.total)
      } catch {
        setRows([])
      } finally {
        setLoading(false)
      }
    },
    [page, size, keyword],
  )

  useEffect(() => {
    // 台账 tab 激活才拉;page / keyword 变化经 load 重建触发重拉
    if (active) load()
  }, [active, load])

  const refreshStatus = async (row: VoiceClone) => {
    try {
      await voiceApi.refreshClone(row.id)
      message.success(t('page.voice.refreshed'))
      load()
    } catch {
      // 拦截器已弹错(含"失败记录没有 voice_id 无法刷新"的后端提示)
    }
  }

  const handleDelete = (row: VoiceClone) => {
    modal.confirm({
      title: t('common.delete'),
      content: `${row.name || row.voice_id || '-'}`,
      okText: t('common.ok'),
      okButtonProps: { danger: true },
      cancelText: t('common.cancel'),
      onOk: async () => {
        await voiceApi.deleteClone(row.id)
        message.success(t('common.success'))
        load()
      },
    })
  }

  const statusTag = (v: string) => {
    const map: Record<string, { label: string; color: string }> = {
      DEPLOYING: { label: t('page.voice.statusDeploying'), color: 'processing' },
      OK: { label: t('page.voice.statusOk'), color: 'success' },
      UNDEPLOYED: { label: t('page.voice.statusUndeployed'), color: 'error' },
      FAILED: { label: t('page.voice.statusFailed'), color: 'error' },
    }
    const conf = map[v]
    return <Tag color={conf?.color ?? 'default'}>{conf?.label ?? (v || '-')}</Tag>
  }

  const columns = [
    {
      title: t('field.name'),
      dataIndex: 'name',
      width: 160,
      fixed: 'left' as const,
      render: (v: string) => v || '-',
    },
    {
      title: t('field.voiceId'),
      dataIndex: 'voice_id',
      width: 220,
      render: (v: string) => (v ? <code style={{ fontSize: 12 }}>{v}</code> : '-'),
    },
    { title: t('common.status'), dataIndex: 'status', width: 100, render: statusTag },
    { title: t('field.targetModel'), dataIndex: 'target_model', width: 170, render: (v: string) => v || '-' },
    {
      title: t('page.voice.errorMsg'),
      dataIndex: 'error_msg',
      width: 260,
      ellipsis: true,
      render: (v: string) => v || '-',
    },
    {
      title: t('common.createTime'),
      dataIndex: 'created_at',
      width: 165,
      render: (v: string) => (v ? v.slice(0, 19).replace('T', ' ') : '-'),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 160,
      fixed: 'right' as const,
      render: (_: unknown, row: VoiceClone) => {
        const actions: RowAction<VoiceClone>[] = [
          {
            key: 'refresh',
            label: t('page.voice.refreshStatus'),
            perm: PermVoice.CLONE_REFRESH,
            disabled: !row.voice_id,
            disabledTip: t('page.voice.refreshUnavailable'),
            onClick: (r) => refreshStatus(r),
          },
          {
            key: 'delete',
            label: t('common.delete'),
            danger: true,
            perm: PermVoice.CLONE_DELETE,
            onClick: (r) => handleDelete(r),
          },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  return (
    <ManagementTable<VoiceClone>
      onSearch={(kw) => {
        setKeyword(kw)
        setPage(1)
      }}
      onReset={() => {
        setKeyword('')
        setPage(1)
      }}
      inlineToolBar={[
        <Button key="reload" icon={<ReloadOutlined />} onClick={() => load()}>
          {t('common.refresh')}
        </Button>,
      ]}
      columns={columns}
      tableProps={{
        loading,
        dataSource: rows,
        rowKey: 'id',
        pagination: {
          current: page,
          pageSize: size,
          total,
          showTotal: (n) => `${n}`,
          onChange: (p) => {
            // 同 VoiceRegistry:只改 state,useEffect 统一拉
            setPage(p)
          },
        },
      }}
      scrollX={1240}
    />
  )
}

export default ClonesTab
