/**
 * 字段级可见性规则 · 纯逻辑 —— M164(2026-11-02)
 *
 * ⚠️ **纯函数必须单独一个文件** —— 本仓 ESLint 是 `--max-warnings 0`,
 * 而"只导出纯函数、不导出组件"的文件才能豁免 react-refresh 那条规则
 * (见 memory:web/ 字典+平台配置层审计 —— 纯函数要单独文件)。
 *
 * ## 这个文件里编码的是"后端的语义",不是"页面的偏好"
 *
 * 每一条都对应后端的一处判定(见 server/internal/pkg/fieldpolicy 的包注释):
 *
 *   - 三态模式: 明文 / 打码 / 隐藏(隐藏 = **从响应里删掉这个键**,不是置空)
 *   - 默认规则组与业务域规则是**逐字段叠加**,不是整组覆盖
 *   - 打码但"按字段名猜不出打码方式" ⇒ **配置期就拒**
 *   - 字段不在这个业务域的候选里 ⇒ **配置期就拒**
 *
 * ⇒ 前端**复述**这些语义,不是为了好看,是为了让管理员在页面上
 *   就能看出"我这么配会怎样",而不是配完才发现不对(或者更糟:
 *   后端拒了但没人知道是哪一条)。
 *
 * ## ⚠️ **与行级数据范围有一处故意的不同**
 *
 *   行级(scope):某业务域配了规则 ⇒ **整组**就近覆盖,默认组不参与。
 *   列级(本文件):默认组与业务域是**逐字段**叠加 ——
 *                 业务域只覆盖**它自己写了规则的那些字段**。
 *
 * ⇒ 业务域里把某个字段设成「明文可见」,会让它在**本业务域**上重新可见
 *   (默认组对它的那条限制在本业务域上不再生效)。
 *   页面必须把这件事说出来,否则管理员以为"默认组是兜底,谁都躲不掉"。
 */
import type { FieldPolicy, FieldPolicyField, FieldPolicyInput, FieldPolicyModule } from '@/api/fieldPolicy'

/** 默认规则组的业务域 —— **空串**,不是 'default' 也不是 undefined */
export const DEFAULT_MODULE = ''

/**
 * 「没有规则」的哨兵值
 *
 * ⚠️ **它不是一种模式,它是"这条字段上没有规则"** ——
 *   草稿里没有这个字段 ⇒ 不发规则 ⇒ 走默认组(业务域)/明文可见(默认组)。
 *
 * ⚠️ **它与"显式的 visible"是两件事,不能合并** ——
 *   "没有规则" = 继承默认组对这个字段的限制;
 *   "显式 visible" = 覆盖默认组对这个字段的限制(让它重新可见)。
 *   合并成一个 ⇒ 管理员在业务域里把某个字段改回「明文可见」时,
 *   默认组那条限制就跟着消失了 —— 而他以为只是"取消了这个域的规则"。
 */
export const INHERIT_MODE = 'inherit'

/** 全部合法的可见性模式 —— **顺序即下拉框顺序**,与后端 `fieldpolicy.AllModes()` 一致 */
export const FIELD_POLICY_MODES = ['visible', 'masked', 'hidden'] as const

export type FieldPolicyMode = (typeof FIELD_POLICY_MODES)[number]

/**
 * 全部合法的打码方式 —— **顺序即下拉框顺序**,与后端 `fieldpolicy.AllMaskTypes()` 一致
 *
 * ⚠️ **这里没有 "auto"** —— auto(空串)是"按字段名猜",不是一种打码方式。
 *   它在页面上是「自动(按字段名推断)」这个**单独的一项**,
 *   放在下拉框第一位(选中它 = 交给后端按字段名推断)。
 */
export const FIELD_POLICY_MASK_TYPES = [
  'phone',
  'id_card',
  'name',
  'email',
  'bank_card',
  'contract_no',
  'full',
] as const

export type FieldPolicyMaskType = (typeof FIELD_POLICY_MASK_TYPES)[number]

/**
 * 草稿里的一条规则
 *
 * ⚠️ **没有 mask_type 的语义是"按字段名自动推断"** —— 与后端一致(空串 = auto)。
 */
export interface FieldPolicyDraft {
  /** visible / masked / hidden */
  mode: string
  /** 打码方式(空串 = 按字段名自动推断) */
  mask_type?: string
  /** 备注 */
  remark?: string
}

/** 规则按业务域分组(每个业务域一个"字段 → 规则"的表) */
export type PolicyMap = Record<string, Record<string, FieldPolicyDraft>>

