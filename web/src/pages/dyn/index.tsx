/**
 * 通用业务记录页 —— M152
 *
 * # 这一页解决什么
 *
 * 用户在「业务表单设计器」里设计好一张业务表之后:
 *   · 菜单自动出现(运行期登记的 `menu.dyn.<集合码>`)
 *   · 增删改查接口自动可用(`/biz/dyn/:collection/records`)
 *   · 权限码自动生成(`dyn:<集合码>:list|read|create|update|delete`)
 *   · **这一页就是那个集合的列表管理界面**(`/dyn/:code`)
 *
 * ⇒ 这一页**不含任何业务代码** —— 列、表单、校验全由运行期的 schema 决定。
 *   新增一个业务集合 = 在设计器里点几下,这一页什么都不用改。
 *
 * # ⚠️ 为什么列标题不是 t() 而是字段的显示名
 *
 * 列标题来自 `schema.fields[].label` —— 那是**用户自己在设计器里填的**,
 * 是数据不是源码文案 ⇒ 走 i18n 反而是错的(翻成英文就与用户填的不一致了)。
 * 全仓的"列标题必须接 i18n"守门网只管**源码里写死的**字面量,动态标题不在其列。
 *
 * # ⚠️ 权限码是运行期拼出来的
 *
 * `dyn:<集合码>:<动作>` —— 集合码是用户起名、运行期才有的,
 * 编译期枚举不了(见 @/api/dyn 的 dynRecordPerm)。
 * 好在前端权限 store 里装的就是后端下发的 api 资源码,含运行期生成的那几条
 * ⇒ `has('dyn:' + code + ':create')` 在运行期是真的能判的。
 */
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { FC } from 'react'
import { useParams } from 'react-router-dom'
import { App, Button, Empty, Input, Modal, Radio, Select, Space, Table, Tag, Upload } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  ApiOutlined,
  DeleteOutlined,
  DownloadOutlined,
  PlusOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal } from '@/components/common/EditModal'
import { ViewDrawer, type ViewField } from '@/components/common/ViewDrawer'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { BatchActionBar } from '@/components/common/BatchActionBar'
import { AuthButton } from '@/components/auth/AuthButton'
import { RowActions, type RowAction } from '@/components/common/RowActions'
import { useTable } from '@/hooks/useTable'
import { useTableSelection } from '@/hooks/useTableSelection'
import { useI18n } from '@/locales'
import { dynRecordApi, dynRecordPerm } from '@/api/dyn'
import type { DynImportErrorItem, DynRecord, DynSchema } from '@/types/dyn'
import type { PageRequest } from '@/types/api'
import {
  buildFieldEditFields,
  buildRecordPayload,
  formatCell,
  toFormValue,
} from './recordForm'
import { FileFieldControl } from './FileFieldControl'
import ApiGuideModal from './ApiGuideModal'

/** 列表查询条件 */
type RecordQuery = PageRequest & { keyword?: string; trash?: boolean; filter?: string }

/** 审批单状态 → 颜色(纯函数,与审批列表页同一套色感) */
const approvalStatusColor = (s: string): string => {
  if (s === 'approved') return 'green'
  if (s === 'rejected') return 'red'
  if (s === 'running') return 'blue'
  if (s === 'returned') return 'orange'
  return 'default'
}

/**
 * 换集合 = 换一个数据集 ⇒ **整个页面重挂**
 *
 * ⚠️ 为什么不用 `useTable({ deps: [code] })` 就完事:
 *   `deps` 只会**重新拉数据**,不会把**页码**和**搜索词**拉回初始 ——
 *   用户在集合 A 翻到第 3 页、搜了个关键词,再点集合 B 的菜单:
 *   路由参数变了但**组件没重挂**(React Router 同路由只换参数不换实例),
 *   于是 B 显示的是"第 3 页 + 那个关键词"的结果 —— 一片空白,
 *   而用户根本没在 B 里搜过。
 *
 *   `key={code}` 让 React 直接卸载重建 ⇒ 页码、搜索词、表单状态全回初始。
 */
const DynRecordList: FC = () => {
  const { code = '' } = useParams<{ code: string }>()
  return <DynRecordPage key={code} code={code} />
}

