/**
 * 审批流程配置 —— 关卡列表(M87)
 *
 * 文档:docs/通用审批系统设计方案V1.0.md §7.2 / §9.1
 *
 * ⚠️ **三级路由的第二级** —— 模板(`/flow`)→ 关卡(`/flow/:id`)→ 规则
 *   (`/flow/:id/stage/:stageId`)。
 *
 * ⚠️ **改任何一级都是「整棵树 PUT 回去」** —— 后端只有模板级的 5 个端点,
 *   `PUT /flows/:id` 是**硬删旧关卡再整棵重写**。所以本页的每个动作都是:
 *   改本地那份 stages → 整棵 PUT → 失败就回滚本地状态。
 *
 * ⚠️ **D7:有在跑的审批时服务端会拒**,报错文案带"N 条在跑"。
 *   那条 toast 是有用的信息,不要吞掉。
 *
 * ⚠️ **不做拖拽排序**(D3)—— 用上移/下移按钮。拖拽要引库,
 *   而排序在这里只是个辅助操作。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Tag, App, Space, Button } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { ArrowLeftOutlined } from '@ant-design/icons'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal, EditField } from '@/components/common/EditModal'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { RowActions, RowAction } from '@/components/common/RowActions'
import { AuthButton } from '@/components/auth/AuthButton'
import { useI18n } from '@/locales'
import { usePermission } from '@/hooks/usePermission'
import { flowApi } from '@/api/flow'
import { userApi } from '@/api/user'
import { roleApi } from '@/api/role'
import { PermButton, PermSystem } from '@/constants/permissions'
import type {
  ApprovalReviewType,
  FlowDetail,
  FlowSaveReq,
  FlowStageInput,
  FlowStageView,
} from '@/types/approval'
import type { SysUser, Role } from '@/types/permission'

/**
 * ⚠️ **选项文案走 i18n** —— 模块顶层的 `const` 在 import 时求值,
 *   直接写 `t('…')` 会把语言冻结在 import 那一刻(坑 2)。
 *   所以这里只存键,渲染时再 `t(o.label)` 查(跟 system/dict 页同一个套路)。
 */
const REVIEW_TYPE_OPTIONS = [
  { value: 'any', label: 'page.flowDetail.reviewTypeAnyDesc' },
  { value: 'all', label: 'page.flowDetail.reviewTypeAllDesc' },
]

/** 视图 → 保存入参 —— **规则必须原样带上,不能丢** */
function toInput(stages: FlowStageView[]): FlowStageInput[] {
  return stages.map((st) => ({
    name: st.name,
    review_type: st.review_type,
    rules: st.rules.map((r) => ({
      rule_type: r.rule_type,
      // ⚠️ **只带对应的那个 ID** —— 三种来源互斥,多带会被服务端判成"规则配错"
      user_id: r.user_id && r.user_id !== '0' ? r.user_id : undefined,
      role_id: r.role_id && r.role_id !== '0' ? r.role_id : undefined,
      levels: r.levels,
    })),
  }))
}

