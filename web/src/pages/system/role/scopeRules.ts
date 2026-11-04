/**
 * 数据范围规则 · 纯逻辑 —— M157(2026-11-02)
 *
 * ⚠️ **纯函数必须单独一个文件** —— 本仓 ESLint 是 `--max-warnings 0`,
 * 而"只导出纯函数、不导出组件"的文件才能豁免 react-refresh 那条规则
 * (见 memory:web/ 字典+平台配置层审计 —— 纯函数要单独文件)。
 *
 * ## 这个文件里编码的是"后端的语义",不是"页面的偏好"
 *
 * 每一条都对应后端的一处判定(见 server/internal/pkg/scope/scope.go 的包注释):
 * 规则之间是"或" / "全部数据"一票放行 / 某业务域配了规则则默认组不参与 /
 * 有限制但一条都表达不出来 ⇒ 匹配不到任何行。
 *
 * ⇒ 前端**复述**这些语义,不是为了好看,是为了让管理员在页面上
 *   就能看出"我这么配会怎样",而不是配完才发现不对。
 */
import type { ScopeRule, ScopeRuleInput, ScopeModule } from '@/api/scope'

/** 全部合法的范围类型 —— **顺序即下拉框顺序**,与后端 `scope.AllTypes()` 一致 */
export const SCOPE_TYPES = [
  'all',
  'project',
  'dept',
  'dept_and_child',
  'self',
  'custom',
] as const

export type ScopeType = (typeof SCOPE_TYPES)[number]

/** 默认规则组的业务域 —— **空串**,不是 'default' 也不是 undefined */
export const DEFAULT_MODULE = ''

/**
 * 规则按业务域分组
 *
 * ⚠️ **空串那一组就是"默认规则组"** —— key 就是空串,
 * 不能用 'default' 之类的哨兵值,否则保存时传出去的 module 就不是空串了
 * (后端 `knownModules` 里有 `out[""] = true`,只认空串)。
 */
export function groupByModule(rules: ScopeRule[]): Record<string, ScopeRule[]> {
  const out: Record<string, ScopeRule[]> = {}
  for (const r of rules ?? []) {
    const key = r.module ?? DEFAULT_MODULE
    if (!out[key]) out[key] = []
    out[key].push(r)
  }
  return out
}

/**
 * 这组规则里有没有"全部数据"
 *
 * ⚠️ **"全部数据"一票放行** —— 后端 `Resolve` 遇到 all 立刻返回
 * `Scope{All: true}`,其余规则**根本不会被解析**。
 * ⇒ 页面上必须提示,否则管理员以为自己配了三条限制,其实一条都没生效。
 */
export function hasAllType(rules: ScopeRule[]): boolean {
  return (rules ?? []).some((r) => r.scope_type === 'all')
}

/** 这条规则是不是"自定义"(要额外的列名 + 取值) */
export function isCustomRule(rule: ScopeRule): boolean {
  return rule.scope_type === 'custom'
}

/**
 * 校验一条规则 —— 返回 i18n key(null = 通过)
 *
 * ⚠️ **自定义规则必须同时给列名和取值** ——
 * 后端 `scope.ValidateCustomColumn` 只校验列名是合法标识符,
 * 但"没给值"时后端**不报错**,只是不产生任何子句
 * (⇒ 而 Restrictive 仍为 true ⇒ 用户一条都看不到)。
 * 那是**安全方向**上的行为(看不到 > 看到不该看的),
 * 但页面上必须说清楚,否则管理员以为配好了,结果什么都看不到。
 */
export function validateRule(rule: ScopeRule): string | null {
  if (!rule.scope_type) return 'page.role.scope.errors.scopeTypeRequired'
  if (!SCOPE_TYPES.includes(rule.scope_type as ScopeType)) {
    return 'page.role.scope.errors.scopeTypeInvalid'
  }
  if (isCustomRule(rule)) {
    const col = (rule.custom_column ?? '').trim()
    if (!col) return 'page.role.scope.errors.customColumnRequired'
    // ⚠️ 与后端 ValidateCustomColumn 同口径:必须是裸标识符
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(col)) {
      return 'page.role.scope.errors.customColumnInvalid'
    }
    if (!rule.custom_values || rule.custom_values.filter((v) => v !== '').length === 0) {
      return 'page.role.scope.errors.customValuesRequired'
    }
  }
  return null
}

/**
 * 新增一条规则时默认选哪个类型
 *
 * ⚠️ **绝不能默认给「全部数据」** —— 后端语义里 all 是一票放行,
 * 管理员点了"添加规则"还没改就保存 ⇒ 整组规则变成"全部数据",
 * 别的限制全被短路掉(而页面上的警告是事后才弹的)。
 *
 * ⚠️ **也不能默认给「自定义」** —— 它还要填列名和取值,
 * 没填就保存会被后端拒("自定义数据范围必须至少给一个取值")。
 *
 * ⚠️ **更不能默认给这个业务域不支持的类型** —— 那样一点保存就 400。
 *
 * ⇒ 按「仅本人 → 本项目 → 本部门 → 本部门及下级 → 自定义 → 全部数据」
 *   的顺序取第一个该业务域支持的。
 */
