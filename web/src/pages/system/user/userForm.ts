/**
 * 用户编辑表单的纯逻辑(抽出来是为了能单测 —— 见 __tests__/userForm.test.ts)
 *
 * 背景(2026-10-25 用户报"用户编辑界面无法设置角色",顺着挖出来的四件事):
 *
 *  1. **角色字段压根没有**。
 *     2026-10-18 的注释写着"角色字段本期不做,完整身份管理作为独立 phase",
 *     于是编辑弹窗里只有账号/昵称/真实姓名/邮箱/手机号/部门/状态 —— 没有角色。
 *
 *  2. **请求体字段名全是 camelCase,后端一个都没收到**。
 *     前端发 departmentIds / primaryDepartmentId / initialPassword,
 *     后端绑的是 department_ids / primary_department_id / password。
 *     Go 的 encoding/json 只做大小写不敏感匹配、**不认下划线**,
 *     所以这些字段被**静默丢弃**(不报错!):
 *       · 新增用户缺 password → 后端直接返"密码不能为空"(新增功能整个是坏的)
 *       · 改部门 / 改状态 → 界面看着保存成功,其实一点没改
 *
 *  3. **identities 是"全量替换"语义,而且是清空重建**。
 *     后端 writeUserIdentitiesFromItems:先清空该用户全部 sys_user_identity,
 *     再按传入数组重建。所以"没改角色"也**绝对不能**把 identities 发出去 ——
 *     发一份不完整的(比如角色列表没加载出来)就等于把这个人的权限清零。
 *
 *  4. **列表 DTO 里没有 identities**。
 *     GET /system/users 返的是 UserListItem(没有 identities 字段),
 *     GET /system/users/:id 才 Preload 了 Identities。
 *     所以角色回填必须先拉详情 —— 只拿列表行去拼,拼出来永远是空。
 *
 * 本模块只做纯计算(不碰网络/不碰 React),三件事:
 *   · roleCodesOf      —— 从身份列表里取角色 code
 *   · mergeRoleIdentities —— 把"选中的角色"合并回原有身份(保留项目/部门/有效期)
 *   · buildUpdatePayload / buildCreatePayload —— 构造请求体
 */
import type { IdentityItem } from '@/types/permission'
import type { CreateUserRequest, UpdateUserRequest } from '@/api/user'

/** 身份(兼容后端两种形状:model.SysUserIdentity 与请求用的 IdentityItem) */
export interface IdentityLike {
  role_code: string
  role_name?: string
  project_id?: string
  dept_id?: string
  /** RFC3339 字符串;后端 model.Time 零值序列化成 null */
  valid_from?: string | null
  valid_to?: string | null
  is_primary?: boolean
}

/** EditModal 提交上来的表单值 */
export interface UserFormValues {
  username?: string
  nickname?: string
  email?: string
  phone?: string
  /** 字典 user_status:active / disabled / locked */
  status?: string
  /** 选中的角色 code(不是 role id) */
  roles?: string[]
  multiDept?: { departmentIds: string[]; primaryDepartmentId?: string }
  /** 明文初始密码(仅新增;提交前要 preHashPassword) */
  initialPassword?: string
}

/** 角色 code → 角色名(用于给新建的身份填 role_name 快照) */
export type RoleNameOf = (code: string) => string | undefined

/**
 * 从身份列表里取角色 code(去重、保持原有顺序)
 *
 * ⚠️ 去重是必要的:同一个角色可能有多条身份(绑不同项目),
 *    而多选框只能选一个 —— 不去重的话回填的选中项会多出来。
 */
export function roleCodesOf(identities?: IdentityLike[] | null): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const it of identities ?? []) {
    if (!it?.role_code) continue
    if (seen.has(it.role_code)) continue
    seen.add(it.role_code)
    out.push(it.role_code)
  }
  return out
}

/**
 * 两边选中的角色集合是否相同(忽略顺序、不看重复)
 *
 * 用于"角色没改就别发 identities" —— 后端是清空重建,发一次就是重写一次。
 */
export function sameRoleSet(
  existing: IdentityLike[] | null | undefined,
  selected: string[] | null | undefined,
): boolean {
  const a = new Set(roleCodesOf(existing))
  const b = new Set((selected ?? []).filter(Boolean))
  if (a.size !== b.size) return false
  for (const code of a) {
    if (!b.has(code)) return false
  }
  return true
}

/**
 * 把"选中的角色"合并回原有身份,得到要提交的 identities
 *
 * 规则:
 *  1. 原有身份里**角色还在选中集合里**的,整条保留
 *     (项目 / 部门 / 有效期 / 主身份标记都不能丢 —— 那是身份的另一半)
 *  2. 新选中的角色,追加一条新的身份(只带 role_code + role_name)
 *  3. 第一条固定 is_primary=true
 *     (后端 writeUserIdentitiesFromItems 会用 ensureSinglePrimary 保证只有一条主身份,
 *      取的也是第一条 —— 前端先对齐,省得两边理解不一致)
 *
 * ⚠️ 同名角色的多条身份不会被合并掉:保留规则是"整条保留",
 *    所以一个人绑了两个项目的同一角色,改完还是两条。
 */
