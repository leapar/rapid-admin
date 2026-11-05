/**
 * white —— 白色(默认)(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 * 颜色取自 2026-11-05 的一轮 antd seed → map 理念重调:
 * 中性面同一色温、品牌浅底同系衍生 —— 改单个色时保持同明度档位即可。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'white',
  name: '白色(默认)',
  appearance: 'light',
  /* 品牌三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: '#f60', hover: '#ff822e', active: '#d15400' },

  palette: {
    /* 画布(布局底 / 容器 / 浮层 / 遮罩) */
    colorBgBase: '#fafafa',
    colorBgLayout: '#f6f6f6',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#2e2b29',
    colorBgMask: 'rgba(46, 43, 41, 0.45)',
    /* 文字梯度(主 → 四级 + 描述 + 占位) */
    colorText: '#292929',
    colorTextSecondary: '#595959',
    colorTextTertiary: '#a1a1a1',
    colorTextQuaternary: '#c7c7c7',
    colorTextDescription: '#6b6b6b',
    colorTextPlaceholder: 'rgba(41, 41, 41, 0.35)',
    /* 边框 / 分割线 */
    colorBorder: '#ebebeb',
    colorBorderSecondary: '#f0f0f0',
    colorSplit: 'rgba(41, 41, 41, 0.06)',
    /* 品牌强调(文字 / 浅底 / 边框 / 链接) */
    primaryText: '#f60',
    primaryTextHover: '#ff822e',
    primaryTextActive: '#d15400',
    colorPrimaryBg: '#fff0e5',
    colorPrimaryBgHover: '#ffe3d1',
    colorPrimaryBorder: '#ffd1b3',
    colorPrimaryBorderHover: '#ffb380',
    colorLink: '#f60',
    colorLinkHover: '#ff822e',
    colorLinkActive: '#d15400',
  },

  comp: {
    /* Layout(顶栏毛玻璃 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
    siderBg: '#ffffff',
    bodyBg: '#f6f6f6',
    triggerColor: '#292929',
    /* Menu(侧边菜单) */
    menuItemBg: '#ffffff',
    menuSelectedBg: '#ffeadb',
    menuSelectedColor: '#f60',
    menuHoverBg: '#fff3eb',
    menuHoverColor: '#f60',
    menuColor: '#595959',
    menuDarkItemSelectedBg: 'rgba(255, 102, 0, 0.16)',
    menuDarkItemSelectedColor: '#ffc299',
    /* Table(表头 / 边框 / 行 hover / 行选中) */
    tableHeaderBg: '#f6f6f6',
    tableHeaderColor: '#595959',
    tableBorderColor: '#ebebeb',
    rowHoverBg: '#f9f9f9',
    rowSelectedBg: '#ffeadb',
    rowSelectedHoverBg: '#ffe3d1',
    /* Button / Input(阴影 / 聚焦光圈) */
    primaryShadow: '0 1px 2px rgba(41, 41, 41, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(41, 41, 41, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(255, 102, 0, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#fff3eb',
    dateCellRangeBg: '#ffeadb',
    /* Breadcrumb / Tabs / Select */
    breadcrumbItemColor: 'rgba(41, 41, 41, 0.65)',
    breadcrumbLastColor: '#292929',
    breadcrumbSeparatorColor: 'rgba(41, 41, 41, 0.3)',
    tabsActiveColor: '#f60',
    selectOptionSelectedColor: '#f60',
  },

  /* --app-* CSS 变量(applySkinCssVars 注入 <html style>);品牌系 7 键必给 */
  cssVars: {
    '--app-primary': '#f60',
    '--app-primary-hover': '#ff822e',
    '--app-primary-active': '#d15400',
    '--app-primary-bg': '#fff0e5',
    '--app-primary-bg-hover': '#ffe3d1',
    '--app-primary-border': '#ffd1b3',
    '--app-primary-border-hover': '#ffb380',
  },
}

export default skin
