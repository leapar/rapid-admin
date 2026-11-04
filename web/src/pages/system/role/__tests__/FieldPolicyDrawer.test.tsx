/**
 * 字段可见性抽屉 渲染测试 —— M164(2026-11-02)
 *
 * ## 这条链上"错了没人知道"的地方
 *
 * 字段级可见性是**列级权限** —— 配错了不会报错,只会**静默地**
 * 让该角色看到本该藏起来的字段。
 *
 * 前端这一层要挡住的是五种"静默":
 *
 *   1. **字段名拼错**      → 候选项必须来自后端,不许前端手打
 *   2. **发空数组当增量**  → 写接口是全量替换,发空 = 清空
 *   3. **发了没改的域**    → 无谓地覆写别的域的规则
 *   4. **把 UI 哨兵当业务域** → 菜单里默认组的 key 是 '@default',
 *      存库的 module 必须是空串。发错 ⇒ 后端直接拒收("业务域 @default 不存在")。
 *   5. **把"跟随默认组"当成"明文可见"** → 前者=继承默认组的限制,
 *      后者=覆盖默认组的限制(让它重新可见)。混成一个 ⇒ 限制被悄悄顶掉。
 *
 * ## ⚠️ 「跟随默认组」与「明文可见」的区别是这一层最要紧的一处
 *
 * 默认规则组(module="")与业务域规则是**逐字段叠加**(不是整组覆盖):
 * 业务域里写了规则的字段覆盖默认组里**同一个字段**的规则,其它字段仍走默认组。
 *
 * ⇒ 业务域里把某个字段设成「明文可见」= **覆盖**默认组对它的那条限制
 *   (它在那个业务域上重新可见);
 *   设成「跟随默认组」= **不发规则**,该字段继续走默认组。
 *
 * 这两件事在界面上是两个不同的下拉选项,在草稿里是两个不同的状态
 * (一个 key 不在 map 里 / 一个 key 在 map 里且 mode=visible)。
 *
 * ## 可逆探针(拆掉实现看测试会不会红)
 *
 *   · setMode 把「跟随默认组」当成 visible 存      → 「改回跟随默认组」红
 *   · save 改成全量发(所有域都 PUT)               → 「只发改动过的域」红
 *   · save 把菜单 key 当 module 发                 → 「module 必须是空串」红
 *   · 表里只渲染候选字段(不渲染已配但已失效的字段) → 「已配但已失效的字段」红
 *   · findPolicyIssues 的结果不阻止保存             → 「有配不合法的东西时阻止保存」红
 *   · 去掉「逐字段叠加」那条提示                    → 「逐字段叠加提示」红
 *   · 去掉「隐藏 = 删键」那条提示                   → 「隐藏 = 删键提示」红
 *   · 打开时永远落在默认组                          → 「落在第一个有规则的域」红
 *   · readOnly 下仍然显示保存按钮                    → 「readOnly 没有保存按钮」红
 *   · 切到「打码」时不预填推荐的打码方式              → 「预填推荐打码方式」红
 *   · buildModuleOptions 不补默认组                 → 「后端没给默认组时补一个」红
 *   · buildModuleOptions 不把默认组提到第一位          → 「默认组排第一」红
 *   · groupByModule 用 'default' 当默认组的 key       → 「默认组的 key 是空串」红
 *   · toInputs 无条件带上 mask_type                  → 「mask_type 只在打码时发」红
 *   · validatePolicy 去掉「猜不出方式」那条            → 「打码但猜不出方式」红
 *
 * ### ⚠️ 这里踩过一次假绿,值得写下来
 *
 * 「只发改动过的业务域」那条用例最初写成
 * `await waitFor(() => expect(replaceMock).toHaveBeenCalledTimes(1))` ——
 * **探针把 save 改成全量发,它照样绿**。
 *
 * 原因:PUT 是**顺序发**的,`waitFor` 断言第一次成立就返回,
 * 那一刻**第二个 PUT 还没发出去** ⇒ 断言抓到的是**中间态**,
 * "只发一个"和"发两个"在这个瞬间长得一模一样。
 *
 * ⇒ 改成等**收尾信号**(抽屉的 `onClose`,它在所有 PUT 发完之后才被调用),
 *   断言才是对**最终状态**的断言。
 *   —— 这一条与 memory 里「等请求 ≠ 等渲染」是同一类病。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { App } from 'antd'
import { FieldPolicyDrawer } from '../FieldPolicyDrawer'
import { fieldPolicyApi } from '@/api/fieldPolicy'
import type { FieldPolicyModule } from '@/api/fieldPolicy'

vi.mock('@/api/fieldPolicy', () => ({
  fieldPolicyApi: {
    listModules: vi.fn(),
    listRoleFieldPolicies: vi.fn(),
    replaceRoleFieldPolicies: vi.fn(),
  },
}))

const listModulesMock = vi.mocked(fieldPolicyApi.listModules)
const listRoleFieldPoliciesMock = vi.mocked(fieldPolicyApi.listRoleFieldPolicies)
const replaceMock = vi.mocked(fieldPolicyApi.replaceRoleFieldPolicies)

const ROLE = { id: 'r1', code: 'ces', name: 'ces' } as never

/**
 * 后端给的候选
 *
 * ⚠️ **默认组的 label 是后端硬编码的中文串** ——
 *   页面必须用前端自己的 i18n(否则英文界面下露出中文)。
 */