export function defaultScopeType(supported: string[]): string {
  const list = supported?.length ? supported : [...SCOPE_TYPES]
  const preference = ['self', 'project', 'dept', 'dept_and_child', 'custom', 'all']
  for (const t of preference) {
    if (list.includes(t)) return t
  }
  return 'all'
}

/** 页面上的规则 → 保存用的入参(去掉 module —— 它在请求体顶层) */
export function ruleToInput(rule: ScopeRule): ScopeRuleInput {
  const out: ScopeRuleInput = { scope_type: rule.scope_type }
  if (isCustomRule(rule)) {
    out.custom_column = (rule.custom_column ?? '').trim()
    out.custom_values = (rule.custom_values ?? []).filter((v) => v !== '')
  }
  const remark = (rule.remark ?? '').trim()
  if (remark) out.remark = remark
  return out
}

/**
 * 两组规则是否等价(与顺序无关)
 *
 * ⚠️ **比较的是集合,不是数组** —— 规则之间是"或",顺序不影响语义。
 * 拿数组直接比会得出"顺序变了 ⇒ 有改动" ⇒ 白白发一次 PUT。
 */
export function sameRuleSet(a: ScopeRule[], b: ScopeRule[]): boolean {
  const norm = (list: ScopeRule[]) =>
    (list ?? [])
      .map((r) => {
        const vals = [...(r.custom_values ?? [])].filter((v) => v !== '').sort()
        return [
          r.scope_type,
          (r.custom_column ?? '').trim(),
          vals.join('\u0001'),
          (r.remark ?? '').trim(),
        ].join('\u0000')
      })
      .sort()
  const x = norm(a)
  const y = norm(b)
  if (x.length !== y.length) return false
  return x.every((v, i) => v === y[i])
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
export function changedModules(
  original: Record<string, ScopeRule[]>,
  draft: Record<string, ScopeRule[]>,
): string[] {
  const keys = new Set([...Object.keys(original ?? {}), ...Object.keys(draft ?? {})])
  const out: string[] = []
  for (const k of keys) {
    const a = original?.[k] ?? []
    const b = draft?.[k] ?? []
    if (!sameRuleSet(a, b)) out.push(k)
  }
  return out.sort()
}

/**
 * 下拉框的候选项 —— 后端给的 + 手工补上的"默认规则组"
 *
 * ⚠️ **默认规则组不在后端候选里** —— 后端 `ListModules` 返回的是
 * "有哪些业务域可以单独配",默认组(module 为空串)是**隐含**的,
 * `knownModules` 里单独补了 `out[""] = true`。
 * ⇒ 前端必须自己补这一项,否则管理员**没有地方配默认组**。
 *
 * ⚠️ **默认组排在第一个** —— 它是最泛的那一条,先看到它才想得起"还有默认"。
 *
 * ⚠️ **默认组的 supported_types 要从后端那一项里继承** ——
 * 后端算的是"所有已知业务域的交集"(默认组的规则会在每个域上各自求值,
 * 各表能表达的维度不一样)⇒ 前端自己编一个就等于"配得出必然炸的规则"。
 * 后端没给(老版本)⇒ 留空 ⇒ 下面的 supportedTypesFor 回落成"全部可用"。
 */
export function buildModuleOptions(modules: ScopeModule[]): ScopeModule[] {
  const list = modules ?? []
  const rest = list.filter((m) => m.module !== DEFAULT_MODULE)
  const backendDefault = list.find((m) => m.module === DEFAULT_MODULE)
  return [
    {
      module: DEFAULT_MODULE,
      label: '',
      source: 'default',
      ...(backendDefault?.supported_types
        ? { supported_types: backendDefault.supported_types }
        : {}),
    },
    ...rest,
  ]
}

/**
 * 某个业务域支持哪些维度 —— 下拉框只给这些
 *
 * ⚠️ **后端没给 supported_types ⇒ 全部可用** ——
 * 老后端没有这个字段,此时把下拉框清空 = "什么都配不了",
 * 那是比"多给几个选项"严重得多的退化。
 *
 * ⚠️ **给的是一个不认识的类型也要放行** ——
 * 后端给了交集之外的类型(理论上不会),前端直接拒掉会让管理员
 * 配不了本来能配的东西;后端那边有响亮报错兜着。
 *
 * ⚠️ **返回的是后端给的顺序** —— 后端按 `scope.AllTypes()` 的顺序给,
 * 那就是下拉框的顺序(与 SCOPE_TYPES 一致)。
 */
export function supportedTypesFor(module: ScopeModule | undefined): string[] {
  const supported = module?.supported_types
  if (!supported || supported.length === 0) return [...SCOPE_TYPES]
  return supported.filter((v) => SCOPE_TYPES.includes(v as ScopeType))
}
