/**
 * useRememberForm —— 软 keepAlive:把表单值存到 tab.cache.formState
 *
 * 用法(以 antd Form 为例):
 *   const [form] = Form.useForm()
 *   const { save, restore } = useRememberForm<{name: string}>(form)
 *   useEffect(() => { restore() }, [restore])
 *   // 提交或卸载时:
 *   await save()
 *
 * 实现:
 *  - 写入:form.getFieldsValue() → JSON.stringify → tab.cache.formState
 *  - 读取:tab.cache.formState → form.setFieldsValue
 *  - 失败静默(表单字段类型不匹配时回退空)
 *
 * 注意:这是**业务层**的 keepAlive。React 组件本身会被卸载重挂,
 * 所以 useState 局部状态不会保留;只有显式调用本 hook 的表单才保留。
 * 真 keepAlive(组件不卸载)需要 react-activation / next.js 路由级缓存,
 * 本期不做。
 */
import { useCallback } from 'react'
import { useLocation } from 'react-router-dom'
import { useTabsStore } from '@/stores/tabs'

interface RememberFormLike {
  getFieldsValue: () => Record<string, unknown>
  setFieldsValue: (v: Record<string, unknown>) => void
}

export function useRememberForm<T extends Record<string, unknown>>(
  form: RememberFormLike
) {
  const { pathname } = useLocation()
  const patchCache = useTabsStore((s) => s.patchCache)

  const save = useCallback(() => {
    try {
      const v = form.getFieldsValue() as T
      patchCache(pathname, { formState: v as Record<string, unknown> })
    } catch {
      /* 表单没挂好,静默 */
    }
  }, [form, patchCache, pathname])

  const restore = useCallback(() => {
    const tab = useTabsStore.getState().tabs.find((t) => t.key === pathname)
    const cached = tab?.cache?.formState as T | undefined
    if (cached && typeof cached === 'object') {
      try {
        form.setFieldsValue(cached as Record<string, unknown>)
      } catch {
        /* 字段不匹配(版本变了)静默 */
      }
    }
  }, [form, pathname])

  const clear = useCallback(() => {
    patchCache(pathname, { formState: undefined })
  }, [patchCache, pathname])

  return { save, restore, clear }
}
