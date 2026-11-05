/**
 * 皮肤定义类型 —— docs/theming.md §3.1
 *
 * 一套皮肤 = **一个整体外观**(2026-11-05 用户拍板扁平化):
 * 黑色皮肤就是黑、白色皮肤就是白 —— 皮肤内部不再分 dark/light 两套,
 * 顶栏也没有明暗切换器;想要暗色就选暗色皮肤。
 *
 * 每套皮肤含:明暗属性(决定 antd algorithm 与 <html data-theme>)
 * + 品牌主色三态 + 一套语义色板 + 一套组件级 token + 一套 --app-* CSS 变量。
 *
 * ⚠️ 字段全集以 white.ts / black.ts 为基准 —— 任何皮肤都必须定义完全
 *   相同的 token 键集(skins.complete.test.ts 在守,漏一个色调即红)。
 */

/** 该皮肤的整体明暗:决定 antd algorithm、<html data-theme> 与首帧防闪底色 */
export type SkinAppearance = 'light' | 'dark'

/** 语义色板(antd 全局 token 的品牌相关子集)—— 原 LangRoot `palette` */
export interface PaletteTokens {
  colorBgBase: string
  colorBgLayout: string
  colorBgContainer: string
  colorBgElevated: string
  colorBgSpotlight: string
  colorBgMask: string
  colorText: string
  colorTextSecondary: string
  colorTextTertiary: string
  colorTextQuaternary: string
  colorTextDescription: string
  colorTextPlaceholder: string
  colorBorder: string
  colorBorderSecondary: string
  colorSplit: string
  /** 品牌文字/链接色 —— 暗底上比主色提亮一档(white 默认: #f60 → #ff8c3a) */
  primaryText: string
  primaryTextHover: string
  primaryTextActive: string
  colorPrimaryBg: string
  colorPrimaryBgHover: string
  colorPrimaryBorder: string
  colorPrimaryBorderHover: string
  /** ⚠️ antd 的 colorLink 不设 = 回落 seed.colorInfo(#1677ff 蓝)—— 必须显式给 */
  colorLink: string
  colorLinkHover: string
  colorLinkActive: string
}

/** 组件级 token 的皮肤相关子集 —— 原 LangRoot `comp` + 原先散在 components 里的硬编码品牌值 */
export interface CompTokens {
  /* Layout */
  layoutHeaderBg: string
  siderBg: string
  bodyBg: string
  triggerColor: string
  /* Menu */
  menuItemBg: string
  menuSelectedBg: string
  menuSelectedColor: string
  menuHoverBg: string
  menuHoverColor: string
  menuColor: string
  /** antd 的 dark* 前缀 token(theme="dark" 菜单) */
  menuDarkItemSelectedBg: string
  menuDarkItemSelectedColor: string
  /* Table */
  tableHeaderBg: string
  tableHeaderColor: string
  tableBorderColor: string
  rowHoverBg: string
  rowSelectedBg: string
  rowSelectedHoverBg: string
  /* Button / Input / DatePicker */
  primaryShadow: string
  defaultShadow: string
  inputActiveShadow: string
  /* Pagination / DatePicker */
  paginationItemBg: string
  dateCellHoverBg: string
  dateCellRangeBg: string
  /* Breadcrumb */
  breadcrumbItemColor: string
  breadcrumbLastColor: string
  breadcrumbSeparatorColor: string
  /* Tabs(激活/选中/hover/墨条同色) */
  tabsActiveColor: string
  /* Select */
  selectOptionSelectedColor: string
}

export interface SkinDefinition {
  /** 内置:'white' | 'black' | 'tech' | 'gov' | 'museum'(旧 'orange' 由 registry 别名到 white) */
  id: string
  /** 展示名,例:"文博棕金" */
  name: string
  /** 该皮肤的整体明暗(黑=dark,其余内置=light) */
  appearance: SkinAppearance
  /** 品牌主色三态:主色 + hover(亮 8–12%)+ active(暗 8–12%) */
  brand: { primary: string; hover: string; active: string }
  palette: PaletteTokens
  comp: CompTokens
  /**
   * --app-* CSS 变量(运行时经 applySkinCssVars 注入 <html style>)。
   * 品牌系 7 键必给;画布/文字/边框等选给;允许皮肤追加专属变量
   * (如 tech 的带色阴影)。阴影结构/圆角/字体等皮肤无关项留在
   * styles/variables.css 作 fallback。
   */
  cssVars: Record<string, string>
}
