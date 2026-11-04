/**
 * useTableEdit 状态机用例(§2.8 #16)
 *
 * 重点是文件头列的那些边角:保存竞态(取消/换行后迟到的成功不许收摊)、
 * 保存中重复点、失败保草稿、行消失自愈、patch 只含真改过的字段。
 * 可逆探针两条(拆竞态守卫 / 拆快照语义 ⇒ 各有红)。
 */
import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { diffPatch, useTableEdit } from '../useTableEdit'

type Row = { id: string; name: string; remark: string }

const rows: Row[] = [
  { id: '1', name: '甲', remark: 'r1' },
  { id: '2', name: '乙', remark: 'r2' },
]

describe('diffPatch', () => {
  it('只含真改过的字段;Object.is 口径', () => {
    const base = { a: 1, b: 'x', c: NaN, d: undefined }
    // NaN 与 NaN:Object.is 视为相等(不该进 patch);±0 视为不等
    expect(diffPatch(base, { a: 1, b: 'y', c: NaN, d: undefined })).toEqual({ b: 'y' })
    expect(diffPatch(base, { a: -0 })).toEqual({ a: -0 })
  })
})

describe('useTableEdit', () => {
  it('start → setField → getDraft 合并视图;getPatch 只含改动', () => {
    const { result } = renderHook(() =>
      useTableEdit<Row>({ data: rows, onSave: vi.fn().mockResolvedValue(undefined) }),
    )
    act(() => result.current.startEdit(rows[0]!))
    expect(result.current.isEditing(rows[0]!)).toBe(true)
    expect(result.current.editingKey).toBe('1')
    act(() => result.current.setField('remark', '改了'))
    // 草稿 = 快照 + 改动;没改的字段保持原值
    expect(result.current.getDraft(rows[0]!)).toEqual({ id: '1', name: '甲', remark: '改了' })
    // 非编辑行原样返回(不是引用相等也行,但值必须相等)
    expect(result.current.getDraft(rows[1]!)).toEqual(rows[1]!)
    expect(result.current.getPatch(rows[0]!)).toEqual({ remark: '改了' })
    expect(result.current.isDirty).toBe(true)
  })

  it('草稿是快照:dataSource 后来变了,编辑中的草稿不被冲掉', () => {
    const { result, rerender } = renderHook(
      ({ data }: { data: Row[] }) =>
        useTableEdit<Row>({ data, onSave: vi.fn().mockResolvedValue(undefined) }),
      { initialProps: { data: rows } },
    )
    act(() => result.current.startEdit(rows[0]!))
    act(() => result.current.setField('remark', '手输的'))
    // 轮询/SSE 重拉:同 id 的行 remark 被别处改了
    const refreshed = [{ ...rows[0]!, remark: '别处改的' }, rows[1]!]
    rerender({ data: refreshed })
    expect(result.current.getDraft(refreshed[0]!).remark).toBe('手输的')
  })

  it('保存成功:收摊;patch 只含改动字段', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useTableEdit<Row>({ data: rows, onSave }))
    act(() => result.current.startEdit(rows[0]!))
    act(() => result.current.setField('name', '丙'))
    await act(async () => {
      expect(await result.current.saveEdit()).toBe(true)
    })
    expect(onSave).toHaveBeenCalledTimes(1)
    // 落库拿到:快照行 + 只改过的字段(整行覆盖会顶回别人的改动,见文件头 #2)
    expect(onSave.mock.calls[0]![0]).toEqual(rows[0]!)
    expect(onSave.mock.calls[0]![1]).toEqual({ name: '丙' })
    expect(result.current.editingKey).toBeNull()
    expect(result.current.isEditing(rows[0]!)).toBe(false)
  })

  it('保存失败:抛原异常,编辑态与草稿原样保留(可直接改完再存)', async () => {
    const boom = new Error('network down')
    const onSave = vi.fn().mockRejectedValue(boom)
    const { result } = renderHook(() => useTableEdit<Row>({ data: rows, onSave }))
    act(() => result.current.startEdit(rows[0]!))
    act(() => result.current.setField('remark', '没存上的'))
    await expect(result.current.saveEdit()).rejects.toBe(boom)
    expect(result.current.isEditing(rows[0]!)).toBe(true)
    expect(result.current.getDraft(rows[0]!).remark).toBe('没存上的')
    expect(result.current.saving).toBe(false)
    // 第二次保存能正常走(不是卡死在 saving)
    onSave.mockResolvedValueOnce(undefined)
    await act(async () => {
      expect(await result.current.saveEdit()).toBe(true)
    })
  })

  it('保存中重复点 saveEdit ⇒ 安静拒绝(false),onSave 不重复调', async () => {
    let release!: () => void
    const gate = new Promise<void>((r) => (release = r))
    const onSave = vi.fn().mockReturnValue(gate)
    const { result } = renderHook(() => useTableEdit<Row>({ data: rows, onSave }))
    act(() => result.current.startEdit(rows[0]!))
    let p!: Promise<boolean>
    act(() => {
      p = result.current.saveEdit()
    })
    expect(result.current.saving).toBe(true)
    await act(async () => {
      expect(await result.current.saveEdit()).toBe(false)
    })
    release()
    await act(async () => {
      await p
    })
    expect(onSave).toHaveBeenCalledTimes(1)
  })

  it('🔴 保存竞态:保存飞着时取消,迟到的成功不许把编辑状态"复活式"清掉', async () => {
    let release!: () => void
    const gate = new Promise<void>((r) => (release = r))
    const onSave = vi.fn().mockReturnValue(gate)
    const { result } = renderHook(() => useTableEdit<Row>({ data: rows, onSave }))
    act(() => result.current.startEdit(rows[0]!))
    let p!: Promise<boolean>
    act(() => {
      p = result.current.saveEdit()
    })
    // 用户取消,然后开始编辑另一行
    act(() => result.current.cancelEdit())
    act(() => result.current.startEdit(rows[1]!))
    const keyBefore = result.current.editingKey
    release()
    await act(async () => {
      await p
    })
    // 迟到的成功不许把第 2 行的编辑态关掉
    expect(result.current.editingKey).toBe(keyBefore)
    expect(result.current.editingKey).toBe('2')
  })

  it('🔴 可逆探针:保存飞着时换行,迟到的失败不许冲掉新行的编辑态', async () => {
    // 这一发对着"失败路径只收 saving、不动编辑态"那条契约 ——
    // 若实现里失败也去清 editingKey(无代际守卫),本用例必红。
    let reject!: (e: unknown) => void
    const gate = new Promise<void>((_, rej) => (reject = rej))
    const onSave = vi.fn().mockReturnValue(gate)
    const { result } = renderHook(() => useTableEdit<Row>({ data: rows, onSave }))
    act(() => result.current.startEdit(rows[0]!))
    let p!: Promise<boolean>
    act(() => {
      p = result.current.saveEdit()
    })
    act(() => result.current.startEdit(rows[1]!)) // 换行(旧草稿丢弃)
    const boom = new Error('late failure')
    act(() => reject(boom))
    // ⚠️ act 包住等 rejection:onRejected 里的 setState 是异步刷新的,
    //    裸 await 会在 React 处理前读 editingKey —— 探针打不中(knows: race)
    await act(async () => {
      await expect(p).rejects.toBe(boom)
    })
    expect(result.current.editingKey).toBe('2')
    expect(result.current.isEditing(rows[1]!)).toBe(true)
  })

  it('编辑中的行从 data 里消失 ⇒ 自动收摊', async () => {
    const { result, rerender } = renderHook(
      ({ data }: { data: Row[] }) =>
        useTableEdit<Row>({ data, onSave: vi.fn().mockResolvedValue(undefined) }),
      { initialProps: { data: rows } },
    )
    act(() => result.current.startEdit(rows[0]!))
    rerender({ data: [rows[1]!] }) // 行被删/被过滤
    await waitFor(() => expect(result.current.editingKey).toBeNull())
  })

  it('onBeforeEdit 返回 false ⇒ startEdit 是 no-op', () => {
    const { result } = renderHook(() =>
      useTableEdit<Row>({
        data: rows,
        onSave: vi.fn().mockResolvedValue(undefined),
        onBeforeEdit: () => false,
      }),
    )
    act(() => result.current.startEdit(rows[0]!))
    expect(result.current.editingKey).toBeNull()
  })

  it('cancel 丢弃草稿;isDirty 回落 false', () => {
    const { result } = renderHook(() =>
      useTableEdit<Row>({ data: rows, onSave: vi.fn().mockResolvedValue(undefined) }),
    )
    act(() => result.current.startEdit(rows[0]!))
    act(() => result.current.setField('remark', 'x'))
    expect(result.current.isDirty).toBe(true)
    act(() => result.current.cancelEdit())
    expect(result.current.editingKey).toBeNull()
    expect(result.current.isDirty).toBe(false)
    expect(result.current.getDraft(rows[0]!)).toEqual(rows[0]!)
  })

  it('无行编辑时 saveEdit ⇒ false', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useTableEdit<Row>({ data: rows, onSave }))
    await act(async () => {
      expect(await result.current.saveEdit()).toBe(false)
    })
  })

  it('id 统一 string 口径(19 位 uint64 不转 number);没传 data 时不做消失自愈', () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useTableEdit<Row>({ onSave }))
    act(() => result.current.startEdit({ id: '94994193092507334', name: '大', remark: '' }))
    expect(result.current.editingKey).toBe('94994193092507334')
  })
})
