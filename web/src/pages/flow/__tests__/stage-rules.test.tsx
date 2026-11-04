/**
 * 审批流程配置 · 规则页 —— 候选审批人名单的**权限门控**(M147)
 *
 * ## 这条网守的是什么
 *
 *   规则页要显示"这条规则指向哪个审批人",而审批人名单来自
 *   `GET /system/users` / `GET /system/roles`。
 *
 *   M147(2026-11-01)之前,后端**每条角色**都带一条 `obj = "*"` 的通配策略,
 *   所以任何人都调得动这两个接口。M147 把通配行删掉、matcher 换成
 *   `keyMatch`(精确/前缀匹配),后端**真的**开始按绑定矩阵强制执行。
 *
 *   ⇒ 后果:没有 `user:list` / `role:list` 的角色打开这一页,会连弹两个
 *     "没有权限"的红色 toast,而且下拉框永远是空的。
 *
 *   审批系统 §8.4 的 C 组(看流程配置:房源/资料/信息/交付管理员)正是这种角色
 *   —— 它们**没有** `user:list` / `role:list`,而且它们在本页能看到的编辑
 *   入口全被 `PermButton.FLOW_*` 挡住了,本来就用不上这份名单。
 *
 *   ⇒ 所以正确做法是**前端按权限码门控**:没有权限码就别调那个接口。
 *     (另一种做法是给 C 组补上这两个码 —— 那会把一份"看不见的名单"
 *      变成一项"看得见的权限",反而把权限面扩大了。)
 *
 * ## 为什么看"调没调接口"而不是看 DOM
 *
 *   名单是空的时候,DOM 上是"下拉框里没有选项" —— 这跟"接口还没回来"
 *   在渲染结果上**长得一样**。真正要守的是**这个请求有没有发出去**:
 *   发了就会被后端 403,弹一个用户看得见的红 toast。
 *
 * ## 可逆探针
 *
 *   ① 把 `has(PermSystem.USER_LIST)` 那道门去掉 ⇒ 第一个用例必须变红;
 *   ② 把 effect 依赖改回 `[has]` ⇒ 第四条(切身份)必须变红 ——
 *      `has` 是 store 里只创建一次的函数,引用永远不变 ⇒ 只跟它走,
 *      权限集换了也不会重拉名单(切身份那条路**不 reload 页面**)。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import FlowStageRules from '../stage/[stageId]'
import { flowApi } from '@/api/flow'
import { userApi } from '@/api/user'
import { roleApi } from '@/api/role'
import { usePermissionStore } from '@/stores/permission'

vi.mock('@/api/flow', () => ({
  flowApi: {
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

vi.mock('@/api/role', () => ({
  roleApi: { list: vi.fn() },
  // ⚠️ `@/stores/permission` 也从这个模块取 `resourceApi` —— mock 里要留着,
  //   否则 vitest 的 mock 代理在**别的**测试走到它时会抛
  //   "No 'resourceApi' export is defined on the '@/api/role' mock"。
  resourceApi: { accessible: vi.fn() },
}))

const detailMock = vi.mocked(flowApi.detail)
const updateMock = vi.mocked(flowApi.update)
const userListMock = vi.mocked(userApi.list)
const roleListMock = vi.mocked(roleApi.list)

/** 模板详情:一关里带一条「指定人」规则(编辑用例要用它) */
const DETAIL = {
  id: 'f1',
  code: 'material_flow',
  name: '资料审批模板',
  biz_type: 'digital_material',
  is_active: true,
  remark: '',
  stages: [
    {
      id: 's1',
      stage_index: 0,
      name: '初审',
      review_type: 'any',
      rules: [{ id: 'r1', seq: 1, rule_type: 'user', user_id: '9001', role_id: '0', levels: 0 }],
    },
  ],
}

function renderPage() {
  return render(
    <App>
      <MemoryRouter initialEntries={['/flow/f1/stage/s1']}>
        <Routes>
          <Route path="/flow/:id/stage/:stageId" element={<FlowStageRules />} />
        </Routes>
      </MemoryRouter>
    </App>,
  )
}

