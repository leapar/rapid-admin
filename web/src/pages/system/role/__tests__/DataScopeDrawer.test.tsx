/**
 * 数据范围抽屉 渲染测试 —— M157(2026-11-02)
 *
 * ## 这条链上"错了没人知道"的地方
 *
 * 数据范围是**行级权限** —— 配错了不会报错,只会**静默地**
 * 让该角色看到全部数据(后端 `internal/pkg/scope` 的注释里明写:
 * 业务域拼错 ⇒ 规则不生效 ⇒ 退化成"全部数据")。
 *
 * 前端这一层要挡住的就是四种"静默":
 *
 *   1. **业务域拼错**      → 候选项必须来自后端,不许前端手打
 *   2. **发空数组当增量**  → 写接口是全量替换,发空 = 清空
 *   3. **发了没改的域**    → 无谓地覆写别的域的规则
 *   4. **把 UI 哨兵当业务域** → 菜单里默认组的 key 是 '@default',
 *      存库的 module 必须是空串。发错 ⇒ 后端直接拒收("业务域不存在")。
 *
 * ⚠️ 第 4 条是最阴的:菜单 key 和存储值**故意不一样**,
 *   一旦有人图省事直接传菜单 key,默认组就**永远配不上**,
 *   而且只有真去后端日志里翻才看得见。
 *
 * ## 可逆探针(拆掉实现看测试会不会红)
 *
 *   · menuItems 改成不补默认组        → C1 红
 *   · active 永远落在默认组            → C2 红
 *   · 去掉「全部数据」警告             → C3 红
 *   · save 改成全量发(所有域都 PUT)    → C4 红
 *   · save 把菜单 key 当 module 发      → C5 红
 *   · readOnly 下仍然显示保存/删除      → C6 红
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { App } from 'antd'
import { DataScopeDrawer } from '../DataScopeDrawer'
import { scopeApi } from '@/api/scope'
import type { ScopeModule } from '@/api/scope'

vi.mock('@/api/scope', () => ({
  scopeApi: {
    listModules: vi.fn(),
    listRoleScopes: vi.fn(),
    replaceRoleScopes: vi.fn(),
  },
}))

const listModulesMock = vi.mocked(scopeApi.listModules)
const listRoleScopesMock = vi.mocked(scopeApi.listRoleScopes)
const replaceMock = vi.mocked(scopeApi.replaceRoleScopes)

const ROLE = { id: 'r1', code: 'ces', name: 'ces' } as never

const MODULES: ScopeModule[] = [
  { module: 'user', label: '用户管理', source: 'api' },
  { module: 'audit', label: '操作日志', source: 'api' },
]

/**
 * 带"支持哪些维度"的候选 —— M161 起后端会报 supported_types
 *
 * ⚠️ **house(房屋)没有归属人列** ⇒ 只支持「全部数据」「本项目」「自定义」。
 */
const MODULES_WITH_TYPES: ScopeModule[] = [
  { module: 'user', label: '用户管理', source: 'api', supported_types: ['all', 'project', 'dept', 'self', 'custom'] },
  { module: 'house', label: '房屋管理', source: 'api', supported_types: ['all', 'project', 'custom'] },
  {
    module: '',
    label: '默认(适用于所有业务域)',
    source: 'default',
    supported_types: ['all', 'project', 'custom'],
  },
]

function renderDrawer(props: Partial<ComponentProps<typeof DataScopeDrawer>> = {}) {
  return render(
    <App>
      <DataScopeDrawer open role={ROLE} onClose={() => undefined} {...props} />
    </App>,
  )
}

/** 等抽屉内容渲染出来(有业务域菜单 = 加载完了) */
async function waitLoaded() {
  await waitFor(() => expect(screen.getByText('默认(全部业务域)')).toBeInTheDocument())
}

