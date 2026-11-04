/**
 * 菜单图标词表 ↔ 后端 seed 的 Icon 字段(2026-11-01 · 开源通用化 §8 G5)
 *
 * ## 这条网守的是什么
 *
 *   后端 `sys_resource.icon` 存的是**字符串 key**(如 'system' / 'job'),
 *   前端拿 key 再换算成图标(`resolveResourceIcon`)。**换算不到就返回 null** ——
 *   侧边栏据此渲染成"没有图标的裸文字",**不报错、不报警、CI 全绿**。
 *
 *   也就是说:后端 seed 里新增一个 Icon,前端忘了登记 ⇒
 *   **菜单那一项安静地没了图标**,没有任何一层会告诉你。
 *
 *   这跟"菜单 path 写错 → 点进去 404"是同一类问题(约定靠人记着),
 *   区别是 path 那条有 `menu-route-parity.test.ts` 守着,图标这条**一直没人守**。
 *
 * ## 方向只能是「后端 seed ⇒ 前端词表有」
 *
 *   前端词表里多出来的 key(比如 'security' / 'team')是**下拉选项的富余**,
 *   资源编辑弹窗就是从这份词表里出选项的 —— 那边多几个是好事,
 *   反过来断言会误报。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 从 seed.go 的 menuList 里删掉一行 Icon → 本文件立刻变红,
 *     报错会指名道姓说是哪个 Icon 没登记。
 *   · 从 RESOURCE_ICON_MAP 里删掉一个 key → 同上。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { RESOURCE_ICON_MAP } from '@/constants/resourceIcons'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

/** 源码根 = `web/src`(本文件在 `src/constants/__tests__/` 下) */
const SRC_ROOT = join(__dirname, '..', '..')

/** 仓库根 = `web/src` 的上两级 */
const REPO_ROOT = join(SRC_ROOT, '..', '..')

/** 后端菜单 seed */
const SEED_GO = join(REPO_ROOT, 'server', 'internal', 'seed', 'seed.go')

/**
 * 取出后端 `menuList()` 里所有菜单的 Icon 值
 *
 * ⚠️ **必须限定在 menuList() 函数体内** —— seed.go 里还有 `MenuDef` 结构体的
 *   `Icon string` 字段(没有引号)和 `Icon: m.Icon` 这样的字段拷贝。
 *   正则要求 `Icon: "..."`(带引号)已经能滤掉它们,但限定函数体更稳。
 */
function parseMenuIcons(go: string): string[] {
  const start = go.indexOf('func menuList()')
  if (start < 0) throw new Error('seed.go 里找不到 menuList() —— 解析器失效,这张网成了空网')
  const end = go.indexOf('\n}', start)
  const body = go.slice(start, end < 0 ? undefined : end)
  const out: string[] = []
  const re = /Icon:\s*"([^"]*)"/g
  let m: RegExpExecArray | null
  while ((m = re.exec(body)) !== null) out.push(m[1]!)
  return out
}

const SEED_ICONS = parseMenuIcons(readFileSync(SEED_GO, 'utf8'))

describe('菜单图标词表 ↔ 后端 seed', () => {
  it('解析器没失效(这张网不是空的)', () => {
    expect(SEED_ICONS.length).toBeGreaterThan(20)
    expect(SEED_ICONS).toContain('dashboard')
    expect(SEED_ICONS).toContain('file')
  })

  it('seed 里用到的每个 Icon 都必须在前端词表里登记', () => {
    const missing = SEED_ICONS.filter((k) => !(k in RESOURCE_ICON_MAP))
    expect(
      missing,
      `后端 seed 的这些菜单 Icon 没在前端词表里登记 —— 侧边栏上它们会渲染成没有图标的裸文字(不报错)。\n` +
        `登记位置:web/src/constants/resourceIcons.ts 的 RESOURCE_ICON_MAP`,
    ).toEqual([])
  })

  it('词表里每个 key 都能换算出图标组件', () => {
    for (const [key, Comp] of Object.entries(RESOURCE_ICON_MAP)) {
      expect(Comp, `词表里的 "${key}" 没有对应组件`).toBeTruthy()
    }
  })

  // ⚠️ 2026-11-01 补的第二条方向:**词表 ⇒ i18n 字典有**
  //
  //   资源编辑弹窗的「图标」下拉框的选项来自
  //   `RESOURCE_ICON_KEYS.map(key => ({ value: key, label: t('resourceIcon.'+key) }))`
  //   (web/src/pages/system/permission/index.tsx:208)——
  //   也就是说**词表里的每个 key 都要有一份中英文标签**。
  //
  //   少登记的症状:下拉框里那一项显示成裸 key("api"),不报错、不空白、
  //   英文环境下还显示成英文 key —— 和"菜单图标没了"同一类静默失败。
  //   (2026-11-01 实际发生过:api 图标进了词表,但 zh/en 两边的
  //   resourceIcon 字典都忘了登记。)
  it('词表里每个 key 在中英文字典里都要有标签', () => {
    const missingZh = Object.keys(RESOURCE_ICON_MAP).filter(
      (k) => !zhCN.resourceIcon[k as keyof typeof zhCN.resourceIcon],
    )
    const missingEn = Object.keys(RESOURCE_ICON_MAP).filter(
      (k) => !enUS.resourceIcon[k as keyof typeof enUS.resourceIcon],
    )
    expect(
      missingZh,
      `这些图标 key 在 zh-CN 的 resourceIcon 字典里没有标签 —— 资源编辑弹窗的下拉里会露出裸 key。\n` +
        `补的位置:web/src/locales/zh-CN.ts 的 resourceIcon 段`,
    ).toEqual([])
    expect(
      missingEn,
      `这些图标 key 在 en-US 的 resourceIcon 字典里没有标签 —— 英文环境的下拉里会露出裸 key。\n` +
        `补的位置:web/src/locales/en-US.ts 的 resourceIcon 段`,
    ).toEqual([])
  })
})
