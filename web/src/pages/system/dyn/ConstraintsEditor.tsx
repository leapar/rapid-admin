/**
 * 值约束编辑器 —— 文本长度 / 数值范围 / 格式正则(M1,对标 PocketBase 字段约束)
 *
 * ⚠️ **单独一个文件是 lint 要求**(`react-refresh/only-export-components`,
 *   本仓 `--max-warnings 0`)—— 它要在设计器里被当 `type: 'custom'` 的 render 用。
 *
 * ⚠️ **值形态是 `DynOptionsDraft` 的约束部分** ——
 *   约束和候选项/来源共存于同一个 options JSON(后端 dynx.Options),
 *   但一个字段要么是选择类(选项集编辑器)要么是约束类(本组件),不会同时出现。
 *   提交前由 @/api/dyn 的 serializeDynOptionsDraft 统一序列化。
 *
 * ⚠️ **类型不认识的输入框不渲染,不是 disabled** ——
 *   渲染出来再禁用,用户会以为"这个约束对我生效只是改不了";
 *   不渲染 + 后端 ValidateForType 兜底,两边语义一致:配了不该配的才报错。
 */
import { Form, Input, InputNumber, Space, Typography } from 'antd'
import { useI18n } from '@/locales'
import type { DynOptionsDraft } from '@/types/dyn'

const { Text } = Typography

/** 这些类型支持值约束(与后端 dynx.constraintsByType 对齐) */
const CONSTRAINED_TYPES = new Set(['text', 'longtext', 'int', 'number'])
const NUMBER_TYPES = new Set(['int', 'number'])

export interface ConstraintsEditorProps {
  /** 字段类型(决定渲染哪组输入) */
  fieldType: string
  /** 当前值(草稿;只用它的约束部分) */
  value?: DynOptionsDraft
  /** 变更回调(整份替换) */
  onChange?: (next: DynOptionsDraft) => void
}

export function ConstraintsEditor({ fieldType, value, onChange }: ConstraintsEditorProps) {
  const { t } = useI18n()
  if (!CONSTRAINED_TYPES.has(fieldType)) return null

  const isNumber = NUMBER_TYPES.has(fieldType)
  // ⚠️ patch 保留另一部分的值(select 的 choices 等)——约束只是草稿的一个切面
  const patch = (next: Partial<DynOptionsDraft>) =>
    onChange?.({ choices: value?.choices ?? [], source: value?.source ?? '', ...value, ...next })

  return (
    <Space direction="vertical" size={8} style={{ width: '100%' }}>
      {isNumber ? (
        <Space size={8}>
          <Form.Item noStyle>
            <InputNumber
              placeholder={t('page.dyn.designer.minValue')}
              value={value?.min}
              onChange={(v) => patch({ min: typeof v === 'number' ? v : undefined })}
              style={{ width: 140 }}
              controls={false}
            />
          </Form.Item>
          <Text type="secondary" style={{ fontSize: 12 }}>~</Text>
          <Form.Item noStyle>
            <InputNumber
              placeholder={t('page.dyn.designer.maxValue')}
              value={value?.max}
              onChange={(v) => patch({ max: typeof v === 'number' ? v : undefined })}
              style={{ width: 140 }}
              controls={false}
            />
          </Form.Item>
        </Space>
      ) : (
        <>
          <Space size={8}>
            <InputNumber
              min={1}
              precision={0}
              placeholder={t('page.dyn.designer.minLength')}
              value={value?.min_length}
              onChange={(v) => patch({ min_length: typeof v === 'number' ? v : undefined })}
              style={{ width: 160 }}
              controls={false}
            />
            <InputNumber
              min={1}
              precision={0}
              placeholder={t('page.dyn.designer.maxLength')}
              value={value?.max_length}
              onChange={(v) => patch({ max_length: typeof v === 'number' ? v : undefined })}
              style={{ width: 160 }}
              controls={false}
            />
          </Space>
          <Input
            allowClear
            placeholder={t('page.dyn.designer.pattern')}
            value={value?.pattern ?? ''}
            onChange={(e) => patch({ pattern: e.target.value || undefined })}
          />
          <Text type="secondary" style={{ fontSize: 12 }}>
            {t('page.dyn.designer.patternHelp')}
          </Text>
        </>
      )}
    </Space>
  )
}

export default ConstraintsEditor
