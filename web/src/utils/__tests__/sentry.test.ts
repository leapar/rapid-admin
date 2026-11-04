/**
 * utils/sentry.ts 回归网(2026-10-29 M109)
 *
 * 这一层此前**一条网都没有**,而它是"线上错误能不能查得到"的唯一通道。
 *
 * 锁住的:
 *   ① DSN 为空 / dev 模式 → 完全 no-op(不 init,reportError 降级 console.error);
 *   ② 有 DSN → init 恰好一次,参数正确(dsn / environment / **不传死选项**);
 *   ③ **不再自己装 window 的 error / unhandledrejection 监听** ——
 *      Sentry v11 的 globalHandlers 集成已经接管了(window.onerror 被**覆盖**),
 *      自己再装一份 = 每个错误上报两次(2026-10-29 M109 P1);
 *   ④ beforeSend 过滤 AbortError,且 **exc.value 不是字符串时不炸**
 *      (此前 `.includes()` 直接 TypeError,而 beforeSend 抛异常 = 这条事件被丢弃);
 *   ⑤ Sentry 用户上下文里的 **role 来自当前身份的 role_code**
 *      (此前写的是 UserInfo 上根本不存在的 `roleCode`,恒为 undefined);
 *   ⑥ **切换身份后 role 会跟着更新**(此前订阅只看 userInfo.id,切身份不触发);
 *   ⑦ 登出 → setUser(null)。
 *
 * 反向探针(见 scripts/probe_m109.mjs):把每一处修复拆掉,对应用例必须变红。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

const sentry = vi.hoisted(() => ({
  init: vi.fn(),
  setUser: vi.fn(),
  setTag: vi.fn(),
  captureException: vi.fn(),
  addBreadcrumb: vi.fn(),
}))

// ⚠️ 必须 mock 整个 '@sentry/react' —— utils/sentry.ts 用 `import * as Sentry`,
//   具名导入的 mock 会漏掉命名空间访问。
vi.mock('@sentry/react', () => sentry)

import type { UserState } from '@/stores/user'

const USER = {
  id: '1001',
  username: 'e2e_house_admin',
  nickname: '管理员',
  phone: '138****0000',
  avatar: '',
  identities: [],
  current_identity_id: '2001',
}

const IDENTITY_A = {
  id: '2001',
  role_code: 'platform_admin',
  role_name: '平台管理员',
  project_id: '1',
  project_name: '平台',
  permissions: [],
  valid_from: '2026-01-01',
  valid_to: '2026-12-31',
  is_active: true,
  is_primary: true,
}

const IDENTITY_B = { ...IDENTITY_A, id: '2002', role_code: 'supplier' }

/**
 * 重新加载被测模块(它有模块级状态 initialized / enabled,只能 init 一次)。
 * ⚠️ resetModules 之后必须**同时**动态 import store —— 否则测试里拿到的
 *   store 和模块里拿到的 store 是两个实例(各自一份 zustand 状态)。
 */
async function loadSentry(env: Record<string, string>) {
  vi.resetModules()
  vi.unstubAllEnvs()
  for (const [k, v] of Object.entries(env)) vi.stubEnv(k, v)
  const [mod, store] = await Promise.all([import('@/utils/sentry'), import('@/stores/user')])
  return { mod, store }
}

const PROD_ENV = {
  VITE_SENTRY_DSN: 'https://key@example.invalid/1',
  MODE: 'production',
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  vi.unstubAllEnvs()
  vi.resetModules()
})

