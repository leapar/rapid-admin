/**
 * 系统配置 —— 审计日志(2026-10-18 从 change.tsx 拆分)
 *
 * 2026-10-18 收窄:
 *   - 移除变更工单双签流,只留"只读审计日志"
 *   - 数据源: GET /rapid-admin/v1/platform/configs/audits
 *   - 列: 操作时间 / configKey / action / 变更人 / before→after
 *   - 顶栏: 关键字(configKey) 过滤 + 刷新
 *   - URL ?configGroup=xxx 参数:按**配置组**过滤
 *
 * 2026-10-29 修「group 过滤没效果」:
 *   以前这里把 URL 上的 group code 塞进 `config_key` 当"前缀过滤"发出去,
 *   而后端 handler 读的是 camelCase 的 `configKey`(Hertz 的 Query 大小写敏感)
 *   ⇒ 读出来永远是空串 ⇒ **过滤从来没生效过**,而且搜索框也是死的。
 *   现在两个过滤彻底分开:
 *     · group  → `config_group`(后端按 sys_config.config_group 精确匹配)
 *     · 关键字 → `config_key`(后端按子串匹配)
 *   两者同时存在时是 AND。
 *
 * 替代之前的 change.tsx 双签流(super_admin 直改,只留审计)
 */
import React, { useState, useEffect, useMemo } from 'react'
import { Tag, Button, Space } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { ManagementTable } from '@/components/common/ManagementTable'
import { ViewDrawer, ViewField } from '@/components/common/ViewDrawer'
import { configApi } from '@/api/config'
import { useI18n, tKey } from '@/locales'
import type { ConfigAuditLog, ConfigAuditQuery } from '@/types/config'

// 2026-10-18 收窄:action 只剩 update 一种
//   - 系统配置只走 PATCH /configs/:id 一条写通道(用户不会创建/删除配置项)
//   - create/delete/view 3 种 action 已全部从后端删掉,前端无 option 可选
//   - 保留 ACTION_COLOR map 仅为兜底(万一以后加新 action)
const ACTION_COLOR: Record<string, string> = {
  update: 'blue',
}