describe('审批规则页 · 候选审批人名单按权限码门控(M147)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    detailMock.mockResolvedValue(DETAIL as never)
    userListMock.mockResolvedValue({ list: [], total: 0 } as never)
    roleListMock.mockResolvedValue({ list: [], total: 0 } as never)
    // 默认:C 组(只有看流程配置的权限,没有 user:list / role:list)
    usePermissionStore.setState({ permissionCodes: new Set(['flow:read', 'menu.flow']) })
  })

  it('没有 user:list / role:list 时,不发这两个请求', async () => {
    renderPage()

    // 等详情真的渲染出来(说明 mount 后的那些 effect 已经跑过了)
    // ⚠️ 锚点用**模板名**(Card 里那个 Tag),不用关卡名 ——
    //   关卡名在标题里被 `stageTitleWithRules` 拼成"初审 · 审批人规则",
    //   整串文本匹配不到;而且标题 span 的父容器也会命中,`getByText` 会报"多个元素"。
    await waitFor(() => expect(screen.getByText('资料审批模板')).toBeTruthy())

    expect(userListMock).not.toHaveBeenCalled()
    expect(roleListMock).not.toHaveBeenCalled()
  })

  it('有 user:list 时才拉用户名单', async () => {
    usePermissionStore.setState({
      permissionCodes: new Set(['flow:read', 'menu.flow', 'user:list']),
    })

    renderPage()

    await waitFor(() => expect(userListMock).toHaveBeenCalledTimes(1))
    expect(userListMock).toHaveBeenCalledWith({ page: 1, page_size: 200 })
    // 没有 role:list ⇒ 角色名单照样不拉
    expect(roleListMock).not.toHaveBeenCalled()
  })

  it('有 role:list 时才拉角色名单', async () => {
    usePermissionStore.setState({
      permissionCodes: new Set(['flow:read', 'menu.flow', 'role:list']),
    })

    renderPage()

    await waitFor(() => expect(roleListMock).toHaveBeenCalledTimes(1))
    expect(roleListMock).toHaveBeenCalledWith({ page: 1, page_size: 200 })
    expect(userListMock).not.toHaveBeenCalled()
  })

  it('权限集变了(切身份)⇒ 名单重新拉', async () => {
    // ⚠️ 这条守的是**切身份那条路**:IdentitySwitcher 走 fetchAccessible(),
    //   **不 reload 页面** ⇒ 组件不重新挂载 ⇒ mount 时那一次判断就成了
    //   "上一个身份"的判断。
    //   store 里的 permissionCodes 换了(每次 setPermissions 都是新 Set),
    //   组件会重渲染 ⇒ effect 必须跟着重跑,否则名单停在旧身份那一份
    //   (从"没有 user:list"切到"有" ⇒ 名单永远是空的,明明有权限却选不了人)。
    renderPage()

    await waitFor(() => expect(screen.getByText('资料审批模板')).toBeTruthy())
    expect(userListMock).not.toHaveBeenCalled()

    // 切到一个有 user:list 的身份
    usePermissionStore.setState({
      permissionCodes: new Set(['flow:read', 'menu.flow', 'user:list']),
    })

    await waitFor(() => expect(userListMock).toHaveBeenCalledTimes(1))
    expect(userListMock).toHaveBeenCalledWith({ page: 1, page_size: 200 })
  })
})

/**
 * 规则编辑弹窗 · 审批人来源切换(2026-11-03)
 *
 * ## 这条网守的是什么
 *
 *   以前"第二个字段显示哪个下拉"由 `editing.data?.rule_type` 算 —— 那是
 *   **打开弹窗那一刻**的快照。用户在弹窗里把"指定人"切成"指定角色"后:
 *
 *   · 字段列表纹丝不动(角色下拉根本不出现,想选也没得选)
 *   · 提交的是 `rule_type: 'role'` + 空 `role_id` → 后端必拒
 *     「第 N 关第 M 条规则是指定角色,但没填 role_id」
 *
 *   ⇒ **用户根本改不了一条规则的来源,只能删了重建**。
 *     修法 = 弹窗打开时把来源存进草稿状态,切 Select 就同步
 *     (跟 system/dyn 设计器的 fieldTypeDraft 同一个坑、同一个修法)。
 *
 * ## 可逆探针
 *
 *   ① 把弹窗打开时那两处 `setRuleTypeDraft` 删掉(草稿停在默认值)
 *      ⇒ 第一条(切到指定角色)变红 —— 角色下拉不出现,选不了角色;
 *   ② 把 editFields 的 `ruleTypeDraft` 改回 `editing.data?.rule_type`
 *      ⇒ 同样变红(这就是原始 bug 本身);
 *   ③ 把新增默认值改回 'user'(C 组没有 user:list,名单是空的)
 *      ⇒ 第二条(新增直接保存)变红 —— required 卡住,update 不会被调。
 */
