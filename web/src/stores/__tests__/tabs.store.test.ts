/**
 * 路由级软缓存 Store(2026-10-25 多 tab 下线后的形状)
 *
 * 多 tab 标签条(TabsBar)下线后,本 store 只剩两件事:
 *   1. open(key)      —— 登记一条缓存槽位
 *   2. patchCache(key) —— 往槽位里写 scrollY / formState
 *
 * 滚动位置恢复是**活的**:BasicLayout 切走时写 scrollY、切回时读回来。
 * 所以这个 store 不能没有回归网 —— 它静默坏掉的话,表现是"切回页面
 * 滚动位置不对",不报错、不白屏,很难归因。
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { useTabsStore, MAX_CACHED_PATHS } from '@/stores/tabs'
import { STORAGE_KEYS } from '@/constants/app'

describe('stores/tabs · 路由级软缓存', () => {
  beforeEach(() => {
    useTabsStore.setState({ tabs: [] })
  })

  it('open 给新 path 登记一条空槽位', () => {
    useTabsStore.getState().open('/system/user')
    const tabs = useTabsStore.getState().tabs
    expect(tabs).toHaveLength(1)
    expect(tabs[0]?.key).toBe('/system/user')
    expect(tabs[0]?.cache).toEqual({})
  })

  it('open 同一个 path 不会重复登记', () => {
    const { open } = useTabsStore.getState()
    open('/system/user')
    open('/system/user')
    expect(useTabsStore.getState().tabs).toHaveLength(1)
  })

  it('open 已存在时不清掉已有缓存(重复进同一个页面不能把 scrollY 抹了)', () => {
    useTabsStore.getState().open('/system/user')
    useTabsStore.getState().patchCache('/system/user', { scrollY: 320 })
    useTabsStore.getState().open('/system/user')
    const tab = useTabsStore.getState().tabs.find((t) => t.key === '/system/user')
    expect(tab?.cache.scrollY).toBe(320)
  })

  it('patchCache 写 scrollY,读回来还是同一个值', () => {
    useTabsStore.getState().open('/system/role')
    useTabsStore.getState().patchCache('/system/role', { scrollY: 128 })
    const tab = useTabsStore.getState().tabs.find((t) => t.key === '/system/role')
    expect(tab?.cache.scrollY).toBe(128)
  })

  it('patchCache 合并而不是覆盖(写 scrollY 不该把 formState 冲掉)', () => {
    useTabsStore.getState().open('/system/dict')
    useTabsStore.getState().patchCache('/system/dict', { formState: { group: 'a' } })
    useTabsStore.getState().patchCache('/system/dict', { scrollY: 10 })
    const tab = useTabsStore.getState().tabs.find((t) => t.key === '/system/dict')
    expect(tab?.cache).toEqual({ formState: { group: 'a' }, scrollY: 10 })
  })

  it('patchCache 打不存在的 path 是安全的空操作(不凭空造一条槽位)', () => {
    useTabsStore.getState().patchCache('/never-visited', { scrollY: 5 })
    expect(useTabsStore.getState().tabs).toHaveLength(0)
  })

  it('多个 path 各自独立存缓存', () => {
    const { open, patchCache } = useTabsStore.getState()
    open('/a')
    open('/b')
    patchCache('/a', { scrollY: 1 })
    patchCache('/b', { scrollY: 2 })
    const find = (k: string) => useTabsStore.getState().tabs.find((t) => t.key === k)?.cache.scrollY
    expect(find('/a')).toBe(1)
    expect(find('/b')).toBe(2)
  })
})

/**
 * 上限(M103)
 *
 * `open(key)` 的 key 就是 pathname,而路由表里有**动态路由**
 * (`flow/:id` / `flow/:id/stage/:stageId` / `approval/:id`)——
 * 每打开一张不同的审批单就是一条新的、永远不会被回收的槽位,
 * 而且整个列表还会 persist 到 localStorage。以前这里是**无上界**的。
 */
describe('stores/tabs · 槽位数量上限', () => {
  beforeEach(() => {
    useTabsStore.setState({ tabs: [] })
    localStorage.clear()
  })

  it('超过上限就淘汰,不会无限增长', () => {
    const { open } = useTabsStore.getState()
    for (let i = 0; i < MAX_CACHED_PATHS + 50; i++) open(`/approval/${i}`)
    expect(useTabsStore.getState().tabs).toHaveLength(MAX_CACHED_PATHS)
  })

  it('淘汰优先挑"没有缓存内容"的条目(丢了也不亏)', () => {
    const { open, patchCache } = useTabsStore.getState()
    open('/keep-me')
    patchCache('/keep-me', { scrollY: 42 })
    // 剩下的都填满上限
    for (let i = 0; i < MAX_CACHED_PATHS - 1; i++) open(`/empty/${i}`)
    expect(useTabsStore.getState().tabs).toHaveLength(MAX_CACHED_PATHS)

    // 再开一条 → 必须淘汰一条空的,**有缓存的那条要留下**
    open('/one-more')
    const tabs = useTabsStore.getState().tabs
    expect(tabs).toHaveLength(MAX_CACHED_PATHS)
    expect(tabs.find((t) => t.key === '/keep-me')?.cache.scrollY).toBe(42)
  })

  it('刚开的那条一定在列表里(不能把自己淘汰掉)', () => {
    const { open } = useTabsStore.getState()
    for (let i = 0; i < MAX_CACHED_PATHS + 10; i++) open(`/flow/${i}`)
    const tabs = useTabsStore.getState().tabs
    expect(tabs[tabs.length - 1]?.key).toBe(`/flow/${MAX_CACHED_PATHS + 9}`)
  })

  it('rehydrate 时把超上限的存量截断(修上限之前写进 localStorage 的那些)', async () => {
    // 模拟"上限还没加的时候"攒下的 300 条
    const legacy = Array.from({ length: 300 }, (_, i) => ({
      key: `/legacy/${i}`,
      cache: {},
    }))
    localStorage.setItem(
      STORAGE_KEYS.TABS_STATE,
      JSON.stringify({ state: { tabs: legacy }, version: 0 }),
    )

    await useTabsStore.persist.rehydrate()
    expect(useTabsStore.getState().tabs).toHaveLength(MAX_CACHED_PATHS)
    // 保留的是**最新**的那批
    expect(useTabsStore.getState().tabs[useTabsStore.getState().tabs.length - 1]?.key).toBe(
      '/legacy/299',
    )
  })
})
