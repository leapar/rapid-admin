/**
 * useSavedViews 用例(§2.8 #21)
 *
 * 三个层次:
 *   1. sanitizeViews 纯函数 —— 坏元素过滤 / 根不是数组 / 已知字段重建
 *   2. hook 状态机(renderHook)—— save/apply/remove/rename 正常路径 +
 *      同名覆盖语义 + activeId 跟随与清空 + 挤出策略
 *   3. 健壮性(本 hook 的价值)—— JSON 损坏静默重置并删坏数据 /
 *      localStorage 写失败不炸只 warn 一次 / storageKey 变化重读(M111 的坑)
 *
 * 可逆探针(见文件末尾注释与报告):
 *   A. 拆掉 sanitizeViews 的坏元素过滤(数组元素原样放行)⇒
 *      「过滤缺字段坏元素」「清洗后不携带未知字段」红
 */
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { sanitizeViews, useSavedViews } from '../useSavedViews'

const KEY = 'sv-test'

/** 只看 id 序(断言顺序用,避免整对象比较的噪音) */
const idsOf = (views: { id: string }[]) => views.map((v) => v.id)

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('sanitizeViews 纯函数', () => {
  it('根不是数组 → []', () => {
    expect(sanitizeViews(null)).toEqual([])
    expect(sanitizeViews({ id: 'a' })).toEqual([])
    expect(sanitizeViews('["x"]')).toEqual([])
  })

  it('过滤缺字段 / 类型不对的坏元素', () => {
    const good = { id: 'a', name: '甲', state: { f: 1 }, createdAt: 100 }
    const raw = [
      good,
      { name: '没id', state: {}, createdAt: 1 },
      { id: 'b', state: {}, createdAt: 1 }, // 没 name
      { id: '', name: '空id', state: {}, createdAt: 1 }, // 空 id
      { id: 'c', name: '没state', createdAt: 1 },
      { id: 'd', name: 'createdAt不是数', state: {} },
      { id: 'e', name: 'createdAt是NaN', state: {}, createdAt: Number.NaN },
      '字符串元素',
      null,
      42,
    ]
    expect(sanitizeViews(raw)).toEqual([good])
  })

  it('只重建已知字段(存储里的未知字段不往内存带)', () => {
    const raw = [{ id: 'a', name: '甲', state: 1, createdAt: 5, legacy: '老版本残留' }]
    expect(sanitizeViews(raw)).toEqual([{ id: 'a', name: '甲', state: 1, createdAt: 5 }])
  })
})

describe('useSavedViews 正常路径', () => {
  it('空存储:views 空、不报错、activeId null', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    expect(result.current.views).toEqual([])
    expect(result.current.activeId).toBeNull()
  })

  it('save → 列表出现且 createdAt 降序;返回值即存入的视图', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    let a: { id: string } | undefined
    let b: { id: string } | undefined
    act(() => {
      a = result.current.save('甲', { f: 1 })
    })
    act(() => {
      b = result.current.save('乙', { f: 2 })
    })
    expect(result.current.views.map((v) => v.name)).toEqual(['乙', '甲']) // 新的在前
    expect(b!.id).not.toBe(a!.id) // id 稳定唯一:两次生成不同
    // 落盘可读回
    expect(JSON.parse(localStorage.getItem('savedview:' + KEY)!)).toHaveLength(2)
  })

  it('apply:命中返回 state 且 activeId 跟随;不存在的 id 返 undefined 且 activeId 不动', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    let saved!: { id: string }
    act(() => {
      saved = result.current.save('甲', { f: 7 })
    })
    act(() => {
      expect(result.current.apply('no-such-id')).toBeUndefined()
    })
    expect(result.current.activeId).toBe(saved.id) // 未命中不动 activeId
    let state: { f: number } | undefined
    act(() => {
      state = result.current.apply(saved.id)
    })
    expect(state).toEqual({ f: 7 })
    expect(result.current.activeId).toBe(saved.id)
  })

  it('remove:删掉视图;删的是生效视图 → activeId 自动清 null', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    let a!: { id: string }
    let b!: { id: string }
    act(() => {
      a = result.current.save('甲', { f: 1 })
    })
    act(() => {
      b = result.current.save('乙', { f: 2 })
    })
    act(() => {
      result.current.apply(a.id)
    })
    act(() => {
      result.current.remove(b.id) // 删非生效视图
    })
    expect(result.current.views.map((v) => v.id)).toEqual([a.id])
    expect(result.current.activeId).toBe(a.id)
    act(() => {
      result.current.remove(a.id) // 删生效视图
    })
    expect(result.current.views).toEqual([])
    expect(result.current.activeId).toBeNull()
  })

  it('remove 不存在的 id:no-op 不落盘', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    act(() => {
      result.current.save('甲', { f: 1 })
    })
    const before = localStorage.getItem('savedview:' + KEY)
    act(() => {
      result.current.remove('no-such-id')
    })
    expect(result.current.views).toHaveLength(1)
    expect(localStorage.getItem('savedview:' + KEY)).toBe(before)
  })

  it('rename:改名字,内容与 id 不变', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    let saved!: { id: string; createdAt: number }
    act(() => {
      saved = result.current.save('旧名', { f: 1 })
    })
    act(() => {
      result.current.rename(saved.id, '新名')
    })
    expect(result.current.views[0]!.name).toBe('新名')
    expect(result.current.views[0]!.id).toBe(saved.id)
    expect(result.current.views[0]!.createdAt).toBe(saved.createdAt)
    expect(result.current.views[0]!.state).toEqual({ f: 1 })
  })

  it('clearActive:退出已选视图态', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    let saved!: { id: string }
    act(() => {
      saved = result.current.save('甲', { f: 1 })
    })
    expect(result.current.activeId).toBe(saved.id) // save 即生效
    act(() => {
      result.current.clearActive()
    })
    expect(result.current.activeId).toBeNull()
    expect(result.current.views).toHaveLength(1) // 视图本身不删
  })

  it('state 原样存取,hook 不窥探内容(嵌套对象往返一致)', () => {
    const { result } = renderHook(() => useSavedViews<Record<string, unknown>>(KEY))
    const state = {
      filters: { status: 'open', q: '甲' },
      sorter: { field: 'name', order: 'descend' },
      columns: [{ key: 'name', visible: true, width: 120 }],
    }
    let saved!: { id: string }
    act(() => {
      saved = result.current.save('全量', state)
    })
    let got: unknown
    act(() => {
      got = result.current.apply(saved.id)
    })
    expect(got).toEqual(state)
  })
})

