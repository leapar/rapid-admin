/**
 * 分配权限 Drawer —— 角色管理页"分配权限"按钮的实装
 *
 * 2026-10-18 由占位 ConfirmDialog 升级为 Drawer + Tree:
 *   - 左侧:按类型分组的 Tree(菜单 / API / 按钮)
 *   - 顶部:已选 / 总数 实时统计 + 关键词搜索 + 类型 Tab 切换
 *   - 保存:把 Tree 的 checkedKeys 全量 POST 给后端 AssignResources(覆盖式)
 *
 * 2026-10-18 加 readOnly 模式:
 *   - 内置/超管角色不能让改权限,但要能看权限(原 disabled 按钮直接堵死入口)
 *   - readOnly=true 时:Tree disabled + 隐藏"本视图全选/清空" + 保存按钮改"关闭"
 *   - 后端 GetRoleResources 走 rbac("role","assign"),protected 角色同样可读
 *
 * 2026-10-25 修 Bug(用户报:"菜单无法勾选,现在全部是默认勾选"):
 *   根因有两个,都在"把后端平铺列表当树用"这一处:
 *     1. 后端 GET /system/resources/tree 返的是**扁平列表**(model.SysResource
 *        没有 children 字段),但这里写的是 `m.children` —— 恒为 undefined,
 *        菜单层级整个丢了(44 个菜单平铺成一层)。
 *     2. 菜单节点被标成 `checkable: false` + key 用 `m-${id}` 而不是真 ID,
 *        于是:菜单**永远勾不上**,而且已绑定的菜单也**显示不出勾选态**
 *        (Tree 的 key 对不上 checkedKeys 里的真 ID)。
 *     3. 菜单标题里画了个蓝色 <CheckCircleOutlined/>(原本大概是"这是菜单"的装饰),
 *        长得跟勾选框一模一样 → 用户看到"全部默认勾选"。
 *   现在:菜单按 parent_id 建真树、key 用真 ID、可勾、装饰图标换成中性文件夹图标。
 *
 *   配套还修了两处(写渲染测试时暴露的,不修界面上还是不对):
 *     4. 三个分组节点是“菜单/按钮/API”三组的父节点,而 Tree 只渲染展开分支下的
 *        节点 —— 分组不展开 = 组里一条资源都看不见(抽屉里只剩三个组标题)。
 *        改成受控 expandedKeys:分组默认展开,菜单子树保持折叠。
 *     5. rc-tree 收到受控 checkedKeys 时会自己级联(勾父带子),但**不回调
 *        onCheck** —— state 里还是原始 ID,树上却是级联后的 → “已选 1”但树上
 *        两个对勾,保存载荷也跟显示对不上。加载时先 conduct 一遍
 *        (assignTree.conductCheckedIds 复刻了 rc-tree 的两条级联规则)。
 *
 * 2026-10-29 修 UI(用户报:"菜单、按钮、api 应该换成 select 组件"):
 *   - 类型切换 Segmented → Select(跟资源管理页 2026-10-26 的改动对齐,
 *     全仓筛选器统一是 Select;Segmented 是"radio 换皮",四段挤在一行还占宽)
 *   - 顺手把写死的 'API' 换成 t('resourceType.api')(中文侧是"接口",
 *     与表格"类型"列的字典 label 一致)
 *
 * 关键设计:
 *   - 打开 Drawer 时同步调 getResources 拿已绑 ID,作为 checkedKeys 初值
 *   - 缺这步,用户一进来 Tree 全空,保存会把所有绑定清空(数据事故)
 *   - 分组标题节点(`__group_*`)不可勾,只是分组;真正的资源节点全部可勾
 *   - 菜单父子用 antd 默认的级联(勾父带子):菜单树本来就是父子结构,
 *     且 seed 也是父子一起授权的(见 role-resource 绑定矩阵)
 *
 * 数据流:
 *   tree:     GET /system/resources/tree         (拉全量,is_active=true;**扁平**)
 *   已绑:     GET /system/roles/:id/resources    (resource_ids[])
 *   保存:     POST /system/roles/:id/resources   (resource_ids[])
 */
