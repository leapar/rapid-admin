/**
 * 业务表单设计器 —— M152
 *
 * # 这一页解决什么
 *
 * 用户在这里设计一张业务表(集合 + 字段),设计完之后:
 *   · 菜单自动出现(运行期登记的 `menu.dyn.<集合码>`,见 internal/service/dyn_resource.go)
 *   · 增删改查接口自动可用(`/biz/dyn/:collection/records`)
 *   · 权限码自动生成(`dyn:<集合码>:list|read|create|update|delete`)
 *   · 列表管理页自动可用(`/dyn/:code`,见 @/pages/dyn)
 *
 * 换句话说:**这一页是"自己设计业务功能"的入口**,
 * 不用再让开发改代码 + 建表 + 写接口 + 加菜单 + 加权限。
 *
 * # 布局
 *
 * 左:集合列表(搜索 + 选中)  右:选中集合的字段表
 * —— 与「部门管理」页同一套结构(那一页是左树右详情),
 *    因为它们的交互形状一样:**左边选一个,右边编辑它的明细**。
 *
 * # ⚠️ 不可改的东西前端必须跟着后端一起"锁死"
 *
 *   · 集合编码 code —— 它是物理表名(`dyn_<code>`)和资源码的来源
 *   · 字段标识 field_key —— 它是物理列名(`f_<key>`)和前端 JSON 键的来源
 *   · 字段类型 type —— 改类型意味着改列类型(重建表 = 全量数据搬迁)
 *
 * 这三条后端 DTO 里**根本没有对应字段**(改了会被静默忽略),
 * 前端必须自己锁住输入框 —— 否则用户填了个新编码,以为改成功了,
 * 下次进页面又变回旧值,数据还对不上。
 */
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { FC } from 'react'
import {
  App,
  Card,
  Col,
  Empty,
  Input,
  List,
  Row,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { PageHeader } from '@/components/common/PageHeader'
import { EditModal, type EditField } from '@/components/common/EditModal'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { AuthButton } from '@/components/auth/AuthButton'
import { RowActions, type RowAction } from '@/components/common/RowActions'
import { useI18n } from '@/locales'
import { dynApi, parseDynIndexes, parseDynOptionsDraft, serializeDynIndexes, serializeDynOptionsDraft, toDynOptionsDraft } from '@/api/dyn'
import { flowApi } from '@/api/flow'
import type { FlowListItem } from '@/types/approval'
import { PermDyn } from '@/constants/permissions'
import IconSelect from '@/components/common/IconSelect'
import type {
  DynCollection,
  DynField,
  DynFieldType,
  DynIndexDef,
  DynRelationSource,
} from '@/types/dyn'
import { OptionsEditor } from './OptionsEditor'
import { ConstraintsEditor } from './ConstraintsEditor'
import { IndexesEditor } from './IndexesEditor'
import { RelationEditor } from './RelationEditor'

const { Text } = Typography

/** 集合编码 / 字段标识的合法形状 —— 与后端 dynx.ValidateCode 一致 */
const CODE_PATTERN = /^[a-z][a-z0-9_]{0,63}$/

/** 单次拉多少个集合(后端 normPage 上限 200) */
const COLLECTION_PAGE_SIZE = 200

/**
 * 新建字段时的默认类型
 *
 * ⚠️ **写死 'text' 而不是取词表第一项** ——
 *   词表是异步拉回来的,它的到达会让 initialValues 的引用变化,
 *   而 EditModal 有一个"initialValues 变了就 resetFields + setFieldsValue"的副作用
 *   ⇒ 弹窗正开着的时候词表姗姗来迟,用户填了一半的表单会被**清空**。
 *   写死常量就没有这个窗口(后端类型词表的第一项本来就是 text)。
 */
const DEFAULT_FIELD_TYPE = 'text'

const DynDesigner: FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()

  // ===== 集合 =====
  const [collections, setCollections] = useState<DynCollection[]>([])
  const [collectionsLoading, setCollectionsLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined)

  // ===== 字段 =====
  const [fields, setFields] = useState<DynField[]>([])
  const [fieldsLoading, setFieldsLoading] = useState(false)
  const [fieldTypes, setFieldTypes] = useState<DynFieldType[]>([])
  /** 可关联的系统表(后端下发的白名单 —— 前端不许硬编码表名) */
  const [relationSources, setRelationSources] = useState<DynRelationSource[]>([])

  /**
   * ⚠️ **字段类型草稿** —— 弹窗里"当前选的是哪个类型"
   *
   * 为什么不能直接读表单的值:
   *   EditModal 传给 render 的 `value` 是**它自己那次渲染**时的表单快照,
   *   而"选项集编辑器要不要出现"这件事必须由**页面**来决定
   *   (字段数组是页面算出来传给 EditModal 的)。
   *   ⇒ 页面必须有一份"当前类型"的副本,类型一改就同步过来。
   *
   * 为什么不用 Form.useWatch:
   *   useWatch 依赖"表单已挂载"(Modal 是 destroyOnHidden,关着就没有 Form.Item),
   *   弹窗刚打开那一帧拿不到值,字段数组会先按"无类型"算一遍 ——
   *   而"无类型"会走到"不显示选项集",用户选了单选之后才补上,闪一下。
   *   草稿状态在**打开弹窗时**就被设好了,没有这一帧。
   */
  const [fieldTypeDraft, setFieldTypeDraft] = useState(DEFAULT_FIELD_TYPE)

  // ===== 弹窗 =====
  const [collectionModal, setCollectionModal] = useState<{ open: boolean; data?: DynCollection | null }>({
    open: false,
  })
  const [fieldModal, setFieldModal] = useState<{ open: boolean; data?: DynField | null }>({ open: false })
  const [deletingCollection, setDeletingCollection] = useState<DynCollection | null>(null)
  const [deletingField, setDeletingField] = useState<DynField | null>(null)

  const loadCollections = useCallback(async (kw?: string) => {
    setCollectionsLoading(true)
    try {
      const res = await dynApi.listCollections({
        keyword: kw ?? '',
        page: 1,
        size: COLLECTION_PAGE_SIZE,
      })
      setCollections(res?.list ?? [])
    } catch {
      setCollections([])
    } finally {
      setCollectionsLoading(false)
    }
  }, [])

  const loadFields = useCallback(async (collectionId?: string) => {
    if (!collectionId) {
      setFields([])
      return
    }
    setFieldsLoading(true)
    try {
      const list = await dynApi.listFields(collectionId)
      setFields(Array.isArray(list) ? list : [])
    } catch {
      setFields([])
    } finally {
      setFieldsLoading(false)
    }
  }, [])

  // 字段类型词表 —— **只拉一次**(它是框架级的固定词表,不会随集合变)
  useEffect(() => {
    dynApi
      .fieldTypes()
      .then((list) => setFieldTypes(Array.isArray(list) ? list : []))
      .catch(() => setFieldTypes([]))
  }, [])

  // 可关联的系统表 —— 同样只拉一次(它是后端的固定注册表)
  //
  // ⚠️ **拉不到就当没有** —— 设计器照样能用(只是不能选来源),
  //   而"整个页面打不开"严重得多。
  useEffect(() => {
    dynApi
      .relationSources()
      .then((list) => setRelationSources(Array.isArray(list) ? list : []))
      .catch(() => setRelationSources([]))
  }, [])

  // ===== 审批模板(2026-11-03,集合绑定审批模板)=====
  // 只拉在用的 —— 停用的模板不该出现在"新绑定"的候选里;
  // 拉不到就当没有(集合不绑模板照样能用,审批是可选能力)。
  const [flowTemplates, setFlowTemplates] = useState<FlowListItem[]>([])
  useEffect(() => {
    flowApi
      .list({ page: 1, size: 200, is_active: true })
      .then((res) => setFlowTemplates(Array.isArray(res.list) ? res.list : []))
      .catch(() => setFlowTemplates([]))
  }, [])

  useEffect(() => {
    loadCollections()
  }, [loadCollections])

  // 选中集合变化 → 拉它的字段
  useEffect(() => {
    loadFields(selectedId)
  }, [selectedId, loadFields])

  const selected = useMemo(
    () => collections.find((c) => c.id === selectedId) ?? null,
    [collections, selectedId],
  )

  // ===== 集合操作 =====
  /**
   * ⚠️ **行级权限两个开关的互斥草稿** ——
   * owner_scoped 与 public_read 互斥(匿名调用者没有 owner,
   * 公开了要么全裸要么全空)。EditModal 的 custom render 只拿得到
   * 自己那个字段的值,兄弟字段的值只能走组件状态同步
   * (同 fieldTypeDraft 的道理:渲染期要用的缓存不能进 effect 补)。
   */
  const [scopeDraft, setScopeDraft] = useState({ owner: false, pub: false })
  const onCreateCollection = () => {
    setScopeDraft({ owner: false, pub: false })
    setCollectionModal({ open: true, data: null })
  }
  const onEditCollection = (c: DynCollection) => {
    setScopeDraft({ owner: c.owner_scoped === true, pub: c.public_read === true })
    setCollectionModal({ open: true, data: c })
  }
  const onDeleteCollection = (c: DynCollection) => setDeletingCollection(c)

  // ===== 字段操作 =====
  const onCreateField = () => {
    if (!selected) {
      message.warning(t('page.dyn.designer.selectCollectionFirst'))
      return
    }
    // ⚠️ 先把类型草稿摆正,再开弹窗 —— 否则弹窗第一帧会按"上一个字段的类型"算字段数组
    setFieldTypeDraft(DEFAULT_FIELD_TYPE)
    setFieldModal({ open: true, data: null })
  }
  /**
   * ⚠️ **这两个必须是 useCallback** ——
   *   它们被 `fieldActionsFor` 的 useMemo 依赖着。
   *   普通函数每次渲染都是新引用 ⇒ useMemo 永远失效(缓存等于没有),
   *   而且 react-hooks/exhaustive-deps 会因为"依赖不稳定"直接报警。
   */
  const onEditField = useCallback((f: DynField) => {
    setFieldTypeDraft(f.type)
    setFieldModal({ open: true, data: f })
  }, [])
  const onDeleteField = useCallback((f: DynField) => setDeletingField(f), [])

  // ===== 集合编辑字段 =====
  const collectionEditFields: EditField[] = useMemo(
    () => [
      {
        name: 'code',
        label: t('page.dyn.designer.code'),
        required: true,
        disabled: !!collectionModal.data,
        help: t('page.dyn.designer.codeHelp'),
        rules: [
          { required: true },
          {
            pattern: CODE_PATTERN,
            message: t('page.dyn.designer.codeInvalid'),
          },
        ],
      },
      { name: 'name', label: t('page.dyn.designer.name'), required: true, rules: [{ required: true }] },
      { name: 'description', label: t('page.dyn.designer.descriptionField'), type: 'textarea' },
      {
        // ⚠️ **custom 而不是内置 switch** —— 互斥禁用需要看兄弟字段的值,
        // 走 scopeDraft(见上面 state 处的注释);后端也会拒绝这个组合,双保险。
        name: 'owner_scoped',
        label: t('page.dyn.designer.ownerScoped'),
        type: 'custom',
        help: t('page.dyn.designer.ownerScopedHelp'),
        render: ({ value, onChange }) => (
          <Switch
            checked={value === true}
            disabled={scopeDraft.pub}
            onChange={(v) => {
              onChange(v)
              setScopeDraft((s) => ({ ...s, owner: v }))
            }}
          />
        ),
      },
      {
        name: 'public_read',
        label: t('page.dyn.designer.publicRead'),
        type: 'custom',
        help: t('page.dyn.designer.publicReadHelp'),
        render: ({ value, onChange }) => (
          <Switch
            checked={value === true}
            disabled={scopeDraft.owner}
            onChange={(v) => {
              onChange(v)
              setScopeDraft((s) => ({ ...s, pub: v }))
            }}
          />
        ),
      },
      {
        name: 'icon',
        label: t('page.dyn.designer.icon'),
        /**
         * ⚠️ **选项画的是真实图标**(2026-11-02,用户报"都是文字")——
         *   与资源管理共用同一个 IconSelect,词表是唯一真源。
         *   动态集合**必须**有图标(侧边栏菜单项靠它显示)⇒ 不传 noneOption。
         */
        type: 'custom',
        render: ({ value, onChange }) => (
          <IconSelect value={value as string | null | undefined} onChange={(v) => onChange(v)} />
        ),
      },
      { name: 'sort', label: t('page.dyn.designer.sort'), type: 'number' },
      {
        name: 'approval_template_id',
        label: t('page.dyn.designer.approvalTemplate'),
        type: 'select',
        help: t('page.dyn.designer.approvalTemplateHelp'),
        options: [
          { value: '0', label: t('page.dyn.designer.approvalTemplateNone') },
          ...flowTemplates.map((f) => ({ value: f.id, label: `${f.name} (${f.code})` })),
        ],
      },
      /**
       * 索引配置(M2)—— **只在编辑已有集合时出现** ——
       * 新建集合还没有任何字段,索引引用的列必然不存在,
       * 后端也只在改集合的入口收这个字段。
       */
      ...(collectionModal.data
        ? [
            {
              name: 'indexes',
              label: t('page.dyn.designer.indexes'),
              type: 'custom' as const,
              help: t('page.dyn.designer.indexesHelp'),
              render: ({ value, onChange }: { value?: unknown; onChange?: (v: unknown) => void }) => (
                <IndexesEditor
                  fieldOptions={fields
                    .filter((f) => f.is_active)
                    .map((f) => ({ value: f.field_key, label: `${f.label} (${f.field_key})` }))}
                  value={Array.isArray(value) ? (value as DynIndexDef[]) : []}
                  onChange={(next) => onChange?.(next)}
                />
              ),
            },
          ]
        : []),
    ],
    [t, collectionModal.data, flowTemplates, fields, scopeDraft],
  )

  /**
   * ⚠️ **initialValues 必须用 useMemo 固定住引用** ——
   *   EditModal 有一个副作用:initialValues 的引用一变就
   *   `resetFields() + setFieldsValue()`(见 EditModal 里的那个 useEffect)。
   *   引用是**内联对象字面量**时,每次渲染都是新对象 ⇒ 那个副作用每次渲染都跑一遍
   *   ⇒ **用户刚填的内容会被清空**。
   *   所以这里必须 memo,让引用只在"打开的是另一条数据"时才变。
   */
  const collectionInitialValues = useMemo<Record<string, unknown>>(
    () =>
      ({
        ...(collectionModal.data ?? {
          code: '',
          name: '',
          description: '',
          owner_scoped: false,
          public_read: false,
          icon: 'table',
          sort: 0,
        }),
        // ⚠️ 后端是 0 = 不绑定,下拉框里 '0' = 不绑定 —— 没绑过的集合回落到 '0'
        approval_template_id: collectionModal.data?.approval_template_id ?? '0',
        // 索引定义(M2):JSON 文本 ⇒ 结构化数组(解析失败返空数组,不炸弹窗)
        indexes: parseDynIndexes(collectionModal.data?.indexes),
      }) as Record<string, unknown>,
    [collectionModal.data],
  )

  // ===== 字段编辑字段 =====
  /**
   * ⚠️ **选项集编辑器只在 select / multiselect 时出现** ——
   *   其它类型的字段带着一个空的选项集,用户会以为"填了没用"，
   *   而后端存进去的那个空 JSON 文本还会在读回来时被解析一次。
   *
   * ⚠️ **类型控件用 custom 而不是内置 select** ——
   *   内置 select 只把值写进表单,页面**收不到通知**,
   *   于是"选了单选 → 选项集编辑器不出现"这件事不会发生。
   *   custom 渲染里除了写表单,还顺手把草稿状态改了(见 fieldTypeDraft)。
   */
  const fieldEditFields: EditField[] = useMemo(() => {
    // ⚠️ **"要不要选项集"以**后端词表**为准** ——
    //   `has_options` 是后端 dynx.Spec 里的那一列,前端硬编码一份
    //   ("只有 select/multiselect 要选项集")的话,后端加一个要选项集的类型时,
    //   前端不会出现选项集编辑器 ⇒ 用户配不了那个字段,而且**两边对不上没人知道**。
    //
    //   词表还没到(第一帧)时回落到硬编码判断 ——
    //   否则会出现"选了单选 → 选项集编辑器不出现 → 词表到了才出现"的闪一下。
    const spec = fieldTypes.find((ft) => ft.type === fieldTypeDraft)
    const needsOptions = spec
      ? spec.has_options
      : fieldTypeDraft === 'select' || fieldTypeDraft === 'multiselect'
    // 值约束(M1):文本/数字类字段 —— 与后端 dynx.constraintsByType 对齐
    const needsConstraints =
      !needsOptions &&
      ['text', 'longtext', 'int', 'number'].includes(spec ? spec.type : fieldTypeDraft)
    // 关联字段(M5):配目标集合与显示字段 —— 词表没到时按类型直判(同 needsOptions 的回落)
    const needsRelation =
      !needsOptions && (spec ? spec.type === 'relation' : fieldTypeDraft === 'relation')
    const typeOptions = fieldTypes.map((ft) => ({
      value: ft.type,
      label: t(`page.dyn.fieldType.${ft.type}`, ft.label),
    }))
    const fields: EditField[] = [
      {
        name: 'field_key',
        label: t('page.dyn.designer.fieldKey'),
        required: true,
        disabled: !!fieldModal.data,
        help: t('page.dyn.designer.fieldKeyHelp'),
        rules: [
          { required: true },
          { pattern: CODE_PATTERN, message: t('page.dyn.designer.fieldKeyInvalid') },
        ],
      },
      { name: 'label', label: t('page.dyn.designer.label'), required: true, rules: [{ required: true }] },
      {
        name: 'type',
        label: t('page.dyn.designer.type'),
        required: true,
        type: 'custom',
        help: t('page.dyn.designer.typeHelp'),
        // ⚠️ **EditModal 只认 rules,不看 required** ——
        //   EditField.required 是声明性的(接口里有),但 renderField 里根本没读它。
        //   不写 rules 的话这个字段**可以留空提交**,后端才报错。
        rules: [{ required: true }],
        // ⚠️ 必须返回 { value } —— 给了 getValueProps 就等于**替换**了
        //   rc-field-form 的默认实现,不返回 value 的话控件会变成完全不受控
        getValueProps: (v) => ({ value: typeof v === 'string' ? v : '' }),
        render: ({ onChange }) => (
          <Select
            value={fieldTypeDraft}
            disabled={!!fieldModal.data}
            options={typeOptions}
            style={{ width: '100%' }}
            onChange={(v) => {
              onChange(v)
              setFieldTypeDraft(v)
            }}
          />
        ),
      },
      { name: 'required', label: t('page.dyn.designer.required'), type: 'switch' },
    ]
    if (needsOptions) {
      fields.push({
        name: 'options',
        label: t('page.dyn.designer.options'),
        type: 'custom',
        help: t('page.dyn.designer.optionsHelp'),
        // ⚠️ **值形态是 `{choices, source}`** —— 不能只传数组:
        //   关联表来源的字段候选项是空的,只传数组的话来源配置会在保存时丢掉。
        getValueProps: (v) => ({ value: toDynOptionsDraft(v) }),
        render: ({ value, onChange }) => (
          <OptionsEditor
            value={toDynOptionsDraft(value)}
            sources={relationSources}
            onChange={(next) => onChange(next)}
          />
        ),
      })
    }
    if (needsConstraints) {
      fields.push({
        name: 'options',
        label: t('page.dyn.designer.constraints'),
        type: 'custom',
        help: t('page.dyn.designer.constraintsHelp'),
        // ⚠️ 与选项集同一个表单字段名 `options` —— 约束和候选项共存于
        //   同一个 options JSON(后端 dynx.Options),落点一致才不会互相冲掉
        getValueProps: (v) => ({ value: toDynOptionsDraft(v) }),
        render: ({ value, onChange }) => (
          <ConstraintsEditor
            fieldType={fieldTypeDraft}
            value={toDynOptionsDraft(value)}
            onChange={(next) => onChange(next)}
          />
        ),
      })
    }
    if (needsRelation) {
      fields.push({
        name: 'options',
        label: t('page.dyn.designer.relation'),
        type: 'custom',
        help: t('page.dyn.designer.relationHelp'),
        // ⚠️ 同一个表单字段名 `options` —— 关联配置与约束共存于同一个 options JSON
        getValueProps: (v) => ({ value: toDynOptionsDraft(v) }),
        render: ({ value, onChange }) => (
          <RelationEditor
            value={toDynOptionsDraft(value)}
            collections={collections}
            onChange={(next) => onChange(next)}
          />
        ),
      })
    }
    fields.push({
      name: 'default_value',
      label: t('page.dyn.designer.defaultValue'),
      help: t('page.dyn.designer.defaultValueHelp'),
    })
    fields.push({ name: 'sort', label: t('page.dyn.designer.sort'), type: 'number' })
    if (fieldModal.data) {
      fields.push({ name: 'is_active', label: t('common.enable'), type: 'switch' })
    }
    return fields
  }, [t, fieldTypes, fieldModal.data, fieldTypeDraft, relationSources, collections])

  /**
   * ⚠️ **依赖里不能有 fieldTypes** ——
   *   字段类型词表是异步拉回来的,它到达时会让这个 memo 重算 ⇒ initialValues 换引用
   *   ⇒ EditModal 的副作用把用户填了一半的表单清空(见 collectionInitialValues 的注释)。
   *   所以新建字段的默认类型写死 DEFAULT_FIELD_TYPE(它本来就是词表第一项)。
   */
  const fieldInitialValues = useMemo<Record<string, unknown>>(
    () =>
      (fieldModal.data
        ? {
            field_key: fieldModal.data.field_key,
            label: fieldModal.data.label,
            type: fieldModal.data.type,
            required: fieldModal.data.required,
            options: parseDynOptionsDraft(fieldModal.data.options),
            default_value: fieldModal.data.default_value,
            sort: fieldModal.data.sort,
            is_active: fieldModal.data.is_active,
          }
        : {
            field_key: '',
            label: '',
            type: DEFAULT_FIELD_TYPE,
            required: false,
            options: [],
            default_value: '',
            sort: 0,
          }) as Record<string, unknown>,
    [fieldModal.data],
  )

  // ===== 字段表列 =====
  /**
   * ⚠️ **必须定义在 fieldColumns 之前** ——
   *   fieldColumns 的 useMemo 工厂里要用 fieldActionsFor,
   *   而 useMemo 的工厂是**渲染时**执行的 ⇒ 后者若是 const,
   *   在定义之前引用就是 TDZ(ReferenceError: Cannot access 'fieldActionsFor' before initialization)。
   */
  const fieldActionsFor = useMemo(
    () =>
      (): RowAction<DynField>[] => [
        {
          key: 'edit',
          label: t('common.edit'),
          perm: PermDyn.FIELD_UPDATE,
          onClick: onEditField,
        },
        {
          key: 'delete',
          label: t('common.delete'),
          perm: PermDyn.FIELD_DELETE,
          danger: true,
          onClick: onDeleteField,
        },
      ],
    [t, onEditField, onDeleteField],
  )

  /**
   * ⚠️ **列标题的键必须落在 field.* / common.* 里** ——
   *   这是全仓统一的约定(table-column-i18n 守门网),列标题是**框架级**词汇,
   *   不是某个页面的专属文案 ⇒ 用共享组,别开 page.* 的新组。
   */
  const fieldColumns: ColumnsType<DynField> = useMemo(
    () => [
      {
        title: t('field.name'),
        dataIndex: 'label',
        width: 180,
        render: (v: string, row: DynField) => (
          <Space size={6}>
            <Text>{v}</Text>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {row.field_key}
            </Text>
            {!row.is_active ? <Tag>{t('dict.status.disabled')}</Tag> : null}
          </Space>
        ),
      },
      {
        title: t('field.type'),
        dataIndex: 'type',
        width: 120,
        render: (v: string) => t(`page.dyn.fieldType.${v}`, v),
      },
      {
        title: t('field.required'),
        dataIndex: 'required',
        width: 80,
        render: (v: boolean) => (v ? t('common.yes') : t('common.no')),
      },
      { title: t('field.sort'), dataIndex: 'sort', width: 70 },
      {
        title: t('common.action'),
        dataIndex: '__actions',
        width: 160,
        render: (_: unknown, row: DynField) => (
          <RowActions<DynField> row={row} actions={fieldActionsFor()} />
        ),
      },
    ],
    [t, fieldActionsFor],
  )

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <PageHeader description={t('page.dyn.designer.description')} />

      <Row gutter={16} style={{ flex: 1, minHeight: 0 }}>
        {/* ===== 左:集合列表 ===== */}
        <Col span={8} style={{ height: '100%', minWidth: 0 }}>
          <Card
            size="small"
            style={{ height: 'calc(100% - 8px)', padding: '14px 16px' }}
            styles={{
              header: { padding: '0' },
              body: { padding: '14px 16px', height: 'calc(100% - 44px)', overflow: 'auto' },
            }}
            title={
              <Space size={6}>
                <Text strong>{t('page.dyn.designer.collections')}</Text>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  ({collections.length})
                </Text>
              </Space>
            }
            extra={
              <AuthButton
                size="small"
                type="primary"
                icon={<PlusOutlined />}
                perm={PermDyn.COLLECTION_CREATE}
                onClick={onCreateCollection}
              >
                {t('page.dyn.designer.newCollection')}
              </AuthButton>
            }
          >
            <Input.Search
              placeholder={t('page.dyn.designer.searchPlaceholder')}
              allowClear
              size="small"
              style={{ marginBottom: 8 }}
              onSearch={(v) => {
                setKeyword(v.trim())
                void loadCollections(v.trim())
              }}
            />
            <List
              size="small"
              bordered
              loading={collectionsLoading}
              dataSource={collections}
              locale={{ emptyText: t('page.dyn.designer.emptyCollections') }}
              renderItem={(c) => (
                <List.Item
                  style={{
                    cursor: 'pointer',
                    background: c.id === selectedId ? 'var(--app-selected-bg)' : undefined,
                    paddingRight: 8,
                  }}
                  onClick={() => setSelectedId(c.id)}
                  actions={[
                    /**
                     * ⚠️ **必须是 AuthButton,不能是裸 Button + perm={undefined}** ——
                     *   裸 antd Button 根本没有 perm 这个属性,写了也只是被 TS 忽略;
                     *   结果就是**谁都能改集合**(后端会拦,但界面不该露)。
                     *   ⚠️ 这里不能用 Tooltip 包 AuthButton ——
                     *   权限不足时 AuthButton 返回 null,Tooltip 会拿 null 去 cloneElement 而崩。
                     */
                    <AuthButton
                      key="edit"
                      type="link"
                      size="small"
                      icon={<EditOutlined />}
                      perm={PermDyn.COLLECTION_UPDATE}
                      aria-label={t('common.edit')}
                      onClick={(e) => {
                        e.stopPropagation()
                        onEditCollection(c)
                      }}
                    />,
                    /**
                     * ⚠️ 删集合是**不可逆**的(连带物理表 dyn_<code>)——
                     *   所以按钮只露出入口,真正确认在 ConfirmDialog 里,
                     *   而且删除按钮要**红色 danger** 一眼能认出来。
                     */
                    <AuthButton
                      key="delete"
                      type="link"
                      size="small"
                      danger
                      icon={<DeleteOutlined />}
                      perm={PermDyn.COLLECTION_DELETE}
                      aria-label={t('common.delete')}
                      onClick={(e) => {
                        e.stopPropagation()
                        onDeleteCollection(c)
                      }}
                    />,
                  ]}
                >
                  <Space direction="vertical" size={0} style={{ width: '100%', minWidth: 0 }}>
                    <Space size={6} style={{ width: '100%' }}>
                      <Text strong>{c.name}</Text>
                      {!c.is_active ? <Tag>{t('dict.status.disabled')}</Tag> : null}
                    </Space>
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      {c.code}
                    </Text>
                  </Space>
                </List.Item>
              )}
            />
          </Card>
        </Col>

        {/* ===== 右:字段表 ===== */}
        <Col span={16} style={{ height: '100%', minWidth: 0 }}>
          <Card
            size="small"
            style={{ height: 'calc(100% - 8px)', padding: '14px 16px' }}
            styles={{
              header: { padding: '0' },
              body: { padding: '14px 16px', height: 'calc(100% - 44px)', overflow: 'auto' },
            }}
            title={
              <Space size={6}>
                <Text strong>{t('page.dyn.designer.fields')}</Text>
                {selected ? (
                  <Tag color="blue" style={{ margin: 0 }}>
                    {selected.name}
                  </Tag>
                ) : null}
                <Text type="secondary" style={{ fontSize: 12 }}>
                  ({fields.length})
                </Text>
              </Space>
            }
            extra={
              <Space size={4}>
                <AuthButton
                  size="small"
                  icon={<ReloadOutlined />}
                  onClick={() => {
                    void loadCollections(keyword)
                    void loadFields(selectedId)
                  }}
                >
                  {t('common.refresh')}
                </AuthButton>
                <AuthButton
                  size="small"
                  type="primary"
                  icon={<PlusOutlined />}
                  perm={PermDyn.FIELD_CREATE}
                  disabled={!selected}
                  onClick={onCreateField}
                >
                  {t('page.dyn.designer.newField')}
                </AuthButton>
              </Space>
            }
          >
            {!selected ? (
              <Empty description={t('page.dyn.designer.selectCollectionHint')} />
            ) : (
              <Table<DynField>
                size="small"
                rowKey="id"
                columns={fieldColumns}
                dataSource={fields}
                loading={fieldsLoading}
                pagination={false}
                scroll={{ y: 320 }}
                locale={{ emptyText: t('page.dyn.designer.emptyFields') }}
              />
            )}
          </Card>
        </Col>
      </Row>

      {/* ===== 集合编辑弹窗 ===== */}
      <EditModal<Record<string, unknown>>
        title={collectionModal.data ? t('page.dyn.designer.editCollection') : t('page.dyn.designer.newCollection')}
        open={collectionModal.open}
        initialValues={collectionInitialValues}
        fields={collectionEditFields}
        onCancel={() => setCollectionModal({ open: false })}
        onSubmit={async (vals) => {
          if (collectionModal.data) {
            await dynApi.updateCollection(collectionModal.data.id, {
              name: String(vals.name ?? ''),
              description: String(vals.description ?? ''),
              owner_scoped: vals.owner_scoped === true,
              public_read: vals.public_read === true,
              icon: String(vals.icon ?? 'table'),
              sort: Number(vals.sort ?? 0),
              // '0' = 不绑定(后端按 0 处理)
              approval_template_id: String(vals.approval_template_id ?? '0'),
              // 索引(M2):始终传 —— 后端指针语义,恒传才能表达"清空全部";
              // 保存时后端同步物理 DDL,唯一索引撞存量重复值会报可读的错
              indexes: serializeDynIndexes(vals.indexes as DynIndexDef[] | undefined),
            })
          } else {
            await dynApi.createCollection({
              code: String(vals.code ?? '').trim(),
              name: String(vals.name ?? ''),
              description: String(vals.description ?? ''),
              owner_scoped: vals.owner_scoped === true,
              public_read: vals.public_read === true,
              icon: String(vals.icon ?? 'table'),
              sort: Number(vals.sort ?? 0),
              approval_template_id: String(vals.approval_template_id ?? '0'),
            })
          }
          message.success(t('common.success'))
          setCollectionModal({ open: false })
          await loadCollections(keyword)
        }}
      />

      {/* ===== 字段编辑弹窗 ===== */}
      <EditModal<Record<string, unknown>>
        title={fieldModal.data ? t('page.dyn.designer.editField') : t('page.dyn.designer.newField')}
        open={fieldModal.open}
        initialValues={fieldInitialValues}
        fields={fieldEditFields}
        onCancel={() => setFieldModal({ open: false })}
        onSubmit={async (vals) => {
          if (!selected) return
          const payload = {
            label: String(vals.label ?? ''),
            required: vals.required === true,
            options: serializeDynOptionsDraft(toDynOptionsDraft(vals.options)),
            default_value: String(vals.default_value ?? ''),
            sort: Number(vals.sort ?? 0),
          }
          if (fieldModal.data) {
            await dynApi.updateField(fieldModal.data.id, {
              ...payload,
              is_active: vals.is_active !== false,
            })
          } else {
            await dynApi.createField(selected.id, {
              ...payload,
              field_key: String(vals.field_key ?? '').trim(),
              type: String(vals.type ?? 'text'),
            })
          }
          message.success(t('common.success'))
          setFieldModal({ open: false })
          await loadFields(selected.id)
        }}
      />

      {/* ===== 删集合确认 ===== */}
      <ConfirmDialog
        open={!!deletingCollection}
        danger
        title={`${t('common.delete')} ${deletingCollection?.name}?`}
        description={t('page.dyn.designer.deleteCollectionTip')}
        onCancel={() => setDeletingCollection(null)}
        onOk={async () => {
          if (!deletingCollection) return
          await dynApi.deleteCollection(deletingCollection.id)
          message.success(t('common.success'))
          setDeletingCollection(null)
          if (selectedId === deletingCollection.id) setSelectedId(undefined)
          await loadCollections(keyword)
        }}
      />

      {/* ===== 删字段确认 ===== */}
      <ConfirmDialog
        open={!!deletingField}
        danger
        title={`${t('common.delete')} ${deletingField?.label}?`}
        description={t('page.dyn.designer.deleteFieldTip')}
        onCancel={() => setDeletingField(null)}
        onOk={async () => {
          if (!deletingField) return
          await dynApi.deleteField(deletingField.id)
          message.success(t('common.success'))
          setDeletingField(null)
          await loadFields(selectedId)
        }}
      />
    </div>
  )
}

export default DynDesigner
