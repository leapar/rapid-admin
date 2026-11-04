/**
 * 表格列标题必须接 i18n(2026-10-25)
 *
 * **这条守的是用户报的那个 bug:「很多table的标题也没有进行l8n」。**
 *
 * 为什么值得单独一条测试:
 *   - `t()` 查不到 key 时**原样返回 key**(永不空白、永不报错),
 *     所以"漏接 i18n"在运行时是**完全静默**的 —— 英文用户看到的是一句中文,
 *     没有任何日志、没有异常、CI 全绿。
 *   - 键在不在字典里由 locale-parity.test.ts 管(中英两侧对齐),
 *     **接没接线由本条管**;两条合起来才是"既能切语言、也不会缺键"。
 *
 * 判「是不是表格列」不靠数组形态(列数组有 5 种写法,加一种漏一种),
 * 而靠**所在对象字面量里有没有列的同伴属性** —— 见 @/test/columnScan。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'
import { findColumnTitles } from '@/test/columnScan'
import { listTsx } from '@/test/sourceScan'

const SRC_ROOT = join(__dirname, '..', '..')

/** 按点分路径取值,取不到返 undefined */
function pick(dict: any, path: string): unknown {
  return path.split('.').reduce((acc, k) => (acc == null ? acc : acc[k]), dict)
}

describe('表格列标题 i18n', () => {
  it('扫描器本身没失效(非空断言 —— 扫描器漏扫时下面几条会假绿)', () => {
    // ⚠️ 这一条是**给守门网自己上的**:
    //   上面三条断言的都是"扫到的东西对不对",
    //   如果扫描器某天坏了(比如列的判据写错、正则写坏),
    //   它会**一个都扫不到**,于是上面三条全绿 —— 假绿比没有网更危险。
    //   所以必须有一条独立断言:扫到的列标题数量不能是 0。
    let total = 0
    for (const file of listTsx(SRC_ROOT)) {
      total += findColumnTitles(readFileSync(file, 'utf8')).length
    }
    expect(total).toBeGreaterThan(100)
  })

  it('表格列标题不得写死字符串字面量(必须走 t())', () => {
    const offenders: string[] = []

    for (const file of listTsx(SRC_ROOT)) {
      const rel = file.slice(SRC_ROOT.length + 1).replace(/\\/g, '/')
      const src = readFileSync(file, 'utf8')
      for (const col of findColumnTitles(src)) {
        if (col.literal !== null) {
          offenders.push(`${rel}:${col.line} · title: ${col.expr}`)
        }
      }
    }

    expect(offenders).toEqual([])
  })

  it('列标题引用的键必须在中英两侧都真实存在(拼错 = 静默显示裸 key)', () => {
    const missing: string[] = []

    for (const file of listTsx(SRC_ROOT)) {
      const rel = file.slice(SRC_ROOT.length + 1).replace(/\\/g, '/')
      const src = readFileSync(file, 'utf8')
      for (const col of findColumnTitles(src)) {
        if (!col.key) continue
        if (pick(zhCN, col.key) === undefined) {
          missing.push(`${rel}:${col.line} · ${col.key} —— zh-CN 字典里没有`)
        }
        if (pick(enUS, col.key) === undefined) {
          missing.push(`${rel}:${col.line} · ${col.key} —— en-US 字典里没有`)
        }
      }
    }

    expect(missing).toEqual([])
  })

  it('field 组两侧条目数一致', () => {
    expect(Object.keys(zhCN.field).length).toBe(Object.keys(enUS.field).length)
  })

  it('列标题用的键确实落在 field.* / common.* 这两个共享组里(别新开一个组)', () => {
    const stray: string[] = []

    for (const file of listTsx(SRC_ROOT)) {
      const rel = file.slice(SRC_ROOT.length + 1).replace(/\\/g, '/')
      const src = readFileSync(file, 'utf8')
      for (const col of findColumnTitles(src)) {
        if (!col.key) continue
        if (!col.key.startsWith('field.') && !col.key.startsWith('common.')) {
          stray.push(`${rel}:${col.line} · ${col.key}`)
        }
      }
    }

    expect(stray).toEqual([])
  })

  it('扫描器不把弹窗配置误判成表格列(嵌套的 { key: … } 不算同伴属性)', () => {
    // 弹窗标题不是列标题。旧扫描器拿整个对象文本去 includes('key:'),
    // 结果 t() 变量里那个**嵌套**的 { key: … } 被当成了列的同伴属性 ——
    // 弹窗标题被误判成"列标题",守门网就报了个假阳性(Module B-4 撞上的)。
    const modalCfg = `
      modal.error({
        title: t('page.job.triggerFailedTitle', undefined, { key: triggering.job_key }),
        content: run.error || 'x',
        width: 600,
      })
    `
    expect(findColumnTitles(modalCfg)).toEqual([])

    // 反向探针:真列还得扫得出来(嵌套的大括号不能把同伴属性一起抹掉)
    const column = `
      const columns = [{
        title: t('field.name'),
        dataIndex: 'name',
        render: (v) => <span>{v}</span>,
      }]
    `
    expect(findColumnTitles(column).length).toBe(1)
  })
})
