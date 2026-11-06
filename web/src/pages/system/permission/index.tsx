/**
 * 资源管理 —— 菜单/按钮/API 三类权限点统一管理
 *
 * 2026-10-18 由骨架占位 → 真实化
 *   - 替代 PagePlaceholder,直接调用后端 resourceApi.tree / create / update / remove
 *   - 类型筛选(全部/菜单/按钮/API)过滤数据源
 *   - parent 字段用 Select 树选(后端 sys_resource.parent_id 引用,自引用)
 *   - 删除时若后端报 "已被角色绑定" / "存在子资源",用 message.error 提示并保留 ConfirmDialog 关闭
 *   - 资源类型(菜单/按钮/API)用 useDict('resource_type') 取中文 label
 *
 * 后端约定:
 *   GET    /rapid-admin/v1/system/resources        list (有分页/搜索/类型过滤,本页用 tree 替代)
 *   GET    /rapid-admin/v1/system/resources/tree   全量树(本系统全部权限点,is_active=true)
 *   POST   /rapid-admin/v1/system/resources        create
 *   PATCH  /rapid-admin/v1/system/resources/:id    update
 *   DELETE /rapid-admin/v1/system/resources/:id    delete(自动校验子资源 / 角色绑定)
 */
import React, { useEffect, useMemo, useState } from 'react'
import { Button, Tag, App, Form, Select, TreeSelect, Space } from 'antd'
import { AuthButton } from '@/components/auth/AuthButton'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal, EditField } from '@/components/common/EditModal'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { RowActions, RowAction } from '@/components/common/RowActions'
import { useI18n } from '@/locales'
import { useDict } from '@/hooks/useDict'
import { resourceApi } from '@/api/role'
import { PermButton } from '@/constants/permissions'
import IconSelect from '@/components/common/IconSelect'
import {
  buildParentCandidates,
  METHOD_ALL,
  ICON_NONE,
  normalizeMethodPayload,
  normalizeIconPayload,
  HTTP_METHODS,
  type ResourceType,
} from './resourceRules'
import type { Resource } from '@/types/permission'

type TypeTab = 'all' | 'menu' | 'button' | 'api'

