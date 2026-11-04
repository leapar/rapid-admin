/**
 * 写死的中文不得与字典里已有文案重复(2026-10-25)
 *
 * **这条守的是 2026-10-25 用户报的那句:「全部、菜单、按钮、启用、刷新,
 * 这几个也还有未 i8n 的。你可以用正则搜索一遍写死的汉字,看看还有哪些。」**
 *
 * ## 为什么值得单独一条测试
 *
 *   `t()` 查不到 key 时**原样返回 key**(永不空白、永不报错),
 *   所以"漏接 i18n"在运行时是**完全静默**的 —— 英文用户看到的是一句中文,
 *   没有任何日志、没有异常、CI 全绿。唯一的兜底就是**在测试里扫源码**。
 *
 * ## 这条网守的到底是哪一类
 *
 *   判据不是"有没有写死中文",而是:
 *   **写死的中文,恰好等于字典里已有的文案** ⇒ 违规。
 *
 *   理由:字典里有的文案,英文侧一定已经翻好了(键对齐由 locale-parity 管)。
 *   代码里还写着一份中文,意味着**英文用户永远看不到那句英文** ——
 *   而且是"翻译已经做完了、只是没接线"这种纯浪费。
 *
 *   别的守门网(page-header / placeholder / table-column)守的是**具体位置**;
 *   本条守的是**全仓任意位置** —— 因为"写死的中文"这件事本身没有位置之分。
 *
 * ## 为什么这个判据会越用越强
 *
 *   以后每往字典里加一个键,任何还没接线的同款写死中文就自动变成违规。
 *   守门网不用跟着改,自己就长出了新的牙齿 —— 下一轮(长句文案)搬进字典时,
 *   漏掉的那几处会自己叫出来。
 *
 * ## ⚠️ 兜底参数不算违规
 *
 *   `t('common.status', '状态')` —— 第 2 个实参是**兜底文案**,
 *   只有字典里真的查不到 key 时才生效。"查不到 key"是另一条断言的事
 *   (键必须存在),"写了兜底"本身不是 bug。所以这里排除它。
 *
 * ## ⚠️ 但"兜底参数里写着中文"是另一回事(2026-10-26 加)
 *
 *   上面那条排除的是**重复判据**:兜底文案跟字典里某条撞了,不算重复。
 *   但兜底参数里**本来就写着中文**这件事本身仍然是缺陷:
 *
 *     · 字典是静态 import,永远完整 —— 兜底**永远不会**生效,它就是死代码;
 *     · 死代码里的中文会骗人:读代码的人以为"这里的中文是文案",
 *       实际它根本不会显示;
 *     · 键一旦被改名(而调用点漏改),兜底就会**真的生效** ——
 *       英文用户看到一句中文,而且是静默的。
 *
 *   所以另立一条:**兜底参数里不得有写死的中文**。要占位符就写
 *   `t('key', undefined, { n })`,别把中文塞进兜底位。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'
import {
  dictValues,
  findCjkLiterals,
  findDictDuplicates,
  findTCalls,
  placeholders,
} from '@/test/i18nLiteralScan'
import { listSourceFiles } from '@/test/sourceScan'

/** 源码根 = `web/src`(本文件在 `src/locales/__tests__/` 下) */
const SRC_ROOT = join(__dirname, '..', '..')

/** 字典文件本身要排除 —— 它**就是**"字典" */
const DICT_FILES = new Set(['locales/zh-CN.ts', 'locales/en-US.ts'])

/** 参与扫描的产品源码(.ts + .tsx) */
function productFiles(): string[] {
  return listSourceFiles(SRC_ROOT).filter((f) => {
    const rel = f.slice(SRC_ROOT.length + 1).split('\\').join('/')
    return !DICT_FILES.has(rel)
  })
}

/** 字典里所有字符串叶子值 */
const ZH_VALUES = dictValues(zhCN)

/** 把嵌套字典摊平成 "点分路径 → 值" */
function flatten(obj: unknown, prefix = ''): Map<string, string> {
  const out = new Map<string, string>()
  if (obj == null || typeof obj !== 'object') return out
  for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
    const p = prefix ? `${prefix}.${k}` : k
    if (v != null && typeof v === 'object' && !Array.isArray(v)) {
      for (const [pk, pv] of flatten(v, p)) out.set(pk, pv)
    } else if (typeof v === 'string') {
      out.set(p, v)
    }
  }
  return out
}

const ZH = flatten(zhCN)
const EN = flatten(enUS)

