/// <reference types="vite/client" />

// 环境变量类型声明
interface ImportMetaEnv {
  readonly VITE_APP_NAME: string
  readonly VITE_APP_VERSION: string
  readonly VITE_API_BASE: string
  readonly VITE_API_PROXY_TARGET: string
  readonly VITE_OSS_PROXY_TARGET: string
  readonly VITE_USE_MOCK: string
  readonly VITE_LOG_LEVEL: string
  // §2.5 #5 P1 —— Sentry 错误上报(空 = 关闭)
  readonly VITE_SENTRY_DSN: string
  readonly VITE_SENTRY_TRACES_SAMPLE_RATE: string
  readonly VITE_SENTRY_REPLAYS_SAMPLE_RATE: string
  // §2.3 #8 P1 —— 网络抖动重试
  readonly VITE_HTTP_RETRY: string
  readonly VITE_HTTP_RETRY_MAX: string
  readonly VITE_HTTP_RETRY_BASE_MS: string
  // §2.3 #11 P1 —— 多 baseURL 通道
  readonly VITE_API_BASE_ADMIN: string
  readonly VITE_API_BASE_OWNER: string
  readonly VITE_API_BASE_SUPPLIER: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

// 静态资源模块声明
declare module '*.svg' {
  const content: string
  export default content
}
declare module '*.png' {
  const content: string
  export default content
}
declare module '*.jpg' {
  const content: string
  export default content
}
