/**
 * Layout 统一导出
 */
export { BasicLayout } from './BasicLayout'
export { Header } from './Header'
export { Sidebar } from './Sidebar'
export { Breadcrumb } from './Breadcrumb'
export { IdentitySwitcher } from './IdentitySwitcher'
export { LanguageSwitcher } from './LanguageSwitcher'
// 页面名解析(面包屑用;tab 标题的规则原本也在这,2026-10-25 多 tab 下线后只剩面包屑)
// 见 pageName.ts
export { resolvePageTitle, resolveMenuLabel, findLongestPrefix } from './pageName'
