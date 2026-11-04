/**
 * SearchSection —— 搜索区容器(浅灰底 + L1 阴影 + 边缘高光 + 圆角 14 + hover 升级 L2)
 *
 * 2026-10-18 抽出:原本 inline 在 ManagementTable.tsx 的 searchBar,
 *   config/index.tsx 是 Collapse + Form 布局,不走 ManagementTable,
 *   但搜索区视觉应该和其他页一致 — 抽成公共组件复用
 *
 * 用法:
 *   <SearchSection>
 *     <Input.Search ... />
 *     <Button ... />
 *   </SearchSection>
 *
 * 国际化 / 业务字段:内部不放任何业务元素,只负责容器
 *
 * ⚠️ **M111 收回:`fullWidth` 这个 prop**
 *   以前接口里声明了 `fullWidth?: boolean`,但组件体**从来没读过它** ——
 *   容器本来就是 `display:flex`,子元素自己决定宽度,没有"占满整宽"这回事。
 *   全仓两个调用方(ManagementTable / system/config)也都没传过它。
 *   一个不生效的开关留在公开接口上,只会让人以为"传了就能占满" ⇒ 收回。
 */
import React from 'react'

export interface SearchSectionProps {
  children: React.ReactNode
  /** 额外样式 */
  style?: React.CSSProperties
}

export const SearchSection: React.FC<SearchSectionProps> = ({ children, style }) => (
  <div
    className="search-section"
    style={{
      margin: '0 4px 12px',
      padding: '14px 16px',
      borderRadius: 'var(--app-radius-lg)',
      background: 'var(--app-surface-bg)',
      border: '0.5px solid var(--app-surface-border)',
      boxShadow: 'var(--app-shadow-l1), var(--app-edge-light)',
      transition:
        'box-shadow var(--app-duration-mid) var(--app-ease-out-soft), border-color var(--app-duration-mid) var(--app-ease-out-soft)',
      display: 'flex',
      alignItems: 'center',
      gap: 12,
      flexShrink: 0,
      flexWrap: 'wrap',
      ...style,
    }}
    onMouseEnter={(e) => {
      e.currentTarget.style.boxShadow = 'var(--app-shadow-l2), var(--app-edge-light)'
      e.currentTarget.style.borderColor = 'var(--app-hover-border)'
    }}
    onMouseLeave={(e) => {
      e.currentTarget.style.boxShadow = 'var(--app-shadow-l1), var(--app-edge-light)'
      e.currentTarget.style.borderColor = 'var(--app-surface-border)'
    }}
  >
    {children}
  </div>
)

export default SearchSection
