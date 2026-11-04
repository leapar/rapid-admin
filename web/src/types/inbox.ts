/**
 * 站内信类型 —— 对应服务端 inbox.MessageView
 * (server/internal/pkg/inbox/inbox.go)
 *
 * ⚠️ **改字段必须同 commit 改 server + web + doc**(项目规则)。这里是
 * wire 契约的镜像,不是"前端自己理解的形状"。
 *
 * ## 为什么 ID 是 string
 *
 * 后端是 uint64(雪花),裸数字过 JSON 会被前端 JSON.parse 静默截断,
 * 末几位变 0,回传后端就点到**错误的那一条**。服务端 MessageView 显式
 * 转成字符串就是为了这个 —— 前端**永远不要 Number(msg.id)**。
 *
 * ## 为什么时间只有一种格式
 *
 * 服务端统一走 model.Time 的 MarshalJSON,输出恒为 `+08:00` 秒级
 * (即 `2026-10-22T12:38:05+08:00`)。这里曾经有两种格式并存
 * (本地带纳秒 / UTC 秒级),已在 M74 统一。
 *
 * ## null 的约定
 *
 * 服务端已把 NULL 与零值归一成同一个形状:
 *   - `ref_id` / `sender_id` 为 0 时是**空串**而不是 "0"
 *   - `content` / `link` / `ref_type` 是字符串,**不会是 null**
 * 唯一保留 null 的是 `read_at` / `expired_at`(可选时间点),
 * **判未读请用 `read_at === null`**,不要比时间戳。
 */
export interface InboxMessage {
  /** 雪花 ID,**字符串**。永远别转 number */
  id: string
  /** system / biz / notice */
  msg_type: string
  title: string
  content: string
  /** 可空(空串)。非空时前端渲染"查看详情" */
  link: string
  /** 关联业务资源类型,框架只透传 */
  ref_type: string
  /** 无关联时为空串 */
  ref_id: string
  /** 空串 = 系统发的 */
  sender_id: string
  /** ISO8601 +08:00;null = 未读 */
  read_at: string | null
  expired_at: string | null
  /** ISO8601 +08:00 */
  created_at: string
  updated_at: string
}

/** msg_type 的已知取值。未知值(业务自定义)前端按 system 兜底渲染 */
export const INBOX_MSG_TYPES = ['system', 'biz', 'notice'] as const
export type InboxMsgType = (typeof INBOX_MSG_TYPES)[number]