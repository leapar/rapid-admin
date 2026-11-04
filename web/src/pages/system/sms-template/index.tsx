/**
 * 短信模板审核状态 —— M115
 * 2026-10-30 新建
 *
 * # 这个页面解决什么
 *
 *   模板在短信服务商那边**过没过审**,平台只有主动去问才知道。
 *   服务商拒发时只给一句 isv.SMS_TEMPLATE_ILLEGAL,用户看到的是"发送失败" ——
 *   平台侧维护一本登记册(pkg/smstpl),后台页面看状态、守卫(pkg/smstpl.Guard)用状态。
 *
 *   页面提供三件事:
 *   1. **看** 四个场景各自是什么状态、结论是谁给的、什么时候给的;
 *   2. **止血** —— 人工停用某个场景(该场景的验证码立刻发不出去);
 *   3. **同步** —— 手动拉一轮服务商的真实审核状态(与定时任务同一个入口)。
 *
 * # ⚠️ 三条"不要在页面上自作主张"的规矩
 *
 *   1. **状态标签与颜色走字典**(useDict('sms_template_status')),
 *      前端不自己写一张 status → 文案/颜色的表 —— 字典是运营能改的
 *      (下线一个状态、加一个新状态),前端那份副本必然漂移。
 *   2. **场景名走 i18n**(constants/sms 的 SMS_SCENE_LABEL_KEY),
 *      不用后端返的 `scene_label` —— 那是写死的中文,英文环境必然露馅。
 *   3. **停用与恢复都只是"止血/放行",不是"判定通过"** ——
 *      人工无权判定通过(硬规则 1):恢复落到的是"待审核",
 *      最终结论要等服务商下一次同步。按钮文案不能写成"启用"。
 *
 * # ⚠️ 「立即同步」是**同一个入口**,不是另一份同步逻辑
 *
 *   页面调 POST /platform/sms-templates/sync,后端走的是
 *   `smstpl.Runtime.SyncOnce` —— 与每 10 分钟那个定时任务**完全同源**。
 *   两处各写一遍的话,迟早漂移:页面按 A 判、定时任务按 B 判,
 *   运营在页面上看到的和实际生效的不是一回事。
 */
import { useState, useEffect, useCallback } from 'react'
import type { FC } from 'react'
import { Tag, Button, App, Alert, Tooltip, Space } from 'antd'
import {
  ReloadOutlined,
  SyncOutlined,
  WarningOutlined,
  InfoCircleOutlined,
} from '@ant-design/icons'
import { ManagementTable } from '@/components/common/ManagementTable'
import { EditModal } from '@/components/common/EditModal'
import type { EditField } from '@/components/common/EditModal'
import { RowActions } from '@/components/common/RowActions'
import type { RowAction } from '@/components/common/RowActions'
import { useI18n } from '@/locales'
import { useDict } from '@/hooks/useDict'
import { smsTemplateApi } from '@/api/smsTemplate'
import { PermButton } from '@/constants/permissions'
import { SMS_SCENE_LABEL_KEY, type SmsScene } from '@/constants/sms'
import type {
  SmsTemplateItem,
  SmsTemplateActionForm,
  SmsTemplateListView,
} from '@/types/smsTemplate'
import History from './History'

/** 行操作弹窗的两种动作 */
type ActionKind = 'disable' | 'enable'

/** 弹窗上下文(哪个场景 + 做什么) */
interface ActionState {
  kind: ActionKind
  item: SmsTemplateItem
}

/**
 * 截断一段文字(给提示用)
 *
 * ⚠️ 不截断的话,服务商的报错原文可能几百字,message 会被撑爆。
 */
function clip(text: string, max: number): string {
  const s = (text ?? '').trim()
  return s.length > max ? `${s.slice(0, max)}…` : s
}