const MODULES: FieldPolicyModule[] = [
  {
    module: '',
    label: '默认(适用于所有业务域)',
    source: 'default',
    fields: [
      { field: 'phone', label: 'phone', mask_type: 'phone', sensitive: true },
      { field: 'real_name', label: 'real_name', mask_type: 'name', sensitive: true },
      { field: 'address', label: 'address', mask_type: '', sensitive: false },
    ],
  },
  {
    module: 'owner',
    label: '业主信息',
    source: 'api',
    fields: [
      { field: 'real_name', label: 'real_name', mask_type: 'name', sensitive: true },
      { field: 'phone', label: 'phone', mask_type: 'phone', sensitive: true },
      { field: 'address', label: 'address', mask_type: '', sensitive: false },
    ],
  },
]

function renderDrawer(props: Partial<ComponentProps<typeof FieldPolicyDrawer>> = {}) {
  // ⚠️ **onClose 必须是 spy** —— 它是"保存流程整个走完了"的信号,见 waitSaved。
  const onClose = vi.fn()
  const utils = render(
    <App>
      <FieldPolicyDrawer open role={ROLE} onClose={onClose} {...props} />
    </App>,
  )
  return { ...utils, onClose }
}

/** 等抽屉内容渲染出来(左侧菜单出来了 = 加载完了) */
async function waitLoaded() {
  await waitFor(() => expect(screen.getByText('默认(全部业务域)')).toBeInTheDocument(), {
    timeout: 15000,
  })
}

/**
 * 某字段所在的表格行
 *
 * ⚠️ **不能按文本找** —— rc-select 会在无障碍隐藏列表里放一个**原值**的
 *   role="option"(值是 "phone" 时它就渲染成 "phone"),
 *   同一个词在 DOM 里出现两次 ⇒ getByText 报"找到多个"。
 *   ⇒ 用组件给的类名钩子 `.field-policy-field-name` 精确指认。
 */
function rowOf(field: string): HTMLElement {
  const name = Array.from(document.querySelectorAll('.field-policy-field-name')).find(
    (el) => (el.textContent ?? '').trim() === field,
  )
  const tr = name?.closest('tr')
  if (!tr) throw new Error(`找不到字段 ${field} 所在的行`)
  return tr as HTMLElement
}

/** 某行"可见性"下拉框当前显示的值 */
function modeValueOf(field: string): string {
  const item = rowOf(field).querySelector('.ant-select-selection-item')
  return (item?.textContent ?? '').trim()
}

/** 当前可见的下拉(没关的那些) */
function visibleDropdowns(): HTMLElement[] {
  return Array.from(
    document.querySelectorAll('.ant-select-dropdown:not(.ant-select-dropdown-hidden)'),
  )
}

/** 当前可见的下拉里的选项 */
function visibleOptions(): HTMLElement[] {
  const out: HTMLElement[] = []
  for (const dd of visibleDropdowns()) {
    out.push(...Array.from(dd.querySelectorAll<HTMLElement>('.ant-select-item-option')))
  }
  return out
}

/**
 * 打开某一行里的第 n 个下拉框(0 = 可见性,1 = 打码方式)
 *
 * ⚠️ **先等上一个下拉关掉再开新的** ——
 *   antd 的下拉是 portal 挂到 body 上的,关掉有一段动画,
 *   关的过程中它还带着"可见"状态 ⇒ 后面 `querySelector` 会拿到**上一个**的下拉,
 *   于是"打开打码方式"实际拿到的是"可见性"的下拉(选项完全不对)。
 */
async function openSelect(field: string, index = 0) {
  await waitFor(() => expect(visibleDropdowns()).toHaveLength(0), { timeout: 15000 })
  const selector = rowOf(field).querySelectorAll('.ant-select-selector')[index] as HTMLElement
  expect(selector).toBeTruthy()
  fireEvent.mouseDown(selector)
  await waitFor(() => expect(visibleDropdowns().length).toBeGreaterThan(0), { timeout: 15000 })
}

