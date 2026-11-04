/**
 * 极简 i18n hook(非完整实现,够用即可)
 *
 * t() 行为:
 *   1. 按点分路径查 zh-CN / en-US 字典
 *   2. 命中后用 vars 替换 {key} 占位符
 *   3. 未命中:用 fallback(若有),否则原样返回 key(永不空白)
 *
 * 用法:
 *   const { t } = useI18n()
 *   t('common.save')
 *   t('forgotPassword.smsCodeLen', '{n} 位短信验证码', { n: 6 })
 *   t('layout.copyright', undefined, { year: 2026 })
 *
 * 非 React 模块(如 axios 拦截器)用 tKey():
 *   import { tKey } from '@/locales'
 *   tKey('error.networkError')
 */
import { useMemo } from 'react'
import { useUIStore } from '@/stores/ui'
import { BIZ_CODE_KEYS } from '@/types/api'
import zhCN, { Locale } from './zh-CN'
import { getDict, useLocaleVersion } from './registry'

// ⚠️ en-US **不在这里 import**(§2.6 #7,M173):它经 registry 的
//   import('./en-US') 动态加载,vite 据此拆出独立异步 chunk。
//   这里多一行静态 import,懒加载就整个白做。
//   查找一律走 getDict(lang) ?? zhCN —— 包没到时回落中文,到了自动切。

function getByPath(obj: any, path: string | null | undefined): any {
  if (path == null || typeof path !== 'string' || path === '') return undefined
  return path.split('.').reduce((acc, k) => (acc == null ? acc : acc[k]), obj)
}

/** 把 {a}/{b} 这种占位符替换成 vars.a / vars.b,缺失占位符保持原样 */
function interpolate(template: string, vars?: Record<string, string | number>): string {
  if (!vars) return template
  return template.replace(/\{(\w+)\}/g, (m, k) =>
    vars[k] != null ? String(vars[k]) : m,
  )
}

/** 读当前语言(非响应式,适合 module 顶层拦截器等场景) */
function getCurrentLang(): string {
  try {
    return useUIStore.getState().lang
  } catch {
    return 'zh-CN'
  }
}

/**
 * 非 hook 版 lookup —— 给 axios 拦截器 / message handler 等
 * 不可用 useI18n() 的场景用
 */
export function tKey(
  key: string,
  fallback?: string,
  vars?: Record<string, string | number>,
): string {
  const lang = getCurrentLang()
  const dict = getDict(lang) || zhCN
  const raw = getByPath(dict, key)
  const template = raw != null ? String(raw) : (fallback != null ? fallback : key)
  return interpolate(template, vars)
}

/**
 * 后端业务码 → 用户文案(M6:错误码 i18n 集中入口)
 *   - code:  后端 ApiResponse.code
 *   - fallback: 翻译缺失时兜底(典型用法:后端 message)
 *   - i18nKeyMap: 可选,默认走 types/api.ts::BIZ_CODE_KEYS
 *                测试 / 业务页自定义覆盖可传
 *
 * 优先级:
 *   1. i18nKeyMap[code] 找到 → 查 locales → 找到 → 翻译
 *   2. 翻译缺失 → fallback(后端 message 或自定义)
 *   3. 全缺 → 返 code 字符串(永不空白)
 */
export function tError(
  code: number,
  fallback?: string,
  i18nKeyMap?: Record<number, string>,
): string {
  const map = i18nKeyMap || defaultBizCodeKeys()
  const key = map[code]
  if (key) {
    const lang = getCurrentLang()
    const dict = getDict(lang) || zhCN
    const translated = getByPath(dict, key)
    if (translated != null) {
      return String(translated)
    }
  }
  if (fallback != null && fallback !== '') {
    return fallback
  }
  return `code ${code}`
}

// defaultBizCodeKeys —— 业务码 → i18n key 的映射表
//
// ⚠️ 2026-10-25:**这里原来写的是运行时 require('@/types/api')**,理由是
//   "lazy load 避免 useI18n → api 循环 import"。那个理由是错的:
//     · `@/types/api` 是**纯类型 + 常量**模块(全文件零 import),
//       静态引入它**不可能**形成循环依赖;
//     · `@/api`(axios 那套)才可能形成环 —— 两者不是一回事。
//   而运行时 require 的代价是实打实的:
//     · 打包产物里留着 `require("@/types/api")`(实测 vite build 产物
//       dist/assets/js/index-*.js 里就是这一行)—— 浏览器里没有 require,
//       一调用就是 ReferenceError;
//     · 命中的是 handleBizError → tError → **每一个业务错误码**
//       (code != 0)都会走到这里 → 整个错误处理在浏览器里直接炸掉,
//       业务文案不显示、toast 不弹、调用方拿到的是 ReferenceError
//       而不是业务错误。
//   所以改成静态 import —— 顺带把 vitest 里也修好了(那边同样解析不了
//   带别名路径的 require)。
function defaultBizCodeKeys(): Record<number, string> {
  return BIZ_CODE_KEYS
}

export function useI18n() {
  const lang = useUIStore((s) => s.lang)
  // 订阅"语言包到了"信号:切英文后 en-US 异步到达,这一步让组件**自动**
  // 重渲染一次(否则英文用户会永远停在中文兜底上,且无任何报错)。
  // 返回值不进 useMemo 依赖 —— 包到达后 getDict 返回的 dict **引用变了**,
  // [dict] 依赖本身就足以重算 t;版本号只负责触发这次重渲染。
  useLocaleVersion()
  const dict = getDict(lang) || zhCN
  // t 必须稳定引用, 否则 useEffect([t]) 会因 t 每 render 换新导致循环 setState
  //   (典型受害者: login 页初始读锁定状态的 useEffect, 把 setLockout 跑成死循环)
  // useMemo 的第一个参数必须是 () => T, 不能直接传 T 函数本身 (会被以 () 调用, 返回 undefined)
  const t = useMemo(
    () => (key: string, fallback?: string, vars?: Record<string, string | number>) => {
      const raw = getByPath(dict, key)
      const template = raw != null ? String(raw) : (fallback != null ? fallback : key)
      return interpolate(template, vars)
    },
    [dict],
  )
  return { lang, t, dict }
}

export { zhCN }
export type { Locale }

