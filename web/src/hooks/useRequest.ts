/**
 * useRequest —— 通用异步数据 hook
 *
 * 用法:
 *   const { data, loading, error, refresh, run, cancel } = useRequest(() => userApi.list(q), { deps: [q] })
 *
 * §2.3 #10 集成 AbortController:
 *   - deps 变化/组件卸载时,自动 abort 上一次未完成的请求
 *   - 显式调 cancel() 也能立即取消
 *   - abort 后 setState 仍会触发(React 18 已 warning-safe),err 是 AbortError
 *
 * ⚠️ 2026-10-25 M6 更正一处"名不副实":
 *   以前这个 hook 建了 AbortController、也调了 `ac.abort()`,
 *   但 **signal 从来没传给 service**(`serviceRef.current(...args)` 只传业务参数)
 *   → abort 只能让 hook 忽略这次结果,**网络请求照跑到底**。
 *   现在 signal 会作为 **service 的最后一个参数** 传出来:
 *     useRequest((signal?: AbortSignal) => http.get('/x', undefined, { signal }))
 *   service 声明了它、并且转给 axios(`http.get(url, params, { signal })`),
 *   那才是**真取消**(浏览器直接断连接)。
 *
 *   ⚠️ **service 的最后一个参数位置被 signal 占了**:
 *     业务参数请放在前面(`(id: string, signal?: AbortSignal)`),
 *     自动执行(manual: false)时 service 不该有必填参数 —— 要传参就 manual: true。
 */
import { useCallback, useEffect, useRef, useState } from 'react'

export interface UseRequestOptions<T> {
  /** 依赖项变化时自动重新执行 */
  deps?: React.DependencyList
  /** 手动模式(默认 false,初始化即执行) */
  manual?: boolean
  /** 成功回调 */
  onSuccess?: (data: T) => void
  /** 失败回调 */
  onError?: (err: unknown) => void
  /** 初始数据 */
  initialData?: T
}

export interface UseRequestResult<T, P extends unknown[]> {
  data: T | undefined
  loading: boolean
  error: unknown
  /** 重新拉取 */
  refresh: () => Promise<void>
  /** 手动传参执行 */
  run: (...args: P) => Promise<T | undefined>
  /** 主动取消当前 in-flight 请求(§2.3 #10) */
  cancel: () => void
}

export function useRequest<T, P extends unknown[] = []>(
  service: (...args: P) => Promise<T>,
  options: UseRequestOptions<T> = {}
): UseRequestResult<T, P> {
  const { deps = [], manual = false, onSuccess, onError, initialData } = options
  const [data, setData] = useState<T | undefined>(initialData)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)

  // 用 ref 持有最新 service,避免 deps 引用变化
  const serviceRef = useRef(service)
  serviceRef.current = service
  const onSuccessRef = useRef(onSuccess)
  onSuccessRef.current = onSuccess
  const onErrorRef = useRef(onError)
  onErrorRef.current = onError

  // 当前 in-flight 请求的 AbortController(§2.3 #10)
  const acRef = useRef<AbortController | null>(null)
  // 标记请求是否已被主动取消(避免 cancel 后还走 setState)
  const cancelledRef = useRef(false)

  const run = useCallback(async (...args: P) => {
    // 1. 取消上一次 in-flight(若存在)
    acRef.current?.abort()
    cancelledRef.current = false

    // 2. 新建 controller
    const ac = new AbortController()
    acRef.current = ac

    setLoading(true)
    setError(null)
    try {
      // ⚠️ M6:signal 作为**最后一个参数**透给 service。
      //   service 可以声明成 `(signal?: AbortSignal) => ...` 接住它,
      //   再自己转给 axios(`http.get(url, params, { signal })`)—— 那才是真取消。
      //   老的 service 不声明这个参数就当没看见(JS 多传参数无害)。
      const fn = serviceRef.current as unknown as (...a: unknown[]) => Promise<T>
      const result = await fn(...args, ac.signal)
      if (cancelledRef.current || ac.signal.aborted) {
        return undefined
      }
      setData(result)
      onSuccessRef.current?.(result)
      return result
    } catch (err) {
      // 主动 cancel 不算业务错(走 onError 也无妨,业务方可以 typeof err !== 'AbortError' 过滤)
      if (cancelledRef.current || ac.signal.aborted) {
        return undefined
      }
      setError(err)
      onErrorRef.current?.(err)
      return undefined
    } finally {
      if (acRef.current === ac) {
        acRef.current = null
        setLoading(false)
      }
    }
  }, [])

  const cancel = useCallback(() => {
    cancelledRef.current = true
    acRef.current?.abort()
  }, [])

  useEffect(() => {
    if (!manual) {
      run(...([] as unknown as P))
    }
    // deps 变化 / 组件卸载时:取消 in-flight
    return () => {
      cancelledRef.current = true
      acRef.current?.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { data, loading, error, refresh: async () => { await run(...([] as unknown as P)) }, run, cancel }
}
