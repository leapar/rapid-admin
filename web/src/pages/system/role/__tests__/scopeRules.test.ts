/**
 * 数据范围规则 · 纯逻辑单测 —— M157(2026-11-02)
 *
 * ## 为什么这些"纯函数"也要测
 *
 * 它们编码的是**后端的语义**,不是页面的偏好 ——
 * 规则之间是"或" / "全部数据"一票放行 / 某业务域配了规则则默认组不参与 /
 * 有限制但一条都表达不出来 ⇒ 匹配不到任何行。
 *
 * ⚠️ 这些语义在后端是**安全相关**的(见 server/internal/pkg/scope/scope.go
 * 的包注释)。前端复述错一条 ⇒ 管理员在页面上看到的提示与实际生效的
 * 行为不符 ⇒ 他照着错的提示配 ⇒ 数据边界与预期不符。
 *
 * ## 可逆探针(拆掉实现看测试会不会红)
 *
 *   · groupByModule 改成按 source 分组        → GR1 红
 *   · hasAllType 改成"第一条是 all 就算"      → GR2 红
 *   · validateRule 去掉 custom_values 的检查   → GR3 红
 *   · ruleToInput 保留 module 字段             → GR4 红
 *   · sameRuleSet 改成按数组顺序比             → GR5 红
 *   · changedModules 忽略"清空"                → GR6 红
 *   · buildModuleOptions 不补默认组            → GR7 红
 */
import { describe, it, expect } from 'vitest'
import {
  DEFAULT_MODULE,
  SCOPE_TYPES,
  buildModuleOptions,
  changedModules,
  defaultScopeType,
  groupByModule,
  hasAllType,
  isCustomRule,
  ruleToInput,
  sameRuleSet,
  supportedTypesFor,
  validateRule,
} from '../scopeRules'
import type { ScopeRule } from '@/api/scope'

/** 造一条规则(module 缺省 = 默认组) */
function rule(partial: Partial<ScopeRule> & { scope_type: string }): ScopeRule {
  // ⚠️ module 写在**后面** —— 放前面会被 partial.module(undefined) 覆盖掉
  return { ...partial, module: partial.module ?? DEFAULT_MODULE }
}

describe('SCOPE_TYPES', () => {
  it('六种类型,与后端 scope.AllTypes() 同序', () => {
    // ⚠️ 顺序是 UI 契约 —— 后端注释明写"前端按这个顺序渲染下拉框"
    expect([...SCOPE_TYPES]).toEqual([
      'all',
      'project',
      'dept',
      'dept_and_child',
      'self',
      'custom',
    ])
  })
})

describe('groupByModule', () => {
  // GR1
  it('按业务域分组,空串那一组就是默认规则组', () => {
    const grouped = groupByModule([
      rule({ scope_type: 'self' }),
      rule({ scope_type: 'dept', module: 'user' }),
      rule({ scope_type: 'project', module: 'user' }),
    ])
    expect(Object.keys(grouped).sort()).toEqual(['', 'user'])
    expect(grouped['']).toHaveLength(1)
    expect(grouped['user']).toHaveLength(2)
  })

  it('缺 module 的规则归到默认组', () => {
    // 后端理论上总给 module,但前端不能假设 —— 缺了就当默认组
    const grouped = groupByModule([{ scope_type: 'self' } as unknown as ScopeRule])
    expect(grouped[DEFAULT_MODULE]).toHaveLength(1)
  })

  it('空数组返回空对象', () => {
    expect(groupByModule([])).toEqual({})
  })
})

describe('hasAllType', () => {
  // GR2
  it('有「全部数据」就返回 true', () => {
    expect(hasAllType([rule({ scope_type: 'dept' }), rule({ scope_type: 'all' })])).toBe(true)
  })

  it('没有就返回 false', () => {
    expect(hasAllType([rule({ scope_type: 'dept' }), rule({ scope_type: 'self' })])).toBe(false)
  })

  it('空数组返回 false', () => {
    expect(hasAllType([])).toBe(false)
  })
})

