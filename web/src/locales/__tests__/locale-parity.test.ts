/**
 * zh-CN / en-US 字典的键必须一一对应(2026-10-25)
 *
 * 这条守的是"页头文案写死中文,英文环境不跟着切"那类问题的**字典侧**:
 * `t()` 查不到就原样返回 key(永不空白),所以少一个 key **不会报错** ——
 * 只会让英文用户看到 "page.xxx.description" 这种裸 key。静默失败。
 * 另一个方向同样要守:某个 key 只在 zh 里有,英文环境就退化成显示裸 key。
 *
 * 背景:2026-10-25 发现 12 个页面的 PageHeader description 是写死的中文
 * (字典里其实早有对应的 page.*.description,只是没接上),搬进字典时
 * 顺手加这条,以后两边漏一条立刻红。
 */
import { describe, it, expect } from 'vitest'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

/** 把嵌套字典摊平成点分路径列表 */
function flatKeys(obj: unknown, prefix = ''): string[] {
  if (obj == null || typeof obj !== 'object') return []
  const out: string[] = []
  for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
    const p = prefix ? `${prefix}.${k}` : k
    if (v != null && typeof v === 'object' && !Array.isArray(v)) out.push(...flatKeys(v, p))
    else out.push(p)
  }
  return out
}

const zh = new Set(flatKeys(zhCN))
const en = new Set(flatKeys(enUS))

describe('locales · zh-CN / en-US 键对齐', () => {
  it('zh-CN 有的键 en-US 也得有(缺一条 = 英文环境显示裸 key)', () => {
    expect([...zh].filter((k) => !en.has(k)).sort()).toEqual([])
  })

  it('en-US 有的键 zh-CN 也得有', () => {
    expect([...en].filter((k) => !zh.has(k)).sort()).toEqual([])
  })

  it('两边的键数量一致', () => {
    expect(zh.size).toBe(en.size)
  })
})
