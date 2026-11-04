/**
 * useTablePro 回归网(2026-10-29 M111)
 *
 * 此前这一层**一条网都没有** —— 而 §2.8 #10/11/12/13 四条能力
 * (虚拟滚动 / 列设置 / 导出 CSV / 树形)全挂在它身上,
 * 而且这些能力是**框架层对外承诺过的**(清单里标着 ✅ M56)。
 *
 * 锁住的(每一条背后都是一个真 bug,见 scripts/probe_m111.mjs):
 *
 *   ① **虚拟滚动以前从来没打开过** —— antd 5 的虚拟滚动是显式开关
 *      (`virtual: true`),以前这个 hook 只算了 `scroll.y` ⇒ 1000 行就是
 *      1000 个 DOM 节点,§2.8 #10 承诺的"超阈值自动启用"是假的。
 *      而且 rc-table 的 VirtualTable 要求 `scroll.x` **必须是数字**
 *      (非数字会 warning 并退化成 1px 宽)⇒ 开了 virtual 就必须同时改 x。
 *   ② **CSV 表头以前是 `[object Object]`** —— `title` 的类型是 ReactNode,
 *      而导出时直接 `String(c.title)`。
 *   ③ **CSV 公式注入** —— 以 `=`/`+`/`-`/`@` 开头的字符串被 Excel 当公式执行。
 *   ④ **`rowKey` 是死参数** —— hook 取了不用;`TablePro` 里函数形式还被
 *      静默丢弃(类型声明支持函数,运行时变成 undefined)。
 *   ⑤ **`hideable: false` 的列可能被存储状态永久藏起来** —— 而列设置面板里
 *      它的复选框是禁用的 ⇒ 用户根本没法勾回来。
 *   ⑥ **换 `storageKey` 不重读 localStorage** —— 上一个表格的列状态串到下一个。
 *   ⑦ 列宽 / 显隐 / 恢复默认 / 树形模式 / CSV 转义 / BOM / 锚点挂载 等常规行为。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useTablePro, type TableProColumn } from '@/hooks/useTablePro'

interface Row {
  id: string
  name: string
  phone: string
}

const COLUMNS: TableProColumn<Row>[] = [
  { key: 'name', title: '姓名', dataIndex: 'name', width: 120 },
  { key: 'phone', title: '手机', dataIndex: 'phone', width: 150 },
]

const ROWS: Row[] = [
  { id: '1', name: '甲', phone: '13800000001' },
  { id: '2', name: '乙', phone: '13800000002' },
]

/** localStorage 里存着的列状态(测试直接写,模拟"上次用完留下的") */
function seedStored(
  storageKey: string,
  state: Array<{ key: string; visible: boolean; width: number }>,
) {
  localStorage.setItem('tablepro:' + storageKey, JSON.stringify(state))
}

function readStored(storageKey: string): Array<{ key: string; visible: boolean; width: number }> {
  const raw = localStorage.getItem('tablepro:' + storageKey)
  return raw ? JSON.parse(raw) : []
}

beforeEach(() => {
  localStorage.clear()
})

/* ------------------------------------------------------------------ */
/* CSV 导出的浏览器能力桩(jsdom 没有 createObjectURL,也不该真去下载) */
/* ------------------------------------------------------------------ */
let capturedBlob: Blob | null = null
let anchorClicks: Array<{ connected: boolean; download: string }> = []
const origCreate = (URL as unknown as Record<string, unknown>).createObjectURL
const origRevoke = (URL as unknown as Record<string, unknown>).revokeObjectURL

beforeEach(() => {
  capturedBlob = null
  anchorClicks = []
  ;(URL as unknown as Record<string, unknown>).createObjectURL = vi.fn((b: Blob) => {
    capturedBlob = b
    return 'blob:mock'
  })
  ;(URL as unknown as Record<string, unknown>).revokeObjectURL = vi.fn()
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    // 记录"点击那一刻"锚点是否在文档里 —— 游离元素上的 click 在部分浏览器不触发下载
    anchorClicks.push({ connected: document.body.contains(this), download: this.download })
  })
})

