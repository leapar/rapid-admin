/**
 * 部门编辑弹窗 · 「负责人 / 父部门 为空时显示 0」(用户 2026-10-30 报)
 *
 * ## 用户报的现象
 *
 *   "系统管理的部门管理里面,部门编辑弹窗,负责人为空时显示数值 0,父部门为空时也显示 0"
 *
 * ## 界面上的 0 是怎么来的
 *
 *   1. 后端把"没有负责人 / 顶级部门"表示成哨兵值 **"0"**
 *      (`model.SysDepartment.LeaderID/ParentID` 是 `uint64` + `json:",string"`);
 *   2. antd 的 Select / TreeSelect 选项里**没有** '0' ⇒
 *      antd 只能把它当成"找不到 label 的值"**原样渲染出来**;
 *   3. 页面作者想到了,在 render 里写了
 *      `value={value && value !== '0' ? value : undefined}` —— **但那是死代码**:
 *      rc-field-form 的 getControlled 是 `control = {...childProps, ...valueProps}`,
 *      **表单里的值会盖掉子组件自己写的 value prop**。
 *
 * ⇒ 修法:映射必须放在 Form.Item 的 `getValueProps` 上(EditModal 已透传),
 *   '0' → undefined ⇒ 控件显示占位符。
 *
 * ## 这张网还顺手钉住了"清空"这件事
 *
 * 界面上"显示 0"只是表象,**底下压着两个真的错值 bug**(后端):
 *   · `if dept.LeaderID > 0` ⇒ **清空负责人整个是死的**(改了没生效);
 *   · `if dept.ParentID > 0 || dept.ParentID == 0` 是**恒真式** ⇒ parent_id 永远被写
 *     ⇒ 只想改个名字的 PATCH 会把部门**静默搬到根**。
 * 后端那两条在 server/internal/service/role_department_test.go 里钉死,
 * 这边钉的是"前端提交上去的载荷对不对"。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import DepartmentList from '../index'
import { departmentApi } from '@/api/role'
import { userApi } from '@/api/user'
import { usePermissionStore } from '@/stores/permission'

vi.mock('@/api/role', () => ({
  departmentApi: {
    tree: vi.fn(),
    list: vi.fn(),
    detail: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
  },
}))

vi.mock('@/api/user', () => ({
  userApi: { list: vi.fn() },
}))

const treeMock = vi.mocked(departmentApi.tree)
const updateMock = vi.mocked(departmentApi.update)
const userListMock = vi.mocked(userApi.list)

/**
 * 一棵两层的部门树
 *
 * ⚠️ **总部没有负责人**(leader_id = '0'),**研发部有**(leader_id = 'u1') ——
 *   这两个正好把"该显示占位符"和"该显示人名"两种情况都覆盖了。
 */
const TREE = [
  {
    id: 'd1',
    code: 'dept.root',
    name: '总部',
    parent_id: '0',
    path: '',
    sort: 1,
    leader_id: '0',
    leader_name: '',
    member_count: 0,
    is_active: true,
    children: [
      {
        id: 'd2',
        code: 'dept.dev',
        name: '研发部',
        parent_id: 'd1',
        path: 'd1,',
        sort: 2,
        leader_id: 'u1',
        leader_name: '张三',
        member_count: 0,
        is_active: true,
      },
    ],
  },
]

const USERS = {
  list: [
    {
      id: 'u1',
      username: 'zhangsan',
      nickname: '张三',
      phone: '',
      email: '',
      avatar: '',
      department_id: 'd1',
      department_name: '总部',
      is_active: true,
    },
  ],
  total: 1,
}

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <DepartmentList />
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

/** 某个表单项(Form.Item) */
function formItem(labelText: string): HTMLElement {
  const label = within(modal()).getByText(labelText)
  const item = label.closest('.ant-form-item') as HTMLElement
  if (!item) throw new Error(`找不到「${labelText}」这一项`)
  return item
}

/**
 * 某个表单项里,控件当前显示的**选中项文案**(没选中就是空串)
 *
 * ⚠️ antd 的 Select / TreeSelect 选中时是 `.ant-select-selection-item`,
 *   没选中时那个元素**根本不存在**(只有 `.ant-select-selection-placeholder`)。
 *   所以"没选中"= 查不到 `.ant-select-selection-item`。
 */
function selectionText(labelText: string): string {
  const el = formItem(labelText).querySelector('.ant-select-selection-item')
  return (el?.getAttribute('title') ?? el?.textContent ?? '').trim()
}

/** 某个表单项里,控件当前的**占位符文案**(没占位符就是空串) */
function placeholderText(labelText: string): string {
  const el = formItem(labelText).querySelector('.ant-select-selection-placeholder')
  return (el?.textContent ?? '').trim()
}

