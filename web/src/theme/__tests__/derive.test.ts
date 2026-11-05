/**
 * derive 建议值引擎守卫 —— 编辑器生成的皮肤必须与内置皮肤同构且同调:
 *   1. 键集与 white 基准完全一致(生成物可以直接过 skins.complete.test.ts);
 *   2. 品牌浅底/文字与主色同色相(不许出现孤立色);
 *   3. 明度关系保持(参考皮肤的对比度阶梯不被破坏)。
 */
import { describe, expect, it } from 'vitest'
import { suggestSkin, suggestSkinParts, hexToHsl, hslToHex } from '../derive'
import white from '../skins/white'

const paletteKeys = Object.keys(white.palette).sort()
const compKeys = Object.keys(white.comp).sort()

describe('hexToHsl / hslToHex 往返', () => {
  it.each(['#C7000B', '#8C5A2B', '#2F6BFF', '#fff', '#000'])('%s 往返误差 ≤ 1', (hex) => {
    const { h, s, l } = hexToHsl(hex)
    const back = hexToHsl(hslToHex({ h, s, l }))
    expect(Math.abs(back.h - h) % 360).toBeLessThanOrEqual(1)
    expect(Math.abs(back.s - s)).toBeLessThanOrEqual(1)
    expect(Math.abs(back.l - l)).toBeLessThanOrEqual(1)
  })

  it('灰色返回 s=0 且 h=0(不产生 NaN)', () => {
    expect(hexToHsl('#888888')).toEqual({ h: 0, s: 0, l: expect.closeTo(53.3, 0) })
  })
})

describe('suggestSkinParts:浅色皮肤', () => {
  const parts = suggestSkinParts({ appearance: 'light', primary: '#7C3AED' })

  it('键集与 white 基准完全一致', () => {
    expect(Object.keys(parts.palette).sort()).toEqual(paletteKeys)
    expect(Object.keys(parts.comp).sort()).toEqual(compKeys)
    expect(Object.keys(parts.cssVars)).toContain('--app-primary')
    expect(Object.keys(parts.cssVars)).toHaveLength(7)
  })

  it('brand 三态:L hover > primary > active', () => {
    const lP = hexToHsl(parts.brand.primary).l
    const lH = hexToHsl(parts.brand.hover).l
    const lA = hexToHsl(parts.brand.active).l
    expect(lH).toBeGreaterThan(lP)
    expect(lA).toBeLessThan(lP)
  })

  it('品牌浅底色相 ≈ 主色(±15°)—— 同调性', () => {
    const seedHue = hexToHsl(parts.brand.primary).h
    const { h } = hexToHsl(parts.palette.colorPrimaryBg)
    const dist = Math.abs((((h - seedHue) % 360) + 540) % 360 - 180)
    expect(dist).toBeLessThanOrEqual(15)
  })

  it('primaryText / colorLink = 主色三态(浅色契约,与内置皮肤一致)', () => {
    expect(parts.palette.primaryText).toBe(parts.brand.primary)
    expect(parts.palette.colorLink).toBe(parts.brand.primary)
    expect(parts.palette.colorLinkHover).toBe(parts.brand.hover)
  })

  it('明度阶梯保持:文字深于画布,画布深于容器(对比度关系不塌)', () => {
    const text = hexToHsl(parts.palette.colorText).l
    const layout = hexToHsl(parts.palette.colorBgLayout).l
    const container = hexToHsl(parts.palette.colorBgContainer).l
    expect(text).toBeLessThan(25)
    expect(layout).toBeGreaterThan(80)
    expect(container).toBeGreaterThan(layout)
  })

  it('cssVars 品牌系与 brand/palette 对齐', () => {
    expect(parts.cssVars['--app-primary']).toBe(parts.brand.primary)
    expect(parts.cssVars['--app-primary-bg']).toBe(parts.palette.colorPrimaryBg)
    expect(parts.cssVars['--app-primary-border-hover']).toBe(parts.palette.colorPrimaryBorderHover)
  })
})

