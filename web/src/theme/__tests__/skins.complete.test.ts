/**
 * 皮肤字段全集守卫 —— 所有皮肤必须与 white.ts/black.ts 基准**字段完全一致**,
 * 漏掉任何一个色调键(哪怕 antd 会推导)都算遗漏 —— skins.complete.test.ts。
 *
 * 背景:token 显式值优先级高于算法推导值,少写一个键 = 那个表面在切肤后
 * "穿帮"(见 white.ts 头注释里的 colorBgContainer 教训)。
 *
 * 2026-11-05 皮肤扁平化:皮肤 = 整体外观,一套色板(无 dark/light 内部分轴),
 * appearance 必填且合法。
 */
import { describe, expect, it } from 'vitest'
import type { SkinAppearance, SkinDefinition } from '../types'
import white from '../skins/white'
import black from '../skins/black'
import museum from '../skins/museum'
import tech from '../skins/tech'
import gov from '../skins/gov'
import customExample from '../skins/custom.example'

const BASE: Record<string, SkinDefinition> = { white, black, museum, tech, gov }

// 基准键集(取自 white —— 字段全集的唯一定义处)
const brandKeys = Object.keys(white.brand).sort()
const paletteKeys = Object.keys(white.palette).sort()
const compKeys = Object.keys(white.comp).sort()
const cssVarKeys = Object.keys(white.cssVars).sort()

// 每种 token 的值都必须是非空字符串(颜色/rgba/阴影串)
const allValuesNonEmpty = (obj: object): boolean =>
  Object.values(obj).every((v) => typeof v === 'string' && v.trim() !== '')

describe('skins 字段全集(白/黑基准,扁平单套)', () => {
  it('基准自洽:white 与 black 键集一致(仅 id/name/appearance 不同)', () => {
    expect(Object.keys(black.brand).sort()).toEqual(brandKeys)
    expect(Object.keys(black.palette).sort()).toEqual(paletteKeys)
    expect(Object.keys(black.comp).sort()).toEqual(compKeys)
    expect(Object.keys(black.cssVars).sort()).toEqual(cssVarKeys)
    expect(black.appearance).toBe('dark')
    expect(white.appearance).toBe('light')
  })

  for (const [id, skin] of Object.entries(BASE)) {
    it(`${id}:与基准字段完全一致,无遗漏色调`, () => {
      expect(skin.id).toBe(id)
      expect(skin.name.trim()).not.toBe('')
      expect(['light', 'dark']).toContain(skin.appearance)
      expect(Object.keys(skin.brand).sort()).toEqual(brandKeys)
      expect(Object.keys(skin.palette).sort()).toEqual(paletteKeys)
      expect(Object.keys(skin.comp).sort()).toEqual(compKeys)
      // cssVars 允许皮肤追加专属变量(如 tech 的带色阴影),但基准全集一个不能少
      for (const k of cssVarKeys) {
        expect(Object.keys(skin.cssVars)).toContain(k)
      }

      // 值完整性:每个键都有真值
      expect(allValuesNonEmpty(skin.brand)).toBe(true)
      expect(allValuesNonEmpty(skin.palette)).toBe(true)
      expect(allValuesNonEmpty(skin.comp)).toBe(true)
      expect(allValuesNonEmpty(skin.cssVars)).toBe(true)
    })
  }

  it('custom.example 模板同样满足字段全集(用户拷走即合规)', () => {
    expect(Object.keys(customExample.palette).sort()).toEqual(paletteKeys)
    expect(Object.keys(customExample.comp).sort()).toEqual(compKeys)
    expect(['light', 'dark']).toContain(customExample.appearance as SkinAppearance)
    for (const k of cssVarKeys) {
      expect(Object.keys(customExample.cssVars)).toContain(k)
    }
  })
})
