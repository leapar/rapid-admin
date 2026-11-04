/**
 * 角色 code 契约测试 —— 前端 `RoleCode` 必须与后端 `sys_role.code` 逐字一致
 *
 * ## 这条网守的是什么
 *
 *   `web/src/constants/roles.ts` 里那套角色 code,以前是**前端自己编的一套**
 *   (`role_platform_super_admin` / `role_business_lead` / `role_customer_service`
 *   / `role_house_manager` / `role_asset_manager` / `role_material_reviewer`
 *   / `role_material_editor` / `role_delivery_manager` / `role_delivery_staff`)——
 *   **后端从来没有签发过其中任何一个**。
 *
 *   写错不会报错,只会让三处功能**静默死掉**:
 *     ① IdentitySwitcher 的 `HIGH_RISK_ROLES` 里没有真超管
 *        → **切超管身份不需要短信二次验证**(安全洞);
 *     ② 用户列表「重置密码」按钮的 `roleAnyOf: [RoleCode.SUPER_ADMIN]`
 *        → `hasRole` 恒 false → **这个按钮对谁都渲染不出来**;
 *     ③ `routes.tsx` 里 13 处 `anyOf={[perm, RoleCode.SUPER_ADMIN]}`
 *        → 路由守卫的"或超管"兜底恒不命中。
 *
 * ## 为什么直接解析后端源码(而不是抄一份常量)
 *
 *   抄一份 = 再造一个会漂移的副本 —— 这正是这个 bug 的成因。
 *   直接读 `model/enums.go` + `seed.go` 的 `roleList()`:
 *   **后端加角色 / 改 code,这条网立刻变红**,前端必须跟。
 *
 *   `roleList()` 才是权威(真正落库的那 13 个);
 *   `enums.go` 里第 14 个常量 `role_auditor` 是**兼容旧名的别名,从来没 seed 过**
 *   (后端 `seed/approval_seed_test.go` 里专门断言了这一点)—— 所以前端也不该有它。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { RoleCode, ROLE_NAME_MAP } from '@/constants/roles'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

/** 源码根 = `web/src`(本文件在 `src/constants/__tests__/` 下) */
const SRC_ROOT = join(__dirname, '..', '..')
/** 仓库根 = `web/src` 的上两级 */
const REPO_ROOT = join(SRC_ROOT, '..', '..')

const ENUMS_GO = join(REPO_ROOT, 'server', 'internal', 'model', 'enums.go')
const SEED_GO = join(REPO_ROOT, 'server', 'internal', 'seed', 'seed.go')

/**
 * 后端 Go 常量名 → code
 *
 * `model/enums.go` 里是 `RoleSuperAdmin = "role_super_admin"` 这种形式。
 */
function parseRoleConstants(go: string): Map<string, string> {
  const map = new Map<string, string>()
  const re = /^\s*Role([A-Za-z0-9_]+)\s*=\s*"(role_[a-z_]+)"/gm
  let m: RegExpExecArray | null
  while ((m = re.exec(go)) !== null) map.set(`Role${m[1]!}`, m[2]!)
  return map
}

/**
 * 后端**真正落库**的角色 code(取自 `seed.go` 的 `roleList()`)
 *
 * ⚠️ 必须限定在 `roleList()` 函数体内 —— enums.go 里那 14 个常量不是全部落库的。
 * ⚠️ 截断用的是"下一个 `func` 声明",**不是**第一个 `\n}`:
 *   `roleList()` 的返回类型是**匿名 struct**(`) []struct {`),
 *   第一个 `\n}` 是那段类型声明的结尾 —— 按它截会只拿到一段类型声明,
 *   里面一个角色都没有(解析器"看着能跑"但恒为空网)。
 */
