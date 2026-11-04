/**
 * 用户 / 身份 Store
 *
 * 持久化策略("记住我"**只**决定 token 落在哪个后端,不改变服务端 TTL):
 *   - 勾选   → 写 localStorage (storage),浏览器关掉也保留
 *   - 未勾   → 写 sessionStorage (session),关掉标签页即清空
 *   - 启动时 hydrateUserStore() 自动恢复
 *
 * ## 不变式:REFRESH_TOKEN 恰好存在于一个后端
 *
 * 所有 token 读写都走 activeBackend()(见 utils/storage.ts),它以 refresh
 * token 的落点为准。**任何时候都不要"两个后端都读一遍"来读 token** —— 那正是
 * "记住我勾了好像没实现"的成因(不勾的登录会被上一次残留的 localStorage
 * token 顶掉,效果撑不过一次 reload)。
 *
 * 写入路径遵守"**先清后写**":换持久化策略时先把另一个后端的会话键全清掉,
 * 否则会留下两份,靠 activeBackend 的平局规则去猜 —— 猜错就是静默的会话泄漏。
 *
 * token / refresh token / currentIdentity 各自单独 key,便于精确删除。
 */
import { create } from 'zustand'
import { storage, session, activeBackend, clearSessionFrom, purgeLegacyKeys } from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'
import type { UserInfo, Identity } from '@/types/user'

/* ====================== 持久化片段 ====================== */

/** 持久化到 USER 这个 key 的数据(由 hydrateUserStore 读回) */
interface PersistedUser {
  userInfo: UserInfo
  currentIdentity: Identity | null
  isLoggedIn: true
}

/* ====================== 纯函数(便于单测) ====================== */

/**
 * 决定登录后 / 启动时"当前激活身份"是谁。
 *
 * ⚠️ 这里的历史故障是**每次都被重置回第一个身份**:原先无条件取
 * `identities[0]`,于是用户切到"供应商"身份后,任何一次重新 hydrate
 * (F5、切 tab、启动)都把他打回主身份 —— 表现为"我刚切的身份怎么又变回来了"。
 *
 * 规则:已持久化的选择优先,但只在它**仍然存在于服务端返回的列表里**时才用。
 * 身份被停用/删除后回落到第一个,而不是留一个指向不存在对象的脏状态。
 */
export function resolveIdentity(
  userInfo: UserInfo | null,
  persisted: Identity | null,
): Identity | null {
  if (!userInfo) return null
  const list = userInfo.identities ?? []
  if (persisted) {
    const stillValid = list.find((i) => i.id === persisted.id)
    if (stillValid) return stillValid
  }
  return list[0] ?? null
}

/* ====================== 空闲基线 ====================== */

/**
 * 把"最近一次活动时间"对齐到现在 —— **一次会话开始时**必须调。
 *
 * ⚠️ 为什么这个键归会话管(2026-10-28 M108):
 *   空闲超时(30min)判的是"用户是不是还在用"。计时器**只在应用运行时存在** ——
 *   浏览器关掉的那几个小时里没有任何代码在跑,那段时间**不该算进空闲预算**。
 *   可这个时间戳是落盘的(localStorage,为了多标签页共享),于是它会跨会话残留:
 *     · 用户 18:00 登出、19:00 重新登录 —— 旧时间戳还躺在盘上;
 *     · 浏览器 18:00 关掉、第二天 09:00 再打开 —— 同样是 15 小时前的旧时间戳。
 *   只要旧时间戳比 30min 老,空闲计时器**第一次轮询(15s)就会把刚建立的会话踢掉**
 *   ⇒「记住我」的持久会话(服务端 refresh token 活 30 天)实际上只值 30 分钟。
 *
 *   所以:会话开始(登录)时对齐一次、会话结束(登出/重置)时清掉
 *   (LAST_ACTIVE_AT 已在 utils/storage.ts 的 SESSION_KEYS 里,清会话时一并清)。
 *
 * 落盘仍走 **localStorage**(不是会话所在的那个后端):空闲判定要**多标签页共享** ——
 * A 标签页挂着不动时,B 标签页有操作就该给整个会话续命(见 utils/idleTimer.ts)。
 */
function touchIdleBaseline(): void {
  storage.set(STORAGE_KEYS.LAST_ACTIVE_AT, Date.now())
}

