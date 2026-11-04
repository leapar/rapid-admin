/**
 * SchemaForm 纯逻辑(buildRules / normalizeOptions / isFieldHidden)的全分支单测
 *
 * # 可逆探针(拆掉实现看测试会不会红)
 *
 *	拆法                                             必须变红
 *	───────────────────────────────────────────────  ──────────────────────────
 *	buildRules 的 required 分支直接跳过               「required 生成规则」一族
 *	required 规则去掉 whitespace(input)              「input/textarea 纯空格」
 *	number 的 min/max 派生规则删掉                    「数值范围」一族
 *	extraRules:false 仍生成派生规则                   「extraRules=false 关掉」
 *	调用方 rules 不追加(或追加在最前)               「rules 追加在后」
 *	normalizeOptions 不认 number 简写                 「number 简写」
 *	isFieldHidden 函数形态恒 false                    「函数形态」
 */
import { describe, it, expect } from 'vitest'
import { buildRules, normalizeOptions, isFieldHidden } from '@/components/common/schemaFormRules'
import type { SchemaField } from '@/components/common/schemaFormRules'

const base = (over: Partial<SchemaField>): SchemaField => ({
  name: 'title',
  label: '标题',
  type: 'input',
  ...over,
})

describe('buildRules · required', () => {
  it('required 生成「{label}不能为空」', () => {
    const rules = buildRules(base({ required: true }))
    expect(rules).toHaveLength(1)
    expect(rules[0]).toMatchObject({ required: true, message: '标题不能为空' })
  })

  it('不 required 就没有 required 规则', () => {
    const rules = buildRules(base({}))
    expect(rules).toHaveLength(0)
  })

  it('input/textarea/password 的 required 带 whitespace(纯空格=没填),select 不带', () => {
    for (const type of ['input', 'textarea', 'password'] as const) {
      const rules = buildRules(base({ type, required: true }))
      expect(rules[0]).toMatchObject({ required: true, whitespace: true })
    }
    const rules = buildRules(base({ type: 'select', required: true }))
    expect(rules[0]).not.toHaveProperty('whitespace')
  })

  it('select/number/switch 的 required 不带 whitespace', () => {
    for (const type of ['select', 'number', 'switch'] as const) {
      const rules = buildRules(base({ type, required: true }))
      expect(rules[0]).not.toHaveProperty('whitespace')
    }
  })
})

describe('buildRules · min/max/maxLength 自动派生', () => {
  it('number 带 min+max → 一条数值范围规则', () => {
    const rules = buildRules(base({ type: 'number', min: 1, max: 10 }))
    expect(rules).toHaveLength(1)
    expect(rules[0]).toMatchObject({
      type: 'number',
      min: 1,
      max: 10,
      message: '标题必须在 1 ~ 10 之间',
    })
  })

  it('number 只有 min / 只有 max → 单边文案', () => {
    const [onlyMin] = buildRules(base({ type: 'number', min: 3 }))
    expect(onlyMin).toMatchObject({ type: 'number', min: 3, message: '标题不能小于 3' })

    const [onlyMax] = buildRules(base({ type: 'number', max: 99 }))
    expect(onlyMax).toMatchObject({ type: 'number', max: 99, message: '标题不能大于 99' })
  })

  it('min/max 只对 number 生效,不对 input 生效', () => {
    expect(buildRules(base({ type: 'input', min: 1, max: 10 }))).toHaveLength(0)
  })

  it('文本类 maxLength → string max 规则', () => {
    const [rule] = buildRules(base({ type: 'textarea', maxLength: 100 }))
    expect(rule).toMatchObject({ max: 100, message: '标题不能超过 100 个字符' })
  })

  it('maxLength 只对文本类生效,不对 number 生效', () => {
    expect(buildRules(base({ type: 'number', maxLength: 10 }))).toHaveLength(0)
  })

  it('extraRules:false 关掉自动派生,required 仍在', () => {
    const rules = buildRules(base({ type: 'number', min: 1, max: 10, required: true, extraRules: false }))
    expect(rules).toHaveLength(1)
    expect(rules[0]).toMatchObject({ required: true })
  })
})

describe('buildRules · 调用方 rules 追加', () => {
  it('手动规则追加在自动规则之后', () => {
    const manual = { pattern: /^a/, message: '必须以 a 开头' }
    const rules = buildRules(base({ type: 'number', min: 1, required: true, rules: [manual] }))
    expect(rules).toHaveLength(3)
    expect(rules[0]).toMatchObject({ required: true })
    expect(rules[1]).toMatchObject({ type: 'number', min: 1 })
    expect(rules[2]).toBe(manual)
  })

  it('没有其它规则时手动规则也在(且是唯一规则)', () => {
    const manual = { pattern: /^a/, message: '必须以 a 开头' }
    const rules = buildRules(base({ rules: [manual] }))
    expect(rules).toEqual([manual])
  })
})

describe('normalizeOptions', () => {
  it('string 简写 → label 与 value 同值', () => {
    expect(normalizeOptions(['启用', '禁用'])).toEqual([
      { label: '启用', value: '启用' },
      { label: '禁用', value: '禁用' },
    ])
  })

  it('number 简写 → label 是 String(value),value 保持 number', () => {
    expect(normalizeOptions([1, 2])).toEqual([
      { label: '1', value: 1 },
      { label: '2', value: 2 },
    ])
  })

  it('完整对象原样透传(含 disabled)', () => {
    const opt = { label: '停用', value: 'off', disabled: true }
    expect(normalizeOptions([opt])).toEqual([opt])
  })

  it('混合数组简写 + 完整对象', () => {
    expect(normalizeOptions(['a', { label: 'B', value: 'b' }])).toEqual([
      { label: 'a', value: 'a' },
      { label: 'B', value: 'b' },
    ])
  })

  it('undefined / 空数组 → 空数组', () => {
    expect(normalizeOptions(undefined)).toEqual([])
    expect(normalizeOptions([])).toEqual([])
  })
})

describe('isFieldHidden', () => {
  it('没写 hidden → false', () => {
    expect(isFieldHidden(base({}), undefined)).toBe(false)
  })

  it('boolean 形态原样返回', () => {
    expect(isFieldHidden(base({ hidden: true }), undefined)).toBe(true)
    expect(isFieldHidden(base({ hidden: false }), undefined)).toBe(false)
  })

  it('函数形态:拿当前表单值算;values 缺失按 {} 兜底', () => {
    const field = base({ hidden: (v) => v.mode === 'hide' })
    expect(isFieldHidden(field, { mode: 'hide' })).toBe(true)
    expect(isFieldHidden(field, { mode: 'show' })).toBe(false)
    expect(isFieldHidden(field, undefined)).toBe(false)
  })
})
