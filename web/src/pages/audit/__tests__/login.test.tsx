/**
 * 登录日志页(2026-10-30)
 *
 * ## 这条网守的是什么
 *
 *   1. **页面必须读 snake_case 的 wire 名**(`session_id` / `login_at` / `ip` …)。
 *      读成驼峰(`sessionId`)的话,TypeScript **不会报错**(类型是自己写的),
 *      而那一列**永远是空的** —— 2026-10-30 之前的操作日志页就是这么坏的
 *      (读 `requestUrl`,后端返 `request_url`)。
 *      ⇒ 这里直接断言"wire 里的值出现在表格里",而不是断言源码里写了什么字段名。
 *
 *   2. **成功/失败筛选必须发 `success`,不能发 `status`**。
 *      后端 `LoginLogQuery.Success` 是 `*bool`,读的是 `c.Query("success")`;
 *      发 `status=0` 会被**完全忽略** —— HTTP 200、结果不变、不报错。
 *      这是"筛选控件看着能点、其实什么都没筛"那类静默失效。
 *
 *   3. **"不筛"必须是真的不筛** —— 首屏不能带 `success`,
 *      选完再点重置也要把 `success` 摘掉(而不是留一个 `false` 变成"只看失败")。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把列的 dataIndex 改成 `sessionId` → 「表格读的是 wire 上的 session_id」红;
 *   · 把 browser / os 两列删掉 → 「browser / os 两列显示后端解析出来的值」红;
 *   · 把 onChange 里的 `success` 改成 `status` → 「失败筛选发 success」红;
 *   · 把 Select 的 value 改成字符串 '0' → 「success 是布尔」红;
 *   · 把 reset 去掉 → 「重置后不筛」红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import dayjs from 'dayjs'
import LoginLogList from '../login'
import { auditApi } from '@/api/audit'
import { useDictStore } from '@/stores/dict'

vi.mock('@/api/audit', () => ({
  auditApi: { loginLogs: vi.fn() },
}))

const loginLogsMock = vi.mocked(auditApi.loginLogs)

/** waitFor 超时 —— 显式给,别用默认的 1000ms(机器一忙就集体假红) */
const WAIT = { timeout: 10_000 }

/**
 * 字典种子
 *
 * ⚠️ 必须种下去,否则 `useDict` 会去发真实请求(axios → jsdom → 网络错误),
 *   状态标签就退化成裸数字,断言"成功/失败"文案会变成一句废话。
 */
const DICT_AUDIT_OP_STATUS = [
  {
    id: 'd1',
    group_code: 'audit_op_status',
    item_key: 'success',
    item_value: '1',
    label: '成功',
    sort: 1,
    is_active: true,
    description: 'color:green',
  },
  {
    id: 'd2',
    group_code: 'audit_op_status',
    item_key: 'fail',
    item_value: '0',
    label: '失败',
    sort: 2,
    is_active: true,
    description: 'color:red',
  },
]

/** 一行登录日志(wire 上的 snake_case 形状) */
const ROW = {
  id: 'l1',
  user_id: '42',
  username: 'zhangsan',
  ip: '10.0.0.9',
  location: '',
  device: 'Mozilla/5.0 (Windows NT 10.0)',
  browser: 'Chrome 120',
  os: 'Windows 10/11',
  status: 1,
  message: '登录成功',
  login_at: '2026-10-30T08:00:00+08:00',
  logout_at: null,
  session_id: 'sid-abc123',
  last_active_at: null,
  created_at: '2026-10-30T08:00:00+08:00',
}

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <LoginLogList />
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