afterEach(() => {
  ;(URL as unknown as Record<string, unknown>).createObjectURL = origCreate
  // ⚠️ 导出里有个 setTimeout(100) 才 revoke;用例跑完它可能还在队列里。
  //   如果这时把 revokeObjectURL 还原成 undefined,那个定时器会炸
  //   (TypeError: not a function)—— 用例已经"通过"了,却留下一个异步错误。
  //   所以还原成"没有就补一个空实现"。
  ;(URL as unknown as Record<string, unknown>).revokeObjectURL = origRevoke ?? vi.fn()
})

/**
 * 读 Blob 的文本内容
 *
 * ⚠️ jsdom 的 Blob **只有** slice / size / type —— 没有 text() / arrayBuffer()
 *   (实测:proto 上就这四个)。所以走 FileReader(jsdom 实现了它)。
 */
function readBlob(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const fr = new FileReader()
    fr.onload = () => resolve(String(fr.result))
    fr.onerror = () => reject(fr.error)
    fr.readAsText(blob)
  })
}

/**
 * 读 Blob 的字节
 *
 * ⚠️ 必须走字节而不是字符串来验 BOM:`FileReader.readAsText` 按 UTF-8
 *   解码规范会**吃掉**开头的 BOM(所以文本里看不到 BOM 字符),
 *   BOM 到底在不在只能看字节 EF BB BF。
 */
function readBlobBytes(blob: Blob): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const fr = new FileReader()
    fr.onload = () => resolve(new Uint8Array(fr.result as ArrayBuffer))
    fr.onerror = () => reject(fr.error)
    fr.readAsArrayBuffer(blob)
  })
}

async function exportAndRead(
  exportCSV: (filename?: string) => void,
  filename?: string,
): Promise<string> {
  act(() => {
    exportCSV(filename)
  })
  expect(capturedBlob, 'createObjectURL 没被调用 ⇒ 导出根本没发生').not.toBeNull()
  return readBlob(capturedBlob as unknown as Blob)
}

/* ================================================================== */

describe('useTablePro · 列状态', () => {
  it('默认:所有列可见,宽度取列定义', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 't1', columns: COLUMNS, dataSource: ROWS }),
    )

    expect(result.current.columns).toHaveLength(2)
    expect(result.current.columns[0]).toMatchObject({ key: 'name', width: 120 })
    expect(result.current.columns[1]).toMatchObject({ key: 'phone', width: 150 })
  })

  it('defaultVisible:false 的列默认就不渲染', () => {
    const cols: TableProColumn<Row>[] = [
      { key: 'name', title: '姓名', dataIndex: 'name' },
      { key: 'phone', title: '手机', dataIndex: 'phone', defaultVisible: false },
    ]
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 't2', columns: cols, dataSource: ROWS }),
    )

    expect(result.current.columns.map((c) => c.key)).toEqual(['name'])
  })

  it('渲染后把列状态写进 localStorage', () => {
    renderHook(() => useTablePro<Row>({ storageKey: 't3', columns: COLUMNS, dataSource: ROWS }))

    expect(readStored('t3')).toEqual([
      { key: 'name', visible: true, width: 120 },
      { key: 'phone', visible: true, width: 150 },
    ])
  })

  it('从 localStorage 恢复:被隐藏的列不再渲染', () => {
    seedStored('t4', [
      { key: 'name', visible: true, width: 120 },
      { key: 'phone', visible: false, width: 150 },
    ])
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 't4', columns: COLUMNS, dataSource: ROWS }),
    )

    expect(result.current.columns.map((c) => c.key)).toEqual(['name'])
  })

  it('localStorage 里存着已删除的列 → 过滤掉,只认还在的列', () => {
    seedStored('t5', [{ key: 'gone', visible: true, width: 100 }])
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 't5', columns: COLUMNS, dataSource: ROWS }),
    )

    expect(result.current.columns.map((c) => c.key)).toEqual(['name', 'phone'])
  })

  it('localStorage 里的内容是坏 JSON → 静默回落默认', () => {
    localStorage.setItem('tablepro:t6', '{ 这不是 JSON')
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 't6', columns: COLUMNS, dataSource: ROWS }),
    )

    expect(result.current.columns).toHaveLength(2)
  })
})