/* ====================== 会话清理 ====================== */

/**
 * 清空整个会话 —— 两个后端都清,内存状态一并复位。
 *
 * logout() 与 reset() 语义上曾经不同(前者可以先调后端吊销,后者是 401 路径
 * 调不了后端),但**落到本地这一步的动作逐字节相同**,所以合并成一个实现。
 * 差异留在调用点上表达,不要在这里假装有两种行为。
 */
function clearSession(set: (partial: Partial<UserState>) => void): void {
  clearSessionFrom()
  set({ userInfo: null, currentIdentity: null, isLoggedIn: false })
}

/**
 * 用户 store 的状态形状。
 *
 * ⚠️ **导出理由(2026-10-29 M109)**:不是"给零调用方的类型加字段",
 *   而是 utils/sentry.ts 的回归网需要用 `Partial<UserState>` 精确描述
 *   "我只 set 了哪几个字段" —— 之前测试里写 `as Partial<UserState>` 拿不到这个类型,
 *   只能退化成 `as any`,于是**写错字段名也不会被 tsc 发现**(假绿)。
 */
export interface UserState {
  /** 用户基本信息(null = 未登录) */
  userInfo: UserInfo | null
  /** 当前激活身份 */
  currentIdentity: Identity | null
  /** 是否已登录 */
  isLoggedIn: boolean

  /* -------- actions -------- */
  /**
   * 登录成功: 写入 token + userInfo
   *
   * @param remember **必填**,且不给默认值
   *   漏传必须是编译错误,而不是静默变成"永久记住" —— 之前这里是
   *   `remember = true` 默认值,PermissionLoader 启动时没传就等于
   *   "每次刷新都把会话升级成永久",这正是"记住我"白做的最后一根稻草。
   * @param expiresIn 服务端 access token 剩余秒数
   * @param refreshExpiresIn 服务端 refresh token 剩余秒数
   */
  setLogin: (data: {
    userInfo: UserInfo
    accessToken: string
    refreshToken: string
    remember: boolean
    /** access token 有效秒数(用于自动刷新调度 + 本地 TTL) */
    expiresIn?: number
    /** refresh token 有效秒数(本地 TTL) */
    refreshExpiresIn?: number
  }) => void
  /**
   * 只更新 userInfo(个人资料页)—— 不碰 token、不动当前身份。
   * 写回 activeBackend(),因为会话键只存在于那一个后端。
   */
  setUserInfo: (userInfo: UserInfo) => void
  /**
   * 服务端 /auth/me 返回后回填用户信息(启动路径)。
   *
   * ⚠️ 与 setLogin 的区别是这一轮修的要点:启动时**不该重新决定持久化策略**。
   * 此前 PermissionLoader 调的是 setLogin 且不传 remember → 走默认 true →
   * 不勾"记住我"的 sessionStorage 登录,第一次刷新就被搬进 localStorage,
   * 关掉浏览器依然在。这里只更新内存 userInfo + 把当前身份按
   * resolveIdentity 重新定位,**不碰 token、不清任何东西**。
   */
  setUserInfoFromServer: (userInfo: UserInfo) => void
  /**
   * 切换身份(服务端同时签发新的 access / refresh token)
   *
   * @param identity 新的激活身份
   * @param tokens 服务端本次签发的 token(后端必然重签 —— 旧 access 的 JTI 被吊销)
   * @param serverUser 服务端本次返回的**权威用户视图**
   *
   * ⚠️ **serverUser 是必填的,不是可选。**
   *   `switch-identity` 的响应里带着完整的 user(identities 全量 +
   *   新的 current_identity_id),那是服务端对"你是谁、当前是谁"的权威回答。
   *   此前前端只把它用来**取**新身份对象,取完就丢 —— 于是
   *   `userInfo.current_identity_id` 仍是切之前的那个,
   *   个人中心("当前身份:XXX",见 profile/index.tsx)显示的是**切之前的角色名**,
   *   `is_platform_super_admin` 也是旧值。
   *   把它做成可选 = 给"忘了传"留了一条**静默错位**的路(而且不报错)。
   */
  switchIdentity: (
    identity: Identity,
    tokens: {
      accessToken?: string
      expiresIn?: number
      refreshToken?: string
      refreshExpiresIn?: number
    },
    serverUser: UserInfo,
  ) => void
  /**
   * 写 token(续期成功后用)—— 不动 userInfo、不动 currentIdentity。
   *
   * TTL 一律来自服务端下发的 expires_in / refresh_expires_in。
   * 此前这里写死 2h / 30d,而后端 access TTL 是 8h:浏览器会在 2h 把一个
   * **服务端还有 6h 才过期**的 token 扔掉,用户被无端踢下线。
   */
  setTokens: (data: {
    accessToken: string
    expiresIn?: number
    refreshToken?: string
    refreshExpiresIn?: number
  }) => void
  /** 读 access token 过期时间戳(用于调度下一次刷新) */
  getAccessTokenExpiresAt: () => number | null
  /**
   * 读 refresh token 过期时间戳(2026-10-25 M8)
   *
   * 跨标签页同步时,采用来的 refresh token 要按**它自己的**剩余寿命写盘。
   * 没有这个值就只剩 8h 兜底 —— 会把"记住我"该有的 30 天寿命砍成 8 小时,
   * 用户表现为"过了 8 小时莫名其妙又要登录"。
   */
  getRefreshTokenExpiresAt: () => number | null
  logout: () => void
  /** 状态重置(被 401 拦截器调用;与 logout 落到本地后行为一致) */
  reset: () => void
}

