/**
 * api/upload —— resolveFileUrl 的两条路(M80 私有文件直链)
 *
 * ## 这条网守的是什么
 *
 *   `<img src>` 要的是一个**浏览器能自己去拉**的地址。但后端有两种形态:
 *
 *   | 形态 | `/files/:id/url` | 前端怎么办 |
 *   |---|---|---|
 *   | OSS / 公开文件 | 返签名直链 | 直接用 |
 *   | **local 私有文件** | **给不出直链** | 退回 `/files/:id/content` 拉 blob |
 *
 *   "给不出直链"有两种可能的信号,**两种都得兜住**:
 *
 *   ① **200 + 空 url** —— 设计文档 §8.1 定的契约(新后端);
 *   ② **500** —— 旧后端(本地开发经常跑着旧二进制)。axios 拦截器遇 500
 *      直接 reject ⇒ 如果不 catch,那句 fallback **永远执行不到**。
 *
 *   ⚠️ **只测 ① 不测 ② 是假绿** —— 页面级用例和 api 级用例都容易只覆盖
 *   "空 url" 这条,于是"对着旧后端图片裂在原地"这个真故障一路溜过去。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `resolveFileUrl` 里的 `catch` 去掉 → 「/url 报错也要退回」红;
 *   · 把 `url = r?.url ?? ''` 改成 `url = r.url` → 「返空 url 也要退回」红;
 *   · 去掉 `silent: true` → 「/url 失败不许弹 toast」红。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

vi.mock('@/api/client', () => ({
  http: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  },
  instance: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

import { http, instance } from '@/api/client'
import { resolveFileUrl } from '@/api/upload'

const getMock = vi.mocked(http.get)
const instGetMock = vi.mocked(instance.get)

/**
 * ⚠️ **jsdom 没有 `URL.createObjectURL`** —— `src/test/setup.ts` 也没补
 * (它只 stub 了 ResizeObserver / matchMedia / getComputedStyle)。
 * 不 stub 的话 `fetchFileContentUrl` 会在这一行抛 `TypeError`,
 * 失败原因看起来像"fallback 有 bug",其实是环境缺 API。
 */
const createObjectURL = vi.fn(() => 'blob:mock-object-url')

describe('api/upload —— resolveFileUrl(M80 私有文件直链)', () => {
  beforeEach(() => {
    getMock.mockReset()
    instGetMock.mockReset()
    createObjectURL.mockClear()
    vi.stubGlobal('URL', { ...URL, createObjectURL, revokeObjectURL: vi.fn() })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('拿到直链就直接返回(不能多打一次 /content —— 那是白拉一遍字节)', async () => {
    getMock.mockResolvedValue({ url: 'https://cdn.example.com/a.jpg' })
    instGetMock.mockResolvedValue({ data: new Blob() })

    const u = await resolveFileUrl('123')

    expect(u).toBe('https://cdn.example.com/a.jpg')
    expect(instGetMock).not.toHaveBeenCalled()
  })

  it('/url 返空串 → 退回 /content 中转(新后端的契约:200 + 空 url)', async () => {
    getMock.mockResolvedValue({ url: '' })
    instGetMock.mockResolvedValue({ data: new Blob(['bytes']) })

    const u = await resolveFileUrl('123')

    expect(instGetMock).toHaveBeenCalledTimes(1)
    expect(instGetMock.mock.calls[0]![0]).toBe('/files/123/content')
    expect(u).toBe('blob:mock-object-url')
  })

  it('/url 报错 → 照样退回 /content(旧后端返 500 的情形)', async () => {
    getMock.mockRejectedValue(new Error('500 生成访问地址失败'))
    instGetMock.mockResolvedValue({ data: new Blob(['bytes']) })

    const u = await resolveFileUrl('123')

    expect(instGetMock).toHaveBeenCalledTimes(1)
    expect(u).toBe('blob:mock-object-url')
  })

  it('/url 失败时必须静默(那条 500 是预期内的,弹红条只会误导用户)', async () => {
    getMock.mockRejectedValue(new Error('boom'))
    instGetMock.mockResolvedValue({ data: new Blob() })

    await resolveFileUrl('123')

    expect(getMock.mock.calls[0]![2]).toEqual(expect.objectContaining({ silent: true }))
  })

  it('两条路都失败 → 错误照样抛出去(不能把"文件已删 / 401"也吞成空串)', async () => {
    getMock.mockRejectedValue(new Error('500'))
    instGetMock.mockRejectedValue(new Error('404 文件不存在'))

    await expect(resolveFileUrl('123')).rejects.toThrow('404 文件不存在')
  })
})
