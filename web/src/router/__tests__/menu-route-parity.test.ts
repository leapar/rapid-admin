/**
 * 后端菜单 seed 的 path 必须在前端路由表里都有对应路由(2026-10-26)
 *
 * ## 这条网守的是什么
 *
 *   侧边栏的菜单项**不是前端写死的** —— 它来自后端 `sys_resource` 里
 *   `menu.*` 那批资源,点一下就 `navigate(r.path)`(Sidebar.tsx 的 onClick)。
 *   所以**后端 seed 的 path 写错 = 前端点进去 404**,而前端这边毫无提示:
 *   路由表里没有这条 path,React Router 落到 `path: '*'` 兜底 → 404 页。
 *
 *   这类事故两处各自都合理,合起来就断链:一边改了菜单 path(或加了菜单),
 *   另一边的路由表没跟上 —— 点侧边栏就是 404,CI 全绿。
 *
 * ## 为什么值得单独立一条测试
 *
 *   seed.go 的注释自己写着「与 web/src/router/routes.tsx 一一对应」——
 *   这条**约定**全靠人记着,没有任何东西守着它。本测试就是那个"东西"。
 *
 *   ⚠️ 方向只能是「后端菜单 ⇒ 前端必须有路由」,**不能反过来**:
 *   前端有一批**故意没有菜单**的路由(个人中心 / 配置审计子页 / 详情页
 *   `/approval/:id`、`/flow/:id`),反过来断言会误报。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   把 `routes.tsx` 里任意一条菜单路由删掉 → 本文件立刻变红,
 *   报错会指名道姓说是哪条菜单 path 没有对应路由。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import type { RouteObject } from 'react-router-dom'
import { routes } from '@/router/routes'

/** 源码根 = `web/src`(本文件在 `src/router/__tests__/` 下) */
const SRC_ROOT = join(__dirname, '..', '..')

/** 仓库根 = `web/src` 的上两级 */
const REPO_ROOT = join(SRC_ROOT, '..', '..')

/** 后端菜单 seed */
const SEED_GO = join(REPO_ROOT, 'server', 'internal', 'seed', 'seed.go')

/**
 * 取出后端 `menuList()` 里的所有菜单 path。
 *
 * ⚠️ **必须限定在 menuList() 函数体内** —— seed.go 里还有 `MenuDef` 结构体的
 *   `Path string` 字段(没有引号)和 `Path: m.Path` 这样的字段拷贝。
 *   正则要求 `Path: "..."`(带引号)已经能滤掉它们,但限定函数体更稳:
 *   以后有人在别处加一个带引号的 Path 字面量,不会混进菜单清单。
 */
function parseMenuPaths(go: string): string[] {
  const start = go.indexOf('func menuList()')
  if (start < 0) throw new Error('seed.go 里找不到 menuList() —— 解析器失效,这张网成了空网')
  const end = go.indexOf('\n}', start)
  const body = go.slice(start, end < 0 ? undefined : end)
  const out: string[] = []
  const re = /Path:\s*"([^"]*)"/g
  let m: RegExpExecArray | null
  while ((m = re.exec(body)) !== null) out.push(m[1]!)
  return out
}

/** 把路由树摊平成"绝对路径"列表 */
function flattenRoutes(list: RouteObject[], base = ''): string[] {
  const out: string[] = []
  for (const r of list) {
    // '*' 是兜底路由,不参与匹配
    if (typeof r.path !== 'string' || r.path === '*') continue
    const full = r.path.startsWith('/') ? r.path : `${base.replace(/\/$/, '')}/${r.path}`
    out.push(full)
    if (r.children?.length) out.push(...flattenRoutes(r.children, full))
  }
  return out
}

const MENU_PATHS = parseMenuPaths(readFileSync(SEED_GO, 'utf8'))
const ROUTE_PATHS = flattenRoutes(routes as unknown as RouteObject[])

describe('后端菜单 path ↔ 前端路由 对齐', () => {
  it('解析器没失效(这张网不是空的)', () => {
    // 网空了就等于没有网 —— 先证明网里有东西
    expect(MENU_PATHS.length).toBeGreaterThan(20)
    expect(MENU_PATHS).toContain('/system/user')
    expect(MENU_PATHS).toContain('/audit/operation')
    expect(ROUTE_PATHS.length).toBeGreaterThan(30)
  })

  it('每条后端菜单 path 都必须有对应的前端路由', () => {
    const missing = MENU_PATHS.filter((p) => !ROUTE_PATHS.includes(p))
    expect(missing).toEqual([])
  })

  it('回归:一级菜单的 Navigate 重定向目标必须真实存在(404 教训)', () => {
    // 侧边栏一级菜单是纯重定向(/system → /system/user 等),
    // 重定向目标在路由表里不存在 = 点菜单 404
    expect(ROUTE_PATHS).toContain('/system/user')
    expect(ROUTE_PATHS).toContain('/file/list')
    expect(ROUTE_PATHS).toContain('/audit/operation')
    expect(ROUTE_PATHS).toContain('/approval')
  })
})
