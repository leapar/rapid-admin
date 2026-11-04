/**
 * api/upload 的参数传递(M82 文件夹搜索)
 *
 * ## 这条网守的是什么
 *
 *   文件夹搜索要一路走通三层,每一层的名字都得对得上:
 *
 *   | 层 | 名字 | 定义在 |
 *   |---|---|---|
 *   | 前端 api 层 | `keyword` | 本文件 |
 *   | HTTP query | `keyword` | `http.get` 的第二个参数 |
 *   | 后端 handler | `c.Query("keyword")` | server/internal/api/handler/v1/file.go |
 *   | 后端 service | `FolderListQuery.Keyword` | server/internal/service/folder.go |
 *
 *   **任何一层名字对不上都是静默失效**:前端能打字、回车不报错、
 *   后端返回 200、列表原样返回全量。用户看到的是"搜索框没用"。
 *
 *   ⚠️ **页面级用例覆盖不到这一层** —— `pages/file/__tests__/folder.test.tsx`
 *   把整个 `@/api/upload` 模块 mock 掉了,所以"api 层有没有把 keyword
 *   真的放进 query"这件事,只有这个文件在守。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `keyword: q.keyword` 从 params 里删掉 → 「keyword 必须进 query」红;
 *   · 把 wire 名改成 `kw` → 同上红。
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
  instance: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

import { http } from '@/api/client'
import { listFolders, createFolder } from '@/api/upload'

const getMock = vi.mocked(http.get)
const postMock = vi.mocked(http.post)

describe('api/upload —— 文件夹搜索(M82)', () => {
  beforeEach(() => {
    getMock.mockReset()
  })

  it('keyword 必须进 query(名字要和后端 c.Query("keyword") 一致)', () => {
    listFolders({ keyword: '户型图' })

    expect(getMock).toHaveBeenCalledTimes(1)
    const [url, params] = getMock.mock.calls[0]!
    expect(url).toBe('/biz/folders')
    expect(params).toEqual(expect.objectContaining({ keyword: '户型图' }))
    // ⚠️ 名字错一个字母就是静默失效 —— 显式断言 key 名,不靠 objectContaining 蒙混
    expect(Object.keys(params as Record<string, unknown>)).toContain('keyword')
  })

  it('不传关键字时不要塞空串(空串和"没传"在后端是两回事)', () => {
    listFolders({})

    const [, params] = getMock.mock.calls[0]!
    expect((params as Record<string, unknown>).keyword).toBeUndefined()
  })

  it('biz 和分页参数跟 keyword 一起传(不能因为加了 keyword 就把别的丢了)', () => {
    listFolders({ biz: 'house', keyword: '户型图', page: 2, size: 50 })

    const [, params] = getMock.mock.calls[0]!
    expect(params).toEqual(
      expect.objectContaining({ biz: 'house', keyword: '户型图', page: 2, size: 50 }),
    )
  })
})

/**
 * parent_id 的三态(M125)
 *
 * ⚠️ **三态不能合并成两态** —— 后端 `FolderListQuery.ParentID` 是 `*uint64`:
 *   · nil(不传)= **不限层级**(老调用方 / 下拉框靠这个);
 *   · 0 = **只列根级**;
 *   · id = **只列它的直接子级**。
 *
 *   所以"不传"绝不能翻译成 "0" —— 那样"进入文件夹"和"看全部"就变成同一个请求,
 *   而界面上表现为"进去之后列的还是全部文件夹"。
 *
 * ⚠️ **值必须是字符串** —— 后端 DTO 是 `json:"parent_id,string"`,
 *   传数字会被 Go 的 encoding/json 拒掉("invalid use of ,string struct tag")。
 */
describe('api/upload —— 文件夹层级 parent_id(M125)', () => {
  beforeEach(() => {
    getMock.mockReset()
    postMock.mockReset()
  })

  it('不传 parentId 时不能塞 0(0 = 只列根级,不是"不限层级")', () => {
    listFolders({})

    const [, params] = getMock.mock.calls[0]!
    expect((params as Record<string, unknown>).parent_id).toBeUndefined()
  })

  it('parentId="0" 就是"只列根级"', () => {
    listFolders({ parentId: '0' })

    const [, params] = getMock.mock.calls[0]!
    expect((params as Record<string, unknown>).parent_id).toBe('0')
  })

  it('parentId 传具体 id 就是"只列它的直接子级",而且是字符串', () => {
    listFolders({ parentId: '123' })

    const [, params] = getMock.mock.calls[0]!
    const v = (params as Record<string, unknown>).parent_id
    expect(v).toBe('123')
    expect(typeof v).toBe('string')
  })

  it('createFolder 的 parent_id 必须是字符串("0" = 根级)', () => {
    createFolder({ name: '素材' })

    const [, body] = postMock.mock.calls[0]!
    const v = (body as Record<string, unknown>).parent_id
    expect(v).toBe('0')
    expect(typeof v).toBe('string')
  })

  it('createFolder 建子文件夹时把 parent_id 带上去', () => {
    createFolder({ name: '图纸', parentId: '77' })

    const [, body] = postMock.mock.calls[0]!
    expect((body as Record<string, unknown>).parent_id).toBe('77')
  })
})
