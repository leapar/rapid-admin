/**
 * 执行记录抽屉 —— M69
 * 2026-10-21 新建
 *
 * 挂在任务列表的行操作上(按钮级权限 button.job.view-run),不做成独立路由:
 *   记录必须按 job_key 过滤才有意义,脱离任务单独一个「所有执行记录」入口
 *   在几十个任务的规模下只会让人分不清在看谁。
 *
 * 三态渲染的取舍:status 只有 running / success / failed 三种(后端没有 skipped)。
 * 失败行的 error 可能很长(服务端截断到 2KB),所以失败时直接把整条记录
 * 展开在一个可滚动的红框里,而不是塞进 Table 单元格被截掉。
 */
import { useState, useEffect, useCallback } from 'react'
import { Drawer, Table, Tag, Select, Button, Space, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { jobApi } from '@/api/job'
import { useI18n } from '@/locales'
import { useEventStream, EVENT } from '@/hooks/useEventStream'
import type { EventPayload } from '@/hooks/useEventStream'
import type { JobRun, JobStatus, JobTriggerType, JobRunListQuery } from '@/types/job'

export interface HistoryProps {
  open: boolean
  jobKey: string
  onClose: () => void
}

const STATUS_COLOR: Record<string, string> = {
  running: 'blue',
  success: 'green',
  failed: 'red',
}
/**
 * 运行状态 → i18n 键(2026-10-25 Module B-2)
 *
 * ⚠️ 这里存的是**键**不是文案 —— 模块顶层的常量在 import 时就求值了,
 *   里面直接写 t('…') 的话,语言切换之后它还是切换前那句话(而且切不动)。
 *   要在渲染时查字典,就得把映射做成键,再在组件里用 t() 查。
 */
const STATUS_TEXT_KEY: Record<string, string> = {
  running: 'page.job.status.running',
  success: 'common.resultSuccess',
  failed: 'common.resultFailed',
}
/**
 * 触发方式 → i18n 键(2026-10-25 Module B-4)
 *
 * ⚠️ 同 STATUS_TEXT_KEY:模块顶层的常量在 import 时求值,
 *   里面直接写 t('…') 的话语言切不动。存键,渲染时再查。
 */
const TRIGGER_TEXT_KEY: Record<string, string> = {
  schedule: 'page.job.triggerSchedule',
  manual: 'page.job.triggerManual',
}

/** Unix 秒 → 'YYYY-MM-DD HH:mm:ss'(本机时区,这里只是给人读的相对时间参考) */
function fmtSec(sec: number): string {
  if (!sec) return '-'
  const d = new Date(sec * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

/** 毫秒 → 人类可读。超过 1s 用秒,不然 ms 一眼看不出量级 */
function fmtDuration(ms: number): string {
  if (!ms) return '-'
  if (ms < 1000) return `${ms} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`
  return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`
}

export function History({ open, jobKey, onClose }: HistoryProps) {
  const { t } = useI18n()
  /** 运行状态文案(键 → 查字典) */
  const statusText = (v: string) => {
    const key = STATUS_TEXT_KEY[v]
    return key ? t(key) : v
  }
  /** 触发方式文案(键 → 查字典) */
  const triggerText = (v: string) => {
    const key = TRIGGER_TEXT_KEY[v]
    return key ? t(key) : v
  }
  const { subscribe } = useEventStream()
  const [runs, setRuns] = useState<JobRun[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [status, setStatus] = useState<JobStatus | undefined>()
  const [triggerType, setTriggerType] = useState<JobTriggerType | undefined>()
  const [degradedOnly, setDegradedOnly] = useState(false)

  const load = useCallback(
    async (p: number, ps: number) => {
      setLoading(true)
      try {
        const q: JobRunListQuery = { page: p, size: ps, jobKey }
        if (status) q.status = status
        if (triggerType) q.triggerType = triggerType
        if (degradedOnly) q.degradedOnly = true
        const res = await jobApi.runs(q)
        // 后端 PageData 的字段是 list/total/page/size(不是 page_size),见 types/job.ts
        setRuns(res?.list || [])
        setTotal(res?.total || 0)
      } catch {
        setRuns([])
        setTotal(0)
      } finally {
        setLoading(false)
      }
    },
    [jobKey, status, triggerType, degradedOnly],
  )

  // 换任务 / 开抽屉 / 改筛选 → 回到第一页重新加载。
  // 依赖里只写 [open, load]:load 是 useCallback([jobKey, status, triggerType, degradedOnly]),
  // 它的引用变化**恰好**等价于这四个筛选条件变化,所以不必再逐个列出来。
  //
  // ⚠️ pageSize 刻意不进依赖:分页 onChange 里已经显式 load(p, ps) 了,
  // 让它进依赖会导致改「每页条数」时多跑一遍,并把页码重置回 1
  // (改条数本来应该停在当前页)。
  useEffect(() => {
    if (!open) return
    setPage(1)
    load(1, pageSize)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, load])

  // ---- 事件驱动的实时刷新(M73)----
  //
  // 这是本页从"纯手动"变成"能看到任务跑完"的关键:开着抽屉时任务跑完,
  // 那一行 running 会自己翻成 success / failed,不用手点刷新。
  //
  // ⚠️ **必须按 job_key 过滤**。事件是全站广播的(凡是连着这条 SSE 的人
  // 都会收到),不筛的话,别人家的任务跑完也会把本页表格刷一遍 ——
  // 白请求,而且会让用户以为列表在无意义地跳。
  useEffect(() => {
    if (!open) return
    const reloadIfMine = (p: EventPayload) => {
      if (p.job_key !== jobKey) return
      // 停在**当前页**刷新,而不是回到第一页:用户正在翻第 3 页时
      // 被拽回第 1 页,是这类自动刷新最招人烦的地方
      load(page, pageSize)
    }
    const offFinished = subscribe(EVENT.jobFinished, reloadIfMine)
    const offFailed = subscribe(EVENT.jobFailed, reloadIfMine)
    return () => {
      offFinished()
      offFailed()
    }
  }, [open, jobKey, page, pageSize, load, subscribe])

  // lag = 服务端缓冲区溢出,有事件被丢弃了 —— 这时**不知道漏了谁**,
  // 所以不做 job_key 过滤,直接补拉一次当前视图。
  useEffect(() => {
    if (!open) return
    return subscribe(EVENT.lag, () => load(page, pageSize))
  }, [open, page, pageSize, load, subscribe])

  const columns = [
    {
      title: t('field.startTime'),
      dataIndex: 'started_at',
      width: 165,
      render: (v: number) => fmtSec(v),
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      width: 90,
      render: (v: string) => (
        <Tag color={STATUS_COLOR[v] || 'default'}>{statusText(v)}</Tag>
      ),
    },
    {
      title: t('field.trigger'),
      dataIndex: 'trigger_type',
      width: 80,
      render: (v: string) => triggerText(v),
    },
    {
      title: t('field.duration'),
      dataIndex: 'duration_ms',
      width: 100,
      render: (v: number) => fmtDuration(v),
    },
    {
      title: t('field.operator'),
      dataIndex: 'operator',
      width: 110,
      // 只有手动触发才有操作人;定时触发这一列留空而不是显示 '-',
      // 是为了让"这行是人点的还是时钟点的"一眼可分
      render: (v: string, row: JobRun) => v || (row.trigger_type === 'manual' ? '-' : '—'),
    },
    {
      title: t('common.remark'),
      dataIndex: 'degraded',
      width: 90,
      render: (v: boolean) =>
        v ? <Tag color="orange">{t('page.job.degraded')}</Tag> : null,
    },
  ]

  // 失败详情单独一块,放表格下方而不是塞进某列:
  // 一行只展示最近一次失败的完整错误,比让用户在 20 行里找红字靠谱
  const lastFailed = runs.find((r) => r.status === 'failed')

  return (
    <Drawer
      title={`${t('page.job.runHistory')} · ${jobKey}`}
      open={open}
      onClose={onClose}
      width={920}
      destroyOnHidden
    >
      <Space style={{ marginBottom: 12 }} wrap>
        <Select
          placeholder={t('common.placeholder.status')}
          allowClear
          style={{ width: 120 }}
          value={status}
          onChange={(v) => setStatus(v as JobStatus | undefined)}
          options={[
            { value: 'success', label: t('common.resultSuccess') },
            { value: 'failed', label: t('common.resultFailed') },
            { value: 'running', label: t('page.job.status.running') },
          ]}
        />
        <Select
          placeholder={t('common.placeholder.triggerType')}
          allowClear
          style={{ width: 120 }}
          value={triggerType}
          onChange={(v) => setTriggerType(v as JobTriggerType | undefined)}
          options={[
            { value: 'schedule', label: t('page.job.triggerSchedule') },
            { value: 'manual', label: t('page.job.triggerManual') },
          ]}
        />
        <Button
          type={degradedOnly ? 'primary' : 'default'}
          onClick={() => setDegradedOnly((b) => !b)}
        >
          {t('page.job.degradedOnly')}
        </Button>
        <Button icon={<ReloadOutlined />} onClick={() => load(page, pageSize)}>
          {t('common.refresh')}
        </Button>
      </Space>

      <Table<JobRun>
        rowKey="id"
        size="small"
        columns={columns}
        loading={loading}
        dataSource={runs}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          showTotal: (n) => t('common.total', undefined, { n }),
          onChange: (p, ps) => {
            setPage(p)
            setPageSize(ps)
            load(p, ps)
          },
        }}
      />

      {lastFailed && (
        <div style={{ marginTop: 16 }}>
          <Typography.Text type="danger" style={{ fontSize: 12 }}>
            {t('page.job.lastFailed', undefined, { time: fmtSec(lastFailed.started_at) })}
          </Typography.Text>
          <pre
            style={{
              marginTop: 6,
              maxHeight: 220,
              overflow: 'auto',
              background: 'var(--app-error-bg)',
              border: '0.5px solid var(--app-error-border)',
              borderRadius: 4,
              padding: 12,
              fontSize: 12,
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-all',
              margin: '6px 0 0',
            }}
          >
            {lastFailed.error || t('page.job.noErrorInfo')}
          </pre>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            trace_id:{lastFailed.trace_id || '-'} · instance:{lastFailed.instance_id || '-'}
          </Typography.Text>
        </div>
      )}
    </Drawer>
  )
}

export default History