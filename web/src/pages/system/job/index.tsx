/**
 * 定时任务管理 —— M69
 * 2026-10-21 新建
 *
 * 设计前提(**没有**新增/删除按钮,这不是遗漏):
 *   任务本体只在代码里 cronx.Register —— 发版才能增删,后台改不了。
 *   sys_job 表存的只是「运行态覆盖」:启停、改频率、改名、备注、排序。
 *   所以本页只有一条写通道:PATCH /jobs/:jobKey。调 create/delete 会 404。
 *
 * 三个刻意不做的事:
 *   1. 不显示「新建任务」——后端没这个路由,而且任务里含 Handler 函数指针,
 *      天然不是一条数据库记录能表达的。
 *   2. 不用「删除」停用——停用是 enabled=false 的语义切换,可逆;
 *      删了 sys_job 那行就丢了当初设的 cron 覆盖值。
 *   3. 手动触发**同步**等待(后端返回执行记录),按钮上写明「可能耗时」,
 *      因为内置任务默认超时 30 分钟,不是点一下就秒回的。
 */
import { useState, useEffect, useCallback } from 'react'
import type { FC } from 'react'
import { Tag, Button, App, Alert, Tooltip, Select, Space } from 'antd'
import { ReloadOutlined, InfoCircleOutlined, WarningOutlined } from '@ant-design/icons'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal } from '@/components/common/EditModal'
import type { EditField } from '@/components/common/EditModal'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { RowActions } from '@/components/common/RowActions'
import type { RowAction } from '@/components/common/RowActions'
import { useI18n } from '@/locales'
import { jobApi } from '@/api/job'
import { useEventStream } from '@/hooks/useEventStream'
import { PermButton } from '@/constants/permissions'
import type { JobItem, JobRuntime } from '@/types/job'
import { CronInput } from './CronInput'
import { describeCron } from './cronDescribe'
import { History } from './History'

/**
 * 编辑表单的形状。
 *
 * 刻意**不**用 JobItem 当表单模型:JobItem 的启停字段叫 enabled_effective /
 * enabled_code / enabled_db(三个,因为"代码里就是关的"和"后台关的"处置方式不同),
 * 而表单里只有一个开关。用 JobItem 的话,提交时就得写
 * `enabled: vals.enabled_effective` —— 名字对不上,类型系统也只能在运行时抓。
 */
interface JobEditForm {
  name?: string
  description?: string
  cron_expr?: string
  enabled?: boolean
  sort?: number
}

