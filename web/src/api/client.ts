/**
 * Axios 客户端 —— 全局唯一实例
 *
 * 职责:
 * 1. 注入 X-Request-Id(与后端日志串联)
 * 2. 注入 Authorization
 * 3. 写操作注入 Idempotency-Key
 * 4. 统一业务错误码处理
 * 5. 401 自动登出
 * 6. 网络/超时降级
 */
import axios, { AxiosError, AxiosInstance, AxiosRequestConfig, AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import { message, notification } from 'antd'
import { BizCode, ApiResponse } from '@/types/api'
import { getToken } from '@/stores/user'
import { genRequestId, genIdempotencyKey } from '@/utils/request'
import { ERROR_MESSAGES } from '@/constants/app'
import { useUserStore } from '@/stores/user'
import { tKey, tError } from '@/locales'
import { doRefresh, cancelTokenRefresh } from '@/utils/tokenScheduler'
import { retryRequest, canceledError } from '@/utils/retry'
import { normalizeProgress, type HttpProgress } from '@/api/progress'
import { getBaseURL, type Channel } from '@/api/baseURL'
import {
  FIELD_POLICY_HEADER,
  currentPagePath,
  parseFieldPolicyHeader,
  setLatestFieldPolicyReport,
  stampFieldPolicyReport,
} from '@/utils/fieldPolicy'

const baseURL = getBaseURL()
const timeout = Number(import.meta.env.VITE_API_TIMEOUT) || 30000

export const instance: AxiosInstance = axios.create({
  baseURL,
  timeout,
  // 业务数据走 response.data,不直接以 axios 状态码为错
  validateStatus: (s) => s >= 200 && s < 300,
})

/* ====================== 请求头 / baseURL 小工具 ====================== */

/**
 * 大小写不敏感地读一个请求头
 *
 * ⚠️ **为什么不能直接 `config.headers['X-Request-Id']`**:
 *   axios 的 `AxiosHeaders` 存头名时**保留调用方写的大小写**
 *   (第一次写进去的是 `self[key || _header] = value`),
 *   但它内部**查找**是大小写不敏感的(`utils.findKey` 两边都 lowerCase 再比)。
 *   而方括号访问是**精确匹配** —— 调用方写 `x-request-id`(小写;HTTP 头
 *   本来就大小写不敏感)时,`config.headers['X-Request-Id']` 读到 undefined,
 *   拦截器就以为"没设过" → **重新生成一个,把调用方的值覆盖掉**。
 *
 *   实测:调用方传 `{'x-request-id': 'CALLER-LOWER'}` → 出去的是新生成的 UUID,
 *   调用方的值没了。
 *
 *   对 X-Request-Id 只是"关联 id 换了";对 **Idempotency-Key** 是
 *   **防重复提交失效** —— 后端按 key 去重,key 换了就等于没去重。
 */
function getHeader(headers: unknown, name: string): string | undefined {
  if (!headers || typeof headers !== 'object') return undefined
  const h = headers as { get?: (n: string) => unknown } & Record<string, unknown>
  if (typeof h.get === 'function') {
    // AxiosHeaders.get() 内部会 normalizeHeader + findKey → 大小写不敏感
    const v = h.get(name)
    return v === null || v === undefined ? undefined : String(v)
  }
  // 退化路径:普通对象(axios 之外的调用方 / 单测)
  const key = Object.keys(h).find((k) => k.toLowerCase() === name.toLowerCase())
  if (key === undefined) return undefined
  const v = h[key]
  return v === null || v === undefined ? undefined : String(v)
}

/**
 * 判断一个 baseURL 是不是"通道名"(而不是路径 / 完整 URL)
 *
 * 通道名(如 'owner' / 'supplier')要查 env 换成真正的 baseURL;
 * 路径('/owner/v1')和完整 URL('https://x/api')原样用。
 */
function isChannelName(v: string): boolean {
  if (v.startsWith('/')) return false
  if (v.startsWith('//')) return false
  if (/^[a-z][a-z0-9+.-]*:/i.test(v)) return false
  return true
}

/* ====================== 请求拦截 ====================== */

instance.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    // 1. 注入 X-Request-Id
    //    ⚠️ M101:必须**大小写不敏感**地查(理由见 getHeader 的说明)——
    //    直接 `config.headers['X-Request-Id']` 是精确匹配,调用方写小写
    //    `x-request-id` 时会被当成"没设过"→ 重新生成 → **把调用方的值覆盖掉**。
    if (!getHeader(config.headers, 'X-Request-Id')) {
      config.headers['X-Request-Id'] = genRequestId()
    }

    // 2. 注入 Authorization
    //   token 可能落在 localStorage(记住我) 或 sessionStorage(临时), 走 helper 双后端读
    const token = getToken()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }

    // 3. 写操作注入 Idempotency-Key
    //    ⚠️ M101:同样的大小写问题 —— 覆盖掉调用方的 key = **防重复提交失效**
    //    (后端按 key 去重,key 换了就等于没去重)
    if (['post', 'put', 'patch', 'delete'].includes(config.method?.toLowerCase() || '')) {
      if (!getHeader(config.headers, 'Idempotency-Key')) {
        config.headers['Idempotency-Key'] = genIdempotencyKey()
      }
    }

    // M18: refresh 请求打标(避免响应拦截器对它再触发刷新)
    if (config.url?.includes('/public/auth/refresh')) {
      config._isRefreshRequest = true
    }

    // M168:记下这次请求是在**哪个页面**发起的 ——
    //   响应回来时要把"字段可见性报告"按页面归档
    //   (页面自己重新拼了行对象时,盖在对象上的戳会丢 ⇒ 需要这条兜底)。
    //   ⚠️ **在请求发起时记,不是在响应回来时记** ——
    //   用户在请求飞着的时候跳走了,响应回来时算路径就串到新页面上了。
    config._pagePath = currentPagePath()

    // 4. 注入客户端标识(后端日志需要区分来源)
    config.headers['X-Client'] = 'admin-web'
    config.headers['X-Client-Version'] = import.meta.env.VITE_APP_VERSION || '0.1.0'

    // M31:多 baseURL 切换 —— per-request 覆盖
    //
    // ⚠️ M101:这里原来是一段**死代码** ——
    //   条件写的是 `override !== config.baseURL`,而 `override` **就是**
    //   `config.baseURL`(axios 已经把 per-request 的 baseURL 合并进 config 了),
    //   所以这个条件**永远为假**,整段从来没执行过。
    //   后果:传 `baseURL: 'owner'` 时请求打到 `owner/system/users` ——
    //   那是**相对当前页面 origin 的相对路径**,不是 `/owner/v1/system/users`
    //   (实测 `instance.getUri()` = "owner/system/users")。静默 404,零报错。
    //
    //   现在:通道名 → 查 env 拿该通道的 baseURL;路径 / 完整 URL → 原样用。
    const rawBase = config.baseURL
    if (typeof rawBase === 'string' && rawBase && isChannelName(rawBase)) {
      config.baseURL = getBaseURL(rawBase as Channel)
    }

    return config
  },
  (error) => Promise.reject(error)
)

