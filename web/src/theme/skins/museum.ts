/**
 * museum —— 文博棕金(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 * 取值策略:**整面暖纸** —— 侧栏/画布/表头都是奶油米色(棕金相 29°),
 * 文字暖棕、品牌浅底同系衍生,整个界面一眼"棕金";大面积表面上色是
 * 2026-11-05 用户实测定的方向(只在强调位上色会跟政务红撞观感)。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'museum',
  name: '文博棕金',
  appearance: 'light',
  /* 品牌三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: '#8C5A2B', hover: '#af7136', active: '#694320' },

  palette: {
    /* 画布(布局底 / 容器 / 浮层 / 遮罩)—— 暖纸米色 */
    colorBgBase: '#fcfbfa',
    colorBgLayout: '#f4f0eb',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#3b2b1b',
    colorBgMask: 'rgba(59, 43, 27, 0.45)',
    /* 文字梯度 —— 暖棕 */
    colorText: '#36281c',
    colorTextSecondary: '#705942',
    colorTextTertiary: '#b3a08f',
    colorTextQuaternary: '#d0c7be',
    colorTextDescription: '#836a53',
    colorTextPlaceholder: 'rgba(54, 40, 28, 0.35)',
    /* 边框 / 分割线 —— 暖米 */
    colorBorder: '#e8e0d9',
    colorBorderSecondary: '#efeae6',
    colorSplit: 'rgba(54, 40, 28, 0.06)',
    /* 品牌强调(文字 / 浅底 / 边框 / 链接)—— 棕金系 */
    primaryText: '#8C5A2B',
    primaryTextHover: '#af7136',
    primaryTextActive: '#694320',
    colorPrimaryBg: '#f9f2eb',
    colorPrimaryBgHover: '#f4e8dc',
    colorPrimaryBorder: '#edd8c4',
    colorPrimaryBorderHover: '#e1be9d',
    colorLink: '#8C5A2B',
    colorLinkHover: '#af7136',
    colorLinkActive: '#694320',
  },

  comp: {
    /* Layout(顶栏暖玻璃 / 暖米侧栏 / 暖纸画布) */
    layoutHeaderBg: 'rgba(251, 249, 247, 0.78)',
    siderBg: '#fbf9f7',
    bodyBg: '#f4f0eb',
    triggerColor: '#36281c',
    /* Menu(选中/hover 走棕金浅底;透明底让暖纸侧栏透出来) */
    menuItemBg: 'transparent',
    menuSelectedBg: '#f1e5da',
    menuSelectedColor: '#8C5A2B',
    menuHoverBg: '#f7f1eb',
    menuHoverColor: '#8C5A2B',
    menuColor: '#705942',
    menuDarkItemSelectedBg: 'rgba(140, 90, 43, 0.16)',
    menuDarkItemSelectedColor: '#d6a77a',
    /* Table(暖米表头 / 行选中同菜单浅底) */
    tableHeaderBg: '#f4f0eb',
    tableHeaderColor: '#705942',
    tableBorderColor: '#e8e0d9',
    rowHoverBg: '#f7f5f2',
    rowSelectedBg: '#f1e5da',
    rowSelectedHoverBg: '#ebdbcb',
    /* Button / Input(阴影暖棕 / 聚焦光圈棕金) */
    primaryShadow: '0 1px 2px rgba(54, 40, 28, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(54, 40, 28, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(140, 90, 43, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#f7f1eb',
    dateCellRangeBg: '#f1e5da',
    /* Breadcrumb / Tabs / Select */
    breadcrumbItemColor: 'rgba(54, 40, 28, 0.65)',
    breadcrumbLastColor: '#36281c',
    breadcrumbSeparatorColor: 'rgba(54, 40, 28, 0.3)',
    tabsActiveColor: '#8C5A2B',
    selectOptionSelectedColor: '#8C5A2B',
  },

  /* --app-* CSS 变量(applySkinCssVars 注入 <html style>);品牌系 7 键必给 */
  cssVars: {
    '--app-primary': '#8C5A2B',
    '--app-primary-hover': '#af7136',
    '--app-primary-active': '#694320',
    '--app-primary-bg': '#f9f2eb',
    '--app-primary-bg-hover': '#f4e8dc',
    '--app-primary-border': '#edd8c4',
    '--app-primary-border-hover': '#e1be9d',
  },
}

export default skin
