/**
 * 通用记录页的纯规则 —— 单测
 *
 * ⚠️ **为什么这些规则值得单测** ——
 *   动态业务表的列与表单全由运行期的 schema 生成,
 *   前端这两个方向各做一次转换(读 → 表单值 / 表单值 → 后端要的形状)。
 *   转换一旦错了,症状是"存进去了但显示不对"或者"报一个看不懂的错",
 *   而且**当场不报错** —— 这种错靠点页面是点不出来的。
 *
 * ⚠️ **断言必须与后端一一对应,不是与"我觉得对"对应** ——
 *   写路径的权威是 server/internal/pkg/dynx/normalize.go
 *   读路径的权威是 server/internal/pkg/dynx/denormalize.go
 *   本文件里每条"为什么"都指向那两个文件的一处具体行为。
 *
 * ⚠️ **本文件不 import 组件** —— recordForm.ts 里的规则都是纯函数,
 *   组件那一层(EditModal / ManagementTable)的交互另有页面级用例覆盖。
 */
import { describe, it, expect } from 'vitest'
import dayjs from 'dayjs'
import type { Rule, RuleObject } from 'antd/es/form'
import {
  editTypeOf,
  toFormValue,
  toWireValue,
  formatCell,
  buildFieldEditFields,
  buildRecordPayload,
  type Translate,
} from '../recordForm'
import type { DynSchemaField } from '@/types/dyn'

/** 假 t():原样返回键 —— 断言时能一眼看出"显示的是哪个键" */
const t: Translate = (key) => key

/**
 * 只取**对象形态**的校验规则
 *
 * ⚠️ antd 的 `Rule` 是联合类型(`RuleObject | RuleRender`)——
 *   `RuleRender` 是个函数,身上没有 `required` / `validator` 字段。
 *   直接 `r.required` 编译不过(TS 说"这个属性不存在"是对的)。
 */
function objectRules(rules: Rule[] | undefined): RuleObject[] {
  return (rules ?? []).filter((r): r is RuleObject => typeof r === 'object')
}

/** 造一个 schema 字段 */
function field(over: Partial<DynSchemaField> = {}): DynSchemaField {
  return {
    field_key: 'title',
    label: '标题',
    type: 'text',
    required: false,
    options: [],
    default_value: '',
    sort: 0,
    ...over,
  }
}

describe('editTypeOf · 字段类型 → 表单控件', () => {
  it('文本类 / 长文本 / JSON 都是文本框', () => {
    expect(editTypeOf('text')).toBe('text')
    expect(editTypeOf('longtext')).toBe('textarea')
    expect(editTypeOf('json')).toBe('textarea')
  })

  it('数字类是数字框,开关是开关,日期是日期选择器', () => {
    expect(editTypeOf('int')).toBe('number')
    expect(editTypeOf('number')).toBe('number')
    expect(editTypeOf('bool')).toBe('switch')
    expect(editTypeOf('date')).toBe('date')
  })

  it('单选 / 多选', () => {
    expect(editTypeOf('select')).toBe('select')
    expect(editTypeOf('multiselect')).toBe('multiSelect')
  })

  /**
   * ⚠️ **附件不是文本框** —— 它要能上传、能显示原文件名、能点开。
   *   具体控件由页面注入(recordForm.ts 是纯 .ts,放不了 JSX)。
   */
  it('附件 → custom(页面注入渲染器)', () => {
    expect(editTypeOf('file')).toBe('custom')
  })

  /**
   * ⚠️ **未知类型必须退化成文本,不能抛** ——
   *   后端加了新类型而前端还不知道时,这一列至少还能显示、还能编辑字符串,
   *   而不是整个页面炸掉。
   */
  it('未知类型退化成单行文本(不抛)', () => {
    expect(editTypeOf('datetime')).toBe('text')
    expect(editTypeOf('geo')).toBe('text')
    expect(editTypeOf('')).toBe('text')
  })
})