/* ====================== 响应拦截 ====================== */

/** 给 axios config 加一个内部标记,避免 refresh 请求本身再次触发 401 refresh */
declare module 'axios' {
  export interface InternalAxiosRequestConfig {
    _isRefreshRequest?: boolean
    _retry?: boolean
    /**
     * M101:**这次请求是重试链里发出来的**
     *
     * 重试走的是 `instance.request(config)` —— 会**完整重跑一遍拦截器链**。
     * 于是重试那一次的错误拦截器也会走一遍、也会弹一次 toast;
     * 不加这个标记,一次失败就弹**两个**红条(实测)。
     * 标记之后:**内层不弹**,由最外层统一弹一次(且用真实错误的码)。
     */
    _isRetryAttempt?: boolean
    /** M31:per-request baseURL 覆盖(channel 名 或 完整 URL) */
    baseURL?: string
    /**
     * M168:这次请求发起时所在的页面路径
     *
     * 用来把"字段可见性报告"按页面归档(见 utils/fieldPolicy.ts)。
     */
    _pagePath?: string
  }
}

instance.interceptors.response.use(
  (resp: AxiosResponse<ApiResponse>) => {
    const data = resp.data
    // 取请求侧 silent 选项(axios config.silent)
    const silent = (resp.config as InternalAxiosRequestConfig & { silent?: boolean }).silent === true
    // 后端约定:HTTP 200 + 业务 code,code 非 0 即业务错误
    if (data && typeof data === 'object' && 'code' in data) {
      if (data.code === BizCode.SUCCESS) {
        applyFieldPolicyReport(resp)
        return resp
      }
      handleBizError(data.code, data.message || tKey('error.businessError'), data.request_id, silent)
      return Promise.reject({
        code: data.code,
        message: data.message,
        requestId: data.request_id,
        data: data.data,
      })
    }
    // 兼容非标准响应(老接口或下载流)
    return resp
  },
  async (error: AxiosError<ApiResponse>) => {
    /**
     * ⚠️ **取消不是错误**(2026-10-25 M6)
     *
     * axios 主动 abort(组件卸载 / 页面切换 / useRequest 的 cancel())
     * 抛的是 CanceledError(code = 'ERR_CANCELED'),**没有 response**。
     * 以前它一路落到最后那个 else 分支 → `message.error("canceled")`
     * → **每取消一个请求就闪一个红色 toast**。用户主动取消是正常操作,
     * 不该报错,更不该占屏。
     *
     * 这里直接静默 reject:调用方拿到的错误对象上带 `canceled: true`,
     * 业务层想区分"被取消"和"真失败"时按这个字段判。
     */
    if (isCanceled(error)) {
      // ⚠️ M110:形状的真源在 utils/retry.ts 的 canceledError() ——
      //   退避等待被打断时(utils/retry.ts 的 sleep)也用同一个,
      //   两条"取消"路径对外**完全同形**(此前一条是 DOMException,一条是这个对象)。
      return Promise.reject(canceledError())
    }

    /**
     * ⚠️ M101:`silent` 必须**覆盖 HTTP 状态码错误**
     *
     * 以前 `silent` 只在**成功**拦截器(业务码那条路,见下面第 116-117 行)被读,
     * 错误拦截器(HTTP 4xx/5xx)从头到尾没看过它 —— 于是:
     *   `api/inbox.ts` 的 `unreadCount` 挂在 Header 上、每个登录用户都会打,
     *   它的注释明写"没有 inbox:read 的角色拿到的是 403,不静默的话,
     *   页面上每刷新一次就弹一个红色的'无权限'"。
     * 而后端把业务码 40300 映射成 **HTTP 403**
     * (server/internal/pkg/errcode/errcode.go:190),走的正是错误拦截器
     * → **silent 不生效 → 那个红色 toast 照弹**。
     *
     * 静默选项写了、理由也写了、就是没接上。
     */
    const cfg = error.config as (InternalAxiosRequestConfig & { silent?: boolean }) | undefined
    const silent = cfg?.silent === true
    /**
     * ⚠️ M101:**重试链里的那一帧不弹提示、不产出对外错误**
     *
     * 它把原始 AxiosError 原样抛给最外层,由最外层统一弹一次 + 规范化 ——
     * 否则一次失败会弹两个红条,而且报出来的还是**第一次**失败的码。
     */
    const isRetryAttempt = cfg?._isRetryAttempt === true

    /* ---------- ① 401:刷新 token 后重发原请求 ---------- */
    const status0 = error.response?.status
    if (status0 === 401 && cfg && !cfg._isRefreshRequest && !cfg._retry) {
      // M18:401 自动刷新 + 重试
      //   - refresh 请求本身不再触发刷新(避免循环)
      //   - 已重试过的不再重试
      //   - 401 refresh 失败 → doRefresh 内部已 handleUnauthorized
      const ok = await doRefresh()
      if (ok) {
        cfg._retry = true
        // 用新 token 重发原请求
        const newToken = getToken()
        if (newToken) {
          cfg.headers = cfg.headers ?? {}
          ;(cfg.headers as Record<string, string>).Authorization = `Bearer ${newToken}`
        }
        return instance.request(cfg)
      }
      // refresh 失败 → doRefresh 内部已 handleUnauthorized,这里不需要再处理
      return Promise.reject(normalizeAxiosError(error))
    }

    /**
     * refresh 请求自身 401 → refresh token 被拒(已过期 / 已被消费 / 已吊销)
     *
     * ⚠️ 2026-10-25 M8:这里**不能**直接登出。
     *   refresh token 是单次使用的(后端轮换),两个标签页同时续期时
     *   必然有一个拿着已被消费的那个 token 被拒 —— 那是**竞态**,不是掉线。
     *   在这里登出会把恢复机会掐掉:失败的那个标签页本来能跨标签页找回
     *   新的一组 token(见 utils/tokenScheduler.ts 的 recoverFromSiblingTab)。
     *
     *   登出的决定权交给 doRefresh —— 它是 refreshToken() 的唯一调用方,
     *   恢复链跑完仍然失败才登出。
     */
    if (status0 === 401 && cfg?._isRefreshRequest) {
      return Promise.reject(normalizeAxiosError(error))
    }

    /* ---------- ② 重试(M30) ---------- */
    /**
     * M30:网络抖动 / 5xx / 408 / 429 自动重试
     *   - 401 / 业务 4xx 不在此处重试(已上分支处理)
     *   - 写操作(POST/PUT/PATCH/DELETE)默认不重试(避免双发)
     *
     * ⚠️ M101:重试走的是 `instance.request(config)` —— 会**完整重跑一遍
     *   拦截器链**,于是原来有两个后果,现在两个都修:
     *   1. 内层(重试那一次)自己也会弹一次 toast → **一次失败弹两个红条**;
     *   2. 内层失败时抛的是**新的**错误,而老代码 `catch {}` 把它**整个吞掉**,
     *      改用**第一次**失败的 status 继续走 → 调用方拿到的 error.code
     *      是过期的(实测:第一次 500、重试后 400,报出来的是 50001)。
     *
     *   现在:内层把**原始 AxiosError** 原样抛上来(靠 `_isRetryAttempt` 标记),
     *   最外层拿它继续往下走 —— toast 弹一次,且用的是**最后一次**失败的码。
     */
    let err: AxiosError = error
    try {
      return await retryRequest(err, instance)
    } catch (e) {
      if (isNormalizedError(e)) {
        // 内层已经规范化过(取消 / 401 分支)→ 原样透传,不再提示
        return Promise.reject(e)
      }
      if (isAxiosErrorLike(e)) {
        err = e
      }
      // 不是 axios 错误 → 沿用最初的 error 继续(下面的网络错误分支会兜住)
    }

    /* ---------- ③ 提示 + 对外 reject ---------- */
    if (isRetryAttempt) {
      // 重试链里的中间帧:不弹提示、不规范化 —— 交给最外层统一做
      return Promise.reject(err)
    }

    if (err.response) {
      const status = err.response.status
      const data = err.response.data as ApiResponse | undefined
      if (status === 401) {
        // 已经在上面处理过刷新重试,到这里说明是第二次失败
        handleUnauthorized()
      } else if (!silent) {
        // ⚠️ 400 / 403 / 404 / 409 / 413 / 429 / 5xx 都在这里。
        //   以前直接把**后端 body 里的中文 message** 甩给用户 ——
        //   英文环境下弹出来的也是中文。现在先按业务码查 i18n 翻译,
        //   查不到才回落后端原文。
        const bizCode = typeof data?.code === 'number' ? data.code : 0
        notifyHttpError(status, bizCode, data?.message || '')
      }
      return Promise.reject(normalizeAxiosError(err))
    }

    // 无 response:网络错误 / 超时
    if (!silent) {
      notifyNetworkError(err)
    }
    return Promise.reject(normalizeAxiosError(err))
  }
)

