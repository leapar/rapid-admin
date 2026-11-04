/**
 * useConfig / useConfigValue 的回归网(2026-10-28 M107)
 *
 * ## 这条网守的是什么
 *
 * ### ① `useConfigValue` **永远不重渲染**(P2,但是"功能整个是坏的"那种)
 *
 *   旧实现:
 *     const getValue = useConfigStore((s) => s.getValue)
 *     return getValue<T>(code, defaultValue)
 *
 *   zustand 的 selector 返回值用 `Object.is` 比较 —— 而 `s.getValue` 是
 *   store 里**稳定不变的函数引用** ⇒ 比较永远相等 ⇒ **订阅永远不会触发**。
 *   ⇒ 配置拉到之后,组件不重渲染,`useConfigValue` 一直返回**默认值**。
 *   (除非别的什么原因碰巧触发重渲染,那是运气,不是机制。)
 *
 *   现在订阅的是**值本身**(`s.configMap[code]`),配置到达 → 引用变了 → 重渲染 ✓
 *
 * ### ② 拉失败被吞成「永远在加载」(P2)
 *
 *   旧实现 `fetchAll().catch(() => undefined)` + `loading: !item`
 *   ⇒ 调用方永远转圈,分不清「没这配置」和「拉挂了」。
 *
 * ### ③ 配置值按 `value_type` 解析,而不是「无脑 JSON.parse」(P2)
 *
 *   旧实现(store 的 `getValue`)直接 `JSON.parse(item.value)`:
 *     · `value_type='string'`、值是 `"123"` → 给出**数字 123**(类型撒谎);
 *     · `value_type='string'`、值是 `"true"` → 给出**布尔**。
 *   服务端明明在 `value_type` 里写了类型,前端却绕过它自己猜。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `useConfigValue` 改回订阅 `s.getValue` → ① 那条立刻变红。
 *   · 把 `parseConfigValue` 里的 `switch (item.value_type)` 换成无脑 JSON.parse → ③ 变红。
 *   · 把 `.catch(() => { if (!cancelled) setFailed(true) })` 去掉 → ② 变红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { configApi } from '@/api/config'
import { useConfigStore } from '@/stores/config'
import { useConfig, useConfigValue } from '@/hooks/useConfig'
import type { ConfigItem, ConfigValueType } from '@/types/config'

vi.mock('@/api/config', () => ({
  configApi: { list: vi.fn() },
}))

const listMock = vi.mocked(configApi.list)

/** 造一个配置项 */
function cfg(key: string, value: string, valueType: ConfigValueType): ConfigItem {
  return {
    id: '1',
    config_group: 'aliyun.oss',
    config_key: key,
    config_name: key,
    description: '',
    value_type: valueType,
    value,
    value_preview: value,
    is_sensitive: false,
    regex_validate: '',
    min_length: 0,
    max_length: 0,
    is_required: false,
    is_readonly: false,
    env_var_name: '',
    sort: 1,
    is_active: true,
    remarks: '',
    version: 1,
    last_changed_by: 0,
    last_changed_by_name: '',
    last_changed_at: '',
  }
}

function page(list: ConfigItem[]) {
  return { list, total: list.length, page: 1, size: 100 }
}

describe('useConfigValue · 到达即更新(M107)', () => {
  beforeEach(() => {
    useConfigStore.setState({ configMap: {}, lastFetchAt: 0, loading: false })
    listMock.mockReset()
  })

  it('配置拉到之后,值从默认值变成真实值(旧实现永远停在默认值)', async () => {
    listMock.mockResolvedValue(page([cfg('sys.title', '示例平台', 'string')]))

    const { result } = renderHook(() => useConfigValue('sys.title', '默认值'))

    // 还没拉到 → 默认值
    expect(result.current).toBe('默认值')

    await waitFor(() => expect(result.current).toBe('示例平台'))
  })

  it('配置**不存在**时返回默认值(而不是 undefined)', async () => {
    listMock.mockResolvedValue(page([]))

    const { result } = renderHook(() => useConfigValue('sys.not_exist', '默认值'))

    await waitFor(() => expect(listMock).toHaveBeenCalled())
    expect(result.current).toBe('默认值')
  })
})

describe('useConfig · 值按 value_type 解析(M107)', () => {
  beforeEach(() => {
    useConfigStore.setState({ configMap: {}, lastFetchAt: 0, loading: false })
    listMock.mockReset()
  })

  it("value_type='string' 且值是 '123' → 仍然是**字符串**(不是数字 123)", async () => {
    listMock.mockResolvedValue(page([cfg('sys.code', '123', 'string')]))

    const { result } = renderHook(() => useConfig('sys.code'))

    await waitFor(() => expect(result.current.item).toBeDefined())
    expect(result.current.value).toBe('123')
    expect(typeof result.current.value).toBe('string')
  })

  it("value_type='int' → 解析成数字", async () => {
    listMock.mockResolvedValue(page([cfg('oss.ttl', '3600', 'int')]))

    const { result } = renderHook(() => useConfig('oss.ttl'))

    await waitFor(() => expect(result.current.item).toBeDefined())
    expect(result.current.value).toBe(3600)
  })

  it("value_type='bool' → 解析成布尔", async () => {
    listMock.mockResolvedValue(page([cfg('oss.enabled', 'true', 'bool')]))

    const { result } = renderHook(() => useConfig('oss.enabled'))

    await waitFor(() => expect(result.current.item).toBeDefined())
    expect(result.current.value).toBe(true)
  })

  it("value_type='json' → 解析成对象", async () => {
    listMock.mockResolvedValue(page([cfg('oss.policy', '{"a":1}', 'json')]))

    const { result } = renderHook(() => useConfig('oss.policy'))

    await waitFor(() => expect(result.current.item).toBeDefined())
    expect(result.current.value).toEqual({ a: 1 })
  })
})

describe('useConfig · 失败态(M107)', () => {
  beforeEach(() => {
    useConfigStore.setState({ configMap: {}, lastFetchAt: 0, loading: false })
    listMock.mockReset()
  })

  it('拉失败 → 不再永远 loading,error 变 true', async () => {
    listMock.mockRejectedValue(new Error('boom'))

    const { result } = renderHook(() => useConfig('sys.title'))

    await waitFor(() => expect(result.current.error).toBe(true))
    expect(result.current.loading).toBe(false)
    expect(result.current.value).toBeUndefined()
  })
})
