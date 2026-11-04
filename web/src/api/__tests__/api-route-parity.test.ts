/**
 * 前端 API 调用 ↔ 后端路由 对齐(2026-10-26)
 *
 * ## 这条网守的是什么
 *
 *   前端写的路径是**相对 baseURL** 的(baseURL = `/rapid-admin/v1`),
 *   而后端把端点挂在 group 下(如 `system` 组)—— 少写一个 group 前缀,
 *   真正的路径就差一截。
 *
 *   这种错在运行时是**完全静默**的:axios 收到 404 → 弹一个"资源不存在"
 *   的 toast → 页面其它部分照常渲染 → CI 全绿。没有任何东西会红。
 *   (2026-10-26 在业务模块上就实际发生过:五个 api 模块的 77 处调用
 *   全漏了 group 前缀,每一个都必然 404。)
 *
 * ## 为什么值得单独立一条测试
 *
 *   这条约定同样只靠人记着:每加一个 api 模块,都得记得"路径要带 group 前缀"。
 *   而漏了不会编译报错、不会 lint 报错、不会让任何测试变红 —— 直到用户点那个页面。
 *   本测试就是那个"东西"。
 *
 * ## 白名单:后端**还没实现**的端点
 *
 *   有一批端点是前端先写、后端待补的(前端代码里都有 TODO 注释)。
 *   它们照样 404,但**不是这条网该管的** —— 那是"后端欠一个端点",
 *   不是"前端路径写错了"。所以:
 *   · 白名单里的端点**必须带 TODO 注释**,并在 docs/框架层未完成项清单v1.md
 *     里有对应记录;
 *   · 白名单**只允许缩小**:后端把端点补上之后,这条断言会要求把它从
 *     白名单里删掉(否则就是一条永远过期的豁免)。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   把任一 `http.get('/biz/xxx')` 的 `/biz` 去掉 → 本文件立刻红,
 *   报出是哪个文件的哪一行打到了哪个不存在的路径。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { parseGoRoutes, findHttpCalls, findMatch, normalizePath } from '@/test/apiRouteScan'

/** `web/src` */
const SRC_ROOT = join(__dirname, '..', '..')
/** 仓库根 */
const REPO_ROOT = join(SRC_ROOT, '..', '..')
/** 后端路由表 */
const ROUTER_GO = join(REPO_ROOT, 'server', 'internal', 'api', 'router', 'router.go')
/** 前端 api 目录 */
const API_DIR = join(SRC_ROOT, 'api')

/**
 * 后端还没实现的端点(白名单)。
 *
 * 每一项的格式是 `<文件> <METHOD> <路径>`。
 *
 * ⚠️ **这些端点前端代码里都有 TODO 注释**,写明"后端暂无对应 endpoint"。
 *   补后端端点时,要把 TODO 注释和这里的白名单**一起**删掉 ——
 *   本文件第 3 条断言会检查白名单里有没有"后端已经实现了却还留着"的过期条目。
 */
const BACKEND_NOT_IMPLEMENTED = new Set<string>([
  // ⚠️ 2026-10-30(M116)删掉了 'dashboard.ts GET /common/region/{...}':
  //   后端**从来没有** `common` 组,`regionApi` 零调用方,
  //   是一条"永远 404 但没人碰"的死代码 ⇒ 连同白名单一起删。
  // 角色
  'role.ts GET /system/roles/:p/users',
])

/** 后端路由表 */
const ROUTES = parseGoRoutes(readFileSync(ROUTER_GO, 'utf8'))

/** 前端 api 目录里的每个文件 → 它的全部调用 */
function collectCalls(): { file: string; method: string; path: string; line: number }[] {
  const out: { file: string; method: string; path: string; line: number }[] = []
  for (const name of readdirSync(API_DIR)) {
    if (!name.endsWith('.ts')) continue
    if (name.endsWith('.d.ts')) continue
    const src = readFileSync(join(API_DIR, name), 'utf8')
    for (const c of findHttpCalls(src, name)) {
      out.push({ file: name, method: c.method, path: c.path, line: c.line })
    }
  }
  return out
}

const CALLS = collectCalls()

