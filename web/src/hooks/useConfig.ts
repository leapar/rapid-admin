/**
 * useConfig —— 平台配置 hook
 *
 * 用法:
 *   const { value, loading } = useConfig<string>('sys.title')
 *   const value2 = useConfigValue('sys.max-upload', 10)  // 简化版,带默认值
 *
 * ⚠️ **读接口只有 super_admin 能用** —— `/platform/configs` 挂 `rbac("config","list")`,
 *   `config:*` 只绑给 super_admin。所以这两个 hook **当前零调用方**:
 *   普通业务页调它只会拿到 403(见 `stores/config.ts` 头部说明)。
 *   维护口径:**预留 API** —— 修好、测住、标明边界,不删。
 */
import { useEffect, useState } from 'react'
import { useConfigStore, parseConfigValue } from '@/stores/config'

export function useConfig<T = string>(code: string) {
  const item = useConfigStore((s) => s.configMap[code])
  const fetchAll = useConfigStore((s) => s.fetchAll)
  const lastFetchAt = useConfigStore((s) => s.lastFetchAt)

  // ⚠️ 拉失败必须能落到 UI 上 —— 否则 `loading: !item` 永远为 true,
  //   调用方永远转圈,分不清"没这配置"和"拉挂了"(同 M106 CaptchaField 那一类)。
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    if (lastFetchAt) return
    let cancelled = false
    setFailed(false)
    fetchAll().catch(() => {
      if (!cancelled) setFailed(true)
    })
    return () => {
      cancelled = true
    }
  }, [lastFetchAt, fetchAll])

  return {
    /** 按 `value_type` 解析后的值(解析不了就是原字符串) */
    value: parseConfigValue<T | undefined>(item, undefined),
    item,
    loading: !item && !failed,
    /** 拉取失败(拦截器已弹过提示) */
    error: failed,
  }
}

export function useConfigValue<T = string>(code: string, defaultValue: T): T {
  /**
   * ⚠️ **不能订阅 `s.getValue`** —— 那是 store 里的**稳定函数引用**,
   *   zustand 用 `Object.is` 比较 selector 的返回值 ⇒ 引用永远不变
   *   ⇒ **组件永远不会因为配置到达而重渲染**。
   *   以前这个 hook 就是这么写的,于是 `useConfigValue('x', 默认值)`
   *   在配置拉到之前返回默认值,拉到之后**也不会更新**(除非别的原因碰巧触发重渲染)。
   *
   *   现在订阅的是**值本身**(`s.configMap[code]`),配置到达 → 引用变了 → 重渲染 ✓
   */
  const { item } = useConfig<T>(code)
  return parseConfigValue<T>(item, defaultValue)
}
