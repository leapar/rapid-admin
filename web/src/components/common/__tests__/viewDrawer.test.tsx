/**
 * ViewDrawer 回归网(2026-10-29 M111)
 *
 * ViewDrawer 是 12 个业务页在用的通用查看抽屉(用户 / 部门 / 业主 / 操作审计 /
 * 工单 / 公告 / 配置审计 / 资料 / 流程 / 字典 / 文件 / 文件夹),
 * 但此前**一条用例都没有**。
 *
 * 锁住的:
 *   · 基本渲染:label / 值 / 空值占位 / 自定义 render
 *   · `data` 为空 → 降级成 Empty(而不是渲染一个空表格)
 *   · `render` 回调拿到的是 **(原始值, 整行数据)**
 *   · ⚠️ **`span` 已被收回**(Descriptions 的 column 固定为 1,
 *     传 span>1 会触发 antd 的 "span 之和与 column 不匹配" dev 警告)
 */ import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ViewDrawer, type ViewField } from '@/components/common/ViewDrawer'

interface Row {
  id: string
  username: string
  nickname: string | null
  phone: string
}

const DATA: Row = {
  id: '1',
  username: 'zhangsan',
  nickname: null,
  phone: '13800000001',
}

const FIELDS: ViewField<Row>[] = [
  { name: 'username', label: '账号' },
  { name: 'nickname', label: '昵称' },
  { name: 'phone', label: '手机' },
]

describe('ViewDrawer', () => {
  it('open=false → 不渲染任何字段', () => {
    render(
      <ViewDrawer<Row>
        title="用户详情"
        open={false}
        data={DATA}
        fields={FIELDS}
        onClose={() => {}}
      />,
    )

    expect(screen.queryByText('账号')).toBeNull()
    expect(screen.queryByText('zhangsan')).toBeNull()
  })

  it('open=true → 渲染标题、label 和值', () => {
    render(<ViewDrawer<Row> title="用户详情" open data={DATA} fields={FIELDS} onClose={() => {}} />)

    expect(screen.getByText('用户详情')).toBeInTheDocument()
    expect(screen.getByText('账号')).toBeInTheDocument()
    expect(screen.getByText('zhangsan')).toBeInTheDocument()
    expect(screen.getByText('手机')).toBeInTheDocument()
    expect(screen.getByText('13800000001')).toBeInTheDocument()
  })

  it('值为 null / undefined → 显示 "-" 占位', () => {
    render(<ViewDrawer<Row> title="用户详情" open data={DATA} fields={FIELDS} onClose={() => {}} />)

    // nickname 是 null
    expect(screen.getByText('-')).toBeInTheDocument()
  })

  it('data 为 null → 降级成空状态(不是空表格)', () => {
    render(<ViewDrawer<Row> title="用户详情" open data={null} fields={FIELDS} onClose={() => {}} />)

    expect(screen.queryByText('账号')).toBeNull()
    // antd Empty 的根节点(抽屉在 portal 里,所以查 document 而不是 container)
    expect(document.querySelector('.ant-empty')).not.toBeNull()
  })

  it('render 回调拿到 (原始值, 整行数据)', () => {
    const render_ = vi.fn((v: unknown) => <span>渲染:{String(v)}</span>)
    const fields: ViewField<Row>[] = [{ name: 'phone', label: '手机', render: render_ }]
    render(<ViewDrawer<Row> title="用户详情" open data={DATA} fields={fields} onClose={() => {}} />)

    expect(render_).toHaveBeenCalledWith('13800000001', DATA)
    expect(screen.getByText('渲染:13800000001')).toBeInTheDocument()
  })

  it('extra 渲染在抽屉里', () => {
    render(
      <ViewDrawer<Row>
        title="用户详情"
        open
        data={DATA}
        fields={FIELDS}
        onClose={() => {}}
        extra={<button data-testid="extra">编辑</button>}
      />,
    )

    expect(screen.getByTestId('extra')).toBeInTheDocument()
  })

  it('点关闭按钮 → 调 onClose', () => {
    const onClose = vi.fn()
    render(<ViewDrawer<Row> title="用户详情" open data={DATA} fields={FIELDS} onClose={onClose} />)

    const closeBtn = document.querySelector('.ant-drawer-close') as HTMLElement
    expect(closeBtn).not.toBeNull()
    fireEvent.click(closeBtn)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('⚠️ 不读 span(传了也不会触发 antd 的 span 不匹配警告)', () => {
    // ⚠️ 这条测的是"组件不读 f.span":
    //   Descriptions 的 column 固定为 1,span=2 会让 antd 打
    //   "Sum of column `span` in a line not match `column` of Descriptions."
    //   收回 span 之后,ViewDrawer 压根不看这个字段 ⇒ 不会有警告。
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const fields = [{ name: 'username', label: '账号', span: 2 }] as unknown as ViewField<Row>[]
    render(<ViewDrawer<Row> title="用户详情" open data={DATA} fields={fields} onClose={() => {}} />)

    expect(screen.getByText('账号')).toBeInTheDocument()
    const spanWarning = errorSpy.mock.calls.filter((c) => String(c[0]).includes('span'))
    expect(spanWarning).toHaveLength(0)
    errorSpy.mockRestore()
  })
})
