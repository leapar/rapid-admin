/**
 * 操作日志 —— 列表
 *
 * Phase 5 M14 业务页批量 - 3/3(字典/部门/操作日志)
 *
 * # ⚠️ 2026-10-30 修的三处"看起来有、其实是空的"
 *
 *   1. **列名全错** —— 页面读的是 `requestMethod` / `requestUrl` /
 *      `requestParams` / `responseResult` / `errorMsg` / `createdAt`,
 *      而后端 wire 是 **snake_case**:`request_method` / `request_url` /
 *      `request_params` / `response_summary` / `error_msg` / `created_at`。
 *      TypeScript 不报错(类型是自己写的),于是"请求方法 / 请求 URL /
 *      响应结果 / 错误信息"四列**永远是空的**,而且没有任何东西会红。
 *      ⇒ 现在类型统一到 `@/types/audit`(单一真相源),列名与它对齐。
 *
 *   2. **"模块"下拉框是摆设** —— 它的选项是写死的 `user` / `role` / `config` …,
 *      而审计中间件写进 `module` 的是**真实请求路径**
 *      (`Module: string(c.Path())`,如 `/rapid-admin/v1/system/users`)。
 *      写死的枚举**永远匹配不上任何一行** ⇒ 这个下拉框点了永远没反应。
 *      ⇒ 现在**删掉这个下拉框**:模块/动作/用户名/描述的检索统一由
 *      **关键字搜索框**承担(后端 `Keyword` 对 module/action/description
 *      做 LIKE,能真的命中)。留一个必然失效的控件比没有更糟。
 *
 *   3. **成功/失败筛选是摆设** —— 前端发的是 `status`,后端 handler
 *      压根没往 query struct 里塞 ⇒ HTTP 200、结果不变。
 *      ⇒ 2026-10-30 后端已接上(`queryOptionalInt`,0 = 失败用**指针**区分
 *      "不筛"),这个下拉框现在是真的了。
 *
 * # module / action 到底是什么
 *
 *   审计中间件写的是:`module` = 真实请求路径、`action` = HTTP 方法。
 *   另有几处 service 层直写的是逻辑名(如 `auth` / `user`)。
 *   ⇒ **两列都不是稳定的枚举**,页面按"原样显示"处理,不做字典映射。
 *
 * # 抽屉里**故意没放**的两个字段(以及为什么)
 *
 *   - `request_method` —— 与 `action` **完全同值**(都是 HTTP 方法),
 *     而且中间件**故意不写**它(写一遍就是冗余副本,见 middleware/audit.go)。
 *   - `cost_ms`        —— 与 `duration` **完全同值**
 *     (middleware/audit.go 里两个字段写的是同一个 `time.Since(start)`)。
 *   ⇒ 两行都是邻居行的副本,展示它们只会让人以为是三个不同的量。
 *
 *   ⚠️ `request_url` **现在展示了**(2026-10-30)—— 它此前被算作 module 的
 *     副本,**那个判断是错的**:module 是 `c.Path()`(**不含 query**),
 *     request_url 是完整 URI(路径 **+ query**)。差的那段 query 才是
 *     "这次到底动了哪个对象"(`?userId=123` / `?force=1`)。
 *
 * # 抽屉里**现在有值了**的四个字段(2026-10-30 M126 起)
 *
 *   `request_body` / `request_params` / `response_summary` / `location`
 *   —— 此前中间件**不写**这四列,service 层直写的那几条也不写 ⇒ **每一行都是空的**。
 *   ⇒ M126 补上了**采集端**(server/internal/api/middleware/audit_capture.go):
 *     · `request_body`     脱敏后的请求体(仅 JSON;非 JSON / 超 8KB / 解析失败 ⇒ 空)
 *     · `request_params`   脱敏后的 query
 *     · `response_summary` 响应的**骨架**(`{"code":0,"data_keys":[...]}`)—— **不含值**
 *     · `location`         路径参数(如 `id=9527`);没有路径参数的路由(如 POST 新建)为空
 *
 *   ⚠️ **这里展示的内容里没有明文凭证,页面不用再遮一遍**:
 *     · 请求体 / query 都过了 `pkg/desensitize`(password / token / secret /
 *       验证码 / 证件 / 银行卡 / 手机 / 邮箱 … 全部遮成 `***`);
 *     · 响应**只落骨架不落值** —— 响应体里全是解密后的手机号、身份证号,
 *       把它整份复制进"保留 5 年、可批量导出"的合规表 = 把 PII 复制到
 *       更容易被读到的地方。
 *   ⇒ 所以 `request_body` 这一行是 2026-10-30 才补上的:后端写它了,页面也得看得见。
 *
 *   (`error_msg` **也不再空着** —— 2026-10-30 起中间件会把失败响应的
 *    `message` 摘出来写进去。)
 */
