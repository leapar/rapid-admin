/**
 * custom.example.ts —— 自定义皮肤模板
 *
 * 用法(docs/theming.md §5,约 20 分钟):
 *   1. cp custom.example.ts my-brand.ts
 *   2. 改 id / name / brand 三态 + palette 浅底(其余键保持模板默认就已协调)
 *   3. 在 ../registry.ts 的 SKINS 表挂一行 → 顶栏皮肤切换器立即可选
 *
 * 必改 6 键(其余都是模板派生好的):
 *   brand.primary/hover/active + palette.light.colorBgLayout/colorPrimaryBg
 *   + palette.dark.colorPrimaryBg
 *
 * 选色心法(theming.md §5,照做就不会丑):
 *   1. hover 亮 8–12%,active 暗 8–12%
 *   2. 一切浅底(Bg 系)用主色 ≥10% 透明度或 95% 明度的衍生色,别手调新色相
 *   3. 深色模式"品牌文字/链接"比主色提亮一档(orange: #f60 → #ff8c3a)
 *   4. 阴影别动结构,只把 slate 色相换成主色淡晕(参考 tech.ts)
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'my-brand', // ← 唯一 id,别和内置 orange/museum/tech/gov 撞
  name: '我的品牌',
  brand: {
    primary: '#7C3AED',
    hover: '#9B5CF6',
    active: '#5B21B6',
  },

  palette: {
    dark: {
      // 中性黑基底直接沿用(大多皮肤不需要动);只把品牌系换成你的主色
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
      // 暗底品牌文字比主色提亮一档(心法 3)
      primaryText: '#B794F6',
      primaryTextHover: '#CBADFA',
      primaryTextActive: '#DFC8FD',
      colorPrimaryBg: 'rgba(124, 58, 237, 0.12)',
      colorPrimaryBgHover: 'rgba(124, 58, 237, 0.2)',
      colorPrimaryBorder: 'rgba(124, 58, 237, 0.32)',
      colorPrimaryBorderHover: 'rgba(124, 58, 237, 0.5)',
      colorLink: '#B794F6',
      colorLinkHover: '#CBADFA',
      colorLinkActive: '#DFC8FD',
    },
    light: {
      colorBgLayout: '#F6F4FA', // ← 画布底色(必改):主色 95% 明度衍生
      colorBgBase: '#FCFBFE',
      colorBgContainer: '#ffffff',
      colorBgElevated: '#ffffff',
      colorBgSpotlight: '#241A38',
      colorBgMask: 'rgba(36, 26, 56, 0.45)',
      colorText: '#241A38',
      colorTextSecondary: '#4A3E63',
      colorTextTertiary: '#9188A8',
      colorTextQuaternary: '#C0B9D4',
      colorTextDescription: '#5E5378',
      colorTextPlaceholder: 'rgba(36, 26, 56, 0.35)',
      colorBorder: '#EAE6F4',
      colorBorderSecondary: '#F0EDF8',
      colorSplit: 'rgba(36, 26, 56, 0.06)',
      primaryText: '#7C3AED',
      primaryTextHover: '#9B5CF6',
      primaryTextActive: '#5B21B6',
      colorPrimaryBg: '#F1EAFD', // ← 浅底(必改)
      colorPrimaryBgHover: '#E6D9FB',
      colorPrimaryBorder: '#CDB4F5',
      colorPrimaryBorderHover: '#B18EEF',
      colorLink: '#7C3AED',
      colorLinkHover: '#9B5CF6',
      colorLinkActive: '#5B21B6',
    },
  },

  // comp / cssVars 模板已按主色派生好 —— 只微调 menuSelectedBg /
  // rowSelectedBg / --app-primary-bg 三组就能再贴近品牌气质
  comp: {
    dark: {
      layoutHeaderBg: 'rgba(26, 26, 26, 0.78)',
      siderBg: '#262626',
      bodyBg: '#1a1a1a',
      triggerColor: '#e8e8e8',
      menuItemBg: 'transparent',
      menuSelectedBg: 'rgba(124, 58, 237, 0.16)',
      menuSelectedColor: '#B794F6',
      menuHoverBg: 'rgba(255, 255, 255, 0.06)',
      menuHoverColor: '#B794F6',
      menuColor: '#bfbfbf',
      menuDarkItemSelectedBg: 'rgba(124, 58, 237, 0.16)',
      menuDarkItemSelectedColor: '#B794F6',
      tableHeaderBg: 'rgba(255, 255, 255, 0.05)',
      tableHeaderColor: '#bfbfbf',
      tableBorderColor: 'rgba(255, 255, 255, 0.1)',
      rowHoverBg: 'rgba(255, 255, 255, 0.04)',
      rowSelectedBg: 'rgba(124, 58, 237, 0.12)',
      rowSelectedHoverBg: 'rgba(124, 58, 237, 0.2)',
      primaryShadow: '0 1px 2px rgba(0, 0, 0, 0.4), inset 0 1px 0 rgba(255, 255, 255, 0.12)',
      defaultShadow: '0 1px 2px rgba(0, 0, 0, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.06)',
      inputActiveShadow: '0 0 0 3px rgba(124, 58, 237, 0.25)',
      paginationItemBg: 'transparent',
      dateCellHoverBg: 'rgba(124, 58, 237, 0.12)',
      dateCellRangeBg: 'rgba(124, 58, 237, 0.2)',
      breadcrumbItemColor: 'rgba(232, 232, 232, 0.65)',
      breadcrumbLastColor: '#e8e8e8',
      breadcrumbSeparatorColor: 'rgba(232, 232, 232, 0.3)',
      tabsActiveColor: '#B794F6',
      selectOptionSelectedColor: '#B794F6',
    },
    light: {
      layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
      siderBg: '#ffffff',
      bodyBg: '#F6F4FA',
      triggerColor: '#241A38',
      menuItemBg: '#ffffff',
      menuSelectedBg: '#F1EAFD',
      menuSelectedColor: '#7C3AED',
      menuHoverBg: '#F5EFFE',
      menuHoverColor: '#7C3AED',
      menuColor: '#4A3E63',
      menuDarkItemSelectedBg: 'rgba(124, 58, 237, 0.16)',
      menuDarkItemSelectedColor: '#B794F6',
      tableHeaderBg: '#F6F2FC',
      tableHeaderColor: '#4A3E63',
      tableBorderColor: '#EAE6F4',
      rowHoverBg: '#F7F4FD',
      rowSelectedBg: '#F1EAFD',
      rowSelectedHoverBg: '#E6D9FB',
      primaryShadow: '0 1px 2px rgba(36, 26, 56, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
      defaultShadow: '0 1px 2px rgba(36, 26, 56, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
      inputActiveShadow: '0 0 0 3px rgba(124, 58, 237, 0.18)',
      paginationItemBg: '#ffffff',
      dateCellHoverBg: '#F5EFFE',
      dateCellRangeBg: '#F1EAFD',
      breadcrumbItemColor: 'rgba(36, 26, 56, 0.65)',
      breadcrumbLastColor: '#241A38',
      breadcrumbSeparatorColor: 'rgba(36, 26, 56, 0.3)',
      tabsActiveColor: '#7C3AED',
      selectOptionSelectedColor: '#7C3AED',
    },
  },

  cssVars: {
    light: {
      '--app-primary': '#7C3AED',
      '--app-primary-hover': '#9B5CF6',
      '--app-primary-active': '#5B21B6',
      '--app-primary-bg': '#F1EAFD',
      '--app-primary-bg-hover': '#E6D9FB',
      '--app-primary-border': '#CDB4F5',
      '--app-primary-border-hover': '#B18EEF',
    },
    dark: {
      '--app-primary-bg': 'rgba(124, 58, 237, 0.12)',
      '--app-primary-bg-hover': 'rgba(124, 58, 237, 0.2)',
      '--app-primary-border': 'rgba(124, 58, 237, 0.32)',
      '--app-primary-border-hover': 'rgba(124, 58, 237, 0.5)',
    },
  },
}

export default skin
