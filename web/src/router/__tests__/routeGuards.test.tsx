/**
 * 路由守卫 · 权限码 **与角色码** 都要认(2026-10-28 M103)
 *
 * ## 这条网守的是什么
 *
 *   `routes.tsx` 里有 13 处这样的写法:
 *
 *     <RoutePermissionGuard anyOf={[PermSystem.USER_LIST, RoleCode.SUPER_ADMIN]}>
 *
 *   语义是"**有 user:list 权限,或者本身是超管**,就能进这个页面"。
 *   而守卫以前只查 `permissionCodes`(权限码集合)——
 *   **角色 code 永远不在那个集合里** → "或超管"那一支恒不命中。
 *
 *   以前因为 `RoleCode.SUPER_ADMIN` 的值本身也是错的
 *   (`role_platform_super_admin`,后端从没签发过),
 *   这一支**双重恒 false**。M103 把 role code 改对了之后,
 *   守卫再不改就变成"改对了一半":超管仍然进不了那些页面。
 *
 *   ⚠️ 角色 code 与权限码是两套**不相交**的命名空间
 *     (`role_*` vs `user:list` / `button.*`),
 *     所以"任一命中"地查两个集合不会有歧义。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   把 `guards.tsx` 的 `satisfied` 退回只查 `has(code)` → 本文件立刻变红。
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { RoutePermissionGuard } from '@/router/guards'
import { usePermissionStore } from '@/stores/permission'
import { RoleCode } from '@/constants/roles'

/** 后端 seed 的权限码(见 server/internal/seed/seed.go 的 resourceList) */
const USER_LIST = 'user:list'

function renderGuard(props: {
  anyOf?: string[]
  allOf?: string[]
}): void {
  render(
    <MemoryRouter initialEntries={['/target']}>
      <Routes>
        <Route
          path="/target"
          element={
            <RoutePermissionGuard {...props}>
              <div>目标内容</div>
            </RoutePermissionGuard>
          }
        />
        <Route path="/forbidden" element={<div>没有权限</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  usePermissionStore.setState({ permissionCodes: new Set(), roleCodes: [] })
})

describe('RoutePermissionGuard · 权限码', () => {
  it('有权限码 → 放行', () => {
    usePermissionStore.setState({ permissionCodes: new Set([USER_LIST]) })
    renderGuard({ anyOf: [USER_LIST] })
    expect(screen.getByText('目标内容')).toBeInTheDocument()
  })

  it('没权限码 → 跳 /forbidden', () => {
    renderGuard({ anyOf: [USER_LIST] })
    expect(screen.queryByText('目标内容')).toBeNull()
    expect(screen.getByText('没有权限')).toBeInTheDocument()
  })

  it('allOf 要全都有', () => {
    usePermissionStore.setState({ permissionCodes: new Set([USER_LIST]) })
    renderGuard({ allOf: [USER_LIST, 'role:list'] })
    expect(screen.getByText('没有权限')).toBeInTheDocument()
  })
})

describe('RoutePermissionGuard · 角色码(M103 修的真 bug)', () => {
  it('只有角色码、没有任何权限码 → 照样放行', () => {
    // 超管在后端拿得到全部资源,但这里刻意**只给角色码**:
    // 证明守卫认的是"角色",不是碰巧权限码集合里恰好有值。
    usePermissionStore.setState({ roleCodes: [RoleCode.SUPER_ADMIN] })
    renderGuard({ anyOf: [USER_LIST, RoleCode.SUPER_ADMIN] })
    expect(screen.getByText('目标内容')).toBeInTheDocument()
  })

  it('"有权限 **或** 是超管" —— 两边各命中一半都放行', () => {
    // 只有权限、没有超管
    usePermissionStore.setState({ permissionCodes: new Set([USER_LIST]) })
    const { unmount } = render(
      <MemoryRouter initialEntries={['/target']}>
        <Routes>
          <Route
            path="/target"
            element={
              <RoutePermissionGuard anyOf={[USER_LIST, RoleCode.SUPER_ADMIN]}>
                <div>目标内容</div>
              </RoutePermissionGuard>
            }
          />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText('目标内容')).toBeInTheDocument()
    unmount()

    // 只有超管、没有权限
    usePermissionStore.setState({ permissionCodes: new Set(), roleCodes: [RoleCode.SUPER_ADMIN] })
    renderGuard({ anyOf: [USER_LIST, RoleCode.SUPER_ADMIN] })
    expect(screen.getByText('目标内容')).toBeInTheDocument()
  })

  it('既没权限又不是超管 → 跳 /forbidden', () => {
    usePermissionStore.setState({ roleCodes: [RoleCode.READONLY] })
    renderGuard({ anyOf: [USER_LIST, RoleCode.SUPER_ADMIN] })
    expect(screen.queryByText('目标内容')).toBeNull()
    expect(screen.getByText('没有权限')).toBeInTheDocument()
  })

  it('allOf 里写角色码也算数', () => {
    usePermissionStore.setState({
      permissionCodes: new Set([USER_LIST]),
      roleCodes: [RoleCode.SUPER_ADMIN],
    })
    renderGuard({ allOf: [USER_LIST, RoleCode.SUPER_ADMIN] })
    expect(screen.getByText('目标内容')).toBeInTheDocument()
  })
})