describe('toFormValue · 记录值 → 表单初值', () => {
  it('日期变成 dayjs 对象(后端存的是 "yyyy-mm-dd")', () => {
    const v = toFormValue(field({ type: 'date' }), '2026-11-02')
    expect(dayjs.isDayjs(v)).toBe(true)
    expect((v as dayjs.Dayjs).format('YYYY-MM-DD')).toBe('2026-11-02')
  })

  it('日期为空 → undefined(不是空串,否则日期选择器显示 1970)', () => {
    expect(toFormValue(field({ type: 'date' }), '')).toBeUndefined()
    expect(toFormValue(field({ type: 'date' }), null)).toBeUndefined()
  })

  it('多选:数组原样,JSON 数组文本要解析', () => {
    expect(toFormValue(field({ type: 'multiselect' }), ['a', 'b'])).toEqual(['a', 'b'])
    expect(toFormValue(field({ type: 'multiselect' }), '["a","b"]')).toEqual(['a', 'b'])
  })

  /**
   * ⚠️ **默认值的形态是文本** —— 设计器里"默认值"是一个字符串输入框,
   *   多选的默认值只能写成 '["a","b"]' 这种 JSON 数组文本。
   *   不解析的话,新建记录时默认值会被当成一个普通字符串塞进多选框。
   */
  it('多选默认值:单个值也包成数组', () => {
    expect(toFormValue(field({ type: 'multiselect' }), 'a')).toEqual(['a'])
    expect(toFormValue(field({ type: 'multiselect' }), '')).toEqual([])
  })

  it('开关:布尔直传,文本默认值认 true/1/yes/y/on', () => {
    expect(toFormValue(field({ type: 'bool' }), true)).toBe(true)
    expect(toFormValue(field({ type: 'bool' }), 'true')).toBe(true)
    expect(toFormValue(field({ type: 'bool' }), '1')).toBe(true)
    expect(toFormValue(field({ type: 'bool' }), 'yes')).toBe(true)
    expect(toFormValue(field({ type: 'bool' }), 'on')).toBe(true)
    expect(toFormValue(field({ type: 'bool' }), 'false')).toBe(false)
    expect(toFormValue(field({ type: 'bool' }), '0')).toBe(false)
    expect(toFormValue(field({ type: 'bool' }), 1)).toBe(true)
    expect(toFormValue(field({ type: 'bool' }), 0)).toBe(false)
  })

  /**
   * ⚠️ **JSON 字段必须在这里变回字符串** ——
   *   后端读出来的是**已解析的 JSON 值**(dynx.Denormalize 的 denormJSON),
   *   而表单控件是文本框 ⇒ 不转回去的话文本框里会显示 "[object Object]"。
   */
  it('JSON:对象要序列化回文本,文本原样', () => {
    const v = toFormValue(field({ type: 'json' }), { a: 1 })
    expect(typeof v).toBe('string')
    expect(JSON.parse(v as string)).toEqual({ a: 1 })
    expect(toFormValue(field({ type: 'json' }), '{"a":1}')).toBe('{"a":1}')
  })

  it('数字:空 → undefined,文本 → 数字', () => {
    expect(toFormValue(field({ type: 'int' }), '')).toBeUndefined()
    expect(toFormValue(field({ type: 'int' }), null)).toBeUndefined()
    expect(toFormValue(field({ type: 'int' }), '42')).toBe(42)
    expect(toFormValue(field({ type: 'number' }), 1.5)).toBe(1.5)
  })

  it('文本:空 → 空串', () => {
    expect(toFormValue(field({ type: 'text' }), null)).toBe('')
    expect(toFormValue(field({ type: 'text' }), 'abc')).toBe('abc')
  })

  /**
   * ⚠️ **附件的值就是文件 key(一个字符串)** ——
   *   原文件名 / 直链在 `_files` 里,不在这个值里。
   *   所以这里只做"去空白 + 空值归一成空串"。
   */
  it('附件:key 原样带过去(去空白),空 → 空串', () => {
    expect(toFormValue(field({ type: 'file' }), 'rapid-admin/1/dyn/0/a.pdf')).toBe(
      'rapid-admin/1/dyn/0/a.pdf',
    )
    expect(toFormValue(field({ type: 'file' }), '  key  ')).toBe('key')
    expect(toFormValue(field({ type: 'file' }), null)).toBe('')
    expect(toFormValue(field({ type: 'file' }), undefined)).toBe('')
  })
})

