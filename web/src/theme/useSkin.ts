/**
 * 订阅式取当前皮肤 —— 全站**唯一**的皮肤读取入口(LangRoot / Header 等)
 *
 * store 里的 skin 只是 id:`registerSkin` 用**同一个 id** 替换对象(皮肤
 * 编辑器试穿的热更新路径)不会触发订阅 store 的组件重渲染 —— 症状是
 * "试穿那一刻生效、之后编辑全都不动"(LangRoot 已修过一次,Header 又漏,
 * 2026-11-05 收口成 hook,别再自己写 getSkin(useUIStore(...)))。
 *
 * 这里补上 registry 变更订阅:id 变了走 store,对象换了走广播,两条都覆盖。
 */
import React from 'react'
import { useUIStore } from '@/stores/ui'
import { getSkin, onRegistryChange } from './registry'
import type { SkinDefinition } from './types'

export function useSkin(): SkinDefinition {
  const id = useUIStore((s) => s.skin)
  const [, bumpRegistry] = React.useReducer((n: number) => n + 1, 0)
  React.useEffect(() => onRegistryChange(bumpRegistry), [])
  return getSkin(id)
}
