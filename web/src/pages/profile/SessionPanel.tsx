/**
 * 登录设备(会话)面板 —— SRS §2.2.1 #6「查看活跃设备 / 强制下线其他设备」
 *
 * 挂在个人中心 /profile 的「登录设备」Tab。
 *
 * # 与后端的契约(改后端时同 commit 改这里)
 *
 *   GET  /auth/sessions?page=&size=   → { active: SessionView[], history: SessionPage }
 *   POST /auth/sessions/revoke         → { revoked: number }
 *
 * 后端:server/internal/service/auth_sessions.go(ListSessions / RevokeSessions)
 *
 * # "本机"由后端说了算,前端不自己算
 *
 * is_current 是后端拿 token 里的 sid 与登记簿(sys_login_log)比对出来的。
 * 前端**不自己判断** —— 前端没有 sid 的可靠来源(本轮上线前签发的老 token
 * 压根没有 sid claim),自己算就是猜,猜错的后果是"把当前这条会话也下线了"。
 *
 * # 为什么"本机"那行没有「下线」按钮
 *
 * 后端 RevokeSessions 故意跳过当前这条:当前这条要走 /auth/logout
 * (它连带把 access 的 jti 也吊了,语义完整)。在这里吊掉当前这条的结果是
 * "access 还活着、refresh 已死" —— 用户眼前还好好登着,却已经不能续期了。
 *
 * # 时间字段用后端的 ISO8601(+08:00)
 *
 * model.Time.MarshalJSON 固定输出东八区偏移,前端 dayjs 直接 parse 即可,
 * 不要自己再加一次时区偏移(会差 8 小时)。
 */
import React, { useCallback, useEffect, useRef, useState } from 'react'
import { App, Button, Card, Col, Empty, Popconfirm, Row, Space, Table, Tag, Tooltip } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { GetRef } from 'antd'
import { DesktopOutlined, LogoutOutlined, ReloadOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { listSessions, revokeSessions } from '@/api/auth'
import { useI18n } from '@/locales'
import type { SessionOverview, SessionView } from '@/types/user'

/** 时间格式化:后端给的是 ISO8601(东八区),空值显示 '-' */
const fmtTime = (v?: string | null): string => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '-')

/** "下线其他所有设备"这个批量动作的标记(不能与任何 sid 撞上) */
const ALL_MARK = '__all__'

/** 设备名:User-Agent 原文很长,列表里只显示前缀,全量挂 Tooltip */
const deviceLabel = (s: SessionView): string => {
  if (s.browser || s.os) return [s.browser, s.os].filter(Boolean).join(' / ')
  return s.device || '-'
}

