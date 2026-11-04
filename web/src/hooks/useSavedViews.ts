/**
 * useSavedViews —— 保存的列表视图(§2.8 #21,P2)
 *
 * 框架级能力:把"我常用的那个视图"命名存下来。视图内容(筛选条件 + 排序 +
 * 列状态打包成什么形状)完全由调用方决定 —— 本 hook 对 state 只做原样存取,
 * 不窥探内容;它只管"命名快照"的存 / 取 / 删 / 列 / 生效态。
 *
 * ⚠️ **为什么不能让业务页自己 localStorage 搞定**(这就是本 hook 存在的理由):
 *
 *   1. **localStorage 里的数据不受你控制**:用户手动改过、老版本格式、
 *      扩展程序写入、JSON 被截断 —— parse 出来什么都可能。业务页裸写的
 *      版本几乎必然 `JSON.parse` 一炸整个列表页白屏。本 hook 把所有脏数据
 *      形态(JSON 损坏 / 不是数组 / 元素缺字段)统一清洗成合法视图列表,
 *      坏到救不了(JSON 损坏 / 根本不是数组)就静默重置为空并把坏数据删掉,
 *      不让下一次加载再踩同一个坑。
 *
 *   2. **写失败不是异常路径,是常态**:隐私模式、配额满、Safari 无痕,
 *      `setItem` 都会抛。抛出去就是把异常甩给列表页 —— 而正确行为是
 *      内存态照常可用(这次会话里视图还能用),只 warn 一次不刷屏。
 *
 *   3. **storageKey 是会变的**(切页面配置 / 切租户 / 切用户):换 key 必须
 *      重新读新 key 的数据 —— 这是本仓 M111 真实踩过的坑(换 key 不重读,
 *      界面显示的是上一个 key 的数据)。本 hook 响应 storageKey 变化,
 *      换 key 时重读并清空生效态(activeId 属于旧 key 的视图,不作保留)。
 *
 * state 的推荐结构(调用方自行组装,hook 不管形状):
 *   interface MyViewState {
 *     filters: Record<string, unknown>   // 筛选条件
 *     sorter: { field: string; order: 'ascend' | 'descend' } | null
 *     columns: { key: string; visible: boolean; width: number }[]
 *   }
 * ⚠️ state 必须可 JSON 序列化(经 localStorage 落盘;函数 / DOM 节点 / undefined
 *   字段会在序列化时丢失 —— 丢了的视图元素重读时会被清洗掉)。
 *
 * id 生成:时间戳(36 进制)+ 随机段拼接,不用 crypto.randomUUID ——
 *   稳妥兼容,不赌目标浏览器的 crypto 可用性。
 *
 * 同名覆盖语义(**写死**):`save` 遇到同名视图 = 覆盖内容(state),
 *   **保留原 id 与原 createdAt** —— createdAt 的语义是"首次创建时间",
 *   不是"最后一次保存时间";不引入 updatedAt,保持结构最小。
 *   覆盖后视图仍按 createdAt 排序,位置不变,用户不会看到"刚存过的
 *   视图突然跳到最前"。
 *
 * 挤出策略:超过 max(默认 20,最小按 1 算)时挤掉 createdAt 最旧的,
 *   且**绝不挤掉刚 save 的这条**(时钟回拨等病态情况下也保它)。
 */
import { useCallback, useEffect, useRef, useState } from 'react'

/** 与 useTablePro 的 `tablepro:` 同风格的前缀 */
const STORAGE_PREFIX = 'savedview:'

export interface SavedView<T = unknown> {
  /** 时间戳+随机 拼接,稳定且唯一 */
  id: string
  name: string
  /** 调用方的任意视图状态对象(筛选+排序+列状态),hook 不窥探内容 */
  state: T
  /** 首次创建时间(epoch ms);同名覆盖不改写它 */
  createdAt: number
}

export interface UseSavedViewsResult<T> {
  /** 全部视图,createdAt 降序 */
  views: SavedView<T>[]
  /** 当前生效视图 id(apply/save 更新;remove 掉生效视图自动清;换 key 清) */
  activeId: string | null
  /** 命名保存;同名 → 覆盖内容(保留原 id 与 createdAt,见文件头语义) */
  save: (name: string, state: T) => SavedView<T>
  /** 应用某视图;不存在的 id 返回 undefined 且不动 activeId */
  apply: (id: string) => T | undefined
  remove: (id: string) => void
  rename: (id: string, name: string) => void
  /** 用户手动改了筛选,退出"已选视图"态 */
  clearActive: () => void
}

/**
 * 清洗从 localStorage 读出来的原始数据(纯函数,可独立测试)。
 *
 * 只认"长得像 SavedView"的元素:id/name 是非空 string、createdAt 是有限
 * number、state 字段存在。其余(缺字段 / 类型不对 / 压根不是对象)一律
 * 丢弃;根不是数组返回 []。只重建已知字段 —— 存储里多出来的未知字段
 * (老版本残留)不往内存带。返回值按 createdAt 降序(与 hook 的内存
 * 不变量一致,加载即排序)。
 */
export function sanitizeViews<T>(raw: unknown): SavedView<T>[] {
  if (!Array.isArray(raw)) return []
  const out: SavedView<T>[] = []
  for (const item of raw) {
    if (item === null || typeof item !== 'object') continue
    const rec = item as Record<string, unknown>
    if (typeof rec.id !== 'string' || rec.id === '') continue
    if (typeof rec.name !== 'string' || rec.name === '') continue
    if (typeof rec.createdAt !== 'number' || !Number.isFinite(rec.createdAt)) continue
    if (!('state' in rec)) continue
    out.push({ id: rec.id, name: rec.name, state: rec.state as T, createdAt: rec.createdAt })
  }
  out.sort((a, b) => b.createdAt - a.createdAt)
  return out
}

