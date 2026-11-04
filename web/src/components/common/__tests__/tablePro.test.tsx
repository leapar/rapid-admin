/**
 * TablePro / ColumnSettings 回归网(2026-10-29 M111)
 *
 * `useTablePro` 的逻辑用例在 `src/hooks/__tests__/useTablePro.test.tsx`。
 * 这里补的是**组件层**的部分 —— 组件才是"能力有没有真的接上"的地方:
 *
 *   ① ⚠️ **`rowKey` 传函数以前会被静默丢掉**
 *      `TablePro` 的类型声明是 `string | ((record) => string)`,但旧实现是
 *      `typeof rowKey === 'string' ? rowKey : undefined` ⇒ 函数形式变 undefined
 *      ⇒ 所有 `<tr>` 的 React key 变成 undefined(勾选串行 / 翻页错位)。
 *      断言落在 `tr[data-row-key]` 上 —— rc-table 会把行 key 写进这个属性。
 *   ② ⚠️ **虚拟滚动以前从来没打开过**(antd 5 要显式 `virtual: true`)。
 *      组件层能看见的差别 = 表格根节点上多一个 `ant-table-virtual` 类名。
 *   ③ ⚠️ **`hideable: false` 的列可能被存储状态永久藏起来** ——
 *      而列设置面板里它的复选框是禁用的 ⇒ 用户根本没法勾回来。
 *      组件层用"伪造的旧 localStorage"复现这个场景。
 *   ④ 工具栏的四个开关(showToolbar / showExport / showColumnSettings)。
 *   ⑤ 列设置面板:打开、勾选联动、宽度输入。
 *
 * ⚠️ **别用 `getByText('列标题')` 断言表头** —— antd 的表格会额外渲染一行
 *   **隐藏的测量行**(`tr.ant-table-measure-row`),那一行里也有一份列标题
 *   (用来量列宽)。所以每个标题在 DOM 里出现**两次**,`getByText` 会报
 *   "Found multiple elements"。这里统一走 `headerTexts()` 把测量行滤掉。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { TablePro } from '@/components/common/TablePro'
import type { TableProColumn } from '@/hooks/useTablePro'

interface Row {
  id: string
  name: string
  phone: string
  op?: string
}

const COLUMNS: TableProColumn<Row>[] = [
  { key: 'name', title: '姓名', dataIndex: 'name', width: 120 },
  { key: 'phone', title: '手机', dataIndex: 'phone', width: 150 },
]

const ROWS: Row[] = [
  { id: '1', name: '甲', phone: '13800000001' },
  { id: '2', name: '乙', phone: '13800000002' },
]

/** 真实表头的列标题(滤掉测量行) */
function headerTexts(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll('thead th'))
    .filter((th) => !th.closest('tr.ant-table-measure-row'))
    .map((th) => (th.textContent ?? '').trim())
}

/* ------------------------------------------------------------------ */
/* CSV 导出的浏览器能力桩(jsdom 没有 createObjectURL,也不该真去下载) */
/* ------------------------------------------------------------------ */
let capturedBlob: Blob | null = null
let anchorClicks: Array<{ connected: boolean; download: string }> = []
const origCreate = (URL as unknown as Record<string, unknown>).createObjectURL
const origRevoke = (URL as unknown as Record<string, unknown>).revokeObjectURL

beforeEach(() => {
  localStorage.clear()
  capturedBlob = null
  anchorClicks = []
  ;(URL as unknown as Record<string, unknown>).createObjectURL = vi.fn((b: Blob) => {
    capturedBlob = b
    return 'blob:mock'
  })
  ;(URL as unknown as Record<string, unknown>).revokeObjectURL = vi.fn()
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    anchorClicks.push({ connected: document.body.contains(this), download: this.download })
  })
})

afterEach(() => {
  ;(URL as unknown as Record<string, unknown>).createObjectURL = origCreate
  ;(URL as unknown as Record<string, unknown>).revokeObjectURL = origRevoke ?? vi.fn()
})

/* ================================================================== */

