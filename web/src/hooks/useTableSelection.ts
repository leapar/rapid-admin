/**
 * useTableSelection —— 表格行选择 + 批量操作(§2.8 #14,P1)
 *
 * 框架级能力:业务页只负责"勾哪些"和"批量做什么",勾选状态机由这里统一管。
 *
 * ⚠️ **为什么不能直接抄 antd 的 rowSelection**(这就是本 hook 存在的理由):
 *
 *   1. **跨页选择会被 antd 静默吃掉**
 *      antd 的 `useSelection.setSelectedKeys()` 会把"不在当前 dataSource 里的 key"
 *      过滤掉(node_modules/antd/es/table/hooks/useSelection.js:168-181),
 *      除非 `rowSelection.preserveSelectedRowKeys = true`。
 *      而后台管理表格基本都是**后端分页**(默认每页 20 条)→
 *      "第 1 页勾 3 条 → 翻到第 2 页 → 点表头全选"会把第 1 页那 3 条丢掉,
 *      可用户以为"全选"= 全部。
 *      本 hook 默认开 `preserveSelectedRowKeys`,antd 的表头全选是**合并**语义
 *      (useSelection.js:270-283:keySet 从已选 key 起手),跨页不会被冲掉。
 *
 *   2. **antd 的 onChange 第 2 个参数 records 在跨页时不可信**
 *      `preserveSelectedRowKeys = true` 时 antd 用自己的缓存回填,
 *      没渲染过的行拿到的是 `undefined`。
 *      本 hook 自己维护 key→行 的缓存,`selectedRows` 永远拿得到行对象。
 *
 *   3. **id 精度** —— 后端 id 是 uint64(19 位),JS number 会丢精度。
 *      key 一律用后端原样返回的 string,`selectedIds` 也是 string。
 *
 * 用法:
 *   const sel = useTableSelection<SysUser>({ data, rowKey: 'id' })
 *   <Table {...tableProps} rowSelection={sel.rowSelection} />
 *   <BatchActionBar count={sel.selectedCount} onClear={sel.clear}>
 *     <Button onClick={() => batch(sel.selectedIds)}>批量删除</Button>
 *   </BatchActionBar>
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Key } from 'react'
// ⚠️ **RowSelectionProps 在 antd 5 里叫 `TableRowSelection`,而且只在
//    `antd/es/table/interface` 导出** —— `antd/es/table` 的 index 只导出
//    ColumnsType / ColumnType / ColumnGroupType(见 node_modules/antd/es/table/index.d.ts),
//    从 'antd' 根取也没有(跟 ColumnsType 同一个坑)。
import type { TableRowSelection } from 'antd/es/table/interface'

/** 行 key —— 后端 id 是 string(19 位 uint64,不能转 number) */
export type SelectionKey = string | number

export interface UseTableSelectionOptions<T> {
  /** 当前页数据 —— 用来把 key 映射回行对象(跨页时靠缓存) */
  data?: T[]
  /** 行 key 字段名(默认 'id')或取值函数 */
  rowKey?: string | ((row: T) => SelectionKey)
  /** 行级禁用(如"不能删自己")—— 语义与 antd 的 getCheckboxProps 一致 */
  getCheckboxProps?: TableRowSelection<T>['getCheckboxProps']
  /**
   * 是否跨页保留选择(默认 true)
   *
   * false = 翻页即清空(适合"全选 = 本页全部"的语义)
   */
  preserveAcrossPages?: boolean
  /** 初始选中 */
  defaultSelectedRowKeys?: SelectionKey[]
}

export interface UseTableSelectionResult<T> {
  /** 选中的 key(类型与行 key 一致,别转成 number) */
  selectedRowKeys: SelectionKey[]
  /** 选中的 id(全 string)—— 直接喂给后端的批量接口 */
  selectedIds: string[]
  /** 选中的行对象(跨页也能拿到) */
  selectedRows: T[]
  /** 已选数量 */
  selectedCount: number
  /** 某行是否被选中 */
  isSelected: (key: SelectionKey) => boolean
  /** 直接改选中集合 */
  setSelectedRowKeys: (keys: SelectionKey[]) => void
  /** 勾/取消勾某一行(selected 不传 = 取反) */
  toggleRow: (row: T, selected?: boolean) => void
  /** 全选(不传 rows = 当前页全部) */
  selectAll: (rows?: T[]) => void
  /** 清空选择 */
  clear: () => void
  /** 直接喂给 antd <Table rowSelection={...}> */
  rowSelection: TableRowSelection<T>
}