function parseSeededRoleCodes(go: string, consts: Map<string, string>): string[] {
  const start = go.indexOf('func roleList()')
  if (start < 0) throw new Error('seed.go 里找不到 roleList() —— 解析器失效,这张网成了空网')
  const nextFn = go.indexOf('\nfunc ', start + 1)
  const body = go.slice(start, nextFn < 0 ? undefined : nextFn)
  const out: string[] = []
  const re = /model\.(Role[A-Za-z0-9_]+)/g
  let m: RegExpExecArray | null
  while ((m = re.exec(body)) !== null) {
    const name = m[1]!
    // `model.RoleTypePlatform` / `model.RoleTypeBusiness` 是**角色分类**常量,
    //   不是角色 code —— 跳过。其它不认识的名字一律当拼写错,抛错。
    if (name.startsWith('RoleType')) continue
    const code = consts.get(name)
    if (!code) throw new Error(`roleList() 里的 ${name} 在 model/enums.go 里找不到对应常量`)
    out.push(code)
  }
  return out
}

const CONSTS = parseRoleConstants(readFileSync(ENUMS_GO, 'utf8'))
const SEEDED = parseSeededRoleCodes(readFileSync(SEED_GO, 'utf8'), CONSTS)

/** 前端这一套(以前是错的 13 个) */
const FRONTEND = Object.values(RoleCode)

/** 以前那套"后端从来没签发过"的 code —— 一条都不该再出现 */
const LEGACY_CODES = [
  'role_platform_super_admin',
  'role_business_lead',
  'role_customer_service_manager',
  'role_customer_service',
  'role_house_manager',
  'role_asset_manager',
  'role_material_reviewer',
  'role_material_editor',
  'role_delivery_manager',
  'role_delivery_staff',
]

describe('角色 code 契约 · 前端 ↔ 后端', () => {
  it('解析器没失效(这张网不是空的)', () => {
    // 网空了就等于没有网 —— 先证明网里有东西
    expect(CONSTS.size).toBeGreaterThanOrEqual(3)
    expect(SEEDED.length).toBeGreaterThanOrEqual(3)
    expect(SEEDED).toContain('role_super_admin')
    // 框架开源版前端只留 3 个角色(超管 / 平台运营 / 只读)
    expect(FRONTEND.length).toBe(3)
  })

  it('前端 RoleCode 是后端 roleList() 落库 code 的子集(方向:前端 ⊆ 后端)', () => {
    // 框架开源版:后端可能还带着业务角色(裁剪节奏不同步),
    // 但前端注册的每一个 code 都必须是后端真的会签发的 ——
    // 写一个后端没有的 code 会让 HIGH_RISK_ROLES / roleAnyOf 静默失效。
    const seeded = new Set(SEEDED)
    const unknown = FRONTEND.filter((code) => !seeded.has(code))
    expect(unknown).toEqual([])
  })

  it('兼容别名 role_auditor 不许出现在任何一侧(从没 seed 过)', () => {
    // 旧后端 model/enums.go 里有个 RoleAuditor 别名,但 roleList() 从没 seed 过
    // —— 开源版后端连这个别名也删了,前端同样不该有。
    expect(CONSTS.get('RoleAuditor')).toBeUndefined()
    expect(SEEDED).not.toContain('role_auditor')
    expect(FRONTEND).not.toContain('role_auditor')
  })

  it('那套"后端从来没签发过"的旧 code 一条都不许残留', () => {
    for (const code of LEGACY_CODES) {
      expect(FRONTEND).not.toContain(code)
      expect(SEEDED).not.toContain(code)
      expect(Object.keys(ROLE_NAME_MAP)).not.toContain(code)
    }
  })

  it('ROLE_NAME_MAP 覆盖全部角色,且不多不少', () => {
    expect(Object.keys(ROLE_NAME_MAP).sort()).toEqual([...FRONTEND].sort())
  })

  it('每个角色名都能在 zh / en 两份 locale 里查到(不能 t() 落空显示 key)', () => {
    for (const code of FRONTEND) {
      const key = ROLE_NAME_MAP[code]
      expect(key, `${code} 没有登记角色名`).toBeTruthy()
      const sub = key!.slice('role.'.length)
      expect((zhCN.role as Record<string, string>)[sub], `zh 缺 ${key}`).toBeTruthy()
      expect((enUS.role as Record<string, string>)[sub], `en 缺 ${key}`).toBeTruthy()
    }
  })

  it('中英文角色名不是同一个 key(两份 locale 的 role 段必须对称)', () => {
    const zhKeys = Object.keys(zhCN.role).sort()
    const enKeys = Object.keys(enUS.role).sort()
    expect(zhKeys).toEqual(enKeys)
  })
})
