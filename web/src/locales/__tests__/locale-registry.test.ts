/**
 * 语言包注册表(§2.6 #7,M173)
 *
 * registry 是**有状态的模块级单例**(loaded 缓存 / version / pending),
 * 所以每个用例都用 `vi.resetModules()` + 动态 import 拿一份**全新的实例**,
 * 用例之间互不污染。
 *
 * ⚠️ mock 用 `vi.doMock`(不用顶层 `vi.mock`):顶层工厂的结果会被缓存,
 *   `vi.resetModules()` 不清 mock 工厂缓存 —— "失败可重试"那条用例里,
 *   前面用例已经用 fail=false 求值过工厂,后面把 fail 拨成 true 也**不会再生效**
 *   (实测:ensureLocale 静默 resolve,假绿)。doMock 在每个用例内现注册现用。
 *
 * useI18n 的端到端(包到达 → 组件重渲染成英文)在 locale-lazy.test.tsx,
 * 那边要真模块,不能 mock。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockState = vi.hoisted(() => ({ fail: false }))

function mockEnUS() {
  vi.doMock('@/locales/en-US', () => {
    if (mockState.fail) {
      throw new Error('network down (mock)')
    }
    return {
      default: {
        common: { save: 'Save (mock)' },
        layout: { appTitle: 'Mock Platform' },
      },
    }
  })
}

/** 每个用例拿一份全新的 registry(模块级状态 + mock 工厂都归零) */
async function freshRegistry() {
  vi.resetModules()
  mockEnUS()
  return await import('@/locales/registry')
}

describe('locale registry(§2.6 #7 懒加载语言包)', () => {
  beforeEach(() => {
    mockState.fail = false
  })

  it('默认态:en-US 未加载,getDict 返 undefined(isLocaleLoaded 同步为假)', async () => {
    const reg = await freshRegistry()
    expect(reg.getDict('en-US')).toBeUndefined()
    expect(reg.isLocaleLoaded('en-US')).toBe(false)
    // zh-CN 常驻(同步查找的兜底)
    expect(reg.isLocaleLoaded('zh-CN')).toBe(true)
    expect(reg.getDict('zh-CN')).toBeDefined()
  })

  it('ensureLocale(en-US) 加载成功 → getDict 返包 + 版本号 +1', async () => {
    const reg = await freshRegistry()
    const v0 = reg.getLocaleVersion()
    await reg.ensureLocale('en-US')
    expect(reg.isLocaleLoaded('en-US')).toBe(true)
    expect(reg.getDict('en-US')!.common.save).toBe('Save (mock)')
    expect(reg.getLocaleVersion()).toBe(v0 + 1)
  })

  it('幂等:重复 ensureLocale 不重复加载(版本号不再涨)', async () => {
    const reg = await freshRegistry()
    await reg.ensureLocale('en-US')
    const v1 = reg.getLocaleVersion()
    await reg.ensureLocale('en-US')
    await reg.ensureLocale('en-US')
    expect(reg.getLocaleVersion()).toBe(v1)
  })

  it('zh-CN 与未登记的语言:直接 resolve,不报错、版本号不动', async () => {
    const reg = await freshRegistry()
    const v0 = reg.getLocaleVersion()
    await expect(reg.ensureLocale('zh-CN')).resolves.toBeUndefined()
    await expect(reg.ensureLocale('fr-FR')).resolves.toBeUndefined()
    expect(reg.getLocaleVersion()).toBe(v0)
  })

  it('失败可重试:第一次 import 挂 → reject、版本号不动;再调 → 真的重试并成功', async () => {
    const reg = await freshRegistry()
    const v0 = reg.getLocaleVersion()

    mockState.fail = true
    // 同一时刻并发两个调用:共享同一个 pending promise,都 reject
    // (vitest 会把工厂抛的错包成自己的 "error when mocking a module" 文案,
    //   所以这里只断言"确实 reject 了 Error",不断言错误原文)
    await expect(reg.ensureLocale('en-US')).rejects.toThrow(Error)
    await expect(reg.ensureLocale('en-US')).rejects.toThrow(Error)
    expect(reg.getLocaleVersion()).toBe(v0)
    expect(reg.isLocaleLoaded('en-US')).toBe(false)

    // pending 已清 → 下一次调用重新发起 import(而不是把同一个 rejected
    // promise 缓存到死 —— 那等于网络抖一次,英文就永久没了)
    mockState.fail = false
    await reg.ensureLocale('en-US')
    expect(reg.isLocaleLoaded('en-US')).toBe(true)
    expect(reg.getLocaleVersion()).toBe(v0 + 1)
  })

  it('版本信号:加载完成后订阅者被通知,取消订阅后不再收', async () => {
    const reg = await freshRegistry()
    const changes: number[] = []
    const unsub = reg.subscribeLocale(() => changes.push(reg.getLocaleVersion()))
    await reg.ensureLocale('en-US')
    unsub()
    expect(changes).toEqual([1])
    // 取消订阅后不再收通知(幂等调用也不该有)
    await reg.ensureLocale('en-US')
    expect(changes).toEqual([1])
  })
})
