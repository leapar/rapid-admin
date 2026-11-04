/**
 * useTablePro —— TablePro 增强表格 hook(§2.8 #10/11/12/13 P2)
 *
 * 集成 4 大能力:
 *  1. 列设置(列宽 + 显示/隐藏,localStorage 记忆)
 *  2. 虚拟滚动(> 100 行自动启用,Antd Table 用 `virtual` + `scroll.y` 触发)
 *  3. 树形展开(行含 children → 折叠/展开状态)
 *  4. 导出 CSV(纯前端,Blob + a.download)
 *
 * 用法:
 *  const { columns, dataSource, tableProps, exportCSV } = useTablePro({
 *    storageKey: 'user-list',
 *    columns: baseColumns,
 *    dataSource: rows,
 *  })
 *  <Table {...tableProps} columns={columns} dataSource={dataSource} />
 *
 * ⚠️ **M111 修正:虚拟滚动以前从来没生效过**
 *   以前这个 hook 只设了 `scroll.y`,**从没设 antd 的 `virtual` prop** ——
 *   而 antd 5 的虚拟滚动是**显式开关**(`virtual: true` 才走 rc-table 的
 *   VirtualTable),不是"行数多了自动来"。所以 §2.8 #10 写的
 *   "行数 > virtualThreshold(默认 100)自动启用 Antd 5 内置虚拟滚动"
 *   **是假的**:1000 行就是 1000 个 DOM 节点,虚拟滚动要解决的那个问题
 *   一个都没解决。
 *   现在真接上了,并且满足 rc-table 的硬约束:
 *   `VirtualTableProps.scroll = { x?: number; y: number }` ——
 *   **`scroll.x` 必须是数字**。rc-table 的 VirtualTable 里:
 *     if (typeof scrollX !== 'number') { warning(...); scrollX = 1 }
 *   ⇒ 传字符串的话表格宽度会**塌成 1px**。所以虚拟模式下 `scroll.x`
 *   改成"可见列宽之和"(数字);非虚拟模式保持原来的 `'max-content'`。
 *
 * ⚠️ **虚拟滚动与树形模式互斥**:树形表格展开后行数会变,而虚拟列表按
 *   固定行高渲染 ⇒ 展开的子行会错位。两边同时要时,树形优先(关虚拟)。
 */
import { useEffect, useMemo, useState, useCallback, useRef } from 'react'
import type React from 'react'

export interface TableProColumn<T = any> {
  /** 列 key(必填,用于 localStorage 记忆) */
  key: string
  /** Antd Table column.title */
  title: React.ReactNode
  /** Antd Table column.dataIndex */
  dataIndex?: string | string[]
  /** Antd Table column.render */
  render?: (value: any, record: T, index: number) => React.ReactNode
  /** 列宽(px,默认 120) */
  width?: number
  /** 默认显示(true) */
  defaultVisible?: boolean
  /** 固定列 left/right */
  fixed?: 'left' | 'right'
  /** 对齐 */
  align?: 'left' | 'center' | 'right'
  /** 排序 */
  sorter?: boolean
  /** 是否可隐藏(默认 true) */
  hideable?: boolean
}

export interface UseTableProOptions<T = any> {
  /** localStorage 记忆 key */
  storageKey: string
  /** 基础列定义 */
  columns: TableProColumn<T>[]
  /** 数据源 */
  dataSource: T[]
  /**
   * 行唯一 key(默认 id)
   *
   * ⚠️ **M111 修正:以前这个参数是死的**(`_rowKey`,取了不用)——
   *   hook 自己不渲染 Table,行 key 到底怎么算,这个 hook 无从决定。
   *   现在它**原样透出到 `tableProps.rowKey`**,由消费方
   *   (`TablePro`)直接展开给 `<Table>` —— 一处真源。
   */
  rowKey?: string | ((record: T) => string)
  /** 虚拟滚动阈值(行数超过即启用,默认 100) */
  virtualThreshold?: number
  /** 是否启用树形模式(行有 children) */
  treeMode?: boolean
  /** 默认展开所有(树形) */
  defaultExpandAll?: boolean
  /** 虚拟滚动固定行高(默认 54) */
  rowHeight?: number
}

interface StoredColState {
  key: string
  visible: boolean
  width: number
}

