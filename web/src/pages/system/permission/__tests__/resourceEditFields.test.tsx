/**
 * 资源编辑弹窗 · 三处修正(用户 2026-10-30 报)
 *
 *   1. 「图标」是让人**手填 key** 的文本框 → 改成下拉选择(带真实图标)
 *   2. 「HTTP 方法」没有「全部」 → 补上,并且说清"填不填方法"到底有没有区别
 *   3. 「父资源」能跨类型选 → 按类型约束(菜单只能挂菜单下)
 *
 * ## 为什么这三件事值得单独一网
 *
 * 它们都是**"界面上看着对、实际是错的"**那一类:
 *   · 图标那个框,用户不打开 seed 就永远不知道该填什么 —— 填错了不报错,
 *     只是侧边栏的图标**悄悄不显示**(resolveResourceIcon 返回 null);
 *   · HTTP 方法那一列压根**不参与鉴权**(RBAC 只认 `module:action` 编码),
 *     用户却在为一个不影响权限的字段纠结"是不是要填";
 *   · 父资源跨类型挂,菜单挂到按钮下面 ⇒ 这个菜单**在侧边栏里永远不出现**,
 *     而且界面上一点提示都没有。
 *
 * 三条里没有一条会报错,三条都是"静默地不对" ⇒ 只能靠断言守住。
 *
 * ## ⚠️ 假绿陷阱(本文件特意绕开的)
 *
 * antd 的下拉是**懒渲染**的(不打开,一个选项都不在 DOM 里),
 * 关闭之后**popup 仍然留在 DOM 里**(只是隐藏)⇒
 * `document.querySelectorAll('.ant-select-item-option-content')` 会把
 * **之前打开过的下拉的选项一起算进来**。
 * 所以本文件的选项查询一律**限定在"当前可见的那个 dropdown"里**。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import PermissionList from '../index'
import { resourceApi } from '@/api/role'
import { usePermissionStore } from '@/stores/permission'
import type { Resource } from '@/types/permission'

vi.mock('@/api/role', () => ({
  resourceApi: {
    tree: vi.fn(),
    list: vi.fn(),
    detail: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
  },
}))

/** 字典 hook 打桩 —— 真实实现会去后端拉 sys_dict */
vi.mock('@/hooks/useDict', () => ({
  useDict: () => ({
    options: [],
    raw: [],
    loading: false,
    getLabel: (v: string | number) => String(v),
    getColor: () => 'default',
  }),
}))

const treeMock = vi.mocked(resourceApi.tree)
const createMock = vi.mocked(resourceApi.create)
const updateMock = vi.mocked(resourceApi.update)

/**
 * 一棵混类型的资源树
 *
 *   甲菜单(m1)─乙菜单(m2)─丙菜单(m3)      ← 菜单三级
 *   甲菜单(m1)─丁按钮(b1)                  ← 按钮
 *  甲菜单(m1)─戊接口(a1)                   ← 接口
 *
 * ⚠️ **b1 / a1 挂在 m1 下面,不是顶级** —— 但页面的候选树是**扁平的**
 *   (`parentCandidates.map(...)` 没有 children)⇒ 所有候选都是顶层节点、
 *   默认全部可见。所以"下拉里看不到 b1"这个断言,只能是**类型过滤**造成的,
 *   不会是"折叠着看不见"造成的假绿。
 */
const MIXED = [
  { id: 'm1', code: 'menu.a', name: '甲菜单', type: 'menu' as const, parent_id: '0', sort: 1, method: 'GET', icon: 'system' },
  { id: 'm2', code: 'menu.b', name: '乙菜单', type: 'menu' as const, parent_id: 'm1', sort: 2, method: '', icon: '' },
  { id: 'm3', code: 'menu.c', name: '丙菜单', type: 'menu' as const, parent_id: 'm2', sort: 3, method: 'POST', icon: 'job' },
  { id: 'b1', code: 'btn.a', name: '丁按钮', type: 'button' as const, parent_id: 'm1', sort: 4, method: 'GET', icon: 'user' },
  { id: 'a1', code: 'api.a', name: '戊接口', type: 'api' as const, parent_id: 'm1', sort: 5, method: 'GET', icon: '' },
]

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <PermissionList />
      </MemoryRouter>
    </App>,
  )
}

/** 弹窗本体 */
function modal(): HTMLElement {
  const el = document.querySelector('.ant-modal-content') as HTMLElement
  if (!el) throw new Error('编辑弹窗还没打开')
  return el
}