/** 点当前打开的下拉里的某个选项 */
async function pickOption(label: string) {
  await waitFor(
    () =>
      expect(
        visibleOptions().some((el) => (el.textContent ?? '').trim() === label),
      ).toBe(true),
    { timeout: 15000 },
  )
  const option = visibleOptions().find((el) => (el.textContent ?? '').trim() === label)!
  fireEvent.click(option)
}

/** 在某一行里选可见性模式 */
async function pickMode(field: string, label: string) {
  await openSelect(field, 0)
  await pickOption(label)
  await waitFor(() => expect(modeValueOf(field)).toBe(label), { timeout: 15000 })
}

/** 点「保存」 */
function clickSave() {
  // ⚠️ antd 会给两个汉字的按钮文案自动插空格 ⇒ 匹配用 /保\s*存/
  fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }))
}

/**
 * 等保存**整个流程**走完
 *
 * ⚠️ **不能只等"第一个 PUT 发出去了"** ——
 *   PUT 是**顺序发**的(一个业务域一次)。`waitFor` 断言第一次成立就返回,
 *   而那一刻第二个 PUT 可能还没发出去 ⇒
 *   "只发改动过的域"和"全量发所有域"两种实现**长得一模一样**,
 *   这条用例就守不住任何东西。
 *   (实测踩过:把 save 改成全量发,`toHaveBeenCalledTimes(1)` 这条断言照样绿 ——
 *    因为它抓到的是**中间态**。)
 *   ⇒ 等**收尾信号**:抽屉的 onClose —— 它在所有 PUT 发完之后才被调用。
 */
async function waitSaved(onClose: ReturnType<typeof vi.fn>) {
  await waitFor(() => expect(onClose).toHaveBeenCalled(), { timeout: 15000 })
}