const FlowStages: React.FC = () => {
  const { t } = useI18n()
  const { has, permissionCodes } = usePermission()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const { id } = useParams<{ id: string }>()
  const flowId = id ?? ''

  const [detail, setDetail] = useState<FlowDetail | null>(null)
  const [stages, setStages] = useState<FlowStageView[]>([])
  const [loading, setLoading] = useState(false)

  // 名单 —— 用于把审批人规则里的裸 ID 翻译成名字(见下方 effect 的注释)
  const [users, setUsers] = useState<SysUser[]>([])
  const [roles, setRoles] = useState<Role[]>([])

  const [editing, setEditing] = useState<{ open: boolean; data?: FlowStageView | null }>({ open: false })
  const [deleting, setDeleting] = useState<FlowStageView | null>(null)

  const load = useCallback(async () => {
    if (!flowId) return
    setLoading(true)
    try {
      const d = await flowApi.detail(flowId)
      setDetail(d)
      setStages(d.stages)
    } catch {
      // 拦截器已经弹过 toast
    } finally {
      setLoading(false)
    }
  }, [flowId])

  useEffect(() => {
    void load()
  }, [load])

  /**
   * 拉用户 / 角色名单 —— 把审批人规则里的**裸 ID** 翻译成名字
   *
   *   后端 GetFlow 只返 ID(`user_id: "97292523896222376"`),以前本页直接
   *   显示 `指定人 #97292523896222376` —— 一串 19 位数字,没人认得。
   *
   * ⚠️ **M147:没有 `user:list` / `role:list` 就别调这两个接口**(跟规则子页
   *   同一个门控,那里有完整论证)—— C 组角色没有这两个码,调了就是
   *   连弹两个"没有权限"的红 toast。拉不到名单时回退显示 `#ID`,
   *   跟拉名单之前一样(信息没变差,只是没变好)。
   *
   * ⚠️ **依赖项里的 `permissionCodes` 不是 `has`** —— `has` 引用永远不变,
   *   切身份不重载页面,只放 `has` 的话名单停在旧身份那一份。
   */
  useEffect(() => {
    if (has(PermSystem.USER_LIST)) {
      void userApi
        .list({ page: 1, page_size: 200 })
        .then((r) => setUsers(r.list ?? []))
        .catch(() => setUsers([]))
    }
    if (has(PermSystem.ROLE_LIST)) {
      void roleApi
        .list({ page: 1, page_size: 200 })
        .then((r) => setRoles(r.list ?? []))
        .catch(() => setRoles([]))
    }
  }, [has, permissionCodes])

  /** 规则 → 显示文案:名单里能查到就给名字,查不到(或没权限拉)回落 #ID */
  function ruleLabel(r: FlowStageView['rules'][number]): string {
    if (r.rule_type === 'user') {
      const u = users.find((x) => x.id === r.user_id)
      return u ? `${u.nickname || u.username}(${u.username})` : `#${r.user_id}`
    }
    if (r.rule_type === 'role') {
      const role = roles.find((x) => x.id === r.role_id)
      return role ? `${role.name}(${role.code})` : `#${r.role_id}`
    }
    return t('page.flowDetail.ruleDeptLeaderTag', undefined, { n: r.levels })
  }

  /**
   * 整棵保存
   *
   * ⚠️ **失败要回滚本地状态** —— 否则界面显示的是一份根本没存进去的树,
   *   用户以为改成功了,实际后端还是旧的。
   */
  async function save(next: FlowStageView[], okMsg = t('common.saveSuccess')) {
    if (!detail) return
    const payload: FlowSaveReq = {
      code: detail.code,
      name: detail.name,
      biz_type: detail.biz_type,
      remark: detail.remark,
      is_active: detail.is_active,
      stages: toInput(next),
    }
    try {
      await flowApi.update(detail.id, payload)
      message.success(okMsg)
      await load()
    } catch {
      // ⚠️ **回滚本地状态** —— 界面不能留着一份没存进去的树
      await load()
    }
  }

  /** 上移 / 下移 —— 不引拖拽库(D3) */
  async function move(stage: FlowStageView, dir: -1 | 1) {
    const idx = stages.findIndex((s) => s.id === stage.id)
    const to = idx + dir
    if (idx < 0 || to < 0 || to >= stages.length) return
    const next = [...stages]
    const a = next[idx]
    const b = next[to]
    if (!a || !b) return
    next[idx] = b
    next[to] = a
    // ⚠️ **stage_index 跟着位置走** —— 后端按数组顺序写 stage_index
    await save(next, t('page.flowDetail.orderChanged'))
  }

  const columns = [
    {
      title: t('field.index'),
      // ⚠️ **用 render 的 index,不用 dataIndex** —— dataSource 是 FlowStageView[],
      //   里面没有序号字段(序号就是数组位置)。
      //   `key` 只是给 antd 一个稳定的列标识,**不对应任何数据字段**。
      key: '__index',
      width: 80,
      align: 'center' as const,
      render: (_v: unknown, _r: FlowStageView, index: number) => index + 1,
    },
    { title: t('field.stageName'), dataIndex: 'name', width: 220 },
    {
      title: t('field.passType'),
      dataIndex: 'review_type',
      width: 200,
      render: (v: ApprovalReviewType) =>
        v === 'all' ? <Tag color="purple">{t('page.flowDetail.reviewTypeAllTag')}</Tag> : <Tag color="blue">{t('page.flowDetail.reviewTypeAnyTag')}</Tag>,
    },
    {
      title: t('field.approverRule'),
      dataIndex: 'rules',
      width: 260,
      render: (rules: FlowStageView['rules']) =>
        rules.length === 0 ? (
          <span style={{ color: 'var(--app-text-tertiary)' }}>—</span>
        ) : (
          <Space size={4} wrap>
            {rules.map((r) => (
              <Tag key={r.id || `${r.rule_type}-${r.seq}`}>{ruleLabel(r)}</Tag>
            ))}
          </Space>
        ),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 280,
      fixed: 'right' as const,
      render: (_: any, row: FlowStageView) => {
        const idx = stages.findIndex((s) => s.id === row.id)
        const actions: RowAction<FlowStageView>[] = [
          {
            key: 'rules',
            label: t('page.flowDetail.rules'),
            onClick: (r) => navigate(`/flow/${flowId}/stage/${r.id}`),
          },
          {
            key: 'edit',
            label: t('common.edit'),
            perm: PermButton.FLOW_EDIT,
            onClick: (r) => setEditing({ open: true, data: r }),
          },
          {
            key: 'up',
            label: t('page.flowDetail.moveUp'),
            perm: PermButton.FLOW_EDIT,
            disabled: idx <= 0,
            onClick: (r) => void move(r, -1),
          },
          {
            key: 'down',
            label: t('page.flowDetail.moveDown'),
            perm: PermButton.FLOW_EDIT,
            disabled: idx < 0 || idx >= stages.length - 1,
            onClick: (r) => void move(r, 1),
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

  const editFields: EditField[] = [
    { name: 'name', label: t('field.stageName'), required: true, rules: [{ required: true, message: t('page.flowDetail.stageNameRequired') }] },
    {
      name: 'review_type',
      label: t('field.passType'),
      type: 'select',
      required: true,
      options: REVIEW_TYPE_OPTIONS.map((o) => ({ ...o, label: t(o.label) })),
      /**
       * ⚠️ **新增时说明那条"凭空出现"的规则** ——
       *   后端拒"没有配任何审批人规则的关卡",所以新增会**悄悄**带一条
       *   默认的 dept_leader 规则。以前用户创建完进规则子页会看到一条
       *   自己没配过的"部门主管 +1",像是 bug;一句话说明它从哪来。
       */
      help: editing.data ? undefined : t('page.flowDetail.newStageAutoRuleHint'),
    },
  ]

  /**
   * ⚠️ **initialValues 必须 memo** ——
   *   EditModal 有"initialValues 变引用就 resetFields"的副作用,内联字面量
   *   每次渲染都是新对象 ⇒ 弹窗开着时用户/角色名单拉回来(本页新增的
   *   名单拉取会 setState 重渲染)会把用户填了一半的表单**清空**。
   *   memo 让引用只在"打开的是另一关"时才变(system/dyn 同一个坑)。
   */
  const stageInitialValues = useMemo<Record<string, unknown>>(
    () =>
      (editing.data
        ? { name: editing.data.name, review_type: editing.data.review_type }
        : { review_type: 'any' }) as Record<string, unknown>,
    [editing.data],
  )

  const rows: FlowStageView[] = stages

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/*
        2026-11-03 统一页头结构:原来的信息 Card 挂在 <ManagementTable> 外面,
        而页头(功能描述)是 ManagementTable 内部渲染的 —— 所以这张 Card 实际
        落在功能描述**上面**(与字典页 2026-10-25 同款坑)。
        模板信息走 searchExtra(搜索区左侧),「返回模板列表」+「新增关卡」
        都走 inlineToolBar(右侧)—— 与其它列表页"左内容 + 右工具栏"同一行
        同构,不再额外多出一条独立信息条。
      */}
      <ManagementTable<FlowStageView>
        pageDescription={t('page.flowDetail.description')}
        searchExtra={
          <Space size={12} wrap>
            <span>{detail ? `${detail.name}(${detail.code})` : t('common.loading')}</span>
            <Tag>{detail?.biz_type}</Tag>
            <Tag color={detail?.is_active ? 'green' : 'default'}>
              {detail?.is_active ? t('common.enable') : t('common.disable')}
            </Tag>
            <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>
              {t('page.flowDetail.stageCountHint', undefined, { n: stages.length })}
            </span>
          </Space>
        }
        inlineToolBar={
          <>
            <Button type="primary" icon={<ArrowLeftOutlined />} onClick={() => navigate('/flow')}>
              {t('page.flowDetail.backToTemplateList')}
            </Button>
            <AuthButton
              type="primary"
              perm={PermButton.FLOW_ADD}
              onClick={() => setEditing({ open: true, data: null })}
            >
              {t('page.flowDetail.addStage')}
            </AuthButton>
          </>
        }
        columns={columns}
        tableProps={{
          dataSource: rows,
          loading,
          pagination: false,
          rowKey: (r: any) => r.id,
        }}
        scrollX={1100}
      />

      <EditModal<Record<string, unknown>>
        title={editing.data ? t('page.flowDetail.editStage') : t('page.flowDetail.addStage')}
        open={editing.open}
        initialValues={stageInitialValues}
        fields={editFields}
        onCancel={() => setEditing({ open: false })}
        onSubmit={async (vals) => {
          const name = String(vals.name ?? '').trim()
          const reviewType = (vals.review_type as ApprovalReviewType) ?? 'any'
          if (editing.data) {
            const next = stages.map((s) =>
              s.id === editing.data?.id ? { ...s, name, review_type: reviewType } : s
            )
            await save(next)
          } else {
            // ⚠️ **新关卡必须自带一条规则** —— 后端拒"第 N 关没有配任何审批人规则"。
            //   默认给 dept_leader(申请人的直属主管):它在预检期**不可判定**,
            //   不会因为"这个角色没人"就把模板存不进去(§5.3①)。
            const next = [
              ...stages,
              {
                id: `new-${stages.length + 1}`,
                stage_index: stages.length + 1,
                name,
                review_type: reviewType,
                rules: [
                  {
                    id: `new-rule-${stages.length + 1}`,
                    seq: 1,
                    rule_type: 'dept_leader' as const,
                    user_id: '0',
                    role_id: '0',
                    levels: 1,
                  },
                ],
              },
            ]
            await save(next)
          }
          setEditing({ open: false })
        }}
      />

      <ConfirmDialog
        open={!!deleting}
        danger
        title={t('page.flowDetail.deleteStageTitle', undefined, { name: deleting?.name ?? '' })}
        description={t('page.flowDetail.deleteStageDesc')}
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          const next = stages.filter((s) => s.id !== deleting.id)
          await save(next, t('page.flowDetail.stageDeleted'))
          setDeleting(null)
        }}
      />
    </div>
  )
}

export default FlowStages