/* ====================== 字段级可见性报告(M168) ====================== */

/**
 * 把"字段可见性报告"落到这次响应的数据上
 *
 * # 两个落点,缺一不可
 *
 *   ① **盖 Symbol 戳**(`stampFieldPolicyReport`)——
 *      报告跟着**数据对象**走:表格行、`initialValues`、详情 data,
 *      拿到这份数据的任何组件都能读回报告
 *      (读回接口见 utils/fieldPolicy.ts 的 readFieldPolicyReport)。
 *      盖的是 `resp.data.data`(业务数据本体)——
 *      信封(code/message/timestamp)不是业务行,不用盖。
 *
 *   ② **按页面归档**(`setLatestFieldPolicyReport`)——
 *      页面经常**重新拼**行对象(逐字段手抄 ⇒ 戳丢了)或重拼分页信封
 *      (file/list.tsx、folder.tsx)。这时戳没了,兜底就靠这条。
 *
 * ⚠️ **为什么要在这里做,而不是让每个页面自己读头** ——
 *   头只在这一个地方能拿到(拦截器是唯一看见原始响应的层);
 *   而且业务页面一行都不用改 —— 这正是 M168 的设计目标。
 *
 * ⚠️ **没有头 ⇒ 什么都不做** ——
 *   绝大多数响应没有字段级可见性参与(没配规则 / 公开接口),
 *   这条必须是零开销的快路径。
 */
