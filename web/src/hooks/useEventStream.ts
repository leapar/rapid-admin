/**
 * useEventStream —— 通用事件推送通道(前端唯一入口)—— M72
 *
 * 取代 `usePermissionStream`(已删)。一条 SSE 连接承载全部 topic:
 *
 * ```
 *   GET /rapid-admin/v1/events   ← EventSource(带 ?access_token=)
 *     event: connected      服务端已订阅,带回了本次订阅的 topic 列表
 *     event: degraded       Redis 不可用 → 主动断开,让 EventSource 按
 *                           服务端给的 retry:60000 慢速重试
 *     event: lag            订阅缓冲区溢出,N 条事件被丢弃 → 让页面重新拉
 *     event: perm_changed   权限变了 → 重拉权限 + 重建连接(重新判订阅范围)
 *     event: job.finished   定时任务跑完
 *     event: job.failed     定时任务失败
 *     event: inbox.unread   站内信未读数变了 → 重拉 unread-count
 *     event: approval.todo  新待办 → 重拉待办角标
 *     event: approval.done  待办处理完 → 重拉待办角标
 *     event: dict.changed   字典组被改了 → 作废该组的本地缓存
 * ```
 *
 * ## 用法
 *
 * ```ts
 * const { subscribe } = useEventStream()
 * useEffect(() => subscribe('job.finished', () => load()), [load])
 * ```
 *
 * 组件挂载即开连接、卸载即退订;最后一个消费者卸载时连接关闭
 * (BasicLayout 常驻,所以实际是它持有连接)。
 *
 * ## ⚠️ 为什么必须用 ?access_token=
 *
 * `EventSource` 是 W3C 标准 API,**不能设置自定义 Header**,Bearer 走不通。
 * 而 `middleware/sseauth.go` 的三级兜底里,Cookie 那一级是**死代码** ——
 * 全仓(前后端)grep 不到任何一处 `SetCookie` / `document.cookie`,
 * 也就是说 Cookie 永远不存在,只有 `?access_token=` 这条路能走通。
 *
 * 代价:token 会进 nginx access log(query string)。这是 SSE + Bearer 的
 * 固有代价(见 sseauth.go 头部注释),本期接受;真要消掉得换成
 * 「先 POST 换一个 30s 一次性 ticket,再拿 ticket 连」,列入后续项。
 *
 * ## ⚠️ 订阅范围由服务端决定,前端不能自选
 *
 * 连上之后服务端按 RBAC 决定给你订哪些 topic(见 handler/v1/event.go
 * subscribableTopics)。所以**权限变了必须重建连接** —— 老连接上的
 * topic 是建连那一刻的快照,不会自己变。
 */
import { useEffect } from 'react'
import { create } from 'zustand'
import { getToken } from '@/stores/user'
import { usePermissionStore } from '@/stores/permission'
import { useUIStore } from '@/stores/ui'
import { useDictStore } from '@/stores/dict'
import { inboxApi } from '@/api/inbox'
import { approvalApi } from '@/api/approval'

// === 事件名常量 ===
//
// ⚠️ 与 server/internal/pkg/push/event.go 一一对应。改后端要同步改这里,
//    两边没有共享的编译期约束,只能靠这行注释 + 心智模型兜着。
export const EVENT = {
  connected: 'connected',
  degraded: 'degraded',
  lag: 'lag',
  permChanged: 'perm_changed',
  jobFinished: 'job.finished',
  jobFailed: 'job.failed',
  inboxUnread: 'inbox.unread',
  // 审批 / 待办(M88 落地生产端)—— 服务端按 `approval:read` 资源码放行
  // TopicApproval(M83-d 起这个资源码就在种子里了,之前是**没有生产者**,
  // 不是没有订阅者)。⚠️ **事件名从 M73 起就没改过,也不许改** ——
  // 改名等于让所有已发版的前端收不到事件。
  approvalTodo: 'approval.todo',
  approvalDone: 'approval.done',
  // 字典变更(M94 落地生产端)—— 服务端按 `dict:list` 资源码放行。
  // payload 只有 `group_code`(不带字典内容),收到就把该组的本地缓存作废。
  // ⚠️ **事件名不许改** —— 改名等于让所有已发版的前端收不到这个信号,
  // 而症状是"字典改了不生效",不报错、很难归因。
  dictChanged: 'dict.changed',
} as const

