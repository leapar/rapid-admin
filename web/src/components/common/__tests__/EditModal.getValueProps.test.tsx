/**
 * EditModal 的 `getValueProps` 契约 —— 表单值 → 控件 props 的映射
 *
 * ## 为什么需要这一层(框架级 bug,不是页面级)
 *
 * 用户 2026-10-30 报:"部门编辑弹窗,负责人为空时显示数值 0,父部门为空时也显示 0"。
 *
 * 界面上那两处"0"是这么来的:
 *
 *   1. 后端把"没有负责人 / 顶级部门"表示成哨兵值 **"0"**(uint64 + `,string`),
 *      前端表单里存的就是字符串 '0';
 *   2. antd 的 Select / TreeSelect 的选项里**没有** '0' 这一项 ⇒
 *      antd 只能把 '0' **当成一个找不到 label 的值原样渲染出来**;
 *   3. 页面作者当然想到了这一点,于是在 render 里写了
 *      `value={value && value !== '0' ? value : undefined}` ——
 *      **但这行是死代码**。
 *
 * ## 死代码的原因(rc-field-form 2.7.1 Field.js)
 *
 *     // getControlled()
 *     var control = { ...childProps, ...valueProps };   // ← valueProps 覆盖 childProps
 *     ...
 *     return React.cloneElement(child, this.getControlled(child.props));
 *
 * **表单里的值会盖掉子组件自己写的 value prop** ⇒ render 里算出来的 value
 * 根本到不了控件 ⇒ antd 拿到的还是原始的 '0' ⇒ 界面上显示 "0"。
 *
 * ⇒ 唯一的映射入口是 Form.Item 的 `getValueProps`(它就是那个 valueProps)。
 *   本文件锁的就是"EditModal 确实把它透传下去了"。
 *
 * ## 为什么要有"对照组"那一条
 *
 * 断言"显示占位符"很容易假绿 —— 万一是 antd 版本变了、或者 antd 自己改对了,
 * 这条用例照样绿。所以这里必须有**反向**的一条:
 * **不给 getValueProps 时,控件确实会显示 "0"** ——
 * 这才证明被测的确实是"映射有没有生效",而不是"antd 恰好不这么渲染"。
 */
import { describe, it, expect, vi } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { App, Select } from 'antd'
import { EditModal } from '@/components/common/EditModal'
import type { EditField } from '@/components/common/EditModal'

const OPTIONS = [
  { value: 'u1', label: '张三' },
  { value: 'u2', label: '李四' },
]

/**
 * 一个"负责人"字段。
 *
 * ⚠️ render 里的 `value={value}` 跟 `value={value && value !== '0' ? value : undefined}`
 *   **是同一件事** —— 后者是死代码(见文件头)。这里故意写后者,
 *   用来证明"写在 render 里没用"。
 */
function leaderField(withGetValueProps: boolean): EditField {
  return {
    name: 'leader_id',
    label: '负责人',
    type: 'custom',
    ...(withGetValueProps
      ? { getValueProps: (v: unknown) => ({ value: v && v !== '0' ? v : undefined }) }
      : {}),
    render: ({ value, onChange }) => (
      <Select
        style={{ width: '100%' }}
        value={value && value !== '0' ? value : undefined}
        onChange={(v) => onChange(v ?? '0')}
        placeholder="请选择负责人"
        allowClear
        options={OPTIONS}
      />
    ),
  }
}

/** 控件当前选中的那一项的文案(没有选中就是空串) */
function selectionText(): string {
  const el = document.querySelector('.ant-select-selection-item')
  return (el?.getAttribute('title') ?? el?.textContent ?? '').trim()
}

/** 控件的占位符文案 */
function placeholderText(): string {
  const el = document.querySelector('.ant-select-selection-placeholder')
  return (el?.textContent ?? '').trim()
}

function renderModal(field: EditField) {
  return render(
    <App>
      <EditModal
        title="编辑"
        open
        // 表单里存的是后端哨兵 '0' = 没有负责人
        initialValues={{ leader_id: '0' }}
        fields={[field]}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />
    </App>,
  )
}

describe('EditModal · custom 字段的 getValueProps', () => {
  it('给了 getValueProps → 哨兵值被映射掉,控件显示占位符(不是 "0")', async () => {
    renderModal(leaderField(true))
    await waitFor(() => expect(document.querySelector('.ant-select')).toBeTruthy())

    expect(selectionText()).not.toBe('0')
    expect(selectionText()).toBe('')
    expect(placeholderText()).toBe('请选择负责人')
  })

  it('对照组:不给 getValueProps → 控件确实把 "0" 原样显示出来', async () => {
    // ⚠️ 这条**必须红**,它是上一条的对照组:
    //   证明"有没有显示 0"取决于映射有没有生效,而不是 antd 恰好这么渲染。
    //   如果哪天 antd 改了行为(自己把找不到 label 的值藏起来),这条会先红 ——
    //   那时候上一条就变成假绿了,得重新找别的判据。
    renderModal(leaderField(false))
    await waitFor(() => expect(document.querySelector('.ant-select')).toBeTruthy())

    expect(selectionText()).toBe('0')
  })
})
