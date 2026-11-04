/**
 * Vitest 全局 setup —— 每个测试文件加载前跑一次
 *
 * 这里的每一段都不是"以防万一",都是**实际缺了就会炸**的东西:
 *   - jest-dom 匹配器:toBeInTheDocument / toHaveTextContent
 *   - cleanup:RTL 16 在 globals:false 时**不会自动清理**(它靠全局 afterEach 注册),
 *     不手动清的话 DOM 会跨用例累积,断言会莫名其妙地命中上一个用例的节点
 *   - ResizeObserver:jsdom 没有,antd Table / rc-resize-observer 直接抛错
 *   - matchMedia:jsdom 的实现很弱,antd 的响应式栅格会用到
 */
import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup, configure } from '@testing-library/react'

afterEach(() => {
  cleanup()
})

// ---------------------------------------------------------------------------
// waitFor 的超时预算(asyncUtilTimeout)
// ---------------------------------------------------------------------------
//
// ⚠️ **这是 M121 补上的** —— @testing-library/dom 的 `asyncUtilTimeout` 默认
//   **1000ms**,本仓 32 个文件、347 处 `waitFor` 全都吃这个默认值。
//
//   它跟 vitest 的 `testTimeout` 是**两回事**,两个都得在:
//     · testTimeout       = 整条用例的上限( vitest 管)
//     · asyncUtilTimeout = waitFor **每一次**等待的上限(@testing-library 管)
//
//   M106 把 `testTimeout` 从 5s 提到 60s —— **只动了外层那一个**。
//   于是满载时(99 个文件并行)真正先炸的是**内层**:一次 antd 渲染在满载下
//   要好几秒(实测单条用例 5~17s),`waitFor` 等满 1s 就放弃了,报的是
//
//     Unable to find an element with the text: ...
//
//   —— 看着像"断言写错了",其实是**机器慢了**。单独跑同一个文件是绿的。
//   **假红比假绿更坑**:它让人以为"刚改的东西把别的用例搞坏了",
//   于是去查一个根本不存在的问题。
//
//   10s 的依据:满载下单条 antd 交互实测 5~17s(见 vitest.config.ts 里
//   testTimeout 那段注释),10s 覆盖得住绝大多数等待;同时**必须小于**外层的
//   120s —— 内层超了还能被外层兜住,反过来(内层比外层还长)就等于没有内层。
//
// ⚠️ RTL 在 **import 时**也会调一次 `configure`(只设 unstable_advanceTimersWrapper
//   和 asyncWrapper,**不动** asyncUtilTimeout),所以这里的 configure 不会被它覆盖;
//   ES import 提升保证 RTL 先加载完,再执行这一行。
configure({ asyncUtilTimeout: 10_000 })

// --- ResizeObserver ---
// jsdom 不实现 ResizeObserver。antd 5 的 Table / Grid 走 rc-resize-observer,
// 没有它渲染就抛 "ResizeObserver is not defined"。
// 这里只做"存在即够用"的桩:不回调。antd 的 auto-height 逻辑本来就靠
// 500ms 兜底定时器,测试里不需要真实测量。
if (typeof globalThis.ResizeObserver === 'undefined') {
  class ResizeObserverStub {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  globalThis.ResizeObserver =
    ResizeObserverStub as unknown as typeof globalThis.ResizeObserver
}

// --- matchMedia ---
// antd 的响应式观察(Row/Col 的断点)会调 matchMedia。
// jsdom 自带的实现存在但行为弱(永远 matches:false),这里统一成可控的桩。
if (typeof window !== 'undefined' && !window.matchMedia) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string): MediaQueryList =>
      ({
        matches: false,
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => false,
      }) as unknown as MediaQueryList,
  })
}

// --- getComputedStyle(伪元素) ---
// antd Table 会调 rc-util 的 getScrollBarSize,里面是
//   getComputedStyle(ele, '::-webkit-scrollbar')
// jsdom 不实现伪元素样式,直接抛 "Not implemented"(每次渲染都刷一遍 stderr)。
//
// 处理方式:把伪元素参数丢掉,退化成"取元素自身样式"。
// ⚠️ **这不是掩盖问题**:jsdom 里本来就没有伪元素样式,丢掉参数后拿到的是
//   元素自身的计算样式 —— 对 scrollbar 宽度测量来说,结果和浏览器一样是 0。
//   真正的浏览器行为没有被改动,只是不再刷噪音。
if (typeof window !== 'undefined') {
  const originalGetComputedStyle = window.getComputedStyle.bind(window)
  window.getComputedStyle = ((el: Element, _pseudo?: string | null) =>
    originalGetComputedStyle(el)) as typeof window.getComputedStyle
}

// --- React 18.3 的 act 弃用告警 ---
// React 18.3 没导出 `React.act`(那是 React 19 才有的),于是 @testing-library/react
// 退回用 `react-dom/test-utils` 的 act —— 而后者在 18.3 里已被标记弃用,
// 每次跑都打一条告警。
//
// ⚠️ **只精确吞掉这一句**,其余 console.error 一律原样抛出。
//   理由:满屏的已知第三方告警会让人习惯性无视 stderr,真出问题时反而看不见。
//   升级 React 19 / RTL 19 之后删掉这段。
const DEPRECATED_ACT_WARNING =
  'Warning: `ReactDOMTestUtils.act` is deprecated in favor of `React.act`'
const originalConsoleError = console.error.bind(console)
console.error = (...args: unknown[]) => {
  const first = args[0]
  if (typeof first === 'string' && first.startsWith(DEPRECATED_ACT_WARNING)) return
  originalConsoleError(...args)
}

// --- en-US 语言包预载(M173 懒加载回归) ---
// M173 起 en-US 是异步 chunk,生产里由 LangRoot / setLang action 触发 ensureLocale。
// 测试里大量用例直接 useUIStore.setState({ lang: 'en-US' }),不会触发加载 ——
// tKey/useI18n 查不到字典就永远回落中文,英文断言全红。这里全局预载一次,
// 之后 getDict('en-US') 同步命中缓存。
import { ensureLocale } from '@/locales/registry'
await ensureLocale('en-US')
