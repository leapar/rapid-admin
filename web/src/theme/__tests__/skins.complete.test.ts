/**
 * 皮肤守卫 —— 每套皮肤都是**显式全量定义**(skins/<id>.ts 逐参数写色值),
 * 本测试网两件事:
 *   1. **字段全集**:palette / comp / cssVars 与基准键集一致 —— 漏一个
 *      色调即红(新增 token 键时所有皮肤要同步补,types.ts 是唯一定义处);
 *   2. **同调性**:品牌浅底/文字与 brand.primary 同色相,不许出现
 *      "棕色皮肤配蓝灰 header"这类孤立色。
 */
import { describe, expect, it } from 'vitest'
import type { SkinDefinition } from '../types'
import white from '../skins/white'
import black from '../skins/black'
import museum from '../skins/museum'
import tech from '../skins/tech'
import gov from '../skins/gov'
import customExample from '../skins/custom.example'

const SKINS: Record<string, SkinDefinition> = {
  white,
  black,
  museum,
  tech,
  gov,
}

// 基准键集(取自 white —— 字段全集的唯一定义处是 types.ts)
const brandKeys = Object.keys(white.brand).sort()
const paletteKeys = Object.keys(white.palette).sort()
const compKeys = Object.keys(white.comp).sort()
const cssVarKeys = Object.keys(white.cssVars).sort()

// 每种 token 的值都必须是非空字符串(颜色/rgba/阴影串)
const allValuesNonEmpty = (obj: object): boolean =>
  Object.values(obj).every((v) => typeof v === 'string' && v.trim() !== '')

/** 简版 hex → HSL(只在此测试用;返回 0-360 / 0-100 / 0-100) */
function hexToHsl(hex: string): { h: number; s: number; l: number } {
  let h = hex.replace('#', '')
  if (h.length === 3) h = h.split('').map((c) => c + c).join('')
  const n = parseInt(h, 16)
  const rn = ((n >> 16) & 255) / 255, gn = ((n >> 8) & 255) / 255, bn = (n & 255) / 255
  const max = Math.max(rn, gn, bn), min = Math.min(rn, gn, bn)
  const l = (max + min) / 2
  if (max === min) return { h: 0, s: 0, l: l * 100 }
  const d = max - min
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  let hue: number
  if (max === rn) hue = ((gn - bn) / d + (gn < bn ? 6 : 0)) / 6
  else if (max === gn) hue = ((bn - rn) / d + 2) / 6
  else hue = ((rn - gn) / d + 4) / 6
  return { h: hue * 360, s: s * 100, l: l * 100 }
}

/** 色相环形距离(0-180) */
function hueDist(a: number, b: number): number {
  return Math.abs((((a - b) % 360) + 540) % 360 - 180)
}

describe('皮肤字段全集(白/黑基准)', () => {
  for (const [id, skin] of Object.entries(SKINS)) {
    it(`${id}:字段全集完整、值非空、三态关系正确`, () => {
      expect(skin.id).toBe(id)
      expect(skin.name.trim()).not.toBe('')
      expect(['light', 'dark']).toContain(skin.appearance)
      expect(Object.keys(skin.brand).sort()).toEqual(brandKeys)
      expect(Object.keys(skin.palette).sort()).toEqual(paletteKeys)
      expect(Object.keys(skin.comp).sort()).toEqual(compKeys)
      // cssVars 至少含品牌系 7 键(允许皮肤追加专属变量)
      for (const k of cssVarKeys) {
        expect(Object.keys(skin.cssVars)).toContain(k)
      }

      expect(allValuesNonEmpty(skin.brand)).toBe(true)
      expect(allValuesNonEmpty(skin.palette)).toBe(true)
      expect(allValuesNonEmpty(skin.comp)).toBe(true)
      expect(allValuesNonEmpty(skin.cssVars)).toBe(true)

      // 品牌三态:hover 亮于主色,active 暗于主色(L 轴)
      const { l: lP } = hexToHsl(skin.brand.primary)
      const { l: lH } = hexToHsl(skin.brand.hover)
      const { l: lA } = hexToHsl(skin.brand.active)
      expect(lH).toBeGreaterThan(lP)
      expect(lA).toBeLessThan(lP)
    })

    if (skin.appearance === 'light') {
      // 浅色:品牌浅底/文字的色相必须跟主色走,不允许孤立灰/异色
      it(`${id}:colorPrimaryBg 色相 ≈ 主色色相(±15°)`, () => {
        const seedHue = hexToHsl(skin.brand.primary).h
        const { h } = hexToHsl(skin.palette.colorPrimaryBg)
        expect(hueDist(h, seedHue)).toBeLessThanOrEqual(15)
      })

      it(`${id}:主文字色为深色(明度 ≤ 20%)且 colorLink = 主色`, () => {
        expect(hexToHsl(skin.palette.colorText).l).toBeLessThanOrEqual(20)
        expect(skin.palette.colorLink).toBe(skin.brand.primary)
      })
    } else {
      // 深色:文字为亮色;品牌浅底 = 主色 rgba(浅色皮肤不适用)
      it(`${id}:主文字色为亮色(明度 ≥ 85%)且 colorLink = primaryText`, () => {
        expect(hexToHsl(skin.palette.colorText).l).toBeGreaterThanOrEqual(85)
        expect(skin.palette.colorLink).toBe(skin.palette.primaryText)
      })
    }
  }

  it('黑色皮肤:深色画布明度 ≤ 12%(接近纯黑但不死黑)', () => {
    const { l } = hexToHsl(black.palette.colorBgLayout)
    expect(l).toBeLessThanOrEqual(12)
    expect(l).toBeGreaterThanOrEqual(8)
  })

  it('白色皮肤:中性面为纯灰(colorText 饱和度 0)——不与文博暖纸底混淆', () => {
    expect(hexToHsl(white.palette.colorText).s).toBe(0)
    expect(hexToHsl(white.palette.colorBgLayout).s).toBe(0)
  })

  it('custom.example 模板同样字段全集完整(用户拷走即合规)', () => {
    expect(Object.keys(customExample.palette).sort()).toEqual(paletteKeys)
    expect(Object.keys(customExample.comp).sort()).toEqual(compKeys)
    for (const k of cssVarKeys) {
      expect(Object.keys(customExample.cssVars)).toContain(k)
    }
  })
})

describe('键数守卫(types 与皮肤文件无漂移)', () => {
  it('palette 25 键 / comp 29 键 / brand 3 键', () => {
    expect(paletteKeys).toHaveLength(25)
    expect(compKeys).toHaveLength(29)
    expect(brandKeys).toEqual(['active', 'hover', 'primary'])
  })
})