export type EventName = (typeof EVENT)[keyof typeof EVENT]

/** 事件负载。服务端一律走 JSON,所以这里一律是对象(不会是裸字符串)。 */
export type EventPayload = Record<string, unknown>

export type EventHandlerFn = (payload: EventPayload) => void

/** idle=没连(未登录/无消费者) / connecting=连或重连中 / open=通 / degraded=服务端降级 */
export type StreamStatus = 'idle' | 'connecting' | 'open' | 'degraded'

// === 常量 ===

/**
 * 兜底重连间隔(毫秒)。
 *
 * 只在 **EventSource 原生放弃重连** 时用(readyState === CLOSED,
 * 即响应不是 200 或 Content-Type 不对,比如 token 过期后的 401)。
 * 正常断线一律走原生重连 —— 它会遵守服务端下发的 `retry:`,
 * 我们插手反而会把服务端的退避策略顶掉。
 */
const RETRY_MS = 3000

/** 权限变更后重建连接的延迟:一次改角色会连发多条 perm_changed,等它们落定再连 */
const PERM_RECONNECT_MS = 500

/** inbox.unread 的合并窗口:NotifyBatch 可能给同一用户连推几条 */
const UNREAD_DEBOUNCE_MS = 500

/**
 * approval.todo / approval.done 的合并窗口。
 *
 * ⚠️ **为什么要合并** —— 一次送审会给**每一个**审批人各发一条
 * `approval.todo`(§9.2 的表格:「submit / 重提,逐个审批人」)。
 * 会签关卡三个人的时候,三条事件会在几十毫秒内到达,不合并就是
 * 三次 `GET /system/approvals?box=todo`。
 */
const APPROVAL_DEBOUNCE_MS = 500

// === 状态 store ===
//
// 为什么 zustand 而不是 useState:连接是**模块级单例**(跨组件共享),
// 而 status / unreadCount 要能被任意组件读到。用 useState 就得层层透传,
// 或者搞 Context —— 都不如一个轻量 store 直接。

interface EventStreamState {
  status: StreamStatus
  /** 服务端在 connected 事件里回报的实际订阅 topic —— 排障时最有用的一条 */
  topics: string[]
  /** 未读站内信数;null = 不知道(未登录 / 无 inbox:read 权限 / 请求失败) */
  unreadCount: number | null
  /** 待我审批的条数;null = 不知道(未登录 / 无 approval:read 权限 / 请求失败) */
  approvalTodoCount: number | null
}

interface EventStreamActions {
  setStatus: (s: StreamStatus) => void
  setTopics: (t: string[]) => void
  setUnreadCount: (n: number | null) => void
  setApprovalTodoCount: (n: number | null) => void
  /**
   * 重拉未读数。
   *
   * ⚠️ **故意不消费 payload 里的 delta**:后端 `inbox.publishUnread` 里
   * `Delta` 恒为 +1 —— 连"标已读"也推 +1(见该函数注释:delta 只用于让红点
   * "动一下",真实值以 unread-count 为准)。前端要是拿 delta 去加减,
   * 标已读之后角标反而会涨。所以这里一律重拉。
   *
   * 静默失败:没 inbox:read 的人拿到 403,不该弹 toast,把值留成 null,
   * 角标自然不显示。
   */
  refreshUnread: () => Promise<void>
  /**
   * 重拉待我审批的条数(M88)。
   *
   * ⚠️ **同样是一律重拉,不做 +1 / -1** —— 理由跟 `refreshUnread` 一样,
   * 但这里更硬:设计稿 §9.1 硬要求 3 写的是「`approval.todo` 收到后 +1,
   * `approval.done` 收到后 -1」,**那个算法在打回时是错的** ——
   * `rolled_back` 之后单子回到发起人手里,发起人的待办**多了一条**,
   * 而事件只带一个 `action`,没有说"该给谁 +1"。
   *
   * 而且「谁该 +1」这件事前端根本判不出来:事件是**广播**的,
   * 同一个 topic 上既有"分给我的"也有"我发起的"。`payload.assignee_id`
   * 只在 `assigned` 时非零,`approved` / `rejected` / `rolled_back` 都是空的
   * —— 拿不到"这条该动谁的角标"。
   *
   * 所以:收到事件 → 重拉 `GET /system/approvals?box=todo&size=1` → 读 total。
   * 一次多拉一次列表,换来"角标永远等于真实待办数"这个不变式。
   */
  refreshApprovalTodo: () => Promise<void>
}