describe('suggestSkinParts:双输入(界面基色 ≠ 品牌主色)', () => {
  // 绿界面 + 橙按钮 —— white/black"中性面与品牌色无关"的生成版
  const parts = suggestSkinParts({ appearance: 'light', primary: '#f60', surface: '#6b9e78' })

  const hueDist = (a: string, b: string): number => {
    const ha = hexToHsl(a).h
    const hb = hexToHsl(b).h
    return Math.abs((((ha - hb) % 360) + 540) % 360 - 180)
  }

  it('中性面(画布/文字/边框/侧栏)跟随界面基色(绿色)', () => {
    expect(hueDist(parts.palette.colorBgLayout, '#6b9e78')).toBeLessThanOrEqual(15)
    expect(hueDist(parts.comp.siderBg, '#6b9e78')).toBeLessThanOrEqual(15)
    expect(hueDist(parts.palette.colorBorder, '#6b9e78')).toBeLessThanOrEqual(15)
  })

  it('强调键(colorLink/选中色)仍跟品牌主色(橙)', () => {
    expect(hueDist(parts.palette.colorLink, '#f60')).toBeLessThanOrEqual(15)
    expect(hueDist(parts.comp.menuSelectedColor, '#f60')).toBeLessThanOrEqual(15)
    expect(hueDist(parts.palette.colorPrimaryBg, '#f60')).toBeLessThanOrEqual(15)
  })

  it('界面基色选纯灰 → 中性面无彩(white/black 式),强调键不受影响', () => {
    const gray = suggestSkinParts({ appearance: 'light', primary: '#f60', surface: '#888888' })
    expect(hexToHsl(gray.palette.colorBgLayout).s).toBeLessThanOrEqual(8)
    expect(hexToHsl(gray.comp.siderBg).s).toBeLessThanOrEqual(8)
    expect(gray.palette.colorLink).toBe(gray.brand.primary)
  })

  it('surface 缺省跟随主色(单输入旧行为不破坏)', () => {
    const legacy = suggestSkinParts({ appearance: 'light', primary: '#7C3AED' })
    const withSurface = suggestSkinParts({ appearance: 'light', primary: '#7C3AED', surface: '#7C3AED' })
    expect(legacy.palette.colorBgLayout).toBe(withSurface.palette.colorBgLayout)
    expect(legacy.comp.siderBg).toBe(withSurface.comp.siderBg)
  })
})

describe('suggestSkinParts:深色皮肤', () => {
  const parts = suggestSkinParts({ appearance: 'dark', primary: '#2F6BFF' })

  it('colorLink = primaryText(深色契约,与内置皮肤一致)', () => {
    expect(parts.palette.colorLink).toBe(parts.palette.primaryText)
  })

  it('文字为亮色(明度 ≥ 85%)', () => {
    expect(hexToHsl(parts.palette.colorText).l).toBeGreaterThanOrEqual(85)
  })

  it('品牌浅底为 rgba 半透明档且色相随主色(red 通道最大,蓝主色 → 蓝晕)', () => {
    // #2F6BFF 的色相在蓝区,重色相后 bg 的蓝通道应显著高于红通道
    const m = /rgba\((\d+), (\d+), (\d+),/.exec(parts.palette.colorPrimaryBg)
    expect(m).not.toBeNull()
    const [, r, , b] = m!
    expect(Number(b)).toBeGreaterThan(Number(r))
  })
})

describe('suggestSkin:完整定义', () => {
  it('meta 字段落位,生成物结构 = SkinDefinition', () => {
    const skin = suggestSkin({ id: 'my-skin', name: '我的皮肤' }, { appearance: 'light', primary: '#C7000B' })
    expect(skin.id).toBe('my-skin')
    expect(skin.name).toBe('我的皮肤')
    expect(skin.appearance).toBe('light')
    expect(Object.keys(skin)).toEqual(['id', 'name', 'appearance', 'brand', 'palette', 'comp', 'cssVars'])
    // 直接能过键集守卫(与 white 基准一致)
    expect(Object.keys(skin.palette).sort()).toEqual(paletteKeys)
    expect(Object.keys(skin.comp).sort()).toEqual(compKeys)
  })

  it('低饱和主色不炸(灰主色 → scale 兜底 1,全灰皮肤)', () => {
    const parts = suggestSkinParts({ appearance: 'light', primary: '#666666' })
    expect(parts.palette.colorPrimaryBg).toMatch(/^#[0-9a-f]{6}$/)
    expect(hexToHsl(parts.palette.colorText).s).toBe(0)
  })
})
