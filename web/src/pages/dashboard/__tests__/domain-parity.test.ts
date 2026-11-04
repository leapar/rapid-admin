/**
 * 看板的域清单:前端画的 ⟷ 后端能给的 必须**逐个字段**对齐
 *
 * ## 这条网守的是什么
 *
 *   看板内容按域权限过滤:服务端把**没权限的域整个从响应里省掉**
 *   (`Overview` 的字段是 `*int64` + `omitempty`
 *   ⇒ JSON 里压根没有这个 key),前端据此决定"这张卡片要不要画"。
 *
 *   这条约定横跨**四份清单**,任何一份对不上都是**静默故障**:
 *
 *   ① 后端 `Overview` 结构体的 json tag —— 服务端能给出哪些指标
 *   ② 后端 `dashboardMetricPerms` 的 key —— 每个指标要哪个权限码
 *   ③ 前端 `DashboardOverview` 接口的字段 —— 前端**以为**能拿到哪些
 *   ④ 前端 `DASHBOARD_KPI_KEYS` —— 前端**实际**会画哪些
 *
 *   少一个 ⇒ **后端给了、前端不画**(那个指标永远看不见);
 *   多一个 ⇒ **前端画一张永远空的卡**。
 *
 *   ⚠️ 四条对不上,**没有任何东西会报错**:
 *   TypeScript 的接口是前端自己写的(后端加字段它不知道),
 *   axios 层是纯透传(后端少给字段它也不报),
 *   页面上少一张卡片 / 多一张空卡片 —— **测试全绿**。
 *   ⇒ 这条网就是那个"东西"。
 *
 * ## 方向:后端 ⊇ 前端(子集断言)
 *
 *   后端 `Overview` 里可以有前端不画的指标(平台裁剪节奏与前端不同步),
 *   但**前端画的每一张卡,后端都必须真的能给** ——
 *   否则那张卡永远是 "-"。所以:
 *
 *   · 后端 tag  ⟷ 前端清单:**后端 ⊇ 前端**;
 *   · 权限表    ⟷ 前端清单:**权限表 ⊇ 前端**(少一个键 ⇒ 那个指标
 *     vis.Allows("") 恒 false,永远不出现,而且不报错);
 *   · 前端接口  ⟷ 前端清单:**完全一致**(接口少一个字段 ⇒ 那张卡恒 "-")。
 *
 * ## 为什么直接读后端 Go 源码
 *
 *   跨语言的契约只能靠"读另一边的源码"来比对 ——
 *   前端拿不到后端的结构体定义,后端也不知道前端画哪些。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 前端 `domains.ts` 里加一个后端给不出的 key
 *     → 「后端结构体 ⊇ 前端清单」红;
 *   · 后端 `dashboardMetricPerms` 里删掉一个前端在画的 key
 *     → 「权限表 ⊇ 前端清单」红;
 *   · 前端 `api/dashboard.ts` 的接口里加/删一个字段
 *     → 「前端接口 ⟷ 前端清单」红。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { DASHBOARD_KPI_KEYS } from '../domains'

/** `web/src` */
const SRC_ROOT = join(__dirname, '..', '..', '..')
/** 仓库根 */
const REPO_ROOT = join(SRC_ROOT, '..', '..')

const BACKEND_DASHBOARD_GO = join(REPO_ROOT, 'server', 'internal', 'service', 'dashboard.go')
const FRONTEND_DASHBOARD_API = join(SRC_ROOT, 'api', 'dashboard.ts')

const backendSrc = readFileSync(BACKEND_DASHBOARD_GO, 'utf8')
const frontendSrc = readFileSync(FRONTEND_DASHBOARD_API, 'utf8')

/** Go 结构体体 → 它的 json tag 列表 */
function goStructJsonTags(src: string, structName: string): string[] {
  const m = new RegExp(`type\\s+${structName}\\s+struct\\s*\\{([\\s\\S]*?)\\n\\}`).exec(src)
  if (!m) throw new Error(`后端 dashboard.go 里找不到 struct ${structName}`)
  const out: string[] = []
  const re = /json:"([a-z0-9_]+)/g
  let hit: RegExpExecArray | null
  while ((hit = re.exec(m[1]!)) !== null) out.push(hit[1]!)
  return out
}