const useEventStreamStore = create<EventStreamState & EventStreamActions>((set) => ({
  status: 'idle',
  topics: [],
  unreadCount: null,
  approvalTodoCount: null,

  setStatus: (status) => set({ status }),
  setTopics: (topics) => set({ topics }),
  setUnreadCount: (unreadCount) => set({ unreadCount }),
  setApprovalTodoCount: (approvalTodoCount) => set({ approvalTodoCount }),

  refreshUnread: async () => {
    try {
      const res = await inboxApi.unreadCount()
      const n = Number(res?.unread)
      set({ unreadCount: Number.isFinite(n) ? n : null })
    } catch {
      // 403(无 inbox:read) / 网络错误:不知道就不知道,别弹 toast
      set({ unreadCount: null })
    }
  },

  refreshApprovalTodo: async () => {
    try {
      // ⚠️ **size=1 —— 只要 total,不要列表** —— 角标要的是一个数字。
      const res = await approvalApi.list({ box: 'todo', page: 1, page_size: 1 })
      const n = Number(res?.total)
      set({ approvalTodoCount: Number.isFinite(n) ? n : null })
    } catch {
      // 403(无 approval:read) / 网络错误:不知道就不知道,别弹 toast。
      // 留成 null → 侧边栏那个角标自然不显示。
      set({ approvalTodoCount: null })
    }
  },
}))

// === 订阅者注册表 ===
//
// 事件名 → 回调集合。模块级,不是 React state:注册/注销发生在组件
// effect 里,不该触发任何重渲染。

const handlers = new Map<string, Set<EventHandlerFn>>()

/**
 * 订阅一个事件。返回退订函数。
 *
 * 用法必须放进 useEffect —— 裸调会每次渲染都注册一个新回调:
 *
 * ```ts
 * useEffect(() => subscribe('job.finished', onDone), [onDone])
 * ```
 */
export function subscribe(event: EventName | string, fn: EventHandlerFn): () => void {
  let set = handlers.get(event)
  if (!set) {
    set = new Set()
    handlers.set(event, set)
  }
  const bucket = set
  bucket.add(fn)

  return () => {
    bucket.delete(fn)
    // 空桶顺手删掉,免得 handlers 无限增长
    // (连接名册会随路由不断出现新名字,长会话下这是真实的泄漏路径)
    if (bucket.size === 0) handlers.delete(event)
  }
}

/**
 * 派发事件。
 *
 * ⚠️ 两个防御:
 *   1. 先拷贝再遍历 —— 回调里退订(常见于回调触发 setState 导致卸载)
 *      不该让遍历跳格或崩;
 *   2. 单个回调抛错不牵连其余回调 —— 否则一个页面写坏的 handler
 *      会让**所有**页面都收不到事件,而且报错现场离真正的原因很远。
 */
function dispatch(event: string, payload: EventPayload) {
  const set = handlers.get(event)
  if (!set) return
  for (const fn of Array.from(set)) {
    try {
      fn(payload)
    } catch (err) {
      // eslint-disable-next-line no-console
      console.error(`[events] handler "${event}" 抛错,已隔离`, err)
    }
  }
}

// === 连接单例 ===

let es: EventSource | null = null
let retryTimer: ReturnType<typeof setTimeout> | null = null
let releaseTimer: ReturnType<typeof setTimeout> | null = null
let unreadTimer: ReturnType<typeof setTimeout> | null = null
let approvalTimer: ReturnType<typeof setTimeout> | null = null
/** 当前"消费者"数量(调用 useEventStream 的组件实例数) */
let consumers = 0

/**
 * 合并窗口内只重拉一次待办数。
 *
 * ⚠️ **清掉旧定时器再排新的** —— 跟 inbox 的合并窗口同一套写法。
 * 不清的话连推三条会排三个定时器,等于重拉三次。
 */