function applyFieldPolicyReport(resp: AxiosResponse<ApiResponse>): void {
  const raw = getHeader(resp.headers, FIELD_POLICY_HEADER)
  if (!raw) return
  const report = parseFieldPolicyReport(raw)
  if (!report) return
  // ① 盖到数据本体上(行 / 行数组 / 分页信封的 data)
  const payload = resp.data?.data
  if (payload && typeof payload === 'object') {
    stampFieldPolicyReport(payload, report)
  }
  // ② 按页面归档(请求发起时记下的路径 —— 页面跳走了也不串)
  const pagePath =
    (resp.config as InternalAxiosRequestConfig & { _pagePath?: string })._pagePath ??
    currentPagePath()
  setLatestFieldPolicyReport(pagePath, report)
}

// 头的解析逻辑真源在 utils/fieldPolicy.ts;这里只是给它起个别名,
// 让上面那段读起来是"解析报告"而不是"解析头"。
const parseFieldPolicyReport = parseFieldPolicyHeader

/* ====================== 业务错误处理 ====================== */

/**
 * 判断一个错误是不是"请求被取消"
 *
 * 四种判法都要留:
 *   · axios.isCancel —— axios 官方判法(打的是 __CANCEL__ 标记)
 *   · error.code === 'ERR_CANCELED' —— axios 1.x 的标准 code
 *   · error.name === 'CanceledError' —— axios 1.x 的错误类名
 *     (拦截器里 catch 到的是 CanceledError 实例,code 有时会被别的层改写)
 *   · **error.code === 'CANCELED' / error.canceled === true** —— M110 补的:
 *     本文件自己的取消分支 reject 出去的就是这个形状
 *     (真源:utils/retry.ts 的 canceledError())。
 *     此前这个函数**认不出自己产生的取消对象** ⇒ 导出给业务层用的取消判定
 *     对框架自己抛的取消恒 false。
 */