const STORAGE_PREFIX = 'tablepro:'

/** 列宽缺省值(与 loadStored / 应用阶段保持一致) */
const DEFAULT_COL_WIDTH = 120

/**
 * 从 localStorage 读列状态,fallback 到 default
 */
function loadStored(storageKey: string, columns: TableProColumn[]): StoredColState[] {
  try {
    const raw = localStorage.getItem(STORAGE_PREFIX + storageKey)
    if (raw) {
      const parsed = JSON.parse(raw) as StoredColState[]
      // 校验 keys 仍存在
      const validKeys = new Set(columns.map((c) => c.key))
      const filtered = parsed.filter((s) => validKeys.has(s.key))
      if (filtered.length > 0) return filtered
    }
  } catch {
    // ignore
  }
  return columns.map((c) => ({
    key: c.key,
    visible: c.defaultVisible !== false,
    width: c.width || DEFAULT_COL_WIDTH,
  }))
}

/**
 * 写列状态到 localStorage
 */
function saveStored(storageKey: string, state: StoredColState[]): void {
  try {
    localStorage.setItem(STORAGE_PREFIX + storageKey, JSON.stringify(state))
  } catch {
    // localStorage 满 / 禁用 → 静默
  }
}

/**
 * ReactNode → 纯文本(给 CSV 表头用)
 *
 * ⚠️ **M111 修的真 bug:CSV 表头以前是 `[object Object]`**
 *   `TableProColumn.title` 的类型是 `React.ReactNode`,而导出时写的是
 *   `escape(c.title)` ⇒ `String(<span>姓名</span>)` ⇒ **`[object Object]`**。
 *   也就是说:只要列标题不是纯字符串(带图标 / 加粗 / 用了 i18n 组件),
 *   导出的 CSV 表头就是一串 `[object Object]`,Excel 里完全没法看。
 *   现在按节点类型逐层取文本。
 */
