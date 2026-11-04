/**
 * useTableSelection 的回归网(2026-10-25,§2.8 #14)
 *
 * 锁三件事:
 *   1. 基础勾选:keys / ids / rows / count
 *   2. **跨页选择不被吃掉** —— 这是本 hook 存在的核心理由:
 *      antd 的 useSelection.setSelectedKeys() 会把"不在当前 dataSource 里的 key"
 *      过滤掉(node_modules/antd/es/table/hooks/useSelection.js:168-181),
 *      除非 preserveSelectedRowKeys = true。
 *      后台表格都是后端分页 → 不开这个开关,"第 1 页勾 3 条 → 第 2 页点全选"
 *      会把第 1 页那 3 条悄悄丢掉。
 *   3. selectedRows 跨页也拿得到行对象(antd 的 records 在跨页时是 undefined)
 */
import { describe, it, expect } from 'vitest'
import { renderHook, act, render, screen, fireEvent } from '@testing-library/react'
import { Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useTableSelection } from '@/hooks/useTableSelection'

interface Row {
  id: string
  name: string
}

const columns: ColumnsType<Row> = [
  { title: 'ID', dataIndex: 'id', key: 'id' },
  { title: '名称', dataIndex: 'name', key: 'name' },
]

const page1: Row[] = [
  { id: '1', name: '甲' },
  { id: '2', name: '乙' },
]
const page2: Row[] = [
  { id: '3', name: '丙' },
  { id: '4', name: '丁' },
]

describe('useTableSelection · 基础', () => {
  it('勾一行 → keys / ids / rows / count 都对', () => {
    const { result } = renderHook(() => useTableSelection<Row>({ data: page1, rowKey: 'id' }))

    expect(result.current.selectedCount).toBe(0)

    act(() => {
      result.current.toggleRow(page1[0]!)
    })

    expect(result.current.selectedRowKeys).toEqual(['1'])
    expect(result.current.selectedIds).toEqual(['1'])
    expect(result.current.selectedCount).toBe(1)
    expect(result.current.selectedRows).toEqual([page1[0]])
    expect(result.current.isSelected('1')).toBe(true)
    expect(result.current.isSelected('2')).toBe(false)
  })

  it('再勾一次 = 取消勾选', () => {
    const { result } = renderHook(() => useTableSelection<Row>({ data: page1, rowKey: 'id' }))

    act(() => {
      result.current.toggleRow(page1[0]!)
    })
    act(() => {
      result.current.toggleRow(page1[0]!)
    })

    expect(result.current.selectedCount).toBe(0)
    expect(result.current.selectedRowKeys).toEqual([])
  })

  it('clear() 清空全部', () => {
    const { result } = renderHook(() => useTableSelection<Row>({ data: page1, rowKey: 'id' }))

    act(() => {
      result.current.selectAll(page1)
    })
    expect(result.current.selectedCount).toBe(2)

    act(() => {
      result.current.clear()
    })
    expect(result.current.selectedCount).toBe(0)
    expect(result.current.selectedRows).toEqual([])
  })

  it('selectAll 之后 selectedRows 拿得到行对象(这条路不经过 antd 的 onChange)', () => {
    const { result } = renderHook(() => useTableSelection<Row>({ data: page1, rowKey: 'id' }))

    act(() => {
      result.current.selectAll(page1)
    })

    expect(result.current.selectedIds).toEqual(['1', '2'])
    expect(result.current.selectedRows).toEqual(page1)
  })

  it('defaultSelectedRowKeys 也能拿到行对象', () => {
    const { result } = renderHook(() =>
      useTableSelection<Row>({ data: page1, rowKey: 'id', defaultSelectedRowKeys: ['1'] }),
    )

    expect(result.current.selectedIds).toEqual(['1'])
    expect(result.current.selectedRows).toEqual([page1[0]])
  })

  it('程序化选中"上一页渲染过"的行,也能拿到行对象(跨页缓存)', () => {
    const { result, rerender } = renderHook(
      ({ data }: { data: Row[] }) => useTableSelection<Row>({ data, rowKey: 'id' }),
      { initialProps: { data: page1 } },
    )

    // 翻到第 2 页 —— '1' 已经不在当前页里了
    rerender({ data: page2 })

    act(() => {
      result.current.setSelectedRowKeys(['1'])
    })

    expect(result.current.selectedIds).toEqual(['1'])
    expect(result.current.selectedRows).toEqual([page1[0]])
  })

  it('id 是 19 位数字也按字符串原样带出去(不转 number 丢精度)', () => {
    const big = '9223372036854775807'
    const { result } = renderHook(() =>
      useTableSelection<Row>({ data: [{ id: big, name: '大' }], rowKey: 'id' }),
    )

    act(() => {
      result.current.toggleRow({ id: big, name: '大' })
    })

    expect(result.current.selectedIds).toEqual([big])
    expect(result.current.selectedIds[0]).toHaveLength(19)
  })
})

