/**
 * black —— 黑色(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 * 颜色取自 2026-11-05 的一轮 antd seed → map 理念重调:
 * 中性面同一色温、品牌浅底同系衍生 —— 改单个色时保持同明度档位即可。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'black',
  name: '黑色',
  appearance: 'dark',
  /* 品牌三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: '#f60', hover: '#ff822e', active: '#d15400' },

  palette: {
    /* 画布(布局底 / 容器 / 浮层 / 遮罩) */
    colorBgBase: '#1c1b1a',
    colorBgLayout: '#1c1b1a',
    colorBgContainer: '#282625',
    colorBgElevated: '#353331',
    colorBgSpotlight: '#423f3d',
    colorBgMask: 'rgba(0, 0, 0, 0.65)',
    /* 文字梯度(主 → 四级 + 描述 + 占位) */
    colorText: '#e8e8e8',
    colorTextSecondary: '#c0bfbe',
    colorTextTertiary: '#8e8c8a',
    colorTextQuaternary: '#686664',
    colorTextDescription: '#8e8c8a',
    colorTextPlaceholder: 'rgba(232, 232, 232, 0.3)',
    /* 边框 / 分割线 */
    colorBorder: '#423f3d',
    colorBorderSecondary: '#353331',
    colorSplit: 'rgba(255, 255, 255, 0.08)',
    /* 品牌强调(文字 / 浅底 / 边框 / 链接) */
    primaryText: '#ff9d5c',
    primaryTextHover: '#ffc299',
    primaryTextActive: '#ffdac2',
    colorPrimaryBg: 'rgba(255, 102, 0, 0.12)',
    colorPrimaryBgHover: 'rgba(255, 102, 0, 0.2)',
    colorPrimaryBorder: 'rgba(255, 102, 0, 0.32)',
    colorPrimaryBorderHover: 'rgba(255, 102, 0, 0.5)',
    colorLink: '#ff9d5c',
    colorLinkHover: '#ffc299',
    colorLinkActive: '#ffdac2',
  },

  comp: {
    /* Layout(顶栏毛玻璃 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: 'rgba(28, 27, 26, 0.78)',
    siderBg: '#282625',
    bodyBg: '#1c1b1a',
    triggerColor: '#e8e8e8',
    /* Menu(侧边菜单) */
    menuItemBg: 'transparent',
    menuSelectedBg: 'rgba(255, 102, 0, 0.16)',
    menuSelectedColor: '#ffc299',
    menuHoverBg: 'rgba(255, 255, 255, 0.06)',
    menuHoverColor: '#ffc299',
    menuColor: '#c0bfbe',
    menuDarkItemSelectedBg: 'rgba(255, 102, 0, 0.16)',
    menuDarkItemSelectedColor: '#ffc299',
    /* Table(表头 / 边框 / 行 hover / 行选中) */
    tableHeaderBg: 'rgba(255, 255, 255, 0.05)',
    tableHeaderColor: '#c0bfbe',
    tableBorderColor: 'rgba(255, 255, 255, 0.1)',
    rowHoverBg: 'rgba(255, 255, 255, 0.04)',
    rowSelectedBg: 'rgba(255, 102, 0, 0.12)',
    rowSelectedHoverBg: 'rgba(255, 102, 0, 0.2)',
    /* Button / Input(阴影 / 聚焦光圈) */
    primaryShadow: '0 1px 2px rgba(0, 0, 0, 0.4), inset 0 1px 0 rgba(255, 255, 255, 0.12)',
    defaultShadow: '0 1px 2px rgba(0, 0, 0, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.06)',
    inputActiveShadow: '0 0 0 3px rgba(255, 102, 0, 0.25)',
    /* Pagination / DatePicker */
    paginationItemBg: 'transparent',
    dateCellHoverBg: 'rgba(255, 102, 0, 0.12)',
    dateCellRangeBg: 'rgba(255, 102, 0, 0.2)',
    /* Breadcrumb / Tabs / Select */
    breadcrumbItemColor: 'rgba(232, 232, 232, 0.65)',
    breadcrumbLastColor: '#e8e8e8',
    breadcrumbSeparatorColor: 'rgba(232, 232, 232, 0.3)',
    tabsActiveColor: '#ff9d5c',
    selectOptionSelectedColor: '#ff9d5c',
  },

  /* --app-* CSS 变量(applySkinCssVars 注入 <html style>);品牌系 7 键必给 */
  cssVars: {
    '--app-primary': '#f60',
    '--app-primary-hover': '#ff822e',
    '--app-primary-active': '#d15400',
    '--app-primary-bg': 'rgba(255, 102, 0, 0.12)',
    '--app-primary-bg-hover': 'rgba(255, 102, 0, 0.2)',
    '--app-primary-border': 'rgba(255, 102, 0, 0.32)',
    '--app-primary-border-hover': 'rgba(255, 102, 0, 0.5)',
  },
}

export default skin
