/**
 * M110 —— §2.1 前端 utils 层零覆盖审计的回归网(2026-10-29)
 *
 * 这一组锁的是 **取消语义在 HTTP 层的自洽性**:
 *
 * ⚠️ **M110 P2 真 bug:`isCanceled()` 认不出自己规范化出来的取消对象**
 *   client.ts 的错误拦截器里,取消走这条分支:
 *     `if (isCanceled(error)) return Promise.reject(canceledError())`
 *   而 `canceledError()` 产出的是 `{ code: 'CANCELED', message: 'canceled', canceled: true }`。
 *   旧的 `isCanceled()` 只认 `ERR_CANCELED` / `CanceledError` / `__CANCEL__`
 *   ⇒ **对框架自己抛的取消恒 false**。
 *   而 `isCanceled` 是**导出给业务层用**的判断函数(判"这是取消,不是失败"),
 *   业务层拿它判框架的取消对象 ⇒ 永远判不出 ⇒ 被取消的请求被当成失败上报。
 *
 * ⚠️ 另一条同源的坑(utils/retry.ts 的退避被打断):
 *   那条路径此前抛 `DOMException('Aborted','AbortError')` ⇒ DOMException **有**
 *   `code` 属性 ⇒ client.ts 的 `isNormalizedError()` 判它是"已规范化"
 *   ⇒ 原样 reject ⇒ 调用方拿到 `{ code: 20, name: 'AbortError' }`,
 *   与 client 自己的 `{ code: 'CANCELED', canceled: true }` **不同形**。
 *   现在两条路径统一走 `canceledError()`(真源在 utils/retry.ts)。
 *
 * 另外锁住 5 个 axios 泛型参数的修复:
 *   `instance.get<unknown, ApiResponse<T>>(…)` —— 第二个泛型把 `AxiosResponse`
 *   顶掉了 ⇒ `r` 的类型变成 `ApiResponse<T>` ⇒ `r.data.data` 编译不过
 *   (5 处 tsc 错误)。运行时是对的,**类型是错的** —— 改错一个字段名没人会发现。
 */
import { describe, it, expect, vi, afterEach } from 'vitest'
import { message } from 'antd'
import { http, instance, isCanceled } from '@/api/client'
import { canceledError } from '@/utils/retry'

const originalAdapter = instance.defaults.adapter

afterEach(() => {
  instance.defaults.adapter = originalAdapter
})

describe('isCanceled · 认得出框架自己的取消对象(M110 P2 回归)', () => {
  it('canceledError() 产出的形状 → true', () => {
    expect(isCanceled(canceledError())).toBe(true)
  })

  it('code === "CANCELED" → true', () => {
    expect(isCanceled({ code: 'CANCELED' })).toBe(true)
  })

  it('canceled === true → true', () => {
    expect(isCanceled({ canceled: true })).toBe(true)
  })

  it('axios 自己的三种形状 → true', () => {
    expect(isCanceled({ code: 'ERR_CANCELED' })).toBe(true)
    expect(isCanceled({ name: 'CanceledError' })).toBe(true)
    expect(isCanceled({ __CANCEL__: true })).toBe(true)
  })

  it('普通错误 → false', () => {
    expect(isCanceled({ code: 50000, message: 'boom' })).toBe(false)
    expect(isCanceled({ code: 'ECONNABORTED' })).toBe(false)
    expect(isCanceled(new Error('boom'))).toBe(false)
  })

  it('空值 → false', () => {
    expect(isCanceled(null)).toBe(false)
    expect(isCanceled(undefined)).toBe(false)
    expect(isCanceled(0)).toBe(false)
  })
})

describe('取消的请求 · 对外形状是统一的(M110)', () => {
  it('axios abort ⇒ reject 出去的是 canceledError() 形状', async () => {
    const ac = new AbortController()
    ac.abort()

    const err = await http
      .get('/system/users', undefined, { signal: ac.signal })
      .then(() => null)
      .catch((e: unknown) => e)

    expect(err).toMatchObject({ code: 'CANCELED', canceled: true })
    expect(isCanceled(err)).toBe(true)
  })

  it('被取消的请求不弹 toast', async () => {
    const spy = vi.spyOn(message, 'error')
    const ac = new AbortController()
    ac.abort()

    await expect(http.get('/system/users', undefined, { signal: ac.signal })).rejects.toBeTruthy()
    expect(spy).not.toHaveBeenCalled()
  })

  it('退避被打断(utils/retry.ts 的路径)⇒ 形状与上面完全一致', async () => {
    // 这一条锁的是"两条取消路径同形":utils/retry.ts 的 sleep() 被 abort 打断时
    // 抛的必须是 canceledError(),**不是** DOMException(DOMException.code = 20)。
    // 走真实的拦截器链:adapter 抛一个可重试的 500 → 退避 → 退避期间 abort。
    const ac = new AbortController()
    instance.defaults.adapter = async (config) => {
      const err = new Error('server error') as Error & {
        isAxiosError?: boolean
        response?: unknown
        config?: unknown
      }
      err.isAxiosError = true
      err.config = config
      err.response = { status: 500, data: { code: 50000, message: 'err' }, headers: {}, config }
      throw err
    }

    const p = http.get('/system/users', undefined, { signal: ac.signal, retryBaseMs: 2000 })
    // 等退避开始后打断
    await new Promise((r) => setTimeout(r, 20))
    ac.abort()

    const err = await p.then(() => null).catch((e: unknown) => e)
    expect(err).toMatchObject({ code: 'CANCELED', canceled: true })
    expect(isCanceled(err)).toBe(true)
  })
})

describe('http · 泛型修复后的解包契约(M110 顺手修的 5 条 tsc 错误)', () => {
  const ok = (data: unknown) => ({
    data: { code: 0, message: 'ok', data },
    status: 200,
    statusText: 'OK',
    headers: {},
    config: {},
  })

  it('get / post / put / patch / delete 都解包到 r.data.data', async () => {
    instance.defaults.adapter = async (config) =>
      ok({ url: config.url, method: config.method }) as never

    const g = await http.get<{ url: string; method: string }>('/a')
    expect(g).toEqual({ url: '/a', method: 'get' })

    const p = await http.post<{ url: string; method: string }>('/b', { x: 1 })
    expect(p).toEqual({ url: '/b', method: 'post' })

    const u = await http.put<{ url: string; method: string }>('/c', { x: 1 })
    expect(u).toEqual({ url: '/c', method: 'put' })

    const pa = await http.patch<{ url: string; method: string }>('/d', { x: 1 })
    expect(pa).toEqual({ url: '/d', method: 'patch' })

    const d = await http.delete<{ url: string; method: string }>('/e')
    expect(d).toEqual({ url: '/e', method: 'delete' })
  })
})