const PermissionList: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  // 字典约束(2026-10-18):resource_type 来自后端 sys_dict,不再硬编码 menu/button/api 判断
  const { getLabel: getTypeLabel, getColor: getTypeColor, options: typeOptions } = useDict('resource_type')

  const [typeTab, setTypeTab] = useState<TypeTab>('all')
  const [keyword, setKeyword] = useState('')
  const [tree, setTree] = useState<Resource[]>([])
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<{ open: boolean; data?: Resource | null }>({ open: false })
  const [deleting, setDeleting] = useState<Resource | null>(null)

  /**
   * 弹窗的 Form 实例(提到页面层)
   *
   * ⚠️ **为什么不能只用 EditModal 内部那个** —— 父资源候选项要跟着
   *   「资源类型」变(菜单只能挂菜单、按钮能挂按钮、接口能挂接口),
   *   而资源类型是**表单里的值**,只有拿到 Form 实例才能订阅它的变化。
   */
  const [form] = Form.useForm()

  /** 拉全量树(is_active=true,后端不过滤 type,前端按类型筛选) */
  const load = async () => {
    setLoading(true)
    try {
      const list = await resourceApi.tree()
      setTree(Array.isArray(list) ? list : [])
    } catch {
      setTree([])
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { load() }, [])

  /**
   * 当前表单里的资源类型
   *
   * ⚠️ `useWatch` 在字段还没注册时返回 undefined ⇒ 依次兜底:
   *   编辑行上的类型 → 'menu'(新增时的默认值)。
   */
  const watchedType = Form.useWatch('type', form)
  const currentType = (watchedType ?? editing.data?.type ?? 'menu') as ResourceType

  /** 按类型筛选 + 关键词过滤后的扁平列表(给表格用) */
  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    const out: Resource[] = []
    const walk = (nodes: Resource[], parents: string[]) => {
      for (const n of nodes) {
        const path = [...parents, n.name]
        const matchType = typeTab === 'all' || n.type === typeTab
        const matchKw =
          !kw ||
          n.code?.toLowerCase().includes(kw) ||
          n.name?.toLowerCase().includes(kw) ||
          n.path?.toLowerCase().includes(kw)
        if (matchType && matchKw) {
          out.push({
            ...n,
            // 附加一个 _path 字段,表格渲染 breadcrumb 用
            ...({ _path: path.slice(0, -1).join(' / ') } as any),
          } as any)
        }
        if (n.children?.length) walk(n.children, path)
      }
    }
    walk(tree, [])
    return out
  }, [tree, typeTab, keyword])

  const columns = [
    {
      title: t('field.resourceCode'),
      dataIndex: 'code',
      width: 260,
      fixed: 'left' as const,
      render: (v: string) => <Tag color="blue">{v}</Tag>,
    },
    {
      title: t('field.resourceName'),
      dataIndex: 'name',
      width: 200,
      render: (v: string, row: any) => (
        <Space direction="vertical" size={0}>
          <span>{v}</span>
          {row._path && <span style={{ fontSize: 12, color: 'var(--app-text-tertiary)' }}>{row._path}</span>}
        </Space>
      ),
    },
    {
      title: t('field.type'),
      dataIndex: 'type',
      width: 100,
      render: (v: string) => {
        const label = getTypeLabel(v)
        if (label === String(v)) return <Tag>{v}</Tag>
        return <Tag color={getTypeColor(v)}>{label}</Tag>
      },
    },
    { title: t('field.path'), dataIndex: 'path', width: 220, ellipsis: true, render: (v: string) => v || '-' },
    { title: t('field.method'), dataIndex: 'method', width: 80, render: (v: string) => v ? <Tag color="purple">{v}</Tag> : '-' },
    { title: t('field.sort'), dataIndex: 'sort', width: 80 },
    {
      title: t('common.status'),
      dataIndex: 'is_active',
      width: 90,
      // 状态列的取值是"启用/禁用"(状态形容词),不是"启用/停用"(动作动词) ——
      // 中文用字不同,英文也不同:Active/Disabled vs Enable/Disable。
      render: (v: boolean) => (v ? <Tag color="green">{t('dict.status.active')}</Tag> : <Tag>{t('dict.status.disabled')}</Tag>),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 220,
      fixed: 'right' as const,
      render: (_: any, row: Resource) => {
        const actions: RowAction<Resource>[] = [
          // ⚠️ 「加子级」只对菜单行提供(2026-11-03 用户拍板):父级只对
          //   menu 有意义,给按钮/接口加子级 = 造一条没有读者的死数据。
          ...(row.type === 'menu'
            ? [
                {
                  key: 'add-child',
                  label: t('page.permission.addChild'),
                  perm: PermButton.RESOURCE_ADD_CHILD,
                  onClick: (r: Resource) => setEditing({ open: true, data: { ...r, id: undefined, code: '', name: '', parent_id: r.id } as any }),
                } as RowAction<Resource>,
              ]
            : []),
          {
            key: 'edit',
            label: t('common.edit'),
            perm: PermButton.RESOURCE_EDIT,
            onClick: (r) => setEditing({ open: true, data: r }),
          },
          {
            key: 'delete',
            label: t('common.delete'),
            perm: PermButton.RESOURCE_DELETE,
            danger: true,
            onClick: setDeleting,
          },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  // 父资源候选(排除自己、排除自己后代、按类型过滤)
  //
  // ⚠️ **后代必须按 parent_id 反查,不能靠 children 递归** ——
  //   后端 GET /system/resources/tree 返的是**扁平列表**
  //   (service/role_impl.go:360 Find 出来的 []model.SysResource,模型里压根没有
  //   children 字段),所以老代码里那段 `collectChildren(n.children)` **一次都没跑过**
  //   ⇒ 后代照样能被选成父资源 ⇒ 父子成环。
  //   环不是"渲染时死循环":环上的节点都不是根,于是它们在 buildMenuTree 里
  //   **凭空消失**(菜单和它的整个子树一起消失,静默的)。
  //
  // ⚠️ **类型过滤是用户 2026-10-30 提的**:"父资源的选择现在可以跨类型选择,
  //   这个合理吗?" —— 不合理:菜单只能挂在菜单下,否则它在侧边栏里永远不出现。
  //   规则见 ./resourceRules.ts(与后端 allowedParentTypes 镜像)。
  const parentCandidates = useMemo(
    () => buildParentCandidates(tree, { id: editing.data?.id, type: currentType }),
    [tree, editing.data?.id, currentType],
  )

  const editFields: EditField[] = useMemo(() => {
    const fields: EditField[] = [
      {
        name: 'code',
        label: t('field.resourceCode'),
        required: true,
        disabled: !!editing.data?.id,
        rules: [
          { required: true, message: t('common.placeholder.code') },
          { pattern: /^[a-z][a-z0-9_.*]+$/, message: t('page.permission.codePattern') },
        ],
      },
      { name: 'name', label: t('field.resourceName'), required: true, rules: [{ required: true, max: 64 }] },
      {
        name: 'type',
        label: t('field.type'),
        type: 'select',
        required: true,
        options: typeOptions.length > 0 ? typeOptions : [
          { value: 'menu', label: t('resourceType.menu') },
          { value: 'button', label: t('resourceType.button') },
          { value: 'api', label: t('resourceType.api') },
        ],
        disabled: !!editing.data?.id, // 改 type 风险大(影响 casbin 策略),编辑时禁掉
      },
      // ⚠️ **父级字段只对 menu 渲染**(2026-11-03 用户拍板):
      //   parent_id 唯一的消费者是菜单树(侧边栏 / 分配 Drawer),
      //   按钮 / 接口在所有消费方里都是平铺的 —— 字段对它们是死数据。
      //   隐藏之后 EditModal 用 validateFields() 收载荷,**没渲染的字段
      //   进不了载荷** ⇒ 提交不会碰 parent_id(历史脏值也保持原样,PATCH 语义)。
      ...(currentType === 'menu'
        ? ([
            {
        name: 'parent_id',
        label: t('page.permission.parentResource'),
        /**
         * ⚠️ **type 必须写 'custom'** ——
         * EditModal.renderField 只在 `f.type === 'custom'` 时才认 f.render,
         * 否则会落进 default 分支渲染成**普通输入框**。
         * 以前这里只写了 render 没写 type ⇒ 界面上是一个让人手填 19 位
         * sonyflake ID 的文本框(用户 2026-10-29 报:"应该下拉框选择,
         * 不然不知道父资源数值是多少")。
         *
         * ⚠️ **顶级选项的值是字符串 '0',不是数字 0** ——
         * 后端 SysResource.ParentID 是 `uint64 \`json:"parent_id,string"\``,
         * Go 的 ,string 选项只接受**带引号的**数字;发裸 0 会被
         * "json: invalid use of ,string struct tag" 直接拒收。
         * 同理,所有候选资源的 ID 一律是字符串(见 types/permission.ts 顶部注释)。
         */
        type: 'custom',
        render: ({ value, onChange }) => (
          <TreeSelect
            style={{ width: '100%' }}
            value={value ?? '0'}
            onChange={(v) => onChange(v ?? '0')}
            treeData={[{ value: '0', label: t('page.permission.topLevel') }, ...parentCandidates.map((c) => ({
              value: c.value,
              label: c.label,
            }))]}
            placeholder={t('common.placeholder.topLevelDefault')}
            allowClear
            treeDefaultExpandAll={false}
            showSearch
            treeNodeFilterProp="label"
          />
        ),
        help: t('page.permission.parentHelp'),
            },
          ] as EditField[])
        : []),
      {
        name: 'path',
        label: t('page.permission.pathLabel'),
        placeholder: t('page.permission.pathPlaceholder'),
      },
      {
        name: 'method',
        label: t('page.permission.httpMethod'),
        /**
         * ⚠️ **「全部」是这个字段的默认值,不是"没填"** ——
         *   method 这一列**不参与鉴权**(RBAC 只认 `module:action` 编码,
         *   压根不读 HTTP 方法),它只是"这个权限点对应哪个接口"的**标注**。
         *   所以"填了 GET"和"填了全部"在权限上**完全一样**,
         *   区别只在列表里那一列显示什么 —— 填"全部"才是诚实的默认值。
         *
         * ⚠️ 用 `custom` 而不是内置 'select' —— 内置那个不带 allowClear,
         *   清空后表单值是 undefined,显示成占位符而不是"全部"。
         */
        type: 'custom',
        render: ({ value, onChange }) => (
          <Select
            style={{ width: '100%' }}
            value={value ?? METHOD_ALL}
            onChange={(v) => onChange(v ?? METHOD_ALL)}
            options={[
              { value: METHOD_ALL, label: t('common.all') },
              ...HTTP_METHODS.map((m) => ({ value: m, label: m })),
            ]}
          />
        ),
        help: t('page.permission.methodHelp'),
      },
      {
        name: 'icon',
        label: t('page.permission.iconLabel'),
        /**
         * ⚠️ **改成下拉选择**(用户 2026-10-30 报:"图标现在是输入,
         *   用户根本不知道输入什么")—— 词表在 `@/constants/resourceIcons`,
         *   侧边栏用的是**同一张表**,不会对不上。
         *
         * 选项里画的是**真实图标**(2026-11-02 起由 IconSelect 统一渲染,
         *   与业务表单设计器共用同一个组件),用户直接看到选完长什么样,
         *   不用去记 'system' / 'job' 这种 key。
         */
        type: 'custom',
        render: ({ value, onChange }) => (
          <IconSelect
            value={(value as string | null | undefined) ?? ICON_NONE}
            onChange={(v) => onChange(v ?? ICON_NONE)}
            noneOption={{ value: ICON_NONE, label: t('page.permission.iconNone') }}
            placeholder={t('page.permission.iconPlaceholder')}
          />
        ),
        help: t('page.permission.iconHelp'),
      },
      { name: 'sort', label: t('field.sort'), type: 'number', initialValue: 0 },
    ]
    return fields
    // ⚠️ 依赖里必须有 t —— t 的引用随语言切换而变(见 locales/useI18n.ts 的 useMemo([dict])),
    // 漏掉它会让下拉选项在切换语言后停在旧语言上。
    // ⚠️ currentType 也要在依赖里 —— 父级字段按它条件渲染,漏掉的话
    //    切换类型后字段不会跟着出现/消失。
  }, [editing.data, typeOptions, parentCandidates, currentType, t])

  /**
   * 弹窗初值
   *
   * ⚠️ **必须 useMemo** —— EditModal 里有这么一段:
   *     useEffect(() => { if (open) { form.resetFields(); form.setFieldsValue(initialValues) } },
   *               [open, initialValues, form])
   *   `initialValues` 每次渲染都是**新对象**的话,这个 effect 每次渲染都重跑
   *   ⇒ 弹窗开着的时候页面只要重渲染一次,用户填的东西就被 reset 掉。
   *
   *   以前这里是行内字面量(每次渲染新对象)—— 编辑期间页面很少重渲染,
   *   所以一直没暴露;加了 `Form.useWatch('type')` 之后**每次改资源类型都会重渲染**
   *   ⇒ 不 memo 就是"改类型时把用户填的内容清空"。
   *
   * ⚠️ method / icon 归一化成哨兵值:
   *   后端返空 method 时表单要显示「全部」,空 icon 要显示「无图标」。
   */
  const initialValues = useMemo(() => {
    const base: Partial<Resource> = editing.data ??
      { type: 'menu', sort: 0, parent_id: '0', is_active: true }
    return {
      ...base,
      method: base.method ? base.method : METHOD_ALL,
      icon: base.icon ? base.icon : ICON_NONE,
    }
  }, [editing.data])

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<Resource>
        pageDescription={t('page.permission.description')}
        searchPlaceholder={t('common.placeholder.searchPermission')}
        onSearch={setKeyword}
        onReset={() => { setKeyword(''); setTypeTab('all') }}
        searchExtra={[
          /**
           * 2026-10-26:原来是 <Segmented>(全部/菜单/按钮/API 四段),
           *   换成 Select —— 跟全仓其它列表页一致(筛选器统一是 Select)。
           *   ⚠️ label 顺带把写死的 'API' 换成 t('resourceType.api'):
           *   中文侧是"接口",跟表格"类型"那一列的字典 label 一致
           *   (后端 sys_dict:resource_type/api → "接口");英文侧两边都是 API,不变。
           */
          <Select
            key="type"
            style={{ width: 140 }}
            value={typeTab}
            onChange={(v) => setTypeTab(v as TypeTab)}
            options={[
              { value: 'all', label: t('common.all') },
              { value: 'menu', label: t('resourceType.menu') },
              { value: 'button', label: t('resourceType.button') },
              { value: 'api', label: t('resourceType.api') },
            ]}
          />,
        ]}
        inlineToolBar={[
          // reload 走 RouteGuard 路由级(只读能力,所有能进页面的角色都该能刷新)
          <Button key="reload" icon={<ReloadOutlined />} onClick={load}>
            {t('common.refresh')}
          </Button>,
          <AuthButton
            key="create"
            type="primary"
            icon={<PlusOutlined />}
            perm={PermButton.RESOURCE_ADD}
            onClick={() => setEditing({ open: true, data: null })}
          >
            {t('common.add')}
          </AuthButton>,
        ]}
        columns={columns}
        tableProps={{
          loading,
          dataSource: filtered,
          rowKey: 'id',
          pagination: {pageSize: 50, showTotal: (n) => t('common.total', undefined, { n }) },
        }}
        scrollX={1500}
      />

      <EditModal<Resource>
        title={editing.data?.id ? t('common.edit') : t('resource.resource_create')}
        open={editing.open}
        form={form}
        forceRender
        initialValues={initialValues}
        fields={editFields}
        onCancel={() => setEditing({ open: false })}
        width={680}
        onSubmit={async (vals) => {
          // 后端 PATCH 接口对 parentId / sort / isActive 是基于空值才赋值,
          // 这里把 parent_id 显式置 '0'(顶级)。
          // ⚠️ **必须是字符串 '0'** —— 后端字段是 `uint64 \`json:"parent_id,string"\``,
          //   发裸数字 0 会被 Go 的 ,string 选项拒收(整个请求 400)。
          //   '0' 是非空字符串 ⇒ 下面的 `!payload.parent_id` 判不出来 ⇒ 不会被覆盖,
          //   只有"用户清空下拉"(值变 undefined)时才会兜底成 '0'。
          const payload: any = { ...vals }
          if (!payload.parent_id) payload.parent_id = '0'
          // ⚠️ **method / icon 必须显式发空串** —— 后端这两个字段是 `*string`,
          //   空串 = "清空这一列",不传 = "保持原值"。
          //   如果这里发 undefined,JSON 里根本没这个 key ⇒ 用户选了「全部」
          //   /「无图标」,库里还留着老值 —— **改了跟没改一样**,而且不报错。
          payload.method = normalizeMethodPayload(payload.method)
          payload.icon = normalizeIconPayload(payload.icon)
          if (editing.data?.id) {
            await resourceApi.update(String(editing.data.id), payload)
          } else {
            await resourceApi.create(payload)
          }
          message.success(t('common.success'))
          setEditing({ open: false })
          load()
        }}
      />

      <ConfirmDialog
        open={!!deleting}
        danger
        title={`${t('resource.resource_delete')} ${deleting?.name}?`}
        description={
          <Space direction="vertical" size={4}>
            <span>{t('page.permission.deleteTip1')}</span>
            <span style={{ color: 'var(--app-text-tertiary)' }}>
              {t('page.permission.deleteTip2')}
            </span>
          </Space>
        }
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          try {
            await resourceApi.remove(String(deleting.id))
            message.success(t('common.success'))
            setDeleting(null)
            load()
          } catch (e: any) {
            // 后端 400/409 时 message.error 会自动展示,这里仅保底兜一次
            message.error(e?.message || t('common.deleteFailed'))
            setDeleting(null)
          }
        }}
      />
    </div>
  )
}

export default PermissionList
