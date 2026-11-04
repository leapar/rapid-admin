/**
 * 跨标签页会话同步 —— 解决"多标签页互相把对方踢下线"(§1.2 E)
 *
 * # 病根:refresh token 是单次使用的,而浏览器里同一个会话可以有多份副本
 *
 * 后端 refresh token 轮换(用一次就吊销、换一张新的)是对的:它让"token 被偷"
 * 变成一次性的,而不是长期有效的。
 * 但浏览器里**同一个会话可以同时存在多份 refresh token 副本**:
 *   · 两个标签页各刷各的(最常见)
 *   · 一个标签页的刷新请求还在飞,另一个标签页也发了
 * 副本之间没有任何同步机制,于是:
 *
 *   标签页 A 拿 R 刷新成功 → 服务端吊销 R、发出 R2
 *   标签页 B 同时拿 R 刷新 → 服务端看到 R 已吊销 → **拒绝**
 *   → B 收到 401 → 前端 handleUnauthorized() → **B 被踢回登录页**
 *
 * 用户看到的是"我开了两个标签页,有一个莫名其妙被登出了"。
 *
 * # 两种存储后端,两种疼法
 *
 *   · 勾了"记住我"(localStorage):两个标签页**共享**同一份 R。
 *     B 失败后只要**重新读一次**存储就能拿到 A 写进去的 R2 → 重试即可。
 *   · 没勾"记住我"(sessionStorage):sessionStorage **按标签页隔离**,
 *     B 读不到 A 写进去的 R2 —— 重新读存储也没用,
 *     只有**跨标签页广播**能把 R2 送到 B 手里。
 *
 * 所以本模块干两件事:
 *   1. publish —— 谁刷新成功了,把新的 token 组广播出去
 *   2. request —— 刷新被拒时,问其它标签页"有没有更新的一组"
 *
 * # 为什么用 BroadcastChannel 而不是 localStorage 当信使
 *
 *   · BroadcastChannel **不落盘**:消息只在内存里,关掉浏览器就没了。
 *     拿 localStorage 当信使就得把 refresh token 写进 localStorage,
 *     那等于**把"不记住我"这个选择废掉**(关掉浏览器还在)。
 *   · 同源限制天然成立:BroadcastChannel 只在同源标签页之间通信 ——
 *     不用自己判断 origin,也不用担心把 token 发给第三方页面。
 *
 * # 降级
 *
 * 没有 BroadcastChannel 的老浏览器(Chrome 54+ / Firefox 38+ / Safari 15.4+ 都有;
 * 后台管理端是 PC,不受影响)→ 通道为 null,退化成"只有本地存储那一条路":
 * 勾了记住我的场景照样能自愈,没勾的场景退回旧行为(被踢下线)。
 *
 * ⚠️ 本模块**不碰 store** —— 它只管协议。所有"我现在持有什么""收到之后
 *   写到哪里"都在 utils/tokenScheduler.ts 里接线。分开是为了让协议能被
 *   单独测(协议出错的症状是"偶发地被踢下线",极难复现)。
 */

/** 一组 token —— 跨标签页传递的最小单位 */
export interface TokenPayload {
  /** 会话所属用户 —— 防止把别的用户的 token 吃进自己的会话 */
  userId: string
  accessToken: string
  refreshToken: string
  /** access token 的到期时刻(epoch ms)—— 判"还有效吗"靠它 */
  expiresAt: number
  /** access token 剩余秒数 */
  expiresIn?: number
  /** refresh token 剩余秒数 —— 接收方写盘时给多少 TTL 靠它 */
  refreshExpiresIn?: number
}

/** 通道上跑的两种消息 */
export type TokenMessage =
  | { kind: 'tokens'; payload: TokenPayload }
  | { kind: 'need'; userId: string }