/**
 * 规则按业务域分组
 *
 * ⚠️ **空串那一组就是"默认规则组"** —— key 就是空串,
 * 不能用 'default' 之类的哨兵值,否则保存时传出去的 module 就不是空串了
 * (后端 `knownFieldModules` 里只认空串)。
 *
 * ⚠️ **mode 为空串按 visible 处理** —— 与后端 `fieldpolicy.ParseMode("")` 同口径
 * ("没配" 与 "配成明文" 效果一样)。
 */
export function groupByModule(rules: FieldPolicy[]): PolicyMap {
  const out: PolicyMap = {}
  for (const r of rules ?? []) {
    const module = r.module ?? DEFAULT_MODULE
    const field = (r.field ?? '').trim()
    if (!field) continue
    if (!out[module]) out[module] = {}
    out[module][field] = {
      mode: (r.mode ?? '').trim() || 'visible',
      mask_type: (r.mask_type ?? '').trim(),
      remark: r.remark ?? '',
    }
  }
  return out
}

/**
 * 下拉框的候选项 —— 后端给的,默认组排第一
 *
 * ⚠️ **后端已经把默认规则组放在第一位了**(它自己算的并集候选),
 *   这里只做两件事:
 *   ① 后端没给默认组时补一个(老版本后端)
 *   ② 默认组不在第一位时把它提到前面
 *
 * ⚠️ **默认组的 label 用前端自己的 i18n** ——
 *   后端那个 label 是**硬编码的中文串**(service 层拼的),
 *   英文界面下会露出中文。
 */
export function buildModuleOptions(modules: FieldPolicyModule[]): FieldPolicyModule[] {
  const list = modules ?? []
  const rest = list.filter((m) => m.module !== DEFAULT_MODULE)
  const backendDefault = list.find((m) => m.module === DEFAULT_MODULE)
  const fallbackDefault: FieldPolicyModule = {
    module: DEFAULT_MODULE,
    label: '',
    source: 'default',
    fields: [],
  }
  return [backendDefault ?? fallbackDefault, ...rest]
}

/**
 * 切到「打码」时默认用哪种打码方式
 *
 * ⚠️ **候选给了推荐值就用推荐值** —— 后端按字段名推断出来的那个,
 *   正好是管理员"本来想要的"那一种(phone / id_card / …)。
 *
 * ⚠️ **猜不出来时兜底「整串打码」** —— 与后端运行期的兜底一致
 *   (fieldpolicy.Mask 的 default 分支就是整串打码)。
 *   给一个明确值而不是留空:留空会被后端在**配置期**拒掉
 *   ("打码但猜不出方式"),管理员会看到一句他看不懂的报错。
 */
export function defaultMaskType(candidate: FieldPolicyField | undefined): string {
  const recommended = (candidate?.mask_type ?? '').trim()
  return recommended || 'full'
}

/**
 * 一条规则是否合法 —— 返回 i18n key(null = 通过)
 *
 * ⚠️ **「打码」但猜不出打码方式 ⇒ 配置期就拒** ——
 *   后端 `parseFieldPolicyInputs` 就是这么拒的:
 *   配了 masked 但 mask_type 为空 ⇒ 后端按字段名猜 ⇒ 猜不出来 ⇒ 400。
 *   运行期的兜底是"整串打码",那会让管理员看到一堆意料之外的星号,
 *   而他以为自己配的是"手机号打码"。
 *
 * ⚠️ **候选里没有这个字段 ⇒ 也要拒** ——
 *   规则作用不到任何键上 ⇒ **静默不生效**(那正是这个机制要治的病)。
 */
export function validatePolicy(
  policy: FieldPolicyDraft,
  candidate: FieldPolicyField | undefined,
): string | null {
  if (!FIELD_POLICY_MODES.includes(policy.mode as FieldPolicyMode)) {
    return 'page.role.fieldPolicy.errors.modeInvalid'
  }
  if (policy.mode === 'masked') {
    const explicit = (policy.mask_type ?? '').trim()
    if (!explicit && !(candidate?.mask_type ?? '').trim()) {
      return 'page.role.fieldPolicy.errors.maskTypeRequired'
    }
  }
  return null
}

/**
 * 一处配置问题
 *
 * ⚠️ **module + field 都要带上** —— 管理员看到的是"哪个业务域的哪个字段不对",
 *   而不是一句"有 3 处不合法"(还得自己找)。
 */
export interface PolicyIssue {
  module: string
  field: string
  /** i18n key */
  key: string
}

