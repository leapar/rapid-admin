/**
 * 字段级可见性(列级权限)· 前端消费那一半 —— M168(2026-11-02)
 *
 * # 这一层在解决什么
 *
 * 服务端把某个字段"隐藏"时是**把那个键整个删掉**(见
 * `server/internal/pkg/fieldpolicy` 的 `ModeHidden` 注释:置空的话字段名本身
 * 还是信息,比如 `emergency_contact_phone` 说明这个人有紧急联系人)。
 *
 * 于是前端拿到的是:
 *
 *   · 表格的列定义**还列着它** ⇒ **列还在、格是空的**
 *   · 表单里那个字段**也还显示** ⇒ 用户还能把**打码后的值原样提交回去**
 *     (`138****1234` 覆盖掉真实的手机号)—— 这是**数据损坏**
 *
 * ⇒ 前端必须知道"这次响应里哪些字段被收窄了",才能把列 / 字段藏起来。
 *
 * # ⚠️ **为什么不让前端自己算**
 *
 * 前端若自己拿"规则表"再算一遍,就多了一份实现 —— 与服务端那份漂移时,
 * 表现是"界面说有这列、后端说没有"或反过来(后者是**泄露**)。
 *
 * ⇒ **报告由执行层(服务端)算,前端只读**(与 M149 同一条纪律:
 * 「显示层不许自己算,必须读执行层那个权威字段」)。
 *
 * # 传输方式:响应头 + 对象上的一个 Symbol 戳
 *
 *   · **响应头** `X-Field-Policy: hidden=a,b;masked=c`
 *     —— 服务端在**响应出口**(`BaseHandler.OK` / `PageWith`)算好后
 *     随响应带回来(见 `server/internal/api/handler/field_policy.go`)。
 *   · **Symbol 戳** —— 前端在 axios 拦截器里把报告盖到响应数据对象上,
 *     于是**拿着这份数据的人**(表格 / 表单 / 抽屉)都能读到它,
 *     **业务页面一行都不用改**。
 *
 * ⚠️ **跨域时这个头必须列进 CORS 的 `ExposeHeaders`** ——
 *    浏览器只把白名单里的响应头暴露给 JS,不列就读不到,
 *    而且**不报任何错**(只是读出来是 undefined)。
 *    后端那边见 `server/internal/api/router/router.go` 的 `corsConfig`。
 */

/** 响应头名 —— 与后端 `handler.FieldPolicyHeader` 逐字相同 */
export const FIELD_POLICY_HEADER = 'X-Field-Policy'

/**
 * 报告在对象上的落点 —— 一个 Symbol 键
 *
 * ⚠️ **为什么用 Symbol 而不是普通字符串键**
 *
 *   · `Object.keys` / `Object.entries` / `for...in` / `JSON.stringify`
 *     **全都看不见 Symbol 键** ⇒ 报告不会漏进请求体、日志、DOM。
 *   · 而它必须**是 enumerable 的** ⇒ **能被 `{...row}` 展开复制**。
 *     这一点不是可选项:页面经常把行对象展开成新的初始值
 *     (见 `pages/system/user/index.tsx` 的 `editInitialValues`)。
 *     不可枚举的话,展开一次报告就没了 ⇒ 保护失效。
 *
 * ⚠️ **`Symbol.for` 而不是 `Symbol()`** ——
 *    全局注册表里的同一个 key,不同模块拿到的是**同一个** Symbol
 *    (哪怕打包/测试时被复制成两份模块)。
 *    否则 utils 里盖的戳、组件里读的戳是两个不同的 Symbol ⇒ 永远读不到。
 */
export const FIELD_POLICY_SYMBOL = Symbol.for('rapid-admin.fieldPolicy')

/**
 * 一份"字段可见性报告"
 *
 * ⚠️ **报的是"规则说了什么",不是"这次响应里改了什么"** ——
 *    哪怕这次响应里根本没有那个字段,也要报出来。
 *    理由:前端要的是"这一列该不该存在",而不是"这一次有没有动过它"。
 */
export interface FieldPolicyReport {
  /** 被隐藏的字段名(响应里根本没有这个键) */
  hidden: string[]
  /** 被打码的字段名(值是打码后的) */
  masked: string[]
}

/**
 * 当前页面最近一次收到的报告
 *
 * ⚠️ **这是"页面级归档",不是全局广播** ——
 *
 *   绝大多数情况下报告是**跟着数据对象走的**(Symbol 戳),
 *   不需要这一条兜底。但有两种情况戳会丢:
 *
 *   ① 页面把行对象**重新拼**成新对象(而不是展开)——
 *      例如 `pages/system/user/index.tsx` 的 `editInitialValues`
 *      是逐字段手抄的 ⇒ 戳没了。
 *   ② 页面把分页信封**重新拼**成新对象(行还是原来那些,信封不是)——
 *      例如 `pages/file/list.tsx`。
 *
 * ⇒ 兜底一份"这个页面最近收到的报告"。
 *
 * ⚠️ **按页面路径归档,是为了不串页** ——
 *    用户页的报告不能拿去套部门页的表单(那会把不该禁用的字段禁用掉)。
 *    路径取自 `window.location.pathname`(SPA 用 `createBrowserRouter`),
 *    **请求发起时**记下、**响应回来时**归档 ⇒ 页面跳走了也不会串。
 *
 * ⚠️ **失败的代价是"多限制",不是"泄露"** ——
 *    兜底报告套错了 ⇒ 多半是把某个字段多禁用了(用户看得见它是灰的),
 *    不会把该藏的字段放出来。
 */
