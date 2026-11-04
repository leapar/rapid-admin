/**
 * 我的审批 —— 列表(M87)
 *
 * 文档:docs/通用审批系统设计方案V1.0.md §8.2 / §9.1
 *
 * ⚠️ **一个页面三个盒子**(§9.1),不是三个路由:
 *   待我审批(todo)/ 我发起的(mine)/ 我已办理(done)。
 *   ⚠️ **没有"全部"** —— §8.2 的硬约束,少传一个参数不该等于"给我看全部"。
 *   加一个 `box=all` 进来,广发的 `approval:read` 立刻变成越权(§12 第 9 条)。
 *
 * ⚠️ **2026-10-26:三个 Tab → 搜索栏里的一个必选 Select**
 *   换形态的理由、**以及被撤回的那条论据**,见
 *   docs/通用审批系统设计方案V1.0.md §13 的 **M90** 条目。
 *   撤回的那条:"清空就 400" —— **不成立**。`allowClear={false}` + 默认值
 *   就足以让"空"在 UI 上不可能出现,必选 Select 和 Tab 在契约上**等价**。
 *   真正换来的是三件事:
 *     ① **全仓一致** —— 其它列表页的筛选器都在 `searchExtra` 里,
 *        **全部是 `Select`**:issue / notice / material / audit / file / dict /
 *        job / permission / flow(10 个页面,零例外)。
 *        ⚠️ permission 那个"全部/菜单/按钮/API"原先是 `Segmented`、flow 那个
 *        原先是一个可点的 `Tag` —— **同一天(2026-10-26)都改成了 `Select`**
 *        (commit 92c4c14 / 894b10b)。审批页用 Tab 是当时**唯一的例外**;
 *     ② **省一行垂直空间** —— Tab 那行是 Card(size=small) + Tabs 内边距
 *        ≈ 60px,而表格是 auto-height,那 60px 全是数据行;
 *     ③ **顺手修掉一个真 bug** —— 见下面 `query.box` 那段。
 *
 * ⚠️ **选框的值必须受控于 `query.box`,不能另开一个 useState**
 *   以前 Tab 的 `activeKey` 是独立的 state,而 `useTable.reset()` 只改
 *   `query` 不改那个 state → **切到"我发起的"再点"重置",表格刷的是
 *   `box=todo` 的数据,Tab 却还高亮着"我发起的"** —— 界面和数据对不上。
 *   `useTable.reset()` = `{page:1, size:20, ...initialQuery}`,而
 *   `initialQuery` 里带 `box:'todo'` —— 所以"重置"本来就是 box-aware 的,
 *   受控于 `query.box` 就不会有第二种状态。
 *
 * ⚠️ **按钮可见性由数据决定,不由 RBAC 决定**(§8.1)——
 *   有 `approval:read` 不等于该你审。能不能审看 `my_task_status`。
 */
import React from 'react'
import { Tag, Select } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ManagementTable } from '@/components/common/ManagementTable'
import { RowActions, RowAction } from '@/components/common/RowActions'
import { useTable } from '@/hooks/useTable'
import { useI18n } from '@/locales'
import { approvalApi } from '@/api/approval'
import type { ApprovalBox, ApprovalListItem, ApprovalListQuery, ApprovalStatus } from '@/types/approval'

/**
 * 状态标签 —— **text 存 i18n key,不是已翻译文案**
 *
 * ⚠️ 这几个 const 在模块顶层,拿不到 hook 里的 t —— 所以只存 key,
 *   渲染时再 t()。约定跟 constants/dict.ts 一样。
 */
const STATUS_TAG: Record<ApprovalStatus, { color: string; text: string }> = {
  draft: { color: 'default', text: 'approval.status.draft' },
  running: { color: 'processing', text: 'approval.status.running' },
  returned: { color: 'warning', text: 'approval.status.returned' },
  approved: { color: 'success', text: 'approval.status.approved' },
  rejected: { color: 'error', text: 'approval.status.rejected' },
  cancelled: { color: 'default', text: 'approval.status.cancelled' },
}

