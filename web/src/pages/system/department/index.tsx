/**
 * 部门管理 —— 左侧组织树 + 右侧部门详情
 *
 * 2026-10-18 重构(用户反馈:17 节点 table 平铺看不清层级 + 风格不统一):
 *   - 旧版:平铺 Table,17 个部门挤在一起,层级关系靠 'path' 列猜
 *   - 新版:左侧 Antd Tree 展示组织树(默认全展开,内置搜索),
 *           右侧选中节点后展示该部门详情 + 子部门列表 + 操作
 *   - 风格对齐其他业务页:
 *     · 顶部 PageHeader(跟 用户/角色/字典 一致)
 *     · 搜索条放在左侧 Card 内,Antd Tree 内部(不单独占 SearchSection,
 *       树本身就是搜索容器,放外面会重复)
 *     · 右侧 Card 用 Descriptions 展示属性(跟用户/角色详情一致)
 *     · 全部使用 antd Tag / Space / Skeleton,字体颜色用 antd token,不写死 hex
 *     · AuthButton 统一权限控制
 *
 * 关联:
 *   - departmentApi.tree() 一次拉全树,Antd Tree 直接消费
 *   - 子部门列表从树节点里直接 children 抽出,无二次请求
 *   - 详情/编辑/删除逻辑沿用 EditModal + ViewDrawer + ConfirmDialog
 *
 * 2026-10-25 排版对齐(用户反馈:左右两个组件的字体、字号、排版、padding、间距没调整):
 *   这个页面是全站唯一大量硬编码字号的页面(本页 5 处 fontSize:14,而 14 不在字号刻度
 *   表 10/10.5/11/12/13/16/18/20 上;全站另一处在审批详情页的标题上,那里是标题
 *   语义,不算越界),且左右两栏各写各的。逐条改法:
 *   · 字号 14 → 13(继承):树节点名、两个 Card 标题、子部门标题、列表项名
 *     —— 14 让左栏正文比右栏 Descriptions 正文(antd 原生 13)大一号,左右对不上。
 *   · 徽标字号 11 → 10(继承):树里的「禁用」Tag 与列表里的「禁用/启用」Tag 同页两种字号
 *   · Card 头高度 40 → 44(继承):写死的 40 比 app 其它 size="small" Card 矮 4px
 *   · Card 内边距 左 12 / 右 16 → 统一 14px 16px(用户指定):
 *     跟 app 自己的 SearchSection(14px 16px)和参考原型的 .table-tools(14px 16px)同刻度;
 *     antd 的 bodyPaddingSM=12 对这个双栏页面偏紧(右栏塞的是带边框的 Descriptions/List)。
 *   · 列表里的 code 字号 11 → 12(全站次级文字惯例:#999 + 12)
 *   ⚠️ 头部高度改成 44 后,body 的 calc(100% - 40px) 基准同步改成 44px。
 *
 * 2026-10-26(用户手动改样式 + 滚动压线修复):
 *   · **内边距从 body 挪到 Card 根**:Card 根现在自带 `padding: '14px 16px'`,
 *     header 的 padding 归零 —— 详见下面 styles 处的逐条注释。
 *   · **树滚动时压住卡片头的底边线**(用户反馈):antd 给 `.ant-card-head` 写了
 *     `margin-bottom: -1px`,让 body 的滚动视口比分隔线高 1px,内容一滚就骑到
 *     线上。已在 `styles/global.css` 里全局归零(`.ant-card .ant-card-head`),
 *     本页不用再单独处理。
 *   ⚠️ 刻意没动的:Space size=6/8/16(app 间距刻度 4/8/16,不是缺陷)。
 *   · 图标色已改 var(--app-primary)(2026-11-05 皮肤沉浸式:写死的蓝在
 *     政务红/文博棕金皮肤下是外来冷色,统计卡图标同理已全站跟主题)。
 *   · 页内大标题去掉(PageHeader title=""):布局里已有面包屑(Breadcrumb 按路由
 *     自动生成菜单名),页内再写一遍「部门管理」是同一句话说两遍。
 *     本页左右两栏的卡片标题(组织架构 / 部门详情)已经说明了这是哪一页。
 *
 * 待优化(P1+):
 *   - 详情页加部门成员列表(member_count 字段已补,成员列表本身待加)
 *   - 拖拽排序同级 sort(sort 字段已加,UI 待加)
 *   - 批量导入 / 导出(P1)
 */
