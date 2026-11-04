/**
 * Breadcrumb —— 面包屑(根据当前路由自动生成)
 *
 * 2026-10-18 改进:子页(/system/config/audit 等)走最长前缀匹配
 *   - 例: pathname=/system/config/audit 时,菜单只有 /system/config
 *   - 用 startsWith 找到最长 prefix 的菜单,把它当父级,后面追加当前 pathname 最后一段作为"当前页"
 *   - 避免每个子页都要在 seed.go 加一个 menu.*.* 子节点
 *
 * ⚠️ **2026-10-24(M3 之后的前端调整)**:最长前缀匹配与"当前页叫什么"抽到了
 * `./pageName.ts`,与 tab 标题**共用同一份规则**。之前面包屑自己写了一份、
 * tab 干脆显示 path,同一个页面两个地方叫法不一样。
 * 顺带:菜单名从"后端 name"改成 **i18n**(英文环境下面包屑不再是中文)。
 */
import React from 'react'
import { Breadcrumb as AntBreadcrumb } from 'antd'
import { useLocation, Link } from 'react-router-dom'
import { usePermissionStore } from '@/stores/permission'
import { useI18n } from '@/locales'
import { findLongestPrefix, resolveMenuLabel, lastSegment, SEGMENT_LABEL_KEYS } from './pageName'

export const Breadcrumb: React.FC = () => {
  const { t } = useI18n()
  const menus = usePermissionStore((s) => s.menus)
  const location = useLocation()

  const matched = findLongestPrefix(menus, location.pathname) || null
  const chain = matched?.chain || []

  if (chain.length === 0) return null

  // 子页:在匹配菜单后追加当前 pathname 最后一段作"当前页"
  const isChild = matched && location.pathname !== matched.matchedPath
  const items = chain.map((m) => ({
    title:
      m.path && m.path !== location.pathname ? (
        <Link to={m.path}>{resolveMenuLabel(t, m)}</Link>
      ) : (
        resolveMenuLabel(t, m)
      ),
  }))
  if (isChild) {
    // 子页 label:已知 segment 走 SEGMENT_LABEL_KEYS 静态 map,未知走 path 末段原文
    // P1+ 改进:从 react-router v6 的 handle.title 或 route meta 读
    const seg = lastSegment(location.pathname)
    const segKey = SEGMENT_LABEL_KEYS[seg]
    items.push({ title: segKey ? t(segKey, seg) : seg })
  }

  return <AntBreadcrumb style={{ margin: '6px 0' }} items={items} />
}

export default Breadcrumb