/**
 * 跟真实 antd Table 一起渲染 —— 验证"跨页选择"这件 antd 默认会吃掉的事
 */
function Harness({ data }: { data: Row[] }) {
  const sel = useTableSelection<Row>({ data, rowKey: 'id' })
  return (
    <div>
      <Table<Row>
        columns={columns}
        dataSource={data}
        rowKey="id"
        size="small"
        pagination={false}
        rowSelection={sel.rowSelection}
      />
      <div data-testid="ids">{sel.selectedIds.join(',')}</div>
      <div data-testid="count">{sel.selectedCount}</div>
    </div>
  )
}

describe('useTableSelection · 跟 antd Table 一起用', () => {
  it('第 1 页勾 1 条 → 翻到第 2 页点表头全选 → 第 1 页那条还在', () => {
    const { rerender } = render(<Harness data={page1} />)

    // 索引 0 = 表头全选,1 = 第 1 行
    const boxes = screen.getAllByRole('checkbox')
    fireEvent.click(boxes[1]!)
    expect(screen.getByTestId('ids').textContent).toBe('1')

    // 翻页
    rerender(<Harness data={page2} />)

    // 表头全选 —— antd 默认会把上一页的 key 过滤掉,开了 preserveSelectedRowKeys 才留得住
    const boxes2 = screen.getAllByRole('checkbox')
    fireEvent.click(boxes2[0]!)

    expect(screen.getByTestId('count').textContent).toBe('3')
    expect(screen.getByTestId('ids').textContent).toBe('1,3,4')
  })

  it('翻页后 selectedRows 仍然拿得到上一页的行对象', () => {
    const seen: Row[] = []
    function Probe({ data }: { data: Row[] }) {
      const sel = useTableSelection<Row>({ data, rowKey: 'id' })
      seen.push(...sel.selectedRows)
      return <Table<Row> columns={columns} dataSource={data} rowKey="id" pagination={false} rowSelection={sel.rowSelection} />
    }

    const { rerender } = render(<Probe data={page1} />)
    fireEvent.click(screen.getAllByRole('checkbox')[1]!)
    rerender(<Probe data={page2} />)

    // 选中行在第一页的对象,翻页后仍然能取到
    expect(seen[seen.length - 1]).toEqual(page1[0])
  })

  it('getCheckboxProps 禁用的行点不动(防止误删自己)', () => {
    function Probe({ data }: { data: Row[] }) {
      const sel = useTableSelection<Row>({
        data,
        rowKey: 'id',
        getCheckboxProps: (r) => ({ disabled: r.id === '1' }),
      })
      return (
        <>
          <Table<Row>
            columns={columns}
            dataSource={data}
            rowKey="id"
            pagination={false}
            rowSelection={sel.rowSelection}
          />
          <div data-testid="ids">{sel.selectedIds.join(',')}</div>
        </>
      )
    }

    render(<Probe data={page1} />)
    const boxes = screen.getAllByRole('checkbox')
    expect(boxes[1]).toBeDisabled()
    fireEvent.click(boxes[1]!)
    // 被禁用的行不会被选中
    expect(screen.getByTestId('ids').textContent).toBe('')
  })
})

describe('useTableSelection · preserveAcrossPages = false', () => {
  it('翻页即清空(适合"全选 = 本页全部"的语义)', () => {
    const { result, rerender } = renderHook(
      ({ data }: { data: Row[] }) =>
        useTableSelection<Row>({ data, rowKey: 'id', preserveAcrossPages: false }),
      { initialProps: { data: page1 } },
    )

    act(() => {
      result.current.toggleRow(page1[0]!)
    })
    expect(result.current.selectedCount).toBe(1)

    rerender({ data: page2 })
    expect(result.current.selectedCount).toBe(0)
  })
})

describe('useTableSelection · 回归探针', () => {
  it('setSelectedRowKeys 能直接改集合', () => {
    const { result } = renderHook(() => useTableSelection<Row>({ data: page1, rowKey: 'id' }))
    act(() => {
      result.current.setSelectedRowKeys(['1', '2'])
    })
    expect(result.current.selectedIds).toEqual(['1', '2'])
    expect(result.current.selectedCount).toBe(2)
  })

  it('rowSelection 带上 preserveSelectedRowKeys(antd 跨页的关键开关)', () => {
    const { result } = renderHook(() => useTableSelection<Row>({ data: page1, rowKey: 'id' }))
    expect(result.current.rowSelection.preserveSelectedRowKeys).toBe(true)
  })

  it('antd 的 onChange 能把选择同步进 hook 状态', () => {
    const { result } = renderHook(() => useTableSelection<Row>({ data: page1, rowKey: 'id' }))
    expect(result.current.rowSelection.onChange).toBeTypeOf('function')
    act(() => {
      result.current.rowSelection.onChange!(['1'], page1, { type: 'single' })
    })
    expect(result.current.selectedIds).toEqual(['1'])
    expect(result.current.selectedRows).toEqual([page1[0]])
  })
})