/**
 * 通道抽象 —— 生产用 BroadcastChannel,测试注入假实现
 *
 * 抽成接口有两个理由:
 *   1. **jsdom 没有 BroadcastChannel**(实测:jsdom 27 的 window 上是 undefined),
 *      不抽接口就没法测协议逻辑 —— 而协议逻辑正是 bug 所在的地方。
 *   2. 降级路径要能被测:通道为 null 时必须安静地什么都不做,而不是抛异常。
 */
export interface TokenChannel {
  post(message: TokenMessage): void
  /** 订阅;返回取消订阅的函数 */
  subscribe(handler: (message: TokenMessage) => void): () => void
  close(): void
}

/** 通道名 —— 同一 origin 的所有标签页共用一个 */
export const TOKEN_CHANNEL_NAME = 'rapid-admin:token-sync'

/**
 * 创建 BroadcastChannel 通道;环境不支持时返回 null(降级)
 *
 * ⚠️ 不要在这里兜底成 localStorage —— 那会把 refresh token 写进磁盘,
 *   把"不记住我"这个选择废掉(见文件头注释)。
 *
 * ⚠️ **必须排除 Node 的 BroadcastChannel**(worker_threads 那个):
 *   jsdom 至今没实现 BroadcastChannel,所以在 Node 环境(测试、SSR)里
 *   全局那个是 Node 的实现,跟浏览器的 DOM 实现不是一回事:
 *     · 它派发的是 Node 的 MessageEvent,在 jsdom 环境里 dispatch 会抛
 *       "The event argument must be an instance of Event"
 *       (jsdom 的 Event 类与 Node 的不是同一个)
 *     · 它**吊住 Node 事件循环** —— 不 unref 的话 vitest 跑完不退出
 *     · 它是**进程级**的 —— 同一进程里的多个测试文件会互相收到消息
 *   Node 版有 `unref()`,DOM 版没有 —— 用它区分。
 */
export function createBroadcastTokenChannel(name: string = TOKEN_CHANNEL_NAME): TokenChannel | null {
  if (typeof BroadcastChannel === 'undefined') return null
  const Ctor = BroadcastChannel as unknown as {
    prototype?: { unref?: unknown }
    new (name: string): BroadcastChannel
  }
  if (typeof Ctor.prototype?.unref === 'function') return null

  const bc = new Ctor(name)
  return {
    post: (message) => bc.postMessage(message),
    subscribe: (handler) => {
      const listener = (event: MessageEvent) => handler(event.data as TokenMessage)
      bc.addEventListener('message', listener)
      return () => bc.removeEventListener('message', listener)
    },
    close: () => bc.close(),
  }
}

/** 同步器的依赖 —— 全部注入,便于测试与"不碰 store" */
export interface TokenSyncOptions {
  /** 当前会话的用户 id(未登录为 null) */
  getUserId: () => string | null
  /** 当前持有的 refresh token(未登录为 null) */
  getLocalRefreshToken: () => string | null
  /** 本标签页当前持有的 token 组(用于回应别人的求助) */
  getLocal: () => TokenPayload | null
  /** 收到"可采用"的一组 token */
  onTokens: (payload: TokenPayload) => void
  /** 当前时间(测试可注入) */
  now?: () => number
}

/** 一个等待中的求助 */
interface Waiter {
  /** 本次求助要排除的 refresh token(就是刚失败的那个) */
  exclude: string | null
  resolve: (payload: TokenPayload | null) => void
}

/**
 * 跨标签页 token 同步器
 *
 * 协议只有两条消息,刻意做得极简 —— 消息越多,两个标签页的时序组合就越多,
 * 而这类协议出问题时症状是"偶发地被踢下线",极难复现。
 */
export class TokenSync {
  private channel: TokenChannel | null
  private options: TokenSyncOptions
  private waiters: Waiter[] = []
  private unsubscribe: (() => void) | null = null

  constructor(channel: TokenChannel | null, options: TokenSyncOptions) {
    this.channel = channel
    this.options = options
    if (channel) {
      this.unsubscribe = channel.subscribe(this.handle)
    }
  }

