/**
 * utils/retry.ts 回归网(2026-10-29 M110)
 *
 * 这一层此前**一条网都没有** —— 而它挂在**全站每个请求**的失败路径上
 * (api/client.ts 的错误拦截器里 `await retryRequest(error, instance)`)。
 *
 * 锁住的:
 *   ① **重试策略**(哪些该重试、哪些绝对不能重试)
 *      —— 写操作默认不重试(避免双发)、4xx 业务错不重试、用户 abort 不重试。
 *   ② **退避期间被 abort ⇒ 对外错误形状必须与 client.ts 的取消分支同形**(M110 P1)
 *      此前 `sleep()` 抛的是 `DOMException('Aborted','AbortError')`:
 *        · DOMException **有** `code` 属性 ⇒ client.ts 的 `isNormalizedError()`
 *          判它是"已规范化" ⇒ 原样 reject ⇒ 调用方拿到 `{ code: 20, name: 'AbortError' }`
 *        · 而 client.ts 自己的取消分支 reject 的是 `{ code: 'CANCELED', canceled: true }`
 *        · ⇒ **同一个"取消"有两副面孔**,业务层判不出这是取消还是失败
 *      现在两条路径统一走 `canceledError()`(真源在本文件,client.ts 引用它 ——
 *      反过来会让 client.ts ↔ retry.ts 形成循环依赖)。
 *   ③ **maxAttempts / retry:false / retry:true / 写操作** 这些开关必须真的生效
 *      —— 之前一个都没有网,改了没人知道。
 *
 * 反向探针(见 scripts/probe_m110.mjs):把每处修复拆掉,对应用例必须变红。
 */
import { describe, it, expect, vi } from 'vitest'
import type { AxiosError, AxiosRequestConfig, InternalAxiosRequestConfig } from 'axios'
import { retryRequest, isRetrying, canceledError } from '@/utils/retry'

/** 造一个"够用"的 AxiosError —— retry.ts 只读这几个字段 */
function makeError(opts: {
  status?: number
  method?: string
  code?: string
  name?: string
  message?: string
  noResponse?: boolean
  retry?: boolean
  retryMax?: number
  retryBaseMs?: number
  signal?: AbortSignal
}): AxiosError {
  const config = {
    method: opts.method ?? 'get',
    headers: {},
    url: '/test',
    ...(opts.retry !== undefined ? { retry: opts.retry } : {}),
    ...(opts.retryMax !== undefined ? { retryMax: opts.retryMax } : {}),
    ...(opts.retryBaseMs !== undefined ? { retryBaseMs: opts.retryBaseMs } : {}),
    ...(opts.signal ? { signal: opts.signal } : {}),
  } as unknown as InternalAxiosRequestConfig & AxiosRequestConfig

  const err = new Error(opts.message ?? 'boom') as AxiosError
  err.config = config
  if (opts.code) err.code = opts.code
  if (opts.name) err.name = opts.name
  if (!opts.noResponse) {
    err.response = {
      status: opts.status ?? 500,
      data: { code: 50000, message: 'err', data: null },
      statusText: '',
      headers: {},
      config,
    } as never
  }
  return err
}

/** 假的 instance —— 记录重发次数 */
function fakeInstance(result: { data: unknown } = { data: 'ok' }) {
  const request = vi.fn(async (_cfg: AxiosRequestConfig) => result)
  return { request, instance: { request } }
}

