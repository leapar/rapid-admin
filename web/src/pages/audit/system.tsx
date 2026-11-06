/**
 * 系统日志 —— 列表 + 详情(2026-11-03)
 *
 * # 这一页是什么
 *
 *   后台运行期的"值得在审计中心看一眼"的四类事件落库(sys_system_log):
 *
 *     slow_sql    慢 SQL(>500ms,slowquery 钩子)   path 列 = 表名
 *     slow_http   慢接口(>2s,AccessLog 中间件钩子)
 *     http_error  接口 5xx(同上)
 *     panic       panic(Recover 中间件钩子)         detail 列 = panic 栈
 *
 * # 这一页踩的"静默失效"点(与登录日志页同一族,先看清楚再改)
 *
 *   1. **level / source 传空串 = 不筛** —— 后端 `SystemLogQuery` 对空串
 *      直接跳过过滤;Select allowClear 清掉后必须把字段**从 query 里删掉**
 *      (传 `level: ''` 恰好等价于不筛,但类型上要显式 undefined,别传空串)。
 *
 *   2. **keyword 命中 path / message 两列** —— 不搜 detail(那是 panic 栈 /
 *      完整 SQL,LIKE 全表扫太重)。搜索框提示词别写"搜索详情"。
 *
 *   3. **source 不是字典** —— 四个取值是代码 enum(中间件硬编码),
 *      但文案走 i18n(page.auditSystem.source*),颜色前端映射;
 *      **别**为它开字典组 —— 取值变了必须同 commit 改后端钩子,字典反而掩盖耦合。
 *      level 才是字典(sys_log_level),因为"要不要新增一个级别"是运营决策。
 */
import { useState } from 'react'
import type { FC } from 'react'
import { Select, Tag, Tooltip } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ManagementTable } from '@/components/common/ManagementTable'
import { ViewDrawer } from '@/components/common/ViewDrawer'
import type { ViewField } from '@/components/common/ViewDrawer'
import { RowActions } from '@/components/common/RowActions'
import type { RowAction } from '@/components/common/RowActions'
import { useTable } from '@/hooks/useTable'
import { useDict } from '@/hooks/useDict'
import { useI18n } from '@/locales'
import { auditApi } from '@/api/audit'
import type { SystemLog, SystemLogQuery } from '@/types/audit'
import AuditRangeFilter from './RangeFilter'
import { fmtAuditTime } from './format'

/** source → i18n key(文案)+ Tag 颜色;取值必须与后端中间件钩子一字不差 */
const SOURCE_META: Record<string, { labelKey: string; color: string }> = {
  slow_sql: { labelKey: 'page.auditSystem.sourceSlowSql', color: 'orange' },
  slow_http: { labelKey: 'page.auditSystem.sourceSlowHttp', color: 'gold' },
  http_error: { labelKey: 'page.auditSystem.sourceHttpError', color: 'volcano' },
  panic: { labelKey: 'page.auditSystem.sourcePanic', color: 'red' },
  // 业务失败(不走 HTTP 状态码的通道):asr_error 实时识别 WS 会话失败 /
  // tts_error 已发响应头的流式合成断流 —— RecordBizSystemLog 钩子
  asr_error: { labelKey: 'page.auditSystem.sourceAsrError', color: 'volcano' },
  tts_error: { labelKey: 'page.auditSystem.sourceTtsError', color: 'volcano' },
}

