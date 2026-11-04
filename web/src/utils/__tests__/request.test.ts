/**
 * utils/request.ts 回归网(2026-10-29 M110)
 *
 * 这一层此前**一条网都没有** —— 而它决定了**每个请求**的两个头:
 *   · `X-Request-Id` —— 报错时给后端"秒查日志"的唯一线索
 *   · `Idempotency-Key` —— 写操作的防重复提交键(后端按它去重)
 *
 * 锁住的:
 *   ① **与后端消毒规则的契约**(直接照抄 server/internal/api/middleware/trace.go
 *      的 `sanitizeRequestID`):长度 1..64、字符集 `[A-Za-z0-9._:-]`。
 *      不合规的后端会**整个丢弃** ⇒ 串日志的链路直接断,而且前端毫无察觉。
 *   ② **唯一性** —— 幂等键重复 = 防重复提交失效。
 *   ③ **非安全上下文的降级路径**(M110 同根问题):
 *      `crypto.randomUUID` 也只在安全上下文存在(HTTPS / localhost),
 *      而 vite dev server 是 `host: '0.0.0.0'` ⇒ 同事从
 *      `http://<局域网IP>:5173` 打开就**没有** randomUUID。
 *      降级必须落在 `crypto.getRandomValues`(所有环境都有),
 *      而不是 `Math.random`(非密码学安全随机源)。
 *
 * 反向探针(见 scripts/probe_m110.mjs):把每处修复拆掉,对应用例必须变红。
 */
import { describe, it, expect, afterEach, vi } from 'vitest'
import { genRequestId, genIdempotencyKey } from '@/utils/request'

/**
 * 临时改写 crypto 上的方法(模拟"这个环境没有这个 API")
 * —— Node / jsdom 的 randomUUID / getRandomValues 都在原型上,
 *    在实例上定义一个自有属性即可遮蔽。
 */
function withCrypto(patch: Record<string, unknown>, fn: () => void): void {
  const c = globalThis.crypto as unknown as Record<string, unknown>
  const saved = new Map<string, PropertyDescriptor | undefined>()
  for (const k of Object.keys(patch)) {
    saved.set(k, Object.getOwnPropertyDescriptor(c, k))
    Object.defineProperty(c, k, { value: patch[k], configurable: true, writable: true })
  }
  try {
    fn()
  } finally {
    for (const [k, d] of saved) {
      if (d) Object.defineProperty(c, k, d)
      else delete c[k]
    }
  }
}

/** 后端 sanitizeRequestID 的规则(逐条照抄 trace.go) */
function backendWouldAccept(id: string): boolean {
  if (id === '' || id.length > 64) return false
  for (const ch of id) {
    if (/[A-Za-z0-9._:-]/.test(ch)) continue
    return false
  }
  return true
}

/** UUID v4 的形状:8-4-4-4-12,version 位=4,variant 位=8/9/a/b */
const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

afterEach(() => {
  // 兜底:别把遮蔽留在全局
  const c = globalThis.crypto as unknown as Record<string, unknown>
  for (const k of ['randomUUID', 'getRandomValues']) {
    if (Object.prototype.hasOwnProperty.call(c, k)) delete c[k]
  }
})

describe('request · 基本性质', () => {
  it('返回非空字符串', () => {
    expect(typeof genRequestId()).toBe('string')
    expect(genRequestId().length).toBeGreaterThan(0)
  })

  it('是 UUID v4', () => {
    expect(genRequestId()).toMatch(UUID_V4)
  })

  it('1000 次调用全部唯一(幂等键重复 = 防重复提交失效)', () => {
    const seen = new Set<string>()
    for (let i = 0; i < 1000; i++) seen.add(genRequestId())
    expect(seen.size).toBe(1000)
  })

  it('genIdempotencyKey 与 genRequestId 同形,且唯一', () => {
    expect(genIdempotencyKey()).toMatch(UUID_V4)
    const seen = new Set<string>()
    for (let i = 0; i < 1000; i++) seen.add(genIdempotencyKey())
    expect(seen.size).toBe(1000)
  })
})

describe('request · 与后端消毒规则的契约', () => {
  it('正常路径的 ID 后端会照单全收', () => {
    for (let i = 0; i < 100; i++) {
      expect(backendWouldAccept(genRequestId())).toBe(true)
    }
  })

  it('非安全上下文(没有 randomUUID)的 ID 后端也会照单全收', () => {
    withCrypto({ randomUUID: undefined }, () => {
      for (let i = 0; i < 100; i++) {
        const id = genRequestId()
        expect(backendWouldAccept(id), `id=${id}`).toBe(true)
        expect(id).toMatch(UUID_V4)
      }
    })
  })

  it('极老环境(连 getRandomValues 都没有)的 ID 后端也会照单全收', () => {
    withCrypto({ randomUUID: undefined, getRandomValues: undefined }, () => {
      for (let i = 0; i < 100; i++) {
        const id = genRequestId()
        expect(backendWouldAccept(id), `id=${id}`).toBe(true)
      }
    })
  })
})

describe('request · 非安全上下文的降级路径', () => {
  it('没有 randomUUID 时,仍然唯一', () => {
    const ids = new Set<string>()
    withCrypto({ randomUUID: undefined }, () => {
      for (let i = 0; i < 1000; i++) ids.add(genRequestId())
    })
    expect(ids.size).toBe(1000)
  })

  it('没有 randomUUID 时,走的是 getRandomValues 而不是 Math.random', () => {
    const spy = vi.spyOn(Math, 'random')
    try {
      withCrypto({ randomUUID: undefined }, () => {
        genRequestId()
      })
      expect(spy).not.toHaveBeenCalled()
    } finally {
      spy.mockRestore()
    }
  })

  it('连 getRandomValues 都没有时,兜底路径仍然唯一', () => {
    const ids = new Set<string>()
    withCrypto({ randomUUID: undefined, getRandomValues: undefined }, () => {
      for (let i = 0; i < 1000; i++) ids.add(genRequestId())
    })
    expect(ids.size).toBe(1000)
  })
})
