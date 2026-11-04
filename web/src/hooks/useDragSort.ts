/**
 * useDragSort —— 拖拽排序状态机(§2.8 #17,P2)
 *
 * 框架级能力:业务页只负责"把列表交给 hook + 在 onSort 里落库",
 * 拖拽的状态机(谁在被拖 / 预览重排 / 放置提交 / 取消还原)由这里统一管。
 * 零 npm 依赖 —— 用原生 HTML5 DnD 事件,不引 sortablejs 一族。
 *
 * ⚠️ **为什么不能让业务页自己 dispatchEvent 搞定**(这就是本 hook 存在的理由):
 *
 *   1. **预览与提交必须是两回事**:拖拽过程中用户"经过"目标的每一行,
 *      展示序都要实时变(拖到哪、列表让位到哪)—— 但落库只允许在 drop
 *      时发生一次。裸 useState 的写法要么把 dragover 当成提交(一次拖拽
 *      打十几次持久化回调),要么没有预览(拖起来列表纹丝不动,放下才跳)。
 *      本 hook 用"工作副本"裁决:拖起那一刻快照出 origin,**预览只改
 *      drag.current,真身(items)在 drop 之前一个字不动** —— cancel /
 *      dragend 未命中目标,预览直接丢弃,天然还原。
 *
 *   2. **索引必须对着工作副本算,不能对着原始列表算**:连续经过两个目标
 *      (先 A 后 B)时,第二次的 from/to 是"当前预览序"里的下标 ——
 *      对着原始列表算,第二次移动必错位(有探针盯着,见测试)。
 *
 *   3. **HTML5 DnD 的默认行为必须显式扳**:dragover 不 preventDefault,
 *      浏览器就不允许 drop(drop 事件根本不发生);dragstart 不往
 *      dataTransfer 塞数据,Firefox 不发起拖拽。这些坑都由 rowProps 兜住,
 *      业务页只管铺 props。
 *
 *   4. **id 精度** —— 后端 id 是 uint64(19 位),JS number 会丢精度。
 *      key 一律 string(与 useTableEdit / useTableSelection 同一口径)。
 *
 * 用法(antd Table,行上铺 via onRow):
 *   const drag = useDragSort({ items, rowKey: 'id', onSort: (rows) => api.resort(rows.map(r => r.id)) })
 *   <Table dataSource={drag.items} rowKey="id" onRow={(record) => drag.rowProps(record)} ... />
 *
 * 用法(自定义列表):
 *   {drag.items.map((row) => (
 *     <li key={row.id} {...drag.rowProps(row)}
 *         style={drag.isDragging(row) ? { opacity: 0.4 } : undefined}>{row.name}</li>
 *   ))}
 *
 * ⚠️ **错误不吞**:onSort 原样调用不 catch —— 落库失败怎么提示是调用方的
 *   事;本层只保证状态机自身干净(拖拽态已收,预览已提交为展示序)。
 */
import { useCallback, useRef, useState } from 'react'
import type { DragEventHandler, Key } from 'react'

/** 行 key —— 与 useTableEdit / useTableSelection 同口径:string */
export type DragKey = string

/**
 * 同列表重排(纯函数,可独立测试)。
 *
 * 语义 = **先摘后插**:把 from 位置的元素摘下来,再插到 to 位置 ——
 * to 是"摘出之后"那个列表里的下标。向下移动(0→2)落到 2,向上移动
 * (2→0)落到 0,与 sortablejs 一族的直觉一致。
 *
 * 越界 clamp 到 [0, n-1];from === to 时返回**原数组引用**(no-op,
 * 调用方可用引用相等短路)。不改传入的数组。
 */
export function reorder<T>(list: readonly T[], from: number, to: number): T[] {
  const n = list.length
  if (n <= 1) return list as T[]
  let f = Math.floor(from)
  let t = Math.floor(to)
  if (f < 0) f = 0
  else if (f >= n) f = n - 1
  if (t < 0) t = 0
  else if (t >= n) t = n - 1
  if (f === t) return list as T[]
  const out = list.slice()
  const moved = out[f]!
  out.splice(f, 1)
  out.splice(t, 0, moved)
  return out
}

