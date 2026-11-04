/**
 * 「点整行进入」的保护(纯函数)
 *
 * ## 这一组守的是什么
 *
 *   这层是**框架原语** —— 表格的行、卡片的整张卡都用它。
 *   它错的方式很隐蔽:保护**漏了一个元素** ⇒ 点那个元素会顺手做两件事
 *   (比如点「删除」顺手把文件夹打开);保护**挡多了** ⇒ 本该能进的地方点不动。
 *
 *   而且页面级用例只能覆盖**那几个页面用到的那几个元素** ——
 *   选择器里少写一个类名,页面级用例照样全绿(它根本没点到那个元素)。
 *   ⇒ 判据本身必须单独钉住。
 *
 * ## 可逆探针
 *
 *   · 把选择器里的 `a` 去掉 → 「链接算交互元素」红;
 *   · 把 `.ant-dropdown` 去掉 → 「portal 里的菜单项算交互元素」红;
 *   · 把 `enterOnRow` 里的 `if (isInteractiveTarget(e.target)) return` 删掉
 *     → 「点按钮不进入」红;
 *   · 把 `style.cursor` 删掉 → 「行上有 pointer 光标」红。
 */
import { describe, it, expect, vi } from 'vitest'
import { isInteractiveTarget, enterOnRow } from '../rowClick'

/** 在 body 里造一个元素(带可选的类名 / 祖先) */
function makeEl(html: string): HTMLElement {
  const host = document.createElement('div')
  host.innerHTML = html
  const el = host.firstElementChild as HTMLElement
  document.body.appendChild(host)
  return el
}

describe('isInteractiveTarget —— 判定"这次点击该不该被行处理器接住"', () => {
  it('没有 target(理论上不会发生)→ 不接', () => {
    expect(isInteractiveTarget(null)).toBe(false)
  })

  it('普通元素(行里的文字 / 数字)→ 接', () => {
    const td = makeEl('<td>12</td>')
    expect(isInteractiveTarget(td)).toBe(false)
  })

  it('链接 → 不接(名字链接自己会处理)', () => {
    const a = makeEl('<a href="#">户型图</a>')
    expect(isInteractiveTarget(a)).toBe(true)
  })

  it('按钮 → 不接(RowActions / CardActionsMenu)', () => {
    const btn = makeEl('<button type="button">删除</button>')
    expect(isInteractiveTarget(btn)).toBe(true)
  })

  it('标签 → 不接(所在目录 / 默认 / 数量)', () => {
    const tag = makeEl('<span class="ant-tag">root</span>')
    expect(isInteractiveTarget(tag)).toBe(true)
  })

  it('封面预览 → 不接(点它会放大大图)', () => {
    const img = makeEl('<span class="ant-image"><img src="/x.png" /></span>')
    expect(isInteractiveTarget(img.querySelector('img'))).toBe(true)
  })

  it('portal 里的下拉菜单项 → 不接(DOM 上不在行里,但事件会冒泡回来)', () => {
    const item = makeEl(
      '<div class="ant-dropdown"><ul class="ant-dropdown-menu"><li class="ant-dropdown-menu-item">删除</li></ul></div>',
    )
    const li = item.querySelector('li')!
    expect(isInteractiveTarget(li)).toBe(true)
  })

  it('交互元素的**子元素**也算(点按钮里面的文字一样要点到按钮)', () => {
    const btn = makeEl('<button type="button"><span>删除</span></button>')
    expect(isInteractiveTarget(btn.querySelector('span'))).toBe(true)
  })

  it('没有 closest 的东西(不是元素)→ 不接,不炸', () => {
    expect(isInteractiveTarget({} as unknown as EventTarget)).toBe(false)
  })
})

describe('enterOnRow —— 行 props 工厂', () => {
  it('普通点击 → 调用 enter,并带上 pointer 光标与可点的标记', () => {
    const enter = vi.fn()
    const props = enterOnRow<{ id: string }>(enter)({ id: '1' })

    expect(props.className).toContain('row-clickable')
    expect(props.style?.cursor).toBe('pointer')

    props.onClick?.({ target: makeEl('<td>12</td>') } as never)
    expect(enter).toHaveBeenCalledTimes(1)
    expect(enter).toHaveBeenCalledWith({ id: '1' })
  })

  it('点在按钮上 → **不**调用 enter', () => {
    const enter = vi.fn()
    const props = enterOnRow<{ id: string }>(enter)({ id: '1' })

    props.onClick?.({ target: makeEl('<button type="button">删除</button>') } as never)
    expect(enter).not.toHaveBeenCalled()
  })
})
