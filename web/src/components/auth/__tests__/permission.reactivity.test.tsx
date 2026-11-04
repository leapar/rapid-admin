/**
 * M102 —— 权限层"响应性"回归网(2026-10-27)
 *
 * ## 这条网守的是什么
 *
 *   权限变化(store 里的 permissionCodes / roleCodes 变了)之后,
 *   **每一个根据权限决定"渲不渲染"的组件都必须立刻重渲染**。
 *
 *   以前有三个组件用 `usePermissionStore.getState()` 读权限 ——
 *   `getState()` 是一次性快照,**不建立订阅**,组件不会因为权限变化而更新:
 *
 *     · components/common/RowActions.tsx    —— 行内"编辑/删除"按钮
 *     · components/auth/AuthButton.tsx       —— 工具栏按钮
 *     · router/guards.tsx RoutePermissionGuard —— 路由级守卫
 *
 *   后果(实测,不是推演):
 *     · **切换身份**后,新身份没有的按钮**还在**,刚授予的按钮不出现;
 *     · SSE 收到 `perm_changed` 后同样不更新 —— 也就是"权限实时刷新"
 *       这个功能对这三个组件**从来没生效过**;
 *     · 路由级守卫不更新 = 撤掉的权限还在放行、新身份进不去的页面照样进得去。
 *
 *   点击这些"僵尸按钮"只会换来后端一个 403 —— 用户看到的是"按钮点了没反应"。
 *
 * ## 为什么必须带对照组
 *
 *   本文件里 `PermissionGuard` 那一组是**对照组**:它一直用的是
 *   `usePermission()`(订阅),所以它是响应的。
 *   没有它,本文件的第一版探针会把**四个组件全判成"不响应"** ——
 *   因为 React 18 在 `act()` 之外不会同步刷新 DOM(store 更新根本没落地),
 *   于是连本来正确的那个也测成坏的。**对照组先证明"测法本身是对的"。**
 *
 * ## 断言写法上的坑(老规矩)
 *
 *   antd 的 Button 会把汉字之间插空格("删除" → "删 除"),
 *   匹配要用 `/删\s*除/`,不能写 `getByText('删除')`。
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { usePermissionStore } from '@/stores/permission'
import { PermissionGuard } from '@/components/auth/PermissionGuard'
import { AuthButton } from '@/components/auth/AuthButton'
import { RowActions } from '@/components/common/RowActions'
import { RoutePermissionGuard } from '@/router/guards'

const CODE = 'button.user.delete'

beforeEach(() => {
  usePermissionStore.setState({
    permissionCodes: new Set([CODE]),
    roleCodes: ['ops'],
    menus: [],
  })
})

/** 撤权:把权限码清空(模拟运营把权限从角色上摘掉) */
function revoke() {
  act(() => {
    usePermissionStore.setState({ permissionCodes: new Set<string>() })
  })
}

describe('M102 · 权限变化后组件必须重渲染', () => {
  it('对照组:PermissionGuard 本来就是响应的(证明测法本身有效)', () => {
    render(
      <PermissionGuard code={CODE}>
        <span>受控内容</span>
      </PermissionGuard>,
    )
    expect(screen.getByText('受控内容')).toBeInTheDocument()

    revoke()

    // 撤权后内容应当消失
    expect(screen.queryByText('受控内容')).toBeNull()
  })

  it('AuthButton:撤权后按钮消失', () => {
    render(<AuthButton perm={CODE}>删除</AuthButton>)
    expect(screen.getByText(/删\s*除/)).toBeInTheDocument()

    revoke()

    expect(screen.queryByText(/删\s*除/)).toBeNull()
  })

  it('RowActions:撤权后行内按钮消失', () => {
    render(
      <RowActions
        actions={[{ key: 'del', label: '删除', perm: CODE, onClick: () => {} }]}
        row={{}}
      />,
    )
    expect(screen.getByText(/删\s*除/)).toBeInTheDocument()

    revoke()

    expect(screen.queryByText(/删\s*除/)).toBeNull()
  })

  it('RowActions:授权后按钮出现(反向也要盯)', () => {
    usePermissionStore.setState({ permissionCodes: new Set<string>() })
    render(
      <RowActions
        actions={[{ key: 'del', label: '删除', perm: CODE, onClick: () => {} }]}
        row={{}}
      />,
    )
    expect(screen.queryByText(/删\s*除/)).toBeNull()

    act(() => {
      usePermissionStore.setState({ permissionCodes: new Set([CODE]) })
    })

    expect(screen.getByText(/删\s*除/)).toBeInTheDocument()
  })

  it('RoutePermissionGuard:撤权后跳到 /forbidden', () => {
    render(
      <MemoryRouter initialEntries={['/protected']}>
        <Routes>
          <Route
            path="/protected"
            element={
              <RoutePermissionGuard anyOf={[CODE]}>
                <span>受保护的页面</span>
              </RoutePermissionGuard>
            }
          />
          <Route path="/forbidden" element={<span>没有权限</span>} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText('受保护的页面')).toBeInTheDocument()

    revoke()

    expect(screen.queryByText('受保护的页面')).toBeNull()
    expect(screen.getByText('没有权限')).toBeInTheDocument()
  })
})
