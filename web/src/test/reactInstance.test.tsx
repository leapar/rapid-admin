/**
 * React 实例唯一性回归测试
 *
 * ⚠️ **这个测试守的是一条「看不见的纪律」**:整个进程里必须只有**一份** react。
 *   一旦变成两份,症状是 `Invalid hook call` / `Cannot read properties of null
 *   (reading 'useMemo')`,但**报错点全在业务组件里**,跟真正的原因隔了三层,
 *   排查成本极高(本项目就为此烧掉了一整轮)。
 *
 * 判据:react-dom 渲染时会往 **共享的 ReactSharedInternals** 上挂
 *   `ReactCurrentDispatcher`。这份 internals 由 react 导出:
 *     React.__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED
 *   ⇒ 渲染结束后,应用侧看到的 dispatcher **必须已经被挂上**(非 null)。
 *   双 React 时 react-dom 挂到了另一份实例上,这边恒为 null。
 *
 * 曾经的真实故障(Windows 专属):
 *   vite 解析应用源码的 `import 'react'` 用的是 root 字符串的大小写,
 *   react-dom 内部 `require('react')` 用的是 Node CJS 解析器 realpath 后的
 *   盘符大写形式 —— 同一个文件、两个路径字符串 ⇒ Node 加载了两份。
 *   详见 `vitest.config.ts` 里 CANONICAL_ROOT 的注释。
 */
import { describe, it, expect } from 'vitest'
import { useMemo, useState } from 'react'
import { render, screen } from '@testing-library/react'
import * as React from 'react'

type SharedInternals = {
  ReactCurrentDispatcher: { current: unknown }
}

const sharedInternals = (
  React as unknown as {
    __SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED: SharedInternals
  }
).__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED

function Probe() {
  const [n] = useState(7)
  const doubled = useMemo(() => n * 2, [n])
  return <span data-testid="probe">{doubled}</span>
}

describe('React 实例唯一性', () => {
  it('组件里的 hook 能正常派发(双 React 时这里会抛 useState/useMemo 读 null)', () => {
    render(<Probe />)
    expect(screen.getByTestId('probe').textContent).toBe('14')
  })

  it('渲染结束后 react-dom 已把 dispatcher 挂到应用侧这份 react 上', () => {
    // 先渲染一次 —— dispatcher 是渲染时挂的,不渲染就一直是 null
    render(<Probe />)

    expect(sharedInternals.ReactCurrentDispatcher.current).not.toBeNull()
  })
})
