/**
 * 字段级可见性 · 前端消费那一半 —— M168(2026-11-02)
 *
 * utils/fieldPolicy.ts 是"报告"在前端的落点:
 *   · parseFieldPolicyHeader —— 解析 X-Field-Policy 响应头
 *   · stamp/read —— 把报告盖到数据对象上(Symbol 戳)、读回来
 *
 * 这里锁的是**三条硬性质**,少一条这套机制就白做:
 *   1. **戳对 Object.keys / JSON.stringify 隐身** ——
 *      报告绝不能漏进请求体、日志、DOM。
 *   2. **戳能被 `{...row}` 复制** ——
 *      页面经常把行对象展开成新的初始值(system/user 的 editInitialValues),
 *      不可枚举的话展开一次报告就没了 ⇒ 保护失效。
 *   3. **环形引用不会把递归打死** ——
 *      响应 JSON 理论上无环,但盖戳函数必须自保。
 *
 * # 可逆探针(拆掉实现看测试会不会红)
 *
 *	拆法                                          必须变红
 *	──────────────────────────────────────────  ─────────────────────────────
 *	defineProperty 的 enumerable 改成 false      「戳能被展开复制」
 *	stampTree 去掉 seen/depth 保护               「环形引用不炸」(栈溢出=红)
 *	parse 里未知段改成"猜成 hidden"              「认不出来的段一律丢掉」
 *	read 不做字段过滤(直接信任戳上的数组)        「读回时剔掉非字符串项」
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  FIELD_POLICY_HEADER,
  FIELD_POLICY_SYMBOL,
  currentPagePath,
  getLatestFieldPolicyReport,
  parseFieldPolicyHeader,
  readFieldPolicyReport,
  setLatestFieldPolicyReport,
  stampFieldPolicyReport,
} from '@/utils/fieldPolicy'

describe('parseFieldPolicyHeader', () => {
  it('空值 / 空串 → null(没有字段级可见性参与)', () => {
    expect(parseFieldPolicyHeader(undefined)).toBeNull()
    expect(parseFieldPolicyHeader(null)).toBeNull()
    expect(parseFieldPolicyHeader('')).toBeNull()
  })

  it('hidden=phone → { hidden: ["phone"], masked: [] }', () => {
    expect(parseFieldPolicyHeader('hidden=phone')).toEqual({
      hidden: ['phone'],
      masked: [],
    })
  })

  it('两种都有,分号隔开;逗号分隔多个字段名', () => {
    expect(parseFieldPolicyHeader('hidden=phone,id_card;masked=real_name')).toEqual({
      hidden: ['phone', 'id_card'],
      masked: ['real_name'],
    })
  })

  it('只有 masked', () => {
    expect(parseFieldPolicyHeader('masked=real_name')).toEqual({
      hidden: [],
      masked: ['real_name'],
    })
  })

  it('键名大小写不敏感(头的值不归我们管,别挑大小写)', () => {
    expect(parseFieldPolicyHeader('Hidden=phone;MASKED=name')).toEqual({
      hidden: ['phone'],
      masked: ['name'],
    })
  })

  it('认不出来的段一律丢掉,不猜', () => {
    // "unknown=foo" 不是合法键;猜错 = 把该显示的字段藏了(或反过来)
    expect(parseFieldPolicyHeader('hidden=phone;unknown=foo')).toEqual({
      hidden: ['phone'],
      masked: [],
    })
  })

  it('空段 / 空值段不产生字段名', () => {
    expect(parseFieldPolicyHeader(';;hidden=;masked=')).toBeNull()
    expect(parseFieldPolicyHeader('hidden=')).toBeNull()
  })

  it('裸字段名(没有 =)整段丢掉', () => {
    expect(parseFieldPolicyHeader('phone;masked=name')).toEqual({
      hidden: [],
      masked: ['name'],
    })
  })
})

describe('stamp / read(Symbol 戳)', () => {
  const REPORT = { hidden: ['phone'], masked: ['real_name'] }

  it('盖了戳的对象能读回同一份报告', () => {
    const row = { id: '1', phone: '138****1234' }
    stampFieldPolicyReport(row, REPORT)
    expect(readFieldPolicyReport(row)).toEqual(REPORT)
  })

  it('没盖章的对象读回 null', () => {
    expect(readFieldPolicyReport({ id: '1' })).toBeNull()
    expect(readFieldPolicyReport(null)).toBeNull()
    expect(readFieldPolicyReport(undefined)).toBeNull()
    expect(readFieldPolicyReport('string')).toBeNull()
  })

  it('⚠️ 戳对 Object.keys / Object.entries / JSON.stringify / for-in 隐身', () => {
    const row = { id: '1', phone: '138****1234' }
    stampFieldPolicyReport(row, REPORT)

    expect(Object.keys(row)).toEqual(['id', 'phone'])
    expect(Object.entries(row)).toEqual([
      ['id', '1'],
      ['phone', '138****1234'],
    ])
    expect(JSON.parse(JSON.stringify(row))).toEqual({ id: '1', phone: '138****1234' })
    const seen: string[] = []
    for (const k in row) seen.push(k)
    expect(seen).toEqual(['id', 'phone'])
  })

  it('⚠️ 戳能被 {...row} 展开复制(enumerable Symbol)', () => {
    const row = { id: '1', phone: '138****1234' }
    stampFieldPolicyReport(row, REPORT)

    const spread = { ...row }
    // 展开复制的是同一个 Symbol 键 —— read 从 spread 上也能读回
    expect(readFieldPolicyReport(spread)).toEqual(REPORT)
    // 而且 Symbol 键还是同一个(Symbol.for 全局注册表)
    expect((spread as Record<symbol, unknown>)[FIELD_POLICY_SYMBOL]).toBe(REPORT)
  })

  it('递归:信封、列表、每一行都被盖上', () => {
    const envelope = {
      list: [{ id: '1', phone: 'a' }, { id: '2', phone: 'b' }],
      total: 2,
      page: 1,
      size: 20,
    }
    stampFieldPolicyReport(envelope, REPORT)
    expect(readFieldPolicyReport(envelope)).toEqual(REPORT)
    for (const row of envelope.list) {
      expect(readFieldPolicyReport(row)).toEqual(REPORT)
    }
  })

  it('嵌套对象也被盖上(报的是字段名,树里每一层都算数)', () => {
    const data = { owner: { phone: 'x' }, items: [{ nested: { real_name: 'y' } }] }
    stampFieldPolicyReport(data, REPORT)
    expect(readFieldPolicyReport(data.owner)).toEqual(REPORT)
    expect(readFieldPolicyReport(data.items[0]!.nested)).toEqual(REPORT)
  })

  it('空报告不盖章(零开销快路径)', () => {
    const row = { id: '1' }
    stampFieldPolicyReport(row, { hidden: [], masked: [] })
    expect(readFieldPolicyReport(row)).toBeNull()
  })

  it('环形引用不炸(深度上限 + seen 集合)', () => {
    const a: Record<string, unknown> = { id: '1' }
    const b: Record<string, unknown> = { id: '2' }
    a.next = b
    b.next = a

    expect(() => stampFieldPolicyReport(a, REPORT)).not.toThrow()
    expect(readFieldPolicyReport(a)).toEqual(REPORT)
  })

  it('冻结对象盖不上戳也不炸(退化为"没有保护",不是请求失败)', () => {
    const row = Object.freeze({ id: '1' })
    expect(() => stampFieldPolicyReport(row, REPORT)).not.toThrow()
    expect(readFieldPolicyReport(row)).toBeNull()
  })

  it('读回时剔掉非字符串项(戳上的数组不可信 —— 它是数据,不是代码)', () => {
    const row: Record<symbol, unknown> = {}
    ;(row as Record<symbol, unknown>)[FIELD_POLICY_SYMBOL] = {
      hidden: ['phone', 42, null, ''],
      masked: ['name', {}],
    }
    expect(readFieldPolicyReport(row)).toEqual({
      hidden: ['phone'],
      masked: ['name'],
    })
  })
})

describe('页面归档(戳丢失时的兜底)', () => {
  beforeEach(() => {
    // 每条用例前清掉归档 —— latestReport 是模块级单例,用例之间不能串
    vi.resetModules()
    // ⚠️ resetModules 之后 import 的引用还是旧的模块实例 ——
    //   所以这里用"归档一份不匹配路径的报告"来覆盖,而不是指望清空。
    setLatestFieldPolicyReport('/__no_such_page__', { hidden: [], masked: [] })
  })

  it('归档 + 同路径 → 读回', () => {
    const path = currentPagePath()
    setLatestFieldPolicyReport(path, { hidden: ['phone'], masked: [] })
    expect(getLatestFieldPolicyReport()).toEqual({ hidden: ['phone'], masked: [] })
  })

  it('路径对不上 → null(宁可没有保护,不拿别的页面的规则禁自己的字段)', () => {
    setLatestFieldPolicyReport('/other-page', { hidden: ['phone'], masked: [] })
    expect(getLatestFieldPolicyReport('/this-page')).toBeNull()
  })

  it('没归档过 → null', () => {
    expect(getLatestFieldPolicyReport('/never-archived')).toBeNull()
  })

  it('显式传路径优先于当前页面路径', () => {
    setLatestFieldPolicyReport('/explicit', { hidden: ['x'], masked: [] })
    expect(getLatestFieldPolicyReport('/explicit')).toEqual({ hidden: ['x'], masked: [] })
  })
})

describe('头名契约', () => {
  it('与后端 handler.FieldPolicyHeader 逐字相同(两边各写一份就一定会漂)', () => {
    // 后端的真源在 server/internal/api/handler/field_policy.go;
    // 那边有 cors_config_test.go 锁两头一致,这里锁前端这一侧的字面量。
    expect(FIELD_POLICY_HEADER).toBe('X-Field-Policy')
  })
})