const JobList: FC = () => {
  const { t } = useI18n()
  const { message, modal } = App.useApp()
  // M73:只用 status,不订阅事件 —— 本页列表数据(启停/频率/下次触发时间)
  // **不会**因为某个任务跑完而改变,订阅它只会白刷一次。
  // 真正需要实时刷的是执行记录抽屉(见 History.tsx)。
  const { status: streamStatus } = useEventStream()

  const [data, setData] = useState<JobItem[]>([])
  const [runtime, setRuntime] = useState<JobRuntime | null>(null)
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [enabledOnly, setEnabledOnly] = useState(false)

  const [editing, setEditing] = useState<{ open: boolean; data?: JobItem | null }>({ open: false })
  const [triggering, setTriggering] = useState<JobItem | null>(null)
  const [resetting, setResetting] = useState<JobItem | null>(null)
  const [historyKey, setHistoryKey] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [jobs, rt] = await Promise.all([jobApi.list({}), jobApi.runtime()])
      setData(jobs || [])
      setRuntime(rt || null)
    } catch {
      setData([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  /** 前端只做关键字过滤(列表总共几十条,不分页,不需要走后端 filter) */
  const filtered = data.filter((j) => {
    if (enabledOnly && !j.enabled_effective) return false
    if (!keyword) return true
    const kw = keyword.toLowerCase()
    return (
      j.job_key.toLowerCase().includes(kw) ||
      j.name.toLowerCase().includes(kw) ||
      (j.description || '').toLowerCase().includes(kw)
    )
  })

  /** 下次触发时间:空串 = 未调度(停用 / 没进 cron)。别 fallback 成 now */
  const nextRunCell = (v: string, row: JobItem) => {
    if (!v) return <span style={{ color: 'var(--app-text-tertiary)' }}>{row.active ? t('page.job.notScheduled') : '—'}</span>
    return v.slice(0, 19).replace('T', ' ')
  }

  const columns = [
    {
      title: t('field.jobKey'),
      dataIndex: 'job_key',
      width: 250,
      fixed: 'left' as const,
      render: (v: string) => <code style={{ fontSize: 12 }}>{v}</code>,
    },
    {
      title: t('field.name'),
      dataIndex: 'name',
      width: 190,
      render: (v: string, row: JobItem) => (
        <Tooltip title={row.description || undefined}>
          <span>{v}</span>
        </Tooltip>
      ),
    },
    {
      title: t('field.cronExpr'),
      dataIndex: 'cron_expr',
      width: 250,
      render: (v: string, row: JobItem) => {
        // M120:把表达式翻译成人话。描述不出来时返回 null —— 那就只显示裸表达式,
        // **不要**显示一句"大概每天"之类的猜测(描述错了比不描述更糟)。
        const desc = describeCron(v, t)
        return (
          <div>
            <div>
              <code style={{ fontSize: 12 }}>{v}</code>
              {row.cron_overridden && (
                <Tooltip title={`${t('page.job.defaultExprLabel')}${row.default_cron_expr}`}>
                  {/* 已覆盖是「运营改过」的信号。灰底避免和生效值本身混淆 */}
                  <Tag color="blue" style={{ marginLeft: 6 }}>{t('page.job.overridden')}</Tag>
                </Tooltip>
              )}
            </div>
            {desc && (
              <div style={{ fontSize: 12, color: 'var(--app-text-tertiary)', marginTop: 2 }}>{desc.text}</div>
            )}
          </div>
        )
      },
    },
    {
      title: t('common.status'),
      dataIndex: 'enabled_effective',
      width: 130,
      render: (v: boolean, row: JobItem) => {
        if (!row.active) {
          return (
            <Tooltip title={t('page.job.residualTip')}>
              <Tag color="red">{t('page.job.residual')}</Tag>
            </Tooltip>
          )
        }
        // enabled_effective = enabled_code && enabled_db。
        // 两层都展示是因为「代码里就是关的」和「后台关的」处置方式完全不同
        // (前者要发版,后者后台点一下就行)
        return (
          <Space size={4}>
            <Tag color={v ? 'green' : 'default'}>
              {v ? t('common.enabled') : t('common.disabled')}
            </Tag>
            {!v && !row.enabled_code && <Tag>{t('page.job.codeSideOff')}</Tag>}
          </Space>
        )
      },
    },
    {
      title: t('field.nextRun'),
      dataIndex: 'next_run_at',
      width: 165,
      render: (v: string, row: JobItem) => nextRunCell(v, row),
    },
    {
      title: t('field.timeout'),
      dataIndex: 'timeout_ms',
      width: 90,
      render: (v: number) => (v ? t('page.job.minutes', undefined, { n: Math.round(v / 60000) }) : '-'),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 300,
      fixed: 'right' as const,
      render: (_: unknown, row: JobItem) => {
        const actions: RowAction<JobItem>[] = [
          {
            key: 'edit',
            label: t('common.edit'),
            perm: PermButton.JOB_EDIT,
            disabled: !row.active,
            disabledTip: t('page.job.removedTip'),
            onClick: (r) => setEditing({ open: true, data: r }),
          },
          {
            key: 'toggle',
            label: row.enabled_effective ? t('common.disable') : t('common.enable'),
            perm: PermButton.JOB_TOGGLE,
            disabled: !row.active,
            disabledTip: t('page.job.removedTipShort'),
            onClick: async (r) => {
              try {
                await jobApi.update(r.job_key, { enabled: !r.enabled_effective })
                message.success(r.enabled_effective ? t('common.disabled') : t('common.enabled'))
              } catch {
                // 错误提示由 axios 拦截器统一弹出(见 api/client.ts:97-108),
                // 这里吞掉只是为了让行操作不产生 unhandled rejection
              }
              load()
            },
          },
          {
            key: 'trigger',
            label: t('page.job.manualTrigger'),
            perm: PermButton.JOB_TRIGGER_BTN,
            disabled: !row.active || !row.enabled_effective,
            disabledTip: !row.active
              ? t('page.job.removedTipShort')
              : t('page.job.disabledTip'),
            onClick: (r) => setTriggering(r),
          },
          {
            key: 'reset',
            label: t('common.resetDefault'),
            perm: PermButton.JOB_RESET_DEFAULT,
            // 没有覆盖行时按钮禁用 —— 恢复默认要有东西可恢复
            disabled: !row.overridden,
            disabledTip: t('page.job.noOverrideTip'),
            onClick: (r) => setResetting(r),
          },
          {
            key: 'runs',
            label: t('page.job.runHistory'),
            perm: PermButton.JOB_VIEW_RUN,
            onClick: (r) => setHistoryKey(r.job_key),
          },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  const editFields: EditField[] = [
    { name: 'name', label: t('field.name') },
    { name: 'description', label: t('common.remark'), type: 'textarea' },
    {
      name: 'cron_expr',
      label: t('field.cronExpr'),
      type: 'custom',
      render: (props) => (
        <CronInput
          value={props.value}
          onChange={props.onChange}
          defaultExpr={editing.data?.default_cron_expr}
        />
      ),
    },
    { name: 'enabled', label: t('common.enable'), type: 'switch' },
    { name: 'sort', label: t('field.sort'), type: 'number' },
  ]

  /**
   * 手动触发的结果判定 —— 这是本页最容易写错的一处。
   *
   * 后端契约(service/job.go TriggerNow 注释):
   *   任务**自身**失败 → HTTP 200 + 一条 status="failed" 的记录(**不是** error)
   *   「没能跑起来」     → 真 error(404 / 400 / 409 / 500)
   *
   * 所以只判 HTTP 成功就弹「触发成功」的话,一个每次都报错的定时任务
   * 会天天弹绿条 —— 这正是这条契约要防的事。
   */
  const handleTriggerOk = async () => {
    if (!triggering) return
    try {
      const run = await jobApi.trigger(triggering.job_key)
      if (run.status === 'success') {
        message.success(t('page.job.triggerSuccess', undefined, { n: run.duration_ms }))
      } else {
        modal.error({
          title: t('page.job.triggerFailedTitle', undefined, { key: triggering.job_key }),
          // error 可能为空(比如超时被 ctx 取消时),给个兜底文案而不是显示空白
          content: run.error || t('page.job.nonSuccessStatus', undefined, { status: run.status }),
          width: 600,
        })
      }
    } catch {
      // 「没能跑起来」(404 / 400 / 409 / 500)是真 error,由拦截器弹提示,
      // 这里只负责收尾
    } finally {
      // 成功失败都要收尾刷新:409 意味着「上一次还在跑」,
      // 此时列表里的下次触发时间已经变了,不刷新就是错的
      setTriggering(null)
      load()
    }
  }

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {runtime && !runtime.enabled && (
        <Alert
          type="warning"
          showIcon
          icon={<WarningOutlined />}
          message={t('page.job.schedulerDisabled')}
          description={t('page.job.schedulerDisabledDesc')}
          style={{ marginBottom: 16 }}
        />
      )}
      {runtime?.degraded && (
        <Alert
          type="warning"
          showIcon
          icon={<WarningOutlined />}
          message={t('page.job.lockDegraded')}
          description={
            <span>
              {t('page.job.lockDegradedPrefix')}
              <code>{runtime.lock_mode}</code>
              {t('page.job.lockDegradedMiddle')}
              <strong>{t('page.job.lockDegradedHighlight')}</strong>
              {t('page.job.lockDegradedSuffix')}
            </span>
          }
          style={{ marginBottom: 16 }}
        />
      )}
      {/*
       * M73 事件通道降级横幅。
       *
       * ⚠️ 这一条是「不静默」原则的直接落地:Redis 挂掉时服务端会主动下发
       * degraded 并断开连接(而不是假装一切正常)。本页如果对此一无所知,
       * 用户看到的就是"任务跑完了但列表不动" —— 表现和数据坏了完全一样,
       * 排障时前端连一句线索都给不出。
       *
       * 只在本页出现,不做成全局横幅:本页恰好是"本来靠事件实时刷新、
       * 现在退回手动"的地方,别处不刷新并不会让人误判。
       */}
      {streamStatus === 'degraded' && (
        <Alert
          type="warning"
          showIcon
          icon={<WarningOutlined />}
          message={t('page.job.streamDegraded')}
          description={t('page.job.streamDegradedDesc')}
          style={{ marginBottom: 16 }}
        />
      )}

      <ManagementTable<JobItem>
        pageDescription={t('page.job.description')}
        searchPlaceholder={t('common.placeholder.searchJob')}
        onSearch={(kw) => setKeyword(kw)}
        onReset={() => {
          setKeyword('')
          setEnabledOnly(false)
        }}
        searchExtra={[
          <Select
            key="enabledOnly"
            placeholder={t('common.placeholder.allStatus')}
            style={{ width: 140 }}
            value={enabledOnly ? 'on' : 'all'}
            onChange={(v) => setEnabledOnly(v === 'on')}
            options={[
              { value: 'all', label: t('common.all') },
              { value: 'on', label: t('page.job.onlyEnabled') },
            ]}
          />,
        ]}
        inlineToolBar={[
          <Button key="reload" icon={<ReloadOutlined />} onClick={load}>
            {t('common.refresh')}
          </Button>,
          runtime && (
            <Tooltip
              key="rt"
              title={t('page.job.runtimeInfo', undefined, {
                id: runtime.instance_id,
                tz: runtime.timezone,
                n: runtime.registered,
              })}
            >
              <Button icon={<InfoCircleOutlined />}>
                {runtime.lock_mode} · {runtime.registered}
              </Button>
            </Tooltip>
          ),
        ]}
        columns={columns}
        tableProps={{
          loading,
          dataSource: filtered,
          // JobItem 没有 id 字段,必须显式给 rowKey,
          // 否则 ManagementTable 默认取 r.id ?? r.ID → 全是 undefined → 选中/展开全乱
          rowKey: 'job_key',
          pagination: { pageSize: 20, showTotal: (n) => t('common.total', undefined, { n }) },
        }}
        scrollX={1400}
      />

      <EditModal<JobEditForm>
        title={
          editing.data
            ? `${t('page.job.editTitle')} · ${editing.data.job_key}`
            : t('page.job.editTitle')
        }
        open={editing.open}
        initialValues={
          editing.data
            ? {
                name: editing.data.name,
                description: editing.data.description,
                cron_expr: editing.data.cron_expr,
                enabled: editing.data.enabled_effective,
                sort: editing.data.sort,
              }
            : undefined
        }
        fields={editFields}
        onCancel={() => setEditing({ open: false })}
        onSubmit={async (vals) => {
          if (!editing.data) return
          // body 走 snake_case(与 query 相反),见 api/job.ts
          await jobApi.update(editing.data.job_key, {
            name: vals.name,
            description: vals.description,
            cron_expr: vals.cron_expr,
            enabled: vals.enabled,
            sort: vals.sort,
          })
          message.success(t('common.success'))
          setEditing({ open: false })
          load()
        }}
      />

      <ConfirmDialog
        open={!!triggering}
        title={t('page.job.manualTriggerTitle', undefined, { key: triggering?.job_key ?? '' })}
        description={
          <span>
            {t('page.job.triggerConfirmPrefix')}
            <b>{t('page.job.triggerConfirmHighlight')}</b>
            {t('page.job.triggerConfirmSuffix', undefined, {
              n: triggering ? Math.round(triggering.timeout_ms / 60000) : 0,
            })}
          </span>
        }
        okText={t('field.trigger')}
        onCancel={() => setTriggering(null)}
        onOk={handleTriggerOk}
      />

      <ConfirmDialog
        open={!!resetting}
        danger
        title={t('page.job.resetTitle')}
        description={
          <span>
            {t('page.job.resetConfirmPrefix')}
            <b>{resetting?.job_key}</b>
            {t('page.job.resetConfirmMiddle')}
            <b>{t('page.job.resetConfirmHighlight')}</b>
            {t('page.job.resetConfirmSuffix')}
          </span>
        }
        okText={t('common.resetDefault')}
        onCancel={() => setResetting(null)}
        onOk={async () => {
          if (!resetting) return
          await jobApi.update(resetting.job_key, { reset_to_default: true })
          message.success(t('page.job.restored'))
          setResetting(null)
          load()
        }}
      />

      <History
        open={!!historyKey}
        jobKey={historyKey || ''}
        onClose={() => setHistoryKey(null)}
      />
    </div>
  )
}

export default JobList