/**
 * 皮肤注册表 —— docs/theming.md §3.2
 *
 * skins/<id>.ts 是**显式全量定义**(palette / comp / cssVars 逐参数写出,
 * 方便拷走改成自己的品牌);这里把它们挂进 SKINS。顶栏菜单呈现的是
 * **五个同级外观选项**:白色(默认)/ 黑色 / 科技蓝 / 政务红 / 文博棕金
 * (SkinSwitcher)。旧默认皮肤 id 'orange' 在 LEGACY_ALIASES 里静默映射到
 * white —— 老用户 localStorage 与后端 branding.skin: orange 都继续可用,
 * 不 warn。
 *
 * ⚠️ 新增品牌色值必须进 skins/,不得直接写进 LangRoot 的 token(AGENTS.md 硬约束 6)。
 */
import type { SkinDefinition } from './types'
import white from './skins/white'
import black from './skins/black'
import museum from './skins/museum'
import tech from './skins/tech'
import gov from './skins/gov'

export const DEFAULT_SKIN_ID = 'white'

/** 旧 id → 新 id(静默别名,不 warn —— 2026-11-05 orange 更名为 white/black) */
const LEGACY_ALIASES: Record<string, string> = {
  orange: 'white',
}

/** 内置皮肤表(声明顺序即切换器展示顺序;自定义皮肤拷 skins/custom.example.ts 后挂一行) */
const SKINS: Record<string, SkinDefinition> = Object.fromEntries(
  [white, black, museum, tech, gov].map((s) => [s.id, s])
)

/** 默认皮肤直引(绕开索引访问的 undefined 推断) */
const defaultSkin: SkinDefinition = white

/** 插件/编辑器运行时注册皮肤(id 冲突时后注册者赢) */
export function registerSkin(skin: SkinDefinition): SkinDefinition {
  SKINS[skin.id] = skin
  registryListeners.forEach((fn) => fn())
  return skin
}

/* ---- 注册表变更广播 ----
 * store 里的 skin 只是 id:同名 id 下**替换对象**(编辑器试穿热更新的路径)
 * 不会让 LangRoot 重渲染 —— 它订阅的是 store 的 id,不是 registry 的对象。
 * 注册者在这里广播,消费方(LangRoot)订阅后强制重算 getSkin,
 * 试穿中的草稿才能"编辑即生效"。(2026-11-05 编辑器 MVP 补) */
type RegistryListener = () => void
const registryListeners = new Set<RegistryListener>()

/** 订阅注册表变更;返回退订函数(React effect cleanup 直接用) */
export function onRegistryChange(fn: RegistryListener): () => void {
  registryListeners.add(fn)
  return () => {
    registryListeners.delete(fn)
  }
}

/** 取皮肤;未知 id 显式降级到默认(warn 不报错 —— theming.md §6 容错降级) */
export function getSkin(id?: string): SkinDefinition {
  const resolved = (id && LEGACY_ALIASES[id]) || id
  if (resolved && SKINS[resolved]) return SKINS[resolved]
  if (id && id !== DEFAULT_SKIN_ID) {
    console.warn(`[theme] 未知皮肤 id "${id}",落回默认 "${DEFAULT_SKIN_ID}"`)
  }
  return defaultSkin
}

/** 内置皮肤 id 清单(供后端 branding.skin 校验参考) */
export function listSkinIds(): string[] {
  return Object.keys(SKINS)
}

/** 皮肤清单(切换器 UI 用):内置声明顺序即展示顺序 */
export function listSkins(): { id: string; name: string }[] {
  return Object.values(SKINS).map((s) => ({ id: s.id, name: s.name }))
}
