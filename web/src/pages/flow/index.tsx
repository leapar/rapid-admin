/**
 * 审批流程配置 —— 模板列表(M87)
 *
 * 文档:docs/通用审批系统设计方案V1.0.md §7.2 / §8.1 / §9.1
 *
 * ⚠️ **三级路由的第一级** —— 模板 → 关卡 → 规则,三层都是 ManagementTable。
 *   本页是模板层;点「关卡」进 `/flow/:id`(关卡层),再点「规则」进
 *   `/flow/:id/stage/:stageId`(规则层)。
 *   ⚠️ **三层不做成一层里的嵌套数组** —— 声明式 EditField[] 表达不了嵌套,
 *   而且三层塞一个页面会让人分不清自己在改哪一层。
 *
 * ⚠️ **本仓 API 形状决定了"改任何一级都是整棵树 PUT"** ——
 *   后端只有模板级的 5 个端点,`PUT /flows/:id` 是硬删旧关卡再整棵重写。
 *   所以本页改模板名/编码时,必须**先拉详情带上原来的关卡和规则**,
 *   否则一 PUT 就把下面的关卡全清空了。
 */
import React, { useState } from 'react'
import { Tag, App, Space, Select } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal, EditField } from '@/components/common/EditModal'
import { ViewDrawer, ViewField } from '@/components/common/ViewDrawer'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { RowActions, RowAction } from '@/components/common/RowActions'
import { AuthButton } from '@/components/auth/AuthButton'
import { useTable } from '@/hooks/useTable'
import { useI18n } from '@/locales'
import { flowApi } from '@/api/flow'
import { PermButton } from '@/constants/permissions'
import type { FlowDetail, FlowListItem, FlowListQuery, FlowSaveReq } from '@/types/approval'

/**
 * 新建模板时带的默认关卡
 *
 * ⚠️ **必须有至少一个关卡、每个关卡至少一条规则** —— 后端
 *   `normalizeFlow` 会拒("至少要配一个关卡" / "第 N 关没有配任何审批人规则")。
 *   声明式 EditField[] 表达不了嵌套,所以这里带一个默认关卡,
 *   用户进关卡页继续细化。
 *
 * ⚠️ **默认规则是 `dept_leader`(申请人的直属主管)** —— 不是"指定角色"。
 *   `role` 规则要 role_id,而角色成员是**运行期数据**:seed 的时候那个角色
 *   通常一个人都没有,预检(§5.3①)直接就拒了。
 *   `dept_leader` 在预检期**不可判定**(§5.3① 的"判不了就放行"),
 *   留到提交期按真实组织架构解析。
 */
/**
 * ⚠️ **默认关卡名走 i18n** —— 模块顶层的 `const` 在 import 时求值,
 *   直接写 `t('…')` 会把语言冻结在 import 那一刻(坑 2)。
 *   所以这里只存键,渲染/提交时再查。
 */
const DEFAULT_STAGE_NAME_KEY = 'page.flowList.defaultStageName'

const DEFAULT_STAGES = [
  {
    name: DEFAULT_STAGE_NAME_KEY,
    review_type: 'any' as const,
    rules: [{ rule_type: 'dept_leader' as const, levels: 1 }],
  },
]

