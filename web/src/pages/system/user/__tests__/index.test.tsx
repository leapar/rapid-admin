/**
 * 用户管理页渲染测试 —— 「用户编辑界面无法设置角色」那个 Bug 的回归网
 *
 * 页面级用例,盯的是**界面上真能做的事**(纯逻辑在 userForm.test.ts):
 *   1. 打开编辑弹窗 → 角色多选框里已经回填了这个人的角色
 *      (以前压根没有这个字段)
 *   2. 只改昵称 → 提交给后端的载荷里**不能**有 identities
 *      (后端 identities 是"清空重建"式全量替换,乱发就把人权限清零)
 *   3. 改了角色 → 载荷里要有 identities,role_code 正确
 *   4. 详情接口失败 → 不开弹窗(不知道现有身份就保存 = 覆盖掉对方的身份)
 *   5. 新增用户 → 载荷带 64 位 hex 的 password(后端只认这个长度)
 *
 * 另外钉住一条"防手滑"的:管理身份抽屉必须先拉详情,
 * 拿列表行(没有 identities)当身份数据 → 抽屉是空的 → 一保存就清空身份。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import UserList from '../index'
import { userApi } from '@/api/user'
import { roleApi, departmentApi } from '@/api/role'
import { usePermissionStore } from '@/stores/permission'

vi.mock('@/api/user', () => ({
  userApi: {
    list: vi.fn(),
    detail: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    batchRemove: vi.fn(),
    toggleStatus: vi.fn(),
    unlock: vi.fn(),
  },
}))

vi.mock('@/api/role', () => ({
  roleApi: { list: vi.fn() },
  departmentApi: { tree: vi.fn() },
  resourceApi: { accessible: vi.fn() },
}))

// 重置密码走短信二次验证,页面里只是个入口 —— 这里不打后端
vi.mock('@/api/auth', () => ({ resetPassword: vi.fn() }))

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

vi.mock('@/hooks/useAuth', () => ({
  useAuth: () => ({ userInfo: { id: 'me', is_platform_super_admin: true } }),
}))

// 浏览器环境算 sha256;jsdom 里不一定有 crypto.subtle,这里直接给 64 位 hex
vi.mock('@/utils/crypto', () => ({
  preHashPassword: async () => 'c'.repeat(64),
}))

const listMock = vi.mocked(userApi.list)
const detailMock = vi.mocked(userApi.detail)
const updateMock = vi.mocked(userApi.update)
const createMock = vi.mocked(userApi.create)
const roleListMock = vi.mocked(roleApi.list)
const deptTreeMock = vi.mocked(departmentApi.tree)

/** 列表行(后端 UserListItem 的形状:没有 identities) */
const ROW = {
  id: 'u1',
  username: 'zhangsan',
  nickname: '张三',
  phone: '13800000000',
  email: 'zhangsan@x.cn',
  avatar: '',
  department_id: '100',
  department_name: '运维部',
  status: 'active' as const,
  mfa_enabled: false,
  is_platform_super_admin: false,
  created_at: '2026-10-01T00:00:00+08:00',
}

/** 详情(后端 *service.UserDetail:带 identities + departments,没有 phone/email/status) */
const DETAIL = {
  ...ROW,
  phone: '',
  email: undefined,
  status: undefined,
  // 2026-10-25 M5d:后端补了 departments[] —— 全部部门,不止主部门
  departments: [
    { id: '100', name: '运维部', is_primary: true },
    { id: '101', name: '客服部', is_primary: false },
  ],
  identities: [
    {
      id: 'i1',
      user_id: 'u1',
      role_code: 'role_user_admin',
      role_name: '用户管理员',
      project_id: '0',
      dept_id: '0',
      valid_from: '2026-01-01T00:00:00+08:00',
      valid_to: null,
      is_active: true,
      is_primary: true,
    },
  ],
}

const ROLES = [
  { id: 'r1', code: 'role_user_admin', name: '用户管理员' },
  { id: 'r2', code: 'role_ops', name: '平台运营' },
]

