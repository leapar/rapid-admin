/**
 * 平台配置中心 类型
 * 2026-10-18 重写:对齐后端 service.ConfigItemView
 *
 * 2026-10-18 收尾:
 *   - 删除 CreateConfigBody(直改新建已删,UI 不会建配置项)
 *   - 删除 ConfigAuditLog.reason(UI 没输入框,前端硬塞无意义)
 *   - 删 audit.action 的 create/delete/view 类型(只留 update)
 * 配套文档: docs/系统配置功能规范.md
 */
// ⚠️ 2026-10-21 全仓 ID 改字符串
//
// 后端主键是 sonyflake 19 位数(~9.5e16),超出 JS 的 2^53 安全整数上限。
// JSON.parse 会静默截断末 6 位 —— 回传后端必然 404(身份切换就是这么死的),
// 而且**不报错**:拿到的是一个"看起来合法"的错 ID,排查成本极高。
//
// 所以 wire 上所有 ID 一律是 string。Go 侧用 `json:",string"` 保持 uint64
// 不变(slice 除外,内置选项对数组无效,改用 jsonid.IDList)。
// 本文件里的 id / *_id 一律 string,**不要**再做算术或隐式转换。
import type { PageRequest } from './api'

/** 配置组枚举(2026-10-18:按 seed 的 6 个 group 收紧,加 user 提示新 group 也可填) */
export type ConfigGroup =
  | 'aliyun.sms'
  | 'aliyun.oss'
  | string

/** 后端 ConfigValueType 字典(7 种) */
export type ConfigValueType =
  | 'string'
  | 'int'
  | 'bool'
  | 'json'
  | 'secret'
  | 'cert_pem'
  | 'private_key_pem'

/** 单个配置项(对齐后端 ConfigItemView) */
export interface ConfigItem {
  id: string
  config_group: ConfigGroup
  config_key: string
  config_name: string
  description: string
  value_type: ConfigValueType
  /** 当前值(已脱敏:敏感=preview,非敏感=原文) */
  value: string
  /** 服务端 preview(敏感**+原文前 4 后 4,非敏感原文) */
  value_preview: string
  is_sensitive: boolean
  regex_validate: string
  min_length: number
  max_length: number
  is_required: boolean
  is_readonly: boolean
  env_var_name: string
  sort: number
  is_active: boolean
  remarks: string
  version: number
  last_changed_by: number
  last_changed_by_name: string
  last_changed_at: string
}

/** 配置分组 KV(后端 ListGroups 返回) */
export interface ConfigGroupKV {
  group: string
  count: number
}

/** 审计日志(对齐后端 SysConfigAudit + SysUser join) */
//   2026-10-18 收窄:
//     - action 只剩 update(create/delete/view 已删)
//     - 删 reason 列(UI 没输入框)
//     - 删 change_req_id 关联工单ID(双签工作流移除后永远是 0,前端无意义显示)
export interface ConfigAuditLog {
  id: string
  config_id: string
  config_key: string
  action: string
  /** 后端 SysConfigAudit.UserID(已 join sys_user) */
  user_id: string
  user_name: string
  user_role: string
  ip: string
  before: string
  after: string
  created_at: string
}

/** 直改更新 config body(任意字段可选) */
export interface UpdateConfigBody {
  config_name?: string
  description?: string
  value?: string
  value_type?: ConfigValueType
  is_sensitive?: boolean
  is_required?: boolean
  is_readonly?: boolean
  env_var_name?: string
  sort?: number
  regex_validate?: string
  min_length?: number
  max_length?: number
  is_active?: boolean
  remarks?: string
}

/** list 接口 query(后端 query.group / query.keyword / page / page_size) */
export interface ConfigListQuery extends PageRequest {
  group?: string
  keyword?: string
}

/**
 * 审计日志 query(后端 query.config_key / query.config_group / page / size)
 *
 * ⚠️ **两个过滤是不同语义,别混**:
 *   - `config_key`   —— **子串**匹配(搜索框)
 *   - `config_group` —— **精确**匹配某个配置组(配置页点"审计日志"带过来的)
 *
 * ⚠️ wire 名必须跟后端 handler 读的一致(后端读 `config_key` / `config_group`)。
 *   2026-10-29 之前前端发 `config_key`、后端读 `configKey` ⇒ 过滤**从来没生效过**。
 */
export interface ConfigAuditQuery extends PageRequest {
  config_key?: string
  config_group?: string
  action?: string
}
