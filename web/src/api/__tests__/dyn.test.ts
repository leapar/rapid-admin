/**
 * 动态业务集合的纯函数 —— 单测
 *
 * ⚠️ **这三个函数是"设计面 ↔ 数据面"之间的翻译层** ——
 *   形状对不上的时候症状是"存进去但读不出来",而且**当场不报错**:
 *   · 选项集写成裸数组 → 后端解析出空选项集 → 单选存不进值且不报错
 *   · 资源码少一个冒号 → 前端显示"能新增"而后端一律 403
 *   所以它们必须有单测钉住。
 */
import { describe, it, expect } from 'vitest'
import {
  parseDynOptions,
  parseDynOptionsDraft,
  serializeDynOptions,
  serializeDynOptionsDraft,
  toDynOptionsDraft,
  dynRecordPerm,
} from '../dyn'

describe('parseDynOptions · 后端存的 JSON 文本 → 数组', () => {
  it('正常解析', () => {
    const out = parseDynOptions('{"choices":[{"value":"a","label":"甲"}]}')
    expect(out).toEqual([{ value: 'a', label: '甲' }])
  })

  it('空串 / 空 → 空数组', () => {
    expect(parseDynOptions('')).toEqual([])
    expect(parseDynOptions('   ')).toEqual([])
    expect(parseDynOptions(undefined)).toEqual([])
    expect(parseDynOptions(null)).toEqual([])
  })

  /**
   * ⚠️ **解析失败返回空数组,不抛错** ——
   *   选项集是设计面写的,写坏了属于"配置坏了",不该让整个列表页打不开。
   *   空数组的表现是"下拉框没有选项",用户自己能看出来。
   */
  it('坏 JSON → 空数组(不抛)', () => {
    expect(parseDynOptions('{坏掉的')).toEqual([])
    expect(parseDynOptions('["a","b"]')).toEqual([])
  })

  it('缺 label 时用 value 顶上', () => {
    expect(parseDynOptions('{"choices":[{"value":"a"}]}')).toEqual([{ value: 'a', label: 'a' }])
  })

  it('丢掉没有 value 的项', () => {
    const out = parseDynOptions('{"choices":[{"label":"没有值"},{"value":"a","label":"甲"}]}')
    expect(out).toEqual([{ value: 'a', label: '甲' }])
  })
})

describe('serializeDynOptions · 数组 → 后端要的 JSON 文本', () => {
  /**
   * ⚠️ **形状必须是 `{choices:[...]}`** ——
   *   后端 dynx.ParseOptions 反序列化后直接读 `Options.Choices`。
   *   写成裸数组的话后端解析出来是空选项集 ⇒ 单选字段存不进值,
   *   而且**不报错**(选项集为空 ⇒ 校验被跳过)。
   */
  it('包成 {choices:[...]}', () => {
    const out = serializeDynOptions([{ value: 'a', label: '甲' }])
    expect(JSON.parse(out)).toEqual({ choices: [{ value: 'a', label: '甲' }] })
  })

  it('空数组 → 空串(与后端 ParseOptions 的空串语义一致)', () => {
    expect(serializeDynOptions([])).toBe('')
  })

  it('往返:解析回去还是原来那份', () => {
    const raw = '[{"value":"a","label":"甲"},{"value":"b","label":"乙"}]'
    const arr = parseDynOptions(serializeDynOptions(JSON.parse(raw)))
    expect(arr).toEqual([
      { value: 'a', label: '甲' },
      { value: 'b', label: '乙' },
    ])
  })
})