function renderPage() {
  return render(
    <App>
      <UserList />
    </App>,
  )
}

/** 打开编辑弹窗(点行操作里的"编辑") */
async function openEdit() {
  renderPage()
  await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument())
  fireEvent.click(screen.getByRole('button', { name: '编辑' }))
  // 弹窗是"先拉详情再开"的,等角色多选框出现
  await waitFor(() => expect(document.getElementById('roles')).toBeInTheDocument())
}

/** 角色多选框当前选中的项(antd 把选中项渲染成 .ant-select-selection-item) */
function selectedRoleLabels(): string[] {
  return Array.from(
    document.querySelectorAll('.ant-select-selection-item'),
  ).map((el) => el.getAttribute('title') ?? el.textContent ?? '')
}

/** 按 label 找弹窗里的表单项(部门那个是自定义组件,拿不到干净的 id) */
function formItemByLabel(label: string): HTMLElement {
  const items = Array.from(document.querySelectorAll('.ant-form-item'))
  const found = items.find(
    (el) => el.querySelector('.ant-form-item-label')?.textContent?.trim() === label,
  )
  if (!found) throw new Error(`弹窗里找不到字段:${label}`)
  return found as HTMLElement
}

/** 在角色多选框里加一个角色 */
async function pickRole(label: string) {
  const input = document.getElementById('roles') as HTMLElement
  const selector = input.closest('.ant-select-selector') as HTMLElement
  fireEvent.mouseDown(selector)
  const option = await screen.findByText(label)
  fireEvent.click(option)
  await waitFor(() => expect(selectedRoleLabels().some((l) => l.includes(label))).toBe(true))
}

/**
 * 点弹窗的"保存"
 *
 * ⚠️ antd 5 的 Button 会在两个汉字之间自动插空格("保存" → "保 存"),
 *    所以这里不能按 "保存" 精确匹配。
 */
function clickSave() {
  fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }))
}