describe('前端 API 调用 ↔ 后端路由 对齐', () => {
  it('解析器没失效(这张网不是空的)', () => {
    // 网空了就等于没有网 —— 先证明网里有东西
    expect(ROUTES.length).toBeGreaterThan(150)
    expect(ROUTES.some((r) => r.path === '/rapid-admin/v1/public/auth/login')).toBe(true)
    expect(ROUTES.some((r) => r.path === '/rapid-admin/v1/system/audit/operation-logs')).toBe(true)
    expect(CALLS.length).toBeGreaterThan(100)
  })

  it('每处前端调用都必须打得到后端的某条路由(或在后端未实现的白名单里)', () => {
    const bad: string[] = []
    for (const c of CALLS) {
      const key = `${c.file} ${c.method} ${c.path}`
      if (BACKEND_NOT_IMPLEMENTED.has(key)) continue
      if (findMatch(ROUTES, c.method, c.path)) continue
      bad.push(`${key}   ← ${c.file}:${c.line}`)
    }
    expect(bad).toEqual([])
  })

  it('白名单不能有陈旧条目(后端已经实现的必须从白名单里删掉)', () => {
    const stale: string[] = []
    for (const key of BACKEND_NOT_IMPLEMENTED) {
      const parts = key.split(' ')
      const method = parts[1]!
      const path = parts.slice(2).join(' ')
      if (findMatch(ROUTES, method, path)) stale.push(key)
    }
    expect(stale).toEqual([])
  })

  it('白名单里的每一条都真的还在被调用(别留一条永远用不到的豁免)', () => {
    const used = new Set(CALLS.map((c) => `${c.file} ${c.method} ${c.path}`))
    const unused: string[] = []
    for (const key of BACKEND_NOT_IMPLEMENTED) {
      if (!used.has(key)) unused.push(key)
    }
    expect(unused).toEqual([])
  })

  it('后端未实现的端点,源码里必须标着 TODO(缺口要写在代码旁边)', () => {
    // 上面的白名单是**机器可读**的缺口清单,源码里的 TODO 注释是**给人看的**。
    // 两者必须同时在 —— 只在白名单里,读代码的人不知道这个端点还没实现;
    // 只在 TODO 里,机器不知道,下次扫描就漏过去了。
    //
    // ⚠️ 2026-10-26 就是靠这条抓出来的:`GET /biz/issues/:id/comments`
    //   (拉评论列表)后端只有 POST、没有 GET,而前端那行**没有 TODO**,
    //   于是谁看代码都以为它通了。
    const bad: string[] = []
    for (const c of CALLS) {
      const key = `${c.file} ${c.method} ${c.path}`
      if (!BACKEND_NOT_IMPLEMENTED.has(key)) continue
      const lines = readFileSync(join(API_DIR, c.file), 'utf8').split(/\r?\n/)
      // 调用点**上面** 12 行内(覆盖 JSDoc 块)必须有 TODO
      const above = lines.slice(Math.max(0, c.line - 1 - 12), c.line - 1).join('\n')
      if (!above.includes('TODO')) bad.push(`${key}   ← ${c.file}:${c.line}`)
    }
    expect(bad).toEqual([])
  })

  it('源码里的 TODO 注释不能是过时的(后端已经实现的端点别再标 TODO)', () => {
    // 反方向的漂移:后端把端点补上了,前端的 TODO 没删 —— 读代码的人会以为
    // 这个端点还没实现,于是重复实现一遍,或者干脆绕开它。
    // 2026-10-26 一次清掉 5 条(dashboard 的 trend/todos/messages、
    // POST /biz/notices、GET /system/resources)。
    //
    // ⚠️ 只认 `TODO: 后续在 server 注册 <METHOD> <path>` 这种标准写法;
    //   没写方法的(比如 `/common/region/{provinces,cities,areas}` 这种
    //   一次列三个的)不在本条断言的射程内,别把它当成"全网都查过了"。
    const bad: string[] = []
    for (const f of readdirSync(API_DIR)) {
      if (!f.endsWith('.ts')) continue
      const lines = readFileSync(join(API_DIR, f), 'utf8').split(/\r?\n/)
      lines.forEach((line, i) => {
        if (!line.includes('TODO: 后续在 server 注册')) return
        const m = /注册\s+(GET|POST|PUT|PATCH|DELETE)\s+(\/\S+)/.exec(line)
        if (!m) return // 没写方法 —— 不在射程内
        const method = m[1]!
        const path = normalizePath(m[2]!.replace(/:[A-Za-z_][A-Za-z0-9_]*/g, ':p'))
        if (findMatch(ROUTES, method, path)) {
          bad.push(`${f}:${i + 1}  ${method} ${path}  ← 后端已实现,TODO 过期了`)
        }
      })
    }
    expect(bad).toEqual([])
  })
})
