/**
 * 路由级软缓存 Store(滚动位置 / 表单值)
 *
 * ⚠️ **2026-10-25:多 tab 功能下线** ——
 *   顶部的 TabsBar 标签条被删除(用户:「顶部的 TabsBar 没什么用,而且整个界面
 *   不搭」)。本 store 原来是为多 tab 服务的:打开的 tab 列表 + activeKey +
 *   关闭 / 关闭其他 / 关闭全部 / 刷新 / 改名 —— 那一整套操作接口随 TabsBar
 *   一起没了。留着就是**零调用方的死代码**(activeKey 同理:切 tab 的概念
 *   没了,谁还关心"当前是哪个 tab")。
 *
 * **留下来的只有一件事:按 path 记软缓存。**
 *   - scrollY:BasicLayout 切走时自动写入、切回时恢复(见 BasicLayout.tsx)
 *   - formState:业务页用 useRememberForm() 自己写(见 hooks/useRememberForm.ts)
 *
 * ⚠️ **数据结构刻意保持 `tabs: Tab[]`,不改成 `Record<path, cache>`**:
 *   老的 localStorage(键 `rapid-admin:tabs`)里存的就是这个形状。
 *   换结构 = 存量数据全部作废 = 一次无谓的缓存丢失 ——
 *   而"多 tab 下线"跟"滚动位置恢复"本来就毫无关系,不该顺手把它一起弄丢。
 *   (老数据里多出来的 title / closable 字段反序列化后没人读,无害,留着。)
 *
 * 持久化:zustand persist → localStorage。
 * ⚠️ 以前还持久化 activeKey(刷新回首页更安全),现在没有 activeKey 这个概念了。
 */
import { create } from 'zustand'
import { persist, createJSONStorage } from 'zustand/middleware'
import { STORAGE_KEYS } from '@/constants/app'

export interface Tab {
  /** tab key = path(保证唯一) */
  key: string
  /**
   * 软缓存
   *  - scrollY: 切走时 BasicLayout 自动写入
   *  - formState: 业务页 useRememberForm() 自管理
   */
  cache: {
    scrollY?: number
    formState?: Record<string, unknown>
  }
}

/**
 * 软缓存槽位上限
 *
 * ⚠️ **M103:以前这个列表是**无上界**的。**
 *   `open(key)` 的 key 就是 pathname,而路由表里有**动态路由**
 *   (`flow/:id` / `flow/:id/stage/:stageId` / `approval/:id`)——
 *   每打开一张不同的审批单,就是一条新的、永远不会被回收的槽位。
 *   列表还会被 persist 到 localStorage,于是**只涨不落**:
 *   逛一天审批,localStorage 里堆几百条 `{key, cache:{}}`。
 *   单条不大,但它是个**只增不减的写放大**:每次渲染 BasicLayout 都要
 *   `tabs.find(...)` 线性扫一遍越来越长的数组。
 */
export const MAX_CACHED_PATHS = 100

/** 槽位是不是"空的"(没有任何缓存内容) */
function isEmptyCache(t: Tab): boolean {
  return Object.keys(t.cache ?? {}).length === 0
}

/**
 * 超上限时淘汰一条。
 *
 * **优先淘汰"没有缓存内容"的那一条** —— 它的 cache 是空的,淘汰它等于
 * 零损失(回到那个页面本来也是 scroll 0、没有表单值);
 * 只有在**每一条都有缓存**时才淘汰最旧的那条。
 * 这么分是因为 BasicLayout 切走时会把 scrollY 写回**上一条**槽位 ——
 * 无脑淘汰最旧的,有可能正好把"刚离开的那一页"的滚动位置写丢。
 */
function evictOne(tabs: Tab[]): Tab[] {
  if (tabs.length <= MAX_CACHED_PATHS) return tabs
  let idx = tabs.findIndex(isEmptyCache)
  if (idx < 0) idx = 0
  const next = tabs.slice()
  next.splice(idx, 1)
  return next
}

/** 截断到上限(保留最新的 MAX 条)—— 给 persist 的 rehydrate 用 */
function trimToCap(tabs: Tab[]): Tab[] {
  return tabs.length <= MAX_CACHED_PATHS ? tabs : tabs.slice(tabs.length - MAX_CACHED_PATHS)
}

interface TabsState {
  tabs: Tab[]
  /**
   * 确保 key 有对应的缓存槽位(已存在则不动)
   *
   * 以前这个方法还管 activeKey(多 tab 切激活),现在只剩"登记一条缓存槽位"。
   */
  open: (key: string) => void
  /** 写软缓存 */
  patchCache: (key: string, patch: Partial<Tab['cache']>) => void
}

export const useTabsStore = create<TabsState>()(
  persist(
    (set) => ({
      tabs: [],

      open: (key) =>
        set((s) =>
          s.tabs.some((t) => t.key === key)
            ? // 已存在 → 什么都不改(返回空 partial,状态引用不变,不会触发重渲染)
              {}
            : { tabs: evictOne([...s.tabs, { key, cache: {} }]) }
        ),

      patchCache: (key, patch) =>
        set((s) => ({
          tabs: s.tabs.map((t) =>
            t.key === key ? { ...t, cache: { ...t.cache, ...patch } } : t
          ),
        })),
    }),
    {
      // ⚠️ key 沿用老的 `rapid-admin:tabs` —— 改名会让存量缓存直接作废
      name: STORAGE_KEYS.TABS_STATE,
      storage: createJSONStorage(() => localStorage),
      partialize: (s) => ({ tabs: s.tabs }),
      /**
       * ⚠️ M103:rehydrate 时**截断到上限** —— 修好上限之前已经写进
       *   localStorage 的几百条旧槽位,光加上限不会自动清理它们。
       *   这一步等于顺手给存量用户"瘦一次身"。
       */
      merge: (persisted, current) => {
        const p = persisted as Partial<TabsState> | undefined
        const tabs = p?.tabs
        return {
          ...current,
          tabs: Array.isArray(tabs) ? trimToCap(tabs) : current.tabs,
        }
      },
    }
  )
)
