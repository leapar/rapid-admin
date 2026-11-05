/**
 * gov —— 政务红(政务 / 国企)
 *
 * 规格:docs/theming.md §4.3。国旗红主色 + 微暖灰画布。
 * ⚠️ 唯一的语义冲突点:品牌红 vs error 红 —— 对策(§6):品牌浅底走
 * `rgba(199,0,11,x)` 暗红系,**不动** `--app-error-*`(保持 #FF4D4F 橙红系,
 * 留在 variables.css 不覆盖),错误态与品牌态拉开明度差。
 * 扁平模型(2026-11-05):浅色外观。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'gov',
  name: '政务红',
  appearance: 'light',
  brand: {
    primary: '#C7000B',
    hover: '#E02020',
    active: '#A3000A',
  },

  // 语义色板(微暖灰系)
  palette: {
    colorBgBase: '#FBF9F8',
    colorBgLayout: '#F9F5F4', // 微暖灰
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#33201F',
    colorBgMask: 'rgba(51, 32, 31, 0.45)',
    colorText: '#33201F',
    colorTextSecondary: '#5C4040',
    colorTextTertiary: '#9C7B78',
    colorTextQuaternary: '#C9B2AE',
    colorTextDescription: '#75514E',
    colorTextPlaceholder: 'rgba(51, 32, 31, 0.35)',
    colorBorder: '#F0E6E4',
    colorBorderSecondary: '#F4EDEB',
    colorSplit: 'rgba(51, 32, 31, 0.06)',
    primaryText: '#C7000B',
    primaryTextHover: '#E02020',
    primaryTextActive: '#A3000A',
    colorPrimaryBg: '#FBE8E8',
    colorPrimaryBgHover: '#F7D4D4',
    colorPrimaryBorder: '#F0AEAE',
    colorPrimaryBorderHover: '#E68989',
    colorLink: '#C7000B',
    colorLinkHover: '#E02020',
    colorLinkActive: '#A3000A',
  },

  // 组件级 token
  comp: {
    layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
    siderBg: '#ffffff',
    bodyBg: '#F9F5F4',
    triggerColor: '#33201F',
    menuItemBg: '#ffffff',
    menuSelectedBg: '#F9E5E5',
    menuSelectedColor: '#C7000B',
    menuHoverBg: '#FBE8E8',
    menuHoverColor: '#C7000B',
    menuColor: '#5C4040',
    menuDarkItemSelectedBg: 'rgba(199, 0, 11, 0.16)',
    menuDarkItemSelectedColor: '#F57C7C',
    tableHeaderBg: '#F7F0EE',
    tableHeaderColor: '#5C4040',
    tableBorderColor: '#F0E6E4',
    rowHoverBg: '#F8F1EF',
    rowSelectedBg: '#F9E5E5',
    rowSelectedHoverBg: '#F7D4D4',
    primaryShadow: '0 1px 2px rgba(51, 32, 31, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(51, 32, 31, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(199, 0, 11, 0.18)',
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#FBE8E8',
    dateCellRangeBg: '#FBE8E8',
    breadcrumbItemColor: 'rgba(51, 32, 31, 0.65)',
    breadcrumbLastColor: '#33201F',
    breadcrumbSeparatorColor: 'rgba(51, 32, 31, 0.3)',
    tabsActiveColor: '#C7000B',
    selectOptionSelectedColor: '#C7000B',
  },

  // --app-* 品牌系变量(全量 7 个;--app-error-* 故意不覆盖,见头注释)
  cssVars: {
    '--app-primary': '#C7000B',
    '--app-primary-hover': '#E02020',
    '--app-primary-active': '#A3000A',
    '--app-primary-bg': '#FBE8E8',
    '--app-primary-bg-hover': '#F7D4D4',
    '--app-primary-border': '#F0AEAE',
    '--app-primary-border-hover': '#E68989',
  },
}

export default skin
