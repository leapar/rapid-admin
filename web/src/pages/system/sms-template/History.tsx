/**
 * 短信模板流转历史抽屉 —— M115
 * 2026-10-30 新建
 *
 * 挂在短信模板列表的行操作上(按钮级权限 button.sms_template.history),
 * 不做成独立路由 —— 历史必须按 scene 过滤才有意义,
 * 脱离模板单独一个「所有模板的流转历史」入口在四个场景的规模下只会让人分不清在看谁。
 *
 * # ⚠️ 这张表回答的是「谁在什么时候把这张模板弄成这样的」
 *
 *   排障时最常问的一句话是"这个状态是谁改的、为什么" ——
 *   人工停用写一条(operator = 运维登录名),
 *   服务商同步也写一条(operator = 空,source = provider)。
 *   两者混在一起的话,看到一条 rejected 会误以为是人工拒的,
 *   而实际上那是服务商驳回的。
 *
 * # ⚠️ 状态标签走字典,不用后端返的 scene_label
 *
 *   状态文案与颜色来自 useDict('sms_template_status')(与列表页同一套),
 *   场景名走 i18n(constants/sms 的 SMS_SCENE_LABEL_KEY)。
 */
import { useState, useEffect, useCallback } from 'react'
import { Drawer, Table, Tag, Button, Space } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { smsTemplateApi } from '@/api/smsTemplate'
import { useI18n } from '@/locales'
import { useDict } from '@/hooks/useDict'
import { SMS_SCENE_LABEL_KEY, type SmsScene } from '@/constants/sms'
import type { SmsTemplateHistoryItem } from '@/types/smsTemplate'

export interface HistoryProps {
  open: boolean
  /** 场景标识 */
  scene: string
  onClose: () => void
}

export function History({ open, scene, onClose }: HistoryProps) {
  const { t } = useI18n()
  const { getLabel: getStatusLabel, getColor: getStatusColor } = useDict('sms_template_status')
  const [rows, setRows] = useState<SmsTemplateHistoryItem[]>([])
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    if (!scene) return
    setLoading(true)
    try {
      const res = await smsTemplateApi.history(scene)
      setRows(res ?? [])
    } catch {
      // 错误提示由 axios 拦截器统一弹出,这里吞掉只为了让表格别卡在 loading
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [scene])

  useEffect(() => {
    if (!open) return
    load()
  }, [open, load])

  /** 场景名走 i18n(后端返的 scene_label 是写死的中文,不用) */
  const sceneLabel = scene
    ? (() => {
        const key = SMS_SCENE_LABEL_KEY[scene as SmsScene]
        return key ? t(key) : scene
      })()
    : ''

  /** 状态标签(字典查不到时显示原值) */
  const statusTag = (v: string) => {
    if (!v) return <Tag color="default">-</Tag>
    const label = getStatusLabel(v)
    if (label === String(v)) return <Tag color="default">{v}</Tag>
    return <Tag color={getStatusColor(v)}>{label}</Tag>
  }

  const columns = [
    {
      title: t('field.opTime'),
      dataIndex: 'at',
      width: 165,
      render: (v: string) => (v ? v.slice(0, 19).replace('T', ' ') : '-'),
    },
    {
      title: t('field.statusChange'),
      dataIndex: 'from',
      width: 190,
      render: (v: string, row: SmsTemplateHistoryItem) => (
        <Space size={4}>
          {statusTag(v)}
          <span style={{ color: 'var(--app-text-tertiary)' }}>→</span>
          {statusTag(row.to)}
        </Space>
      ),
    },
    {
      title: t('field.source'),
      dataIndex: 'source',
      width: 90,
      render: (v: string) =>
        v === 'provider'
          ? t('page.smsTemplate.source.provider')
          : t('page.smsTemplate.source.manual'),
    },
    {
      title: t('field.operator'),
      dataIndex: 'operator',
      width: 120,
      // 服务商流转没有操作人 —— 显示 '-' 而不是空白,
      // 免得"空白"被当成"这一条没记录来源"
      render: (v: string) => v || '-',
    },
    {
      title: t('field.templateCode'),
      dataIndex: 'template_code',
      width: 180,
      render: (v: string) =>
        v ? <code style={{ fontSize: 12 }}>{v}</code> : <span style={{ color: 'var(--app-text-tertiary)' }}>-</span>,
    },
    {
      title: t('field.reason'),
      dataIndex: 'reason',
      width: 240,
      ellipsis: true,
      render: (v: string) => v || '-',
    },
  ]

  return (
    <Drawer
      title={`${t('page.smsTemplate.historyTitle')} · ${sceneLabel}`}
      open={open}
      onClose={onClose}
      width={920}
      destroyOnHidden
    >
      <Space style={{ marginBottom: 12 }} wrap>
        <Button icon={<ReloadOutlined />} onClick={load}>
          {t('common.refresh')}
        </Button>
      </Space>

      <Table<SmsTemplateHistoryItem>
        // ⚠️ 历史记录**没有 id 字段** —— 后端按 (scene, at, from, to) 排序返回,
        //   没有稳定主键。用「时间 + 状态 + 模板号」拼一个行 key,
        //   否则 React 会用数组下标当 key,排序变化时行内容对不上。
        rowKey={(r, i) => `${r.at}|${r.from}|${r.to}|${r.template_code}|${i}`}
        size="small"
        columns={columns}
        loading={loading}
        dataSource={rows}
        pagination={false}
        locale={{ emptyText: t('page.smsTemplate.noHistory') }}
      />
    </Drawer>
  )
}

export default History
