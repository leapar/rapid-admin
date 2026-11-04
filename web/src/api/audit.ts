/**
 * 审计中心 API
 *
 * 路径规则:后端 `sys := api.Group("/system")` → 完整路径 `/rapid-admin/v1/system/audit/*`
 * 详见 server/internal/api/router/router.go:346-355
 *
 * ⚠️ **2026-10-30 从 `api/issue.ts` 独立出来**
 *
 *   审计接口原来混在问题工单的 api 文件里,而且**自带一份 camelCase 类型**
 *   (`OperationLog` / `DataChangeLog` / `LoginLog`),与 `types/audit.ts`
 *   的 snake_case 版本**并存且互相矛盾** —— 页面上"请求 URL / 响应结果"
 *   两列永远空白就是这么来的(读的是 camelCase,后端返的是 snake_case)。
 *
 *   现在:类型只在 `@/types/audit` 一份,api 只负责发请求。
 *
 * ⚠️ **查询参数名必须与后端 handler 读的一字不差**
 *
 *   后端读的是:`keyword` / `status` / `success` / `operation` /
 *   `startAt` / `endAt` / `module` / `action` / `ip` / `tableName` /
 *   `recordId` / `userId`。
 *   名字对不上 = 后端**不读这个参数** —— HTTP 200、结果不变、不报错,
 *   页面上的筛选控件就成了摆设(这是 2026-10-30 修掉的真 bug)。
 */
import { http } from './client'
import type { PageResponse } from '@/types/api'
import type {
  OperationLog,
  OperationLogQuery,
  DataChangeLog,
  DataChangeLogQuery,
  LoginLog,
  LoginLogQuery,
  SystemLog,
  SystemLogQuery,
} from '@/types/audit'

export const auditApi = {
  /**
   * 操作日志 — GET /system/audit/operation-logs
   *
   * ⚠️ **筛选参数(status / keyword / startAt / endAt)是 2026-10-30 才接上的**,
   *   此前 handler 一个都不读 —— 页面上"只看失败"这个下拉框点了没反应。
   */
  operationLogs: (q: OperationLogQuery) =>
    http.get<PageResponse<OperationLog>>(
      '/system/audit/operation-logs',
      q as unknown as Record<string, unknown>,
    ),

  /** 数据变更日志 — GET /system/audit/data-change-logs */
  dataChangeLogs: (q: DataChangeLogQuery) =>
    http.get<PageResponse<DataChangeLog>>(
      '/system/audit/data-change-logs',
      q as unknown as Record<string, unknown>,
    ),

  /**
   * 登录日志 — GET /system/audit/login-logs
   *
   * ⚠️ 成功/失败筛选用 **`success`**(不是 `status`)——
   *   后端 `LoginLogQuery.Success` 是 *bool,读的是 `c.Query("success")`,
   *   传 `status=0` 会被**完全忽略**。
   */
  loginLogs: (q: LoginLogQuery) =>
    http.get<PageResponse<LoginLog>>(
      '/system/audit/login-logs',
      q as unknown as Record<string, unknown>,
    ),

  /**
   * 系统日志 — GET /system/audit/system-logs
   *
   * ⚠️ 后端读的是 `level` / `source` / `keyword` / `startAt` / `endAt`
   *   (buildSystemLogQuery),没有 userId —— 名字对不上 = 静默不筛。
   */
  systemLogs: (q: SystemLogQuery) =>
    http.get<PageResponse<SystemLog>>(
      '/system/audit/system-logs',
      q as unknown as Record<string, unknown>,
    ),
}

export default auditApi
