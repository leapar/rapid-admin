/**
 * 平台配置 Store
 *
 * 把"全量配置"缓存在前端,业务代码通过 getConfig(code) 读取
 * 优势:
 * 1. 减少后端调用(全量配置一次拉,10 分钟内走缓存)
 * 2. 配置变更实时生效(后端推送或主动 refresh)
 * 3. 弱网环境下业务能继续跑
 *
 * ⚠️ **读接口只有 super_admin 能用** —— `/platform/configs` 挂的是
 *   `rbac("config", "list")`,而 `config:*` 只绑给 super_admin
 *   (见 memory:config-rbac-super-admin-only)。
 *   ⇒ 本 store / 本文件配套的 hook(`hooks/useConfig.ts`)**当前零调用方**:
 *   业务页拿不到配置,只有超管配置页在用 —— 而它走的是 `configApi` 直连,不经过本 store。
 *   这不是"忘了接",是**接口权限决定的**:普通业务页要读配置,得先有面向业务的读接口。
 *   所以这里把它当**预留 API**维护(修好、测住、标明边界),而不是删掉。
 */
import { create } from 'zustand'
import { configApi } from '@/api/config'
import type { ConfigItem } from '@/types/config'

interface ConfigState {
  /** 全量配置(以 code 为 key) */
  configMap: Record<string, ConfigItem>
  /** 最后一次拉取时间(毫秒) */
  lastFetchAt: number
  /** 是否正在拉取 */
  loading: boolean

  /** 拉取全量配置(强制 = true 时忽略时间窗口) */
  fetchAll: (force?: boolean) => Promise<void>
  /** 按 code 读 */
  getConfig: (code: string) => ConfigItem | undefined
  /** 按 code 读 value */
  getValue: <T = string>(code: string, defaultValue?: T) => T
  /** 重置 */
  reset: () => void
}

/** 缓存有效期:10 分钟 */
const CACHE_TTL = 10 * 60 * 1000

/**
 * 每页条数 —— **= 后端 `pagination.MaxSize`(100)**。
 *
 * ⚠️ 后端 `Normalize()` 会把 `size` 截到 100,所以 `size: 1000` 实际只回 100 条。
 *   本 store 以前就写 `page_size: 1000` 指望"一次拉全量" —— 那时配置项只有 17 条,
 *   看不出问题;一旦超过 100 条,`configMap` 就会**静默少一半**(没有任何报错)。
 *   现在改成**循环翻页直到拿齐**。
 */
const PAGE_SIZE = 100
/** 翻页硬上限 —— 兜底防死循环(后端要是返回了 total 之外的东西) */
const MAX_PAGES = 50

/**
 * 按 `value_type` 解析配置值 —— **服务端给的类型是真源**。
 *
 * ⚠️ **不要用"无脑 JSON.parse"** —— 那是本文件以前的做法,有两个错:
 *   ① `value_type='string'`、值是 `"123"` → JSON.parse 给出**数字 123**,
 *      而调用方声明的是 `useConfig<string>('x')`;
 *   ② `value_type='string'`、值是 `"true"` → 同理变成布尔。
 *   服务端明明在 `value_type` 里写了类型,前端却绕过它自己猜 —— 这就是"类型真源被绕过"。
 *
 * 规则(与后端 `model.ConfigValueType` 一一对应):
 *   · string / secret / cert_pem / private_key_pem → 原样返回字符串
 *   · int    → Number(值);解析不出来回退原字符串
 *   · bool   → 'true' / '1' → true,其余 false
 *   · json   → JSON.parse;失败回退原字符串
 */
export function parseConfigValue<T = string>(item: ConfigItem | undefined, defaultValue: T): T {
  if (!item) return defaultValue
  const raw = item.value
  switch (item.value_type) {
    case 'int': {
      const n = Number(raw)
      return (raw === '' || Number.isNaN(n) ? raw : n) as unknown as T
    }
    case 'bool':
      return (raw === 'true' || raw === '1') as unknown as T
    case 'json': {
      try {
        return JSON.parse(raw) as T
      } catch {
        return raw as unknown as T
      }
    }
    default:
      return raw as unknown as T
  }
}

export const useConfigStore = create<ConfigState>((set, get) => ({
  configMap: {},
  lastFetchAt: 0,
  loading: false,

  fetchAll: async (force = false) => {
    const { lastFetchAt, loading } = get()
    if (loading) return
    if (!force && Date.now() - lastFetchAt < CACHE_TTL) return
    set({ loading: true })
    try {
      const map: Record<string, ConfigItem> = {}
      let fetched = 0
      let page = 1
      // ⚠️ **必须循环翻页** —— 后端 size 上限 100,一次请求拿不到"全量"。
      //   以前写死 `page_size: 1000` 指望一次拉完,超过 100 条就静默少一半。
      for (;;) {
        const resp = await configApi.list({ page, size: PAGE_SIZE })
        const list = Array.isArray(resp?.list) ? resp.list : []
        list.forEach((item) => {
          map[item.config_key] = item
        })
        fetched += list.length
        // 短页 = 最后一页
        if (list.length < PAGE_SIZE) break
        if (typeof resp.total === 'number' && fetched >= resp.total) break
        if (page >= MAX_PAGES) break
        page += 1
      }
      set({ configMap: map, lastFetchAt: Date.now() })
    } finally {
      set({ loading: false })
    }
  },

  getConfig: (code) => get().configMap[code],

  getValue: <T = string>(code: string, defaultValue?: T) => {
    const item = get().configMap[code]
    return parseConfigValue<T>(item, defaultValue as T)
  },

  reset: () => set({ configMap: {}, lastFetchAt: 0 }),
}))
