/**
 * useTableEdit —— 表格行内编辑状态机(§2.8 #16,P2)
 *
 * 框架级能力:业务页只负责"这行的哪个字段用什么编辑器"和"怎么落库",
 * 编辑态的状态机(谁在编辑 / 草稿快照 / 保存竞态 / 行消失自愈)由这里统一管。
 *
 * ⚠️ **为什么不能让业务页自己 useState 搞定**(这就是本 hook 存在的理由):
 *
 *   1. **保存竞态**:保存是异步的 —— 用户点保存 → 请求在飞 → 又点了"取消"
 *      → 甚至开始编辑**另一行**。裸 useState 的写法里,迟到的保存成功回调
 *      会把**新那行**的编辑状态关掉(或把旧草稿写回新行)。本 hook 用
 *      代际计数(generation)裁决:只有"发起保存的那一代"的回调才能收尾。
 *
 *   2. **草稿必须是快照,不是活引用**:`startEdit(record)` 那一刻把行对象
 *      浅拷贝下来 —— 之后 dataSource 重拉(轮询 / SSE 刷新 / 别的字段被
 *      别处改了)都不许悄悄改用户正在编辑的值。落库时 patch 只含
 *      **真的改过的字段**(与快照逐字段比对),不是整行覆盖 ——
 *      整行覆盖会把"快照之后别人改的其他字段"用旧值顶回去。
 *
 *   3. **编辑中的行从 dataSource 里消失了**(被删 / 被过滤 / 翻页):
 *      继续编辑一个已经不存在的行,保存时必然失败且用户看不懂为什么。
 *      传了 `data` 的话,hook 自动收摊(cancel)。
 *
 *   4. **id 精度** —— 后端 id 是 uint64(19 位),JS number 会丢精度。
 *      editingKey 一律 string(与 useTableSelection 同一口径)。
 *
 * 用法(配合 EditableCell):
 *   const edit = useTableEdit({ data, onSave: (row, patch) => api.update(row.id, patch) })
 *   { title: '备注', dataIndex: 'remark', render: (v, record) => (
 *       <EditableCell
 *         editing={edit.isEditing(record)}
 *         view={<span>{v || '-'}</span>}
 *         value={edit.getDraft(record).remark}
 *         onChange={(val) => edit.setField('remark', val)}
 *         editor={<Input />}
 *       />
 *   )}
 *   操作列:<Button onClick={() => edit.startEdit(record)}>编辑</Button>
 *          <Button onClick={() => edit.saveEdit().catch(showErr)}>保存</Button>
 *          <Button onClick={edit.cancelEdit}>取消</Button>
 *
 * ⚠️ **错误不吞**:saveEdit 不 catch —— 落库失败时**保持编辑态**(草稿原样
 *   保留,用户可以直接改完再存),异常抛给调用方 toast。hook 永远不替
 *   调用方决定"失败要不要弹提示"。成功时才自动收摊(且只收自己那一代)。
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Key } from 'react'

/** 行 key —— 与 useTableSelection 同一口径:string(19 位 uint64 不转 number) */
export type EditKey = string

export interface UseTableEditOptions<T> {
  /** 当前页数据 —— 传了它,"编辑中的行消失"就会自动收摊(见文件头 #3) */
  data?: T[]
  /** 行 key 字段名(默认 'id')或取值函数 */
  rowKey?: string | ((row: T) => Key)
  /** 落库:record 是"开始编辑时的快照行",patch 只含改过的字段 */
  onSave: (record: T, patch: Partial<T>) => Promise<void>
  /** 编辑前置门(如无权限)—— 返回 false 则 startEdit 是 no-op */
  onBeforeEdit?: (record: T) => boolean
}

export interface UseTableEditResult<T> {
  /** 正在编辑的行 key(null = 没有行在编辑) */
  editingKey: EditKey | null
  /** 某行是否正在编辑 */
  isEditing: (row: T) => boolean
  /** 编辑中的草稿(已合并改动);该行不在编辑时返回原行对象(只读语义,别改它) */
  getDraft: (row: T) => T
  /** 相对快照改过哪些字段(落库用) */
  getPatch: (row: T) => Partial<T>
  /** 开始编辑某行(已有别的行在编辑 ⇒ 旧草稿丢弃、换新行) */
  startEdit: (row: T) => void
  /** 改草稿的某个字段 */
  setField: (field: string, value: unknown) => void
  /** 取消编辑(丢弃草稿;保存飞着的时候也允许 —— 保存的收尾被代际计数挡住) */
  cancelEdit: () => void
  /**
   * 保存。成功 ⇒ 清编辑态并 resolve true;
   * 失败 ⇒ **抛原异常**(saving 收回、编辑态保持、草稿不丢),调用方 catch 后 toast。
   * 保存中重复调用 / 没有行在编辑 ⇒ 安静拒绝(resolve false,"重复点"不是错误)。
   */
  saveEdit: () => Promise<boolean>
  /** 是否有保存在飞(按钮 loading 用) */
  saving: boolean
  /** 草稿相对快照是否有改动(取消前要不要确认,调用方自己定) */
  isDirty: boolean
}

