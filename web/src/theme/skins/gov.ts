/**
 * gov —— 政务红(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 * 颜色取自 2026-11-05 的一轮 antd seed → map 理念重调:
 * 中性面同一色温、品牌浅底同系衍生 —— 改单个色时保持同明度档位即可。
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
    colorBgBase: '#fbf9f9',
    colorBgLayout: '#f8f4f4',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#381e20',
    colorBgMask: 'rgba(56, 30, 32, 0.45)',
    /* 文字梯度(主 → 四级 + 描述 + 占位) */
    colorText: '#331f20',
    colorTextSecondary: '#6b4749',
    colorTextTertiary: '#af9294',
    colorTextQuaternary: '#cec0c1',
    colorTextDescription: '#7e585a',
    colorTextPlaceholder: 'rgba(51, 31, 32, 0.35)',
    /* 边框 / 分割线 */
    colorBorder: '#f0e6e6',
    colorBorderSecondary: '#f4ecec',
    colorSplit: 'rgba(51, 31, 32, 0.06)',
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
    /* Layout(顶栏毛玻璃 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
    siderBg: '#ffffff',
    bodyBg: '#f8f4f4',
    triggerColor: '#331f20',
    /* Menu(侧边菜单) */
    menuItemBg: '#ffffff',
    menuSelectedBg: '#ffdbdd',
    menuSelectedColor: '#C7000B',
    menuHoverBg: '#ffebec',
    menuHoverColor: '#C7000B',
    menuColor: '#6b4749',
    menuDarkItemSelectedBg: 'rgba(199, 0, 11, 0.16)',
    menuDarkItemSelectedColor: '#ff616a',
    /* Table(表头 / 边框 / 行 hover / 行选中) */
    tableHeaderBg: '#f8f4f4',
    tableHeaderColor: '#6b4749',
    tableBorderColor: '#f0e6e6',
    rowHoverBg: '#faf7f7',
    rowSelectedBg: '#ffdbdd',
    rowSelectedHoverBg: '#ffd1d4',
    /* Button / Input(阴影 / 聚焦光圈) */
    primaryShadow: '0 1px 2px rgba(51, 31, 32, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(51, 31, 32, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(199, 0, 11, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#ffebec',
    dateCellRangeBg: '#ffdbdd',
    /* Breadcrumb / Tabs / Select */
    breadcrumbItemColor: 'rgba(51, 31, 32, 0.65)',
    breadcrumbLastColor: '#331f20',
    breadcrumbSeparatorColor: 'rgba(51, 31, 32, 0.3)',
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
