/**
 * useTable 的分页字段回归网(2026-10-25)
 *
 * 后端只认 `page` + `size`。以前这个 hook 内部写的是 `pageSize`,
 * 而类型声明的是 `page_size` —— **后端两个都不读**,
 * 于是全仓 useTable 页面的"每页条数"选择器都是失效的
 * (选了 50 条,后端按默认 20 返回;页码是对的,因为 `page` 三方一致)。
 *
 * 这里锁三件事:
 *   1. query 里只有 `size`,没有 `pageSize` / `page_size`
 *   2. 分页器 onChange 改每页条数 → query.size 跟着变
 *   3. fetch 收到的 query 直接就是能发到后端的形状(page + size)
 *
 * ⚠️ 2026-10-25 M6 追加:请求竞态 —— 两个请求同时在飞时,
 *   **先发的不一定先回**。旧的响应晚到会把新数据覆盖掉,
 *   页面显示的是"上一次筛选"的结果,而筛选框写的是新的。
 */
import { describe, it, expect, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useTable } from '@/hooks/useTable'

const emptyPage = { list: [], total: 0, page: 1, size: 20 }

describe('useTable · 分页字段', () => {
  it('初始 query 用 size,没有 pageSize / page_size', async () => {
    const fetchFn = vi.fn().mockResolvedValue(emptyPage)
    const { result } = renderHook(() => useTable({ fetch: fetchFn }))

    await waitFor(() => expect(fetchFn).toHaveBeenCalled())
    expect(result.current.query.size).toBe(20)
    expect('pageSize' in result.current.query).toBe(false)
    expect('page_size' in result.current.query).toBe(false)
  })

  it('改每页条数 → query.size 和分页器都跟着变(以前改不动)', async () => {
    const fetchFn = vi.fn().mockResolvedValue(emptyPage)
    const { result } = renderHook(() => useTable({ fetch: fetchFn }))
    await waitFor(() => expect(fetchFn).toHaveBeenCalled())

    act(() => {
      result.current.tableProps.pagination.onChange(3, 50)
    })

    await waitFor(() => expect(result.current.query.page).toBe(3))
    expect(result.current.query.size).toBe(50)
    expect(result.current.tableProps.pagination.pageSize).toBe(50)
    // 最后一次 fetch 收到的就是这两个值
    const last = fetchFn.mock.calls[fetchFn.mock.calls.length - 1]![0] as Record<string, unknown>
    expect(last).toMatchObject({ page: 3, size: 50 })
  })

  it('reset 回到第一页 + 默认 20 条', async () => {
    const fetchFn = vi.fn().mockResolvedValue(emptyPage)
    const { result } = renderHook(() => useTable({ fetch: fetchFn }))
    await waitFor(() => expect(fetchFn).toHaveBeenCalled())

    act(() => {
      result.current.tableProps.pagination.onChange(4, 100)
    })
    await waitFor(() => expect(result.current.query.page).toBe(4))

    act(() => {
      result.current.reset()
    })
    await waitFor(() => expect(result.current.query.page).toBe(1))
    expect(result.current.query.size).toBe(20)
  })
})

/**
 * 请求竞态(2026-10-25 M6)
 *
 * 场景:用户在"状态"下拉框里快速切了两个值 →
 *   请求 A(旧条件)先发出但**后回来**,请求 B(新条件)后发出但**先回来**。
 *   → A 的响应晚到,把 B 的新数据覆盖成旧数据。
 *
 * 这个 bug 之前没人发现,因为它要后端响应顺序反常才显形;
 * 但慢接口 + 用户狂点筛选 = 生产上必然出现。
 */
