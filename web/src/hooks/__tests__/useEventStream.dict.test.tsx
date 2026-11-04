/**
 * SSE 接线回归网 —— `dict.changed`(2026-10-26 M94)
 *
 * ⚠️ **这里守的是"事件收到了,字典缓存真的被作废了"这条链路**。
 *   `stores/__tests__/dict.store.test.ts` 守的是 store 语义
 *   (invalidate 之后会不会真的重拉),**守不到"事件有没有接上线"** ——
 *   那边是直接调 invalidate 的。
 *
 *   所以这条链路上有三个必须同时成立的环节:
 *     ① 服务端广播 `dict.changed`(后端 pkg/push + service 层已覆盖)
 *     ② 前端 EventSource 收到这个事件名(← **本文件守的就是这条**)
 *     ③ 收到之后调 store 的 invalidate(← 本文件也守)
 *
 *   ② 最容易悄悄坏:谁在 `open()` 里少写一行 `addEventListener`,
 *   或者事件名拼错一个字符 —— 症状是「字典改了不生效」,
 *   **不报错、不白屏、没有任何日志**,极难归因。
 *
 * ⚠️ **用假的 EventSource 而不是真连后端** —— 真连的话,
 *   这条测试依赖 Redis / 后端 / 登录态,CI 上必红。
 *   假的 EventSource 只验"**名字对得上、回调被调用了**"。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, act } from '@testing-library/react'
// ⚠️ **只能用顶层 `import type` 拿类型** —— `import('@/stores/user')` 这种
//   内联 `import()` 类型标注被 `@typescript-eslint/consistent-type-imports`
//   禁掉(ESLint 报 `import() type annotations are forbidden`)。
//   `import type` 编译后完全擦除,不会和下面的 vi.mock 抢运行时。
import type * as UserStoreModule from '@/stores/user'

// 只改 getToken —— buildURL() 拿不到 token 就不会建连接,测试就没有事件源了。
// 其余导出(useUserStore 等)保持真实实现,免得把别的模块的 import 打断。
vi.mock('@/stores/user', async (importOriginal) => {
  const mod = await importOriginal<typeof UserStoreModule>()
  return { ...mod, getToken: () => 'test-token' }
})

import { useEventStream, EVENT } from '@/hooks/useEventStream'
import { useDictStore } from '@/stores/dict'

// === 假的 EventSource ===

type Listener = (e: MessageEvent) => void

class FakeEventSource {
  static instances: FakeEventSource[] = []
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2

  readyState = FakeEventSource.OPEN
  onerror: ((e: unknown) => void) | null = null
  private listeners = new Map<string, Set<Listener>>()

  constructor(public url: string) {
    FakeEventSource.instances.push(this)
  }

  addEventListener(type: string, fn: Listener) {
    let bucket = this.listeners.get(type)
    if (!bucket) {
      bucket = new Set()
      this.listeners.set(type, bucket)
    }
    bucket.add(fn)
  }

  removeEventListener(type: string, fn: Listener) {
    this.listeners.get(type)?.delete(fn)
  }

  close() {
    this.readyState = FakeEventSource.CLOSED
  }

  /** 模拟服务端推一条命名事件 */
  emit(type: string, data: unknown) {
    const payload = { data: JSON.stringify(data) } as MessageEvent
    for (const fn of Array.from(this.listeners.get(type) ?? [])) {
      fn(payload)
    }
  }

  /** 该事件名上确实挂了监听器吗 */
  has(type: string): boolean {
    return (this.listeners.get(type)?.size ?? 0) > 0
  }
}

function lastSocket(): FakeEventSource {
  const s = FakeEventSource.instances[FakeEventSource.instances.length - 1]
  if (!s) throw new Error('没有建出 EventSource —— getToken 没生效?')
  return s
}

/** 挂一个持有 SSE 连接的组件(useEventStream 就是连接的生命周期持有者) */
function Probe() {
  useEventStream()
  return null
}

describe('hooks/useEventStream · dict.changed 接线(M94)', () => {
  beforeEach(() => {
    FakeEventSource.instances = []
    vi.stubGlobal('EventSource', FakeEventSource)
    useDictStore.setState({ dicts: {}, lastFetchAt: {} })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('事件名与服务端一致(改名字等于让已发版的前端收不到信号)', () => {
    expect(EVENT.dictChanged).toBe('dict.changed')
  })

  it('连接上之后确实监听了 dict.changed', () => {
    render(<Probe />)
    expect(lastSocket().has(EVENT.dictChanged)).toBe(true)
  })

  it('收到 dict.changed → 该组的缓存被作废', () => {
    useDictStore.setState({
      dicts: { common_status: [] },
      lastFetchAt: { common_status: Date.now() },
    })
    render(<Probe />)

    act(() => {
      lastSocket().emit(EVENT.dictChanged, { group_code: 'common_status' })
    })

    expect(useDictStore.getState().lastFetchAt.common_status).toBeUndefined()
  })

  it('只作废被点名的那一组(别把整个缓存清了)', () => {
    useDictStore.setState({
      dicts: { common_status: [], role_type: [] },
      lastFetchAt: { common_status: Date.now(), role_type: Date.now() },
    })
    render(<Probe />)

    act(() => {
      lastSocket().emit(EVENT.dictChanged, { group_code: 'common_status' })
    })

    expect(useDictStore.getState().lastFetchAt.common_status).toBeUndefined()
    expect(useDictStore.getState().lastFetchAt.role_type).toBeDefined()
  })

  it('别的组被改,不影响 common_status', () => {
    useDictStore.setState({
      dicts: { common_status: [], role_type: [] },
      lastFetchAt: { common_status: Date.now(), role_type: Date.now() },
    })
    render(<Probe />)

    act(() => {
      lastSocket().emit(EVENT.dictChanged, { group_code: 'role_type' })
    })

    expect(useDictStore.getState().lastFetchAt.common_status).toBeDefined()
    expect(useDictStore.getState().lastFetchAt.role_type).toBeUndefined()
  })

  it('payload 里没有 group_code 时静默忽略(不清空整个缓存)', () => {
    useDictStore.setState({
      dicts: { common_status: [] },
      lastFetchAt: { common_status: Date.now() },
    })
    render(<Probe />)

    act(() => {
      lastSocket().emit(EVENT.dictChanged, { nonsense: 1 })
    })

    expect(useDictStore.getState().lastFetchAt.common_status).toBeDefined()
  })

  it('无关事件不会误伤字典缓存', () => {
    useDictStore.setState({
      dicts: { common_status: [] },
      lastFetchAt: { common_status: Date.now() },
    })
    render(<Probe />)

    act(() => {
      lastSocket().emit(EVENT.jobFinished, { job_id: 1 })
    })

    expect(useDictStore.getState().lastFetchAt.common_status).toBeDefined()
  })
})
