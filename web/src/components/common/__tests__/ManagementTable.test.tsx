/**
 * ManagementTable 渲染冒烟测试
 *
 * ⚠️ **这个用例是 web/ 测试设施的第二个探针** —— 纯逻辑测试(pageName)证明
 *   「别名 + TS + vitest」通了,但没证明「React + antd + jsdom」通了。
 *   而后面 M5b(行选择 + 批量操作)要动的正是 antd Table 的 rowSelection,
 *   如果 jsdom 渲染 antd Table 本身就跑不起来,那个模块一样没有可逆探针。
 *
 *   所以这里渲染的是**真实组件**(不是造个 <div> 意思一下):
 *   antd Table + rowSelection + ResizeObserver 桩 + auto-height 逻辑。
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ManagementTable } from '@/components/common/ManagementTable'
// ⚠️ **ColumnsType 必须从 'antd/es/table' 取,不能从 'antd' 根取** ——
//   antd 5.29 的根导出里没有 ColumnsType(TS2305)。
//   src/pages/file/list.tsx、folder.tsx、profile/SessionPanel.tsx 都是这么写的,
//   跟着它们走,别自创一套。
import type { ColumnsType } from 'antd/es/table'

interface Row {
  id: string
  name: string
}

const columns: ColumnsType<Row> = [
  { title: 'ID', dataIndex: 'id', key: 'id' },
  { title: '名称', dataIndex: 'name', key: 'name' },
]

const rows: Row[] = [
  { id: '1', name: '甲' },
  { id: '2', name: '乙' },
]

describe('ManagementTable', () => {
  it('不再渲染页内大标题(标题交给面包屑),数据行照常渲染', () => {
    render(
      <ManagementTable<Row>
        columns={columns}
        tableProps={{ dataSource: rows, rowKey: 'id' }}
      />,
    )
    // 2026-10-25:页内大标题与布局里的面包屑重复,已从列表页去掉 —— 这里锁住
    // 「不再渲染」,将来谁想加回来,这条会先红。
    expect(screen.queryByText('用户管理')).not.toBeInTheDocument()
    expect(screen.getByText('甲')).toBeInTheDocument()
    expect(screen.getByText('乙')).toBeInTheDocument()
  })

  it('传了 rowSelection 就渲染出选择列的复选框(表头全选 + 每行一个)', () => {
    render(
      <ManagementTable<Row>
        columns={columns}
        tableProps={{
          dataSource: rows,
          rowKey: 'id',
          rowSelection: { selectedRowKeys: [], onChange: vi.fn() },
        }}
      />,
    )
    // 2 行 + 1 个表头全选
    expect(screen.getAllByRole('checkbox')).toHaveLength(3)
  })

  it('勾选一行会把该行的 key 回调出去', () => {
    const onChange = vi.fn()
    render(
      <ManagementTable<Row>
        columns={columns}
        tableProps={{
          dataSource: rows,
          rowKey: 'id',
          rowSelection: { selectedRowKeys: [], onChange },
        }}
      />,
    )
    // 索引 0 是表头全选,1 / 2 是两行
    const checkboxes = screen.getAllByRole('checkbox')
    const rowCheckbox = checkboxes[1]
    expect(rowCheckbox).toBeDefined()
    fireEvent.click(rowCheckbox!)
    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange.mock.calls[0]?.[0]).toEqual(['1'])
  })
})

/**
 * headerExtra 槽位(2026-10-25)
 *
 * 字典管理页要在页头**下面**放一条"字典只读"的说明 Alert。
 * 这条 Alert 一度被直接写死进本组件 —— 而本组件被 **17 个页面**共用
 * (用户/角色/权限/字典/任务/文件/业主/公告/资料/工单/审批/流程/审计/配置审计),
 * 写死 = 用户管理页顶上也会弹出"字典是代码 enum 的中央仓库"。
 *
 * 这里的用例把那条边界钉死:
 *   · 传了 headerExtra → 渲染在页头描述**之后**
 *   · 没传 headerExtra → 页面上一个 Alert 都没有
 */
