/**
 * api/config 的参数传递(2026-10-28 M107)
 *
 * ## 这条网守的是什么
 *
 *   `dictApi.byGroup` 以前**手拼 query**:
 *     http.get(`/system/dicts?groupCode=${groupCode}`)
 *   ⇒ groupCode 里出现 `&` / `#` / 空格时,query 被截断或改写。
 *   现在的 30 个组 code 都是下划线,所以线上没炸 —— 但这是**哑弹**:
 *   哪天有个组 code 带上了连字符以外的东西,就静默查错组。
 *
 *   改成 params 对象(`http.get('/system/dicts', { groupCode })`),
 *   编码交给 axios —— 和同一文件里的 `dictApi.list` 走同一条出口。
 *
 * ⚠️ **断言的是"走 params 而不是手拼 URL"**,不是"值被编码了" ——
 *   编码发生在 axios 里,本层看不到;本层能保证的只有**没有手拼**。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   把 `byGroup` 改回模板字符串拼 query → 第一条立刻变红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@/api/client', () => ({
  http: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  },
}))

import { http } from '@/api/client'
import { dictApi } from '@/api/config'

const getMock = vi.mocked(http.get)

describe('api/config · dictApi(M107)', () => {
  beforeEach(() => {
    getMock.mockReset()
  })

  it('byGroup 用 params 传参(不在 URL 里手拼 query)', () => {
    dictApi.byGroup('common_status')

    expect(getMock).toHaveBeenCalledTimes(1)
    expect(getMock).toHaveBeenCalledWith('/system/dicts', { groupCode: 'common_status' })
  })

  it('byGroup 的 URL 里不含 query(否则 groupCode 里的 & 会截断参数)', () => {
    dictApi.byGroup('a&b=c')

    const url = getMock.mock.calls[0]![0]
    expect(url).not.toContain('?')
    expect(url).not.toContain('&')
  })

  it('list 同样是 params 传参(两条出口一致)', () => {
    dictApi.list({ groupCode: 'common_status' })

    expect(getMock).toHaveBeenCalledWith('/system/dicts', { groupCode: 'common_status' })
  })
})
