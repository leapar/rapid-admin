/**
 * 资源管理页 · 类型筛选(2026-10-26)
 *
 * ## 这条网守的是什么
 *
 *   类型筛选器原来是 <Segmented>(全部/菜单/按钮/API 四段),2026-10-26 换成
 *   Select。控件换了,底下那套过滤逻辑没变 —— 但**"筛完之后表格里到底还剩
 *   哪些行"是最容易悄悄坏掉的地方**:
 *   · 选中"按钮"时,菜单和 API 行必须真的消失(过滤按 type 走,不是按名字);
 *   · "重置"必须把类型**和**关键词一起清掉 —— 以前是 `setKeyword('')` +
 *     `setTypeTab('all')` 两件事,漏一件就会出现"关键词空了但列表还是旧的"。
 *
 * ## 为什么用 DOM 断言而不是看源码
 *
 *   选项是运行时渲染出来的 —— 源码里有几个 option 字面量,跟界面上真能选到
 *   几个是两回事。antd 的下拉还是**懒渲染**的:不打开,一个选项都不在 DOM 里。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import PermissionList from '../index'
import { resourceApi } from '@/api/role'
import { usePermissionStore } from '@/stores/permission'

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

/**
 * 字典 hook 打桩 —— 真实实现会去后端拉 sys_dict,
 * 在这个测试里只会打出一堆无意义的网络错误(而且 label 拿不到)。
 */
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

/** 资源行 —— 一条 menu / 一条 button / 一条 api */
const ROWS = [
  { id: 'r1', code: 'system:user', name: '用户管理', type: 'menu' as const, path: '/system/user', sort: 1 },
  { id: 'r2', code: 'system:user:create', name: '新建用户', type: 'button' as const, path: '/system/user', sort: 2 },
  { id: 'r3', code: 'GET:/system/users', name: '用户列表', type: 'api' as const, path: '/system/user', sort: 3 },
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

/** 当前下拉显示的选中项 */
function selectedType(): string {
  const el = document.querySelector('.ant-select-selection-item')
  return (el?.getAttribute('title') ?? el?.textContent ?? '').trim()
}

/**
 * 打开类型下拉,返回**界面上真能选到的**所有选项文案
 *
 * ⚠️ antd 5 的下拉是**懒渲染**的 —— 不打开,一个选项都不在 DOM 里。
 */
async function openTypeSelect(): Promise<string[]> {
  const selector = document.querySelector('.ant-select-selector') as HTMLElement
  expect(selector).toBeTruthy()
  fireEvent.mouseDown(selector)
  await waitFor(() => expect(document.querySelector('.ant-select-dropdown')).toBeTruthy())
  return Array.from(document.querySelectorAll('.ant-select-item-option-content')).map(
    (el) => (el.textContent ?? '').trim(),
  )
}

/** 点某个选项(在 dropdown 里精确找,避开表格正文里的同名文字) */
async function pickType(label: string) {
  const option = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
    (el) => (el.textContent ?? '').trim() === label,
  )
  if (!option) throw new Error(`下拉里找不到选项:${label}`)
  fireEvent.click(option)
}

/** 表格里当前有几行 */
function rowCount(): number {
  return document.querySelectorAll('.ant-table-tbody > tr.ant-table-row').length
}

/** 点搜索栏的"重置" */
function clickReset() {
  fireEvent.click(screen.getByText('重置'))
}

describe('资源管理页 · 类型筛选', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    treeMock.mockResolvedValue(ROWS as never)
  })

  it('下拉里正好四档:全部 / 菜单 / 按钮 / 接口', async () => {
    renderPage()
    await waitFor(() => expect(treeMock).toHaveBeenCalled())

    const options = await openTypeSelect()
    expect(options).toEqual(['全部', '菜单', '按钮', '接口'])
  })

  it('首屏是"全部",三行都在', async () => {
    renderPage()
    await waitFor(() => expect(treeMock).toHaveBeenCalled())

    expect(selectedType()).toBe('全部')
    await waitFor(() => expect(rowCount()).toBe(3))
  })

  it('选"按钮" → 表格只剩按钮那一行', async () => {
    renderPage()
    await waitFor(() => expect(treeMock).toHaveBeenCalled())
    await waitFor(() => expect(rowCount()).toBe(3))

    await openTypeSelect()
    await pickType('按钮')

    await waitFor(() => expect(rowCount()).toBe(1))
    expect(selectedType()).toBe('按钮')
    // 剩下的是"新建用户"那一条按钮,不是菜单/接口
    expect(document.querySelector('.ant-table-tbody')?.textContent).toContain('新建用户')
    expect(document.querySelector('.ant-table-tbody')?.textContent).not.toContain('用户管理')
  })

  it('点"重置" → 类型和关键词一起清掉,三行全回来', async () => {
    renderPage()
    await waitFor(() => expect(treeMock).toHaveBeenCalled())
    await waitFor(() => expect(rowCount()).toBe(3))

    await openTypeSelect()
    await pickType('按钮')
    await waitFor(() => expect(rowCount()).toBe(1))

    clickReset()

    await waitFor(() => expect(rowCount()).toBe(3))
    expect(selectedType()).toBe('全部')
  })
  // ⚠️ 超时统一由 vitest.config.ts 的 testTimeout 管(antd 页面级用例单条要 10~45s)
})

