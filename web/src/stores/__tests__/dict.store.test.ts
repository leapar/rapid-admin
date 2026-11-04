/**
 * 字典 Store —— 缓存作废语义(2026-10-26 M94)
 *
 * ⚠️ 这里守的是「字典改了马上生效」这条链路的**前半段**:
 *   `invalidate` 之后,下一次取这个组必须**真的重新请求**。
 *
 * ⚠️ 守的坑很具体:`invalidate` 的实现依赖两处实现细节 ——
 *   ① `useDict` 的 effect 条件 `if (!dicts || !lastFetchAt) fetchDict(...)`;
 *   ② `fetchDict` 的 TTL 判断 `Date.now() - (lastFetchAt[groupCode] || 0) < CACHE_TTL`。
 *   这两处任何一处被改,「作废」都会退化成「**静默不生效**」——
 *   症状是字典改了但下拉还是旧值,而且**没有任何报错**,极难归因。
 *   所以这里不只断言 store 内部状态,还直接断言**真的会发请求**。
 *
 * ⚠️ 另一半(「事件真的接到了 invalidate」)在
 *   `web/src/hooks/__tests__/useEventStream.dict.test.tsx` ——
 *   那边守的是 SSE 接线,这边守的是 store 语义,两边都要红才算数。
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'

vi.mock('@/api/config', () => ({
  dictApi: { byGroup: vi.fn() },
}))

import { dictApi } from '@/api/config'
import { useDictStore } from '@/stores/dict'

const byGroupMock = vi.mocked(dictApi.byGroup)

/** 造一个字典项 */
function item(label: string, group = 'common_status') {
  return {
    id: '1',
    group_code: group,
    item_key: 'open',
    item_value: 'open',
    label,
    sort: 1,
    // ⚠️ 2026-10-28 M107:`is_active` 是 **boolean**(wire 上就是 Go bool 的 true/false),
    //   以前这里写的是 `1` —— 那是**类型撒谎**的同伙,已随 `DictItem.is_active` 一起改正。
    is_active: true,
    description: '',
  }
}

describe('stores/dict · 缓存作废(M94)', () => {
  beforeEach(() => {
    useDictStore.setState({ dicts: {}, lastFetchAt: {} })
    byGroupMock.mockReset()
    byGroupMock.mockResolvedValue([item('待处理')])
  })

  it('没作废时,30 分钟内再取同一个组走缓存(不发请求)', async () => {
    await useDictStore.getState().fetchDict('common_status')
    expect(byGroupMock).toHaveBeenCalledTimes(1)

    await useDictStore.getState().fetchDict('common_status')
    expect(byGroupMock).toHaveBeenCalledTimes(1)
  })

  it('invalidate 之后,同一个组会重新请求(而不是继续吃缓存)', async () => {
    await useDictStore.getState().fetchDict('common_status')
    expect(byGroupMock).toHaveBeenCalledTimes(1)

    useDictStore.getState().invalidate('common_status')

    await useDictStore.getState().fetchDict('common_status')
    expect(byGroupMock).toHaveBeenCalledTimes(2)
  })

  it('invalidate 之后拿到的是新值(重拉会覆盖旧值)', async () => {
    await useDictStore.getState().fetchDict('common_status')
    expect(useDictStore.getState().getLabel('common_status', 'open')).toBe('待处理')

    byGroupMock.mockResolvedValue([item('已受理')])
    useDictStore.getState().invalidate('common_status')
    await useDictStore.getState().fetchDict('common_status')

    expect(useDictStore.getState().getLabel('common_status', 'open')).toBe('已受理')
  })

  it('invalidate **保留**旧数据(不把下拉清空)', () => {
    // 先塞一条缓存
    useDictStore.setState({
      dicts: { common_status: [item('待处理')] },
      lastFetchAt: { common_status: Date.now() },
    })

    useDictStore.getState().invalidate('common_status')

    // ⚠️ 旧数据还在 —— 删掉它的话,所有用这个组的下拉会**瞬间变空**,
    //   字典是全局的,一个组被改 → 几十个页面的下拉同时闪一下。
    //   保留旧值,重拉回来之前用户看到的是旧标签(可接受),落地后无缝切换。
    expect(useDictStore.getState().dicts.common_status).toHaveLength(1)
    // 但"新鲜度标记"没了 —— 它是触发重拉的开关
    expect(useDictStore.getState().lastFetchAt.common_status).toBeUndefined()
  })

  it('invalidate 只影响被指名的那个组', async () => {
    await useDictStore.getState().fetchDict('common_status')
    await useDictStore.getState().fetchDict('role_type')
    const callsBefore = byGroupMock.mock.calls.length

    useDictStore.getState().invalidate('common_status')

    expect(useDictStore.getState().lastFetchAt.role_type).toBeDefined()
    expect(useDictStore.getState().lastFetchAt.common_status).toBeUndefined()

    // role_type 仍然走缓存,不发请求
    await useDictStore.getState().fetchDict('role_type')
    expect(byGroupMock).toHaveBeenCalledTimes(callsBefore)
  })

  it('invalidate 一个没人用过的组是安全的空操作', () => {
    useDictStore.getState().invalidate('never_used_group')
    expect(Object.keys(useDictStore.getState().lastFetchAt)).toHaveLength(0)
    expect(Object.keys(useDictStore.getState().dicts)).toHaveLength(0)
  })

  it('invalidate 空串是空操作(不能把整个缓存清掉)', async () => {
    await useDictStore.getState().fetchDict('common_status')
    useDictStore.getState().invalidate('')
    expect(useDictStore.getState().lastFetchAt.common_status).toBeDefined()
    expect(useDictStore.getState().dicts.common_status).toHaveLength(1)
  })

  it('invalidate 重复调用是幂等的(不会把别的组也带下水)', async () => {
    await useDictStore.getState().fetchDict('common_status')
    useDictStore.getState().invalidate('common_status')
    useDictStore.getState().invalidate('common_status')
    useDictStore.getState().invalidate('common_status')

    expect(useDictStore.getState().lastFetchAt.common_status).toBeUndefined()
    expect(useDictStore.getState().dicts.common_status).toHaveLength(1)
  })
})
