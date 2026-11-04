/**
 * EditModal —— 通用编辑/新增弹窗
 *
 * 用法:
 *   <EditModal<User>
 *     title={editing ? '编辑用户' : '新增用户'}
 *     open={open}
 *     initialValues={editing}
 *     fields={[
 *       { name: 'username', label: '账号', required: true },
 *       { name: 'nickname', label: '昵称', required: true },
 *       { name: 'email', label: '邮箱', type: 'email' },
 *       { name: 'status', label: '状态', type: 'select', options: [
 *         { value: 'active', label: '启用' },
 *         { value: 'disabled', label: '禁用' },
 *       ]},
 *       { name: 'remark', label: '备注', type: 'textarea' },
 *     ]}
 *     onSubmit={async (vals) => {
 *       await userApi.update(editing.id, vals)
 *       message.success('保存成功')
 *       reload()
 *     }}
 *     onCancel={() => setOpen(false)}
 *   />
 *
 * field type 决定渲染:
 *   - 'text'     默认 Input
 *   - 'email'    Input type=email
 *   - 'number'   InputNumber
 *   - 'password' Input.Password
 *   - 'textarea' Input.TextArea
 *   - 'select'   Select(需要 options)
 *   - 'date'     DatePicker
 *   - 'switch'   Switch
 *   - 'multiDept' MultiDeptTreeSelect(框架层 M57,需 value={departmentIds, primaryDepartmentId})
 *   - 'multiSelect' Select mode=multiple(2026-10-25,用户编辑"角色"字段用)
 *   - 'custom'   render(...)
 *
 * 每个字段还可以带 help(渲染成 Form.Item 的 extra,即控件下方的小灰字),
 * 用来解释"这个字段为什么是只读的 / 改了会发生什么"—— 这类说明没地方放的时候
 * 就只能靠用户猜,猜错就是数据事故。
 */
import React from 'react'
import { Modal, Form, Input, InputNumber, Select, DatePicker, Switch } from 'antd'
import type { FormInstance, Rule } from 'antd/es/form'
import { useI18n } from '@/locales'
import { MultiDeptTreeSelect, MultiDeptValue } from './MultiDeptTreeSelect'
import {
  getLatestFieldPolicyReport,
  readFieldPolicyReport,
  type FieldPolicyReport,
} from '@/utils/fieldPolicy'
import { filterEditFieldsByReport, omitRestrictedFields } from './fieldPolicy'

export type EditFieldType =
  | 'text'
  | 'email'
  | 'number'
  | 'password'
  | 'textarea'
  | 'select'
  | 'date'
  | 'switch'
  | 'multiDept'
  | 'multiSelect'
  | 'custom'

export interface EditFieldOption {
  value: string | number
  label: string
}

