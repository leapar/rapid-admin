/**
 * 输入框 / 下拉框的 placeholder 必须接 i18n(2026-10-25)
 *
 * **这条守的是用户报的那个 bug:「搜索框的placeholder没有支持il8n,要全部扫一遍」。**
 *
 * 为什么值得单独一条测试:
 *   - `t()` 查不到 key 时**原样返回 key**(永不空白、永不报错),
 *     所以"漏接 i18n"在运行时是**完全静默**的 —— 英文用户看到的是一句中文,
 *     没有任何日志、没有异常、CI 全绿。
 *   - 键在不在字典里由 locale-parity.test.ts 管(中英两侧对齐),
 *     **接没接线由本条管**;两条合起来才是"既能切语言、也不会缺键"。
 *
 * 扫两种形态:
 *   ① 直接字面量   placeholder="搜索用户名"
 *   ② 表达式内嵌   placeholder={it.value || '当前为空'}
 *      —— 只扫 ① 的话,② 会被漏掉(实测仓库里 3 处是这种写法)。
 *
 * 为什么顺带查"键是否真实存在":
 *   `t('common.placeholder.searchUser')` 里 key 拼错时,t() 照样返回那串
 *   字符 —— 用户看到的是 "common.placeholder.searchUser" 而不是提示语。
 *   这是**另一种静默失败**,类型系统拦不住(t 的 key 是 string)。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'
import {
  CJK,
  cjkLiteralsInExpr,
  findJsxTags,
  listTsx,
  literalValue,
  stripComments,
} from '@/test/sourceScan'

/** 会显示占位提示的属性名 */
const PLACEHOLDER_PROPS = ['placeholder', 'searchPlaceholder']

const SRC_ROOT = join(__dirname, '..', '..')

describe('placeholder i18n', () => {
  it('placeholder / searchPlaceholder 不得写死中文', () => {
    const offenders: string[] = []

    for (const file of listTsx(SRC_ROOT)) {
      const rel = file.slice(SRC_ROOT.length + 1).replace(/\\/g, '/')
      const src = stripComments(readFileSync(file, 'utf8'))
      for (const tag of findJsxTags(src)) {
        for (const prop of tag.props) {
          if (!PLACEHOLDER_PROPS.includes(prop.name)) continue
          // ① 直接字面量
          const lit = literalValue(prop.value)
          if (lit && CJK.test(lit)) {
            offenders.push(`${rel}:${tag.line} · <${tag.name} ${prop.name}="${lit}">`)
            continue
          }
          // ② 表达式里内嵌的中文字面量
          for (const inner of cjkLiteralsInExpr(prop.value)) {
            offenders.push(`${rel}:${tag.line} · <${tag.name} ${prop.name}={…'${inner}'…}>`)
          }
        }
      }
    }

    expect(offenders).toEqual([])
  })

  it('源码里引用的 common.placeholder.* 键必须真实存在(拼错 = 静默显示裸 key)', () => {
    const zhKeys = new Set(Object.keys(zhCN.common.placeholder))
    const enKeys = new Set(Object.keys(enUS.common.placeholder))
    const missing: string[] = []

    for (const file of listTsx(SRC_ROOT)) {
      const rel = file.slice(SRC_ROOT.length + 1).replace(/\\/g, '/')
      const src = readFileSync(file, 'utf8')
      // 只取 common.placeholder.* 形态的 key
      for (const m of src.matchAll(/['"](common\.placeholder\.[A-Za-z0-9_]+)['"]/g)) {
        const key = m[1]!
        const leaf = key.slice('common.placeholder.'.length)
        if (!zhKeys.has(leaf)) missing.push(`${rel} · ${key} —— zh-CN 字典里没有`)
        if (!enKeys.has(leaf)) missing.push(`${rel} · ${key} —— en-US 字典里没有`)
      }
    }

    expect(missing).toEqual([])
  })

  it('common.placeholder 组两侧条目数一致', () => {
    expect(Object.keys(zhCN.common.placeholder).length).toBe(
      Object.keys(enUS.common.placeholder).length,
    )
  })
})