describe('isCustomRule', () => {
  it('只有 custom 是自定义', () => {
    expect(isCustomRule(rule({ scope_type: 'custom' }))).toBe(true)
    expect(isCustomRule(rule({ scope_type: 'self' }))).toBe(false)
  })
})

describe('validateRule', () => {
  // GR3
  it('普通类型不校验列名/取值', () => {
    expect(validateRule(rule({ scope_type: 'dept' }))).toBeNull()
  })

  it('空类型不合法', () => {
    expect(validateRule(rule({ scope_type: '' }))).toBe('page.role.scope.errors.scopeTypeRequired')
  })

  it('类型不在清单里不合法', () => {
    expect(validateRule(rule({ scope_type: 'whatever' }))).toBe(
      'page.role.scope.errors.scopeTypeInvalid',
    )
  })

  it('自定义缺列名 → 报错', () => {
    expect(validateRule(rule({ scope_type: 'custom' }))).toBe(
      'page.role.scope.errors.customColumnRequired',
    )
  })

  it('自定义列名不是裸标识符 → 报错', () => {
    expect(
      validateRule(
        rule({ scope_type: 'custom', custom_column: '1abc', custom_values: ['x'] }),
      ),
    ).toBe('page.role.scope.errors.customColumnInvalid')
    expect(
      validateRule(
        rule({ scope_type: 'custom', custom_column: 'a b', custom_values: ['x'] }),
      ),
    ).toBe('page.role.scope.errors.customColumnInvalid')
    expect(
      validateRule(
        rule({ scope_type: 'custom', custom_column: 'a-b', custom_values: ['x'] }),
      ),
    ).toBe('page.role.scope.errors.customColumnInvalid')
  })

  it('自定义缺取值 → 报错(后端不报错但会一条都看不到,页面必须说清楚)', () => {
    expect(
      validateRule(rule({ scope_type: 'custom', custom_column: 'project_id' })),
    ).toBe('page.role.scope.errors.customValuesRequired')
    expect(
      validateRule(
        rule({ scope_type: 'custom', custom_column: 'project_id', custom_values: [] }),
      ),
    ).toBe('page.role.scope.errors.customValuesRequired')
    expect(
      validateRule(
        rule({ scope_type: 'custom', custom_column: 'project_id', custom_values: [''] }),
      ),
    ).toBe('page.role.scope.errors.customValuesRequired')
  })

  it('自定义齐全 → 通过', () => {
    expect(
      validateRule(
        rule({ scope_type: 'custom', custom_column: 'project_id', custom_values: ['1'] }),
      ),
    ).toBeNull()
  })
})

describe('ruleToInput', () => {
  // GR4
  it('不带 module —— 它在请求体顶层', () => {
    const out = ruleToInput(rule({ scope_type: 'self', module: 'user' }))
    expect(out).toEqual({ scope_type: 'self' })
    expect('module' in out).toBe(false)
  })

  it('普通类型不带列名/取值', () => {
    const out = ruleToInput(
      rule({ scope_type: 'dept', custom_column: 'x', custom_values: ['y'] }),
    )
    expect(out).toEqual({ scope_type: 'dept' })
  })

  it('自定义类型带列名与取值,取值里的空串被剔掉', () => {
    const out = ruleToInput(
      rule({
        scope_type: 'custom',
        custom_column: '  project_id  ',
        custom_values: ['1', '', '2'],
      }),
    )
    expect(out).toEqual({
      scope_type: 'custom',
      custom_column: 'project_id',
      custom_values: ['1', '2'],
    })
  })

  it('备注为空串时不带这个字段', () => {
    expect(ruleToInput(rule({ scope_type: 'self', remark: '   ' }))).toEqual({
      scope_type: 'self',
    })
    expect(ruleToInput(rule({ scope_type: 'self', remark: ' 只看自己 ' }))).toEqual({
      scope_type: 'self',
      remark: '只看自己',
    })
  })
})

