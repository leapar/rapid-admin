/**
 * buildMenuTree —— 资源树构建(2026-10-27 M102 补网)
 *
 * ## 为什么补这条网
 *
 *   `buildMenuTree` 是**权限树 / 菜单树 / 分配权限 Drawer 三处共用的建树函数**,
 *   之前**零用例**。它是纯函数(没有 I/O、没有 store 依赖),补网成本极低,
 *   不补才是失职。
 *
 * ## 它到底在做什么(逐行核对过实现)
 *
 *   · 根节点 = `parent_id === '0'` **或** 父节点不在传入列表里
 *     (后端如果只返回了部分层级,不能把子节点整批丢掉);
 *   · 排序:根和每一层 children 都按 `sort` 升序;
 *   · `is_active === false` 的节点**整棵子树都不出现在结果里**
 *     (父节点被跳过 → 子节点挂到了不可达的父上 → 同样出不来),
 *     这正是"隐藏菜单下别再显示二级"的意图;
 *   · 字段名以 wire 实况为准(snake_case),同时兼容老的 camelCase
 *     (`parentId` / `isActive` / `enabled`)—— 老 mock 和旧数据还在用它们。
 *
 * ## 一条**如实记录现状**的用例
 *
 *   `visible` 字段后端**没有这一列**(`model.SysResource` 只有 `is_active`),
 *   所以 Sidebar 里 `.filter((r) => r.visible !== false)` 今天恒为真。
 *   这里把"visible=false 依然出现在树里"钉成用例 ——
 *   等哪天后端真加了这一列,这条会红,提醒同步改前端。
 */
import { describe, it, expect } from 'vitest'
import { buildMenuTree } from '@/utils/resourceTree'
import type { Resource } from '@/types/permission'

function res(id: string, parentId: string, sort: number, extra: Partial<Resource> = {}): Resource {
  return {
    id,
    code: `c${id}`,
    name: `n${id}`,
    type: 'menu',
    parent_id: parentId,
    sort,
    is_active: true,
    ...extra,
  }
}

describe('utils/resourceTree · buildMenuTree', () => {
  it('平铺列表 → 树(根节点 + children 嵌套)', () => {
    const tree = buildMenuTree([res('1', '0', 1), res('2', '1', 1), res('3', '2', 1)])

    expect(tree).toHaveLength(1)
    expect(tree[0]!.id).toBe('1')
    expect(tree[0]!.children?.[0]!.id).toBe('2')
    expect(tree[0]!.children?.[0]!.children?.[0]!.id).toBe('3')
  })

  it('根节点按 sort 升序', () => {
    const tree = buildMenuTree([res('a', '0', 30), res('b', '0', 10), res('c', '0', 20)])

    expect(tree.map((r) => r.id)).toEqual(['b', 'c', 'a'])
  })

  it('children 也按 sort 升序', () => {
    const tree = buildMenuTree([
      res('p', '0', 1),
      res('x', 'p', 9),
      res('y', 'p', 2),
      res('z', 'p', 5),
    ])

    expect(tree[0]!.children?.map((r) => r.id)).toEqual(['y', 'z', 'x'])
  })

  it('父节点不在列表里 → 当作根(不能整批丢掉)', () => {
    const tree = buildMenuTree([res('c', 'missing-parent', 1)])

    expect(tree.map((r) => r.id)).toEqual(['c'])
  })

  it('is_active=false 的节点,连它的子树一起消失', () => {
    const tree = buildMenuTree([
      res('p', '0', 1),
      res('off', 'p', 1, { is_active: false }),
      res('child-of-off', 'off', 1),
      res('ok', 'p', 2),
    ])

    const ids: string[] = []
    const walk = (list: Resource[]) => {
      for (const r of list) {
        ids.push(r.id)
        if (r.children?.length) walk(r.children)
      }
    }
    walk(tree)

    expect(ids).toContain('p')
    expect(ids).toContain('ok')
    expect(ids).not.toContain('off')
    expect(ids).not.toContain('child-of-off')
  })

  it('兼容老的 camelCase 字段(parentId / isActive)', () => {
    const tree = buildMenuTree([
      { id: 'p', code: 'p', name: 'p', type: 'menu', parentId: '0', sort: 1 } as Resource,
      {
        id: 'c',
        code: 'c',
        name: 'c',
        type: 'menu',
        parentId: 'p',
        sort: 1,
        isActive: true,
      } as Resource,
    ])

    expect(tree).toHaveLength(1)
    expect(tree[0]!.children?.map((r) => r.id)).toEqual(['c'])
  })

  it('camelCase 的 isActive=false 同样生效', () => {
    const tree = buildMenuTree([
      { id: 'p', code: 'p', name: 'p', type: 'menu', parentId: '0', sort: 1 } as Resource,
      {
        id: 'c',
        code: 'c',
        name: 'c',
        type: 'menu',
        parentId: 'p',
        sort: 1,
        isActive: false,
      } as Resource,
    ])

    expect(tree[0]!.children).toHaveLength(0)
  })

  it('**visible 现在不参与过滤**(后端没这列)—— 现状如实记录', () => {
    const tree = buildMenuTree([res('a', '0', 1, { visible: false })])

    // visible=false 依然在树里 —— Sidebar 那句 filter 今天恒为真
    expect(tree.map((r) => r.id)).toEqual(['a'])
  })

  it('空列表 → 空树', () => {
    expect(buildMenuTree([])).toEqual([])
  })
})
