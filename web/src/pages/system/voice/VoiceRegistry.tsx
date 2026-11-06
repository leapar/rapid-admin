/**
 * 音色登记册 tab —— 页面主表(sys_tts_voice)
 *
 * 拆成独立文件的理由:主 index 只管 tab 编排,这张表 + 两个弹窗
 * (添加/编辑音色)自成一体,克隆台账和合成面板各自一文件。
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import type { FC, ReactNode } from 'react'
import { Button, Tag, Tooltip, App, Space } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal } from '@/components/common/EditModal'
import type { EditField } from '@/components/common/EditModal'
import { RowActions } from '@/components/common/RowActions'
import type { RowAction } from '@/components/common/RowActions'
import { useI18n } from '@/locales'
import { usePermission } from '@/hooks/usePermission'
import { voiceApi, convertOnce } from '@/api/voice'
import { PermVoice } from '@/constants/permissions'
import type { TtsVoice, TtsVoiceForm } from '@/types/voice'

/** edge 行走 Edge 通道 —— 只出 mp3,试听文本无所谓,固定用后端同款 */
const DEMO_TEXT = '欢迎使用文本朗读，这是我的声音。'

const TTS_TYPE_OPTIONS = [
  { value: 'bailian', label: 'bailian' },
  { value: 'edge', label: 'edge' },
]

const SEX_OPTIONS = [
  { value: 1, labelKey: 'page.voice.male' },
  { value: 2, labelKey: 'page.voice.female' },
] as const