describe('useSavedViews 同名覆盖', () => {
  it('同名 → 覆盖内容,保留原 id 与 createdAt,不产生重复条目', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    let first!: { id: string; createdAt: number }
    act(() => {
      first = result.current.save('甲', { f: 1 })
    })
    let second!: { id: string; createdAt: number }
    act(() => {
      second = result.current.save('甲', { f: 99 })
    })
    expect(second.id).toBe(first.id) // id 不变
    expect(second.createdAt).toBe(first.createdAt) // createdAt = 首次创建时间
    expect(result.current.views).toHaveLength(1) // 不重复
    expect(result.current.views[0]!.state).toEqual({ f: 99 }) // 内容已覆盖
    // 落盘同步覆盖,无重复
    const stored = JSON.parse(localStorage.getItem('savedview:' + KEY)!)
    expect(stored).toHaveLength(1)
    expect(stored[0].state).toEqual({ f: 99 })
  })

  it('覆盖后仍按 createdAt 排序(不会跳到最前——虽然它本来就是最新的)', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    let a!: { id: string }
    act(() => {
      a = result.current.save('甲', { f: 1 })
    })
    let b!: { id: string }
    act(() => {
      b = result.current.save('乙', { f: 2 })
    })
    act(() => {
      result.current.save('甲', { f: 3 }) // 覆盖最旧的
    })
    // createdAt 降序:乙(新)仍在前
    expect(result.current.views.map((v) => v.id)).toEqual([b.id, a.id])
  })
})

describe('useSavedViews 挤出策略', () => {
  it('超过 max → 挤掉 createdAt 最旧的,刚 save 的这条保留', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY, { max: 3 }))
    const saved: { id: string }[] = []
    for (let i = 0; i < 4; i++) {
      act(() => {
        saved.push(result.current.save(`视图${i}`, { f: i }))
      })
    }
    expect(result.current.views).toHaveLength(3)
    expect(idsOf(result.current.views)).not.toContain(saved[0]!.id) // 最旧的被挤掉
    expect(idsOf(result.current.views)).toContain(saved[3]!.id) // 刚存的保留
    // 落盘同步
    expect(JSON.parse(localStorage.getItem('savedview:' + KEY)!)).toHaveLength(3)
  })

  it('自定义 max=1:永远只剩最新一条;同名覆盖不会把自己挤掉', () => {
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY, { max: 1 }))
    act(() => {
      result.current.save('甲', { f: 1 })
    })
    act(() => {
      result.current.save('乙', { f: 2 })
    })
    expect(result.current.views.map((v) => v.name)).toEqual(['乙'])
    act(() => {
      result.current.save('乙', { f: 3 }) // 同名覆盖:max=1 也不能挤掉自己
    })
    expect(result.current.views).toHaveLength(1)
    expect(result.current.views[0]!.state).toEqual({ f: 3 })
  })
})

