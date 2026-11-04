/**
 * DataTable 回归网(2026-10-29 M111)
 *
 * DataTable 是 useTable 的配套外壳(错误态 + 搜索区 + 工具栏 + Card),
 * 全仓**零业务调用方**(只有 barrel 导出),但它是公开的框架组件 ——
 * 没有用例就等于没有契约。
 *
 * 锁住的:
 *   · 错误态两种形态(无数据 → 空态降级;有旧数据 → 提示条 + 旧数据留着)
 *   · `error` / `onRetry` 是框架通道,**不能漏到 DOM 上**
 *   · ⚠️ **调用方传的 `locale` 以前会被吞掉**(M111 修正)
 *   · `rowKey` 生效(字符串 / 函数两种形式)
 *   · card 开关 / searchForm / toolbar 插槽
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DataTable } from '@/components/common/DataTable'
import type { UseTableResult } from '@/hooks/useTable'
import type { PageRequest } from '@/types/api'

/**
 * ⚠️ 用 `type` 而不是 `interface` —— `DataTable` 的泛型约束是
 *   `T extends Record<string, unknown>`,而 **interface 没有隐式索引签名**
 *   ⇒ `interface Row` 满足不了这个约束(TS2344)。
 *   类型别名才有隐式索引签名。
 */
type Row = {
  id: string
  name: string
}

const ROWS: Row[] = [
  { id: '1', name: '甲' },
  { id: '2', name: '乙' },
]

const COLUMNS = [{ title: '名称', dataIndex: 'name', key: 'name' }]

/**
 * 造一个 useTable 的返回值
 *
 * DataTable 只读 `useTableResult.tableProps`,所以这里只需要那一块;
 * 其余字段用 `as unknown as` 补齐(不引入真的 useTable,免得异步时序污染断言)。
 */
function makeResult(
  opts: {
    dataSource?: Row[]
    error?: unknown
    onRetry?: () => void
  } = {},
): UseTableResult<PageRequest, Row> {
  const { dataSource = [], error = null, onRetry = vi.fn() } = opts
  return {
    tableProps: {
      dataSource,
      loading: false,
      error,
      onRetry,
      pagination: {
        current: 1,
        pageSize: 20,
        total: dataSource.length,
        showSizeChanger: true,
        showQuickJumper: true,
        showTotal: (t: number) => `共 ${t} 条`,
        onChange: vi.fn(),
      },
    },
  } as unknown as UseTableResult<PageRequest, Row>
}

describe('DataTable · 基本渲染', () => {
  it('渲染数据行与列标题', () => {
    const { container } = render(
      <DataTable<Row, PageRequest>
        useTableResult={makeResult({ dataSource: ROWS })}
        columns={COLUMNS}
      />,
    )

    expect(screen.getByText('甲')).toBeInTheDocument()
    expect(screen.getByText('乙')).toBeInTheDocument()
    // 表头在 DOM 里出现两次(antd 的隐藏测量行),所以用 getAllByText
    expect(screen.getAllByText('名称').length).toBeGreaterThan(0)
    expect(container.querySelector('tr[data-row-key="1"]')).not.toBeNull()
  })

  it('card=false → 不套 Card;card=true → 套 Card', () => {
    const { container: withCard } = render(
      <DataTable<Row, PageRequest> useTableResult={makeResult()} columns={COLUMNS} />,
    )
    expect(withCard.querySelector('.ant-card')).not.toBeNull()

    const { container: noCard } = render(
      <DataTable<Row, PageRequest> useTableResult={makeResult()} columns={COLUMNS} card={false} />,
    )
    expect(noCard.querySelector('.ant-card')).toBeNull()
  })

  it('searchForm / toolbar 插槽都渲染', () => {
    render(
      <DataTable<Row, PageRequest>
        useTableResult={makeResult()}
        columns={COLUMNS}
        searchForm={<div data-testid="search">搜索区</div>}
        toolbar={<div data-testid="toolbar">工具栏</div>}
      />,
    )

    expect(screen.getByTestId('search')).toBeInTheDocument()
    expect(screen.getByTestId('toolbar')).toBeInTheDocument()
  })

  it('rowKey 传函数也生效', () => {
    const { container } = render(
      <DataTable<Row, PageRequest>
        useTableResult={makeResult({ dataSource: ROWS })}
        columns={COLUMNS}
        rowKey={(r) => `k-${r.id}`}
      />,
    )

    expect(container.querySelector('tr[data-row-key="k-1"]')).not.toBeNull()
  })
})

describe('DataTable · 错误态', () => {
  it('失败且没有数据 → 空态里出现"加载失败"和重试按钮', () => {
    const onRetry = vi.fn()
    render(
      <DataTable<Row, PageRequest>
        useTableResult={makeResult({ error: new Error('boom'), onRetry })}
        columns={COLUMNS}
      />,
    )

    expect(screen.getByText('加载失败')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('失败但还有旧数据 → 出现提示条,旧数据仍然显示', () => {
    render(
      <DataTable<Row, PageRequest>
        useTableResult={makeResult({ dataSource: ROWS, error: new Error('boom') })}
        columns={COLUMNS}
      />,
    )

    expect(screen.getAllByText('加载失败').length).toBeGreaterThan(0)
    expect(screen.getByText('甲')).toBeInTheDocument()
    expect(screen.getByText('乙')).toBeInTheDocument()
  })

  it('没有错误 → 不渲染错误态', () => {
    render(
      <DataTable<Row, PageRequest>
        useTableResult={makeResult({ dataSource: ROWS })}
        columns={COLUMNS}
      />,
    )

    expect(screen.queryByText('加载失败')).toBeNull()
  })

  it('error / onRetry 是框架通道,不能漏到 DOM 上', () => {
    const { container } = render(
      <DataTable<Row, PageRequest>
        useTableResult={makeResult({ dataSource: ROWS, error: new Error('boom') })}
        columns={COLUMNS}
      />,
    )

    expect(container.querySelector('[error]')).toBeNull()
    expect(container.querySelector('[onretry]')).toBeNull()
  })
})

describe('DataTable · locale', () => {
  it('⚠️ 调用方传的 locale 不会被吞掉(以前被覆盖成 undefined)', () => {
    render(
      <DataTable<Row, PageRequest>
        useTableResult={makeResult()}
        columns={COLUMNS}
        locale={{ emptyText: '这里还没有数据' }}
      />,
    )

    expect(screen.getByText('这里还没有数据')).toBeInTheDocument()
  })

  it('出错且无数据时,错误态占位优先于调用方的 emptyText', () => {
    render(
      <DataTable<Row, PageRequest>
        useTableResult={makeResult({ error: new Error('boom') })}
        columns={COLUMNS}
        locale={{ emptyText: '这里还没有数据' }}
      />,
    )

    expect(screen.getByText('加载失败')).toBeInTheDocument()
    expect(screen.queryByText('这里还没有数据')).toBeNull()
  })
})
