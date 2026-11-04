/**
 * SchemaForm —— schema 驱动的通用表单渲染器(§2.7 #15,P1)
 *
 * 业务页"声明一个字段数组就能渲染出一张完整 antd 表单" —— 控件映射、
 * 校验规则、栅格跨度、隐藏字段保值,全部由本组件裁决,业务页只剩 schema。
 *
 *   <SchemaForm
 *     fields={[
 *       { name: 'title', label: '标题', type: 'input', required: true },
 *       { name: 'level', label: '级别', type: 'select', options: ['高', '低'] },
 *     ]}
 *     initialValues={record}
 *     onFinish={save}
 *   />
 *
 * ⚠️ **与 sys_dyn_field / pages/dyn 完全解耦**(解耦理由见 schemaForm.ts
 *   文件头)—— 本组件给**静态业务页**用,字段在代码里写死。
 *
 * # 几个语义的裁决(写在这里,免得每个调用方猜一遍)
 *
 * 1. **form 实例**:传 `form` 就用调用方的(需要在外部 getFieldsValue /
 *    validateFields / setFieldsValue 时传,与 EditModal 同款惯例);
 *    不传自建,并通过 ref 暴露 —— `const ref = useRef<FormInstance>(null)`
 *    + `<SchemaForm ref={ref} ...>`,ref.current 就是当前生效的实例
 *    (传没传 form 都能拿到)。两条路都不堵。
 *
 * 2. **hidden 字段不是"删掉"**:以 `<Form.Item hidden>` 形态保持挂载 ——
 *    antd 的 hidden Form.Item 照常注册、照常参与取值,所以
 *    initialValues 里的值**原样出现在提交值里**。典型用法:编辑页里
 *    用户看不见但不能丢的字段(id / 创建人 / 乐观锁版本号)。
 *    隐藏期间不挂校验规则(看不见的 required 拦提交 = 死表单),
 *    重新可见即恢复。
 *
 * 3. **提交按钮**:组件**默认自带**一个(type=primary / htmlType=submit),
 *    文案 = `submitText` ?? t('common.save')(组件不硬编码业务文案,
 *    默认值走 i18n,想换就传 submitText)。`footer` 传入时**整体接管
 *    底部区域**:footer 收到那个默认按钮节点,返回什么渲染什么 ——
 *    放了 defaultSubmit 就还有默认按钮,不放就等于没有(可放自己的
 *    按钮组);不传 footer 就渲染默认按钮。`loading` 只作用于这个按钮
 *    (loading + 视觉禁用),不碰字段;整表禁用走 `disabled`。
 *
 * 4. **custom 字段**:render(field, form) 返回的节点被包进
 *    `<Form.Item name>` —— value/onChange 由 rc-field-form 注入**单个
 *    子元素**,节点要透传;值通道不是 value 的再配 valuePropName。
 *    详见 SchemaField.render 的注释。
 *
 * 5. **栅格**:Row gutter [16,16](与 SearchForm 同款);columns = 几列,
 *    默认 1(整行);字段默认跨度 = floor(24/columns),`field.span` 覆盖。
 *
 * 6. **hidden 函数形态**的驱动:Form.useWatch 订阅整表值 —— 表单任何
 *    一次值变化都会重渲染本组件。schema 表单字段量级小(十几项),
 *    这个代价可接受;换来的语义是 hidden 字段"跟着其它字段的值实时隐现"。
 */
import React from 'react'
import {
  Form,
  Input,
  InputNumber,
  Select,
  Radio,
  Checkbox,
  Switch,
  DatePicker,
  TimePicker,
  Row,
  Col,
  Button,
} from 'antd'
import type { FormProps } from 'antd'
import type { FormInstance } from 'antd/es/form'
import { useI18n } from '@/locales'
import { buildRules, normalizeOptions, isFieldHidden } from './schemaFormRules'
import type { SchemaField } from './schemaFormRules'

