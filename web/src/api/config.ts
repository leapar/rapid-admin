/**
 * 字典 / 平台配置中心 API
 *
 * 路径规则:
 *   字典: 后端 /rapid-admin/v1/system/dicts (复数, router.go:154)
 *   平台配置: 后端 /rapid-admin/v1/platform/configs(/platform group 下)
 *
 * 2026-10-18 收窄:
 *   - 系统配置只走 update 一条写通道
 *   - 用户不会创建/删除配置项(走 seed.go + 软删 is_active=false)
 *   - 前端 configApi 只剩:groups / list / update / auditLogs
 *   - 删除:create / remove / detail(0 caller)
 */
import { http } from './client'
import type { PageResponse } from '@/types/api'
import type {
  ConfigGroupKV,
  ConfigItem,
  ConfigAuditLog,
  UpdateConfigBody,
  ConfigListQuery,
  ConfigAuditQuery,
} from '@/types/config'

/* ====================== 字典 ====================== */

export interface DictQuery {
  groupCode: string
  keyword?: string
  status?: number
}

export interface DictItem {
  id: string
  group_code: string
  item_key: string
  item_value: string
  label: string
  sort: number
  /**
   * 启用 / 禁用 —— **boolean,不是 1 / 0**。
   *
   * ⚠️ 后端 `model.SysDict.IsActive` 是 Go `bool`,`json:"is_active"` 序列化出来
   *   就是 true / false(SQLite 里存成 1 / 0 只是**存储层**表示,不是 wire)。
   * ⚠️ 2026-10-28 M107:这里以前声明成 `number`,于是页面只能写
   *   `v === 1 || v === true` 兜底 —— 兜底掩盖了类型撒谎,真正的 bug 是声明本身。
   *   (2026-10-18 就因为同一个坑踩过一次:全表 113 行都显示成"禁用"。)
   */
  is_active: boolean
  description?: string
}

/** 字典分组(2026-10-18): code + 中文 label, 给字典管理页"业务类型"下拉用 */
export interface DictGroup {
  code: string
  label: string
}

export const dictApi = {
  /**
   * 取整组字典项(key -> label)
   * 后端 endpoint: GET /dicts?groupCode=xxx (query 传参,非 path)
   *
   * ⚠️ 2026-10-28 M107:以前是手拼 `?groupCode=${groupCode}` —— **不编码**。
   *   groupCode 里一旦出现 `&` / `#` / 空格,query 就被截断或改写
   *   (现在的 30 个组 code 都是下划线,所以线上没炸,但这是**哑弹**)。
   *   改用 params 对象,和下面的 `list` 走同一条出口(axios 负责编码)。
   */
  byGroup: (groupCode: string) =>
    http.get<DictItem[]>('/system/dicts', { groupCode }),

  /**
   * 2026-10-18: 取所有有数据的字典分组(去重, 按 code 升序)
   * 后端: GET /dicts/groups
   */
  groups: () => http.get<DictGroup[]>('/system/dicts/groups'),

  /** 列表查询 — 后端 GET /dicts, query 传参 */
  list: (q: DictQuery) =>
    http.get<DictItem[]>('/system/dicts', q as unknown as Record<string, unknown>),

  /**
   * 字典只读, 只剩 update (编辑展示属性 label / sort / is_active / description)
   * create / remove 已删除(后端路由都卸了, 调了直接 404)
   */
  update: (id: string, data: Partial<DictItem>) =>
    http.patch<void>(`/system/dicts/${id}`, data),
}

/* ====================== 平台配置(/platform group 下) ====================== */

export const configApi = {
  /**
   * 配置分组 KV
   * 后端: GET /platform/configs/groups → ConfigGroupKV[] { group, count }
   * 用途: index.tsx 顶部分组 filter
   */
  groups: () => http.get<ConfigGroupKV[]>('/platform/configs/groups'),

  /** 列表 — 后端 endpoint 在 /platform/configs,非 /system/config */
  list: (q: ConfigListQuery) =>
    http.get<PageResponse<ConfigItem>>(
      '/platform/configs',
      q as unknown as Record<string, unknown>
    ),

  /**
   * 直改更新(super_admin)
   * 后端: PATCH /platform/configs/:id, 任意字段可选(pointer 风格)
   */
  update: (id: string, data: UpdateConfigBody) =>
    http.patch<void>(`/platform/configs/${id}`, data),

  /**
   * 审计日志
   * 后端 endpoint: GET /platform/configs/audits(复数)
   * 2026-10-18 收窄:action 只剩 update(原 create/delete/view 已删)
   *
   * ⚠️ query 的 wire 名跟后端 handler 读的一致:`config_key` / `config_group`。
   *   2026-10-29 之前这里发 `config_key`、后端读 `configKey` ⇒ 过滤从来没生效过。
   */
  auditLogs: (q: ConfigAuditQuery) =>
    http.get<PageResponse<ConfigAuditLog>>(
      '/platform/configs/audits',
      q as unknown as Record<string, unknown>
    ),
}