describe('parseDynOptionsDraft · 选项集 JSON 文本 → 「候选项 + 来源」', () => {
  /**
   * ⚠️ **来源是第二份真相,丢了就回不来** ——
   *   关联表来源的字段,候选项是空的(选项是读的时候现查的),
   *   唯一能认出"这个字段是从表里取选项的"就是 source。
   *   只解析 choices 的话,编辑这个字段时页面会显示成"手工添加且一个候选都没有",
   *   用户一保存就把来源配置**抹掉了** —— 而且当场不报错。
   */
  it('来源必须被解析出来', () => {
    const out = parseDynOptionsDraft('{"choices":[],"source":{"kind":"table","key":"user"}}')
    expect(out.source).toBe('user')
    expect(out.choices).toEqual([])
  })

  it('手工选项集:来源为空串', () => {
    const out = parseDynOptionsDraft('{"choices":[{"value":"a","label":"甲"}]}')
    expect(out.source).toBe('')
    expect(out.choices).toEqual([{ value: 'a', label: '甲' }])
  })

  it('空串 / 坏 JSON → 空草稿(不抛)', () => {
    expect(parseDynOptionsDraft('')).toEqual({ choices: [], source: '' })
    expect(parseDynOptionsDraft('{坏掉的')).toEqual({ choices: [], source: '' })
  })

  /**
   * ⚠️ **source 的形状必须是 `{kind, key}`** ——
   *   后端 dynx.Source 是这么定义的(`json:"kind"` / `json:"key"`),
   *   写成 `{"source":"user"}` 的话后端 unmarshal 出来的 Source 是 nil
   *   ⇒ `HasSource()` 返 false ⇒ 这个字段被当成"没有配选项的单选"。
   */
  it('认得后端那种 {kind,key} 形状', () => {
    const out = parseDynOptionsDraft('{"choices":[],"source":{"kind":"table","key":"dept"}}')
    expect(out.source).toBe('dept')
  })
})

describe('toDynOptionsDraft · 任意值 → 选项集草稿', () => {
  /**
   * ⚠️ **必须容错** —— 这个函数的调用点是表单的 getValueProps,
   *   值可能是 undefined(新建字段时)、老数组(老代码)。
   *   不容错的话,打开字段编辑弹窗就抛。
   */
  it('非对象 → 空草稿', () => {
    expect(toDynOptionsDraft(undefined)).toEqual({ choices: [], source: '' })
    expect(toDynOptionsDraft(null)).toEqual({ choices: [], source: '' })
    expect(toDynOptionsDraft('abc')).toEqual({ choices: [], source: '' })
    expect(toDynOptionsDraft(42)).toEqual({ choices: [], source: '' })
  })

  it('对象:挑出 choices 与 source', () => {
    const out = toDynOptionsDraft({ choices: [{ value: 'a', label: '甲' }], source: 'user' })
    expect(out.choices).toEqual([{ value: 'a', label: '甲' }])
    expect(out.source).toBe('user')
  })
})

describe('serializeDynOptionsDraft · 「候选项 + 来源」→ 后端要的 JSON 文本', () => {
  /**
   * ⚠️ **有来源时不能返回空串** ——
   *   后端 dynx.MarshalOptions 在 choices 为空时返回空串,
   *   那样"关联表来源"的配置会被**静默丢掉**:
   *   字段建好了,却变成一个没有任何选项的单选 —— 而前端不报错。
   */
  it('有来源:不能返回空串,形状必须是 {kind,key}', () => {
    const out = serializeDynOptionsDraft({ choices: [], source: 'user' })
    expect(out).not.toBe('')
    expect(JSON.parse(out)).toEqual({ choices: [], source: { kind: 'table', key: 'user' } })
  })

  it('有来源时手工候选项被清掉(两种来源互斥)', () => {
    const out = serializeDynOptionsDraft({
      choices: [{ value: 'a', label: '甲' }],
      source: 'user',
    })
    expect(JSON.parse(out).choices).toEqual([])
  })

  it('无来源无候选 → 空串(与后端 MarshalOptions 的空串语义一致)', () => {
    expect(serializeDynOptionsDraft({ choices: [], source: '' })).toBe('')
    expect(serializeDynOptionsDraft(null)).toBe('')
    expect(serializeDynOptionsDraft(undefined)).toBe('')
  })

  it('无来源有候选 → {choices:[...]}', () => {
    const out = serializeDynOptionsDraft({ choices: [{ value: 'a', label: '甲' }], source: '' })
    expect(JSON.parse(out)).toEqual({ choices: [{ value: 'a', label: '甲' }] })
  })

  /**
   * ⚠️ **往返必须保住来源** ——
   *   设计器打开一个"关联表来源"的字段 → 改个显示名 → 保存,
   *   如果中间任何一步把 source 弄丢了,那个字段就变成一个空单选。
   */
  it('往返:来源不丢', () => {
    const raw = '{"choices":[],"source":{"kind":"table","key":"user"}}'
    const draft = parseDynOptionsDraft(raw)
    expect(parseDynOptionsDraft(serializeDynOptionsDraft(draft)).source).toBe('user')
  })
})

