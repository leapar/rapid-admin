/**
 * 选项集编辑器 —— 单选 / 多选字段的候选项
 *
 * M152(业务表单设计器)。单独拆一个文件的原因:`react-refresh/only-export-components`
 * 规则要求 .tsx 文件只导出组件(本仓 lint 是 `--max-warnings 0`,警告也算失败),
 * 而这个编辑器要在页面里被当 `type: 'custom'` 的 render 用。
 *
 * ⚠️ **值形态是 `DynOptionsDraft`(候选项 + 来源),不是后端存的那种 JSON 文本** ——
 *   提交前由 @/api/dyn 的 serializeDynOptionsDraft 转成
 *   `{"choices":[{"value":"a","label":"甲"}]}` 或
 *   `{"choices":[],"source":{"kind":"table","key":"user"}}`。
 *   形状必须与后端 `dynx.Options` 一致(后端反序列化后直接读 `.Choices`),
 *   写成裸数组的话后端解析出空选项集 ⇒ 单选存不进值且**不报错**。
 *
 * # ⚠️ 两种来源互斥
 *
 * 「手工添加」与「关联系统表」是**二选一**:
 *   · 手工 —— 候选项写死在字段定义里,写进去的值永远应该在里面
 *   · 关联表 —— 候选项是**读的时候现查**的(表里新增一行,表单里立刻多一个选项)
 * 切来源时另一边的内容会被清掉(手工候选项在关联表模式下没有意义)。
 */
import { Button, Input, Radio, Select, Space, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'
import type { DynChoice, DynOptionsDraft, DynRelationSource } from '@/types/dyn'

const { Text } = Typography

export interface OptionsEditorProps {
  /** 当前选项集 */
  value?: DynOptionsDraft
  /** 变更回调(整份替换) */
  onChange?: (next: DynOptionsDraft) => void
  /** 可关联的系统表(后端下发) */
  sources?: DynRelationSource[]
}

export function OptionsEditor({ value, onChange, sources }: OptionsEditorProps) {
  const { t } = useI18n()
  const draft: DynOptionsDraft = value ?? { choices: [], source: '' }
  const items = Array.isArray(draft.choices) ? draft.choices : []
  const sourceKey = draft.source ?? ''
  const list = Array.isArray(sources) ? sources : []

  const patch = (index: number, next: Partial<DynChoice>) => {
    const nextItems = items.map((it, i) => (i === index ? { ...it, ...next } : it))
    onChange?.({ choices: nextItems, source: sourceKey })
  }

  const remove = (index: number) => {
    onChange?.({ choices: items.filter((_, i) => i !== index), source: sourceKey })
  }

  const add = () => {
    onChange?.({ choices: [...items, { value: '', label: '' }], source: sourceKey })
  }

  /** 切来源:另一边的内容一起清掉(两种来源互斥) */
  const switchSource = (next: 'manual' | 'table') => {
    if (next === 'manual') {
      onChange?.({ choices: [], source: '' })
    } else {
      onChange?.({ choices: [], source: sourceKey || list.find((s) => s.readable)?.key || '' })
    }
  }

  return (
    <Space direction="vertical" size={8} style={{ width: '100%' }}>
      <Radio.Group
        size="small"
        value={sourceKey ? 'table' : 'manual'}
        onChange={(e) => switchSource(e.target.value === 'table' ? 'table' : 'manual')}
        optionType="button"
        buttonStyle="solid"
      >
        <Radio.Button value="manual">{t('page.dyn.options.manual')}</Radio.Button>
        <Radio.Button value="table">{t('page.dyn.options.table')}</Radio.Button>
      </Radio.Group>

      {sourceKey ? (
        <SourcePicker
          sourceKey={sourceKey}
          sources={list}
          onChange={(key) => onChange?.({ choices: [], source: key })}
        />
      ) : (
        <>
          {items.length === 0 ? (
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t('page.dyn.options.empty')}
            </Text>
          ) : null}
          {items.map((item, index) => (
            <Space key={index} size={8} align="start" style={{ width: '100%' }}>
              <Input
                placeholder={t('page.dyn.options.valuePlaceholder')}
                value={item.value}
                onChange={(e) => patch(index, { value: e.target.value })}
                style={{ width: 180 }}
              />
              <Input
                placeholder={t('page.dyn.options.labelPlaceholder')}
                value={item.label}
                onChange={(e) => patch(index, { label: e.target.value })}
                style={{ width: 180 }}
              />
              <Button
                size="small"
                type="text"
                danger
                icon={<DeleteOutlined />}
                onClick={() => remove(index)}
                aria-label={t('page.dyn.options.remove')}
              />
            </Space>
          ))}
          <Button size="small" icon={<PlusOutlined />} onClick={add}>
            {t('page.dyn.options.add')}
          </Button>
          {/**
           * ⚠️ **重复值必须在这里挡住** ——
           *   重复的选项值会让"这条记录的单选值是哪个"变得有歧义,
           *   而这种歧义在数据存进去之后是**查不出来**的
           *   (后端 dynx.Options.Validate 也拦,但那时已经点了保存、弹了 toast)。
           *   前端先提示一句,用户当场就知道是哪两个重了。
           */}
          {hasDuplicateValue(items) ? (
            <Text type="danger" style={{ fontSize: 12 }}>
              {t('page.dyn.options.duplicate')}
            </Text>
          ) : null}
        </>
      )}
    </Space>
  )
}

/**
 * 选一张系统表作为选项来源
 *
 * ⚠️ **读不了的表要标出来,而不是藏起来** ——
 *   藏起来的话用户会以为"系统里只有这几张表能选",
 *   而真实原因是"你这个账号没有那张表的权限"。
 *   ⇒ 不可读的仍然列出来,但 disabled,并说明缺哪个权限码。
 */
function SourcePicker({
  sourceKey,
  sources,
  onChange,
}: {
  sourceKey: string
  sources: DynRelationSource[]
  onChange: (key: string) => void
}) {
  const { t } = useI18n()
  const current = sources.find((s) => s.key === sourceKey)
  return (
    <Space direction="vertical" size={4} style={{ width: '100%' }}>
      <Select
        size="small"
        style={{ width: '100%' }}
        value={sourceKey || undefined}
        placeholder={t('page.dyn.options.sourcePlaceholder')}
        onChange={(v) => onChange(String(v ?? ''))}
        options={sources.map((s) => ({
          value: s.key,
          label: s.readable ? s.label : `${s.label}(${s.resource_code})`,
          disabled: !s.readable,
        }))}
      />
      {current ? (
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t('page.dyn.options.sourceTable', undefined, {
            table: current.table,
            column: current.value_column,
          })}
        </Text>
      ) : null}
      {current && !current.readable ? (
        <Text type="warning" style={{ fontSize: 12 }}>
          {t('page.dyn.options.sourceUnreadable', undefined, { perm: current.resource_code })}
        </Text>
      ) : null}
    </Space>
  )
}

/** 选项值是否重复 */
function hasDuplicateValue(items: DynChoice[]): boolean {
  const seen = new Set<string>()
  for (const it of items) {
    const v = it.value.trim()
    if (!v) continue
    if (seen.has(v)) return true
    seen.add(v)
  }
  return false
}

export default OptionsEditor
