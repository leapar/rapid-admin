/**
 * 页面名解析 —— 面包屑 / tab 标题 共用同一份「路径 → 页面名」规则
 *
 * ⚠️ **为什么要抽出来**:面包屑要显示"当前页叫什么",tab 也要显示"当前页叫什么"。
 * 之前面包屑自己写了一份最长前缀匹配,tab 干脆**直接把 path 当标题** ——
 * 于是同一个页面在两个地方叫法不一样(tab 显示 "/system/user",侧边栏显示"用户管理")。
 * 全仓**只有这一份**规则,改一处两边同时生效。
 *
 * 数据源与侧边栏完全一致:`usePermissionStore.menus`(后端按当前身份返回的菜单树)
 *   菜单 code → i18n key 的映射与 Sidebar 共用 `resolveMenuLabel`。
 *
 * ⚠️ **不进 server**:页面名是**展示层**的东西,后端 seed 的 `sys_resource.name`
 * 只是兜底(缺 i18n 翻译时用),不参与业务逻辑。
 */
import type { Resource } from '@/types/permission'

/**
 * t() 的最小签名 —— 与 `locales/useI18n` 的 t 兼容。
 *
 * ⚠️ 单独声明而不是 import 类型:这个模块被非组件代码 import 时不该把
 * locales 整个拖进来(useI18n 依赖 zustand store)。
 */
export type Translate = (
  key: string,
  fallback?: string,
  vars?: Record<string, string | number>
) => string

/**
 * 菜单 code → i18n key
 *  - menu.system          → resourceMenu.menu_system
 *  - user:list            → resource.user_list
 *  - menu.dashboard       → resourceMenu.menu_dashboard
 */
export const codeToI18nKey = (code: string): string[] => {
  if (code.startsWith('menu.')) {
    return [`resourceMenu.${code.replace(/[:.-]/g, '_')}`]
  }
  return [`resource.${code.replace(/[:.-]/g, '_')}`]
}

/**
 * 菜单的显示名 —— i18n 优先,缺翻译回落后端的中文 name。
 *
 * ⚠️ **与 Sidebar 用的是同一个函数**:侧边栏和 tab 显示的必须是同一个名字,
 * 两处各写一份必然漂移(曾经侧边栏走 i18n、面包屑走后端 name,英文环境下
 * 面包屑一直是中文)。
 */
export function resolveMenuLabel(t: Translate, r: Resource): string {
  for (const k of codeToI18nKey(r.code)) {
    const v = t(k, '')
    if (v) return v
  }
  return r.name || r.code
}

/**
 * 路径末段 → i18n key
 *
 * ⚠️ **只登记"没有菜单节点"的页面**(子页 / 独立页):
 *   - audit   → /system/config/audit(系统配置下的审计日志子页)
 *   - profile → /profile(个人中心,菜单里没有这个节点)
 *
 * 有菜单节点的页面**不要**往这里塞 —— 菜单名才是权威,塞两份必然漂移。
 */
export const SEGMENT_LABEL_KEYS: Record<string, string> = {
  audit: 'tab.page.configAudit',
  profile: 'tab.page.profile',
}

/**
 * 最长前缀匹配:找出 path 命中的**最深**菜单。
 *
 * ⚠️ 为什么不能只按等值匹配:菜单里只有 "/system/config",
 * 而实际路径是 "/system/config/audit" —— 等值匹配会落空。
 */
export function findLongestPrefix(
  menus: Resource[],
  target: string,
  parents: Resource[] = []
): { chain: Resource[]; matchedPath: string } | null {
  let best: { chain: Resource[]; matchedPath: string } | null = null
  for (const m of menus) {
    const cur = [...parents, m]
    if (m.path && (target === m.path || target.startsWith(m.path + '/'))) {
      if (!best || m.path.length > best.matchedPath.length) {
        best = { chain: cur, matchedPath: m.path }
      }
    }
    if (m.children?.length) {
      const child = findLongestPrefix(m.children, target, cur)
      if (child && (!best || child.matchedPath.length > best.matchedPath.length)) {
        best = child
      }
    }
  }
  return best
}

/** 路径末段("/system/user" → "user") */
export function lastSegment(pathname: string): string {
  return pathname.split('/').filter(Boolean).pop() || ''
}

/**
 * 路径 → 页面名。**全仓唯一一份规则**,面包屑与 tab 都调它。
 *
 * 规则:
 *   1. 命中菜单且路径**正好等于**菜单 path → 菜单名
 *      ("/system/user" → "用户管理")
 *   2. 命中菜单但路径**更深**(子页 / 详情页):
 *      · 末段在 SEGMENT_LABEL_KEYS 里 → 该段的名字
 *        ("/system/config/audit" → "审计日志")
 *      · 否则是动态段(详情页,如 "/approval/123")→ 菜单名 + " #末段"
 *        ⚠️ **加末段不是为了好看**:两个详情页如果都叫"我的审批",
 *        tab 栏里就是两个一模一样的东西,分不清自己在看哪张单。
 *   3. 没命中任何菜单:
 *      · 末段在 SEGMENT_LABEL_KEYS 里 → 该段的名字("/profile" → "个人中心")
 *      · 否则 → 末段原文(总比整条 path 体面)
 *
 * ⚠️ **永不掉回整条 path**:这个函数存在的全部意义就是"别把路径当页面名"。
 */
export function resolvePageTitle(pathname: string, menus: Resource[], t: Translate): string {
  const seg = lastSegment(pathname)
  const segKey = SEGMENT_LABEL_KEYS[seg]
  const matched = findLongestPrefix(menus, pathname)

  if (matched) {
    const leaf = matched.chain[matched.chain.length - 1]
    // ⚠️ chain 至少有一个元素(findLongestPrefix 命中时才建 chain),
    //   但 noUncheckedIndexedAccess 让它成了 `Resource | undefined` —— 兜住,
    //   免得为一个理论上不可能的分支写断言。
    if (!leaf) return seg || pathname
    const label = resolveMenuLabel(t, leaf)
    // 路径正好等于菜单 path → 菜单名
    if (matched.matchedPath === pathname) return label
    // 子页:末段有登记名就用登记名
    if (segKey) return t(segKey, label)
    // 详情页(动态段):菜单名 + 末段,避免同名 tab
    return `${label} #${seg}`
  }

  // 没命中菜单
  if (segKey) return t(segKey, seg)
  return seg || pathname
}