describe('useTablePro · hideable:false 的列永远显示(M111 回归)', () => {
  const cols: TableProColumn<Row>[] = [
    { key: 'name', title: '姓名', dataIndex: 'name' },
    { key: 'phone', title: '手机', dataIndex: 'phone', hideable: false },
  ]

  it('存储里存着 visible:false → 这一列照样渲染', () => {
    // 回归:以前只看存储里的 visible ⇒ 这一列永久消失,
    // 而列设置面板里它的复选框是**禁用**的 ⇒ 用户根本没法勾回来。
    seedStored('h1', [
      { key: 'name', visible: true, width: 120 },
      { key: 'phone', visible: false, width: 150 },
    ])
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'h1', columns: cols, dataSource: ROWS }),
    )

    expect(result.current.columns.map((c) => c.key)).toEqual(['name', 'phone'])
  })

  it('setVisible 对 hideable:false 的列无效', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'h2', columns: cols, dataSource: ROWS }),
    )

    act(() => {
      result.current.columnSettings.setVisible('phone', false)
    })

    expect(result.current.columns.map((c) => c.key)).toEqual(['name', 'phone'])
  })
})

describe('useTablePro · 列设置操作', () => {
  it('setVisible 隐藏一列 → 不再渲染,并写回 localStorage', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'o1', columns: COLUMNS, dataSource: ROWS }),
    )

    act(() => {
      result.current.columnSettings.setVisible('phone', false)
    })

    expect(result.current.columns.map((c) => c.key)).toEqual(['name'])
    expect(readStored('o1').find((s) => s.key === 'phone')?.visible).toBe(false)
  })

  it('setWidth 改列宽 → 渲染出来的列宽跟着变', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'o2', columns: COLUMNS, dataSource: ROWS }),
    )

    act(() => {
      result.current.columnSettings.setWidth('name', 300)
    })

    expect(result.current.columns[0]).toMatchObject({ key: 'name', width: 300 })
  })

  it('reset 恢复默认(显隐 + 列宽)', () => {
    seedStored('o3', [
      { key: 'name', visible: true, width: 999 },
      { key: 'phone', visible: false, width: 888 },
    ])
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'o3', columns: COLUMNS, dataSource: ROWS }),
    )

    act(() => {
      result.current.columnSettings.reset()
    })

    expect(result.current.columns.map((c) => c.key)).toEqual(['name', 'phone'])
    expect(result.current.columns[0]).toMatchObject({ width: 120 })
  })

  it('换 storageKey → 重新读那一份的列状态(M111 回归)', () => {
    seedStored('old', [
      { key: 'name', visible: false, width: 120 },
      { key: 'phone', visible: false, width: 150 },
    ])
    seedStored('fresh', [
      { key: 'name', visible: true, width: 120 },
      { key: 'phone', visible: true, width: 150 },
    ])

    const { result, rerender } = renderHook(
      ({ key }: { key: string }) =>
        useTablePro<Row>({ storageKey: key, columns: COLUMNS, dataSource: ROWS }),
      { initialProps: { key: 'old' } },
    )
    expect(result.current.columns.map((c) => c.key)).toEqual([])

    rerender({ key: 'fresh' })

    // 回归:以前换 key 不重读 ⇒ 一直是上一张表的列状态
    expect(result.current.columns.map((c) => c.key)).toEqual(['name', 'phone'])
  })
})

