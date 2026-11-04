/**
 * TableErrorState / FallbackPlaceholder 回归网(2026-10-29 M111)
 *
 * 这是 M6/M7 那条"**状态没人看等于没有**"链路的落地端:
 * useTable 把失败记进 `error` → 外壳把它渲染成用户看得懂的东西。
 * 此前这两个组件**一条用例都没有** —— 也就是说"错误态"这条承诺
 * 完全没有回归保护:哪天有人把 `variant` 判断写反了,没人会发现。
 *
 * 两种形态:
 *   · 'empty' —— 一条数据都没有 → 降级占位(Empty + 重试)
 *   · 'strip' —— 还有旧数据 → 表格上方一条安静的提示条,旧数据留着
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { TableErrorState } from '@/components/common/TableErrorState'
import { FallbackPlaceholder } from '@/components/common/FallbackPlaceholder'

describe('TableErrorState', () => {
  it("variant='empty' → 空态里出现'加载失败'和重试按钮", () => {
    const onRetry = vi.fn()
    render(<TableErrorState variant="empty" onRetry={onRetry} />)

    expect(screen.getByText('加载失败')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it("variant='strip' → 提示条 + 重试按钮", () => {
    const onRetry = vi.fn()
    const { container } = render(<TableErrorState variant="strip" onRetry={onRetry} />)

    expect(screen.getByText('加载失败')).toBeInTheDocument()
    expect(container.querySelector('.ant-alert')).not.toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it("variant='strip' 的提示条是 warning 级(不是 error 级 —— 旧数据还在)", () => {
    const { container } = render(<TableErrorState variant="strip" />)

    expect(container.querySelector('.ant-alert-warning')).not.toBeNull()
    expect(container.querySelector('.ant-alert-error')).toBeNull()
  })

  it('不传 onRetry → 没有重试按钮', () => {
    render(<TableErrorState variant="strip" />)

    expect(screen.queryByRole('button', { name: /重\s*试/ })).toBeNull()
  })
})

describe('FallbackPlaceholder', () => {
  it('默认文案走 i18n(英文环境下不能是中文硬编码)', () => {
    render(<FallbackPlaceholder />)

    expect(screen.getByText('加载失败')).toBeInTheDocument()
  })

  it('text prop 覆盖默认文案', () => {
    render(<FallbackPlaceholder text="网络开小差了" />)

    expect(screen.getByText('网络开小差了')).toBeInTheDocument()
    expect(screen.queryByText('加载失败')).toBeNull()
  })

  it('error 是 Error 对象 → 显示它的 message', () => {
    render(<FallbackPlaceholder error={new Error('网关超时')} />)

    expect(screen.getByText('网关超时')).toBeInTheDocument()
  })

  it('error 是字符串 → 原样显示', () => {
    render(<FallbackPlaceholder error="纯文本错误" />)

    expect(screen.getByText('纯文本错误')).toBeInTheDocument()
  })

  it('有 onRetry → 渲染重试按钮;点了会调', () => {
    const onRetry = vi.fn()
    render(<FallbackPlaceholder onRetry={onRetry} />)

    fireEvent.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('没有 onRetry → 不渲染重试按钮', () => {
    render(<FallbackPlaceholder />)

    expect(screen.queryByRole('button', { name: /重\s*试/ })).toBeNull()
  })

  it("variant='network' 走断网图标, variant='empty' 走默认图标", () => {
    const { container: net } = render(<FallbackPlaceholder variant="network" />)
    const { container: empty } = render(<FallbackPlaceholder variant="empty" />)

    // antd 用 `ant-empty-normal` 这个类名标记"简单版图片"
    // (Empty.PRESENTED_IMAGE_SIMPLE ⇒ 根节点多一个 -normal)
    expect(net.querySelector('.ant-empty-normal')).not.toBeNull()
    expect(empty.querySelector('.ant-empty-normal')).toBeNull()
  })

  it('height 决定最小高度', () => {
    const { container } = render(<FallbackPlaceholder height={160} />)

    const root = container.firstElementChild as HTMLElement
    expect(root.style.minHeight).toBe('160px')
  })
})