describe('toWireValue · 表单值 → 后端载荷', () => {
  /**
   * ⚠️ **空数字必须发 null,不能发空串** ——
   *   后端 dynx.toInt64("") 会报"需要整数,收到 \"\""(400);
   *   发 null 时后端 Normalize(nil) 直接返回 (nil, nil) ⇒ 这一列留空。
   */
  it('空数字 → null(不是空串,后端会拒)', () => {
    expect(toWireValue(field({ type: 'int' }), '')).toBeNull()
    expect(toWireValue(field({ type: 'int' }), null)).toBeNull()
    expect(toWireValue(field({ type: 'int' }), undefined)).toBeNull()
    expect(toWireValue(field({ type: 'number' }), '')).toBeNull()
  })

  it('数字 → number', () => {
    expect(toWireValue(field({ type: 'int' }), 42)).toBe(42)
    expect(toWireValue(field({ type: 'int' }), '42')).toBe(42)
    expect(toWireValue(field({ type: 'number' }), '1.5')).toBe(1.5)
  })

  it('日期 → "yyyy-mm-dd"', () => {
    expect(toWireValue(field({ type: 'date' }), dayjs('2026-11-02'))).toBe('2026-11-02')
    expect(toWireValue(field({ type: 'date' }), '2026-11-02')).toBe('2026-11-02')
  })

  /**
   * ⚠️ **日期时间必须原样发字符串** ——
   *   后端把不带时区的输入按东八区解释、再统一存成 UTC。
   *   前端自己换算的话,浏览器时区 ≠ 东八区时整整差 8 小时,
   *   而且**当场不报错**(存进去了,只是时间不对)。
   */
  it('日期时间原样发字符串(不做时区换算)', () => {
    expect(toWireValue(field({ type: 'datetime' }), '2026-11-01 08:30:00')).toBe(
      '2026-11-01 08:30:00',
    )
    expect(toWireValue(field({ type: 'datetime' }), ' 2026-11-01T00:30:00Z ')).toBe(
      '2026-11-01T00:30:00Z',
    )
  })

  it('多选 → 字符串数组', () => {
    expect(toWireValue(field({ type: 'multiselect' }), ['a', 'b'])).toEqual(['a', 'b'])
    expect(toWireValue(field({ type: 'multiselect' }), null)).toEqual([])
  })

  it('开关 → 布尔', () => {
    expect(toWireValue(field({ type: 'bool' }), true)).toBe(true)
    expect(toWireValue(field({ type: 'bool' }), 'true')).toBe(true)
    expect(toWireValue(field({ type: 'bool' }), '1')).toBe(true)
    expect(toWireValue(field({ type: 'bool' }), 'on')).toBe(true)
    expect(toWireValue(field({ type: 'bool' }), false)).toBe(false)
    expect(toWireValue(field({ type: 'bool' }), '0')).toBe(false)
    expect(toWireValue(field({ type: 'bool' }), undefined)).toBe(false)
  })

  /**
   * ⚠️ **认不出来的字符串必须原样发出去** ——
   *   静默当成 false 的话,用户填"开"存下来是"关",而且全程没有一条错误信息。
   *   后端 dynx.toBool 对认不出来的值是报错的 ⇒ 前端不能替它"通融"。
   */
  it('开关:认不出来的字符串原样发出去(让后端报错,不静默当 false)', () => {
    expect(toWireValue(field({ type: 'bool' }), '开')).toBe('开')
    expect(toWireValue(field({ type: 'bool' }), '开')).not.toBe(false)
  })

  it('JSON → 去掉首尾空白的文本', () => {
    expect(toWireValue(field({ type: 'json' }), ' {"a":1} ')).toBe('{"a":1}')
  })

  it('文本 → 字符串(空值是空串,表示"清空")', () => {
    expect(toWireValue(field({ type: 'text' }), 'abc')).toBe('abc')
    expect(toWireValue(field({ type: 'text' }), null)).toBe('')
  })

  /**
   * ⚠️ **附件发的是 key,不是 URL** ——
   *   后端 dynx.normalizeFile 拒带域名的值(400「文件路径不能带域名」),
   *   也拒 `..` / 反斜杠 / 前导斜杠 ⇒ 前端不能替它"通融"。
   *   这里只做去空白。
   */
  it('附件 → 文件 key(去空白),不是 URL', () => {
    expect(toWireValue(field({ type: 'file' }), ' rapid-admin/1/dyn/0/a.pdf ')).toBe(
      'rapid-admin/1/dyn/0/a.pdf',
    )
    expect(toWireValue(field({ type: 'file' }), null)).toBe('')
  })
})