function scheduleApprovalRefresh() {
  if (approvalTimer) clearTimeout(approvalTimer)
  approvalTimer = setTimeout(() => {
    approvalTimer = null
    void useEventStreamStore.getState().refreshApprovalTodo()
  }, APPROVAL_DEBOUNCE_MS)
}

function buildURL(): string {
  // ⚠️ 每次重连都重读 token:token 会被 tokenScheduler 静默刷新,
  // 拿着旧 token 的 URL 重连只会再吃一次 401。
  const token = getToken()
  if (!token) return ''
  const base = (import.meta.env.VITE_API_BASE as string) || '/rapid-admin/v1'
  return `${base}/events?access_token=${encodeURIComponent(token)}`
}

function parseData(e: MessageEvent): EventPayload {
  try {
    const v = JSON.parse(e.data)
    // 服务端 writeSSE 一律 json.Marshal,正常一定是对象。
    // 这里是防御:万一 payload 是字符串/数组,下面的 handler 会读到 undefined。
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as EventPayload) : {}
  } catch {
    return {}
  }
}

function closeSocket() {
  if (retryTimer) {
    clearTimeout(retryTimer)
    retryTimer = null
  }
  if (es) {
    es.close()
    es = null
  }
}

function scheduleReconnect(delay: number) {
  if (consumers <= 0) return
  if (retryTimer) clearTimeout(retryTimer)
  retryTimer = setTimeout(() => {
    retryTimer = null
    open()
  }, delay)
}