export interface UseDragSortOptions<T> {
  /**
   * 受控模式:列表真相信调用方持有(如 useTable 的 data)。
   * drop 只把新序通过 onSort 交出去,**调用方必须在 onSort 里更新 items**,
   * 否则展示会闪回旧序(标准受控语义)。
   */
  items?: T[]
  /** 非受控模式:初始列表,hook 自己持有顺序(与 items 二选一,items 优先) */
  initialItems?: T[]
  /** 行 key 字段名(默认 'id')或取值函数 */
  rowKey?: string | ((row: T) => Key)
  /**
   * 排序落库回调:drop(或编程式 move)真正发生重排时调用一次。
   * newItems 是**新序的完整数组**(行对象原样,不是 key),from/to 是
   * 重排前后的下标。原地放下(from === to)不调。
   */
  onSort?: (newItems: T[], from: number, to: number) => void
  /** 拖拽前置门(如无权限)—— false 则该行 rowProps.draggable = false 且 dragstart 不生效 */
  canDrag?: (row: T) => boolean
}

/** 铺到行元素上的 props —— antd Table 走 onRow,自定义列表直接展开 */
export interface DragRowProps {
  draggable: boolean
  onDragStart: DragEventHandler<HTMLElement>
  onDragOver: DragEventHandler<HTMLElement>
  onDrop: DragEventHandler<HTMLElement>
  onDragEnd: DragEventHandler<HTMLElement>
}

export interface UseDragSortResult<T> {
  /** 当前展示序(拖拽中 = 预览序;平时 = 受控 items 或内部持有序) */
  items: T[]
  /** 正在被拖的行 key(null = 没有拖拽进行) */
  draggingKey: DragKey | null
  /** 拖拽悬停目标行的 key(高亮"将放到这里"用) */
  overKey: DragKey | null
  /** 是否有拖拽进行中 */
  dragging: boolean
  /** 某行是否正被拖(压暗样式用) */
  isDragging: (row: T) => boolean
  /** 某行是否是悬停目标(高亮样式用) */
  isOver: (row: T) => boolean
  /** 铺到行元素上:antd Table 用 onRow={(r) => drag.rowProps(r)},自定义列表 {...drag.rowProps(r)} */
  rowProps: (row: T) => DragRowProps
  /** 手动:开始拖某行(rowProps 内部也走它) */
  startDrag: (row: T) => void
  /** 手动:悬停到某行上(触发预览重排) */
  dragOver: (row: T) => void
  /** 手动:放置(提交重排 + onSort) */
  drop: () => void
  /** 手动:取消(丢弃预览,还原) */
  cancel: () => void
  /** 编程式重排(不经过拖拽事件;真正移动了才调 onSort) */
  move: (from: number, to: number) => void
}

/** 拖拽中的工作状态(单一对象,避免多个 useState 互相撕裂) */
interface DragState<T> {
  /** 被拖行的 key(拖动全程锁定 —— 行在预览序里换了位置,key 不变) */
  key: DragKey
  /** 拖起那一刻的下标(onSort 的 from) */
  fromIndex: number
  /** 拖起那一刻的列表快照(语义上是"还原基准";非受控模式下真身在 drop 前不动) */
  origin: T[]
  /** 预览工作副本(dragover 实时改它;drop 提交它) */
  current: T[]
  /** 当前悬停目标 key */
  overKey: DragKey | null
}

