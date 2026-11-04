/**
 * 角色管理 —— 样板 2
 *
 * Phase 5 M6 简化版(不做 Tree+Transfer 穿梭框, 那是 Phase 6 单独 phase)
 * 验证通用模板的扩展性:
 *  - 表格 + 搜索 + 工具栏
 *  - EditModal 字段(编码/名称/类型/描述/排序/启用)
 *  - 不可删保护(超管 / 内置角色置灰 + Tooltip)
 *  - 占位: 分配权限按钮(Phase 6 Tree+Transfer 接入)
 *
 * 2026-10-18 内置/超管角色"只查看权限":
 *  - 原逻辑:isProtected 角色把"分配权限"按钮 disabled,根本点不开
 *  - 改:按钮 label 改"查看权限",不 disabled;AssignResourcesDrawer 收 readOnly prop
 *  - 后端 GetRoleResources 走 rbac("role","assign") 同可读,数据 OK
 */
import React, { useState } from 'react'
import { Tag, App } from 'antd'
import { AuthButton } from '@/components/auth/AuthButton'
import { PlusOutlined } from '@ant-design/icons'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal, EditField } from '@/components/common/EditModal'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { RowActions, RowAction } from '@/components/common/RowActions'
import { useTable } from '@/hooks/useTable'
import { useDict } from '@/hooks/useDict'
import { useI18n } from '@/locales'
import { roleApi, CreateRoleRequest } from '@/api/role'
import { AssignResourcesDrawer } from './AssignResourcesDrawer'
import { DataScopeDrawer } from './DataScopeDrawer'
import { FieldPolicyDrawer } from './FieldPolicyDrawer'
import { PermButton } from '@/constants/permissions'
import { RoleCode } from '@/constants/roles'
import type { Role } from '@/types/permission'
import type { PageRequest } from '@/types/api'

