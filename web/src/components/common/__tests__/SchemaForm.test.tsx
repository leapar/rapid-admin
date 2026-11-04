/**
 * SchemaForm 组件测试(antd 5 + jsdom)
 *
 * # 可逆探针(拆掉实现看测试会不会红)
 *
 *	拆法                                          必须变红
 *	────────────────────────────────────────────  ─────────────────────────────
 *	hidden 用"不渲染 Form.Item"代替 <Item hidden>  「hidden 字段值仍在提交值」
 *	hidden 字段仍挂 required 规则                  「hidden 期间不拦提交」
 *	valuePropName 给 switch 用 'value'             「switch 值通道」一族
 *	控件映射里 number 渲染成 Input                 「各类型渲染」
 *	默认跨度无视 columns                           「columns 默认跨度」
 *	field.span 被忽略                              「span 覆盖」
 *	footer 只追加不接管                            「footer 整体接管底部」
 *	整表 disabled 只禁字段不禁按钮                 「整表禁用」
 *	required 文案改掉                              「required 空值被拦 + 文案」
 *
 * ⚠️ 本仓已知的 antd5+jsdom 陷阱(见 MEMORY):
 *   · Button 两个汉字之间会插空格 —— 断言按钮用 /保\s*存/,不能写 '保存'
 *   · 提交/校验是异步的 —— fireEvent 后要 await act / waitFor 等 React 刷完
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { Form } from 'antd'
import type { FormInstance } from 'antd/es/form'
import { SchemaForm } from '@/components/common/SchemaForm'
import type { SchemaField } from '@/components/common/schemaFormRules'

/** 提交按钮(汉字间可能被插空格,用正则兜) */
function submitButton(): HTMLButtonElement {
  return screen.getByRole('button', { name: /保\s*存/ }) as HTMLButtonElement
}

/**
 * 隐藏中的表单项(遍历 + classList,不走 querySelector('.ant-form-item-hidden'))
 *
 * ⚠️ **jsdom(nwsapi)选择器缓存陷阱**(2026-11-03 实测定位):
 *   `querySelector('.ant-form-item-hidden')` 在**首次调用、零匹配**之后,
 *   即使后来 DOM 里真出现了带这个类的节点,它也**永远返回 null** ——
 *   nwsapi 把"空结果"缓存住了(DOM 变了缓存不失效)。
 *   `querySelectorAll` 不吃这一套。所以这里遍历 .ant-form-item 再看 classList。
 */
function hiddenFormItems(): Element[] {
  return Array.from(document.querySelectorAll('.ant-form-item')).filter((el) =>
    el.classList.contains('ant-form-item-hidden'),
  )
}

describe('SchemaForm · 各字段类型渲染出对应控件', () => {
  const fields: SchemaField[] = [
    { name: 'title', label: '标题', type: 'input' },
    { name: 'desc', label: '描述', type: 'textarea' },
    { name: 'pwd', label: '密码', type: 'password' },
    { name: 'count', label: '数量', type: 'number' },
    { name: 'level', label: '级别', type: 'select', options: ['高', '低'] },
    { name: 'kind', label: '类型', type: 'radio', options: ['甲', '乙'] },
    { name: 'agree', label: '同意', type: 'checkbox' },
    { name: 'tags', label: '标签', type: 'checkboxGroup', options: ['红', '蓝'] },
    { name: 'on', label: '开关', type: 'switch' },
    { name: 'day', label: '日期', type: 'date' },
    { name: 'at', label: '时间点', type: 'time' },
  ]

  function renderAll() {
    return render(<SchemaForm fields={fields} onFinish={vi.fn()} />)
  }

  it('input/textarea/password/number', () => {
    const { container } = renderAll()
    expect(document.getElementById('title')).toBeTruthy()
    expect(container.querySelector('textarea')).toBeTruthy()
    expect(document.querySelector('input[type="password"]')).toBeTruthy()
    expect(container.querySelector('.ant-input-number')).toBeTruthy()
  })

  it('select/radio/checkbox/checkboxGroup', () => {
    const { container } = renderAll()
    expect(container.querySelector('.ant-select')).toBeTruthy()
    const radios = container.querySelectorAll('.ant-radio')
    expect(radios.length).toBe(2)
    expect(container.querySelector('.ant-checkbox')).toBeTruthy()
    const boxes = container.querySelectorAll('.ant-checkbox-input')
    // agree 1 个 + checkboxGroup 2 个
    expect(boxes.length).toBe(3)
  })

  it('switch 值通道是 checked(role=switch),date/time 都是 picker', () => {
    const { container } = renderAll()
    expect(screen.getByRole('switch')).toBeTruthy()
    const pickers = container.querySelectorAll('.ant-picker')
    expect(pickers.length).toBe(2)
  })

  it('checkboxGroup 的 string 简写选项渲染出两个可勾选项', () => {
    const { container } = render(
      <SchemaForm
        fields={[{ name: 'tags', label: '标签', type: 'checkboxGroup', options: ['红', '蓝'] }]}
        onFinish={vi.fn()}
      />,
    )
    const wrappers = container.querySelectorAll('.ant-checkbox-group .ant-checkbox-wrapper')
    expect(wrappers.length).toBe(2)
    expect(wrappers[0].textContent).toContain('红')
    expect(wrappers[1].textContent).toContain('蓝')
  })
})

