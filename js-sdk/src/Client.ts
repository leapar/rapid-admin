/**
 * SDK 入口
 *
 * ```ts
 * import { DynClient } from '@rapid-admin/dyn-sdk'
 *
 * const client = new DynClient('https://your-host/rapid-admin/v1')
 * await client.authWithPassword('<账号>', '<密码>') // token 自动挂到后续请求
 * const records = await client.collection('inspection').getList({ page: 1, size: 20 })
 * ```
 *
 * # ⚠️ **baseURL 是 API 根路径(含 /rapid-admin/v1),不是站点根** ——
 *
 * 拼错一步,后面所有 404 都会指向"路由不存在",跟真正的原因(baseURL)对不上。
 * 管理后台里「API 接入」弹窗印的 curl 用的 baseURL 就是这一个。
 */
import { AuthStore } from './AuthStore'
import { ClientResponseError } from './ClientResponseError'
import { RecordService } from './RecordService'
import { preHashPassword } from './sha256'
import type {
  AuthTokens,
  CaptchaInfo,
  ClientOptions,
  Envelope,
  LoginOptions,
  SendOptions,
} from './types'

export class DynClient {
  /** API 根路径(不带结尾斜杠) */
  readonly baseURL: string

  /** token 存放处(内存态,见 AuthStore) */
  readonly authStore: AuthStore = new AuthStore()

  private fetchImpl: typeof fetch
  private beforeSend?: ClientOptions['beforeSend']

  constructor(baseURL: string, options: ClientOptions = {}) {
    if (!baseURL || baseURL.trim() === '') {
      // ⚠️ **空 baseURL 当场报错** —— 静默拼出相对路径,错误会变成
      // "请求打到集成方自己的站点上",排查时完全找不到方向。
      throw new Error('DynClient: baseURL 不能为空(例:https://your-host/rapid-admin/v1)')
    }
    this.baseURL = baseURL.replace(/\/+$/, '')
    this.fetchImpl = options.fetch ?? globalThis.fetch
    if (typeof this.fetchImpl !== 'function') {
      throw new Error(
        'DynClient: 当前环境没有 fetch(浏览器 / Node 18+ 自带;更低版本请传入 options.fetch)',
      )
    }
    this.beforeSend = options.beforeSend
  }

  /** 集合记录服务(同参数重复调用返回同一个实例,不重复构造) */
  collection(code: string): RecordService {
    if (!code || code.trim() === '') {
      throw new Error('DynClient.collection: 集合编码不能为空')
    }
    let svc = this.collectionCache.get(code)
    if (!svc) {
      svc = new RecordService(this, code)
      this.collectionCache.set(code, svc)
    }
    return svc
  }
  private collectionCache = new Map<string, RecordService>()

  /**
   * 发请求:拼 URL / 挂 token / 解信封,失败抛 ClientResponseError
   *
   * # 信封解法
   *
   * 后端所有 JSON 响应都是 { code, data, message, timestamp, trace_id },
   * **code = 0 才算成功** —— HTTP 200 但 code ≠ 0 同样抛错
   * (业务失败不一定配非 200 的 HTTP 状态码,只看 HTTP 会漏)。
   */
  async send<T = any>(path: string, options: SendOptions = {}): Promise<T> {
    const method = options.method ?? 'GET'
    const url = this.buildURL(path, options.query)

    const headers: Record<string, string> = {}
    if (this.authStore.token) {
      headers.Authorization = `Bearer ${this.authStore.token}`
    }
    if (options.body !== undefined) {
      headers['Content-Type'] = 'application/json'
    }
    Object.assign(headers, options.headers)

    let init: RequestInit = {
      method,
      headers,
      signal: options.signal,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    }
    const modified = this.beforeSend?.(url, init)
    if (modified) {
      init = modified
    }

    let res: Response
    try {
      res = await this.fetchImpl(url, init)
    } catch (err: any) {
      if (isAbortError(err)) {
        throw new ClientResponseError(url, 0, 0, '请求已取消', undefined, true)
      }
      throw new ClientResponseError(url, 0, 0, `网络请求失败:${err?.message ?? err}`)
    }

    let body: unknown = null
    const text = await res.text().catch(() => '')
    if (text !== '') {
      try {
        body = JSON.parse(text)
      } catch {
        // 非 JSON(网关错误页、CORS 拦截的空响应等)—— 保 message 就够
      }
    }

    const envelope = asEnvelope(body)
    if (!res.ok || !envelope || envelope.code !== 0) {
      throw new ClientResponseError(
        url,
        res.status,
        envelope?.code ?? 0,
        envelope?.message ?? fallbackMessage(res.status, text),
        body,
      )
    }
    return envelope.data as T
  }

