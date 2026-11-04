/**
 * 应用级常量
 */

/** LocalStorage keys(所有 key 集中管理,避免散落) */
export const STORAGE_KEYS = {
  USER: 'rapid-admin:user',
  TOKEN: 'rapid-admin:token',
  REFRESH_TOKEN: 'rapid-admin:refresh_token',
  THEME: 'rapid-admin:theme',
  LANG: 'rapid-admin:lang',
  SIDEBAR_COLLAPSED: 'rapid-admin:sidebar_collapsed',
  CURRENT_IDENTITY: 'rapid-admin:current_identity',
  // 平台配置中心
  CONFIG_CACHE_PREFIX: 'rapid-admin:config:',
  // 路由多 tab
  TABS_STATE: 'rapid-admin:tabs',
  // token 过期时间戳(用于 M18 自动刷新调度)
  ACCESS_TOKEN_EXPIRES_AT: 'rapid-admin:access_token_expires_at',
  // refresh token 过期时间戳(2026-10-25 M8 跨标签页同步)
  //
  // 为什么要单独记:续期会**轮换** refresh token(后端单次使用),所以"本地这份
  // refresh token 还剩多久"要参与两件事:
  //   1. 写盘 TTL —— 少了它,跨标签页"采用"来的 token 只能退到 8h 兜底,
  //      会把一台设备的会话寿命从 30 天砍到 8 小时;
  //   2. 跨标签页广播的载荷 —— 接收方要靠它算出"还剩多久"再写盘。
  REFRESH_TOKEN_EXPIRES_AT: 'rapid-admin:refresh_token_expires_at',
  // 最近一次活动时间(空闲超时 30min)—— 刻意走 localStorage:多标签页共享,
  // 任一 tab 有操作则所有 tab 一起续命,见 utils/idleTimer.ts
  LAST_ACTIVE_AT: 'rapid-admin:last_active_at',
  // 文件列表的视图模式(表格 / 缩略图)—— M134
  //
  // 为什么单独记:缩略图视图是用户**主动切过去的**(文件列表里图纸照片混着,
  // 表格看不出文件是什么),下次进来还停在上次的视图才合理。
  // 走 utils/storage.ts 的信封格式,不裸写 localStorage。
  FILE_LIST_VIEW: 'rapid-admin:file_list_view',
  // 文件夹列表的视图模式(表格 / 卡片)—— M144
  //
  // 跟 FILE_LIST_VIEW 分开记:两个页面的视图是各自独立的,
  // 共用一个 key 的话在文件列表切到缩略图,进文件夹页也会变成卡片视图。
  FILE_FOLDER_VIEW: 'rapid-admin:file_folder_view',
} as const

/** 路由 path 常量(用于代码内引用,不直接写字符串) */
export const ROUTES = {
  LOGIN: '/login',
  FORGOT_PASSWORD: '/forgot-password',
  FORBIDDEN: '/forbidden',
  NOT_FOUND: '/not-found',
  DASHBOARD: '/',
  // 系统管理
  SYSTEM: '/system',
  SYSTEM_USER: '/system/user',
  SYSTEM_ROLE: '/system/role',
  SYSTEM_PERMISSION: '/system/permission',
  SYSTEM_DEPARTMENT: '/system/department',
  SYSTEM_DICT: '/system/dict',
  SYSTEM_CONFIG: '/system/config',
  // 审计
  AUDIT: '/audit',
  AUDIT_OPERATION: '/audit/operation',
  AUDIT_DATA_CHANGE: '/audit/data-change',
  AUDIT_LOGIN: '/audit/login',
} as const

/**
 * 错误消息常量
 *
 * 注意:这里的字符串是 i18n key,不是已翻译的文案。
 * 消费方应: `import { useI18n } from '@/locales'`, 然后 `t(ERROR_MESSAGES.FORBIDDEN)`.
 * 这样 i18n 责任全在前端,后端不参与文案维护。
 */
export const ERROR_MESSAGES = {
  NETWORK_ERROR: 'error.networkError',
  UNAUTHORIZED: 'error.unauthorized',
  FORBIDDEN: 'error.forbidden',
  SERVER_ERROR: 'error.serverError',
  VALIDATE_FAILED: 'error.validateFailed',
} as const

/** 分页默认值 */
export const PAGE_DEFAULTS = {
  PAGE: 1,
  PAGE_SIZE: 20,
  PAGE_SIZES: [10, 20, 50, 100] as number[],
} as const

/** 文件上传限制 */
export const UPLOAD_LIMITS = {
  IMAGE_MAX_SIZE: 10 * 1024 * 1024, // 10MB
  VIDEO_MAX_SIZE: 500 * 1024 * 1024, // 500MB
  FILE_MAX_SIZE: 100 * 1024 * 1024, // 100MB
  IMAGE_TYPES: ['image/jpeg', 'image/png', 'image/webp', 'image/gif'],
  VIDEO_TYPES: ['video/mp4', 'video/webm'],
} as const
