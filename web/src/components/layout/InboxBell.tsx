/**
 * InboxBell —— 站内信铃铛 + 列表弹层(M74)
 *
 * ## 它补的是 M72 留下的一个洞
 *
 * M72 给 Header 换了真角标,但**铃铛点不动**(没有列表页)。当时的选择是
 * 宁可留一个哑弹也不交付"只有图标没有内容"的半截列表页;M74 把后半句补上。
 *
 * ## 为什么用 Popover 而不是新路由
 *
 * 站内信是"看一眼就走"的东西:不读可以关掉,读了要立刻回到原来在干的活。
 * 新开一个路由会把这个轻量动作变成"离开当前页面",而列表本身也没有分页
 * 筛选需求(后端有 page/size,这里只取前 N 条)。真要做成完整消息中心,
 * 那应该是一个独立页面 + 路由,而不是继续往 Popover 里堆。
 *
 * ## 权限
 *
 * 三个接口的权限码不同(见 api/inbox.ts 注释):
 *   - inbox:read     → 角标(M72 已有)
 *   - inbox:list      → 列表
 *   - inbox:mark_read → 标已读 / 全部已读
 * seed 里只有超管和只读角色绑了这三个,所以**多数人点开就是 403**。
 * 这里的表现是"空列表 + 一句说明",不是 toast —— 用户点个铃铛不该
 * 看到红色的操作失败弹窗。
 */