describe('登录日志页', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useDictStore.setState({
      dicts: { audit_op_status: DICT_AUDIT_OP_STATUS },
      lastFetchAt: { audit_op_status: 1 },
    })
    loginLogsMock.mockResolvedValue({ list: [ROW], total: 1, page: 1, size: 20 } as never)
  })

  it('首屏拉列表,且**不带**任何筛选(不筛 = 不传,不是传空串)', async () => {
    renderPage()
    await waitFor(() => expect(loginLogsMock).toHaveBeenCalled(), WAIT)

    const q = loginLogsMock.mock.calls[0]![0]
    expect(q).toMatchObject({ page: 1, size: 20 })
    // ⚠️ 首屏带一个 success: undefined 也无所谓,但带 success: false 就等于
    //   "只看失败" —— 首屏不该有这个。
    expect(q.success).toBeUndefined()
  })

  it('表格读的是 wire 上的 snake_case 字段(不是驼峰)', async () => {
    renderPage()
    await waitFor(() => expect(loginLogsMock).toHaveBeenCalled(), WAIT)
    await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument(), WAIT)

    // session_id / ip —— 读成驼峰的话这两列会是空的
    expect(screen.getByText('sid-abc123')).toBeInTheDocument()
    expect(screen.getByText('10.0.0.9')).toBeInTheDocument()
    // login_at —— 时间用 dayjs 算期望值,免得断言随机器时区飘
    expect(screen.getByText(dayjs(ROW.login_at).format('YYYY-MM-DD HH:mm:ss'))).toBeInTheDocument()
  })

  it('browser / os 两列显示后端解析出来的值(2026-10-30 起才有值)', async () => {
    renderPage()
    await waitFor(() => expect(loginLogsMock).toHaveBeenCalled(), WAIT)
    await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument(), WAIT)

    // ⚠️ 这两列此前**后端从来不写** ⇒ 摆出来永远是空白。
    //   现在后端从 UA 解析(见 server/internal/pkg/uainfo),页面必须显示出来。
    expect(screen.getByText('Chrome 120')).toBeInTheDocument()
    expect(screen.getByText('Windows 10/11')).toBeInTheDocument()
  })

  it('失败筛选发的是 success=false(后端只读 success,发 status 会被忽略)', async () => {
    renderPage()
    await waitFor(() => expect(loginLogsMock).toHaveBeenCalled(), WAIT)

    await openSelect()
    await pickOption('失败')

    await waitFor(() => expect(loginLogsMock).toHaveBeenCalledTimes(2), WAIT)
    const q = loginLogsMock.mock.calls[1]![0]
    // ⚠️ 必须是**布尔 false** —— 发字符串 'false' 后端 `queryOptionalBool`
    //   认不出来(它只认 "true"/"1"/"false"/"0"),结果是"不筛"。
    expect(q.success).toBe(false)
    expect(q).not.toHaveProperty('status')
  })

  it('选"成功"发的是 success=true', async () => {
    renderPage()
    await waitFor(() => expect(loginLogsMock).toHaveBeenCalled(), WAIT)

    await openSelect()
    await pickOption('成功')

    await waitFor(() => expect(loginLogsMock).toHaveBeenCalledTimes(2), WAIT)
    expect(loginLogsMock.mock.calls[1]![0].success).toBe(true)
  })

  it('关键字搜索带上 keyword', async () => {
    renderPage()
    await waitFor(() => expect(loginLogsMock).toHaveBeenCalled(), WAIT)

    const input = screen.getByPlaceholderText('搜索用户名')
    fireEvent.change(input, { target: { value: 'zhang' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })

    await waitFor(() => expect(loginLogsMock).toHaveBeenCalledTimes(2), WAIT)
    expect(loginLogsMock.mock.calls[1]![0]).toMatchObject({ keyword: 'zhang', page: 1 })
  })

  it('点重置 → 筛选全清(不能留一个 success:false 变成"只看失败")', async () => {
    renderPage()
    await waitFor(() => expect(loginLogsMock).toHaveBeenCalled(), WAIT)

    await openSelect()
    await pickOption('失败')
    await waitFor(() => expect(loginLogsMock).toHaveBeenCalledTimes(2), WAIT)

    fireEvent.click(screen.getByText('重置'))

    await waitFor(() => expect(loginLogsMock).toHaveBeenCalledTimes(3), WAIT)
    const q = loginLogsMock.mock.calls[2]![0]
    expect(q.success).toBeUndefined()
    expect(q.keyword).toBeUndefined()
  })

  it('点"详情" → 抽屉打开,能看到会话 ID / 登录时间', async () => {
    renderPage()
    await waitFor(() => expect(loginLogsMock).toHaveBeenCalled(), WAIT)
    await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument(), WAIT)

    // ⚠️ antd 5 的 Button 会在两个汉字之间插空格 → 用正则而不是全等
    fireEvent.click(screen.getByRole('button', { name: /详\s*情/ }))

    await waitFor(() => expect(screen.getByText('登录日志详情')).toBeInTheDocument(), WAIT)

    // ⚠️ 必须**在抽屉里**找 —— '会话 ID' / '登录时间' 同时也是表头,
    //   全局 getByText 会命中两个(表格列头 + 抽屉字段名)→ 报"找到多个"。
    const drawer = document.querySelector('.ant-drawer')
    expect(drawer).toBeTruthy()
    const inDrawer = within(drawer as HTMLElement)
    expect(inDrawer.getByText('会话 ID')).toBeInTheDocument()
    expect(inDrawer.getByText('登录时间')).toBeInTheDocument()
    expect(inDrawer.getByText('sid-abc123')).toBeInTheDocument()
  })
})