describe('SchemaForm · initialValues 回填与提交', () => {
  it('initialValues 回填到控件', () => {
    const { container } = render(
      <SchemaForm
        fields={[
          { name: 'title', label: '标题', type: 'input' },
          { name: 'on', label: '开关', type: 'switch' },
        ]}
        initialValues={{ title: '已有标题', on: true }}
        onFinish={vi.fn()}
      />,
    )
    const input = document.getElementById('title') as HTMLInputElement
    expect(input.value).toBe('已有标题')
    expect(screen.getByRole('switch').classList.contains('ant-switch-checked')).toBe(true)
    expect(container).toBeTruthy()
  })

  it('onFinish 拿到填写的值', async () => {
    const onFinish = vi.fn()
    render(
      <SchemaForm
        fields={[{ name: 'title', label: '标题', type: 'input' }]}
        onFinish={onFinish}
      />,
    )
    fireEvent.change(document.getElementById('title')!, { target: { value: '交付平台' } })
    await act(async () => {
      fireEvent.click(submitButton())
    })
    await waitFor(() => expect(onFinish).toHaveBeenCalledTimes(1))
    expect(onFinish).toHaveBeenCalledWith({ title: '交付平台' })
  })
})

describe('SchemaForm · required 校验', () => {
  it('空值提交被拦,文案是「{label}不能为空」,onFinish 不触发', async () => {
    const onFinish = vi.fn()
    render(
      <SchemaForm
        fields={[{ name: 'title', label: '标题', type: 'input', required: true }]}
        onFinish={onFinish}
      />,
    )
    await act(async () => {
      fireEvent.click(submitButton())
    })
    await waitFor(() => expect(screen.getByText('标题不能为空')).toBeTruthy())
    expect(onFinish).not.toHaveBeenCalled()
  })

  it('input 的 required 把纯空格也当没填(whitespace)', async () => {
    const onFinish = vi.fn()
    render(
      <SchemaForm
        fields={[{ name: 'title', label: '标题', type: 'input', required: true }]}
        onFinish={onFinish}
      />,
    )
    fireEvent.change(document.getElementById('title')!, { target: { value: '   ' } })
    await act(async () => {
      fireEvent.click(submitButton())
    })
    await waitFor(() => expect(screen.getByText('标题不能为空')).toBeTruthy())
    expect(onFinish).not.toHaveBeenCalled()
  })

  it('number 超 max 出自动派生文案(jsdom 里 InputNumber 受控输入不可靠,经 form 实例注入值)', async () => {
    const onFinish = vi.fn()
    let formRef: FormInstance | null = null
    const Holder = () => {
      const [form] = Form.useForm()
      formRef = form
      return (
        <SchemaForm
          form={form}
          fields={[{ name: 'count', label: '数量', type: 'number', min: 1, max: 10 }]}
          onFinish={onFinish}
        />
      )
    }
    render(<Holder />)
    await act(async () => {
      formRef!.setFieldsValue({ count: 99 })
    })
    await act(async () => {
      fireEvent.click(submitButton())
    })
    await waitFor(() => expect(screen.getByText('数量必须在 1 ~ 10 之间')).toBeTruthy())
    expect(onFinish).not.toHaveBeenCalled()
  })
})

describe('SchemaForm · hidden 字段保值', () => {
  it('boolean hidden:hidden Form.Item 不显示,但值仍在提交值里', async () => {
    const onFinish = vi.fn()
    render(
      <SchemaForm
        fields={[
          { name: 'title', label: '标题', type: 'input', required: true },
          { name: 'creator', label: '创建人', type: 'input', hidden: true },
        ]}
        initialValues={{ creator: 'op-01' }}
        onFinish={onFinish}
      />,
    )
    // 不显示:antd 的 hidden 形态(label 文本仍在 DOM,只是 display:none)
    expect(hiddenFormItems()).toHaveLength(1)
    expect(screen.getByText('创建人').closest('.ant-form-item')!.className).toContain(
      'ant-form-item-hidden',
    )
    fireEvent.change(document.getElementById('title')!, { target: { value: '标题' } })
    await act(async () => {
      fireEvent.click(submitButton())
    })
    await waitFor(() => expect(onFinish).toHaveBeenCalledTimes(1))
    // ⚠️ 关键断言:看不见的字段的值没有被丢掉
    expect(onFinish).toHaveBeenCalledWith({ title: '标题', creator: 'op-01' })
  })

  it('函数 hidden:跟着其它字段的值实时隐现;隐藏期间 required 不拦提交', async () => {
    const onFinish = vi.fn()
    const fields: SchemaField[] = [
      { name: 'toggle', label: '启用细项', type: 'switch' },
      {
        name: 'detail',
        label: '细项',
        type: 'input',
        required: true,
        hidden: (v) => v.toggle === true,
      },
    ]
    render(
      <SchemaForm fields={fields} initialValues={{ detail: '' }} onFinish={onFinish} />,
    )
    // 初始可见(toggle 未定义 → 不隐藏)
    expect(hiddenFormItems()).toHaveLength(0)
    // 打开开关 → 隐藏
    await act(async () => {
      fireEvent.click(screen.getByRole('switch'))
    })
    // ⚠️ 再空转一圈 act:rc-switch 内部 setState → Form store → useWatch 的
    //    联动更新在 jsdom 里偶尔落在 act 边界之后,不补这一圈 waitFor 会等满 10s 假红
    await act(async () => {})
    await waitFor(() => expect(hiddenFormItems()).toHaveLength(1))
    // 隐藏期间:required 不挂,提交直接过(detail 的空值原样带上)
    await act(async () => {
      fireEvent.click(submitButton())
    })
    await waitFor(() => expect(onFinish).toHaveBeenCalledTimes(1))
    expect(onFinish).toHaveBeenCalledWith({ toggle: true, detail: '' })
  })
})