describe('sameRuleSet', () => {
  // GR5
  it('顺序不同也算相等(规则之间是「或」,顺序不影响语义)', () => {
    const a = [rule({ scope_type: 'dept' }), rule({ scope_type: 'self' })]
    const b = [rule({ scope_type: 'self' }), rule({ scope_type: 'dept' })]
    expect(sameRuleSet(a, b)).toBe(true)
  })

  it('取值顺序不同也算相等', () => {
    const a = [rule({ scope_type: 'custom', custom_column: 'p', custom_values: ['1', '2'] })]
    const b = [rule({ scope_type: 'custom', custom_column: 'p', custom_values: ['2', '1'] })]
    expect(sameRuleSet(a, b)).toBe(true)
  })

  it('类型不同就不等', () => {
    expect(sameRuleSet([rule({ scope_type: 'dept' })], [rule({ scope_type: 'self' })])).toBe(false)
  })

  it('备注不同就不等', () => {
    expect(
      sameRuleSet([rule({ scope_type: 'dept', remark: 'a' })], [rule({ scope_type: 'dept', remark: 'b' })]),
    ).toBe(false)
  })

  it('条数不同就不等', () => {
    expect(sameRuleSet([rule({ scope_type: 'dept' })], [])).toBe(false)
  })
})

describe('changedModules', () => {
  // GR6
  it('没改动的业务域不报', () => {
    const original = { user: [rule({ scope_type: 'dept', module: 'user' })] }
    const draft = { user: [rule({ scope_type: 'dept', module: 'user' })] }
    expect(changedModules(original, draft)).toEqual([])
  })

  it('改过的业务域要报', () => {
    const original = { user: [rule({ scope_type: 'dept', module: 'user' })] }
    const draft = { user: [rule({ scope_type: 'self', module: 'user' })] }
    expect(changedModules(original, draft)).toEqual(['user'])
  })

  it('「删光了」也算改动 —— 不发 PUT 的话后端那几条规则还在库里', () => {
    const original = { user: [rule({ scope_type: 'dept', module: 'user' })] }
    const draft = { user: [] }
    expect(changedModules(original, draft)).toEqual(['user'])
  })

  it('新增的业务域要报', () => {
    const original = {}
    const draft = { user: [rule({ scope_type: 'dept', module: 'user' })] }
    expect(changedModules(original, draft)).toEqual(['user'])
  })

  it('默认组(module 为空串)也是业务域', () => {
    const original = { '': [rule({ scope_type: 'dept' })] }
    const draft = { '': [] }
    expect(changedModules(original, draft)).toEqual([''])
  })

  it('多个业务域改动时按字典序返回', () => {
    const original = { b: [], a: [] }
    const draft = { b: [rule({ scope_type: 'dept', module: 'b' })], a: [rule({ scope_type: 'dept', module: 'a' })] }
    expect(changedModules(original, draft)).toEqual(['a', 'b'])
  })
})

