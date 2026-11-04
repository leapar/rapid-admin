/**
 * 数据变更日志页(2026-10-30)
 *
 * ## 这条网守的是什么
 *
 *   1. **列必须读 snake_case 的 wire 名**(`table_name` / `record_id` /
 *      `created_at`)。读成驼峰的话 TypeScript 不报错(类型是自己写的),
 *      而那几列**永远是空的**。
 *
 *   2. **operation 必须是 number,不是字符串**。
 *      字典的 `item_value` 是**字符串**("1"/"2"/"3"),
 *      后端 `queryOptionalInt` 读的是**整数** —— 发 `operation="2"` 会被
 *      `strconv.Atoi` 拒掉(要么 400,要么按"没传"处理)。
 *      ⇒ 下拉的选项必须 `Number(item_value)` 转一道。
 *
 *   3. **操作类型的文案必须来自字典**,不能在前端写死中文。
 *      ("字典 = 代码 enum 契约中央仓库" —— 写死就等于多一处要同步的地方。)
 *
 *   4. **"不筛"必须是真不筛** —— 首屏不带 operation;
 *      清空下拉要回 `undefined` 而不是 `0`。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把列的 dataIndex 改成 `tableName` → 「读的是 wire 上的 table_name」红;
 *   · 把 `Number(o.value)` 去掉 → 「operation 是数字」红;
 *   · 把下拉选项写死成中文 → 「选项来自字典」红;
 *   · 把清空时回 undefined 改成回 0 → 「重置后不筛」红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import dayjs from 'dayjs'
import DataChangeLogList from '../data-change'
import { auditApi } from '@/api/audit'
import { useDictStore } from '@/stores/dict'

vi.mock('@/api/audit', () => ({
  auditApi: { dataChangeLogs: vi.fn() },
}))

const dataChangeLogsMock = vi.mocked(auditApi.dataChangeLogs)

/** waitFor 超时 —— 显式给,别用默认的 1000ms(机器一忙就集体假红) */
const WAIT = { timeout: 10_000 }

/**
 * 字典种子 —— `audit_change_operation`(1 新增 / 2 修改 / 3 删除)
 *
 * ⚠️ 必须种下去,否则 `useDict` 会去发真实请求(axios → jsdom → 网络错误),
 *   下拉里一个选项都没有,「选修改」那几条用例会集体变成假红。
 */
const DICT_AUDIT_CHANGE_OPERATION = [
  {
    id: 'c1',
    group_code: 'audit_change_operation',
    item_key: 'create',
    item_value: '1',
    label: '新增',
    sort: 1,
    is_active: true,
    description: 'color:green',
  },
  {
    id: 'c2',
    group_code: 'audit_change_operation',
    item_key: 'update',
    item_value: '2',
    label: '修改',
    sort: 2,
    is_active: true,
    description: 'color:blue',
  },
  {
    id: 'c3',
    group_code: 'audit_change_operation',
    item_key: 'delete',
    item_value: '3',
    label: '删除',
    sort: 3,
    is_active: true,
    description: 'color:red',
  },
]

/** 一行数据变更日志(wire 上的 snake_case 形状) */
const ROW = {
  id: 'dc1',
  table_name: 'biz_owner_correction',
  record_id: 'r-77',
  operation: 2,
  before: JSON.stringify({ status: 'pending', remark: '' }),
  after: JSON.stringify({ status: 'approved', remark: '已核实' }),
  user_id: '42',
  username: 'lisi',
  created_at: '2026-10-30T09:00:00+08:00',
}

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <DataChangeLogList />
      </MemoryRouter>
    </App>,
  )
}

/**
 * 页面上那个(非分页器的)Select
 *
 * ⚠️ antd Table 的分页器自带一个"20 / page"选择器,DOM 顺序在表格里 ——
 *   按"第一个 .ant-select"取会抓到它,断言就成了一句废话。
 */
function selectEl(): HTMLElement {
  const sels = Array.from(document.querySelectorAll('.ant-select-selector')).filter(
    (el) => !el.closest('.ant-pagination'),
  )
  if (sels.length !== 1) {
    throw new Error(`页面上应该有且只有一个(非分页器的)Select,实际 ${sels.length} 个`)
  }
  return sels[0] as HTMLElement
}

/** antd 5 的下拉是懒渲染的 —— 不打开,一个选项都不在 DOM 里 */
async function openSelect() {
  fireEvent.mouseDown(selectEl())
  await waitFor(() => expect(document.querySelector('.ant-select-dropdown')).toBeTruthy(), WAIT)
}

async function pickOption(label: string) {
  const option = Array.from(
    document.querySelectorAll('.ant-select-dropdown .ant-select-item-option'),
  ).find((el) => (el.textContent ?? '').trim() === label)
  if (!option) throw new Error(`下拉里找不到选项:${label}`)
  fireEvent.click(option)
}