describe('SchemaForm · custom 字段', () => {
  function CustomInput(props: { value?: string; onChange?: (v: string) => void }) {
    return (
      <input
        data-testid="custom"
        value={props.value ?? ''}
        onChange={(e) => props.onChange?.(e.target.value)}
      />
    )
  }

  it('render 的节点被 Form.Item 包裹,value/onChange 正常注入并参与提交', async () => {
    const onFinish = vi.fn()
    const fields: SchemaField[] = [
      {
        name: 'custom1',
        label: '自定义',
        type: 'custom',
        render: () => <CustomInput />,
      },
    ]
    render(<SchemaForm fields={fields} onFinish={onFinish} />)
    fireEvent.change(screen.getByTestId('custom'), { target: { value: 'hello' } })
    await act(async () => {
      fireEvent.click(submitButton())
    })
    await waitFor(() => expect(onFinish).toHaveBeenCalledTimes(1))
    expect(onFinish).toHaveBeenCalledWith({ custom1: 'hello' })
  })
})

describe('SchemaForm · 栅格与状态', () => {
  it('columns=2 默认跨度 12;field.span 覆盖', () => {
    const { container } = render(
      <SchemaForm
        fields={[
          { name: 'a', label: '甲', type: 'input' },
          { name: 'b', label: '乙', type: 'input' },
          { name: 'c', label: '丙', type: 'input', span: 24 },
        ]}
        columns={2}
        onFinish={vi.fn()}
      />,
    )
    // antd Col span=N → ant-col-N
    expect(container.querySelectorAll('.ant-col-12').length).toBe(2)
    expect(container.querySelectorAll('.ant-col-24').length).toBe(1)
  })

  it('默认 columns=1 → 整行(span 24)', () => {
    const { container } = render(
      <SchemaForm fields={[{ name: 'a', label: '甲', type: 'input' }]} onFinish={vi.fn()} />,
    )
    expect(container.querySelector('.ant-col-24')).toBeTruthy()
  })

  it('整表 disabled:按钮和控件都禁', () => {
    render(
      <SchemaForm
        fields={[{ name: 'title', label: '标题', type: 'input' }]}
        disabled
        onFinish={vi.fn()}
      />,
    )
    expect(submitButton().disabled).toBe(true)
    expect((document.getElementById('title') as HTMLInputElement).disabled).toBe(true)
  })

  it('loading:提交按钮进入 loading 态', () => {
    render(
      <SchemaForm fields={[{ name: 'a', label: '甲', type: 'input' }]} loading onFinish={vi.fn()} />,
    )
    expect(submitButton().classList.contains('ant-btn-loading')).toBe(true)
  })

  it('footer 整体接管底部区域:defaultSubmit 放不放由调用方定', () => {
    const { container } = render(
      <SchemaForm
        fields={[{ name: 'a', label: '甲', type: 'input' }]}
        onFinish={vi.fn()}
        footer={(defaultSubmit) => (
          <>
            <span data-testid="hint">只读说明</span>
            {defaultSubmit}
          </>
        )}
      />,
    )
    expect(screen.getByTestId('hint')).toBeTruthy()
    expect(submitButton()).toBeTruthy()
    expect(container.textContent).toContain('只读说明')
  })

  it('submitText 覆盖默认文案', () => {
    render(
      <SchemaForm
        fields={[{ name: 'a', label: '甲', type: 'input' }]}
        submitText="提交审核"
        onFinish={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: /提\s*交\s*审\s*核/ })).toBeTruthy()
  })
})

describe('SchemaForm · 外部 form 实例', () => {
  it('传 form:外部 setFieldsValue 能写进控件', async () => {
    let formRef: FormInstance | null = null
    const Holder = () => {
      const [form] = Form.useForm()
      formRef = form
      return (
        <SchemaForm
          form={form}
          fields={[{ name: 'title', label: '标题', type: 'input' }]}
          onFinish={vi.fn()}
        />
      )
    }
    render(<Holder />)
    await act(async () => {
      formRef!.setFieldsValue({ title: '外部写入' })
    })
    expect((document.getElementById('title') as HTMLInputElement).value).toBe('外部写入')
  })
})