describe('用户管理页 · 角色', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ list: [ROW], total: 1, page: 1, size: 20 } as never)
    detailMock.mockResolvedValue(DETAIL as never)
    updateMock.mockResolvedValue(undefined as never)
    createMock.mockResolvedValue(undefined as never)
    roleListMock.mockResolvedValue({ list: ROLES, total: 2, page: 1, size: 20 } as never)
    deptTreeMock.mockResolvedValue([
      { id: '100', code: 'D1', name: '运维部', parent_id: '0', path: '1,', sort: 1, member_count: 1, is_active: true },
      { id: '101', code: 'D2', name: '客服部', parent_id: '0', path: '1,', sort: 2, member_count: 0, is_active: true },
    ] as never)
    // 行操作按钮按 perm 显隐 —— 放行编辑/新增/批量删除
    usePermissionStore.setState({
      permissionCodes: new Set([
        'button.user.edit',
        'button.user.add',
        'button.user.delete',
        'user:list',
      ]),
      roleCodes: ['role_super_admin'],
    })
  })

  it('编辑弹窗里的角色多选框回填了该用户现有的角色', async () => {
    await openEdit()
    // 以前压根没有"角色"字段,这里必然是空的
    expect(selectedRoleLabels().some((l) => l.includes('用户管理员'))).toBe(true)
  })

  it('只改昵称时,载荷里不能出现 identities(后端是全量替换,乱发就清权限)', async () => {
    await openEdit()
    const nickname = screen.getByLabelText('昵称') as HTMLInputElement
    fireEvent.change(nickname, { target: { value: '张三丰' } })
    clickSave()
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    const payload = updateMock.mock.calls[0]![1] as Record<string, unknown>
    expect(payload.nickname).toBe('张三丰')
    expect('identities' in payload).toBe(false)
    expect('phone' in payload).toBe(false)
    expect('email' in payload).toBe(false)
  })

  it('改了角色 → 载荷里带 identities,role_code 正确', async () => {
    await openEdit()
    await pickRole('平台运营 (role_ops)')
    clickSave()
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    const payload = updateMock.mock.calls[0]![1] as Record<string, unknown>
    const identities = payload.identities as Array<{ role_code: string }>
    expect(identities).toBeDefined()
    expect(identities.map((i) => i.role_code).sort()).toEqual(['role_ops', 'role_user_admin'])
  })

  it('详情接口失败 → 不开弹窗(不知道现有身份就保存 = 覆盖掉对方的身份)', async () => {
    detailMock.mockRejectedValue(new Error('boom') as never)
    renderPage()
    await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    // ⚠️ M132:等**失败提示上屏**,不等"请求发出去了"。
    //   请求发出 ≠ catch 分支跑完 —— catch 里那句 message.error 才证明
    //   组件已经走到"处理完失败"这一步。在这之前断言"弹窗不存在",
    //   弹窗完全可以在断言**之后**才开出来(失败也照样 setEditing 的那种
    //   改法)⇒ 白绿。
    await screen.findByText(/加载用户详情失败/)
    expect(document.getElementById('roles')).toBeNull()
    // ⚠️ M132:再查一遍"**有没有弹窗**"本身 ——
    //   `getElementById('roles')` 查的是弹窗**里面某个字段**。
    //   弹窗开了但那个字段没渲染(或字段 id 改了)⇒ 上面那行照样绿,
    //   而"弹窗开了"这件事已经发生 ⇒ 断言白绿。
    //   `role="dialog"` 是 rc-dialog 自己挂在容器上的,跟字段 id 无关。
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(updateMock).not.toHaveBeenCalled()
  })

  it('新增用户:载荷带 64 位 hex 的 password + 部门 + 角色', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /新增/ }))
    await waitFor(() => expect(document.getElementById('roles')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('账号'), { target: { value: 'lisi' } })
    fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '李四' } })
    fireEvent.change(screen.getByLabelText('手机号'), { target: { value: '13800000001' } })
    fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: 'Passw0rd!' } })
    await pickRole('用户管理员 (role_user_admin)')

    // 部门是多选树(TreeSelect + treeCheckable),要点节点上的**勾选框**
    // (点标题不会勾上 —— 多选树的选中走的是 check 事件)
    const deptSelector = formItemByLabel('部门').querySelector(
      '.ant-select-selector',
    ) as HTMLElement
    fireEvent.mouseDown(deptSelector)
    const deptNode = await screen.findByTitle('运维部')
    const deptCheckbox = deptNode
      .closest('.ant-select-tree-treenode')
      ?.querySelector('.ant-select-tree-checkbox-inner') as HTMLElement
    fireEvent.click(deptCheckbox)

    clickSave()
    await waitFor(() => expect(createMock).toHaveBeenCalled())
    const payload = createMock.mock.calls[0]![0] as unknown as Record<string, unknown>
    expect(payload.username).toBe('lisi')
    expect(payload.password).toBe('c'.repeat(64))
    expect(payload.department_ids).toEqual(['100'])
    expect((payload.identities as Array<{ role_code: string }>)[0]!.role_code).toBe(
      'role_user_admin',
    )
  })
  // ⚠️ 超时统一由 vitest.config.ts 的 testTimeout 管(antd 页面级用例单条要 10~45s)
})

/**
 * 批量操作(2026-10-25 M5b)
 *
 * 页面的勾选状态以前是自己写的 `selectedIds` + 手搓 rowSelection,
 * 现在交给框架的 useTableSelection + BatchActionBar。
 * 这里盯的是**页面接线**:勾一行 → 批量条出现 → 确认 → 调 batchRemove(ids)。
 */
