/**
 * 用户编辑表单纯逻辑单测
 *
 * ⚠️ **这个用例是「用户编辑界面无法设置角色」那个 Bug 的回归网**:
 *   2026-10-25 用户报"用户编辑界面无法设置角色"。表面是少个字段,实际有五个坑:
 *     1. 角色字段压根没有
 *     2. 请求体字段名 camelCase,后端绑的是 snake_case → 静默丢弃
 *        (新增用户缺 password,后端直接返"密码不能为空")
 *     3. identities 是"清空重建"式全量替换 → **没改也绝不能发**
 *     4. 列表 DTO 里没有 identities(只有详情接口有)→ 回填必须先拉详情
 *     5. "管理身份"抽屉之前拿列表行当身份数据 → 保存即清空身份
 *
 * 这里锁的是第 2/3 条的**纯逻辑**(请求体怎么拼);
 * 第 1/4/5 条靠下面 index.test.tsx 的页面级用例 + drawer's 回归网。
 */
import { describe, it, expect } from 'vitest'
import {
  buildCreatePayload,
  buildUpdatePayload,
  mergeRoleIdentities,
  roleCodesOf,
  sameDeptSet,
  sameRoleSet,
} from '../userForm'
import type { IdentityLike } from '../userForm'
import type { IdentityItem } from '@/types/permission'

/** 后端 GET /users/:id 返的身份(字段名与 model.SysUserIdentity 一致) */
function ident(partial: Partial<IdentityLike> & { role_code: string }): IdentityLike {
  return {
    role_name: partial.role_code,
    project_id: '0',
    dept_id: '0',
    valid_from: '2026-01-01T00:00:00+08:00',
    valid_to: null,
    is_primary: false,
    ...partial,
  }
}

const EXISTING: IdentityLike[] = [
  ident({ role_code: 'role_user_admin', role_name: '用户管理员', is_primary: true }),
  ident({ role_code: 'role_house_admin', role_name: ' housemanager' }),
]