export interface EditField {
  name: string
  label: string
  type?: EditFieldType
  required?: boolean
  /** 占位符 */
  placeholder?: string
  /** select 选项 */
  options?: EditFieldOption[]
  /**
   * 自定义渲染(props 含 value, onChange)
   *
   * ⚠️ **props.value 是表单里的原始值**,而且**控件真正拿到的 value 不是它** ——
   *   rc-field-form 会用 `getValueProps` 的返回值把它盖掉(见下面 getValueProps 的注释)。
   *   所以 render 里读 props.value 只是"知道表单现在存了什么",
   *   拿它算控件显示什么**是无效的**(那行代码不会生效)。
   *
   * ⚠️ **写回表单的通道是 props.onChange**(它内部是 `form.setFieldValue`),
   *   而且它在 rc-field-form 的 dispatch **之后**执行 ⇒ **onChange 是最终权威**。
   *   典型写法 `onChange={(v) => onChange(v ?? '0')}` = 把"清空"归一成后端哨兵 '0'。
   */
  render?: (props: { value: any; onChange: (v: any) => void }) => React.ReactNode
  /**
   * 把 **表单里的值** 映射成 **控件的 props**(store → control)。
   *
   * ⚠️ **只有 `type: 'custom'` 用得上** —— 内置类型(text/select/...)的值形态
   *   跟表单值一致,不需要映射。
   *
   * # 为什么映射必须写在这一层,而不是写在 render 里的 `value={...}` 上
   *
   * rc-field-form 的 Field.getControlled 里是:
   *
   *     control = { ...childProps, ...valueProps }
   *
   * —— **表单里的值会盖掉子组件自己写的 value prop**。
   * 所以在 render 里写 `value={value ?? '0'}` 是**死代码**:
   * 真正生效的是表单里的原始值(界面上"显示数值 0"就是这么来的)。
   *
   * # 典型场景
   *
   * 表单里存的是后端哨兵值('0' = 无 / 顶级),但控件的选项里**没有** '0' ⇒
   * antd 把它当成一个不存在的选项**原样渲染出来**(用户报"显示数值 0")。
   *   getValueProps: (v) => ({ value: v && v !== '0' ? v : undefined })
   * 映射成 undefined,控件就显示占位符了。
   *
   * ⚠️ **必须返回 `{ value }`** —— 提供了 getValueProps 就等于**替换**了
   *   rc-field-form 的默认实现(默认是 `{ [valuePropName]: val }`),
   *   不返回 value 的话控件会变成完全不受控(改一次就再也不跟表单同步)。
   */
  getValueProps?: (value: any) => Record<string, any>
  /**
   * 校验规则(antd Rule[])
   *
   * ⚠️ 2026-10-25:以前这里手写了一个"长得像 Rule"的子集
   *    ({required, message, pattern, min, max, type: string, whitespace}),
   *    而 antd 的 Rule.type 是一组**字面量联合**(email/url/pattern/...),
   *    `type?: string` 塞不进去 → 每个用了 rules 的 Form.Item 都报一次
   *    TS2322(4 处)。直接用 antd 的 Rule 类型,规则本身也顺带被类型检查。
   */
  rules?: Rule[]
  /** 是否禁用 */
  disabled?: boolean
  /** 默认值 */
  initialValue?: any
  /** 字段宽度(span),默认 24 (整行) */
  span?: number
  /** 控件下方的小灰字说明(antd Form.Item extra) */
  help?: React.ReactNode
}

export interface EditModalProps<T = Record<string, unknown>> {
  title: React.ReactNode
  open: boolean
  initialValues?: Partial<T>
  fields: EditField[]
  onSubmit: (values: T) => Promise<void> | void
  onCancel: () => void
  /** 弹窗宽度 */
  width?: number
  /** 提交按钮文案 */
  okText?: string
  /** 提交中 disabled(外部可观察) */
  form?: FormInstance
  /**
   * 未打开时也强制渲染 Modal 内容(透传给 Modal)。
   * 用途:外部 `form` 实例被 `Form.useWatch` 订阅时(useWatch 在 Form 未挂载时
   * 调 getFieldsValue → 触发 "Instance created by useForm is not connected" 警告),
   * 需要 Form 常驻挂载 —— 目前 permission 页在用。
   */
  forceRender?: boolean
}