describe('useTablePro · 虚拟滚动(M111 回归:以前根本没打开)', () => {
  it('行数超过阈值 → virtual: true', () => {
    const many = Array.from({ length: 101 }, (_, i) => ({
      id: String(i),
      name: 'n' + i,
      phone: '13800000000',
    }))
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'v1', columns: COLUMNS, dataSource: many }),
    )

    expect(result.current.tableProps.virtual).toBe(true)
  })

  it('行数不超过阈值 → 不开虚拟', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'v2', columns: COLUMNS, dataSource: ROWS }),
    )

    expect(result.current.tableProps.virtual).toBe(false)
  })

  it('虚拟模式下 scroll.x 必须是**数字**(rc-table 非数字会退化成 1px 宽)', () => {
    const many = Array.from({ length: 200 }, (_, i) => ({
      id: String(i),
      name: 'n' + i,
      phone: '13800000000',
    }))
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'v3', columns: COLUMNS, dataSource: many }),
    )

    const x = result.current.tableProps.scroll.x
    expect(typeof x, `scroll.x=${String(x)} 不是数字`).toBe('number')
    // 可见列宽之和(120 + 150)
    expect(x).toBe(270)
    expect(typeof result.current.tableProps.scroll.y).toBe('number')
  })

  it('非虚拟模式 scroll.x 保持 "max-content"(原来的行为)', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'v4', columns: COLUMNS, dataSource: ROWS }),
    )

    expect(result.current.tableProps.scroll.x).toBe('max-content')
  })

  it('树形模式与虚拟滚动互斥(展开后行数会变,固定行高会错位)', () => {
    const many = Array.from({ length: 500 }, (_, i) => ({
      id: String(i),
      name: 'n' + i,
      phone: '13800000000',
    }))
    const { result } = renderHook(() =>
      useTablePro<Row>({
        storageKey: 'v5',
        columns: COLUMNS,
        dataSource: many,
        treeMode: true,
      }),
    )

    expect(result.current.tableProps.virtual).toBe(false)
    expect(result.current.tableProps.expandable).toBeTruthy()
  })

  it('虚拟阈值可配', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({
        storageKey: 'v6',
        columns: COLUMNS,
        dataSource: ROWS,
        virtualThreshold: 1,
      }),
    )

    expect(result.current.tableProps.virtual).toBe(true)
  })
})

describe('useTablePro · 树形模式', () => {
  it('treeMode 关 → 没有 expandable', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'tr1', columns: COLUMNS, dataSource: ROWS }),
    )

    expect(result.current.tableProps.expandable).toBeUndefined()
  })

  it('treeMode 开 → rowExpandable 只认有 children 的行', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'tr2', columns: COLUMNS, dataSource: ROWS, treeMode: true }),
    )
    const expandable = result.current.tableProps.expandable

    expect(expandable.childrenColumnName).toBe('children')
    expect(expandable.rowExpandable({ id: '1', name: 'a', phone: 'b' })).toBe(false)
    expect(
      expandable.rowExpandable({ id: '1', name: 'a', phone: 'b', children: [{ id: '2' }] }),
    ).toBe(true)
  })
})

describe('useTablePro · rowKey 透出(M111 回归:以前是死参数)', () => {
  it('字符串 rowKey 原样透出', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'r1', columns: COLUMNS, dataSource: ROWS, rowKey: 'uid' }),
    )

    expect(result.current.tableProps.rowKey).toBe('uid')
  })

  it('函数形式的 rowKey 原样透出(不再被丢掉)', () => {
    const fn = (r: Row) => r.id
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'r2', columns: COLUMNS, dataSource: ROWS, rowKey: fn }),
    )

    expect(result.current.tableProps.rowKey).toBe(fn)
  })

  it('没传 → 默认 id', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'r3', columns: COLUMNS, dataSource: ROWS }),
    )

    expect(result.current.tableProps.rowKey).toBe('id')
  })
})