import React, { useEffect, useMemo, useState } from 'react'
import { Drawer, Tree, Input, Select, Button, App, Space, Tag, Empty, Spin } from 'antd'
import type { DataNode } from 'antd/es/tree'
import { SearchOutlined, FolderOutlined, SaveOutlined } from '@ant-design/icons'
import { useI18n, tError } from '@/locales'
import { useDict } from '@/hooks/useDict'
import { resourceApi, roleApi } from '@/api/role'
import type { Resource } from '@/types/permission'
import type { Role } from '@/types/permission'
import {
  buildMenuSubtree,
  collectIdsByType,
  conductCheckedIds,
  countResources,
  filterTreeByKeyword,
  matchResource,
} from './assignTree'
import type { ResourceTypeTab } from './assignTree'

type TypeTab = ResourceTypeTab

/** 分组哨兵 key(不是资源 ID,保存前必须剔掉) */
const GROUP_KEYS = ['__group_menu__', '__group_button__', '__group_api__'] as const

interface Props {
  open: boolean
  role: Role | null
  onClose: () => void
  onSaved?: () => void
  /** true = 只读:内置/超管角色查看权限用,禁用 Tree checkbox + 隐藏保存/全选/清空 */
  readOnly?: boolean
}

export const AssignResourcesDrawer: React.FC<Props> = ({ open, role, onClose, onSaved, readOnly = false }) => {
  const { t } = useI18n()
  const { message } = App.useApp()
  // 字典(2026-10-18):类型中文 label 取自 sys_dict
  const { getLabel: getTypeLabel, getColor: getTypeColor } = useDict('resource_type')

  const [typeTab, setTypeTab] = useState<TypeTab>('all')
  const [keyword, setKeyword] = useState('')
  // 后端返的是**扁平列表**,层级在 buildMenuSubtree 里按 parent_id 建
  const [flat, setFlat] = useState<Resource[]>([])
  const [loading, setLoading] = useState(false)
  const [checkedKeys, setCheckedKeys] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  /**
   * 展开状态:**分组节点默认展开**,菜单子树默认折叠。
   * ⚠️ 分组节点是"菜单/按钮/API"三组的父节点,不展开的话组里一条资源都看不见
   *   (Tree 只渲染展开分支下的节点)—— 表现就是"抽屉里空空的,只有三个组标题"。
   *   菜单本身保持折叠,44 个菜单铺开没法看。
   */
  const [expandedKeys, setExpandedKeys] = useState<string[]>([...GROUP_KEYS])

  /** 拉资源列表 + 角色已绑 ID(只 Drawer 打开时拉,避免每次输入搜索都重拉) */
  useEffect(() => {
    if (!open || !role) return
    setLoading(true)
    Promise.all([resourceApi.tree(), roleApi.getResources(String(role.id))])
      .then(([list, bound]) => {
        const list0 = Array.isArray(list) ? list : []
        setFlat(list0)
        // ⚠️ 必须 conduct 一遍:rc-tree 收到受控 checkedKeys 时会自己级联
        //   (勾了父 → 子也算勾上),但**不会回调 onCheck**。
        //   不 conduct 的话:树上显示 m1+m2 都勾上了,「已选」却还是 1,
        //   保存出去的 ID 跟用户看到的也对不上。
        setCheckedKeys(
          conductCheckedIds(list0, Array.isArray(bound?.resource_ids) ? bound.resource_ids : []),
        )
      })
      .catch((e) => {
        message.error(t('page.role.assign.loadFailed', undefined, { msg: tError(e?.code ?? 0, e?.message ?? e) }))
        setFlat([])
        setCheckedKeys([])
      })
      .finally(() => setLoading(false))
  }, [open, role, message, t])

  /** 统计:按类型分组的总数 + 已选数 */
  const stats = useMemo(() => countResources(flat, checkedKeys), [flat, checkedKeys])

  /** 把 扁平列表 + tab + keyword 转成 Antd Tree 用的 DataNode
   *  - 菜单组:真层级(后端扁平 → 这里按 parent_id 建树),菜单节点可勾
   *  - 按钮组 / API 组:各自平铺(后端这两个类型的 parent_id 基本都是 0)
   *  - 分组标题节点(`__group_*`)不可勾
   */
  const treeData: DataNode[] = useMemo(() => {
    const out: DataNode[] = []

    // ---- 菜单组 ----
    if (typeTab === 'all' || typeTab === 'menu') {
      const menuRoots = filterTreeByKeyword(buildMenuSubtree(flat), keyword)
      // 菜单节点自己带 children,递归转成 DataNode
      const toNodes = (nodes: Resource[]): DataNode[] =>
        nodes.map((m) => ({
          key: m.id,
          title: <MenuTitle res={m} />,
          children: toNodes(m.children ?? []),
        }))
      const sub = toNodes(menuRoots)
      if (sub.length) {
        out.push({
          key: '__group_menu__',
          title: <GroupTitle type="menu" total={stats.menu} />,
          checkable: false,
          disableCheckbox: true,
          children: sub,
        })
      }
    }

    // ---- 按钮组 ----
    if (typeTab === 'all' || typeTab === 'button') {
      const buttonList = flat.filter((r) => r.type === 'button' && matchResource(r, keyword))
      if (buttonList.length) {
        out.push({
          key: '__group_button__',
          title: <GroupTitle type="button" total={stats.button} />,
          checkable: false,
          disableCheckbox: true,
          children: buttonList.map<DataNode>((b) => ({
            key: b.id,
            title: <ResourceTitle res={b} />,
          })),
        })
      }
    }

    // ---- API 组 ----
    if (typeTab === 'all' || typeTab === 'api') {
      const apiList = flat.filter((r) => r.type === 'api' && matchResource(r, keyword))
      if (apiList.length) {
        out.push({
          key: '__group_api__',
          title: <GroupTitle type="api" total={stats.api} />,
          checkable: false,
          disableCheckbox: true,
          children: apiList.map<DataNode>((a) => ({
            key: a.id,
            title: <ResourceTitle res={a} />,
          })),
        })
      }
    }

    return out
  }, [flat, typeTab, keyword, stats])

  /** checkedKeys 落地:
   *  - 过滤掉分组哨兵 key(`__group_*`)—— 它们不是资源 ID,
   *    混进去 POST 给后端会换来一句"部分 resource_id 不存在"。
   *  - 菜单节点的 key 现在就是真资源 ID(以前是 `m-${id}`,跟已绑 ID 对不上,
   *    所以菜单的勾选态**从来显示不出来**)。
   *  readOnly 模式下二次拦截:antd Tree 自身有 `disabled`,但 onCheck 仍可能被
   *  父节点折叠等动作触发,这里挡住避免误改本地 state。
   */
  const onCheck = (keys: any) => {
    if (readOnly) return
    const idKeys = (keys as React.Key[])
      .map((k) => String(k))
      .filter((k) => !k.startsWith('__group_'))
    // rc-tree 的 onCheck 已经是 conduct 过的结果,这里再 conduct 一次是幂等的,
    // 但能保证 state 跟树上显示、跟保存载荷三者永远一致。
    setCheckedKeys(conductCheckedIds(flat, idKeys))
  }

  /** 一键全选当前 tab(菜单也算 —— 菜单是能授权的资源点) */
  const selectAllInTab = () => {
    const ids = collectIdsByType(flat, typeTab)
    setCheckedKeys(Array.from(new Set([...checkedKeys, ...ids])))
  }

  /** 清空当前 tab */
  const clearInTab = () => {
    const toRemove = new Set(collectIdsByType(flat, typeTab))
    setCheckedKeys(checkedKeys.filter((k) => !toRemove.has(k)))
  }

  /** 保存:全量覆盖。readOnly 模式此函数不会被调用(按钮换成"关闭") */
  const save = async () => {
    if (!role) return
    setSaving(true)
    try {
      await roleApi.assignResources(String(role.id), checkedKeys.map(String))
      message.success(
        t('page.role.assign.success', undefined, { name: role.name, n: checkedKeys.length }),
      )
      onSaved?.()
      onClose()
    } catch (e: any) {
      message.error(e?.message || t('common.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width={720}
      title={
        role
          ? readOnly
            ? (
              <Space>
                {t('page.role.assign.viewTitle', undefined, { name: role.name })}
                <Tag color="gold">{t('page.role.assign.readOnlyTag')}</Tag>
              </Space>
            )
            : t('page.role.assign.title', undefined, { name: role.name })
          : t('resource.role_assign')
      }
      destroyOnHidden
      extra={
        <Space>
          {readOnly ? (
            <Button type="primary" onClick={onClose}>{t('common.close')}</Button>
          ) : (
            <>
              <Button onClick={onClose}>{t('common.cancel')}</Button>
              <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={save}>
                {t('common.save')}
              </Button>
            </>
          )}
        </Space>
      }
    >
      {/* 顶部统计 + 搜索 + 类型 Tab + 全选/清空 */}
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Space wrap>
          <Tag color="blue">
            {t('page.role.assign.selected', undefined, { n: stats.selected, total: stats.total })}
          </Tag>
          <Tag>{t('resourceType.menu')} {stats.menu}</Tag>
          <Tag color="cyan">{t('resourceType.button')} {stats.button}</Tag>
          <Tag color="purple">{t('resourceType.api')} {stats.api}</Tag>
        </Space>

        <Space wrap>
          <Input
            allowClear
            placeholder={t('common.placeholder.searchResource')}
            prefix={<SearchOutlined />}
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            style={{ width: 260 }}
          />
          {/**
           * 2026-10-29:原来是 <Segmented>(全部/菜单/按钮/API 四段),
           *   换成 Select —— 跟全仓其它列表页一致(筛选器统一是 Select),
           *   资源管理页 2026-10-26 已经换过一轮,这里跟上。
           * ⚠️ label 顺带把写死的 'API' 换成 t('resourceType.api'):
           *   中文侧是"接口",跟资源管理页"类型"那一列的字典 label 一致
           *   (后端 sys_dict:resource_type/api → "接口");英文侧两边都是 API,不变。
           */}
          <Select
            style={{ width: 140 }}
            value={typeTab}
            onChange={(v) => setTypeTab(v as TypeTab)}
            options={[
              { value: 'all', label: t('common.all') },
              { value: 'menu', label: t('resourceType.menu') },
              { value: 'button', label: t('resourceType.button') },
              { value: 'api', label: t('resourceType.api') },
            ]}
          />
          {/* readOnly 模式下隐藏"全选/清空",只允许搜索 + 类型 tab 切换查看 */}
          {!readOnly && (
            <>
              <Button size="small" onClick={selectAllInTab}>
                {t('page.role.assign.selectAllInView')}
              </Button>
              <Button size="small" onClick={clearInTab}>
                {t('page.role.assign.clearInView')}
              </Button>
            </>
          )}
        </Space>

        {loading ? (
          <div style={{ textAlign: 'center', padding: 60 }}><Spin /></div>
        ) : treeData.length === 0 ? (
          <Empty description={t('page.role.assign.empty')} />
        ) : (
          <div style={{ border: '0.5px solid var(--app-surface-border)', borderRadius: 8, padding: 12, maxHeight: 'calc(100vh - 190px)', overflow: 'auto' }}>
            <Tree
              checkable
              checkedKeys={checkedKeys}
              onCheck={onCheck}
              treeData={treeData}
              expandedKeys={expandedKeys}
              onExpand={(keys) => setExpandedKeys(keys.map(String))}
              showLine
              blockNode
              disabled={readOnly}
            />
          </div>
        )}
      </Space>
    </Drawer>
  )
}

/* ===== 子组件 ===== */

const MenuTitle: React.FC<{ res: Resource }> = ({ res }) => (
  <Space size={6}>
    {/* ⚠️ 这里原来画的是 <CheckCircleOutlined/>(蓝色实心对勾),长得跟勾选框一模一样,
        用户看到的是"菜单全部默认勾选了" —— 其实那只是个装饰图标。
        换成中性的文件夹图标,勾选状态一律交给 Tree 自己的复选框表达。 */}
    <FolderOutlined style={{ color: 'var(--app-text-tertiary)' }} />
    <span style={{ fontWeight: 500 }}>{res.name}</span>
    <Tag color="blue">{res.code}</Tag>
    {res.path && <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>{res.path}</span>}
  </Space>
)

const ResourceTitle: React.FC<{ res: Resource }> = ({ res }) => {
  const { getColor } = useDict('resource_type')
  return (
    <Space size={6}>
      <Tag color={getColor(res.type)} style={{ margin: 0 }}>{res.type}</Tag>
      <span>{res.name}</span>
      <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>{res.code}</span>
    </Space>
  )
}

const GroupTitle: React.FC<{ type: 'menu' | 'button' | 'api'; total: number }> = ({ type, total }) => {
  const { getLabel, getColor } = useDict('resource_type')
  // 子组件自己取 t —— 父组件的 t 传不进来(这里是独立组件,不是嵌套在父组件的 JSX 里)
  const { t } = useI18n()
  return (
    <Space size={6} style={{ fontWeight: 600 }}>
      <Tag color={getColor(type)} style={{ margin: 0 }}>{getLabel(type)}</Tag>
      <span style={{ color: 'var(--app-text-tertiary)', fontWeight: 400 }}>
        {t('common.countItems', undefined, { n: total })}
      </span>
    </Space>
  )
}