  // ── 鉴权:登录 / 验证码 / 刷新 / 登出 ────────────────────────────────
  //
  // 写接口(create / update / delete / ...)**必须**先登录拿 token;
  // 读接口(列表 / 详情 / 结构)默认也要 token(dyn:<code>:list),
  // 但集合开了 public_read 时,RecordService 的读动作会自动切到公开
  // 镜像路径,未登录也能读。
  // ⇒ 集成方在第一次写操作前调 authWithPassword 即可,token 会自动
  //   挂到后续所有请求上。

  /**
   * 账号密码登录,成功后 token 自动存入 authStore。
   *
   * ⚠️ **密码在 SDK 内做前置摘要** —— 传**明文**进来,SDK 发出去的是
   *   sha256("rapid-admin::" + 明文),与后端 bcrypt 比对口径一致;
   *   不要自己 hash 过再传(双重摘要 = 永远"账号或密码错误")。
   *
   * ⚠️ **验证码** —— 服务端 captcha.login_enabled 开启时,先 `getCaptcha()`
   *   拿图片给用户看,然后把 `captchaId + captchaCode` 传进来;开关关闭时
   *   什么都不用传。
   */
  async authWithPassword(
    username: string,
    password: string,
    options: LoginOptions = {},
  ): Promise<AuthTokens> {
    if (!username || !password) {
      throw new Error('DynClient.authWithPassword: 用户名和密码不能为空')
    }
    const preHashed = await preHashPassword(password)
    const tokens = await this.send<AuthTokens>('/public/auth/login', {
      method: 'POST',
      body: {
        username,
        password: preHashed,
        captcha_id: options.captchaId ?? '',
        captcha_code: options.captchaCode ?? '',
      },
      signal: options.signal,
    })
    if (tokens?.token) {
      this.authStore.save(tokens.token)
    }
    return tokens
  }

  /**
   * 取一张图形验证码(base64 data URL,集成方自己决定怎么展示)。
   * 登录接口提示"请先输入图形验证码"时走这里。
   */
  async getCaptcha(options: { signal?: AbortSignal } = {}): Promise<CaptchaInfo> {
    return this.send<CaptchaInfo>('/public/captcha', { method: 'GET', signal: options.signal })
  }

  /**
   * 用 refresh_token 换一对新 token(成功后 access token 自动存入 authStore)。
   *
   * ⚠️ **refresh token 是单次使用的** —— 换完旧的那张立即作废;
   *   换出来的新 refresh_token 必须覆盖保存,重放旧值会被直接拒掉。
   */
  async refreshAuth(refreshToken: string, options: { signal?: AbortSignal } = {}): Promise<AuthTokens> {
    if (!refreshToken) {
      throw new Error('DynClient.refreshAuth: refresh_token 不能为空')
    }
    const tokens = await this.send<AuthTokens>('/public/auth/refresh', {
      method: 'POST',
      body: { refresh_token: refreshToken },
      signal: options.signal,
    })
    if (tokens?.token) {
      this.authStore.save(tokens.token)
    }
    return tokens
  }

  /**
   * 登出:吊销服务端会话 + 清本地 token。**永不抛错** ——
   * 服务端拒绝(token 已过期等)对调用方没有可处理的动作,
   * 本地 token 清掉就是登出的全部意义;失败只在返回前被吞掉。
   */
  async logout(options: { signal?: AbortSignal } = {}): Promise<void> {
    try {
      if (this.authStore.isValid) {
        await this.send('/auth/logout', { method: 'POST', signal: options.signal })
      }
    } catch {
      // 服务端吊销失败(token 过期 / 网络断)——本地照清,不向上抛
    } finally {
      this.authStore.clear()
    }
  }

  private buildURL(path: string, query?: Record<string, string>): string {
    const qs = query
      ? Object.entries(query)
          .filter(([, v]) => v !== undefined && v !== null && v !== '')
          .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
          .join('&')
      : ''
    return `${this.baseURL}${path}${qs ? `?${qs}` : ''}`
  }
}

function asEnvelope(body: unknown): Envelope | null {
  if (typeof body !== 'object' || body === null || !('code' in body)) {
    return null
  }
  return body as Envelope
}

function fallbackMessage(status: number, text: string): string {
  if (text !== '') {
    return `响应不是合法 JSON(HTTP ${status}):${text.slice(0, 120)}`
  }
  // ⚠️ **403 空 body 是 CORS 拦截的经典长相** —— 后端对未白名单 Origin
  // 直接 AbortWithStatus(403) 不写 body(见仓库 CORS 笔记)。
  if (status === 403) {
    return 'HTTP 403:可能是 CORS 未放行(响应体为空),也可能是没有该集合的权限'
  }
  return `HTTP ${status}`
}

function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === 'AbortError'
}