const SmsTemplateList: FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  /**
   * 状态标签与颜色**全部来自字典**。
   *
   * ⚠️ `getLabel` 查不到时返回原值(不是空串)—— 所以拿 label === value
   *   判断"字典里没有这一项",这时显示原始值而不是空白。
   */
  const { getLabel: getStatusLabel, getColor: getStatusColor } = useDict('sms_template_status')

  const [view, setView] = useState<SmsTemplateListView | null>(null)
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [action, setAction] = useState<ActionState | null>(null)
  const [historyScene, setHistoryScene] = useState<string | null>(null)
  /** 搜索关键字(过滤场景名 / 模板号 / 状态 / 原因) */
  const [keyword, setKeyword] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await smsTemplateApi.list()
      setView(res)
    } catch {
      // 错误提示由 axios 拦截器统一弹出(见 api/client.ts 的 notifyHttpError),
      // 这里吞掉只是为了让表格别卡在 loading
      setView(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  /** 场景名走 i18n(不用后端返的 scene_label —— 那是写死的中文) */
  const sceneLabel = (scene: string) => {
    const key = SMS_SCENE_LABEL_KEY[scene as SmsScene]
    return key ? t(key) : scene
  }

  /**
   * 按关键字过滤(四个场景全在前端,不必回后端查)
   *
   * ⚠️ **命中范围包含"上次失败原因"** —— 用户搜"超时"就是要找
   *   "哪个场景同步失败、为什么";只搜状态名会一个都搜不到。
   */
  const visibleItems = (() => {
    const items = view?.items ?? []
    const kw = keyword.trim().toLowerCase()
    if (!kw) return items
    return items.filter((it) =>
      [
        it.scene,
        sceneLabel(it.scene),
        it.template_code,
        it.registered_code,
        it.status,
        getStatusLabel(it.status),
        it.reason,
        it.last_error,
      ]
        .join(' ')
        .toLowerCase()
        .includes(kw),
    )
  })()

  /** 状态来源:manual = 人工点的;provider = 服务商同步回来的 */
  const sourceLabel = (source: string) =>
    source === 'provider'
      ? t('page.smsTemplate.source.provider')
      : t('page.smsTemplate.source.manual')

  /**
   * 守卫策略 → 文案
   *
   * ⚠️ 策略是后端定的三档(off / warn / block,见 pkg/smstpl.Policy),
   *   这里**只做展示翻译**,不新增第四档 —— 后端加档位时这个函数要跟着改,
   *   改漏了会退化成"未知策略"(见下面的兜底)。
   *
   * ⚠️ **必须在这里就 t() 掉,不能把 i18n 键当值塞进模板变量** ——
   *   否则界面上会出现 "策略:page.smsTemplate.policyBlock" 这种裸键。
   */
  const policyText = (policy: string): string => {
    if (policy === 'block') return t('page.smsTemplate.policyBlock')
    if (policy === 'warn') return t('page.smsTemplate.policyWarn')
    if (policy === 'off') return t('page.smsTemplate.policyOff')
    return t('page.smsTemplate.policyUnknown')
  }

  /**
   * 立即同步 —— 与定时任务同一个入口(后端 smstpl.Runtime.SyncOnce)
   *
   * ⚠️ **必须判 failed,不能只看 HTTP 成功** ——
   *   `failed` 里混着两种东西:服务商查询失败,和状态机按规则拒绝流转
   *   (人工停用的模板,服务商说通过 —— 硬规则 2 在起作用,不是故障)。
   *   所以提示不能写"失败",要说"没能确定状态"。
   *
   * ⚠️ **提示里要带上原因** —— 只有场景名的话,运维看着还是不知道
   *   为什么没同步上(后端 failures[].reason 就是给这句话用的)。
   */
  const handleSync = async () => {
    setSyncing(true)
    try {
      const res = await smsTemplateApi.sync()
      const updated = res?.updated?.length ?? 0
      const registered = res?.registered?.length ?? 0
      const unchanged = res?.unchanged ?? 0
      const failed = res?.failed ?? []

      if (failed.length > 0) {
        const failures = res?.failures ?? []
        // ⚠️ **老后端没有 failures 字段** —— 退回只列场景名(不能白屏)
        const details = failures.length
          ? failures
              .map((f) => `${sceneLabel(f.scene)}(${clip(f.reason, 60)})`)
              .join('; ')
          : failed.join(', ')
        message.warning(t('page.smsTemplate.syncPartial', undefined, { scenes: details }))
      } else if (updated > 0 || registered > 0) {
        message.success(
          t('page.smsTemplate.syncDone', undefined, { updated, registered, unchanged }),
        )
      } else {
        message.info(t('page.smsTemplate.syncNothing', undefined, { unchanged }))
      }
    } catch {
      // 真失败(比如当前通道不支持查询)由拦截器弹提示
    } finally {
      setSyncing(false)
      // ⚠️ **成功失败都要重拉** —— 同步改了状态,不刷新的话页面显示的还是旧状态,
      // 而用户刚点了"同步",看到没变化会以为同步没生效。
      load()
    }
  }

  /** 停用 / 恢复 */
  const handleAction = async (vals: SmsTemplateActionForm) => {
    if (!action) return
    const { kind, item } = action
    const reason = (vals.reason ?? '').trim()
    if (kind === 'disable') {
      await smsTemplateApi.disable(item.scene, reason)
      message.success(t('page.smsTemplate.disabled'))
    } else {
      await smsTemplateApi.enable(item.scene, reason)
      message.success(t('page.smsTemplate.enabled'))
    }
    setAction(null)
    load()
  }

  const columns = [
    {
      title: t('field.scene'),
      dataIndex: 'scene',
      width: 170,
      fixed: 'left' as const,
      render: (v: string) => (
        <Space direction="vertical" size={0}>
          <span>{sceneLabel(v)}</span>
          <code style={{ fontSize: 11, color: 'var(--app-text-tertiary)' }}>{v}</code>
        </Space>
      ),
    },
    {
      title: t('field.templateCode'),
      dataIndex: 'template_code',
      width: 210,
      render: (v: string, row: SmsTemplateItem) => (
        <Space direction="vertical" size={0}>
          <span>
            <code style={{ fontSize: 12 }}>{v || '-'}</code>
          </span>
          {row.code_mismatch && (
            <Tooltip title={t('page.smsTemplate.codeMismatchTip')}>
              <Tag color="orange" style={{ marginRight: 0 }}>
                {t('page.smsTemplate.codeMismatch')}
              </Tag>
            </Tooltip>
          )}
        </Space>
      ),
    },
    {
      title: t('field.reviewStatus'),
      dataIndex: 'status',
      width: 110,
      render: (v: string) => {
        const label = getStatusLabel(v)
        // 字典里查不到这一项 → 显示原始值(而不是空白),并用灰色
        if (label === String(v)) return <Tag color="default">{v || '-'}</Tag>
        return <Tag color={getStatusColor(v)}>{label}</Tag>
      },
    },
    {
      title: t('field.source'),
      dataIndex: 'source',
      width: 90,
      render: (v: string) => (v ? sourceLabel(v) : '-'),
    },
    {
      title: t('field.usable'),
      dataIndex: 'usable',
      width: 100,
      render: (_: boolean, row: SmsTemplateItem) => {
        if (row.usable) return <Tag color="green">{t('common.yes')}</Tag>
        if (row.blocked) return <Tag color="red">{t('common.no')}</Tag>
        return <Tag color="default">{t('page.smsTemplate.unknown')}</Tag>
      },
    },
    {
      title: t('field.reason'),
      dataIndex: 'reason',
      width: 220,
      ellipsis: true,
      render: (v: string) => v || '-',
    },
    {
      /*
       * ⚠️ **这一列是"未同步"的解释** ——
       *
       * 状态列只说"未同步",不说是**为什么**。运维看着一片"未同步"却无从下手
       * (用户 2026-10-30 报的就是这个)。后端把上一轮的失败原因带回来
       * (SmsTemplateView.LastError),这里原样显示。
       */
      title: t('field.lastSyncError'),
      dataIndex: 'last_error',
      width: 220,
      ellipsis: true,
      render: (v: string) =>
        v ? (
          <Tooltip title={v}>
            <span style={{ color: 'var(--app-error)', cursor: 'help' }}>{v}</span>
          </Tooltip>
        ) : (
          '-'
        ),
    },
    {
      title: t('field.reviewedAt'),
      dataIndex: 'reviewed_at',
      width: 165,
      render: (v: string) => (v ? v.slice(0, 19).replace('T', ' ') : '-'),
    },
    {
      title: t('common.updateTime'),
      dataIndex: 'updated_at',
      width: 165,
      render: (v: string) => (v ? v.slice(0, 19).replace('T', ' ') : '-'),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 240,
      fixed: 'right' as const,
      render: (_: unknown, row: SmsTemplateItem) => {
        const actions: RowAction<SmsTemplateItem>[] = [
          {
            key: 'disable',
            label: t('page.smsTemplate.disable'),
            perm: PermButton.SMS_TEMPLATE_DISABLE,
            // 没配模板号 = 这个场景压根不发短信,停用它没有意义
            // (后端也会拒:「该场景未配置模板号,没有可停用的模板」)
            disabled: !row.template_code,
            disabledTip: t('page.smsTemplate.notConfiguredTip'),
            onClick: (r) => setAction({ kind: 'disable', item: r }),
          },
          {
            key: 'enable',
            label: t('page.smsTemplate.enable'),
            perm: PermButton.SMS_TEMPLATE_ENABLE,
            // 只有停用状态才谈得上恢复(后端:「该场景当前不是停用状态,无需恢复」)
            disabled: row.status !== 'disabled',
            disabledTip: t('page.smsTemplate.notDisabledTip'),
            onClick: (r) => setAction({ kind: 'enable', item: r }),
          },
          {
            key: 'history',
            label: t('page.smsTemplate.history'),
            perm: PermButton.SMS_TEMPLATE_HISTORY,
            onClick: (r) => setHistoryScene(r.scene),
          },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  const actionFields: EditField[] = [
    {
      name: 'reason',
      label: t('page.smsTemplate.reasonLabel'),
      type: 'textarea',
      placeholder: t('page.smsTemplate.reasonPlaceholder'),
      // 原因会写进 sys_sms_template_log.reason(后端截断到 500 字符),
      // 这里给个提示但**不强制必填** —— 后端允许空原因(前端不应比后端更严)
      help: t('page.smsTemplate.reasonHelp'),
    },
  ]

  /** 当前通道能不能查审核状态(console provider 压根没有模板可审) */
  const providerUnsupported = !!view && view.provider !== 'aliyun'

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/*
       * 守卫策略横幅 —— 让人知道"现在这张模板被拦下来会怎么样"。
       *
       * ⚠️ 这一条不是装饰:策略是**平台级开关**,运营在配置中心改成 warn 之后,
       *   页面上一无所知,就会以为"模板被驳回了平台会拦住"—— 实际不会。
       */}
      {view && (
        <Alert
          type={view.policy === 'block' ? 'warning' : 'info'}
          showIcon
          icon={view.policy === 'block' ? <WarningOutlined /> : <InfoCircleOutlined />}
          message={t('page.smsTemplate.policyLabel', undefined, {
            policy: policyText(view.policy),
          })}
          description={t('page.smsTemplate.policyDesc')}
          style={{ marginBottom: 16 }}
        />
      )}

      {/* 当前通道不支持查询审核状态 —— 状态不会自动更新,同步按钮会报 400 */}
      {providerUnsupported && (
        <Alert
          type="warning"
          showIcon
          icon={<WarningOutlined />}
          message={t('page.smsTemplate.providerUnsupported', undefined, {
            provider: view?.provider ?? '',
          })}
          description={t('page.smsTemplate.providerUnsupportedDesc')}
          style={{ marginBottom: 16 }}
        />
      )}

      <ManagementTable<SmsTemplateItem>
        pageDescription={t('page.smsTemplate.description')}
        /*
         * ⚠️ **搜索框必须真的有** ——
         *
         * 之前这个页面只传了 inlineToolBar,ManagementTable 的
         * `showSearchBar = onSearch || onReset || searchExtra || inlineToolBar`
         * 因为 inlineToolBar 而成立 ⇒ SearchSection 渲染出来了,
         * **但里面左边是空的**(Input.Search 只在传了 onSearch 时才渲染)
         * ⇒ 用户看到"搜索区里没有搜索框"(用户 2026-10-30 报的)。
         */
        onSearch={setKeyword}
        onReset={() => setKeyword('')}
        inlineToolBar={[
          <Button key="reload" icon={<ReloadOutlined />} onClick={load}>
            {t('common.refresh')}
          </Button>,
          <Button key="sync" icon={<SyncOutlined />} loading={syncing} onClick={handleSync}>
            {t('page.smsTemplate.syncNow')}
          </Button>,
        ]}
        columns={columns}
        tableProps={{
          loading,
          dataSource: visibleItems,
          // SmsTemplateItem 没有 id 字段,必须显式给 rowKey,
          // 否则 ManagementTable 默认取 r.id → 全是 undefined → 选中/展开全乱
          rowKey: 'scene',
          // 固定四个场景,不分页
          pagination: false,
        }}
        scrollX={1690}
      />

      <EditModal<SmsTemplateActionForm>
        title={
          action?.kind === 'enable'
            ? `${t('page.smsTemplate.enableTitle')} · ${sceneLabel(action.item.scene)}`
            : `${t('page.smsTemplate.disableTitle')} · ${sceneLabel(action?.item.scene ?? '')}`
        }
        open={!!action}
        initialValues={{}}
        fields={actionFields}
        okText={
          action?.kind === 'enable' ? t('page.smsTemplate.enable') : t('page.smsTemplate.disable')
        }
        onCancel={() => setAction(null)}
        onSubmit={handleAction}
      />

      <History
        open={!!historyScene}
        scene={historyScene || ''}
        onClose={() => setHistoryScene(null)}
      />
    </div>
  )
}

export default SmsTemplateList
