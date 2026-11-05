/**
 * museum —— 文博棕金(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 *  * 取值策略:**整体古风米棕(沉浸式)** —— 顶栏深棕实底 + 米色文字,侧栏/
 *  * 画布/表头/卡片都是暖纸米色(29° 相),菜单选中棕金浅底,文字梯度暖棕
 *  * —— 一眼"古籍绢帛"。顶栏文字为米色(headerTextColor 系,Header.tsx 消费)。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'museum',
  name: '文博棕金',
  appearance: 'light',
  /* 品牌三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: '#8C5A2B', hover: '#af7136', active: '#694320' },

  palette: {
    /* 画布(布局底 / 容器 / 浮层 / 遮罩) */
    colorBgBase: '#fcfbfa',
    colorBgLayout: '#f2e9e1',
    colorBgContainer: '#fdfbf9',
    colorBgElevated: '#fdfbf9',
    colorBgSpotlight: '#3b2b1b',
    colorBgMask: 'rgba(59, 43, 27, 0.45)',
    /* 文字梯度(主 → 四级 + 描述 + 占位) */
    colorText: '#36281c',
    colorTextSecondary: '#705942',
    colorTextTertiary: '#a58c74',
    colorTextQuaternary: '#cbbca9',
    colorTextDescription: '#836a53',
    colorTextPlaceholder: 'rgba(54, 40, 28, 0.35)',
    /* 边框 / 分割线 */
    colorBorder: '#e4dbd2',
    colorBorderSecondary: '#ebe5e0',
    colorSplit: 'rgba(54, 40, 28, 0.06)',
    /* 品牌强调(文字 / 浅底 / 边框 / 链接) */
    primaryText: '#8C5A2B',
    primaryTextHover: '#af7136',
    primaryTextActive: '#694320',
    colorPrimaryBg: '#f7efe4',
    colorPrimaryBgHover: '#f1e4d3',
    colorPrimaryBorder: '#e9d5bc',
    colorPrimaryBorderHover: '#dbba92',
    colorLink: '#8C5A2B',
    colorLinkHover: '#af7136',
    colorLinkActive: '#694320',
  },

  comp: {
    /* Layout(顶栏 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: '#5d4128',
    siderBg: '#f7f2ee',
    bodyBg: '#f2e9e1',
    triggerColor: '#f4eae1',
    /* Header 沉浸式配套(顶栏文字 / 次级 / 三级 / 底边线)—— Header.tsx 消费 */
    headerTextColor: '#f4eae1',
    headerTextSecondaryColor: 'rgba(244, 234, 225, 0.75)',
    headerTextTertiaryColor: 'rgba(244, 234, 225, 0.5)',
    headerBorderColor: 'rgba(255, 255, 255, 0.12)',
    /* Menu(侧边菜单) */
    menuItemBg: 'transparent',
    menuSelectedBg: '#eee0d3',
    menuSelectedColor: '#8C5A2B',
    menuHoverBg: '#f4ede6',
    menuHoverColor: '#8C5A2B',
    menuColor: '#705942',
    menuDarkItemSelectedBg: 'rgba(140, 90, 43, 0.16)',
    menuDarkItemSelectedColor: '#d6a77a',
    /* Table(表头 / 边框 / 行 hover / 行选中) */
    tableHeaderBg: '#f3ede7',
    tableHeaderColor: '#705942',
    tableBorderColor: '#e4dbd2',
    rowHoverBg: '#f6f3f1',
    rowSelectedBg: '#eee0d3',
    rowSelectedHoverBg: '#ead8c8',
    /* Button / Input(阴影 / 聚焦光圈) */
    primaryShadow: '0 1px 2px rgba(54, 40, 28, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(54, 40, 28, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(140, 90, 43, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#fdfbf9',
    dateCellHoverBg: '#f4ede6',
    dateCellRangeBg: '#eee0d3',
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
    '--app-primary-bg': '#f7efe4',
    '--app-primary-bg-hover': '#f1e4d3',
    '--app-primary-border': '#e9d5bc',
    '--app-primary-border-hover': '#dbba92',
  },
}

export default skin
