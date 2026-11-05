/**
 * 皮肤应用 —— docs/theming.md §3.2
 *
 * applySkinCssVars:把皮肤里的 --app-* 变量内联注入 <html style>,
 * 覆盖 variables.css 的同名 fallback —— CSS 变量的**单一来源是皮肤对象**,
 * variables.css 只留首帧(SSR/水合前)兜底,不再承载第二份真值。
 *
 * 注入只走 setProperty(值全部来自皮肤对象的字符串),永不碰 innerHTML ——
 * 为 P3 皮肤编辑器的运行时生成对象留同一条安全通道(theming.md §7.3)。
 */
import type { SkinDefinition } from './types'

export function applySkinCssVars(skin: SkinDefinition): void {
  const rootStyle = document.documentElement.style
  for (const [name, value] of Object.entries(skin.cssVars)) {
    rootStyle.setProperty(name, value)
  }
}
