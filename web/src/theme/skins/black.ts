/**
 * black —— 黑色皮肤
 *
 * 2026-11-05 皮肤扁平化:皮肤 = 整体外观,本皮肤 = 原默认观感的深色面,
 * appearance='dark'(决定 darkAlgorithm 与 <html data-theme="dark">,
 * variables.css 的暗色语义段随之生效)。
 *
 * 色板与 styles/variables.css 暗色段同源(中性黑 + 品牌橙):
 *   画布 #1a1a1a / 表面 #262626 / 浮层 #333333 / 实边框 #404040
 *   文字 #e8e8e8 → #bfbfbf → #8c8c8c → #666666
 *   品牌橙本体跨主题不变;暗底上的"品牌文字/链接"提亮一档到 #ff8c3a 系;
 *   品牌浅底改半透明橙(实色浅橙底在深色下刺眼)。
 *
 * 本文件与 white.ts 是**字段全集基准**(skins.complete.test.ts 在守)。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'black',
  name: '黑色',
  appearance: 'dark',
  brand: {
    primary: '#f60',
    hover: '#ff8c3a',
    active: '#d35400',
  },

  // 语义色板(中性黑系,2026-11-03 用户拍板:
  // 画布 #1a1a1a / 表面 #262626 / 浮层 #333333 / 实边框与 tooltip 底 #404040)
  palette: {
    colorBgBase: '#1a1a1a',
    colorBgLayout: '#1a1a1a',
    colorBgContainer: '#262626',
    colorBgElevated: '#333333',
    colorBgSpotlight: '#404040',
    colorBgMask: 'rgba(0, 0, 0, 0.65)',
    colorText: '#e8e8e8',
    colorTextSecondary: '#bfbfbf',
    colorTextTertiary: '#8c8c8c',
    colorTextQuaternary: '#666666',
    colorTextDescription: '#8c8c8c',
    colorTextPlaceholder: 'rgba(232, 232, 232, 0.3)',
    colorBorder: '#404040',
    colorBorderSecondary: '#333333',
    colorSplit: 'rgba(255, 255, 255, 0.08)',
    primaryText: '#ff8c3a',
    primaryTextHover: '#ffa35c',
    primaryTextActive: '#ffc08a',
    colorPrimaryBg: 'rgba(255, 102, 0, 0.12)',
    colorPrimaryBgHover: 'rgba(255, 102, 0, 0.2)',
    colorPrimaryBorder: 'rgba(255, 102, 0, 0.32)',
    colorPrimaryBorderHover: 'rgba(255, 102, 0, 0.5)',
    colorLink: '#ff8c3a',
    colorLinkHover: '#ffa35c',
    colorLinkActive: '#ffc08a',
  },

  // 组件级 token(深色)
  comp: {
    layoutHeaderBg: 'rgba(26, 26, 26, 0.78)',
    siderBg: '#262626',
    bodyBg: '#1a1a1a',
    triggerColor: '#e8e8e8',
    menuItemBg: 'transparent',
    menuSelectedBg: 'rgba(255, 102, 0, 0.16)',
    menuSelectedColor: '#ffae6c',
    menuHoverBg: 'rgba(255, 255, 255, 0.06)',
    menuHoverColor: '#ffae6c',
    menuColor: '#bfbfbf',
    menuDarkItemSelectedBg: 'rgba(255, 102, 0, 0.16)',
    menuDarkItemSelectedColor: '#ffae6c',
    tableHeaderBg: 'rgba(255, 255, 255, 0.05)',
    tableHeaderColor: '#bfbfbf',
    tableBorderColor: 'rgba(255, 255, 255, 0.1)',
    rowHoverBg: 'rgba(255, 255, 255, 0.04)',
    rowSelectedBg: 'rgba(255, 102, 0, 0.12)',
    rowSelectedHoverBg: 'rgba(255, 102, 0, 0.2)',
    primaryShadow: '0 1px 2px rgba(0, 0, 0, 0.4), inset 0 1px 0 rgba(255, 255, 255, 0.12)',
    defaultShadow: '0 1px 2px rgba(0, 0, 0, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.06)',
    inputActiveShadow: '0 0 0 3px rgba(255, 102, 0, 0.25)',
    paginationItemBg: 'transparent',
    dateCellHoverBg: 'rgba(255, 102, 0, 0.12)',
    dateCellRangeBg: 'rgba(255, 102, 0, 0.2)',
    breadcrumbItemColor: 'rgba(232, 232, 232, 0.65)',
    breadcrumbLastColor: '#e8e8e8',
    breadcrumbSeparatorColor: 'rgba(232, 232, 232, 0.3)',
    tabsActiveColor: '#f60',
    selectOptionSelectedColor: '#f60',
  },

  // --app-* 品牌系变量:品牌三态本体跨明暗不变(值与 white 相同),
  // 浅底四键换深色下的半透明橙
  cssVars: {
    '--app-primary': '#f60',
    '--app-primary-hover': '#ff8c3a',
    '--app-primary-active': '#d35400',
    '--app-primary-bg': 'rgba(255, 102, 0, 0.12)',
    '--app-primary-bg-hover': 'rgba(255, 102, 0, 0.2)',
    '--app-primary-border': 'rgba(255, 102, 0, 0.32)',
    '--app-primary-border-hover': 'rgba(255, 102, 0, 0.5)',
  },
}

export default skin