describe('userForm', () => {
  describe('roleCodesOf', () => {
    it('取出角色 code', () => {
      expect(roleCodesOf(EXISTING)).toEqual(['role_user_admin', 'role_house_admin'])
    })

    it('同一个角色的多条身份只算一个(多选框本来就选不重复)', () => {
      const dup = [
        ident({ role_code: 'role_user_admin' }),
        ident({ role_code: 'role_user_admin', project_id: '187' }),
      ]
      expect(roleCodesOf(dup)).toEqual(['role_user_admin'])
    })

    it('没有身份时是空数组(不是 undefined)', () => {
      expect(roleCodesOf(undefined)).toEqual([])
      expect(roleCodesOf([])).toEqual([])
    })
  })

  describe('sameRoleSet', () => {
    it('集合相同(顺序不同也算相同)', () => {
      expect(sameRoleSet(EXISTING, ['role_house_admin', 'role_user_admin'])).toBe(true)
    })

    it('多一个 / 少一个都算不同', () => {
      expect(sameRoleSet(EXISTING, ['role_user_admin'])).toBe(false)
      expect(sameRoleSet(EXISTING, [...roleCodesOf(EXISTING), 'role_ops'])).toBe(false)
    })

    it('两边都空 = 相同', () => {
      expect(sameRoleSet(undefined, [])).toBe(true)
      expect(sameRoleSet([], undefined)).toBe(true)
    })
  })

  describe('mergeRoleIdentities', () => {
    it('没改角色时,原有身份整条保留(项目/部门/有效期不能丢)', () => {
      const out = mergeRoleIdentities(EXISTING, roleCodesOf(EXISTING))
      expect(out).toHaveLength(2)
      expect(out[0]).toMatchObject({
        role_code: 'role_user_admin',
        role_name: '用户管理员',
        project_id: '0',
        dept_id: '0',
        valid_from: '2026-01-01T00:00:00+08:00',
        is_primary: true,
      })
      // valid_to 是 null(永久)→ 转成 undefined,请求里就不带这个 key
      expect(out[0]!.valid_to).toBeUndefined()
    })

    it('新增一个角色 → 追加一条身份,只带 role_code + role_name', () => {
      const out = mergeRoleIdentities(EXISTING, [...roleCodesOf(EXISTING), 'role_ops'], (code) =>
        code === 'role_ops' ? '平台运营' : undefined,
      )
      expect(out).toHaveLength(3)
      expect(out[2]).toEqual({ role_code: 'role_ops', role_name: '平台运营', is_primary: false })
    })

    it('取消一个角色 → 那条身份被去掉', () => {
      const out = mergeRoleIdentities(EXISTING, ['role_user_admin'])
      expect(out.map((i) => i.role_code)).toEqual(['role_user_admin'])
    })

    it('第一条固定是主身份(与后端 ensureSinglePrimary 的取法一致)', () => {
      const out = mergeRoleIdentities(EXISTING, ['role_house_admin', 'role_user_admin'])
      expect(out[0]!.is_primary).toBe(true)
      expect(out[1]!.is_primary).toBe(false)
    })

    it('同一个角色的两条身份不会被合并掉(绑不同项目是合法的)', () => {
      const dup = [
        ident({ role_code: 'role_user_admin', project_id: '0' }),
        ident({ role_code: 'role_user_admin', project_id: '187' }),
      ]
      const out = mergeRoleIdentities(dup, ['role_user_admin'])
      expect(out).toHaveLength(2)
      expect(out.map((i) => i.project_id)).toEqual(['0', '187'])
    })
  })

  describe('buildUpdatePayload', () => {
    const initial = {
      username: 'zhangsan',
      nickname: '张三',
      email: 'zhangsan@x.cn',
      phone: '13800000000',
      status: 'active',
      roles: ['role_user_admin', 'role_house_admin'],
    }

    it('什么都没改 → 空载荷(一个字段都不发,尤其不能发 identities)', () => {
      const payload = buildUpdatePayload({ ...initial }, initial, { identities: EXISTING })
      expect(payload).toEqual({})
    })

    it('只改昵称 → 只发 nickname(手机号/邮箱/身份一律不发)', () => {
      const payload = buildUpdatePayload({ ...initial, nickname: '张三丰' }, initial, {
        identities: EXISTING,
      })
      expect(payload).toEqual({ nickname: '张三丰' })
    })

    it('列表里读不出手机号时(无 user:read-sensitive 权限),留空不能把手机号洗掉', () => {
      // 列表行里 phone 是空串 —— 照单全收就等于"把手机号更新成空"
      const init = { ...initial, phone: '' }
      const payload = buildUpdatePayload({ ...init, nickname: '新昵称' }, init, {
        identities: EXISTING,
      })
      expect(payload).toEqual({ nickname: '新昵称' })
      expect('phone' in payload).toBe(false)
    })

    it('真要改手机号时才发', () => {
      const payload = buildUpdatePayload({ ...initial, phone: '13900000000' }, initial, {
        identities: EXISTING,
      })
      expect(payload).toEqual({ phone: '13900000000' })
    })

    it('角色改了 → 发 identities(全量替换,后端会清空重建)', () => {
      const payload = buildUpdatePayload(
        { ...initial, roles: ['role_user_admin', 'role_ops'] },
        initial,
        { identities: EXISTING, roleNameOf: (c) => (c === 'role_ops' ? '平台运营' : c) },
      )
      expect(payload.identities).toHaveLength(2)
      expect((payload.identities as IdentityItem[])[1]).toMatchObject({
        role_code: 'role_ops',
        role_name: '平台运营',
      })
    })

    it('状态改成停用 → is_active=false', () => {
      const payload = buildUpdatePayload({ ...initial, status: 'disabled' }, initial, {
        identities: EXISTING,
      })
      expect(payload).toEqual({ is_active: false })
    })

    it('状态改成启用 → is_active=true', () => {
      const payload = buildUpdatePayload(
        { ...initial, status: 'active' },
        { ...initial, status: 'disabled' },
        { identities: EXISTING },
      )
      expect(payload).toEqual({ is_active: true })
    })

    it('「锁定」是系统派生的状态,不能发 is_active(会把锁定顺手解掉)', () => {
      const payload = buildUpdatePayload({ ...initial, status: 'locked' }, initial, {
        identities: EXISTING,
      })
      expect(payload).toEqual({})
    })
  })

  /**
   * 部门(2026-10-25 M5d)
   *
   * ⚠️ 这里的每一条都在防**洗数据**:
   *   后端 writeUserDepartments 是"清空该用户全部部门再重建",
   *   所以 department_ids 必须是"改完之后完整的全集"。
   *   发少了 = 把没发的那几个部门删了。
   */
  describe('部门(M5d)', () => {
    const initial = {
      username: 'zhangsan',
      nickname: '张三',
      status: 'active',
      roles: ['role_user_admin'],
      multiDept: { departmentIds: ['10', '20'], primaryDepartmentId: '10' },
    }
    const ctx = { identities: [ident({ role_code: 'role_user_admin' })] }

    it('部门没改 → 一个字段都不发(全量替换语义,没改就别碰)', () => {
      const payload = buildUpdatePayload({ ...initial }, initial, ctx)
      expect(payload).toEqual({})
      expect('department_ids' in payload).toBe(false)
      expect('primary_department_id' in payload).toBe(false)
    })

    it('加了一个部门 → 发的是**完整集合**,不是只发新增的那个', () => {
      const payload = buildUpdatePayload(
        { ...initial, multiDept: { departmentIds: ['10', '20', '30'], primaryDepartmentId: '10' } },
        initial,
        ctx,
      )
      expect(payload.department_ids).toEqual(['10', '20', '30'])
      expect(payload.primary_department_id).toBe('10')
    })

    it('取消了一个部门 → 发剩下的完整集合', () => {
      const payload = buildUpdatePayload(
        { ...initial, multiDept: { departmentIds: ['20'], primaryDepartmentId: '20' } },
        initial,
        ctx,
      )
      expect(payload.department_ids).toEqual(['20'])
      expect(payload.primary_department_id).toBe('20')
    })

    it('清空所有部门 → 发空数组(不是不发 —— 不发 = 不改)', () => {
      const payload = buildUpdatePayload(
        { ...initial, multiDept: { departmentIds: [], primaryDepartmentId: undefined } },
        initial,
        ctx,
      )
      expect(payload.department_ids).toEqual([])
      expect('primary_department_id' in payload).toBe(false)
    })

    it('只换了主部门(集合没变)→ 也要发,否则"切换主部门"点了没反应', () => {
      const payload = buildUpdatePayload(
        { ...initial, multiDept: { departmentIds: ['10', '20'], primaryDepartmentId: '20' } },
        initial,
        ctx,
      )
      expect(payload.department_ids).toEqual(['10', '20'])
      expect(payload.primary_department_id).toBe('20')
    })

    it('主部门被取消勾选 → 不发 primary_department_id(后端会取第一个当主部门)', () => {
      // 主部门不在下发集合里,后端会返 InvalidParam("主部门必须在部门列表中")
      const payload = buildUpdatePayload(
        { ...initial, multiDept: { departmentIds: ['20'], primaryDepartmentId: '10' } },
        initial,
        ctx,
      )
      expect(payload.department_ids).toEqual(['20'])
      expect('primary_department_id' in payload).toBe(false)
    })

    it('sameDeptSet:顺序不同算相同,多一个少一个都算不同', () => {
      expect(sameDeptSet(['10', '20'], ['20', '10'])).toBe(true)
      expect(sameDeptSet(['10'], ['10', '20'])).toBe(false)
      expect(sameDeptSet(['10', '20'], ['10'])).toBe(false)
      expect(sameDeptSet(undefined, [])).toBe(true)
    })
  })

  describe('buildCreatePayload', () => {
    it('新增用户:密码用前端 pre-hash 的 64 位 hex', () => {
      const payload = buildCreatePayload(
        {
          username: 'lisi',
          nickname: '李四',
          phone: '13800000001',
          email: 'lisi@x.cn',
          roles: ['role_user_admin'],
          multiDept: { departmentIds: ['100'], primaryDepartmentId: '100' },
        },
        'a'.repeat(64),
        () => '用户管理员',
      )
      expect(payload).toEqual({
        username: 'lisi',
        password: 'a'.repeat(64),
        nickname: '李四',
        phone: '13800000001',
        email: 'lisi@x.cn',
        department_ids: ['100'],
        primary_department_id: '100',
        identities: [{ role_code: 'role_user_admin', role_name: '用户管理员', is_primary: true }],
      })
    })

    it('没有主部门时 primary_department_id 必须是 undefined(空串会让后端 BindJSON 直接失败)', () => {
      const payload = buildCreatePayload(
        {
          username: 'lisi',
          nickname: '李四',
          phone: '13800000001',
          roles: ['role_user_admin'],
          multiDept: { departmentIds: ['100'] },
        },
        'b'.repeat(64),
      )
      expect(payload.primary_department_id).toBeUndefined()
      // 上线的是 JSON:undefined 的 key 会被 JSON.stringify 丢掉 ✓
      // (要是传空串,后端 uint64 + json:",string" 解析失败 → 整个请求 400)
      expect(JSON.stringify(payload)).not.toContain('primary_department_id')
    })
  })
})