describe('sentry · 开关', () => {
  it('DSN 为空 → 不 init,reportError 降级成 console.error', async () => {
    const { mod } = await loadSentry({ VITE_SENTRY_DSN: '', MODE: 'production' })
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    mod.initSentry()

    expect(sentry.init).not.toHaveBeenCalled()

    const err = new Error('boom')
    mod.reportError(err, { tag: 'unit' })
    expect(consoleSpy).toHaveBeenCalledWith('[error-report]', err, { tag: 'unit' })

    // breadcrumb 也不该发出去
    mod.reportBreadcrumb('hi')
    expect(sentry.addBreadcrumb).not.toHaveBeenCalled()

    consoleSpy.mockRestore()
  })

  it('dev 模式 → 不 init', async () => {
    const { mod } = await loadSentry({
      VITE_SENTRY_DSN: 'https://key@example.invalid/1',
      MODE: 'development',
    })

    mod.initSentry()

    expect(sentry.init).not.toHaveBeenCalled()
  })

  it('有 DSN → init 恰好一次(重复调用是 no-op),参数符合约定', async () => {
    const { mod } = await loadSentry(PROD_ENV)

    mod.initSentry()
    mod.initSentry()

    expect(sentry.init).toHaveBeenCalledTimes(1)
    const opts = sentry.init.mock.calls[0]![0] as Record<string, unknown>
    expect(opts.dsn).toBe('https://key@example.invalid/1')
    expect(opts.environment).toBe('production')
    // ⚠️ **不能传** sendDefaultPii:Sentry v11 已经没有这个选项了
    //   (v11.8.0 全仓 0 处引用),传了 tsc 直接报 TS2353。
    //   这条断言就是"别把它加回来"的守卫。
    expect(opts).not.toHaveProperty('sendDefaultPii')
  })
})

describe('sentry · 全局捕获只留一道(M109 P1)', () => {
  it('init 之后**没有**自己再装 window 的 error / unhandledrejection 监听', async () => {
    const { mod } = await loadSentry(PROD_ENV)

    // ⚠️ spy 要装在 initSentry **之前**,但要晚于模块加载 ——
    //   否则会把 import 期间的监听也算进来。
    const addSpy = vi.spyOn(window, 'addEventListener')

    mod.initSentry()

    const types = addSpy.mock.calls.map((c) => c[0])
    expect(types).not.toContain('error')
    expect(types).not.toContain('unhandledrejection')

    addSpy.mockRestore()
  })
})

describe('sentry · beforeSend 过滤', () => {
  async function getBeforeSend() {
    const { mod } = await loadSentry(PROD_ENV)
    mod.initSentry()
    const opts = sentry.init.mock.calls[0]![0] as {
      beforeSend: (event: unknown) => unknown
    }
    return opts.beforeSend
  }

  it('AbortError / aborted 一律滤掉', async () => {
    const beforeSend = await getBeforeSend()

    expect(beforeSend({ exception: { values: [{ type: 'AbortError', value: 'x' }] } })).toBeNull()
    expect(
      beforeSend({ exception: { values: [{ type: 'Error', value: 'request aborted' }] } }),
    ).toBeNull()
  })

  it('exc.value 不是字符串时**不炸**,而且事件照常放行', async () => {
    // 回归:此前是 `exc?.value?.includes('aborted')` —— value 是 number 时
    // TypeError,而 beforeSend 抛异常 = Sentry 内部 catch 掉 ⇒ 这条事件直接丢。
    const beforeSend = await getBeforeSend()
    const event = { exception: { values: [{ type: 'Error', value: 12345 }] } }

    expect(() => beforeSend(event)).not.toThrow()
    expect(beforeSend(event)).toBeTruthy()
  })

  it('普通错误放行', async () => {
    const beforeSend = await getBeforeSend()

    expect(
      beforeSend({ exception: { values: [{ type: 'TypeError', value: 'x is not a function' }] } }),
    ).toBeTruthy()
  })

  it('框架自己的取消形状(CANCELED)也滤掉(M110)', async () => {
    // M110:`api/client.ts` 的取消分支和**重试退避被打断**都 reject
    // `{ code: 'CANCELED', message: 'canceled', canceled: true }`
    // (真源 `utils/retry.ts` 的 `canceledError()`)—— 它不是 AbortError ⇒
    // 上一条过滤认不出它 ⇒ 用户主动取消会被当成"错误"上报。
    const beforeSend = await getBeforeSend()

    // Sentry 对非 Error 的 rejection 值会字符串化
    expect(
      beforeSend({
        exception: {
          values: [
            {
              type: 'Object',
              value: '{"code":"CANCELED","message":"canceled","canceled":true}',
            },
          ],
        },
      }),
    ).toBeNull()

    // 原始对象形态(没被字符串化)
    expect(
      beforeSend({
        exception: { values: [{ type: 'Object', value: { code: 'CANCELED', canceled: true } }] },
      }),
    ).toBeNull()

    // axios 自己的 CanceledError
    expect(
      beforeSend({ exception: { values: [{ type: 'CanceledError', value: 'canceled' }] } }),
    ).toBeNull()

    // ⚠️ 正文里恰好写着 CANCELED 的普通错误**不能**被误伤
    expect(
      beforeSend({
        exception: { values: [{ type: 'Error', value: 'payment was CANCELED by user' }] },
      }),
    ).toBeTruthy()
  })
})

