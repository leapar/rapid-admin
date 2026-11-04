/**
 * IconSelect 图标选择器(2026-11-02)
 *
 * ## 这条网守的是什么
 *
 *   用户报:「编辑业务集合的弹窗里,菜单图标都是文字,应该显示真实图标出来」。
 *   也就是说这个组件的**存在意义**就是"下拉里画的是真图标,不是 key"。
 *
 *   如果把 optionRender 删掉(退回纯文字下拉),功能"还能用"——
 *   用户还是能选,只是又变回记 key。这正是最容易悄悄退化的地方。
 *
 * ## 第二个用户诉求:多列网格
 *
 *   「IconSelect 改成多列显示,否则太多上下滚动麻烦」——
 *   词表扩到 ~90 个之后,单列要滚 90 行。
 *
 *   ⚠️ **网格是 CSS 实现的,不在组件里** ——
 *     规则在 `styles/global.css` 的 `.icon-select-dropdown` 段(下拉是 antd
 *     用 portal 挂到 body 上的,组件内的局部样式够不着它)。
 *     ⇒ 这条网守的是**两半的接缝**:
 *       ① 组件把 `icon-select-dropdown` 类名挂到下拉面板上(CSS 的钩子)
 *       ② global.css 里那条 grid 规则真的存在、且挂在真实存在的网格容器上
 *     两半任何一半掉了,网格就没了 ⇒ 必须变红。
 *
 *   ⚠️ **网格与虚拟滚动互斥** ——
 *     rc-select 的虚拟列表按"固定行高"绝对定位每个选项,模型是"一行一个";
 *     网格是"一格一个"。⇒ 组件里必须 `virtual={false}`,所有选项同时在 DOM 里。
 *
 * ## ⚠️ 下拉是虚拟滚动 —— 目标选项先搜出来再点
 *
 *   (注:网格化之后已经关掉虚拟滚动,这条约束只剩"搜索过滤"那半仍然成立。)
 *   「直接找某个 label」找不到 ≠ 它不存在,只是没渲染。
 *   ⇒ 测试里走**真实用户路径**:showSearch 输入关键字过滤,目标项就渲染出来了。
 *
 *   另一个坑:**重复选中当前值不触发 onChange** ——
 *   所以"选中 X"类用例的初始值必须不是 X。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 Select 的 optionRender 删掉        → 「下拉里画的是真实图标」变红
 *   · 把 onChange 的回调值改成 label         → 「选中回调的是词表 key」变红
 *   · 无条件加「无图标」项(不看 noneOption) → 「动态集合不给无图标」变红
 *   · 把 labelRender 去掉                    → 「折叠态也画真实图标」变红
 *   · 把 virtual 改回 true                   → 「所有选项同时在 DOM 里」变红
 *   · 把 classNames.popup.root 去掉          → 「下拉挂上 grid 钩子」变红
 *   · 删掉 global.css 里那条 grid 规则       → 「CSS 里真的有网格规则」变红
 */
import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent, waitFor } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'
import IconSelect from '@/components/common/IconSelect'
import { RESOURCE_ICON_KEYS } from '@/constants/resourceIcons'

/**
 * 仓库里的全局样式表路径
 *
 * ⚠️ **不能用 `new URL(相对路径, import.meta.url)`** ——
 *   vitest 的 jsdom 环境里全局 `URL` 被换掉了,拿 vite dev server 的
 *   http://localhost:5173 当 base,拼出来的是 http 地址 ⇒ fs 读不了。
 *   走 node:url 的 fileURLToPath(import.meta.url) 才是文件路径。
 */
const GLOBAL_CSS = resolve(dirname(fileURLToPath(import.meta.url)), '../../../styles/global.css')

/** 打开下拉,返回面板元素 */
async function openDropdown(container: HTMLElement): Promise<HTMLElement> {
  const selector = container.querySelector('.ant-select-selector') as HTMLElement
  expect(selector).toBeTruthy()
  fireEvent.mouseDown(selector)
  await waitFor(() =>
    expect(document.querySelector('.ant-select-dropdown:not(.ant-select-dropdown-hidden)')).toBeTruthy(),
  )
  return document.querySelector('.ant-select-dropdown:not(.ant-select-dropdown-hidden)') as HTMLElement
}

/** 面板里当前渲染出的选项元素 */
function options(dd: HTMLElement): HTMLElement[] {
  return Array.from(dd.querySelectorAll('.ant-select-item-option'))
}

/** 在搜索框里输入关键字(把目标项从 90 个里过滤出来) */
async function searchOption(container: HTMLElement, dd: HTMLElement, keyword: string) {
  const input = container.querySelector('.ant-select-selection-search-input') as HTMLInputElement
  expect(input).toBeTruthy()
  fireEvent.change(input, { target: { value: keyword } })
  await waitFor(() =>
    expect(options(dd).some((el) => (el.textContent ?? '').trim() === keyword)).toBe(true),
  )
}

/** 点面板里文案为 label 的选项 */
function pickOption(dd: HTMLElement, label: string) {
  const target = options(dd).find((el) => (el.textContent ?? '').trim() === label)
  if (!target) throw new Error(`下拉里找不到选项:${label}`)
  fireEvent.click(target)
}