/**
 * 某个表单项(Form.Item)
 *
 * ⚠️ `getByText` 走"直接文本节点"匹配 —— `<label>图标</label>` 命中,
 *   `<div class="ant-form-item-extra">仅菜单使用;图标会…</div>` 不命中
 *   (整段文字 ≠ '图标')。
 */
function formItem(labelText: string): HTMLElement {
  const label = within(modal()).getByText(labelText)
  const item = label.closest('.ant-form-item') as HTMLElement
  if (!item) throw new Error(`找不到「${labelText}」这一项`)
  return item
}

/** 表格里某个资源名所在的那一行 */
function rowOf(name: string): HTMLElement {
  const tr = screen.getByText(name).closest('tr') as HTMLElement
  if (!tr) throw new Error(`找不到「${name}」所在的那一行`)
  return tr
}

/**
 * **当前可见**的下拉面板
 *
 * ⚠️ antd 关闭下拉后 popup **仍留在 DOM 里**(只是隐藏)⇒
 *   直接 `document.querySelector('.ant-select-dropdown')` 拿到的是
 *   **最早打开过的那个**,选项会串。
 *
 * 判定"可见":既没有 `ant-select-dropdown-hidden` 类,行内样式也不是 none。
 */
function visibleDropdown(): HTMLElement | null {
  const list = Array.from(document.querySelectorAll('.ant-select-dropdown')) as HTMLElement[]
  const visible = list.filter((el) => {
    if (el.classList.contains('ant-select-dropdown-hidden')) return false
    if (el.style.display === 'none' || el.style.visibility === 'hidden') return false
    return true
  })
  return visible[visible.length - 1] ?? null
}

/**
 * 打开某个表单项里的下拉,返回**那个下拉面板元素**
 *
 * ⚠️ antd 5 的下拉是懒渲染的 —— 不打开,一个选项都不在 DOM 里。
 */
async function openSelectIn(labelText: string): Promise<HTMLElement> {
  const selector = formItem(labelText).querySelector('.ant-select-selector') as HTMLElement
  expect(selector).toBeTruthy()
  fireEvent.mouseDown(selector)
  await waitFor(() => expect(visibleDropdown()).toBeTruthy())
  return visibleDropdown()!
}

/** 某个下拉面板里的选项文案 */
function optionTexts(dd: HTMLElement): string[] {
  return Array.from(dd.querySelectorAll('.ant-select-item-option-content')).map((el) =>
    (el.textContent ?? '').trim(),
  )
}

/** 点某个下拉面板里的选项 */
function pickOption(dd: HTMLElement, label: string) {
  const option = Array.from(dd.querySelectorAll('.ant-select-item-option')).find(
    (el) => (el.textContent ?? '').trim() === label,
  )
  if (!option) throw new Error(`下拉里找不到选项:${label}`)
  fireEvent.click(option)
}

/** 下拉当前选中的那一项(显示的文案) */
function selectedIn(labelText: string): string {
  const el = formItem(labelText).querySelector('.ant-select-selection-item')
  return (el?.getAttribute('title') ?? el?.textContent ?? '').trim()
}

/**
 * 打开"父资源"下拉,返回下拉里真能选到的所有节点文案
 *
 * ⚠️ TreeSelect 的下拉是**树** —— 节点标题在 `.ant-select-tree-title` 里
 *   (不是 `.ant-select-item-option-content`)。
 */
async function openParentSelect(): Promise<string[]> {
  const selector = formItem('父资源').querySelector('.ant-select-selector') as HTMLElement
  expect(selector).toBeTruthy()
  fireEvent.mouseDown(selector)
  await waitFor(() => expect(document.querySelector('.ant-select-tree')).toBeTruthy())
  return Array.from(document.querySelectorAll('.ant-select-tree-title')).map((el) =>
    (el.textContent ?? '').trim(),
  )
}

/**
 * 弹窗的"保存"按钮
 *
 * ⚠️ EditModal 的 okText = t('common.save') = "保存" —— antd Button 只给
 *   "正好两个汉字"的按钮中间插空格 ⇒ 无障碍名是 "保 存"。
 */
function saveButton(): HTMLElement {
  return screen.getByRole('button', { name: /保\s*存/ })
}

/** 某个表单项里的输入框(按 label 文字定位,不依赖 antd 生成的 id) */
function inputOf(labelText: string): HTMLInputElement {
  const input = formItem(labelText).querySelector('input') as HTMLInputElement
  if (!input) throw new Error(`找不到「${labelText}」这一项的输入框`)
  return input
}

/**
 * 最后一次 update 调用提交出去的载荷
 *
 * ⚠️ `mock.calls[0]` 在 TS 里是 `T | undefined`(noUncheckedIndexedAccess),
 *   直接写 `calls[0][1].method` 编译不过 —— 用这个 helper 收口。
 */
