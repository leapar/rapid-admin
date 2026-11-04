/**
 * M101 —— §2.3 Axios Client / HTTP 链路审计的回归网(2026-10-26)
 *
 * 这一组用例锁的是 **HTTP 层自己**的行为,不是业务页的:
 *   · silent 到底管不管 HTTP 状态码错误
 *   · 一次失败到底弹几个 toast、报出去的是哪一次的错误
 *   · per-request baseURL 到底解不解析通道名
 *   · 调用方自己设的请求头会不会被拦截器覆盖
 *   · withChannel 的并发块会不会交错
 *
 * ⚠️ **每条断言都对应一个实测出来的事实**(M101 的探针记录):
 *   1. `silent` 以前**只**在成功拦截器(业务码那条路)被读,错误拦截器
 *      从头到尾没看过它 → HTTP 403 照样弹红条。
 *      后端把业务码 40300 映射成 **HTTP 403**(errcode.go:190),
 *      而 `api/inbox.ts` 的 `unreadCount`(挂在 Header 上、每个登录用户
 *      都会打)专门写了 `silent: true`,注释里明说"不静默的话,页面上每
 *      刷新一次就弹一个红色的'无权限'" —— 那个红条是真的会弹。
 *   2. 重试走 `instance.request(config)` → **完整重跑一遍拦截器链** →
 *      内层也弹一次 toast(一次失败弹两个红条);而且内层抛的**新错误**
 *      被外层 `catch {}` 整个吞掉,最后报出去的是**第一次**失败的码。
 *   3. per-request baseURL 那段是**死代码**(条件恒假)→ 传 'owner'
 *      时请求打到 `owner/system/users`(相对当前 origin 的相对路径),
 *      静默 404。
 *   4. 拦截器用 `config.headers['X-Request-Id']` 这种**精确匹配**去查
 *      调用方有没有设过 → 调用方写小写 `x-request-id` 时被当成"没设过"
 *      → 重新生成 → **覆盖掉调用方的值**。Idempotency-Key 同理 ——
 *      那等于**防重复提交失效**。
 *   5. `withChannel` 改的是全局 baseURL 且中间有 await → 两个并发块
 *      交错 → **owner 的请求打到 supplier 通道上**。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { AxiosError, AxiosHeaders } from 'axios'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import { message } from 'antd'
import { http, instance, withChannel, setDefaultChannel } from '@/api/client'

const originalAdapter = instance.defaults.adapter
const originalBaseURL = instance.defaults.baseURL

/**
 * 造一个"按 status 抛 AxiosError"的 adapter
 *
 * ⚠️ **自定义 adapter 必须自己 reject** —— axios 的 settle()(validateStatus
 *   判定 + 抛 AxiosError)是**内建 adapter 内部**做的事;自定义 adapter
 *   直接 resolve 一个 4xx/5xx 响应,axios 会把它当成功响应交给"成功
 *   拦截器",错误分支根本没被走到(实测:被当成 code=40300 的业务错误)。
 *   所以这里照内建 adapter 的做法:抛一个带 response 的 AxiosError。
 */
function mockStatusAdapter(
  handler: (config: InternalAxiosRequestConfig, call: number) => { status: number; code: number; message: string },
) {
  let call = 0
  const adapter: AxiosAdapter = async (config) => {
    call++
    const { status, code, message: msg } = handler(config, call)
    const response = {
      status,
      statusText: 'Error',
      data: { code, message: msg, request_id: 'r-m101' },
      headers: new AxiosHeaders(),
      config,
    }
    throw new AxiosError(
      `Request failed with status code ${status}`,
      AxiosError.ERR_BAD_RESPONSE,
      config,
      undefined,
      response,
    )
  }
  instance.defaults.adapter = adapter
}