describe('dynRecordPerm · 运行期拼出来的资源码', () => {
  /**
   * ⚠️ **拼出来的字符串必须与后端完全一致** ——
   *   后端 internal/pkg/dynx/actions.go:
   *     RecordModule(collection) = "dyn:" + collection
   *     RecordCode(collection, action) = RecordModule + ":" + action
   *   少一个冒号或多一个前缀 ⇒ 前端显示"能新增"而后端 403。
   */
  it('形状是 dyn:<集合码>:<动作>', () => {
    expect(dynRecordPerm('inspection', 'list')).toBe('dyn:inspection:list')
    expect(dynRecordPerm('inspection', 'create')).toBe('dyn:inspection:create')
    expect(dynRecordPerm('inspection', 'update')).toBe('dyn:inspection:update')
    expect(dynRecordPerm('inspection', 'delete')).toBe('dyn:inspection:delete')
    expect(dynRecordPerm('inspection', 'read')).toBe('dyn:inspection:read')
  })

  /**
   * ⚠️ **必须是三段** —— 集合编码里不可能出现 ":"(后端 dynx.ValidateCode 挡着)
   * ⇒ 数据面永远三段,与设计面的四段(`dyn:design:collection:list`)撞不了。
   */
  it('永远是三段(与设计面的四段码不撞)', () => {
    expect(dynRecordPerm('collection', 'list').split(':')).toHaveLength(3)
    expect(dynRecordPerm('collection', 'list')).not.toBe('dyn:design:collection:list')
  })
})

describe('relation 配置(M5)· 解析 / 序列化不丢', () => {
  /**
   * ⚠️ **relation 配置是"第二份真相",序列化时丢了就是静默抹掉** ——
   *   与 source 同一个坑:字段保存一次,目标集合就被清空,
   *   字段退化成没有任何选项的空字段,而且不报错。
   */
  it('解析:relation 配置要被读出来', () => {
    const d = parseDynOptionsDraft('{"choices":[],"relation":{"target":"device","display_field":"name"}}')
    expect(d.relation_target).toBe('device')
    expect(d.relation_display_field).toBe('name')
  })

  it('解析:没有 relation 配置 → undefined(不是关系字段)', () => {
    expect(parseDynOptionsDraft('{"choices":[{"value":"a","label":"甲"}]}').relation_target).toBeUndefined()
    expect(parseDynOptionsDraft('').relation_target).toBeUndefined()
  })

  it('序列化:配了目标集合不能返回空串,形状要对', () => {
    const out = serializeDynOptionsDraft({
      choices: [],
      source: '',
      relation_target: 'device',
      relation_display_field: 'name',
    })
    const parsed = JSON.parse(out)
    expect(parsed.relation).toEqual({ target: 'device', display_field: 'name' })
  })

  it('序列化:display_field 为空时不带这个键(后端自动挑文本字段)', () => {
    const out = serializeDynOptionsDraft({ choices: [], source: '', relation_target: 'device' })
    expect(JSON.parse(out).relation).toEqual({ target: 'device' })
  })

  it('往返:目标集合与显示字段都不丢', () => {
    const raw = '{"choices":[],"relation":{"target":"device","display_field":"name"}}'
    const back = serializeDynOptionsDraft(toDynOptionsDraft(parseDynOptionsDraft(raw)))
    const parsed = JSON.parse(back)
    expect(parsed.relation.target).toBe('device')
    expect(parsed.relation.display_field).toBe('name')
  })

  it('不是 relation 的字段序列化结果里没有 relation 键', () => {
    const out = serializeDynOptionsDraft({ choices: [{ value: 'a', label: '甲' }], source: '' })
    expect('relation' in JSON.parse(out)).toBe(false)
  })
})