describe('FieldPolicyDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listModulesMock.mockResolvedValue(MODULES as never)
    listRoleFieldPoliciesMock.mockResolvedValue([] as never)
    replaceMock.mockResolvedValue(undefined as never)
  })

  // C1
  it('业务域候选项来自后端,默认组排第一且用前端自己的文案', async () => {
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('业主信息')).toBeInTheDocument()
    // ⚠️ 默认组的标签用前端 i18n —— 后端那个是硬编码中文(英文界面会露出来)
    expect(screen.getByText('默认(全部业务域)')).toBeInTheDocument()
    expect(screen.queryByText('默认(适用于所有业务域)')).toBeNull()
  })

  it('后端一个业务域都没给时,也仍然有默认组可配', async () => {
    listModulesMock.mockResolvedValue([] as never)
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('默认(全部业务域)')).toBeInTheDocument()
  })

  // 候选字段来自后端,前端不许手打
  it('字段候选项来自后端(页面列出后端给的字段)', async () => {
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('phone')).toBeInTheDocument()
    expect(screen.getByText('real_name')).toBeInTheDocument()
    expect(screen.getByText('address')).toBeInTheDocument()
  })

  // C2
  it('打开时落在第一个有规则的业务域上(不是永远落在默认组)', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: 'owner', field: 'phone', mode: 'masked', mask_type: 'phone' },
    ] as never)
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('这是单个业务域')).toBeInTheDocument()
    expect(screen.queryByText('这是「默认规则组」')).toBeNull()
  })

  it('没有规则时落在默认组', async () => {
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('这是「默认规则组」')).toBeInTheDocument()
  })

  // C3
  it('「逐字段叠加」这条语义提示一直在', async () => {
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('默认组与业务域是「逐字段叠加」')).toBeInTheDocument()
  })

  it('「隐藏 = 删键」这条提示也在(不是置空)', async () => {
    renderDrawer()
    await waitLoaded()
    expect(screen.getByText('「隐藏」是从响应里删掉这个键')).toBeInTheDocument()
  })

  // C4
  it('保存只发改动过的业务域(没改的域一个都不发)', async () => {
    // ⚠️ **两个业务域都要有规则** —— 只配一个域时,"全量发"和"只发改动的"发出来的
    //   请求是一样的,这条用例就守不住任何东西。
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: '', field: 'phone', mode: 'hidden' },
      { module: 'owner', field: 'phone', mode: 'masked', mask_type: 'phone' },
    ] as never)
    const { onClose } = renderDrawer()
    await waitLoaded()
    // 落在默认组(第一个有规则的域)
    expect(screen.getByText('这是「默认规则组」')).toBeInTheDocument()

    // 切到「业主信息」改一个字段 —— 默认组一个字都没动
    fireEvent.click(screen.getByText('业主信息'))
    await waitFor(() => expect(screen.getByText('这是单个业务域')).toBeInTheDocument())

    await pickMode('real_name', '隐藏')
    clickSave()
    // ⚠️ 等**收尾**,不等"第一个 PUT" —— PUT 是顺序发的,
    //   只等第一个的话,"全量发所有域"这种实现也会被抓成绿。
    await waitSaved(onClose)
    expect(replaceMock).toHaveBeenCalledTimes(1)
    expect(replaceMock.mock.calls[0]?.[0]).toBe('r1')
    expect(replaceMock.mock.calls[0]?.[1]).toBe('owner')
  })

  it('什么都没改就保存 ⇒ 不发任何 PUT', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: 'owner', field: 'phone', mode: 'masked', mask_type: 'phone' },
    ] as never)
    const { onClose } = renderDrawer()
    await waitLoaded()

    clickSave()
    await waitSaved(onClose)
    expect(screen.getByText('没有改动')).toBeInTheDocument()
    expect(replaceMock).not.toHaveBeenCalled()
  })

  // C5 —— 最阴的一条:菜单 key ≠ 存储的 module
  it('默认规则组保存时 module 必须是空串,不是菜单里的哨兵 key', async () => {
    const { onClose } = renderDrawer()
    await waitLoaded()

    await pickMode('phone', '打码')
    clickSave()
    await waitSaved(onClose)
    expect(replaceMock).toHaveBeenCalledTimes(1)
    // ⚠️ 第二段必须是 '' —— 菜单里默认组的 key 是 '@default'(UI 哨兵),
    //   发出去的话后端会判"业务域 @default 不存在"直接拒收。
    expect(replaceMock.mock.calls[0]?.[1]).toBe('')
    expect(replaceMock.mock.calls[0]?.[2]).toEqual([
      { field: 'phone', mode: 'masked', mask_type: 'phone' },
    ])
  })

  // C6 —— 「跟随默认组」与「明文可见」不是一回事
  it('把字段改回「跟随默认组」⇒ 删掉这条规则(发空数组)', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: 'owner', field: 'phone', mode: 'masked', mask_type: 'phone' },
    ] as never)
    const { onClose } = renderDrawer()
    await waitLoaded()
    expect(modeValueOf('phone')).toBe('打码')

    await pickMode('phone', '跟随默认组')
    clickSave()
    await waitSaved(onClose)
    expect(replaceMock).toHaveBeenCalledTimes(1)
    // ⚠️ 必须是空数组 —— 留着一条 visible 规则就等于**覆盖**默认组,
    //   那个字段会在本业务域上重新可见(限制被悄悄顶掉)。
    expect(replaceMock.mock.calls[0]?.[2]).toEqual([])
  })

  it('选「明文可见」⇒ 发一条显式的 visible 规则(与"跟随默认组"相反)', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: 'owner', field: 'phone', mode: 'masked', mask_type: 'phone' },
    ] as never)
    const { onClose } = renderDrawer()
    await waitLoaded()

    await pickMode('phone', '明文可见')
    clickSave()
    await waitSaved(onClose)
    expect(replaceMock).toHaveBeenCalledTimes(1)
    expect(replaceMock.mock.calls[0]?.[2]).toEqual([{ field: 'phone', mode: 'visible' }])
  })

  // C7
  it('readOnly 时没有保存按钮,只有关闭', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: 'owner', field: 'phone', mode: 'masked', mask_type: 'phone' },
    ] as never)
    renderDrawer({ readOnly: true })
    await waitLoaded()

    expect(screen.queryByRole('button', { name: /保\s*存/ })).toBeNull()
    expect(screen.getByRole('button', { name: /关\s*闭/ })).toBeInTheDocument()
    expect(screen.getByText('查看字段可见性 — ces')).toBeInTheDocument()
  })

  it('readOnly 时下拉框是禁用的', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: 'owner', field: 'phone', mode: 'masked', mask_type: 'phone' },
    ] as never)
    renderDrawer({ readOnly: true })
    await waitLoaded()

    // ⚠️ antd 把 disabled 类挂在 **.ant-select 根**上,不是 .ant-select-selector
    const root = rowOf('phone').querySelectorAll('.ant-select')[0]
    expect(root?.className).toContain('ant-select-disabled')
    expect(rowOf('phone').querySelector('input')?.disabled).toBe(true)
  })

  // C8
  it('切到「打码」时预填推荐的打码方式(猜不出来时兜底整串打码)', async () => {
    renderDrawer()
    await waitLoaded()

    await pickMode('phone', '打码')
    // phone 的推荐方式是 phone
    const current = rowOf('phone').querySelectorAll('.ant-select-selection-item')[1]
    expect((current?.textContent ?? '').trim()).toBe('手机号')

    // address 猜不出来 ⇒ 兜底「整串打码」
    await pickMode('address', '打码')
    const current2 = rowOf('address').querySelectorAll('.ant-select-selection-item')[1]
    expect((current2?.textContent ?? '').trim()).toBe('整串打码')
  })

  // 打码方式下拉只在「打码」时出现
  it('不是「打码」时不显示打码方式下拉框', async () => {
    renderDrawer()
    await waitLoaded()
    expect(rowOf('phone').querySelectorAll('.ant-select-selector')).toHaveLength(1)

    await pickMode('phone', '打码')
    expect(rowOf('phone').querySelectorAll('.ant-select-selector')).toHaveLength(2)
  })

  // 「自动」只在猜得出来时才有
  it('猜不出打码方式的字段不给「自动」这一项', async () => {
    renderDrawer()
    await waitLoaded()
    await pickMode('address', '打码')
    await openSelect('address', 1)
    const labels = visibleOptions().map((el) => (el.textContent ?? '').trim())
    expect(labels).not.toContain('自动(按字段名推断)')
    expect(labels).toContain('整串打码')
  })

  it('猜得出打码方式的字段给「自动」这一项(后端会按字段名推断)', async () => {
    renderDrawer()
    await waitLoaded()
    await pickMode('phone', '打码')
    await openSelect('phone', 1)
    const labels = visibleOptions().map((el) => (el.textContent ?? '').trim())
    expect(labels).toContain('自动(按字段名推断)')
  })

  // C10
  it('已配但字段已不在候选里的规则也要渲染出来(否则删不掉,永远存不下)', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: '', field: 'legacy_field', mode: 'hidden' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    expect(screen.getByText('legacy_field')).toBeInTheDocument()
    expect(screen.getByText('候选已不存在')).toBeInTheDocument()
    expect(
      screen.getByText('这个字段已经不在该业务域的候选里,请删除这条规则'),
    ).toBeInTheDocument()
  })

  // C11
  it('有配不合法的东西时阻止保存(后端拒的是整组,页面上先说清是哪一条)', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: '', field: 'legacy_field', mode: 'hidden' },
      { module: '', field: 'phone', mode: 'hidden' },
    ] as never)
    renderDrawer()
    await waitLoaded()

    // ⚠️ 改一个**合法**的字段 —— 写接口是全量替换,这次 PUT 会带上整个默认组,
    //   组里那条失效字段会让后端**拒掉整组** ⇒ 页面上必须先拦下来。
    await pickMode('phone', '打码')
    clickSave()
    await waitFor(() => expect(screen.getByText(/配置不合法/)).toBeInTheDocument(), {
      timeout: 15000,
    })
    expect(replaceMock).not.toHaveBeenCalled()
  })

  it('把失效字段的规则删掉(改回"未配置")之后就能保存了', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: '', field: 'legacy_field', mode: 'hidden' },
    ] as never)
    const { onClose } = renderDrawer()
    await waitLoaded()

    // ⚠️ 默认规则组里没有"跟随默认组"这一项(上面没有东西可跟)——
    //   那一项的文案是「未配置(明文可见)」
    // ⚠️ 选它 = **删掉这条规则** ⇒ 这一行整个消失
    //   (字段不在候选里,规则一删就没有理由再显示它)
    await openSelect('legacy_field', 0)
    await pickOption('未配置(明文可见)')
    await waitFor(() => expect(screen.queryByText('legacy_field')).toBeNull(), { timeout: 15000 })

    clickSave()
    await waitSaved(onClose)
    expect(replaceMock).toHaveBeenCalledTimes(1)
    expect(replaceMock.mock.calls[0]?.[1]).toBe('')
    expect(replaceMock.mock.calls[0]?.[2]).toEqual([])
  })

  // 切业务域
  it('点左侧菜单能切到别的业务域', async () => {
    listRoleFieldPoliciesMock.mockResolvedValue([
      { module: '', field: 'phone', mode: 'hidden' },
    ] as never)
    renderDrawer()
    await waitLoaded()
    expect(modeValueOf('phone')).toBe('隐藏')

    fireEvent.click(screen.getByText('业主信息'))
    await waitFor(() => expect(screen.getByText('这是单个业务域')).toBeInTheDocument())
    // owner 域没配 phone ⇒ 回到"跟随默认组"
    expect(modeValueOf('phone')).toBe('跟随默认组')
  })
})
