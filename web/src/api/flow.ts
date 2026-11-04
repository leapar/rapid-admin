/**
 * 通用审批系统 —— 流程模板 API(M87)
 *
 * 文档:docs/通用审批系统设计方案V1.0.md §7.2 / §8.1
 *
 * ⚠️ **没有"单个关卡 / 单条规则"的端点** —— 后端只有模板级的 5 个
 *   (list / detail / create / update / delete),而 `PUT /flows/:id` 是
 *   **整棵树替换**(硬删旧关卡再整棵重写)。
 *   所以前端在关卡页/规则页改任何东西,都是**改本地那棵树 → 整棵 PUT 回去**。
 *   这是本仓 API 形状决定的,不是前端偷懒(见 §13 M87-1a)。
 */
import { http } from './client'
import { PageResponse } from '@/types/api'
import { toPageParams } from './approval'
import { FlowDetail, FlowListItem, FlowListQuery, FlowSaveReq } from '@/types/approval'

export const flowApi = {
  /**
   * 模板列表
   *
   * ⚠️ **`is_active` 是三态** —— 不传 = 不过滤,传 `false` = 只看停用的。
   *   用 `undefined` 表达"不传",别传 `false` 上去(那会变成"只看停用的")。
   */
  list: (q: FlowListQuery) =>
    http.get<PageResponse<FlowListItem>>(
      '/system/flows',
      toPageParams(q as unknown as Record<string, string | number | boolean | undefined>)
    ),

  /** 模板详情(含关卡与规则) */
  detail: (id: string) => http.get<FlowDetail>(`/system/flows/${id}`),

  /** 建模板 —— ⚠️ `code` 必填且要符合格式(小写字母/数字/下划线) */
  create: (data: FlowSaveReq) => http.post<void>('/system/flows', data),

  /**
   * 改模板 —— **整棵树替换**
   *
   * ⚠️ **有非终态实例时服务端会拒**(D7),报错文案里带"N 条在跑"。
   *   这是有意的:改了结构,正在跑的实例的"第 2 关"会指向完全不同的关卡,
   *   症状是单子卡死,而没人会想到是模板被改过。
   */
  update: (id: string, data: FlowSaveReq) => http.put<void>(`/system/flows/${id}`, data),

  /**
   * 删模板 —— 软删 + **改写 code 让出唯一坑位**(D7)。
   * ⚠️ 所以删完之后 code 变了,不是"原 code + 后缀"(见 §13 M86-1i)。
   */
  remove: (id: string) => http.delete<void>(`/system/flows/${id}`),
}
