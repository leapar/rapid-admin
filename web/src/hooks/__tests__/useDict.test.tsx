/**
 * useDict 的回归网(2026-10-28 M107)
 *
 * ## 这条网守的是什么
 *
 * ### ① 「临时下线某个字典项」是个**死开关**(P1)
 *
 *   `docs/数据字典管理规范.md` 规则 2 写着:
 *     "前端 `useDict()` 默认只返回 `is_active=true` 的项 → 自动隐藏"
 *
 *   但修复前:
 *     · `useDict` 的 `options` 把**每一项**都 map 进去,不过滤;
 *     · 后端 `ListDict` 也**不过滤**(`tx.Where("group_code = ?")` 就完了);
 *     · 只有 `ListDictGroups`(分组列表)过滤了 `is_active = true` —— 两个接口自相矛盾。
 *
 *   ⇒ 运营在字典管理页把一项「禁用」之后,**所有业务页的下拉里它还在**。
 *   那个开关是死的 —— 而且规范早就承诺了它该生效。
 *
 *   ⚠️ **过滤只发生在 `options` 上**:`getLabel` / `getColor` / `raw` 保留禁用项,
 *     因为历史数据里已经存了这个 value,表格要照常显示中文标签。
 *
 * ### ② 拉失败被吞成「永远在加载」(P1)
 *
 *   旧实现 `fetchDict(...).catch(() => undefined)` + `loading: !dicts`
 *   ⇒ 下拉永远转圈,分不清「没数据」还是「拉挂了」
 *   (和 M106 在 `CaptchaField` 上踩的是同一个坑:失败被吞成"还在加载")。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `options` 的 `.filter(...)` 去掉 → ① 的两条立刻变红。
 *   · 把 `.catch(() => { if (!cancelled) setFailed(true) })` 去掉 → ② 那条变红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { dictApi } from '@/api/config'
import { useDictStore } from '@/stores/dict'
import { useDict } from '@/hooks/useDict'

vi.mock('@/api/config', () => ({
  dictApi: { byGroup: vi.fn() },
}))

const byGroupMock = vi.mocked(dictApi.byGroup)

/** 造一个字典项(`is_active` 是 **boolean**,wire 上就是 Go bool 的 true/false) */
function item(
  value: string,
  label: string,
  isActive: boolean,
  description = '',
): Awaited<ReturnType<typeof dictApi.byGroup>>[number] {
  return {
    id: value,
    group_code: 'common_status',
    item_key: value,
    item_value: value,
    label,
    sort: 1,
    is_active: isActive,
    description,
  }
}

describe('useDict · is_active 过滤(M107)', () => {
  beforeEach(() => {
    useDictStore.setState({ dicts: {}, lastFetchAt: {} })
    byGroupMock.mockReset()
  })

  it('禁用的项不进 options —— 这就是「临时下线」的实现', async () => {
    byGroupMock.mockResolvedValue([item('open', '待处理', true), item('closed', '已关闭', false)])

    const { result } = renderHook(() => useDict('common_status'))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.options.map((o) => o.value)).toEqual(['open'])
  })

  it('禁用的项仍然查得到标签(历史数据照常显示中文)', async () => {
    byGroupMock.mockResolvedValue([item('open', '待处理', true), item('closed', '已关闭', false)])

    const { result } = renderHook(() => useDict('common_status'))

    await waitFor(() => expect(result.current.loading).toBe(false))
    // ⚠️ 关键:标签仍然查得到 —— 否则历史记录会变成一串裸 value("closed")
    expect(result.current.getLabel('closed')).toBe('已关闭')
    expect(result.current.getLabel('open')).toBe('待处理')
  })

  it('禁用的项仍然保留颜色(Tag 不能掉色)', async () => {
    byGroupMock.mockResolvedValue([item('closed', '已关闭', false, 'color:red')])

    const { result } = renderHook(() => useDict('common_status'))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.getColor('closed')).toBe('red')
  })

  it('raw 保留禁用项(管理/排查场景要能看到全量)', async () => {
    byGroupMock.mockResolvedValue([item('open', '待处理', true), item('closed', '已关闭', false)])

    const { result } = renderHook(() => useDict('common_status'))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.raw).toHaveLength(2)
  })

  it('字段缺失(undefined)时**保留**该项 —— 宁可多显示一个,也不清空整组', async () => {
    byGroupMock.mockResolvedValue([
      { ...item('open', '待处理', true), is_active: undefined } as never,
    ])

    const { result } = renderHook(() => useDict('common_status'))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.options.map((o) => o.value)).toEqual(['open'])
  })
})

describe('useDict · 失败态(M107)', () => {
  beforeEach(() => {
    useDictStore.setState({ dicts: {}, lastFetchAt: {} })
    byGroupMock.mockReset()
  })

  it('拉失败 → 不再永远 loading,error 变 true', async () => {
    byGroupMock.mockRejectedValue(new Error('boom'))

    const { result } = renderHook(() => useDict('common_status'))

    await waitFor(() => expect(result.current.error).toBe(true))
    expect(result.current.loading).toBe(false)
    expect(result.current.options).toEqual([])
  })

  it('拉成功 → loading 变 false,error 为 false', async () => {
    byGroupMock.mockResolvedValue([item('open', '待处理', true)])

    const { result } = renderHook(() => useDict('common_status'))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe(false)
    expect(result.current.options).toHaveLength(1)
  })
})