/**
 * 父资源字段(2026-10-29 用户报:"父资源是输入 id,应该下拉框选择,
 * 不然不知道父资源数值是多少")
 *
 * 三条硬要求:
 *  1. **必须是下拉框** —— EditModal 只在 `f.type === 'custom'` 时才认 f.render,
 *     否则会退化成普通输入框,让人手填 19 位 sonyflake ID。
 *  2. **顶级必须是字符串 '0'** —— 后端 SysResource.ParentID 是
 *     `uint64 \`json:"parent_id,string"\``;Go 的 ,string 选项只收**带引号的**数字,
 *     发裸 0 会被 "invalid use of ,string struct tag" 直接拒收(整个请求 400)。
 *  3. **候选项要排除自己和自己的后代** —— 否则能把父资源设成自己的儿子,
 *     父子成环;环上的节点都不是根,于是在 buildMenuTree 里**凭空消失**
 *     (菜单连同整个子树静默不见)。
 */
describe('资源管理页 · 父资源字段', () => {
  /** 带层级的资源:p2 是 p1 的子,p3 是 p2 的子 */
  const HIER = [
    { id: 'p1', code: 'menu.a', name: 'A', type: 'menu' as const, parent_id: '0', sort: 1 },
    { id: 'p2', code: 'menu.a.b', name: 'B', type: 'menu' as const, parent_id: 'p1', sort: 1 },
    { id: 'p3', code: 'menu.a.b.c', name: 'C', type: 'menu' as const, parent_id: 'p2', sort: 1 },
  ]

  beforeEach(() => {
    vi.clearAllMocks()
    treeMock.mockResolvedValue(HIER as never)
    createMock.mockResolvedValue(undefined as never)
    updateMock.mockResolvedValue(undefined as never)
    // 行内按钮按按钮级权限码过滤 —— 没有它就一个按钮都渲染不出来
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

  /**
   * 弹窗本体
   *
   * ⚠️ 必须限定在弹窗里找 —— 表格**列头**也叫"资源编码"/"资源名称",
   *   `screen.getByText('资源编码')` 会同时命中表头和表单 label。
   */
  function modal(): HTMLElement {
    const el = document.querySelector('.ant-modal-content') as HTMLElement
    if (!el) throw new Error('编辑弹窗还没打开')
    return el
  }

  /** "父资源"那一项(Form.Item) */
  function parentItem(): HTMLElement {
    const label = within(modal()).getByText('父资源')
    const item = label.closest('.ant-form-item') as HTMLElement
    if (!item) throw new Error('找不到"父资源"这一项')
    return item
  }

  /** 表格里某个资源名所在的那一行 */
  function rowOf(name: string): HTMLElement {
    const tr = screen.getByText(name).closest('tr') as HTMLElement
    if (!tr) throw new Error(`找不到「${name}」所在的那一行`)
    return tr
  }

  /**
   * 打开"父资源"下拉,返回下拉里**真能选到的**所有节点文案
   *
   * ⚠️ TreeSelect 的下拉是**树**,不是普通 Select 的选项列表 ——
   *   节点标题在 `.ant-select-tree-title` 里(不是 `.ant-select-item-option-content`)。
   *   antd 的下拉懒渲染:不打开,一个节点都不在 DOM 里。
   */
  async function openParentSelect(): Promise<string[]> {
    const selector = parentItem().querySelector('.ant-select-selector') as HTMLElement
    expect(selector).toBeTruthy()
    fireEvent.mouseDown(selector)
    await waitFor(() => expect(document.querySelector('.ant-select-tree')).toBeTruthy())
    return Array.from(document.querySelectorAll('.ant-select-tree-title')).map(
      (el) => (el.textContent ?? '').trim(),
    )
  }

  /**
   * 弹窗的"保存"按钮
   *
   * ⚠️ EditModal 的 okText = t('common.save') = "保存",**不是**"确定"。
   *   antd Button 只给"正好两个汉字"的按钮中间插空格 ⇒ 无障碍名是 "保 存"。
   */
  function saveButton(): HTMLElement {
    return screen.getByRole('button', { name: /保\s*存/ })
  }

  /** 某个表单项里的输入框(按 label 文字定位,不依赖 antd 生成的 id) */
  function inputOf(labelText: string): HTMLInputElement {
    const item = within(modal()).getByText(labelText).closest('.ant-form-item') as HTMLElement
    const input = item?.querySelector('input') as HTMLInputElement
    if (!input) throw new Error(`找不到「${labelText}」这一项的输入框`)
    return input
  }

  /** 填必填项(资源编码 + 资源名称)—— 不填的话校验不过,提交根本不会发生 */
  function fillRequired() {
    fireEvent.change(inputOf('资源编码'), { target: { value: 'menu.new' } })
    fireEvent.change(inputOf('资源名称'), { target: { value: '新资源' } })
  }

  /** 打开某行的编辑弹窗 */
  async function openEdit(rowName: string) {
    renderPage()
    await waitFor(() => expect(screen.getByText(rowName)).toBeInTheDocument())
    // ⚠️ 每一行都有"编辑"按钮 ⇒ 必须限定在这一行里找(否则 getByRole 报多个)
    fireEvent.click(within(rowOf(rowName)).getByRole('button', { name: '编辑' }))
    await waitFor(() => expect(screen.getByText('父资源')).toBeInTheDocument())
  }

  it('"父资源"是下拉框,不是让人手填 ID 的输入框', async () => {
    await openEdit('A')
    const item = parentItem()
    expect(item.querySelector('.ant-select')).toBeTruthy()
    // 以前这里是 <Input>(render 没写 type:'custom' → EditModal 落进 default 分支)
    expect(item.querySelector('input.ant-input')).toBeNull()
  })

  it('下拉里有"顶级"选项,选中后提交给后端的 parent_id 是字符串 "0"', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('A')).toBeInTheDocument())
    // 工具栏"新增"按钮带 icon ⇒ 无障碍名是 "plus 新增",用正则匹配
    fireEvent.click(screen.getByRole('button', { name: /新\s*增/ }))
    await waitFor(() => expect(screen.getByText('父资源')).toBeInTheDocument())

    // 打开父资源下拉,里面得有"顶级"
    const options = await openParentSelect()
    expect(options).toContain('— 顶级 —')

    // 直接提交(默认值就是顶级)
    fillRequired()
    fireEvent.click(saveButton())
    await waitFor(() => expect(createMock).toHaveBeenCalled())
    const call = createMock.mock.calls[0]
    expect(call).toBeDefined()
    // createMock 的入参类型就是 Partial<Resource>(parent_id?: string)
    const payload = call![0]
    expect(payload.parent_id).toBe('0')
    // ⚠️ 必须是字符串 —— 后端字段是 uint64 + `json:",string"`
    expect(typeof payload.parent_id).toBe('string')
  })

  it('编辑已有资源时,parent_id 原样回填(还是字符串)', async () => {
    await openEdit('B')
    // B 的父资源是 p1,下拉里应显示 p1 的名字(不是一串裸 ID)
    const item = parentItem()
    expect(item.querySelector('.ant-select-selection-item')?.textContent).toContain('A (menu.a)')

    fireEvent.click(saveButton())
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    const call = updateMock.mock.calls[0]
    expect(call).toBeDefined()
    const payload = call![1]
    expect(payload.parent_id).toBe('p1')
    expect(typeof payload.parent_id).toBe('string')
  })

  it('父资源下拉排除自己和自己的后代(避免父子成环)', async () => {
    await openEdit('A')
    const options = await openParentSelect()
    // 顶级还在
    expect(options).toContain('— 顶级 —')
    // 自己不行
    expect(options).not.toContain('A (menu.a)')
    // 后代也不行(B 是 A 的儿子,C 是 A 的孙子)
    expect(options).not.toContain('B (menu.a.b)')
    expect(options).not.toContain('C (menu.a.b.c)')
  })
})
