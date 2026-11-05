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

/**
 * 中性面/文字梯度 → --app-* 变量的映射(2026-11-05 皮肤沉浸式)。
 * variables.css 的静态明暗两套只是首帧兜底;皮肤一旦应用,这里的取值
 * 全部跟随皮肤 —— 政务红下 --app-text --app-bg-layout 等消费方
 * (图例文字/统计卡底/滚动区)自动带同色温,不再出现"皮肤外"的冷灰。
 */
export function skinCssVars(skin: SkinDefinition): Record<string, string> {
  const { palette, comp } = skin
  return {
    ...skin.cssVars,
    /* 画布 / 表面 */
    '--app-bg-layout': comp.bodyBg,
    '--app-bg-base': palette.colorBgBase,
    '--app-surface-bg': palette.colorBgContainer,
    '--app-surface-bg-strong': comp.siderBg,
    '--app-border': palette.colorBorder,
    '--app-border-secondary': palette.colorBorderSecondary,
    '--app-split': palette.colorSplit,
    /* 文字梯度 */
    '--app-text': palette.colorText,
    '--app-text-secondary': palette.colorTextSecondary,
    '--app-text-tertiary': palette.colorTextTertiary,
    '--app-text-quaternary': palette.colorTextQuaternary,
  }
}

export function applySkinCssVars(skin: SkinDefinition): void {
  const rootStyle = document.documentElement.style
  for (const [name, value] of Object.entries(skinCssVars(skin))) {
    rootStyle.setProperty(name, value)
  }
}