describe('formatCell · 记录值 → 单元格文本', () => {
  it('空值走统一的空值文案', () => {
    expect(formatCell(field(), '', t)).toBe('common.emptyValue')
    expect(formatCell(field(), null, t)).toBe('common.emptyValue')
    expect(formatCell(field(), undefined, t)).toBe('common.emptyValue')
  })

  it('开关显示是 / 否', () => {
    expect(formatCell(field({ type: 'bool' }), true, t)).toBe('common.yes')
    expect(formatCell(field({ type: 'bool' }), false, t)).toBe('common.no')
  })

  /**
   * ⚠️ **单选显示的是"显示名"不是"值"** ——
   *   存的是 "pending",但人看到的应该是"待处理"。
   */
  it('单选显示选项的显示名', () => {
    const f = field({
      type: 'select',
      options: [
        { value: 'pending', label: '待处理' },
        { value: 'done', label: '已完成' },
      ],
    })
    expect(formatCell(f, 'pending', t)).toBe('待处理')
  })

  it('单选的值不在选项集里 → 退回显示原值(不显示空白)', () => {
    const f = field({ type: 'select', options: [{ value: 'a', label: '甲' }] })
    expect(formatCell(f, 'zzz', t)).toBe('zzz')
  })

  it('多选显示逗号连起来的显示名', () => {
    const f = field({
      type: 'multiselect',
      options: [
        { value: 'a', label: '甲' },
        { value: 'b', label: '乙' },
      ],
    })
    expect(formatCell(f, ['a', 'b'], t)).toBe('甲, 乙')
    expect(formatCell(f, [], t)).toBe('common.emptyValue')
  })

  it('JSON 字段显示序列化后的文本', () => {
    expect(formatCell(field({ type: 'json' }), { a: 1 }, t)).toBe('{"a":1}')
  })

  /**
   * ⚠️ **附件显示的是"原文件名"不是 key** ——
   *   key 是一长串 `rapid-admin/1844/dyn/0/xxx-户型图.pdf`,没人认得。
   *   原文件名由后端在读记录时解析好(`_files`),取不到就退回显示 key。
   */
  it('附件显示原文件名(取不到就退回 key)', () => {
    const f = field({ type: 'file' })
    expect(formatCell(f, 'rapid-admin/1/dyn/0/a.pdf', t, { name: '户型图.pdf', url: '/x' })).toBe(
      '户型图.pdf',
    )
    expect(formatCell(f, 'rapid-admin/1/dyn/0/a.pdf', t)).toBe('rapid-admin/1/dyn/0/a.pdf')
  })

  it('数字显示成文本', () => {
    expect(formatCell(field({ type: 'int' }), 42, t)).toBe('42')
    expect(formatCell(field({ type: 'number' }), 1.5, t)).toBe('1.5')
  })

  /**
   * ⚠️ **后端给的是 UTC 的 ISO-8601,界面要显示本地时间** ——
   *   与全仓其它时间列同一口径(见 web 排版约定)。
   *   断言写成"格式 + 折回同一时刻" ⇒ 与运行机器的时区无关。
   */
  it('日期时间显示成本地时间', () => {
    const raw = '2026-11-01T00:30:00Z'
    const out = formatCell(field({ type: 'datetime' }), raw, t)
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
    // 折回本地时间必须还是同一个时刻(证明它不是原样透传的 UTC 串)
    expect(dayjs(out).valueOf()).toBe(dayjs(raw).valueOf())
  })
})