describe('retry · 该重试的会重试', () => {
  it('500 → 重发一次', async () => {
    const { request, instance } = fakeInstance()
    const err = makeError({ status: 500, retryBaseMs: 1 })

    const out = await retryRequest(err, instance)
    expect(request).toHaveBeenCalledTimes(1)
    expect(out).toEqual({ data: 'ok' })
  })

  it('408 / 429 → 重发', async () => {
    for (const status of [408, 429]) {
      const { request, instance } = fakeInstance()
      await retryRequest(makeError({ status, retryBaseMs: 1 }), instance)
      expect(request, `status=${status}`).toHaveBeenCalledTimes(1)
    }
  })

  it('网络错误(没有 response)→ 重发', async () => {
    const { request, instance } = fakeInstance()
    await retryRequest(makeError({ noResponse: true, retryBaseMs: 1 }), instance)
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('ECONNABORTED / Network Error → 重发', async () => {
    const { request, instance } = fakeInstance()
    await retryRequest(
      makeError({ code: 'ECONNABORTED', noResponse: true, retryBaseMs: 1 }),
      instance,
    )
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('重发时带上 _retryCount 与 _isRetryAttempt(M101 契约)', async () => {
    const { request, instance } = fakeInstance()
    const err = makeError({ status: 500, retryBaseMs: 1 })
    await retryRequest(err, instance)
    const cfg = request.mock.calls[0]![0] as InternalAxiosRequestConfig & {
      _retryCount?: number
      _isRetryAttempt?: boolean
    }
    expect(cfg._retryCount).toBe(1)
    expect(cfg._isRetryAttempt).toBe(true)
  })
})

describe('retry · 绝对不能重试的', () => {
  it('4xx 业务错 → 不重发,原样抛', async () => {
    const { request, instance } = fakeInstance()
    const err = makeError({ status: 400, retryBaseMs: 1 })
    await expect(retryRequest(err, instance)).rejects.toBe(err)
    expect(request).not.toHaveBeenCalled()
  })

  it('401 → 不重发(重发也还是 401)', async () => {
    const { request, instance } = fakeInstance()
    const err = makeError({ status: 401, retryBaseMs: 1 })
    await expect(retryRequest(err, instance)).rejects.toBe(err)
    expect(request).not.toHaveBeenCalled()
  })

  it('写操作(POST/PUT/PATCH/DELETE)默认不重发(避免双发)', async () => {
    for (const method of ['post', 'put', 'patch', 'delete']) {
      const { request, instance } = fakeInstance()
      const err = makeError({ status: 500, method, retryBaseMs: 1 })
      await expect(retryRequest(err, instance)).rejects.toBe(err)
      expect(request, `method=${method}`).not.toHaveBeenCalled()
    }
  })

  it('写操作显式 retry:true → 重发', async () => {
    const { request, instance } = fakeInstance()
    await retryRequest(
      makeError({ status: 500, method: 'post', retry: true, retryBaseMs: 1 }),
      instance,
    )
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('retry:false → 不重发(业务页显式声明)', async () => {
    const { request, instance } = fakeInstance()
    const err = makeError({ status: 500, retry: false, retryBaseMs: 1 })
    await expect(retryRequest(err, instance)).rejects.toBe(err)
    expect(request).not.toHaveBeenCalled()
  })

  it('用户主动 abort(ERR_CANCELED / CanceledError)→ 不重发', async () => {
    for (const opts of [{ code: 'ERR_CANCELED' }, { name: 'CanceledError' }]) {
      const { request, instance } = fakeInstance()
      const err = makeError({ status: 500, noResponse: true, retryBaseMs: 1, ...opts })
      await expect(retryRequest(err, instance)).rejects.toBe(err)
      expect(request).not.toHaveBeenCalled()
    }
  })

  it('retryMax 到了就不再重发', async () => {
    const { request, instance } = fakeInstance()
    const err = makeError({ status: 500, retryMax: 1, retryBaseMs: 1 })

    // 第一次:重发
    await retryRequest(err, instance)
    expect(request).toHaveBeenCalledTimes(1)

    // 第二次:同一个 config,已重试过 1 次 == retryMax ⇒ 直接抛
    await expect(retryRequest(err, instance)).rejects.toBe(err)
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('没有 config → 直接抛(无从判断)', async () => {
    const { instance } = fakeInstance()
    const err = new Error('boom') as AxiosError
    await expect(retryRequest(err, instance)).rejects.toBe(err)
  })
})

describe('retry · 取消(M110 P1:两条取消路径必须同形)', () => {
  it('退避等待期间被 abort ⇒ reject 的是 canceledError() 形状', async () => {
    const ac = new AbortController()
    const { request, instance } = fakeInstance()
    const err = makeError({ status: 500, retryBaseMs: 1000, signal: ac.signal })

    const p = retryRequest(err, instance)
    // sleep() 已经把 abort 监听挂上了(同步注册),此刻打断退避
    ac.abort()

    await expect(p).rejects.toMatchObject({ code: 'CANCELED', canceled: true })
    expect(request).not.toHaveBeenCalled()
  })

  it('**不是 DOMException**(此前抛的是 DOMException,code=20)', async () => {
    const ac = new AbortController()
    const { instance } = fakeInstance()
    const err = makeError({ status: 500, retryBaseMs: 1000, signal: ac.signal })

    const p = retryRequest(err, instance)
    ac.abort()

    await expect(p).rejects.not.toBeInstanceOf(DOMException)
    await expect(p).rejects.not.toHaveProperty('name', 'AbortError')
  })

  it('signal 一开始就是 aborted ⇒ 立即 reject,不发请求', async () => {
    const ac = new AbortController()
    ac.abort()
    const { request, instance } = fakeInstance()
    const err = makeError({ status: 500, retryBaseMs: 1000, signal: ac.signal })

    await expect(retryRequest(err, instance)).rejects.toMatchObject({
      code: 'CANCELED',
      canceled: true,
    })
    expect(request).not.toHaveBeenCalled()
  })

  it('canceledError() 的形状是固定的(client.ts 的取消分支用它)', () => {
    expect(canceledError()).toEqual({ code: 'CANCELED', message: 'canceled', canceled: true })
  })
})

describe('retry · isRetrying', () => {
  it('没有 _retryCount → false', () => {
    expect(isRetrying(undefined)).toBe(false)
    expect(isRetrying({} as InternalAxiosRequestConfig)).toBe(false)
  })

  it('_retryCount > 0 → true', () => {
    expect(isRetrying({ _retryCount: 1 } as unknown as InternalAxiosRequestConfig)).toBe(true)
  })
})
