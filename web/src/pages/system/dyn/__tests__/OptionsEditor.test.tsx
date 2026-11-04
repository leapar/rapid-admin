/**
 * 选项集编辑器 —— 「手工添加」vs「关联系统表」(2026-11-02)
 *
 * # 这一层在回答什么
 *
 * 单选 / 多选的候选项有两种来法:
 *   · 手工 —— 写死在字段定义里(值永远应该在那几个里)
 *   · 关联表 —— 读记录时现查(表里多一行,表单里就多一个选项)
 *
 * ⚠️ **「关联系统表」这一路的全部价值都建立在"读得到那张表"上** ——
 *   读不到时后端会**降级成空选项**(不报错),
 *   于是这个字段变成一个永远空着的下拉框,而用户看不出原因。
 *   ⇒ 前端必须把"这张表你读不了,缺哪个权限码"**摆在界面上**。
 *
 * # 可逆探针(拆掉实现看测试会不会红)
 *
 *   · 不可读的来源直接藏起来                        → 「不可读的来源仍然列出来」红
 *   · 不可读的来源可点(只是没提示)                 → 「不可读的来源是 disabled」红
 *   · 不提示缺哪个权限码                            → 「提示缺哪个权限码」红
 *   · 切来源时不清掉另一边的内容                    → 「两种来源互斥」红
 *   · 切到关联表时随便挑一个来源(不管读不读得了)   → 「默认挑第一个读得到的来源」红
 *   · 关联表模式下还显示候选项输入框                → 「关联表模式没有输入框」红
 *   · 候选项重复时不提示                            → 「候选项的值重复时给出提示」红
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { OptionsEditor } from '../OptionsEditor'
import type { DynRelationSource } from '@/types/dyn'

const SOURCES: DynRelationSource[] = [
  {
    key: 'user',
    label: '系统用户',
    table: 'sys_user',
    value_column: 'id',
    resource_code: 'user:list',
    readable: true,
  },
  {
    key: 'role',
    label: '系统角色',
    table: 'sys_role',
    value_column: 'id',
    resource_code: 'role:list',
    readable: false,
  },
  {
    key: 'dept',
    label: '部门',
    table: 'sys_department',
    value_column: 'id',
    resource_code: 'dept:list',
    readable: true,
  },
]

function byText(re: RegExp): HTMLElement {
  return screen.getByText(re) as HTMLElement
}

function renderEditor(props: Partial<React.ComponentProps<typeof OptionsEditor>> = {}) {
  const onChange = vi.fn()
  const utils = render(
    <OptionsEditor
      value={{ choices: [], source: '' }}
      onChange={onChange}
      sources={SOURCES}
      {...props}
    />,
  )
  return { ...utils, onChange }
}

/** 当前可见的下拉面板(antd 会留一堆隐藏的) */
function visibleDropdown(): HTMLElement | null {
  const list = Array.from(document.querySelectorAll('.ant-select-dropdown')) as HTMLElement[]
  const visible = list.filter(
    (el) =>
      !el.classList.contains('ant-select-dropdown-hidden') &&
      el.style.display !== 'none' &&
      el.style.visibility !== 'hidden',
  )
  return visible[visible.length - 1] ?? null
}

/** 打开来源下拉,返回下拉面板元素 */
async function openSourceSelect(container: HTMLElement): Promise<HTMLElement> {
  const selector = container.querySelector('.ant-select-selector') as HTMLElement
  expect(selector).toBeTruthy()
  fireEvent.mouseDown(selector)
  await waitFor(() => expect(visibleDropdown()).toBeTruthy())
  return visibleDropdown()!
}