import { useState } from 'react'
import type { FC } from 'react'
import { Tag, Select, Tooltip } from 'antd'
import { ManagementTable } from '@/components/common/ManagementTable'
import { ViewDrawer } from '@/components/common/ViewDrawer'
import type { ViewField } from '@/components/common/ViewDrawer'
import { RowActions } from '@/components/common/RowActions'
import type { RowAction } from '@/components/common/RowActions'
import { useTable } from '@/hooks/useTable'
import { useDict } from '@/hooks/useDict'
import { useI18n } from '@/locales'
import { auditApi } from '@/api/audit'
import type { OperationLog, OperationLogQuery } from '@/types/audit'
import AuditRangeFilter from './RangeFilter'
import { fmtAuditTime } from './format'

/** HTTP 方法配色(仅展示用,不是状态) */
const METHOD_COLOR: Record<string, string> = {
  GET: 'blue',
  POST: 'green',
  PUT: 'orange',
  DELETE: 'red',
  PATCH: 'purple',
}

const OperationLogList: FC = () => {
  const { t } = useI18n()
  // 字典约束(2026-10-18):audit_op_status 来自后端 sys_dict
  const { getLabel: getAuditStatusLabel, getColor: getAuditStatusColor } =
    useDict('audit_op_status')

  const { tableProps, search, reset, query, setQuery } = useTable<OperationLogQuery, OperationLog>({
    fetch: (q) => auditApi.operationLogs(q),
  })

  const [viewing, setViewing] = useState<OperationLog | null>(null)

  /** 状态标签(1 成功 / 0 失败)—— 文案与颜色都走字典 */
  const statusTag = (v: number) => {
    const label = getAuditStatusLabel(v)
    // 字典里查不到这一项 → 显示原始值(而不是空白)
    if (label === String(v)) return <Tag color="default">{v ?? '-'}</Tag>
    return <Tag color={getAuditStatusColor(v)}>{label}</Tag>
  }

  const columns = [
    {
      title: t('field.user'),
      dataIndex: 'username',
      width: 120,
      fixed: 'left' as const,
    },
    {
      // ⚠️ module 记的是**请求路径**,不是业务枚举 —— 用 Tooltip 兜住长路径
      title: t('field.module'),
      dataIndex: 'module',
      width: 200,
      ellipsis: true,
      render: (v: string) => (
        <Tooltip title={v}>
          <code style={{ fontSize: 11 }}>{v || '-'}</code>
        </Tooltip>
      ),
    },
    {
      title: t('field.method'),
      dataIndex: 'action',
      width: 100,
      render: (v: string) => (
        <Tag color={METHOD_COLOR[v] || 'default'} style={{ fontSize: 10 }}>
          {v || '-'}
        </Tag>
      ),
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      width: 90,
      render: (v: number) => statusTag(v),
    },
    { title: t('field.ip'), dataIndex: 'ip', width: 130 },
    { title: t('field.durationMs'), dataIndex: 'duration', width: 100 },
    {
      title: t('field.time'),
      dataIndex: 'created_at',
      width: 165,
      render: (v: string) => fmtAuditTime(v),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 100,
      fixed: 'right' as const,
      render: (_: unknown, row: OperationLog) => {
        const actions: RowAction<OperationLog>[] = [
          // view 详情不需要 button 粒度 perm(只读操作,走路由级 audit:operation:list 守卫)
          { key: 'view', label: t('common.detail'), onClick: setViewing },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  const viewFields: ViewField<OperationLog>[] = [
    { name: 'username', label: t('field.user') },
    { name: 'user_role', label: t('field.role') },
    { name: 'module', label: t('field.module') },
    { name: 'action', label: t('field.method') },
    {
      // ⚠️ 2026-10-30 起有值了 —— 路径 + query(module 只有路径,差的就是 query)
      name: 'request_url',
      label: t('page.auditOperation.requestUrl'),
      render: (v: string) => (v ? <code style={{ fontSize: 11 }}>{v}</code> : '-'),
    },
    { name: 'description', label: t('field.description') },
    {
      // ⚠️ 2026-10-30 M126 起有值了 —— 后端补上了采集端(见文件头)
      //   这一行展示的是**脱敏后**的请求体(凭证已被 `***` 遮掉),
      //   不是客户端发的原文 —— 页面不需要再遮一遍,也不该指望它能还原原文。
      name: 'request_body',
      label: t('page.auditOperation.requestBody'),
      render: (v: string) => (
        <pre
          style={{
            background: 'var(--app-surface-bg)',
            padding: 8,
            borderRadius: 4,
            fontSize: 12,
            maxHeight: 200,
            overflow: 'auto',
          }}
        >
          {v || t('common.emptyValue')}
        </pre>
      ),
    },
    {
      // ⚠️ 2026-10-30 M126 起有值了 —— 脱敏后的 query(此前恒空)
      name: 'request_params',
      label: t('page.auditOperation.requestParams'),
      render: (v: string) => (
        <pre
          style={{
            background: 'var(--app-surface-bg)',
            padding: 8,
            borderRadius: 4,
            fontSize: 12,
            maxHeight: 200,
            overflow: 'auto',
          }}
        >
          {v || t('common.emptyValue')}
        </pre>
      ),
    },
    {
      // ⚠️ wire 名是 `response_summary`(不是 responseResult / responseBody)
      //   内容是响应的**骨架**(`{"code":0,"data_keys":[...]}`)—— **不含值**,
      //   因为响应体里全是解密后的手机号 / 身份证号。
      name: 'response_summary',
      label: t('page.auditOperation.responseSummary'),
      render: (v: string) => (
        <pre
          style={{
            background: 'var(--app-surface-bg)',
            padding: 8,
            borderRadius: 4,
            fontSize: 12,
            maxHeight: 200,
            overflow: 'auto',
          }}
        >
          {v || t('common.emptyValue')}
        </pre>
      ),
    },
    {
      name: 'status',
      label: t('common.status'),
      render: (v: number) => statusTag(v),
    },
    {
      // ⚠️ wire 名是 `error_msg`
      name: 'error_msg',
      label: t('page.auditOperation.errorMsg'),
      render: (v: string) => v || '-',
    },
    { name: 'ip', label: t('field.ip') },
    {
      // ⚠️ 2026-10-30 M126 起有值了 —— 路径参数(如 `id=9527`)
      //   没有路径参数的路由(比如 POST 新建)这一列是空的 —— 后端**不猜**。
      name: 'location',
      label: t('page.auditOperation.location'),
      render: (v: string) => v || '-',
    },
    {
      name: 'user_agent',
      label: t('field.userAgent'),
      render: (v: string) => (v ? <code style={{ fontSize: 11 }}>{v}</code> : '-'),
    },
    { name: 'duration', label: t('field.durationMs') },
    { name: 'request_id', label: t('field.requestId') },
    { name: 'trace_id', label: t('field.traceId') },
    {
      name: 'created_at',
      label: t('field.time'),
      render: (v: string) => fmtAuditTime(v),
    },
  ]

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<OperationLog>
        pageDescription={t('page.auditOperation.description')}
        searchPlaceholder={t('common.placeholder.searchAuditOp')}
        onSearch={(kw) => search({ keyword: kw })}
        onReset={reset}
        searchExtra={[
          <Select
            key="status"
            placeholder={t('common.placeholder.filterStatus')}
            allowClear
            style={{ width: 120 }}
            value={query.status}
            onChange={(v) =>
              setQuery((prev) => ({ ...prev, status: v, page: 1 }) as OperationLogQuery)
            }
            options={[
              { value: 1, label: t('common.resultSuccess') },
              { value: 0, label: t('common.resultFailed') },
            ]}
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
                  }) as OperationLogQuery,
              )
            }
          />,
        ]}
        columns={columns}
        tableProps={tableProps}
        scrollX={1200}
      />

      <ViewDrawer<OperationLog>
        title={t('resource.audit_operation_read')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
        width={720}
      />
    </div>
  )
}

export default OperationLogList