const ConfigAudit: React.FC = () => {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { t } = useI18n()
  const configGroup = params.get('configGroup') || ''

  const [data, setData] = useState<ConfigAuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  // ⚠️ 关键字**不再**预填 group code —— 那是"两个过滤混成一个"的根源。
  //   group 走 config_group,关键字走 config_key,各管各的。
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [viewing, setViewing] = useState<ConfigAuditLog | null>(null)

  // 加载审计日志
  const load = async (p = page, ps = pageSize, kw = keyword) => {
    setLoading(true)
    try {
      const q: ConfigAuditQuery = {
        page: p,
        page_size: ps,
      }
      if (kw) q.config_key = kw
      if (configGroup) q.config_group = configGroup
      const res = await configApi.auditLogs(q)
      setData((res as any).list || (res as any).items || [])
      setTotal((res as any).total || 0)
    } catch {
      setData([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    // 初次加载 + configGroup 变化
    load(1, pageSize, keyword)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [configGroup])

  const columns = useMemo(() => [
    {
      title: t('field.opTime'),
      dataIndex: 'created_at',
      width: 170,
      render: (v: string) => v ? v.slice(0, 19).replace('T', ' ') : '-',
    },
    {
      title: t('field.configKey'),
      dataIndex: 'config_key',
      width: 240,
      render: (v: string) => <code style={{ fontSize: 12 }}>{v || '-'}</code>,
    },
    {
      title: t('common.action'),
      dataIndex: 'action',
      width: 100,
      render: (v: string) => (
        <Tag color={ACTION_COLOR[v] || 'default'}>{v || '-'}</Tag>
      ),
    },
    {
      title: t('field.changedBy'),
      dataIndex: 'user_name',
      width: 120,
      render: (v: string, row: ConfigAuditLog) => v || t('page.configAudit.userFallback', undefined, { id: row.user_id }),
    },
    {
      title: t('field.changeDiff'),
      dataIndex: '__diff',
      width: 360,
      // 2026-10-18 收窄:action 只剩 update,无需 create/delete/view 分支
      render: (_: any, row: ConfigAuditLog) => (
        <span style={{ fontSize: 12 }}>
          <span style={{ color: 'var(--app-text-tertiary)' }}>{truncate(row.before)}</span>
          <span style={{ margin: '0 8px', color: 'var(--app-primary)' }}>→</span>
          <span style={{ color: '#52c41a' }}>{truncate(row.after)}</span>
        </span>
      ),
    },
    {
      title: t('field.ip'),
      dataIndex: 'ip',
      width: 130,
      render: (v: string) => <code style={{ fontSize: 11, color: 'var(--app-text-secondary)' }}>{v || '-'}</code>,
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 100,
      fixed: 'right' as const,
      render: (_: any, row: ConfigAuditLog) => (
        <Button type="link" size="small" onClick={() => setViewing(row)}>
          {t('common.detail')}
        </Button>
      ),
    },
  // ⚠️ 依赖里必须有 t —— t 的引用随语言切换而变(见 locales/useI18n.ts 的 useMemo([dict])),
  // 漏掉它会让列标题在切换语言后停在旧语言上。
  ], [t])

  const viewFields: ViewField<ConfigAuditLog>[] = [
    { name: 'created_at', label: t('field.opTime'), render: (v: string) => v?.slice(0, 19).replace('T', ' ') || '-' },
    { name: 'config_key', label: t('field.configKey') },
    { name: 'action', label: t('common.action'), render: (v: string) => <Tag color={ACTION_COLOR[v] || 'default'}>{v}</Tag> },
    { name: 'user_name', label: t('field.changedBy') },
    { name: 'ip', label: 'IP' },
    { name: 'user_role', label: t('field.role') },
    { name: 'before', label: t('page.configAudit.beforeValue'), render: (v: string) => <span style={{ wordBreak: 'break-all' }}>{v || t('common.emptyValue')}</span> },
    { name: 'after', label: t('page.configAudit.afterValue'), render: (v: string) => <span style={{ wordBreak: 'break-all' }}>{v || t('common.emptyValue')}</span> },
  ]

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <ManagementTable<ConfigAuditLog>
        pageDescription={
          configGroup
            ? t('page.configAudit.descriptionFiltered', undefined, { group: configGroup })
            : t('page.configAudit.description')
        }
        searchPlaceholder={t('common.placeholder.searchConfigAudit')}
        onSearch={(kw) => {
          setKeyword(kw)
          load(1, pageSize, kw)
        }}
        onReset={() => {
          setKeyword('')
          load(1, pageSize, '')
        }}
        searchExtra={configGroup ? (
          <Space size={6}>
            {/* 把"当前按哪个组过滤"显式摆出来 —— 过滤生效了才看得见 */}
            <Tag color="blue" style={{ marginInlineEnd: 0 }}>
              {t('page.configAudit.groupTag', undefined, { group: configGroup })}
            </Tag>
            <Button
              key="clearGroup"
              size="small"
              onClick={() => navigate('/system/config/audit')}
            >
              {t('page.configAudit.clearGroupFilter')}
            </Button>
          </Space>
        ) : null}
        inlineToolBar={[
          // 2026-10-18:返回靠面包屑(Breadcrumb.tsx 已支持最长前缀匹配),
          // 不用再加"返回"按钮,违反"一个入口"原则
          <Button
            key="reload"
            icon={<ReloadOutlined />}
            onClick={() => load(page, pageSize, keyword)}
          >
            {t('common.refresh')}
          </Button>,
        ]}
        columns={columns}
        tableProps={{
          loading,
          dataSource: data,
          rowKey: 'id',
          pagination: {
            current: page,
            pageSize,
            total,
            showSizeChanger: true,
            showTotal: (n) => t('common.total', undefined, { n }),
            onChange: (p, ps) => {
              setPage(p)
              setPageSize(ps)
              load(p, ps, keyword)
            },
          },
        }}
        scrollX={1500}
      />

      <ViewDrawer<ConfigAuditLog>
        title={t('page.configAudit.detailTitle')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
      />
    </div>
  )
}

function truncate(s: string | undefined, max = 30): string {
  if (!s) return tKey('common.emptyValue')
  return s.length > max ? `${s.slice(0, max)}…` : s
}

export default ConfigAudit