/** 真正的页面(重挂后从头开始) */
const DynRecordPage: FC<{ code: string }> = ({ code }) => {
  const { t } = useI18n()
  const { message } = App.useApp()

  // ===== 表单结构(决定列与表单) =====
  const [schema, setSchema] = useState<DynSchema | null>(null)
  const [schemaLoading, setSchemaLoading] = useState(false)
  const [schemaError, setSchemaError] = useState<unknown>(null)

  // ===== 弹窗 =====
  const [editing, setEditing] = useState<{ open: boolean; data?: DynRecord | null }>({ open: false })
  const [viewing, setViewing] = useState<DynRecord | null>(null)
  const [deleting, setDeleting] = useState<DynRecord | null>(null)
  /** 恢复确认(回收站视图里的行内动作) */
  const [restoring, setRestoring] = useState<DynRecord | null>(null)
  /** 批量修改弹窗 */
  const [batchEditing, setBatchEditing] = useState(false)
  const [batchFieldKey, setBatchFieldKey] = useState<string>('')
  const [batchValue, setBatchValue] = useState<string>('')
  /** 批量删除确认 */
  const [batchDeleting, setBatchDeleting] = useState(false)
  /** 导入弹窗(选语义 → 执行 → 展示结果与错误行报告) */
  const [importOpen, setImportOpen] = useState(false)
  const [importFile, setImportFile] = useState<File | null>(null)
  const [importMode, setImportMode] = useState<'all' | 'skip'>('all')
  const [importing, setImporting] = useState(false)
  const [importResult, setImportResult] = useState<{
    total: number
    inserted: number
    skipped: number
    errors: DynImportErrorItem[]
  } | null>(null)
  /** 导出中 */
  const [exporting, setExporting] = useState(false)
  /** 「API 接入」说明弹窗 */
  const [apiGuideOpen, setApiGuideOpen] = useState(false)

  /**
   * ⚠️ **结构与记录是两次独立的请求** ——
   *   表单结构接口归 `dyn:<码>:list` 管(能看列表就能拿到结构),
   *   它决定了表格有哪些列;记录列表是另一个接口。
   *   两个都失败时页面给一个"取不到表结构"的提示,而不是一张空表。
   */
  useEffect(() => {
    if (!code) return
    let cancelled = false
    setSchemaLoading(true)
    setSchemaError(null)
    dynRecordApi
      .schema(code)
      .then((s) => {
        if (cancelled) return
        setSchema(s)
      })
      .catch((err) => {
        if (cancelled) return
        setSchema(null)
        setSchemaError(err)
      })
      .finally(() => {
        if (!cancelled) setSchemaLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [code])

  const fetchRecords = useCallback(
    (q: RecordQuery) => dynRecordApi.listRecords(code, q),
    [code],
  )

  const { tableProps, search, reload, query } = useTable<RecordQuery, DynRecord>({
    fetch: fetchRecords,
    // ⚠️ **这里不需要 deps** —— 换集合时整个页面已经重挂了(见 DynRecordList)
  })

  const fields = useMemo(() => schema?.fields ?? [], [schema])

  // ===== 回收站视图 =====
  /**
   * ⚠️ **软删的记录从普通列表里消失了,恢复按钮必须有个落点** ——
   *   这个开关就是落点:开着时列表只显示软删的记录,行内动作换成"恢复"。
   *   切换走 search()(会顺带把页码拉回第 1 页),
   *   普通搜索的 reset 会把 trash 一并清掉 —— 所以 onReset 不用 reset(),
   *   用 search({ keyword: undefined }) 保住视图开关。
   */
  const [trash, setTrash] = useState(false)
  const toggleTrash = useCallback(() => {
    const next = !trash
    setTrash(next)
    search({ trash: next })
  }, [trash, search])

  // ===== 行选择(批量操作) =====
  const selection = useTableSelection<DynRecord>({
    data: tableProps.dataSource,
    rowKey: 'id',
  })

  // ===== 送审 =====
  /**
   * 送审 = 建草稿 + 提交一步走(后端 dyn_approval.go 文件头 ③)。
   * 成功后刷新列表 —— 徽标来自 `_approval`,不重拉就看不到"审批中"。
   */
  const handleSubmitApproval = useCallback(
    async (r: DynRecord) => {
      try {
        await dynRecordApi.submitRecordApproval(code, r.id)
        message.success(t('page.dyn.records.approvalSubmitted'))
        reload()
      } catch {
        // 错误 toast 由 HTTP 层统一弹,这里只吞掉异常避免 unhandled rejection
      }
    },
    [code, message, t, reload],
  )

  /** 审批单状态 → 文案(字面量 key,i18n 守门网扫得到) */
  const approvalStatusText = useCallback(
    (s: string): string => {
      if (s === 'draft') return t('approval.status.draft')
      if (s === 'running') return t('approval.status.running')
      if (s === 'returned') return t('approval.status.returned')
      if (s === 'approved') return t('approval.status.approved')
      if (s === 'rejected') return t('approval.status.rejected')
      if (s === 'cancelled') return t('approval.status.cancelled')
      return s
    },
    [t],
  )

  // ===== 行操作 =====
  /**
   * ⚠️ **回收站视图里的动作集不一样** ——
   *   软删的记录不能再"编辑/删除"(还没删透,也没有"删两次"的语义),
   *   行内动作换成"恢复";普通视图里不出现恢复(没删的记录点了就 404)。
   */
  const actionsFor = useMemo(
    () =>
      (row?: DynRecord): RowAction<DynRecord>[] => {
        if (trash) {
          return [
            { key: 'view', label: t('common.detail'), onClick: setViewing },
            {
              key: 'restore',
              label: t('page.dyn.records.restore'),
              perm: dynRecordPerm(code, 'restore'),
              onClick: setRestoring,
            },
          ]
        }
        return [
          { key: 'view', label: t('common.detail'), onClick: setViewing },
          {
            key: 'edit',
            label: t('common.edit'),
            perm: dynRecordPerm(code, 'update'),
            onClick: (r) => setEditing({ open: true, data: r }),
          },
          // 送审(2026-11-03)—— 集合绑了审批模板且这条记录还没进过审批流时出现
          // (引擎 D9:同记录一辈子一张审批单,终局后不能重发,同 material 口径)
          ...(schema?.approval_template_id &&
          schema.approval_template_id !== '0' &&
          row &&
          !row._approval
            ? [
                {
                  key: 'submit-approval',
                  label: t('page.dyn.records.submitApproval'),
                  perm: dynRecordPerm(code, 'update'),
                  onClick: (r: DynRecord) => void handleSubmitApproval(r),
                },
              ]
            : []),
          {
            key: 'delete',
            label: t('common.delete'),
            perm: dynRecordPerm(code, 'delete'),
            danger: true,
            onClick: setDeleting,
          },
        ]
      },
    [t, code, trash, schema, handleSubmitApproval],
  )

  // ===== 批量修改 =====
  const batchField = useMemo(
    () => fields.find((f) => f.field_key === batchFieldKey),
    [fields, batchFieldKey],
  )

  // ===== 导出 =====
  /**
   * ⚠️ **导出带上当前筛选与回收站开关** ——
   *   界面上看到的是哪批数据,导出的就得是哪批。
   *   blob 下载完延迟 revoke(立即 revoke 的症状是"点了没反应")。
   */
  const handleExport = useCallback(async () => {
    setExporting(true)
    try {
      const blob = await dynRecordApi.exportRecords(code, {
        keyword: query.keyword,
        trash,
        filter: query.filter,
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `dyn-${code}-${dayjs().format('YYYYMMDD-HHmmss')}.csv`
      // Firefox 要求 anchor 在 DOM 里才响应 click
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(url), 30000)
    } finally {
      setExporting(false)
    }
  }, [code, query.keyword, query.filter, trash])

  // ===== 导入 =====
  const openImport = useCallback((file: File) => {
    setImportFile(file)
    setImportResult(null)
    setImportMode('all')
    setImportOpen(true)
  }, [])

  const runImport = useCallback(async () => {
    if (!importFile) return
    setImporting(true)
    try {
      const res = await dynRecordApi.importRecords(code, importFile, importMode)
      setImportResult({
        total: res.total,
        inserted: res.inserted,
        skipped: res.skipped,
        errors: res.errors ?? [],
      })
      reload()
    } finally {
      setImporting(false)
    }
  }, [code, importFile, importMode, reload])

  // ===== 表格列(按结构生成) =====
  const columns = useMemo<ColumnsType<DynRecord>>(() => {
    const fieldColumns: ColumnsType<DynRecord> = fields.map((f) => ({
      // ⚠️ **动态标题**(用户的显示名)—— 不是源码文案,不走 i18n
      title: f.label,
      dataIndex: f.field_key,
      width: 160,
      ellipsis: true,
      // ⚠️ **附件要带上 `_files` 里的原文件名** ——
      //   字段值是一长串 key,没人认得;原文件名由后端在读记录时解析好。
      //   ⚠️ **关联字段同理带上 `_rels` 里的显示名** ——
      //   字段值是 19 位的目标记录 id,没人认得(M5)。
      render: (v: unknown, row: DynRecord) =>
        formatCell(f, v, t, row._files?.[f.field_key], row._rels?.[f.field_key]),
    }))
    return [
      ...fieldColumns,
      {
        // ⚠️ **创建人显示的是名字,不是 owner_id** ——
        //   owner_id 是 19 位数字,界面上没人认得。
        //   owner_name 由后端在读的时候从 sys_user 补上
        //   (口径同配置中心的"操作人":真实姓名 → 昵称 → 账号);
        //   取不到(用户被删了 / id 是 0)时回落到显示 owner_id。
        title: t('common.creator'),
        dataIndex: 'owner_name',
        width: 140,
        ellipsis: true,
        render: (v: string, row: DynRecord) => v || row.owner_id || '-',
      },
      {
        title: t('common.createTime'),
        dataIndex: 'created_at',
        width: 170,
        render: (v: string) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-'),
      },
      {
        title: t('common.updateTime'),
        dataIndex: 'updated_at',
        width: 170,
        render: (v: string) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-'),
      },
      // 审批状态(2026-11-03)—— 只在集合绑了审批模板时出现;
      // 没送审过的记录这一格是空(后端不给 `_approval`,和 `_files` 同口径)。
      ...(schema?.approval_template_id && schema.approval_template_id !== '0'
        ? [
            {
              title: t('field.approvalStatus'),
              dataIndex: '_approval',
              width: 110,
              render: (_: unknown, row: DynRecord) =>
                row._approval ? (
                  <Tag color={approvalStatusColor(row._approval.status)}>
                    {approvalStatusText(row._approval.status)}
                  </Tag>
                ) : (
                  <Tag>{t('page.dyn.records.approvalNone')}</Tag>
                ),
            },
          ]
        : []),
      {
        title: t('common.action'),
        dataIndex: '__actions',
        width: 180,
        fixed: 'right' as const,
        render: (_: unknown, row: DynRecord) => (
          <RowActions<DynRecord> row={row} actions={actionsFor(row)} />
        ),
      },
    ]
  }, [fields, t, actionsFor, schema, approvalStatusText])

  // ===== 表单 =====
  /**
   * ⚠️ **附件控件由这里注入** ——
   *   recordForm.ts 是纯 .ts(不能放 JSX),而附件控件是个组件。
   *   ⇒ 这里把"渲染器"传进去,recordForm 那边只留一个口子。
   *
   * ⚠️ **附件的展示信息取自 `editing.data._files`** ——
   *   那是后端读这条记录时解析出来的(原文件名 + 直链);
   *   新建记录时没有记录 ⇒ 传 undefined(控件会显示"还没传文件")。
   */
  const editFields = useMemo(
    () =>
      buildFieldEditFields(fields, t, {
        files: editing.data?._files,
        renderFile: (_f, file) => ({ value, onChange }) => (
          <FileFieldControl
            value={typeof value === 'string' ? value : ''}
            file={file}
            onChange={(v) => onChange(v)}
          />
        ),
      }),
    [fields, t, editing.data],
  )

  /**
   * ⚠️ **initialValues 必须固定引用** ——
   *   EditModal 有个副作用:initialValues 引用一变就 resetFields + setFieldsValue
   *   ⇒ 引用是内联字面量时,用户填一半的内容会被清空(见 designer 页的同名注释)。
   */
  const initialValues = useMemo<Record<string, unknown>>(() => {
    const rec = editing.data
    const out: Record<string, unknown> = {}
    for (const f of fields) {
      // 新建时用字段的默认值;编辑时用记录里的值
      out[f.field_key] = rec
        ? toFormValue(f, rec[f.field_key])
        : toFormValue(f, f.default_value)
    }
    return out
  }, [editing.data, fields])

  // ===== 详情抽屉 =====
  const viewFields = useMemo<ViewField<DynRecord>[]>(
    () =>
      fields.map((f) => ({
        name: f.field_key,
        label: f.label,
        // ⚠️ **附件要带上 `_files` 里的原文件名**(同表格列)
        render: (v: unknown, row: DynRecord) => {
          const ref = row._files?.[f.field_key]
          // ⚠️ **附件渲染成可点的直链**(2026-11-03 用户直报)——
          //   `_files` 里的 url 是 M80 的**不带签名稳定直链**(前端塞 <img src>
          //   的同一条),新标签页打开就能看。没拿到 ref / url 为空(签不出)
          //   就退回 formatCell 的纯文本 —— 名字还在,只是点不了。
          if (f.type === 'file' && ref?.url) {
            return (
              <a href={ref.url} target="_blank" rel="noreferrer">
                {ref.name || String(v ?? '')}
              </a>
            )
          }
          return formatCell(f, v, t, ref, row._rels?.[f.field_key])
        },
      })),
    [fields, t],
  )

  /** 结构还没到手 / 拿不到 —— 不渲染空表格 */
  if (schemaError) {
    return (
      <div style={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Empty description={t('page.dyn.records.schemaFailed')} />
      </div>
    )
  }

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<DynRecord>
        pageDescription={t('page.dyn.records.description')}
        searchPlaceholder={t('page.dyn.records.searchPlaceholder')}
        onSearch={(kw) => search({ keyword: kw })}
        // ⚠️ **不用 reset()** —— reset 会连回收站开关一起清掉;
        //    视图开关不是筛选条件,清筛选不该把人从回收站踢回普通列表。
        onReset={() => search({ keyword: undefined, filter: undefined })}
        columns={columns}
        tableProps={tableProps}
        scrollX={120 + fields.length * 160}
        inlineToolBar={
          <Space size={8} wrap>
            {/* ⚠️ **高级筛选(M4)= 表达式直填,不是表单构造器** ——
                目标用户是管理员(配得动 `status = 'active' && amount > 1000`);
                表单构造器等真有非管理员需要时再做,别为一个猜测建两层 UI。 */}
            <Input.Search
              allowClear
              placeholder={t('page.dyn.records.filterPlaceholder')}
              title={t('page.dyn.records.filterTitle')}
              style={{ width: 300 }}
              defaultValue={query.filter}
              onSearch={(v) => search({ filter: v || undefined })}
            />
            {!trash && (
              <AuthButton
                type="primary"
                icon={<PlusOutlined />}
                perm={dynRecordPerm(code, 'create')}
                disabled={schemaLoading || fields.length === 0}
                onClick={() => setEditing({ open: true, data: null })}
              >
                {t('page.dyn.records.newRecord')}
              </AuthButton>
            )}
            {/* ⚠️ **批量操作条只在普通视图出现** ——
                回收站里没有"批量恢复"这个接口(只有单条恢复),
                批量删/批量改对软删的记录也没有语义。 */}
            {!trash && (
              <BatchActionBar count={selection.selectedCount} onClear={selection.clear}>
                <AuthButton
                  danger
                  icon={<DeleteOutlined />}
                  perm={dynRecordPerm(code, 'batch_delete')}
                  onClick={() => setBatchDeleting(true)}
                >
                  {t('page.dyn.records.batchDelete')}
                </AuthButton>
                <AuthButton
                  perm={dynRecordPerm(code, 'batch_update')}
                  disabled={fields.length === 0}
                  onClick={() => {
                    setBatchFieldKey('')
                    setBatchValue('')
                    setBatchEditing(true)
                  }}
                >
                  {t('page.dyn.records.batchEdit')}
                </AuthButton>
              </BatchActionBar>
            )}
            {/* 回收站开关:软删记录的查看与恢复入口 */}
            <Button
              icon={<DeleteOutlined />}
              type={trash ? 'primary' : 'default'}
              onClick={toggleTrash}
            >
              {trash ? t('page.dyn.records.trashOn') : t('page.dyn.records.trash')}
            </Button>
            <AuthButton
              icon={<DownloadOutlined />}
              perm={dynRecordPerm(code, 'export')}
              loading={exporting}
              onClick={handleExport}
            >
              {t('page.dyn.records.export')}
            </AuthButton>
            <Upload
              accept=".csv"
              showUploadList={false}
              maxCount={1}
              beforeUpload={(file) => {
                openImport(file)
                return false // ⚠️ 不走 antd 的自动上传,由导入弹窗统一发请求
              }}
            >
              <AuthButton icon={<UploadOutlined />} perm={dynRecordPerm(code, 'import')}>
                {t('page.dyn.records.import')}
              </AuthButton>
            </Upload>
            {/* ⚠️ **「API 接入」按钮放在搜索区(不是页面顶上)** ——
                接口是自动生成的,但用户看不见;这个按钮是"怎么调它"的入口。
                它不带 perm:这一页能被打开就说明有 list 权限,
                而说明文字里列出的权限码是给别人去角色配置页勾的。 */}
            <Button icon={<ApiOutlined />} onClick={() => setApiGuideOpen(true)}>
              {t('page.dyn.records.apiGuide.button')}
            </Button>
          </Space>
        }
      />

      {/* ===== API 接入说明 ===== */}
      <ApiGuideModal
        open={apiGuideOpen}
        code={code}
        fields={fields}
        onClose={() => setApiGuideOpen(false)}
      />

      {/* ===== 新增 / 编辑 ===== */}
      <EditModal<Record<string, unknown>>
        title={editing.data ? t('page.dyn.records.editRecord') : t('page.dyn.records.newRecord')}
        open={editing.open}
        initialValues={initialValues}
        fields={editFields}
        onCancel={() => setEditing({ open: false })}
        onSubmit={async (vals) => {
          const payload = buildRecordPayload(fields, vals)
          if (editing.data) {
            await dynRecordApi.updateRecord(code, editing.data.id, payload)
          } else {
            await dynRecordApi.createRecord(code, payload)
          }
          message.success(t('common.success'))
          setEditing({ open: false })
          reload()
        }}
      />

      {/* ===== 详情 ===== */}
      <ViewDrawer<DynRecord>
        title={t('page.dyn.records.detailTitle')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
        width={640}
      />

      {/* ===== 删除确认 ===== */}
      <ConfirmDialog
        open={!!deleting}
        danger
        title={`${t('common.delete')} ${deleting?.id}?`}
        description={t('page.dyn.records.deleteTip')}
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          await dynRecordApi.deleteRecord(code, deleting.id)
          message.success(t('common.success'))
          setDeleting(null)
          reload()
        }}
      />

      {/* ===== 恢复确认(回收站) ===== */}
      <ConfirmDialog
        open={!!restoring}
        title={`${t('page.dyn.records.restore')} ${restoring?.id}?`}
        description={t('page.dyn.records.restoreTip')}
        onCancel={() => setRestoring(null)}
        onOk={async () => {
          if (!restoring) return
          await dynRecordApi.restoreRecord(code, restoring.id)
          message.success(t('common.success'))
          setRestoring(null)
          reload()
        }}
      />

      {/* ===== 批量删除确认 ===== */}
      <ConfirmDialog
        open={batchDeleting}
        danger
        title={`${t('page.dyn.records.batchDelete')} (${selection.selectedCount})`}
        description={t('page.dyn.records.batchDeleteTip')}
        onCancel={() => setBatchDeleting(false)}
        onOk={async () => {
          await dynRecordApi.batchDeleteRecords(code, selection.selectedIds)
          message.success(t('common.success'))
          setBatchDeleting(false)
          selection.clear()
          reload()
        }}
      />

      {/* ===== 批量修改单字段 ===== */}
      <Modal
        title={t('page.dyn.records.batchEdit')}
        open={batchEditing}
        onCancel={() => setBatchEditing(false)}
        okButtonProps={{ disabled: !batchFieldKey }}
        onOk={async () => {
          // ⚠️ **值以文本形态发给后端** —— 类型归一化在 normalizeField 那一层
          //    (字符串 "3" → 整数、"true" → 开关),前端不做类型转换。
          await dynRecordApi.batchUpdateRecords(
            code,
            selection.selectedIds,
            batchFieldKey,
            batchValue,
          )
          message.success(t('common.success'))
          setBatchEditing(false)
          selection.clear()
          reload()
        }}
      >
        <Space direction="vertical" style={{ width: '100%' }} size={12}>
          <div>{t('page.dyn.records.batchEditTip', undefined, { n: selection.selectedCount })}</div>
          <Select
            style={{ width: '100%' }}
            placeholder={t('page.dyn.records.batchEditSelectField')}
            value={batchFieldKey || undefined}
            onChange={(v) => {
              setBatchFieldKey(v)
              setBatchValue('')
            }}
            options={fields.map((f) => ({ value: f.field_key, label: f.label }))}
          />
          {/* ⚠️ **值控件按字段类型换** —— 单选/关联/开关给选项下拉,
              其他类型是文本框(多选提示 JSON 数组写法);归一化在后端。
              关联字段的候选(schema 下发的)就是目标记录,value 是 id 文本,
              直接交给后端校验存在性(M5)。 */}
          {(batchField?.type === 'select' || batchField?.type === 'relation') && (
            <Select
              style={{ width: '100%' }}
              value={batchValue || undefined}
              placeholder={t('page.dyn.records.batchEditValuePlaceholder')}
              options={batchField.options.map((c) => ({
                value: c.value,
                label: c.label,
              }))}
              onChange={(v) => setBatchValue(String(v))}
            />
          )}
          {batchField?.type === 'bool' && (
            <Select
              style={{ width: '100%' }}
              value={batchValue || undefined}
              placeholder={t('page.dyn.records.batchEditValuePlaceholder')}
              options={[
                { value: 'true', label: t('common.yes') },
                { value: 'false', label: t('common.no') },
              ]}
              onChange={(v) => setBatchValue(String(v))}
            />
          )}
          {batchField && batchField.type !== 'select' && batchField.type !== 'relation' && batchField.type !== 'bool' && (
            <Input
              value={batchValue}
              placeholder={
                batchField.type === 'multiselect'
                  ? t('page.dyn.records.batchEditValueJson')
                  : t('page.dyn.records.batchEditValuePlaceholder')
              }
              onChange={(e) => setBatchValue(e.target.value)}
            />
          )}
        </Space>
      </Modal>

      {/* ===== 导入(CSV) ===== */}
      <Modal
        title={t('page.dyn.records.importTitle')}
        open={importOpen}
        onCancel={() => setImportOpen(false)}
        okText={t('page.dyn.records.importStart')}
        okButtonProps={{ disabled: !importFile || importing }}
        onOk={runImport}
      >
        <Space direction="vertical" style={{ width: '100%' }} size={12}>
          <div>{t('page.dyn.records.importSelectFile')}</div>
          <div>
            {importFile ? importFile.name : t('page.dyn.records.importNoFile')}
          </div>
          <div>{t('page.dyn.records.importMode')}</div>
          <Radio.Group
            value={importMode}
            onChange={(e) => setImportMode(e.target.value as 'all' | 'skip')}
          >
            <Radio value="all">{t('page.dyn.records.importModeAll')}</Radio>
            <Radio value="skip">{t('page.dyn.records.importModeSkip')}</Radio>
          </Radio.Group>
          <div style={{ color: 'var(--app-text-tertiary)' }}>
            {importMode === 'all'
              ? t('page.dyn.records.importModeAllTip')
              : t('page.dyn.records.importModeSkipTip')}
          </div>

          {/* 结果与错误行报告 */}
          {importResult && (
            <>
              <div>
                {t('page.dyn.records.importResultStat', undefined, {
                  total: importResult.total,
                  inserted: importResult.inserted,
                  skipped: importResult.skipped,
                })}
              </div>
              {importResult.errors.length > 0 ? (
                <Table<DynImportErrorItem>
                  size="small"
                  rowKey={(r) => `${r.row}-${r.field}-${r.message}`}
                  dataSource={importResult.errors}
                  pagination={{ pageSize: 5 }}
                  scroll={{ y: 200 }}
                  columns={[
                    {
                      title: t('field.importErrorRow'),
                      dataIndex: 'row',
                      width: 70,
                    },
                    {
                      title: t('field.importErrorField'),
                      dataIndex: 'field',
                      width: 140,
                      ellipsis: true,
                    },
                    {
                      title: t('field.importErrorMessage'),
                      dataIndex: 'message',
                      ellipsis: true,
                    },
                  ]}
                />
              ) : (
                <div>{t('page.dyn.records.importNoErrors')}</div>
              )}
            </>
          )}
        </Space>
      </Modal>
    </div>
  )
}

export default DynRecordList
