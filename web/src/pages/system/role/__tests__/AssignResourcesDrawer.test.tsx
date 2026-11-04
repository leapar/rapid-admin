/**
 * 分配权限 Drawer 渲染测试 —— 「菜单勾不上 / 看着全勾上了」那个 Bug 的回归网
 *
 * 用户报的现象(2026-10-25):
 *   打开角色 → 分配权限,菜单一栏 44 条**全部显示成勾选态**,但点不动。
 * 逐条钉住根因:
 *   1. 菜单节点 checkable:false → 菜单行**没有复选框**
 *      (本文件:菜单行必须有 checkbox)
 *   2. 菜单 key 用 `m-${id}` 而不是真资源 ID → 已绑定的菜单**显示不出勾选态**
 *      (本文件:已绑菜单的 checkbox 必须是 checked)
 *   3. 菜单标题画了个蓝色 <CheckCircleOutlined/> → 长得跟勾选框一样,
 *      于是"没勾"看着像"勾了"(本文件:标题里不能再有 check-circle 图标)
 *   4. 分组节点没展开 → 组里的资源一条都不渲染
 *      (本文件:顶级 4 个资源行(2 菜单 + 1 按钮 + 1 API)的复选框必须在)
 *   5. 受控 checkedKeys 没按 rc-tree 的级联规则归一 → "已选 N"与树上对勾对不上
 *      (本文件:绑父菜单 m1 → 子菜单 m2 也算勾上,"已选 2 / 5")
 *
 * 另外钉住:后端返的是**扁平列表**,菜单层级必须由前端按 parent_id 建出来
 * (子菜单要挂在父菜单下,而不是平铺)。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { AssignResourcesDrawer } from '../AssignResourcesDrawer'
import { resourceApi, roleApi } from '@/api/role'
import type { Resource } from '@/types/permission'

vi.mock('@/api/role', () => ({
  resourceApi: { tree: vi.fn() },
  roleApi: { getResources: vi.fn(), assignResources: vi.fn() },
}))

// 字典 hook 会去打后端,测试里换成固定实现(标签原样回显)
vi.mock('@/hooks/useDict', () => ({
  useDict: () => ({
    options: [],
    getLabel: (v: unknown) => String(v),
    getColor: () => 'default' as const,
    loading: false,
    raw: [],
  }),
}))

const treeMock = vi.mocked(resourceApi.tree)
const getResourcesMock = vi.mocked(roleApi.getResources)
const assignMock = vi.mocked(roleApi.assignResources)

function res(partial: Partial<Resource> & { id: string; code: string }): Resource {
  return { name: partial.code, type: 'menu', sort: 0, ...partial } as Resource
}

/** 扁平列表(后端真实返回的样子):2 个顶级菜单(1 个带子菜单)+ 1 按钮 + 1 API */
const FLAT: Resource[] = [
  res({ id: 'm1', code: 'menu.system', name: '系统管理', path: '/system', sort: 1 }),
  res({ id: 'm2', code: 'menu.system.user', name: '用户管理', path: '/system/user', parent_id: 'm1', sort: 1 }),
  res({ id: 'm3', code: 'menu.dashboard', name: '工作台', path: '/', sort: 0 }),
  res({ id: 'b1', code: 'button.user.add', name: '新增', type: 'button' }),
  res({ id: 'a1', code: 'user:list', name: '用户列表', type: 'api' }),
]

const ROLE = { id: 'r1', code: 'ces', name: 'ces' } as never

function renderDrawer() {
  return render(
    <App>
      <AssignResourcesDrawer open role={ROLE} onClose={() => undefined} />
    </App>,
  )
}

/** 只读模式(内置/超管角色"查看权限") */
function renderReadOnlyDrawer() {
  return render(
    <App>
      <AssignResourcesDrawer open role={ROLE} readOnly onClose={() => undefined} />
    </App>,
  )
}

