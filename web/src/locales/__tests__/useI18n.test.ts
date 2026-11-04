/**
 * tError —— 后端业务码 → 翻译文案(2026-10-25 修 require 回归)
 *
 * ⚠️ 这个用例守的是一次**真事故**:
 *   defaultBizCodeKeys() 原来写的是运行时 `require('@/types/api')`,
 *   打包产物里留着这一行 —— 浏览器里没有 require,一调用就是
 *   ReferenceError。而它挂在 handleBizError → tError 这条链上,
 *   也就是**每一个业务错误码**(code != 0)都会炸。
 *
 *   所以这里断言两件事:
 *   1. tError 不抛;
 *   2. 拿到的是**翻译后的文案**,不是 key、也不是 code 字符串。
 */
import { describe, it, expect } from 'vitest'
import { tError, tKey } from '@/locales'
import { BIZ_CODE_KEYS } from '@/types/api'

describe('tError', () => {
  it('已知业务码 → 返回翻译文案(不抛异常)', () => {
    const code = Number(Object.keys(BIZ_CODE_KEYS)[0])
    const text = tError(code, '后端原文')
    expect(typeof text).toBe('string')
    expect(text).not.toBe('')
    // 不是把 key 原样吐出来(除非字典里恰好没这条)
    expect(text).not.toBe(BIZ_CODE_KEYS[code])
  })

  it('未知业务码 → 回落到后端原文', () => {
    expect(tError(999999, '后端原文')).toBe('后端原文')
  })

  it('未知业务码 + 没有原文 → 返 code 字符串(永不空白)', () => {
    expect(tError(999999)).toBe('code 999999')
  })

  it('显式传入 i18nKeyMap 时用它', () => {
    expect(tError(999999, undefined, { 999999: 'error.networkError' })).toBe(
      tKey('error.networkError'),
    )
  })
})
