/**
 * gov —— 政务红(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 *  * 取值策略:**整体中国红(沉浸式)** —— 顶栏国旗红实底 + 白字,侧栏/画布/
 *  * 表头/卡片带浅红晕(359° 相),菜单选中/表格选中红浅底收口。文字保持
 *  * 纯灰(掺红相的文字会发棕,2026-11-05 实测踩过);顶栏文字为白色
 *  * (headerTextColor 系,Header.tsx 消费)。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'gov',
  name: '政务红',
  appearance: 'light',
  /* 品牌三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: '#C7000B', hover: '#f5000e', active: '#990008' },

  palette: {
    /* 画布(布局底 / 容器 / 浮层 / 遮罩) */
    colorBgBase: '#fcfafa',
    colorBgLayout: '#f7eeee',
    colorBgContainer: '#fdfcfc',
    colorBgElevated: '#fdfcfc',
    colorBgSpotlight: '#2b2b2b',
    colorBgMask: 'rgba(43, 43, 43, 0.45)',
    /* 文字梯度(主 → 四级 + 描述 + 占位) */
    colorText: '#292929',
    colorTextSecondary: '#595959',
    colorTextTertiary: '#a1a1a1',
    colorTextQuaternary: '#c7c7c7',
    colorTextDescription: '#6b6b6b',
    colorTextPlaceholder: 'rgba(41, 41, 41, 0.35)',
    /* 边框 / 分割线 */
    colorBorder: '#eadcdc',
    colorBorderSecondary: '#f0e8e8',
    colorSplit: 'rgba(41, 41, 41, 0.06)',
    /* 品牌强调(文字 / 浅底 / 边框 / 链接) */
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
    /* Layout(顶栏 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: '#C7000B',
    siderBg: '#f9f3f3',
    bodyBg: '#f7eeee',
    triggerColor: '#ffffff',
    /* Header 沉浸式配套(顶栏文字 / 次级 / 三级 / 底边线)—— Header.tsx 消费 */
    headerTextColor: '#ffffff',
    headerTextSecondaryColor: 'rgba(255, 255, 255, 0.78)',
    headerTextTertiaryColor: 'rgba(255, 255, 255, 0.55)',
    headerBorderColor: 'rgba(255, 255, 255, 0.14)',
    /* Menu(侧边菜单) */
    menuItemBg: 'transparent',
    menuSelectedBg: '#fad6d6',
    menuSelectedColor: '#C7000B',
    menuHoverBg: '#f9e9ea',
    menuHoverColor: '#C7000B',
    menuColor: '#595959',
    menuDarkItemSelectedBg: 'rgba(199, 0, 11, 0.16)',
    menuDarkItemSelectedColor: '#ff616a',
    /* Table(表头 / 边框 / 行 hover / 行选中) */
    tableHeaderBg: '#f6eeef',
    tableHeaderColor: '#595959',
    tableBorderColor: '#eadcdc',
    rowHoverBg: '#f8f4f4',
    rowSelectedBg: '#fad6d6',
    rowSelectedHoverBg: '#f6cbcc',
    /* Button / Input(阴影 / 聚焦光圈) */
    primaryShadow: '0 1px 2px rgba(41, 41, 41, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(41, 41, 41, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(199, 0, 11, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#fdfcfc',
    dateCellHoverBg: '#f9e9ea',
    dateCellRangeBg: '#fad6d6',
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