export interface SchemaFormProps {
  /** 字段 schema(顺序即渲染顺序) */
  fields: SchemaField[]
  /** 外部 Form 实例(需要在外部取值/校验/赋值时传);不传自建(可经 ref 拿) */
  form?: FormInstance
  /** 初始值(也承担"hidden 字段的载荷" —— 见文件头 #2) */
  initialValues?: Record<string, unknown>
  /** antd Form onFinish 原样透传 */
  onFinish?: (values: Record<string, unknown>) => void
  /** antd Form onFinishFailed 原样透传 */
  onFinishFailed?: FormProps['onFinishFailed']
  /** 整表禁用(查看态)—— antd Form disabled 上下文,字段级 disabled 叠加其上 */
  disabled?: boolean
  /** 布局,默认 vertical(与 EditModal / 本仓弹窗表单主流一致) */
  layout?: 'horizontal' | 'vertical'
  /** 栅格列数,默认 1(整行);字段 span 可逐个覆盖 */
  columns?: number
  /** 提交中 —— 默认提交按钮 loading(见文件头 #3) */
  loading?: boolean
  /** 提交按钮文案(默认 t('common.save')) */
  submitText?: string
  /**
   * 底部区域接管:收到默认提交按钮节点,返回什么渲染什么。
   * 不传 = 渲染默认提交按钮;返回值不放 defaultSubmit = 没有默认按钮。
   */
  footer?: (defaultSubmit: React.ReactNode) => React.ReactNode
}

export const SchemaForm = React.forwardRef<FormInstance, SchemaFormProps>(function SchemaForm(
  {
    fields,
    form: externalForm,
    initialValues,
    onFinish,
    onFinishFailed,
    disabled,
    layout = 'vertical',
    columns = 1,
    loading,
    submitText,
    footer,
  },
  ref,
) {
  const { t } = useI18n()
  const [internalForm] = Form.useForm()
  const form = externalForm ?? internalForm

  // 把当前生效的实例暴露给 ref(传没传 form 都一致)
  React.useImperativeHandle(ref, () => form, [form])

  /**
   * 订阅整表值 —— hidden 函数形态的驱动源(见文件头 #6)。
   * 首帧拿到的是 undefined,按 {} 处理(值还没注入)。
   */
  const watched = Form.useWatch((values: Record<string, unknown>) => values, form)

  const defaultSpan = columns > 1 ? Math.floor(24 / columns) : 24

  const defaultSubmit = (
    <Button type="primary" htmlType="submit" loading={loading}>
      {submitText ?? t('common.save')}
    </Button>
  )

  const renderControl = (f: SchemaField) => {
    const common = { placeholder: f.placeholder, disabled: f.disabled }
    switch (f.type) {
      case 'textarea':
        return <Input.TextArea rows={3} {...common} />
      case 'password':
        // new-password:浏览器密码管理器会把带 password 框的整页当登录页
        return <Input.Password autoComplete="new-password" {...common} />
      case 'number':
        return <InputNumber style={{ width: '100%' }} min={f.min} max={f.max} {...common} />
      case 'select':
        return (
          <Select
            style={{ width: '100%' }}
            options={normalizeOptions(f.options)}
            allowClear
            {...common}
          />
        )
      case 'radio':
        return <Radio.Group options={normalizeOptions(f.options)} {...common} />
      case 'checkbox':
        // 单个勾选框:值通道是 checked,不是 value
        return <Checkbox {...common} />
      case 'checkboxGroup':
        return <Checkbox.Group options={normalizeOptions(f.options)} {...common} />
      case 'switch':
        return <Switch {...common} />
      case 'date':
        return <DatePicker style={{ width: '100%' }} {...common} />
      case 'datetime':
        return <DatePicker showTime style={{ width: '100%' }} {...common} />
      case 'time':
        return <TimePicker style={{ width: '100%' }} {...common} />
      case 'custom':
        return f.render ? f.render(f, form) : null
      case 'input':
      default:
        return <Input {...common} />
    }
  }

  const renderItem = (f: SchemaField) => {
    const hidden = isFieldHidden(f, watched)
    const valuePropName =
      f.type === 'switch' || f.type === 'checkbox' ? 'checked' : f.valuePropName
    return (
      <Form.Item
        name={f.name}
        label={f.label}
        rules={hidden ? [] : buildRules(f)}
        extra={f.extra}
        hidden={hidden}
        valuePropName={valuePropName}
      >
        {renderControl(f)}
      </Form.Item>
    )
  }

  return (
    <Form
      form={form}
      layout={layout}
      initialValues={initialValues}
      onFinish={onFinish}
      onFinishFailed={onFinishFailed}
      disabled={disabled}
      autoComplete="off"
    >
      <Row gutter={[16, 16]}>
        {fields.map((f) => (
          <Col
            key={f.name}
            span={f.span ?? defaultSpan}
            style={isFieldHidden(f, watched) ? { display: 'none' } : undefined}
          >
            {renderItem(f)}
          </Col>
        ))}
      </Row>
      {footer ? footer(defaultSubmit) : defaultSubmit}
    </Form>
  )
})

export default SchemaForm
