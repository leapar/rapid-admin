/**
 * tech —— 科技蓝(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 * 颜色取自 2026-11-05 的一轮 antd seed → map 理念重调:
 * 中性面同一色温、品牌浅底同系衍生 —— 改单个色时保持同明度档位即可。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'tech',
  name: '科技蓝',
  appearance: 'light',
  /* 品牌三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: '#2F6BFF', hover: '#5d8cff', active: '#014aff' },

  palette: {
    /* 画布(布局底 / 容器 / 浮层 / 遮罩) */
    colorBgBase: '#f9f9fb',
    colorBgLayout: '#f2f4fa',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#172340',
    colorBgMask: 'rgba(23, 35, 64, 0.45)',
    /* 文字梯度(主 → 四级 + 描述 + 占位) */
    colorText: '#18223a',
    colorTextSecondary: '#3b4d77',
    colorTextTertiary: '#8997b8',
    colorTextQuaternary: '#bbc2d3',
    colorTextDescription: '#4c5e8b',
    colorTextPlaceholder: 'rgba(24, 34, 58, 0.35)',
    /* 边框 / 分割线 */
    colorBorder: '#e2e7f3',
    colorBorderSecondary: '#e9edf6',
    colorSplit: 'rgba(24, 34, 58, 0.06)',
    /* 品牌强调(文字 / 浅底 / 边框 / 链接) */
    primaryText: '#2F6BFF',
    primaryTextHover: '#5d8cff',
    primaryTextActive: '#014aff',
    colorPrimaryBg: '#e5edff',
    colorPrimaryBgHover: '#d1deff',
    colorPrimaryBorder: '#b3c9ff',
    colorPrimaryBorderHover: '#80a4ff',
    colorLink: '#2F6BFF',
    colorLinkHover: '#5d8cff',
    colorLinkActive: '#014aff',
  },

  comp: {
    /* Layout(顶栏毛玻璃 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
    siderBg: '#ffffff',
    bodyBg: '#f2f4fa',
    triggerColor: '#18223a',
    /* Menu(侧边菜单) */
    menuItemBg: '#ffffff',
    menuSelectedBg: '#dbe6ff',
    menuSelectedColor: '#2F6BFF',
    menuHoverBg: '#ebf0ff',
    menuHoverColor: '#2F6BFF',
    menuColor: '#3b4d77',
    menuDarkItemSelectedBg: 'rgba(47, 107, 255, 0.16)',
    menuDarkItemSelectedColor: '#c2d3ff',
    /* Table(表头 / 边框 / 行 hover / 行选中) */
    tableHeaderBg: '#f2f4fa',
    tableHeaderColor: '#3b4d77',
    tableBorderColor: '#e2e7f3',
    rowHoverBg: '#f6f7fb',
    rowSelectedBg: '#dbe6ff',
    rowSelectedHoverBg: '#d1deff',
    /* Button / Input(阴影 / 聚焦光圈) */
    primaryShadow: '0 1px 2px rgba(24, 34, 58, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(24, 34, 58, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(47, 107, 255, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#ebf0ff',
    dateCellRangeBg: '#dbe6ff',
    /* Breadcrumb / Tabs / Select */
    breadcrumbItemColor: 'rgba(24, 34, 58, 0.65)',
    breadcrumbLastColor: '#18223a',
    breadcrumbSeparatorColor: 'rgba(24, 34, 58, 0.3)',
    tabsActiveColor: '#2F6BFF',
    selectOptionSelectedColor: '#2F6BFF',
  },

  /* --app-* CSS 变量(applySkinCssVars 注入 <html style>);品牌系 7 键必给 */
  cssVars: {
    '--app-primary': '#2F6BFF',
    '--app-primary-hover': '#5d8cff',
    '--app-primary-active': '#014aff',
    '--app-primary-bg': '#e5edff',
    '--app-primary-bg-hover': '#d1deff',
    '--app-primary-border': '#b3c9ff',
    '--app-primary-border-hover': '#80a4ff',
  },
}

export default skin
