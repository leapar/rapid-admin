/**
 * SearchForm —— 搜索表单(轻量)
 *
 * 用法:
 *   <SearchForm
 *     initialValues={query}
 *     fields={[
 *       { name: 'keyword', label: '关键字' },
 *       { name: 'status', label: '状态', type: 'select', options: [...] },
 *     ]}
 *     onSearch={(values) => search(values)}
 *     onReset={reset}
 *   />
 *
 * 国际化:SearchForm 内置的 placeholder / 按钮 / 展开收起都走 t(),
 * 业务字段的 label 由调用方传入(可以也是 t() 结果).
 */
import React, { useState } from 'react'
import { Form, Input, Select, DatePicker, Button, Space, Row, Col } from 'antd'
import { SearchOutlined, ReloadOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'

export type FieldType = 'input' | 'select' | 'date' | 'dateRange' | 'number'

export interface SearchField {
  name: string
  label: string
  type?: FieldType
  options?: { label: string; value: string | number }[]
  placeholder?: string
  width?: number
}

export interface SearchFormProps {
  fields: SearchField[]
  initialValues?: Record<string, unknown>
  onSearch: (values: Record<string, unknown>) => void
  onReset?: () => void
  /** 默认折叠(只显示 1 行) */
  defaultCollapsed?: boolean
}

export const SearchForm: React.FC<SearchFormProps> = ({
  fields,
  initialValues,
  onSearch,
  onReset,
  defaultCollapsed = true,
}) => {
  const { t } = useI18n()
  const [form] = Form.useForm()
  const [collapsed, setCollapsed] = useState(defaultCollapsed)

  const renderField = (f: SearchField) => {
    switch (f.type) {
      case 'select':
        return (
          <Select
            placeholder={f.placeholder || t('validation.pleaseSelect', undefined, { label: f.label })}
            options={f.options || []}
            allowClear
            style={{ width: f.width || 200 }}
          />
        )
      case 'date':
        return <DatePicker style={{ width: f.width || 200 }} />
      case 'dateRange':
        return <DatePicker.RangePicker style={{ width: f.width || 280 }} />
      case 'number':
        return <Input type="number" style={{ width: f.width || 200 }} />
      case 'input':
      default:
        return (
          <Input
            placeholder={f.placeholder || t('validation.pleaseInput', undefined, { label: f.label })}
            allowClear
            style={{ width: f.width || 200 }}
          />
        )
    }
  }

  const visibleFields = collapsed ? fields.slice(0, 3) : fields

  return (
    <Form
      form={form}
      layout="inline"
      initialValues={initialValues}
      onFinish={onSearch}
      style={{ marginBottom: 16 }}
    >
      <Row gutter={[16, 16]} style={{ width: '100%' }}>
        {visibleFields.map((f) => (
          <Col key={f.name}>
            <Form.Item name={f.name} label={f.label}>
              {renderField(f)}
            </Form.Item>
          </Col>
        ))}
        <Col>
          <Space>
            <Button
              type="primary"
              htmlType="submit"
              icon={<SearchOutlined />}
            >
              {t('common.search')}
            </Button>
            <Button
              icon={<ReloadOutlined />}
              onClick={() => {
                form.resetFields()
                onReset?.()
              }}
            >
              {t('common.reset')}
            </Button>
            {fields.length > 3 && (
              <Button type="link" onClick={() => setCollapsed(!collapsed)}>
                {collapsed ? t('searchForm.expand') : t('searchForm.collapse')}
              </Button>
            )}
          </Space>
        </Col>
      </Row>
    </Form>
  )
}

export default SearchForm
