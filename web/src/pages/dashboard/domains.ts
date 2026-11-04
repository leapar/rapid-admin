/**
 * 看板的业务域清单 —— **前端画的 ⟷ 后端能给的**
 *
 * ## 为什么单独拎一个文件
 *
 *   2026-11-01 起,看板内容按业务域权限过滤:
 *   服务端把**没权限的域整个从响应里省掉**
 *   (`Overview` / `Trend` 的字段是指针 / 切片 + `omitempty`
 *   ⇒ JSON 里压根没有这个 key),前端据此决定"这张卡片 / 这张图要不要画"。
 *
 *   这条约定横跨**三份清单**,任何一份对不上都是静默故障:
 *
 *   ① 后端 `Overview` 结构体的 json tag(= 服务端能给的指标)
 *   ② 后端 `dashboardMetricPerms` 的 key(= 每个指标要哪个权限码)
 *   ③ 前端 `DASHBOARD_KPI_KEYS`(= 前端会画哪些卡片)
 *
 *   前端那份如果少一个 ⇒ **后端给了、前端不画**(指标永远看不见);
 *   多一个 ⇒ **前端画一张永远空的卡**。
 *   两条都不会报错、不会让任何测试变红 —— 纯靠人记着。
 *
 *   ⇒ 三份清单由 `__tests__/domain-parity.test.ts` **逐个字段比对**
 *     (那份测试直接读后端 Go 源码,跟 api-route-parity 一个套路)。
 *
 * ## ⚠️ 这里只放"键",不放文案
 *
 *   文案要 `t()`,而 `t()` 只能在组件里调(hook)。
 *   把 label 放进这个文件 ⇒ 要么把 `t` 传进来(变成动态 key,
 *   i18n 守门网扫不到、拼错了也不报),要么把文案复制一份到别处。
 *   ⇒ 键在这里,label 在组件里(组件用 `Record<DashboardKpiKey, …>`
 *     把两份**焊死**:后端加一个指标而组件忘了加 label ⇒ 编译期就红)。
 */

/** 概览指标的 wire 字段名(与后端 `Overview` 的 json tag 的框架项一一对应) */
export const DASHBOARD_KPI_KEYS = [
  'file_total',
  'file_today',
  'file_storage_bytes',
  'folder_total',
  // 当前在线人数(2026-11-03)—— 纯计数,每个登录角色都看得到
  'user_online',
] as const

export type DashboardKpiKey = (typeof DASHBOARD_KPI_KEYS)[number]
