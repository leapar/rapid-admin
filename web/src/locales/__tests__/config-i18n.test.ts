/**
 * 系统配置页的 i18n 词条必须跟后端 seed 的配置项清单一一对应(2026-10-29)
 *
 * ## 这条网守的是什么
 *
 *   用户报的是:**「系统配置界面:当前值、加速域名、服务提供方 这些没有 i8n。」**
 *
 *   查下来是两个来源:
 *
 *   1. 「当前值」—— 表单 label 走字典 `config_ui_label`,字典里存的是
 *      **运营可改的中文**("当前值"),英文环境下当然还是中文;
 *   2. 「CDN 加速域名」「OSS 服务提供方」—— 卡片标题直接渲染 DB 里的
 *      `sys_config.config_name`,那也是中文。
 *
 *   ⇒ 修法:**i18n 优先,DB / 字典兜底**。于是多了一份"配置项清单 ↔ 词条"的映射,
 *   而这份映射**必须有网** —— `t()` 查不到 key 时原样返回 key(永不空白、
 *   不报错),漏一条 ⇒ 英文用户看到的是一串 "page.config.item.xxx"(或者,
 *   如果同时改了兜底逻辑,就是一句中文)。两种都是**静默**的。
 *
 * ## 为什么从**后端源码**里解析,而不是在前端抄一份清单
 *
 *   抄一份 = 两份清单迟早漂移,而漂移的方向永远是"前端那份少了新配置项"。
 *   直接读 `seed.go`,后端加一个配置项而前端忘了加词条 → **本条测试当场变红**,
 *   报错还会指名道姓说是哪个 config_key。
 *
 *   (跟 biz-code-i18n.test.ts 一个套路:后端是唯一真源,前端只管接上。)
 *
 * ## 三条断言各自的职责
 *
 *   · zh-CN 的值必须**等于** seed 里的中文名 —— 中文名只存一份(在 seed 里),
 *     词条这边是镜像。运营改了名字 → 测试变红 → 同一个 commit 里改词条。
 *   · en-US 必须有值 —— 缺一条就是英文环境回落到中文。
 *   · 扫描器本身没失效 —— 解析到的条目数必须对得上(见下)。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 从 zh-CN.ts 里删掉任意一条 `page.config.item.*` → 对应断言变红
 *   · 改掉词条派生规则(`locales/configKeys.ts` 里 config_key 的 '.' 不换成 '_')
 *     → 全部变红(**这条网 import 的就是页面上真在用的那个函数**,不是抄一份)
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'
import { configItemI18nKey, configDictI18nKey } from '@/locales/configKeys'

/** 源码根 = `web/src`(本文件在 `src/locales/__tests__/` 下) */
const SRC_ROOT = join(__dirname, '..', '..')

/** 仓库根 = `web/src` 的上两级 */
const REPO_ROOT = join(SRC_ROOT, '..', '..')

/** 后端配置项 + 字典的唯一出处 */
const SEED_GO = join(REPO_ROOT, 'server', 'internal', 'seed', 'seed.go')

/** 按点分路径取值(跟 locales/useI18n.ts 的 getByPath 一个口径) */
function getByPath(obj: unknown, path: string): unknown {
  let acc: unknown = obj
  for (const k of path.split('.')) {
    if (acc === null || acc === undefined) return undefined
    acc = (acc as Record<string, unknown>)[k]
  }
  return acc
}

/** 词条缺失判定(t() 查不到时返回 undefined) */
function isMissing(v: unknown): boolean {
  return v === null || v === undefined
}

/** 从 seed.go 的 mustSysConfigs 里解析配置项清单 */
function parseSeedConfigs(): { key: string; name: string }[] {
  const src = readFileSync(SEED_GO, 'utf-8')
  const out: { key: string; name: string }[] = []
  // {Group: "aliyun.sms", Key: "aliyun.sms.provider", Name: "短信服务提供方", ...}
  const re = /\{Group:\s*"[^"]+",\s*Key:\s*"([^"]+)",\s*Name:\s*"([^"]+)"/g
  let m: RegExpExecArray | null
  while ((m = re.exec(src)) !== null) {
    out.push({ key: m[1]!, name: m[2]! })
  }
  return out
}

