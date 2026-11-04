/**
 * 字典常量(与后端 sys_dict 表对齐)
 * 一些跨模块通用的小字典,直接前端写死避免后端联调依赖
 *
 * 国际化约定:`label` 字段是 i18n key(不是已翻译文案)。
 * 消费方: `import { useI18n } from '@/locales'`, 然后 `t(item.label)`.
 */

/** 通用状态 */
export const STATUS = {
  ACTIVE: { value: 'active', label: 'dict.status.active', color: 'green' },
  DISABLED: { value: 'disabled', label: 'dict.status.disabled', color: 'red' },
  LOCKED: { value: 'locked', label: 'dict.status.locked', color: 'orange' },
} as const

/** 用户状态 */
export const USER_STATUS = {
  ACTIVE: { value: 'active', label: 'dict.userStatus.active', color: 'green' },
  DISABLED: { value: 'disabled', label: 'dict.userStatus.disabled', color: 'red' },
  LOCKED: { value: 'locked', label: 'dict.userStatus.locked', color: 'orange' },
} as const

/** 性别 */
export const GENDER = {
  MALE: { value: 'male', label: 'dict.gender.male' },
  FEMALE: { value: 'female', label: 'dict.gender.female' },
  UNKNOWN: { value: 'unknown', label: 'dict.gender.unknown' },
} as const

export const CONFIG_CHANGE_STATUS = {
  PENDING: { value: 'pending', label: 'dict.configChangeStatus.pending', color: 'blue' },
  APPROVED: { value: 'approved', label: 'dict.configChangeStatus.approved', color: 'green' },
  REJECTED: { value: 'rejected', label: 'dict.configChangeStatus.rejected', color: 'red' },
  EXPIRED: { value: 'expired', label: 'dict.configChangeStatus.expired', color: 'default' },
} as const

/**
 * 帮助函数:把 dict 转为 Antd Select options
 * 返回的 label 仍是 i18n key,消费方用 t() 渲染
 */
export function dictToOptions(
  dict: Record<string, { value: string; label: string; color?: string }>,
) {
  return Object.values(dict).map((item) => ({
    value: item.value,
    label: item.label,
  }))
}