describe('TablePro · 渲染与工具栏', () => {
  it('渲染出列标题和数据行', () => {
    const { container } = render(
      <TablePro<Row> storageKey="tp1" columns={COLUMNS} dataSource={ROWS} />,
    )

    expect(headerTexts(container)).toEqual(['姓名', '手机'])
    expect(screen.getByText('甲')).toBeInTheDocument()
    expect(screen.getByText('乙')).toBeInTheDocument()
  })

  it('没有数据时导出按钮禁用', () => {
    render(<TablePro<Row> storageKey="tp2" columns={COLUMNS} dataSource={[]} />)

    const exportBtn = screen.getByRole('button', { name: /导出\s*CSV/ })
    expect(exportBtn).toBeDisabled()
  })

  it('有数据时点导出 → 生成 blob 并触发下载', () => {
    render(<TablePro<Row> storageKey="tp3" columns={COLUMNS} dataSource={ROWS} />)

    const exportBtn = screen.getByRole('button', { name: /导出\s*CSV/ })
    expect(exportBtn).not.toBeDisabled()
    fireEvent.click(exportBtn)

    expect(capturedBlob).not.toBeNull()
    expect(anchorClicks).toHaveLength(1)
    expect(anchorClicks[0]!.download).toBe('tp3.csv')
  })

  it('exportFilename 覆盖默认文件名', () => {
    render(
      <TablePro<Row>
        storageKey="tp3b"
        columns={COLUMNS}
        dataSource={ROWS}
        exportFilename="用户列表"
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /导出\s*CSV/ }))

    expect(anchorClicks[0]!.download).toBe('用户列表.csv')
  })

  it('showToolbar=false → 工具栏整个不渲染', () => {
    render(
      <TablePro<Row> storageKey="tp4" columns={COLUMNS} dataSource={ROWS} showToolbar={false} />,
    )

    expect(screen.queryByRole('button', { name: /导出\s*CSV/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /列设置/ })).toBeNull()
  })

  it('showExport=false → 只剩列设置按钮', () => {
    render(
      <TablePro<Row> storageKey="tp5" columns={COLUMNS} dataSource={ROWS} showExport={false} />,
    )

    expect(screen.queryByRole('button', { name: /导出\s*CSV/ })).toBeNull()
    expect(screen.getByRole('button', { name: /列设置/ })).toBeInTheDocument()
  })

  it('showColumnSettings=false → 只剩导出按钮', () => {
    render(
      <TablePro<Row>
        storageKey="tp6"
        columns={COLUMNS}
        dataSource={ROWS}
        showColumnSettings={false}
      />,
    )

    expect(screen.getByRole('button', { name: /导出\s*CSV/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /列设置/ })).toBeNull()
  })
})

describe('TablePro · rowKey(M111 回归)', () => {
  it('rowKey 传字符串 → 行上带 data-row-key', () => {
    const { container } = render(
      <TablePro<Row> storageKey="rk1" columns={COLUMNS} dataSource={ROWS} rowKey="id" />,
    )

    expect(container.querySelector('tr[data-row-key="1"]')).not.toBeNull()
    expect(container.querySelector('tr[data-row-key="2"]')).not.toBeNull()
  })

  it('⚠️ rowKey 传函数也必须生效(以前被静默丢弃 ⇒ 行 key 全是 undefined)', () => {
    const { container } = render(
      <TablePro<Row>
        storageKey="rk2"
        columns={COLUMNS}
        dataSource={ROWS}
        rowKey={(r) => `u-${r.id}`}
      />,
    )

    // 以前这里是 undefined ⇒ 这条会红
    expect(container.querySelector('tr[data-row-key="u-1"]')).not.toBeNull()
    expect(container.querySelector('tr[data-row-key="u-2"]')).not.toBeNull()
  })
})

