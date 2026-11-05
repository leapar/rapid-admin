/**
 * 皮肤定义类型 —— docs/theming.md §3.1
 *
 * 一套皮肤 = 一个声明式对象:品牌主色三态 + 深浅两套语义色板 +
 * 深浅两套组件级 token + 深浅两套 --app-* CSS 变量。
 *
 * ⚠️ 键名与既有 LangRoot 硬编码 palette / comp 逐一对应(2026-11-05 原样搬家,
 *   未做任何"优化")—— 首版验收标准是 orange 皮肤与搬家前逐像素一致。
 */

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
  /** 品牌文字/链接色 —— 暗底上比主色提亮一档(orange: #f60 → #ff8c3a) */
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
  /** antd 的 dark* 前缀 token(theme="dark" 菜单),现实现里跨明暗同值 —— 两套各放一份 */
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
  /* Tabs(激活/选中/hover/墨条同色,现实现写死 4 处) */
  tabsActiveColor: string
  /* Select */
  selectOptionSelectedColor: string
}

export interface SkinDefinition {
  /** 内置:'orange' | 'museum' | 'tech' | 'gov';后端 branding.skin 只认内置 id */
  id: string
  /** 展示名,例:"文博棕金" */
  name: string
  /** 品牌主色三态:主色 + hover(亮 8–12%)+ active(暗 8–12%) */
  brand: { primary: string; hover: string; active: string }
  palette: { light: PaletteTokens; dark: PaletteTokens }
  comp: { light: CompTokens; dark: CompTokens }
  /**
   * --app-* CSS 变量(运行时经 applySkinCssVars 注入 <html style>)。
   * 只放**随皮肤变**的键(品牌系必给;画布/文字/边框等选给);
   * 阴影结构/圆角/字体等皮肤无关项留在 styles/variables.css 作 fallback。
   */
  cssVars: { light: Record<string, string>; dark: Record<string, string> }
}