export function isCanceled(error: unknown): boolean {
  if (!error) return false
  const e = error as {
    code?: unknown
    name?: unknown
    __CANCEL__?: unknown
    canceled?: unknown
  }
  return (
    e.code === 'ERR_CANCELED' ||
    e.code === 'CANCELED' ||
    e.canceled === true ||
    e.name === 'CanceledError' ||
    e.__CANCEL__ === true ||
    (typeof axios.isCancel === 'function' && axios.isCancel(error))
  )
}

/**
 * 把 AxiosError 规范化成对外 reject 的形状
 *
 * ⚠️ **code 给的是业务码(后端 body 里的 code),不是 HTTP 状态码** ——
 *   调用方要拿它查 i18n 翻译(tError),给状态码的话一次都查不中。
 *   body 里没有 code 的非标准响应才回落到状态码。
 */
function normalizeAxiosError(err: AxiosError): {
  code: number
  message: string
  requestId?: string
  data?: unknown
} {
  const status = err.response?.status
  const data = err.response?.data as ApiResponse | undefined
  return {
    code: typeof data?.code === 'number' ? data.code : (status ?? -1),
    message:
      data?.message ||
      (status ? `HTTP ${status}` : err.message || tKey('error.networkException')),
    requestId: data?.request_id,
    data: data?.data,
  }
}

/**
 * 判断一个 catch 到的值是不是"已经规范化过的错误对象"
 *
 * 规范化过的对象长这样:{ code, message, requestId?, data? } —— 没有 isAxiosError。
 * axios 自己的错误带 `isAxiosError` 标记,据此区分两者。
 */
function isNormalizedError(e: unknown): e is { code: number | string; message: string } {
  if (!e || typeof e !== 'object') return false
  const o = e as Record<string, unknown>
  if ('isAxiosError' in o) return false
  return 'code' in o && 'message' in o
}

/** 判断一个 catch 到的值是不是"能继续往下处理的 axios 错误" */
function isAxiosErrorLike(e: unknown): e is AxiosError {
  if (!e || typeof e !== 'object') return false
  const o = e as { isAxiosError?: boolean; config?: unknown; message?: unknown }
  if (o.isAxiosError === true) return true
  return o.config !== undefined && o.message !== undefined
}

/**
 * HTTP 层错误的统一提示
 *
 * ⚠️ **401 不走这里** —— 会话失效由 handleUnauthorized 处理(不受 silent 影响)
 */
function notifyHttpError(status: number, bizCode: number, fallback: string): void {
  switch (status) {
    case 403:
      message.error(tKey(ERROR_MESSAGES.FORBIDDEN))
      return
    case 404:
      message.error(tKey('error.notFound'))
      return
    case 500:
    case 502:
    case 503:
    case 504:
      message.error(tKey(ERROR_MESSAGES.SERVER_ERROR))
      return
    default:
      // ⚠️ 400 / 409 / 413 / 429 都落到这里。以前直接把**后端 body 里的
      //   中文 message** 甩给用户 —— 英文环境下弹出来的也是中文。
      //   现在先按业务码查 i18n 翻译(body 里带 code),查不到才回落后端原文。
      message.error(
        tError(bizCode, fallback || tKey('error.requestFailedWithStatus', undefined, { status })),
      )
  }
}

