/**
 * 分页参数契约的回归网(2026-10-25)
 *
 * 后端只认 `page` + `size`(`BaseHandler.GetPage`,
 * server/internal/api/handler/handler.go:86),**全仓不读 `page_size`**。
 *
 * 以前前端三处写法不一,谁也没对上:
 *   · hooks/useTable.ts 运行时写 `pageSize`
 *   · types/api.ts 的 PageRequest 声明 `page_size`
 *   · api/approval.ts 自己翻成 `size`
 * 结果:**全仓 useTable 页面的"每页条数"选择器都是失效的** ——
 * 选了 50 条,前端发的是 `pageSize=50`,后端读 `size` 拿不到 → 按默认 20 返回。
 * (页码是对的,因为 `page` 三方一致 —— 所以这个 bug 特别难被发现。)
 *
 * 现在统一在 api/client.ts 的 query 出口翻译,这里锁住翻译规则 + 最终 URL。
 */
import { describe, it, expect, afterEach, vi } from 'vitest'
import { message } from 'antd'
import { http, instance, normalizeQueryParams } from '@/api/client'

describe('normalizeQueryParams', () => {
  it('page_size → size', () => {
    expect(normalizeQueryParams({ page: 1, page_size: 200 })).toEqual({ page: 1, size: 200 })
  })

  it('pageSize → size', () => {
    expect(normalizeQueryParams({ page: 1, pageSize: 50 })).toEqual({ page: 1, size: 50 })
  })

  it('显式的 size 优先(不会被旧名覆盖)', () => {
    expect(normalizeQueryParams({ size: 30, page_size: 200 })).toEqual({ size: 30 })
    expect(normalizeQueryParams({ size: 30, pageSize: 50 })).toEqual({ size: 30 })
  })

  it('没有旧名时原样返回(同一个对象引用)', () => {
    const p = { page: 1, size: 20, keyword: 'x' }
    expect(normalizeQueryParams(p)).toBe(p)
  })

  it('undefined → undefined', () => {
    expect(normalizeQueryParams(undefined)).toBeUndefined()
  })

  it('旧名不会留在 query 里(两个都不能出现)', () => {
    const out = normalizeQueryParams({ page: 1, page_size: 200, pageSize: 50 })!
    expect('page_size' in out).toBe(false)
    expect('pageSize' in out).toBe(false)
    expect(out.size).toBe(50)
  })
})

describe('http.get 的 query 出口(最终 URL)', () => {
  const originalAdapter = instance.defaults.adapter

  afterEach(() => {
    instance.defaults.adapter = originalAdapter
  })

  it('page_size 真的变成了 size —— 后端读得到', async () => {
    let captured: { params?: Record<string, unknown> } | undefined
    instance.defaults.adapter = async (config) => {
      captured = config
      return {
        data: { code: 0, data: { list: [], total: 0, page: 1, size: 200 }, message: 'ok' },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }

    await http.get('/system/users', { page: 1, page_size: 200 })

    // adapter 收到的是归一化后的 params
    expect(captured?.params).toEqual({ page: 1, size: 200 })

    // 再用 axios 自己的 URL 拼装确认最终 query 串长什么样
    const uri = instance.getUri({ url: '/system/users', params: captured?.params })
    expect(uri).toContain('size=200')
    expect(uri).not.toContain('page_size')
    expect(uri).not.toContain('pageSize')
  })
})

/**
 * 取消的请求不该报错(2026-10-25 M6)
 *
 * axios 主动 abort 抛的是 CanceledError(code='ERR_CANCELED',没有 response)。
 * 以前它一路落到错误拦截器最后那个 else 分支 → `message.error("canceled")`
 * → **每取消一个请求就闪一个红色 toast**。
 *
 * 用户主动取消(组件卸载 / 页面切换 / useRequest 的 cancel())是正常操作。
 */
describe('取消的请求', () => {
  it('abort 之后 reject 出去,但不弹 toast', async () => {
    const spy = vi.spyOn(message, 'error')
    const ac = new AbortController()
    ac.abort() // 已取消 —— axios 在 dispatchRequest 里就会抛 CanceledError

    await expect(
      http.get('/system/users', undefined, { signal: ac.signal }),
    ).rejects.toMatchObject({ canceled: true })

    expect(spy).not.toHaveBeenCalled()
  })

  it('被取消的请求不会被自动重试(重试一个已取消的请求毫无意义)', async () => {
    const originalAdapter = instance.defaults.adapter
    let calls = 0
    instance.defaults.adapter = async () => {
      calls++
      // 模拟 axios 的取消错误
      const err = new Error('canceled') as Error & { code?: string; name?: string }
      err.code = 'ERR_CANCELED'
      err.name = 'CanceledError'
      throw err
    }

    try {
      await expect(http.get('/system/users')).rejects.toBeTruthy()
      expect(calls).toBe(1)
    } finally {
      instance.defaults.adapter = originalAdapter
    }
  })
})