const FlowList: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()

  const { tableProps, search, reset, reload, query, setQuery } = useTable<FlowListQuery, FlowListItem>({
    fetch: (q) => flowApi.list(q),
  })

  const [editing, setEditing] = useState<{ open: boolean; data?: FlowListItem | null }>({ open: false })
  const [viewing, setViewing] = useState<FlowListItem | null>(null)
  const [deleting, setDeleting] = useState<FlowListItem | null>(null)

  /** 编辑时要带上的原树 —— PUT 是整棵替换,不带就清空了 */
  const [editDetail, setEditDetail] = useState<FlowDetail | null>(null)

  const columns = [
    { title: t('field.code'), dataIndex: 'code', width: 180, fixed: 'left' as const },
    { title: t('field.name'), dataIndex: 'name', width: 200 },
    { title: t('field.bizType'), dataIndex: 'biz_type', width: 160 },
    { title: t('field.stageCount'), dataIndex: 'stage_count', width: 90, align: 'center' as const },
    {
      title: t('common.status'),
      dataIndex: 'is_active',
      width: 100,
      render: (v: boolean) => (v ? <Tag color="green">{t('common.enable')}</Tag> : <Tag>{t('common.disable')}</Tag>),
    },
    { title: t('common.remark'), dataIndex: 'remark', width: 260, ellipsis: true },
    {
      title: t('common.updateTime'),
      dataIndex: 'updated_at',
      width: 170,
      render: (v: string) => v?.slice(0, 16).replace('T', ' ') || '-',
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 260,
      fixed: 'right' as const,
      render: (_: any, row: FlowListItem) => {
        const actions: RowAction<FlowListItem>[] = [
          { key: 'view', label: t('common.detail'), onClick: setViewing },
          {
            key: 'stages',
            label: t('page.flowList.stages'),
            onClick: (r) => navigate(`/flow/${r.id}`),
          },
          {
            key: 'edit',
            label: t('common.edit'),
            perm: PermButton.FLOW_EDIT,
            onClick: openEdit,
          },
          {
            key: 'delete',
            label: t('common.delete'),
            perm: PermButton.FLOW_DELETE,
            danger: true,
            onClick: setDeleting,
          },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  /**
   * 打开编辑 —— **先拉详情**
   *
   * ⚠️ **必须带原树回去** —— `PUT /flows/:id` 是硬删旧关卡再整棵重写,
   *   只传模板字段等于把下面的关卡和规则全清空。
   */
  async function openEdit(row: FlowListItem) {
    try {
      const detail = await flowApi.detail(row.id)
      setEditDetail(detail)
      setEditing({ open: true, data: row })
    } catch {
      // ⚠️ **不重复弹 toast** —— 响应拦截器已经弹过业务错误了(api/client.ts
      //   的 handleBizError)。这里再弹一次就是两条一样的错误。
    }
  }

  const editFields: EditField[] = [
    {
      name: 'code',
      label: t('page.flowList.templateCode'),
      required: true,
      disabled: !!editing.data,
      rules: [
        { required: true, message: t('page.flowList.templateCodeRequired') },
        {
          pattern: /^[a-z][a-z0-9_]*$/,
          message: t('page.flowList.templateCodePattern'),
        },
      ],
    },
    { name: 'name', label: t('page.flowList.templateName'), required: true, rules: [{ required: true, message: t('page.flowList.templateNameRequired') }] },
    {
      name: 'biz_type',
      label: t('field.bizType'),
      required: true,
      disabled: !!editing.data,
      rules: [{ required: true, message: t('page.flowList.bizTypeRequired') }],
    },
    { name: 'remark', label: t('common.remark'), type: 'textarea' },
    { name: 'is_active', label: t('common.enable'), type: 'switch' },
  ]

  const viewFields: ViewField<FlowListItem>[] = [
    { name: 'code', label: t('page.flowList.templateCode') },
    { name: 'name', label: t('page.flowList.templateName') },
    { name: 'biz_type', label: t('field.bizType') },
    { name: 'stage_count', label: t('field.stageCount') },
    { name: 'is_active', label: t('common.status'), render: (v: boolean) => (v ? t('common.enable') : t('common.disable')) },
    { name: 'remark', label: t('common.remark') },
    { name: 'created_at', label: t('common.createTime'), render: (v: string) => v?.slice(0, 16).replace('T', ' ') || '-' },
    { name: 'updated_at', label: t('common.updateTime'), render: (v: string) => v?.slice(0, 16).replace('T', ' ') || '-' },
  ]

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<FlowListItem>
        pageDescription={t('page.flowList.description')}
        searchPlaceholder={t('common.placeholder.searchFlow')}
        onSearch={(kw) => search({ keyword: kw })}
        onReset={reset}
        searchExtra={[
          /**
           * 2026-10-26:原来是一个可点击的 <Tag>(显示当前状态,点一下在
           *   "全部 / 只看停用"之间切)。改成 Select 三态:
           *   · 跟全仓其它列表页一致(job / notice / audit / dict 都是 Select),
           *     Tag 那种写法看不出是能点的,还把"当前状态"当成"筛选项"显示;
           *   · 后端本来就支持三态 —— service.FlowListQuery.IsActive 是 *bool
           *     (nil=不过滤 / true=只看启用 / false=只看停用),
           *     Tag 只暴露了其中两态,没法"只看启用"。
           *   ⚠️ "全部" = **不传** is_active,不是传 false —— 传 false 是"只看停用"
           *     (后端 handler 是 `if v := c.Query("is_active"); v != ""` 才解析)。
           */
          <Select
            key="status"
            style={{ width: 140 }}
            value={query.is_active === undefined ? 'all' : query.is_active ? 'on' : 'off'}
            onChange={(v) =>
              setQuery((prev) => ({
                ...prev,
                is_active: v === 'all' ? undefined : v === 'on',
                page: 1,
              }))
            }
            options={[
              { value: 'all', label: t('common.all') },
              { value: 'on', label: t('common.enable') },
              { value: 'off', label: t('common.disable') },
            ]}
          />,
        ]}
        inlineToolBar={[
          <AuthButton
            key="create"
            type="primary"
            perm={PermButton.FLOW_ADD}
            onClick={() => {
              setEditDetail(null)
              setEditing({ open: true, data: null })
            }}
          >
            {t('page.flowList.createTemplate')}
          </AuthButton>,
        ]}
        columns={columns}
        tableProps={tableProps}
        scrollX={1200}
      />

      <EditModal<Record<string, unknown>>
        title={editing.data ? t('common.edit') : t('page.flowList.createTemplate')}
        open={editing.open}
        width={640}
        initialValues={
          (editing.data
            ? {
                code: editing.data.code,
                name: editing.data.name,
                biz_type: editing.data.biz_type,
                remark: editing.data.remark,
                is_active: editing.data.is_active,
              }
            : { is_active: true }) as Record<string, unknown>
        }
        fields={editFields}
        onCancel={() => setEditing({ open: false })}
        onSubmit={async (vals) => {
          const payload: FlowSaveReq = {
            code: String(vals.code ?? ''),
            name: String(vals.name ?? ''),
            biz_type: String(vals.biz_type ?? ''),
            remark: String(vals.remark ?? ''),
            is_active: vals.is_active === undefined ? true : Boolean(vals.is_active),
            // ⚠️ **编辑时必须带上原来的关卡树** —— PUT 是整棵替换
            stages: editDetail
              ? editDetail.stages.map((st) => ({
                  name: st.name,
                  review_type: st.review_type,
                  rules: st.rules.map((r) => ({
                    rule_type: r.rule_type,
                    user_id: r.user_id && r.user_id !== '0' ? r.user_id : undefined,
                    role_id: r.role_id && r.role_id !== '0' ? r.role_id : undefined,
                    levels: r.levels,
                  })),
                }))
              : DEFAULT_STAGES.map((st) => ({ ...st, name: t(st.name) })),
          }
          if (editing.data) {
            await flowApi.update(editing.data.id, payload)
          } else {
            await flowApi.create(payload)
          }
          message.success(t('common.success'))
          setEditing({ open: false })
          setEditDetail(null)
          reload()
        }}
      />

      <ViewDrawer<FlowListItem>
        title={t('page.flowList.templateDetail')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
        extra={
          viewing ? (
            <Space>
              <AuthButton perm={PermButton.FLOW_EDIT} onClick={() => { setViewing(null); void openEdit(viewing) }}>
                {t('common.edit')}
              </AuthButton>
              <AuthButton onClick={() => navigate(`/flow/${viewing.id}`)}>{t('page.flowList.stageConfig')}</AuthButton>
            </Space>
          ) : null
        }
      />

      <ConfirmDialog
        open={!!deleting}
        danger
        title={t('page.flowList.deleteTemplateTitle', undefined, { name: deleting?.name ?? '' })}
        description={t('page.flowList.deleteTemplateDesc')}
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          await flowApi.remove(deleting.id)
          message.success(t('common.success'))
          setDeleting(null)
          reload()
        }}
      />
    </div>
  )
}

export default FlowList