/** Go `map[string]string{...}` → 它的 key 列表 */
function goMapKeys(src: string, varName: string): string[] {
  const m = new RegExp(
    `var\\s+${varName}\\s*=\\s*map\\[string\\]string\\{([\\s\\S]*?)\\n\\}`,
  ).exec(src)
  if (!m) throw new Error(`后端 dashboard.go 里找不到 map ${varName}`)
  const out: string[] = []
  const re = /"([a-z0-9_]+)"\s*:/g
  let hit: RegExpExecArray | null
  while ((hit = re.exec(m[1]!)) !== null) out.push(hit[1]!)
  return out
}

/** TS `interface X { ... }` → 它的字段名列表 */
function tsInterfaceFields(src: string, name: string): string[] {
  const m = new RegExp(`export\\s+interface\\s+${name}\\s*\\{([\\s\\S]*?)\\n\\}`).exec(src)
  if (!m) throw new Error(`前端 api/dashboard.ts 里找不到 interface ${name}`)
  const out: string[] = []
  // ⚠️ 只认"行首 + 标识符 + 可选 ? + 冒号" —— 注释行以 `//` / `*` / `/**` 开头,
  //   都不匹配(注释里出现的 `owner:list` 之类不会被误当成字段)。
  const re = /^[ \t]*([A-Za-z_][A-Za-z0-9_]*)\??[ \t]*:/gm
  let hit: RegExpExecArray | null
  while ((hit = re.exec(m[1]!)) !== null) out.push(hit[1]!)
  return out
}

/** 比对用:排序后的数组 */
function sorted(xs: readonly string[]): string[] {
  return [...xs].sort()
}

/** 后端列表 ⊇ 前端清单(前端画的每一张卡,后端都必须真的能给) */
function superset(backend: readonly string[], frontend: readonly string[]): string[] {
  const have = new Set(backend)
  return frontend.filter((k) => !have.has(k))
}

describe('看板域清单 · 前后端逐字段对齐', () => {
  it('扫描器本身没失效(这张网不是空的)', () => {
    // 网空了就等于没有网 —— 先证明网里有东西
    expect(goStructJsonTags(backendSrc, 'Overview').length).toBeGreaterThan(0)
    expect(goMapKeys(backendSrc, 'dashboardMetricPerms').length).toBeGreaterThan(0)
    expect(tsInterfaceFields(frontendSrc, 'DashboardOverview').length).toBeGreaterThan(0)
    expect(DASHBOARD_KPI_KEYS.length).toBeGreaterThan(0)
  })

  it('后端 Overview 的字段 ⊇ 前端会画的 KPI 清单', () => {
    const missing = superset(goStructJsonTags(backendSrc, 'Overview'), DASHBOARD_KPI_KEYS)
    expect(missing, `前端要画、但后端 Overview 给不出的指标: ${missing.join(', ')}`).toEqual([])
  })

  it('后端权限表 dashboardMetricPerms 的键 ⊇ 前端 KPI 清单', () => {
    // ⚠️ 权限表少一个键 ⇒ 那个指标**永远不出现**(vis.Allows("") 恒 false),
    //   而且**不报错** —— 与其错的是"少画一张卡",不如直接红。
    const missing = superset(goMapKeys(backendSrc, 'dashboardMetricPerms'), DASHBOARD_KPI_KEYS)
    expect(missing, `前端要画、但权限表没登记的指标: ${missing.join(', ')}`).toEqual([])
  })

  it('前端 DashboardOverview 接口的字段 == 前端 KPI 清单', () => {
    // 接口少一个字段 ⇒ `overview?.[key]` 恒 undefined ⇒ 那张卡永远是 "-"
    expect(sorted(tsInterfaceFields(frontendSrc, 'DashboardOverview'))).toEqual(
      sorted(DASHBOARD_KPI_KEYS),
    )
  })
})
