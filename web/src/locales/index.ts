/**
 * locales 统一导出
 *
 * ⚠️ **en-US 不在这里 export**(§2.6 #7,M173):barrel 被 ~50 个组件
 *   `from '@/locales'` 引用,这里静态 re-export en-US 会把它拽回主 chunk,
 *   registry 的动态加载整个白做。需要 enUS 的(测试 / 对账脚本)直接
 *   `from '@/locales/en-US'` 引模块文件。
 */
export { default as zhCN } from './zh-CN'
export { useI18n, tKey, tError } from './useI18n'
export { ensureLocale, getDict, isLocaleLoaded } from './registry'
export type { Locale } from './zh-CN'
