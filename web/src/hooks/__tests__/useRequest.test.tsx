/**
 * useRequest 的取消语义回归网(2026-10-25 M6)
 *
 * ⚠️ 以前这个 hook 的 AbortController 是**假的**:
 *   它建了 controller、也调了 `ac.abort()`,但 **signal 从来没传给 service**
 *   (`serviceRef.current(...args)` 只传业务参数)
 *   → abort 只能让 hook 忽略这次结果,**网络请求照跑到底**。
 *   而 `api/client.ts` 的注释还写着"useRequest 已自动集成,业务层不用关心" ——
 *   文档与实现不符。
 *
 * 现在 signal 作为 **service 的最后一个参数** 传出来:
 *   useRequest((signal?: AbortSignal) => http.get('/x', undefined, { signal }))
 * service 声明了它并转给 axios,那才是**真取消**。
 *
 * 这里锁三件事:
 *   1. service 确实拿得到一个 AbortSignal
 *   2. 组件卸载 → 那个 signal 变成 aborted
 *   3. deps 变化 → 上一个 signal 被 abort(而不是把新请求也一起 abort)
 */
import { describe, it, expect, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useRequest } from '@/hooks/useRequest'

describe('useRequest · 取消', () => {
  it('service 拿得到一个 AbortSignal(以前压根没传)', async () => {
    let captured: AbortSignal | undefined
    const service = vi.fn(async (signal?: AbortSignal) => {
      captured = signal
      return 'ok'
    })

    renderHook(() => useRequest(service))

    await waitFor(() => expect(captured).toBeDefined())
    expect(captured).toBeInstanceOf(AbortSignal)
    expect(captured!.aborted).toBe(false)
  })

  it('组件卸载 → 那个 signal 被 abort', async () => {
    let captured: AbortSignal | undefined
    const service = vi.fn((signal?: AbortSignal) => {
      captured = signal
      return new Promise<string>(() => {}) // 一直挂着
    })

    const { unmount } = renderHook(() => useRequest(service))
    await waitFor(() => expect(captured).toBeDefined())

    unmount()
    expect(captured!.aborted).toBe(true)
  })

  it('deps 变化 → 上一个 signal 被 abort,新请求拿到的是新 signal', async () => {
    const signals: AbortSignal[] = []
    const service = vi.fn((signal?: AbortSignal) => {
      if (signal) signals.push(signal)
      return new Promise<string>(() => {})
    })

    const { rerender } = renderHook(({ dep }: { dep: number }) => useRequest(service, { deps: [dep] }), {
      initialProps: { dep: 1 },
    })
    await waitFor(() => expect(signals).toHaveLength(1))

    rerender({ dep: 2 })
    await waitFor(() => expect(signals).toHaveLength(2))

    // 旧的被中止了,新的还活着
    expect(signals[0]!.aborted).toBe(true)
    expect(signals[1]!.aborted).toBe(false)
  })

  it('卸载 → 请求被中止,且不算业务失败(error 不被记上)', async () => {
    let captured: AbortSignal | undefined
    const service = vi.fn((signal?: AbortSignal) => {
      captured = signal
      return new Promise<string>((_resolve, reject) => {
        // 真实的 axios 取消就是这个形状:signal 一 abort,请求就 reject
        signal?.addEventListener('abort', () =>
          reject(new DOMException('Aborted', 'AbortError')),
        )
      })
    })

    const { result, unmount } = renderHook(() => useRequest(service))
    await waitFor(() => expect(captured).toBeDefined())

    unmount()
    await waitFor(() => expect(captured!.aborted).toBe(true))
    // 组件没了,hook 不该再 setState —— 取消不是业务错误
    expect(result.current.error).toBeNull()
  })

  it('手动 cancel() → 结果不落 state', async () => {
    let captured: AbortSignal | undefined
    // ⚠️ **结果必须由测试自己放行**,不能用固定 sleep ——
    //   以前这里是 `await new Promise(r => setTimeout(r, 20))`,
    //   于是"cancel 发生在结果到达**之前**"这件事**取决于机器快慢**:
    //   满载时事件循环被饿过 20ms+,service 先 resolve、数据先落 state,
    //   cancel() 变成"事后补一刀" → 用例红,但**产品行为是对的**。
    //   (2026-10-30 M121 全量跑实测:满载时这条红,单独跑绿。)
    //   改成"测试手动放行"之后,顺序是**写死的**,跟机器快慢无关。
    let release: ((v: string) => void) | undefined
    const service = vi.fn((signal?: AbortSignal) => {
      captured = signal
      return new Promise<string>((resolve) => {
        release = resolve
      })
    })

    const { result } = renderHook(() => useRequest(service))
    await waitFor(() => expect(captured).toBeDefined())

    act(() => {
      result.current.cancel()
    })

    // 取消之后,迟到的结果才到达 —— 这正是要验的场景
    await act(async () => {
      release?.('late')
    })
    // 等这次请求**彻底结束**(hook 的 finally 会把 loading 置回 false),
    // 不然断言可能跑在 setState 之前
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.data).toBeUndefined()
    expect(result.current.error).toBeNull()
  })
})
