/**
 * components/common/ErrorBoundary.tsx 回归网(2026-10-29 M109)
 *
 * 锁住的:
 *   ① 子组件抛错 → 渲染默认 fallback(antd Result,带错误文案),并调 reportError;
 *   ② `fallback` 传**节点** → 原样渲染;
 *   ③ `renderFallback` 传**渲染函数** → 拿到 (error, reset);
 *   ④ 点"重试" → onReset 被调用,children 重新渲染;
 *   ⑤ **`fallback` 传组件时不会被当普通函数调用**(M109 P2):
 *      此前是一个 union 类型 `ReactNode | ((err, reset) => ReactNode)`,
 *      靠 `typeof fallback === 'function'` 区分 —— 而组件**也是函数**,
 *      于是 `fallback(error, reset)` 把组件当普通函数调 ⇒ 组件里的 hooks 全废
 *      ⇒ React 抛 Invalid hook call ⇒ **兜底 UI 自己把树炸了,整页白屏**。
 *      现在 fallback 只收节点、函数走 renderFallback;万一还是传了函数,
 *      退回默认 UI 并 warn,而不是炸掉整棵树。
 */
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'

const reportErrorMock = vi.hoisted(() => vi.fn())
vi.mock('@/utils/sentry', () => ({ reportError: reportErrorMock }))

import { ErrorBoundary, ErrorFallbackUI, FatalErrorUI } from '@/components/common/ErrorBoundary'

function Thrower({ error }: { error?: Error }): React.ReactElement {
  throw error ?? new Error('业务组件炸了')
}

beforeEach(() => {
  vi.clearAllMocks()
  // React 会把捕获到的错误打到 console.error —— 噪音压掉,不影响断言
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ErrorBoundary · 基本行为', () => {
  it('没有错误时正常渲染 children', () => {
    render(
      <ErrorBoundary>
        <div>页面内容</div>
      </ErrorBoundary>,
    )

    expect(screen.getByText('页面内容')).toBeInTheDocument()
    expect(reportErrorMock).not.toHaveBeenCalled()
  })

  it('子组件抛错 → 渲染默认 fallback,并把错误上报出去', () => {
    render(
      <ErrorBoundary tag="page">
        <Thrower />
      </ErrorBoundary>,
    )

    expect(screen.getByTestId('error-fallback')).toBeInTheDocument()
    expect(screen.getByText('业务组件炸了')).toBeInTheDocument()

    // 上报:带 tag 和 componentStack
    expect(reportErrorMock).toHaveBeenCalledTimes(1)
    const [err, ctx] = reportErrorMock.mock.calls[0] as [Error, Record<string, unknown>]
    expect(err.message).toBe('业务组件炸了')
    expect(ctx.tag).toBe('page')
    expect(typeof ctx.componentStack).toBe('string')
  })
})

describe('ErrorBoundary · 自定义 fallback', () => {
  it('fallback 传节点 → 原样渲染', () => {
    render(
      <ErrorBoundary fallback={<div>自定义兜底</div>}>
        <Thrower />
      </ErrorBoundary>,
    )

    expect(screen.getByText('自定义兜底')).toBeInTheDocument()
    expect(screen.queryByTestId('error-fallback')).not.toBeInTheDocument()
  })

  it('renderFallback 传渲染函数 → 拿到 error 和 reset', () => {
    const seen: Array<{ message: string; hasReset: boolean }> = []

    render(
      <ErrorBoundary
        renderFallback={(err, reset) => {
          seen.push({ message: err.message, hasReset: typeof reset === 'function' })
          return <div>渲染函数兜底</div>
        }}
      >
        <Thrower />
      </ErrorBoundary>,
    )

    expect(screen.getByText('渲染函数兜底')).toBeInTheDocument()
    // ⚠️ React 捕获错误后会重渲染边界,renderFallback 可能被调不止一次 ——
    //   断言"至少被调过 + 参数正确",不去数次数(那是 React 的实现细节)。
    expect(seen.length).toBeGreaterThan(0)
    expect(seen[0]).toEqual({ message: '业务组件炸了', hasReset: true })
    for (const call of seen) {
      expect(call).toEqual({ message: '业务组件炸了', hasReset: true })
    }
  })

  it('fallback 传组件时**不会**被当普通函数调用(退回默认 UI + warn)', () => {
    // ⚠️ 这条锁的是 M109 修的 P2:组件也是函数,旧写法会 `fallback(error, reset)`
    //   直接调用它 —— 组件里的 useState 会在渲染期外执行 ⇒ Invalid hook call
    //   ⇒ 兜底 UI 自己把 React 树炸掉。
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})

    function ComponentFallback(): React.ReactElement {
      const [n] = React.useState(1)
      return <div>组件兜底 {n}</div>
    }

    render(
      <ErrorBoundary fallback={ComponentFallback as unknown as React.ReactNode}>
        <Thrower />
      </ErrorBoundary>,
    )

    // 没有炸树,退回了默认 UI
    expect(screen.getByTestId('error-fallback')).toBeInTheDocument()
    expect(screen.queryByText(/组件兜底/)).not.toBeInTheDocument()
    expect(warnSpy).toHaveBeenCalledWith(
      '[ErrorBoundary] fallback 收到的是组件,请改用 renderFallback',
    )
  })
})

describe('ErrorBoundary · 重置', () => {
  it('点"重试" → 调 onReset,并把 children 重新渲染出来', () => {
    const onReset = vi.fn()
    let shouldThrow = true

    function MaybeThrow(): React.ReactElement {
      if (shouldThrow) throw new Error('第一次炸')
      return <div>恢复了</div>
    }

    render(
      <ErrorBoundary onReset={onReset}>
        <MaybeThrow />
      </ErrorBoundary>,
    )

    expect(screen.getByTestId('error-fallback')).toBeInTheDocument()

    shouldThrow = false
    // ⚠️ antd 5 会在汉字之间自动插空格("重试" → "重 试"),匹配要用正则
    fireEvent.click(screen.getByText(/重\s*试/))

    expect(onReset).toHaveBeenCalledTimes(1)
    expect(screen.getByText('恢复了')).toBeInTheDocument()
    expect(screen.queryByTestId('error-fallback')).not.toBeInTheDocument()
  })
})

describe('ErrorBoundary · 兜底 UI 自身', () => {
  it('ErrorFallbackUI 显示错误文案', () => {
    render(<ErrorFallbackUI error={new Error('具体错误')} reset={() => {}} />)

    expect(screen.getByText('具体错误')).toBeInTheDocument()
  })

  it('FatalErrorUI 是纯 HTML(不依赖 antd),并且能刷新页面', () => {
    const reload = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, reload },
    })

    render(<FatalErrorUI />)

    expect(screen.getByText('应用遇到严重错误')).toBeInTheDocument()
    expect(screen.getByText('刷新页面')).toBeInTheDocument()
    // 关键:纯 HTML —— 没有任何 antd 的类名
    expect(document.querySelector('.ant-result')).toBeNull()
  })
})
