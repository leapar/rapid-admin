/**
 * 请求重试工具 —— 给 axios 错误用,指数回退 + full jitter
 *
 * M30 §2.3 #8 P1
 *   - 默认:网络抖动(Network Error) / ECONNABORTED / 5xx / 408 / 429 自动重试
 *   - 不重试:4xx 业务错(400/401/403/404/...)、业务页声明 `retry:false`、写操作
 *   - 默认 3 次,初始 500ms,封顶 5s
 *   - 用 AbortSignal 取消:用户主动 abort 立即停止
 *   - 全 jitter:wait = uniform(0, backoff),避免雷鸣群
 *
 * 用法:在 axios response error 拦截器里调 retryRequest(error)
 */
import type { AxiosError, AxiosRequestConfig, InternalAxiosRequestConfig } from 'axios'

const DEFAULT_MAX_ATTEMPTS = 3
const DEFAULT_INITIAL_BACKOFF = 500
const DEFAULT_MAX_BACKOFF = 5_000
const DEFAULT_MULTIPLIER = 2

/** 全局开关:VITE_HTTP_RETRY=false 时禁用 */
function retryEnabled(): boolean {
  return (import.meta.env.VITE_HTTP_RETRY ?? 'true') !== 'false'
}
function retryMaxGlobal(): number {
  const n = Number(import.meta.env.VITE_HTTP_RETRY_MAX)
  return Number.isFinite(n) && n > 0 ? n : DEFAULT_MAX_ATTEMPTS
}
function retryBaseGlobal(): number {
  const n = Number(import.meta.env.VITE_HTTP_RETRY_BASE_MS)
  return Number.isFinite(n) && n > 0 ? n : DEFAULT_INITIAL_BACKOFF
}

declare module 'axios' {
  export interface InternalAxiosRequestConfig {
    /** 已重试次数(拦截器维护) */
    _retryCount?: number
  }
  export interface AxiosRequestConfig {
    /** 显式禁用重试(`retry: false`) */
    retry?: boolean
    /** 显式最大重试次数(覆盖默认) */
    retryMax?: number
    /** 显式初始 backoff ms */
    retryBaseMs?: number
  }
}

/** 判断 err 是否值得重试(默认策略) */
function shouldRetryDefault(
  error: AxiosError,
  config: InternalAxiosRequestConfig & AxiosRequestConfig
): boolean {
  // 全局开关
  if (!retryEnabled()) return false
  // 业务层显式禁用
  if (config.retry === false) return false

  // 用户主动 abort → 不重试
  if (error.code === 'ERR_CANCELED' || error.name === 'CanceledError') return false

  // 写操作(POST/PUT/PATCH/DELETE)默认不重试(避免双发)
  const m = (config.method || 'get').toLowerCase()
  if (m !== 'get' && m !== 'head' && config.retry !== true) return false

  // 网络错误
  if (error.code === 'ECONNABORTED') return true
  if (error.message?.includes('Network Error')) return true

  // 无 response(连接中断)
  if (!error.response) return true

  const status = error.response.status
  // 可重试状态码
  if (status === 408 || status === 429) return true
  if (status >= 500 && status < 600) return true

  return false
}

/** 算下一次等待(指数封顶 + full jitter) */
function nextBackoff(attempt: number, baseMs: number, maxMs: number, mult: number): number {
  let cur = baseMs
  for (let i = 1; i < attempt; i++) {
    cur = Math.min(maxMs, cur * mult)
  }
  return Math.floor(Math.random() * cur)
}

/**
 * 取消错误的**统一形状** —— 必须与 `api/client.ts` 的取消分支完全一致。
 *
 * ⚠️ M110:此前退避等待被打断时抛的是 `DOMException('Aborted','AbortError')`。
 *   而 client.ts 的 `isNormalizedError()` 判它是"已规范化"(DOMException 上
 *   **有** `code` 属性和 `message`)⇒ 直接 `Promise.reject(DOMException)`
 *   ⇒ 调用方拿到的是 `{ code: 20, name: 'AbortError' }`,
 *   **不是** client 约定的 `{ code: 'CANCELED', canceled: true }`
 *   ⇒ 业务层判不出"这是取消,不是失败"(M6 的约定:被取消的请求不是错误)。
 *   现在两条路径同形。
 *
 * ⚠️ **为什么定义在这里而不是 client.ts**:client.ts 已经 import 本文件
 *   (`retryRequest`);反过来让本文件 import client.ts 会形成**循环依赖**。
 *   所以形状的真源放在这里,由 client.ts 引用 —— 一处定义,两处使用。
 */
export function canceledError(): { code: string; message: string; canceled: true } {
  return { code: 'CANCELED', message: 'canceled', canceled: true }
}

/** 睡眠,可被 AbortSignal 打断(axios signal 是 GenericAbortSignal 子类型) */
function sleep(ms: number, signal?: AbortSignal | unknown): Promise<void> {
  const s = signal as AbortSignal | undefined
  return new Promise((resolve, reject) => {
    if (s?.aborted) {
      reject(canceledError())
      return
    }
    const t = setTimeout(() => {
      s?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    const onAbort = () => {
      clearTimeout(t)
      reject(canceledError())
    }
    s?.addEventListener('abort', onAbort, { once: true })
  })
}

/**
 * 包装一个请求,失败时按策略重试
 *   - 业务层调 instance(config) → 在拦截器里 catch (err) → retryRequest(err)
 *   - 成功 / 不该重试 → 透传原 err
 */
export async function retryRequest<T = unknown>(
  error: AxiosError,
  instance: { request: (cfg: AxiosRequestConfig) => Promise<{ data: T }> }
): Promise<{ data: T }> {
  const config = error.config as (InternalAxiosRequestConfig & AxiosRequestConfig) | undefined
  if (!config) throw error

  const max = config.retryMax ?? retryMaxGlobal()
  const base = config.retryBaseMs ?? retryBaseGlobal()
  const current = config._retryCount ?? 0

  if (current >= max) throw error
  if (!shouldRetryDefault(error, config)) throw error

  // 标记重试次数
  config._retryCount = current + 1
  /**
   * ⚠️ M101:标记"这次是重试链里发出来的"
   *
   * `instance.request(config)` 会**完整重跑一遍拦截器链**,于是重试那一次的
   * 错误拦截器也会走一遍 —— 它会**再弹一次 toast**(一次失败弹两个红条),
   * 而且它抛出来的新错误会被外层 `catch {}` 整个吞掉,最后报出去的还是
   * **第一次**失败的码(实测:第一次 500、重试后 400,报出来的是 50001)。
   *
   * 打了这个标之后,内层只做判定、**不弹提示也不规范化**,把原始 AxiosError
   * 原样抛给最外层,由最外层统一弹一次 + 用**最后一次**失败的码。
   *
   * 声明在 api/client.ts 的 `declare module 'axios'` 里(同一份 interface
   * 不能在两处重复声明)。
   */
  config._isRetryAttempt = true

  const wait = nextBackoff(current + 1, base, DEFAULT_MAX_BACKOFF, DEFAULT_MULTIPLIER)
  await sleep(wait, config.signal)

  // 重发(保留所有原始字段,包括 _retryCount / _isRetryAttempt)
  return instance.request(config) as Promise<{ data: T }>
}

/** 工具:判断 config 是否已经在重试中 */
export function isRetrying(config: InternalAxiosRequestConfig | undefined): boolean {
  return (config?._retryCount ?? 0) > 0
}