describe('用户管理页 · 批量操作', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ list: [ROW], total: 1, page: 1, size: 20 } as never)
    detailMock.mockResolvedValue(DETAIL as never)
    vi.mocked(userApi.batchRemove).mockResolvedValue(undefined as never)
    roleListMock.mockResolvedValue({ list: ROLES, total: 2, page: 1, size: 20 } as never)
    deptTreeMock.mockResolvedValue([] as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['button.user.delete', 'user:list']),
      roleCodes: ['role_super_admin'],
    })
  })

  it('没勾选时不出现批量条', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument())
    expect(screen.queryByText(/已选/)).toBeNull()
  })

  it('勾一行 → 出现"已选 1 项" → 确认后调 batchRemove(勾选的 id)', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument())

    // 索引 0 = 表头全选,1 = 第一行
    const boxes = screen.getAllByRole('checkbox')
    fireEvent.click(boxes[1]!)

    await waitFor(() => expect(screen.getByText(/已选\s*1\s*项/)).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: /批量删除/ }))

    // ConfirmDialog 确认
    // ⚠️ antd 5 的 Button 会在两个汉字之间自动插空格("确认" → "确 认"),不能精确匹配
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /确\s*认/ })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('button', { name: /确\s*认/ }))

    await waitFor(() => expect(userApi.batchRemove).toHaveBeenCalled())
    expect(userApi.batchRemove).toHaveBeenCalledWith(['u1'])
  })

  it('取消选择后批量条消失', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument())

    const boxes = screen.getAllByRole('checkbox')
    fireEvent.click(boxes[1]!)
    await waitFor(() => expect(screen.getByText(/已选\s*1\s*项/)).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: /取消选择/ }))
    await waitFor(() => expect(screen.queryByText(/已选/)).toBeNull())
  })
  // ⚠️ 超时统一由 vitest.config.ts 的 testTimeout 管(antd 页面级用例单条要 10~45s)
})

/**
 * 部门(2026-10-25 M5d)
 *
 * 后端补了 GET /system/users/:id 的 departments[] 之后,编辑弹窗的"部门"
 * 从只读改回可编辑。这里盯的是**页面接线**:
 *   · 回填必须是详情里的**全部**部门(不是只有主部门)
 *   · 保存时发的是"改完之后完整的全集"(后端是清空重建式全量替换)
 *   · 没改就一个字段都不发
 */
describe('用户管理页 · 部门', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ list: [ROW], total: 1, page: 1, size: 20 } as never)
    detailMock.mockResolvedValue(DETAIL as never)
    updateMock.mockResolvedValue(undefined as never)
    roleListMock.mockResolvedValue({ list: ROLES, total: 2, page: 1, size: 20 } as never)
    deptTreeMock.mockResolvedValue([
      { id: '100', code: 'D1', name: '运维部', parent_id: '0', path: '1,', sort: 1, member_count: 1, is_active: true },
      { id: '101', code: 'D2', name: '客服部', parent_id: '0', path: '1,', sort: 2, member_count: 0, is_active: true },
    ] as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['button.user.edit', 'user:list']),
      roleCodes: ['role_super_admin'],
    })
  })

  /** 部门树里某个部门的勾选框 */
  function deptCheckbox(title: string): HTMLElement {
    const nodes = Array.from(document.querySelectorAll('.ant-select-tree-treenode'))
    const found = nodes.find(
      (n) => n.querySelector('.ant-select-tree-title')?.textContent === title,
    )
    if (!found) throw new Error(`部门树里找不到:${title}`)
    return found.querySelector('.ant-select-tree-checkbox') as HTMLElement
  }

  /**
   * 打开部门树的下拉
   *
   * ⚠️ 树是**懒渲染**的:TreeSelect 的树在 dropdown 里,
   *    不打开下拉就一个节点都不渲染(选中的标签是另一份 DOM,别拿它当树)。
   */
  async function openDeptTree() {
    const input = document.getElementById('multiDept') as HTMLElement
    const selector = input.closest('.ant-select-selector') as HTMLElement
    fireEvent.mouseDown(selector)
    await waitFor(() => expect(document.querySelector('.ant-select-tree')).toBeTruthy())
  }

  it('编辑弹窗里的部门回填了详情里的全部部门(不只是主部门)', async () => {
    await openEdit()
    await openDeptTree()

    expect(deptCheckbox('运维部')).toHaveAttribute('aria-checked', 'true')
    expect(deptCheckbox('客服部')).toHaveAttribute('aria-checked', 'true')
  })

  it('取消勾选一个部门 → 保存时发的是改完之后完整的集合', async () => {
    await openEdit()
    await openDeptTree()

    const cb = deptCheckbox('客服部').querySelector(
      '.ant-select-tree-checkbox-inner',
    ) as HTMLElement
    fireEvent.click(cb)
    await waitFor(() =>
      expect(deptCheckbox('客服部')).toHaveAttribute('aria-checked', 'false'),
    )

    clickSave()
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    const payload = updateMock.mock.calls[0]![1] as Record<string, unknown>
    expect(payload.department_ids).toEqual(['100'])
    expect(payload.primary_department_id).toBe('100')
  })

  it('部门没改 → 载荷里没有 department_ids(全量替换语义,没改就别碰)', async () => {
    await openEdit()
    await openDeptTree()

    clickSave()
    await waitFor(() => expect(updateMock).toHaveBeenCalled())
    const payload = updateMock.mock.calls[0]![1] as Record<string, unknown>
    expect('department_ids' in payload).toBe(false)
    expect('primary_department_id' in payload).toBe(false)
  })
  // ⚠️ 超时统一由 vitest.config.ts 的 testTimeout 管(antd 页面级用例单条要 10~45s)
})