export function useDragSort<T extends object>(
  options: UseDragSortOptions<T> = {},
): UseDragSortResult<T> {
  const {
    items: controlledItems,
    initialItems,
    rowKey: rowKeyOption = 'id',
    onSort,
    canDrag,
  } = options

  const isControlled = controlledItems !== undefined
  const [innerItems, setInnerItems] = useState<T[]>(() => initialItems ?? [])
  const baseItems = isControlled ? controlledItems : innerItems

  const keyOf = useCallback(
    (row: T): DragKey => {
      const k =
        typeof rowKeyOption === 'function'
          ? rowKeyOption(row)
          : (row as Record<string, unknown>)?.[rowKeyOption]
      return k === undefined || k === null ? '' : String(k)
    },
    [rowKeyOption],
  )

  const [drag, setDrag] = useState<DragState<T> | null>(null)

  /**
   * 拖拽态的真身在 ref,useState 只做镜像(驱动重渲染)。
   * 原因:HTML5 DnD 的事件是**同步连发**的 —— drop 之后浏览器立刻派发
   * dragend,中间没有一次渲染。如果状态只在 state 里,dragend 读到的
   * 还是拖拽中的旧值(drop 明明已经收摊)→ 误判成"没 drop 过"又取消一次。
   * ref 先行写入 + state 跟进,事件路径读到的永远是最新的。
   */
  const dragRef = useRef<DragState<T> | null>(null)

  /** 拖拽态唯一写入口:ref 即时写 + state 镜像 */
  const applyDrag = useCallback((next: DragState<T> | null) => {
    dragRef.current = next
    setDrag(next)
  }, [])

  // 渲染期兜底同步(eager 写与 setDrag 之间若有别的渲染触发,值也一致)
  dragRef.current = drag

  const itemsRef = useRef(baseItems)
  itemsRef.current = baseItems

  const onSortRef = useRef(onSort)
  onSortRef.current = onSort

  const startDrag = useCallback(
    (row: T) => {
      // 前置门(与 rowProps.draggable 同一把尺子):手动 API 也不许绕过
      if (canDrag && !canDrag(row)) return
      const cur = itemsRef.current
      const k = keyOf(row)
      const idx = cur.findIndex((r) => keyOf(r) === k)
      if (idx < 0) return
      // 已有拖拽进行中 ⇒ no-op(重复 dragstart 不是错误,也不是新拖拽)
      if (dragRef.current) return
      applyDrag({ key: k, fromIndex: idx, origin: cur, current: cur, overKey: k })
    },
    [keyOf, applyDrag, canDrag],
  )

  const dragOver = useCallback(
    (row: T) => {
      const d = dragRef.current
      if (!d) return
      const k = keyOf(row)
      // 索引必须对着**工作副本**算(见文件头 #2),对着 origin 算连续经过必错位
      const current = d.current
      const fromIdx = current.findIndex((r) => keyOf(r) === d.key)
      const toIdx = current.findIndex((r) => keyOf(r) === k)
      if (fromIdx < 0 || toIdx < 0) {
        // 目标不在列表里(行被删/被过滤/外部元素):不动预览,也不高亮
        return
      }
      if (fromIdx === toIdx) {
        // 悬停到自己:不重排,只更新悬停高亮
        if (d.overKey !== k) applyDrag({ ...d, overKey: k })
        return
      }
      applyDrag({ ...d, current: reorder(current, fromIdx, toIdx), overKey: k })
    },
    [keyOf, applyDrag],
  )

  const drop = useCallback(() => {
    const d = dragRef.current
    if (!d) return
    const toIndex = d.current.findIndex((r) => keyOf(r) === d.key)
    // 先收摊:drop 之后浏览器立刻派发 dragend,收晚了会被误判成"未放置"
    applyDrag(null)
    if (toIndex < 0 || toIndex === d.fromIndex) return // 原地放下:不算重排
    if (!isControlled) setInnerItems(d.current)
    // ⚠️ 不吞错:落库失败原样抛给事件层,状态机自身保持干净(已提交)
    onSortRef.current?.(d.current, d.fromIndex, toIndex)
  }, [keyOf, applyDrag, isControlled])

  const cancel = useCallback(() => {
    applyDrag(null)
  }, [applyDrag])

  const move = useCallback(
    (from: number, to: number) => {
      const next = reorder(itemsRef.current, from, to)
      if (next === itemsRef.current) return // clamp 后原地:no-op
      if (!isControlled) setInnerItems(next)
      onSortRef.current?.(next, from, to)
    },
    [isControlled],
  )

  const draggingKey = drag?.key ?? null
  const overKey = drag?.overKey ?? null

  const isDragging = useCallback(
    (row: T) => draggingKey !== null && keyOf(row) === draggingKey,
    [draggingKey, keyOf],
  )

  const isOver = useCallback(
    (row: T) => overKey !== null && keyOf(row) === overKey,
    [overKey, keyOf],
  )

  const rowProps = useCallback(
    (row: T): DragRowProps => {
      const allowed = canDrag ? canDrag(row) : true
      return {
        draggable: allowed,
        onDragStart: (e) => {
          if (!allowed || dragRef.current) return
          // Firefox 要求 dragstart 往 dataTransfer 塞数据才发起拖拽;
          // jsdom 没实现 DataTransfer ⇒ 运行时可能没有,可选链兜底
          e.dataTransfer?.setData('text/plain', keyOf(row))
          if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
          startDrag(row)
        },
        onDragOver: (e) => {
          if (!dragRef.current) return
          // HTML5 DnD:dragover 不 preventDefault,浏览器不允许 drop
          e.preventDefault()
          dragOver(row)
        },
        onDrop: (e) => {
          if (!dragRef.current) return
          e.preventDefault()
          drop()
        },
        // dragend 在 drop 之后立刻派发(中间无渲染);drop 已收摊 ⇒ 这里
        // 只会命中"真没放到目标上"(拖出列表 / 按 Esc)⇒ 丢弃预览还原
        onDragEnd: () => {
          if (dragRef.current) cancel()
        },
      }
    },
    [canDrag, keyOf, startDrag, dragOver, drop, cancel],
  )

  return {
    items: drag ? drag.current : baseItems,
    draggingKey,
    overKey,
    dragging: drag !== null,
    isDragging,
    isOver,
    rowProps,
    startDrag,
    dragOver,
    drop,
    cancel,
    move,
  }
}

export default useDragSort
