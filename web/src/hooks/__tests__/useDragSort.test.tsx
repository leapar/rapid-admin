/**
 * useDragSort 用例(§2.8 #17)
 *
 * 三个层次:
 *   1. reorder 纯函数 —— 摘后插语义 / 越界 clamp / 不改原数组
 *   2. 状态机(renderHook)—— 拖起锁定 / 预览与提交分离 / cancel 还原 /
 *      受控 vs 非受控 / drop-后-dragend 不重入
 *   3. DOM 集成(React 合成事件路径)—— 手动 dispatchEvent 走完整
 *      dragstart → dragover → drop → dragend 流(⚠️ jsdom 不实现 DnD
 *      默认行为,但事件派发是通的;dataTransfer 要自己挂在事件上)
 *
 * 可逆探针两条(见文件末尾注释与报告):
 *   A. 拆掉放置时的索引计算(drop 提交 origin 而不是 current)⇒ 「完整流」红
 *   B. 拆掉"拖起锁定工作副本"(dragOver 对着 origin 算索引)⇒ 「连续经过」红
 */
import { act, fireEvent, render, renderHook, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useState } from 'react'

import { reorder, useDragSort } from '../useDragSort'

type Row = { id: string; name: string }

const rows: Row[] = [
  { id: '1', name: '甲' },
  { id: '2', name: '乙' },
  { id: '3', name: '丙' },
]

/** 手动派发 DnD 事件 —— jsdom 没有 DragEvent/DataTransfer,自己拼 */
function fireDrag(el: Element, type: string) {
  const e = new Event(type, { bubbles: true, cancelable: true })
  // 挂一个假 dataTransfer(React 合成事件会把它透传给 handler)
  Object.assign(e, {
    dataTransfer: { setData: vi.fn(), effectAllowed: null, dropEffect: null },
  })
  el.dispatchEvent(e)
}

/** 从 DOM 里读当前展示序 */
function orderOf(list: HTMLElement): string[] {
  return Array.from(list.children).map((li) => li.getAttribute('data-testid')!.replace('row-', ''))
}

