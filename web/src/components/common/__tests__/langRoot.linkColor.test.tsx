/**
 * 主题 token —— **link 按钮的颜色**(2026-10-31)
 *
 * ## 这条网守的是什么
 *
 * antd 的 `colorLink` **不设 ≠ 跟随主色**,而是**回落到 `seed.colorInfo`**:
 *
 * ```
 * antd/lib/theme/themes/shared/genColorMapToken.js:28
 *   const colorLink = seed.colorLink || seed.colorInfo
 * ```
 *
 * 而 `seed.colorInfo` 的默认值是 **#1677ff 蓝**。
 *
 * ⇒ 本仓设计系统里**一个蓝都没有**(variables.css 只有橙 / 灰 / 红 / 绿),
 *   但凡渲染了一个 `type="link"` 的按钮 —— 表格操作列(`RowActions`,
 *   20 个业务页在用)、空态的「重试」、搜索区的「展开/收起」、批量条的「清除」
 *   —— 就会在整站冒出一片**外来的蓝**。
 *
 * 用户 2026-10-31 报的「文字都是蓝色的」就是它。
 *
 * ⚠️ **为什么这条网在主题层,不在页面层**:改页面只能治标
 *   —— 下次谁再写一个 link 按钮,蓝色又回来了。而 `colorLink` 是**唯一**的
 *   决策点,改一次全站生效。
 */
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { theme } from 'antd'
import { LangRoot } from '@/components/common/LangRoot'
import { getSkin } from '@/theme/registry'

/** 探针 —— 把主题算出来的 token 吐到 DOM 上(jsdom 里读不到 CSS 变量) */
function TokenProbe() {
  const { token } = theme.useToken()
  return (
    <div>
      <span data-testid="colorLink">{token.colorLink}</span>
      <span data-testid="colorLinkHover">{token.colorLinkHover}</span>
      <span data-testid="colorLinkActive">{token.colorLinkActive}</span>
    </div>
  )
}

describe('主题 · link 按钮颜色', () => {
  it('colorLink 是品牌橙,不是 antd 默认的 #1677ff 蓝', () => {
    render(
      <LangRoot>
        <TokenProbe />
      </LangRoot>,
    )

    // ⚠️ 断言的是 **#ff6600** 而不是 '#f60' —— antd 会把颜色**解析再序列化**
    //   (theme 内部走 @ctrl/tinycolor),'#f60' 被规范化成六位形式。
    //   两种写法指的是同一个颜色。
    expect(screen.getByTestId('colorLink').textContent).toBe('#ff6600')
    // 反断言:antd 的默认蓝(不设 colorLink 时的回落值)
    expect(screen.getByTestId('colorLink').textContent).not.toBe('#1677ff')
  })

  it('hover / active 也走品牌色那一档(不能掉回蓝)', () => {
    render(
      <LangRoot>
        <TokenProbe />
      </LangRoot>,
    )

    // 期望值不再硬编码:皮肤色值调整时这里自动跟随(getSkin('white'))——
    // 本测试网的是"link 三态必须是品牌色,不能回落 antd 蓝"
    const { brand } = getSkin('white')
    expect(screen.getByTestId('colorLinkHover').textContent).toBe(brand.hover)
    expect(screen.getByTestId('colorLinkActive').textContent).toBe(brand.active)
    expect(screen.getByTestId('colorLinkHover').textContent).not.toBe('#1677ff')
    expect(screen.getByTestId('colorLinkActive').textContent).not.toBe('#1677ff')
  })
})
