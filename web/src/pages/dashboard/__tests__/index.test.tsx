/**
 * 工作台(仪表盘)页
 *
 * ## 这一组守的是什么
 *
 *   1. **KPI 必须读 wire 上的 snake_case**(`file_total` / `user_online` …)。
 *      读成驼峰的话 TypeScript **不会报错**(类型是前端自己写的),
 *      而卡片**永远是 "-"**。
 *      ⇒ 这里断言"wire 里的数字出现在页面上",而不是断言源码里写了什么字段名。
 *
 *   2. **服务端按域权限过滤看板内容** —— 没权限的域
 *      **压根不出现在响应里**(字段缺省)⇒ 前端**只画服务端给了的那些**。
 *      这组用例守的就是"少了不画 / 多了也不画 / 0 也要画"。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `web/src/api/dashboard.ts` 的 `file_total` 改名成 `fileTotal`
 *     → 「KPI 读的是 snake_case」红;
 *   · 把页面里的 `overview?.[s.key] !== undefined` 改成真值判断
 *     → 「值为 0 的指标照样要画」红(0 被当成"没有");
 *   · 把 `visibleKpis` 的过滤去掉(全画) → 「没权限的域不画」红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import Dashboard from '../index'
import { dashboardApi } from '@/api/dashboard'

vi.mock('@/api/dashboard', () => ({
  dashboardApi: {
    overview: vi.fn(),
    loginHeatmap: vi.fn(),
    serverMetrics: vi.fn(),
  },
}))

const overviewMock = vi.mocked(dashboardApi.overview)
const heatmapMock = vi.mocked(dashboardApi.loginHeatmap)

/** waitFor 超时 —— 显式给,别用默认的 1000ms(机器一忙就集体假红) */
const WAIT = { timeout: 10_000 }

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <Dashboard />
      </MemoryRouter>
    </App>,
  )
}

describe('工作台(仪表盘)页', () => {
  beforeEach(() => {
    vi.clearAllMocks()

    overviewMock.mockResolvedValue({
      file_total: 101,
      file_today: 102,
      folder_total: 103,
      user_online: 104,
    } as never)
    heatmapMock.mockResolvedValue([] as never)
  })

  it('KPI 卡片读的是 wire 上的 snake_case(读驼峰就恒为 "-")', async () => {
    renderPage()
    await waitFor(() => expect(overviewMock).toHaveBeenCalled(), WAIT)

    // 给了的指标一个都不能少
    for (const v of ['101', '102', '103', '104']) {
      await waitFor(() => expect(screen.getByText(v)).toBeInTheDocument(), WAIT)
    }
    // 兜底值 "-" 不该出现(出现了 = 读到 undefined)
    expect(screen.queryByText('-')).toBeNull()
  })

  it('概览 / 热力图接口都被调到了', async () => {
    renderPage()
    await waitFor(() => expect(overviewMock).toHaveBeenCalled(), WAIT)
    await waitFor(() => expect(heatmapMock).toHaveBeenCalled(), WAIT)
  })

  // ===== 服务端按域权限过滤 =====

  it('服务端只给了有权限的域 → 只画那几张卡片(没给的域连标题都不出现)', async () => {
    overviewMock.mockResolvedValue({
      file_total: 101,
      user_online: 104,
    } as never)

    renderPage()
    await waitFor(() => expect(overviewMock).toHaveBeenCalled(), WAIT)

    // 有权限的两个指标要画
    await waitFor(() => expect(screen.getByText('101')).toBeInTheDocument(), WAIT)
    expect(screen.getByText('104')).toBeInTheDocument()
    expect(screen.getByText('文件总数')).toBeInTheDocument()

    // 没权限的域:**标题都不该出现**(不是显示成 "-")
    expect(screen.queryByText('文件夹总数')).toBeNull()
    expect(screen.queryByText('存储占用')).toBeNull()
  })

  it('一个域都看不到 → 给"没有可查看的看板数据"的空态(不是加载失败)', async () => {
    overviewMock.mockResolvedValue({} as never)

    renderPage()
    await waitFor(() => expect(overviewMock).toHaveBeenCalled(), WAIT)

    await waitFor(
      () => expect(screen.getByText('当前身份没有可查看的看板数据')).toBeInTheDocument(),
      WAIT,
    )
    expect(screen.queryByText('加载失败')).toBeNull()
    // 一张 KPI 卡片都不该有
    expect(screen.queryByText('文件总数')).toBeNull()
  })

  it('值为 0 的指标照样要画(0 ≠ 没有 —— 真值判断会把 0 当成"看不见")', async () => {
    overviewMock.mockResolvedValue({ user_online: 0 } as never)

    renderPage()
    await waitFor(() => expect(overviewMock).toHaveBeenCalled(), WAIT)

    await waitFor(() => expect(screen.getByText('当前在线')).toBeInTheDocument(), WAIT)
    expect(screen.getByText('0')).toBeInTheDocument()
    // 没权限的域还是不画
    expect(screen.queryByText('文件总数')).toBeNull()
  })

  it('概览接口失败 → 给"加载失败"空态(不铺 "-" 的假卡片)', async () => {
    overviewMock.mockRejectedValue(new Error('boom'))

    renderPage()
    await waitFor(() => expect(overviewMock).toHaveBeenCalled(), WAIT)

    await waitFor(() => expect(screen.getByText('加载失败')).toBeInTheDocument(), WAIT)
    expect(screen.queryByText('文件总数')).toBeNull()
  })
})
