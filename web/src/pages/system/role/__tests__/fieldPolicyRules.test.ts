/**
 * 字段级可见性规则 · 纯逻辑测试 —— M164(2026-11-02)
 *
 * ## 这条链上"错了没人知道"的地方
 *
 * 字段级可见性是**列级权限** —— 配错了不会报错,只会**静默地**
 * 让该角色看到本该藏起来的字段(后端 `internal/pkg/fieldpolicy` 的注释里明写:
 * 字段名配错 ⇒ 规则作用在一个不存在的键上 ⇒ 静默不生效)。
 *
 * 前端这一层要挡住的是五种"静默":
 *
 *   1. **字段名拼错**      → 候选项必须来自后端,不许前端手打
 *   2. **发空数组当增量**  → 写接口是全量替换,发空 = 清空
 *   3. **发了没改的域**    → 无谓地覆写别的域的规则
 *   4. **把 UI 哨兵当业务域** → 菜单里默认组的 key 是 '@default',
 *      存库的 module 必须是空串。发错 ⇒ 后端直接拒收("业务域 @default 不存在")。
 *   5. **把"跟随默认组"当成"明文可见"** → 前者=继承默认组的限制,
 *      后者=覆盖默认组的限制(让它重新可见)。混成一个 ⇒ 限制被悄悄顶掉。
 *
 * ## 可逆探针(拆掉实现看测试会不会红)
 *
 *   · groupByModule 改用 'default' 当默认组的 key → 组分组测试红
 *   · toInputs 无条件带上 mask_type               → 入参测试红
 *   · validatePolicy 去掉"猜不出方式"那条          → 校验测试红
 *   · findPolicyIssues 忽略候选为空的情况          → 空候选测试红
 *   · samePolicySet 改成按数组比(顺序敏感)        → 差量测试红
 */
import { describe, it, expect } from 'vitest'
import {
  DEFAULT_MODULE,
  INHERIT_MODE,
  FIELD_POLICY_MASK_TYPES,
  FIELD_POLICY_MODES,
  buildModuleOptions,
  changedModules,
  defaultMaskType,
  findPolicyIssues,
  groupByModule,
  samePolicySet,
  toInputs,
  validatePolicy,
  type PolicyMap,
} from '../fieldPolicyRules'
import type { FieldPolicyField, FieldPolicyModule } from '@/api/fieldPolicy'

const CAND_PHONE: FieldPolicyField = {
  field: 'phone',
  label: 'phone',
  mask_type: 'phone',
  sensitive: true,
}

/** 猜不出来打码方式的字段(后端给的就是空串) */
const CAND_UNKNOWN: FieldPolicyField = {
  field: 'address',
  label: 'address',
  mask_type: '',
  sensitive: false,
}

describe('fieldPolicyRules · 分组', () => {
  it('按业务域分组,默认规则组的 key 是空串', () => {
    const grouped = groupByModule([
      { module: '', field: 'phone', mode: 'masked', mask_type: 'phone' },
      { module: 'owner', field: 'real_name', mode: 'hidden' },
    ])
    // ⚠️ 必须是空串 —— 后端 knownFieldModules 里只认空串
    expect(Object.keys(grouped)).toContain(DEFAULT_MODULE)
    expect(grouped['']?.phone?.mode).toBe('masked')
    expect(grouped.owner?.real_name?.mode).toBe('hidden')
  })

  it('mode 为空串按 visible 处理(与后端 ParseMode("") 同口径)', () => {
    const grouped = groupByModule([{ module: '', field: 'phone', mode: '' }])
    expect(grouped['']?.phone?.mode).toBe('visible')
  })

  it('字段名为空的规则直接丢掉(不发到后端)', () => {
    const grouped = groupByModule([{ module: '', field: '  ', mode: 'hidden' }])
    expect(Object.keys(grouped)).toHaveLength(0)
  })
})

describe('fieldPolicyRules · 业务域候选', () => {
  it('后端给了默认组就原样用,且排在第一位', () => {
    const list = buildModuleOptions([
      { module: 'owner', label: '业主信息', source: 'api', fields: [] },
      { module: '', label: '默认(适用于所有业务域)', source: 'default', fields: [] },
    ])
    expect(list[0]?.module).toBe('')
    expect(list[1]?.module).toBe('owner')
  })

  it('后端没给默认组时补一个(老版本后端也能配)', () => {
    const list = buildModuleOptions([
      { module: 'owner', label: '业主信息', source: 'api', fields: [] },
    ])
    expect(list[0]?.module).toBe('')
    expect(list[0]?.source).toBe('default')
  })
})

describe('fieldPolicyRules · 打码方式', () => {
  it('候选项给了推荐值就用推荐值', () => {
    expect(defaultMaskType(CAND_PHONE)).toBe('phone')
  })

  it('猜不出来时兜底「整串打码」(与后端运行期兜底一致)', () => {
    expect(defaultMaskType(CAND_UNKNOWN)).toBe('full')
    expect(defaultMaskType(undefined)).toBe('full')
  })

  it('打码方式词表与后端一致(七种,没有 auto)', () => {
    expect([...FIELD_POLICY_MASK_TYPES]).toEqual([
      'phone',
      'id_card',
      'name',
      'email',
      'bank_card',
      'contract_no',
      'full',
    ])
  })

  it('可见性模式词表与后端一致(三态)', () => {
    expect([...FIELD_POLICY_MODES]).toEqual(['visible', 'masked', 'hidden'])
  })
})