/**
 * 某个树节点那一行的复选框(按标题文字定位)
 *
 * ⚠️ rc-tree 5.13 的复选框**不是** <input type="checkbox">,而是
 *   <span class="ant-tree-checkbox" role="checkbox" aria-checked="true|false|mixed">,
 *   勾选状态看 aria-checked,点击看 onClick。
 */
function checkboxOf(title: string): HTMLElement {
  const node = screen.getByText(title).closest('.ant-tree-treenode') as HTMLElement
  const box = node?.querySelector('.ant-tree-checkbox') as HTMLElement
  if (!box) throw new Error(`没有找到「${title}」这一行的复选框`)
  return box
}

/** 某一行是不是勾上了 */
function isChecked(title: string): boolean {
  return checkboxOf(title).getAttribute('aria-checked') === 'true'
}

/** 展开某个节点(点它的展开开关) */
function expand(title: string) {
  const node = screen.getByText(title).closest('.ant-tree-treenode') as HTMLElement
  const switcher = node.querySelector('.ant-tree-switcher') as HTMLElement
  fireEvent.click(switcher)
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
  return Array.from(document.querySelectorAll('.ant-select-item-option-content')).map((el) =>
    (el.textContent ?? '').trim(),
  )
}

/** 点某个选项(在 dropdown 里精确找,避开树里的同名文字) */
async function pickType(label: string) {
  const option = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
    (el) => (el.textContent ?? '').trim() === label,
  )
  if (!option) throw new Error(`下拉里找不到选项:${label}`)
  fireEvent.click(option)
}

