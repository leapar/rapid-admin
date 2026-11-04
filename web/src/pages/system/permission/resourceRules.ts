/**
 * 资源编辑弹窗的纯规则 —— 抽出来是为了能单测
 *
 * 2026-10-30 用户报了三件事,这里是其中两件(第三件是图标,词表在
 * `@/constants/resourceIcons`):
 *
 *  1. HTTP 方法没有「全部」—— 加一个哨兵值表示"不限定";
 *  2. 父资源可以跨类型选 —— 按类型约束。
 *
 * ⚠️ **这些规则在后端有一份镜像**(server/internal/service/role_impl.go 的
 * `allowedParentTypes`)。前端是**建议**(过滤下拉里的候选项),
 * 后端才是**约束**(拒绝非法写入)。两边必须一致,否则会出现
 * "前端不让选、后端却收" 或者 "前端让选、后端拒了"的割裂。
 */
import type { Resource } from '@/types/permission'

export type ResourceType = Resource['type']

/**
 * HTTP 方法「全部」的哨兵值
 *
 * ⚠️ **不能用空串当哨兵** —— antd Select 的 `allowClear` 清空后值是
 * `undefined`,而 `value=""` 在 rc-select 里是"选中了一个空字符串选项",
 * 两者在表单里表现不一样(一个是"有值",一个是"没值")。
 * 用一个显式哨兵把"不限"和"没填"区分开,提交时再统一翻译成空串。
 */
export const METHOD_ALL = 'ALL'

/**
 * 「无图标」的哨兵值
 *
 * ⚠️ **不能用空串** —— antd Select 里 `value=""` 会渲染成一个**空标签**
 *   (不是占位符),看起来像坏了。undefined 才是"没值"的正确表示。
 */
export const ICON_NONE = '__none__'

/** 可选的 HTTP 方法 */
export const HTTP_METHODS = ['GET', 'POST', 'PATCH', 'PUT', 'DELETE'] as const

/**
 * 「子资源类型 → 允许的父资源类型」白名单
 *
 * 为什么菜单只能挂在菜单下面:
 *   菜单树(前端 buildMenuTree / 后端菜单查询)只认菜单节点。
 *   把菜单挂到按钮/接口下面 ⇒ 这个菜单在侧边栏里**永远不出现**
 *   —— 它不在任何菜单的子树里,界面不报错、也不像"权限丢了",就是菜单没了。
 *
 * ⚠️ **2026-11-03 用户拍板:button / api 不允许有父级**(原来是
 *   button→[menu,button]、api→[menu,button,api])。parent_id 在这个系统里
 *   只有一个消费者 —— 菜单树(侧边栏 / 分配 Drawer 树形勾选);按钮和接口
 *   在所有消费方里都是平铺列表(seed 恒 0 / Drawer 不进树 / casbin 只认
 *   code 字符串),父级对它们是**没有读者的死数据** ⇒ 编辑弹窗直接不渲染
 *   父级字段,后端同步拒绝(镜像见 role_impl.go 的 allowedParentTypes)。
 */
export const ALLOWED_PARENT_TYPES: Record<ResourceType, ResourceType[]> = {
  menu: ['menu'],
  button: [],
  api: [],
  // 'data' 后端目前不认(只允许 menu/button/api),但类型里留着,按 api 对待
  data: [],
}

/** 某个资源类型允许的父资源类型 */
export function allowedParentTypes(type: ResourceType): ResourceType[] {
  return ALLOWED_PARENT_TYPES[type] ?? []
}

/** 父资源候选项 */
export interface ParentCandidate {
  value: string
  /** 展示文案:`— ` 前缀表示层级深度,后面是 `名称 (编码)` */
  label: string
  /** 层级深度(顶级 = 0) */
  depth: number
  type: ResourceType
}

/**
 * 计算每个资源在树里的深度
 *
 * ⚠️ **后端 GET /system/resources/tree 返的是扁平列表**(model.SysResource
 *   没有 children 字段,后端也从不填)⇒ 深度只能靠 parent_id 自己推。
 *   推法:从自己往上数祖先个数。
 *
 * 祖先链上有环(历史脏数据)时**必须有上界**,否则这里死循环。
 */
