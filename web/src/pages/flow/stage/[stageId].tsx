/**
 * 审批流程配置 —— 规则列表(M87)
 *
 * 文档:docs/通用审批系统设计方案V1.0.md §5.1 / §7.2 / §9.1
 *
 * ⚠️ **三级路由的第三级** —— 模板(`/flow`)→ 关卡(`/flow/:id`)→ 规则
 *   (`/flow/:id/stage/:stageId`)。
 *
 * ⚠️ **改规则也是「整棵树 PUT」** —— 后端没有"单条规则"的端点,
 *   `PUT /flows/:id` 是硬删旧关卡再整棵重写。所以本页改完一条规则,
 *   要把**整个模板**(所有关卡 + 所有规则)原样带回去。
 *   少带一条 = 少一条规则 = 那一关的审批人变了。
 *
 * ⚠️ **三种来源互斥**(§5.1)—— 按 rule_type 只填对应的那一个 ID,
 *   另两个必须留 0。填了非 0 的那个,后端判"规则配错"。
 *
 * ⚠️ **一条关卡里的多条规则是「或」** —— 命中任意一条就算有人能审。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Tag, App, Space, Button, Select } from 'antd'
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
  ApprovalRuleType,
  FlowDetail,
  FlowRuleView,
  FlowSaveReq,
  FlowStageView,
} from '@/types/approval'
import type { SysUser } from '@/types/permission'
import type { Role } from '@/types/permission'

/**
 * ⚠️ **选项文案走 i18n** —— 模块顶层的 `const` 在 import 时求值,
 *   直接写 `t('…')` 会把语言冻结在 import 那一刻(坑 2)。
 *   所以这里只存键,渲染时再 `t(o.label)` 查(跟 system/dict 页同一个套路)。
 */
const RULE_TYPE_OPTIONS = [
  { value: 'user', label: 'page.flowStage.ruleSourceUser' },
  { value: 'role', label: 'page.flowStage.ruleSourceRole' },
  { value: 'dept_leader', label: 'page.flowStage.ruleSourceDeptLeader' },
]

