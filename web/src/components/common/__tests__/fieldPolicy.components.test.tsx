/**
 * 字段级可见性 · 界面层消费(组件级)—— M168(2026-11-02)
 *
 * 三个共用外壳各自消费报告的方式:
 *   · ManagementTable —— 隐藏的**列**整个不渲染("列还在、格是空的"的修法)
 *   · EditModal       —— 隐藏的表单项不渲染;打码的变只读;
 *                        ⚠️ 提交载荷里**剔掉**打码字段(数据损坏的修法)
 *   · ViewDrawer      —— 隐藏的详情项不渲染
 *
 * # 可逆探针(拆掉实现看测试会不会红)
 *
 *	拆法                                              必须变红
 *	────────────────────────────────────────────────  ─────────────────────────
 *	ManagementTable 里 filterColumnsByReport 那行删掉   「隐藏的列不渲染」
 *	  (visibleColumns 换回 columns)
 *	EditModal 里 visibleFields 换回 fields              「隐藏字段不渲染」「打码只读」
 *	handleOk 里 omitRestrictedFields 那行删掉            「载荷里剔掉打码字段」
 *	  ⚠️ 这条是数据损坏那半个洞的守卫 —— 最要紧的一条
 *	ViewDrawer 里 visibleFields 换回 fields             「详情里隐藏项不渲染」
 *	EditModal 的兜底(getLatestFieldPolicyReport)删掉   「新建流程用页面归档兜底」
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal } from '@/components/common/EditModal'
import { ViewDrawer } from '@/components/common/ViewDrawer'
import { stampFieldPolicyReport } from '@/utils/fieldPolicy'
import { setLatestFieldPolicyReport, getLatestFieldPolicyReport } from '@/utils/fieldPolicy'

const REPORT = { hidden: ['phone'], masked: ['real_name'] }

beforeEach(() => {
  // 清页面归档,防止上一条用例的归档串进来(单例状态)
  setLatestFieldPolicyReport('/__reset__', { hidden: [], masked: [] })
})

afterEach(() => {
  setLatestFieldPolicyReport('/__reset__', { hidden: [], masked: [] })
})

/** 表格里真实渲染出来的列头(antd 的 measure cell 会复制一份文本,不能按文本查) */
function tableHeaders(): string[] {
  return Array.from(document.querySelectorAll('.ant-table-thead th')).map(
    (th) => th.textContent ?? '',
  )
}

describe('ManagementTable · 隐藏的列不渲染', () => {
  const columns: ColumnsType<Record<string, unknown>> = [
    { title: '昵称', dataIndex: 'nickname', key: 'nickname' },
    { title: '手机号', dataIndex: 'phone', key: 'phone' },
  ]

  it('报告里 hidden 的列整个消失,其它列照常', () => {
    render(
      <ManagementTable
        columns={columns}
        tableProps={{
          dataSource: [{ id: '1', nickname: '张三', phone: '138****1234' }],
          rowKey: 'id',
          fieldPolicy: REPORT,
        }}
      />,
    )
    expect(screen.getByText('张三')).toBeInTheDocument()
    const headers = tableHeaders()
    expect(headers).toContain('昵称')
    // ⚠️ 列头都不在了 —— 不是"格是空的"那种半吊子状态
    expect(headers).not.toContain('手机号')
    expect(screen.queryByText('138****1234')).toBeNull()
  })

  it('没有报告 ⇒ 行为与 M168 之前完全一致', () => {
    render(
      <ManagementTable
        columns={columns}
        tableProps={{
          dataSource: [{ id: '1', nickname: '张三', phone: '138****1234' }],
          rowKey: 'id',
        }}
      />,
    )
    const headers = tableHeaders()
    expect(headers).toContain('手机号')
    expect(screen.getByText('138****1234')).toBeInTheDocument()
  })
})

