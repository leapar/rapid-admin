/**
 * 字典管理 —— 列表
 *
 * Phase 5 M14 业务页批量 - 字典
 *
 * 改动 (2026-10-17):
 *   - 重构为 ManagementTable 模板:Card 包裹 + inlineToolBar + Card body 接管滚动
 *   - 字典分组 Select + 搜索 + 刷新 + 新增 全部放到 inlineToolBar
 *   - 文案走 i18n key (page.dict.*)
 *   - groupCode 切换 / 关键字 / 刷新均由 page state 接管
 *
 * 改动 (2026-10-18):
 *   - 删除前端硬编码 GROUP_OPTIONS(biz_type/decoration/house_type/identity_type 都不存在, 列表永远空)
 *   - 改成 useEffect 拉 dictApi.groups() 动态获取
 *   - 默认选中第一个有数据的 group
 *   - groups() 返回 DictGroup[] { code, label }, Select 显示中文 label, value 存 code
 *
 * 改动 (2026-10-18 字典只读):
 *   - 删除 inlineToolBar 的"新增"按钮(后端 POST /dicts 路由已卸)
 *   - 删除 RowActions 的"删除"按钮(后端 DELETE /dicts/:id 路由已卸)
 *   - 删除 ConfirmDialog(不再需要删除确认)
 *   - EditModal 改为"只编辑"模式(从已有 row 编辑, 不支持新增)
 *   - 字典管理页变成"代码 enum 的中央显示 + 微调面板", 只能改 label/sort/is_active/description
 *   - groupCode / itemKey / itemValue 全部 disabled(代码契约, 不可改)
 *   - 字典管理规范: docs/数据字典管理规范.md
 */
import type React from 'react'
import { useState, useEffect, useMemo, useCallback } from 'react'
import { Button, Tag, App, Select, Alert } from 'antd'
import { ReloadOutlined, InfoCircleOutlined } from '@ant-design/icons'
import { ManagementTable } from '@/components/common/ManagementTable'
import type { EditField } from '@/components/common/EditModal'
import { EditModal } from '@/components/common/EditModal'
import type { ViewField } from '@/components/common/ViewDrawer'
import { ViewDrawer } from '@/components/common/ViewDrawer'
import type { RowAction } from '@/components/common/RowActions'
import { RowActions } from '@/components/common/RowActions'
import { useI18n } from '@/locales'
import type { DictItem, DictGroup } from '@/api/config'
import { dictApi } from '@/api/config'
import { toWireIsActive, toFormIsActive } from './isActive'
import { PermButton } from '@/constants/permissions'
import { useDictStore } from '@/stores/dict'
import { subscribe, EVENT } from '@/hooks/useEventStream'

/**
 * 字典编辑表单的值类型 —— 与 `DictItem` 的差别只有 `is_active`:
 *
 *   · 表单里的 Select 用 **1 / 0**(antd Select 的 option value 只收 `string | number`,
 *     塞 boolean 进不去 —— 见 `EditModal` 的 `EditFieldOption`);
 *   · wire 上是 **boolean**(后端 Go bool)。
 *
 * ⚠️ 所以这里**显式声明**表单值的类型,而不是靠 `as any` 兜 ——
 *   以前 `DictItem.is_active` 声明成 number,页面只能写 `v === 1 || v === true`
 *   同时兼容两种,一旦后端改了 schema 就静默走错分支(2026-10-18 踩过:全表显示"禁用")。
 *
 * ⚠️ 两个方向的转换函数在 `./isActive`(单独一个文件,页面组件文件里放非组件导出会
 *   触发 `react-refresh/only-export-components` 警告,而本仓 lint 是 `--max-warnings 0`)。
 */
type DictFormValues = Omit<DictItem, 'is_active'> & {
  /** 表单层:1 = 启用,0 = 禁用(提交时转成 boolean) */
  is_active: number
  /** 临时字段:'颜色'下拉的选中值(提交时拼回 description,不直接发给后端) */
  color?: string
}

