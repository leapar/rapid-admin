/**
 * museum —— 文博棕金(博物馆 / 文博领域)
 *
 * 规格:docs/theming.md §4.1。赭石棕主色 + 宣纸暖灰画布;
 * 中性基底(文字/边框)为暖棕灰系。扁平模型(2026-11-05):浅色外观。
 * 注意:规格里的"深胡桃侧栏"是可选庄重款,浅色下会让浅色菜单文字失去对比,
 * 本版不启用 —— 想要就在 comp 里把 siderBg 与 menuColor/menuHoverBg 成组改。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'museum',
  name: '文博棕金',
  appearance: 'light',
  brand: {
    primary: '#8C5A2B',
    hover: '#A9743D',
    active: '#6E4520',
  },

  // 语义色板(宣纸暖灰系)
  palette: {
    colorBgBase: '#FFFDF9',
    colorBgLayout: '#F7F4EF', // 宣纸暖灰
    colorBgContainer: '#FFFDF9',
    colorBgElevated: '#FFFDF9',
    colorBgSpotlight: '#3D2E1F',
    colorBgMask: 'rgba(61, 46, 31, 0.45)',
    colorText: '#3D2E1F',
    colorTextSecondary: '#6B563D',
    colorTextTertiary: '#A08B70',
    colorTextQuaternary: '#C4B29A',
    colorTextDescription: '#7A6549',
    colorTextPlaceholder: 'rgba(61, 46, 31, 0.35)',
    colorBorder: '#EBE2D3',
    colorBorderSecondary: '#F0E9DC',
    colorSplit: 'rgba(61, 46, 31, 0.06)',
    primaryText: '#8C5A2B',
    primaryTextHover: '#A9743D',
    primaryTextActive: '#6E4520',
    colorPrimaryBg: '#F5EBDD', // 浅驼底
    colorPrimaryBgHover: '#EFE1CB',
    colorPrimaryBorder: '#DCC3A4',
    colorPrimaryBorderHover: '#C9A87C',
    colorLink: '#8C5A2B',
    colorLinkHover: '#A9743D',
    colorLinkActive: '#6E4520',
  },

  // 组件级 token
  comp: {
    layoutHeaderBg: 'rgba(255, 253, 249, 0.78)',
    siderBg: '#FFFDF9',
    bodyBg: '#F7F4EF',
    triggerColor: '#3D2E1F',
    menuItemBg: '#FFFDF9',
    menuSelectedBg: '#F0E4D2',
    menuSelectedColor: '#8C5A2B',
    menuHoverBg: '#F5EBDD',
    menuHoverColor: '#8C5A2B',
    menuColor: '#6B563D',
    menuDarkItemSelectedBg: 'rgba(140, 90, 43, 0.16)',
    menuDarkItemSelectedColor: '#C89B6E',
    tableHeaderBg: '#F3EEE4',
    tableHeaderColor: '#6B563D',
    tableBorderColor: '#EBE2D3',
    rowHoverBg: '#F7F3EA',
    rowSelectedBg: '#F0E4D2',
    rowSelectedHoverBg: '#EFE1CB',
    primaryShadow: '0 1px 2px rgba(61, 46, 31, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(61, 46, 31, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(140, 90, 43, 0.18)',
    paginationItemBg: '#FFFDF9',
    dateCellHoverBg: '#F5EBDD',
    dateCellRangeBg: '#F5EBDD',
    breadcrumbItemColor: 'rgba(61, 46, 31, 0.65)',
    breadcrumbLastColor: '#3D2E1F',
    breadcrumbSeparatorColor: 'rgba(61, 46, 31, 0.3)',
    tabsActiveColor: '#8C5A2B',
    selectOptionSelectedColor: '#8C5A2B',
  },

  // --app-* 品牌系变量(全量 7 个)
  cssVars: {
    '--app-primary': '#8C5A2B',
    '--app-primary-hover': '#A9743D',
    '--app-primary-active': '#6E4520',
    '--app-primary-bg': '#F5EBDD',
    '--app-primary-bg-hover': '#EFE1CB',
    '--app-primary-border': '#DCC3A4',
    '--app-primary-border-hover': '#C9A87C',
  },
}

export default skin
