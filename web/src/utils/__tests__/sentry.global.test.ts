/**
 * 「全局错误捕获交给 SDK」这条决定的**依据**(2026-10-29 M109)
 *
 * 背景:utils/sentry.ts 原先自己装了一份
 *   `window.addEventListener('error')` + `window.addEventListener('unhandledrejection')`,
 *   而 Sentry v11 的 `globalHandlers` 集成(**默认集成之一**)会**接管**
 *   `window.onerror` / `window.onunhandledrejection` —— 而这两个属性本身就是
 *   对应事件的监听器 ⇒ 两份监听器**都会跑** ⇒ 每个未捕获错误上报两次。
 *
 * 本文件**不 mock** @sentry/react,跑的是真 SDK,断言:
 *   init 之后 `globalThis.onerror.__SENTRY_INSTRUMENTED__ === true`
 * —— 这个标记是 @sentry/core 的 `instrument/globalError.js` 自己打的,
 * 用来证明"SDK 确实接管了全局错误处理",也就是"我们把自己的那份删掉是安全的"。
 *
 * ⚠️ 为什么断言 globalThis 而不是 window:
 *   Sentry 改的是 `worldwide.GLOBAL_OBJ.onerror`,而 GLOBAL_OBJ === globalThis。
 *   vitest 的 jsdom 环境里 globalThis 与 window 是**两个对象**(populateGlobal
 *   把 window 的属性**拷贝**到 Node 全局上,不是活引用)⇒ 断言 window.onerror
 *   会永远拿到拷贝时的初值。真实浏览器里 globalThis === window,两者等价。
 */
import { describe, it, expect, vi, afterEach } from 'vitest'

type Instrumented = { __SENTRY_INSTRUMENTED__?: boolean }

afterEach(() => {
  vi.unstubAllEnvs()
  vi.resetModules()
})

describe('Sentry SDK 的全局插桩', () => {
  it('init 之后 window.onerror / window.onunhandledrejection 被 SDK 接管', async () => {
    vi.stubEnv('VITE_SENTRY_DSN', 'https://key@example.invalid/1')
    vi.stubEnv('MODE', 'production')

    const mod = await import('@/utils/sentry')
    mod.initSentry()

    const onerror = (globalThis as unknown as { onerror?: Instrumented }).onerror
    const onunhandled = (globalThis as unknown as { onunhandledrejection?: Instrumented })
      .onunhandledrejection

    expect(onerror?.__SENTRY_INSTRUMENTED__).toBe(true)
    expect(onunhandled?.__SENTRY_INSTRUMENTED__).toBe(true)
  })
})
