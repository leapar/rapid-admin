/**
 * 资源树工具(纯函数,可单测)
 *
 * ⚠️ **后端所有资源接口返的都是「扁平列表」,不是树**:
 *   - GET /system/resources/tree —— 名字叫 tree,实际返 `[]SysResource` 平铺,
 *     `children` 字段后端**从来不填**(model.SysResource 没有这个字段)。
 *   - 所以前端凡是"按 parent_id 拼层级"的地方都必须自己建树。
 *   历史上踩过的坑:分配权限 Drawer 直接把平铺列表当树用
 *   (见 `m.children` 恒为 undefined),菜单层级整个丢了。
 *
 * buildMenuTree 原来放在 stores/permission.ts 里(只给 store 自己用),
 * 2026-10-25 提到 utils —— 分配权限 Drawer 也要用,纯函数不该挂在 store 上。
 */
import type { Resource } from '@/types/permission'

/**
 * buildMenuTree 把扁平的 Resource[] (按 parent_id 关联) 构造成顶层 + children 嵌套树
 *   - 顶级 parent_id=0 或父节点不在列表中
 *   - 排序按 sort 升序
 *   - 跳过 is_active=false 的节点(及它们的子树,避免隐藏菜单下还显示二级)
 *
 * ⚠️ 2026-10-21:字段名以 snake_case 为准。旧代码这里读 r.isActive / r.parentId,
 * 而后端 SysResource 发的是 is_active / parent_id —— 也就是说
 * "跳过 isActive=false 的节点" 这条过滤**从来没生效过**(两个 key 都 undefined,
 * 一路兜底到 true)。现在按 wire 实况读,注释也一并纠正。
 */
export function buildMenuTree(flat: Resource[]): Resource[] {
  const isEnabled = (r: Resource) => (r.is_active ?? r.isActive ?? r.enabled ?? true) !== false
  const parentIdOf = (r: Resource) => r.parent_id ?? r.parentId ?? '0'

  const byId = new Map<string, Resource>()
  const roots: Resource[] = []
  for (const r of flat) {
    byId.set(r.id, { ...r, children: [] })
  }
  for (const r of flat) {
    const node = byId.get(r.id)!
    if (!isEnabled(r)) continue
    const pid = parentIdOf(r)
    if (pid === '0' || !byId.has(pid)) {
      roots.push(node)
    } else {
      const parent = byId.get(pid)!
      if (!parent.children) parent.children = []
      parent.children.push(node)
    }
  }
  const sortRec = (arr: Resource[]) => {
    arr.sort((a, b) => (a.sort || 0) - (b.sort || 0))
    for (const r of arr) if (r.children?.length) sortRec(r.children)
  }
  sortRec(roots)
  return roots
}
