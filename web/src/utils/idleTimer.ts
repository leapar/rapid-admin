/**
 * 空闲超时 —— N = 30min(2026-10-21 定,关闭 SRS §2.2.1 #5 的悬空委托)
 *
 * SRS 原文:"退出登录、token 过期、长时间无操作(**超时时间由技术方案确定**)
 * 后自动回到登录页"。技术方案一直没定这个 N,于是这条要求从未落地。
 *
 * ## 与"记住我"正交(最常见的误解)
 *
 * 勾了"记住我"照样 30min 掉线。**记住我只控制 token 落在 localStorage 还是
 * sessionStorage,不控制会话寿命** —— 前者管"关掉浏览器还在不在",后者管
 * "开着不动会不会被踢"。UI 与文档都要说清楚,否则用户永远觉得"记住我都勾了
 * 怎么还是被踢"。
 *
 * ## 为什么时间戳放 localStorage(多标签页共享)
 *
 * 后台管理系统经常同时开好几个 tab。如果每个 tab 各记各的空闲时间,会出现:
 * A 标签页挂着不动 → A 自己判定超时 → **把正在用的 B 标签页也踢了**。
 * 共享一个时间戳后,任一 tab 有操作,所有 tab 的计时一起续命 —— 这才是
 * "用户还在用"的意思。
 *
 * 放在 sessionStorage 就退化成每 tab 独立,上面那个坑立刻出现。
 *
 * ## 空闲预算只在"应用运行时"消耗(2026-10-28 M108)
 *
 * 技术方案 §7.7.4.15 对这一项的措辞是"**开着不动**会不会被踢"。计时器只在应用
 * 跑着的时候存在,所以**浏览器关掉的那几个小时不该算进空闲预算**。
 *
 * 以前这个时间戳是"落盘就一直在"的,于是它跨会话、跨浏览器重启地残留:
 * 昨天 18:00 登出、今天 09:00 重新打开 → 空闲计时器第一次轮询(15s)就把刚建立的
 * 会话踢掉 ⇒「记住我」(服务端 refresh token 活 30 天)实际只值 30 分钟。
 * 这与技术方案同一节里"记住我 → 浏览器关掉也在"**自相矛盾**。
 *
 * 现在时间戳是**会话的一部分**:
 *   · 会话开始(登录 / 启动恢复)时把基线对齐到现在 —— stores/user.ts 的
 *     touchIdleBaseline();
 *   · 会话结束(登出 / 401 重置)时清掉 —— LAST_ACTIVE_AT 已进
 *     utils/storage.ts 的 SESSION_KEYS。
 *
 * ## 为什么用轮询而不是一个长 setTimeout
 *
 * 浏览器会把后台标签页的 setTimeout 限流到 ≥1min 甚至完全冻结;标签页从后台
 * 切回前台时那个 30min 的定时器可能早就该触发却没触发。改成每 15s 查一次
 * 共享时间戳,判定永远基于**真实经过的时间**,不受节流影响。
 *
 * 注意:判定条件是 `now - last_active_at >= IDLE_TIMEOUT`,**没有**给后台标签页
 * 补宽限 —— 一个切回前台时已经空闲 40 分钟的标签页就该被踢,补宽限等于
 * 给了绕过手段。
 */
import { STORAGE_KEYS } from '@/constants/app'
import { storage } from '@/utils/storage'
import { useUserStore } from '@/stores/user'
import { logout as logoutApi } from '@/api/auth'
import { cancelTokenRefresh } from '@/utils/tokenScheduler'

/** 空闲多久算超时 —— 技术方案 §2.2.1 #5 的 N */
export const IDLE_TIMEOUT_MS = 30 * 60 * 1000

/** 轮询间隔。够密(能及时踢),又不至于成负担 */
const CHECK_INTERVAL_MS = 15 * 1000

/** 活动事件节流:这个间隔内的重复事件只记一次 */
const ACTIVITY_THROTTLE_MS = 5 * 1000

/** 会被当成"用户在操作"的事件 */
const ACTIVITY_EVENTS = [
  'mousemove',
  'mousedown',
  'keydown',
  'wheel',
  'touchstart',
  'scroll',
] as const

