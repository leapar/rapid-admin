/**
 * 站内信 API —— 框架层(M72 事件推送配套 + M74 列表入口)
 *
 * 路径: 后端 /rapid-admin/v1/biz/inbox/* (server/internal/api/router/router.go:427-430)
 *
 * 四个方法一一对应后端四条路由,**不增不减**:
 *   GET  /biz/inbox/messages            → list          rbac inbox:list
 *   GET  /biz/inbox/unread-count        → unreadCount   rbac inbox:read
 *   POST /biz/inbox/messages/:id/read   → markRead      rbac inbox:mark_read
 *   POST /biz/inbox/mark-all-read       → markAllRead   rbac inbox:mark_read
 *
 * ⚠️ 这三个写/读方法此前是**零调用方**(M72 只接了角标)。M74 的列表弹层
 * 把它们接上 —— 没有新增后端接口,也没有新增权限码。
 *
 * ⚠️ **详情页没有对应接口,也不需要**:list 返回的行已带 content/link/ref_*,
 * 前端展开即可。真要单独一个详情接口时,记得那是新增能力不是"补前端"。
 */
import { http } from './client'
import type { InboxMessage } from '@/types/inbox'

export const inboxApi = {
  /**
   * 未读数 —— GET /inbox/unread-count?msg_type=
   *
   * ⚠️ **默认 silent**:这条挂在 Header 上,每个登录用户都会打。
   * 而它带 rbac("inbox","read") —— 没有 inbox:read 的角色(平台超管以外
   * 大多数)拿到的是 403。不静默的话,页面上每刷新一次就弹一个红色的
   * "无权限",而角标本来就该是"没权限就不显示"。
   * 调用方据返回值决定角标显不显示,不需要感知 403。
   *
   * 返回 { unread: number }。
   */
  unreadCount: (msgType = '', silent = true) =>
    http.get<{ unread: number }>(
      '/biz/inbox/unread-count',
      msgType ? { msg_type: msgType } : undefined,
      { silent },
    ),

  /**
   * 消息列表 —— GET /inbox/messages?page=&size=&msgType=&onlyUnread=
   *
   * ⚠️ **默认 silent 的理由比 unreadCount 更硬**:这条**不在启动路径上**
   * (只在用户点开铃铛时才打),但它是 403 概率最高的一条 ——
   * inbox:list 绑给超管和只读角色,其余 11 个角色都没有。所以点开铃铛
   * 对多数人本来就是 403,让组件自己决定"没权限时显示什么",而不是让
   * http 层弹一个 toast(用户莫名其妙地看到"操作失败")。
   *
   * 参数字段名是**驼峰**(msgType/onlyUnread)—— 这是后端 handler 里
   * c.Query() 的原样,M74 顺手把它对齐成 snake_case,与全仓其它接口一致。
   * 没有前端在用旧名,迁移成本为零。
   */
  list: (params: { page?: number; size?: number; msgType?: string; onlyUnread?: boolean } = {}) => {
    const q: Record<string, string | number> = {}
    if (params.page) q.page = params.page
    if (params.size) q.size = params.size
    if (params.msgType) q.msg_type = params.msgType
    // ⚠️ 必须转成字符串 'true' 而不是塞 boolean:后端 c.Query() 拿到的永远是
    // 字符串,现在判断的是 `== "true" || == "1"`。传 JSON 的 true 过去会变成
    // "true" 恰好对,但传 false 过去就成了 "false" —— 而那条分支根本不会被
    // 命中(它只判 true/1),所以 false 是安全的。写死 'true' 是为了不依赖
    // axios 的序列化行为。
    if (params.onlyUnread) q.only_unread = 'true'
    return http.get<{ list: InboxMessage[]; total: number; page: number; size: number }>(
      '/biz/inbox/messages',
      q,
      { silent: true },
    )
  },

  /**
   * 单条标已读 —— POST /inbox/messages/:id/read
   *
   * ⚠️ **id 是字符串,别 Number()**:后端是 uint64(见 MessageView 注释),
   * 一旦变成 number 就会静默截断,标的是**另一条**记录。
   *
   * 幂等:已读的再标一次不报错,不是我的也静默成功(防 IDOR 的既有行为)。
   */
  markRead: (id: string) => http.post(`/biz/inbox/messages/${encodeURIComponent(id)}/read`, undefined, { silent: true }),

  /**
   * 全部标已读 —— POST /inbox/mark-all-read?msg_type=
   * 返回 { affected: number }
   *
   * ⚠️ 查询串拼在 URL 里而不是走 opts.params:RequestOptions 从
   * AxiosRequestConfig 里 Omit 掉了 `params`(本仓的 GET 走独立的第二
   * 形参,POST 一直没有查询参数这条路径)。为了一个可选的 msg_type 去改
   * RequestOptions 不划算 —— M74 的调用点本来就不传 msg_type。
   */
  markAllRead: (msgType = '') =>
    http.post<{ affected: number }>(
      msgType ? `/biz/inbox/mark-all-read?msg_type=${encodeURIComponent(msgType)}` : '/biz/inbox/mark-all-read',
      undefined,
      { silent: true },
    ),
}