describe('AssignResourcesDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    treeMock.mockResolvedValue(FLAT as never)
    getResourcesMock.mockResolvedValue({ resource_ids: [] } as never)
    assignMock.mockResolvedValue(undefined as never)
  })

  it('菜单行有复选框(以前 checkable:false,菜单根本勾不上)', async () => {
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    // 顶级可见行:系统管理 / 工作台 / 按钮 / API = 4 个
    // (子菜单在折叠的父节点里,不渲染;三个分组标题不可勾,没有复选框)
    expect(screen.getAllByRole('checkbox')).toHaveLength(4)
  })

  it('已绑定的菜单显示为勾选态(key 必须是真资源 ID,以前是 m-${id} 对不上)', async () => {
    getResourcesMock.mockResolvedValue({ resource_ids: ['m3'] } as never)
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    expect(isChecked('工作台')).toBe(true)
    expect(isChecked('系统管理')).toBe(false)
    // 统计标签同步:已选 1 / 5
    expect(screen.getByText(/已选 1 \/ 5/)).toBeInTheDocument()
  })

  it('绑了父菜单时子菜单也级联勾上(受控 checkedKeys 要按 rc-tree 规则归一)', async () => {
    // 服务端只绑了父菜单 m1。rc-tree 收到后会自己级联(子 m2 算勾上),
    // 但不会回调 onCheck → 不归一的话「已选」是 1、树上却是两个对勾。
    getResourcesMock.mockResolvedValue({ resource_ids: ['m1'] } as never)
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    expect(isChecked('系统管理')).toBe(true)
    expect(screen.getByText(/已选 2 \/ 5/)).toBeInTheDocument()
    // 展开父节点后,子菜单也是勾上的
    expand('系统管理')
    await waitFor(() => expect(screen.getByText('用户管理')).toBeInTheDocument())
    expect(isChecked('用户管理')).toBe(true)
  })

  it('点菜单的复选框能选上(以前点了没反应)', async () => {
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    fireEvent.click(checkboxOf('工作台'))
    await waitFor(() => expect(screen.getByText(/已选 1 \/ 5/)).toBeInTheDocument())
    expect(isChecked('工作台')).toBe(true)
  })

  it('菜单标题里不再有蓝色对勾图标(那玩意儿看着像"默认勾选")', async () => {
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    expect(screen.queryByLabelText('check-circle')).toBeNull()
    expect(screen.getAllByLabelText('folder').length).toBeGreaterThan(0)
  })

  it('子菜单挂在父菜单下(后端返的是扁平列表,层级要前端建)', async () => {
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    // 父节点默认折叠 → 子菜单"用户管理"不在 DOM 里
    expect(screen.queryByText('用户管理')).toBeNull()
    // 展开父节点后子菜单出现
    expand('系统管理')
    await waitFor(() => expect(screen.getByText('用户管理')).toBeInTheDocument())
  })

  it('保存时提交的是真资源 ID(不含分组哨兵 key)', async () => {
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    // 本视图全选(all)= 5 个资源
    fireEvent.click(screen.getByText('本视图全选'))
    await waitFor(() => expect(screen.getByText(/已选 5 \/ 5/)).toBeInTheDocument())
    // 保存按钮的无障碍名 = 图标的 aria-label("save") + 文字("保存") → 用正则匹配
    fireEvent.click(screen.getByRole('button', { name: /保存/ }))
    await waitFor(() => expect(assignMock).toHaveBeenCalled())
    const call = assignMock.mock.calls[0]
    expect(call).toBeDefined()
    const ids = call![1]
    expect(ids).toEqual(['m1', 'm2', 'm3', 'b1', 'a1'])
    expect(ids.every((i: string) => !i.startsWith('__group_'))).toBe(true)
  })

  // ===== 类型切换:Segmented → Select(2026-10-29 用户报) =====
  it('类型切换是 Select,不是 Segmented(用户报:"应该换成 select 组件")', async () => {
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    // antd Segmented 的根 class 就是 .ant-segmented
    expect(document.querySelector('.ant-segmented')).toBeNull()
    expect(document.querySelector('.ant-select-selector')).toBeTruthy()
  })

  it('下拉里正好四档:全部 / 菜单 / 按钮 / 接口', async () => {
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    const options = await openTypeSelect()
    // ⚠️ "接口"而不是 "API" —— 写死的 'API' 已换成 t('resourceType.api')
    expect(options).toEqual(['全部', '菜单', '按钮', '接口'])
  })

  it('选"按钮" → 树上只剩按钮那一条(筛选按 type 走,不是按名字)', async () => {
    renderDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())
    // 全量视图:2 菜单 + 1 按钮 + 1 API = 4 个复选框
    expect(screen.getAllByRole('checkbox')).toHaveLength(4)

    await openTypeSelect()
    await pickType('按钮')

    await waitFor(() => expect(screen.getAllByRole('checkbox')).toHaveLength(1))
    // 只剩按钮那一条("新增"),两个菜单都退场了
    expect(screen.getByText('新增')).toBeInTheDocument()
    expect(screen.queryByText('系统管理')).toBeNull()
    expect(screen.queryByText('工作台')).toBeNull()
  })

  // ===== 只读模式(readOnly)=====
  //
  // 这条链是"页面只读标志 → 抽屉 → 树的 disabled"的**后半截**;
  // 前半截(页面关抽屉时不能把角色清掉)在 role/__tests__/index.test.tsx 里。
  // 两边合起来才是"关闭那一刻不会闪成可编辑"。

  it('readOnly 时树是禁用的,底部只有关闭按钮(没有保存/全选/清空)', async () => {
    renderReadOnlyDrawer()
    await waitFor(() => expect(screen.getByText('系统管理')).toBeInTheDocument())

    // ⚠️ rc-tree 的 disabled 表现是 class `ant-tree-checkbox-disabled`
    //   (rc-tree/lib/TreeNode.js:240),不是 <input disabled>
    const box = checkboxOf('系统管理')
    expect(box.classList.contains('ant-tree-checkbox-disabled')).toBe(true)

    // 底部:没有保存、没有全选/清空,只有关闭
    expect(screen.queryByRole('button', { name: /保\s*存/ })).toBeNull()
    expect(screen.queryByText('本视图全选')).toBeNull()
    expect(screen.queryByText('本视图清空')).toBeNull()
    expect(screen.getByRole('button', { name: /关\s*闭/ })).toBeInTheDocument()
  })
})
