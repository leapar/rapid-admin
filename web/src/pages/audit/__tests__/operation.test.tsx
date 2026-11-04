/**
 * 操作日志页(2026-10-30 M126)
 *
 * ## 这条网守的是什么
 *
 *   M126 在服务端补上了**审计采集端**(server/internal/api/middleware/audit_capture.go),
 *   `sys_operation_log` 的 `request_body` / `request_params` /
 *   `response_summary` / `location` 四列**从此有值**。
 *
 *   页面这一侧要跟上两件事:
 *
 *   1. **抽屉里要真的有 `request_body` 这一行** ——
 *      后端写了、页面不显示 = 白写(前端那四行此前还挂着
 *      "后端目前不写这一列"的注释,注释比功能先过期)。
 *
 *   2. **抽屉里**仍然不能**有 `request_method` / `cost_ms`** ——
 *      它们分别是 `action` / `duration` 的**同值副本**
 *      (中间件故意不写 request_method;cost_ms 和 duration 写的是同一个
 *      `time.Since(start)`)。展示它们只会让人以为是三个不同的量。
 *      ⇒ 这两条是**反向守卫**:防止后人"顺手补全字段"把它们加回来。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `request_body` 那行的 `name` 改成 `requestBody`(驼峰)
 *     → 「抽屉里有请求体这一行」红(值取不到,显示「空值」);
 *   · 把 `request_body` 那行整个删掉 → 同上红;
 *   · 把 `request_method` / `cost_ms` 加回抽屉 → 「故意不放的两列」红;
 *   · 把文案从 `t('page.auditOperation.requestBody')` 改成写死中文
 *     → 「文案来自 i18n」红(切到 en-US 还是中文);
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import OperationLogList from '../operation'
import { auditApi } from '@/api/audit'
import { useDictStore } from '@/stores/dict'
import { useUIStore } from '@/stores/ui'

vi.mock('@/api/audit', () => ({
  auditApi: { operationLogs: vi.fn() },
}))

const operationLogsMock = vi.mocked(auditApi.operationLogs)

/** waitFor 超时 —— 显式给,别用默认的 1000ms(机器一忙就集体假红) */
const WAIT = { timeout: 10_000 }

/**
 * 字典种子 —— `audit_op_status`(1 成功 / 0 失败)
 *
 * ⚠️ 必须种下去,否则 `useDict` 会去发真实请求(axios → jsdom → 网络错误)。
 */
const DICT_AUDIT_OP_STATUS = [
  {
    id: 's1',
    group_code: 'audit_op_status',
    item_key: 'success',
    item_value: '1',
    label: '成功',
    sort: 1,
    is_active: true,
    description: 'color:green',
  },
  {
    id: 's2',
    group_code: 'audit_op_status',
    item_key: 'failed',
    item_value: '0',
    label: '失败',
    sort: 2,
    is_active: true,
    description: 'color:red',
  },
]

/** 一行操作日志(wire 上的 snake_case 形状) */
const ROW = {
  id: 'op1',
  user_id: '42',
  username: 'zhangsan',
  user_role: 'admin',
  module: '/rapid-admin/v1/system/users',
  action: 'PATCH',
  description: '',
  request_url: '/rapid-admin/v1/system/users/9527?force=1',
  // ⚠️ `request_method` 与 `action` 同值(都是 HTTP 方法)⇒ 中间件故意不写它。
  //   这里塞一个**标记值**:抽屉里如果出现了它,说明有人把这列加回来了。
  request_method: 'REQ_METHOD_MARKER',
  // 后端 M126 起会写这一列(脱敏后的请求体)
  request_body: '{"username":"alice","password":"***"}',
  request_params: 'force=1',
  response_summary: '{"code":0,"data_keys":["username","password"]}',
  ip: '10.0.0.1',
  location: 'id=9527',
  user_agent: 'UA_MARKER',
  duration: 42,
  // ⚠️ `cost_ms` 与 `duration` 同值 ⇒ 抽屉里不展示它。
  //   这里塞一个**标记值**:出现了就是有人把它加回来了。
  cost_ms: 9999,
  status: 1,
  error_msg: '',
  trace_id: 'trace-1',
  request_id: 'req-1',
  created_at: '2026-10-30T09:00:00+08:00',
}

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <OperationLogList />
      </MemoryRouter>
    </App>,
  )
}

/**
 * 打开详情抽屉
 *
 * ⚠️ "详情"按钮的文案**也**来自 i18n(`common.detail`)⇒ 切到 en-US 之后
 *   它叫 "Detail"。所以按钮名要跟着当前语言走,不能写死。
 */
async function openDrawer(detailLabel = /详\s*情/, drawerTitle = '操作日志详情') {
  await waitFor(() => expect(screen.getByText('zhangsan')).toBeInTheDocument(), WAIT)
  fireEvent.click(screen.getByRole('button', { name: detailLabel }))
  await waitFor(() => expect(screen.getByText(drawerTitle)).toBeInTheDocument(), WAIT)
}

describe('操作日志页', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useUIStore.setState({ lang: 'zh-CN' })
    useDictStore.setState({
      dicts: { audit_op_status: DICT_AUDIT_OP_STATUS },
      lastFetchAt: { audit_op_status: 1 },
    })
    operationLogsMock.mockResolvedValue({ list: [ROW], total: 1, page: 1, size: 20 } as never)
  })

  it('抽屉里有「请求体」这一行,而且显示的是后端给的脱敏串', async () => {
    renderPage()
    await openDrawer()

    expect(screen.getByText('请求体')).toBeInTheDocument()
    // ⚠️ 读的是 wire 上的 `request_body` —— 改成驼峰(`requestBody`)这行就空
    expect(screen.getByText('{"username":"alice","password":"***"}')).toBeInTheDocument()
  })

  it('M126 起这四行都有值(此前每一行都是空的)', async () => {
    renderPage()
    await openDrawer()

    expect(screen.getByText('请求参数')).toBeInTheDocument()
    expect(screen.getByText('force=1')).toBeInTheDocument()

    expect(screen.getByText('响应摘要')).toBeInTheDocument()
    expect(screen.getByText('{"code":0,"data_keys":["username","password"]}')).toBeInTheDocument()

    expect(screen.getByText('位置')).toBeInTheDocument()
    expect(screen.getByText('id=9527')).toBeInTheDocument()
  })

  it('故意不放的两列(request_method / cost_ms)不能在抽屉里', async () => {
    renderPage()
    await openDrawer()

    // 与 action / duration 是同值副本 —— 展示它们只会让人以为是三个不同的量
    expect(screen.queryByText('REQ_METHOD_MARKER')).toBeNull()
    expect(screen.queryByText('9999')).toBeNull()
  })

  it('文案来自 i18n(不是前端写死的中文)', async () => {
    useUIStore.setState({ lang: 'en-US' })
    renderPage()
    // ⚠️ 按钮文案本身也走 i18n(`common.detail` = "Detail"),抽屉标题同理
    await openDrawer(/Detail/, 'Operation Log Detail')

    expect(screen.getByText('Request Body')).toBeInTheDocument()
    expect(screen.queryByText('请求体')).toBeNull()
  })
})
