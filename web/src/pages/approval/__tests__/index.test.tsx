/**
 * 我的审批页 —— 三个盒子的选择器(2026-10-26)
 *
 * ## 这条网守的是什么
 *
 *   `box` 是**安全边界的一部分**,不是普通筛选项(设计文档 §8.2):
 *   三个盒子各自由"我和这条单的关系"定义,**没有 `box=all`**。
 *   后端对空 / 非法 box 直接报错(`approval_query_impl.go` 的 `default:` 分支):
 *   "退化 = 前端漏传一个参数 = 全平台审批单裸奔"。
 *
 *   页面把三个盒子做成**一个必选下拉**(2026-10-26 从三个 Tab 改过来),
 *   那么"往里加一个'全部'"就变成一件**很容易顺手做掉**的事 ——
 *   而那一步就是越权。所以这里把它钉死。
 *
 * ## 为什么不是"看源码有没有 all"那种断言
 *
 *   选项是从 `BOXES` 常量渲染出来的,源码里有没有 `all` 字面量跟界面上
 *   到底给不给用户选是两回事。**这条网看的是界面上真能选到什么。**
 *
 * ## 顺带守的那条:重置不能把"显示"和"数据"拆成两套
 *
 *   旧实现(Tab)的 `activeKey` 是独立的 useState,而 `useTable.reset()` 只改
 *   `query` 不改那个 state → 切到"我发起的"再点"重置",表格刷的是
 *   `box=todo` 的数据,Tab 却还高亮着"我发起的"。第 4 条用例就是这个 bug
 *   的回归网。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import ApprovalList from '../index'
import { approvalApi } from '@/api/approval'
import { findJsxTags, stripComments } from '@/test/sourceScan'

vi.mock('@/api/approval', () => ({
  approvalApi: { list: vi.fn() },
}))

const listMock = vi.mocked(approvalApi.list)

/** 列表行(后端 ApprovalListItem 的形状) */
const ROW = {
  id: 'a1',
  biz_type: 'digital_material',
  biz_id: 'm1',
  title: '资料审批',
  status: 'running' as const,
  current_stage: 0,
  current_stage_name: '部门审批',
  applicant_user_id: 'u2',
  applicant_name: '李四',
  return_count: 0,
  my_task_status: 'pending' as const,
  template_code: 'T1',
  template_name: '资料模板',
  created_at: '2026-10-01T00:00:00+08:00',
  updated_at: '2026-10-01T00:00:00+08:00',
  submitted_at: '2026-10-01T00:00:00+08:00',
  finished_at: null,
}

/**
 * waitFor 的超时 —— **必须显式给,不能用默认的 1000ms**
 *
 * ⚠️ 实测过:第 1 条用例正常跑就要 1.1~1.4s(冷启动还要 transform + collect),
 *   而 testing-library 的 `waitFor` 默认只等 **1000ms**。机器一忙(并行跑别的
 *   vitest / 编译 / 打包)就集体超时 → **5 条全红,而且是偶发的**。
 *   一次后台跑就这样假红过一轮(同一份代码连跑三次又全绿)。
 *   **测试的成败不能取决于机器负载** —— 所以这里统一给 10s。
 */
const WAIT = { timeout: 10_000 }

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <ApprovalList />
      </MemoryRouter>
    </App>,
  )
}

/**
 * 选盒子的那个 Select 控件
 *
 * ⚠️ **必须排除表格分页器里的那个** —— antd Table 的分页器自带一个
 *   "20 / page" 的页大小选择器,DOM 顺序在表格里(搜索栏之后)。
 *   按"第一个 .ant-select"取,一旦 DOM 顺序变了就会抓到它,
 *   断言会变成一句废话(探针实测踩到过:旧实现里抓到的是 "10 / page")。
 */
function boxSelectEl(): HTMLElement {
  const sels = Array.from(document.querySelectorAll('.ant-select-selector')).filter(
    (el) => !el.closest('.ant-pagination'),
  )
  if (sels.length !== 1) {
    throw new Error(`页面上应该有且只有一个(非分页器的)Select,实际 ${sels.length} 个`)
  }
  return sels[0] as HTMLElement
}

/** 当前选中的盒子(antd 把选中项渲染成 .ant-select-selection-item) */
function selectedBox(): string {
  const el = boxSelectEl().querySelector('.ant-select-selection-item')
  return (el?.getAttribute('title') ?? el?.textContent ?? '').trim()
}

