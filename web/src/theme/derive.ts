/**
 * 皮肤建议值生成器 —— 皮肤编辑器(/system/skin)的"从主色派生"引擎
 *
 * # 架构定位(关键,别越界)
 *
 * 运行时**没有推导层** —— 内置皮肤是显式全量定义(theming.md §3.1),
 * 本文件生成的只是**编辑器里的建议值**:生成结果直接落到具体色值,
 * 用户在高级模式可逐键覆盖;导出/保存的产物与内置皮肤**同构**
 * (同一 SkinDefinition,不存在第二套格式)。
 *
 * # 算法:参考皮肤重色相(rehue)—— 双输入
 *
 * 以 museum(浅色)/ black(深色)为**结构参考** —— 它们的明度阶梯
 * (画布 90% / 容器 99% / 文字 16%…)承载了全部对比度关系。
 * **两个互相独立的创意输入**(2026-11-05 用户拍板拆开):
 * - **主色**(品牌强调色):只决定按钮 / 链接 / 选中态等强调键 ——
 *   white / black 皮肤的中性面与 #f60 无关,就是这个道理;
 * - **界面基色 surface**:决定中性面(画布 / 文字 / 边框 / 顶栏 / 侧栏)的
 *   色相与浓度 —— 选纯灰(s=0)即 white / black 式"无彩中性底"。
 *
 * 换肤 = 把参考皮肤每个色值的色相换成所属输入的色相、饱和度按
 * (输入饱和度 / 参考锚点饱和度)等比缩放,**明度全部保留**。
 * 这样"浅底比容器浅、文字比画布深"等所有关系自动成立,用户怎么选
 * 都不会配出脏色。纯灰(s=0)不受影响;rgba()/阴影串里的色值
 * 一并重着色(alpha 不动)。
 */
import type { SkinDefinition, SkinAppearance, PaletteTokens, CompTokens } from './types'
import museum from './skins/museum'
import black from './skins/black'

/* ====================== 颜色数学 ====================== */

export interface Hsl {
  h: number
  s: number
  l: number
}

/** hex(#rgb/#rrggbb)→ HSL(h:0-360,s/l:0-100;灰色的 h=0) */
export function hexToHsl(hex: string): Hsl {
  const v = hex.replace('#', '')
  const full = v.length === 3 ? v.split('').map((c) => c + c).join('') : v
  const n = parseInt(full, 16)
  const rn = ((n >> 16) & 255) / 255
  const gn = ((n >> 8) & 255) / 255
  const bn = (n & 255) / 255
  const max = Math.max(rn, gn, bn)
  const min = Math.min(rn, gn, bn)
  const l = ((max + min) / 2) * 100
  if (max === min) return { h: 0, s: 0, l }
  const d = max - min
  const s = (l > 50 ? d / (2 - max - min) : d / (max + min)) * 100
  let h: number
  if (max === rn) h = ((gn - bn) / d + (gn < bn ? 6 : 0)) * 60
  else if (max === gn) h = ((bn - rn) / d + 2) * 60
  else h = ((rn - gn) / d + 4) * 60
  return { h, s, l }
}

/** HSL → #rrggbb(四舍五入,与 antd 皮肤文件同精度) */
export function hslToHex({ h, s, l }: Hsl): string {
  const sn = s / 100
  const ln = l / 100
  const c = (1 - Math.abs(2 * ln - 1)) * sn
  const hp = (((h % 360) + 360) % 360) / 60
  const x = c * (1 - Math.abs((hp % 2) - 1))
  let rn = 0, gn = 0, bn = 0
  if (hp < 1) [rn, gn, bn] = [c, x, 0]
  else if (hp < 2) [rn, gn, bn] = [x, c, 0]
  else if (hp < 3) [rn, gn, bn] = [0, c, x]
  else if (hp < 4) [rn, gn, bn] = [0, x, c]
  else if (hp < 5) [rn, gn, bn] = [x, 0, c]
  else [rn, gn, bn] = [c, 0, x]
  const m = ln - c / 2
  const to = (v: number): string =>
    Math.round((v + m) * 255)
      .toString(16)
      .padStart(2, '0')
  return `#${to(rn)}${to(gn)}${to(bn)}`
}

function rgbToHsl(r: number, g: number, b: number): Hsl {
  const hex = `#${[r, g, b].map((v) => Math.round(v).toString(16).padStart(2, '0')).join('')}`
  return hexToHsl(hex)
}

/** 把字符串里所有 #hex 与 rgba(r,g,b,a) 色值重色相(保明度,scale 缩放饱和度) */
function recolor(value: string, hue: number, scale: number): string {
  return value
    .replace(/#[0-9a-fA-F]{6}\b|#[0-9a-fA-F]{3}\b/g, (m) => {
      const { s, l } = hexToHsl(m)
      return hslToHex({ h: hue, s: Math.min(100, s * scale), l })
    })
    .replace(/rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*([^)]*)\)/g, (_m, r, g, b, rest) => {
      const { s, l } = rgbToHsl(+r, +g, +b)
      const hex = hslToHex({ h: hue, s: Math.min(100, s * scale), l })
      const n = parseInt(hex.slice(1), 16)
      const parts = `${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}`
      return rest.trim() ? `rgba(${parts}${rest})` : `rgb(${parts})`
    })
}

/* ====================== 建议值生成 ====================== */

/** 浅色参考 = 文博棕金(暖棕中性面,饱和度档位最"标准");深色参考 = 黑 */
const LIGHT_REF = museum
const DARK_REF = black