/** 网络错误 / 超时的提示 */
function notifyNetworkError(err: AxiosError): void {
  if (err.code === 'ECONNABORTED') {
    message.error(tKey('error.timeout'))
  } else if (err.message?.includes('Network Error')) {
    message.error(tKey(ERROR_MESSAGES.NETWORK_ERROR))
  } else {
    message.error(err.message || tKey('error.requestFailed'))
  }
}

function handleBizError(code: number, msg: string, _requestId?: string, silent = false) {
  // 401 业务码 — 不论 silent 与否,都要清登录态(否则下次仍 401)
  if (code === BizCode.UNAUTHORIZED) {
    handleUnauthorized()
    return
  }
  // silent: 业务页自己处理(比如表单字段内联报错) → 拦截器不弹 toast
  if (silent) {
    return
  }
  // 业务码 → i18n 翻译(M6),拿不到走后端 message,再拿不到走 unknown
  const i18nText = tError(code, msg)
  // 4xxx 业务参数 / 校验类用 message(轻量);其他用 notification(更醒目)
  if (code >= 40000 && code < 50000) {
    message.error(i18nText)
    return
  }
  notification.error({
    message: tKey('error.operationFailed'),
    description: i18nText,
    placement: 'topRight',
  })
}

function handleUnauthorized() {
  message.warning(tKey(ERROR_MESSAGES.UNAUTHORIZED))
  // 清理本地状态(reset 已清双后端, 不需要单独再 remove)
  try {
    useUserStore.getState().reset()
    cancelTokenRefresh()
  } catch {
    /* store 还没初始化时不处理 */
  }
  // 跳登录页(避免在登录页循环)
  if (window.location.pathname !== '/login') {
    const redirect = encodeURIComponent(window.location.pathname + window.location.search)
    window.location.href = `/login?redirect=${redirect}`
  }
}

/** 暴露 handleUnauthorized 给 tokenScheduler 使用 */
export { handleUnauthorized }

/* ====================== 便捷方法 ====================== */

export interface RequestOptions extends Omit<AxiosRequestConfig, 'url' | 'method' | 'data' | 'params' | 'baseURL' | 'onUploadProgress' | 'onDownloadProgress'> {
  /** 自定义错误处理(返回 true 表示已自行处理,不弹默认 toast) */
  silent?: boolean
  /**
   * 上传进度回调(§2.3 #9,M172)—— FormData 上传 / 大文件直传时用
   *
   * 收到的是归一化的 {@link HttpProgress}(total/percent 拿不到时是 null,
   * 不用自己判 axios 的 undefined)。底层是 axios 的 onUploadProgress。
   *
   * ⚠️ M30 的重试会**重发整个请求** ⇒ 每次重试进度会从头再报一遍 ——
   *   进度条要做"可回退"处理(或只在第一次报)。
   */
  onUploadProgress?: (p: HttpProgress) => void
  /** 下载进度回调 —— 大文件下载 / 导出时用(口径同上) */
  onDownloadProgress?: (p: HttpProgress) => void
  /**
   * AbortController signal —— 用于取消请求(§2.3 #10)
   *   - 组件卸载/页面切换时调 controller.abort() 立即终止 in-flight 请求
   *
   * ⚠️ 2026-10-25 M6 更正"已自动集成"这句措辞:
   *   useRequest 会把 signal 作为 **service 的最后一个参数** 传出来
   *   (service 声明成 `(signal?: AbortSignal) => ...` 就能拿到),
   *   但**要不要真的中止这次 HTTP 请求,取决于 service 有没有把它转给 axios** ——
   *   转了才是真取消(浏览器直接断连接),没转只是 hook 忽略这次结果而已。
   *   写法:`http.get('/x', params, { signal })`。
   */
  signal?: AbortSignal
  /**
   * M31:多 baseURL 通道 —— 传 channel 名('admin'/'owner'/'supplier'/...)或完整 URL
   *   - 不传 → 用 instance 默认 baseURL
   */
  baseURL?: Channel | string
}

/** 当前 default channel(模块级,运行时可改) */
let _defaultChannel: Channel = 'admin'

/** 设置全局默认通道(影响后续未显式传 baseURL 的请求) */
export function setDefaultChannel(c: Channel): void {
  _defaultChannel = c
  instance.defaults.baseURL = getBaseURL(c)
}

