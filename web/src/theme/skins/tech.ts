/**
 * tech —— 科技蓝(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 *  * 取值策略:**整体科技蓝(沉浸式)** —— 顶栏深蓝实底 + 白字,侧栏/画布/
 *  * 表头/卡片带冷蓝晕(226° 相),菜单选中蓝浅底;文字梯度冷藏青。
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
    colorBgBase: '#f7f8fc',
    colorBgLayout: '#e6e9f5',
    colorBgContainer: '#fafbfe',
    colorBgElevated: '#fafbfe',
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
    colorBorder: '#d2d8ea',
    colorBorderSecondary: '#e2e6f1',
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
    /* Layout(顶栏 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: '#192f76',
    siderBg: '#eef1fa',
    bodyBg: '#e6e9f5',
    triggerColor: '#ffffff',
    /* Header 沉浸式配套(顶栏文字 / 次级 / 三级 / 底边线)—— Header.tsx 消费 */
    headerTextColor: '#ffffff',
    headerTextSecondaryColor: 'rgba(255, 255, 255, 0.78)',
    headerTextTertiaryColor: 'rgba(255, 255, 255, 0.55)',
    headerBorderColor: 'rgba(255, 255, 255, 0.14)',
    /* Menu(侧边菜单) */
    menuItemBg: 'transparent',
    menuSelectedBg: '#d0deff',
    menuSelectedColor: '#2F6BFF',
    menuHoverBg: '#e4ebff',
    menuHoverColor: '#2F6BFF',
    menuColor: '#3b4d77',
    menuDarkItemSelectedBg: 'rgba(47, 107, 255, 0.16)',
    menuDarkItemSelectedColor: '#c2d3ff',
    /* Table(表头 / 边框 / 行 hover / 行选中) */
    tableHeaderBg: '#eaedf6',
    tableHeaderColor: '#3b4d77',
    tableBorderColor: '#d2d8ea',
    rowHoverBg: '#f0f3fa',
    rowSelectedBg: '#d0deff',
    rowSelectedHoverBg: '#c6d6ff',
    /* Button / Input(阴影 / 聚焦光圈) */
    primaryShadow: '0 1px 2px rgba(24, 34, 58, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(24, 34, 58, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(47, 107, 255, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#fafbfe',
    dateCellHoverBg: '#e4ebff',
    dateCellRangeBg: '#d0deff',
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