export function useTableSelection<T = any>(
  options: UseTableSelectionOptions<T> = {},
): UseTableSelectionResult<T> {
  const {
    data,
    rowKey: rowKeyOption = 'id',
    getCheckboxProps,
    preserveAcrossPages = true,
    defaultSelectedRowKeys,
  } = options

  const [selectedRowKeys, setSelectedRowKeys] = useState<SelectionKey[]>(
    defaultSelectedRowKeys ?? [],
  )

  // key → 行 的缓存(跨页取行对象靠它;antd 的 records 不可信,见文件头注释)
  const rowCacheRef = useRef<Map<SelectionKey, T>>(new Map())

  const keyOf = useCallback(
    (row: T): SelectionKey =>
      typeof rowKeyOption === 'function'
        ? rowKeyOption(row)
        : ((row as any)?.[rowKeyOption] as SelectionKey),
    [rowKeyOption],
  )

  // ⚠️ **当前页的行在「渲染期」进缓存,不是放进 effect** ——
  //   放 effect 里有两个坏处:
  //     1. 首屏 selectedRows 会算成空数组(effect 还没跑,memo 已经算完了)
  //     2. 更糟的是它**不会自愈** —— selectedRowKeys / data 都没变,
  //        memo 不会重算,于是 defaultSelectedRowKeys 选中的行
  //        到下一次别的 state 变化为止都是空的(假绿:代码看着对,拿到的是空)
  //   React 官方推荐的写法就是"渲染期把纯计算结果缓存进 ref"
  //   (react.dev/reference/react/useMemo#memoizing-a-value-with-a-ref-cache),
  //   这里同理:key→行 的映射只由 data 决定,渲染期填是幂等的。
  //
  //   拆成两层:
  //   · currentRows —— 当前页的 key→行(useMemo,data 变了就换新 identity)
  //   · rowCacheRef —— **跨页**累计缓存(只增不减),翻页后旧页的行对象还在
  const currentRows = useMemo(() => {
    const m = new Map<SelectionKey, T>()
    if (data) {
      for (const row of data) {
        const k = keyOf(row)
        if (k !== undefined && k !== null) m.set(k, row)
      }
    }
    return m
  }, [data, keyOf])

  // 当前页并进跨页缓存(渲染期,幂等)
  for (const [k, row] of currentRows) rowCacheRef.current.set(k, row)

  // preserveAcrossPages = false → 数据换页(真的换了,不是重渲染)就清空选择
  const prevDataRef = useRef<T[] | undefined>(undefined)
  useEffect(() => {
    const prev = prevDataRef.current
    prevDataRef.current = data
    if (preserveAcrossPages) return
    if (prev === undefined || prev === data) return
    setSelectedRowKeys([])
  }, [data, preserveAcrossPages])

  /** antd 的 onChange —— 顺手把 records 塞进缓存(页面没传 data 时的兜底) */
  const handleChange = useCallback(
    (keys: Key[], rows?: T[]) => {
      if (rows?.length) {
        for (const r of rows) {
          const k = keyOf(r)
          if (k !== undefined && k !== null) rowCacheRef.current.set(k, r)
        }
      }
      setSelectedRowKeys(keys as SelectionKey[])
    },
    [keyOf],
  )

  const selectedSet = useMemo(() => new Set(selectedRowKeys), [selectedRowKeys])

  const isSelected = useCallback(
    (key: SelectionKey) => selectedSet.has(key),
    [selectedSet],
  )

  const selectedIds = useMemo(
    () => selectedRowKeys.map((k) => String(k)),
    [selectedRowKeys],
  )

  // 当前页的行优先(对象是新的),翻页前的行从跨页缓存里找
  const selectedRows = useMemo(() => {
    const out: T[] = []
    for (const k of selectedRowKeys) {
      const row = currentRows.get(k) ?? rowCacheRef.current.get(k)
      if (row !== undefined) out.push(row)
    }
    return out
  }, [selectedRowKeys, currentRows])

  const toggleRow = useCallback(
    (row: T, selected?: boolean) => {
      const k = keyOf(row)
      if (k === undefined || k === null) return
      rowCacheRef.current.set(k, row)
      setSelectedRowKeys((prev) => {
        const has = prev.includes(k)
        const next = selected === undefined ? !has : selected
        if (next === has) return prev
        return next ? [...prev, k] : prev.filter((x) => x !== k)
      })
    },
    [keyOf],
  )

  const selectAll = useCallback(
    (rows?: T[]) => {
      const list = rows ?? data ?? []
      const keys = list
        .map((r) => keyOf(r))
        .filter((k): k is SelectionKey => k !== undefined && k !== null)
      setSelectedRowKeys((prev) => Array.from(new Set([...prev, ...keys])))
    },
    [data, keyOf],
  )

  const clear = useCallback(() => setSelectedRowKeys([]), [])

  const rowSelection = useMemo<TableRowSelection<T>>(
    () => ({
      selectedRowKeys,
      // 跨页保留:不加这个,antd 会在翻页后把上一页的 key 悄悄丢掉
      preserveSelectedRowKeys: preserveAcrossPages,
      getCheckboxProps,
      onChange: (keys, rows) => handleChange(keys, rows),
    }),
    [selectedRowKeys, preserveAcrossPages, getCheckboxProps, handleChange],
  )

  return {
    selectedRowKeys,
    selectedIds,
    selectedRows,
    selectedCount: selectedRowKeys.length,
    isSelected,
    setSelectedRowKeys,
    toggleRow,
    selectAll,
    clear,
    rowSelection,
  }
}

export default useTableSelection