/** 读取当前默认通道 */
export function getDefaultChannel(): Channel {
  return _defaultChannel
}

/**
 * 临时块:在 fn 执行期间改默认通道,退出后还原
 *
 * ⚠️ M101:原来这里有个**跨通道串请求**的洞 ——
 *   `withChannel` 改的是**全局** `instance.defaults.baseURL`,而且是
 *   「改 → await fn() → 还原」,中间那个 `await` 让出执行权。于是:
 *
 *     await Promise.all([
 *       withChannel('owner',    () => ownerApi.list()),     // A
 *       withChannel('supplier', () => supplierApi.list()),  // B
 *     ])
 *
 *   A 改完默认值开始 await;B 进来把默认值改成 supplier;B 结束还原成 owner;
 *   这时 A 还在跑 —— **A 的请求打到 supplier 通道上去了**。
 *   owner 的 token 去打 supplier 的接口,轻则 404,重则**跨端数据串了**。
 *
 *   现在:**同一时刻只允许一个 withChannel 块**,后到的排队等前一个跑完。
 *   两个并发块不再交错。
 *
 * ⚠️ **残留风险(改不掉的)**:块**外面**发起的请求,只要恰好落在块执行的
 *   那段时间里,照样会拿到块内的默认通道 —— 因为通道是全局的。
 *   真要彻底避免,只有两条路:① 请求一律显式传 `baseURL`;
 *   ② 不传 baseURL 的请求统一走一个固定通道(不允许运行时切)。
 *   **单请求覆盖(第 2 种用法)没有这个问题**,能用就用它。
 */
let _channelLock: Promise<unknown> = Promise.resolve()

export async function withChannel<T>(c: Channel, fn: () => Promise<T>): Promise<T> {
  // 排队:等前一个 withChannel 块彻底结束(含它的 finally 还原)再进来
  const prevLock = _channelLock
  let release!: () => void
  _channelLock = new Promise<void>((resolve) => {
    release = resolve
  })
  await prevLock

  const prev = _defaultChannel
  setDefaultChannel(c)
  try {
    return await fn()
  } finally {
    setDefaultChannel(prev)
    release()
  }
}

/**
 * query 参数归一化:把分页参数的两个旧名翻成后端真正认的 `size`
 *
 * ⚠️ **为什么要有这一层**:
 *   后端只读 `page` + `size`(`BaseHandler.GetPage`,
 *   server/internal/api/handler/handler.go:86),
 *   **全仓不读 `page_size`**。而前端过去三处写法都不一样:
 *     · hooks/useTable.ts 运行时写的是 `pageSize`
 *     · types/api.ts 的 PageRequest 声明的是 `page_size`
 *     · api/approval.ts 自己翻了一层(当时注释里就记着"全仓 useTable
 *       页面的每页条数选择器都是失效的",但只在自己模块里补了)
 *   结果:凡是走 useTable 的页面,选了 50 条后端只回 20 条
 *   (页码是对的,因为 `page` 三方一致)。
 *
 * **为什么在出口翻而不是改类型 + 改十几个页面**:
 *   一次改动覆盖全部 30+ 列表端点,业务页面零改动、零回归面;
 *   旧名保留为可选别名,老代码继续能编译;
 *   翻译点只有一处,以后要收紧成只认 `size`,改这一处即可。
 *
 * 规则:显式的 `size` 优先;`page_size` / `pageSize` 是它的别名。
 */
export function normalizeQueryParams(
  params?: Record<string, unknown>,
): Record<string, unknown> | undefined {
  if (!params) return params
  if (!('page_size' in params) && !('pageSize' in params)) return params

  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(params)) {
    if (k === 'page_size' || k === 'pageSize') continue
    out[k] = v
  }
  if (out.size === undefined) {
    const alias = 'pageSize' in params ? params.pageSize : params.page_size
    if (alias !== undefined) out.size = alias
  }
  return out
}

/**
 * RequestOptions → axios config(§2.3 #9,M172)
 *
 * 进度回调在这层**包一层归一化**再交给 axios —— 这是它成为"一等选项"的落点:
 * 以前 axios 原生的 onUploadProgress 靠 `extends AxiosRequestConfig` 透传,
 * 类型上存在但没人知道能用,而且给的是裸 AxiosProgressEvent
 * (total 在无 Content-Length 时是 undefined,每个调用方都得自己判)。
 *
 * 其余字段原样透传 —— signal / silent / headers / timeout 等语义不变。
 */