function lastUpdatePayload(): Partial<Resource> {
  const calls = updateMock.mock.calls
  return calls[calls.length - 1]?.[1] ?? {}
}

/** 打开某行的编辑弹窗 */
async function openEdit(rowName: string) {
  renderPage()
  await waitFor(() => expect(screen.getByText(rowName)).toBeInTheDocument())
  // ⚠️ 每一行都有"编辑"按钮 ⇒ 必须限定在这一行里找
  fireEvent.click(within(rowOf(rowName)).getByRole('button', { name: '编辑' }))
  await waitFor(() => expect(screen.getByText('父资源')).toBeInTheDocument())
}

describe('资源编辑弹窗 · 图标改成下拉选择', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    treeMock.mockResolvedValue(MIXED as never)
    createMock.mockResolvedValue(undefined as never)
    updateMock.mockResolvedValue(undefined as never)
    usePermissionStore.setState({
      permissionCodes: new Set([
        'button.resource.add',
        'button.resource.edit',
        'button.resource.delete',
        'button.resource.add-child',
      ]),
      roleCodes: [],
    })
  })

  it('「图标」是下拉框,不是让人手填 key 的文本框', async () => {
    await openEdit('甲菜单')
    const item = formItem('图标')
    expect(item.querySelector('.ant-select')).toBeTruthy()
    // 以前这里是 <Input> —— 用户不知道该填 'system' 还是 'setting'
    expect(item.querySelector('input.ant-input')).toBeNull()
  })

  it('下拉里有「无图标」和词表里的图标', async () => {
    await openEdit('甲菜单')
    const dd = await openSelectIn('图标')
    const options = optionTexts(dd)
    expect(options[0]).toBe('无图标')
    expect(options).toContain('仪表盘')
  })

  it('选项按关键字过滤(词表里的每一项都能搜到)', async () => {
    await openEdit('甲菜单')
    const dd = await openSelectIn('图标')
    // 「定时任务」在词表里排第 9 —— 靠"打开下拉能看到全部"来断言会被
    // antd 的虚拟滚动坑掉,改成"搜得到"更稳,也更接近用户实际用法。
    fireEvent.change(inputOf('图标'), { target: { value: '定时任务' } })
    await waitFor(() => {
      const texts = optionTexts(visibleDropdown()!)
      expect(texts).toEqual(['定时任务'])
    })
    expect(optionTexts(dd).length).toBeGreaterThan(0)
  })

  it('「无图标」→ 提交空串(而不是 "__none__" 这个哨兵)', async () => {
    await openEdit('乙菜单') // icon 是空的
    expect(selectedIn('图标')).toBe('无图标')

    fireEvent.click(saveButton())
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    expect(lastUpdatePayload().icon).toBe('')
  })

  it('选中某个图标 → 提交的是那个 key', async () => {
    await openEdit('甲菜单') // icon = 'system'
    expect(selectedIn('图标')).toBe('系统')

    fireEvent.click(saveButton())
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    expect(lastUpdatePayload().icon).toBe('system')
  })
})

describe('资源编辑弹窗 · HTTP 方法加「全部」', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    treeMock.mockResolvedValue(MIXED as never)
    createMock.mockResolvedValue(undefined as never)
    updateMock.mockResolvedValue(undefined as never)
    usePermissionStore.setState({
      permissionCodes: new Set([
        'button.resource.add',
        'button.resource.edit',
        'button.resource.delete',
        'button.resource.add-child',
      ]),
      roleCodes: [],
    })
  })

  it('下拉第一项是「全部」,后面才是各个 HTTP 方法', async () => {
    await openEdit('甲菜单')
    const dd = await openSelectIn('HTTP 方法')
    expect(optionTexts(dd)).toEqual(['全部', 'GET', 'POST', 'PATCH', 'PUT', 'DELETE'])
  })

  it('新建时默认就是「全部」', async () => {
    renderPage()
    await waitFor(() => expect(treeMock).toHaveBeenCalled())
    // 工具栏"新增"按钮带 icon ⇒ 无障碍名是 "plus 新增"
    fireEvent.click(screen.getByRole('button', { name: /新\s*增/ }))
    await waitFor(() => expect(screen.getByText('父资源')).toBeInTheDocument())

    expect(selectedIn('HTTP 方法')).toBe('全部')
  })

  it('改回「全部」→ 提交 method 是空串(而不是 "ALL")', async () => {
    await openEdit('甲菜单') // method = 'GET'
    expect(selectedIn('HTTP 方法')).toBe('GET')

    const dd = await openSelectIn('HTTP 方法')
    pickOption(dd, '全部')

    fireEvent.click(saveButton())
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    // ⚠️ 必须是 '' —— 后端 ResourceUpdateInput.Method 是 *string,
    //   空串 = 显式清空这一列;发 "ALL" 会撞 validateMethod 被拒。
    expect(lastUpdatePayload().method).toBe('')
  })

  it('本来就没设 method 的资源,弹窗里显示「全部」,提交也是空串', async () => {
    await openEdit('乙菜单') // method = ''
    expect(selectedIn('HTTP 方法')).toBe('全部')

    fireEvent.click(saveButton())
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    expect(lastUpdatePayload().method).toBe('')
  })

  it('选了具体方法 → 提交的是那个方法', async () => {
    await openEdit('乙菜单')
    const dd = await openSelectIn('HTTP 方法')
    pickOption(dd, 'PATCH')

    fireEvent.click(saveButton())
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    expect(lastUpdatePayload().method).toBe('PATCH')
  })
})