describe('TablePro · 虚拟滚动(M111 回归)', () => {
  const manyRows: Row[] = Array.from({ length: 30 }, (_, i) => ({
    id: String(i),
    name: `第${i}行`,
    phone: '13800000000',
  }))

  it('行数超过阈值 → 走虚拟表格(ant-table-virtual)', () => {
    const { container } = render(
      <TablePro<Row>
        storageKey="v1"
        columns={COLUMNS}
        dataSource={manyRows}
        virtualThreshold={10}
      />,
    )

    expect(container.querySelector('.ant-table-virtual')).not.toBeNull()
  })

  it('行数没超过阈值 → 普通表格', () => {
    const { container } = render(
      <TablePro<Row> storageKey="v2" columns={COLUMNS} dataSource={ROWS} virtualThreshold={10} />,
    )

    expect(container.querySelector('.ant-table-virtual')).toBeNull()
  })

  it('树形模式与虚拟滚动互斥(树形优先)', () => {
    const { container } = render(
      <TablePro<Row>
        storageKey="v3"
        columns={COLUMNS}
        dataSource={manyRows}
        virtualThreshold={10}
        treeMode
      />,
    )

    expect(container.querySelector('.ant-table-virtual')).toBeNull()
  })
})

describe('TablePro · 列设置面板', () => {
  it('点"列设置"弹出面板,列出所有列', async () => {
    render(<TablePro<Row> storageKey="cs1" columns={COLUMNS} dataSource={ROWS} />)

    fireEvent.click(screen.getByRole('button', { name: /列设置/ }))

    await waitFor(() => {
      expect(document.querySelector('.ant-modal')).not.toBeNull()
    })
    // 面板里两列各一个复选框
    expect(screen.getAllByRole('checkbox')).toHaveLength(2)
  })

  it('取消勾选一列 → 该列从表头里消失', async () => {
    const { container } = render(
      <TablePro<Row> storageKey="cs2" columns={COLUMNS} dataSource={ROWS} />,
    )

    fireEvent.click(screen.getByRole('button', { name: /列设置/ }))
    await waitFor(() => {
      expect(screen.getAllByRole('checkbox')).toHaveLength(2)
    })

    // 勾掉"手机"那一列
    const phoneBox = screen.getAllByRole('checkbox')[1]!
    expect(phoneBox).toBeChecked()
    fireEvent.click(phoneBox)

    await waitFor(() => {
      expect(headerTexts(container)).toEqual(['姓名'])
    })
  })

  it('⚠️ hideable:false 的列:复选框禁用,且旧的 localStorage 也藏不住它', async () => {
    // 伪造"旧版本把这一列存成隐藏"的现场
    localStorage.setItem(
      'tablepro:cs3',
      JSON.stringify([
        { key: 'name', visible: true, width: 120 },
        { key: 'op', visible: false, width: 120 },
      ]),
    )
    const cols: TableProColumn<Row>[] = [
      ...COLUMNS,
      { key: 'op', title: '操作', dataIndex: 'op', hideable: false },
    ]
    const { container } = render(
      <TablePro<Row> storageKey="cs3" columns={cols} dataSource={ROWS} />,
    )

    // 表格里这一列**必须在**(以前会被存储状态藏起来,而复选框是禁用的 ⇒ 永久消失)
    expect(headerTexts(container)).toContain('操作')

    fireEvent.click(screen.getByRole('button', { name: /列设置/ }))
    await waitFor(() => {
      expect(screen.getAllByRole('checkbox')).toHaveLength(3)
    })
    const opBox = screen.getAllByRole('checkbox')[2]!
    expect(opBox).toBeDisabled()
    expect(opBox).toBeChecked()
  })

  it('列宽改动写回 localStorage', async () => {
    render(<TablePro<Row> storageKey="cs4" columns={COLUMNS} dataSource={ROWS} />)

    fireEvent.click(screen.getByRole('button', { name: /列设置/ }))
    await waitFor(() => {
      expect(screen.getAllByRole('checkbox')).toHaveLength(2)
    })

    const widthInput = document.querySelector('.ant-input-number-input') as HTMLInputElement
    expect(widthInput).not.toBeNull()
    fireEvent.change(widthInput, { target: { value: '200' } })

    await waitFor(() => {
      const raw = localStorage.getItem('tablepro:cs4')
      expect(raw).not.toBeNull()
      const state = JSON.parse(raw!) as Array<{ key: string; width: number }>
      expect(state.find((s) => s.key === 'name')?.width).toBe(200)
    })
  })
})