describe('buildFieldEditFields · schema 字段 → 表单项', () => {
  it('name 用字段标识(不是显示名)', () => {
    const out = buildFieldEditFields([field({ field_key: 'field_key_x', label: '显示名' })], t)
    expect(out[0]?.name).toBe('field_key_x')
    expect(out[0]?.label).toBe('显示名')
  })

  it('必填字段带 required 规则', () => {
    const out = buildFieldEditFields([field({ required: true })], t)
    expect(objectRules(out[0]?.rules).some((r) => r.required === true)).toBe(true)
  })

  it('非必填字段不带 required 规则', () => {
    const out = buildFieldEditFields([field({ required: false })], t)
    expect(objectRules(out[0]?.rules).some((r) => r.required === true)).toBe(false)
  })

  it('单选 / 多选带选项', () => {
    const out = buildFieldEditFields(
      [
        field({ type: 'select', options: [{ value: 'a', label: '甲' }] }),
        field({ field_key: 'm', type: 'multiselect', options: [{ value: 'b', label: '乙' }] }),
      ],
      t,
    )
    expect(out[0]?.options).toEqual([{ value: 'a', label: '甲' }])
    expect(out[1]?.options).toEqual([{ value: 'b', label: '乙' }])
  })

  it('文本字段不带选项', () => {
    const out = buildFieldEditFields([field({ type: 'text' })], t)
    expect(out[0]?.options).toBeUndefined()
  })

  /**
   * ⚠️ **JSON 的合法性在提交前就查** ——
   *   后端也会查(报"不是合法 JSON"),但那时用户已经填完一整张表了。
   */
  it('JSON 字段带合法性校验规则', async () => {
    const out = buildFieldEditFields([field({ type: 'json' })], t)
    const rule = objectRules(out[0]?.rules).find((r) => typeof r.validator === 'function')
    expect(rule).toBeDefined()
    const validator = rule?.validator as unknown as (
      r: unknown,
      v: unknown,
    ) => Promise<void>
    await expect(validator(rule, '{"a":1}')).resolves.toBeUndefined()
    await expect(validator(rule, '{坏掉的')).rejects.toThrow('page.dyn.records.jsonInvalid')
  })

  it('JSON 字段为空时跳过校验', async () => {
    const out = buildFieldEditFields([field({ type: 'json' })], t)
    const rule = objectRules(out[0]?.rules).find((r) => typeof r.validator === 'function')
    const validator = rule?.validator as unknown as (
      r: unknown,
      v: unknown,
    ) => Promise<void>
    await expect(validator(rule, '')).resolves.toBeUndefined()
    await expect(validator(rule, '   ')).resolves.toBeUndefined()
  })

  /**
   * ⚠️ **附件的渲染器由页面注入** ——
   *   recordForm.ts 是纯 .ts(不能放 JSX),而附件控件是个组件。
   */
  it('附件:注入渲染器时用 custom + 带上渲染器', () => {
    const render = () => null
    const out = buildFieldEditFields([field({ type: 'file' })], t, {
      renderFile: () => render,
    })
    expect(out[0]?.type).toBe('custom')
    expect(out[0]?.render).toBe(render)
  })

  /**
   * ⚠️ **没有渲染器必须退回文本框,不能留一个空的 custom** ——
   *   EditModal 的 renderField 里 `case 'custom': return null`:
   *   type='custom' 而没有 render ⇒ 那个字段**整条消失**,
   *   用户看不出"这个字段被吞了"。
   */
  it('附件:没有渲染器时退回文本框(字段不能凭空消失)', () => {
    const out = buildFieldEditFields([field({ type: 'file' })], t)
    expect(out[0]?.type).toBe('text')
    expect(out[0]?.render).toBeUndefined()
  })

  /**
   * ⚠️ **getValueProps 必须给 value** ——
   *   提供了 getValueProps 就等于**替换**了 rc-field-form 的默认实现,
   *   不返回 value 的话控件变成完全不受控(改一次就再也不跟表单同步)。
   */
  it('附件:getValueProps 必须给出值(字符串以外归一成空串)', () => {
    const out = buildFieldEditFields([field({ type: 'file' })], t, {
      renderFile: () => () => null,
    })
    const gvp = out[0]?.getValueProps
    expect(gvp).toBeDefined()
    expect(gvp?.('rapid-admin/1/dyn/0/a.pdf')).toEqual({ value: 'rapid-admin/1/dyn/0/a.pdf' })
    expect(gvp?.(null)).toEqual({ value: '' })
    expect(gvp?.(undefined)).toEqual({ value: '' })
  })
})