describe('EditModal · 表单字段的收窄', () => {
  const fields = [
    { name: 'nickname', label: '昵称' },
    { name: 'phone', label: '手机号' },
    { name: 'real_name', label: '姓名' },
  ]

  it('隐藏的表单项不渲染', () => {
    render(
      <App>
        <EditModal
          title="编辑"
          open
          initialValues={stampFieldPolicyReport<Partial<Record<string, unknown>>>(
            { nickname: '张三', phone: '138****1234', real_name: '张*' },
            REPORT,
          )}
          fields={fields}
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />
      </App>,
    )
    expect(screen.getByText('昵称')).toBeInTheDocument()
    expect(screen.queryByText('手机号')).toBeNull()
  })

  it('打码的字段照常显示但变只读', async () => {
    render(
      <App>
        <EditModal
          title="编辑"
          open
          initialValues={stampFieldPolicyReport<Partial<Record<string, unknown>>>(
            { nickname: '张三', real_name: '张*' },
            { hidden: [], masked: ['real_name'] },
          )}
          fields={fields}
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />
      </App>,
    )
    const input = await waitFor(() => {
      const el = screen.getByLabelText('姓名') as HTMLInputElement
      expect(el.value).toBe('张*') // 打码值照常显示
      return el
    })
    expect(input.disabled).toBe(true)
  })

  it('⚠️ 提交载荷里剔掉打码字段(打码值覆盖真实值 = 数据损坏)', async () => {
    const onSubmit = vi.fn()
    render(
      <App>
        <EditModal
          title="编辑"
          open
          initialValues={stampFieldPolicyReport<Partial<Record<string, unknown>>>(
            { nickname: '张三', real_name: '张*' },
            { hidden: [], masked: ['real_name'] },
          )}
          fields={fields}
          onSubmit={onSubmit}
          onCancel={vi.fn()}
        />
      </App>,
    )
    // 改昵称,然后保存(antd Button 汉字间会插空格,匹配要容 \s*)
    fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '张四' } })
    fireEvent.click(screen.getByText(/保\s*存/))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1))
    const payload = onSubmit.mock.calls[0]![0] as Record<string, unknown>
    expect(payload.nickname).toBe('张四')
    // ⚠️ real_name 是打码值('张*'),绝不能进载荷 ——
    //    原样提交 = 数据库里的真实姓名被自己的打码值覆盖
    expect('real_name' in payload).toBe(false)
  })

  it('⚠️ 新建流程(initialValues 没盖章)用页面归档兜底', async () => {
    // 页面归档:本页最近一次列表响应说 phone 被隐藏
    setLatestFieldPolicyReport(window.location.pathname, REPORT)

    render(
      <App>
        <EditModal
          title="新增"
          open
          initialValues={{ nickname: '', phone: '', real_name: '' }}
          fields={fields}
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />
      </App>,
    )
    expect(screen.getByText('昵称')).toBeInTheDocument()
    expect(screen.queryByText('手机号')).toBeNull()
  })

  it('新建流程 + 没有归档 ⇒ 一切照旧(M168 之前的行为)', () => {
    // beforeEach 已把归档指到 /__reset__,当前路径(/)读不到
    expect(getLatestFieldPolicyReport()).toBeNull()
    render(
      <App>
        <EditModal
          title="新增"
          open
          initialValues={{ nickname: '', phone: '' }}
          fields={fields}
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />
      </App>,
    )
    expect(screen.getByText('昵称')).toBeInTheDocument()
    expect(screen.getByText('手机号')).toBeInTheDocument()
  })
})

describe('ViewDrawer · 详情字段的收窄', () => {
  const fields = [
    { name: 'nickname', label: '昵称' },
    { name: 'phone', label: '手机号' },
  ]

  it('隐藏的详情项不渲染;打码的照常显示', () => {
    render(
      <ViewDrawer
        title="详情"
        open
        data={stampFieldPolicyReport<Record<string, unknown>>(
          { nickname: '张三', phone: '138****1234' },
          REPORT,
        )}
        fields={fields}
        onClose={vi.fn()}
      />,
    )
    expect(screen.getByText('张三')).toBeInTheDocument()
    // phone 在报告里是 hidden ⇒ 整项不渲染(连打码值也不该出现)
    expect(screen.queryByText('手机号')).toBeNull()
    expect(screen.queryByText('138****1234')).toBeNull()
  })

  it('打码的详情项照常显示(值已经是服务端打码后的)', () => {
    render(
      <ViewDrawer
        title="详情"
        open
        data={stampFieldPolicyReport<Record<string, unknown>>(
          { nickname: '张三', phone: '138****1234' },
          { hidden: [], masked: ['phone'] },
        )}
        fields={fields}
        onClose={vi.fn()}
      />,
    )
    expect(screen.getByText('手机号')).toBeInTheDocument()
    expect(screen.getByText('138****1234')).toBeInTheDocument()
  })

  it('行对象没盖章(页面重拼过)⇒ 用页面归档兜底', () => {
    setLatestFieldPolicyReport(window.location.pathname, REPORT)
    render(
      <ViewDrawer
        title="详情"
        open
        data={{ nickname: '张三', phone: '138****1234' }}
        fields={fields}
        onClose={vi.fn()}
      />,
    )
    expect(screen.getByText('张三')).toBeInTheDocument()
    expect(screen.queryByText('手机号')).toBeNull()
  })

  it('没有报告 ⇒ 一切照旧', () => {
    render(
      <ViewDrawer
        title="详情"
        open
        data={{ nickname: '张三', phone: '138****1234' }}
        fields={fields}
        onClose={vi.fn()}
      />,
    )
    expect(screen.getByText('手机号')).toBeInTheDocument()
    expect(screen.getByText('138****1234')).toBeInTheDocument()
  })
})
