import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { EditableCell } from '../EditableCell'

describe('EditableCell', () => {
  it('非编辑态:只渲染 view', () => {
    render(
      <EditableCell
        editing={false}
        view={<span>只读值</span>}
        editor={<input />}
        value=""
        onChange={vi.fn()}
      />,
    )
    expect(screen.getByText('只读值')).toBeTruthy()
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('编辑态:渲染 editor 并注入 value/onChange', () => {
    const onChange = vi.fn()
    render(
      <EditableCell
        editing
        view={<span>只读值</span>}
        editor={<input aria-label="编辑" placeholder="占位" />}
        value="草稿值"
        onChange={onChange}
      />,
    )
    const box = screen.getByLabelText('编辑') as HTMLInputElement
    expect(box.value).toBe('草稿值')
    // 页面传的其余 props 保留(placeholder 没被注入冲掉)
    expect(box.placeholder).toBe('占位')
    fireEvent.change(box, { target: { value: '新值' } })
    expect(onChange).toHaveBeenCalledWith('新值')
  })

  it('editor 传了非法值:回退 view + console.error(编程错误要喊出来)', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    render(
      <EditableCell
        editing
        view={<span>只读值</span>}
        editor={undefined as unknown as React.ReactElement}
        value=""
        onChange={vi.fn()}
      />,
    )
    expect(screen.getByText('只读值')).toBeTruthy()
    expect(err).toHaveBeenCalled()
    err.mockRestore()
  })
})
