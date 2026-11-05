/**
 * 皮肤类型 —— docs/theming.md §3.1
 *
 * 皮肤 = **整体外观**(含明暗),显式全量定义:skins/<id>.ts 把
 * palette / comp / cssVars 的每个颜色键逐一写出来(逐参数可改,方便
 * 拷走改成自己的品牌色)。内置五套的取值是 2026-11-05 按 antd
 * "seed → map token" 算法理念整体重调过的 —— 中性面同一色温、品牌浅底
 * 同系衍生,不存在"棕色皮肤配蓝灰 header"这类割裂。
 */

/** 该皮肤的整体明暗:决定 antd algorithm、<html data-theme> 与首帧防闪底色 */
export type SkinAppearance = 'light' | 'dark'

/** 语义色板(antd 全局 token 的品牌相关子集)—— LangRoot `token` 段 */
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
  /** 品牌文字色 —— 暗底上比主色提亮一档(参考 black 皮肤) */
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

/** 组件级 token 的皮肤相关子集 —— LangRoot `components` 段 */
export interface CompTokens {
  /* Layout(header 毛玻璃或沉浸实底 / 侧栏 / 画布) */
  layoutHeaderBg: string
  siderBg: string
  bodyBg: string
  triggerColor: string
  /**
   * Header 沉浸式配套 —— Header.tsx 消费(内联 remap 到 --app-text* 变量)。
   * 浅玻璃顶栏(white/black)写常规深字;实底顶栏(gov/museum/tech)写
   * 浅色文字,保证深底上的对比度。
   */
  headerTextColor: string
  headerTextSecondaryColor: string
  headerTextTertiaryColor: string
  headerBorderColor: string
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

/** 完整皮肤定义,LangRoot 只消费这个; skins/<id>.ts 直接写全量 */
export interface SkinDefinition {
  id: string
  name: string
  appearance: SkinAppearance
  /** 品牌主色三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: string; hover: string; active: string }
  palette: PaletteTokens
  comp: CompTokens
  /**
   * --app-* CSS 变量(运行时经 applySkinCssVars 注入 <html style>)。
   * 品牌系 7 键必给;允许皮肤追加专属变量(如 tech 的带色阴影)。
   */
  cssVars: Record<string, string>
}