import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Badge, Button, Empty, List, Popover, Space, Spin, Typography, App } from 'antd'
import {
  BellOutlined,
  CheckOutlined,
  NotificationOutlined,
  LinkOutlined,
  ExclamationCircleOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { inboxApi } from '@/api/inbox'
import type { InboxMessage } from '@/types/inbox'
import { useEventStream, EVENT } from '@/hooks/useEventStream'
import { formatDate } from '@/utils/format'
import { useI18n } from '@/locales'

const { Text, Paragraph } = Typography

/** 弹层一次拉多少条。够看完,又不至于让下拉变成滚动条测试场 */
const PAGE_SIZE = 10

/** 切页与关闭时的加载态最长等待。超时后按"加载失败"处理,不让转圈转到底 */
const LOAD_TIMEOUT_MS = 8000

type LoadState = 'idle' | 'loading' | 'ready' | 'error'

export const InboxBell: React.FC = () => {
  const { t } = useI18n()
  const navigate = useNavigate()
  const { modal } = App.useApp()
  const { unreadCount, refreshUnread, subscribe } = useEventStream()

  const [open, setOpen] = useState(false)
  const [state, setState] = useState<LoadState>('idle')
  const [items, setItems] = useState<InboxMessage[]>([])
  /** 403 与"真的没消息"要分开:前者是权限问题,后者是正常的空状态 */
  const [forbidden, setForbidden] = useState(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(async () => {
    setState('loading')
    if (timerRef.current) clearTimeout(timerRef.current)
    // 兜底超时:接口挂住时不能让弹层一直转圈
    let timedOut = false
    timerRef.current = setTimeout(() => {
      timedOut = true
      setState('error')
    }, LOAD_TIMEOUT_MS)
    try {
      const res = await inboxApi.list({ size: PAGE_SIZE })
      if (timedOut) return
      setItems(Array.isArray(res?.list) ? res.list : [])
      setForbidden(false)
      setState('ready')
    } catch {
      if (timedOut) return
      // ⚠️ 不弹 toast。没 inbox:list 的人点开铃铛本来就是 403,
      // 弹一个"操作失败"只会让用户以为系统坏了。这里给一句人话就够了。
      setForbidden(true)
      setItems([])
      setState('error')
    } finally {
      if (timerRef.current) {
        clearTimeout(timerRef.current)
        timerRef.current = null
      }
    }
  }, [])

  useEffect(() => () => {
    if (timerRef.current) clearTimeout(timerRef.current)
  }, [])

  // 打开时拉一次。⚠️ 不在每次 unreadCount 变化时拉 ——
  // 角标事件可能连着来好几条(NotifyBatch),每条都重拉列表会抖。
  useEffect(() => {
    if (!open) return
    void load()
  }, [open, load])

  // 弹层开着时收到未读变更 → 静默重拉列表(此时用户正盯着它)
  useEffect(() => {
    if (!open) return
    return subscribe(EVENT.inboxUnread, () => {
      void load()
    })
  }, [open, subscribe, load])

  const handleRead = useCallback(
    async (msg: InboxMessage) => {
      if (msg.read_at) {
        // 已读:只处理跳转,不重复打接口(后端幂等,但没必要)
        if (msg.link) navigate(msg.link)
        return
      }
      // 乐观更新:先把这行画成已读,失败再回滚。标已读是个纯交互动作,
      // 等接口回来才更新会让点击"没反应"。
      // ⚠️ 这里的 ISO 时间戳只是**真值标记**(下游只判 `!msg.read_at`),
      // 格式是 UTC 而服务端是 +08:00 —— 刻意不去对齐,因为对齐它就得引入
      // 一个"本地已读集合"状态,为一个只用真假的地方不值当。
      const prev = items
      setItems((cur) => cur.map((m) => (m.id === msg.id ? { ...m, read_at: new Date().toISOString() } : m)))
      try {
        await inboxApi.markRead(msg.id)
        void refreshUnread()
      } catch {
        setItems(prev)
      }
      if (msg.link) navigate(msg.link)
    },
    [items, navigate, refreshUnread],
  )

  const handleAllRead = useCallback(() => {
    modal.confirm({
      title: t('inbox.markAllRead'),
      content: t('inbox.markAllReadConfirm'),
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        await inboxApi.markAllRead()
        await load()
        void refreshUnread()
      },
    })
  }, [modal, load, refreshUnread, t])

  const unreadTotal = items.filter((m) => !m.read_at).length

  const content = (
    <div style={{ width: 360, maxWidth: '80vw' }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '4px 4px 8px',
          borderBottom: '0.5px solid var(--app-border)',
        }}
      >
        <Text strong>{t('inbox.title')}</Text>
        <Button
          type="link"
          size="small"
          icon={<CheckOutlined />}
          disabled={unreadTotal === 0 || state === 'loading'}
          onClick={handleAllRead}
        >
          {t('inbox.markAllRead')}
        </Button>
      </div>

      {state === 'loading' && (
        <div style={{ padding: '24px 0', textAlign: 'center' }}>
          <Spin size="small" />
        </div>
      )}

      {state === 'error' && (
        <div style={{ padding: '24px 8px', textAlign: 'center' }}>
          <Empty
            image={<ExclamationCircleOutlined style={{ fontSize: 28, color: 'var(--app-text-tertiary)' }} />}
            description={
              <Space direction="vertical" size={2}>
                <Text type="secondary">{forbidden ? t('inbox.noPermission') : t('inbox.loadFailed')}</Text>
                {!forbidden && (
                  <Button type="link" size="small" onClick={() => void load()}>
                    {t('common.refresh')}
                  </Button>
                )}
              </Space>
            }
          />
        </div>
      )}

      {state === 'ready' && (
        <List
          size="small"
          dataSource={items}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('inbox.empty')} /> }}
          style={{ maxHeight: 380, overflowY: 'auto', marginTop: 4 }}
          renderItem={(msg) => {
            const unread = !msg.read_at
            return (
              <List.Item
                onClick={() => void handleRead(msg)}
                style={{ cursor: 'pointer', padding: '8px 4px', opacity: unread ? 1 : 0.6 }}
              >
                <List.Item.Meta
                  avatar={<NotificationOutlined style={{ color: unread ? 'var(--app-primary)' : 'var(--app-text-tertiary)' }} />}
                  title={
                    <Space size={4}>
                      {msg.title && (
                        <Text strong={unread} style={{ fontSize: 13 }}>
                          {msg.title}
                        </Text>
                      )}
                      {msg.link && <LinkOutlined style={{ fontSize: 11, color: 'var(--app-text-tertiary)' }} />}
                    </Space>
                  }
                  description={
                    <>
                      <Paragraph
                        ellipsis={{ rows: 2 }}
                        style={{ fontSize: 12, margin: '2px 0 0', color: 'var(--app-text-secondary)' }}
                      >
                        {msg.content}
                      </Paragraph>
                      <Text type="secondary" style={{ fontSize: 11 }}>
                        {/* ⚠️ 不用 formatDate.relative:仓库里从没 dayjs.extend(relativeTime),
                            调它运行时会抛 TypeError(见 format.ts 的既有 tsc 报错)。
                            绝对时间对排障也更有用。 */}
                        {formatDate.datetime(msg.created_at)}
                      </Text>
                    </>
                  }
                />
              </List.Item>
            )
          }}
        />
      )}
    </div>
  )

  return (
    <Popover
      content={content}
      trigger="click"
      placement="bottomRight"
      open={open}
      onOpenChange={setOpen}
      // 每次打开重新拉(见上面的 load),所以销毁隐藏时的 DOM,
      // 免得上一次的状态残留到下一次(antd 5.21 的属性名)
      destroyTooltipOnHide
    >
      {/* ⚠️ 这里**故意不套 Tooltip**:Popover 和 Tooltip 都是 overlay,
          套一起会让弹层打开时 tooltip 还跟着 hover 亮起来。弹层头部
          已经写着标题,提示是多余的。 */}
      <Badge count={unreadCount ?? 0} size="small" showZero={false} offset={[-2, 2]}>
        <Button type="text" icon={<BellOutlined />} style={BELL_STYLE} />
      </Badge>
    </Popover>
  )
}

/**
 * 图标按钮样式 —— 与 Header 里其它图标按钮(全屏/帮助)保持一致。
 * 单独抽出来而不是从 Header 传 prop:那会让 Header 为了一个子组件多一个
 * 必传参数,而这个值本身不会随 Header 的状态变。
 */
const BELL_STYLE: React.CSSProperties = {
  color: 'var(--app-text-secondary)',
}