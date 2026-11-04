/**
 * Sidebar —— 侧边栏菜单(2026-10-28 M103)
 *
 * ## 这条网守的是什么
 *
 *   侧边栏是**全站每一页都在的壳**。菜单数据来自后端 `sys_resource`
 *   (`usePermissionStore.menus`),点一下就 `navigate(r.path)`。
 *
 *   M103 之前,`components/layout/` 整个目录**只有一条 pageName 的纯函数测试**,
 *   Sidebar / Header / BasicLayout / Breadcrumb **一条用例都没有** ——
 *   而这四个组件挂在**每一个路由**上。
 *
 * ## 三条硬约束(每条都对应一个真问题)
 *
 *   1. **菜单只认 `menu.` 开头的 code** —— 后端 accessible('menu') 已按
 *      type 过滤,但 DB 缺 seed 时 api 资源会混进来,叶子按钮级条目会污染侧边栏。
 *   2. **当前页所在的父菜单必须自动展开** —— 切到另一个顶级菜单后,
 *      那一组父菜单不展开的话,**当前页在侧边栏里根本看不见**。
 *      ⚠️ antd 的 `defaultOpenKeys` 是**挂载时一次性**的值
 *      (rc-menu 走 `useMergedState(defaultOpenKeys, {value: undefined})`,
 *      value 为 undefined 时内部 state 只在挂载时取一次默认值,之后
 *      `defaultOpenKeys` 变了也不跟随)—— 只写 defaultOpenKeys 是不够的。
 *   3. **选中态跟着路由走** —— 父级 + 子级都要高亮(antd 会给父子同时打选中样式)。
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { useEffect } from 'react'
import { Sidebar } from '@/components/layout/Sidebar'
import { usePermissionStore } from '@/stores/permission'
import type { Resource } from '@/types/permission'

function menu(
  code: string,
  name: string,
  path: string,
  sort: number,
  children?: Resource[],
): Resource {
  return { id: code, code, name, type: 'menu', path, sort, is_active: true, children }
}

const MENUS: Resource[] = [
  menu('menu.system', '系统管理', '/system', 10, [
    menu('menu.system.user', '用户管理', '/system/user', 11),
    menu('menu.system.role', '角色管理', '/system/role', 12),
  ]),
  menu('menu.owner', '业主管理', '/owner', 20, [
    menu('menu.owner.list', '业主列表', '/owner/list', 21),
  ]),
]

/** 渲染并暴露一个"跳到某路径"的按钮 */
function Nav({ to }: { to: string }) {
  const navigate = useNavigate()
  useEffect(() => {
    navigate(to)
  }, [navigate, to])
  return null
}

function renderSidebar(path: string, navTo?: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      {navTo && <Nav to={navTo} />}
      <Sidebar />
    </MemoryRouter>,
  )
}

/** 某个子菜单标题所在的 <li> 是否处于"展开"状态 */
function submenuOpen(label: string): boolean {
  const el = screen.getByText(label).closest('li')
  return !!el?.classList.contains('ant-menu-submenu-open')
}

/**
 * 只取**一级**菜单项的标题
 *
 * ⚠️ 两个坑:
 *   ① `getAllByRole('menuitem')` 会把**嵌套的子项**也一起捞出来
 *      (DOM 顺序:系统管理 / 用户管理 / 角色管理 / 业主管理 / 业主列表),
 *      拿它断言"一级菜单按 sort 排序"必然错。
 *   ② `ul.ant-menu > li` 也不行 —— rc-menu 的子列表 class 是
 *      `ant-menu ant-menu-sub ant-menu-inline`(**带着 ant-menu**),
 *      所以嵌套项照样命中。必须 `:not(.ant-menu-sub)`。
 */
function topLevelLabels(container: HTMLElement): string[] {
  return Array.from(
    container.querySelectorAll('ul.ant-menu:not(.ant-menu-sub) > li'),
  ).map((li) => li.querySelector('.ant-menu-title-content')?.textContent ?? '')
}

beforeEach(() => {
  usePermissionStore.setState({ menus: MENUS })
})

describe('Sidebar · 菜单渲染', () => {
  it('按 sort 升序渲染一级菜单', () => {
    const { container } = renderSidebar('/system/user')
    const labels = topLevelLabels(container)
    expect(labels[0]).toContain('系统管理')
    expect(labels[1]).toContain('业主管理')
    expect(labels).toHaveLength(2)
  })

  it('只认 menu. 开头的 code(api/button 资源混进来也不渲染)', () => {
    usePermissionStore.setState({
      menus: [
        ...MENUS,
        { id: 'x', code: 'user:list', name: '用户列表', type: 'api', sort: 99, is_active: true },
      ],
    })
    renderSidebar('/system/user')
    expect(screen.queryByText('用户列表')).toBeNull()
  })

  it('非 menu. 前缀的菜单资源被过滤掉(后端 seed 异常时的兜底)', () => {
    usePermissionStore.setState({
      menus: [{ id: 'x', code: 'weird.code', name: '怪菜单', type: 'menu', sort: 1, is_active: true }],
    })
    renderSidebar('/system/user')
    expect(screen.queryByText('怪菜单')).toBeNull()
  })
})

describe('Sidebar · 选中态', () => {
  it('当前页所在的父菜单 + 当前页自己都高亮', () => {
    renderSidebar('/system/user')
    // 当前页(叶子):ant-menu-item-selected
    expect(screen.getByText('用户管理').closest('li')).toHaveClass('ant-menu-item-selected')
    // 父级(子菜单):antd 给的是 **ant-menu-submenu-selected**,不是 item-selected
    //   (rc-menu: SubMenu 的 className 里只有 submenu-open / submenu-active /
    //    submenu-selected,没有 item-selected)
    expect(screen.getByText('系统管理').closest('li')).toHaveClass(
      'ant-menu-submenu-selected',
    )
  })

  it('换个路由,选中态跟着换', () => {
    renderSidebar('/system/user', '/owner/list')
    expect(screen.getByText('业主列表').closest('li')).toHaveClass('ant-menu-item-selected')
    expect(screen.getByText('用户管理').closest('li')).not.toHaveClass('ant-menu-item-selected')
  })
})

describe('Sidebar · 父菜单自动展开', () => {
  it('首次落在 /system/user 时,「系统管理」这一组是展开的', () => {
    renderSidebar('/system/user')
    expect(submenuOpen('系统管理')).toBe(true)
  })

  it('切到另一个顶级菜单后,那一组父菜单也必须自动展开', () => {
    // ⚠️ 这条是 M103 挖出来的真 bug:
    //   antd 的 `defaultOpenKeys` 只在**挂载时**取一次
    //   (rc-menu: useMergedState(defaultOpenKeys, { value: openKeys }),
    //    openKeys 是 undefined → 内部 state 只在挂载时初始化一次默认值,
    //    之后 defaultOpenKeys 变了也不跟随)。
    //   于是从 /system/user 切到 /owner/list 之后,「业主管理」这一组
    //   **不展开** —— 当前页在侧边栏里看不见,用户以为"菜单丢了"。
    renderSidebar('/system/user', '/owner/list')
    expect(submenuOpen('业主管理')).toBe(true)
  })

  it('切走之后,原来那一组可以收起(不要求一直开着)', () => {
    renderSidebar('/system/user', '/owner/list')
    // 两组都展开不算错,但"原来的那组还开着"会让侧边栏越点越乱
    // —— 这条只断言"当前组展开",不强行约束另一组
    expect(submenuOpen('业主管理')).toBe(true)
  })
})
