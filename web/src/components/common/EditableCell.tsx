/**
 * EditableCell —— 行内编辑的"视图 / 编辑器"切换单元格(§2.8 #16,配合 useTableEdit)
 *
 * 编辑中 → 渲染 `editor`(用 cloneElement 把 value / onChange 注入进去);
 * 非编辑 → 渲染 `view`。
 *
 * ⚠️ **为什么用 cloneElement 而不是 render prop**(以及为什么这样反而更稳):
 *   业务页的列 render 里已经有 record / 编辑 hook 的上下文,让页面直接把
 *   antd 控件作为 JSX 传进来(<Input /> / <Select options=... /> / <InputNumber />),
 *   控件的**其余 props 由页面自己写**(placeholder / options / 禁用态),
 *   本组件只注入**值链路**的 value / onChange —— 编辑器类型随便换,
 *   状态机一行业务页代码都不用写。
 *
 * 用法见 useTableEdit.ts 文件头。
 */
import { cloneElement, isValidElement } from 'react'
import type { ReactElement, ReactNode } from 'react'

export interface EditableCellProps {
  /** 该行是否在编辑(useTableEdit.isEditing(record)) */
  editing: boolean
  /** 非编辑态展示(原 render 输出) */
  view: ReactNode
  /** 编辑态的控件(必须是被控组件,value / onChange 会被注入) */
  editor: ReactElement
  /** 当前值(draft 合并视图里的字段值) */
  value: unknown
  /** 值变化回调(useTableEdit.setField 绑好字段名的那个) */
  onChange: (value: unknown) => void
}

/**
 * 编辑器 onChange 的取值:antd 控件传裸值,原生控件传事件对象。
 * 与 antd Form 默认的 getValueFromEvent 同一口径 —— target 上有 value 就取,
 * 否则原样透传(checkbox 的 target 只有 checked,不该在这里替它做决定)。
 */
function extractValue(v: unknown): unknown {
  if (v && typeof v === 'object' && 'target' in v) {
    const t = (v as { target?: { value?: unknown } }).target
    if (t && typeof t === 'object' && 'value' in t) return t.value
  }
  return v
}

export function EditableCell({ editing, view, editor, value, onChange }: EditableCellProps) {
  if (!editing) return <>{view}</>
  // ⚠️ editor 必须是合法元素 —— 页面手滑传了 undefined 时给出可读报错,
  //    而不是 cloneElement 里一记莫名的 invariant
  if (!isValidElement(editor)) {
    // 编程错误,任何环境都该喊出来(而不是"点了编辑格子没反应"再查半天)
    // eslint-disable-next-line no-console
    console.error('[EditableCell] editor 必须是 React 元素,收到:', editor)
    return <>{view}</>
  }
  return cloneElement(editor as ReactElement<Record<string, unknown>>, {
    value,
    onChange: (v: unknown) => onChange(extractValue(v)),
  })
}

export default EditableCell
