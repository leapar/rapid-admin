/**
 * 字典 groupCode 注册表(L3)
 *
 * 与 server/internal/seed/seed.go mustDicts 严格镜像。
 * 任何新增/删除 groupCode 必须 server + web 同步提交。
 *
 * 镜像规则:
 *   server mustDicts 数组 ←→ 本联合类型
 *   增删必须在同一 PR 内完成
 *   PR 标题约定: `feat(dict): add <group_code> dictionary`(增) / `refactor(dict): remove <group_code>`(删)
 *
 * 字典项(itemValue)由后端 sys_dict.item_value 决定,前端不另做枚举。
 *
 * 当前覆盖(13 组,全框架级):
 *   - 用户/角色:user_status, role_type
 *   - 审计:audit_op_status, audit_change_operation, sys_log_level
 *   - 配置中心:config_change_status, config_value_type(7 项含 cert_pem/private_key_pem), config_effective_type
 *   - P2 通用:common_status
 *   - 微信模板:wxtemplate_status
 *   - 短信模板审核状态:sms_template_status
 *   - 资源类型:resource_type
 *   - 配置 UI Label:config_ui_label
 *
 * 业务方要加自己的字典(自由 enum 场景)时,在 mustDicts + 本类型同步登记。
 */
export type DictGroupCode =
  // === 用户/角色 ===
  | 'user_status'
  | 'role_type'
  // === 审计 ===
  | 'audit_op_status'
  // 数据变更页:operation create / update / delete
  | 'audit_change_operation'
  // 审计中心「系统日志」页:info / warn / error
  | 'sys_log_level'
  // === 配置中心 ===
  | 'config_change_status'
  | 'config_value_type'
  | 'config_effective_type'
  // === 通用 ===
  | 'common_status'
  // === 微信模板 ===
  | 'wxtemplate_status'
  // === 资源类型(资源管理页) ===
  | 'resource_type'
  // === 配置 UI Label(系统配置 Collapse 表单) ===
  | 'config_ui_label'
  // === 短信模板审核状态 ===
  | 'sms_template_status'

/**
 * 字典项 itemValue 类型。
 * 大多数字典的 itemValue 是字符串(role_type / config_*);
 * 部分是数字串(后端 int)。
 * 业务页调用 useDict 后通过 useDictStore.getLabel(groupCode, value) 取 label。
 */
export type DictValue = string | number