describe('fieldPolicyRules · 校验', () => {
  it('模式不合法 ⇒ 报错', () => {
    expect(validatePolicy({ mode: 'wat' }, CAND_PHONE)).toBe(
      'page.role.fieldPolicy.errors.modeInvalid',
    )
  })

  it('打码但猜不出打码方式 ⇒ 报错(后端在配置期就拒)', () => {
    expect(validatePolicy({ mode: 'masked', mask_type: '' }, CAND_UNKNOWN)).toBe(
      'page.role.fieldPolicy.errors.maskTypeRequired',
    )
  })

  it('打码 + 显式指定了方式 ⇒ 通过', () => {
    expect(validatePolicy({ mode: 'masked', mask_type: 'full' }, CAND_UNKNOWN)).toBeNull()
  })

  it('打码 + 候选项能猜出来 ⇒ 空 mask_type 也通过(后端会自己推断)', () => {
    expect(validatePolicy({ mode: 'masked', mask_type: '' }, CAND_PHONE)).toBeNull()
  })

  it('可见 / 隐藏不需要打码方式', () => {
    expect(validatePolicy({ mode: 'visible' }, CAND_UNKNOWN)).toBeNull()
    expect(validatePolicy({ mode: 'hidden' }, CAND_UNKNOWN)).toBeNull()
  })
})

describe('fieldPolicyRules · 配置期的问题清单', () => {
  const modules: FieldPolicyModule[] = [
    {
      module: '',
      label: '默认',
      source: 'default',
      fields: [CAND_PHONE, CAND_UNKNOWN],
    },
  ]

  it('字段已经不在候选里 ⇒ 报出来(后端会拒整组)', () => {
    const draft: PolicyMap = { '': { legacy: { mode: 'hidden' } } }
    const issues = findPolicyIssues(modules, draft, [''])
    expect(issues).toHaveLength(1)
    expect(issues[0]?.field).toBe('legacy')
    expect(issues[0]?.key).toBe('page.role.fieldPolicy.errors.fieldNotCandidate')
  })

  it('候选列表为空时不当成"字段非法"(不然一条都存不下)', () => {
    const emptyMods: FieldPolicyModule[] = [
      { module: '', label: '默认', source: 'default', fields: [] },
    ]
    const draft: PolicyMap = { '': { legacy: { mode: 'hidden' } } }
    expect(findPolicyIssues(emptyMods, draft, [''])).toHaveLength(0)
  })

  it('打码但猜不出方式 ⇒ 报出来', () => {
    const draft: PolicyMap = { '': { address: { mode: 'masked', mask_type: '' } } }
    const issues = findPolicyIssues(modules, draft, [''])
    expect(issues).toHaveLength(1)
    expect(issues[0]?.key).toBe('page.role.fieldPolicy.errors.maskTypeRequired')
  })

  it('没改动的业务域不进清单(只查改动过的)', () => {
    const draft: PolicyMap = { '': { legacy: { mode: 'hidden' } } }
    expect(findPolicyIssues(modules, draft, [])).toHaveLength(0)
  })
})

describe('fieldPolicyRules · 入参与差量', () => {
  it('mask_type 只在「打码」时发', () => {
    const inputs = toInputs({
      phone: { mode: 'masked', mask_type: 'phone' },
      real_name: { mode: 'visible', mask_type: 'phone' },
    })
    expect(inputs).toEqual([
      { field: 'phone', mode: 'masked', mask_type: 'phone' },
      { field: 'real_name', mode: 'visible' },
    ])
  })

  it('入参按字段名排序(与后端读出来的顺序一致)', () => {
    const inputs = toInputs({
      zzz: { mode: 'hidden' },
      aaa: { mode: 'hidden' },
    })
    expect(inputs.map((i) => i.field)).toEqual(['aaa', 'zzz'])
  })

  it('备注为空就不发(不塞一串空串进库)', () => {
    const inputs = toInputs({ phone: { mode: 'hidden', remark: '   ' } })
    expect(inputs[0]?.remark).toBeUndefined()
  })

  it('同一组规则换个顺序 ⇒ 不算改动', () => {
    const a: PolicyMap = { '': { a: { mode: 'hidden' }, b: { mode: 'masked', mask_type: 'name' } } }
    const b: PolicyMap = { '': { b: { mode: 'masked', mask_type: 'name' }, a: { mode: 'hidden' } } }
    expect(samePolicySet(a[''], b[''])).toBe(true)
    expect(changedModules(a, b)).toEqual([])
  })

  it('改了备注 ⇒ 算改动(归一化口径要跟入参一致)', () => {
    const a: PolicyMap = { '': { a: { mode: 'hidden', remark: '' } } }
    const b: PolicyMap = { '': { a: { mode: 'hidden', remark: 'x' } } }
    expect(changedModules(a, b)).toEqual([''])
  })

  it('删光一个业务域的规则 ⇒ 算改动(必须发一次空数组清掉)', () => {
    const a: PolicyMap = { '': { a: { mode: 'hidden' } } }
    const b: PolicyMap = { '': {} }
    expect(changedModules(a, b)).toEqual([''])
  })

  it('新增一个业务域的规则 ⇒ 算改动', () => {
    const a: PolicyMap = {}
    const b: PolicyMap = { owner: { a: { mode: 'hidden' } } }
    expect(changedModules(a, b)).toEqual(['owner'])
  })

  it('「跟随默认组」= 没有规则,与「显式明文可见」不是一回事', () => {
    const inherit: PolicyMap = { owner: {} }
    const visible: PolicyMap = { owner: { phone: { mode: 'visible' } } }
    // ⚠️ 这两条必须判成"有改动" —— 否则管理员把字段改回"跟随默认组"时
    //   页面以为没变,默认组那条限制却再也回不来。
    expect(samePolicySet(inherit.owner, visible.owner)).toBe(false)
    expect(INHERIT_MODE).not.toBe('visible')
  })
})