const SessionPanel: React.FC = () => {
  const { message } = App.useApp()
  const { t } = useI18n()

  const [overview, setOverview] = useState<SessionOverview | undefined>()
  const [loading, setLoading] = useState(false)
  /** 正在下线的会话 id;'__all__' = "下线其他所有设备"这个批量动作 */
  const [revoking, setRevoking] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(20)

  /**
   * 历史登录记录表格的行区高度 —— **运行时量出来的**(M113,2026-10-29)
   *
   * ⚠️ 这不是"随便挑个数":量的是**表格容器的可用高度**
   *   (容器高度由个人中心那条 flex 高度链决定,与表格自身无关 ⇒ 不会自指),
   *   再减去表头高和分页高。
   *
   *   scroll.y = 容器高 - 表头高 - 分页高
   *
   * 详见文件头 HISTORY_SCROLL_Y 那段(改名成 MIN_HISTORY_SCROLL_Y 了)的说明。
   */
  const historyBoxRef = useRef<HTMLDivElement>(null)
  const historyTableRef = useRef<GetRef<typeof Table>>(null)

  const load = useCallback(async (p: number, s: number) => {
    setLoading(true)
    try {
      setOverview(await listSessions({ page: p, size: s }))
    } catch {
      // http 层已经统一弹了错误提示,这里不重复弹(否则一条错误弹两遍)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load(page, size)
  }, [load, page, size])

  /** 强制下线若干会话;成功后重拉列表 */
  const doRevoke = async (sids: string[], marker: string) => {
    if (sids.length === 0) return
    setRevoking(marker)
    try {
      const res = await revokeSessions(sids)
      message.success(t('session.revoked', undefined, { n: res.revoked }))
      await load(page, size)
    } catch {
      // 同上:错误提示由 http 层统一弹
    } finally {
      setRevoking(null)
    }
  }

  const active = overview?.active ?? []
  /** 别人的会话(本机排除在外)。"本机"标记由后端给,前端只筛不算。 */
  const others = active.filter((s) => !s.is_current)
  const otherSids = others.map((s) => s.session_id).filter(Boolean)
  const history = overview?.history

  const historyColumns: ColumnsType<SessionView> = [
    {
      title: t('field.device'),
      dataIndex: 'device',
      key: 'device',
      ellipsis: true,
      width: 200,
      render: (_: string, r: SessionView) => (
        <Tooltip title={r.device || '-'}>
          <span>{deviceLabel(r)}</span>
        </Tooltip>
      ),
    },
    {
      title: t('field.ip'),
      dataIndex: 'ip',
      key: 'ip',
      width: 100,
      render: (v: string, r: SessionView) => (
        <span>
          {v || '-'}
          {r.location ? <span style={{ color: 'var(--app-text-tertiary)' }}> ({r.location})</span> : null}
        </span>
      ),
    },
    {
      title: t('field.result'),
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (v: number, r: SessionView) =>
        v === 1 ? (
          <Tag color="green">{t('common.resultSuccess')}</Tag>
        ) : (
          <Tag color="red">{r.message || t('common.resultFailed')}</Tag>
        ),
    },
    {
      title: t('field.loginTime'),
      dataIndex: 'login_at',
      key: 'login_at',
      width: 100,
      render: (v: string | null) => fmtTime(v),
    },
    {
      title: t('field.lastActivity'),
      dataIndex: 'last_active_at',
      key: 'last_active_at',
      width: 100,
      render: (v: string | null) => fmtTime(v),
      // 失败登录压根没有会话,不会有"最后活动"
    },
    {
      title: t('field.offlineTime'),
      dataIndex: 'logout_at',
      key: 'logout_at',
      width: 100,
      render: (v: string | null) => fmtTime(v),
    },
  ]

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 16,
        height: '100%',
        minHeight: 0,
      }}
    >
      {/* ---------- 活跃设备 ---------- */}
      <Card
        type="inner"
        // 高度不参与伸缩:这张卡的内容有多少就多高,
        // 剩下的高度全给下面的历史登录记录表格
        style={{ flexShrink: 0 }}
        styles={{
          body:{
            maxHeight: 186,
            overflowY:"auto"
          }
        }}
        title={
          <Space>
            <DesktopOutlined /> {t('session.activeDevices')}
            <Tag color="blue">{active.length}</Tag>
          </Space>
        }
        extra={
          <Space>
            <Tooltip title={t('session.reload')}>
              <Button
                size="small"
                icon={<ReloadOutlined />}
                onClick={() => void load(page, size)}
                loading={loading}
              />
            </Tooltip>
            <Popconfirm
              title={t('session.revokeAllTitle')}
              description={t('session.revokeAllDesc', undefined, { n: otherSids.length })}
              okText={t('session.revokeOk')}
              cancelText={t('common.cancel')}
              onConfirm={() => void doRevoke(otherSids, ALL_MARK)}
              disabled={otherSids.length === 0}
            >
              <Button
                size="small"
                danger
                icon={<LogoutOutlined />}
                disabled={otherSids.length === 0}
                loading={revoking === ALL_MARK}
              >
                {t('session.revokeAll')}
              </Button>
            </Popconfirm>
          </Space>
        }
      >
        {active.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={loading ? t('common.loading') : t('session.noActive')}
          />
        ) : (
          <Space direction="vertical" style={{ width: '100%' }} size="small">
            {active.map((s) => (
              <Card
                key={s.session_id}
                size="small"
                style={{
                  borderColor: s.is_current ? 'var(--app-selected-border-strong)' : undefined,
                  background: s.is_current ? 'var(--app-selected-bg)' : undefined,
                }}
              >
                <Row justify="space-between" align="middle">
                  <Col>
                    <Space direction="vertical" size={2}>
                      <Space>
                        <span style={{ fontWeight: 500 }}>{deviceLabel(s)}</span>
                        {s.is_current && <Tag color="blue">{t('session.currentDevice')}</Tag>}
                        {s.expired && <Tag color="default">{t('session.expired')}</Tag>}
                      </Space>
                      <span style={{ color: 'var(--app-text-tertiary)', fontSize: 12 }}>
                        {s.ip || '-'}
                        {s.location ? ` · ${s.location}` : ''} · {t('session.loggedInAt')}{' '}
                        {fmtTime(s.login_at)} · {t('field.lastActivity')} {fmtTime(s.last_active_at)}
                      </span>
                    </Space>
                  </Col>
                  <Col>
                    {s.is_current ? (
                      <Tooltip title={t('session.revokeCurrentHint')}>
                        <Button size="small" disabled>
                          {t('session.revoke')}
                        </Button>
                      </Tooltip>
                    ) : (
                      <Popconfirm
                        title={t('session.revokeOneTitle')}
                        description={t('session.revokeOneDesc')}
                        okText={t('session.revokeOk')}
                        cancelText={t('common.cancel')}
                        onConfirm={() => void doRevoke([s.session_id], s.session_id)}
                      >
                        <Button size="small" danger loading={revoking === s.session_id}>
                          {t('session.revoke')}
                        </Button>
                      </Popconfirm>
                    )}
                  </Col>
                </Row>
              </Card>
            ))}
          </Space>
        )}
      </Card>

      {/* ---------- 历史登录记录 ---------- */}
      <Card
        type="inner"
        title={t('session.history')}
        // 这张卡吃掉剩下的全部高度(表格行区自己滚)
        style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
        styles={{
          body: {
            flex: 1,
            minHeight: 0,
            display: 'flex',
            flexDirection: 'column',
            overflow: 'hidden',
            // 表格要贴着卡片左边,留出 Card body 的默认内边距即可
            padding: 16,
          },
        }}
      >
        {/*
          ⚠️ M113(2026-10-29):**表格容器 —— 高度链的最后一环**
          这个 div 是 `flex:1 / min-height:0`,高度**只由 flex 剩余空间决定**,
          与表格自身高度无关 ⇒ 量它不会自指(这正是以前算不了的原因)。
        */}
        <div
          ref={historyBoxRef}
          style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
        >
          <Table<SessionView>
            ref={historyTableRef}
            size="small"
            rowKey={(r, i) => `${r.session_id || 'nosid'}-${i ?? 0}`}
            columns={historyColumns}
            dataSource={history?.list ?? []}
            loading={loading}
            /**
             * ⚠️ M113(2026-10-29):**行区高度是量出来的,不是写死的**
             *
             * 用户原话:"这个高度要动态计算,不应该写死,不同分辨率,这个高度要变化,
             * 让整个界面看的全所有内容,不要整个界面存在竖向滚动"。
             *
             * 量法:容器高 - 表头高 - 分页高(见 measureHistory)。
             * 容器高随分辨率变 ⇒ 不同屏幕下表格行区自动跟着变,
             * 整页刚好铺满一屏,页面本身不出现竖向滚动条。
             */
            scroll={{ y: window.innerHeight - 660 }}
            pagination={{
              current: history?.page ?? page,
              pageSize: history?.size ?? size,
              total: history?.total ?? 0,
              showSizeChanger: true,
              showTotal: (n) => t('common.total', undefined, { n }),
              onChange: (p, s) => {
                setPage(p)
                setSize(s)
              },
            }}
          />
        </div>
      </Card>
    </div>
  )
}

export default SessionPanel
