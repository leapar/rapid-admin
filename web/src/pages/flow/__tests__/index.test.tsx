/**
 * 审批流程模板列表 · 状态筛选(2026-10-26)
 *
 * ## 这条网守的是什么
 *
 *   `is_active` 在线上是**三态**:不传 = 不过滤 / true = 只看启用 / false = 只看停用。
 *   "全部"必须对应**不传**,不是传 false —— 传 false 是"只看停用"。
 *   这两者差得很远:传错了列表会**只剩停用的模板**,而界面上看不出异常
 *   (列表还是满的,只是内容全变了,用户以为"这个模板库就这几条")。
 *
 *   后端那边是 `if v := c.Query("is_active"); v != ""` 才解析
 *   (server/internal/api/handler/v1/approval_flow.go:37),空串 = 不过滤;
 *   前端这边 axios 会把 undefined 的参数丢掉 —— 两头一拼,"全部"只能靠
 *   **不传**来实现。
 *
 * ## 为什么今天才有这条网
 *
 *   2026-10-26 之前这里是一个可点击的 <Tag>(显示"全部状态 / 只看停用",
 *   点一下在两态之间切)。它只暴露了三态里的两态,而且看不出是能点的 ——
 *   今天换成了 Select 三态(跟 job / notice / audit 等其它列表页一致)。
 *   换完之后"三态到底怎么映射到请求参数"就成了新的、容易写错的地方。
 *
 * ## 为什么看 DOM 而不是看源码
 *
 *   选项是运行时渲染出来的,源码里有没有 'all' 字面量跟界面上真能选到什么
 *   是两回事 —— 这条网看的是**界面上真能选到的选项**。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import FlowList from '../index'
import { flowApi } from '@/api/flow'

vi.mock('@/api/flow', () => ({
  flowApi: {
    list: vi.fn(),
    detail: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
  },
}))

const listMock = vi.mocked(flowApi.list)

/** 列表行(后端 FlowListItem 的形状) */
const ROW = {
  id: 'f1',
  code: 'material_flow',
  name: '资料审批模板',
  biz_type: 'digital_material',
  stage_count: 2,
  is_active: true,
  remark: '',
  created_at: '2026-10-01T00:00:00+08:00',
  updated_at: '2026-10-01T00:00:00+08:00',
}

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <FlowList />
      </MemoryRouter>
    </App>,
  )
}

/** 当前下拉显示的选中项 */
function selectedStatus(): string {
  const el = document.querySelector('.ant-select-selection-item')
  return (el?.getAttribute('title') ?? el?.textContent ?? '').trim()
}

/**
 * 打开状态下拉,返回**界面上真能选到的**所有选项文案
 *
 * ⚠️ antd 5 的下拉是**懒渲染**的 —— 不打开,一个选项都不在 DOM 里。
 */
async function openStatusSelect(): Promise<string[]> {
  const selector = document.querySelector('.ant-select-selector') as HTMLElement
  expect(selector).toBeTruthy()
  fireEvent.mouseDown(selector)
  await waitFor(() => expect(document.querySelector('.ant-select-dropdown')).toBeTruthy())
  return Array.from(document.querySelectorAll('.ant-select-item-option-content')).map(
    (el) => (el.textContent ?? '').trim(),
  )
}

/** 点某个选项(在 dropdown 里精确找,避开页面正文里的同名文字) */
async function pickStatus(label: string) {
  const option = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
    (el) => (el.textContent ?? '').trim() === label,
  )
  if (!option) throw new Error(`下拉里找不到选项:${label}`)
  fireEvent.click(option)
}

/** 点搜索栏的"重置" */
function clickReset() {
  fireEvent.click(screen.getByText('重置'))
}

/** 第 n 次请求里的 is_active —— undefined = 不过滤,false = 只看停用 */
function isActiveOfCall(n: number): boolean | undefined {
  return (listMock.mock.calls[n]![0] as { is_active?: boolean }).is_active
}

describe('审批流程模板列表 · 状态筛选', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ list: [ROW], total: 1, page: 1, size: 20 } as never)
  })

  it('下拉里正好三档:全部 / 启用 / 停用', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const options = await openStatusSelect()
    expect(options).toEqual(['全部', '启用', '停用'])
    // 旧 Tag 的文案 —— 换控件时别把它带过来当第四档
    expect(options).not.toContain('只看停用')
  })

  it('首屏不过滤:请求里没有 is_active,更不是 false', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ 传 false 是"只看停用" —— 那会让列表只剩停用的模板
    expect(isActiveOfCall(0)).toBeUndefined()
    expect(isActiveOfCall(0)).not.toBe(false)
    expect(selectedStatus()).toBe('全部')
  })

  it('选"启用" → 请求带 is_active=true', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    await openStatusSelect()
    await pickStatus('启用')

    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(2))
    expect(isActiveOfCall(1)).toBe(true)
    expect(selectedStatus()).toBe('启用')
  })

  it('选"停用" → 请求带 is_active=false', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    await openStatusSelect()
    await pickStatus('停用')

    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(2))
    expect(isActiveOfCall(1)).toBe(false)
    expect(selectedStatus()).toBe('停用')
  })

  it('从"停用"切回"全部" → 请求回到不过滤(不是 false)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    await openStatusSelect()
    await pickStatus('停用')
    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(2))

    await openStatusSelect()
    await pickStatus('全部')
    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(3))

    expect(isActiveOfCall(2)).toBeUndefined()
    expect(isActiveOfCall(2)).not.toBe(false)
    expect(selectedStatus()).toBe('全部')
  })

  it('点"重置" → 回到"全部",请求也跟着回到不过滤(显示和数据不能是两套)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    await openStatusSelect()
    await pickStatus('停用')
    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(2))

    clickReset()

    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(3))
    expect(isActiveOfCall(2)).toBeUndefined()
    expect(selectedStatus()).toBe('全部')
  })
  // ⚠️ 超时统一由 vitest.config.ts 的 testTimeout 管(antd 页面级用例单条要 10~45s)
})
