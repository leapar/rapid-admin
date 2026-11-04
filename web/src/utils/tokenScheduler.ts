/**
 * Token 自动刷新调度器(§1.2 #12 P1)
 *
 * 设计:
 *  - 主动刷新:在 access token 过期前 5 分钟调 /public/auth/refresh 续期
 *  - 被动刷新:请求返回 401 时,client.ts 调用 doRefresh() 续期并重试
 *  - 两者用同一个 doRefresh() 实现(单飞锁,避免多个请求并发触发)
 *
 * 2026-10-25 M8 —— **续期失败不再直接登出**,而是先"跨标签页找回"
 *
 * 病根:refresh token 是**单次使用**的(后端轮换,见 service/auth_impl.go)。
 * 浏览器里同一个会话可以有多份 refresh token 副本(每个标签页一份),
 * 而副本之间没有任何同步机制。于是两个标签页同时续期时,必然有一个
 * 拿着**已被消费掉**的那个 token 被拒 —— 那是**竞态**,不是掉线。
 *
 * 旧行为:续期一失败就 handleUnauthorized() → 弹"登录已过期" + 跳登录页。
 * 用户看到的是"我开了两个标签页,有一个莫名其妙被登出了"。
 *
 * 新行为:续期失败先走恢复链(recoverFromSiblingTab),全落空才登出:
 *   ① 共享存储(勾了"记住我"):兄弟标签页刷新成功后,新的 token 组已经写进
 *      **同一个**存储 → 重读一次就是它,直接采用(**不再发请求**)
 *   ② 隔离存储(没勾"记住我"):sessionStorage 按标签页隔离,本地读不到 →
 *      靠跨标签页广播要一组(utils/tokenSync.ts)
 *   ③ 都没有 → 才登出
 *
 * ⚠️ ① 里"不再发请求"是刻意的:再发一次 refresh 等于把兄弟刚拿到的
 *   refresh token 又消费掉,两个标签页从此互相"抢",最后谁都续不上。
 */
import { useUserStore, getToken, getRefreshToken, getPersistedUserId } from '@/stores/user'
import { refreshToken } from '@/api/auth'
import { handleUnauthorized } from '@/api/client'
import { TokenSync, createBroadcastTokenChannel } from '@/utils/tokenSync'
import type { TokenPayload } from '@/utils/tokenSync'

/** 提前刷新的安全余量(5 分钟) */
const REFRESH_AHEAD_MS = 5 * 60 * 1000

/** 最小刷新间隔(避免两个 refresh 几乎同时触发,30 秒内不重复刷) */
const MIN_REFRESH_INTERVAL_MS = 30 * 1000

/** 等兄弟标签页回应"要一组 token"的最长时间(毫秒) */
const SIBLING_RESPONSE_TIMEOUT_MS = 800

let timer: ReturnType<typeof setTimeout> | null = null
let inFlight: Promise<boolean> | null = null
let lastRefreshAt = 0
/**
 * 上一次调度时对应的 access token —— 用来发现"token 被别的标签页换过了"
 *
 * 共享 localStorage 时,两个标签页的刷新时刻几乎相同(都按同一份到期时刻
 * 排的)。谁先发谁成功,后发的那个拿着已被消费的 token 被拒。
 * 有了这个快照,后发的那个能在发请求**之前**发现"token 已经换过了",
 * 直接重排即可 —— 竞态压根不发生。
 */
let scheduledToken: string | null = null

/**
 * 实际调 refresh API 并写新 token
 * 单飞锁:in-flight 时,其他调用 await 同一个 promise
 * @returns true = 刷新成功, false = 失败(已触发登出)
 */
export async function doRefresh(): Promise<boolean> {
  if (inFlight) return inFlight
  const refresh_token = getRefreshToken()
  if (!refresh_token) {
    handleUnauthorized()
    return false
  }
  inFlight = (async () => {
    try {
      return await performRefresh(refresh_token)
    } catch (err) {
      // 续期失败 —— **先别登出**,试试能不能从兄弟标签页把 token 找回来
      console.warn('[tokenScheduler] refresh failed', err)
      const recovered = await recoverFromSiblingTab(refresh_token)
      if (recovered) {
        console.warn('[tokenScheduler] 已从其它标签页恢复会话(续期竞态,非掉线)')
        return true
      }
      handleUnauthorized()
      return false
    } finally {
      inFlight = null
    }
  })()
  return inFlight
}

/**
 * 发一次续期请求,成功就写盘 + 广播
 *
 * 失败时抛出去(由 doRefresh 的 catch 走恢复链)——
 * 只有"响应缺 token 字段"这种契约漂移是就地失败:那不是竞态,
 * 跨标签页找也找不回一个没有 token 的响应。
 */