/** 下拉里某一项 */
function optionOf(dd: HTMLElement, label: string): HTMLElement {
  const opt = Array.from(
    dd.querySelectorAll<HTMLElement>('.ant-select-item-option'),
  ).find((el) => (el.textContent ?? '').trim() === label)
  if (!opt) throw new Error(`下拉里找不到选项:${label}`)
  return opt
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('OptionsEditor · 手工模式', () => {
  it('手工模式:有候选项时每个候选项两个输入框(值 / 显示名)', () => {
    renderEditor({
      value: { choices: [{ value: 'a', label: '甲' }], source: '' },
    })
    expect(screen.getAllByRole('textbox')).toHaveLength(2)
  })

  it('手工模式:没有候选项时给提示 + 一个「添加候选项」按钮', () => {
    renderEditor()
    expect(byText(/还\s*没\s*有\s*候\s*选\s*项/)).toBeInTheDocument()
    expect(byText(/添\s*加\s*候\s*选\s*项/)).toBeInTheDocument()
    // 一个候选项都没有时,输入框一个都不渲染(先点「添加候选项」)
    expect(screen.queryAllByRole('textbox')).toHaveLength(0)
  })

  it('手工模式:不显示来源下拉', () => {
    const { container } = renderEditor()
    expect(container.querySelector('.ant-select-selector')).toBeNull()
  })

  /**
   * ⚠️ **重复值必须挡住** ——
   *   重复的选项值会让"这条记录的单选值是哪个"变得有歧义,
   *   而这种歧义在数据存进去之后是**查不出来**的。
   */
  it('候选项的值重复时给出提示', () => {
    renderEditor({
      value: {
        choices: [
          { value: 'a', label: '甲' },
          { value: 'a', label: '乙' },
        ],
        source: '',
      },
    })
    expect(byText(/值\s*重\s*复\s*了/)).toBeInTheDocument()
  })
})

describe('OptionsEditor · 关联表模式', () => {
  it('关联表模式:没有候选项输入框(两种来源互斥)', () => {
    const { container } = renderEditor({ value: { choices: [], source: 'user' } })
    expect(screen.queryAllByRole('textbox')).toHaveLength(0)
    expect(container.querySelector('.ant-select-selector')).toBeTruthy()
  })

  it('关联表模式:说清楚选项是从哪张表的哪一列来的', () => {
    renderEditor({ value: { choices: [], source: 'user' } })
    const text = document.body.textContent ?? ''
    expect(text).toContain('sys_user')
    expect(text).toContain('id')
  })

  /**
   * ⚠️ **不可读的来源必须"看得见但点不了",不能藏** ——
   *   藏起来的话用户会以为"系统里只有这几张表能选",
   *   而真实原因是"你这个账号没有那张表的权限"。
   */
  it('不可读的来源仍然列出来,但不可点', async () => {
    const { container } = renderEditor({ value: { choices: [], source: 'user' } })
    const dd = await openSourceSelect(container)
    const unreadable = optionOf(dd, '系统角色(role:list)')
    expect(unreadable.className).toContain('ant-select-item-option-disabled')
    const readable = optionOf(dd, '系统用户')
    expect(readable.className).not.toContain('ant-select-item-option-disabled')
  })

  /**
   * ⚠️ **必须提示缺哪个权限码** ——
   *   配了读不了的来源时,后端会**降级成空选项**(不报错),
   *   界面上就变成一个永远空着的下拉框 —— 用户看不出原因。
   */
  it('当前来源读不了时,提示缺哪个权限码', () => {
    renderEditor({ value: { choices: [], source: 'role' } })
    const text = document.body.textContent ?? ''
    expect(text).toContain('role:list')
  })

  it('当前来源读得了时,不提示缺权限码', () => {
    renderEditor({ value: { choices: [], source: 'user' } })
    const text = document.body.textContent ?? ''
    expect(text).not.toContain('user:list')
  })
})

describe('OptionsEditor · 切来源', () => {
  it('切到「关联系统表」:清掉手工候选项,默认挑第一个读得到的来源', () => {
    const { onChange } = renderEditor({
      value: { choices: [{ value: 'a', label: '甲' }], source: '' },
    })
    fireEvent.click(byText(/关\s*联\s*系\s*统\s*表/))
    expect(onChange).toHaveBeenCalledWith({ choices: [], source: 'user' })
  })

  it('切回「手工添加」:清掉来源', () => {
    const { onChange } = renderEditor({ value: { choices: [], source: 'user' } })
    fireEvent.click(byText(/手\s*工\s*添\s*加/))
    expect(onChange).toHaveBeenCalledWith({ choices: [], source: '' })
  })

  /**
   * ⚠️ **默认挑的那个来源必须读得了** ——
   *   默认挑一个读不了的来源 ⇒ 用户还没做任何事,
   *   这个字段就已经是"配了但取不到选项"的状态了。
   */
  it('切到「关联系统表」时挑的是读得到的那个(不是第一个)', () => {
    const { onChange } = renderEditor({
      value: { choices: [], source: '' },
      sources: [
        { ...SOURCES[0]!, readable: false },
        { ...SOURCES[2]!, readable: true },
      ],
    })
    fireEvent.click(byText(/关\s*联\s*系\s*统\s*表/))
    expect(onChange).toHaveBeenCalledWith({ choices: [], source: 'dept' })
  })

  it('选一个来源:交出 {choices:[], source:key}', async () => {
    const { container, onChange } = renderEditor({ value: { choices: [], source: 'user' } })
    const dd = await openSourceSelect(container)
    fireEvent.click(optionOf(dd, '部门'))
    await waitFor(() => expect(onChange).toHaveBeenCalled())
    expect(onChange).toHaveBeenLastCalledWith({ choices: [], source: 'dept' })
  })
})
