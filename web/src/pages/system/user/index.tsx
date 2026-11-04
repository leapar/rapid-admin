/**
 * 用户管理 —— 样板 1
 *
 * Phase 5 M5 验证 ManagementTable + EditModal + ViewDrawer + RowActions + ConfirmDialog
 * 通用模板的样板实现
 *
 * 业务特性:
 *  1. 搜索(用户名/昵称/手机号)
 *  2. 新增 / 编辑 / 查看 / 删除 / 启停用 / 解锁 / 重置密码
 *  3. 敏感字段手机号脱敏(用 SensitiveField + SmsCodeGuard)
 *  4. 行操作按钮按 perm + roleAnyOf 动态显隐
 *  5. 超管自己不允许删/启停
 *
 * 2026-10-18 修复编辑表单:
 *  - 部门 select options:[] 硬编码空,改成打开弹窗时拉 departmentApi.list
 *  - 角色字段本期不做 ← **2026-10-25 补上了,见下**
 *
 * 2026-10-25 修 Bug(用户报:"用户编辑界面无法设置角色"):
 *   表面上是没有角色字段,顺着挖出五个坑:
 *   1. 角色字段压根没有(2026-10-18 记的"角色字段本期不做")
 *   2. 请求体字段名是 camelCase,后端绑的是 snake_case →
 *      departmentIds / primaryDepartmentId / initialPassword 全被**静默丢弃**
 *      (Go 的 encoding/json 不认下划线,而且不报错!);
 *      新增用户更是缺 password,后端直接返"密码不能为空" —— 新增功能整个是坏的
 *   3. identities 是"清空重建"式全量替换 → **没改也绝不能发**
 *   4. 列表 DTO(UserListItem)里没有 identities,只有详情接口有 →
 *      角色回填必须先拉 GET /system/users/:id
 *   5. "管理身份"抽屉之前拿列表行当身份数据 → 列表行没有 identities →
 *      抽屉打开是空的,一保存就把这个人的身份全清空(数据销毁级)
 *
 *   现在的做法:
 *   · 编辑 / 查看 / 管理身份,都先拉 GET /system/users/:id(带 identities)
 *   · 角色用多选框,回填 = 详情里的 role_code;提交时只发"真的改了"的字段
 *     (纯逻辑见 ./userForm.ts,单测见 __tests__/userForm.test.ts)
 *   · 新增用户补"初始密码"(pre-hash 后发 password,后端只认 64 位 hex)
 *   · 真实姓名从表单里去掉:后端 Create/Update 请求体里没有 real_name
 *     (model.SysUser 上那列没有写入路径),填了也存不进去;详情里只读展示
 *
 * 2026-10-25 M5d 补部门契约(上一个 commit 留下的尾巴):
 *   · 后端 GET /system/users/:id 现在返 departments[] 了(全部部门 + 主部门标记),
 *     列表接口也填上了 department_id / department_name
 *     (以前这两个字段在 DTO 里但**从来没人赋值**,前端"部门"列恒为空)
 *   · 所以编辑弹窗的"部门"从只读改回可编辑;发出去的 department_ids 是
 *     **改完之后完整的全集**(后端是"清空重建"式全量替换,少一个就洗掉一个)
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Space, Tag, App, Button } from 'antd'
import { AuthButton } from '@/components/auth/AuthButton'
import { PlusOutlined, IdcardOutlined, DeleteOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal, EditField } from '@/components/common/EditModal'
import { ViewDrawer, ViewField } from '@/components/common/ViewDrawer'
import { RowActions, RowAction } from '@/components/common/RowActions'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { SensitiveField } from '@/components/common/SensitiveField'
import { UserAvatar } from '@/components/common/UserAvatar'
import { IdentityEditorDrawer } from '@/components/common/IdentityEditorDrawer'
import { BatchActionBar } from '@/components/common/BatchActionBar'
import { useTable } from '@/hooks/useTable'
import { useTableSelection } from '@/hooks/useTableSelection'
import { useDict } from '@/hooks/useDict'
import { useI18n, tError } from '@/locales'
import { useAuth } from '@/hooks/useAuth'
import { userApi } from '@/api/user'
import type { UserListQuery } from '@/api/user'
import { roleApi } from '@/api/role'
import { resetPassword } from '@/api/auth'
import { preHashPassword } from '@/utils/crypto'
import { PermButton } from '@/constants/permissions'
import { RoleCode } from '@/constants/roles'
import type { SysUser, SysUserRole, IdentityItem, Role } from '@/types/permission'
import { buildCreatePayload, buildUpdatePayload, roleCodesOf } from './userForm'
import type { UserFormValues } from './userForm'

/**
 * 列表行 + 详情 合成一份给界面用
 *
 * 后端两个接口给的东西是互补的:
 *   列表(UserListItem):phone / email / status / department_* / avatar
 *   详情(*service.UserDetail):identities(角色!)+ departments(部门!)
 * 详情里**没有** phone / email / status(前者密文 json:"-",后者是派生字段),
 * 所以是"详情覆盖列表",不是"只用详情"。
 */
