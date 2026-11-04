/**
 * 字典 Store
 *
 * 集中管理前端用到的所有字典(下拉选项源)
 * 业务组件通过 useDict('user_status') 拿到 {value, label} 数组
 *
 * 字典约束(2026-10-18 L3):
 *   - groupCode 类型收紧为 DictGroupCode(联合类型)
 *   - 但 Store 内部仍用 string 作 map key(运行时动态加载所有 groupCode)
 *   - 类型在边界(getLabel / fetchDict)处收紧
 *
 * 缓存失效(2026-10-26 M94):
 *   - 30 分钟 TTL 原本意味着"管理员改了标签,别人最长半小时看不到"。
 *   - 现在服务端在字典改动后会广播 `dict.changed`,前端收到就调
 *     `invalidate(group_code)` 作废对应组的缓存(见下)。
 */
import { create } from 'zustand'
import { dictApi } from '@/api/config'
import type { DictItem } from '@/api/config'
import type { DictGroupCode } from '@/types/dict'

interface DictState {
  /** 字典缓存(以 groupCode 为 key) */
  dicts: Record<string, DictItem[]>
  /** 最后拉取时间(以 groupCode 为 key) */
  lastFetchAt: Record<string, number>

  /** 拉取某个字典组(已存在 + 未过期则直接返回) */
  fetchDict: (groupCode: DictGroupCode, force?: boolean) => Promise<DictItem[]>
  /** 取某个字典项的 label */
  getLabel: (groupCode: DictGroupCode, value: string | number) => string
  /**
   * 作废某个字典组的缓存 —— 2026-10-26 M94
   *
   * ⚠️ **参数是 `string` 不是 `DictGroupCode`** —— 调用方有两个:
   * ① 字典管理页自己改完(手里有 `DictItem.group_code`,运行时是 string);
   * ② SSE 事件 `dict.changed` 的 payload(服务端给的运行时字符串,
   *    编译期**不可能**是 `DictGroupCode` 联合类型的成员)。
   * 强行断言成 `DictGroupCode` 只会把编译期检查变成一句空话。
   */
  invalidate: (groupCode: string) => void
  /** 批量重置 */
  reset: () => void
}

const CACHE_TTL = 30 * 60 * 1000 // 字典变化少,30 分钟

export const useDictStore = create<DictState>((set, get) => ({
  dicts: {},
  lastFetchAt: {},

  fetchDict: async (groupCode, force = false) => {
    const { dicts, lastFetchAt } = get()
    if (!force && dicts[groupCode] && Date.now() - (lastFetchAt[groupCode] || 0) < CACHE_TTL) {
      return dicts[groupCode]
    }
    const list = await dictApi.byGroup(groupCode)
    set((s) => ({
      dicts: { ...s.dicts, [groupCode]: list },
      lastFetchAt: { ...s.lastFetchAt, [groupCode]: Date.now() },
    }))
    return list
  },

  getLabel: (groupCode, value) => {
    const list = get().dicts[groupCode] || []
    const item = list.find((d) => String(d.item_value) === String(value))
    return item?.label || String(value)
  },

  /**
   * 作废某个字典组的缓存 —— 2026-10-26 M94
   *
   * ## 语义:**删掉 `lastFetchAt[groupCode]`,保留 `dicts[groupCode]`**
   *
   * 为什么**不**把 `dicts` 一起删:
   *   删了的话,所有正在用这个组的下拉会**瞬间变空**(options 变成 []),
   *   直到重拉回来。字典是全局的 —— 一个组被改,几十个页面的下拉同时闪一下,
   *   比"多显示半秒旧标签"糟糕得多。
   *   保留旧值的话,重拉回来之前用户看到的是**旧标签**(可接受),
   *   重拉落地后无缝切到新标签。
   *
   * 为什么删 `lastFetchAt` 就能触发重拉:
   *   ① `useDict` 的 effect 条件是 `if (!dicts || !lastFetchAt) fetchDict(...)`
   *      —— `lastFetchAt` 一没,effect 的依赖变了 → effect 重跑 → 发起重拉;
   *   ② `fetchDict` 的 TTL 判断是
   *      `Date.now() - (lastFetchAt[groupCode] || 0) < CACHE_TTL`
   *      —— 没有 `lastFetchAt` 就是 0,`Date.now() - 0` 远大于 TTL → 判过期 → 真发请求。
   *
   * ⚠️ **这依赖 `useDict` / `fetchDict` 的上述两处实现细节** ——
   *   改这两处时这里必须一起改,否则"作废"会退化成"静默不生效"
   *   (症状:字典改了但下拉还是旧值,而且没有任何报错)。
   *   回归网在 `web/src/stores/__tests__/dict.store.test.ts`。
   */
  invalidate: (groupCode) => {
    if (!groupCode) return
    set((s) => {
      // 已经作废过了 → 什么都不用做(上一次作废触发的重拉还没回来)
      if (!(groupCode in s.lastFetchAt)) return s
      const lastFetchAt = { ...s.lastFetchAt }
      delete lastFetchAt[groupCode]
      return { lastFetchAt }
    })
  },

  reset: () => set({ dicts: {}, lastFetchAt: {} }),
}))