/**
 * 浅比较两个对象,返回 patch:a 相对 base 改过的字段。
 * 纯函数 —— 字段值用 Object.is 比(NaN 等值、±0 不等,与 React 同一口径)。
 * ⚠️ 只做一层:嵌套对象整个换引用才算改(编辑器产出的都是新原始值,
 * 没有深层编辑场景;为不存在的场景做深比较是负资产)。
 */
export function diffPatch<T extends object>(base: T, draft: Partial<T>): Partial<T> {
  const rec = base as Record<string, unknown>
  const patch = {} as Record<string, unknown>
  for (const k of Object.keys(draft) as (keyof T & string)[]) {
    if (!Object.is(draft[k], rec[k])) {
      patch[k] = draft[k]
    }
  }
  return patch as Partial<T>
}

export function useTableEdit<T extends object>(
  options: UseTableEditOptions<T>,
): UseTableEditResult<T> {
  const { data, rowKey: rowKeyOption = 'id', onSave, onBeforeEdit } = options

  const [editingKey, setEditingKey] = useState<EditKey | null>(null)
  /** 开始编辑那一刻的行快照 */
  const [snapshot, setSnapshot] = useState<T | null>(null)
  /** 用户在快照上改出来的草稿(只含被改的字段也行,全量也行 —— 统一存全量) */
  const [draft, setDraft] = useState<Partial<T>>({})
  const [saving, setSaving] = useState(false)

  /**
   * 代际计数 —— 每次编辑目标变化(开始 / 取消 / 换行)+1。
   * saveEdit 起飞时记下自己那一代;成功收尾(清编辑态)前先对表:
   * 代数对不上说明用户已经取消或换行了,这一笔的**成功**无权再动编辑状态
   * (失败本来就不动 —— 编辑态保持,见 saveEdit)。
   */
  const genRef = useRef(0)

  const keyOf = useCallback(
    (row: T): EditKey => {
      const k =
        typeof rowKeyOption === 'function'
          ? rowKeyOption(row)
          : (row as Record<string, unknown>)?.[rowKeyOption]
      return k === undefined || k === null ? '' : String(k)
    },
    [rowKeyOption],
  )

  const isEditing = useCallback(
    (row: T) => editingKey !== null && keyOf(row) === editingKey,
    [editingKey, keyOf],
  )

  /** 编辑中的行拿到"快照 + 改动"的合并视图;非编辑行原样返回 */
  const getDraft = useCallback(
    (row: T): T => {
      if (!isEditing(row)) return row
      return { ...row, ...draft } as T
    },
    [isEditing, draft],
  )

  const getPatch = useCallback(
    (row: T): Partial<T> => {
      if (!isEditing(row) || !snapshot) return {}
      return diffPatch(snapshot, { ...row, ...draft })
    },
    [isEditing, snapshot, draft],
  )

  const startEdit = useCallback(
    (row: T) => {
      if (onBeforeEdit && !onBeforeEdit(row)) return
      genRef.current += 1
      setEditingKey(keyOf(row))
      // 快照:浅拷贝留住"开始那一刻"(dataSource 之后怎么刷都不影响草稿)
      setSnapshot({ ...row })
      setDraft({})
    },
    [keyOf, onBeforeEdit],
  )

  const setField = useCallback((field: string, value: unknown) => {
    setDraft((prev) => ({ ...prev, [field]: value }))
  }, [])

  const cancelEdit = useCallback(() => {
    genRef.current += 1
    setEditingKey(null)
    setSnapshot(null)
    setDraft({})
  }, [])

  const saveEdit = useCallback((): Promise<boolean> => {
    if (saving || editingKey === null || !snapshot) {
      // 保存中重复点 / 没有行在编辑:不是错误,安静拒绝
      return Promise.resolve(false)
    }
    const patch = diffPatch(snapshot, draft)
    const gen = genRef.current
    setSaving(true)
    // onSave 同步抛错也要把 saving 收回来
    let p: Promise<void>
    try {
      p = onSave(snapshot, patch)
    } catch (err) {
      setSaving(false)
      throw err
    }
    // ⚠️ 不吞错:失败把 saving 收回来后**原样抛给调用方**,编辑态保留(草稿不丢)。
    //    成功对表 —— 只有"还是这一代"才许收摊;用户中途取消/换行了就不许动。
    return p.then(
      () => {
        setSaving(false)
        if (genRef.current === gen) {
          setEditingKey(null)
          setSnapshot(null)
          setDraft({})
        }
        return true
      },
      (err) => {
        setSaving(false)
        throw err
      },
    )
  }, [saving, editingKey, snapshot, draft, onSave])

  // 编辑中的行从 data 里消失 ⇒ 自动收摊(没传 data 就不管 —— 单测 / 静态行也够用)
  useEffect(() => {
    if (!data || editingKey === null) return
    const stillThere = data.some((row) => keyOf(row) === editingKey)
    if (!stillThere) {
      genRef.current += 1
      setEditingKey(null)
      setSnapshot(null)
      setDraft({})
    }
  }, [data, editingKey, keyOf])

  const isDirty = useMemo(() => {
    if (!snapshot) return false
    return Object.keys(diffPatch(snapshot, draft)).length > 0
  }, [snapshot, draft])

  return {
    editingKey,
    isEditing,
    getDraft,
    getPatch,
    startEdit,
    setField,
    cancelEdit,
    saveEdit,
    saving,
    isDirty,
  }
}

export default useTableEdit