const FlowStageRules: React.FC = () => {
  const { t } = useI18n()
  const { has, permissionCodes } = usePermission()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const { id, stageId } = useParams<{ id: string; stageId: string }>()
  const flowId = id ?? ''
  const targetStageId = stageId ?? ''

  const [detail, setDetail] = useState<FlowDetail | null>(null)
  const [stage, setStage] = useState<FlowStageView | null>(null)
  const [loading, setLoading] = useState(false)

  const [users, setUsers] = useState<SysUser[]>([])
  const [roles, setRoles] = useState<Role[]>([])

  const [editing, setEditing] = useState<{ open: boolean; data?: FlowRuleView | null }>({ open: false })
  const [deleting, setDeleting] = useState<FlowRuleView | null>(null)

  /**
   * ⚠️ **审批人来源草稿** —— 弹窗里"当前选的是哪个 rule_type"
   *
   * # 为什么要单独存一份(不能直接读表单的值)
   *
   *   "第二个字段显示哪个下拉"由**页面**决定(字段数组是页面算出来传给
   *   EditModal 的),而 EditModal 传给 render 的 `value` 是**它自己那次渲染**
   *   时的表单快照。以前直接从 `editing.data?.rule_type` 算 —— 那是**打开弹窗
   *   那一刻**的值,用户在弹窗里把"指定人"切成"指定角色"不会改它 ⇒
   *   字段列表纹丝不动,提交的还是 `rule_type: 'role'` + 空 `role_id`,
   *   后端必拒 —— **用户根本改不了一条规则的来源,只能删了重建**。
   *
   *   草稿在**打开弹窗时**就被设好(编辑 = 该规则的类型,新增 = 默认值),
   *   弹窗里一改 Select 就同步 —— 跟 system/dyn 设计器的 fieldTypeDraft
   *   同一个修法(同一个坑,第二次踩)。
   */
  const [ruleTypeDraft, setRuleTypeDraft] = useState<ApprovalRuleType>('dept_leader')

  const load = useCallback(async () => {
    if (!flowId) return
    setLoading(true)
    try {
      const d = await flowApi.detail(flowId)
      setDetail(d)
      setStage(d.stages.find((s) => s.id === targetStageId) ?? null)
    } catch {
      // 拦截器已经弹过 toast
    } finally {
      setLoading(false)
    }
  }, [flowId, targetStageId])

  useEffect(() => {
    void load()
  }, [load])

  /**
   * 拉候选审批人
   *
   * ⚠️ **只在"指定人 / 指定角色"时才需要** —— `dept_leader` 不填任何 ID,
   *   留到提交期按真实组织架构解析(§5.3③)。
   *
   * ⚠️ **M147(2026-11-01):没有 `user:list` / `role:list` 就别调这两个接口。**
   *
   *   后端从 M147 起**真的**按绑定矩阵强制执行(以前每条角色都有一条
   *   `obj = "*"` 的通配策略,谁都能调任何接口)。C 组(看流程配置)的四个角色
   *   —— 房源/资料/信息/交付管理员 —— **没有** `user:list` / `role:list`,
   *   他们打开本页会连弹两个"没有权限"的红色 toast,下拉框还永远是空的。
   *
   *   为什么不给它们补这两个码(那才是更省事的做法):§8.4 的 C 组语义是
   *   **"看"**,D 组(改流程配置)才是超管专属;候选审批人名单是**编辑时**才
   *   要的东西,而 C 组在本页能看到的编辑入口全都被 `PermButton.FLOW_*`
   *   挡住了 —— 也就是说 C 组根本用不上这份名单。
   *
   *   ⇒ 判据是**"我要调的东西我有没有权限"**,不是角色码
   *
   * ⚠️ **依赖项里的 `permissionCodes` 不是 `has`**:
   *   `has` 是 store 里只创建一次的函数(stores/permission.ts)
   *   → 引用**永远不变** → 只把它放进依赖里,effect 永远不会
   *   因权限变化而重跑。
   *   而**切身份是不刷新页面的**(IdentitySwitcher 走 fetchAccessible,“不 reload 页面”)
   *   → 权限集换了(每次 setPermissions 都是**新 Set**)但组件没重挂载
   *   → 名单停在旧身份那一份:从"没有 user:list"切到"有"→ 名单永远空;
   *   从"有"切到"没有"→ 名单还留着旧数据。
   *   (前端统一判权限码,见 permissions.ts 的头注释)。
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

  /**
   * 整棵保存
   *
   * ⚠️ **失败回滚本地状态** —— 界面不能留着一份没存进去的树。
   */
  async function save(nextRules: FlowRuleView[], okMsg = t('common.saveSuccess')) {
    if (!detail || !stage) return
    const nextStages = detail.stages.map((s) =>
      s.id === stage.id ? { ...s, rules: nextRules } : s
    )
    const payload: FlowSaveReq = {
      code: detail.code,
      name: detail.name,
      biz_type: detail.biz_type,
      remark: detail.remark,
      is_active: detail.is_active,
      stages: nextStages.map((st) => ({
        name: st.name,
        review_type: st.review_type,
        rules: st.rules.map((r) => ({
          rule_type: r.rule_type,
          user_id: r.user_id && r.user_id !== '0' ? r.user_id : undefined,
          role_id: r.role_id && r.role_id !== '0' ? r.role_id : undefined,
          levels: r.levels,
        })),
      })),
    }
    try {
      await flowApi.update(detail.id, payload)
      message.success(okMsg)
      await load()
    } catch {
      await load()
    }
  }

  /**
   * 规则列表
   *
   * ⚠️ **序号直接用后端的 `seq`** —— 它是 1 基的、跟写入顺序一致
   *   (后端 `Seq: j + 1`),不用在前端另编一份序号。
   */
  const rules: FlowRuleView[] = stage?.rules ?? []

  const columns = [
    {
      title: t('field.index'),
      dataIndex: 'seq',
      width: 80,
      align: 'center' as const,
    },
    {
      title: t('field.source'),
      dataIndex: 'rule_type',
      width: 160,
      render: (v: ApprovalRuleType) => {
        if (v === 'user') return <Tag color="blue">{t('page.flowStage.ruleSourceUserTag')}</Tag>
        if (v === 'role') return <Tag color="purple">{t('page.flowStage.ruleSourceRoleTag')}</Tag>
        return <Tag color="cyan">{t('page.flowStage.ruleSourceDeptLeaderTag')}</Tag>
      },
    },
    {
      title: t('field.approver'),
      dataIndex: 'user_id',
      width: 260,
      render: (v: string, r: FlowRuleView) => {
        if (r.rule_type === 'user') {
          const u = users.find((x) => x.id === v)
          return u ? `${u.nickname || u.username}(#${v})` : `#${v}`
        }
        if (r.rule_type === 'role') {
          const role = roles.find((x) => x.id === r.role_id)
          return role ? `${role.name}(${role.code})` : `#${r.role_id}`
        }
        return t('page.flowStage.deptLeaderApprover', undefined, { n: r.levels })
      },
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 180,
      fixed: 'right' as const,
      render: (_: any, row: FlowRuleView) => {
        const actions: RowAction<FlowRuleView>[] = [
          {
            key: 'edit',
            label: t('common.edit'),
            perm: PermButton.FLOW_EDIT,
            onClick: (r) => {
              setRuleTypeDraft(r.rule_type)
              setEditing({ open: true, data: r })
            },
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
   * 规则编辑字段
   *
   * ⚠️ **只显示当前 rule_type 对应的那一个选择器** —— 三种来源互斥,
   *   同时显示三个下拉框会让人以为能同时填两个。
   *
   * ⚠️ **由 `ruleTypeDraft` 驱动,不是 `editing.data?.rule_type`** ——
   *   后者是打开弹窗那一刻的快照,弹窗里切换来源不会更新它(P0,见草稿注释)。
   */
  const editFields: EditField[] = (() => {
    const type = ruleTypeDraft
    const base: EditField[] = [
      {
        name: 'rule_type',
        label: t('page.flowStage.approverSource'),
        type: 'custom',
        // ⚠️ **EditModal 只认 rules,不看 required**(EditField.required 是声明性的,
        //   renderField 里根本没读它)—— 不写 rules 这个字段可以留空提交。
        rules: [{ required: true }],
        render: ({ onChange }) => (
          <Select
            value={type}
            options={RULE_TYPE_OPTIONS.map((o) => ({ value: o.value, label: t(o.label) }))}
            style={{ width: '100%' }}
            onChange={(v) => {
              // ⚠️ **两条都要走** —— onChange 写回表单(最终权威),
              //   setRuleTypeDraft 让"第二个字段"跟着换
              onChange(v)
              setRuleTypeDraft(v as ApprovalRuleType)
            }}
          />
        ),
      },
    ]
    if (type === 'user') {
      base.push({
        name: 'user_id',
        label: t('page.flowStage.ruleSourceUser'),
        type: 'select',
        required: true,
        rules: [{ required: true }],
        options: users.map((u) => ({ value: u.id, label: `${u.nickname || u.username}(${u.username})` })),
      })
    } else if (type === 'role') {
      base.push({
        name: 'role_id',
        label: t('page.flowStage.ruleSourceRole'),
        type: 'select',
        required: true,
        rules: [{ required: true }],
        options: roles.map((r) => ({ value: r.id, label: `${r.name}(${r.code})` })),
      })
    } else {
      base.push({
        name: 'levels',
        label: t('page.flowStage.levelsUp'),
        type: 'number',
        initialValue: 1,
      })
    }
    return base
  })()

  /**
   * ⚠️ **initialValues 必须 memo** ——
   *   EditModal 有一个"initialValues 变了就 resetFields + setFieldsValue"的副作用,
   *   引用是内联对象字面量时**每次渲染都是新对象** ⇒ 弹窗开着的时候任何一次
   *   重渲染(比如切审批人来源触发的那次)都会把用户刚选的值**冲回初始值**。
   *   所以 memo,让引用只在"打开的是另一条规则"时才变
   *   (system/dyn 设计器的 collectionInitialValues 同一个坑)。
   */
  const ruleInitialValues = useMemo<Record<string, unknown>>(
    () =>
      (editing.data
        ? {
            rule_type: editing.data.rule_type,
            user_id: editing.data.user_id,
            role_id: editing.data.role_id,
            // ⚠️ `|| 1`:user/role 规则的 levels 是 0,编辑弹窗里显示 0 会让人
            //   顺手改成别的;给个语义上中性的 1(dept_leader 提交时也会归一到 ≥1)
            levels: editing.data.levels || 1,
          }
        /**
         * ⚠️ **新增默认 `dept_leader`,不是 `user`** ——
         *   默认 'user' 时,下拉的候选名单来自 `user:list`,而 M147 之后
         *   C 组(房源/资料/信息/交付管理员)没有这个码 ⇒ 名单是空的,
         *   required 卡住 ⇒ **新增规则永远存不进去**。`dept_leader` 不填 ID
         *   (预检期不可判定,§5.3③),跟"新关卡自动带的那条规则"同理由。
         */
        : { rule_type: 'dept_leader', levels: 1 }) as Record<string, unknown>,
    [editing.data],
  )

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/*
        2026-11-03 统一页头结构:信息 Card 挂在 <ManagementTable> 外面会落在
        功能描述**上面**(与 [id].tsx 同一轮修正,字典页 2026-10-25 的同款坑)。
        关卡信息走 searchExtra(搜索区左侧),「返回关卡」+「新增规则」都走
        inlineToolBar(右侧)—— 与其它列表页"左内容 + 右工具栏"同一行同构。
      */}
      <ManagementTable<FlowRuleView>
        pageDescription={t('page.flowStage.description')}
        searchExtra={
          <Space size={12} wrap>
            <span>
              {stage
                ? t('page.flowStage.stageTitleWithRules', undefined, { name: stage.name })
                : t('common.loading')}
            </span>
            <Tag>{detail?.name}</Tag>
            <Tag color={stage?.review_type === 'all' ? 'purple' : 'blue'}>
              {stage?.review_type === 'all'
                ? t('approval.reviewType.all')
                : t('approval.reviewType.any')}
            </Tag>
            <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>
              {t('page.flowStage.multiRuleHint')}
            </span>
          </Space>
        }
        inlineToolBar={
          <>
            <Button
              type="primary"
              icon={<ArrowLeftOutlined />}
              onClick={() => navigate(`/flow/${flowId}`)}
            >
              {t('page.flowStage.backToStage')}
            </Button>
            <AuthButton
              type="primary"
              perm={PermButton.FLOW_ADD}
              onClick={() => {
                setRuleTypeDraft('dept_leader')
                setEditing({ open: true, data: null })
              }}
            >
              {t('page.flowStage.addRule')}
            </AuthButton>
          </>
        }
        columns={columns}
        tableProps={{
          dataSource: rules,
          loading,
          pagination: false,
          rowKey: (r: any) => r.id,
        }}
        scrollX={900}
      />

      <EditModal<Record<string, unknown>>
        title={editing.data ? t('page.flowStage.editRule') : t('page.flowStage.addRule')}
        open={editing.open}
        initialValues={ruleInitialValues}
        fields={editFields}
        onCancel={() => setEditing({ open: false })}
        onSubmit={async (vals) => {
          const ruleType = (vals.rule_type as ApprovalRuleType) ?? 'dept_leader'
          // ⚠️ **只填对应的那一个 ID** —— 三种来源互斥(§5.1)
          const rule: FlowRuleView = {
            id: editing.data?.id ?? `new-rule-${Date.now()}`,
            seq: editing.data?.seq ?? (rules.length + 1),
            rule_type: ruleType,
            user_id: ruleType === 'user' ? String(vals.user_id ?? '') : '0',
            role_id: ruleType === 'role' ? String(vals.role_id ?? '') : '0',
            levels: ruleType === 'dept_leader' ? Number(vals.levels ?? 1) || 1 : 0,
          }
          const next = editing.data
            ? rules.map((r) => (r.id === editing.data?.id ? rule : r))
            : [...rules, rule]
          await save(next)
          setEditing({ open: false })
        }}
      />

      <ConfirmDialog
        open={!!deleting}
        danger
        title={t('page.flowStage.deleteRuleTitle')}
        description={t('page.flowStage.deleteRuleDesc')}
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          const next = rules.filter((r) => r.id !== deleting.id)
          await save(next, t('page.flowStage.ruleDeleted'))
          setDeleting(null)
        }}
      />
    </div>
  )
}

export default FlowStageRules
