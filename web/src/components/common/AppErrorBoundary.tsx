/**
 * 应用级错误边界装配 —— 三层结构
 *
 * 2026-10-29 M109 新增(原先这段装配直接写在 main.tsx 里)。
 *
 * ## 为什么是三层
 *
 * ```
 * <ErrorBoundary fallback={<FatalErrorUI/>}>   ← ① 最外层安全网
 *   <LangRoot>                                ← ② ConfigProvider(主题 / 语言)
 *     <ErrorBoundary>                         ← ③ 业务边界
 *       <App/>
 *     </ErrorBoundary>
 *   </LangRoot>
 * </ErrorBoundary>
 * ```
 *
 * ① **最外层安全网**:它渲染的 `FatalErrorUI` **刻意不依赖 antd / ConfigProvider**。
 *    理由:这一层存在的意义是"连渲染环境都出问题了"(ConfigProvider 崩了 /
 *    antd 自身抛错)。用 antd 渲染兜底 = 用坏掉的东西兜坏掉的东西 ⇒ 二次抛错、白屏。
 *
 * ② **ConfigProvider 在边界之上**(不是里面):这是 M109 修的一个真 bug。
 *    错误边界捕获时会把**自己的 children 换掉**渲染 fallback —— 所以
 *    fallback 拿到的是**边界所在位置**的 provider 上下文。
 *    原先的装配是 `<SentryErrorBoundary><LocalErrorBoundary><LangRoot><App/></LangRoot></...>`
 *    ⇒ fallback 落在 LangRoot **之外** ⇒ **ConfigProvider 被卸载** ⇒
 *    深色主题用户看到的是**白底**错误页,英文用户看到的是 antd 默认英文。
 *    现在 LangRoot 在边界之上 ⇒ fallback 保留主题与语言。
 *
 * ③ **业务边界**:捕获 App 渲染错误 → `reportError()` 上报(有 Sentry 走 Sentry,
 *    没有就 console.error) + 渲染 antd `ErrorFallbackUI`(带重试 / 刷新按钮)。
 *
 * ## 为什么不再用 Sentry.ErrorBoundary
 *
 * React 的错误边界是**就近捕获**的 —— 只有**最内层**那个边界会拿到错误。
 * 原先的装配里外层是 Sentry 边界、内层是本地边界 ⇒
 *   · App 的错误**永远到不了** Sentry 边界(被内层先截住了);
 *   · 所以 main.tsx 那句注释"外层 SentryErrorBoundary:有 Sentry 时上报"是**假的** ——
 *     真正在上报的是内层边界的 `reportError`;
 *   · 而外层边界的 fallback 写的是 `() => <LocalErrorBoundary><App/></LocalErrorBoundary>`
 *     —— 它**重挂整棵 App 且丢掉 LangRoot**(主题/语言全丢),还得再套一层边界。
 * 现在只有本地边界一个,上报路径唯一(`reportError`),外层安全网用纯 HTML 兜底。
 */
import type React from 'react'
import { ErrorBoundary, FatalErrorUI } from './ErrorBoundary'
import { LangRoot } from './LangRoot'

export function AppErrorBoundary({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <ErrorBoundary fallback={<FatalErrorUI />}>
      <LangRoot>
        <ErrorBoundary>{children}</ErrorBoundary>
      </LangRoot>
    </ErrorBoundary>
  )
}