/**
 * 找出所有会在**配置期**被后端拒掉的规则
 *
 * ⚠️ **前端先拒一遍,是为了让错误落在那一行上** ——
 *   后端拒的是**整组**(一个字段不合法 ⇒ 整个业务域的这次 PUT 全废),
 *   管理员只看到一句"保存失败",不知道是哪一条。
 *   前端先拒 ⇒ 精确到行 + 能说出该怎么改。
 *
 * ⚠️ **候选列表为空时跳过"字段不在候选里"的检查** ——
 *   后端没给候选(老版本 / 动态集合被删了)时,
 *   把已有规则判成非法 ⇒ 管理员**一条都存不下**(比"多给几个选项"严重得多)。
 *   后端那边有响亮报错兜着。
 */
export function findPolicyIssues(
  modules: FieldPolicyModule[],
  draft: PolicyMap,
  changed: string[],
): PolicyIssue[] {
  const out: PolicyIssue[] = []
  for (const module of changed ?? []) {
    const mod = modules.find((m) => m.module === module)
    const candidates = mod?.fields ?? []
    const map = new Map(candidates.map((f) => [f.field, f]))
    for (const [field, policy] of Object.entries(draft?.[module] ?? {})) {
      const candidate = map.get(field)
      if (candidate) {
        const key = validatePolicy(policy, candidate)
        if (key) out.push({ module, field, key })
        continue
      }
      // ⚠️ 后端**没给候选**时不当成"字段非法"(见函数头注释)
      if (candidates.length > 0) {
        out.push({
          module,
          field,
          key: 'page.role.fieldPolicy.errors.fieldNotCandidate',
        })
      }
    }
  }
  return out
}

/**
 * 草稿 → 保存用的入参
 *
 * ⚠️ **mask_type 只在「打码」时发** ——
 *   visible / hidden 时打码方式没有意义,发过去也只是让库里存一串没用的值。
 *   与 `samePolicySet` 的归一化口径一致(两边都按"非 masked ⇒ mask_type 不参与"比)。
 */
export function toInputs(draft: Record<string, FieldPolicyDraft> | undefined): FieldPolicyInput[] {
  const out: FieldPolicyInput[] = []
  for (const [field, policy] of Object.entries(draft ?? {})) {
    const input: FieldPolicyInput = { field, mode: policy.mode }
    if (policy.mode === 'masked') input.mask_type = (policy.mask_type ?? '').trim()
    const remark = (policy.remark ?? '').trim()
    if (remark) input.remark = remark
    out.push(input)
  }
  return out.sort((a, b) => a.field.localeCompare(b.field))
}

/**
 * 一条规则的归一化字符串(比较用)
 *
 * ⚠️ **归一化口径必须与 toInputs 一致** ——
 *   否则"改了备注但归一化后一样"会被判成没改动(白白发一次 PUT),
 *   或者"没改却被判成改了"(白白发一次 PUT + 一次缓存失效)。
 */
function normRule(field: string, policy: FieldPolicyDraft): string {
  const mask = policy.mode === 'masked' ? (policy.mask_type ?? '').trim() : ''
  return [field, policy.mode, mask, (policy.remark ?? '').trim()].join('\u0000')
}

/**
 * 两组规则是否等价(与顺序无关)
 *
 * ⚠️ **比较的是集合,不是数组** —— 一个字段一条规则(唯一键 role_id+module+field),
 *   顺序不影响语义。拿数组直接比会得出"顺序变了 ⇒ 有改动" ⇒ 白白发一次 PUT。
 */
export function samePolicySet(
  a: Record<string, FieldPolicyDraft> | undefined,
  b: Record<string, FieldPolicyDraft> | undefined,
): boolean {
  const keys = new Set([...Object.keys(a ?? {}), ...Object.keys(b ?? {})])
  for (const k of keys) {
    const x = a?.[k]
    const y = b?.[k]
    // ⚠️ 一个有一个没有 ⇒ 有改动(加了一条 / 删了一条)
    if (!x || !y) {
      if (x || y) return false
      continue
    }
    if (normRule(k, x) !== normRule(k, y)) return false
  }
  return true
}

/**
 * 哪些业务域的规则被改了
 *
 * ⚠️ **只发改动的** —— 写接口是**全量替换**语义:
 * 发一次就覆盖该业务域的全部规则。
 * 遍历所有业务域全量重发的话,一次保存会触发 N 次写 + N 次缓存失效,
 * 而其中 N-1 次是"用同样的内容覆盖同样的内容"。
 *
 * ⚠️ **"清空"也算改动** —— 原来有规则、现在删光了 ⇒ 必须发一次空数组,
 * 否则后端那几条规则还在库里(页面上看着清了,实际没清)。
 */
export function changedModules(original: PolicyMap, draft: PolicyMap): string[] {
  const keys = new Set([...Object.keys(original ?? {}), ...Object.keys(draft ?? {})])
  const out: string[] = []
  for (const k of keys) {
    if (!samePolicySet(original?.[k], draft?.[k])) out.push(k)
  }
  return out.sort()
}