export const useUserStore = create<UserState>()((set) => ({
  userInfo: null,
  currentIdentity: null,
  isLoggedIn: false,

  setLogin: ({ userInfo, accessToken, refreshToken, remember, expiresIn, refreshExpiresIn }) => {
    const backend = remember ? storage : session

    // ⚠️ 先清后写。顺序反了会在另一个后端留下上一轮的 token:
    // 用户先勾选后取消勾选时,activeBackend 的平局规则会挑到旧的那份,
    // 于是"不记住我"这次选择被静默推翻 —— 不报错,只是关掉浏览器还在。
    clearSessionFrom()

    // TTL 全部来自服务端。之前这四个常量(2h/30d/8h/1d)与后端的 8h/720h
    // 直接冲突:2h < 8h 会让浏览器提前丢弃仍然有效的 token。
    // 走 writeTokens 而不是就地写 —— 少一处"写 token"的地方就少一处
    // "忘了记 refresh 到期时刻"的地方(跨标签页同步要靠它)。
    writeTokens(backend, { accessToken, expiresIn, refreshToken, refreshExpiresIn })

    // 登录这一刻没有"已选身份",所以 resolveIdentity 必然回落到 identities[0]
    const identity = resolveIdentity(userInfo, null)
    if (identity) backend.set(STORAGE_KEYS.CURRENT_IDENTITY, identity)

    // userInfo 持久化(以备刷新页面时直接用, 减少一次 /me 请求)
    backend.set(STORAGE_KEYS.USER, { userInfo, currentIdentity: identity, isLoggedIn: true })

    // ⚠️ 新会话 = 空闲计时重新起算(理由见 touchIdleBaseline)。
    //   必须放在 clearSessionFrom() 之后 —— 那一步会把 LAST_ACTIVE_AT 一并清掉。
    touchIdleBaseline()

    set({ userInfo, currentIdentity: identity, isLoggedIn: true })
  },

  setUserInfo: (userInfo) => {
    const backend = activeBackend()
    const persisted = backend.get<PersistedUser>(STORAGE_KEYS.USER)
    if (persisted) backend.set(STORAGE_KEYS.USER, { ...persisted, userInfo })
    set({ userInfo })
  },

  setUserInfoFromServer: (userInfo) => {
    const backend = activeBackend()
    const persisted = backend.get<PersistedUser>(STORAGE_KEYS.USER)
    // 已选身份优先 —— 启动时如果用户上次切到了供应商身份,别把他打回主身份
    const identity = resolveIdentity(userInfo, persisted?.currentIdentity ?? null)
    backend.set(STORAGE_KEYS.USER, { userInfo, currentIdentity: identity, isLoggedIn: true })
    if (identity) backend.set(STORAGE_KEYS.CURRENT_IDENTITY, identity)
    // ⚠️ isLoggedIn 必须一起置 true(2026-10-25 修):
    //   本函数只可能在 /auth/me **成功**之后被调用(唯一调用方 PermissionLoader),
    //   即"服务端已确认这个 token 有效" —— 此时置 true 无条件成立。
    //   此前内存态漏了这一位,而同一个函数写盘的 blob 里却写着 isLoggedIn: true,
    //   内存和落盘自相矛盾。后果:落盘那份若来自**旧版本**(没有 isLoggedIn 字段)
    //   或被人清过,hydrate 出来的内存态就是 false →
    //     · AuthGuard(guards.tsx)把一个**有效会话**踢回登录页;
    //     · tokenScheduler(`if (!store.isLoggedIn) return`)永远不排续期,
    //       access token 到点就死,用户被迫重新登录。
    set({ userInfo, currentIdentity: identity, isLoggedIn: true })
  },

  switchIdentity: (identity, tokens, serverUser) => {
    const backend = activeBackend()
    backend.set(STORAGE_KEYS.CURRENT_IDENTITY, identity)
    // ⚠️ M146b:服务端给了权威 user ⇒ 整体换掉 userInfo。
    //   current_identity_id 用**服务端**的值(它跟 target.ID 一致);
    //   万一将来两端不一致,以服务端为准 —— 它才是签发 token 的那一方。
    const merged: UserInfo = {
      ...serverUser,
      current_identity_id: serverUser.current_identity_id || identity.id,
    }
    backend.set(STORAGE_KEYS.USER, {
      userInfo: merged,
      currentIdentity: identity,
      isLoggedIn: true,
    })
    writeTokens(backend, tokens)
    set({ userInfo: merged, currentIdentity: identity, isLoggedIn: true })
  },

  setTokens: (tokens) => {
    writeTokens(activeBackend(), tokens)
  },

  getAccessTokenExpiresAt: () => activeBackend().get<number>(STORAGE_KEYS.ACCESS_TOKEN_EXPIRES_AT),

  getRefreshTokenExpiresAt: () => activeBackend().get<number>(STORAGE_KEYS.REFRESH_TOKEN_EXPIRES_AT),

  logout: () => clearSession(set),
  reset: () => clearSession(set),
}))