/** 视图 id:时间戳(36 进制)+ 随机段,不依赖 crypto.randomUUID */
function genId(): string {
  return `sv-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

/** 从 localStorage 读一个 key 并清洗;JSON 损坏 / 不是数组 → 删掉坏数据,静默重置 */
function loadViews<T>(storageKey: string): SavedView<T>[] {
  let raw: string | null
  try {
    raw = localStorage.getItem(STORAGE_PREFIX + storageKey)
  } catch {
    return [] // localStorage 读都读不了(隐私模式极端情况)→ 当空
  }
  if (raw === null) return []
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) {
      // 根本不是数组:整份数据都是垃圾,删掉免得每次加载都白解析
      try {
        localStorage.removeItem(STORAGE_PREFIX + storageKey)
      } catch {
        // ignore
      }
      return []
    }
    return sanitizeViews<T>(parsed)
  } catch {
    // JSON 损坏:同上,静默重置并删坏数据
    try {
      localStorage.removeItem(STORAGE_PREFIX + storageKey)
    } catch {
      // ignore
    }
    return []
  }
}

export function useSavedViews<T>(
  storageKey: string,
  options?: { max?: number },
): UseSavedViewsResult<T> {
  /** max 最小按 1 算(传 0 / 负数不等于"一个都存不下还报错"的场景值得支持) */
  const max = Math.max(1, options?.max ?? 20)

  const [views, setViews] = useState<SavedView<T>[]>(() => loadViews<T>(storageKey))
  const [activeId, setActiveId] = useState<string | null>(null)
  /**
   * views 的同步镜像:写操作(ref 读 → 算 next → ref 与 state 一起更)。
   * ⚠️ 不直接读 state 闭包的理由:同一轮事件里连续两次写(存完紧接着删),
   * 后一次的闭包还是旧列表,会把前一次的改动整个顶掉 —— 与 React 批处理
   * 无关的"自己人打架"。ref 同步更新,连续写永远接续最新结果。
   */
  const viewsRef = useRef<SavedView<T>[]>(views)

  // ⚠️ M111 的坑:storageKey 变化必须重读新 key 的数据(首次挂载也会跑一遍,
  //    重读同 key 是幂等的,无副作用)。activeId 属于旧 key 的视图,一并清空。
  useEffect(() => {
    const loaded = loadViews<T>(storageKey)
    viewsRef.current = loaded
    setViews(loaded)
    setActiveId(null)
  }, [storageKey])

  /** 写失败只 warn 一次的旗标(挂在实例上,不跨实例刷屏) */
  const warnRef = useRef(false)

  /**
   * 写盘:state 是真相,storage 只是持久化。写失败(配额 / 隐私模式)不炸,
   * 内存态照常可用;只 warn 一次,不刷屏。
   */
  const persist = useCallback(
    (next: SavedView<T>[]) => {
      try {
        localStorage.setItem(STORAGE_PREFIX + storageKey, JSON.stringify(next))
      } catch {
        if (!warnRef.current) {
          warnRef.current = true
          console.warn('[useSavedViews] localStorage 写入失败(配额/隐私模式),视图仅保留在内存中')
        }
      }
    },
    [storageKey],
  )

  const save = useCallback(
    (name: string, state: T): SavedView<T> => {
      const current = viewsRef.current
      const existing = current.find((v) => v.name === name)
      const saved: SavedView<T> = existing
        ? { ...existing, state } // 同名覆盖:保留原 id 与 createdAt
        : { id: genId(), name, state, createdAt: Date.now() }
      // 不做整体 sort:新建的插最前(now ≥ 既有所有 createdAt);覆盖的原位替换,
      // 保持"createdAt 降序"不变量 —— 同毫秒存的视图顺序也稳定
      const next = existing
        ? current.map((v) => (v.id === saved.id ? saved : v))
        : [saved, ...current]
      // 挤出最旧的,但绝不挤掉刚 save 的这条(时钟回拨的病态情况下也保它)
      while (next.length > max) {
        let oldest = -1
        for (let i = next.length - 1; i >= 0; i--) {
          if (next[i]!.id !== saved.id) {
            oldest = i
            break
          }
        }
        if (oldest < 0) break
        next.splice(oldest, 1)
      }
      viewsRef.current = next
      setViews(next)
      persist(next)
      setActiveId(saved.id)
      return saved
    },
    [max, persist],
  )

  const apply = useCallback(
    (id: string): T | undefined => {
      const hit = viewsRef.current.find((v) => v.id === id)
      if (!hit) return undefined // 不存在:不炸,也不把 activeId 指到一个空 id 上
      setActiveId(id)
      return hit.state
    },
    [],
  )

  const remove = useCallback(
    (id: string) => {
      const current = viewsRef.current
      const next = current.filter((v) => v.id !== id)
      if (next.length === current.length) return // 没这条,no-op
      viewsRef.current = next
      setViews(next)
      persist(next)
      setActiveId((prev) => (prev === id ? null : prev))
    },
    [persist],
  )

  const rename = useCallback(
    (id: string, name: string) => {
      const hit = viewsRef.current.find((v) => v.id === id)
      if (!hit || hit.name === name) return
      // 改名不制造同名冲突:同名旧视图保持不动,改名视图顶着重名共存
      // (save 的同名覆盖按 name 找,重名时覆盖先遇到的那个 —— 调用方自己避免)
      const next = viewsRef.current.map((v) => (v.id === id ? { ...v, name } : v))
      viewsRef.current = next
      setViews(next)
      persist(next)
    },
    [persist],
  )

  const clearActive = useCallback(() => setActiveId(null), [])

  return { views, activeId, save, apply, remove, rename, clearActive }
}

export default useSavedViews