export function mergeRoleIdentities(
  existing: IdentityLike[] | null | undefined,
  selected: string[] | null | undefined,
  roleNameOf?: RoleNameOf,
): IdentityItem[] {
  const sel = new Set((selected ?? []).filter(Boolean))
  const kept = (existing ?? []).filter((it) => it?.role_code && sel.has(it.role_code))

  const out: IdentityItem[] = kept.map((it) => ({
    role_code: it.role_code,
    role_name: it.role_name ?? roleNameOf?.(it.role_code),
    project_id: it.project_id || undefined,
    dept_id: it.dept_id || undefined,
    valid_from: it.valid_from ?? undefined,
    valid_to: it.valid_to ?? undefined,
    is_primary: it.is_primary,
  }))

  const used = new Set(kept.map((it) => it.role_code))
  for (const code of selected ?? []) {
    if (!code || used.has(code)) continue
    used.add(code)
    out.push({ role_code: code, role_name: roleNameOf?.(code), is_primary: false })
  }

  // 第一条 = 主身份(与后端 ensureSinglePrimary 的取法一致)
  out.forEach((it, idx) => {
    it.is_primary = idx === 0
  })
  return out
}

/** 编辑用户时需要的上下文 */
export interface UpdatePayloadContext {
  /** 该用户**当前**的身份(来自 GET /system/users/:id) */
  identities?: IdentityLike[] | null
  /** 角色 code → 角色名 */
  roleNameOf?: RoleNameOf
}

/**
 * 两个部门集合是否相同(忽略顺序、不看重复)
 *
 * 用于"部门没改就别发 department_ids" —— 后端是清空重建,发一次就是重写一次。
 */
export function sameDeptSet(
  a?: string[] | null,
  b?: string[] | null,
): boolean {
  const x = new Set((a ?? []).filter(Boolean))
  const y = new Set((b ?? []).filter(Boolean))
  if (x.size !== y.size) return false
  for (const id of x) {
    if (!y.has(id)) return false
  }
  return true
}

/**
 * 构造编辑用户(PATCH /system/users/:id)的请求体 —— **只发改动过的字段**
 *
 * ⚠️ 为什么必须"只发改动过的":
 *   后端 UpdateUserRequest 全是指针字段,nil = 不改。
 *   但前端表单里的手机号/邮箱是从**列表行**回填的,而列表在调用方没有
 *   user:read-sensitive 权限时给的是**空串** —— 照单全收就等于
 *   "把用户的手机号洗成空"。同理 identities / department_ids 是全量替换语义。
 *   所以这里逐字段比对,没改的一个都不发。
 *
 * 状态映射:active → is_active=true,disabled → is_active=false。
 *   'locked' 是后端**派生**出来的状态(锁定到期时间未过),不是可写字段,
 *   所以 locked 一律不发 is_active(发 true 会把锁定状态顺手解掉)。
 *
 * ⚠️ 部门(2026-10-25 M5d 补的):
 *   · 后端 writeUserDepartments 是"清空该用户全部部门再重建",
 *     所以 department_ids 必须是**改完之后完整的那个集合**,
 *     不是"新增了哪几个"。initial 里必须是详情接口返的**全部**部门。
 *   · 主部门也可能单独改(集合没变,只是换了 ⭐)→ 也要发,
 *     否则"切换主部门"点了没反应。
 *   · 主部门必须在下发的集合里,否则后端返 InvalidParam("主部门必须在部门列表中");
 *     不在就干脆不发 primary_department_id,让后端取第一个当主部门。
 */
export function buildUpdatePayload(
  vals: UserFormValues,
  initial: UserFormValues,
  ctx: UpdatePayloadContext = {},
): UpdateUserRequest {
  const payload: UpdateUserRequest = {}

  if (vals.nickname !== undefined && vals.nickname !== initial.nickname) {
    payload.nickname = vals.nickname
  }
  if (vals.phone !== undefined && vals.phone !== initial.phone) {
    payload.phone = vals.phone
  }
  if (vals.email !== undefined && vals.email !== initial.email) {
    payload.email = vals.email
  }
  if (vals.status && vals.status !== initial.status && vals.status !== 'locked') {
    payload.is_active = vals.status !== 'disabled'
  }

  const selected = vals.roles ?? []
  if (!sameRoleSet(ctx.identities, selected)) {
    payload.identities = mergeRoleIdentities(ctx.identities, selected, ctx.roleNameOf)
  }

  // 部门:集合变了 or 主部门变了,才发
  const nextDeptIds = vals.multiDept?.departmentIds ?? []
  const initDeptIds = initial.multiDept?.departmentIds ?? []
  const nextPrimary = vals.multiDept?.primaryDepartmentId
  const initPrimary = initial.multiDept?.primaryDepartmentId
  const deptSetChanged = !sameDeptSet(nextDeptIds, initDeptIds)
  const primaryChanged = (nextPrimary ?? '') !== (initPrimary ?? '')
  if (deptSetChanged || primaryChanged) {
    payload.department_ids = nextDeptIds
    if (nextPrimary && nextDeptIds.includes(nextPrimary)) {
      payload.primary_department_id = nextPrimary
    }
  }

  return payload
}

/**
 * 构造新增用户(POST /system/users)的请求体
 *
 * @param passwordHash 64 位 hex(preHashPassword 的结果)—— 后端只认这个长度
 */
export function buildCreatePayload(
  vals: UserFormValues,
  passwordHash: string,
  roleNameOf?: RoleNameOf,
): CreateUserRequest {
  const deptIds = vals.multiDept?.departmentIds ?? []
  const primary = vals.multiDept?.primaryDepartmentId
  return {
    username: (vals.username ?? '').trim(),
    password: passwordHash,
    nickname: (vals.nickname ?? '').trim(),
    phone: (vals.phone ?? '').trim(),
    email: (vals.email ?? '').trim(),
    department_ids: deptIds,
    // ⚠️ 不能传空串:后端是 uint64 + json:",string",空串会让 BindJSON 直接失败
    primary_department_id: primary || undefined,
    identities: mergeRoleIdentities([], vals.roles ?? [], roleNameOf),
  }
}
