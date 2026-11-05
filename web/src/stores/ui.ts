/**
 * UI Store
 *
 * - 侧边栏折叠状态
 * - 皮肤(整体外观,含明暗 —— 2026-11-05 扁平化后不再有独立 theme 轴)
 * - 语言
 * - 全局 loading 计数
 */
import { create } from 'zustand'
import { persist, createJSONStorage } from 'zustand/middleware'
import { STORAGE_KEYS } from '@/constants/app'
// M173:切语言时顺带触发语言包懒加载(registry 只 import zh-CN + react,
// 无循环依赖)。ensureLocale 幂等,LangRoot 的 useEffect([lang]) 那条触发
// 照旧 —— 两道保险,非 React 场景(测试 / 脚本)调 setLang 也能拉到包。
import { ensureLocale } from '@/locales/registry'

type Lang = 'zh-CN' | 'en-US'
/** 皮肤 id(registry.getSkin 未知 id 显式降级 white,见 web/src/theme/registry.ts) */
type Skin = string

interface UIState {
  sidebarCollapsed: boolean
  /** 皮肤 = 整体外观(明暗由皮肤的 appearance 决定) */
  skin: Skin
  lang: Lang
  /** 全局 loading 任务计数(>0 显示 loading bar) */
  loadingCount: number

  toggleSidebar: () => void
  setSidebar: (collapsed: boolean) => void
  setSkin: (s: Skin) => void
  setLang: (l: Lang) => void

  /** loading 计数(允许多任务并存) */
  addLoading: () => void
  removeLoading: () => void
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
      sidebarCollapsed: false,
      skin: 'white',
      lang: 'zh-CN',
      loadingCount: 0,

      toggleSidebar: () => set({ sidebarCollapsed: !get().sidebarCollapsed }),
      setSidebar: (collapsed) => set({ sidebarCollapsed: collapsed }),
      setSkin: (s) => set({ skin: s }),
      setLang: (l) => {
        set({ lang: l })
        void ensureLocale(l).catch(() => undefined)
      },

      addLoading: () => set({ loadingCount: get().loadingCount + 1 }),
      removeLoading: () => set({ loadingCount: Math.max(0, get().loadingCount - 1) }),
    }),
    {
      name: STORAGE_KEYS.THEME,
      storage: createJSONStorage(() => localStorage),
      partialize: (s) => ({
        sidebarCollapsed: s.sidebarCollapsed,
        skin: s.skin,
        lang: s.lang,
      }),
    }
  )
)
