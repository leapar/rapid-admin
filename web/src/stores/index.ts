/**
 * Store 统一导出
 */
export { useUserStore } from './user'
export { usePermissionStore } from './permission'
export { useUIStore } from './ui'
export { useConfigStore } from './config'
export { useDictStore } from './dict'
/** 匿名开关(登录前可读)—— 区别于 useConfigStore,后者走受保护的 /platform/configs */
export { usePublicConfigStore } from './publicConfig'