/* ====================== 内部辅助 ====================== */

/**
 * 服务端没给 expires_in / refresh_expires_in 时的兜底 TTL。
 *
 * 只在**老版本后端**响应缺字段时才会走到 —— 此时按"短"的一侧兜底:
 * 宁可让用户多续一次期,也不要把一个已被吊销的 token 留在浏览器里。
 * 正常路径上 TTL 一律来自服务端。
 */
const FALLBACK_TTL_SECONDS = 60 * 60 * 8

function ttlOf(expiresIn?: number): number {
  return typeof expiresIn === 'number' && expiresIn > 0 ? expiresIn : FALLBACK_TTL_SECONDS
}

/** 把一组 token 写进指定后端(TTL 全来自服务端) */
function writeTokens(
  backend: ReturnType<typeof activeBackend>,
  tokens: { accessToken?: string; expiresIn?: number; refreshToken?: string; refreshExpiresIn?: number },
): void {
  if (tokens.accessToken) {
    backend.set(STORAGE_KEYS.TOKEN, tokens.accessToken, ttlOf(tokens.expiresIn))
  }
  if (typeof tokens.expiresIn === 'number' && tokens.expiresIn > 0) {
    backend.set(STORAGE_KEYS.ACCESS_TOKEN_EXPIRES_AT, Date.now() + tokens.expiresIn * 1000)
  }
  if (tokens.refreshToken) {
    const ttl = ttlOf(tokens.refreshExpiresIn)
    backend.set(STORAGE_KEYS.REFRESH_TOKEN, tokens.refreshToken, ttl)
    // refresh token 的到期时刻要单独记一份(2026-10-25 M8):
    // 跨标签页同步时,接收方拿到的 refresh token 要按**它自己的**剩余寿命
    // 写盘。少了这个值就只剩 8h 兜底 —— "记住我"的 30 天寿命被砍成 8 小时。
    backend.set(STORAGE_KEYS.REFRESH_TOKEN_EXPIRES_AT, Date.now() + ttl * 1000)
  }
}

/* ====================== 启动时 hydrate ====================== */