/** 生成建议值的入参(编辑器基础模式的全部输入) */
export interface SuggestInput {
  appearance: SkinAppearance
  /** 品牌主色(#rrggbb)—— 按钮/链接/选中态等**强调键**的色相来源 */
  primary: string
  /**
   * 界面基色(#rrggbb)—— 中性面(画布/文字/边框/顶栏/侧栏)的色相与
   * 浓度来源;选纯灰即 white/black 式无彩底。缺省跟随主色(单输入旧行为)。
   */
  surface?: string
  /** hover/active 不传就按主色明度 L±8 派生 */
  hover?: string
  active?: string
}

/**
 * 跟随**品牌主色**的强调键 —— 其余 palette/comp 键全部跟随界面基色。
 * 划分依据 = museum 的取值:强调键的色相与 brand.primary 同族,
 * 中性键的色相与 colorBgLayout 同族(文字梯度也带界面色相,是"暖灰"不是品牌色)。
 */
const BRAND_PALETTE_KEYS = new Set<string>([
  'primaryText',
  'primaryTextHover',
  'primaryTextActive',
  'colorPrimaryBg',
  'colorPrimaryBgHover',
  'colorPrimaryBorder',
  'colorPrimaryBorderHover',
  'colorLink',
  'colorLinkHover',
  'colorLinkActive',
])
const BRAND_COMP_KEYS = new Set<string>([
  'menuSelectedBg',
  'menuSelectedColor',
  'menuHoverColor',
  'menuDarkItemSelectedBg',
  'menuDarkItemSelectedColor',
  'rowSelectedBg',
  'rowSelectedHoverBg',
  'dateCellRangeBg',
  'inputActiveShadow',
  'tabsActiveColor',
  'selectOptionSelectedColor',
])

/**
 * 生成一套完整的 palette + comp + cssVars + brand 建议值。
 * 结构(键集)与内置皮肤完全一致 —— `skins.complete.test.ts` 的字段
 * 全集守卫对生成的皮肤同样成立。
 */
export function suggestSkinParts(input: SuggestInput): {
  brand: SkinDefinition['brand']
  palette: PaletteTokens
  comp: CompTokens
  cssVars: Record<string, string>
} {
  const ref = input.appearance === 'dark' ? DARK_REF : LIGHT_REF
  const { h: hue, s: sBrand, l: lPrimary } = hexToHsl(input.primary)
  const { s: sRef } = hexToHsl(ref.brand.primary)
  const scale = sRef === 0 ? 1 : sBrand / sRef

  // 界面基色(缺省跟随主色 = 单输入旧行为);饱和度锚点取参考皮肤三个
  // 代表性中性面的最大值(下限 4 防除零,上限 3 防深色参考低饱和锚点失控)
  const surf = input.surface ?? input.primary
  const { h: hueS, s: sSurf } = hexToHsl(surf)
  const refSurfAnchor = Math.max(
    4,
    hexToHsl(ref.palette.colorBgLayout).s,
    hexToHsl(ref.palette.colorBgContainer).s,
    hexToHsl(ref.comp.siderBg).s
  )
  const scaleS = sSurf === 0 ? 0 : Math.min(3, sSurf / refSurfAnchor)

  // hover/active 从**用户主色**的明度派生(L±8,theming.md §5 心法),
  // 不从参考皮肤继承 —— 参考档位只在用户主色明度与参考一致时才成立
  const brand: SkinDefinition['brand'] = {
    primary: input.primary,
    hover: input.hover ?? hslToHex({ h: hue, s: sBrand, l: Math.min(96, lPrimary + 8) }),
    active: input.active ?? hslToHex({ h: hue, s: sBrand, l: Math.max(4, lPrimary - 8) }),
  }
  const palette = Object.fromEntries(
    Object.entries(ref.palette).map(([k, v]) => {
      const brandKey = BRAND_PALETTE_KEYS.has(k)
      return [k, recolor(v, brandKey ? hue : hueS, brandKey ? scale : scaleS)]
    })
  ) as unknown as PaletteTokens
  const comp = Object.fromEntries(
    Object.entries(ref.comp).map(([k, v]) => {
      const brandKey = BRAND_COMP_KEYS.has(k)
      return [k, recolor(v, brandKey ? hue : hueS, brandKey ? scale : scaleS)]
    })
  ) as unknown as CompTokens

  // 品牌强调位显式钉值,不信任 rehue 的取整:
  // 浅色皮肤 primaryText/link 三态 = brand 三态;深色皮肤 colorLink = primaryText
  if (input.appearance === 'light') {
    palette.primaryText = brand.primary
    palette.primaryTextHover = brand.hover
    palette.primaryTextActive = brand.active
    palette.colorLink = brand.primary
    palette.colorLinkHover = brand.hover
    palette.colorLinkActive = brand.active
  } else {
    palette.colorLink = palette.primaryText
    palette.colorLinkHover = palette.primaryTextHover
    palette.colorLinkActive = palette.primaryTextActive
  }

  const cssVars: Record<string, string> = {
    '--app-primary': brand.primary,
    '--app-primary-hover': brand.hover,
    '--app-primary-active': brand.active,
    '--app-primary-bg': palette.colorPrimaryBg,
    '--app-primary-bg-hover': palette.colorPrimaryBgHover,
    '--app-primary-border': palette.colorPrimaryBorder,
    '--app-primary-border-hover': palette.colorPrimaryBorderHover,
  }

  return { brand, palette, comp, cssVars }
}

/** 生成完整 SkinDefinition(编辑器"新建皮肤"入口) */
export function suggestSkin(
  meta: { id: string; name: string },
  input: SuggestInput
): SkinDefinition {
  const { brand, palette, comp, cssVars } = suggestSkinParts(input)
  return { id: meta.id, name: meta.name, appearance: input.appearance, brand, palette, comp, cssVars }
}
