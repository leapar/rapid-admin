/**
 * custom.example.ts —— 自定义皮肤模板(显式全量定义,逐参数可改)
 *
 * 用法(约 15 分钟):
 *   1. cp custom.example.ts my-brand.ts
 *   2. 逐参数改色:id / name / brand 三态必改;palette / comp / cssVars
 *      按需微调(键都有分组注释,对着内置皮肤同键位抄档位最稳)
 *   3. 在 ../registry.ts 的 SKINS 表挂一行 → 顶栏皮肤切换器立即可选
 *
 * ⚠️ 字段全集守卫:skins.complete.test.ts 要求 palette / comp / cssVars
 * 与基准键集一致(漏一个色调即红);新增 token 键时所有皮肤要同步补。
 * 这份模板的取值是 2026-11-05 按 antd seed → map 理念重调过的紫色系 ——
 * 中性面同一色温、品牌浅底同系衍生;改单个色时保持同明度档位即可。
 */
import type { SkinDefinition } from '../types'

const skin: SkinDefinition = {
  id: 'my-brand',
  name: '我的品牌',
  appearance: 'light',
  /* 品牌三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: '#7C3AED', hover: '#9864f1', active: '#6115e4' },

  palette: {
    /* 画布(布局底 / 容器 / 浮层 / 遮罩) */
    colorBgBase: '#faf9fb',
    colorBgLayout: '#f5f3f9',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgSpotlight: '#271d3a',
    colorBgMask: 'rgba(39, 29, 58, 0.45)',
    /* 文字梯度(主 → 四级 + 描述 + 占位) */
    colorText: '#261d35',
    colorTextSecondary: '#54456e',
    colorTextTertiary: '#9c90b1',
    colorTextQuaternary: '#c5bfcf',
    colorTextDescription: '#655581',
    colorTextPlaceholder: 'rgba(38, 29, 53, 0.35)',
    /* 边框 / 分割线 */
    colorBorder: '#e9e5f1',
    colorBorderSecondary: '#efebf4',
    colorSplit: 'rgba(38, 29, 53, 0.06)',
    /* 品牌强调(文字 / 浅底 / 边框 / 链接) */
    primaryText: '#7C3AED',
    primaryTextHover: '#9864f1',
    primaryTextActive: '#6115e4',
    colorPrimaryBg: '#efe8fd',
    colorPrimaryBgHover: '#e3d5fb',
    colorPrimaryBorder: '#d0b9f9',
    colorPrimaryBorderHover: '#b18af4',
    colorLink: '#7C3AED',
    colorLinkHover: '#9864f1',
    colorLinkActive: '#6115e4',
  },

  comp: {
    /* Layout(顶栏毛玻璃 / 侧栏 / 画布 / 触发器) */
    layoutHeaderBg: 'rgba(255, 255, 255, 0.78)',
    /* Header 沉浸式配套 —— 想要沉浸实底(如参考 gov/museum)时:
       layoutHeaderBg 换实色,下面四个键换浅色文字 */
    headerTextColor: '#261d35',
    headerTextSecondaryColor: '#54456e',
    headerTextTertiaryColor: '#9c90b1',
    headerBorderColor: 'rgba(38, 29, 53, 0.06)',
    siderBg: '#ffffff',
    bodyBg: '#f5f3f9',
    triggerColor: '#261d35',
    /* Menu(侧边菜单) */
    menuItemBg: '#ffffff',
    menuSelectedBg: '#e9defc',
    menuSelectedColor: '#7C3AED',
    menuHoverBg: '#f3ecfd',
    menuHoverColor: '#7C3AED',
    menuColor: '#54456e',
    menuDarkItemSelectedBg: 'rgba(124, 58, 237, 0.16)',
    menuDarkItemSelectedColor: '#d9c6fa',
    /* Table(表头 / 边框 / 行 hover / 行选中) */
    tableHeaderBg: '#f5f3f9',
    tableHeaderColor: '#54456e',
    tableBorderColor: '#e9e5f1',
    rowHoverBg: '#f8f7fa',
    rowSelectedBg: '#e9defc',
    rowSelectedHoverBg: '#e3d5fb',
    /* Button / Input(阴影 / 聚焦光圈) */
    primaryShadow: '0 1px 2px rgba(38, 29, 53, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.25)',
    defaultShadow: '0 1px 2px rgba(38, 29, 53, 0.04), inset 0 1px 0 rgba(255, 255, 255, 0.5)',
    inputActiveShadow: '0 0 0 3px rgba(124, 58, 237, 0.18)',
    /* Pagination / DatePicker */
    paginationItemBg: '#ffffff',
    dateCellHoverBg: '#f3ecfd',
    dateCellRangeBg: '#e9defc',
    /* Breadcrumb / Tabs / Select */
    breadcrumbItemColor: 'rgba(38, 29, 53, 0.65)',
    breadcrumbLastColor: '#261d35',
    breadcrumbSeparatorColor: 'rgba(38, 29, 53, 0.3)',
    tabsActiveColor: '#7C3AED',
    selectOptionSelectedColor: '#7C3AED',
  },

  /* --app-* CSS 变量(applySkinCssVars 注入 <html style>);品牌系 7 键必给 */
  cssVars: {
    '--app-primary': '#7C3AED',
    '--app-primary-hover': '#9864f1',
    '--app-primary-active': '#6115e4',
    '--app-primary-bg': '#efe8fd',
    '--app-primary-bg-hover': '#e3d5fb',
    '--app-primary-border': '#d0b9f9',
    '--app-primary-border-hover': '#b18af4',
  },
}

export default skin
