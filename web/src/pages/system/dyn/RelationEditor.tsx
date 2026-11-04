/**
 * 关联字段编辑器 —— relation 字段的目标集合与显示字段(M5)
 *
 * 单独拆一个文件的原因:`react-refresh/only-export-components`
 * 规则要求 .tsx 文件只导出组件(本仓 lint 是 `--max-warnings 0`,警告也算失败),
 * 这个编辑器要在页面里被当 `type: 'custom'` 的 render 用(同 OptionsEditor)。
 *
 * ⚠️ **值形态仍是 `DynOptionsDraft`** —— 与 OptionsEditor / ConstraintsEditor
 *   共用同一个表单字段名 `options`(后端 dynx.Options 一个 JSON 装下
 *   候选项 / 约束 / 关联配置),落点一致才不会互相冲掉。
 *   序列化由页面统一走 serializeDynOptionsDraft(漏 relation 就是静默抹掉目标集合)。
 *
 * # 与 select 的「关联系统表」来源是两回事
 *
 *   · select.source —— 选项从哪张**系统表**来(user / role / dept),落库的是值列
 *   · relation —— 引用哪个**业务集合**的哪条**记录**,落库的是目标记录 id
 *
 * # ⚠️ 允许自引用
 *
 * 集合引用自己(比如"任务的父任务")是合法的 —— 后端 validateRelationConfig
 * 不拦,这里下拉里也不排除自己。
 */
import { useEffect, useState } from 'react'
import { Select, Space, Typography } from 'antd'
import { useI18n } from '@/locales'
import { dynApi } from '@/api/dyn'
import type { DynCollection, DynField, DynOptionsDraft } from '@/types/dyn'

const { Text } = Typography

/**
 * 能做显示名的字段类型 —— 与后端 validateRelationConfig 的白名单对齐
 *
 * json / multiselect / file / relation 存的是序列化文本、JSON 数组、
 * 对象存储 key、别人的 id,拿来做显示名没有意义;后端也会拒,
 * 但前端先一步不给选,用户不用等保存时才挨一个 400。
 */
const displayableTypes = new Set([
  'text',
  'longtext',
  'select',
  'int',
  'number',
  'bool',
  'date',
  'datetime',
])

export interface RelationEditorProps {
  /** 当前选项集草稿 */
  value?: DynOptionsDraft
  /** 变更回调(整份替换) */
  onChange?: (next: DynOptionsDraft) => void
  /** 全部集合(页面已加载的那份;选目标集合用) */
  collections: DynCollection[]
}

export function RelationEditor({ value, onChange, collections }: RelationEditorProps) {
  const { t } = useI18n()
  const draft: DynOptionsDraft = value ?? { choices: [], source: '' }
  const target = draft.relation_target ?? ''
  const displayField = draft.relation_display_field ?? ''

  // 目标集合的在用字段(挑显示字段用)—— 换目标就重拉
  const [targetFields, setTargetFields] = useState<DynField[]>([])
  const [fieldsLoading, setFieldsLoading] = useState(false)

  const activeCols = collections.filter((c) => c.is_active)
  const targetCol = collections.find((c) => c.code === target)

  useEffect(() => {
    if (!targetCol) {
      setTargetFields([])
      return
    }
    let alive = true
    setFieldsLoading(true)
    dynApi
      .listFields(targetCol.id)
      .then((res) => {
        if (alive) setTargetFields(res.filter((f) => f.is_active))
      })
      .catch(() => {
        // ⚠️ **拉不到就给空列表,不炸弹窗** ——
        //   显示字段是可选项;拉不到时用户仍能保存"自动挑文本字段"的配置
        if (alive) setTargetFields([])
      })
      .finally(() => {
        if (alive) setFieldsLoading(false)
      })
    return () => {
      alive = false
    }
  }, [targetCol])

  const selectable = targetFields.filter((f) => displayableTypes.has(f.type))

  return (
    <Space direction="vertical" size={8} style={{ width: '100%' }}>
      <Select
        size="small"
        style={{ width: '100%' }}
        value={target || undefined}
        placeholder={t('page.dyn.designer.relationTargetPlaceholder')}
        showSearch
        optionFilterProp="label"
        onChange={(v) => {
          // 换目标集合:显示字段一起清掉(旧显示字段在别的集合里没有意义)
          onChange?.({ ...draft, relation_target: String(v ?? ''), relation_display_field: undefined })
        }}
        options={activeCols.map((c) => ({ value: c.code, label: `${c.name}(${c.code})` }))}
      />
      <Select
        size="small"
        style={{ width: '100%' }}
        value={displayField || undefined}
        placeholder={t('page.dyn.designer.relationDisplayPlaceholder')}
        loading={fieldsLoading}
        allowClear
        onChange={(v) => {
          onChange?.({ ...draft, relation_display_field: v ? String(v) : undefined })
        }}
        options={selectable.map((f) => ({ value: f.field_key, label: f.label }))}
      />
      <Text type="secondary" style={{ fontSize: 12 }}>
        {t('page.dyn.designer.relationDisplayHelp')}
      </Text>
      {target && !targetCol ? (
        <Text type="warning" style={{ fontSize: 12 }}>
          {t('page.dyn.designer.relationTargetMissing', undefined, { code: target })}
        </Text>
      ) : null}
    </Space>
  )
}

export default RelationEditor
