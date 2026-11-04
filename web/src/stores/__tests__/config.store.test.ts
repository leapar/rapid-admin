/**
 * 平台配置 Store 的回归网(2026-10-28 M107)
 *
 * ## 这条网守的是什么
 *
 * ### ① `fetchAll` 以前**静默截断**在 100 条(P2)
 *
 *   后端 `pagination.Normalize()` 把 `size` 截到 `MaxSize = 100`,
 *   而 `fetchAll` 以前写的是 `page_size: 1000` 指望"一次拉全量"。
 *   配置项只有 17 条时看不出问题 —— 一旦超过 100 条,`configMap` 就**少一半**,
 *   而且**没有任何报错**(页面照样显示,只是少了几个 key)。
 *
 *   现在改成**循环翻页直到拿齐**。
 *
 * ### ② `getValue` 按 `value_type` 解析,不是「无脑 JSON.parse」(P2)
 *
 *   旧实现直接 `JSON.parse(item.value)`:
 *     · `value_type='string'`、值是 `"123"` → 给出**数字 123**(类型撒谎);
 *     · `value_type='string'`、值是 `"true"` → 给出**布尔**。
 *   服务端在 `value_type` 里写了类型,前端却绕过它自己猜。
 *
 * ### ③ 10 分钟缓存窗口(M77 的既有语义,顺带锁住)
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `fetchAll` 的翻页循环改回"只请求一页" → ① 那条立刻变红。
 *   · 把 `parseConfigValue` 的 `switch (item.value_type)` 换成无脑 JSON.parse → ② 变红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { configApi } from '@/api/config'
import { useConfigStore } from '@/stores/config'
import type { ConfigItem, ConfigValueType } from '@/types/config'

vi.mock('@/api/config', () => ({
  configApi: { list: vi.fn() },
}))

const listMock = vi.mocked(configApi.list)

function cfg(key: string, value = 'v', valueType: ConfigValueType = 'string'): ConfigItem {
  return {
    id: key,
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

/**
 * 造一页响应。
 *
 * ⚠️ `total` 是**全量条数**(后端 `h.Page(c, res.List, res.Total)` 里的
 *   `res.Total` 来自 count 查询),**不是这一页的条数** —— 弄错的话
 *   前端的翻页判断会以为"已经拿齐",少拉一页还不报错。
 */
function pageOf(list: ConfigItem[], page = 1, total = list.length) {
  return { list, total, page, size: 100 }
}

describe('stores/config · fetchAll 翻页(M107)', () => {
  beforeEach(() => {
    useConfigStore.setState({ configMap: {}, lastFetchAt: 0, loading: false })
    listMock.mockReset()
  })

  it('不足一页时只请求一次', async () => {
    listMock.mockResolvedValue(pageOf([cfg('a'), cfg('b')]))

    await useConfigStore.getState().fetchAll()

    expect(listMock).toHaveBeenCalledTimes(1)
    expect(Object.keys(useConfigStore.getState().configMap)).toHaveLength(2)
  })

  it('超过 100 条时**循环翻页**直到拿齐(后端 size 上限 100)', async () => {
    const first = Array.from({ length: 100 }, (_, i) => cfg(`k${i}`))
    const second = Array.from({ length: 5 }, (_, i) => cfg(`k${100 + i}`))
    listMock.mockImplementation(async (q) =>
      q.page === 1 ? pageOf(first, 1, 105) : pageOf(second, 2, 105),
    )

    await useConfigStore.getState().fetchAll()

    expect(listMock).toHaveBeenCalledTimes(2)
    const map = useConfigStore.getState().configMap
    expect(Object.keys(map)).toHaveLength(105)
    expect(map.k0).toBeDefined()
    expect(map.k104).toBeDefined()
  })

  it('刚好一页(100 条)时不多发一次请求', async () => {
    const exactly = Array.from({ length: 100 }, (_, i) => cfg(`k${i}`))
    listMock.mockResolvedValue(pageOf(exactly))

    await useConfigStore.getState().fetchAll()

    // total = 100,拉满 100 条 ⇒ 不用再翻第二页
    expect(listMock).toHaveBeenCalledTimes(1)
  })

  it('10 分钟内再取走缓存(不发第二次请求)', async () => {
    listMock.mockResolvedValue(pageOf([cfg('a')]))

    await useConfigStore.getState().fetchAll()
    await useConfigStore.getState().fetchAll()

    expect(listMock).toHaveBeenCalledTimes(1)
  })

  it('force = true 时忽略缓存窗口', async () => {
    listMock.mockResolvedValue(pageOf([cfg('a')]))

    await useConfigStore.getState().fetchAll()
    await useConfigStore.getState().fetchAll(true)

    expect(listMock).toHaveBeenCalledTimes(2)
  })
})

describe('stores/config · getValue 按 value_type 解析(M107)', () => {
  beforeEach(() => {
    useConfigStore.setState({ configMap: {}, lastFetchAt: 0, loading: false })
    listMock.mockReset()
  })

  it("value_type='string' 且值是 '123' → 仍然是字符串", () => {
    useConfigStore.setState({ configMap: { k: cfg('k', '123', 'string') } })
    const v = useConfigStore.getState().getValue<string>('k')
    expect(v).toBe('123')
    expect(typeof v).toBe('string')
  })

  it("value_type='int' → 数字", () => {
    useConfigStore.setState({ configMap: { k: cfg('k', '3600', 'int') } })
    expect(useConfigStore.getState().getValue('k')).toBe(3600)
  })

  it("value_type='bool' → 布尔", () => {
    useConfigStore.setState({ configMap: { k: cfg('k', 'true', 'bool') } })
    expect(useConfigStore.getState().getValue('k')).toBe(true)
  })

  it("value_type='json' → 对象", () => {
    useConfigStore.setState({ configMap: { k: cfg('k', '{"a":1}', 'json') } })
    expect(useConfigStore.getState().getValue('k')).toEqual({ a: 1 })
  })

  it('取不存在的 key → 返回默认值', () => {
    expect(useConfigStore.getState().getValue('nope', '兜底')).toBe('兜底')
  })
})
