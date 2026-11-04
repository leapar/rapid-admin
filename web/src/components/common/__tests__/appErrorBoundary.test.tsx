/**
 * components/common/AppErrorBoundary.tsx 回归网(2026-10-29 M109)
 *
 * 锁住的是**装配顺序** —— 它决定"出错了显示什么":
 *
 *   ① App 抛错 → 渲染**业务边界**的 fallback(antd ErrorFallbackUI),
 *      而且这个 fallback **在 LangRoot 的 ConfigProvider 之内** ⇒ 继承主题/语言。
 *      回归:旧的装配是 `<边界><边界><LangRoot><App/></LangRoot></边界></边界>`
 *      —— 边界捕获时会把自己的 children 换掉,所以 fallback 落到 LangRoot
 *      **外面** ⇒ ConfigProvider 被卸载 ⇒ 深色主题用户看到**白底**错误页。
 *      断言方式:ErrorFallbackUI 把 colorPrimary 落在 data-color-primary 上
 *      (LangRoot 的品牌色 = '#f60';provider 之外则是 antd 默认的 '#1677ff')。
 *
 *   ② 正常时不报错、不上报。
 *
 * 「最外层安全网」那条(外层兜底不重挂 App)放在
 * `appErrorBoundary.fatal.test.tsx`(那边 mock 掉了 LangRoot 才能逼出外层)。
 */
import type React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'

const reportErrorMock = vi.hoisted(() => vi.fn())
vi.mock('@/utils/sentry', () => ({ reportError: reportErrorMock }))

import { AppErrorBoundary } from '@/components/common/AppErrorBoundary'

function Thrower(): React.ReactElement {
  throw new Error('App 渲染炸了')
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AppErrorBoundary · 装配', () => {
  it('App 抛错 → 渲染业务边界的 fallback(不是最外层的致命兜底)', () => {
    render(
      <AppErrorBoundary>
        <Thrower />
      </AppErrorBoundary>,
    )

    expect(screen.getByTestId('error-fallback')).toBeInTheDocument()
    expect(screen.getByText('App 渲染炸了')).toBeInTheDocument()
    // 最外层的 FatalErrorUI 不该出现
    expect(screen.queryByText('应用遇到严重错误')).not.toBeInTheDocument()
    // 错误上报过一次
    expect(reportErrorMock).toHaveBeenCalledTimes(1)
  })

  it('fallback **继承** LangRoot 的 ConfigProvider(深色主题下不是白底页)', () => {
    render(
      <AppErrorBoundary>
        <Thrower />
      </AppErrorBoundary>,
    )

    const fallback = screen.getByTestId('error-fallback')
    // LangRoot 的品牌色写的是 '#f60',antd 会把它规范化成 6 位十六进制 '#ff6600';
    // 如果 fallback 掉到了 provider 外面,拿到的就是 antd 默认色 '#1677ff'。
    expect(fallback.getAttribute('data-color-primary')).toBe('#ff6600')
    expect(fallback.getAttribute('data-color-primary')).not.toBe('#1677ff')
  })

  it('没有错误时正常渲染 children,不触发上报', () => {
    render(
      <AppErrorBoundary>
        <div>业务页面</div>
      </AppErrorBoundary>,
    )

    expect(screen.getByText('业务页面')).toBeInTheDocument()
    expect(screen.queryByTestId('error-fallback')).not.toBeInTheDocument()
    expect(reportErrorMock).not.toHaveBeenCalled()
  })
})