const RoleList: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  // 字典约束(2026-10-18):role_type 来自后端 sys_dict,不再硬编码 platform/business 判断
  const { getLabel: getRoleTypeLabel, getColor: getRoleTypeColor } = useDict('role_type')

  const { tableProps, search, reset, reload } = useTable<PageRequest, Role>({
    fetch: (q) => roleApi.list(q),
  })

  const [editing, setEditing] = useState<{ open: boolean; data?: Role | null }>({ open: false })
  const [deleting, setDeleting] = useState<Role | null>(null)
  /**
   * 分配权限 — Phase 6 真实化(Drawer + Tree + Transfer)
   *
   * ⚠️ **open 和 role 必须是两件事,不能一起清** ——
   * 以前是 `const [assigning, setAssigning] = useState<Role|null>(null)`,
   * 关闭时 `onClose={() => setAssigning(null)}` 一把清空 ⇒ role 没了 ⇒
   * `readOnly={!!assigning && isProtected(assigning)}` 立刻变成 false。
   * 而 Drawer 带 destroyOnHidden ⇒ **关闭动画走完之前内容还挂在 DOM 上** ⇒
   * 用户看到"只读的权限树在关闭那一瞬间变成可编辑的"(2026-10-29 用户报)。
   *
   * ⇒ 现在关闭只翻 open,role 留着(下次打开时被新角色覆盖)。
   *   留着 role 的代价只是内存里多一个对象,换来的是关闭动画期间
   *   标题 / 只读标志 / 树的 disabled **一个都不闪**。
   */
  const [assignOpen, setAssignOpen] = useState(false)
  const [assignRole, setAssignRole] = useState<Role | null>(null)

  /**
   * 数据范围(行级权限)—— M157
   *
   * ⚠️ **与「分配权限」是两件事** ——
   *   分配权限 = 这个角色**能调哪些接口**(RBAC 资源码)
   *   数据范围 = 接口放行之后,他**能看到哪些行**
   * 两者分开配、分开存(资源码 vs sys_data_scope)。
   *
   * ⚠️ **按钮权限复用 `button.role.assign`** —— 不新开 `button.role.scope`。
   *   新开按钮码要进 seed 的资源表 + 13 个角色的绑定矩阵,
   *   漏绑一个角色就是那个角色整个按钮不渲染(且红 toast 说的是别的码,极难排查)。
   *   后端也是这么定的(scope handler 复用 role:read / role:assign,理由同款)。
   */
  const [scopeOpen, setScopeOpen] = useState(false)
  const [scopeRole, setScopeRole] = useState<Role | null>(null)

  /**
   * 字段可见性(列级权限)—— M164
   *
   * ⚠️ **这是第三根轴,不是数据范围的加强版** ——
   *   分配权限 = 这个角色**能调哪些接口**(RBAC 资源码)
   *   数据范围 = 接口放行之后,他**能看到哪些行**(行级)
   *   字段可见性 = 看到的那一行里,**哪些字段会被打码或删掉**(列级)
   * 三者互相独立:能看到某一行 ≠ 能看到那一行里的每个字段。
   *
   * ⚠️ **按钮权限复用 `button.role.assign`** —— 不新开 `button.role.fieldPolicy`。
   *   新开按钮码要进 seed 的资源表 + 13 个角色的绑定矩阵,
   *   漏绑一个角色就是那个角色整个按钮不渲染(且红 toast 说的是别的码,极难排查)。
   *   后端也是这么定的(field_policy handler 复用 role:read / role:assign,理由同款)。
   */
  const [fieldPolicyOpen, setFieldPolicyOpen] = useState(false)
  const [fieldPolicyRole, setFieldPolicyRole] = useState<Role | null>(null)

  /** 不可删:超管 + 内置角色 */
  const isProtected = (row: Role) =>
    row.is_builtin || row.code === RoleCode.SUPER_ADMIN

  const columns = [
    {
      title: t('field.roleCode'),
      dataIndex: 'code',
      width: 220,
      fixed: 'left' as const,
      render: (v: string) => <Tag color="blue">{v}</Tag>,
    },
    { title: t('field.roleName'), dataIndex: 'name', width: 160 },
    {
      title: t('field.type'),
      dataIndex: 'type',
      width: 100,
      render: (v: string) => {
        const label = getRoleTypeLabel(v)
        if (label === String(v)) {
          return <Tag color="default">{v ?? '-'}</Tag>
        }
        return <Tag color={getRoleTypeColor(v)}>{label}</Tag>
      },
    },
    { title: t('field.sort'), dataIndex: 'sort', width: 80 },
    {
      title: t('field.builtin'),
      dataIndex: 'is_builtin',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="gold">{t('field.builtin')}</Tag> : '-'),
    },
    {
      title: t('common.status'),
      dataIndex: 'enabled',
      width: 100,
      render: (v: boolean) => (
        <Tag color={v ? 'green' : 'default'}>
          {v ? t('common.enable') : t('common.disable')}
        </Tag>
      ),
    },
    {
      title: t('common.createTime'),
      dataIndex: 'created_at',
      width: 160,
      render: (v: string) => (v ? v.slice(0, 16).replace('T', ' ') : '-'),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      // 2026-11-02 M157:多了「数据范围」一个行内动作;M164 再加「字段可见性」
      // (分配权限 / 数据范围 / 字段可见性 / 编辑 / 删除)
      width: 460,
      fixed: 'right' as const,
      render: (_: any, row: Role) => {
        const actions: RowAction<Role>[] = [
          {
            key: 'assign',
            // 内置/超管角色:可查看但不可改权限,按钮 label 切到"查看权限"
            // 不 disabled —— 原逻辑会直接堵死 Drawer,用户看不到权限列表
            label: isProtected(row) ? t('page.role.viewPermissions') : t('resource.role_assign'),
            perm: PermButton.ROLE_ASSIGN,
            onClick: (r) => {
              setAssignRole(r)
              setAssignOpen(true)
            },
          },
          {
            key: 'scope',
            label: t('page.role.scope.button'),
            perm: PermButton.ROLE_ASSIGN,
            onClick: (r) => {
              setScopeRole(r)
              setScopeOpen(true)
            },
          },
          {
            key: 'fieldPolicy',
            label: t('page.role.fieldPolicy.button'),
            perm: PermButton.ROLE_ASSIGN,
            onClick: (r) => {
              setFieldPolicyRole(r)
              setFieldPolicyOpen(true)
            },
          },
          {
            key: 'edit',
            label: t('common.edit'),
            perm: PermButton.ROLE_EDIT,
            disabled: row.is_builtin,
            disabledTip: t('page.role.builtinNotEditable'),
            onClick: (r) => setEditing({ open: true, data: r }),
          },
          {
            key: 'delete',
            label: t('common.delete'),
            perm: PermButton.ROLE_DELETE,
            danger: true,
            disabled: isProtected(row),
            disabledTip: row.is_builtin
              ? t('page.role.builtinNotDeletable')
              : t('page.role.superAdminNotDeletable'),
            onClick: setDeleting,
          },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  const editFields: EditField[] = [
    {
      name: 'code',
      label: t('field.roleCode'),
      required: true,
      disabled: !!editing.data,
      rules: [
        { required: true, message: t('common.placeholder.code') },
        { pattern: /^[a-z][a-z0-9_]*$/, message: t('page.role.codePattern') },
      ],
    },
    {
      name: 'name',
      label: t('field.roleName'),
      required: true,
      rules: [{ required: true, message: t('common.placeholder.name') }],
    },
    {
      name: 'type',
      label: t('field.type'),
      type: 'select',
      options: [
        { value: 'business', label: t('page.role.typeBusiness') },
        { value: 'platform', label: t('page.role.typePlatform') },
      ],
    },
    { name: 'description', label: t('field.description'), type: 'textarea' },
    { name: 'sort', label: t('field.sort'), type: 'number' },
    { name: 'enabled', label: t('common.enable'), type: 'switch' },
  ]

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<Role>
        pageDescription={t('page.role.description')}
        searchPlaceholder={t('common.placeholder.searchRole')}
        onSearch={(kw) => search({ keyword: kw })}
        onReset={reset}
        inlineToolBar={[
          <AuthButton
            key="create"
            type="primary"
            icon={<PlusOutlined />}
            perm={PermButton.ROLE_ADD}
            onClick={() => setEditing({ open: true, data: null })}
          >
            {t('common.add')}
          </AuthButton>,
        ]}
        columns={columns}
        tableProps={tableProps}
        scrollX={1200}
      />

      <EditModal<Role>
        title={editing.data ? t('common.edit') : t('common.add')}
        open={editing.open}
        initialValues={editing.data ?? { type: 'business', sort: 0, enabled: true }}
        fields={editFields}
        onCancel={() => setEditing({ open: false })}
        onSubmit={async (vals) => {
          if (editing.data) {
            // 编辑时 type 不让改(避免运行中切换影响范围)
            await roleApi.update(String(editing.data.id), vals)
          } else {
            // 新建时把 type 转成 roleType 数值(0/1)
            const roleType = vals.type === 'platform' ? 1 : 0
            await roleApi.create({ ...vals, roleType, resourceIds: [] } as unknown as CreateRoleRequest)
          }
          message.success(t('common.success'))
          setEditing({ open: false })
          reload()
        }}
      />

      <ConfirmDialog
        open={!!deleting}
        danger
        title={`${t('resource.role_delete')} ${deleting?.name}?`}
        description={t('page.role.deleteTip')}
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          await roleApi.remove(String(deleting.id))
          message.success(t('common.success'))
          setDeleting(null)
          reload()
        }}
      />

      {/* 分配权限 Drawer(2026-10-18 由 ConfirmDialog 占位升级为真实 Drawer)
       *  readOnly 模式:内置/超管角色只查看,Tree disabled + 隐藏保存/全选/清空
       *
       *  ⚠️ onClose **只关 open,不清 role** —— 见上面 assignOpen/assignRole 的注释:
       *     清了 role,关闭动画期间 readOnly 会翻成 false,只读的树会闪成可编辑。
       */}
      <AssignResourcesDrawer
        open={assignOpen}
        role={assignRole}
        readOnly={!!assignRole && isProtected(assignRole)}
        onClose={() => setAssignOpen(false)}
        onSaved={reload}
      />

      {/* 数据范围抽屉(M157)
       *  ⚠️ onClose 同样**只关 open,不清 role** —— 与上面同一个理由:
       *     清了 role,关闭动画期间 readOnly 会翻成 false,只读内容会闪成可编辑。 */}
      <DataScopeDrawer
        open={scopeOpen}
        role={scopeRole}
        readOnly={!!scopeRole && isProtected(scopeRole)}
        onClose={() => setScopeOpen(false)}
        onSaved={reload}
      />

      {/* 字段可见性抽屉(M164)
       *  ⚠️ onClose 同样**只关 open,不清 role** —— 与上面同一个理由:
       *     清了 role,关闭动画期间 readOnly 会翻成 false,只读内容会闪成可编辑。 */}
      <FieldPolicyDrawer
        open={fieldPolicyOpen}
        role={fieldPolicyRole}
        readOnly={!!fieldPolicyRole && isProtected(fieldPolicyRole)}
        onClose={() => setFieldPolicyOpen(false)}
        onSaved={reload}
      />
  </div>
  )
}

export default RoleList
