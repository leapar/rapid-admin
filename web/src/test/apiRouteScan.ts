/**
 * 前端 API 调用 ↔ 后端路由 对齐扫描 —— 给"前端打错 URL = 404"守门测试用
 *
 * ## 这条网守的是什么
 *
 *   前端的 API 路径是**相对 baseURL** 的(baseURL = `/rapid-admin/v1`,见
 *   `src/api/baseURL.ts` + `.env.development` 的 `VITE_API_BASE_ADMIN`),
 *   所以 `http.get('/issues')` 实际打的是 `GET /rapid-admin/v1/issues`。
 *
 *   后端把业务端点挂在 `biz` 组下(`api.Group("/biz")`),
 *   真正的路径是 `GET /rapid-admin/v1/biz/issues` —— **差一个 `/biz`**。
 *
 *   这种错**在运行时是完全静默的**:axios 收到 404 → 拦截器弹一个
 *   "资源不存在"的 toast → 页面其它部分照常渲染 → CI 全绿。
 *   2026-10-26 用户报"问题列表打开是 404",查下来就是它:
 *   5 个 api 模块(asset/house/issue/owner/upload)的 **77 处调用**全漏了
 *   group 前缀,每一个都必然 404。
 *
 * ## 为什么用 TypeScript 编译器 API 而不是正则
 *
 *   URL 字面量有三种形态,正则要分别处理,而且**每种都有坑**:
 *   · 普通串   `http.get('/issues')`
 *   · 模板串   http.get(`/issues/${id}`)   ← 路径参数
 *   · 模板串里带条件表达式(查询串) ——
 *     http.get(`/system/resource/accessible${typeFilter ? `?type=${typeFilter}` : ''}`)
 *     这里的 `${...}` **不是路径参数**,是查询串片段;当成参数就会凭空
 *     多出一段,匹配全错。
 *
 *   用 `ts.createSourceFile` 拿 AST,注释根本不在树里(不会把注释里的
 *   示例 URL 当成真调用),节点类型也是确定的。
 *
 * ## ⚠️ Go 侧的 group 前缀必须**沿父链解析**
 *
 *   router.go 里是嵌套的:
 *     api := h.Group("/rapid-admin/v1")
 *     biz := api.Group("/biz")     ← 前缀是 "/rapid-admin/v1/biz",不是 "/biz"
 *     sys := api.Group("/system")
 *   只看一层会把 `biz` 当成 "/biz" → 与前端的 "/rapid-admin/v1" 基准对不上
 *   → **整张网全是空的**(全对不上),或者反过来全对得上。
 */
import ts from 'typescript'

/** 前端 baseURL —— 前端写的路径都是相对它的 */
export const API_BASE = '/rapid-admin/v1'

/* ====================== 后端路由 ====================== */

/** 后端一条路由 */
export interface GoRoute {
  /** HTTP 方法 */
  method: string
  /** 组名(biz / sys / platform / public / api) */
  group: string
  /** 完整路径,如 `/rapid-admin/v1/biz/issues/:id` */
  path: string
}

/**
 * 解析 router.go,取出全部路由。
 *
 * 只认 `xxx.GET("...")` 这种**注册调用** —— 后端路由全部是这么写的。
 */
export function parseGoRoutes(go: string): GoRoute[] {
  // ① group 声明:x := y.Group("前缀")
  const decl = new Map<string, { parent: string; sub: string }>()
  const declRe = /(\w+)\s*:=\s*(\w+)\.Group\("([^"]*)"/g
  let m: RegExpExecArray | null
  while ((m = declRe.exec(go)) !== null) {
    decl.set(m[1]!, { parent: m[2]!, sub: m[3]! })
  }

  // ② 沿父链解析出绝对前缀
  const resolve = (name: string, seen = new Set<string>()): string => {
    if (seen.has(name)) return ''
    seen.add(name)
    const d = decl.get(name)
    if (!d) return '' // 'h' = 根 group
    const parent = d.parent === 'h' ? '' : resolve(d.parent, seen)
    return parent + d.sub
  }

  const out: GoRoute[] = []
  const routeRe = /\b(\w+)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]*)"/g
  while ((m = routeRe.exec(go)) !== null) {
    const group = m[1]!
    if (!decl.has(group)) continue // 不是 group 上的注册(例如 h.GET)→ 跳过
    out.push({ method: m[2]!, group, path: resolve(group) + m[3]! })
  }
  return out
}

/* ====================== 前端调用 ====================== */

/** 前端一处 API 调用 */
export interface HttpCall {
  /** 行号(1 起) */
  line: number
  /** HTTP 方法 */
  method: string
  /**
   * 归一化后的路径(相对 baseURL),形如 `/biz/issues/:p`。
   * 路径参数统一记成 `:p` —— 只关心**段数**和**字面量段**,
   * 参数叫什么对匹配没影响。
   */
  path: string
}

