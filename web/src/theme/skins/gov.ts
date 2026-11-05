/**
 * gov —— 政务红(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 * 取值策略:**红调氛围面** —— 侧栏/画布/表头带浅红晕(359° 相),文字
 * 保持纯灰(掺红相的文字会发棕,2026-11-05 实测踩过),红色再以强调位
 * 收口(按钮/链接/菜单选中/表格选中/聚焦光圈)。大面积红晕 + 纯灰文字
 * 既"一眼红主题",又不与文博棕金的暖纸底混淆。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'gov',
  name: '政务红',
  appearance: 'light',
  /* 品牌三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: '#C7000B', hover: '#f5000e', active: '#990008' },

  palette: {
    /* 画布(布局底 / 容器 / 浮层 / 遮罩)—— 浅红晕 */
    colorBgBase: '#fcfafa',
    colorBgLayout: '#f8f1f1',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#2b2b2b',
    colorBgMask: 'rgba(43, 43, 43, 0.45)',
    /* 文字梯度 —— 纯灰(不掺红相,防发棕) */
    colorText: '#292929',
    colorTextSecondary: '#595959',
    colorTextTertiary: '#a1a1a1',
    colorTextQuaternary: '#c7c7c7',
    colorTextDescription: '#6b6b6b',
    colorTextPlaceholder: 'rgba(41, 41, 41, 0.35)',
    /* 边框 / 分割线 —— 浅红晕 */
    colorBorder: '#ecdfdf',
    colorBorderSecondary: '#f1eaea',
    colorSplit: 'rgba(41, 41, 41, 0.06)',
    /* 品牌强调(文字 / 浅底 / 边框 / 链接)—— 国旗红系 */
    primaryText: '#C7000B',
    primaryTextHover: '#f5000e',
    primaryTextActive: '#990008',
    colorPrimaryBg: '#ffe5e7',
    colorPrimaryBgHover: '#ffd1d4',
    colorPrimaryBorder: '#ffb3b7',
    colorPrimaryBorderHover: '#ff8087',
    colorLink: '#C7000B',
    colorLinkHover: '#f5000e',
    colorLinkActive: '#990008',
  },

  comp: {
    /* Layout(顶栏红晕玻璃 / 浅红侧栏 / 红晕画布) */
    layoutHeaderBg: 'rgba(251, 247, 247, 0.78)',
    siderBg: '#fbf7f7',
    bodyBg: '#f8f1f1',
    triggerColor: '#292929',
    /* Menu(选中/hover 走红浅底;透明底让红晕侧栏透出来) */
    menuItemBg: 'transparent',
    menuSelectedBg: '#fcdede',
    menuSelectedColor: '#C7000B',
    menuHoverBg: '#fbefef',
    menuHoverColor: '#C7000B',
    menuColor: '#595959',
    menuDarkItemSelectedBg: 'rgba(199, 0, 11, 0.16)',
    menuDarkItemSelectedColor: '#ff616a',
    /* Table(红晕表头 / 行选中同菜单浅底) */
    tableHeaderBg: '#f7f0f0',
    tableHeaderColor: '#595959',
    tableBorderColor: '#ecdfdf',
    rowHoverBg: '#f9f5f6',
    rowSelectedBg: '#fcdede',
    rowSelectedHoverBg: '#ffd1d4',
    /* Button / Input(阴影纯灰 / 聚焦光圈红) */
    primaryShadow: '0 1px 2px rgba(41, 41, 41, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(41, 41, 41, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(199, 0, 11, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#fbefef',
    dateCellRangeBg: '#fcdede',
    /* Breadcrumb / Tabs / Select */
    breadcrumbItemColor: 'rgba(41, 41, 41, 0.65)',
    breadcrumbLastColor: '#292929',
    breadcrumbSeparatorColor: 'rgba(41, 41, 41, 0.3)',
    tabsActiveColor: '#C7000B',
    selectOptionSelectedColor: '#C7000B',
  },

  /* --app-* CSS 变量(applySkinCssVars 注入 <html style>);品牌系 7 键必给 */
  cssVars: {
    '--app-primary': '#C7000B',
    '--app-primary-hover': '#f5000e',
    '--app-primary-active': '#990008',
    '--app-primary-bg': '#ffe5e7',
    '--app-primary-bg-hover': '#ffd1d4',
    '--app-primary-border': '#ffb3b7',
    '--app-primary-border-hover': '#ff8087',
  },
}

export default skin