let interval: ReturnType<typeof setInterval> | null = null
let listeners: Array<[string, () => void]> = []
let lastRecordedAt = 0
/** 正在处理超时中(重入守卫,见 handleIdleTimeout) */
let handling = false

/* ====================== 时间戳读写 ====================== */

/** 读最近一次活动时间(epoch ms);没有记录过返回 0 */
export function getLastActiveAt(): number {
  return storage.get<number>(STORAGE_KEYS.LAST_ACTIVE_AT) ?? 0
}

/** 记一次"刚刚有活动"(带节流) */
function recordActivity(now: number): void {
  if (now - lastRecordedAt < ACTIVITY_THROTTLE_MS) return
  lastRecordedAt = now
  storage.set(STORAGE_KEYS.LAST_ACTIVE_AT, now)
}

/* ====================== 判定 ====================== */

/**
 * 纯判定:距上次活动是否已超过阈值。
 *
 * 抽成纯函数是为了能直接测边界(29min59s 不超时 / 30min00s 超时),
 * 不用去等真实的 30 分钟。
 */
export function isIdleExpired(lastActiveAt: number, now: number, timeoutMs = IDLE_TIMEOUT_MS): boolean {
  // lastActiveAt === 0 表示"从来没有活动记录"(例如 hydrate 之后还没动过鼠标)。
  // 这时**不**判定超时:刚打开的页面不该因为没有历史记录就被踢。
  if (lastActiveAt <= 0) return false
  return now - lastActiveAt >= timeoutMs
}

/* ====================== 到期处理 ====================== */

/**
 * 超时了:先让后端吊销,再清本地,最后跳登录页。
 *
 * 顺序有讲究 —— 调后端要用 access token,所以必须**在清会话之前**调。
 * 后端调不通(断网/后端已挂)也要照常清本地:这时 token 虽然没被吊销,
 * 但本地已经没有任何凭证留着,用户必须重新登录,这是更安全的一侧。
 *
 * ⚠️ **重入守卫**:15s 轮询可能在 `await logoutApi()` 的网络往返期间再次触发
 *   (此时 isLoggedIn 还是 true —— reset 还没执行)⇒ 重复发 /auth/logout、
 *   重复跳登录页。这里用 `handling` 挡住。
 */
export async function handleIdleTimeout(): Promise<void> {
  if (handling) return
  handling = true
  try {
    cancelTokenRefresh()
    try {
      await logoutApi()
    } catch {
      // 网络错误不阻断本地清理 —— 见上面的说明
    }
    useUserStore.getState().reset()
    if (window.location.pathname !== '/login') {
      window.location.href = buildIdleLoginUrl()
    }
  } finally {
    handling = false
  }
}

/**
 * 空闲超时后要跳的登录页 URL(导出是为了能直接测"redirect 不丢")
 *
 * ⚠️ **必须带 redirect**:被踢的人重新登录之后应该回到他刚才在看的那一页。
 *   401 路径(api/client.ts 的 handleUnauthorized)一直是这么做的;
 *   空闲路径以前只带 `?reason=idle` ⇒ 两条路径行为不一致,
 *   用户重新登录后被丢回首页,得自己再点回去。
 */
export function buildIdleLoginUrl(): string {
  const target = window.location.pathname + window.location.search
  return `/login?reason=idle&redirect=${encodeURIComponent(target)}`
}

/* ====================== 启停 ====================== */

/** 启动空闲计时。幂等,重复调用不会重复挂监听。 */
export function startIdleTimer(): void {
  if (interval) return

  const onActivity = () => recordActivity(Date.now())
  listeners = ACTIVITY_EVENTS.map((name) => {
    const fn = () => onActivity()
    window.addEventListener(name, fn, { passive: true })
    return [name, fn] as [string, () => void]
  })

  interval = setInterval(() => {
    // 未登录不计时:否则会在登录页反复触发跳转
    if (!useUserStore.getState().isLoggedIn) return
    if (isIdleExpired(getLastActiveAt(), Date.now())) {
      void handleIdleTimeout()
    }
  }, CHECK_INTERVAL_MS)
}

/** 停掉计时并摘掉全部监听。 */
export function stopIdleTimer(): void {
  if (interval) {
    clearInterval(interval)
    interval = null
  }
  for (const [name, fn] of listeners) window.removeEventListener(name, fn)
  listeners = []
  lastRecordedAt = 0
  handling = false
}