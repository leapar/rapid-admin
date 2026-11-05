/**
 * 皮肤注册表 —— docs/theming.md §3.2
 *
 * 内置皮肤在这里挂进 SKINS;皮肤编辑器(docs/theming.md §7,P3)用 registerSkin
 * 运行时合入 —— 内置与编辑器皮肤同表竞争,走同一条 applySkin 渲染路径。
 *
 * ⚠️ 新增品牌色值必须进 skins/,不得直接写进 LangRoot 的 token(AGENTS.md 硬约束 6)。
 */
import type { SkinDefinition } from './types'
import orange from './skins/orange'

export const DEFAULT_SKIN_ID = 'orange'

/** 内置皮肤表(museum / tech / gov 按 theming.md P1 逐步挂入) */
const SKINS: Record<string, SkinDefinition> = {
  orange,
}

/** 默认皮肤的直接引用(绕开索引访问的 undefined 推断) */
const defaultSkin: SkinDefinition = orange

/** 编辑器/插件运行时注册皮肤;id 冲突时覆盖(后注册者赢) */
export function registerSkin(def: SkinDefinition): void {
  SKINS[def.id] = def
}

/** 取皮肤;未知 id 显式降级到默认橙(warn 不报错 —— theming.md §6 容错降级) */
export function getSkin(id: string | undefined): SkinDefinition {
  if (id && SKINS[id]) return SKINS[id]
  if (id && id !== DEFAULT_SKIN_ID) {
    console.warn(`[theme] 未知皮肤 id "${id}",落回默认 "${DEFAULT_SKIN_ID}"`)
  }
  return defaultSkin
}

/** 内置皮肤 id 清单(供未来的皮肤切换器 / 后端 branding.skin 校验用) */
export function listSkinIds(): string[] {
  return Object.keys(SKINS)
}
