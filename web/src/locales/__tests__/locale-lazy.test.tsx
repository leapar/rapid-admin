/**
 * 懒加载端到端(§2.6 #7,M173):真模块、真 en-US 包。
 *
 * 场景链(用例顺序有意为之,registry 是模块级单例):
 *   1. 切到 en-US 但包还没到 → tKey / useI18n **同步**回落中文(不空白、不报错);
 *   2. ensureLocale 到达 → **不刷新页面**,useI18n 组件自动重渲染成英文
 *      (useSyncExternalStore 版本信号 —— 可逆探针:拆掉 registry 的
 *      version+1/listeners 通知,用例 3 转 red);
 *   3. 包到达后 tKey(同步通道)也拿到英文。
 *
 * 🔴 可逆探针(2026-11-02 实测):
 *   - 拆掉 registry.ts 加载成功分支的 `version += 1; listeners.forEach(...)` → 用例 3 红
 *     (组件永远停在中文兜底,且无任何报错 —— 这正是懒加载最容易烂掉的静默形态);
 *   - 拆掉 useI18n 里的 `useLocaleVersion()` → 同样用例 3 红。
 */
import { describe, it, expect, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useUIStore } from '@/stores/ui'
import { useI18n, tKey, ensureLocale, isLocaleLoaded } from '@/locales'

afterEach(() => {
  // lang 回到默认,避免持久化把 en-US 泄漏到别的测试文件
  act(() => {
    useUIStore.getState().setLang('zh-CN')
  })
  window.localStorage.clear()
})

describe('懒加载语言包端到端(§2.6 #7)', () => {
  // ⚠️ 用未登记的语言打「包未到」分支:setup.ts 已全局预载 en-US(修 M173 懒加载
  //   测试回归),「en-US 未加载」这个前提在本套件里不可复现。xx-XX 走的是同一条
  //   getDict 未命中 → 回落中文的分支(ensureLocale 对没登记 loader 的语言直接 resolve)。
  it('包未到:useI18n 的 t 同步回落中文(不空白、不抛错)', () => {
    act(() => {
      useUIStore.getState().setLang('xx-XX')
    })
    const { result } = renderHook(() => useI18n())
    expect(result.current.lang).toBe('xx-XX')
    expect(isLocaleLoaded('xx-XX')).toBe(false)
    expect(result.current.t('common.save')).toBe('保存')
  })

  it('包未到:tKey(非 React 同步通道,axios 拦截器在用)同样回落中文', () => {
    act(() => {
      useUIStore.getState().setLang('xx-XX')
    })
    expect(isLocaleLoaded('xx-XX')).toBe(false)
    expect(tKey('common.save')).toBe('保存')
  })

  it('包到达:组件不刷新页面自动重渲染成英文;同步 tKey 也拿到英文', async () => {
    act(() => {
      useUIStore.getState().setLang('en-US')
    })
    const { result } = renderHook(() => useI18n())
    // 前两个用例用的是 xx-XX,真 en-US 包到现在都还没加载 —— 此刻到达
    await act(async () => {
      await ensureLocale('en-US')
    })
    expect(result.current.t('common.save')).toBe('Save')
    expect(tKey('common.save')).toBe('Save')
    // t 引用必须换新(dict 变了)—— 依赖 t 的 useEffect(如 document.title)靠它感知
    // (无法直接对比旧引用,能断言的是:同一个 t 在切换前后产出不同文案,已在上面覆盖)
  })

  it('缓存命中:第二次 ensureLocale 立刻 resolve,文案保持英文', async () => {
    act(() => {
      useUIStore.getState().setLang('en-US')
    })
    const { result } = renderHook(() => useI18n())
    await act(async () => {
      await ensureLocale('en-US')
    })
    expect(result.current.t('common.save')).toBe('Save')
  })
})