describe('useTable · 请求竞态', () => {
  it('旧响应晚到也不能覆盖新数据', async () => {
    let resolveOld!: (v: unknown) => void
    const oldPending = new Promise((r) => {
      resolveOld = r
    })
    const fetchFn = vi
      .fn()
      // 第一次(旧条件):慢,一直挂着
      .mockReturnValueOnce(oldPending)
      // 第二次(新条件):快,先回
      .mockResolvedValueOnce({
        list: [{ id: 'new' }],
        total: 1,
        page: 1,
        size: 20,
      })

    const { result } = renderHook(() => useTable({ fetch: fetchFn }))
    await waitFor(() => expect(fetchFn).toHaveBeenCalledTimes(1))

    // 改条件 → 第二次请求
    act(() => {
      result.current.search({ status: 'active' })
    })
    await waitFor(() => expect(fetchFn).toHaveBeenCalledTimes(2))

    // 新的先回来 → 页面显示新数据
    await waitFor(() => expect(result.current.data).toHaveLength(1))
    expect((result.current.data[0] as { id: string }).id).toBe('new')

    // ⚠️ 旧的现在才回来 —— **不能**把新数据覆盖掉
    await act(async () => {
      resolveOld({ list: [{ id: 'old' }], total: 1, page: 1, size: 20 })
      await Promise.resolve()
    })

    expect((result.current.data[0] as { id: string }).id).toBe('new')
    expect(result.current.total).toBe(1)
  })

  it('旧请求收尾也不能把新请求的 loading 关掉(转圈提前消失 = 数据还没到)', async () => {
    let resolveOld!: (v: unknown) => void
    const oldPending = new Promise((r) => {
      resolveOld = r
    })
    const fetchFn = vi
      .fn()
      .mockReturnValueOnce(oldPending)
      .mockReturnValueOnce(new Promise(() => {})) // 新请求一直挂着(模拟慢)

    const { result } = renderHook(() => useTable({ fetch: fetchFn }))
    await waitFor(() => expect(fetchFn).toHaveBeenCalledTimes(1))

    act(() => {
      result.current.search({ status: 'active' })
    })
    await waitFor(() => expect(fetchFn).toHaveBeenCalledTimes(2))
    expect(result.current.loading).toBe(true)

    // 旧的先收尾 → loading 必须还是 true(新请求还在飞)
    await act(async () => {
      resolveOld({ list: [], total: 0, page: 1, size: 20 })
      await Promise.resolve()
    })

    expect(result.current.loading).toBe(true)
  })
})

/**
 * 失败处理(2026-10-25 M6)
 *
 * 以前 fetch 失败会变成 unhandled rejection(useEffect 里 `load(query)` 没有 catch)
 * → 控制台一片红,而且页面既不知道失败了、也没地方显示。
 */
describe('useTable · 失败处理', () => {
  it('fetch 失败 → 记进 error,不抛未处理的 rejection,也不清空已有数据', async () => {
    const boom = new Error('boom')
    const fetchFn = vi.fn().mockRejectedValue(boom)
    const { result } = renderHook(() => useTable({ fetch: fetchFn }))

    await waitFor(() => expect(result.current.error).toBe(boom))
    expect(result.current.loading).toBe(false)
    expect(result.current.data).toEqual([])
  })

  it('失败过一次之后重新加载成功 → error 被清掉', async () => {
    const fetchFn = vi
      .fn()
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce({ list: [{ id: 'x' }], total: 1, page: 1, size: 20 })

    const { result } = renderHook(() => useTable({ fetch: fetchFn }))
    await waitFor(() => expect(result.current.error).toBeInstanceOf(Error))

    await act(async () => {
      await result.current.reload()
    })

    expect(result.current.error).toBeNull()
    expect(result.current.data).toHaveLength(1)
  })
})

/**
 * 断网恢复自动重载(2026-10-25 M7)
 *
 * 以前这条链是断的:OfflineBanner 只负责"提示",它自己的注释写着
 * "网络恢复时调 useTable 的 reload 是组件外的事,此组件只负责提示,不做事"
 * —— 而**没有任何组件**去接这个"组件外的事"。
 * 结果:断网时列表加载失败,网络恢复后页面还是空的,
 * 用户只能自己刷新浏览器。
 */
describe('useTable · 断网恢复自动重载', () => {
  it('失败之后网络恢复 → 自动重载一次,错误被清掉', async () => {
    const fetchFn = vi
      .fn()
      .mockRejectedValueOnce(new Error('Network Error'))
      .mockResolvedValue({ list: [{ id: 'x' }], total: 1, page: 1, size: 20 })

    const { result } = renderHook(() => useTable({ fetch: fetchFn }))
    await waitFor(() => expect(result.current.error).toBeInstanceOf(Error))
    expect(fetchFn).toHaveBeenCalledTimes(1)

    await act(async () => {
      window.dispatchEvent(new Event('online'))
      await Promise.resolve()
    })

    await waitFor(() => expect(result.current.error).toBeNull())
    expect(fetchFn).toHaveBeenCalledTimes(2)
    expect(result.current.data).toHaveLength(1)
  })

  it('本来就加载成功 → 网络恢复不重复请求(别白刷)', async () => {
    const fetchFn = vi.fn().mockResolvedValue({ list: [{ id: 'x' }], total: 1, page: 1, size: 20 })
    renderHook(() => useTable({ fetch: fetchFn }))
    await waitFor(() => expect(fetchFn).toHaveBeenCalledTimes(1))

    await act(async () => {
      window.dispatchEvent(new Event('online'))
      await Promise.resolve()
    })

    expect(fetchFn).toHaveBeenCalledTimes(1)
  })
})