describe('数据变更日志页', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useDictStore.setState({
      dicts: { audit_change_operation: DICT_AUDIT_CHANGE_OPERATION },
      lastFetchAt: { audit_change_operation: 1 },
    })
    dataChangeLogsMock.mockResolvedValue({ list: [ROW], total: 1, page: 1, size: 20 } as never)
  })

  it('首屏拉列表,且**不带** operation(不筛 = 不传,不是传 0)', async () => {
    renderPage()
    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalled(), WAIT)

    const q = dataChangeLogsMock.mock.calls[0]![0]
    expect(q).toMatchObject({ page: 1, size: 20 })
    expect(q.operation).toBeUndefined()
  })

  it('下拉选项来自字典(不是前端写死的中文)', async () => {
    renderPage()
    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalled(), WAIT)

    await openSelect()
    // 字典里那三项必须在;写死中文的话这条一样会绿 —— 所以下面还有一条
    // 「改了字典就变」的探针用例。
    for (const label of ['新增', '修改', '删除']) {
      expect(
        Array.from(document.querySelectorAll('.ant-select-dropdown .ant-select-item-option')).some(
          (el) => (el.textContent ?? '').trim() === label,
        ),
      ).toBe(true)
    }
  })

  it('字典改了,下拉文案跟着改(证明选项是数据驱动的,不是写死的)', async () => {
    useDictStore.setState({
      dicts: {
        audit_change_operation: DICT_AUDIT_CHANGE_OPERATION.map((d) =>
          d.item_value === '2' ? { ...d, label: '变更' } : d,
        ),
      },
      lastFetchAt: { audit_change_operation: 1 },
    })

    renderPage()
    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalled(), WAIT)

    await openSelect()
    const labels = Array.from(
      document.querySelectorAll('.ant-select-dropdown .ant-select-item-option'),
    ).map((el) => (el.textContent ?? '').trim())
    expect(labels).toContain('变更')
    expect(labels).not.toContain('修改')
  })

  it('表格读的是 wire 上的 snake_case 字段(不是驼峰)', async () => {
    renderPage()
    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalled(), WAIT)
    await waitFor(() => expect(screen.getByText('lisi')).toBeInTheDocument(), WAIT)

    expect(screen.getByText('biz_owner_correction')).toBeInTheDocument()
    expect(screen.getByText('r-77')).toBeInTheDocument()
    expect(
      screen.getByText(dayjs(ROW.created_at).format('YYYY-MM-DD HH:mm:ss')),
    ).toBeInTheDocument()
  })

  it('选"修改"发的是 operation=2(数字,不是字符串 "2")', async () => {
    renderPage()
    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalled(), WAIT)

    await openSelect()
    await pickOption('修改')

    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalledTimes(2), WAIT)
    const q = dataChangeLogsMock.mock.calls[1]![0]
    // ⚠️ 必须是**数字 2** —— 发字符串 "2" 后端 `queryOptionalInt` 认不出来。
    expect(q.operation).toBe(2)
    expect(typeof q.operation).toBe('number')
  })

  it('关键字搜索带上 keyword', async () => {
    renderPage()
    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalled(), WAIT)

    const input = screen.getByPlaceholderText('搜索表名')
    fireEvent.change(input, { target: { value: 'biz_owner' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })

    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalledTimes(2), WAIT)
    expect(dataChangeLogsMock.mock.calls[1]![0]).toMatchObject({ keyword: 'biz_owner', page: 1 })
  })

  it('点重置 → 筛选全清(不能留一个 operation:0 变成"筛出 0")', async () => {
    renderPage()
    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalled(), WAIT)

    await openSelect()
    await pickOption('修改')
    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalledTimes(2), WAIT)

    fireEvent.click(screen.getByText('重置'))

    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalledTimes(3), WAIT)
    const q = dataChangeLogsMock.mock.calls[2]![0]
    expect(q.operation).toBeUndefined()
    expect(q.keyword).toBeUndefined()
  })

  it('点"详情" → 抽屉打开,能看到变更前 / 变更后的 JSON', async () => {
    renderPage()
    await waitFor(() => expect(dataChangeLogsMock).toHaveBeenCalled(), WAIT)
    await waitFor(() => expect(screen.getByText('lisi')).toBeInTheDocument(), WAIT)

    fireEvent.click(screen.getByRole('button', { name: /详\s*情/ }))

    await waitFor(() => expect(screen.getByText('数据变更日志详情')).toBeInTheDocument(), WAIT)
    expect(screen.getByText('变更前')).toBeInTheDocument()
    expect(screen.getByText('变更后')).toBeInTheDocument()
    // JSON 应该是**格式化**过的(可读),不是一坨原始串
    expect(screen.getByText(/"status": "pending"/)).toBeInTheDocument()
    expect(screen.getByText(/"status": "approved"/)).toBeInTheDocument()
  })
})
