/**
 * Client 层测试 —— fetch 用注入替身(ClientOptions.fetch),不打真网络
 *
 * ⚠️ **断言抓的是"信封语义"** —— code≠0 也抛错、Abort 单独分类、
 * token 头挂没挂 —— 这些是集成方最容易踩的坑。
 */
import { describe, expect, it, vi } from 'vitest'
import { ClientResponseError, DynClient } from '../src'

type FetchHandler = (url: string, init: RequestInit) => Response

/** 用注入 fetch 建客户端;返回 client、记录到的请求列表、可复用的 fetch 替身 */
function makeClient(handler: FetchHandler) {
  const calls: Array<{ url: string; init: RequestInit }> = []
  const fetchImpl = vi.fn(async (url: any, init: any) => {
    calls.push({ url: String(url), init })
    return handler(String(url), init)
  })
  const client = new DynClient('https://api.test/rapid-admin/v1', { fetch: fetchImpl as any })
  return { client, calls, fetchImpl: fetchImpl as any }
}

function okResponse(data: unknown) {
  return new Response(
    JSON.stringify({ code: 0, data, message: 'ok', timestamp: 1730000000000 }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  )
}

describe('构造', () => {
  it('空 baseURL 当场报错', () => {
    expect(() => new DynClient('', { fetch: fetch as any })).toThrow(/baseURL/)
  })

  it('baseURL 去掉结尾斜杠', async () => {
    const { client, calls } = makeClient(() => okResponse(null))
    const c2 = new DynClient('https://api.test/rapid-admin/v1/', { fetch: async () => okResponse(null) })
    expect(c2.baseURL).toBe('https://api.test/rapid-admin/v1')
    await client.send('/ping')
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/ping')
  })
})

describe('send 信封语义', () => {
  it('成功解 data;query 空值不进 URL', async () => {
    const { client, calls } = makeClient(() => okResponse({ id: '1' }))
    const data = await client.send('/x', { query: { a: '1', b: '', c: undefined as any } })
    expect(data).toEqual({ id: '1' })
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/x?a=1')
  })

  it('HTTP 200 但业务码非 0 也抛错(带 code + trace_id)', async () => {
    const { client } = makeClient(
      () =>
        new Response(
          JSON.stringify({ code: 40301, message: '没有权限', timestamp: 1, trace_id: 't-1' }),
          { status: 200 },
        ),
    )
    const err = await client.send('/x').catch((e) => e)
    expect(err).toBeInstanceOf(ClientResponseError)
    expect(err.code).toBe(40301)
    expect(err.status).toBe(200)
    expect(err.message).toBe('没有权限')
    expect(err.traceId).toBe('t-1')
  })

  it('HTTP 403 空 body(经典 CORS 拦截长相)提示 CORS 可能', async () => {
    const { client } = makeClient(() => new Response('', { status: 403 }))
    const err = await client.send('/x').catch((e) => e)
    expect(err.status).toBe(403)
    expect(err.message).toContain('CORS')
  })

  it('非 JSON 响应体不炸解析,抛带 status 的错', async () => {
    const { client } = makeClient(() => new Response('<html>bad gateway</html>', { status: 502 }))
    const err = await client.send('/x').catch((e) => e)
    expect(err.status).toBe(502)
    expect(err.message).toContain('502')
  })

  it('fetch 抛网络错 → status=0 的 ClientResponseError', async () => {
    const client = new DynClient('https://api.test', {
      fetch: async () => {
        throw new Error('connect ECONNREFUSED')
      },
    })
    const err = await client.send('/x').catch((e) => e)
    expect(err.status).toBe(0)
    expect(err.message).toContain('ECONNREFUSED')
  })

  it('AbortSignal 触发 → isAbort = true', async () => {
    const client = new DynClient('https://api.test', {
      fetch: async (_url: any, init: any) => {
        const err = new Error('The operation was aborted')
        err.name = 'AbortError'
        throw err
      },
    })
    const err = await client.send('/x', { signal: new AbortController().signal }).catch((e) => e)
    expect(err.isAbort).toBe(true)
  })
})

describe('鉴权', () => {
  it('有 token 挂 Authorization,没有就不挂', async () => {
    const { client, calls } = makeClient(() => okResponse(null))
    await client.send('/x')
    expect(calls[0].init.headers).not.toHaveProperty('Authorization')

    client.authStore.save('tk-1')
    await client.send('/x')
    expect((calls[1].init.headers as any).Authorization).toBe('Bearer tk-1')

    client.authStore.clear()
    await client.send('/x')
    expect(calls[2].init.headers).not.toHaveProperty('Authorization')
  })

  it('authStore.save 同值不通知、变值通知;clear 通知空串', () => {
    const client = new DynClient('https://api.test', { fetch: async () => okResponse(null) })
    const seen: string[] = []
    client.authStore.onChange((t) => seen.push(t))
    client.authStore.save('a')
    client.authStore.save('a')
    client.authStore.save('b')
    client.authStore.clear()
    expect(seen).toEqual(['a', 'b', ''])
  })

  it('beforeSend 钩子可加请求头', async () => {
    const { calls, fetchImpl } = makeClient(() => okResponse(null))
    const plain = new DynClient('https://api.test', { fetch: fetchImpl })
    const hooked = new DynClient('https://api.test', {
      fetch: fetchImpl,
      beforeSend: (_url, init) => ({
        ...init,
        headers: { ...(init.headers as any), 'X-Channel': 'third-party' },
      }),
    })
    await plain.send('/x')
    await hooked.send('/x')
    expect((calls[0].init.headers as any)).not.toHaveProperty('X-Channel')
    expect((calls[1].init.headers as any)['X-Channel']).toBe('third-party')
  })
})
