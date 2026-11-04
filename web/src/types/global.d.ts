/**
 * 全局类型扩展
 */

// Window 扩展
declare global {
  interface Window {
    /** 请求 ID 注入标识 */
    __REQUEST_ID__?: string
  }
}

export {}