describe('IconSelect', () => {
  it('下拉里画的是真实图标(选项里有 anticon,不是纯文字)', async () => {
    const { container } = render(<IconSelect value="table" onChange={() => {}} />)
    const dd = await openDropdown(container)

    const opts = options(dd)
    expect(opts.length).toBeGreaterThan(0)
    const withIcon = opts.filter((el) => el.querySelector('.anticon'))
    // ⚠️ 渲染出来的每一个选项都必须有图标 —— "有的有有的没有"也是退化
    expect(withIcon.length).toBe(opts.length)
  })

  it('选中回调的是词表 key,不是 label', async () => {
    const onChange = vi.fn()
    // ⚠️ 初始值不能是要点的那个(重复选中当前值不触发 onChange)
    const { container } = render(<IconSelect value="table" onChange={onChange} />)
    const dd = await openDropdown(container)

    await searchOption(container, dd, '系统')
    pickOption(dd, '系统')
    expect(onChange).toHaveBeenCalledWith('system')
  })

  it('折叠态显示的是 label 文本(搜索/无障碍都靠它)', () => {
    const { container } = render(<IconSelect value="table" onChange={() => {}} />)
    const item = container.querySelector('.ant-select-selection-item')
    expect((item?.getAttribute('title') ?? item?.textContent ?? '').trim()).toBe('业务表')
  })

  it('动态集合用法:不传 noneOption ⇒ 下拉里没有「无图标」,搜「业务表」有', async () => {
    const { container } = render(<IconSelect value="table" onChange={() => {}} />)
    const dd = await openDropdown(container)

    // 「无图标」在词表之外,只有 noneOption 才加 ⇒ 全量选项里也不该有
    const texts = options(dd).map((el) => (el.textContent ?? '').trim())
    expect(texts).not.toContain('无图标')

    // 词表里的项搜得到(这条顺带守"选项来自词表")
    await searchOption(container, dd, '业务表')
  })

  it('资源管理用法:传 noneOption ⇒ 有「无图标」项,选中回调哨兵值', async () => {
    const onChange = vi.fn()
    // 初始值不是 __none__,选中它才会回调
    const { container } = render(
      <IconSelect
        value="system"
        onChange={onChange}
        noneOption={{ value: '__none__', label: '无图标' }}
      />,
    )
    const dd = await openDropdown(container)

    await searchOption(container, dd, '无图标')
    pickOption(dd, '无图标')
    expect(onChange).toHaveBeenCalledWith('__none__')
  })
})

describe('IconSelect · 多列网格(2026-11-02)', () => {
  it('下拉面板挂上 grid 钩子类名(CSS 靠它选中面板)', async () => {
    const { container } = render(<IconSelect value="table" onChange={() => {}} />)
    const dd = await openDropdown(container)
    expect(dd.classList.contains('icon-select-dropdown')).toBe(true)
  })

  it('所有选项同时在 DOM 里(虚拟滚动与网格互斥)', async () => {
    const { container } = render(<IconSelect value="table" onChange={() => {}} />)
    const dd = await openDropdown(container)
    // ⚠️ 开着虚拟滚动时这里只有 ~10 个(视窗高度)⇒ 数量对不上就是退化了
    expect(options(dd).length).toBe(RESOURCE_ICON_KEYS.length)
  })

  it('网格容器就是 rc-select 真实渲染的那一层', async () => {
    const { container } = render(<IconSelect value="table" onChange={() => {}} />)
    const dd = await openDropdown(container)
    // rc-virtual-list 的 holder-inner 是选项的直接父元素 —— CSS 的 grid 容器
    const grid = dd.querySelector('.rc-virtual-list-holder-inner')
    expect(grid).toBeTruthy()
    expect(grid?.querySelectorAll('.ant-select-item-option').length).toBe(RESOURCE_ICON_KEYS.length)
  })

  it('磁贴是"图标在上、文字在下"的结构', async () => {
    const { container } = render(<IconSelect value="table" onChange={() => {}} />)
    const dd = await openDropdown(container)
    const tile = dd.querySelector('.icon-select-tile')
    expect(tile).toBeTruthy()
    expect(tile?.querySelector('.icon-select-tile-icon .anticon')).toBeTruthy()
    expect(tile?.querySelector('.icon-select-tile-label')?.textContent).toBe('仪表盘')
  })

  it('CSS 里真的有网格规则(删了 CSS 就该红)', async () => {
    // ⚠️ 网格规则不在组件里 —— 在全局样式表里。
    //   组件只负责挂类名;类名在、规则没了 ⇒ 网格静默失效(单列长列表)。
    const css = readFileSync(GLOBAL_CSS, 'utf8')
    // 规则必须挂在真实存在的网格容器上(见上面那条用例)
    expect(css).toContain('.rc-virtual-list-holder-inner')
    // 且这条规则里真的有 display: grid
    expect(css).toMatch(/\.icon-select-dropdown[^{]*\{[^}]*display:\s*grid/)
  })

  it('折叠态也画真实图标(不只是文字)', () => {
    const { container } = render(<IconSelect value="table" onChange={() => {}} />)
    const item = container.querySelector('.ant-select-selection-item')
    expect(item?.querySelector('.anticon')).toBeTruthy()
    expect(item?.textContent?.trim()).toBe('业务表')
  })
})