describe('资源编辑弹窗 · 父资源按类型约束', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    treeMock.mockResolvedValue(MIXED as never)
    createMock.mockResolvedValue(undefined as never)
    updateMock.mockResolvedValue(undefined as never)
    usePermissionStore.setState({
      permissionCodes: new Set([
        'button.resource.add',
        'button.resource.edit',
        'button.resource.delete',
        'button.resource.add-child',
      ]),
      roleCodes: [],
    })
  })

  it('菜单的候选里没有按钮和接口', async () => {
    await openEdit('丙菜单') // 菜单
    const options = await openParentSelect()
    // 菜单候选 = 菜单里除了自己和自己的后代
    expect(options).toContain('甲菜单 (menu.a)')
    expect(options).toContain('— 乙菜单 (menu.b)')
    // ⚠️ 这两条是**类型过滤**挡掉的 —— b1/a1 不是丙菜单的后代,
    //   没有类型过滤的话它们就在候选里(候选树是扁平的,全都可见)。
    expect(options).not.toContain('— 丁按钮 (btn.a)')
    expect(options).not.toContain('— 戊接口 (api.a)')
  })

  // ⚠️ 2026-11-03 用户拍板:parent_id 只有菜单树一个读者,按钮/接口挂父级
  //    是死数据 ⇒ 编辑弹窗对 button/api **整个不渲染父级字段**(不只是候选过滤)。
  //    原来这两条用例断言"按钮/接口的父资源候选里有菜单",作废。
  //
  //    ⚠️ 断言要限定在弹窗里 —— 表格列头也叫"资源名称",screen.getByText 会
  //       同时命中两个。"弹窗开着"用弹窗内的字段锚,不能用裸 screen 查。
  it('编辑按钮时父资源字段整个不渲染', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('丁按钮')).toBeInTheDocument())
    fireEvent.click(within(rowOf('丁按钮')).getByRole('button', { name: '编辑' }))
    // 弹窗开了(其他字段在),但父资源字段不在
    const modal = await waitFor(() => {
      const el = document.querySelector('.ant-modal-content') as HTMLElement
      expect(el).toBeTruthy()
      expect(within(el).getByText('资源名称')).toBeTruthy()
      return el
    })
    expect(within(modal).queryByText('父资源')).toBeNull()
  })

  it('编辑接口时父资源字段整个不渲染', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('戊接口')).toBeInTheDocument())
    fireEvent.click(within(rowOf('戊接口')).getByRole('button', { name: '编辑' }))
    const modal = await waitFor(() => {
      const el = document.querySelector('.ant-modal-content') as HTMLElement
      expect(el).toBeTruthy()
      expect(within(el).getByText('资源名称')).toBeTruthy()
      return el
    })
    expect(within(modal).queryByText('父资源')).toBeNull()
  })

  it('编辑菜单时父资源字段还在', async () => {
    await openEdit('丙菜单')
    expect(screen.getByText('父资源')).toBeInTheDocument()
  })

  it('候选里排除自己和自己的后代', async () => {
    await openEdit('甲菜单')
    const options = await openParentSelect()
    expect(options).not.toContain('甲菜单 (menu.a)')
    expect(options).not.toContain('— 乙菜单 (menu.b)')
    expect(options).not.toContain('— — 丙菜单 (menu.c)')
    // ⚠️ 对照组:编辑丙菜单时,乙菜单**是**候选(上面那条用例已经断言过)
    //   ⇒ "编辑甲菜单时看不到乙菜单"确实是后代过滤造成的,
    //   不是"节点折叠着看不见"。
  })
})