/** 三个盒子的标签 —— 存 i18n key,渲染时 t() */
const BOX_LABEL: Record<ApprovalBox, string> = {
  todo: 'approval.box.todo',
  mine: 'approval.box.mine',
  done: 'approval.box.done',
}

/**
 * 三个盒子 —— **只有这三个,没有"全部"**(§8.2)
 *
 * ⚠️ 这份列表是**安全边界的一部分**:后端 `box` 必填、空 / 非法 box 直接报错
 *   ("退化 = 前端漏传一个参数 = 全平台审批单裸奔")。往里加一个 `all`,
 *   等于把"我能看到什么"从"我和这条单的关系"改成"我有什么权限码"。
 */
const BOXES: ApprovalBox[] = ['todo', 'mine', 'done']

const ApprovalList: React.FC = () => {
  const { t } = useI18n()
  const navigate = useNavigate()

  const { tableProps, search, reset, setQuery, query } = useTable<ApprovalListQuery, ApprovalListItem>({
    fetch: (q) => approvalApi.list(q),
    initialQuery: { box: 'todo' },
  })

  const columns = [
    {
      title: t('field.title'),
      dataIndex: 'title',
      width: 240,
      fixed: 'left' as const,
      ellipsis: true,
    },
    {
      title: t('field.bizType'),
      dataIndex: 'biz_type',
      width: 160,
      render: (v: string) => <Tag>{v}</Tag>,
    },
    {
      title: t('field.currentStage'),
      dataIndex: 'current_stage_name',
      width: 180,
      render: (v: string) => v || '—',
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      width: 130,
      render: (v: ApprovalStatus) => {
        const s = STATUS_TAG[v]
        return s ? <Tag color={s.color}>{t(s.text)}</Tag> : <Tag>{v}</Tag>
      },
    },
    { title: t('field.initiator'), dataIndex: 'applicant_name', width: 120 },
    {
      title: t('field.rolledBack'),
      dataIndex: 'return_count',
      width: 90,
      align: 'center' as const,
      render: (v: number) => (v > 0 ? <Tag color="orange">{t('page.approvalList.returnCount', undefined, { n: v })}</Tag> : '—'),
    },
    {
      title: t('field.submitTime'),
      dataIndex: 'submitted_at',
      width: 170,
      render: (v: string | null) => (v ? v.slice(0, 16).replace('T', ' ') : '—'),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 120,
      fixed: 'right' as const,
      render: (_: any, row: ApprovalListItem) => {
        const actions: RowAction<ApprovalListItem>[] = [
          {
            key: 'detail',
            label: t('common.detail'),
            onClick: (r) => navigate(`/approval/${r.id}`),
          },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<ApprovalListItem>
        pageDescription={t('page.approvalList.description')}
        searchPlaceholder={t('common.placeholder.searchApproval')}
        onSearch={(kw) => search({ keyword: kw })}
        onReset={reset}
        searchExtra={[
          <Select<ApprovalBox>
            key="box"
            style={{ width: 160 }}
            // ⚠️ **必选** —— `allowClear={false}` + 默认 `todo`,
            //   让"空"在 UI 上不可能出现(后端 `box` 必填,空 box 直接 400)。
            //   **不给"全部"**(§8.2)—— 选项只有下面这三个。
            allowClear={false}
            value={query.box}
            onChange={(next) => {
              // ⚠️ **切盒子 = 改查询条件** —— box 是查询的一部分
              setQuery((prev) => ({ ...prev, box: next, page: 1 }))
            }}
            options={BOXES.map((b) => ({ value: b, label: t(BOX_LABEL[b]) }))}
          />,
        ]}
        columns={columns}
        tableProps={tableProps}
        scrollX={1200}
      />
    </div>
  )
}

export default ApprovalList