describe('buildModuleOptions', () => {
  // GR7
  it('默认组排在第一个,且候选项都来自后端', () => {
    const opts = buildModuleOptions([
      { module: 'user', label: '用户管理', source: 'api' },
      { module: 'dyn:inspection', label: '设备巡检', source: 'dyn' },
    ])
    expect(opts).toHaveLength(3)
    expect(opts[0]?.module).toBe(DEFAULT_MODULE)
    expect(opts[0]?.source).toBe('default')
    expect(opts.slice(1).map((o) => o.module)).toEqual(['user', 'dyn:inspection'])
  })

  it('后端万一也返回了空串那一项,不能出现两项默认组', () => {
    const opts = buildModuleOptions([
      { module: '', label: 'x', source: 'api' },
      { module: 'user', label: '用户管理', source: 'api' },
    ])
    expect(opts.filter((o) => o.module === DEFAULT_MODULE)).toHaveLength(1)
  })

  it('后端返回空列表时,仍然只有默认组一项', () => {
    const opts = buildModuleOptions([])
    expect(opts).toHaveLength(1)
    expect(opts[0]?.module).toBe(DEFAULT_MODULE)
  })

  // GR8
  it('默认组的 supported_types 从后端那一项继承(前端不自己编)', () => {
    // ⚠️ 默认组的规则会在**每个**业务域上各自求值,
    //   各表能表达的维度不一样 ⇒ 可用维度是"所有业务域的交集"。
    //   前端自己编一个就等于"配得出必然在某些域炸的规则"。
    const opts = buildModuleOptions([
      { module: '', label: 'x', source: 'default', supported_types: ['all', 'project'] },
      { module: 'user', label: '用户管理', source: 'api', supported_types: ['all', 'self'] },
    ])
    expect(opts[0]?.supported_types).toEqual(['all', 'project'])
    expect(opts[1]?.supported_types).toEqual(['all', 'self'])
  })

  it('后端没给 supported_types 时,默认组留空(而不是编一个)', () => {
    const opts = buildModuleOptions([
      { module: '', label: 'x', source: 'default' },
      { module: 'user', label: '用户管理', source: 'api' },
    ])
    expect(opts[0]?.supported_types).toBeUndefined()
  })
})

describe('supportedTypesFor', () => {
  // GR9
  it('后端给了就按它给', () => {
    expect(
      supportedTypesFor({ module: 'house', label: '房屋', source: 'api', supported_types: ['all', 'project'] }),
    ).toEqual(['all', 'project'])
  })

  it('后端没给(老版本)⇒ 全部可用,不能清空下拉框', () => {
    // ⚠️ 清空 = "什么都配不了",那比"多给几个选项"严重得多
    expect(supportedTypesFor({ module: 'house', label: '房屋', source: 'api' })).toEqual([
      ...SCOPE_TYPES,
    ])
    expect(supportedTypesFor(undefined)).toEqual([...SCOPE_TYPES])
    expect(supportedTypesFor({ module: 'x', label: 'x', source: 'api', supported_types: [] })).toEqual([
      ...SCOPE_TYPES,
    ])
  })

  it('后端给了不认识的类型要剔掉(前端只认六种)', () => {
    expect(
      supportedTypesFor({
        module: 'x',
        label: 'x',
        source: 'api',
        supported_types: ['all', '外星类型'],
      }),
    ).toEqual(['all'])
  })

  it('顺序按后端给的来(后端给的就是下拉框顺序)', () => {
    expect(
      supportedTypesFor({
        module: 'x',
        label: 'x',
        source: 'api',
        supported_types: ['all', 'self', 'project'],
      }),
    ).toEqual(['all', 'self', 'project'])
  })
})

describe('defaultScopeType', () => {
  // GR10
  it('绝不默认给「全部数据」—— 它一票放行,别的限制全被短路', () => {
    expect(defaultScopeType(['all', 'project'])).toBe('project')
    expect(defaultScopeType(['all', 'self', 'dept'])).toBe('self')
  })

  it('绝不默认给「自定义」—— 它还要填列名和取值', () => {
    expect(defaultScopeType(['all', 'custom'])).toBe('custom')
    // ⚠️ 但只有 all/custom 可用时,custom 仍是唯一合理的选择
    //   (总比 all 好 —— all 会一票放行)
  })

  it('不支持的类型不会被选中(那个业务域上默认类型必须配得上去)', () => {
    // house 只有 project ⇒ 默认不能是 self
    expect(defaultScopeType(['all', 'project'])).not.toBe('self')
  })

  it('没给清单时退回全部类型', () => {
    expect([...SCOPE_TYPES]).toContain(defaultScopeType([]))
  })
})