function open() {
  if (consumers <= 0) return
  // 先拆旧连接:open 可能被重连定时器 / perm_changed / refCount 归零后重开
  // 这三条路径触发,不拆会漏连接
  closeSocket()

  const url = buildURL()
  if (!url) {
    // 没 token = 没登录。BasicLayout 一般已经卸载了,这里兜底不连。
    useEventStreamStore.getState().setStatus('idle')
    return
  }

  useEventStreamStore.getState().setStatus('connecting')

  let socket: EventSource
  try {
    socket = new EventSource(url, { withCredentials: true })
  } catch {
    // 构造就抛(极罕见:URL 非法 / 浏览器不支持)。兜底重试,别静默死掉。
    scheduleReconnect(RETRY_MS)
    return
  }
  es = socket

  socket.addEventListener(EVENT.connected, (e) => {
    const p = parseData(e as MessageEvent)
    useEventStreamStore.getState().setStatus('open')
    useEventStreamStore.getState().setTopics(
      Array.isArray(p.topics) ? p.topics.map(String) : [],
    )
    // 连上了才拉未读数 —— 顺带把"上次会话残留的旧值"校准掉
    void useEventStreamStore.getState().refreshUnread()
    // ⚠️ **待办数也一样要校准** —— 连接建立之前发生的审批事件收不到
    //   (事件是"状态变了"的广播,不是"当前状态"),所以首次连上必须拉一次。
    void useEventStreamStore.getState().refreshApprovalTodo()
    dispatch(EVENT.connected, p)
  })

  socket.addEventListener(EVENT.degraded, (e) => {
    const p = parseData(e as MessageEvent)
    // ⚠️ 只改状态,**不要**自己重连:服务端紧接着下发 retry:60000 并主动
    // 断开,原生 EventSource 会按这个间隔重连。我们要是插一脚 3s 重连,
    // 等于把一个已经挂掉的 Redis 打成风暴 —— 降级的意义就反了。
    useEventStreamStore.getState().setStatus('degraded')
    // eslint-disable-next-line no-console
    console.warn('[events] 服务端降级(Redis 不可用),当前非实时', p)
    dispatch(EVENT.degraded, p)
  })

  socket.addEventListener(EVENT.lag, (e) => {
    const p = parseData(e as MessageEvent)
    // ⚠️ 这是"你可能已经看到过期数据了"的信号,不是可忽略的噪音。
    // 订阅方(job 列表 / 未读角标)收到后应该重新拉一次。
    //
    // ⚠️ **待办数在这里必须重拉** —— 丢掉的正是"待办变了"的事件,
    //   角标会一直停在错的值上,而且**没有任何报错**。
    //   (未读数在这一处没有跟着重拉,那是既有行为,不在 M88 的范围里。)
    void useEventStreamStore.getState().refreshApprovalTodo()
    // eslint-disable-next-line no-console
    console.warn('[events] 事件积压被丢弃,已丢', p.dropped, '条 —— 建议重拉数据', p)
    dispatch(EVENT.lag, p)
  })

  socket.addEventListener(EVENT.permChanged, (e) => {
    const p = parseData(e as MessageEvent)
    void onPermChanged(p)
    dispatch(EVENT.permChanged, p)
  })

  // job / inbox 事件统一转发(具体页面自己订阅)
  socket.addEventListener(EVENT.jobFinished, (e) => dispatch(EVENT.jobFinished, parseData(e as MessageEvent)))
  socket.addEventListener(EVENT.jobFailed, (e) => dispatch(EVENT.jobFailed, parseData(e as MessageEvent)))

  socket.addEventListener(EVENT.inboxUnread, (e) => {
    const p = parseData(e as MessageEvent)
    // 合并窗口:NotifyBatch 会给同一用户连推;合并成一次重拉
    if (unreadTimer) clearTimeout(unreadTimer)
    unreadTimer = setTimeout(() => {
      unreadTimer = null
      void useEventStreamStore.getState().refreshUnread()
    }, UNREAD_DEBOUNCE_MS)
    dispatch(EVENT.inboxUnread, p)
  })

  // ⚠️ **两个审批事件都只是"重拉待办数"的信号** —— 不用 payload 里的
  //   action 做 +1 / -1(理由见 store 里 refreshApprovalTodo 的注释)。
  //   两个事件共用一个合并窗口:一次送审会连发多条 assigned,
  //   一条 approved 后面也可能跟着别的关卡带来的 assigned。
  socket.addEventListener(EVENT.approvalTodo, (e) => {
    const p = parseData(e as MessageEvent)
    scheduleApprovalRefresh()
    dispatch(EVENT.approvalTodo, p)
  })

  socket.addEventListener(EVENT.approvalDone, (e) => {
    const p = parseData(e as MessageEvent)
    scheduleApprovalRefresh()
    dispatch(EVENT.approvalDone, p)
  })

  // ⚠️ **字典变更(M94)—— 收到就作废本地缓存**。
  //   事件只带 `group_code`,不带字典内容(服务端那边是"推信号,不推数据"),
  //   所以这里**不**拿 payload 里的值去改本地字典 —— 改了就等于在前端
  //   造出第二份数据契约,两边会漂。正确做法是丢掉缓存、让 useDict 重拉。
  socket.addEventListener(EVENT.dictChanged, (e) => {
    const p = parseData(e as MessageEvent)
    onDictChanged(p)
    dispatch(EVENT.dictChanged, p)
  })

  socket.onerror = () => {
    // readyState === CONNECTING:原生正在重连(正常断线 / 降级后重连)——别插手
    // readyState === CLOSED:原生**放弃**了(非 200 / Content-Type 不对,典型是 401),
    //                这时只能我们自己做重连(每次都重读 token,已过期的 token 会被换掉)
    if (socket.readyState === EventSource.CLOSED) {
      scheduleReconnect(RETRY_MS)
      return
    }
    useEventStreamStore.getState().setStatus('connecting')
  }
}

/**
 * 权限变更:重拉权限 + **重建连接**。
 *
 * 重建不是可选优化 —— topic 是服务端在建连那一刻按 RBAC 算出来的快照,
 * 权限变了之后老连接上仍挂着已经不属于自己的 topic(比如刚被撤销了
 * job:read 的用户还能继续收 job 事件)。只有重连才能让服务端重判。
 *
 * 窗口:变更到重连之间(约 PERM_RECONNECT_MS),老连接上的那点信号仍然会到。
 * 信号本身不含业务数据,窗口只有半秒,可接受。
 */
async function onPermChanged(payload: EventPayload) {
  const { fetchAccessible } = usePermissionStore.getState()
  const { addLoading, removeLoading } = useUIStore.getState()
  addLoading()
  try {
    await fetchAccessible()
  } catch {
    // 拉权限失败:不清登录态(http 拦截器会管 401),继续等重连
  } finally {
    removeLoading()
  }
  // eslint-disable-next-line no-console
  console.info('[events] 权限已变更,重建连接', payload)
  scheduleReconnect(PERM_RECONNECT_MS)
}