/**
 * 右侧详情卡片头部的「编辑」按钮
 *
 * ⚠️ 页面上有**两个**「编辑」:卡片头部(改当前选中部门)和子部门列表行里的
 *   (改那个子部门)。两个都必须限定住,否则点错一个测试就变成测另一件事。
 *   卡片头部的那个在 `.ant-card-extra` 里。
 *
 * ⚠️ **无障碍名要用正则**:antd 的图标是个 `role="img"` + `aria-label="edit"`
 *   的 span ⇒ 按钮的无障碍名是 **"edit 编辑"**(图标名 + 文字),
 *   精确匹配 '编辑' 找不到。
 */
function headerEditButton(): HTMLElement {
  const extras = Array.from(document.querySelectorAll('.ant-card-extra')) as HTMLElement[]
  for (const ex of extras) {
    const btn = within(ex).queryByRole('button', { name: /编\s*辑/ })
    if (btn) return btn
  }
  throw new Error('找不到卡片头部的「编辑」按钮')
}

/** 弹窗的「保存」按钮 —— antd 给两个汉字的按钮中间插空格 ⇒ "保 存" */
function saveButton(): HTMLElement {
  return screen.getByRole('button', { name: /保\s*存/ })
}

/** 最后一次 update 提交出去的载荷 */
function lastUpdatePayload(): Record<string, unknown> {
  const calls = updateMock.mock.calls
  return (calls[calls.length - 1]?.[1] ?? {}) as Record<string, unknown>
}

/**
 * 左侧组织树里的某个节点
 *
 * ⚠️ **不能直接 `getByText('总部')`** —— "总部"在页面上出现多次
 *   (树节点 / 右侧详情的名称 / 下拉里的部门名)⇒ getByText 会因为
 *   "找到多个"而抛错。必须限定在树的标题里。
 */
function treeNode(name: string): HTMLElement {
  const els = Array.from(document.querySelectorAll('.ant-tree-title')) as HTMLElement[]
  const found = els.find((el) => (el.textContent ?? '').trim() === name)
  if (!found) throw new Error(`组织树里找不到「${name}」`)
  return found
}

/** 打开当前选中部门的编辑弹窗 */
async function openEditOfSelected() {
  await waitFor(() => expect(treeNode('总部')).toBeTruthy())
  fireEvent.click(headerEditButton())
  await waitFor(() => expect(modal()).toBeTruthy())
  await waitFor(() => expect(formItem('负责人')).toBeTruthy())
}

describe('部门编辑弹窗 · 负责人 / 父部门 为空时不显示 0', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    treeMock.mockResolvedValue(TREE as never)
    updateMock.mockResolvedValue(undefined as never)
    userListMock.mockResolvedValue(USERS as never)
    usePermissionStore.setState({
      permissionCodes: new Set([
        'button.dept.add',
        'button.dept.edit',
        'button.dept.delete',
      ]),
      roleCodes: [],
    })
  })

  it('没有负责人的部门 → 负责人那一栏显示占位符,不是 "0"', async () => {
    renderPage()
    await openEditOfSelected() // 总部:leader_id = '0'

    expect(selectionText('负责人')).not.toBe('0')
    expect(selectionText('负责人')).toBe('')
    expect(placeholderText('负责人')).toContain('负责人')
  })

  it('顶级部门 → 父部门那一栏显示占位符,不是 "0"', async () => {
    renderPage()
    await openEditOfSelected() // 总部:parent_id = '0'

    expect(selectionText('父部门')).not.toBe('0')
    expect(selectionText('父部门')).toBe('')
    expect(placeholderText('父部门')).toContain('父部门')
  })

  it('对照组:有负责人的部门 → 显示的是人名(不是占位符)', async () => {
    renderPage()
    // 点左边的树选中「研发部」(leader_id = 'u1')
    await waitFor(() => expect(treeNode('研发部')).toBeTruthy())
    fireEvent.click(treeNode('研发部'))
    await waitFor(() => expect(headerEditButton()).toBeTruthy())

    fireEvent.click(headerEditButton())
    await waitFor(() => expect(formItem('负责人')).toBeTruthy())

    // ⚠️ 这条是**对照组**:证明上面两条不是因为"下拉根本没加载出来"才空着
    expect(selectionText('负责人')).toBe('张三')
    expect(placeholderText('负责人')).toBe('')
  })

  it('提交时把 "0" 原样发出去(后端据此清空负责人 / 提到顶级)', async () => {
    renderPage()
    await openEditOfSelected() // 总部

    fireEvent.click(saveButton())
    await waitFor(() => expect(updateMock).toHaveBeenCalled())

    const payload = lastUpdatePayload()
    expect(payload.leader_id).toBe('0')
    expect(payload.parent_id).toBe('0')
  })
})
