/**
 * DataTable —— 通用数据表格(基于 Antd Table)
 *
 * 用法:
 *   <DataTable
 *     columns={columns}
 *     useTableResult={table}
 *     rowKey="id"
 *     searchForm={<SearchForm .../>}
 *   />
 */
import type React from 'react'
import { Table, Card } from 'antd'
import type { TableProps } from 'antd'
import type { UseTableResult } from '@/hooks/useTable'
import type { PageRequest } from '@/types/api'
import { TableErrorState } from './TableErrorState'
import { filterColumnsByReport } from './fieldPolicy'

export interface DataTableProps<T, Q extends PageRequest> extends Omit<
  TableProps<T>,
  'dataSource' | 'loading' | 'pagination'
> {
  useTableResult: UseTableResult<Q, T>
  /** 搜索表单(可放 Card 顶部) */
  searchForm?: React.ReactNode
  /** 工具栏(放表格上方右侧) */
  toolbar?: React.ReactNode
  rowKey?: string | ((row: T) => string)
  /** 是否用 Card 包裹 */
  card?: boolean
}

export function DataTable<T extends Record<string, unknown>, Q extends PageRequest>({
  useTableResult,
  searchForm,
  toolbar,
  rowKey = 'id',
  card = true,
  ...rest
}: DataTableProps<T, Q>) {
  /**
   * 错误态(2026-10-25 M7)—— 与 ManagementTable 同一套规矩
   *   · `error` / `onRetry` / `fieldPolicy` 是 useTable 的框架通道,
   *     **不是 Antd 的 prop**,展开给 `<Table>` 之前必须摘掉
   *   · 没数据 → 降级占位放进表格的 empty 区
   *   · 有旧数据 → 表格上方一条提示条,旧数据留着
   */
  const { error, onRetry, fieldPolicy, ...tableProps } = useTableResult.tableProps
  const hasData = (tableProps.dataSource?.length ?? 0) > 0
  const showError = error !== null && error !== undefined

  /**
   * ⚠️ **M111 修正:调用方传的 `locale` 以前会被吞掉**
   *   旧写法是 `<Table {...rest} {...tableProps} locale={...} />` ——
   *   `locale` 写在最后,没出错时它的值是 `undefined`,
   *   于是**把 `rest.locale` 覆盖成了 undefined**。
   *   也就是说 `<DataTable locale={{ emptyText: '自定义' }} />` 静默失效。
   *   现在把 `locale` 从 rest 里摘出来,没出错时原样还给 Antd。
   */
  const { locale: callerLocale, ...tableRest } = rest

  // M168:把"服务端已经删掉键"的列藏掉(报告来自响应,前端不自己算规则)
  const { columns: callerColumns, ...tableRest2 } = tableRest as TableProps<T>
  const visibleColumns = filterColumnsByReport(callerColumns ?? [], fieldPolicy ?? null)

  const inner = (
    <>
      {searchForm}
      {toolbar}
      {showError && hasData && <TableErrorState variant="strip" onRetry={onRetry} />}
      <Table<T>
        {...tableRest2}
        // ⚠️ 没传列时给 undefined(保持原行为),不要给 [](空数组会盖掉一切)
        columns={callerColumns ? visibleColumns : undefined}
        {...tableProps}
        rowKey={rowKey as TableProps<T>['rowKey']}
        locale={
          showError && !hasData
            ? { emptyText: <TableErrorState variant="empty" onRetry={onRetry} /> }
            : callerLocale
        }
      />
    </>
  )
  if (card) {
    // ⚠️ M111:`bordered` 在 antd 5.29 已废弃(控制台会打 deprecated 警告),
    //   等价的新写法是 `variant="borderless"`。
    return <Card variant="borderless">{inner}</Card>
  }
  return <>{inner}</>
}

export default DataTable