async function performRefresh(usedToken: string): Promise<boolean> {
  const resp = await refreshToken(usedToken)
  // access token 的字段名是 `token`,不是 `access_token` —— 后端
  // service.AuthTokens.Token 的 json tag。写错会让续期"成功但拿不到
  // token",下一次请求发出 `Bearer undefined`,表现成续期失败把人踢下线。
  const { token: new_token, refresh_token: new_refresh, expires_in, refresh_expires_in } = resp
  if (!new_token) {
    // 契约漂移的兜底:没有 token 就别装成功,直接当失败走登出
    console.warn('[tokenScheduler] refresh 响应缺 token 字段', resp)
    handleUnauthorized()
    return false
  }
  // ⚠️ 2026-10-21:后端现在**轮换** refresh token(W7),同一个 refresh 只能用
  // 一次。所以 new_refresh 必须落盘 —— 不写的话下次续期会带着一个已被
  // 消费掉的旧 refresh 去,后端按"单次使用"拒掉,表现为**第一次续期成功、
  // 之后就一直失败**。
  //
  // 写入统一交给 store.setTokens():后端锚点(activeBackend)由它算,
  // TTL 全取服务端下发值。2026-10-21 之前这里是四行 await import() +
  // 手写 "localStorage 有 refresh_token 就写 local" 的判断 ——
  // 那段判断与 stores/user 里的规则是两份独立实现,改一处忘另一处
  // 就会写到错的后端(不报错,只是下次读不到)。
  useUserStore.getState().setTokens({
    accessToken: new_token,
    expiresIn: expires_in,
    // 后端没轮换(老版本)时 new_refresh 为空,保持原值不动
    refreshToken: new_refresh && new_refresh !== usedToken ? new_refresh : undefined,
    refreshExpiresIn: refresh_expires_in,
  })
  // 更新过期时间 + 调度下次
  if (typeof expires_in === 'number' && expires_in > 0) {
    scheduleTokenRefresh(expires_in * 1000)
  }
  lastRefreshAt = Date.now()
  // 广播新的一组 —— 让兄弟标签页立刻拿到,别等它们自己撞上"已消费"
  publishLocalTokens()
  return true
}

/**
 * 续期失败后的恢复链(2026-10-25 M8)
 *
 * @param failedToken 刚被拒的那个 refresh token(已消费,谁拿着都换不出新的)
 */
async function recoverFromSiblingTab(failedToken: string): Promise<boolean> {
  // ① 共享存储:另一个标签页刷新成功后,新的一组已经写进同一个存储。
  //    重读一次就是它 —— 本地不用再写(存储里已经是新的了),只需重排调度。
  if (adoptFromSharedStorage(failedToken)) return true

  // ② 隔离存储:本地读不到兄弟的结果,只能靠跨标签页广播
  const sync = getTokenSync()
  if (sync.available) {
    const payload = await sync.request(failedToken, SIBLING_RESPONSE_TIMEOUT_MS)
    if (payload) {
      applyPayload(payload)
      return true
    }
  }
  return false
}

/**
 * 从共享存储里捡起兄弟标签页换新的那组 token
 *
 * 成立条件(三条都要满足):
 *   1. 存储里的 refresh token 换过了(否则兄弟还没刷,没什么可捡)
 *   2. 存储里这一份会话**还是本标签页内存里那个用户**
 *      —— 另一个标签页可能登出后换了个账号登录,那不是"我的会话"
 *   3. access token 也在(它跟 refresh token 一起写盘)
 *
 * 注意这里**不写盘**:新的一组已经在存储里了,再写一遍只会让 TTL 漂移。
 */
function adoptFromSharedStorage(failedToken: string): boolean {
  const current = getRefreshToken()
  if (!current || current === failedToken) return false

  const localUserId = useUserStore.getState().userInfo?.id
  if (!localUserId || getPersistedUserId() !== localUserId) return false

  const accessToken = getToken()
  if (!accessToken) return false

  // 新的一组已经在存储里 —— 只需把调度对齐到新的到期时刻
  rescheduleFromStore()
  return true
}

/** 把一组远端 token 落到本地(跨标签页广播来的) */
function applyPayload(payload: TokenPayload): void {
  useUserStore.getState().setTokens({
    accessToken: payload.accessToken,
    expiresIn: payload.expiresIn,
    refreshToken: payload.refreshToken,
    refreshExpiresIn: payload.refreshExpiresIn,
  })
  rescheduleFromStore()
}

/** 按存储里的 access token 到期时刻重排刷新调度 */
function rescheduleFromStore(): void {
  const expireAt = useUserStore.getState().getAccessTokenExpiresAt()
  if (expireAt) scheduleTokenRefresh(expireAt - Date.now())
}

/** 把本标签页当前持有的 token 组广播出去(兄弟标签页据此更新) */
function publishLocalTokens(): void {
  const sync = getTokenSync()
  if (!sync.available) return
  const payload = buildLocalPayload()
  if (payload) sync.publish(payload)
}

/* ====================== 跨标签页同步的接线 ====================== */

/**
 * 组装本标签页当前持有的 token 组
 *
 * ⚠️ userId 必填:它挡住"同一 origin 下另一个用户登录在别的标签页"
 *   —— 吃下别人的 token 就是会话串号(见 TokenSync.acceptable)。
 */
