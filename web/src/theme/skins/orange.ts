/**
 * orange —— 默认皮肤(品牌橙)
 *
 * 2026-11-05 从 LangRoot.tsx 的 palette/comp 与 variables.css 的品牌段
 * **原样搬入**(docs/theming.md P0)—— 值未做任何改动,验收标准:逐像素一致。
 *
 * 深浅两套色板的来源注释一并搬入,保持决策可追溯。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'orange',
  name: '品牌橙(默认)',
  brand: {
    primary: '#f60',
    hover: '#ff8c3a',
    active: '#d35400',
  },

  // ============================================================
  // 深浅两套语义色板(2026-11-03 深色模式补全)
  //
  // ⚠️ 为什么必须显式给两套:token 里的**显式值优先级高于** darkAlgorithm
  //   的推导值 —— 之前 colorBgContainer '#ffffff' / Table.headerBg '#f7f8fb'
  //   这些写死浅色,切了 darkAlgorithm 表面/文字/边框照样浅色。
  //
  // 色板与 styles/variables.css 的暗色段同源(中性黑 + 品牌橙):
  //   画布 #101010 / 表面 #212121 / 浮层 #313131 / 实边框 #404040
  //   文字 #e8e8e8 → #bfbfbf → #8c8c8c → #666666
  //   品牌橙本体跨主题不变;暗底上的"品牌文字/链接"提亮一档到 #ff8c3a 系;
  //   品牌浅底改半透明橙(实色浅橙底在深色下刺眼)。
  // ============================================================
  palette: {
    dark: {
      // 中性黑系(2026-11-03 用户拍板):画布 #1a1a1a / 表面 #262626 /
      // 浮层 #333333 / 实边框与 tooltip 底 #404040
      colorBgBase: '#1a1a1a',
      colorBgLayout: '#1a1a1a',
      colorBgContainer: '#262626',
      colorBgElevated: '#333333',
      colorBgSpotlight: '#404040',
      colorBgMask: 'rgba(0, 0, 0, 0.65)',
      colorText: '#e8e8e8',
      colorTextSecondary: '#bfbfbf',
      colorTextTertiary: '#8c8c8c',
      colorTextQuaternary: '#666666',
      colorTextDescription: '#8c8c8c',
      colorTextPlaceholder: 'rgba(232, 232, 232, 0.3)',
      colorBorder: '#404040',
      colorBorderSecondary: '#333333',
      colorSplit: 'rgba(255, 255, 255, 0.08)',
      primaryText: '#ff8c3a',
      primaryTextHover: '#ffa35c',
      primaryTextActive: '#ffc08a',
      colorPrimaryBg: 'rgba(255, 102, 0, 0.12)',
      colorPrimaryBgHover: 'rgba(255, 102, 0, 0.2)',
      colorPrimaryBorder: 'rgba(255, 102, 0, 0.32)',
      colorPrimaryBorderHover: 'rgba(255, 102, 0, 0.5)',
      colorLink: '#ff8c3a',
      colorLinkHover: '#ffa35c',
      colorLinkActive: '#ffc08a',
    },
    light: {
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
  },

  // 组件级深浅差异(与 palette 同理:不写死,跟 isDark 走)。
  // menuDark* / tabsActiveColor / selectOptionSelectedColor 在原实现里跨明暗同值,
  // 搬家时按"原样"在两套里放同一份值 —— 想按明暗分化时改对应那套即可。
  comp: {
    dark: {
      layoutHeaderBg: 'rgba(26, 26, 26, 0.78)',
      siderBg: '#262626',
      bodyBg: '#1a1a1a',
      triggerColor: '#e8e8e8',
      menuItemBg: 'transparent',
      menuSelectedBg: 'rgba(255, 102, 0, 0.16)',
      menuSelectedColor: '#ffae6c',
      menuHoverBg: 'rgba(255, 255, 255, 0.06)',
      menuHoverColor: '#ffae6c',
      menuColor: '#bfbfbf',
      menuDarkItemSelectedBg: 'rgba(255, 102, 0, 0.16)',
      menuDarkItemSelectedColor: '#ffae6c',
      tableHeaderBg: 'rgba(255, 255, 255, 0.05)',
      tableHeaderColor: '#bfbfbf',
      tableBorderColor: 'rgba(255, 255, 255, 0.1)',
      rowHoverBg: 'rgba(255, 255, 255, 0.04)',
      rowSelectedBg: 'rgba(255, 102, 0, 0.12)',
      rowSelectedHoverBg: 'rgba(255, 102, 0, 0.2)',
      primaryShadow: '0 1px 2px rgba(0, 0, 0, 0.4), inset 0 1px 0 rgba(255, 255, 255, 0.12)',
      defaultShadow: '0 1px 2px rgba(0, 0, 0, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.06)',
      inputActiveShadow: '0 0 0 3px rgba(255, 102, 0, 0.25)',
      paginationItemBg: 'transparent',
      dateCellHoverBg: 'rgba(255, 102, 0, 0.12)',
      dateCellRangeBg: 'rgba(255, 102, 0, 0.2)',
      breadcrumbItemColor: 'rgba(232, 232, 232, 0.65)',
      breadcrumbLastColor: '#e8e8e8',
      breadcrumbSeparatorColor: 'rgba(232, 232, 232, 0.3)',
      tabsActiveColor: '#f60',
      selectOptionSelectedColor: '#f60',
    },
    light: {
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
  },

  // --app-* 品牌系变量(与 variables.css「品牌色 (橙)」段同源):
  // light 给全量 7 个;dark 段原实现只覆盖 4 个浅底(主色本体跨主题不变)。
  cssVars: {
    light: {
      '--app-primary': '#f60',
      '--app-primary-hover': '#ff8c3a',
      '--app-primary-active': '#d35400',
      '--app-primary-bg': '#fff5e6',
      '--app-primary-bg-hover': '#ffe7c2',
      '--app-primary-border': '#ffcaa1',
      '--app-primary-border-hover': '#ffae6c',
    },
    dark: {
      '--app-primary-bg': 'rgba(255, 102, 0, 0.12)',
      '--app-primary-bg-hover': 'rgba(255, 102, 0, 0.2)',
      '--app-primary-border': 'rgba(255, 102, 0, 0.32)',
      '--app-primary-border-hover': 'rgba(255, 102, 0, 0.5)',
    },
  },
}

export default skin