function mergeUserDetail(row: SysUser, detail?: SysUser | null): SysUser {
  const merged: SysUser = { ...row, ...(detail ?? {}) }
  // 兜底:列表行里的主部门 → 一个 departments 元素
  // ⚠️ department_id 是 uint64 + json:",string",没有部门时后端给的是 "0",
  //    而 "0" 在 JS 里是**真值** —— 不挡掉的话会造出一个 id=0 的假部门。
  if (!merged.departments?.length && merged.department_id && merged.department_id !== '0') {
    merged.departments = [
      { id: merged.department_id, name: merged.department_name, is_primary: true },
    ]
  }
  return merged
}

const UserList: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  const { userInfo } = useAuth()
  // 字典约束(2026-10-18):user_status 来自后端 sys_dict,本地不再硬编码 STATUS_MAP
  const { getLabel: getUserStatusLabel, getColor: getUserStatusColor } = useDict('user_status')

  // ⚠️ useTable 的第一个泛参是**查询条件**类型,不是行类型
  //    (以前这里写的是 useTable<SysUser, SysUser>,于是 fetch 的入参被当成
  //     SysUser,userApi.list(q) 一直报类型错)
  const { tableProps, search, reset, reload } = useTable<UserListQuery, SysUser>({
    fetch: (q) => userApi.list(q),
  })

  // 查看抽屉
  const [viewing, setViewing] = useState<SysUser | null>(null)
  // 新增/编辑弹窗(detail = GET /users/:id 的结果,带 identities,角色回填靠它)
  const [editing, setEditing] = useState<{
    open: boolean
    data?: SysUser | null
    detail?: SysUser | null
  }>({ open: false })
  // 角色下拉选项(编辑弹窗里多选用)
  const [roles, setRoles] = useState<Role[]>([])
  // 删除确认
  const [deleting, setDeleting] = useState<SysUser | null>(null)
  // 重置密码确认
  const [resetting, setResetting] = useState<SysUser | null>(null)

  // 多身份编辑 Drawer 状态
  // 2026-10-19 框架层补完(M58):从 EditModal 独立出来,Draw 大屏编辑更顺手
  // 2026-10-25:identities 必须来自详情接口(列表行没有)→ 单独存一份
  const [identityEditing, setIdentityEditing] = useState<{
    open: boolean
    user: SysUser | null
    identities: IdentityItem[]
  }>({ open: false, user: null, identities: [] })

  // 2026-10-19 M63a 批量删除 — 勾选行状态
  // 2026-10-25 M5b:行选择交给框架 hook(跨页选择不会被 antd 静默吃掉,
  //   以前这里是自己写的 selectedIds + 手搓 rowSelection)
  const getCheckboxProps = useCallback(
    // 排除自己(防误删自己)
    (record: SysUser) => ({ disabled: record.id === userInfo?.id }),
    [userInfo?.id],
  )
  const selection = useTableSelection<SysUser>({
    data: tableProps.dataSource,
    rowKey: 'id',
    getCheckboxProps,
  })
  // 批量删除确认
  const [batchDeleting, setBatchDeleting] = useState(false)

  const isCurrentSuperAdmin = (row: SysUser) =>
    userInfo?.is_platform_super_admin && row.is_platform_super_admin

  /** 拉角色列表(编辑弹窗打开时拉一次)—— 后端分页参数名是 size,不是 page_size */
  const loadRoles = useCallback(async () => {
    try {
      const res = await roleApi.list({ page: 1, size: 200 } as any)
      setRoles(res?.list ?? [])
    } catch {
      // 拉不到就空着:多选框会显示已选中的 code(回填不依赖角色列表)
      setRoles([])
    }
  }, [])

  useEffect(() => {
    if (editing.open) void loadRoles()
  }, [editing.open, loadRoles])

  /** 打开编辑弹窗:先拉详情(identities 在详情接口里,列表行没有) */
  const openEdit = useCallback(
    async (row: SysUser) => {
      try {
        const detail = await userApi.detail(String(row.id))
        setEditing({ open: true, data: row, detail })
      } catch (e: any) {
        // 详情拉不到就不开弹窗:没有它就不知道该用户现有的角色,
        // 盲目保存等于用一份不完整的清单覆盖掉对方的身份。
        message.error(t('page.user.detailLoadFailed', undefined, { msg: tError(e?.code ?? 0, e?.message ?? e) }))
      }
    },
    // ⚠️ `t` 必须在依赖里 —— 它的引用随语言切换而变(见 locales/useI18n.ts),
    //   不列的话切语言后这个回调还是旧语言那句报错文案。
    [message, t],
  )

  /** 打开详情抽屉:列表行先显示,详情回来后补上身份 */
  const openView = useCallback(
    async (row: SysUser) => {
      setViewing(row)
      try {
        const detail = await userApi.detail(String(row.id))
        setViewing(mergeUserDetail(row, detail))
      } catch {
        // 详情拉不到就只显示列表行(降级,不报错打断查看)
      }
    },
    [],
  )

  /** 打开"多身份编辑"抽屉:身份清单必须来自详情接口 */
  const openIdentity = useCallback(
    async (row: SysUser) => {
      try {
        const detail = await userApi.detail(String(row.id))
        setIdentityEditing({
          open: true,
          user: row,
          identities: (detail?.identities ?? []) as IdentityItem[],
        })
      } catch (e: any) {
        message.error(t('page.user.identityLoadFailed', undefined, { msg: tError(e?.code ?? 0, e?.message ?? e) }))
      }
    },
    [message, t],
  )

  const columns = [
    {
      title: t('common.avatar'),
      dataIndex: 'avatar',
      width: 60,
      render: (_: unknown, row: SysUser) => (
        // ⚠️ M113:走 UserAvatar —— 以前是 <Avatar src={row.avatar}>{row.nickname?.[0]}</Avatar>,
        //   两个坑:① avatar 为空串时 antd 渲染 `<img src="">` = **浏览器裂图**
        //   (而且空 src 不触发 onError ⇒ antd 的兜底永远等不到);
        //   ② `nickname?.[0]` 是 **UTF-16 码元**下标,不是码点下标 ——
        //      emoji / 𠮷 这类**代理对**字符会被切成半个代理对(渲染成豆腐块);
        //      中文 / 拉丁文在 BMP 里,两种切法结果一样。
        //      UserAvatar 两个都治了。
        <UserAvatar size="small" src={row.avatar} name={row.nickname ?? row.username} />
      ),
    },
    { title: t('field.account'), dataIndex: 'username', width: 120 },
    { title: t('field.nickname'), dataIndex: 'nickname', width: 120 },
    { title: t('field.department'), dataIndex: 'department_name', width: 140 },
    {
      // 2026-10-30 M122:列表里加一列「权限」——
      //   以前管理员只能点进详情抽屉才看得到这个人有什么权限,
      //   而"这个人到底能干什么"恰恰是用户管理页最该一眼看到的信息。
      //
      // ⚠️ 键必须挂在 `field.*` 下 —— 表格列标题只准用 field.* / common.*
      //   (table-column-i18n.test.ts 会把 page.* 的键判红)。
      title: t('field.roles'),
      dataIndex: 'roles',
      width: 200,
      render: (v?: SysUserRole[]) => {
        // 没有角色 → 显示 "-"。
        // ⚠️ 不能留空 cell —— 空 cell 看不出来是"确实没有角色"
        //   还是"这一列压根没接上"。
        if (!v || v.length === 0) return '-'
        return (
          <Space size={4} wrap>
            {v.map((r) => (
              // role_name 可能为空(后端不保证)→ 退回 role_code
              <Tag key={r.role_code} color="blue" title={r.role_code}>
                {r.role_name || r.role_code}
              </Tag>
            ))}
          </Space>
        )
      },
    },
    {
      title: t('common.phone'),
      dataIndex: 'phone',
      width: 160,
      render: (v: string) => <SensitiveField type="phone" value={v} />,
    },
    { title: t('common.email'), dataIndex: 'email', width: 180 },
    {
      title: t('common.status'),
      dataIndex: 'status',
      width: 100,
      render: (v: string) => {
        const label = getUserStatusLabel(v)
        // 字典未命中(后端未 seed):直接展示原值,避免空 cell
        if (label === String(v)) {
          return <Tag color="default">{v ?? '-'}</Tag>
        }
        return <Tag color={getUserStatusColor(v)}>{label}</Tag>
      },
    },
    {
      title: t('field.lastLogin'),
      dataIndex: 'last_login_at',
      width: 160,
      render: (v?: string) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '-'),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 280,
      fixed: 'right' as const,
      render: (_: any, row: SysUser) => {
        const actions: RowAction<SysUser>[] = [
          { key: 'view', label: t('common.detail'), onClick: (r) => void openView(r) },
          {
            key: 'edit',
            label: t('common.edit'),
            perm: PermButton.USER_EDIT,
            onClick: (r) => void openEdit(r),
          },
          {
            key: 'resetPwd',
            label: t('page.user.resetPassword'),
            perm: PermButton.USER_RESET_PASSWORD,
            roleAnyOf: [RoleCode.SUPER_ADMIN],
            onClick: setResetting,
          },
          {
            key: 'delete',
            label: t('common.delete'),
            perm: PermButton.USER_DELETE,
            danger: true,
            disabled: isCurrentSuperAdmin(row),
            disabledTip: t('page.user.cannotDeleteSelf'),
            onClick: setDeleting,
          },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  // ===== 编辑弹窗 =====
  const isEdit = !!editing.data

  /** 角色下拉选项:值用 role.code(身份表里存的就是 code,不是 role id) */
  const roleOptions = useMemo(
    () => roles.map((r) => ({ value: r.code, label: `${r.name} (${r.code})` })),
    [roles],
  )

  /** role_code → 角色名(新建身份时填 role_name 快照用) */
  const roleNameOf = useCallback(
    (code: string) => roles.find((r) => r.code === code)?.name,
    [roles],
  )

  /**
   * EditModal fields
   * 2026-10-19:多部门走 'multiDept' field type(M59),身份走 IdentityEditorDrawer 独立 Drawer(M58)
   * 2026-10-25:补"角色"多选(M11 multiSelect);编辑态部门只读;新增态补"初始密码"
   */
  const editFields: EditField[] = useMemo(() => {
    const fields: EditField[] = [
      {
        name: 'username',
        label: t('field.account'),
        required: true,
        disabled: isEdit,
        rules: [{ required: true, message: t('page.user.accountRequired') }],
      },
      {
        name: 'nickname',
        label: t('field.nickname'),
        required: true,
        rules: [{ required: true, message: t('common.placeholder.nickname') }],
      },
      {
        name: 'email',
        label: t('common.email'),
        type: 'email',
        rules: [{ type: 'email', message: t('validation.emailInvalid') }],
      },
      {
        name: 'phone',
        label: t('common.phone'),
        // 编辑态不强制填:没有 user:read-sensitive 权限时列表里读不出原值,
        // 强制必填会让人卡死在弹窗里。留空 = 不改(见 userForm.buildUpdatePayload)
        required: !isEdit,
        rules: isEdit
          ? []
          : [
              { required: true, message: t('validation.phoneRequired') },
              { pattern: /^1[3-9]\d{9}$/, message: t('validation.phoneInvalid') },
            ],
        help: isEdit ? t('page.user.phoneUnchangedTip') : undefined,
      },
      {
        name: 'roles',
        label: t('field.role'),
        type: 'multiSelect',
        required: true,
        // ⚠️ EditField.required 只是个声明,不会自动生成校验规则(规则一律走 rules)
        rules: [{ required: true, message: t('page.user.roleRequired') }],
        options: roleOptions,
        placeholder: t('page.user.roleMultiTip'),
        help: t('page.user.identityOverwriteTip'),
      },
      {
        name: 'multiDept',
        label: t('field.department'),
        type: 'multiDept',
        required: !isEdit,
        // 2026-10-25 M5d:编辑态**开放**了 ——
        //   以前是只读,理由是"后端详情接口还没返全部部门,改了会丢次要部门"。
        //   现在详情接口返 departments[] 了(而且 department_ids 是
        //   "清空重建"式全量替换,所以发出去的是**改完之后完整的全集**),
        //   只读的临时措施可以撤了。
        help: isEdit ? t('page.user.deptOverwriteTip') : undefined,
      },
    ]
    if (isEdit) {
      fields.push({
        name: 'status',
        label: t('common.status'),
        type: 'select',
        options: [
          { value: 'active', label: t('dict.userStatus.active') },
          { value: 'disabled', label: t('dict.userStatus.disabled') },
          { value: 'locked', label: t('dict.userStatus.locked') },
        ],
        help: t('page.user.lockedStatusTip'),
      })
    } else {
      fields.push({
        name: 'initialPassword',
        label: t('page.user.initialPassword'),
        type: 'password',
        required: true,
        rules: [
          { required: true, message: t('page.user.initialPasswordRequired') },
          { min: 8, message: t('validation.passwordMinLength') },
        ],
        help: t('page.user.firstLoginForceChangeTip'),
      })
    }
    return fields
  }, [isEdit, roleOptions, t])

  /**
   * 编辑弹窗的初始值
   *
   * ⚠️ 角色的回填**只能**用详情接口的 identities —— 列表行(UserListItem)
   *    没有 identities 字段,拿列表行拼出来永远是空,于是"保存"等于清空身份。
   *
   * ⚠️ 部门同理(2026-10-25 M5d):后端部门接口补齐之前,
   *    这里只能拿列表行的**主部门**兜底 —— 而 department_ids 是
   *    "清空重建"式全量替换,拿主部门当全集发出去 = 把次要部门洗掉。
   *    现在详情接口返 departments[],这里用它当全集(一个都不能少),
   *    少一个 = 保存一次洗掉一个。
   */
  const editInitialValues = useMemo<UserFormValues>(() => {
    if (!editing.data) {
      return { status: 'active', roles: [] }
    }
    const row = editing.data
    const detailDepts = editing.detail?.departments ?? []
    // 详情里的部门才是全集;详情没有(接口失败/老后端)才退回列表行的主部门
    const deptIds = detailDepts.length
      ? detailDepts.map((d) => d.id)
      : row.department_id && row.department_id !== '0'
        ? [row.department_id]
        : []
    const primaryDept =
      detailDepts.find((d) => d.is_primary)?.id ??
      (detailDepts[0]?.id ?? deptIds[0])
    return {
      username: row.username,
      nickname: row.nickname,
      email: row.email ?? '',
      phone: row.phone ?? '',
      status: row.status,
      roles: roleCodesOf(editing.detail?.identities),
      multiDept: {
        departmentIds: deptIds,
        primaryDepartmentId: primaryDept,
      },
    }
  }, [editing.data, editing.detail])

  // ViewDrawer fields
  // 2026-10-19:多部门 + 多身份展示(后端 Get 返 departments[] / identities[])
  // 2026-10-25:详情数据来自 GET /users/:id(列表行没有 identities)
  const viewFields: ViewField<SysUser>[] = [
    { name: 'username', label: t('field.account') },
    { name: 'nickname', label: t('field.nickname') },
    { name: 'real_name' as any, label: t('field.realName') },
    { name: 'email', label: t('common.email') },
    { name: 'phone', label: t('common.phone'), render: (v: string) => <SensitiveField type="phone" value={v} /> },
    {
      name: 'departments' as any,
      label: t('page.user.departmentsLabel'),
      render: (v?: { id: string; name: string; is_primary: boolean }[]) => {
        if (!v || v.length === 0) {
          return <span style={{ color: 'var(--app-text-tertiary)' }}>{t('page.user.notSet')}</span>
        }
        return (
          <Space wrap>
            {v.map((d) => (
              <Tag key={d.id} color={d.is_primary ? 'gold' : 'default'}>
                {d.is_primary ? '⭐ ' : ''}
                {d.name}
              </Tag>
            ))}
          </Space>
        )
      },
    },
    {
      name: 'identities' as any,
      label: t('page.user.identitiesLabel'),
      render: (v?: { id: string; role_code: string; role_name: string; is_primary: boolean; is_active: boolean }[]) => {
        if (!v || v.length === 0) {
          return <span style={{ color: 'var(--app-text-tertiary)' }}>{t('page.user.notSet')}</span>
        }
        return (
          <Space wrap>
            {v.map((id) => (
              <Tag
                key={id.id}
                color={id.is_primary ? 'gold' : id.is_active ? 'blue' : 'default'}
                title={`role_code: ${id.role_code}`}
              >
                {id.is_primary ? '⭐ ' : ''}
                {id.role_name || id.role_code}
                {!id.is_active && t('page.user.inactiveSuffix')}
              </Tag>
            ))}
          </Space>
        )
      },
    },
    { name: 'status', label: t('common.status'), render: (v: string) => {
      const label = getUserStatusLabel(v)
      if (label === String(v)) {
        return <Tag color="default">{v ?? '-'}</Tag>
      }
      return <Tag color={getUserStatusColor(v)}>{label}</Tag>
    } },
    { name: 'last_login_at', label: t('field.lastLogin'), render: (v?: string) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '-') },
    { name: 'created_at', label: t('common.createTime'), render: (v: string) => dayjs(v).format('YYYY-MM-DD HH:mm') },
  ]

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<SysUser>
        pageDescription={t('page.user.description')}
        searchPlaceholder={t('common.placeholder.searchUser')}
        onSearch={(kw) => search({ keyword: kw })}
        onReset={reset}
        inlineToolBar={[
          <AuthButton
            key="create"
            type="primary"
            icon={<PlusOutlined />}
            perm={PermButton.USER_ADD}
            onClick={() => setEditing({ open: true, data: null })}
          >
            {t('common.add')}
          </AuthButton>,
          // 2026-10-19 M63a 批量删除按钮(勾选行后出现)
          // 2026-10-25 M5b:换成框架的 BatchActionBar(自带"已选 N 项"和"取消选择")
          <BatchActionBar
            key="batch"
            count={selection.selectedCount}
            onClear={selection.clear}
          >
            <AuthButton
              danger
              icon={<DeleteOutlined />}
              perm={PermButton.USER_DELETE}
              onClick={() => setBatchDeleting(true)}
            >
              {t('page.user.batchDelete', undefined, { n: selection.selectedCount })}
            </AuthButton>
          </BatchActionBar>,
        ]}
        columns={columns}
        tableProps={{
          ...tableProps,
          rowSelection: selection.rowSelection,
          rowKey: 'id',
        }}
        scrollX={1300}
      />

      <EditModal<UserFormValues>
        title={editing.data ? t('common.edit') : t('common.add')}
        open={editing.open}
        initialValues={editInitialValues}
        fields={editFields}
        onCancel={() => setEditing({ open: false })}
        onSubmit={async (vals) => {
          const v = vals as UserFormValues
          try {
            // 角色:至少一个 —— 没有角色的用户什么权限都没有
            const roleCodes = v.roles ?? []
            if (roleCodes.length === 0) {
              message.error(t('page.user.roleRequired'))
              return
            }
            if (editing.data) {
              // 编辑:只发改动过的字段(后端 PATCH 语义 + identities 全量替换)
              const payload = buildUpdatePayload(v, editInitialValues, {
                identities: editing.detail?.identities,
                roleNameOf,
              })
              await userApi.update(String(editing.data.id), payload)
            } else {
              const pwd = (v.initialPassword ?? '').trim()
              if (!pwd) {
                message.error(t('page.user.initialPasswordRequired'))
                return
              }
              const deptIds = v.multiDept?.departmentIds ?? []
              if (deptIds.length === 0) {
                message.error(t('page.user.deptRequired'))
                return
              }
              // 后端只认 64 位 hex(sha256("rapid-admin::" + 明文))
              const hash = await preHashPassword(pwd)
              if (hash.length !== 64) {
                message.error(t('page.user.preHashUnsupported'))
                return
              }
              await userApi.create(buildCreatePayload(v, hash, roleNameOf))
            }
            message.success(t('common.success'))
            setEditing({ open: false })
            reload()
          } catch (e: any) {
            message.error(e?.message || t('common.saveFailed'))
          }
        }}
      />

      {/* 多身份编辑 Drawer(M58) — 2026-10-19 框架层补完 */}
      <IdentityEditorDrawer
        open={identityEditing.open}
        identities={identityEditing.identities}
        onClose={() => setIdentityEditing({ open: false, user: null, identities: [] })}
        onSave={async (items: IdentityItem[]) => {
          if (!identityEditing.user) return
          await userApi.update(String(identityEditing.user.id), {
            identities: items,
          })
          message.success(t('page.user.identityUpdated'))
          reload()
        }}
      />

      <ViewDrawer<SysUser>
        title={t('common.detail')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
        extra={
          viewing && (
            <Button
              type="link"
              icon={<IdcardOutlined />}
              onClick={() => {
                void openIdentity(viewing)
                setViewing(null)
              }}
            >
              {t('page.user.manageIdentity')}
            </Button>
          )
        }
      />

      <ConfirmDialog
        open={!!deleting}
        danger
        title={`${t('resource.user_delete')} ${deleting?.username}?`}
        description={t('page.user.deleteTip')}
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          await userApi.remove(String(deleting.id))
          message.success(t('common.success'))
          setDeleting(null)
          reload()
        }}
      />

      <ConfirmDialog
        open={batchDeleting}
        danger
        title={t('page.user.batchDeleteTitle', undefined, { n: selection.selectedCount })}
        description={t('page.user.batchDeleteTip')}
        onCancel={() => setBatchDeleting(false)}
        onOk={async () => {
          // 2026-10-19 M63a 真实调用: 后端 POST /users/batch-delete
          try {
            await userApi.batchRemove(selection.selectedIds)
            message.success(t('page.user.batchDeleted', undefined, { n: selection.selectedCount }))
            selection.clear()
            reload()
          } catch (err: any) {
            message.error(err?.message || t('page.user.batchDeleteFailed'))
          } finally {
            setBatchDeleting(false)
          }
        }}
      />

      <ConfirmDialog
        open={!!resetting}
        title={t('page.user.resetPasswordTitle', undefined, { name: resetting?.username ?? '' })}
        description={t('page.user.resetPasswordTip')}
        onCancel={() => setResetting(null)}
        onOk={async () => {
          // 2026-10-19 M63a 真实调用: 后端 resetPassword 走 SMS 二次验证后写新密码
          //   - 此处直接生成一个 64 字符 hex 模拟前端 pre-hash(SHA-256)
          //   - 生产环境应在弹窗里要求输入 SMS 验证码
          //   - 必须由超管调用(user_admin 无权代重置,RBAC 拒绝)
          if (!resetting) return
          try {
            // 用 crypto.subtle 生成 16 字节随机,hex 编码后 32 字符
            // 实际应为 64 字符(32 字节)以匹配后端 SHA-256 pre-hash
            // 这里直接生成 32 字节随机 hex(64 字符)
            const buf = new Uint8Array(32)
            crypto.getRandomValues(buf)
            const newPwd = Array.from(buf).map((b) => b.toString(16).padStart(2, '0')).join('')
            await resetPassword(String(resetting.id), newPwd)
            message.success(t('page.user.resetDone', undefined, { pwd: newPwd }))
          } catch (err: any) {
            message.error(err?.message || t('page.user.resetFailed'))
          } finally {
            setResetting(null)
          }
        }}
      />
    </div>
  )
}

export default UserList