function computeDepths(flat: Resource[]): Map<string, number> {
  const byId = new Map<string, Resource>()
  for (const r of flat) byId.set(r.id, r)

  const depthOf = (id: string): number => {
    const seen = new Set<string>()
    let depth = 0
    let cur = byId.get(id)
    while (cur) {
      const pid = cur.parent_id ?? '0'
      if (!pid || pid === '0') break
      if (seen.has(pid)) break // 环 —— 停下,别死循环
      seen.add(pid)
      const parent = byId.get(pid)
      if (!parent) break
      depth++
      cur = parent
    }
    return depth
  }

  const depths = new Map<string, number>()
  for (const r of flat) depths.set(r.id, depthOf(r.id))
  return depths
}

/**
 * 找出某个资源的所有后代(自己除外)
 *
 * ⚠️ **后代必须按 parent_id 反查,不能靠 children 递归** ——
 *   后端返的是扁平列表,`children` 恒为 undefined,靠它算出来的"后代"永远是空集,
 *   于是后代照样能被选成父资源 ⇒ 父子成环。
 */
export function descendantIds(flat: Resource[], selfId: string): Set<string> {
  const childrenOf = new Map<string, string[]>()
  for (const r of flat) {
    const pid = r.parent_id ?? '0'
    const list = childrenOf.get(pid)
    if (list) list.push(r.id)
    else childrenOf.set(pid, [r.id])
  }

  const banned = new Set<string>([selfId])
  const queue = [selfId]
  while (queue.length) {
    const cur = queue.shift()!
    for (const child of childrenOf.get(cur) ?? []) {
      if (banned.has(child)) continue
      banned.add(child)
      queue.push(child)
    }
  }
  return banned
}

/**
 * 父资源候选项
 *
 * 三重过滤:
 *   1. **排除自己和自己的后代** —— 否则能把自己设成儿子的父亲,父子成环;
 *      环上的节点都不是根,在 buildMenuTree 里会**凭空消失**(菜单连同子树静默不见)。
 *   2. **按类型过滤** —— 菜单只能挂在菜单下,按钮可挂菜单/按钮,接口可挂菜单/按钮/接口。
 *   3. **顶级永远是合法选项**(不在列表里,单独一个入口)。
 */
export function buildParentCandidates(
  flat: Resource[],
  self: { id?: string; type: ResourceType },
): ParentCandidate[] {
  const allowed = allowedParentTypes(self.type)
  const banned = self.id ? descendantIds(flat, self.id) : new Set<string>()
  const depths = computeDepths(flat)

  const out: ParentCandidate[] = []
  for (const r of flat) {
    if (banned.has(r.id)) continue
    if (!allowed.includes(r.type)) continue
    const depth = depths.get(r.id) ?? 0
    out.push({
      value: r.id,
      label: `${'— '.repeat(depth)}${r.name} (${r.code})`,
      depth,
      type: r.type,
    })
  }
  return out
}

/**
 * 表单里的 method 值 → 提交给后端的值
 *
 * 「全部」和"没填"都翻译成空串 —— 后端 `ResourceUpdateInput.Method` 是
 * `*string`,空串 = **显式清空这一列**(区别于"没传这个字段")。
 */
export function normalizeMethodPayload(value: unknown): string {
  if (value === undefined || value === null) return ''
  const v = String(value).trim()
  if (v === '' || v === METHOD_ALL) return ''
  return v.toUpperCase()
}

/**
 * 表单里的 icon 值 → 提交给后端的值
 *
 * 「无图标」和"没填"都翻译成空串 —— 后端 `ResourceUpdateInput.Icon` 是
 * `*string`,空串 = **显式清空这一列**(区别于"没传这个字段")。
 */
export function normalizeIconPayload(value: unknown): string {
  if (value === undefined || value === null) return ''
  const v = String(value).trim()
  if (v === '' || v === ICON_NONE) return ''
  return v
}
