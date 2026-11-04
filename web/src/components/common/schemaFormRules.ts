/**
 * schemaFormRules —— SchemaForm 的纯逻辑(类型 + 规则构建 + 选项归一)(§2.7 #15)
 *
 * ⚠️ **为什么这个文件不叫 schemaForm.ts**(2026-11-03,踩出来的):
 *   vite 的 resolve.extensions 里 `.ts` 排在 `.tsx` 之前,而 Windows 文件系统
 *   **大小写不敏感** —— `import ... from './SchemaForm'` 会先拿 `SchemaForm.ts`
 *   去碰盘,碰到的却是 `schemaForm.ts`(纯逻辑模块)⇒ 组件**整个解析错文件**,
 *   渲染时 "Element type is invalid ... got: undefined"。
 *   (与 vitest 双 React 是同一家族的问题:路径字符串在 Windows 上不分大小写。)
 *   同目录已有 `avatar.ts` + `UserAvatar.tsx` 这类"纯逻辑 + 组件"并存,
 *   命名都刻意错开 —— 本模块遵循同一规则。
 *
 * # 这一层为什么存在
 *
 * 业务页要的是"声明一个字段数组就能得到一张完整表单" —— 控件映射、
 * 校验规则、栅格跨度这些**每页都在重复**的决定,由 SchemaForm 统一裁决。
 * 但本仓 eslint 配了 react-refresh + `--max-warnings 0`:**纯逻辑不能和
 * 组件放同一个文件**(组件文件热更新边界只允许组件导出),所以
 * buildRules / normalizeOptions / isFieldHidden 全部落在 .ts 这边,可独立单测。
 *
 * ⚠️ **与 sys_dyn_field / pages/dyn 完全解耦** —— 那套是"动态集合"专用
 *   (schema 在数据库里、运行期经接口下发、值进出要对齐后端 dynx);
 *   这里是给**静态业务页**声明即用的(字段在代码里写死,值形状由调用方
 *   自己定)。两边不共享类型、不共享转换,谁也不 import 谁 ——
 *   一旦共享,动态侧改 schema 形状会静默炸掉所有静态页。
 *
 * # 校验文案的口径(为什么不走 t())
 *
 * buildRules 是纯函数,拿不到 useI18n(那是个 hook)。仓库现有表单的
 * required 文案都是"每字段一个 i18n key"或 t('validation.pleaseInput',
 * undefined, {label}),那要求调用方逐字段写 —— 对 schema 驱动的渲染器
 * 不成立。这里取"框架默认文案"折中:
 *   · required        → `${label}不能为空`
 *   · 数值范围        → `${label}必须在 ${min} ~ ${max} 之间`(单边同理)
 *   · maxLength       → `${label}不能超过 ${maxLength} 个字符`
 * 业务页对文案有讲究时,用 `field.rules` 追加自己的规则(排在默认规则之后,
 * antd 按序取**第一条**失败规则,所以想完全接管就别开 required,把
 * required 写进自己的 rules 里)。
 */
import type { ReactNode } from 'react'
import type { Rule, FormInstance } from 'antd/es/form'

/** 支持的控件类型 */
export type SchemaFieldType =
  | 'input'
  | 'textarea'
  | 'password'
  | 'number'
  | 'select'
  | 'radio'
  | 'checkbox'
  | 'checkboxGroup'
  | 'switch'
  | 'date'
  | 'datetime'
  | 'time'
  | 'custom'

export interface SchemaFieldOption {
  label: string
  value: string | number
  disabled?: boolean
}

/**
 * 选项的两种写法:
 *   简写:['启用', '禁用'] / [1, 2]  → label 与 value 同值(normalizeOptions 归一)
 *   完整:{ label: '启用', value: 'active', disabled?: true }
 *   混合数组也行 —— 所以是扁平联合,不是两个数组的联合
 *   (后者会拒绝 ['a', {label:'B', value:'b'}] 这种字面量)。
 */
export type SchemaFieldOptions = (string | number | SchemaFieldOption)[]