describe('sentry · 用户上下文', () => {
  it('role 取自**当前身份**的 role_code(不是 UserInfo.roleCode)', async () => {
    const { mod, store } = await loadSentry(PROD_ENV)
    mod.initSentry()

    store.useUserStore.setState({
      userInfo: USER,
      currentIdentity: IDENTITY_A,
    } as Partial<UserState>)

    expect(sentry.setUser).toHaveBeenLastCalledWith({
      id: '1001',
      username: 'e2e_house_admin',
      role: 'platform_admin',
    })
  })

  it('切换身份后 role 跟着更新', async () => {
    // 回归:订阅此前只看 userInfo?.id,而 switchIdentity 只 set currentIdentity
    //       ⇒ 切完身份 Sentry 里的 role 还停在旧身份上。
    const { mod, store } = await loadSentry(PROD_ENV)
    mod.initSentry()

    store.useUserStore.setState({
      userInfo: USER,
      currentIdentity: IDENTITY_A,
    } as Partial<UserState>)
    expect(sentry.setUser).toHaveBeenLastCalledWith(
      expect.objectContaining({ role: 'platform_admin' }),
    )

    store.useUserStore.setState({ currentIdentity: IDENTITY_B } as Partial<UserState>)
    expect(sentry.setUser).toHaveBeenLastCalledWith(expect.objectContaining({ role: 'supplier' }))
  })

  it('登出 → setUser(null)', async () => {
    const { mod, store } = await loadSentry(PROD_ENV)
    mod.initSentry()

    store.useUserStore.setState({
      userInfo: USER,
      currentIdentity: IDENTITY_A,
    } as Partial<UserState>)
    store.useUserStore.setState({ userInfo: null, currentIdentity: null } as Partial<UserState>)

    expect(sentry.setUser).toHaveBeenLastCalledWith(null)
  })
})

describe('sentry · 手动上报', () => {
  it('enabled 时 reportError 走 Sentry.captureException,ctx 挂在 extra 上', async () => {
    const { mod } = await loadSentry(PROD_ENV)
    mod.initSentry()

    const err = new Error('boom')
    mod.reportError(err, { tag: 'unit', componentStack: 'at Foo' })

    expect(sentry.captureException).toHaveBeenCalledWith(err, {
      extra: { tag: 'unit', componentStack: 'at Foo' },
    })
  })

  it('reportBreadcrumb 把 message / data 交给 Sentry', async () => {
    const { mod } = await loadSentry(PROD_ENV)
    mod.initSentry()

    mod.reportBreadcrumb('switch identity', { id: '2002' })

    expect(sentry.addBreadcrumb).toHaveBeenCalledWith({
      message: 'switch identity',
      data: { id: '2002' },
      level: 'info',
    })
  })
})