  /** 通道是否可用(降级时为 false) */
  get available(): boolean {
    return this.channel !== null
  }

  /** 广播本标签页当前持有的 token 组 */
  publish(payload: TokenPayload): void {
    this.channel?.post({ kind: 'tokens', payload })
  }

  /**
   * 求助:等其它标签页给出"更新的一组"
   *
   * @param exclude 刚失败的那个 refresh token —— 回应里带着它没有意义
   *   (它已经被消费过,谁拿着它都换不出新 token)
   * @param timeoutMs 等多久
   * @returns 拿到就返回该组;超时/无回应返回 null
   */
  request(exclude: string | null, timeoutMs: number): Promise<TokenPayload | null> {
    const userId = this.options.getUserId()
    if (!this.channel || !userId) return Promise.resolve(null)

    return new Promise<TokenPayload | null>((resolve) => {
      let settled = false
      const finish = (payload: TokenPayload | null) => {
        if (settled) return
        settled = true
        clearTimeout(timer)
        this.waiters = this.waiters.filter((w) => w !== waiter)
        resolve(payload)
      }
      const timer = setTimeout(() => finish(null), timeoutMs)
      const waiter: Waiter = { exclude, resolve: finish }
      this.waiters.push(waiter)
      this.channel?.post({ kind: 'need', userId })
    })
  }

  /** 关闭通道(登出 / 卸载时) */
  dispose(): void {
    this.unsubscribe?.()
    this.unsubscribe = null
    for (const w of this.waiters) w.resolve(null)
    this.waiters = []
    this.channel?.close()
  }

  /**
   * 判断远端的一组 token 值不值得采用
   *
   * 四道闸,少一道都会出事:
   *   1. **同一个人** —— 同一 origin 下可能有另一个用户登录在别的标签页
   *      (一个勾了记住我、另一个没勾)。吃下别人的 token = 会话串号,
   *      表现为"我切到另一个标签页,里面的人变成别人了"。
   *   2. **access token 还有效** —— 过期的那组只会把本标签页拖进 401 循环。
   *   3. **不是刚失败的那个** —— 刚失败的那个已经被服务端消费过,
   *      谁拿着它都换不出新 token,采用它等于白忙一场。
   *   4. **不是本地的那个** —— 相同即无事可做(也顺手挡掉了两个标签页
   *      互相"采用"对方、形成乒乓)。
   */
  private acceptable(payload: TokenPayload, exclude: string | null): boolean {
    const userId = this.options.getUserId()
    if (!userId || payload.userId !== userId) return false
    if (!payload.accessToken || !payload.refreshToken) return false
    const now = this.options.now ? this.options.now() : Date.now()
    if (payload.expiresAt <= now) return false
    if (exclude && payload.refreshToken === exclude) return false
    if (payload.refreshToken === this.options.getLocalRefreshToken()) return false
    return true
  }

  private handle = (message: TokenMessage): void => {
    if (message.kind === 'need') {
      // 有人求助 → 把我这儿的当前组给他。他只会在"对自己有用"时采用,
      // 所以这里不需要替他判断(我也不知道他的 local 长什么样)。
      const local = this.options.getLocal()
      if (local) this.channel?.post({ kind: 'tokens', payload: local })
      return
    }

    const payload = message.payload
    const matched = this.waiters.filter((w) => this.acceptable(payload, w.exclude))
    if (matched.length > 0) {
      // 有求助者认领 → 唤醒他们。
      // ⚠️ 这里**不**调 onTokens:采用由求助方自己完成(它拿到 payload 后
      //   写盘 + 重排调度)。两处都写会让 TTL 漂移,还多一个"谁负责写"的歧义。
      for (const w of matched) w.resolve(payload)
      return
    }
    // 没人求助(或者带上的是刚失败的那个)→ 主动同步:直接采用
    if (this.acceptable(payload, null)) {
      this.options.onTokens(payload)
    }
  }
}
