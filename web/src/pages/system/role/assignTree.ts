/**
 * 分配权限 Drawer 的纯逻辑(抽出来是为了能单测 —— 见 __tests__/assignTree.test.ts)
 *
 * ⚠️ **后端 GET /system/resources/tree 返的是「扁平列表」,不是树**:
 *   model.SysResource 没有 children 字段,后端也从不填。
 *   2026-10-25 修 Bug 前,这个 Drawer 直接把平铺列表当树用
 *   (`m.children` 恒为 undefined)→ 菜单层级整个丢了,
 *   并且菜单节点被标成 `checkable: false` → **菜单永远勾不上**。
 *   现在统一在这里按 parent_id 建树。
 */
import type { Resource } from '@/types/permission'
import { buildMenuTree } from '@/utils/resourceTree'

export type ResourceTypeTab = 'all' | 'menu' | 'button' | 'api'

/** 资源是否命中关键词(code / 名称 / 路径) */
export function matchResource(r: Resource, keyword: string): boolean {
  const kw = keyword.trim().toLowerCase()
  if (!kw) return true
  return (
    r.code.toLowerCase().includes(kw) ||
    r.name.toLowerCase().includes(kw) ||
    (r.path ?? '').toLowerCase().includes(kw)
  )
}

/**
 * 菜单子树:只取 type=menu 的资源,按 parent_id 嵌套。
 * 按钮 / API 不进这棵树(它们在 Drawer 里是各自独立的一组平铺列表),
 * 这样每个资源只渲染一次,不会出现同一个 key 出现在两个分组里。
 */
export function buildMenuSubtree(flat: Resource[]): Resource[] {
  return buildMenuTree(flat.filter((r) => r.type === 'menu'))
}

/**
 * 树按关键词过滤:自身命中 **或** 任一后代命中才保留。
 * 只按自身过滤会把「搜子菜单 → 父菜单被滤掉 → 子菜单跟着消失」。
 */
export function filterTreeByKeyword(tree: Resource[], keyword: string): Resource[] {
  const kw = keyword.trim().toLowerCase()
  if (!kw) return tree
  const walk = (nodes: Resource[]): Resource[] => {
    const out: Resource[] = []
    for (const n of nodes) {
      const children = walk(n.children ?? [])
      const selfHit =
        n.code.toLowerCase().includes(kw) ||
        n.name.toLowerCase().includes(kw) ||
        (n.path ?? '').toLowerCase().includes(kw)
      if (selfHit || children.length > 0) out.push({ ...n, children })
    }
    return out
  }
  return walk(tree)
}

/**
 * 复刻 antd(rc-tree 5.13 conductCheck)的级联勾选规则,把一组 ID 补全成
 * Tree 内部那一套状态:
 *   1. 自顶向下:勾了父节点 → 它的所有后代都算勾上
 *   2. 自底向上:某个节点的所有子节点都勾上了 → 这个节点也算勾上
 *
 * ⚠️ 为什么需要自己复刻一遍:
 *   rc-tree 在收到受控 checkedKeys 时会**自己** conduct 一遍用于显示,
 *   但**不会回调 onCheck** —— 于是 state 里还是原始 ID,树上却是 conduct 后的。
 *   结果就是「已选 1」但树上两个对勾,保存时也跟用户看到的对不上。
 *   加载已绑 ID 时先 conduct 一遍,state / 树上显示 / 保存载荷三者才对齐。
 *
 * 菜单之外没有父子关系(按钮 / API 在 Drawer 里各自平铺一组),所以只需处理菜单树。
 */
export function conductCheckedIds(flat: Resource[], ids: string[]): string[] {
  const tree = buildMenuSubtree(flat)
  const selected = new Set(ids)

  // 规则 1:勾了父 → 所有后代
  const fillDown = (nodes: Resource[]): void => {
    for (const n of nodes) {
      if (selected.has(n.id)) {
        const stack = [...(n.children ?? [])]
        while (stack.length) {
          const c = stack.pop()!
          selected.add(c.id)
          stack.push(...(c.children ?? []))
        }
      }
      fillDown(n.children ?? [])
    }
  }
  fillDown(tree)

  // 规则 2:所有子都勾上 → 父也算勾上
  const fillUp = (nodes: Resource[]): boolean => {
    let all = true
    for (const n of nodes) {
      const children = n.children ?? []
      const childAll = fillUp(children)
      if (childAll && children.length > 0) selected.add(n.id)
      if (!selected.has(n.id)) all = false
    }
    return all
  }
  fillUp(tree)

  return Array.from(selected)
}

/** 按类型收集资源 ID('all' = 全部类型),供「本视图全选 / 清空」用 */
export function collectIdsByType(flat: Resource[], type: ResourceTypeTab): string[] {
  const out: string[] = []
  for (const r of flat) {
    if (type === 'all' || r.type === type) out.push(r.id)
  }
  return out
}

/** 统计:各类型总数 + 已选数 */
export function countResources(
  flat: Resource[],
  selectedKeys: string[],
): { total: number; menu: number; button: number; api: number; selected: number } {
  const sel = new Set(selectedKeys)
  let menu = 0
  let button = 0
  let api = 0
  let selected = 0
  for (const r of flat) {
    if (r.type === 'menu') menu++
    else if (r.type === 'button') button++
    else if (r.type === 'api') api++
    if (sel.has(r.id)) selected++
  }
  return { total: flat.length, menu, button, api, selected }
}