const SystemLogList: FC = () => {
  const { t } = useI18n()
  /** level 走字典 sys_log_level(info / warn / error) */
  const { getLabel: getLevelLabel, getColor: getLevelColor } = useDict('sys_log_level')

  const { tableProps, search, reset, query, setQuery } = useTable<SystemLogQuery, SystemLog>({
    fetch: (q) => auditApi.systemLogs(q),
  })

  const [viewing, setViewing] = useState<SystemLog | null>(null)

  /** 级别标签 —— 文案与颜色都走字典 */
  const levelTag = (v: string) => {
    const label = getLevelLabel(v)
    if (label === v) return <Tag color="default">{v}</Tag>
    return <Tag color={getLevelColor(v)}>{label}</Tag>
  }

  /** 来源标签 —— 代码 enum,文案走 i18n(见文件头注释 #3) */
  const sourceTag = (v: string) => {
    const meta = SOURCE_META[v]
    if (!meta) return <Tag color="default">{v}</Tag>
    return <Tag color={meta.color}>{t(meta.labelKey)}</Tag>
  }

  const columns: ColumnsType<SystemLog> = [
    {
      title: t('field.level'),
      dataIndex: 'level',
      width: 90,
      fixed: 'left',
      render: (v: string) => levelTag(v),
    },
    {
      title: t('field.source'),
      dataIndex: 'source',
      width: 110,
      render: (v: string) => sourceTag(v),
    },
    {
      // 慢 SQL 行的 path 是表名、method 为空 —— 渲染成 "GET /api/xxx" 或 "表名"
      title: t('field.request'),
      dataIndex: 'path',
      width: 260,
      ellipsis: true,
      render: (v: string, row: SystemLog) => {
        const text = row.method ? `${row.method} ${v}` : v
        return text ? (
          <Tooltip title={text}>
            <code style={{ fontSize: 11 }}>{text}</code>
          </Tooltip>
        ) : (
          '-'
        )
      },
    },
    {
      title: t('field.message'),
      dataIndex: 'message',
      width: 260,
      ellipsis: true,
    },
    {
      title: t('field.durationMs'),
      dataIndex: 'duration_ms',
      width: 110,
      render: (v: number) => (v > 0 ? `${v}ms` : '-'),
    },
    {
      title: t('field.traceId'),
      dataIndex: 'trace_id',
      width: 170,
      ellipsis: true,
      render: (v: string) => v || '-',
    },
    {
      title: t('common.createTime'),
      dataIndex: 'created_at',
      width: 165,
      render: (v: string) => fmtAuditTime(v),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 100,
      fixed: 'right',
      render: (_: unknown, row: SystemLog) => {
        const actions: RowAction<SystemLog>[] = [
          // view 详情不需要 button 粒度 perm(只读操作,走路由级 audit:system:list 守卫)
          { key: 'view', label: t('common.detail'), onClick: setViewing },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  const viewFields: ViewField<SystemLog>[] = [
    { name: 'level', label: t('field.level'), render: (v: string) => levelTag(v) },
    { name: 'source', label: t('field.source'), render: (v: string) => sourceTag(v) },
    {
      name: 'path',
      label: t('field.request'),
      render: (v: string, row: SystemLog) => {
        const text = row.method ? `${row.method} ${v}` : v
        return text ? <code style={{ fontSize: 11 }}>{text}</code> : '-'
      },
    },
    { name: 'message', label: t('field.message'), render: (v: string) => v || '-' },
    {
      name: 'duration_ms',
      label: t('field.durationMs'),
      render: (v: number) => (v > 0 ? `${v}ms` : '-'),
    },
    { name: 'trace_id', label: t('field.traceId'), render: (v: string) => v || '-' },
    { name: 'user_id', label: t('field.userId'), render: (v: string) => (v && v !== '0' ? v : '-') },
    {
      name: 'detail',
      label: t('common.detail'),
      // 完整 SQL / panic 栈用 pre 展示 —— 等宽 + 保留换行
      render: (v: string) =>
        v ? (
          <pre
            style={{
              margin: 0,
              maxHeight: 360,
              overflow: 'auto',
              fontSize: 11,
              lineHeight: 1.6,
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-all',
            }}
          >
            {v}
          </pre>
        ) : (
          '-'
        ),
    },
    { name: 'created_at', label: t('common.createTime'), render: (v: string) => fmtAuditTime(v) },
  ]

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<SystemLog>
        pageDescription={t('page.auditSystem.description')}
        searchPlaceholder={t('common.placeholder.searchAuditSystem')}
        onSearch={(kw) => search({ keyword: kw })}
        onReset={reset}
        searchExtra={[
          <Select
            key="level"
            placeholder={t('common.placeholder.filterLevel')}
            allowClear
            style={{ width: 120 }}
            value={query.level}
            onChange={(v) => setQuery((prev) => ({ ...prev, level: v, page: 1 }) as SystemLogQuery)}
            options={[
              { value: 'info', label: getLevelLabel('info') },
              { value: 'warn', label: getLevelLabel('warn') },
              { value: 'error', label: getLevelLabel('error') },
            ]}
          />,
          <Select
            key="source"
            placeholder={t('common.placeholder.filterSource')}
            allowClear
            style={{ width: 140 }}
            value={query.source}
            onChange={(v) =>
              setQuery((prev) => ({ ...prev, source: v, page: 1 }) as SystemLogQuery)
            }
            options={Object.entries(SOURCE_META).map(([value, meta]) => ({
              value,
              label: t(meta.labelKey),
            }))}
          />,
          <AuditRangeFilter
            key="range"
            startAt={query.startAt}
            endAt={query.endAt}
            onChange={(r) =>
              setQuery(
                (prev) =>
                  ({
                    ...prev,
                    startAt: r.startAt,
                    endAt: r.endAt,
                    page: 1,
                  }) as SystemLogQuery,
              )
            }
          />,
        ]}
        columns={columns}
        tableProps={tableProps}
        scrollX={1300}
      />

      <ViewDrawer<SystemLog>
        title={t('resource.audit_system_read')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
        width={720}
      />
    </div>
  )
}

export default SystemLogList
