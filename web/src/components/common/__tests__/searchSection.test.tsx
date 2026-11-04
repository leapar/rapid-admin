/**
 * SearchSection 回归网(2026-10-29 M111)
 *
 * 搜索区容器(浅灰底 + L1 阴影 + 圆角 14 + hover 升级 L2)。
 * 业务上是"每个列表页搜索区的统一外观",框架上是两个调用方
 * (ManagementTable / system-config)共用的壳。
 *
 * 锁住的:
 *   · 子元素原样渲染
 *   · hover / 离开时阴影与描边切换(L1 ⇄ L2)
 *   · `style` 追加在默认样式之后(能覆盖)
 *   · ⚠️ **`fullWidth` 已收回**(以前声明了但组件体从来没读过)
 */
import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { SearchSection } from '@/components/common/SearchSection'

describe('SearchSection', () => {
  it('渲染子元素', () => {
    render(
      <SearchSection>
        <button data-testid="btn">查询</button>
      </SearchSection>,
    )

    expect(screen.getByTestId('btn')).toBeInTheDocument()
  })

  it('容器有 search-section 类名(ManagementTable 的用例靠它认"同一行")', () => {
    const { container } = render(
      <SearchSection>
        <span>内容</span>
      </SearchSection>,
    )

    expect(container.querySelector('.search-section')).not.toBeNull()
  })

  it('hover 升级到 L2 阴影,移开回到 L1', () => {
    const { container } = render(
      <SearchSection>
        <span>内容</span>
      </SearchSection>,
    )
    const root = container.querySelector('.search-section') as HTMLElement
    expect(root.style.boxShadow).toContain('var(--app-shadow-l1)')

    fireEvent.mouseEnter(root)
    expect(root.style.boxShadow).toContain('var(--app-shadow-l2)')

    fireEvent.mouseLeave(root)
    expect(root.style.boxShadow).toContain('var(--app-shadow-l1)')
  })

  it('style 追加在默认样式之后(调用方能覆盖)', () => {
    const { container } = render(
      <SearchSection style={{ padding: 0 }}>
        <span>内容</span>
      </SearchSection>,
    )
    const root = container.querySelector('.search-section') as HTMLElement

    expect(root.style.padding).toBe('0px')
  })
})