describe('规则编辑弹窗 · 审批人来源切换', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    detailMock.mockResolvedValue(DETAIL as never)
    updateMock.mockResolvedValue({} as never)
    userListMock.mockResolvedValue({ list: [], total: 0 } as never)
    roleListMock.mockResolvedValue({
      list: [{ id: '9201', code: 'role_finance', name: '财务', is_active: true }],
      total: 1,
    } as never)
  })

  /** 弹窗里的第 n 个下拉(字段顺序:审批人来源在最前) */
  function modalSelect(n: number): HTMLElement {
    const els = document.querySelectorAll<HTMLElement>('.ant-modal .ant-select-selector')
    expect(els.length).toBeGreaterThan(n)
    return els[n]!
  }

  /** 在弹窗第 n 个下拉里点掉文案为 label 的选项 */
  async function pickOption(n: number, label: string) {
    fireEvent.mouseDown(modalSelect(n))
    await waitFor(() => expect(document.querySelector('.ant-select-dropdown')).toBeTruthy())
    const option = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
      (el) => (el.textContent ?? '').trim() === label,
    )
    if (!option) throw new Error(`下拉里找不到选项:${label}`)
    fireEvent.click(option)
  }

  it('编辑:切到「指定角色」⇒ 角色下拉出现,提交的是 rule + role_id', async () => {
    usePermissionStore.setState({
      permissionCodes: new Set(['flow:read', 'menu.flow', 'button.flow.edit', 'role:list']),
    })
    renderPage()
    await waitFor(() => expect(screen.getByText('资料审批模板')).toBeTruthy())

    // 打开编辑弹窗
    fireEvent.click(screen.getByRole('button', { name: /编\s*辑/ }))
    await waitFor(() => expect(screen.getByText('审批人来源')).toBeTruthy())

    // 切到「指定角色」
    await pickOption(0, '指定角色')

    // ⭐ 角色下拉必须**跟着出现**(原始 bug:它根本不出现,提交必被后端拒)
    //   —— 等字段列表真的换掉(旧的「指定人」下拉卸载),再往里点
    await waitFor(() =>
      expect(document.querySelectorAll('.ant-modal .ant-select-selector')).toHaveLength(2),
    )
    //   在第 2 个下拉里能选到角色,就是它出现的证明
    await pickOption(1, '财务(role_finance)')

    // 保存
    fireEvent.click(screen.getByText(/保\s*存/))
    await waitFor(() => expect(updateMock).toHaveBeenCalledTimes(1))

    const payload = updateMock.mock.calls[0]![1] as unknown as { stages: { rules: Record<string, unknown>[] }[] }
    const rule = payload.stages[0]!.rules[0]!
    expect(rule.rule_type).toBe('role')
    expect(rule.role_id).toBe('9201')
    // ⚠️ 互斥:换成了 role,原来的 user_id 必须清掉(后端判"规则配错"的另一种方式)
    expect(rule.user_id).toBeUndefined()
  })

  it('新增:默认「部门主管」⇒ 不用拉名单、直接能存(C 组没有 user:list)', async () => {
    // C 组:没有 user:list / role:list(只有能编辑流程这件事本身)
    usePermissionStore.setState({
      permissionCodes: new Set(['flow:read', 'menu.flow', 'button.flow.add']),
    })
    renderPage()
    await waitFor(() => expect(screen.getByText('资料审批模板')).toBeTruthy())

    fireEvent.click(screen.getByRole('button', { name: /新\s*增/ }))
    await waitFor(() => expect(screen.getByText('审批人来源')).toBeTruthy())

    // 默认来源是「部门主管」⇒ 第二个字段是「往上几级」,不是需要名单的「指定人」
    //   (⚠️ 别用 screen.queryByText —— 表格里已有规则的头像 Tag 也叫「指定人」)
    expect(screen.getByText('往上几级')).toBeTruthy()
    const modalText = document.querySelector('.ant-modal')?.textContent ?? ''
    expect(modalText).not.toContain('指定人')

    fireEvent.click(screen.getByText(/保\s*存/))
    await waitFor(() => expect(updateMock).toHaveBeenCalledTimes(1))

    const payload = updateMock.mock.calls[0]![1] as unknown as { stages: { rules: Record<string, unknown>[] }[] }
    const rule = payload.stages[0]!.rules[1]! // 新规则排在已有那条后面
    expect(rule.rule_type).toBe('dept_leader')
    expect(rule.levels).toBe(1)
    expect(rule.user_id).toBeUndefined()
    expect(rule.role_id).toBeUndefined()
  })
})
