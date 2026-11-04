/**
 * 审计中心 —— 页面内共用的小工具
 *
 * # 为什么不放进 @/utils/format
 *
 *   那一支是**全站**通用格式化(时间 / 数字 / 脱敏)。这里这个只服务审计三页,
 *   而且它解决的是**审计表特有的 wire 形状**(ISO8601 字符串 **或 null**),
 *   放进全站工具就等于让全站都背这个特例。
 *
 * # 为什么需要它
 *
 *   后端 `model.Time` 零值时序列化成 **null**(不是 "0001-01-01"),
 *   `*Time` 没赋值时也是 null。于是审计三页的时间字段类型是
 *   `string | null` —— 而 `formatDate.datetime` 的入参签名是
 *   `string | number | Date | undefined`,**不接受 null**
 *   (strict 模式下会报 TS2345)。
 *
 *   三页 × 4~5 个时间列 = 十来处 `?? undefined` 的噪音,
 *   抽一个具名函数比每处写 `?? undefined` 清楚。
 */
import { formatDate } from '@/utils/format'

/**
 * 审计表的时间字段 → 展示文本
 *
 * @param v wire 上的时间值(ISO8601 +08:00 字符串,或 null / undefined)
 * @returns 格式化后的时间;空值返 '-'
 *
 * ⚠️ **不做时区换算的取舍**:wire 上是东八区(后端 `timeutil.AsiaShanghai` 固化),
 *   这里走 `formatDate.datetime` → dayjs 会转成**浏览器本地时区**。
 *   与全站其它页面一致(审计页不该有一套自己的时区),
 *   但要知道:后端记的是东八区,运营在别的时区看会差几个小时。
 */
export function fmtAuditTime(v?: string | null): string {
  return formatDate.datetime(v ?? undefined)
}

/**
 * 审计表的 JSON 文本字段 → 缩进后的可读文本
 *
 * 后端存的是 `before` / `after` / `request_params` 这类**文本**列,
 * 内容通常是 JSON 但**不保证是**(早期记录可能是纯文本或空串)——
 * 所以这里 parse 失败时**原样返回**,不抛异常、不显示 "Invalid"。
 */
export function fmtAuditJson(v?: string | null): string {
  if (!v) return ''
  try {
    return JSON.stringify(JSON.parse(v), null, 2)
  } catch {
    return v
  }
}
