/**
 * 语言包注册表(§2.6 #7,M173)
 *
 * **zh-CN 静态打包,en-US 动态加载**:
 *   - zh-CN 是默认语言 + 全部同步查找的兜底(tKey / tError 被 axios 拦截器
 *     等**非 React 模块**同步调用,等不了动态 import)⇒ 必须常驻;
 *   - en-US(~2500 行)此前在 `useI18n.ts` / `index.ts` 里被**静态 import**,
 *     vite 把它打进了主 chunk —— 切成英文的用户是少数,多数中文用户
 *     首屏白白多下载这一包;
 *   - 改成 `import('./en-US')` 后,vite 自动把它拆成独立异步 chunk,
 *     只有真的切到英文(或持久化语言是 en-US)才会拉。
 *
 * **同步查找怎么降级**:没加载完成时 `getDict(lang)` 返 undefined,
 *   调用方回落 zh-CN —— 英文用户在包到达前**短暂看到中文**,到了自动切换
 *   (订阅 `useLocaleVersion` 重渲染,不需要刷新页面)。
 *   宁可短暂闪中文,不阻塞首次渲染。
 *
 * ⚠️ 本模块**只 import zh-CN 和类型**,绝不静态 import en-US(否则前功尽弃)。
 */
import { useSyncExternalStore } from 'react'
import zhCN, { type Locale } from './zh-CN'

/** 已加载的语言包(zh-CN 常驻;其他语言加载后进缓存) */
const loaded: Record<string, Locale> = { 'zh-CN': zhCN }

/** 各非默认语言包的异步 loader —— 新增语言在这里登记一行 */
const loaders: Record<string, () => Promise<Locale>> = {
  'en-US': () => import('./en-US').then((m) => m.default),
}

/** 并发去重:同一种语言只发起一次 import */
const pending = new Map<string, Promise<void>>()

// —— 版本信号:语言包到达后通知订阅者(组件据此重渲染)——
let version = 0
const listeners = new Set<() => void>()

/**
 * 同步取已加载的语言包;没加载(或未知语言)返 undefined,
 * 调用方回落 zh-CN。给 tKey / tError 这类**必须同步**的场景用。
 */
export function getDict(lang: string): Locale | undefined {
  return loaded[lang]
}

/** 某语言包是否已加载(测试 / 调试用) */
export function isLocaleLoaded(lang: string): boolean {
  return lang in loaded
}

/**
 * 确保某语言包已加载并缓存:
 *   - 已加载 → 立刻 resolve(幂等);
 *   - 没登记 loader 的语言(含 zh-CN)→ resolve,保持兜底,不报错;
 *   - 加载成功 → 进缓存 + 版本号 +1 + 通知订阅者;
 *   - 加载失败 → 清掉 pending,**下次调用会重试**(网络抖动不该永久打死英文);
 *     这次调用拿到的 promise 仍然 reject,调用方自己兜底。
 */
export function ensureLocale(lang: string): Promise<void> {
  if (lang in loaded) return Promise.resolve()
  const loader = loaders[lang]
  if (!loader) return Promise.resolve()
  let p = pending.get(lang)
  if (!p) {
    p = loader()
      .then((dict) => {
        loaded[lang] = dict
        version += 1
        listeners.forEach((l) => l())
      })
      .catch((err) => {
        pending.delete(lang)
        throw err
      })
    pending.set(lang, p)
  }
  return p
}

// —— useSyncExternalStore 三件套(useI18n 用它感知"包到了")——

export function subscribeLocale(onChange: () => void): () => void {
  listeners.add(onChange)
  return () => {
    listeners.delete(onChange)
  }
}

export function getLocaleVersion(): number {
  return version
}

/** 订阅语言包加载进度:包到达时组件重渲染(useI18n 内部调用) */
export function useLocaleVersion(): number {
  return useSyncExternalStore(subscribeLocale, getLocaleVersion)
}