describe('ManagementTable · headerExtra', () => {
  it('headerExtra 渲染在页头描述之后、搜索区之前', () => {
    render(
      <ManagementTable<Row>
        pageDescription="页头描述"
        headerExtra={<div data-testid="extra">页头下方的额外内容</div>}
        searchPlaceholder="搜索关键字"
        onSearch={() => {}}
        columns={columns}
        tableProps={{ dataSource: rows, rowKey: 'id' }}
      />,
    )
    const desc = screen.getByText('页头描述')
    const extra = screen.getByTestId('extra')
    const search = screen.getByPlaceholderText('搜索关键字')
    // DOCUMENT_POSITION_FOLLOWING(4):排在后面的那个带这个位
    expect(desc.compareDocumentPosition(extra) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(extra.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('不传 headerExtra → 页面上没有任何 Alert(页面专属文案不许写死进共享组件)', () => {
    const { container } = render(
      <ManagementTable<Row>
        pageDescription="页头描述"
        columns={columns}
        tableProps={{ dataSource: rows, rowKey: 'id' }}
      />,
    )
    expect(container.querySelector('.ant-alert')).toBeNull()
  })
})

/**
 * inlineToolBar 的位置(2026-10-25)
 *
 * 用户要求:「inlineToolBar 放 searchBar 的右侧。searchBar 左侧是搜索组件,
 * 右侧放 inlineToolBar」。
 *
 * 之前 inlineToolBar 渲染在 Card 里、表格正上方,跟搜索框上下叠成两行。
 * 现在跟搜索框同一行:左搜索、右工具栏。
 */
describe('ManagementTable · inlineToolBar 位置', () => {
  it('工具栏在搜索区容器内、且排在搜索框之后(同一行:搜索在左,工具栏在右)', () => {
    render(
      <ManagementTable<Row>
        searchPlaceholder="搜索关键字"
        onSearch={() => {}}
        inlineToolBar={<button data-testid="toolbar">刷新</button>}
        columns={columns}
        tableProps={{ dataSource: rows, rowKey: 'id' }}
      />,
    )
    const search = screen.getByPlaceholderText('搜索关键字')
    const toolbar = screen.getByTestId('toolbar')
    // ⚠️ **必须断言"在搜索区容器内"** —— 只断言"排在搜索框之后"是不够的:
    //   旧布局(工具栏在 Card 里、表格上方)同样满足"排在搜索框之后",
    //   探针实测过:那样这条用例**不会红**,是假绿。
    //   真正区分"同一行"还是"下一行"的属性 = 工具栏是不是 SearchSection 的后代。
    expect(toolbar.closest('.search-section')).not.toBeNull()
    expect(search.compareDocumentPosition(toolbar) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('只有工具栏、没有搜索框时,工具栏照样渲染(不能跟着 searchBar 一起消失)', () => {
    render(
      <ManagementTable<Row>
        inlineToolBar={<button data-testid="toolbar">刷新</button>}
        columns={columns}
        tableProps={{ dataSource: rows, rowKey: 'id' }}
      />,
    )
    expect(screen.getByTestId('toolbar')).toBeInTheDocument()
  })
})

/**
 * 错误态(2026-10-25 M7)
 *
 * M6 让 useTable 把失败记进 `error`,但**状态没人看等于没有**:
 *   · 失败且一条数据都没有 → 表格的空态区显示"加载失败 + 重试"
 *   · 失败但还有旧数据     → 表格上方一条提示条,旧数据继续留着
 *     (一次抖动把整页洗白比不新更糟)
 *
 * ⚠️ antd 5 的 Button 在两个汉字之间会自动插空格("重试" → "重 试"),
 *   所以按钮匹配用正则 `/重\s*试/`。
 */
describe('ManagementTable · 错误态', () => {
  it('失败且没有数据 → 空态里出现"加载失败"和"重试"按钮', () => {
    const onRetry = vi.fn()
    render(
      <ManagementTable<Row>
        columns={columns}
        tableProps={{
          dataSource: [],
          rowKey: 'id',
          error: new Error('boom'),
          onRetry,
        }}
      />,
    )

    expect(screen.getByText('加载失败')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('失败但还有旧数据 → 出现提示条,旧数据仍然显示', () => {
    render(
      <ManagementTable<Row>
        columns={columns}
        tableProps={{
          dataSource: rows,
          rowKey: 'id',
          error: new Error('boom'),
          onRetry: vi.fn(),
        }}
      />,
    )

    expect(screen.getAllByText('加载失败').length).toBeGreaterThan(0)
    // 旧数据没被洗白
    expect(screen.getByText('甲')).toBeInTheDocument()
    expect(screen.getByText('乙')).toBeInTheDocument()
  })

  it('没有错误 → 不渲染错误态', () => {
    render(
      <ManagementTable<Row>
        columns={columns}
        tableProps={{ dataSource: rows, rowKey: 'id' }}
      />,
    )
    expect(screen.queryByText('加载失败')).toBeNull()
  })

  it('error / onRetry 是框架通道,不能漏到 DOM 上', () => {
    const { container } = render(
      <ManagementTable<Row>
        columns={columns}
        tableProps={{
          dataSource: rows,
          rowKey: 'id',
          error: new Error('boom'),
          onRetry: vi.fn(),
        }}
      />,
    )
    // ⚠️ 这条是**防御性断言,不是探针**(变异时它不会红):
    //   Antd Table 本身会把不认识的 prop 吃掉,所以现在两条都是 null。
    //   它守的是"将来有人把 `<Table {...tableProps}>` 换成展开到 DOM 上"
    //   这种改法 —— 那时 error 会被渲染成 error="[object Object]"。
    expect(container.querySelector('[error]')).toBeNull()
    expect(container.querySelector('[onretry]')).toBeNull()
  })
})