describe('useTablePro · 导出 CSV', () => {
  it('表头取列标题的**文本**(ReactNode 不能变成 [object Object])', async () => {
    const cols: TableProColumn<Row>[] = [
      { key: 'name', title: <span>姓名</span>, dataIndex: 'name' },
      { key: 'phone', title: '手机', dataIndex: 'phone' },
    ]
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c1', columns: cols, dataSource: ROWS }),
    )

    const csv = await exportAndRead(() => result.current.exportCSV())

    expect(csv).not.toContain('[object Object]')
    expect(csv.split('\n')[0]).toBe('姓名,手机')
  })

  it('首行是 BOM(Excel 打开中文不乱码)', async () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c2', columns: COLUMNS, dataSource: ROWS }),
    )

    act(() => {
      result.current.exportCSV()
    })
    expect(capturedBlob).not.toBeNull()

    // ⚠️ 验字节不验字符:readAsText 会按 UTF-8 规范吃掉 BOM
    const bytes = await readBlobBytes(capturedBlob as unknown as Blob)
    expect([bytes[0], bytes[1], bytes[2]]).toEqual([0xef, 0xbb, 0xbf])
  })

  it('数据行按可见列输出', async () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c3', columns: COLUMNS, dataSource: ROWS }),
    )

    const csv = await exportAndRead(() => result.current.exportCSV())

    expect(csv).toContain('甲,13800000001')
    expect(csv).toContain('乙,13800000002')
  })

  it('含逗号 / 引号 / 换行的值要加引号并转义', async () => {
    const rows: Row[] = [{ id: '1', name: '张,三', phone: '他说"你好"' }]
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c4', columns: COLUMNS, dataSource: rows }),
    )

    const csv = await exportAndRead(() => result.current.exportCSV())

    expect(csv).toContain('"张,三"')
    expect(csv).toContain('"他说""你好"""')
  })

  it('⚠️ 公式注入被中和(M111 回归:以前原样导出,Excel 会当公式执行)', async () => {
    const rows: Row[] = [{ id: '1', name: "=cmd|'/c calc'!A1", phone: '@SUM(A1)' }]
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c5', columns: COLUMNS, dataSource: rows }),
    )

    const csv = await exportAndRead(() => result.current.exportCSV())

    expect(csv).not.toMatch(/^=cmd/m)
    expect(csv).toContain("'=cmd")
    expect(csv).toContain("'@SUM(A1)")
  })

  it('数字不加前缀(负数不能被改成文本)', async () => {
    const cols: TableProColumn<Row>[] = [{ key: 'name', title: '值', dataIndex: 'name' }]
    // ⚠️ 必须是**数字** -5,不是字符串 '-5' —— 字符串按公式注入规则会加前缀
    const rows: Row[] = [{ id: '1', name: -5 as never, phone: '' }]
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c6', columns: cols, dataSource: rows }),
    )

    const csv = await exportAndRead(() => result.current.exportCSV())

    expect(csv).toContain('-5')
    expect(csv).not.toContain("'-5")
  })

  it('字符串形式的负号仍然加前缀(它可能被当成公式/被截断)', async () => {
    const cols: TableProColumn<Row>[] = [{ key: 'name', title: '值', dataIndex: 'name' }]
    const rows: Row[] = [{ id: '1', name: '-5', phone: '' }]
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c6b', columns: cols, dataSource: rows }),
    )

    const csv = await exportAndRead(() => result.current.exportCSV())

    expect(csv).toContain("'-5")
  })

  it('文件名:不传用 storageKey,传了用传入的', async () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c7', columns: COLUMNS, dataSource: ROWS }),
    )

    await exportAndRead(() => result.current.exportCSV())
    expect(anchorClicks[0]!.download).toBe('c7.csv')

    await exportAndRead(() => result.current.exportCSV('用户列表'))
    expect(anchorClicks[1]!.download).toBe('用户列表.csv')
  })

  it('⚠️ 点击那一刻锚点必须在 document 里(M111 回归:游离元素点击不触发下载)', async () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c8', columns: COLUMNS, dataSource: ROWS }),
    )

    await exportAndRead(() => result.current.exportCSV())

    expect(anchorClicks).toHaveLength(1)
    expect(anchorClicks[0]!.connected).toBe(true)
  })

  it('一列都没显示时导出的什么都不导出', () => {
    const { result } = renderHook(() =>
      useTablePro<Row>({ storageKey: 'c9', columns: COLUMNS, dataSource: ROWS }),
    )

    act(() => {
      result.current.columnSettings.setVisible('name', false)
      result.current.columnSettings.setVisible('phone', false)
    })

    // ⚠️ 这里不能走 exportAndRead —— 它的第一个断言就是"必须有 blob",
    //   而本用例要断言的恰恰是"**不该**有 blob"。
    act(() => {
      result.current.exportCSV()
    })

    expect(capturedBlob, '一列都没显示时不该生成 blob').toBeNull()
    expect(anchorClicks).toHaveLength(0)
  })
})