describe('reorder 纯函数', () => {
  it('向下移动:摘后插语义(0→2 落到 2)', () => {
    expect(reorder(['a', 'b', 'c'], 0, 2)).toEqual(['b', 'c', 'a'])
    expect(reorder(['a', 'b', 'c'], 0, 1)).toEqual(['b', 'a', 'c'])
  })

  it('向上移动(2→0)', () => {
    expect(reorder(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b'])
  })

  it('from === to 返回原引用(no-op);越界 clamp;单元素 no-op', () => {
    const list = ['a', 'b', 'c']
    expect(reorder(list, 1, 1)).toBe(list)
    // 越界 clamp 到 [0, n-1] 后 2===2
    expect(reorder(list, 5, 99)).toBe(list)
    expect(reorder(['x'], 0, 5)).toEqual(['x'])
  })

  it('不改传入的数组', () => {
    const list = ['a', 'b', 'c']
    reorder(list, 0, 2)
    expect(list).toEqual(['a', 'b', 'c'])
  })
})

describe('useDragSort 状态机', () => {
  it('非受控:initialItems 展示;startDrag 锁定被拖行(draggingKey / isDragging)', () => {
    const { result } = renderHook(() =>
      useDragSort<Row>({ initialItems: rows, rowKey: 'id', onSort: vi.fn() }),
    )
    expect(result.current.items.map((r) => r.id)).toEqual(['1', '2', '3'])
    expect(result.current.dragging).toBe(false)
    act(() => result.current.startDrag(rows[2]!))
    expect(result.current.draggingKey).toBe('3')
    expect(result.current.dragging).toBe(true)
    expect(result.current.isDragging(rows[2]!)).toBe(true)
    expect(result.current.isDragging(rows[0]!)).toBe(false)
  })

  it('dragOver 预览重排(真身在 drop 前不动)+ overKey 记录', () => {
    const onSort = vi.fn()
    const { result } = renderHook(() =>
      useDragSort<Row>({ initialItems: rows, rowKey: 'id', onSort }),
    )
    act(() => result.current.startDrag(rows[2]!))
    act(() => result.current.dragOver(rows[0]!))
    // 预览:丙挪到甲的位置
    expect(result.current.items.map((r) => r.id)).toEqual(['3', '1', '2'])
    expect(result.current.overKey).toBe('1')
    expect(result.current.isOver(rows[0]!)).toBe(true)
    // drop 之前不提交:onSort 一次都不调
    expect(onSort).not.toHaveBeenCalled()
  })

  it('drop:提交预览 + onSort(newItems, from, to)+ 状态收摊', () => {
    const onSort = vi.fn()
    const { result } = renderHook(() =>
      useDragSort<Row>({ initialItems: rows, rowKey: 'id', onSort }),
    )
    act(() => result.current.startDrag(rows[2]!))
    act(() => result.current.dragOver(rows[0]!))
    act(() => result.current.drop())
    expect(onSort).toHaveBeenCalledTimes(1)
    expect(onSort).toHaveBeenCalledWith([rows[2], rows[0], rows[1]], 2, 0)
    // 非受控:预览提交为真身
    expect(result.current.items.map((r) => r.id)).toEqual(['3', '1', '2'])
    expect(result.current.draggingKey).toBeNull()
    expect(result.current.dragging).toBe(false)
    expect(result.current.overKey).toBeNull()
  })

  it('原地放下(没经过别的行)⇒ onSort 不调', () => {
    const onSort = vi.fn()
    const { result } = renderHook(() =>
      useDragSort<Row>({ initialItems: rows, rowKey: 'id', onSort }),
    )
    act(() => result.current.startDrag(rows[0]!))
    act(() => result.current.drop())
    expect(onSort).not.toHaveBeenCalled()
    expect(result.current.draggingKey).toBeNull()
  })

  it('悬停到自己 / 不在列表里的行 ⇒ 不重排', () => {
    const onSort = vi.fn()
    const { result } = renderHook(() =>
      useDragSort<Row>({ initialItems: rows, rowKey: 'id', onSort }),
    )
    act(() => result.current.startDrag(rows[0]!))
    act(() => result.current.dragOver(rows[0]!))
    expect(result.current.items.map((r) => r.id)).toEqual(['1', '2', '3'])
    act(() => result.current.dragOver({ id: 'zz', name: '不在列表' }))
    expect(result.current.items.map((r) => r.id)).toEqual(['1', '2', '3'])
    act(() => result.current.drop())
    expect(onSort).not.toHaveBeenCalled()
  })

  it('cancel:预览丢弃,真身还原,onSort 不调', () => {
    const onSort = vi.fn()
    const { result } = renderHook(() =>
      useDragSort<Row>({ initialItems: rows, rowKey: 'id', onSort }),
    )
    act(() => result.current.startDrag(rows[2]!))
    act(() => result.current.dragOver(rows[0]!))
    expect(result.current.items.map((r) => r.id)).toEqual(['3', '1', '2'])
    act(() => result.current.cancel())
    expect(result.current.items.map((r) => r.id)).toEqual(['1', '2', '3'])
    expect(result.current.draggingKey).toBeNull()
    expect(onSort).not.toHaveBeenCalled()
  })

  it('move:编程式重排(真移动了才调 onSort;clamp 后原地 no-op)', () => {
    const onSort = vi.fn()
    const { result } = renderHook(() =>
      useDragSort<Row>({ initialItems: rows, rowKey: 'id', onSort }),
    )
    act(() => result.current.move(0, 2))
    expect(result.current.items.map((r) => r.id)).toEqual(['2', '3', '1'])
    expect(onSort).toHaveBeenCalledWith([rows[1], rows[2], rows[0]], 0, 2)
    act(() => result.current.move(1, 1))
    expect(onSort).toHaveBeenCalledTimes(1)
    // 越界:两端都 clamp 到 0 ⇒ 原地 no-op(注意 99,-5 会 clamp 成 2,0,是真移动)
    act(() => result.current.move(-5, -1))
    expect(onSort).toHaveBeenCalledTimes(1)
  })

  it('🔴 受控模式:展示跟随 items prop;drop 交回调不内部提交(调用方 rerender 才变)', () => {
    const onSort = vi.fn()
    const { result, rerender } = renderHook(
      ({ items }: { items: Row[] }) => useDragSort<Row>({ items, rowKey: 'id', onSort }),
      { initialProps: { items: rows } },
    )
    act(() => result.current.startDrag(rows[2]!))
    act(() => result.current.dragOver(rows[0]!))
    expect(result.current.items.map((r) => r.id)).toEqual(['3', '1', '2'])
    act(() => result.current.drop())
    expect(onSort).toHaveBeenCalledTimes(1)
    // 受控:hook 不动真身 —— items prop 没变,展示回到旧序(调用方必须更新 items)
    expect(result.current.items).toEqual(rows)
    // 调用方落库后更新 items prop ⇒ 展示跟随
    rerender({ items: [rows[2]!, rows[0]!, rows[1]!] })
    expect(result.current.items.map((r) => r.id)).toEqual(['3', '1', '2'])
  })

  it('canDrag false ⇒ rowProps.draggable false,dragstart 不生效', () => {
    const { result } = renderHook(() =>
      useDragSort<Row>({
        initialItems: rows,
        rowKey: 'id',
        onSort: vi.fn(),
        canDrag: (r) => r.id !== '1',
      }),
    )
    expect(result.current.rowProps(rows[0]!).draggable).toBe(false)
    expect(result.current.rowProps(rows[1]!).draggable).toBe(true)
    act(() => result.current.startDrag(rows[0]!))
    expect(result.current.draggingKey).toBeNull()
  })

  it('startDrag 不在列表里的行 ⇒ no-op;重复 dragstart ⇒ 仍是第一次的拖拽', () => {
    const { result } = renderHook(() =>
      useDragSort<Row>({ initialItems: rows, rowKey: 'id', onSort: vi.fn() }),
    )
    act(() => result.current.startDrag({ id: 'zz', name: '幽灵' }))
    expect(result.current.draggingKey).toBeNull()
    act(() => result.current.startDrag(rows[0]!))
    act(() => result.current.dragOver(rows[2]!))
    // 重复 dragstart(换行):不许抢当前拖拽
    act(() => result.current.startDrag(rows[1]!))
    expect(result.current.draggingKey).toBe('1')
  })

  it('函数 rowKey 的口径也统一成 string(19 位 uint64 不转 number)', () => {
    const big = { id: '94994193092507334', name: '大' }
    const { result } = renderHook(() =>
      useDragSort<{ id: string; name: string }>({
        initialItems: [big],
        rowKey: (r) => r.id,
        onSort: vi.fn(),
      }),
    )
    act(() => result.current.startDrag(big))
    expect(result.current.draggingKey).toBe('94994193092507334')
  })

  it('onSort 同步抛错不吞(异常穿透给事件层);状态机保持收摊后的干净', () => {
    const onSort = vi.fn(() => {
      throw new Error('network down')
    })
    const { result } = renderHook(() =>
      useDragSort<Row>({ initialItems: rows, rowKey: 'id', onSort }),
    )
    act(() => result.current.startDrag(rows[2]!))
    act(() => result.current.dragOver(rows[0]!))
    // 异常原样穿透(不吞);act 里接住,让 React 把 setDrag(null) 刷完再断言
    let caught: unknown
    act(() => {
      try {
        result.current.drop()
      } catch (e) {
        caught = e
      }
    })
    expect((caught as Error).message).toBe('network down')
    expect(result.current.draggingKey).toBeNull()
  })
})

describe('useDragSort DOM 集成(React 合成事件 + 手动 dispatchEvent)', () => {
  function DragList(props: {
    initial: Row[]
    onSort: (rows: Row[], from: number, to: number) => void
  }) {
    const drag = useDragSort<Row>({
      initialItems: props.initial,
      rowKey: 'id',
      onSort: props.onSort,
    })
    return (
      <ul data-testid="list">
        {drag.items.map((r) => (
          <li
            key={r.id}
            data-testid={`row-${r.id}`}
            data-dragging={drag.isDragging(r) ? 'true' : undefined}
            {...drag.rowProps(r)}
          >
            {r.name}
          </li>
        ))}
      </ul>
    )
  }

  it('🔴 完整流:dragstart(丙) → dragover(甲) → drop ⇒ 顺序重排 + onSort 参数 + dragend 不重入', () => {
    const onSort = vi.fn()
    render(<DragList initial={rows} onSort={onSort} />)
    const list = screen.getByTestId('list')
    expect(orderOf(list)).toEqual(['1', '2', '3'])

    act(() => fireDrag(screen.getByTestId('row-3'), 'dragstart'))
    expect(screen.getByTestId('row-3').getAttribute('data-dragging')).toBe('true')
    expect(screen.getByTestId('row-3').getAttribute('draggable')).toBe('true')

    act(() => fireDrag(screen.getByTestId('row-1'), 'dragover'))
    // 预览实时可见(拖到哪,列表让位到哪)
    expect(orderOf(list)).toEqual(['3', '1', '2'])

    act(() => fireDrag(screen.getByTestId('row-1'), 'drop'))
    expect(orderOf(list)).toEqual(['3', '1', '2'])
    expect(onSort).toHaveBeenCalledTimes(1)
    expect(onSort).toHaveBeenCalledWith([rows[2], rows[0], rows[1]], 2, 0)

    // ⚠️ drop 之后浏览器立刻派发 dragend(中间无渲染):不许把已提交的当"未放置"取消
    act(() => fireDrag(screen.getByTestId('row-3'), 'dragend'))
    expect(orderOf(list)).toEqual(['3', '1', '2'])
    expect(onSort).toHaveBeenCalledTimes(1)
  })

  it('dragend 未 drop ⇒ 丢弃预览还原(拖出列表 / Esc 语义)', () => {
    const onSort = vi.fn()
    render(<DragList initial={rows} onSort={onSort} />)
    const list = screen.getByTestId('list')
    act(() => fireDrag(screen.getByTestId('row-3'), 'dragstart'))
    act(() => fireDrag(screen.getByTestId('row-1'), 'dragover'))
    expect(orderOf(list)).toEqual(['3', '1', '2'])
    act(() => fireDrag(screen.getByTestId('row-3'), 'dragend'))
    expect(orderOf(list)).toEqual(['1', '2', '3'])
    expect(onSort).not.toHaveBeenCalled()
  })

  it('🔴 连续经过两个目标:索引对着工作副本算(对着原始列表算必错位)', () => {
    // 拖丙:先经过甲([丙甲乙]),再经过乙 ⇒ 丙落到乙的位置([甲乙丙],
    // 回到原位 ⇒ onSort 不调)。若索引对着 origin 算,第二次会算出 [甲丙乙]。
    const onSort = vi.fn()
    render(<DragList initial={rows} onSort={onSort} />)
    const list = screen.getByTestId('list')
    act(() => fireDrag(screen.getByTestId('row-3'), 'dragstart'))
    act(() => fireDrag(screen.getByTestId('row-1'), 'dragover'))
    expect(orderOf(list)).toEqual(['3', '1', '2'])
    act(() => fireDrag(screen.getByTestId('row-2'), 'dragover'))
    expect(orderOf(list)).toEqual(['1', '2', '3'])
    act(() => fireDrag(screen.getByTestId('row-2'), 'drop'))
    expect(onSort).not.toHaveBeenCalled()
  })

  it('受控列表:drop 回调里 setState 更新 items ⇒ DOM 跟新序', () => {
    let setItems!: (rows: Row[]) => void
    const onSort = vi.fn((rows_: Row[]) => setItems(rows_))
    function ControlledList() {
      const [items, s] = useState(rows)
      setItems = (next) => act(() => s(next))
      const drag = useDragSort<Row>({ items, rowKey: 'id', onSort })
      return (
        <ul data-testid="list">
          {drag.items.map((r) => (
            <li key={r.id} data-testid={`row-${r.id}`} {...drag.rowProps(r)}>
              {r.name}
            </li>
          ))}
        </ul>
      )
    }
    render(<ControlledList />)
    const list = screen.getByTestId('list')
    act(() => fireDrag(screen.getByTestId('row-2'), 'dragstart'))
    act(() => fireDrag(screen.getByTestId('row-1'), 'dragover'))
    act(() => fireDrag(screen.getByTestId('row-1'), 'drop'))
    // onSort → setState(受控真身更新)⇒ DOM 跟进
    expect(onSort).toHaveBeenCalledTimes(1)
    expect(orderOf(list)).toEqual(['2', '1', '3'])
  })

  it('非拖拽行上乱发 drop/dragover ⇒ 不炸、不产生状态', () => {
    const onSort = vi.fn()
    render(<DragList initial={rows} onSort={onSort} />)
    const list = screen.getByTestId('list')
    // 没拖过任何行就 drop / dragover / dragend:全是 no-op
    act(() => fireDrag(screen.getByTestId('row-1'), 'drop'))
    act(() => fireDrag(screen.getByTestId('row-1'), 'dragover'))
    act(() => fireDrag(screen.getByTestId('row-1'), 'dragend'))
    expect(orderOf(list)).toEqual(['1', '2', '3'])
    expect(onSort).not.toHaveBeenCalled()
  })

  it('fireEvent 派发(替代路径)同样驱动状态机', () => {
    const onSort = vi.fn()
    render(<DragList initial={rows} onSort={onSort} />)
    const list = screen.getByTestId('list')
    act(() => fireDrag(screen.getByTestId('row-1'), 'dragstart'))
    // testing-library 的 fireEvent.dragOver 造的是纯 Event,也能走到 handler
    act(() => fireEvent.dragOver(screen.getByTestId('row-3')))
    expect(orderOf(list)).toEqual(['2', '3', '1'])
    act(() => fireDrag(screen.getByTestId('row-3'), 'drop'))
    expect(onSort).toHaveBeenCalledWith([rows[1], rows[2], rows[0]], 0, 2)
  })
})

/*
 * ── 可逆探针记录(实测,均可逆)──────────────────────────────────────
 *
 * 探针 A —— 拆掉「放置时的索引计算」:
 *   drop() 里把 `d.current.findIndex(...)` 换成 `d.origin.findIndex(...)`
 *   (等价于把预览当没发生过,提交回原序)
 *   ⇒ 红:「DOM 集成 · 完整流」(orderOf(list) 得 ['1','2','3'],
 *      onSort 参数得 (origin, 2, 2) 而非 ([丙甲乙], 2, 0))
 *
 * 探针 B —— 拆掉「拖起时锁定工作副本」:
 *   dragOver() 里把 `const current = d.current` 换成 `const current = d.origin`
 *   (预览索引对着原始列表算 —— 连续经过两个目标时第二次必错位)
 *   ⇒ 红:「DOM 集成 · 连续经过两个目标」(第二次 dragover 后 orderOf(list)
 *      得 ['1','3','2'] 而非 ['1','2','3'])
 *
 * 复原(照抄原版)后两条用例转绿。
 */