/**
 * seed.go 里 `{Group:` 出现的**原始次数** —— 用来当"解析器没坏"的对照。
 *
 * ⚠️ **不要写死条数**(以前写的是 17)。写死的那个数会在每次增删配置项时
 * 变成一颗哑弹:要么忘了改 → 测试红(但红的原因跟"词条齐不齐"无关,
 * 排查成本高),要么顺手改成新数 → 那条网就永久失去了意义。
 *
 * 现在改成**自洽对照**:解析出来的条数必须等于源码里 `{Group:` 的出现次数。
 * 解析器写坏了(解析出 0 条 / 多解析出别的行)→ 立刻红;配置项增删 →
 * 两边一起变,不用改测试。
 */
function countSeedConfigEntries(): number {
  const src = readFileSync(SEED_GO, 'utf-8')
  return (src.match(/\{Group:\s*"/g) ?? []).length
}

/** 从 seed.go 的字典项里解析 config_ui_label 这一组 */
function parseSeedDictLabels(groupCode: string): { itemKey: string; label: string }[] {
  const src = readFileSync(SEED_GO, 'utf-8')
  const out: { itemKey: string; label: string }[] = []
  // {"config_ui_label", "config.value", "config.value", "当前值", 1, "color:default"}
  const re = new RegExp(
    `\\{"${groupCode}",\\s*"([^"]+)",\\s*"[^"]*",\\s*"([^"]*)"`,
    'g',
  )
  let m: RegExpExecArray | null
  while ((m = re.exec(src)) !== null) {
    out.push({ itemKey: m[1]!, label: m[2]! })
  }
  return out
}

const SEED_CONFIGS = parseSeedConfigs()
const SEED_DICT_LABELS = parseSeedDictLabels('config_ui_label')

describe('系统配置页 · i18n 词条齐全性', () => {
  it('扫描器本身没失效:seed 里的配置项解析出来了', () => {
    // ⚠️ 这一条是**给守门网自己上的**:
    //   下面所有断言都建立在"解析到了正确的清单"之上,
    //   正则哪天写坏了 → 解析出 0 条 → 下面的循环一次都不跑 → 全绿。
    //   假绿比没有网更危险,所以必须有一条独立断言。
    //
    //   判据是**跟源码里的原始条数对照**,不是写死一个数字(见
    //   countSeedConfigEntries 的注释)。
    expect(SEED_CONFIGS.length).toBe(countSeedConfigEntries())
    expect(SEED_CONFIGS.map((c) => c.key)).toContain('aliyun.oss.cdn_domain')
    expect(SEED_CONFIGS.find((c) => c.key === 'aliyun.oss.cdn_domain')!.name).toBe('CDN 加速域名')
  })

  it('每个配置项都有 zh-CN 词条,且与 seed 里的中文名一致', () => {
    const missing: string[] = []
    const drifted: string[] = []
    for (const c of SEED_CONFIGS) {
      const v = getByPath(zhCN, configItemI18nKey(c.key))
      if (isMissing(v)) {
        missing.push(c.key)
        continue
      }
      if (v !== c.name) drifted.push(`${c.key}: 词条="${v}" seed="${c.name}"`)
    }
    expect(missing).toEqual([])
    expect(drifted).toEqual([])
  })

  it('每个配置项都有 en-US 词条', () => {
    const missing: string[] = []
    const blank: string[] = []
    for (const c of SEED_CONFIGS) {
      const v = getByPath(enUS, configItemI18nKey(c.key))
      if (isMissing(v)) {
        missing.push(c.key)
        continue
      }
      if (typeof v !== 'string' || v.trim() === '') blank.push(c.key)
    }
    expect(missing).toEqual([])
    expect(blank).toEqual([])
  })

  it('字典驱动的表单 label 也有词条(当前值)', () => {
    // 字典组解析器也要自己验一下(否则下面那条是"0 条 == 0 条"的白绿)
    expect(SEED_DICT_LABELS.length).toBeGreaterThanOrEqual(1)

    const missing: string[] = []
    for (const d of SEED_DICT_LABELS) {
      const key = configDictI18nKey(d.itemKey)
      if (isMissing(getByPath(zhCN, key))) missing.push(`zh:${d.itemKey}`)
      if (isMissing(getByPath(enUS, key))) missing.push(`en:${d.itemKey}`)
    }
    expect(missing).toEqual([])
    expect(getByPath(zhCN, 'page.config.dict.config_value')).toBe('当前值')
  })
})