export function buildLocalPayload(): TokenPayload | null {
  const store = useUserStore.getState()
  const userId = store.userInfo?.id
  if (!userId) return null
  const accessToken = getToken()
  const refreshToken = getRefreshToken()
  if (!accessToken || !refreshToken) return null
  const expiresAt = store.getAccessTokenExpiresAt() ?? 0
  const refreshExpiresAt = store.getRefreshTokenExpiresAt() ?? 0
  return {
    userId,
    accessToken,
    refreshToken,
    expiresAt,
    expiresIn: expiresAt ? Math.max(0, Math.round((expiresAt - Date.now()) / 1000)) : undefined,
    refreshExpiresIn: refreshExpiresAt
      ? Math.max(0, Math.round((refreshExpiresAt - Date.now()) / 1000))
      : undefined,
  }
}

let syncInstance: TokenSync | null = null

/**
 * 取全局同步器(首次调用时创建)
 *
 * ⚠️ 必须在**任何续期可能发生之前**就建好 —— 订阅晚了会错过兄弟标签页
 *   发来的"新 token 组"广播(消息不排队)。main.tsx 启动时调一次。
 */
export function getTokenSync(): TokenSync {
  if (!syncInstance) {
    syncInstance = new TokenSync(createBroadcastTokenChannel(), {
      getUserId: () => useUserStore.getState().userInfo?.id ?? null,
      getLocalRefreshToken: () => getRefreshToken(),
      getLocal: () => buildLocalPayload(),
      // 收到"可采用"的一组 → 写盘 + 重排调度
      onTokens: (payload) => applyPayload(payload),
    })
  }
  return syncInstance
}

/**
 * 替换全局同步器(测试注入用)
 *
 * ⚠️ 传 null 表示"恢复成默认实现"而不是"禁用同步" ——
 *   禁用请用 `new TokenSync(null, opts)` 自己造一个。
 */
export function setTokenSyncForTest(sync: TokenSync | null): void {
  syncInstance?.dispose()
  syncInstance = sync
}

/**
 * 在 access token 过期前 REFRESH_AHEAD_MS 调度一次刷新
 * - 已有 timer → 取消重排
 * - expiresInMs ≤ 0:取消
 * - 与 lastRefreshAt 间隔 < MIN_REFRESH_INTERVAL_MS:跳过(避免刷新风暴)
 */
export function scheduleTokenRefresh(expiresInMs: number): void {
  if (timer) {
    clearTimeout(timer)
    timer = null
  }
  if (!Number.isFinite(expiresInMs) || expiresInMs <= 0) return

  const delay = Math.max(expiresInMs - REFRESH_AHEAD_MS, 1000)
  const sinceLast = Date.now() - lastRefreshAt
  if (sinceLast < MIN_REFRESH_INTERVAL_MS) {
    // 30s 内刚刷过,不做预防性调度
    return
  }
  // 记下这次是**为哪个 token** 排的 —— 触发时据此判断"token 是不是被换过了"
  scheduledToken = getToken()

  timer = setTimeout(() => {
    timer = null
    /**
     * ⚠️ 2026-10-25 M8:**先看 token 换没换过,再决定发不发请求**
     *
     * 共享 localStorage 时,两个标签页按同一份到期时刻排刷新,触发时刻几乎
     * 一样。先发的人成功,后发的人拿着已被消费的 token 被拒 → 被踢下线。
     * 这里发现 token 已经换过(兄弟刷过了)→ 直接按新的到期时刻重排,
     * **不发请求** —— 既省一次往返,也避免"两边互相消费对方的 refresh"。
     */
    if (scheduledToken && getToken() !== scheduledToken) {
      scheduledToken = null
      rescheduleFromStore()
      return
    }
    // 自动触发:成功就继续 schedule,失败由 doRefresh 内部处理(已登出)
    doRefresh().then((ok) => {
      if (ok) {
        const store = useUserStore.getState()
        const expireAt = store.getAccessTokenExpiresAt()
        if (expireAt) {
          scheduleTokenRefresh(expireAt - Date.now())
        }
      }
    })
  }, delay)
}

/** 登出 / 401 时清理(取消未触发的 timer) */
export function cancelTokenRefresh(): void {
  if (timer) {
    clearTimeout(timer)
    timer = null
  }
  inFlight = null
  lastRefreshAt = 0
  scheduledToken = null
}

/**
 * 应用启动时如果已经登录,恢复调度
 * - 启动时机:hydrateUserStore() 之后
 * - 如果 access token 已过期或剩余 < 0,立即触发一次 doRefresh
 */
export function bootstrapTokenRefresh(): void {
  const store = useUserStore.getState()
  if (!store.isLoggedIn) return
  const expireAt = store.getAccessTokenExpiresAt()
  if (!expireAt) return
  const remaining = expireAt - Date.now()
  if (remaining <= 0) {
    // 已经过期,主动刷
    doRefresh()
  } else {
    scheduleTokenRefresh(remaining)
  }
}