import React, { useEffect, useMemo, useState } from 'react'
import {
  Tree, TreeSelect, Select, Row, Col, Card, Tag, App, Empty, Skeleton, Descriptions, List, Input, Space, Button, Typography, Spin,
} from 'antd'
import type { DataNode } from 'antd/es/tree'
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  EyeOutlined,
  ApartmentOutlined,
} from '@ant-design/icons'
import { PageHeader } from '@/components/common/PageHeader'
import { EditModal, EditField } from '@/components/common/EditModal'
import { ViewDrawer, ViewField } from '@/components/common/ViewDrawer'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { AuthButton } from '@/components/auth/AuthButton'
import { useI18n } from '@/locales'
import { departmentApi, DepartmentTreeNode } from '@/api/role'
import { userApi } from '@/api/user'
import { PermButton } from '@/constants/permissions'
import type { Department } from '@/types/permission'
import type { SysUser } from '@/types/permission'

const { Text } = Typography

// Antd DataNode 包装(后端 tree 端点返 DepartmentTreeNode[] children 嵌套,Antd 需要 key/title/children)
type TreeNode = DataNode & {
  raw: DepartmentTreeNode
}

const DepartmentList: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()

  // ===== 数据 =====
  const [tree, setTree] = useState<DepartmentTreeNode[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined)
  // 展开节点 keys(普通 Tree 没 treeExpandAction,改用受控 expandedKeys 让用户点 switcher 能正常 toggle)
  const [expandedKeys, setExpandedKeys] = useState<string[]>([])

  // ===== Modals =====
  const [editing, setEditing] = useState<{ open: boolean; data?: Department | null; parentId?: string }>({ open: false })
  const [viewing, setViewing] = useState<Department | null>(null)
  const [deleting, setDeleting] = useState<Department | null>(null)
  // 搜索关键字(传给 antd Tree 的 searchValue,内置高亮)
  const [searchKw, setSearchKw] = useState('')

  // ===== 加载树 =====
  const loadTree = async () => {
    setLoading(true)
    try {
      const data = await departmentApi.tree()
      const arr = Array.isArray(data) ? data : []
      setTree(arr)
      // 默认全展开(扁平化所有 key,放进受控 expandedKeys,用户能 toggle)
      const allKeys: string[] = []
      const walk = (nodes: DepartmentTreeNode[]) => {
        for (const n of nodes) {
          allKeys.push(String(n.id))
          if (n.children?.length) walk(n.children)
        }
      }
      walk(arr)
      setExpandedKeys(allKeys)
      // 默认选中根节点(让右侧详情立刻有内容,避免空白)
      if (!selectedId && arr.length > 0 && arr[0]) {
        setSelectedId(arr[0].id)
      }
    } catch {
      setTree([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadTree()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ===== Antd Tree 树形数据(把 DepartmentTreeNode 转成 DataNode + 客户端搜索过滤) =====
  // 2026-10-18:搜索走前端过滤,Antd Tree 的 searchValue prop 在大树上性能一般,
  //   这里用更稳的方式:搜索关键字命中 name/code 时,只保留该节点 + 其祖先链,
  //   默认全展开,用户能直接看到匹配项
  const treeData: TreeNode[] = useMemo(() => {
    const matchNode = (n: DepartmentTreeNode, kw: string): boolean => {
      if (!kw) return true
      const lower = kw.toLowerCase()
      return n.name.toLowerCase().includes(lower) || n.code.toLowerCase().includes(lower)
    }
    // 递归过滤:节点自身或任一后代命中 → 保留(只保留命中的子树分支)
    const filterTree = (nodes: DepartmentTreeNode[]): DepartmentTreeNode[] => {
      if (!searchKw) return nodes
      const out: DepartmentTreeNode[] = []
      for (const n of nodes) {
        const selfHit = matchNode(n, searchKw)
        const filteredChildren = n.children ? filterTree(n.children) : []
        if (selfHit || filteredChildren.length > 0) {
          out.push({ ...n, children: filteredChildren })
        }
      }
      return out
    }
    const filtered = filterTree(tree)
    const toNode = (n: DepartmentTreeNode): TreeNode => ({
      key: String(n.id),
      title: (
        // 2026-10-18:title 容器加 width:100% + min-width:0,防止部门名过长撑出横向滚动条
        // 2026-10-25:去掉 fontSize:14 —— 14 不在字号刻度表(10/10.5/11/12/13/16/18/20)上,
        //   全站只有这一个文件在用。树节点名跟右侧 Descriptions 正文(13)必须同号,
        //   否则左右两栏正文大小不一 —— 这就是「左右两个组件字号没调整」的实锤。
        <Space size={6} style={{ width: '100%', minWidth: 0, overflow: 'hidden' }}>
          <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {n.name}
          </span>
          {/* 2026-10-25:去掉 fontSize:11 —— Tag 的字号由 app 主题 Tag.fontSizeSM=10 决定
             (徽标刻度),右侧列表里同一个「禁用」Tag 就是 10,原来这里是 11,同页两个同义
             徽标两个字号。 */}
          {!n.is_active && <Tag color="default" style={{ margin: 0, flexShrink: 0 }}>{t('dict.status.disabled')}</Tag>}
        </Space>
      ),
      raw: n,
      children: n.children?.map(toNode),
    })
    return filtered.map(toNode)
    // ⚠️ 依赖里必须有 t —— t 的引用随语言切换而变(见 locales/useI18n.ts 的 useMemo([dict])),
    // 漏掉它会让树节点上的状态徽标在切换语言后停在旧语言上。
  }, [tree, searchKw, t])

  // 拍平收集所有 key(用于搜索时强制全展开)
  const collectAllKeys = (n: DepartmentTreeNode): string[] => {
    const out = [String(n.id)]
    if (n.children) n.children.forEach((c) => out.push(...collectAllKeys(c)))
    return out
  }

  // ===== 把树拍平查 id(树深度 ≤ 3,拍平成本可接受) =====
  const flatMap = useMemo(() => {
    const map = new Map<string, DepartmentTreeNode>()
    const walk = (nodes: DepartmentTreeNode[]) => {
      for (const n of nodes) {
        map.set(n.id, n)
        if (n.children?.length) walk(n.children)
      }
    }
    walk(tree)
    return map
  }, [tree])

  // ⚠️ 原来是 flatMap.get(Number(selectedId)) || flatMap.get(selectedId):
  // Number() 把 19 位 ID 截断,第一个 lookup 永远 miss,靠第二个兜底才碰巧能用。
  const selected = selectedId ? flatMap.get(selectedId) : undefined
  const children = (selected?.children ?? []) as DepartmentTreeNode[]

  // ===== 操作 =====
  const onAddRoot = () => { loadUsers(); setEditing({ open: true, data: null, parentId: '0' }) }
  const onAddChild = () => {
    if (!selected) {
      message.warning(t('page.dept.selectParentFirst'))
      return
    }
    loadUsers()
    setEditing({ open: true, data: null, parentId: selected.id })
  }
  const onEdit = (d: Department) => { loadUsers(); setEditing({ open: true, data: d }) }
  const onDelete = (d: Department) => setDeleting(d)
  const onView = (d: Department) => setViewing(d)

  // ===== 用户列表(给 leader_id Select 用) =====
  // 2026-10-18:modal 打开时按需加载,而不是页 mount 时一次拉完
  //   后续可加 onSearch 触发 server side filter
  const [userOptions, setUserOptions] = useState<{ value: string; label: string }[]>([])
  const [usersLoading, setUsersLoading] = useState(false)
  const loadUsers = async () => {
    if (userOptions.length > 0) return // 已加载过就不重拉
    setUsersLoading(true)
    try {
      // page_size=200 一次拉够,后期用户多了再改 server search
      const res = await userApi.list({ page: 1, page_size: 200, keyword: '' } as any)
      const list = (res?.list ?? []) as SysUser[]
      setUserOptions(
        list.map((u) => ({
          value: u.id,
          label: u.nickname || u.username,
        })),
      )
    } catch {
      setUserOptions([])
    } finally {
      setUsersLoading(false)
    }
  }

  // ===== TreeSelect 用的扁平+children 数据 =====
  const parentTreeOptions = useMemo(() => {
    // 排除自己 + 后代(不能让部门选自己当父级,会形成环)
    const collectDescendantIDs = (root: DepartmentTreeNode): Set<string> => {
      const out = new Set<string>([root.id])
      const walk = (n: DepartmentTreeNode) => {
        n.children?.forEach((c) => { out.add(c.id); walk(c) })
      }
      walk(root)
      return out
    }
    const excluded = editing.data
      ? collectDescendantIDs(tree.find((t) => t.id === editing.data!.id) ?? tree[0] ?? { id: '0' } as any)
      : new Set<string>([])
    const toNode = (n: DepartmentTreeNode): any => ({
      value: n.id,
      title: n.name,
      disabled: excluded.has(n.id),
      children: n.children?.length ? n.children.map(toNode) : undefined,
    })
    return tree.map(toNode)
  }, [tree, editing.data])

  // ===== 编辑 modal 字段 =====
  // 2026-10-18:parent_id 改 TreeSelect(从已有部门树里选,排除自己 + 后代防环)
  //           leader_id 改 Select(从已加载用户列表里选,按 nickname 显示)
  const editFields: EditField[] = [
    { name: 'name', label: t('field.departmentName'), required: true, rules: [{ required: true, min: 2, max: 64 }] },
    {
      name: 'parent_id',
      label: t('page.dept.parentDept'),
      // 根部门 parent_id = "0"(自身就是根);后端是 uint64 + `,string`,
      // 所以必须发字符串 —— 发裸 0 会被 json 的 ,string 选项直接拒收
      initialValue: editing.data?.parent_id ?? editing.parentId ?? '0',
      type: 'custom',
      /**
       * ⚠️ **'0' 必须在这里映射成 undefined** ——
       *   表单里存的是后端哨兵 '0'(= 顶级),但树里**没有** '0' 这个选项 ⇒
       *   antd 把它当成不存在的选项**原样渲染成 "0"**(用户 2026-10-30 报:
       *   "父部门为空时也显示 0")。
       *   映射成 undefined 之后,控件显示占位符
       *   ("选择父部门(不选或选根 = 顶级部门)"),语义也对。
       *
       * ⚠️ **映射不能写在 render 里的 `value={...}` 上** ——
       *   rc-field-form 的 getControlled 是 `control = {...childProps, ...valueProps}`,
       *   表单里的值会**盖掉**子组件自己写的 value ⇒ 写在 render 里是死代码。
       */
      getValueProps: (v) => ({ value: v && v !== '0' ? v : undefined }),
      render: ({ value, onChange }) => (
        <TreeSelect
          style={{ width: '100%' }}
          treeData={parentTreeOptions}
          // ⚠️ 这个 value 是**表单里的原始值**,真正生效的是上面 getValueProps
          //   映射后的值(rc-field-form 会盖掉这里的 value prop)。
          value={value}
          onChange={(v) => onChange(v ?? '0')}
          placeholder={t('common.placeholder.parentDept')}
          allowClear
          treeDefaultExpandAll
        />
      ),
    },
    { name: 'sort', label: t('field.sort'), type: 'number' },
    {
      name: 'leader_id',
      label: t('field.leader'),
      type: 'custom',
      /**
       * ⚠️ 同 parent_id:'0' 是后端的"无负责人"哨兵,用户列表里**没有** '0' 这个
       *   用户 ⇒ antd 会把它**原样渲染成 "0"**(用户 2026-10-30 报:
       *   "负责人为空时显示数值 0")。
       * ⚠️ 不能用 `value || undefined` —— "0" 是**非空字符串**,在 JS 里 truthy,
       *   那样 placeholder 永远不会出现。必须显式比 '0'。
       */
      getValueProps: (v) => ({ value: v && v !== '0' ? v : undefined }),
      render: ({ value, onChange }) => (
        <Select
          style={{ width: '100%' }}
          showSearch
          // ⚠️ 同上:真正生效的是 getValueProps 映射后的值
          value={value}
          onChange={(v) => onChange(v ?? '0')}
          placeholder={usersLoading ? t('common.loading') : t('common.placeholder.selectOwner')}
          allowClear
          loading={usersLoading}
          notFoundContent={usersLoading ? <Spin size="small" /> : t('common.noUsers')}
          optionFilterProp="label"
          options={userOptions}
        />
      ),
    },
    { name: 'is_active', label: t('common.enable'), type: 'switch' },
  ]

  const viewFields: ViewField<Department>[] = [
    { name: 'name', label: t('field.departmentName') },
    { name: 'path', label: t('field.departmentPath') },
    { name: 'sort', label: t('field.sort') },
    { name: 'leader_name', label: t('field.leader') },
    { name: 'member_count', label: t('field.memberCount') },
    { name: 'is_active', label: t('common.status'), render: (v: boolean) => (v ? t('dict.status.active') : t('dict.status.disabled')) },
  ]

  return (
    <div className='department-container' style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <PageHeader
        description={t('page.dept.description')}
      />

      <Row gutter={16} style={{ flex: 1, minHeight: 0 }}>
        {/* ===== 左侧:组织树 ===== */}
        {/* 2026-10-18:Col 加 minWidth:0,Row 默认 flex 行为会让子 Col 保持内容宽度,撑出横向滚动条 */}
        <Col span={9} style={{ height: '100%', minWidth: 0 }}>
          <Card
            size="small"
            style={{ height: 'calc(100% - 8px)', padding: '14px 16px' }}
            styles={{
              // 2026-10-25 排版对齐(用户指定 14px 16px):
              //   · 头高度交给 antd(size="small" → headerHeightSM=44,app 主题里配的),
              //     原来写死的 40 比 app 其它 small Card 矮 4px。
              //   · body 内边距 14px 16px —— 跟 app 自己的 SearchSection(14px 16px)、
              //     参考原型的 .table-tools(14px 16px)同刻度;antd 的 bodyPaddingSM=12
              //     对这个双栏页面偏紧(右栏塞的是带边框的 Descriptions/List)。
              //     原来左右两块还写成了两个不同的值(左 12 / 右 16),左右对不上。
              //   · body 的 calc 基准跟着 header 高度一起改(40 → 44)。
              //
              // 2026-10-26(用户手动改):**内边距从 body 挪到 Card 根上**
              //   (style 里的 padding: '14px 16px'),header 的 padding 归零。
              //   · 横向那 16px 以前是 header 自己给的('0 16px'),现在改由 Card 根统一给
              //     —— 两边都给就变成 32px,标题会比正文多缩进一截。所以 header 必须归零。
              //   · 效果:卡片边框跟内容之间留出 14/16 白边(卡片"浮"在页面上),
              //     头部那条分隔线也跟着内缩,不再顶到卡片边框。
              //   · ⚠️ body 仍有自己的 14/16 → body 内容距卡片边框 32px,比头部标题
              //     (16px)多缩进一档。这是当前的样子;要动 body 的内边距,两处一起看。
              header: { padding: '0' },
              body: { padding: '14px 16px', height: 'calc(100% - 44px)', overflow: 'auto' },
            }}
            title={
              <Space size={6}>
                <ApartmentOutlined style={{ color: 'var(--app-primary)' }} />
                {/* 2026-10-25:去掉 fontSize:14 —— Card 头文字本来就是 13
                    (headerFontSizeSM = token.fontSize),写死 14 反而比全站大一号。 */}
                <Text strong>{t('page.dept.orgTree')}</Text>
                <Text type="secondary" style={{ fontSize: 12 }}>({flatMap.size})</Text>
              </Space>
            }
            extra={
              <AuthButton
                size="small"
                type="primary"
                icon={<PlusOutlined />}
                perm={PermButton.DEPT_ADD}
                onClick={onAddRoot}
              >
                {t('page.dept.addRoot')}
              </AuthButton>
            }
          >
            <Input.Search
              placeholder={t('common.placeholder.searchDept')}
              allowClear
              size="small"
              style={{ marginBottom: 8 }}
              onChange={(e) => setSearchKw(e.target.value)}
            />
            {loading ? (
              <Skeleton active paragraph={{ rows: 6 }} />
            ) : treeData.length === 0 ? (
              <Empty description={t('page.dept.empty')} image={Empty.PRESENTED_IMAGE_SIMPLE} />
            ) : (
              <Tree
                style={{ width: '100%', minWidth: 0 }}
                treeData={treeData as DataNode[]}
                expandedKeys={
                  searchKw
                    ? // 搜索时强制展开所有可见节点,确保匹配项的祖先链都展开
                      treeData.flatMap((n) => collectAllKeys(n.raw))
                    : expandedKeys
                }
                onExpand={(keys) => {
                  if (!searchKw) setExpandedKeys(keys.map(String))
                }}
                showLine={{ showLeafIcon: false }}
                showIcon={false}
                // 2026-10-18:不设 blockNode,否则 switcher 图标点击事件被整行 layer 吞掉
                //   普通 Tree 没有 treeExpandAction(DirectoryTree 才有),改用受控 expandedKeys
                selectedKeys={selectedId ? [selectedId] : []}
                onSelect={(keys) => {
                  if (keys.length > 0) setSelectedId(String(keys[0]))
                }}
              />
            )}
          </Card>
        </Col>

        {/* ===== 右侧:详情面板 ===== */}
        <Col span={15} style={{ height: '100%', minWidth: 0 }}>
          <Card
            size="small"
            style={{ height: 'calc(100% - 8px)', padding: '14px 16px' }}
            styles={{
              // 2026-10-25:与左栏逐项一致(14px 16px / 头 44)——
              //   不要再各写各的,左 12 右 16 的不一致就是这么来的。
              // 2026-10-26:同样把内边距挪到 Card 根、header padding 归零,
              //   跟左栏保持一致(理由见左栏那段注释)。
              header: { padding: '0' },
              body: { padding: '14px 16px', height: 'calc(100% - 44px)', overflow: 'auto' },
            }}
            title={
              <Space size={6}>
                <Text strong>{t('resource.dept_read')}</Text>
                {selected && <Tag color="blue" style={{ margin: 0 }}>{selected.code}</Tag>}
                {selected && (
                  selected.is_active
                    ? <Tag color="green" style={{ margin: 0 }}>{t('dict.status.active')}</Tag>
                    : <Tag style={{ margin: 0 }}>{t('dict.status.disabled')}</Tag>
                )}
              </Space>
            }
            extra={
              selected ? (
                <Space size={4}>
                  <AuthButton
                    size="small"
                    icon={<PlusOutlined />}
                    perm={PermButton.DEPT_ADD}
                    onClick={onAddChild}
                  >
                    {t('page.dept.addChild')}
                  </AuthButton>
                  <AuthButton
                    size="small"
                    icon={<EyeOutlined />}
                    onClick={() => onView(selected)}
                  >
                    {t('common.detail')}
                  </AuthButton>
                  <AuthButton
                    size="small"
                    icon={<EditOutlined />}
                    perm={PermButton.DEPT_EDIT}
                    onClick={() => onEdit(selected)}
                  >
                    {t('common.edit')}
                  </AuthButton>
                  <AuthButton
                    size="small"
                    danger
                    icon={<DeleteOutlined />}
                    perm={PermButton.DEPT_DELETE}
                    disabled={(selected.member_count ?? 0) > 0}
                    onClick={() => onDelete(selected)}
                    title={(selected.member_count ?? 0) > 0 ? t('page.dept.deleteBlocked') : ''}
                  >
                    {t('common.delete')}
                  </AuthButton>
                </Space>
              ) : null
            }
          >
            {!selected ? (
              <Empty description={t('page.dept.searchHint')} />
            ) : (
              <Space direction="vertical" size={16} style={{ width: '100%' }}>
                {/* 基本信息 — 跟其他页详情一致:Descriptions 2 列 bordered */}
                <Descriptions
                  size="small"
                  column={2}
                  bordered
                  items={[
                    { key: 'name', label: t('field.departmentName'), children: selected.name },
                    { key: 'code', label: t('field.departmentCode'), children: <code style={{ fontSize: 12 }}>{selected.code}</code> },
                    { key: 'sort', label: t('field.sort'), children: selected.sort },
                    { key: 'path', label: t('field.departmentPath'), children: <code style={{ fontSize: 12 }}>{selected.path || '-'}</code> },
                    { key: 'leader', label: t('field.leader'), children: selected.leader_name || <Text type="secondary">-</Text> },
                    { key: 'members', label: t('field.memberCount'), children: <Text>{selected.member_count ?? 0}</Text> },
                  ]}
                />

                {/* 子部门列表 */}
                <div>
                  <Space size={6} style={{ marginBottom: 8 }}>
                    {/* 2026-10-25:去掉 fontSize:14 —— 分区标题跟 Card 头标题同一刻度(13) */}
                    <Text strong>{t('page.dept.subDepartments')}</Text>
                    <Tag style={{ margin: 0 }}>{children.length}</Tag>
                  </Space>
                  {children.length === 0 ? (
                    <Empty description={t('page.dept.noSubDepartments')} image={Empty.PRESENTED_IMAGE_SIMPLE} />
                  ) : (
                    <List
                      size="small"
                      bordered
                      dataSource={children}
                      renderItem={(c) => (
                        <List.Item
                          actions={[
                            <Button
                              key="view"
                              type="link"
                              size="small"
                              icon={<EyeOutlined />}
                              onClick={() => onView(c)}
                            >
                              {t('common.detail')}
                            </Button>,
                            <Button
                              key="edit"
                              type="link"
                              size="small"
                              icon={<EditOutlined />}
                              onClick={() => onEdit(c)}
                            >
                              {t('common.edit')}
                            </Button>,
                            <Button
                              key="delete"
                              type="link"
                              size="small"
                              danger
                              icon={<DeleteOutlined />}
                              disabled={(c.member_count ?? 0) > 0}
                              onClick={() => onDelete(c)}
                            >
                              {t('common.delete')}
                            </Button>,
                          ]}
                        >
                          <Space size={8}>
                            <ApartmentOutlined style={{ color: 'var(--app-primary)' }} />
                            {/* 2026-10-25:去掉 fontSize:14 —— 列表项名称跟左边树节点名、
                                右边 Descriptions 正文统一到 13;code 的字号从 11 提到 12
                                (全站次级文字惯例:color #999 + fontSize 12)。 */}
                            <Text>{c.name}</Text>
                            <code style={{ fontSize: 12, color: 'var(--app-text-tertiary)' }}>{c.code}</code>
                            <Text type="secondary" style={{ fontSize: 12 }}>{t('field.sort')} {c.sort}</Text>
                            {c.is_active
                              ? <Tag color="green" style={{ margin: 0 }}>{t('dict.status.active')}</Tag>
                              : <Tag style={{ margin: 0 }}>{t('dict.status.disabled')}</Tag>}
                            <Text type="secondary" style={{ fontSize: 12 }}>
                              {t('page.dept.memberUnit', undefined, { n: c.member_count ?? 0 })}
                            </Text>
                          </Space>
                        </List.Item>
                      )}
                    />
                  )}
                </div>
              </Space>
            )}
          </Card>
        </Col>
      </Row>

      {/* ===== Modals ===== */}
      <EditModal<Department>
        title={editing.data ? t('common.edit') : t('common.add')}
        open={editing.open}
        initialValues={
          (editing.data
            ?? { parent_id: editing.parentId ?? '0', sort: 0, is_active: true }) as Partial<Department>
        }
        fields={editFields}
        onCancel={() => setEditing({ open: false })}
        onSubmit={async (vals) => {
          if (editing.data) {
            await departmentApi.update(String(editing.data.id), vals)
          } else {
            await departmentApi.create(vals)
          }
          message.success(t('common.success'))
          setEditing({ open: false })
          await loadTree()
        }}
      />

      <ViewDrawer<Department>
        title={t('resource.dept_read')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
      />

      <ConfirmDialog
        open={!!deleting}
        danger
        title={`${t('resource.dept_delete')} ${deleting?.name}?`}
        description={t('page.dept.deleteTip')}
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          await departmentApi.remove(String(deleting.id))
          message.success(t('common.success'))
          setDeleting(null)
          // 删完后,如果删的就是当前选中,清空选择
          if (selectedId === deleting.id) setSelectedId(undefined)
          await loadTree()
        }}
      />
    </div>
  )
}

export default DepartmentList