/** 造一个"成功返回"的 adapter,顺带把 config 抓下来 */
function mockOkAdapter(capture?: (config: InternalAxiosRequestConfig) => void) {
  const adapter: AxiosAdapter = async (config) => {
    capture?.(config)
    return {
      data: { code: 0, message: 'ok', data: null, request_id: 'r-m101', timestamp: 0 },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  instance.defaults.adapter = adapter
}

function spyMessage() {
  return vi.spyOn(message, 'error').mockImplementation(() => undefined as never)
}

beforeEach(() => {
  // 重试退避调到 1ms —— 否则每个用例要白等最多 500ms 的随机 jitter
  vi.stubEnv('VITE_HTTP_RETRY_BASE_MS', '1')
})

afterEach(() => {
  instance.defaults.adapter = originalAdapter
  instance.defaults.baseURL = originalBaseURL
  vi.unstubAllEnvs()
})

/* ============================================================
 * B1 · silent 必须覆盖 HTTP 状态码错误
 * ============================================================ */

describe('B1 · silent 覆盖 HTTP 状态码错误', () => {
  it('silent:true + HTTP 403 → 不弹 toast(但请求仍然 reject)', async () => {
    const spy = spyMessage()
    mockStatusAdapter(() => ({ status: 403, code: 40300, message: '没有权限' }))

    await expect(
      // 这就是 api/inbox.ts 的 unreadCount 的写法
      http.get('/biz/inbox/unread-count', undefined, { silent: true }),
    ).rejects.toBeTruthy()

    expect(spy).not.toHaveBeenCalled()
  })

  it('对照组:不带 silent + HTTP 403 → 弹一次 toast', async () => {
    // 没有这条,上面那条就可能是"永远不弹"而不是"silent 生效了"
    const spy = spyMessage()
    mockStatusAdapter(() => ({ status: 403, code: 40300, message: '没有权限' }))

    await expect(http.get('/biz/inbox/unread-count')).rejects.toBeTruthy()

    expect(spy).toHaveBeenCalledTimes(1)
  })

  it('silent:true + HTTP 500 → 不弹 toast', async () => {
    const spy = spyMessage()
    mockStatusAdapter(() => ({ status: 500, code: 50000, message: '服务端错误' }))

    await expect(http.get('/x', undefined, { silent: true })).rejects.toBeTruthy()

    expect(spy).not.toHaveBeenCalled()
  })

  it('silent 抑制的是 HTTP 层错误,不影响业务码那条路(HTTP 200 + code != 0)', async () => {
    const spy = spyMessage()
    const adapter: AxiosAdapter = async (config) => ({
      data: { code: 40001, message: '必填参数缺失', data: null, request_id: 'r-m101', timestamp: 0 },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    })
    instance.defaults.adapter = adapter

    await expect(http.get('/x', undefined, { silent: true })).rejects.toBeTruthy()

    expect(spy).not.toHaveBeenCalled()
  })

  it('silent 不抑制 401 —— 会话失效不是页面自己的事,必须照常登出', async () => {
    // handleUnauthorized 会跳登录页;把地址栏停在 /login 上让它别跳,
    // 只观察"会话有没有被清"。
    window.history.replaceState({}, '', '/login')
    const spyErr = spyMessage()
    const spyWarn = vi.spyOn(message, 'warning').mockImplementation(() => undefined as never)
    mockStatusAdapter(() => ({ status: 401, code: 40100, message: '未登录' }))

    await expect(http.get('/x', undefined, { silent: true })).rejects.toBeTruthy()

    expect(spyWarn).toHaveBeenCalledTimes(1)
    expect(spyErr).not.toHaveBeenCalled()
    window.history.replaceState({}, '', '/')
  })
})

/* ============================================================
 * B2 · 重试链:一次失败只弹一个 toast,报的是最后一次的码
 * ============================================================ */

describe('B2 · 重试链的错误处理', () => {
  it('一次失败只弹一个 toast(不是两个)', async () => {
    const spy = spyMessage()
    // 第一次 500(可重试) → 第二次 400(不可重试) → 结束
    mockStatusAdapter((_config, call) =>
      call === 1
        ? { status: 500, code: 50001, message: '服务端错误' }
        : { status: 400, code: 40001, message: '必填参数缺失' },
    )

    await expect(http.get('/system/users')).rejects.toBeTruthy()

    expect(spy).toHaveBeenCalledTimes(1)
  })

  it('reject 出去的是**最后一次**失败的码,不是第一次的', async () => {
    mockStatusAdapter((_config, call) =>
      call === 1
        ? { status: 500, code: 50001, message: '服务端错误' }
        : { status: 400, code: 40001, message: '必填参数缺失' },
    )

    await expect(http.get('/system/users')).rejects.toMatchObject({ code: 40001 })
  })

  it('不可重试的失败(403)只弹一次', async () => {
    const spy = spyMessage()
    mockStatusAdapter(() => ({ status: 403, code: 40300, message: '没有权限' }))

    await expect(http.get('/system/users')).rejects.toBeTruthy()

    expect(spy).toHaveBeenCalledTimes(1)
  })

  it('重试链里内层的 401 不会被外层再弹一次错误提示', async () => {
    // 第一次 500(可重试)→ 退避期间 token 过期 → 重试那次拿到 401
    // → 内层走 401 分支(规范化之后 reject)→ 外层必须**原样透传**,
    //    不能"按第一次的 500"再弹一个 SERVER_ERROR。
    window.history.replaceState({}, '', '/login')
    const spyErr = spyMessage()
    const spyWarn = vi.spyOn(message, 'warning').mockImplementation(() => undefined as never)
    mockStatusAdapter((_config, call) =>
      call === 1
        ? { status: 500, code: 50001, message: '服务端错误' }
        : { status: 401, code: 40100, message: '未登录' },
    )

    await expect(http.get('/system/users')).rejects.toBeTruthy()

    // 内层的 401 走 handleUnauthorized(那是 warning,不是 error)
    expect(spyWarn).toHaveBeenCalledTimes(1)
    // 外层不能多弹一个错误提示
    expect(spyErr).not.toHaveBeenCalled()
    window.history.replaceState({}, '', '/')
  })

  it('重试链不会无限发:最多 1 次原始 + 3 次重试', async () => {
    let calls = 0
    const adapter: AxiosAdapter = async (config) => {
      calls++
      const response = {
        status: 500,
        statusText: 'Error',
        data: { code: 50001, message: '服务端错误', request_id: 'r-m101' },
        headers: new AxiosHeaders(),
        config,
      }
      throw new AxiosError(
        'Request failed with status code 500',
        AxiosError.ERR_BAD_RESPONSE,
        config,
        undefined,
        response,
      )
    }
    instance.defaults.adapter = adapter
    spyMessage()

    await expect(http.get('/system/users')).rejects.toBeTruthy()

    expect(calls).toBe(4) // 1 次原始 + DEFAULT_MAX_ATTEMPTS(3)次重试
  })
})

/* ============================================================
 * B3 · per-request baseURL 的通道名解析
 * ============================================================ */

describe('B3 · per-request baseURL', () => {
  it("通道名 'owner' → 该通道的 baseURL(不是相对路径 'owner/...')", async () => {
    vi.stubEnv('VITE_API_BASE_OWNER', '/owner/v1')
    let captured: InternalAxiosRequestConfig | undefined
    mockOkAdapter((c) => {
      captured = c
    })

    await http.get('/system/users', undefined, { baseURL: 'owner' })

    expect(captured?.baseURL).toBe('/owner/v1')
  })

  it('完整 URL 原样用(不被当成通道名)', async () => {
    let captured: InternalAxiosRequestConfig | undefined
    mockOkAdapter((c) => {
      captured = c
    })

    await http.get('/system/users', undefined, { baseURL: 'https://other.example.com/api' })

    expect(captured?.baseURL).toBe('https://other.example.com/api')
  })

  it('路径原样用(不被当成通道名)', async () => {
    let captured: InternalAxiosRequestConfig | undefined
    mockOkAdapter((c) => {
      captured = c
    })

    await http.get('/system/users', undefined, { baseURL: '/owner/v1' })

    expect(captured?.baseURL).toBe('/owner/v1')
  })
})

/* ============================================================
 * B6 · 调用方自己设的请求头不能被拦截器覆盖
 * ============================================================ */

describe('B6 · 调用方的请求头不被覆盖', () => {
  it("小写 'x-request-id' 不会被重新生成的 UUID 顶掉", async () => {
    let captured: InternalAxiosRequestConfig | undefined
    mockOkAdapter((c) => {
      captured = c
    })

    await instance.get('/x', { headers: { 'x-request-id': 'CALLER-LOWER' } })

    expect(captured?.headers.get('X-Request-Id')).toBe('CALLER-LOWER')
  })

  it("大写 'X-Request-Id' 同样保留", async () => {
    let captured: InternalAxiosRequestConfig | undefined
    mockOkAdapter((c) => {
      captured = c
    })

    await instance.get('/x', { headers: { 'X-Request-Id': 'CALLER-UPPER' } })

    expect(captured?.headers.get('X-Request-Id')).toBe('CALLER-UPPER')
  })

  it("小写 'idempotency-key' 不会被顶掉(顶掉 = 防重复提交失效)", async () => {
    let captured: InternalAxiosRequestConfig | undefined
    mockOkAdapter((c) => {
      captured = c
    })

    await instance.post('/x', {}, { headers: { 'idempotency-key': 'CALLER-KEY' } })

    expect(captured?.headers.get('Idempotency-Key')).toBe('CALLER-KEY')
  })

  it('没设过的时候照旧自动生成', async () => {
    let captured: InternalAxiosRequestConfig | undefined
    mockOkAdapter((c) => {
      captured = c
    })

    await instance.get('/x')

    expect(captured?.headers.get('X-Request-Id')).toBeTruthy()
  })

  it('写操作没设过 Idempotency-Key 时自动生成一个', async () => {
    let captured: InternalAxiosRequestConfig | undefined
    mockOkAdapter((c) => {
      captured = c
    })

    await instance.post('/x', {})

    expect(captured?.headers.get('Idempotency-Key')).toBeTruthy()
  })
})

/* ============================================================
 * B4 · withChannel 的并发块不能交错
 * ============================================================ */

describe('B4 · withChannel 并发', () => {
  it('两个并发 withChannel 块各自看到自己的通道(不串)', async () => {
    vi.stubEnv('VITE_API_BASE_OWNER', '/owner/v1')
    vi.stubEnv('VITE_API_BASE_SUPPLIER', '/supplier/v1')
    const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

    const [ownerBase, supplierBase] = await Promise.all([
      withChannel('owner', async () => {
        await sleep(30)
        return instance.defaults.baseURL
      }),
      withChannel('supplier', async () => {
        await sleep(60)
        return instance.defaults.baseURL
      }),
    ])

    expect(ownerBase).toBe('/owner/v1')
    expect(supplierBase).toBe('/supplier/v1')
  })

  it('块结束后默认通道还原', async () => {
    vi.stubEnv('VITE_API_BASE_OWNER', '/owner/v1')
    // 起点归一 —— 上一条用例留下的全局状态不该影响这条
    setDefaultChannel('admin')
    const before = instance.defaults.baseURL

    await withChannel('owner', async () => {
      expect(instance.defaults.baseURL).toBe('/owner/v1')
    })

    expect(instance.defaults.baseURL).toBe(before)
  })
})
