/**
 * 角色管理页 · 「查看权限」抽屉的只读标志 —— 回归网
 *
 * 用户报的现象(2026-10-29):
 *   "查看权限 drawer 关闭时候,里面的组件在只读状态下,在关闭那一刻,
 *    好像会变成可编辑状态。"
 *
 * ## 根因
 *
 * 页面以前只有一个 state:
 *
 *   const [assigning, setAssigning] = useState<Role | null>(null)
 *   <AssignResourcesDrawer
 *     open={!!assigning}
 *     role={assigning}
 *     readOnly={!!assigning && isProtected(assigning)}
 *     onClose={() => setAssigning(null)}        // ← 一把清空
 *   />
 *
 * `onClose` 把 role 清成 null ⇒ `readOnly` 立刻变成 false。
 * 而 Drawer 带 `destroyOnHidden` ⇒ **关闭动画走完之前,内容还挂在 DOM 上**
 * ⇒ 用户看到"只读的权限树在关闭那一瞬间变成可编辑的"(还能点复选框)。
 *
 * ## 为什么这里 mock 掉抽屉(而不是断言 DOM)
 *
 * 浏览器里关闭动画有几百毫秒,那段时间里内容还在 ⇒ 用户能点到。
 * 但 **jsdom 里 rc-motion 判定"无过渡"⇒ 关闭是同步完成的**,DOM 里根本
 * 抓不到那一帧(实测:点完关闭,`.ant-tree-treenode` 立刻变成 0 个)。
 * ⇒ 断言 DOM 只能得到"抽屉没了"这种恒真结论 = 假绿。
 *
 * ⇒ 这里改成断言**抽屉收到的 props**:"关闭"这个动作不许把 role / readOnly
 *     一起带走。props 变了 ⇒ 真实抽屉里那个"只读标志"必然跟着变。
 *     (抽屉本身把 readOnly 接到树的 disabled 上,那条链在
 *      AssignResourcesDrawer.test.tsx 里单独钉住。)
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import RoleList from '../index'
import { roleApi, resourceApi } from '@/api/role'
import { usePermissionStore } from '@/stores/permission'

vi.mock('@/api/role', () => ({
  roleApi: {
    list: vi.fn(),
    getResources: vi.fn(),
    assignResources: vi.fn(),
    detail: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
  },
  resourceApi: { tree: vi.fn(), accessible: vi.fn(), list: vi.fn() },
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

/** 抽屉探针收到的 props(只声明要断言的那几个) */
interface DrawerProbeProps {
  open: boolean
  role?: { code?: string } | null
  readOnly?: boolean
  onClose: () => void
}

// 抽屉被换成"探针":把收到的 props 原样显示出来(见文件头「为什么 mock 抽屉」)
vi.mock('../AssignResourcesDrawer', () => ({
  AssignResourcesDrawer: (props: DrawerProbeProps) => (
    <div data-testid="drawer">
      <span data-testid="drawer-open">{String(props.open)}</span>
      <span data-testid="drawer-role">{props.role?.code ?? 'none'}</span>
      <span data-testid="drawer-readonly">{String(props.readOnly)}</span>
      <button onClick={props.onClose}>close-drawer</button>
    </div>
  ),
}))

const listMock = vi.mocked(roleApi.list)
const getResourcesMock = vi.mocked(roleApi.getResources)
const treeMock = vi.mocked(resourceApi.tree)

/** 内置角色 ⇒ isProtected ⇒ 抽屉进只读模式 */
const BUILTIN_ROW = {
  id: 'r1',
  code: 'role_readonly',
  name: '只读查看员',
  description: '',
  sort: 1,
  type: 'platform' as const,
  is_builtin: true,
  enabled: true,
  created_at: '2026-10-01T00:00:00+08:00',
  updated_at: '2026-10-01T00:00:00+08:00',
}

function renderPage() {
  return render(
    <App>
      <RoleList />
    </App>,
  )
}

/** 打开「查看权限」抽屉(内置角色 ⇒ 只读) */
async function openViewDrawer() {
  renderPage()
  await waitFor(() => expect(screen.getByText('只读查看员')).toBeInTheDocument())
  fireEvent.click(screen.getByRole('button', { name: '查看权限' }))
  await waitFor(() => expect(screen.getByTestId('drawer-open')).toHaveTextContent('true'))
}

/** 探针上读到的三个值 */
function drawerProps() {
  return {
    open: screen.getByTestId('drawer-open').textContent,
    role: screen.getByTestId('drawer-role').textContent,
    readOnly: screen.getByTestId('drawer-readonly').textContent,
  }
}

describe('角色管理页 · 查看权限抽屉的只读标志', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ list: [BUILTIN_ROW], total: 1, page: 1, size: 20 } as never)
    getResourcesMock.mockResolvedValue({ resource_ids: [] } as never)
    treeMock.mockResolvedValue([] as never)
    // 行内按钮按按钮级权限码过滤 —— 没有它就一个按钮都渲染不出来
    usePermissionStore.setState({
      permissionCodes: new Set(['button.role.assign', 'button.role.edit', 'button.role.delete']),
      roleCodes: [],
    })
  })

  it('内置角色打开的是只读抽屉', async () => {
    await openViewDrawer()
    expect(drawerProps()).toEqual({
      open: 'true',
      role: 'role_readonly',
      readOnly: 'true',
    })
  })

  it('关闭抽屉时,角色和只读标志都**不能**被一起清掉', async () => {
    await openViewDrawer()

    fireEvent.click(screen.getByText('close-drawer'))

    // open 翻了,role 和 readOnly 必须原封不动
    // ⚠️ 少了这两条,浏览器里关闭动画期间那棵树就会从"禁用"变成"可勾"
    expect(drawerProps()).toEqual({
      open: 'false',
      role: 'role_readonly',
      readOnly: 'true',
    })
  })

  it('关闭后重新打开,只读标志还在(角色没被清掉)', async () => {
    await openViewDrawer()
    fireEvent.click(screen.getByText('close-drawer'))
    fireEvent.click(screen.getByRole('button', { name: '查看权限' }))
    await waitFor(() => expect(screen.getByTestId('drawer-open')).toHaveTextContent('true'))
    expect(drawerProps()).toEqual({
      open: 'true',
      role: 'role_readonly',
      readOnly: 'true',
    })
  })
  // ⚠️ 超时统一由 vitest.config.ts 的 testTimeout 管(antd 页面级用例单条要 10~45s)
})
