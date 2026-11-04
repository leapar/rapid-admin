/**
 * 集合索引编辑器 —— 集合级自定义索引(M2,对标 PocketBase collection.Indexes)
 *
 * ⚠️ **单独一个文件是 lint 要求**(`react-refresh/only-export-components`,
 *   本仓 `--max-warnings 0`)—— 它要在设计器的集合编辑弹窗里当 custom render 用。
 *
 * ⚠️ **表单里的值是 `DynIndexDef[]`,不是 JSON 文本** ——
 *   JSON 文本 ⇄ 数组的转换在提交时做(serializeDynIndexes / parseDynIndexes),
 *   控件只面对结构化数据,校验和提示才有落点。
 *
 * ⚠️ **columns 选的是字段标识** ——
 *   选项来自当前集合的在用字段;引用已停用字段的新索引后端会拒,
 *   前端只给在用字段,让"配不了"在界面上就不出现。
 */
import { Button, Input, Select, Space, Switch, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'
import type { DynIndexDef } from '@/types/dyn'

const { Text } = Typography

export interface IndexesEditorProps {
  /** 当前集合的在用字段(列下拉的选项) */
  fieldOptions: { value: string; label: string }[]
  value?: DynIndexDef[]
  onChange?: (next: DynIndexDef[]) => void
}

export function IndexesEditor({ fieldOptions, value, onChange }: IndexesEditorProps) {
  const { t } = useI18n()
  const defs = value ?? []

  const patch = (idx: number, next: Partial<DynIndexDef>) => {
    const updated = defs.map((d, i) => (i === idx ? { ...d, ...next } : d))
    onChange?.(updated)
  }

  if (fieldOptions.length === 0) {
    return (
      <Text type="secondary" style={{ fontSize: 12 }}>
        {t('page.dyn.designer.indexesNoFields')}
      </Text>
    )
  }

  return (
    <Space direction="vertical" size={8} style={{ width: '100%' }}>
      {defs.map((d, idx) => (
        <Space key={idx} size={6} style={{ width: '100%' }} wrap>
          <Input
            placeholder={t('page.dyn.designer.indexName')}
            value={d.name}
            onChange={(e) => patch(idx, { name: e.target.value })}
            style={{ width: 160 }}
          />
          <Select
            mode="multiple"
            placeholder={t('page.dyn.designer.indexColumns')}
            value={d.columns}
            options={fieldOptions}
            onChange={(cols) => patch(idx, { columns: cols })}
            style={{ minWidth: 220, flex: 1 }}
            maxTagCount="responsive"
          />
          <Space size={4}>
            <Text type="secondary" style={{ fontSize: 12 }}>{t('page.dyn.designer.indexUnique')}</Text>
            <Switch size="small" checked={d.unique === true} onChange={(v) => patch(idx, { unique: v })} />
          </Space>
          <Button
            type="text"
            size="small"
            danger
            icon={<DeleteOutlined />}
            aria-label={t('common.delete')}
            onClick={() => onChange?.(defs.filter((_, i) => i !== idx))}
          />
        </Space>
      ))}
      <Button
        type="dashed"
        size="small"
        icon={<PlusOutlined />}
        onClick={() => onChange?.([...defs, { name: '', columns: [], unique: false }])}
      >
        {t('page.dyn.designer.addIndex')}
      </Button>
    </Space>
  )
}

export default IndexesEditor
