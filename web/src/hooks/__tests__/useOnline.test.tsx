/**
 * useOnline 的回归网(2026-10-25 M7)
 *
 * 这个 hook 以前没人测,而 M7 把"断网恢复自动重载"挂在了它身上 ——
 * 它的回调如果不可靠,重载就不可靠。
 *
 * 盯两件事:
 *   1. online / offline 事件确实会打到回调
 *   2. **重新渲染不会反复重绑监听**
 *      旧实现的 effect 依赖是 `[onReconnect, onDisconnect]`,而调用方几乎都写
 *      `useOnline({ onReconnect: () => reload() })` —— 每次 render 都是新函数
 *      → 依赖每渲染都变 → 监听被 remove/add 反复重绑。
 *      看着只是白做功,实际有窗口期:'online' 事件正好落在这个缝里就漏掉了,
 *      而"网络恢复后自动重载"恰恰依赖这个事件。
 */
import { describe, it, expect, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useOnline } from '@/hooks/useOnline'

describe('useOnline · 事件与回调', () => {
  it('offline 事件 → onDisconnect;online 事件 → onReconnect', () => {
    const onReconnect = vi.fn()
    const onDisconnect = vi.fn()
    renderHook(() => useOnline({ onReconnect, onDisconnect }))

    act(() => {
      window.dispatchEvent(new Event('offline'))
    })
    expect(onDisconnect).toHaveBeenCalledTimes(1)

    act(() => {
      window.dispatchEvent(new Event('online'))
    })
    expect(onReconnect).toHaveBeenCalledTimes(1)
  })

  it('重新渲染不会反复重绑 online / offline 监听', () => {
    const addSpy = vi.spyOn(window, 'addEventListener')
    const removeSpy = vi.spyOn(window, 'removeEventListener')
    // ⚠️ 不能写成 `c[0] === 'online'` —— vi.spyOn 会把第一个参数的类型
    //   收窄成某个具体 overload 的字面量,TS 会判"两个类型没有交集"。
    //   统一转成 string 再比。
    const countOnline = (calls: unknown[][]) =>
      calls.filter((c) => String(c[0]) === 'online').length

    const { rerender, unmount } = renderHook(() =>
      // 每次 render 都是新函数 —— 旧实现在这里会被判成"依赖变了"
      useOnline({ onReconnect: () => {}, onDisconnect: () => {} }),
    )

    expect(countOnline(addSpy.mock.calls)).toBe(1)

    rerender()
    rerender()

    // 旧实现:每渲染一次 +1 次 addEventListener('online')
    expect(countOnline(addSpy.mock.calls)).toBe(1)

    unmount()
    // 卸载时清理一次(不多不少)
    expect(countOnline(removeSpy.mock.calls)).toBe(1)
  })

  it('回调换了一个之后,事件打在新回调上(旧的不再被打)', () => {
    const first = vi.fn()
    const second = vi.fn()
    const { rerender } = renderHook(({ cb }) => useOnline({ onReconnect: cb }), {
      initialProps: { cb: first },
    })

    rerender({ cb: second })

    act(() => {
      window.dispatchEvent(new Event('online'))
    })

    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledTimes(1)
  })
})
