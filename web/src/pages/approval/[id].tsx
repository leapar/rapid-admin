/**
 * 审批详情 —— 含轮次切换(M87)
 *
 * 文档:docs/通用审批系统设计方案V1.0.md §3.3 / §3.4 / §6.1 / §9.1
 *
 * ⚠️ **轮次切换器(V2.1)** —— 一排「第1轮 / 第2轮 / 第3轮(当前)」,
 *   `rounds.length > 1` 时才渲染。**只有一轮时不渲染它** ——
 *   否则单轮单子的详情页凭空多一个没用的控件。
 *   · 切到历史轮次 → 标题/事由区**换成那一轮的快照**,并置灰 +
 *     一行"这是第 N 轮的存档,当前生效的是第 M 轮"
 *   · **切轮次只改本地 state,不发请求** —— `rounds[]` 和 `logs[]` 一次已全部返回(§7.5)
 *   · 当前轮永远默认选中,且默认可编辑;历史轮次只读
 *
 * ⚠️ **「重提必须改内容」(§3.3)** —— 打回之后,申请人不改内容直接重提会被服务端拒。
 *   所以 `returned` 态下标题/事由是**行内可编辑表单**(不是弹窗)——
 *   打回重提是主路径,不该藏在弹窗里。
 *
 * ⚠️ **驳回/打回的备注不少于 5 个字**(§3.4)—— 后端会拒。
 *   前端那道只是体验,边界在后端。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  App,
  Button,
  Card,
  Descriptions,
  Empty,
  Input,
  Radio,
  Space,
  Steps,
  Table,
  Tag,
} from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { ArrowLeftOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'
import { approvalApi } from '@/api/approval'
import { refreshApprovalTodo } from '@/hooks/useEventStream'
import { useUserStore } from '@/stores/user'
import type {
  ApprovalAction,
  ApprovalAssigneeView,
  ApprovalDetail,
  ApprovalLogView,
  ApprovalRoundView,
  ApprovalStatus,
} from '@/types/approval'

/**
 * 驳回/打回意见的最少字数(设计稿 §3.4)
 *
 * ⚠️ **后端用 `utf8.RuneCountInString` 数** —— 中文一条意见 15~30 字节,
 *   用 `length` 数会把"5 个字"变成实际只要 2 个字。
 * ⚠️ 前端这里用 `[...str].length`(按码点算)对齐后端的 rune 计数。
 */
const MIN_COMMENT = 5
const countChars = (s: string) => [...s.trim()].length

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

/** 轮次结果 —— 同上,存 i18n key */
const ROUND_OUTCOME: Record<string, { color: string; text: string }> = {
  running: { color: 'processing', text: 'approval.round.running' },
  approved: { color: 'success', text: 'approval.round.approved' },
  rejected: { color: 'error', text: 'approval.round.rejected' },
  rolled_back: { color: 'warning', text: 'approval.round.rolledBack' },
}

/** 意见流水动作 —— 同上,存 i18n key */
const LOG_ACTION: Record<string, { color: string; text: string }> = {
  submit: { color: 'default', text: 'approval.logAction.submit' },
  approve: { color: 'success', text: 'approval.logAction.approve' },
  reject: { color: 'error', text: 'approval.logAction.reject' },
  rollback: { color: 'warning', text: 'approval.logAction.rollback' },
  cancel: { color: 'default', text: 'approval.logAction.cancel' },
}

/**
 * 业务类型 → 业务页路由
 *
 * ⚠️ 接入审批的**业务线**在这里登记 `biz_type → 前端路由`;
 *   没登记的类型,详情页里"业务类型"就不渲染跳转链接(只展示文字)——
 *   给了没接审批的页面一个入口,点进去就是 404。
 *   (框架开源版默认零业务线接入,各业务方自行登记。)
 */
const BIZ_TYPE_ROUTE: Record<string, string> = {}