function toAxiosConfig(opts?: RequestOptions): AxiosRequestConfig | undefined {
  if (!opts) return undefined
  const { onUploadProgress, onDownloadProgress, ...rest } = opts
  if (!onUploadProgress && !onDownloadProgress) return rest as AxiosRequestConfig
  return {
    ...(rest as AxiosRequestConfig),
    onUploadProgress: onUploadProgress
      ? (e) => onUploadProgress(normalizeProgress(e))
      : undefined,
    onDownloadProgress: onDownloadProgress
      ? (e) => onDownloadProgress(normalizeProgress(e))
      : undefined,
  }
}

export const http = {
  /**
   * ⚠️ M110:泛型参数原来写成 `instance.get<unknown, ApiResponse<T>>(...)` ——
   *   axios 的签名是 `get<T = any, R = AxiosResponse<T>, D = any>(...): Promise<R>`,
   *   第二个参数 R **顶掉了** `AxiosResponse` ⇒ 返回类型成了 `Promise<ApiResponse<T>>`
   *   ⇒ `r` 是 `ApiResponse<T>` ⇒ `r.data` 是 `T` ⇒ 再取 `.data` 就报
   *   "Property 'data' does not exist on type 'T'"(tsc 基线里的 5 条)。
   *   运行时其实是对的(axios 返回的确实是 AxiosResponse),**只是类型写错了** ——
   *   编译器因此没法校验 `r.data.data` 这一层。
   *   改成只传第一个泛型:`R` 回落成 `AxiosResponse<ApiResponse<T>>` ⇒ 类型对得上。
   */
  get<T = unknown>(url: string, params?: Record<string, unknown>, opts?: RequestOptions) {
    return instance
      .get<ApiResponse<T>>(url, { params: normalizeQueryParams(params), ...toAxiosConfig(opts) })
      .then((r) => r.data.data as T)
  },
  post<T = unknown>(url: string, data?: unknown, opts?: RequestOptions) {
    return instance.post<ApiResponse<T>>(url, data, toAxiosConfig(opts)).then((r) => r.data.data as T)
  },
  put<T = unknown>(url: string, data?: unknown, opts?: RequestOptions) {
    return instance.put<ApiResponse<T>>(url, data, toAxiosConfig(opts)).then((r) => r.data.data as T)
  },
  patch<T = unknown>(url: string, data?: unknown, opts?: RequestOptions) {
    return instance.patch<ApiResponse<T>>(url, data, toAxiosConfig(opts)).then((r) => r.data.data as T)
  },
  delete<T = unknown>(url: string, params?: Record<string, unknown>, opts?: RequestOptions) {
    return instance
      .delete<ApiResponse<T>>(url, { params: normalizeQueryParams(params), ...toAxiosConfig(opts) })
      .then((r) => r.data.data as T)
  },
  /**
   * 创建带 AbortController 的 http 客户端
   *   const ac = new AbortController()
   *   const req = http.withSignal(ac.signal)
   *   req.get('/users').then(...)
   *   // 组件卸载:
   *   ac.abort()
   */
  withSignal(signal: AbortSignal) {
    return {
      get: <T = unknown>(url: string, params?: Record<string, unknown>, opts?: Omit<RequestOptions, 'signal'>) =>
        http.get<T>(url, params, { ...opts, signal }),
      post: <T = unknown>(url: string, data?: unknown, opts?: Omit<RequestOptions, 'signal'>) =>
        http.post<T>(url, data, { ...opts, signal }),
      put: <T = unknown>(url: string, data?: unknown, opts?: Omit<RequestOptions, 'signal'>) =>
        http.put<T>(url, data, { ...opts, signal }),
      patch: <T = unknown>(url: string, data?: unknown, opts?: Omit<RequestOptions, 'signal'>) =>
        http.patch<T>(url, data, { ...opts, signal }),
      delete: <T = unknown>(url: string, params?: Record<string, unknown>, opts?: Omit<RequestOptions, 'signal'>) =>
        http.delete<T>(url, params, { ...opts, signal }),
    }
  },
  /**
   * 原始实例(仅限 http 方法覆盖不了的特殊场景)
   * ⚠️ M172:上传/下载进度已是一等选项(`onUploadProgress` / `onDownloadProgress`),
   *   不再需要绕到 raw 实例 —— ImageUploader 的裸 XHR 是历史遗留,迁移时改这里。
   */
  raw: instance,
}

export default instance