/**
 * 字典变更(M94)—— 作废该组的本地缓存。
 *
 * ⚠️ **只作废,不重拉** —— 重拉由 `useDict` 的 effect 负责(它监着
 * `lastFetchAt`,一变就重拉)。这里要是自己发请求,就会和 effect 的那次
 * 重拉撞车,同一个组发两遍请求。
 *
 * ⚠️ **payload 只认 `group_code` 这一个字段** —— 服务端刻意不带字典内容
 * (见 push.DictChangedPayload 的注释:推的是信号,不是数据)。
 * 前端要是照着 payload 改本地字典,就成了第二份数据契约,两边必然漂。
 *
 * ⚠️ **payload 形状不对时静默忽略** —— 事件是"尽力而为"的信号:
 * 拿不到 group_code 就不知道该作废哪一组,此时静默比"清空整个缓存"好
 * (清空会让所有页面的下拉同时空掉)。
 */
function onDictChanged(payload: EventPayload) {
  const groupCode = payload?.group_code
  if (typeof groupCode !== 'string' || !groupCode) return
  useDictStore.getState().invalidate(groupCode)
}

// === React 绑定 ===

/**
 * useEventStream —— 事件通道的唯一入口。
 *
 * 挂载即订阅连接、卸载即释放;返回值全部是**引用稳定**的
 * (actions 是模块级函数,selector 拿到的是 store 里的原值),
 * 所以可以放心放进 useEffect 依赖数组。
 *
 * ```ts
 * const { subscribe, unreadCount } = useEventStream()
 * useEffect(() => subscribe('inbox.unread', onUnread), [onUnread])
 * ```
 */
export function useEventStream() {
  useEffect(() => {
    consumers += 1
    if (consumers === 1) open()

    return () => {
      consumers -= 1
      if (consumers > 0) return
      consumers = 0
      // ⚠️ 延迟一拍再关:React 18 StrictMode 下 effect 会 mount→unmount→mount,
      // 同步关闭会让开发环境每次进页面都白白断开重连一次。
      // 生产不会双调用,但这一拍延迟没有任何代价。
      if (releaseTimer) clearTimeout(releaseTimer)
      releaseTimer = setTimeout(() => {
        releaseTimer = null
        if (consumers > 0) return
        closeSocket()
        if (unreadTimer) {
          clearTimeout(unreadTimer)
          unreadTimer = null
        }
        if (approvalTimer) {
          clearTimeout(approvalTimer)
          approvalTimer = null
        }
        useEventStreamStore.setState({
          status: 'idle',
          topics: [],
          unreadCount: null,
          approvalTodoCount: null,
        })
      }, 0)
    }
  }, [])

  const status = useEventStreamStore((s) => s.status)
  const topics = useEventStreamStore((s) => s.topics)
  const unreadCount = useEventStreamStore((s) => s.unreadCount)
  const approvalTodoCount = useEventStreamStore((s) => s.approvalTodoCount)

  return {
    subscribe,
    status,
    topics,
    unreadCount,
    approvalTodoCount,
    refreshUnread: useEventStreamStore.getState().refreshUnread,
    refreshApprovalTodo: useEventStreamStore.getState().refreshApprovalTodo,
  }
}

/** 页面里手动触发一次未读数刷新(例如标已读之后) */
export function refreshUnread(): Promise<void> {
  return useEventStreamStore.getState().refreshUnread()
}

/** 页面里手动触发一次待办数刷新(例如处理完一条待办之后) */
export function refreshApprovalTodo(): Promise<void> {
  return useEventStreamStore.getState().refreshApprovalTodo()
}

/**
 * 只读待办数,**不建立 SSE 连接**。
 *
 * ⚠️ **侧边栏用这个而不是 `useEventStream()`** —— `useEventStream()`
 *   挂载即 `consumers += 1` 并开连接。侧边栏只是**读一个数**,
 *   让它也当连接持有者的话,连接生命周期就跟着侧边栏的挂载走了
 *   (侧边栏折叠 / 换布局可能重新挂载)。
 *
 *   连接的持有者应该是**常驻的 BasicLayout**。
 */
export function useApprovalTodoCount(): number | null {
  return useEventStreamStore((s) => s.approvalTodoCount)
}