const DictList: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()

  // 字典分组:从后端动态拉(2026-10-18 替换硬编码 GROUP_OPTIONS)
  const [groups, setGroups] = useState<DictGroup[]>([])
  const [groupCode, setGroupCode] = useState<string>('')
  const [items, setItems] = useState<DictItem[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')

  const [editing, setEditing] = useState<DictItem | null>(null)
  const [viewing, setViewing] = useState<DictItem | null>(null)

  // 拉取所有有数据的 group
  // 2026-10-18:不再默认选第一个,默认 groupCode='' 查全部,用户在 Select 里选具体分组才过滤
  useEffect(() => {
    dictApi.groups()
      .then((arr) => {
        setGroups(Array.isArray(arr) ? arr : [])
      })
      .catch(() => setGroups([]))
  }, [])

  // ⚠️ **useCallback 是为了下面那个 SSE 订阅** —— 订阅回调要在
  //   `dict.changed` 到达时用**当前**的 load。load 不依赖任何渲染期状态
  //   (只用 setItems / setLoading / dictApi),所以用空依赖数组包一层是安全的,
  //   换来的是订阅 effect 不会每次渲染都退订重订。
  const load = useCallback(async (gc: string) => {
    // 2026-10-18:允许空 groupCode 查全部(后端 ListDict 已支持)
    setLoading(true)
    try {
      const list = await dictApi.list({ groupCode: gc })
      setItems(Array.isArray(list) ? list : [])
    } catch {
      setItems([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load(groupCode)
  }, [groupCode, load])

  // 2026-10-26 M94:**别人改了字典,这个列表也要跟着变**。
  //   服务端在字典改动后广播 `dict.changed`(带 group_code),
  //   这里收到就重拉一次列表 —— 否则这个页会一直停在旧数据上,
  //   除非用户手动点刷新。
  //
  //   ⚠️ 订阅的是**模块级** `subscribe`(不是 `useEventStream()`)——
  //     后者会 `consumers += 1` 并持有 SSE 连接的生命周期。
  //     连接的持有者应该是常驻的 BasicLayout,不该由这个页面来决定。
  useEffect(() => {
    return subscribe(EVENT.dictChanged, () => {
      void load(groupCode)
    })
  }, [groupCode, load])

  const filtered = useMemo(() => {
    if (!keyword) return items
    const kw = keyword.toLowerCase()
    return items.filter(
      (i) =>
        i.item_key?.toLowerCase().includes(kw) ||
        i.label?.toLowerCase().includes(kw) ||
        i.item_value?.toLowerCase().includes(kw),
    )
  }, [items, keyword])

  // groupCode -> 中文 label 映射(全表浏览时显示中文分组名更友好)
  const groupLabelMap = useMemo(
    () => Object.fromEntries(groups.map((g) => [g.code, g.label])),
    [groups]
  )

  // 2026-10-18:parse description 拆 '颜色' + '说明'
  //   字段约定 (server/internal/seed/seed.go:848):
  //     'color:<antd-tag-color> # 备注'
  //   字典管理页之前直接渲染 raw,看到 'color:default' 这种机器约定
  //   拆出来让人能看懂,运营编辑时也不容易丢前缀
  //   后续 P1+ 应该把 color 拆成独立列,这里只做 UI 兜底
  const parseDescription = (raw?: string): { color: string; text: string } => {
    if (!raw) return { color: '', text: '' }
    const m = raw.match(/^color:(\S+)(?:\s*#\s*(.*))?$/i)
    if (m && m[1]) return { color: m[1], text: (m[2] ?? '').trim() }
    return { color: '', text: raw }
  }

  // Antd Tag 颜色白名单 — 跟 seed 里实际写过的对齐
  // https://ant.design/components/tag#components-tag-demo-colorful
  // 2026-10-18:从 description 里抽 color 给运营改,白名单防止乱填
  const COLOR_OPTIONS: { value: string; label: string }[] = [
    { value: '', label: 'tagColor.none' },
    { value: 'default', label: 'tagColor.default' },
    { value: 'red', label: 'tagColor.red' },
    { value: 'orange', label: 'tagColor.orange' },
    { value: 'yellow', label: 'tagColor.yellow' },
    { value: 'green', label: 'tagColor.green' },
    { value: 'blue', label: 'tagColor.blue' },
    { value: 'purple', label: 'tagColor.purple' },
    { value: 'magenta', label: 'tagColor.magenta' },
    { value: 'cyan', label: 'tagColor.cyan' },
    { value: 'gold', label: 'tagColor.gold' },
    { value: 'lime', label: 'tagColor.lime' },
    { value: 'volcano', label: 'tagColor.volcano' },
    { value: 'geekblue', label: 'tagColor.geekblue' },
  ]

  // ⚠️ 2026-10-28 M107:`is_active` **类型兜底已删** ——
  //   以前 `DictItem.is_active` 声明成 `number`,而 wire 上是 Go bool 的 true/false,
  //   于是页面只能写 `v === 1 || v === true` 兜底(2026-10-18 因为同一个坑,
  //   全表 113 行都显示成"禁用")。
  //   现在 `api/config.ts` 的 `DictItem.is_active` 已经是 `boolean`,类型对上了,
  //   兜底就是**纯冗余** —— 留着只会让人以为 wire 上还有 1/0。
  //   (表单层的 Select 仍然用 1/0,因为 `EditFieldOption.value` 只收 string | number;
  //    边界处显式转换,不靠"any 兜住"。)

  const columns = [
    // 2026-10-18:加"分组"列 + 后端 Order("group_code ASC, sort ASC, id ASC")
    //   全表浏览时一眼看出属于哪个 group,过滤后也能再确认
    {
      title: t('field.group'),
      dataIndex: 'group_code',
      width: 160,
      render: (v: string) => groupLabelMap[v] || v || '-',
    },
    { title: t('field.dictKey'), dataIndex: 'item_key', width: 160, fixed: 'left' as const },
    { title: t('field.dictValue'), dataIndex: 'item_value', width: 200, ellipsis: true },
    { title: t('field.displayName'), dataIndex: 'label', width: 160 },
    { title: t('field.sort'), dataIndex: 'sort', width: 80 },
    {
      title: t('common.status'),
      dataIndex: 'is_active',
      width: 100,
      render: (v: boolean) => (v ? <Tag color="green">{t('dict.status.active')}</Tag> : <Tag>{t('dict.status.disabled')}</Tag>),
    },
    // 2026-10-18:从 description 拆出'颜色'和'说明'两列
    {
      title: t('field.color'),
      dataIndex: 'description',
      width: 100,
      render: (v: string) => {
        const { color } = parseDescription(v)
        return color ? <Tag color={color}>{color}</Tag> : <span style={{ color: 'var(--app-text-tertiary)' }}>—</span>
      },
    },
    {
      title: t('field.dictDescription'),
      dataIndex: 'description',
      width: 240,
      ellipsis: true,
      render: (v: string) => parseDescription(v).text || <span style={{ color: 'var(--app-text-tertiary)' }}>—</span>,
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 200,
      fixed: 'right' as const,
      render: (_: unknown, row: DictItem) => {
        // 字典只读, 操作列只保留"详情"和"编辑"(不能新增,不能删除)
        const actions: RowAction<DictItem>[] = [
          { key: 'view', label: t('common.detail'), onClick: setViewing },
          { key: 'edit', label: t('common.edit'), perm: PermButton.DICT_EDIT, onClick: setEditing },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  // 2026-10-18 字典只读: 编辑模式只允许改 label/sort/is_active/description
  // group_code / item_key / item_value 是代码契约, disabled 不可改
  // 2026-10-18:field.name 必须用 snake_case,匹配 DictItem(commit f91b5a3 后全量 snake_case)
  //   之前写成 camelCase 导致 view/edit 全部字段显示空
  // 2026-10-18:description 拆 'color:xxx # 备注' 协议,EditModal 拆成 '颜色' + '说明' 两字段:
  //   - 颜色:下拉选择 antd Tag 颜色(白名单)
  //   - 说明:textarea 只装纯文字部分,不再让用户手写 'color:xxx #'
  //   提交时按选中 color 自动拼回 'color:xxx # 文字',无 color 段时存纯文字
  const editFields: EditField[] = [
    { name: 'group_code', label: t('field.groupCode'), disabled: true },
    { name: 'item_key', label: t('field.dictKey'), disabled: true },
    { name: 'item_value', label: t('field.dictValue'), disabled: true },
    { name: 'label', label: t('field.displayName'), required: true, rules: [{ required: true, message: t('page.dict.displayNameRequired') }] },
    { name: 'sort', label: t('field.sort'), type: 'number' },
    {
      name: 'is_active',
      label: t('common.status'),
      type: 'select',
      options: [
        { value: 1, label: t('dict.status.active') },
        { value: 0, label: t('dict.status.disabled') },
      ],
    },
    {
      name: 'color',
      label: t('field.color'),
      type: 'select',
      options: COLOR_OPTIONS.map((o) => ({ ...o, label: t(o.label) })),
      // 给个提示让用户明白选择会展示为 Tag
    },
    { name: 'description', label: t('field.dictDescription'), type: 'textarea', placeholder: t('page.dict.descriptionPlaceholder') },
  ]

  // 2026-10-18:viewFields 同步改 snake_case + description 解析后展示
  //   - 颜色:用 Tag 渲染 color 字符串
  //   - 说明:展示 # 后的纯文字(空时显示 —)
  const viewFields: ViewField<DictItem>[] = [
    { name: 'group_code', label: t('field.groupCode'), render: (v: string) => groupLabelMap[v] || v || '-' },
    { name: 'item_key', label: t('field.dictKey') },
    { name: 'item_value', label: t('field.dictValue') },
    { name: 'label', label: t('field.displayName') },
    { name: 'sort', label: t('field.sort') },
    {
      name: 'is_active',
      label: t('common.status'),
      render: (v: boolean) => (v ? <Tag color="green">{t('dict.status.active')}</Tag> : <Tag>{t('dict.status.disabled')}</Tag>),
    },
    {
      name: 'description',
      label: t('field.color'),
      render: (v: string) => {
        const { color } = parseDescription(v)
        return color ? <Tag color={color}>{color}</Tag> : <span style={{ color: 'var(--app-text-tertiary)' }}>—</span>
      },
    },
    {
      name: 'description',
      label: t('field.dictDescription'),
      render: (v: string) => parseDescription(v).text || <span style={{ color: 'var(--app-text-tertiary)' }}>—</span>,
    },
  ]

  return (
    <div style={{ height: '100%', display:"flex", flexDirection:"column" }}>

      <ManagementTable<DictItem>
        pageDescription={t('page.dict.description')}
        headerExtra={
          // 2026-10-18 字典只读: 顶部 Alert 提示用户正确的加字典路径
          // 2026-10-25: 移到页头**下面**。原来这条 Alert 挂在 <ManagementTable>
          //   外面,而页头是 ManagementTable 内部渲染的 —— 所以它实际落在
          //   页头描述**上面**。走 headerExtra 槽位才是"页头下面"。
          // ⚠️ 内容是**字典页专属**的,不能写死进 ManagementTable(那 17 个页面共用)。
          <Alert
            type="info"
            showIcon
            icon={<InfoCircleOutlined />}
            message={t('page.dict.repoNotice')}
            description={
              // ⚠️ 这段是**一句富文本**被 <code> 打断后的碎片。
              //   本项目的 i18n 只做纯字符串占位替换,插不进标签,
              //   所以只能把句子按 <code> 的位置拆成几段,渲染时按原顺序拼回去。
              //   中文侧的值跟原句逐字一致(含标点前后的空格),
              //   英文侧每个片段**首尾都带空格** —— 否则拼起来会粘词。
              // ⚠️ `docs/数据字典管理规范.md` 是**仓库里的真实文件路径**,
              //   不是文案 —— 译了它引用就指不到文件了,所以保持写死。
              <span>
                {t('page.dict.repoDesc1')}
                <code>server/internal/seed/seed.go</code>
                {t('page.dict.repoDesc2')}
                <code>mustDicts</code> + <code>seedAllDicts</code>
                {t('page.dict.repoDesc3')}
                <code>go run ./cmd/migrate</code>
                {t('page.dict.repoDesc4')}
                <code>is_active=false</code>
                {t('page.dict.repoDesc5')}
                <code>docs/数据字典管理规范.md</code>
              </span>
            }
            style={{ marginBottom: 16 }}
            closable
          />
        }
        searchPlaceholder={t('common.placeholder.searchDict')}
        onSearch={(kw) => setKeyword(kw)}
        onReset={() => {
          setKeyword('')
          const first = groups[0]
          if (first) setGroupCode(first.code)
        }}
        searchExtra={[
          // 2026-10-18:DB 有 30 个字典 group,Antd Select 默认 listHeight=256 只显示 ~7 项,
          // 用户看不到所有选项。提到 400 让 ~12 项可见,剩下滚动即可
          // 2026-10-18 二次改进:加"全部"选项(value='' 查全部),用户能浏览全表,
          //   而不是强制先选一个分组才能看
          <Select
            key="group"
            placeholder={t('common.placeholder.filterDictGroup')}
            value={groupCode || undefined}
            onChange={setGroupCode}
            allowClear
            style={{ width: 240 }}
            showSearch
            listHeight={400}
            options={[
              { value: '', label: t('common.all') },
              ...groups.map((g) => ({ value: g.code, label: g.label })),
            ]}
            loading={groups.length === 0}
            notFoundContent={groups.length === 0 ? t('common.loading') : t('common.noData')}
            optionFilterProp="label"
          />,
        ]}
        inlineToolBar={[
          // 2026-10-18 字典只读: 工具栏只剩"刷新"按钮, 没有"新增"
          <Button
            key="reload"
            icon={<ReloadOutlined />}
            onClick={() => load(groupCode)}
          >
            {t('common.refresh')}
          </Button>,
        ]}
        columns={columns}
        tableProps={{
          loading,
          dataSource: filtered,
          rowKey: 'id',
          pagination: { pageSize: 20, showTotal: (n) => t('common.total', undefined, { n }) },
        }}
        scrollX={1200}
      />

      {/* 2026-10-18 字典只读: EditModal 只支持编辑, 没有"新增"入口 */}
      {/* 2026-10-18:description 字段 'color:xxx # 备注' 协议 — 进入编辑时拆成 '颜色' + '说明' 两字段,
        颜色下拉选 antd Tag 颜色(白名单),说明 textarea 只装纯文字;
        提交时按用户当前选的 color 自动拼回 'color:xxx # 文字',无 color 段时存纯文字
        只向 PATCH 提交 DictItem 真实字段(label/sort/is_active/description),不把临时 color 字段透传到后端 */}
      {editing && (() => {
        const { color: origColor, text: origText } = parseDescription(editing.description)
        return (
          <EditModal<DictFormValues>
            title={t('page.dict.editTitle')}
            open={!!editing}
            initialValues={{
              group_code: editing.group_code,
              item_key: editing.item_key,
              item_value: editing.item_value,
              label: editing.label,
              sort: editing.sort,
              // 2026-10-28 M107:wire 上是 boolean,表单层的 Select 用 1/0
              //   (`EditFieldOption.value` 只收 string | number)—— 边界处显式转换
              is_active: toFormIsActive(editing.is_active),
              color: origColor || undefined,
              description: origText,
            }}
            fields={editFields}
            onCancel={() => setEditing(null)}
            onSubmit={async (vals) => {
              if (!editing) return
              // ⚠️ **先把这行的 group_code 存下来** —— group_code 是代码契约的一部分,
              //   改它要走别的通道,所以它不会出现在 PATCH 的 payload 里。
              const rowGroup = editing.group_code
              // 重组 description:按用户选的 color 拼回 'color:xxx # 文字'
              const newText = (vals.description ?? '').trim()
              const newColor = (vals.color ?? '').trim()
              let finalDesc = newText
              if (newColor) {
                finalDesc = newText ? `color:${newColor} # ${newText}` : `color:${newColor}`
              }
              // 严格只 PATCH DictItem 真实字段,不把临时 color 透传到后端
              // 2026-10-28 M107:select 返的 1/0 归一回 server 期望的 boolean
              //   (以前靠 `as any` + `v === 1 || v === true` 兜底,类型已经对上了,不再需要 any)
              const payload: Partial<DictItem> = {
                label: vals.label,
                sort: vals.sort,
                is_active: toWireIsActive(vals.is_active),
                description: finalDesc,
              }
              await dictApi.update(String(editing.id), payload)
              message.success(t('common.success'))
              // 2026-10-26 M94:**作废这一组的本地缓存** ——
              //   否则别的页面(下拉 / 标签)最长 30 分钟看不到新值。
              //   ⚠️ **用的是这一行自己的 group_code**(rowGroup),
              //     **不是页面的 groupCode** —— 页面的 groupCode 可能是 ''
              //     (表示"查全部"),那不是任何一个字典组的 code,拿它去作废
              //     等于作废了一个不存在的组,真正的组反而不会刷新。
              useDictStore.getState().invalidate(rowGroup)
              setEditing(null)
              load(groupCode)
            }}
          />
        )
      })()}

      <ViewDrawer<DictItem>
        title={t('page.dict.detailTitle')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
      />
    </div>
  )
}

export default DictList
