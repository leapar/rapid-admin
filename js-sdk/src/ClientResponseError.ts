/**
 * 请求失败抛的统一错误
 *
 * # 一条失败同时有两层"码"
 *
 *   status —— HTTP 状态码(401 / 403 / 404 / 500 ...)
 *   code   —— 响应体里的业务码(0 = 成功,非 0 见后端 error-codes)
 *
 * 两者都可能为 0:网络层失败(断网、JSON 解析不了)时 status = 0、code = 0,
 * 只有 message 能信 —— 所以**判错误一律用 `err instanceof ClientResponseError`**,
 * 不要拿数字比(后端 40101 token 过期这种业务码最容易在这一步漏掉)。
 */
export class ClientResponseError extends Error {
  /** 请求的完整 URL(排查"到底打了哪个地址"用) */
  readonly url: string

  /** HTTP 状态码;网络层失败 = 0 */
  readonly status: number

  /** 响应体里的业务码;body 不是 JSON / 网络层失败 = 0 */
  readonly code: number

  /** 后端响应体原始形态(有就带着) */
  readonly response: { code?: number; message?: string; trace_id?: string } | null

  /** 响应体里的 trace_id,报障时交给后端查日志 */
  readonly traceId: string

  /** true = 请求被 AbortSignal 取消(不是服务器的错,别弹错误 toast) */
  readonly isAbort: boolean

  constructor(
    url: string,
    status: number,
    code: number,
    message: string,
    response?: unknown,
    isAbort = false,
  ) {
    super(message)
    this.name = 'ClientResponseError'
    this.url = url
    this.status = status
    this.code = code
    this.isAbort = isAbort
    this.traceId = traceOf(response)
    this.response = isBody(response) ? response : null
    // ⚠️ Error 子类会被 instanceof 断(目标编译到 ES5 时原型链断掉)
    Object.setPrototypeOf(this, ClientResponseError.prototype)
  }
}

function traceOf(response: unknown): string {
  return isBody(response) && typeof response.trace_id === 'string' ? response.trace_id : ''
}

function isBody(response: unknown): response is { code?: number; message?: string; trace_id?: string } {
  return typeof response === 'object' && response !== null
}
