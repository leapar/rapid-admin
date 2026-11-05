/**
 * gov —— 政务红(显式全量定义,逐参数可改)
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时五套皮肤要同步补。
 * 取值策略:**中性面纯净**(与白色皮肤同档位的纯灰)—— 红色只出现在
 * 强调位(按钮/链接/菜单选中/表格选中/聚焦光圈),这样整站读出来才是
 * "红主题";中性面若掺红相会发暖发棕,与文博棕金难以区分(2026-11-05
 * 用户实测反馈后从"红相染中性面"改回纯灰中性面)。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'gov',
  name: '政务红',
  appearance: 'light',
  /* 品牌三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: '#C7000B', hover: '#f5000e', active: '#990008' },

  palette: {
    /* 画布(布局底 / 容器 / 浮层 / 遮罩)—— 纯灰,不掺红相 */
    colorBgBase: '#fafafa',
    colorBgLayout: '#f6f6f6',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#2b2b2b',
    colorBgMask: 'rgba(43, 43, 43, 0.45)',
    /* 文字梯度(主 → 四级 + 描述 + 占位)—— 纯灰 */
    colorText: '#292929',
    colorTextSecondary: '#595959',
    colorTextTertiary: '#a1a1a1',
    colorTextQuaternary: '#c7c7c7',
    colorTextDescription: '#6b6b6b',
    colorTextPlaceholder: 'rgba(41, 41, 41, 0.35)',
    /* 边框 / 分割线 —— 纯灰 */
    colorBorder: '#ebebeb',
    colorBorderSecondary: '#f0f0f0',
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
    /* Layout(顶栏毛玻璃 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
    siderBg: '#ffffff',
    bodyBg: '#f6f6f6',
    triggerColor: '#292929',
    /* Menu(侧边菜单)—— 选中/hover 走红浅底,品牌红 */
    menuItemBg: '#ffffff',
    menuSelectedBg: '#ffdbdd',
    menuSelectedColor: '#C7000B',
    menuHoverBg: '#ffebec',
    menuHoverColor: '#C7000B',
    menuColor: '#595959',
    menuDarkItemSelectedBg: 'rgba(199, 0, 11, 0.16)',
    menuDarkItemSelectedColor: '#ff616a',
    /* Table(表头 / 边框 / 行 hover / 行选中)—— 选中行走红浅底 */
    tableHeaderBg: '#f6f6f6',
    tableHeaderColor: '#595959',
    tableBorderColor: '#ebebeb',
    rowHoverBg: '#f9f9f9',
    rowSelectedBg: '#ffdbdd',
    rowSelectedHoverBg: '#ffd1d4',
    /* Button / Input(阴影 / 聚焦光圈)—— 阴影纯灰,光圈红 */
    primaryShadow: '0 1px 2px rgba(41, 41, 41, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(41, 41, 41, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(199, 0, 11, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#ffebec',
    dateCellRangeBg: '#ffdbdd',
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
