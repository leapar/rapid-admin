/**
 * 登录日志 —— 列表 + 详情
 *
 * # 这一页此前是 PagePlaceholder(2026-10-30 补实)
 *
 *   路由 / 菜单 / 权限码都在,点进去只有一个骨架屏。
 *
 * # 这一页踩到的几个"静默失效"点(改之前先看清楚)
 *
 *   1. **成功/失败筛选必须叫 `success`,不能叫 `status`** ——
 *      后端 `LoginLogQuery.Success` 是 `*bool`,handler 读的是 `c.Query("success")`
 *      (`queryOptionalBool`:"true"/"1" → true,"false"/"0" → false)。
 *      传 `status=0` 会被**完全忽略**:HTTP 200、结果不变、不报错。
 *      ⇒ 页面上的成功/失败下拉框,参数名写错的话就是个纯装饰。
 *
 *   2. **keyword 只搜 username** —— 后端登录日志的 keyword 是
 *      `username LIKE`(不像操作日志那样还搜 IP / 模块 / 描述)。
 *      ⇒ 搜索框提示词写"搜索用户名/IP"会**骗人**:输了 IP 结果为空,
 *      用户以为功能坏了。
 *
 *   3. **时间范围筛的是 `login_at`,不是 `created_at`** ——
 *      登录日志的时间筛选走 `login_at` 列(另外两张表走 `created_at`)。
 *      对登录日志来说这才是对的语义(查"某次登录发生在什么时候")。
 *
 *   4. **Select 的 value 不能是 boolean** —— antd 5 的 `RawValue = string | number`,
 *      传 `false` 过不了类型检查 ⇒ 界面上的选中值用 1/0 承载,
 *      进 query 时再翻回 boolean。
 *
 * # `browser` / `os` 两列(2026-10-30 起才有值)
 *
 *   此前这两列**后端从来不写** —— `authService.recordLoginLog` 只填
 *   user_id / username / ip / device / status / message / login_at / session_id,
 *   而表里(model.SysLoginLog)**一直就有** browser / os 两列 ⇒ 两列永远是空的。
 *   ⇒ 现在后端从 UA 解析出来填上(规则见 server/internal/pkg/uainfo),
 *     这两列才摆出来 —— **摆出来之前它们是噪声,摆出来之后才是信息。**
 *
 *   解析不出来时后端写**空串**(不猜)⇒ 前端渲染成 "-"。
 *
 * # 仍然不展示的列(为什么)
 *
 *   `location` —— 后端**从来不写**(没有 IP → 地区的库),摆出来永远是空白。
 *   `device` 那一列装的是 **User-Agent 原文**(不是设备型号)。
 */
import { useState } from 'react'
import type { FC } from 'react'
import { Select, Tag, Tooltip } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ManagementTable } from '@/components/common/ManagementTable'
import { ViewDrawer } from '@/components/common/ViewDrawer'
import type { ViewField } from '@/components/common/ViewDrawer'
import { RowActions } from '@/components/common/RowActions'
import type { RowAction } from '@/components/common/RowActions'
import { useTable } from '@/hooks/useTable'
import { useDict } from '@/hooks/useDict'
import { useI18n } from '@/locales'
import { auditApi } from '@/api/audit'
import type { LoginLog, LoginLogQuery } from '@/types/audit'
import AuditRangeFilter from './RangeFilter'
import { fmtAuditTime } from './format'

