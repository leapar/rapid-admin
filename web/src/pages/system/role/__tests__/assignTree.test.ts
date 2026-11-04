/**
 * 分配权限 Drawer 纯逻辑单测
 *
 * ⚠️ **这个用例是「菜单勾不上」那个 Bug 的回归网**:
 *   2026-10-25 用户报"分配权限里菜单无法勾选,现在全部是默认勾选"。
 *   根因是「后端返扁平列表,前端当树用」:
 *     - 菜单节点 checkable:false → 永远勾不上
 *     - 菜单 key 用 `m-${id}` 而不是真 ID → 已绑定的菜单也显示不出勾选态
 *     - 标题里画了个蓝色对勾图标 → 看着像"全部默认勾选"
 *   这三个都在 AssignResourcesDrawer.tsx 里(靠渲染才能验),
 *   这里锁的是它依赖的**纯逻辑**:建树 / 过滤 / 收集 ID / 统计。
 *   抽屉本体(菜单节点可勾 + key 用真 ID + 去掉假对勾图标)靠下面
 *   「变异探针」清单在人工 review 时确认,没有自动化用例覆盖 —— 见
 *   __tests__/../AssignResourcesDrawer.tsx 顶部注释。
 */
import { describe, it, expect } from 'vitest'
import type { Resource } from '@/types/permission'
import {
  buildMenuSubtree,
  collectIdsByType,
  countResources,
  filterTreeByKeyword,
  matchResource,
} from '../assignTree'

/** 造资源(后端 SysResource 的 json 形状:snake_case) */
function res(partial: Partial<Resource> & { id: string; code: string }): Resource {
  return {
    name: partial.code,
    type: 'menu',
    sort: 0,
    ...partial,
  } as Resource
}

const FLAT: Resource[] = [
  // 菜单:系统管理(根) → 用户管理 / 角色管理(子)
  res({ id: 'm1', code: 'menu.system', name: '系统管理', path: '/system', sort: 1 }),
  res({ id: 'm2', code: 'menu.system.user', name: '用户管理', path: '/system/user', parent_id: 'm1', sort: 1 }),
  res({ id: 'm3', code: 'menu.system.role', name: '角色管理', path: '/system/role', parent_id: 'm1', sort: 2 }),
  // 菜单:工作台(根,无子)
  res({ id: 'm4', code: 'menu.dashboard', name: '工作台', path: '/', sort: 0 }),
  // 按钮(挂在菜单下)
  res({ id: 'b1', code: 'button.user.add', name: '新增', type: 'button', parent_id: 'm2' }),
  // API(平铺)
  res({ id: 'a1', code: 'user:list', name: '用户列表', type: 'api' }),
  res({ id: 'a2', code: 'user:read', name: '用户详情', type: 'api' }),
]

describe('assignTree', () => {
  describe('buildMenuSubtree', () => {
    it('按 parent_id 把平铺的菜单嵌套成树', () => {
      const tree = buildMenuSubtree(FLAT)
      const sys = tree.find((n) => n.id === 'm1')
      expect(sys).toBeDefined()
      expect(sys!.children?.map((c) => c.id)).toEqual(['m2', 'm3'])
    })

    it('只保留 menu 类型 —— 按钮 / API 不进菜单树(它们各自平铺一组,避免同 key 渲染两次)', () => {
      const tree = buildMenuSubtree(FLAT)
      const ids: string[] = []
      const walk = (nodes: Resource[]) => {
        for (const n of nodes) {
          ids.push(n.id)
          if (n.children?.length) walk(n.children)
        }
      }
      walk(tree)
      expect(ids).toEqual(expect.arrayContaining(['m1', 'm2', 'm3', 'm4']))
      expect(ids).not.toContain('b1')
      expect(ids).not.toContain('a1')
    })

    it('顶级菜单没有父节点,按 sort 升序', () => {
      const tree = buildMenuSubtree(FLAT)
      expect(tree.map((n) => n.id)).toEqual(['m4', 'm1'])
    })
  })

  describe('filterTreeByKeyword', () => {
    it('命中自身才保留', () => {
      const tree = buildMenuSubtree(FLAT)
      const hit = filterTreeByKeyword(tree, '工作台')
      expect(hit.map((n) => n.id)).toEqual(['m4'])
    })

    it('只命中子菜单时,父菜单要留着(否则搜子菜单会把整条路径滤没)', () => {
      const tree = buildMenuSubtree(FLAT)
      const hit = filterTreeByKeyword(tree, '用户管理')
      expect(hit.map((n) => n.id)).toEqual(['m1'])
      expect(hit[0]!.children?.map((c) => c.id)).toEqual(['m2'])
    })

    it('关键词为空时原样返回', () => {
      const tree = buildMenuSubtree(FLAT)
      expect(filterTreeByKeyword(tree, '  ')).toBe(tree)
    })
  })

  describe('matchResource', () => {
    it('code / 名称 / 路径 任一命中即可', () => {
      expect(matchResource(FLAT[1]!, 'user')).toBe(true)
      expect(matchResource(FLAT[1]!, '用户管理')).toBe(true)
      expect(matchResource(FLAT[1]!, '/system/user')).toBe(true)
      expect(matchResource(FLAT[1]!, '不存在')).toBe(false)
    })
  })

  describe('collectIdsByType', () => {
    it('all = 全部资源', () => {
      expect(collectIdsByType(FLAT, 'all').sort()).toEqual(['a1', 'a2', 'b1', 'm1', 'm2', 'm3', 'm4'])
    })

    it('menu 只收菜单(菜单是可授权的资源点,以前被漏掉了)', () => {
      expect(collectIdsByType(FLAT, 'menu').sort()).toEqual(['m1', 'm2', 'm3', 'm4'])
    })

    it('button / api 各自只收自己那类', () => {
      expect(collectIdsByType(FLAT, 'button')).toEqual(['b1'])
      expect(collectIdsByType(FLAT, 'api').sort()).toEqual(['a1', 'a2'])
    })
  })

  describe('countResources', () => {
    it('按类型计数 + 已选数(已选用真 ID 比对,19 位 ID 不能转 Number)', () => {
      const s = countResources(FLAT, ['m2', 'a1'])
      expect(s).toEqual({ total: 7, menu: 4, button: 1, api: 2, selected: 2 })
    })
  })
})
