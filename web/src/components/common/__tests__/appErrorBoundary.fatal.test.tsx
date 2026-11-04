/**
 * AppErrorBoundary 的**最外层安全网**(2026-10-29 M109)
 *
 * 最外层存在的意义是"连渲染环境都出问题了"。要逼出它,必须让 **LangRoot 自己抛错**
 * —— 因为 LangRoot 在内层边界**之外**,它抛错时内层边界根本看不到。
 * 所以这里 mock 掉 LangRoot。
 *
 * 锁住的:
 *   ① LangRoot 抛错 → 渲染 FatalErrorUI(纯 HTML,不依赖 antd);
 *   ② **最外层兜底不会重挂整棵 App** —— 这是 M109 修的 P1:
 *      旧的装配里外层边界的 fallback 写的是
 *      `() => <LocalErrorBoundary><App /></LocalErrorBoundary>`
 *      ⇒ 它会把 App **整个重新挂一遍**(状态全丢、副作用重跑),
 *        而且**丢掉了 LangRoot**(主题 / 语言全没)。
 *      现在最外层只渲染 FatalErrorUI —— 不重挂、不依赖任何 provider。
 *   ③ 错误照样上报(不是静默吞掉)。
 */
import type React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'

const reportErrorMock = vi.hoisted(() => vi.fn())
vi.mock('@/utils/sentry', () => ({ reportError: reportErrorMock }))

// mock 掉 LangRoot:让"provider 层"自己炸,逼出最外层安全网
vi.mock('@/components/common/LangRoot', () => ({
  LangRoot: (): React.ReactElement => {
    throw new Error('ConfigProvider 挂了')
  },
}))

import { AppErrorBoundary } from '@/components/common/AppErrorBoundary'

let mountCount = 0

function App(): React.ReactElement {
  mountCount += 1
  return <div>业务页面</div>
}

beforeEach(() => {
  vi.clearAllMocks()
  mountCount = 0
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AppErrorBoundary · 最外层安全网', () => {
  it('provider 层抛错 → 渲染 FatalErrorUI(纯 HTML)', () => {
    render(
      <AppErrorBoundary>
        <App />
      </AppErrorBoundary>,
    )

    expect(screen.getByText('应用遇到严重错误')).toBeInTheDocument()
    // 纯 HTML:没有 antd 的类名
    expect(document.querySelector('.ant-result')).toBeNull()
  })

  it('最外层兜底**不重挂整棵 App**', () => {
    render(
      <AppErrorBoundary>
        <App />
      </AppErrorBoundary>,
    )

    // 旧实现会把 App 再挂一遍(嵌套 LocalErrorBoundary + <App/>),
    // 于是 mountCount 会 ≥1;现在 App 一次都没挂上。
    expect(mountCount).toBe(0)
    expect(screen.queryByText('业务页面')).not.toBeInTheDocument()
  })

  it('这一层的错误照样上报(不是静默吞掉)', () => {
    render(
      <AppErrorBoundary>
        <App />
      </AppErrorBoundary>,
    )

    expect(reportErrorMock).toHaveBeenCalled()
    const [err] = reportErrorMock.mock.calls[0] as [Error]
    expect(err.message).toBe('ConfigProvider 挂了')
  })
})