export interface SchemaField {
  /** 字段名 —— 提交值里的键(与后端 JSON 键一致,显示名可能重复它不会) */
  name: string
  /** 显示名(Form.Item label) */
  label: string
  type: SchemaFieldType
  /** 必填(生成 `${label}不能为空`;想自定义文案就自己写进 rules) */
  required?: boolean
  /** select / radio / checkboxGroup 的选项(简写或完整,见 SchemaFieldOptions) */
  options?: SchemaFieldOptions
  placeholder?: string
  /** number 的下/上界(双用:InputNumber 控件属性 + 自动校验规则) */
  min?: number
  max?: number
  /** 文本类(input/textarea/password)最大长度 —— 只生成校验规则,不裁剪输入 */
  maxLength?: number
  /**
   * 调用方手动追加的校验规则 —— **追加在自动规则之后**(antd 按序取第一条
   * 失败规则,所以手动规则在前意味着默认文案被架空)。
   */
  rules?: Rule[]
  /** 控件下方的小灰字说明(antd Form.Item extra) */
  extra?: ReactNode
  /** Col 跨度(1~24);不传用 columns 算出的默认值 */
  span?: number
  /**
   * 隐藏(值或"拿当前表单值算"的函数)。隐藏字段**不消失、只不显示**:
   * Form.Item 以 hidden 形态保持挂载,值照常参与提交 —— 这正是"编辑页
   * 带一个用户看不见但不能丢的字段(id / 创建人)"的用法。
   * ⚠️ 函数形态靠 Form.useWatch 驱动重渲染,每次表单值变化都会重算。
   */
  hidden?: boolean | ((values: Record<string, unknown>) => boolean)
  /** 单字段禁用(与整表 disabled 叠加) */
  disabled?: boolean
  /**
   * 是否由 min/max/maxLength 自动派生校验规则(默认 true)。
   * 关掉的场景:文案要精修、或调用方在 rules 里自己写了同语义规则,
   * 不想看到两条同义错误。
   */
  extraRules?: boolean
  /**
   * type='custom' 时的自定义渲染:(field, form) ⇒ 控件节点。
   * SchemaForm 会把它包进 `<Form.Item name>` —— 节点必须把 value/onChange
   * 透传给真正受控的控件(rc-field-form 只对**单个子元素**注入这两个 prop);
   * 值通道不是 value/checked 的(比如日期对象),再配 `valuePropName`。
   * 想完全自管(自己渲染 Form.Item / noStyle)就别用本组件的 custom,
   * 把那块表单拆出去 —— 嵌套同名 Form.Item 是 undefined 行为。
   */
  render?: (field: SchemaField, form: FormInstance) => ReactNode
  /** custom 字段的值通道 prop 名(默认 'value';开关类用 'checked') */
  valuePropName?: string
}

/**
 * 字段 → 校验规则
 *
 * 顺序:required ⇒ 自动派生(min/max/maxLength)⇒ 调用方 rules。
 * antd 按序校验、取**第一条**失败规则的 message 展示。
 *
 * whitespace:input/textarea/password 的 required 规则带 whitespace:true ——
 *   纯空格视为"没填"。仓库现有表单没有这个习惯(逐字段手写规则,没人
 *   提过纯空格),这里作为框架默认补上;不想要的把 required 写进自己的
 *   rules 并关掉(见 SchemaField.rules 注释)。
 */
export function buildRules(field: SchemaField): Rule[] {
  const rules: Rule[] = []

  if (field.required) {
    const rule: Rule = { required: true, message: `${field.label}不能为空` }
    if (field.type === 'input' || field.type === 'textarea' || field.type === 'password') {
      // whitespace 是 required 规则的修饰符:纯空格串按"空值"处理
      rule.whitespace = true
    }
    rules.push(rule)
  }

  if (field.extraRules !== false) {
    if (field.type === 'number' && (field.min !== undefined || field.max !== undefined)) {
      const { min, max } = field
      const message =
        min !== undefined && max !== undefined
          ? `${field.label}必须在 ${min} ~ ${max} 之间`
          : min !== undefined
            ? `${field.label}不能小于 ${min}`
            : `${field.label}不能大于 ${max}`
      rules.push({
        type: 'number',
        ...(min !== undefined ? { min } : {}),
        ...(max !== undefined ? { max } : {}),
        message,
      })
    }
    const isTextLike =
      field.type === 'input' || field.type === 'textarea' || field.type === 'password'
    if (isTextLike && field.maxLength !== undefined) {
      rules.push({
        max: field.maxLength,
        message: `${field.label}不能超过 ${field.maxLength} 个字符`,
      })
    }
  }

  if (field.rules && field.rules.length > 0) {
    rules.push(...field.rules)
  }
  return rules
}

/**
 * 选项归一:简写(string/number)→ 完整形态,label 与 value 同值
 * (数字转 String 做 label)。原样返回完整对象(不拷贝)。
 */
export function normalizeOptions(options?: SchemaFieldOptions): SchemaFieldOption[] {
  if (!options) return []
  return options.map((o) =>
    typeof o === 'string' || typeof o === 'number' ? { label: String(o), value: o } : o,
  )
}

/**
 * 字段当前是否应隐藏。values 是**当前整表值**(Form.useWatch 拿到的),
 * 没有时按 {} 算(函数形态自己兜底)。
 */
export function isFieldHidden(
  field: SchemaField,
  values: Record<string, unknown> | undefined,
): boolean {
  if (field.hidden === undefined) return false
  if (typeof field.hidden === 'function') return field.hidden(values ?? {})
  return field.hidden
}
