/**
 * white —— 白色皮肤(默认观感)
 *
 * 2026-11-05 皮肤扁平化(docs/theming.md):皮肤 = 整体外观,不再内部分
 * dark/light。本皮肤 = 原默认观感的浅色面,值未做任何改动(逐像素一致);
 * 品牌色仍是框架既有默认橙 #f60(换品牌色相属于产品决策,不悄悄改)。
 *
 * 本文件与 black.ts 是**字段全集基准**:任何皮肤都必须定义与此处完全
 * 相同的 token 键集(skins.complete.test.ts 在守,漏一个色调即红)。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'white',
  name: '白色(默认)',
  appearance: 'light',
  brand: {
    primary: '#f60',
    hover: '#ff8c3a',
    active: '#d35400',
  },

  // 语义色板(浅色;色板与 styles/variables.css 同源,slate 中性调)
  palette: {
    colorBgBase: '#fafafa',
    colorBgLayout: '#f4f6fa',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#1f2937',
    colorBgMask: 'rgba(15, 23, 42, 0.45)',
    colorText: '#1f2937',
    colorTextSecondary: '#475569',
    colorTextTertiary: '#94a3b8',
    colorTextQuaternary: '#cbd5e1',
    colorTextDescription: '#64748b',
    colorTextPlaceholder: 'rgba(15, 23, 42, 0.35)',
    colorBorder: '#eef0f4',
    colorBorderSecondary: '#f0f2f5',
    colorSplit: 'rgba(15, 23, 42, 0.06)',
    primaryText: '#f60',
    primaryTextHover: '#ff8c3a',
    primaryTextActive: '#d35400',
    colorPrimaryBg: '#fff5e6',
    colorPrimaryBgHover: '#ffe7c2',
    colorPrimaryBorder: '#ffcaa1',
    colorPrimaryBorderHover: '#ffae6c',
    colorLink: '#f60',
    colorLinkHover: '#ff8c3a',
    colorLinkActive: '#d35400',
  },

  // 组件级 token(浅色)
  comp: {
    layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
    siderBg: '#ffffff',
    bodyBg: '#f4f6fa',
    triggerColor: '#1f2937',
    menuItemBg: '#ffffff',
    menuSelectedBg: '#fff5e6',
    menuSelectedColor: '#f60',
    menuHoverBg: '#fff7e8',
    menuHoverColor: '#f60',
    menuColor: '#475569',
    menuDarkItemSelectedBg: 'rgba(255, 102, 0, 0.16)',
    menuDarkItemSelectedColor: '#ffae6c',
    tableHeaderBg: '#f7f8fb',
    tableHeaderColor: '#475569',
    tableBorderColor: '#eef0f4',
    rowHoverBg: '#f5f7fa',
    rowSelectedBg: '#fff5e6',
    rowSelectedHoverBg: '#ffe7c2',
    primaryShadow: '0 1px 2px rgba(15, 23, 42, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(15, 23, 42, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(255, 102, 0, 0.18)',
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#fff7e8',
    dateCellRangeBg: '#fff5e6',
    breadcrumbItemColor: 'rgba(15, 23, 42, 0.65)',
    breadcrumbLastColor: '#1f2937',
    breadcrumbSeparatorColor: 'rgba(15, 23, 42, 0.3)',
    tabsActiveColor: '#f60',
    selectOptionSelectedColor: '#f60',
  },

  // --app-* 品牌系变量(与 variables.css「品牌色」段同源,全量 7 个)
  cssVars: {
    '--app-primary': '#f60',
    '--app-primary-hover': '#ff8c3a',
    '--app-primary-active': '#d35400',
    '--app-primary-bg': '#fff5e6',
    '--app-primary-bg-hover': '#ffe7c2',
    '--app-primary-border': '#ffcaa1',
    '--app-primary-border-hover': '#ffae6c',
  },
}

export default skin
