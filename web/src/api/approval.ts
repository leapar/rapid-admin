/**
 * 通用审批系统 —— 审批单 API(M87)
 *
 * 文档:docs/通用审批系统设计方案V1.0.md §7.1 / §7.3 / §7.4 / §8.2
 *
 * ⚠️ **路径前缀是 `/system`** —— 后端挂在 `/rapid-admin/v1/system/approvals`
 *   (router.go 的 `api.Group("/system")`),baseURL 已经是 `/rapid-admin/v1`,
 *   所以这里写 `/system/approvals`。
 *   ✅ 2026-10-26:`api/asset.ts` 等五个模块漏 `/biz` 前缀的既有缺陷**已修**
 *   (见 `src/api/__tests__/api-route-parity.test.ts` 那条守门网),
 *   现在全仓的写法统一了:**路径 = 后端完整路径去掉 baseURL**。
 */
import { http } from './client'
import { PageResponse } from '@/types/api'
import {
  ApprovalDetail,
  ApprovalListItem,
  ApprovalListQuery,
  ActReq,
  UpdateDraftReq,
} from '@/types/approval'

/** 查询参数(宽松类型) */
type QueryParams = Record<string, string | number | boolean | undefined>

/**
 * 分页参数翻译:`pageSize` / `page_size` → `size`
 *
 * ⚠️ **历史包袱**(2026-10-25 已在框架层统一,见技术方案 §7.7.8):
 *   · 后端 `GetPage`(`server/internal/api/handler/handler.go:88`)只读 **`size`**,
 *     全仓不读 `page_size`
 *   · 以前前端三处写法不一:`useTable` 写 `pageSize` / `PageRequest` 声明
 *     `page_size` / 本文件自己翻成 `size`
 *   · 结果**全仓用 `useTable` 的页面,页大小选择器都是失效的**
 *     (选了 50 条,后端拿到 size=0,按默认值 20 返回;页码是对的,
 *      因为 `page` 三方一致)
 *
 *   现在 `useTable` 内部统一写 `size`,`api/client.ts` 的出口也会把
 *   `page_size` / `pageSize` 翻成 `size`。本函数保留是因为
 *   `ApprovalListQuery` 允许调用方用旧名传 size,这里先归一化再发。
 */
export function toPageParams(q: QueryParams): QueryParams {
  const size = q.size ?? q.pageSize ?? q.page_size
  return size === undefined ? { ...q } : { ...q, size }
}

export const approvalApi = {
  /**
   * 列表(§8.2)
   *
   * ⚠️ **`box` 必填** —— todo / mine / done,**没有 `all`**。
   *   少传一个参数不该等于"给我看全部"(§12 第 9 条)。
   */
  list: (q: ApprovalListQuery) =>
    http.get<PageResponse<ApprovalListItem>>(
      '/system/approvals',
      toPageParams(q as unknown as QueryParams)
    ),

  /** 详情 —— `rounds[]` 和 `logs[]` 一次返回(§7.5) */
  detail: (id: string) => http.get<ApprovalDetail>(`/system/approvals/${id}`),

  /**
   * 改草稿(§7.4)—— 三个字段都是**可选的指针式语义**,nil = 不改
   *
   * ⚠️ **不写意见流水** —— 它不是审批动作。
   */
  updateDraft: (id: string, data: UpdateDraftReq) =>
    http.put<void>(`/system/approvals/${id}/draft`, data),

  /**
   * 送审(§3.3)
   *
   * ⚠️ **首次提交与打回后重提走同一个端点**,守卫不同:
   *   首次要"草稿且有审批人",重提要"打回过 **且内容改过**"。
   *   原样重提会被拒(§3.3 的"重提必须改内容"守卫)。
   */
  submit: (id: string, comment?: string) =>
    http.post<void>(`/system/approvals/${id}/submit`, { comment: comment ?? '' }),

  /** 作废 —— **running 状态不给作废**(D9) */
  cancel: (id: string) => http.post<void>(`/system/approvals/${id}/cancel`),

  /**
   * 审批动作(§3.3)—— approve / reject / rollback 三合一
   *
   * ⚠️ **驳回/打回的 comment 不少于 5 个字**(§3.4)—— 后端会拒。
   *   前端那道校验只是体验,边界在后端。
   */
  act: (id: string, data: ActReq) => http.post<void>(`/system/approvals/${id}/act`, data),
}