export function EditModal<T = Record<string, unknown>>({
  title,
  open,
  initialValues,
  fields,
  onSubmit,
  onCancel,
  width = 640,
  okText,
  form: externalForm,
  forceRender,
}: EditModalProps<T>) {
  const { t } = useI18n()
  const [internalForm] = Form.useForm()
  const form = externalForm ?? internalForm
  const [loading, setLoading] = React.useState(false)

  /**
   * 字段级可见性报告(M168)
   *
   * # 报告从哪来(两级)
   *
   *   ① **盖在 `initialValues` 上的 Symbol 戳** —— 编辑已有行时,
   *      行对象是服务端响应里的那一份(api/client.ts 盖过章),
   *      这是最权威的来源。
   *   ② **本页面最近一次响应的报告**(兜底)—— 两个场景戳会丢:
   *      · 页面把行对象**逐字段手抄**成新对象(system/user 的 editInitialValues)
   *      · **新建**流程的 `initialValues` 是页面拼的默认值,没经过响应
   *      这时用页面归档的那份(按路径归档,不会串页)。
   *
   * ⚠️ **没有报告时一行字段都不动** ——
   *   绝大多数页面没配字段规则,这条必须是零开销的快路径。
   */
  const fieldPolicyReport: FieldPolicyReport | null =
    readFieldPolicyReport(initialValues) ?? getLatestFieldPolicyReport()

  /**
   * 字段过滤:隐藏的删掉,打码的变只读
   *
   * ⚠️ **打码 ⇒ `disabled` 而不是删掉** ——
   *   用户要能看到"这里有个手机号,只是看不全";删掉反而让人以为字段不存在。
   *   ⚠️ **光只读还不够** —— antd 的 `validateFields()` 会把禁用字段的值
   *   照常收进来,所以提交前还要在 handleOk 里把打码字段**从载荷里剔掉**
   *   (否则 `138****1234` 会原样写回数据库,覆盖真实值 —— 数据损坏)。
   */
  const visibleFields = React.useMemo(
    () => filterEditFieldsByReport(fields, fieldPolicyReport),
    [fields, fieldPolicyReport],
  )

  // 每次 open 时重置表单
  React.useEffect(() => {
    if (open) {
      form.resetFields()
      form.setFieldsValue((initialValues ?? {}) as any)
    }
  }, [open, initialValues, form])

  /**
   * Form 的 initialValues 与 Form.Item 的 initialValue 只能留一个
   *
   * antd 5:两个都给会告警 "Form already set 'initialValues' with path 'xxx'",
   * 而且 **Form.Item 的那份不生效** —— 值到底取哪个取决于渲染顺序,
   * 属于"看运气"的写法。这里统一以 Form 的 initialValues 为准,
   * Form.Item 的 initialValue 只在 Form 没给这个路径时兜底。
   *
   * ⚠️ 告警的触发条件是 `Form.Item 的 initialValue !== undefined` **且** Form 也设了同名路径
   *    (rc-field-form useForm.js 的 resetWithFields)。所以"Form 有值"时,
   *    Form.Item 的 initialValue 必须是 **undefined 本身**,不能是 `undefined ?? []`
   *    —— 那个 `?? []` 会把 undefined 变成 [],告警照旧,而且是"看着有值其实是默认值"。
   */
  const formHasValue = (name: string) =>
    !!initialValues && Object.prototype.hasOwnProperty.call(initialValues, name)

  const fieldInitialValue = (f: EditField) =>
    formHasValue(f.name) ? undefined : f.initialValue

  const renderField = (f: EditField) => {
    if (f.type === 'custom' && f.render) {
      return (
        <Form.Item
          key={f.name}
          name={f.name}
          label={f.label}
          rules={f.rules}
          extra={f.help}
          initialValue={fieldInitialValue(f)}
          valuePropName="value"
          // ⚠️ **store → control 的映射只能走这一层** ——
          //   rc-field-form 的 getControlled 是 `control = {...childProps, ...valueProps}`,
          //   表单里的值会盖掉子组件自己写的 value prop。
          //   所以在 render 里写 `value={value ?? '0'}` 是死代码,必须在这里映射。
          getValueProps={f.getValueProps}
        >
          {f.render({ value: form.getFieldValue(f.name), onChange: (v) => form.setFieldValue(f.name, v) })}
        </Form.Item>
      )
    }
    const common = {
      placeholder: f.placeholder,
      disabled: f.disabled,
    }
    let inputEl: React.ReactNode
    switch (f.type) {
      case 'email':
        inputEl = <Input type="email" {...common} />
        break
      case 'number':
        inputEl = <InputNumber style={{ width: '100%' }} {...common} />
        break
      case 'password':
        inputEl = <Input.Password autoComplete="new-password" {...common} />
        break
      case 'textarea':
        inputEl = <Input.TextArea rows={3} {...common} />
        break
      case 'select':
        inputEl = (
          <Select
            {...common}
            options={f.options?.map((o) => ({ value: o.value, label: o.label }))}
            allowClear
          />
        )
        break
      case 'date':
        inputEl = <DatePicker style={{ width: '100%' }} {...common} />
        break
      case 'switch':
        return (
          <Form.Item
            key={f.name}
            name={f.name}
            label={f.label}
            valuePropName="checked"
            initialValue={formHasValue(f.name) ? undefined : (f.initialValue ?? false)}
            rules={f.rules}
            extra={f.help}
          >
            <Switch disabled={f.disabled} />
          </Form.Item>
        )
      case 'multiSelect':
        // 多选:值是 string[]。options 里的 value 统一转成字符串
        // (后端 ID 是 19 位数字,转 Number 会丢精度 —— 见 types/permission.ts 顶部注释)
        return (
          <Form.Item
            key={f.name}
            name={f.name}
            label={f.label}
            rules={f.rules}
            extra={f.help}
            initialValue={formHasValue(f.name) ? undefined : (f.initialValue ?? [])}
          >
            <Select
              mode="multiple"
              allowClear
              placeholder={f.placeholder}
              disabled={f.disabled}
              options={f.options?.map((o) => ({ value: String(o.value), label: o.label }))}
              optionFilterProp="label"
              style={{ width: '100%' }}
            />
          </Form.Item>
        )
      case 'multiDept': {
        // 受控组件:Form value 透传给 MultiDeptTreeSelect
        return (
          <Form.Item
            key={f.name}
            name={f.name}
            label={f.label}
            rules={f.rules}
            extra={f.help}
            initialValue={
              formHasValue(f.name)
                ? undefined
                : (f.initialValue ?? { departmentIds: [], primaryDepartmentId: undefined })
            }
            getValueFromEvent={(_v: MultiDeptValue | undefined) => _v ?? { departmentIds: [], primaryDepartmentId: undefined }}
            getValueProps={(v: MultiDeptValue | undefined) => ({ value: v ?? { departmentIds: [], primaryDepartmentId: undefined } })}
          >
            <MultiDeptTreeSelect id={f.name} placeholder={f.placeholder} readOnly={f.disabled} />
          </Form.Item>
        )
      }
      case 'custom':
        return null
      case 'text':
      default:
        inputEl = <Input {...common} />
    }
    return (
      <Form.Item
        key={f.name}
        name={f.name}
        label={f.label}
        rules={f.rules}
        extra={f.help}
        initialValue={fieldInitialValue(f)}
      >
        {inputEl}
      </Form.Item>
    )
  }

  const handleOk = async () => {
    let values: T
    try {
      values = (await form.validateFields()) as T
    } catch {
      return
    }
    // ⚠️ **M168:被收窄的字段必须从载荷里剔掉** ——
    //   打码字段在表单里是只读的,但 `validateFields()` 照样把它的值收进来
    //   (antd 不因为 disabled 就跳过字段)。原样提交 = 用 `138****1234`
    //   覆盖数据库里的真实手机号 —— **数据损坏**,而且没有任何报错。
    //   隐藏字段同理再挡一道(正常情况下它压根没渲染,进不了 values)。
    const payload = omitRestrictedFields(values, fieldPolicyReport)
    setLoading(true)
    try {
      await onSubmit(payload)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Modal
      open={open}
      forceRender={forceRender}
      title={title}
      onOk={handleOk}
      onCancel={onCancel}
      width={width}
      confirmLoading={loading}
      destroyOnHidden
      maskClosable={false}
      okText={okText ?? t('common.save')}
      cancelText={t('common.cancel')}
    >
      <Form
        form={form}
        layout="vertical"
        initialValues={initialValues as any}
        autoComplete="off"
        preserve={false}
      >
        {visibleFields.map(renderField)}
      </Form>
    </Modal>
  )
}

export default EditModal
