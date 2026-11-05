/**
 * 皮肤注册表 —— docs/theming.md §3.2
 *
 * 内置皮肤在这里挂进 SKINS;皮肤编辑器(docs/theming.md §7,P3)用 registerSkin
 * 运行时合入 —— 内置与编辑器皮肤同表竞争,走同一条 applySkin 渲染路径。
 *
 * 顶栏菜单呈现的是**五个同级外观选项**:白色(默认)/ 黑色 / 科技蓝 /
 * 政务红 / 文博棕金(SkinSwitcher 里的预设清单)。旧默认皮肤 id 'orange'
 * 在 LEGACY_ALIASES 里静默映射到 white —— 老用户 localStorage 里的
 * skin: "orange" 与后端 branding.skin: orange 都继续可用,不 warn。
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

/** 内置皮肤表(自定义皮肤拷 skins/custom.example.ts 后在这里挂一行) */
const SKINS: Record<string, SkinDefinition> = {
  white,
  black,
  tech,
  gov,
  museum,
}

/** 默认皮肤的直接引用(绕开索引访问的 undefined 推断) */
const defaultSkin: SkinDefinition = white

/** 编辑器/插件运行时注册皮肤;id 冲突时覆盖(后注册者赢) */
export function registerSkin(def: SkinDefinition): void {
  SKINS[def.id] = def
}

/** 取皮肤;未知 id 显式降级到默认(warn 不报错 —— theming.md §6 容错降级) */
export function getSkin(id: string | undefined): SkinDefinition {
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