describe('写死中文 i18n 守门网', () => {
  it('扫描器本身没失效(合成样本里的 4 处必须都被扫到)', () => {
    // ⚠️ 这一条是**给守门网自己上的**:
    //   下面那条断言"扫到的东西是干净的",如果扫描器某天坏了
    //   (比如 AST 遍历写错、正则写坏),它会**一个都扫不到**,
    //   于是下面那条全绿 —— 假绿比没有网更危险。
    //   所以必须有一条独立断言:喂一段已知样本,必须精确扫出已知结果。
    //
    //   样本里刻意放了 7 类东西:
    //     ① JSX 文本里的中文            → 要扫到
    //     ② JSX 属性里的中文            → 要扫到
    //     ③ 对象字段值里的中文          → 要扫到
    //     ④ 模板字符串片段里的中文      → 要扫到
    //     ⑤ t() 的兜底参数(第 2 个实参) → **不许**扫到
    //     ⑥ tKey() 的**模板**兜底参数    → **不许**扫到
    //        (2026-10-25 实测踩到的洞:模板片段的父节点是 TemplateExpression,
    //         不是 CallExpression,老实现把它当"写死的中文",把
    //         useTable.ts 的分页文案误报成违规)
    //     ⑦ 注释里的中文 / 纯 ASCII 串  → **不许**扫到
    const sample = [
      "const A = () => (",
      "  <div title={'标题'}>文本</div>",
      ")",
      "const obj = { label: '字段' }",
      "const s = `前缀 ${x} 后缀`",
      "const k = t('common.status', '兜底中文')",
      "const k2 = tKey('common.total', `共 ${n} 条`)",
      "// 注释里的中文不算",
      "const ascii = 'plain'",
    ].join('\n')

    const found = findCjkLiterals(sample, 'sample.tsx')
    const texts = found.map((f) => f.text).sort()

    expect(texts).toEqual(
      ['兜底中文', '共 ', ' 条', '前缀 ', '字段', '标题', '文本', ' 后缀'].sort(),
    )
    // 兜底参数必须被标出来(不是"没扫到",是"扫到了但不算违规")
    const fb = found.filter((f) => f.isFallback)
    expect(fb.map((f) => f.text).sort()).toEqual(['共 ', ' 条', '兜底中文'].sort())
    // 其余 5 个节点(文本/属性/字段/模板片段 2 个)都必须是"非兜底"
    expect(found.filter((f) => !f.isFallback).length).toBe(5)
  })

  it('守门函数对合成违规样本必须报得出来(证明这不是一张空网)', () => {
    const sample = ["const obj = { label: '状态' }"].join('\n')
    const dup = findDictDuplicates(sample, 'sample.tsx', new Set(['状态']))
    expect(dup.map((d) => d.text)).toEqual(['状态'])
  })

  it('t() 的兜底参数(哪怕是模板)不算违规 —— 兜底不是"漏接 i18n"', () => {
    const sample = [
      "const k = t('common.status', '状态')",
      "const k2 = tKey('common.total', `共 ${n} 条`)",
    ].join('\n')
    // 兜底文案即使**正好**在字典里,也不算违规
    const dup = findDictDuplicates(sample, 'sample.tsx', new Set(['状态', '共 ', ' 条']))
    expect(dup.map((d) => d.text)).toEqual([])
  })

  it('写死的中文不得与字典里已有文案重复(英文用户永远看不到那句翻译)', () => {
    const offenders: string[] = []

    for (const file of productFiles()) {
      const rel = file.slice(SRC_ROOT.length + 1).split('\\').join('/')
      const src = readFileSync(file, 'utf8')
      for (const d of findDictDuplicates(src, rel, ZH_VALUES)) {
        offenders.push(`${rel}:${d.line} · [${d.kind}] ${JSON.stringify(d.text)}`)
      }
    }

    expect(offenders).toEqual([])
  })

  it('t()/tKey() 的兜底参数里不得有写死的中文(死代码 + 键一改名就漏中文)', () => {
    // 先证明这条网不是空的:喂一段已知样本,必须精确扫出那 2 处兜底
    const sample = [
      "const a = t('common.status', '状态')",
      "const b = tKey('common.total', `共 ${n} 条`)",
      "const c = t('common.ok', undefined, { n })",
      "const d = t('common.ok')",
    ].join('\n')
    const fb = findCjkLiterals(sample, 'sample.tsx').filter((l) => l.isFallback)
    expect(fb.map((l) => l.text).sort()).toEqual(['共 ', ' 条', '状态'].sort())

    // 正式扫产品源码
    const offenders: string[] = []
    for (const file of productFiles()) {
      const rel = file.slice(SRC_ROOT.length + 1).split('\\').join('/')
      const src = readFileSync(file, 'utf8')
      for (const l of findCjkLiterals(src, rel)) {
        if (l.isFallback) {
          offenders.push(`${rel}:${l.line} · [${l.kind}] ${JSON.stringify(l.text)}`)
        }
      }
    }

    expect(offenders).toEqual([])
  })

  it('源码里引用的 t() 键必须在中英两侧都真实存在(拼错 = 静默显示裸 key)', () => {
    const missing: string[] = []

    for (const file of productFiles()) {
      const rel = file.slice(SRC_ROOT.length + 1).split('\\').join('/')
      const src = readFileSync(file, 'utf8')
      for (const call of findTCalls(src, rel)) {
        // 动态 key(模板/变量)查不了,不是"键不存在"
        if (call.key === null) continue
        if (!ZH.has(call.key)) {
          missing.push(`${rel}:${call.line} · ${call.key} —— zh-CN 字典里没有`)
        }
        if (!EN.has(call.key)) {
          missing.push(`${rel}:${call.line} · ${call.key} —— en-US 字典里没有`)
        }
      }
    }

    expect(missing).toEqual([])
  })

  it('同一个键的中英两侧占位符必须一致(中文能插值、英文插不进去 = 露出 {name})', () => {
    const mismatched: string[] = []

    for (const [key, zhValue] of ZH) {
      const enValue = EN.get(key)
      if (enValue === undefined) continue // 键缺失由 locale-parity 管
      const zhPh = placeholders(zhValue).sort()
      const enPh = placeholders(enValue).sort()
      if (zhPh.join(',') !== enPh.join(',')) {
        mismatched.push(`${key} · zh=${JSON.stringify(zhValue)} en=${JSON.stringify(enValue)}`)
      }
    }

    expect(mismatched).toEqual([])
  })
})