/** 打开规则类型下拉(第一个 .ant-select-selector 就是类型选择器) */
async function openTypeSelect() {
  const selector = document.querySelector('.ant-select-selector') as HTMLElement
  expect(selector).toBeTruthy()
  fireEvent.mouseDown(selector)
  await waitFor(() => expect(document.querySelector('.ant-select-dropdown')).toBeTruthy())
}

/** 点某个下拉选项 */
async function pickOption(label: string) {
  const option = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
    (el) => (el.textContent ?? '').trim() === label,
  )
  if (!option) throw new Error(`下拉里找不到选项:${label}`)
  fireEvent.click(option)
}

/** 点「保存」 */
function clickSave() {
  // ⚠️ antd 会给两个汉字的按钮文案自动插空格 ⇒ 匹配用 /保\s*存/
  fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }))
}

describe('DataScopeDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listModulesMock.mockResolvedValue(MODULES as never)
    listRoleScopesMock.mockResolvedValue([] as never)
    replaceMock.mockResolvedValue(undefined as never)
  })

  // C1
  it('业务域候选项来自后端,并且带一个「默认规则组」', async () => {
    renderDrawer()
    await waitLoaded()
    // 后端给的两个域都在
    expect(screen.getByText('用户管理')).toBeInTheDocument()
    expect(screen.getByText('操作日志')).toBeInTheDocument()
    // 默认组是前端补的(后端候选里没有它)
    expect(screen.getByText('默认(全部业务域)')).toBeInTheDocument()
  })

  it('后端一个业务域都没给时,也仍然有默认组可配(不能出现"无处可配")', async () => {
    listModulesMock.mockResolvedValue([] as never)
    renderDrawer()
    await waitFor(() => expect(screen.getByText('默认(全部业务域)')).toBeInTheDocument())
    // ⚠️ 这里曾经是"没有业务域就渲染 Empty" ⇒ 默认组(那时是唯一能配的东西)
    //   被一起藏掉了 ⇒ 一个规则都配不了。现在改成:菜单永远渲染。
    expect(screen.getByText('后端暂未登记任何业务域,当前只能配默认规则组')).toBeInTheDocument()
  })

  // C2
  it('打开时落在第一个有规则的业务域上(不是永远落在默认组)', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: 'user', scope_type: 'self' },
    ] as never)
    renderDrawer()
    await waitLoaded()
    // 「这是单个业务域」= 落在具体域上;「这是「默认规则组」」= 落在默认组
    expect(screen.getByText('这是单个业务域')).toBeInTheDocument()
    expect(screen.queryByText('这是「默认规则组」')).toBeNull()
    // 规则内容也渲染出来了
    expect(screen.getByText('仅本人')).toBeInTheDocument()
  })

  it('没有规则时落在默认组', async () => {
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('这是「默认规则组」')).toBeInTheDocument()
    expect(screen.getByText('本业务域还没有配置规则')).toBeInTheDocument()
  })

  // C3
  it('有「全部数据」时给出警告(其余规则是空转的)', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: '', scope_type: 'all' },
      { module: '', scope_type: 'dept' },
    ] as never)
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('本组里有「全部数据」')).toBeInTheDocument()
  })

  it('没有「全部数据」时不显示那个警告', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: '', scope_type: 'dept' },
    ] as never)
    renderDrawer()
    await waitLoaded()
    expect(screen.queryByText('本组里有「全部数据」')).toBeNull()
  })

  it('「规则之间是或」这条语义提示一直在', async () => {
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('多条规则之间是「或」的关系')).toBeInTheDocument()
  })

  // C4
  it('保存只发改动过的业务域(没改的域一个都不发)', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: 'user', scope_type: 'self' },
      { module: 'audit', scope_type: 'dept' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    // 删掉当前域(user)的那条规则
    fireEvent.click(screen.getByRole('button', { name: /删\s*除/ }))
    await waitFor(() => expect(screen.getByText('本业务域还没有配置规则')).toBeInTheDocument())

    clickSave()
    await waitFor(() => expect(replaceMock).toHaveBeenCalledTimes(1))
    expect(replaceMock.mock.calls[0]).toEqual(['r1', 'user', []])
  })

  it('什么都没改就保存 ⇒ 不发任何 PUT', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: 'user', scope_type: 'self' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    clickSave()
    await waitFor(() => expect(screen.getByText('没有改动')).toBeInTheDocument())
    expect(replaceMock).not.toHaveBeenCalled()
  })

  it('改了规则类型 ⇒ PUT 的 body 是新类型', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: 'user', scope_type: 'self' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    await openTypeSelect()
    await pickOption('本部门')
    // ⚠️ 下拉**选中后不卸载** ⇒ DOM 里同时有"下拉里的选项"和"选择器里的当前值",
    //   两处文字都是「本部门」⇒ getByText 会报"找到多个"。
    //   只认选择器当前值(.ant-select-selection-item)那一个。
    await waitFor(() =>
      expect(document.querySelector('.ant-select-selection-item')?.textContent).toBe('本部门'),
    )

    clickSave()
    await waitFor(() => expect(replaceMock).toHaveBeenCalledTimes(1))
    expect(replaceMock.mock.calls[0]).toEqual(['r1', 'user', [{ scope_type: 'dept' }]])
  })

  // C5 —— 最阴的一条:菜单 key ≠ 存储的 module
  it('默认规则组保存时 module 必须是空串,不是菜单里的哨兵 key', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: '', scope_type: 'dept' },
    ] as never)
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('这是「默认规则组」')).toBeInTheDocument()

    // 删光默认组的规则 ⇒ 这次改动就是"清空默认组"
    fireEvent.click(screen.getByRole('button', { name: /删\s*除/ }))
    await waitFor(() => expect(screen.getByText('本业务域还没有配置规则')).toBeInTheDocument())

    clickSave()
    await waitFor(() => expect(replaceMock).toHaveBeenCalledTimes(1))
    // ⚠️ 第三段必须是 '' —— 菜单里默认组的 key 是 '@default'(UI 哨兵),
    //   发出去的话后端会判"业务域 @default 不存在"直接拒收。
    expect(replaceMock.mock.calls[0]).toEqual(['r1', '', []])
  })

  it('默认规则组新增一条规则时,module 也是空串', async () => {
    renderDrawer()
    await waitLoaded()

    fireEvent.click(screen.getByText('添加规则'))
    await waitFor(() => expect(screen.getByText('仅本人')).toBeInTheDocument())

    clickSave()
    await waitFor(() => expect(replaceMock).toHaveBeenCalledTimes(1))
    expect(replaceMock.mock.calls[0]).toEqual(['r1', '', [{ scope_type: 'self' }]])
  })

  // 自定义规则的列名/取值
  it('自定义规则要求列名与取值,缺了就当场标出来', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: 'user', scope_type: 'custom' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    expect(screen.getByPlaceholderText('列名(如 project_id)')).toBeInTheDocument()
    expect(screen.getByText('自定义范围必须指定列名')).toBeInTheDocument()
  })

  it('自定义规则列名非法时给出的是列名那条提示(不是"缺列名")', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: 'user', scope_type: 'custom', custom_column: '1abc' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    expect(screen.getByText(/列名必须是裸标识符/)).toBeInTheDocument()
    expect(screen.queryByText('自定义范围必须指定列名')).toBeNull()
  })

  // C6
  it('readOnly 时没有保存/添加/删除,只有关闭', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: 'user', scope_type: 'self' },
    ] as never)
    renderDrawer({ readOnly: true })
    await waitLoaded()

    expect(screen.queryByRole('button', { name: /保\s*存/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /删\s*除/ })).toBeNull()
    expect(screen.queryByText('添加规则')).toBeNull()
    expect(screen.getByRole('button', { name: /关\s*闭/ })).toBeInTheDocument()
    // 标题也切到"查看"
    expect(screen.getByText('查看数据范围 — ces')).toBeInTheDocument()
  })

  it('readOnly 时输入框是禁用的', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: 'user', scope_type: 'custom', custom_column: 'project_id' },
    ] as never)
    renderDrawer({ readOnly: true })
    await waitLoaded()

    expect(screen.getByPlaceholderText('列名(如 project_id)')).toBeDisabled()
  })

  // 切业务域
  it('点左侧菜单能切到别的业务域', async () => {
    listRoleScopesMock.mockResolvedValue([
      { module: 'user', scope_type: 'self' },
      { module: 'audit', scope_type: 'dept' },
    ] as never)
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('仅本人')).toBeInTheDocument()

    fireEvent.click(screen.getByText('操作日志'))
    await waitFor(() => expect(screen.getByText('本部门')).toBeInTheDocument())
    expect(screen.queryByText('仅本人')).toBeNull()
  })

  // ===== M161:维度下拉框按业务域的能力过滤 =====
  //
  // ⚠️ **后端在配置期就拒"这一维在这张表上落不下去"的组合** ——
  // 房屋表(biz_house)没有归属人列 ⇒ 「仅本人」「本部门」配不上去。
  // 前端不按 supported_types 过滤,就是让管理员配一个注定被拒的组合:
  // 用户看到的是"我明明能选",点保存却报错。
  it('维度下拉框只给这个业务域支持的类型', async () => {
    listModulesMock.mockResolvedValue(MODULES_WITH_TYPES as never)
    listRoleScopesMock.mockResolvedValue([
      { module: 'house', scope_type: 'project' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    // 切到房屋管理
    fireEvent.click(screen.getByText('房屋管理'))
    await waitFor(() => expect(screen.getByText('本项目')).toBeInTheDocument())

    await openTypeSelect()
    const labels = Array.from(document.querySelectorAll('.ant-select-item-option')).map((el) =>
      (el.textContent ?? '').trim(),
    )
    expect(labels).toContain('本项目')
    expect(labels).not.toContain('仅本人')
    expect(labels).not.toContain('本部门')
  })

  it('默认规则组的维度是所有业务域的交集(后端算好,前端照用)', async () => {
    listModulesMock.mockResolvedValue(MODULES_WITH_TYPES as never)
    // ⚠️ 默认组得先有一条规则,页面上才会有那个维度下拉框
    listRoleScopesMock.mockResolvedValue([
      { module: '', scope_type: 'all' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    await openTypeSelect()
    const labels = Array.from(document.querySelectorAll('.ant-select-item-option')).map((el) =>
      (el.textContent ?? '').trim(),
    )
    expect(labels).toContain('本项目')
    expect(labels).not.toContain('本部门')
    expect(labels).not.toContain('仅本人')
  })

  it('新增规则时默认类型是"这个业务域支持的"(不是硬编码 self)', async () => {
    listModulesMock.mockResolvedValue(MODULES_WITH_TYPES as never)
    listRoleScopesMock.mockResolvedValue([
      { module: 'house', scope_type: 'project' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    fireEvent.click(screen.getByText('房屋管理'))
    await waitFor(() => expect(screen.getByText('本项目')).toBeInTheDocument())

    fireEvent.click(screen.getByText('添加规则'))
    await waitFor(() =>
      expect(document.querySelectorAll('.ant-select-selection-item').length).toBeGreaterThan(1),
    )
    // ⚠️ house 不支持 self ⇒ 新增的那条不能默认是 self
    const items = Array.from(document.querySelectorAll('.ant-select-selection-item')).map((el) =>
      (el.textContent ?? '').trim(),
    )
    expect(items).toContain('本项目')
    expect(items).not.toContain('仅本人')
  })

  it('已存的规则里若有"现在不支持"的类型,仍要显示出来(不能看着像丢了)', async () => {
    listModulesMock.mockResolvedValue(MODULES_WITH_TYPES as never)
    listRoleScopesMock.mockResolvedValue([
      // ⚠️ 这条是"以前能配、现在交集变了配不了了"的规则
      { module: '', scope_type: 'dept' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    expect(screen.getByText('本部门')).toBeInTheDocument()
  })
})