/**
 * 用户管理页 · 「权限」列 —— 2026-10-30 M122
 *
 * # 这条网守的是什么
 *
 * 列表里**完全没有角色信息** —— 管理员只能点进详情抽屉才看得到
 * "这个人到底有哪些权限"。而"这个人能干什么"恰恰是用户管理页
 * 最该一眼看到的信息。
 *
 * 后端 `UserListItem.Roles` 返的是**当前有效**的角色(停用的身份不算),
 * 按 role_code 去重。这里锁住三件事:
 *   1. 列头在、角色名以标签形式显示出来
 *   2. 没有角色 → 显示 `-`(不是空 cell —— 空 cell 分不清"确实没有"
 *      和"这一列压根没接上")
 *   3. role_name 为空 → 退回 role_code(不然是个没字的白标签)
 */
describe('用户管理页 · 权限列(M122)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    roleListMock.mockResolvedValue({ list: ROLES, total: 2, page: 1, size: 20 } as never)
    deptTreeMock.mockResolvedValue([] as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['user:list']),
      roleCodes: ['role_super_admin'],
    })
  })

  /**
   * 「权限」列在表格里的**列下标**
   *
   * ⚠️ **按表头找,不写死列号** ——
   *   写死 `td[3]` 的话,以后往表里加一列就全错位,
   *   而错位的断言照样是绿的(它测的是另一列)。
   *   (antd 5 的 `<td>` 上没有 data-column-key,所以只能按序号对齐。)
   */
  function rolesColumnIndex(): number {
    const headers = Array.from(document.querySelectorAll('.ant-table-thead th')).map(
      (th) => th.textContent?.trim() ?? '',
    )
    const idx = headers.indexOf('权限')
    if (idx < 0) throw new Error(`表头里找不到「权限」:${JSON.stringify(headers)}`)
    return idx
  }

  /** 表格里某一行、某一列的文本 */
  function cellText(colIndex: number, rowIndex = 0): string {
    const row = document.querySelectorAll('.ant-table-tbody tr.ant-table-row')[rowIndex]
    const cells = row?.querySelectorAll('td')
    return cells?.[colIndex]?.textContent?.trim() ?? ''
  }

  it('列头是「权限」,角色名以标签显示出来', async () => {
    listMock.mockResolvedValue({
      list: [
        {
          ...ROW,
          roles: [
            { role_code: 'role_super_admin', role_name: '超级管理员' },
            { role_code: 'role_ops', role_name: '平台运营' },
          ],
        },
      ],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()

    // ⚠️ **等的是"角色标签渲染出来了"**,不是"接口被调过了" ——
    //   后者只说明请求发出去了,数据回来 + React 重渲染是之后的事
    await waitFor(() => expect(screen.getByText('超级管理员')).toBeInTheDocument())
    expect(screen.getByText('平台运营')).toBeInTheDocument()

    // 列头也要在(页面上还有别的「权限」字样,所以按表头找)
    const col = rolesColumnIndex()
    // 两个角色都要在**这一列**里
    expect(cellText(col)).toContain('超级管理员')
    expect(cellText(col)).toContain('平台运营')
  })

  it('没有角色 → 显示 "-"(不是空 cell)', async () => {
    listMock.mockResolvedValue({
      list: [{ ...ROW, roles: [] }],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()

    await waitFor(() => expect(listMock).toHaveBeenCalled())
    // 等表格出行
    await waitFor(() =>
      expect(
        document.querySelectorAll('.ant-table-tbody tr.ant-table-row').length,
      ).toBeGreaterThan(0),
    )
    expect(cellText(rolesColumnIndex())).toBe('-')
  })

  it('role_name 为空 → 退回 role_code(不能是个没字的白标签)', async () => {
    listMock.mockResolvedValue({
      list: [{ ...ROW, roles: [{ role_code: 'role_ops', role_name: '' }] }],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()

    await waitFor(() => expect(screen.getByText('role_ops')).toBeInTheDocument())
  })
})

/**
 * 用户管理页 · 头像列(M124,2026-10-30)
 *
 * 用户要求「用户管理列表要相应显示用户头像」。
 *
 * ⚠️ **这一列的渲染代码 M113 就有了**(走 `UserAvatar`),
 *   以前一直显示成首字母圆牌,是因为**根本没有上传入口** ——
 *   用户没法设头像 ⇒ 这一列永远没内容。
 *   M124 加了上传通道(个人中心点头像直接传)⇒ 这一列才有东西可显示。
 *
 * 所以这里守的是**数据通路的最后一环**:
 *   后端 UserListItem.avatar(直链)→ 前端 → 渲染成 <img src=直链>。
 *
 * ⚠️ **空头像必须是"首字母圆牌"而不是裂图** ——
 *   antd 的 `<Avatar src="">` 会渲染 `<img src="">`,浏览器直接显示裂图图标,
 *   而且空 src **不触发 onError** ⇒ 连兜底的机会都没有。
 */
describe('用户管理页 · 头像列(M124)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    detailMock.mockResolvedValue(DETAIL as never)
    roleListMock.mockResolvedValue({ list: ROLES, total: 2, page: 1, size: 20 } as never)
    deptTreeMock.mockResolvedValue([] as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['user:list']),
      roleCodes: ['role_super_admin'],
    })
  })

  /** 表格第一行的头像单元格里那个 <img> 的 src */
  async function firstRowAvatarSrc(): Promise<string | null> {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())
    await waitFor(() =>
      expect(
        document.querySelectorAll('.ant-table-tbody tr.ant-table-row').length,
      ).toBeGreaterThan(0),
    )
    const row = document.querySelector('.ant-table-tbody tr.ant-table-row') as HTMLElement
    const img = row.querySelector('.ant-avatar img')
    return img?.getAttribute('src') ?? null
  }

  it('行里有头像 → 渲染成那张图', async () => {
    listMock.mockResolvedValue({
      list: [{ ...ROW, avatar: '/rapid-admin/u-1/avatar/9/me.png' }],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    expect(await firstRowAvatarSrc()).toBe('/rapid-admin/u-1/avatar/9/me.png')
  })

  it('行里没头像 → **不渲染 <img>**(不能是裂图),而是首字母圆牌', async () => {
    listMock.mockResolvedValue({
      list: [{ ...ROW, avatar: '' }],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    expect(await firstRowAvatarSrc()).toBeNull()

    const row = document.querySelector('.ant-table-tbody tr.ant-table-row') as HTMLElement
    const avatar = row.querySelector('.ant-avatar') as HTMLElement
    expect(avatar).not.toBeNull()
    expect(avatar.textContent).toContain('张')
  })
})
