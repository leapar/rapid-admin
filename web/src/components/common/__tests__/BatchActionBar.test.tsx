/**
 * BatchActionBar 渲染冒烟测试(2026-10-25,§2.8 #14)
 *
 * 锁三件事:
 *   1. count <= 0 → 整个组件不渲染(业务页不用写条件判断)
 *   2. count > 0 → 显示"已选 N 项" + 业务给的批量动作 + "取消选择"
 *   3. 点"取消选择" → onClear 被调用
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { BatchActionBar } from '@/components/common/BatchActionBar'

describe('BatchActionBar', () => {
  it('count = 0 什么都不渲染', () => {
    const { container } = render(
      <BatchActionBar count={0} onClear={vi.fn()}>
        <button type="button">批量删除</button>
      </BatchActionBar>,
    )
    expect(container.firstChild).toBeNull()
    expect(screen.queryByText(/批量删除/)).toBeNull()
  })

  it('count > 0 显示已选数量和批量动作', () => {
    render(
      <BatchActionBar count={3} onClear={vi.fn()}>
        <button type="button">批量删除</button>
      </BatchActionBar>,
    )
    expect(screen.getByText(/已选\s*3\s*项/)).toBeInTheDocument()
    expect(screen.getByText('批量删除')).toBeInTheDocument()
    expect(screen.getByText(/取消选择/)).toBeInTheDocument()
  })

  it('点"取消选择"调 onClear', () => {
    const onClear = vi.fn()
    render(
      <BatchActionBar count={2} onClear={onClear}>
        <button type="button">批量删除</button>
      </BatchActionBar>,
    )
    fireEvent.click(screen.getByText(/取消选择/))
    expect(onClear).toHaveBeenCalledTimes(1)
  })

  it('可以自定义文案 / 额外内容', () => {
    render(
      <BatchActionBar count={1} onClear={vi.fn()} text="已选择 1 位用户" extra={<span>提示</span>}>
        <button type="button">停用</button>
      </BatchActionBar>,
    )
    expect(screen.getByText('已选择 1 位用户')).toBeInTheDocument()
    expect(screen.getByText('提示')).toBeInTheDocument()
  })
})
