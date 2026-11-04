/**
 * 数据变更日志 —— 列表 + 详情
 *
 * # 这一页此前是 PagePlaceholder(2026-10-30 补实)
 *
 *   路由 / 菜单 / 权限码都在,点进去只有一个骨架屏。
 *
 * # 一处**必须改掉**的权限码
 *
 *   占位页写的是 `audit:data-change:list`,而后端路由 / 种子里的权限码是
 *   **`audit:data:list`**(见 server/internal/api/router/router.go 的
 *   `/system/audit/data-change-logs` 与 seed 的资源登记)。
 *   写错了的话:菜单点得进去、页面也渲染,但接口 403 ——
 *   而页面当时只是个占位骨架,**403 根本不会发生**(它压根不发请求),
 *   所以这个错一直没被发现。页面一实装就会立刻暴露。
 *
 * # operation 的取值来自字典,不在前端写死
 *
 *   `model.SysDataChangeLog.Operation`:1 新增 / 2 修改 / 3 删除。
 *   按「字典 = 代码 enum 契约中央仓库」的规矩,这三项登记在
 *   **audit_change_operation** 字典组里(后端 seed + web 的 DictGroupCode
 *   联合类型同 commit 落地),文案与颜色都走字典。
 *
 * # 表名不是枚举
 *
 *   `table_name` 记的是**真实的表名**(如 `biz_user`),不是业务模块名 ——
 *   目前只有业主勘误那一条写入方(owner_impl.go 的 ApplyCorrection)。
 *   ⇒ 页面按"原样显示"处理,**不做字典映射**;搜索走 keyword(后端 LIKE)。
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
import type { DataChangeLog, DataChangeLogQuery } from '@/types/audit'
import AuditRangeFilter from './RangeFilter'
import { fmtAuditTime, fmtAuditJson } from './format'

const DataChangeLogList: FC = () => {
  const { t } = useI18n()
  /** 操作类型(1 新增 / 2 修改 / 3 删除)—— 文案与颜色都走字典 */
  const {
    options: operationOptions,
    getLabel: getOperationLabel,
    getColor: getOperationColor,
  } = useDict('audit_change_operation')

  /**
   * 下拉选项 —— 从字典来,**不在前端写死中文**
   *
   * ⚠️ 字典的 `item_value` 是**字符串**("1" / "2" / "3"),
   *   而 query 里的 `operation` 是 number(后端 `queryOptionalInt` 读的是整数)。
   *   这里统一转成 number,免得界面上选中"新增"、发出去的是 `operation=1`
   *   (字符串)而后端与类型都对不上。
   */
  const operationSelectOptions = operationOptions.map((o) => ({
    value: Number(o.value),
    label: o.label,
  }))

  const { tableProps, search, reset, query, setQuery } = useTable<
    DataChangeLogQuery,
    DataChangeLog
  >({
    fetch: (q) => auditApi.dataChangeLogs(q),
  })

  const [viewing, setViewing] = useState<DataChangeLog | null>(null)

  /** 操作类型标签 —— 字典里查不到这一项就显示原始值(而不是空白) */
  const operationTag = (v: number) => {
    const label = getOperationLabel(v)
    if (label === String(v)) return <Tag color="default">{v}</Tag>
    return <Tag color={getOperationColor(v)}>{label}</Tag>
  }

  const columns: ColumnsType<DataChangeLog> = [
    {
      title: t('field.tableName'),
      dataIndex: 'table_name',
      width: 180,
      fixed: 'left',
      render: (v: string) =>
        v ? (
          <Tooltip title={v}>
            <code style={{ fontSize: 11 }}>{v}</code>
          </Tooltip>
        ) : (
          '-'
        ),
    },
    {
      title: t('field.recordId'),
      dataIndex: 'record_id',
      width: 190,
    },
    {
      title: t('field.operation'),
      dataIndex: 'operation',
      width: 100,
      render: (v: number) => operationTag(v),
    },
    {
      title: t('field.user'),
      dataIndex: 'username',
      width: 120,
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
      render: (_: unknown, row: DataChangeLog) => {
        const actions: RowAction<DataChangeLog>[] = [
          // view 详情不需要 button 粒度 perm(只读操作,走路由级 audit:data:list 守卫)
          { key: 'view', label: t('common.detail'), onClick: setViewing },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  const viewFields: ViewField<DataChangeLog>[] = [
    { name: 'table_name', label: t('field.tableName'), render: (v: string) => v || '-' },
    { name: 'record_id', label: t('field.recordId'), render: (v: string) => v || '-' },
    {
      name: 'operation',
      label: t('field.operation'),
      render: (v: number) => operationTag(v),
    },
    { name: 'username', label: t('field.user'), render: (v: string) => v || '-' },
    { name: 'user_id', label: t('field.userId'), render: (v: string) => v || '-' },
    { name: 'created_at', label: t('common.createTime'), render: (v: string) => fmtAuditTime(v) },
    {
      name: 'before',
      label: t('field.beforeValue'),
      render: (v: string) => (
        <pre
          style={{
            background: 'var(--app-surface-bg)',
            padding: 8,
            borderRadius: 4,
            fontSize: 12,
            maxHeight: 240,
            overflow: 'auto',
            margin: 0,
          }}
        >
          {fmtAuditJson(v) || t('common.emptyValue')}
        </pre>
      ),
    },
    {
      name: 'after',
      label: t('field.afterValue'),
      render: (v: string) => (
        <pre
          style={{
            background: 'var(--app-surface-bg)',
            padding: 8,
            borderRadius: 4,
            fontSize: 12,
            maxHeight: 240,
            overflow: 'auto',
            margin: 0,
          }}
        >
          {fmtAuditJson(v) || t('common.emptyValue')}
        </pre>
      ),
    },
  ]

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<DataChangeLog>
        pageDescription={t('page.auditDataChange.description')}
        searchPlaceholder={t('common.placeholder.searchAuditChange')}
        onSearch={(kw) => search({ keyword: kw })}
        onReset={reset}
        searchExtra={[
          <Select
            key="operation"
            placeholder={t('common.placeholder.filterOperation')}
            allowClear
            style={{ width: 120 }}
            // ⚠️ 操作类型的"不筛"和"筛出某个值"必须能区分 ——
            //   operation=0 是个合法边界(后端指针语义:不传 = 不筛),
            //   所以清空时回 undefined 而不是 0。
            value={query.operation}
            onChange={(v) =>
              setQuery(
                (prev) =>
                  ({
                    ...prev,
                    operation: v,
                    page: 1,
                  }) as DataChangeLogQuery,
              )
            }
            options={operationSelectOptions}
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
                  }) as DataChangeLogQuery,
              )
            }
          />,
        ]}
        columns={columns}
        tableProps={tableProps}
        scrollX={1000}
      />

      <ViewDrawer<DataChangeLog>
        title={t('resource.audit_data_read')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
        width={720}
      />
    </div>
  )
}

export default DataChangeLogList