const VoiceRegistry: FC<{ active: boolean }> = ({ active }) => {
  const { t } = useI18n()
  const { message, modal } = App.useApp()
  const { has } = usePermission()
  const canCreate = has(PermVoice.CREATE)

  const [rows, setRows] = useState<TtsVoice[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [size] = useState(20)
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [ttsType, setTtsType] = useState<string | undefined>(undefined)
  /** undefined = 关闭;null = 新增;对象 = 编辑 */
  const [editing, setEditing] = useState<TtsVoice | null | undefined>(undefined)
  /** 试听:哪一行在合成中 + 当前播放器(切行时停掉上一个) */
  const [auditioning, setAuditioning] = useState<string | null>(null)
  const audioRef = useRef<HTMLAudioElement | null>(null)

  const load = useCallback(async (p = page, kw = keyword, tt = ttsType) => {
    setLoading(true)
    try {
      const res = await voiceApi.listVoices({ page: p, size, keyword: kw || undefined, tts_type: tt })
      setRows(res.list)
      setTotal(res.total)
    } catch {
      // 错误提示由 axios 拦截器统一弹出,这里只让表格别卡在 loading
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [page, size, keyword, ttsType])

  useEffect(() => {
    if (active) load()
  }, [active, load])

  useEffect(() => {
    // 卸载时停掉还在放的试听
    return () => {
      audioRef.current?.pause()
      if (audioRef.current) URL.revokeObjectURL(audioRef.current.src)
    }
  }, [])

  const stopAudio = () => {
    audioRef.current?.pause()
    if (audioRef.current) URL.revokeObjectURL(audioRef.current.src)
    audioRef.current = null
  }

  /**
   * 试听 —— 直接调 /tts/convert 合成一句固定文本。
   *
   * ⚠️ **不用 demo_key 直链** —— demo 音频的对象 key 在 OSS 上,平台没有
   *   "按 key 出音频"的接口(sys_file 是另一套按记录 id 的体系,登记册的
   *   demo 音频不走 sys_file)。按需合成一条是最短路径,权限码
   *   tts:convert 本来就在这个页面的手上。
   */
  const audition = async (row: TtsVoice) => {
    stopAudio()
    setAuditioning(row.id)
    try {
      const blob = await convertOnce({
        text: DEMO_TEXT,
        voice: row.id,
        provider: row.tts_type === 'edge' ? 'edge' : undefined,
        format: 'mp3',
      })
      const url = URL.createObjectURL(blob)
      const audio = new Audio(url)
      audioRef.current = audio
      audio.onended = () => {
        stopAudio()
        setAuditioning(null)
      }
      await audio.play()
    } catch {
      setAuditioning(null)
      // 失败提示由拦截器弹(bailian 未配 synth_url 会显式报错)
    }
  }

  const handleDelete = (row: TtsVoice) => {
    modal.confirm({
      title: t('common.delete'),
      content: `${row.name}(${row.voice_id})`,
      okText: t('common.ok'),
      okButtonProps: { danger: true },
      cancelText: t('common.cancel'),
      onOk: async () => {
        await voiceApi.deleteVoice(row.id)
        message.success(t('common.success'))
        load()
      },
    })
  }

  const handleSubmit = async (vals: TtsVoiceForm) => {
    if (editing === null) {
      await voiceApi.createVoice(vals)
      message.success(t('page.voice.created'))
    } else if (editing) {
      await voiceApi.updateVoice(editing.id, vals)
      message.success(t('page.voice.updated'))
    }
    setEditing(undefined)
    load()
  }

  const editingFields: EditField[] = [
    {
      name: 'name',
      label: t('field.name'),
      type: 'text',
      required: true,
    },
    {
      name: 'voice_id',
      label: t('page.voice.voiceId'),
      type: 'text',
      required: editing === null,
      placeholder: t('page.voice.voiceIdPlaceholder'),
      disabled: editing !== null && editing !== undefined,
      help: editing !== null && editing !== undefined ? t('page.voice.voiceIdLocked') : undefined,
    },
    {
      name: 'tts_type',
      label: t('page.voice.ttsType'),
      type: 'select',
      required: true,
      options: TTS_TYPE_OPTIONS,
      disabled: editing !== null && editing !== undefined,
    },
    { name: 'gender', label: t('page.voice.gender'), type: 'text', placeholder: t('page.voice.genderPh') },
    {
      name: 'sex',
      label: t('page.voice.sex'),
      type: 'select',
      options: SEX_OPTIONS.map((o) => ({ value: o.value, label: t(o.labelKey) })),
    },
    { name: 'sort', label: t('field.sort'), type: 'number' },
    { name: 'description', label: t('field.description'), type: 'textarea' },
    { name: 'recommend', label: t('page.voice.recommend'), type: 'switch' },
  ]

  const ttsTypeTag = (v: string) => (
    <Tag color={v === 'edge' ? 'green' : 'blue'}>{v || '-'}</Tag>
  )

  const columns = [
    {
      title: t('field.name'),
      dataIndex: 'name',
      width: 180,
      fixed: 'left' as const,
      render: (v: string, row: TtsVoice) => (
        <span>
          {v}
          {row.recommend && (
            <Tooltip title={t('page.voice.recommend')}>
              <Tag color="gold" style={{ marginLeft: 6, marginRight: 0 }}>
                ★
              </Tag>
            </Tooltip>
          )}
        </span>
      ),
    },
    {
      title: t('field.voiceId'),
      dataIndex: 'voice_id',
      width: 240,
      render: (v: string) => <code style={{ fontSize: 12 }}>{v}</code>,
    },
    { title: t('field.ttsType'), dataIndex: 'tts_type', width: 100, render: ttsTypeTag },
    { title: t('field.gender'), dataIndex: 'gender', width: 90, render: (v: string) => v || '-' },
    { title: t('field.sort'), dataIndex: 'sort', width: 70 },
    { title: t('field.description'), dataIndex: 'description', width: 260, ellipsis: true, render: (v: string) => v || '-' },
    {
      title: t('common.updateTime'),
      dataIndex: 'updated_at',
      width: 165,
      render: (v: string) => (v ? v.slice(0, 19).replace('T', ' ') : '-'),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 220,
      fixed: 'right' as const,
      render: (_: unknown, row: TtsVoice) => {
        const actions: RowAction<TtsVoice>[] = [
          {
            key: 'audition',
            label: t('page.voice.play'),
            perm: PermVoice.CONVERT,
            disabled: auditioning !== null && auditioning !== row.id,
            onClick: (r) => audition(r),
          },
          {
            key: 'edit',
            label: t('common.edit'),
            perm: PermVoice.UPDATE,
            onClick: (r) => setEditing(r),
          },
          {
            key: 'delete',
            label: t('common.delete'),
            danger: true,
            perm: PermVoice.DELETE,
            onClick: (r) => handleDelete(r),
          },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  return (
    <>
      <ManagementTable<TtsVoice>
        onSearch={(kw) => {
          setKeyword(kw)
          setPage(1)
        }}
        onReset={() => {
          setKeyword('')
          setTtsType(undefined)
          setPage(1)
        }}
        searchExtra={
          // 平台筛选 —— 内置音色百炼两百来条,按通道切着看才翻得动
          <Space.Compact>
            {[
              { v: undefined, label: t('common.all') },
              { v: 'bailian', label: 'bailian' },
              { v: 'edge', label: 'edge' },
            ].map(({ v, label }) => (
              <Button
                key={label}
                type={ttsType === v ? 'primary' : 'default'}
                onClick={() => {
                  setTtsType(v)
                  setPage(1)
                }}
              >
                {label}
              </Button>
            ))}
          </Space.Compact>
        }
        inlineToolBar={[
          canCreate && (
            <Button
              key="add"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setEditing(null)}
            >
              {t('page.voice.addVoice')}
            </Button>
          ),
          <Button key="reload" icon={<ReloadOutlined />} onClick={() => load()}>
            {t('common.refresh')}
          </Button>,
        ].filter(Boolean) as ReactNode[]}
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
              // ⚠️ 只改 state,拉数据交给 useEffect([load]) —— 这里再手动
              // load(p) 会跟 effect 撞出一页双发
              setPage(p)
            },
          },
        }}
        scrollX={1320}
      />

      <EditModal<TtsVoiceForm>
        title={editing === null ? t('page.voice.addVoice') : t('page.voice.editVoice')}
        open={editing !== undefined}
        initialValues={
          editing
            ? {
                name: editing.name,
                voice_id: editing.voice_id,
                tts_type: editing.tts_type,
                gender: editing.gender,
                sex: editing.sex,
                sort: editing.sort,
                description: editing.description,
                recommend: editing.recommend,
              }
            : { tts_type: 'bailian', sex: 0, sort: 10, recommend: false }
        }
        fields={editingFields}
        onCancel={() => setEditing(undefined)}
        onSubmit={handleSubmit}
      />
    </>
  )
}

export default VoiceRegistry