/**
 * 打开盒子下拉,返回**界面上真能选到的**所有选项文案
 *
 * ⚠️ antd 5 的下拉是**懒渲染**的 —— 不打开,一个选项都不在 DOM 里。
 */
async function openBoxSelect(): Promise<string[]> {
  fireEvent.mouseDown(boxSelectEl())
  await waitFor(() => expect(document.querySelector('.ant-select-dropdown')).toBeTruthy(), WAIT)
  return Array.from(
    document.querySelectorAll('.ant-select-dropdown .ant-select-item-option-content'),
  ).map((el) => (el.textContent ?? '').trim())
}

/** 点某个选项(在 dropdown 里精确找,避开页面正文里的同名文字) */
async function pickBox(label: string) {
  const option = Array.from(
    document.querySelectorAll('.ant-select-dropdown .ant-select-item-option'),
  ).find((el) => (el.textContent ?? '').trim() === label)
  if (!option) throw new Error(`下拉里找不到选项:${label}`)
  fireEvent.click(option)
}

/** 点搜索栏的"重置" */
function clickReset() {
  fireEvent.click(screen.getByText('重置'))
}

/**
 * 页面源码里那个选盒子的 Select
 *
 * ⚠️ **要去注释再扫** —— 否则文件头那段注释里写着 `allowClear={false}`,
 *   扫出来的是注释里的那一份,断言就成了一句废话。
 */
function boxSelectTag() {
  const src = stripComments(readFileSync(join(__dirname, '..', 'index.tsx'), 'utf8'))
  const selects = findJsxTags(src).filter((t) => t.name === 'Select')
  expect(selects.length).toBe(1)
  return selects[0]!
}

/** 某个属性的原始值(形如 `{false}` / `{query.box}`) */
function propOf(tag: { props: { name: string; value: string }[] }, name: string): string | undefined {
  return tag.props.find((p) => p.name === name)?.value
}

describe('我的审批页 · 三个盒子', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ list: [ROW], total: 1, page: 1, size: 20 } as never)
  })

  it('下拉里只有三个盒子,没有"全部"(§8.2 越权红线)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled(), WAIT)

    const options = await openBoxSelect()
    expect(options).toEqual(['待我审批', '我发起的', '我已办理'])
    // 显式钉一次:多一个"全部"就是全平台审批单裸奔
    expect(options).not.toContain('全部')
  })

  it('首屏拉的是"待我审批"这一盒(box=todo)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled(), WAIT)

    expect(listMock.mock.calls[0]![0]).toMatchObject({ box: 'todo' })
    expect(selectedBox()).toBe('待我审批')
  })

  it('切到"我发起的" → 重新拉列表,且带上 box=mine', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled(), WAIT)

    await openBoxSelect()
    await pickBox('我发起的')

    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(2), WAIT)
    expect(listMock.mock.calls[1]![0]).toMatchObject({ box: 'mine' })
    expect(selectedBox()).toBe('我发起的')
  })

  it('点"重置" → 回到"待我审批",下拉显示也跟着回来(显示和数据不能是两套)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled(), WAIT)

    await openBoxSelect()
    await pickBox('我发起的')
    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(2), WAIT)

    clickReset()

    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(3), WAIT)
    expect(listMock.mock.calls[2]![0]).toMatchObject({ box: 'todo' })
    // ⚠️ **这条是旧实现会红的** —— Tab 的 activeKey 是独立 state,
    //   reset 只改 query,于是"显示停在'我发起的'、数据却是 todo 的"。
    expect(selectedBox()).toBe('待我审批')
  })

  /**
   * 两条**源码级**的断言 —— 有些契约 DOM 表达不出来
   *
   * · `allowClear={false}` —— 必选。DOM 里看不出来:清空按钮要 hover 才渲染,
   *   而一旦被清空,`box` 就是 undefined,后端直接 400。
   * · `value={query.box}` —— 值只有一个来源(query)。DOM 里也看不出来:
   *   另开一个 useState 存选中项,界面一样正常,只是"重置"之后会和数据脱节。
   */
  it('选框是必选的(allowClear={false}),且值只来自 query.box', () => {
    const tag = boxSelectTag()
    expect(propOf(tag, 'allowClear')).toBe('{false}')
    expect(propOf(tag, 'value')).toBe('{query.box}')
  })
  // ⚠️ 超时统一由 vitest.config.ts 的 testTimeout 管(antd 页面级用例单条要 10~45s)
})