let latestReport: { path: string; report: FieldPolicyReport } | null = null

/** 当前页面路径 —— 拿不到就当空串(测试环境 / 非浏览器) */
export function currentPagePath(): string {
  try {
    if (typeof window !== 'undefined' && window.location) return window.location.pathname
  } catch {
    /* 忽略:非浏览器环境 */
  }
  return ''
}

/** 记下"这个页面最近收到的一份报告" */
export function setLatestFieldPolicyReport(path: string, report: FieldPolicyReport): void {
  latestReport = { path, report }
}

/**
 * 取"这个页面最近收到的一份报告"
 *
 * ⚠️ **路径对不上就返 null** —— 宁可没有保护(退回 M168 之前的行为),
 *    也不要拿别的页面的规则来禁自己的字段。
 */
export function getLatestFieldPolicyReport(path?: string): FieldPolicyReport | null {
  if (!latestReport) return null
  const p = path ?? currentPagePath()
  if (latestReport.path !== p) return null
  return latestReport.report
}

/**
 * 解析 `X-Field-Policy` 响应头
 *
 * 格式:`hidden=phone,id_card;masked=real_name`
 *
 * ⚠️ **认不出来的段一律丢掉,不猜** ——
 *    猜错 = 把该显示的字段藏了(或者反过来)。宁可少一份报告。
 *
 * ⚠️ **空报告返 null**(而不是空对象)——
 *    调用方据此判断"这次响应没有字段级可见性参与"。
 */
export function parseFieldPolicyHeader(raw: string | undefined | null): FieldPolicyReport | null {
  if (!raw) return null
  const hidden: string[] = []
  const masked: string[] = []
  for (const part of raw.split(';')) {
    const seg = part.trim()
    if (!seg) continue
    const eq = seg.indexOf('=')
    if (eq <= 0) continue
    const key = seg.slice(0, eq).trim().toLowerCase()
    const names = seg
      .slice(eq + 1)
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
    if (names.length === 0) continue
    if (key === 'hidden') hidden.push(...names)
    else if (key === 'masked') masked.push(...names)
  }
  if (hidden.length === 0 && masked.length === 0) return null
  return { hidden, masked }
}

/**
 * 从一个对象上读回报告
 *
 * ⚠️ **只认"盖在对象自己身上"的那一份** ——
 *    不去遍历对象树找(那是 `stampFieldPolicyReport` 的活)。
 */
export function readFieldPolicyReport(value: unknown): FieldPolicyReport | null {
  if (!value || typeof value !== 'object') return null
  const holder = value as unknown as Record<symbol, unknown>
  const raw = holder[FIELD_POLICY_SYMBOL]
  if (!raw || typeof raw !== 'object') return null
  const report = raw as Partial<FieldPolicyReport>
  const hidden = Array.isArray(report.hidden) ? report.hidden.filter(isNonEmptyString) : []
  const masked = Array.isArray(report.masked) ? report.masked.filter(isNonEmptyString) : []
  if (hidden.length === 0 && masked.length === 0) return null
  return { hidden, masked }
}

/** 把报告盖到一棵对象树上(就地改,返回同一个引用) */
export function stampFieldPolicyReport<T>(value: T, report: FieldPolicyReport): T {
  if (!report) return value
  if (report.hidden.length === 0 && report.masked.length === 0) return value
  stampTree(value, report, 0, new WeakSet<object>())
  return value
}

/**
 * 递归盖章的深度上限
 *
 * ⚠️ **必须有上限** —— 响应 JSON 理论上无环,但一份被塞了环的对象
 *    会让递归永远不结束(页面白屏)。
 */
const MAX_STAMP_DEPTH = 8

function stampTree(value: unknown, report: FieldPolicyReport, depth: number, seen: WeakSet<object>): void {
  if (!value || typeof value !== 'object') return
  if (depth > MAX_STAMP_DEPTH) return
  const obj = value as object
  if (seen.has(obj)) return
  seen.add(obj)
  defineStamp(obj, report)
  if (Array.isArray(value)) {
    for (const item of value) stampTree(item, report, depth + 1, seen)
    return
  }
  for (const item of Object.values(value as Record<string, unknown>)) {
    stampTree(item, report, depth + 1, seen)
  }
}

/**
 * 盖戳
 *
 * ⚠️ **`enumerable: true` 是刻意的** —— 见 `FIELD_POLICY_SYMBOL` 的注释:
 *    不可枚举的话 `{...row}` 就把报告丢了。
 *
 * ⚠️ **失败要吞掉** —— 冻结的对象(`Object.freeze`)上 `defineProperty` 会抛,
 *    而"盖不上戳"只该退化成"没有字段级保护",不该让整个请求失败。
 */
function defineStamp(obj: object, report: FieldPolicyReport): void {
  try {
    Object.defineProperty(obj, FIELD_POLICY_SYMBOL, {
      value: report,
      enumerable: true,
      configurable: true,
      writable: true,
    })
  } catch {
    /* 盖不上就算了(见上面的注释) */
  }
}

function isNonEmptyString(v: unknown): v is string {
  return typeof v === 'string' && v.length > 0
}
