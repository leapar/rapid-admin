/**
 * ErrorBoundary — 不依赖 Sentry 的 React 错误边界
 * 2026-10-19 M66 降级 UI / 2026-10-29 M109 拆分 fallback prop + 补 FatalErrorUI
 *
 * 为什么自己写不用 Sentry.ErrorBoundary:
 *   - Sentry.ErrorBoundary 在 DSN 空时退化为 no-op(不传 fallback)
 *   - 即使有 Sentry,本地开发环境也不上报,但仍需 fallback UI
 *   - 业务方可能想在小范围 useErrorBoundary(Suspense 风格)
 *   - Sentry 那个在本仓**零调用方**(2026-10-29 M109 删除),上报统一走 reportError()
 *
 * 设计:
 *   - 类组件实现(getDerivedStateFromError + componentDidCatch)
 *   - 错误时自动调 reportError(Sentry 有就上报,没就 console.error)
 *   - fallback 缺省用内置 <ErrorFallbackUI>(antd Result + 重试/刷新按钮)
 *   - 支持 onReset 回调(业务方重置表单 state)
 *
 * ⚠️ **fallback 与 renderFallback 分开(2026-10-29 M109)**:
 *   此前是一个 union 类型 `ReactNode | ((err, reset) => ReactNode)`,
 *   靠 `typeof fallback === 'function'` 区分两种用法。问题是**组件也是函数** ——
 *   业务方写 `fallback={MyFallback}`(最自然的一种)会被**当普通函数调用**
 *   (`fallback(error, reset)`),组件里的 hooks 全部失效
 *   ⇒ React 抛 "Invalid hook call",兜底 UI 自己把树炸了,整页白屏。
 *   现在:`fallback` 只收**节点**,需要 error / reset 就用 `renderFallback`。
 *   （`ReactNode` 不含函数 —— @types/react 的 `ReactFragment = Iterable<ReactNode>`
 *   —— 所以传组件在**编译期**就会报错,不必等到运行时。）
 *
 * 用法:
 *   <ErrorBoundary>
 *     <SomePage />
 *   </ErrorBoundary>
 *
 *   <ErrorBoundary renderFallback={(err, reset) => <MyFallback err={err} onRetry={reset} />}>
 *     <SomePage />
 *   </ErrorBoundary>
 */
import React from 'react'
import { Button, Result, theme } from 'antd'
import { useI18n } from '@/locales'
import { reportError } from '@/utils/sentry'

interface ErrorBoundaryProps {
  children: React.ReactNode
  /** 自定义 fallback UI(纯节点)。不传则用默认 ErrorFallbackUI */
  fallback?: React.ReactNode
  /**
   * 自定义 fallback 的**渲染函数** —— 需要拿到 error / reset 时用这个。
   * ⚠️ 别把组件直接传给 `fallback`(见文件头),组件请走这里。
   */
  renderFallback?: (err: Error, reset: () => void) => React.ReactNode
  /** 重置时调(业务方清 state) */
  onReset?: () => void
  /** 自定义错误 tag,上报时用 */
  tag?: string
}

interface ErrorBoundaryState {
  error: Error | null
}

export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: React.ErrorInfo): void {
    // 上报(有 Sentry 就走 Sentry,没有就 console.error)
    reportError(error, { tag: this.props.tag, componentStack: info.componentStack })
  }

  reset = (): void => {
    this.props.onReset?.()
    this.setState({ error: null })
  }

  render(): React.ReactNode {
    const { error } = this.state
    if (!error) {
      return this.props.children
    }

    const { fallback, renderFallback } = this.props
    if (renderFallback) {
      return renderFallback(error, this.reset)
    }
    if (fallback !== undefined && fallback !== null) {
      // 运行时兜底:万一传进来的是**组件**(TS 拦不住 JS 调用方 / any 逃逸),
      //   绝不能把它当普通函数调用 —— 那样组件里的 hooks 会炸。
      //   退回默认 UI,并留一条 warn 提示正确用法。
      if (typeof (fallback as unknown) === 'function') {
        console.warn('[ErrorBoundary] fallback 收到的是组件,请改用 renderFallback')
        return <ErrorFallbackUI error={error} reset={this.reset} />
      }
      return fallback
    }
    return <ErrorFallbackUI error={error} reset={this.reset} />
  }
}

/** 默认 fallback UI(全屏 + 重置按钮) */
export const ErrorFallbackUI: React.FC<{
  error: Error
  reset: () => void
}> = ({ error, reset }) => {
  const { t } = useI18n()
  // ⚠️ 这行不是装饰 —— 它是"fallback 有没有继承 ConfigProvider"的可观测点:
  //   错误边界捕获时会**把自己的 children 换掉**渲染 fallback,所以 fallback
  //   拿到的是**边界所在位置**的 provider 上下文。边界若被放在 ConfigProvider
  //   **里面**,fallback 就跑到 provider 外面去了 ⇒ 深色主题用户看到白底错误页。
  //   把 colorPrimary 落到 data-* 上,单测就能断言这件事(见 __tests__/appErrorBoundary.test.tsx)。
  const { token } = theme.useToken()
  return (
    <div
      data-testid="error-fallback"
      data-color-primary={token.colorPrimary}
      style={{
        minHeight: '60vh',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
      }}
    >
      <Result
        status="error"
        title={t('error.boundaryTitle')}
        subTitle={error.message || t('error.unknownError')}
        extra={[
          <Button type="primary" key="reset" onClick={reset}>
            {t('common.retry')}
          </Button>,
          <Button key="reload" onClick={() => window.location.reload()}>
            {t('error.boundaryReload')}
          </Button>,
        ]}
      />
    </div>
  )
}

/**
 * 致命兜底 UI —— **刻意不依赖 antd / ConfigProvider**。
 *
 * 为什么不用 antd Result:这一层存在的意义是"连渲染环境都出问题了"
 * (ConfigProvider 崩了 / antd 自身抛错 / 语言包读不出来)。
 * 用 antd 渲染兜底 = **用坏掉的东西去兜坏掉的东西** —— 会二次抛错,整页白屏,
 * 而白屏是用户能看到的最后一种失败。
 * 所以这里只用最朴素的 HTML + 内联样式,没有任何外部依赖。
 *
 * 渲染位置:应用级边界的**最外层**(在 LangRoot 之外)——
 * 那里连 ConfigProvider 都没有,所以也不能用 antd。
 */
export const FatalErrorUI: React.FC = () => {
  const { t } = useI18n()
  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 12,
        padding: 24,
        fontFamily:
          "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif",
        color: 'var(--app-text)',
        background: 'var(--app-surface-bg)',
      }}
    >
      <h1 style={{ margin: 0, fontSize: 20 }}>{t('error.fatalTitle')}</h1>
      <p style={{ margin: 0, fontSize: 13, color: 'var(--app-text-secondary)', textAlign: 'center' }}>
        {t('error.fatalDesc')}
      </p>
      <button
        type="button"
        onClick={() => window.location.reload()}
        style={{
          marginTop: 8,
          padding: '8px 20px',
          fontSize: 13,
          color: '#fff',
          background: '#f60',
          border: 'none',
          borderRadius: 10,
          cursor: 'pointer',
        }}
      >
        {t('error.boundaryReload')}
      </button>
    </div>
  )
}
