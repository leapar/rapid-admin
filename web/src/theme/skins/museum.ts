/**
 * museum —— 文博棕金(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 * 颜色取自 2026-11-05 的一轮 antd seed → map 理念重调:
 * 中性面同一色温、品牌浅底同系衍生 —— 改单个色时保持同明度档位即可。
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
    colorBgBase: '#fbfaf9',
    colorBgLayout: '#f9f6f3',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#3b2b1b',
    colorBgMask: 'rgba(59, 43, 27, 0.45)',
    /* 文字梯度(主 → 四级 + 描述 + 占位) */
    colorText: '#36281c',
    colorTextSecondary: '#705942',
    colorTextTertiary: '#b3a08f',
    colorTextQuaternary: '#d0c7be',
    colorTextDescription: '#836a53',
    colorTextPlaceholder: 'rgba(54, 40, 28, 0.35)',
    /* 边框 / 分割线 */
    colorBorder: '#f1eae4',
    colorBorderSecondary: '#f5f0eb',
    colorSplit: 'rgba(54, 40, 28, 0.06)',
    /* 品牌强调(文字 / 浅底 / 边框 / 链接) */
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
    /* Layout(顶栏毛玻璃 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
    siderBg: '#ffffff',
    bodyBg: '#f9f6f3',
    triggerColor: '#36281c',
    /* Menu(侧边菜单) */
    menuItemBg: '#ffffff',
    menuSelectedBg: '#f7ede4',
    menuSelectedColor: '#8C5A2B',
    menuHoverBg: '#faf5ef',
    menuHoverColor: '#8C5A2B',
    menuColor: '#705942',
    menuDarkItemSelectedBg: 'rgba(140, 90, 43, 0.16)',
    menuDarkItemSelectedColor: '#d6a77a',
    /* Table(表头 / 边框 / 行 hover / 行选中) */
    tableHeaderBg: '#f9f6f3',
    tableHeaderColor: '#705942',
    tableBorderColor: '#f1eae4',
    rowHoverBg: '#fbf9f7',
    rowSelectedBg: '#f7ede4',
    rowSelectedHoverBg: '#f4e8dc',
    /* Button / Input(阴影 / 聚焦光圈) */
    primaryShadow: '0 1px 2px rgba(54, 40, 28, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(54, 40, 28, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(140, 90, 43, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#faf5ef',
    dateCellRangeBg: '#f7ede4',
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
