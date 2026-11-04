/**
 * EditModal 的 initialValues 契约测试
 *
 * 起因(2026-10-25,修「用户编辑界面无法设置角色」时挖出来的框架级 bug):
 *   antd 5 里 **Form 的 initialValues 和 Form.Item 的 initialValue 只能留一个**。
 *   两个都给会告警 "Form already set 'initialValues' with path 'xxx'. Field can not overwrite it.",
 *   而且 Form.Item 那份**不生效** —— 最终值取决于渲染顺序,属于"看运气"的写法。
 *
 *   触发告警的精确条件(rc-field-form useForm.js 的 resetWithFields):
 *     Form.Item 的 initialValue !== undefined **且** Form 也设了同名路径。
 *   所以"Form 有值"时,Form.Item 的 initialValue 必须是 undefined **本身**,
 *   不能是 `undefined ?? []`(那会把 undefined 变成 [],告警照旧)。
 *
 * 这里锁两件事:
 *   1. Form 给的值生效,Form.Item 的默认值不掺和(多选框 / 开关 / 部门树)
 *   2. Form 没给这个路径时,Form.Item 的 initialValue 兜底生效
 */
import { describe, it, expect, vi } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { App } from 'antd'
import { EditModal } from '@/components/common/EditModal'
import type { EditField } from '@/components/common/EditModal'

const ROLES_FIELD: EditField = {
  name: 'roles',
  label: '角色',
  type: 'multiSelect',
  options: [
    { value: 'a', label: '甲角色' },
    { value: 'b', label: '乙角色' },
  ],
}

/** 读多选框当前选中的项 */
function selectedLabels(): string[] {
  return Array.from(document.querySelectorAll('.ant-select-selection-item')).map(
    (el) => el.getAttribute('title') ?? el.textContent ?? '',
  )
}

describe('EditModal · initialValues 契约', () => {
  it('Form 的 initialValues 生效,Form.Item 的默认值不掺和', async () => {
    render(
      <App>
        <EditModal
          title="编辑"
          open
          // Form 说 roles = ['a']
          initialValues={{ roles: ['a'] }}
          // Form.Item 的 default 说 roles = ['b'] —— 按契约这一份应该被忽略
          fields={[{ ...ROLES_FIELD, initialValue: ['b'] }]}
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />
      </App>,
    )
    await waitFor(() => expect(selectedLabels()).toHaveLength(1))
    expect(selectedLabels()[0]).toContain('甲角色')
    expect(selectedLabels()[0]).not.toContain('乙角色')
  })

  it('Form 没给这个路径时,Form.Item 的 initialValue 兜底生效', async () => {
    render(
      <App>
        <EditModal
          title="编辑"
          open
          initialValues={{ nickname: '张三' }}
          fields={[{ ...ROLES_FIELD, initialValue: ['b'] }]}
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />
      </App>,
    )
    await waitFor(() => expect(selectedLabels()).toHaveLength(1))
    expect(selectedLabels()[0]).toContain('乙角色')
  })

  it('Form 给了值 + Form.Item 也给了默认值 → 不触发 antd 的 initialValues 告警', async () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      render(
        <App>
          <EditModal
            title="编辑"
            open
            initialValues={{ roles: ['a'], status: 'active' }}
            fields={[
              { ...ROLES_FIELD, initialValue: ['b'] },
              { name: 'status', label: '状态', type: 'switch', initialValue: true },
            ]}
            onSubmit={vi.fn()}
            onCancel={vi.fn()}
          />
        </App>,
      )
      await waitFor(() => expect(selectedLabels()).toHaveLength(1))
      const messages = spy.mock.calls.map((c) => String(c[0] ?? ''))
      expect(messages.some((m) => m.includes("already set 'initialValues'"))).toBe(false)
    } finally {
      spy.mockRestore()
    }
  })
})