/** 这个表达式是不是"一个路径参数" */
function isPathParamExpr(e: ts.Expression): boolean {
  // `id` / `data.ownerId` / `arr[0]` —— 值是什么不重要,重要的是它占一段
  if (ts.isIdentifier(e) || ts.isPropertyAccessExpression(e) || ts.isElementAccessExpression(e)) {
    return true
  }
  // `encodeURIComponent(id)` —— 还是"一段路径参数",只是外面多包了一层 URL 编码。
  // ⚠️ 只认这两个标准编码函数:别把**任何**调用表达式都当成参数 ——
  //   条件表达式那种"其实是查询串片段"的写法(见文件头注释)必须排除在外。
  if (ts.isCallExpression(e) && ts.isIdentifier(e.expression)) {
    return e.expression.text === 'encodeURIComponent' || e.expression.text === 'encodeURI'
  }
  return false
}

/**
 * 把一个字符串字面量 / 模板字面量归一化成路径。
 *
 * 返回 null 表示"这个字面量不是路径"(比如空串)。
 */
export function literalToPath(lit: ts.Node): string | null {
  if (ts.isStringLiteral(lit) || ts.isNoSubstitutionTemplateLiteral(lit)) {
    return lit.text
  }
  if (ts.isTemplateExpression(lit)) {
    let s = lit.head.text
    for (const span of lit.templateSpans) {
      // 条件表达式 / 函数调用这类不是路径参数(多半是查询串片段)
      if (isPathParamExpr(span.expression)) s += ':p'
      s += span.literal.text
    }
    return s
  }
  return null
}

/**
 * 找出源码里所有 `http.<verb>(...)` 调用的 URL。
 *
 * 只认 `http` 这个对象的 get/post/put/patch/delete —— `http.raw` 是
 * axios 实例本身(用于下载 / 上传进度),不是普通调用。
 */
export function findHttpCalls(src: string, fileName = 'x.ts'): HttpCall[] {
  const kind = fileName.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  const sf = ts.createSourceFile(fileName, src, ts.ScriptTarget.Latest, true, kind)
  const out: HttpCall[] = []
  const VERBS = new Set(['get', 'post', 'put', 'patch', 'delete'])

  const walk = (node: ts.Node): void => {
    if (
      ts.isCallExpression(node) &&
      ts.isPropertyAccessExpression(node.expression) &&
      ts.isIdentifier(node.expression.expression) &&
      node.expression.expression.text === 'http' &&
      VERBS.has(node.expression.name.text) &&
      node.arguments.length > 0
    ) {
      const arg = node.arguments[0]!
      const raw = literalToPath(arg)
      if (raw !== null) {
        const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf))
        out.push({
          line: line + 1,
          method: node.expression.name.text.toUpperCase(),
          path: normalizePath(raw),
        })
      }
    }
    ts.forEachChild(node, walk)
  }
  walk(sf)
  return out
}

/**
 * 归一化路径:
 *   · 去掉查询串(**它不参与路由匹配**)
 *   · 补上前导 `/`
 */
export function normalizePath(p: string): string {
  let x = p.split('?')[0] ?? ''
  if (!x.startsWith('/')) x = '/' + x
  return x
}

/* ====================== 匹配 ====================== */

/**
 * 一条前端路径能不能命中一条后端路由。
 *
 * 段数必须相同,而且**逐段要么都是参数、要么都是字面量**:
 *   · 两边都是 `:param` → 对上(参数名不一样没关系,URL 里根本没有参数名)
 *   · 两边都是字面量   → 必须**逐字相同**
 *
 * ⚠️ **后端 `:param` 不吃前端的字面量段** —— 这是刻意的。
 *   后端有 `GET /biz/issues/:id`,前端写 `GET /biz/issues/statistics`,
 *   看着"能对上",实际会打到 `GetIssue` 并把 `"statistics"` 当成 id,
 *   拿到一个"问题不存在" —— **跟 404 一样难受,但更难查**。
 *   所以字面量段必须逐字对上,才算这条前端调用真的通。
 */
export function routeMatches(backendPath: string, frontendPath: string): boolean {
  const a = backendPath.split('/')
  const b = frontendPath.split('/')
  if (a.length !== b.length) return false
  return a.every((seg, i) => {
    const f = b[i]!
    if (seg.startsWith(':') && f.startsWith(':')) return true
    return seg === f
  })
}

/**
 * 前端路径(相对 baseURL)能不能命中一条后端路由。
 */
export function findMatch(routes: GoRoute[], method: string, frontendPath: string): GoRoute | null {
  const full = API_BASE + frontendPath
  for (const r of routes) {
    if (r.method === method && routeMatches(r.path, full)) return r
  }
  return null
}