function nodeToText(node: React.ReactNode): string {
  if (node === null || node === undefined || node === false || node === true) return ''
  if (typeof node === 'string') return node
  if (typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(nodeToText).join('')
  // ⚠️ 这里必须 `as unknown as` —— ReactNode 里还有 ReactPortal,
  //   它跟 `Record<string, unknown>` 没有足够重叠,直接 as 会被 tsc 判 TS2352。
  if (typeof node === 'object' && 'props' in (node as unknown as Record<string, unknown>)) {
    const children = (node as { props?: { children?: React.ReactNode } }).props?.children
    return nodeToText(children)
  }
  return String(node)
}

/**
 * CSV 单元格转义
 *
 * ⚠️ **M111 加的一档:CSV 公式注入**
 *   以 `=` / `+` / `-` / `@` 开头的**字符串**被 Excel 当成公式执行
 *   (`=cmd|'/c calc'!A1` 这类)。导出的是业务数据(备注 / 昵称 / 标题
 *   都可能由用户输入)⇒ 别人双击打开就可能被执行。
 *   行业通行做法是前置一个单引号把它降级成文本。
 *   ⚠️ **只对字符串加前缀**:数字(-5)不该被改成文本,否则 Excel 里
 *   负数会变成字符串(左对齐、不能求和)。
 */
function csvCell(value: unknown): string {
  if (value === null || value === undefined) return ''
  const raw = typeof value === 'string' ? value : String(value)
  const guarded = typeof value === 'string' && /^[=+\-@\t\r]/.test(raw) ? `'${raw}` : raw
  if (/["\n\r,]/.test(guarded)) {
    return '"' + guarded.replace(/"/g, '""') + '"'
  }
  return guarded
}

export interface UseTableProResult<T = any> {
  /** 过滤 + 应用状态后的列 */
  columns: any[]
  /** 数据源(树形模式下注入 children 处理) */
  dataSource: T[]
  /** Antd Table props(已经处理 scroll / expandable / virtual) */
  tableProps: {
    scroll: { x?: number | 'max-content'; y?: number }
    expandable?: any
    pagination?: false
    size?: 'middle' | 'small' | 'large'
    /**
     * 虚拟滚动开关(M111 新增的真开关)
     *
     * ⚠️ antd 5 的虚拟滚动**不是自动的**,必须显式 `virtual: true`。
     */
    virtual?: boolean
    /** 行 key(原样透出,消费方直接展开给 `<Table>`) */
    rowKey: string | ((record: T) => string)
  }
  /** 列设置 UI 用的元数据 */
  columnSettings: {
    all: TableProColumn[]
    states: StoredColState[]
    setVisible: (key: string, visible: boolean) => void
    setWidth: (key: string, width: number) => void
    reset: () => void
  }
  /** 导出当前 dataSource 为 CSV */
  exportCSV: (filename?: string) => void
}

export function useTablePro<T extends Record<string, any>>(
  opts: UseTableProOptions<T>,
): UseTableProResult<T> {
  const {
    storageKey,
    columns: baseColumns,
    dataSource,
    rowKey = 'id',
    virtualThreshold = 100,
    treeMode = false,
    defaultExpandAll = false,
    rowHeight = 54,
  } = opts

  // 列状态(visible + width)
  const [states, setStates] = useState<StoredColState[]>(() => loadStored(storageKey, baseColumns))

  /**
   * ⚠️ **M111 修正:换 storageKey 时要重读**
   *   以前只有 `useState` 的初始化器读一次 localStorage ⇒ 同一个组件
   *   实例换 `storageKey`(比如按项目切表格)之后,列状态还停在上一个
   *   表格上 —— 而且**永远回不去**(只有刷新页面才重置)。
   *   用 ref 记住"上一次读的是哪个 key",只在 key 真的变了才重读。
   */
  const storageKeyRef = useRef(storageKey)
  useEffect(() => {
    if (storageKeyRef.current === storageKey) return
    storageKeyRef.current = storageKey
    setStates(loadStored(storageKey, baseColumns))
  }, [storageKey, baseColumns])

  // baseColumns 变化时(传新列),补上缺失的 key 状态
  useEffect(() => {
    setStates((prev) => {
      const known = new Set(prev.map((s) => s.key))
      const missing = baseColumns
        .filter((c) => !known.has(c.key))
        .map((c) => ({
          key: c.key,
          visible: c.defaultVisible !== false,
          width: c.width || DEFAULT_COL_WIDTH,
        }))
      if (missing.length === 0) return prev
      // ⚠️ 这里**不写 localStorage** —— 下面那个 effect 会在 states 变化后写。
      //   写在 setState 的 updater 里是副作用:React 18 StrictMode 会把
      //   updater 调用两次 ⇒ 写两遍(而且写的是闭包里的旧值)。
      return [...prev, ...missing]
    })
  }, [baseColumns, storageKey])

  // 写回 localStorage
  useEffect(() => {
    saveStored(storageKey, states)
  }, [storageKey, states])

  // 应用列状态
  const columns = useMemo(() => {
    const stateMap = new Map(states.map((s) => [s.key, s]))
    return baseColumns
      .map((c) => {
        const s = stateMap.get(c.key) || { visible: true, width: c.width || DEFAULT_COL_WIDTH }
        /**
         * ⚠️ **M111 修正:`hideable: false` 的列必须永远显示**
         *   ColumnSettings 里 `hideable: false` 的列复选框是**禁用**的
         *   ⇒ 用户没法把它勾回来。而以前这里只看存储里的 `visible` ⇒
         *   一旦某个旧版本的 localStorage 把它存成 `visible: false`
         *   (那时候还没有 hideable 这个概念),这一列就**永久消失**,
         *   只有"恢复默认"能救 —— 而很多人根本不知道那个按钮在哪。
         *   现在:不可隐藏的列,状态里写什么都没用,一律显示。
         */
        if (!s.visible && c.hideable !== false) return null
        return {
          key: c.key,
          title: c.title,
          dataIndex: c.dataIndex,
          render: c.render,
          width: s.width,
          fixed: c.fixed,
          align: c.align,
          sorter: c.sorter,
        }
      })
      .filter(Boolean)
  }, [baseColumns, states])

  /**
   * 虚拟滚动
   *
   * ⚠️ **M111 修正:以前这里只算 scroll.y,`virtual` 从来没被打开过**
   *   ⇒ "超过阈值自动启用虚拟滚动"是假的(见文件头)。
   *   另外:**树形模式与虚拟滚动互斥**(展开后行数会变,固定行高的
   *   虚拟列表会错位)—— 树形优先。
   */
  const useVirtual = !treeMode && dataSource.length > virtualThreshold
  const scrollY = useVirtual ? Math.min(600, dataSource.length * rowHeight) : undefined

  const tableScroll = useMemo(() => {
    if (!useVirtual) {
      // 非虚拟模式:原来的行为(x 用 'max-content' 让列不挤在一起)
      return { x: 'max-content' as const, y: scrollY }
    }
    // ⚠️ 虚拟模式:`scroll.x` **必须是数字** —— rc-table 的 VirtualTable
    //   里非数字会被 warning 并**退化成 1**(表格宽度塌成 1px)。
    //   这里取可见列的宽度之和。
    const totalWidth = columns.reduce(
      (sum: number, c: any) => sum + (typeof c?.width === 'number' ? c.width : DEFAULT_COL_WIDTH),
      0,
    )
    return { x: Math.max(totalWidth, 1), y: scrollY }
  }, [useVirtual, scrollY, columns])

  // 树形配置
  const expandable = useMemo(() => {
    if (!treeMode) return undefined
    return {
      defaultExpandAllRows: defaultExpandAll,
      rowExpandable: (record: T) =>
        Array.isArray((record as any).children) && (record as any).children.length > 0,
      childrenColumnName: 'children',
    }
  }, [treeMode, defaultExpandAll])

  // 列设置操作
  const setVisible = useCallback(
    (key: string, visible: boolean) => {
      setStates((prev) =>
        prev.map((s) => {
          if (s.key !== key) return s
          // ⚠️ 不可隐藏的列不许被隐藏(与 ColumnSettings 里禁用复选框一致)
          const col = baseColumns.find((c) => c.key === key)
          if (col && col.hideable === false) return s
          return { ...s, visible }
        }),
      )
    },
    [baseColumns],
  )

  const setWidth = useCallback((key: string, width: number) => {
    setStates((prev) => prev.map((s) => (s.key === key ? { ...s, width } : s)))
  }, [])

  const reset = useCallback(() => {
    const next = baseColumns.map((c) => ({
      key: c.key,
      visible: c.defaultVisible !== false,
      width: c.width || DEFAULT_COL_WIDTH,
    }))
    setStates(next)
  }, [baseColumns])

  // 导出 CSV
  const exportCSV = useCallback(
    (filename?: string) => {
      const visibleCols = columns
      if (visibleCols.length === 0) return
      const header = visibleCols.map((c: any) => csvCell(nodeToText(c.title))).join(',')
      const rows = dataSource
        .map((row) =>
          visibleCols
            .map((c: any) => {
              const di = c.dataIndex
              let v: any
              if (Array.isArray(di)) {
                v = di.reduce((acc, k) => (acc !== null && acc !== undefined ? acc[k] : acc), row)
              } else {
                v = di ? row[di] : row[c.key]
              }
              return csvCell(v)
            })
            .join(','),
        )
        .join('\n')
      // BOM 头:Excel 打开 UTF-8 CSV 不加 BOM 会中文乱码
      // (写成 \uFEFF 而不是直接贴那个不可见的 BOM 字符 ——
      //  源码里藏一个不可见字符,迟早被人手抖删掉,而且 diff 里根本看不出来)
      const csv = '\uFEFF' + header + '\n' + rows
      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = (filename || storageKey) + '.csv'
      /**
       * ⚠️ **M111 修正:锚点必须先挂到 document 上**
       *   以前是 `a.click()` 直接调 —— 游离元素上的 click 在部分浏览器
       *   (Firefox 尤其)不触发下载,表现是"点了导出按钮,什么也没发生"。
       *   标准写法:挂上 → 点 → 摘掉。
       */
      document.body.appendChild(a)
      try {
        a.click()
      } finally {
        document.body.removeChild(a)
        // 延迟回收:立刻 revoke 会在部分浏览器里打断正在进行的下载
        setTimeout(() => URL.revokeObjectURL(url), 100)
      }
    },
    [columns, dataSource, storageKey],
  )

  return {
    columns,
    dataSource,
    tableProps: {
      scroll: tableScroll,
      expandable,
      pagination: false,
      size: 'middle',
      virtual: useVirtual,
      rowKey,
    },
    columnSettings: {
      all: baseColumns,
      states,
      setVisible,
      setWidth,
      reset,
    },
    exportCSV,
  }
}