/**
 * 应用启动时调用 —— 从 localStorage / sessionStorage 中恢复用户状态。
 *
 * 走 activeBackend() 而不是"local 优先":startup 读的就是会话真相所在的
 * 那个后端,这里若与后续 token 读取用不同规则,会出现"内存里是 A 用户、
 * 拦截器发出去的是 B 的 token"这种最难查的错位。
 *
 * 调用时机: main.tsx 在 React 挂载前同步执行。
 */
export function hydrateUserStore(): void {
  // 先清双前缀遗留 key(见 utils/storage.ts fullKey 注释):那些是"登出后仍
  // 有效"的孤儿 refresh token,最长还能再用 720h。
  purgeLegacyKeys()
  const backend = activeBackend()
  const persisted = backend.get<PersistedUser>(STORAGE_KEYS.USER)

  /**
   * ⚠️ **这个后端里还有没有活凭证?**(2026-10-28 M108)
   *
   * USER blob 是**没有 TTL 的**(它是"给页面首屏少发一次 /me 请求"用的缓存),
   * 而 token 有 TTL。于是会出现这种状态:**token 全过期了,blob 还在**。
   * 以前这里只看 `persisted.isLoggedIn` ⇒ 内存态被置成"已登录",可实际上
   * **一个凭证都没有**:
   *   · AuthGuard 放行 → 受保护页面渲染出来;
   *   · 每个请求都因为没有 Authorization 头而 401;
   *   · 401 → doRefresh() → 没有 refresh token → handleUnauthorized()
   *     → 弹"登录已过期" + 跳登录页。
   * 用户看到的体验是"首页闪一下,然后被踢回登录页",而不是干脆停在登录页。
   *
   * 判据:**同一个后端里确实还有 access 或 refresh token**,才算还有会话。
   * (access token 过期但 refresh token 活着是正常的 —— 那正是要续期的场景,
   *  bootstrapTokenRefresh 会接手。)
   */
  const hasCredential =
    backend.get<string>(STORAGE_KEYS.TOKEN) !== null ||
    backend.get<string>(STORAGE_KEYS.REFRESH_TOKEN) !== null

  if (persisted?.isLoggedIn && hasCredential) {
    useUserStore.setState({
      userInfo: persisted.userInfo,
      currentIdentity: resolveIdentity(persisted.userInfo, persisted.currentIdentity),
      isLoggedIn: true,
    })
    // ⚠️ 恢复了会话 ⇒ 空闲计时**重新起算**(理由见 touchIdleBaseline)。
    //   浏览器关掉的那段时间不算空闲 —— 计时器那会儿根本没在跑。
    //   不对齐的话:关掉浏览器 1 小时再打开,第一次轮询(15s)就会把用户踢下线,
    //   「记住我」的 30 天持久会话实际只值 30 分钟。
    touchIdleBaseline()
    return
  }

  // 没有凭证却躺着一份 blob ⇒ 孤儿(会话早就过期/被吊销了)。
  // 顺手删掉:它里面有用户资料(PII),留着没意义,还会让下一次 hydrate
  // 再误判一次"已登录"。
  if (persisted && !hasCredential) {
    backend.remove(STORAGE_KEYS.USER)
  }
}

/* ====================== 读 token helper ====================== */

/** 当前会话的 access token。给 axios 拦截器与 PermissionLoader 用。 */
export function getToken(): string | null {
  return activeBackend().get<string>(STORAGE_KEYS.TOKEN)
}

/** 当前会话的 refresh token —— 活跃后端就是以它为锚点判定的 */
export function getRefreshToken(): string | null {
  return activeBackend().get<string>(STORAGE_KEYS.REFRESH_TOKEN)
}

/**
 * 存储里那一份会话属于哪个用户(2026-10-25 M8)
 *
 * 用途:跨标签页恢复时确认"存储里的这份会话还是**我**的"。
 * 同一个 origin 下另一个标签页可能登出后换了个人登录 —— 那时不能把
 * 它的 token 当成自己的捡起来(否则就是会话串号)。
 */
export function getPersistedUserId(): string | null {
  const persisted = activeBackend().get<PersistedUser>(STORAGE_KEYS.USER)
  return persisted?.userInfo?.id ?? null
}