describe('useSavedViews 健壮性(脏数据)', () => {
  it('JSON 损坏 → 静默重置为空,且删掉坏数据(下次不再踩)', () => {
    localStorage.setItem('savedview:' + KEY, '{这 不是 json')
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    expect(result.current.views).toEqual([])
    expect(localStorage.getItem('savedview:' + KEY)).toBeNull() // 坏数据已删
    expect(warnSpy).not.toHaveBeenCalled() // 静默:不是错误
  })

  it('存储里不是数组 → 当空并删掉坏数据', () => {
    localStorage.setItem('savedview:' + KEY, '{"id":"a"}')
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    expect(result.current.views).toEqual([])
    expect(localStorage.getItem('savedview:' + KEY)).toBeNull()
  })

  it('数组里混着坏元素 → 只留好的(旧数据可部分存活)', () => {
    const good = { id: 'a', name: '甲', state: { f: 1 }, createdAt: 200 }
    localStorage.setItem(
      'savedview:' + KEY,
      JSON.stringify([good, { id: 'bad' }, '垃圾', { id: 'b', name: '缺state', createdAt: 1 }]),
    )
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    expect(result.current.views).toEqual([good])
  })

  it('正常数据读入后按 createdAt 降序', () => {
    localStorage.setItem(
      'savedview:' + KEY,
      JSON.stringify([
        { id: 'old', name: '旧', state: {}, createdAt: 100 },
        { id: 'new', name: '新', state: {}, createdAt: 300 },
        { id: 'mid', name: '中', state: {}, createdAt: 200 },
      ]),
    )
    const { result } = renderHook(() => useSavedViews(KEY))
    expect(idsOf(result.current.views)).toEqual(['new', 'mid', 'old'])
  })
})

describe('useSavedViews 健壮性(写失败 / 换 key)', () => {
  it('localStorage 写失败(配额/隐私模式)→ 不炸,内存态仍可用,只 warn 一次', () => {
    const setItemSpy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('quota exceeded', 'QuotaExceededError')
    })
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { result } = renderHook(() => useSavedViews<{ f: number }>(KEY))
    let saved!: { id: string }
    act(() => {
      saved = result.current.save('甲', { f: 1 }) // 不抛
    })
    expect(result.current.views).toHaveLength(1) // 内存态可用
    expect(result.current.views[0]!.state).toEqual({ f: 1 })
    expect(result.current.activeId).toBe(saved.id)
    act(() => {
      result.current.save('乙', { f: 2 })
      result.current.remove(saved.id) // 后续写操作继续不炸
    })
    expect(result.current.views).toHaveLength(1)
    expect(warnSpy).toHaveBeenCalledTimes(1) // 只 warn 一次,不刷屏
    expect(setItemSpy).toHaveBeenCalled()
  })

  it('storageKey 变化 → 重新读新 key 的数据(M111 的坑),activeId 清空', () => {
    localStorage.setItem(
      'savedview:key-b',
      JSON.stringify([{ id: 'b1', name: '乙页视图', state: { f: 2 }, createdAt: 1 }]),
    )
    const { result, rerender } = renderHook(({ k }: { k: string }) => useSavedViews<{ f: number }>(k), {
      initialProps: { k: 'key-a' },
    })
    let saved!: { id: string }
    act(() => {
      saved = result.current.save('甲页视图', { f: 1 })
    })
    expect(result.current.views.map((v) => v.name)).toEqual(['甲页视图'])
    expect(result.current.activeId).toBe(saved.id)

    rerender({ k: 'key-b' })
    // ⚠️ 换 key 必须显示新 key 的数据,不能还是旧 key 的
    expect(result.current.views.map((v) => v.name)).toEqual(['乙页视图'])
    expect(result.current.activeId).toBeNull() // activeId 属于旧 key,清空

    // 新 key 上的写操作落到新 key 的存储;旧 key 数据不受影响
    act(() => {
      result.current.save('乙页新存', { f: 9 })
    })
    expect(result.current.views).toHaveLength(2)
    expect(JSON.parse(localStorage.getItem('savedview:key-b')!)).toHaveLength(2)
    expect(JSON.parse(localStorage.getItem('savedview:key-a')!)).toHaveLength(1)

    rerender({ k: 'key-a' })
    expect(result.current.views.map((v) => v.name)).toEqual(['甲页视图'])
  })
})

/*
 * 【可逆探针记录】(跑完即恢复,不留死码)
 *
 *   探针 A:把 sanitizeViews 的坏元素过滤临时改坏 —— 循环体里 `continue`
 *   的守卫全部拆掉,元素原样 `out.push(item as SavedView<T>)`:
 *     ⇒ 「过滤缺字段 / 类型不对的坏元素」红(期望 [good] 实得 10 个元素)
 *     ⇒ 「只重建已知字段」红(legacy 字段没被剥掉)
 *     ⇒ 「数组里混着坏元素 → 只留好的」红(坏元素混进 views)
 *   恢复后三条全绿。
 *
 *   (写坏实现 → npx vitest run 该文件 → 记录红条目 → git restore 式恢复)
 */