const LoginLogList: FC = () => {
  const { t } = useI18n()
  /**
   * 登录结果复用 `audit_op_status`(1 成功 / 0 失败)——
   * 取值与语义跟操作日志的 status **完全一致**,不必再开一个字典组
   * (字典组 = 代码 enum 契约,多开一个就多一处要同步的地方)。
   */
  const { getLabel: getStatusLabel, getColor: getStatusColor } = useDict('audit_op_status')

  const { tableProps, search, reset, query, setQuery } = useTable<LoginLogQuery, LoginLog>({
    fetch: (q) => auditApi.loginLogs(q),
  })

  const [viewing, setViewing] = useState<LoginLog | null>(null)

  /** 状态标签(1 成功 / 0 失败)—— 文案与颜色都走字典 */
  const statusTag = (v: number) => {
    const label = getStatusLabel(v)
    // 字典里查不到这一项 → 显示原始值(而不是空白)
    if (label === String(v)) return <Tag color="default">{v}</Tag>
    return <Tag color={getStatusColor(v)}>{label}</Tag>
  }

  const columns: ColumnsType<LoginLog> = [
    {
      title: t('field.user'),
      dataIndex: 'username',
      width: 120,
      fixed: 'left',
    },
    {
      title: t('field.ip'),
      dataIndex: 'ip',
      width: 130,
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      width: 90,
      render: (v: number) => statusTag(v),
    },
    {
      title: t('field.message'),
      dataIndex: 'message',
      width: 200,
      ellipsis: true,
    },
    {
      title: t('field.loginTime'),
      dataIndex: 'login_at',
      width: 165,
      render: (v: string | null) => fmtAuditTime(v),
    },
    {
      title: t('field.lastActivity'),
      dataIndex: 'last_active_at',
      width: 165,
      render: (v: string | null) => fmtAuditTime(v),
    },
    {
      title: t('field.offlineTime'),
      dataIndex: 'logout_at',
      width: 165,
      render: (v: string | null) => fmtAuditTime(v),
    },
    {
      // ⚠️ device 装的是 User-Agent 原文,不是"设备型号"
      title: t('field.device'),
      dataIndex: 'device',
      width: 240,
      ellipsis: true,
      render: (v: string) =>
        v ? (
          <Tooltip title={v}>
            <code style={{ fontSize: 11 }}>{v}</code>
          </Tooltip>
        ) : (
          '-'
        ),
    },
    {
      // ⚠️ 2026-10-30 起才有值 —— 后端从 UA 解析出来的产品名 + 版本
      title: t('field.browser'),
      dataIndex: 'browser',
      width: 150,
      ellipsis: true,
      render: (v: string) => v || '-',
    },
    {
      title: t('field.os'),
      dataIndex: 'os',
      width: 150,
      ellipsis: true,
      render: (v: string) => v || '-',
    },
    {
      title: t('field.sessionId'),
      dataIndex: 'session_id',
      width: 180,
      ellipsis: true,
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 100,
      fixed: 'right',
      render: (_: unknown, row: LoginLog) => {
        const actions: RowAction<LoginLog>[] = [
          // view 详情不需要 button 粒度 perm(只读操作,走路由级 audit:login:list 守卫)
          { key: 'view', label: t('common.detail'), onClick: setViewing },
        ]
        return <RowActions row={row} actions={actions} />
      },
    },
  ]

  const viewFields: ViewField<LoginLog>[] = [
    { name: 'username', label: t('field.user') },
    { name: 'user_id', label: t('field.userId') },
    { name: 'ip', label: t('field.ip') },
    {
      name: 'status',
      label: t('common.status'),
      render: (v: number) => statusTag(v),
    },
    { name: 'message', label: t('field.message'), render: (v: string) => v || '-' },
    {
      name: 'device',
      label: t('field.userAgent'),
      render: (v: string) => (v ? <code style={{ fontSize: 11 }}>{v}</code> : '-'),
    },
    {
      // ⚠️ 2026-10-30 起才有值(后端从 UA 解析)
      name: 'browser',
      label: t('field.browser'),
      render: (v: string) => v || '-',
    },
    {
      name: 'os',
      label: t('field.os'),
      render: (v: string) => v || '-',
    },
    { name: 'session_id', label: t('field.sessionId'), render: (v: string) => v || '-' },
    {
      name: 'login_at',
      label: t('field.loginTime'),
      render: (v: string | null) => fmtAuditTime(v),
    },
    {
      name: 'last_active_at',
      label: t('field.lastActivity'),
      render: (v: string | null) => fmtAuditTime(v),
    },
    {
      name: 'logout_at',
      label: t('field.offlineTime'),
      render: (v: string | null) => fmtAuditTime(v),
    },
    { name: 'created_at', label: t('common.createTime'), render: (v: string) => fmtAuditTime(v) },
  ]

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<LoginLog>
        pageDescription={t('page.auditLogin.description')}
        searchPlaceholder={t('common.placeholder.searchAuditLogin')}
        onSearch={(kw) => search({ keyword: kw })}
        onReset={reset}
        searchExtra={[
          <Select
            key="success"
            placeholder={t('common.placeholder.filterStatus')}
            allowClear
            style={{ width: 120 }}
            // ⚠️ antd 5 的 Select value 只吃 string | number —— boolean 过不了类型检查,
            //   所以界面上用 1/0 承载,进 query 时再翻回 boolean。
            value={query.success === undefined ? undefined : query.success ? 1 : 0}
            onChange={(v) =>
              setQuery(
                (prev) =>
                  ({
                    ...prev,
                    success: v === undefined ? undefined : v === 1,
                    page: 1,
                  }) as LoginLogQuery,
              )
            }
            options={[
              { value: 1, label: t('common.resultSuccess') },
              { value: 0, label: t('common.resultFailed') },
            ]}
          />,
          <AuditRangeFilter
            key="range"
            startAt={query.startAt}
            endAt={query.endAt}
            onChange={(r) =>
              setQuery(
                (prev) =>
                  ({
                    ...prev,
                    startAt: r.startAt,
                    endAt: r.endAt,
                    page: 1,
                  }) as LoginLogQuery,
              )
            }
          />,
        ]}
        columns={columns}
        tableProps={tableProps}
        scrollX={1500}
      />

      <ViewDrawer<LoginLog>
        title={t('resource.audit_login_read')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
        width={720}
      />
    </div>
  )
}

export default LoginLogList
