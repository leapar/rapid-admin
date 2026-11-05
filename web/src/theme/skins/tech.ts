/**
 * tech —— 科技蓝(数据 / 科技产品)
 *
 * 规格:docs/theming.md §4.2。电光蓝主色 + 冷灰蓝画布;
 * 关键差异在"冷调阴影"(选色心法 4)—— 阴影结构不动,只把 slate 色相
 * 换成主色淡晕,科技感主要来自这里而不只是换主色。
 * 扁平模型(2026-11-05):浅色外观。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'tech',
  name: '科技蓝',
  appearance: 'light',
  brand: {
    primary: '#2F6BFF',
    hover: '#5384FF',
    active: '#1E4FD6',
  },

  // 语义色板(冷灰蓝系)
  palette: {
    colorBgBase: '#FBFCFE',
    colorBgLayout: '#F3F6FC', // 冷灰蓝
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#1B2540',
    colorBgMask: 'rgba(27, 37, 64, 0.45)',
    colorText: '#1B2540',
    colorTextSecondary: '#45516E',
    colorTextTertiary: '#8D9AB8',
    colorTextQuaternary: '#C4CDE2',
    colorTextDescription: '#5D6C8E',
    colorTextPlaceholder: 'rgba(27, 37, 64, 0.35)',
    colorBorder: '#E5EAF5',
    colorBorderSecondary: '#EBEFF8',
    colorSplit: 'rgba(27, 37, 64, 0.06)',
    primaryText: '#2F6BFF',
    primaryTextHover: '#5384FF',
    primaryTextActive: '#1E4FD6',
    colorPrimaryBg: '#EBF0FF',
    colorPrimaryBgHover: '#DCE5FF',
    colorPrimaryBorder: '#B8C9FF',
    colorPrimaryBorderHover: '#8FA9FF',
    colorLink: '#2F6BFF',
    colorLinkHover: '#5384FF',
    colorLinkActive: '#1E4FD6',
  },

  // 组件级 token
  comp: {
    layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
    siderBg: '#ffffff',
    bodyBg: '#F3F6FC',
    triggerColor: '#1B2540',
    menuItemBg: '#ffffff',
    menuSelectedBg: '#EBF0FF',
    menuSelectedColor: '#2F6BFF',
    menuHoverBg: '#F0F4FF',
    menuHoverColor: '#2F6BFF',
    menuColor: '#45516E',
    menuDarkItemSelectedBg: 'rgba(47, 107, 255, 0.16)',
    menuDarkItemSelectedColor: '#7CA0FF',
    tableHeaderBg: '#F5F7FC',
    tableHeaderColor: '#45516E',
    tableBorderColor: '#E5EAF5',
    rowHoverBg: '#F5F8FE',
    rowSelectedBg: '#EBF0FF',
    rowSelectedHoverBg: '#DCE5FF',
    primaryShadow: '0 1px 2px rgba(27, 37, 64, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(27, 37, 64, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(47, 107, 255, 0.18)',
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#F0F4FF',
    dateCellRangeBg: '#EBF0FF',
    breadcrumbItemColor: 'rgba(27, 37, 64, 0.65)',
    breadcrumbLastColor: '#1B2540',
    breadcrumbSeparatorColor: 'rgba(27, 37, 64, 0.3)',
    tabsActiveColor: '#2F6BFF',
    selectOptionSelectedColor: '#2F6BFF',
  },

  // --app-* 品牌系变量(7 键)+ 冷调阴影皮肤专属变量(选色心法 4):
  // 结构不动(slate → 电光蓝淡晕,alpha 略降防脏)
  cssVars: {
    '--app-primary': '#2F6BFF',
    '--app-primary-hover': '#5384FF',
    '--app-primary-active': '#1E4FD6',
    '--app-primary-bg': '#EBF0FF',
    '--app-primary-bg-hover': '#DCE5FF',
    '--app-primary-border': '#B8C9FF',
    '--app-primary-border-hover': '#8FA9FF',
    '--app-shadow-l2':
      '0 1px 2px rgba(27, 37, 64, 0.04), 0 2px 6px rgba(47, 107, 255, 0.05), 0 4px 12px rgba(47, 107, 255, 0.04)',
    '--app-shadow-l3':
      '0 1px 2px rgba(27, 37, 64, 0.04), 0 6px 16px rgba(47, 107, 255, 0.07), 0 16px 40px rgba(47, 107, 255, 0.05)',
    '--app-shadow-l4':
      '0 2px 4px rgba(27, 37, 64, 0.04), 0 12px 24px rgba(47, 107, 255, 0.07), 0 28px 64px rgba(47, 107, 255, 0.06), 0 48px 96px rgba(27, 37, 64, 0.08)',
  },
}

export default skin