const ApprovalDetailPage: React.FC = () => {
  const { t } = useI18n()
  const { message, modal } = App.useApp()
  const navigate = useNavigate()
  const { id } = useParams<{ id: string }>()
  const approvalId = id ?? ''

  const [detail, setDetail] = useState<ApprovalDetail | null>(null)
  const [loading, setLoading] = useState(false)

  /** 选中的轮次 —— **只改本地 state,不发请求**(§7.5) */
  const [roundNo, setRoundNo] = useState<number>(0)
  /** 行内编辑的草稿(标题 / 事由) */
  const [draftTitle, setDraftTitle] = useState('')
  const [draftVariables, setDraftVariables] = useState('')
  const [dirty, setDirty] = useState(false)
  /** 审批意见 */
  const [comment, setComment] = useState('')
  /** 正在提交的动作 —— 防止重复点 */
  const [acting, setActing] = useState(false)

  const load = useCallback(async () => {
    if (!approvalId) return
    setLoading(true)
    try {
      const d = await approvalApi.detail(approvalId)
      setDetail(d)
      setDraftTitle(d.title)
      setDraftVariables(d.variables)
      setDirty(false)
      // ⚠️ **当前轮永远默认选中** —— 轮次号最大的那轮
      const last = d.rounds.length > 0 ? d.rounds[d.rounds.length - 1] : undefined
      setRoundNo(last ? last.round_no : 0)
    } catch {
      // 拦截器已经弹过 toast
    } finally {
      setLoading(false)
    }
  }, [approvalId])

  useEffect(() => {
    void load()
  }, [load])

  /**
   * 当前用户 id
   *
   * ⚠️ **用 store selector 而不是 `getState()`** —— `getState()` 是一次性快照,
   *   userInfo 还没 hydrate 的时候读到的是 `undefined`,而且之后不会重渲染纠正。
   *   selector 是响应式的,userInfo 到位后本页会自动重算。
   */
  const myUserId = useUserStore((s) => s.userInfo?.id ?? '')
  const isApplicant = !!detail && detail.applicant_user_id === myUserId
  const rounds = detail?.rounds ?? []
  /** 轮次号最大的那轮 = "当前生效"的那轮 */
  const currentRoundNo = rounds.length > 0 ? (rounds[rounds.length - 1]?.round_no ?? 0) : 0
  const isHistorical = roundNo !== 0 && roundNo !== currentRoundNo
  const selectedRound: ApprovalRoundView | undefined = rounds.find((r) => r.round_no === roundNo)

  /**
   * 能不能改内容
   *
   * ⚠️ **只有「当前轮 + 我是申请人 + 单子还在草稿/打回态」能改** ——
   *   历史轮次是存档,只读;别人发起的单子我改不了。
   */
  const editable = !!detail && isApplicant && !isHistorical &&
    (detail.status === 'draft' || detail.status === 'returned')

  /** 能不能送审/重提 */
  const canSubmit = editable
  /** 能不能作废 —— **running 不给作废**(D9) */
  const canCancel = !!detail && isApplicant &&
    (detail.status === 'draft' || detail.status === 'returned')

  /** 能不能审批 —— 后端算的 `can_act`(它已经算过"是不是本关待办人") */
  const canAct = !!detail?.can_act

  /** 事由(JSON 文本)→ 可读的键值对 */
  const variablePairs = useMemo(() => {
    const raw = isHistorical ? selectedRound?.variables ?? '' : draftVariables
    if (!raw) return [] as Array<{ key: string; value: string }>
    try {
      const obj = JSON.parse(raw) as Record<string, unknown>
      return Object.entries(obj).map(([key, value]) => ({
        key,
        value: typeof value === 'string' ? value : JSON.stringify(value),
      }))
    } catch {
      // ⚠️ **不是 JSON 就原样显示** —— variables 是自由文本,不强求是 JSON
      return [{ key: t('page.approvalDetail.rawContentKey'), value: raw }]
    }
    // ⚠️ `t` 必须在依赖里 —— 它的引用随语言切换而变,不列的话切语言后
    //   这个 memo 不会重算,标签会停在旧语言上(见 locales/useI18n.ts)。
  }, [isHistorical, selectedRound, draftVariables, t])

  /** 意见流水按轮次分组 */
  const logGroups = useMemo(() => {
    const logs = detail?.logs ?? []
    const byRound = new Map<number, ApprovalLogView[]>()
    for (const l of logs) {
      const arr = byRound.get(l.round_no) ?? []
      arr.push(l)
      byRound.set(l.round_no, arr)
    }
    return Array.from(byRound.entries())
      .sort((a, b) => b[0] - a[0])
      .map(([no, items]) => ({
        roundNo: no,
        // ⚠️ **时间倒序**(§9.1)—— 最新的意见在最上面
        items: [...items].sort(
          (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
        ),
      }))
  }, [detail])

  /**
   * 切轮次
   *
   * ⚠️ **改到一半切走要提示"未保存"**(§9.1)—— 静默丢掉用户打的字是体验事故。
   */
  function switchRound(no: number) {
    if (no === roundNo) return
    if (dirty) {
      modal.confirm({
        title: t('page.approvalDetail.dirty'),
        content: t('page.approvalDetail.switchConfirm'),
        okText: t('page.approvalDetail.discardAndSwitch'),
        cancelText: t('page.approvalDetail.continueEditing'),
        onOk: () => doSwitchRound(no),
      })
      return
    }
    doSwitchRound(no)
  }

  function doSwitchRound(no: number) {
    setRoundNo(no)
    const r = rounds.find((x) => x.round_no === no)
    if (r) {
      // ⚠️ **切到历史轮次 → 标题/事由换成那一轮的快照**
      setDraftTitle(r.title)
      setDraftVariables(r.variables)
    }
    setDirty(false)
  }

  /** 送审 / 重提 */
  async function doSubmit() {
    if (!detail) return
    // ⚠️ **重提必须改内容**(§3.3)—— 逐字节比,和后端的守卫一模一样。
    //   后端比的是"实例当前值 vs 本轮快照",这里比的是"表单里的值 vs 本轮快照"。
    //   不在前端拦这一道,用户点了"重提"只会收到一句后端的报错,
    //   而且他不知道自己该改什么。
    const lastRound = rounds.length > 0 ? rounds[rounds.length - 1] : undefined
    if (detail.status === 'returned' && lastRound &&
      draftTitle === lastRound.title && draftVariables === lastRound.variables) {
      message.warning(t('page.approvalDetail.resubmitMustChange'))
      return
    }
    setActing(true)
    try {
      if (dirty) {
        // ⚠️ **先存草稿再送审** —— 否则送审带的是旧内容,
        //   §3.3 的"重提必须改内容"守卫会把这次重提拒掉
        await approvalApi.updateDraft(detail.id, {
          title: draftTitle,
          variables: draftVariables,
        })
        setDirty(false)
      }
      await approvalApi.submit(detail.id)
      message.success(
        detail.status === 'returned'
          ? t('page.approvalDetail.resubmitted')
          : t('page.approvalDetail.submittedForReview'),
      )
      await load()
    } catch {
      // 拦截器弹过 toast
    } finally {
      setActing(false)
    }
  }

  /** 审批动作 */
  async function doAct(action: ApprovalAction) {
    if (!detail) return
    const text = comment.trim()
    // ⚠️ **前端这道是体验,后端那道才是边界** —— 两边都留
    if ((action === 'reject' || action === 'rollback') && countChars(text) < MIN_COMMENT) {
      message.error(
        t('approval.actionReasonRequired', undefined, {
          action: action === 'reject' ? t('approval.logAction.reject') : t('approval.logAction.rollback'),
          n: MIN_COMMENT,
        }),
      )
      return
    }
    setActing(true)
    try {
      await approvalApi.act(detail.id, { action, comment: text })
      message.success(t('page.approvalDetail.submitted'))
      setComment('')
      await load()
      // ⚠️ **审批完立刻重拉一次待办角标**(M89-3)——
      //
      //   角标是"待我审批"的条数,而 approve / reject / rollback 三个动作
      //   **都会让我自己那条 pending task 不再是 pending** → 角标该 -1。
      //
      //   原来这里只 `await load()`(重拉详情),角标完全靠 SSE 事件带过来。
      //   那条路有**两个洞**:
      //     ① 服务端在会签"还有人没批"那一支原来**一条事件都不发**(M89-1 修的)
      //        → 角标停在错的数上;
      //     ② Redis 挂了 / SSE 降级时(M88-y)事件根本不来 → 角标永远不刷。
      //
      //   自己刚做完的动作,**不等广播** —— 直接拉一次,这是最确定的。
      //   ⚠️ **不自己算 ±1** —— 数字必须来自 `box=todo` 的 total(理由见
      //   `useEventStream.ts` 里 `refreshApprovalTodo` 的注释)。
      await refreshApprovalTodo()
    } catch {
      // 拦截器弹过 toast
    } finally {
      setActing(false)
    }
  }

  /** 作废 */
  async function doCancel() {
    if (!detail) return
    setActing(true)
    try {
      await approvalApi.cancel(detail.id)
      message.success(t('approval.status.cancelled'))
      await load()
    } catch {
      // 拦截器弹过 toast
    } finally {
      setActing(false)
    }
  }

  const assigneeColumns = [
    { title: t('field.approver'), dataIndex: 'name', width: 160 },
    {
      title: t('common.status'),
      dataIndex: 'status',
      width: 120,
      render: (v: string) => {
        if (v === 'pending') return <Tag color="processing">{t('approval.assigneeStatus.pending')}</Tag>
        if (v === 'approved') return <Tag color="success">{t('approval.assigneeStatus.approved')}</Tag>
        if (v === 'rejected') return <Tag color="error">{t('approval.assigneeStatus.rejected')}</Tag>
        if (v === 'stale') return <Tag>{t('approval.assigneeStatus.stale')}</Tag>
        return <Tag>{t('approval.assigneeStatus.skipped')}</Tag>
      },
    },
    {
      title: t('field.handleTime'),
      dataIndex: 'actioned_at',
      width: 170,
      render: (v: string | null) => (v ? v.slice(0, 16).replace('T', ' ') : '—'),
    },
  ]

  if (!detail) {
    return (
      <div style={{ padding: 24 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/approval')}>
          {t('page.approvalDetail.back')}
        </Button>
        <Empty style={{ marginTop: 48 }} description={loading ? t('common.loading') : t('page.approvalDetail.notFound')} />
      </div>
    )
  }

  const statusTag = STATUS_TAG[detail.status]
  const bizRoute = BIZ_TYPE_ROUTE[detail.biz_type]

  return (
    <div style={{ padding: 16, height: '100%', overflow: 'auto' }}>
      {/* ===== 顶部:标题 / 业务类型 / 状态 ===== */}
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Space>
            <Button type="text" size="small" icon={<ArrowLeftOutlined />} onClick={() => navigate('/approval')}>
              {t('page.approvalDetail.back')}
            </Button>
            <span style={{ fontSize: 16, fontWeight: 600 }}>{detail.title}</span>
            {statusTag ? <Tag color={statusTag.color}>{t(statusTag.text)}</Tag> : null}
            <Tag color="blue">{t('page.approvalDetail.title')}</Tag>
          </Space>
          <Descriptions size="small" column={4} bordered>
            <Descriptions.Item label={t('field.bizType')}>
              {bizRoute ? (
                <a onClick={() => navigate(bizRoute)}>{detail.biz_type}</a>
              ) : (
                detail.biz_type
              )}
            </Descriptions.Item>
            <Descriptions.Item label={t('page.approvalDetail.bizId')}>{detail.biz_id}</Descriptions.Item>
            <Descriptions.Item label={t('page.approvalDetail.templateName')}>{detail.template_name || detail.template_code}</Descriptions.Item>
            <Descriptions.Item label={t('field.initiator')}>{detail.applicant_name}</Descriptions.Item>
            <Descriptions.Item label={t('field.currentStage')}>{detail.current_stage_name || '—'}</Descriptions.Item>
            <Descriptions.Item label={t('page.approvalDetail.returnCount')}>{detail.return_count}</Descriptions.Item>
            <Descriptions.Item label={t('common.createTime')}>
              {detail.created_at?.slice(0, 16).replace('T', ' ')}
            </Descriptions.Item>
            <Descriptions.Item label={t('page.approvalDetail.finishTime')}>
              {detail.finished_at ? detail.finished_at.slice(0, 16).replace('T', ' ') : '—'}
            </Descriptions.Item>
          </Descriptions>
        </Space>
      </Card>

      {/* ===== 轮次切换器(只有多轮才渲染)===== */}
      {rounds.length > 1 ? (
        <Card size="small" style={{ marginBottom: 12 }} title={t('page.approvalDetail.roundSection')}>
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Radio.Group
              value={roundNo}
              onChange={(e) => switchRound(Number(e.target.value))}
              optionType="button"
              buttonStyle="solid"
            >
              {rounds.map((r) => (
                <Radio.Button key={r.round_no} value={r.round_no}>
                  {t('approval.roundTitle', undefined, { n: r.round_no })}
                  {r.round_no === currentRoundNo ? t('page.approvalDetail.roundCurrent') : ''}
                </Radio.Button>
              ))}
            </Radio.Group>
            {isHistorical ? (
              <AlertLike
                text={t('page.approvalDetail.roundArchive', undefined, {
                  n: roundNo,
                  m: currentRoundNo,
                })}
              />
            ) : null}
          </Space>
        </Card>
      ) : null}

      {/* ===== 标题 / 事由(可编辑 or 快照)===== */}
      <Card size="small" style={{ marginBottom: 12 }} title={t('page.approvalDetail.contentSection')}>
        {editable ? (
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Input
              value={draftTitle}
              onChange={(e) => {
                setDraftTitle(e.target.value)
                setDirty(true)
              }}
              placeholder={t('common.placeholder.approvalTitle')}
              disabled={isHistorical}
            />
            <Input.TextArea
              rows={3}
              value={draftVariables}
              onChange={(e) => {
                setDraftVariables(e.target.value)
                setDirty(true)
              }}
              placeholder={t('common.placeholder.approvalReason')}
              disabled={isHistorical}
            />
            <Space>
              <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>
                {dirty
                  ? t('page.approvalDetail.dirty')
                  : t('page.approvalDetail.sameAsSnapshot')}
              </span>
              {canSubmit ? (
                <Button type="primary" loading={acting} onClick={() => void doSubmit()}>
                  {detail.status === 'returned'
                    ? t('page.approvalDetail.saveAndResubmit')
                    : t('page.approvalDetail.submit')}
                </Button>
              ) : null}
            </Space>
            {/* ⚠️ **行内编辑,不是弹窗** —— 打回重提是主路径(§9.1) */}
          </Space>
        ) : (
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <div style={{ fontSize: 14 }}>{detail.title}</div>
            {variablePairs.length > 0 ? (
              <Descriptions size="small" column={2} bordered>
                {variablePairs.map((p) => (
                  <Descriptions.Item key={p.key} label={p.key}>
                    {p.value}
                  </Descriptions.Item>
                ))}
              </Descriptions>
            ) : (
              <span style={{ color: 'var(--app-text-tertiary)' }}>
                {t('page.approvalDetail.noExtra')}
              </span>
            )}
          </Space>
        )}
      </Card>

      {/* ===== 阶段进度 + 当前关审批人 ===== */}
      <Card size="small" style={{ marginBottom: 12 }} title={t('page.approvalDetail.progressSection')}>
        {detail.stages.length === 0 ? (
          <Empty description={t('page.approvalDetail.noStage')} />
        ) : (
          <Space direction="vertical" size={16} style={{ width: '100%' }}>
            <Steps
              // ⚠️ **草稿态还没进任何关卡** —— current_stage=0,这时要传 -1,
              //   否则第 1 关会被画成"进行中",而实际上还没开始
              current={detail.current_stage > 0 ? detail.current_stage - 1 : -1}
              items={detail.stages.map((s) => ({
                title: s.name,
                description: (
                  <Space direction="vertical" size={0}>
                    {/* ⚠️ **会签要显示"共 N 人,已批 M 人"**(§9.1 硬要求 2)——
                        否则共同审批人以为"我批了就行了",然后单子卡住 */}
                    <span style={{ fontSize: 12 }}>
                      {t('page.approvalDetail.assigneeProgress', undefined, {
                        n: s.total_count,
                        m: s.approved_count,
                      })}
                    </span>
                    {s.review_type === 'all' ? (
                      <Tag color="purple">{t('approval.reviewType.all')}</Tag>
                    ) : (
                      <Tag color="blue">{t('approval.reviewType.any')}</Tag>
                    )}
                  </Space>
                ),
                status:
                  s.status === 'done'
                    ? 'finish'
                    : s.status === 'current'
                      ? 'process'
                      : s.status === 'rejected'
                        ? 'error'
                        : 'wait',
              }))}
            />
            {detail.stages.map((s) => (
              <div key={s.stage_index}>
                <div style={{ fontSize: 13, marginBottom: 6 }}>
                  {t('page.approvalDetail.stageTitle', undefined, {
                    n: s.stage_index,
                    name: s.name,
                  })}
                </div>
                <Table<ApprovalAssigneeView>
                  size="small"
                  rowKey={(r) => r.user_id}
                  columns={assigneeColumns}
                  dataSource={s.assignees}
                  pagination={false}
                />
              </div>
            ))}
          </Space>
        )}
      </Card>

      {/* ===== 意见流水(按轮次分组)===== */}
      <Card size="small" style={{ marginBottom: 12 }} title={t('page.approvalDetail.logSection')}>
        {logGroups.length === 0 ? (
          <Empty description={t('page.approvalDetail.noLog')} />
        ) : (
          logGroups.map((g) => {
            const round = rounds.find((r) => r.round_no === g.roundNo)
            const submitLog = g.items.find((l) => l.action === 'submit')
            const outcome = round ? ROUND_OUTCOME[round.outcome] : undefined
            return (
              <div key={g.roundNo} style={{ marginBottom: 16 }}>
                <Space style={{ marginBottom: 8 }}>
                  <Tag>{t('approval.roundTitle', undefined, { n: g.roundNo })}</Tag>
                  <span style={{ fontSize: 13 }}>
                    {submitLog ? t('approval.submittedBy', undefined, { name: submitLog.operator_name }) : '—'}
                  </span>
                  {outcome ? <Tag color={outcome.color}>{t(outcome.text)}</Tag> : null}
                </Space>
                <Table<ApprovalLogView>
                  size="small"
                  rowKey={(r) => r.id}
                  pagination={false}
                  dataSource={g.items}
                  columns={[
                    {
                      title: t('field.logAction'),
                      dataIndex: 'action',
                      width: 100,
                      render: (v: string) => {
                        const a = LOG_ACTION[v]
                        return a ? <Tag color={a.color}>{t(a.text)}</Tag> : <Tag>{v}</Tag>
                      },
                    },
                    { title: t('field.operator'), dataIndex: 'operator_name', width: 140 },
                    { title: t('field.comment'), dataIndex: 'comment', ellipsis: true },
                    {
                      title: t('field.time'),
                      dataIndex: 'created_at',
                      width: 170,
                      render: (v: string) => v?.slice(0, 16).replace('T', ' '),
                    },
                  ]}
                />
              </div>
            )
          })
        )}
      </Card>

      {/* ===== 底部动作区 ===== */}
      {canAct || canCancel ? (
        <Card size="small" title={t('common.action')}>
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            {canAct ? (
              <>
                <Input.TextArea
                  rows={3}
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                  placeholder={t('common.placeholder.approvalComment', undefined, { n: MIN_COMMENT })}
                  maxLength={500}
                  showCount
                />
                <Space wrap>
                  <Button
                    type="primary"
                    loading={acting}
                    onClick={() => void doAct('approve')}
                  >
                    {t('approval.logAction.approve')}
                  </Button>
                  <Button
                    danger
                    loading={acting}
                    // ⚠️ **条件不满足就不让点** —— 与其点了被后端拒,不如置灰
                    disabled={countChars(comment) < MIN_COMMENT}
                    onClick={() => void doAct('reject')}
                  >
                    {t('approval.logAction.reject')}
                  </Button>
                  <Button
                    loading={acting}
                    disabled={countChars(comment) < MIN_COMMENT}
                    onClick={() => void doAct('rollback')}
                  >
                    {t('page.approvalDetail.rollbackRequest')}
                  </Button>
                </Space>
              </>
            ) : null}
            {canCancel ? (
              <Button danger loading={acting} onClick={() => void doCancel()}>
                {t('approval.logAction.cancel')}
              </Button>
            ) : null}
          </Space>
        </Card>
      ) : null}
    </div>
  )
}

/** 一行灰底提示(历史轮次) */
function AlertLike({ text }: { text: string }) {
  return (
    <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12, background: 'var(--app-surface-bg)', padding: '4px 8px' }}>
      {text}
    </span>
  )
}

export default ApprovalDetailPage