describe('buildRecordPayload · 表单值 → 记录载荷', () => {
  it('键是字段标识(后端按字段标识找列)', () => {
    const fields = [field({ field_key: 'field_key_a' }), field({ field_key: 'field_key_b' })]
    const out = buildRecordPayload(fields, { field_key_a: 'x' })
    expect(Object.keys(out).sort()).toEqual(['field_key_a', 'field_key_b'])
  })

  it('值按类型归一化(空数字是 null)', () => {
    const fields = [
      field({ field_key: 'f_cnt', type: 'int' }),
      field({ field_key: 'f_title', type: 'text' }),
    ]
    const out = buildRecordPayload(fields, { f_cnt: '', f_title: '标题' })
    expect(out.f_cnt).toBeNull()
    expect(out.f_title).toBe('标题')
  })

  it('表单里没有的字段也会出现(值按"空"归一化)', () => {
    const out = buildRecordPayload([field({ field_key: 'f_x', type: 'int' })], {})
    expect('f_x' in out).toBe(true)
    expect(out.f_x).toBeNull()
  })
})

describe('relation 字段(M5)', () => {
  /**
   * ⚠️ **候选由后端在 schema 里现查好塞进 field.options**
   *   (value = 目标记录 id 文本,label = 显示字段值),
   *   形状与单选的候选完全一样 ⇒ 复用 select 控件零成本。
   */
  it('editTypeOf:relation → select(候选形状与单选一样)', () => {
    expect(editTypeOf('relation')).toBe('select')
  })

  it('buildFieldEditFields:relation 的候选进 options', () => {
    const f = field({
      type: 'relation',
      options: [{ value: '1844674407370955161', label: '3 号水泵' }],
    })
    const items = buildFieldEditFields([f], t)
    const first = items[0]
    expect(first).toBeDefined()
    expect(first?.options).toEqual([{ value: '1844674407370955161', label: '3 号水泵' }])
  })

  it('formatCell:显示 _rels 里的显示名,不显示 19 位 id', () => {
    const f = field({ type: 'relation' })
    expect(formatCell(f, '1844674407370955161', t, undefined, '3 号水泵')).toBe('3 号水泵')
  })

  /**
   * ⚠️ **取不到标签退回显示 id,不显示空白** ——
   *   目标记录被删 / 标签没解析出来时,空白格子只会让人以为
   *   "这条记录没有关联";id 至少还能让人拿着它去查。
   */
  it('formatCell:标签取不到 → 退回显示 id', () => {
    const f = field({ type: 'relation' })
    expect(formatCell(f, '1844674407370955161', t)).toBe('1844674407370955161')
    expect(formatCell(f, '1844674407370955161', t, undefined, '')).toBe('1844674407370955161')
  })

  it('formatCell:没填关联 → 统一空值文案', () => {
    const f = field({ type: 'relation' })
    expect(formatCell(f, '', t)).toBe('common.emptyValue')
  })
})
