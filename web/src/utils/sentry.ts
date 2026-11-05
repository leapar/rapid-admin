/**
 * 错误上报 —— Sentry 集成(§2.5 #5 P1)
 *
 * 设计原则:
 *   - Opt-in:VITE_SENTRY_DSN 为空时 no-op(运行时零开销)
 *   - 不上报开发环境(MODE=development 也不上报)
 *   - React 渲染错误由 `components/common/ErrorBoundary` 捕获 → 走 reportError()
 *   - 全局未捕获 JS 错误 / 未处理 rejection 由 **SDK 自己**接管(见下方 ⚠️)
 *   - 自动附 user context(从 user store 读 user_id + 当前身份的 role_code)
 *   - 手动上报入口:reportError(err, ctx) 给业务代码用
 *
 * ⚠️ **全局捕获只有一道,不是两道**(2026-10-29 M109):
 *   Sentry v11 的 `globalHandlers` 集成(默认集成之一)会**接管**
 *   `window.onerror` 与 `window.onunhandledrejection`。
 *   实测路径:`@sentry/core/build/cjs/instrument/globalError.js` 里是
 *   `worldwide.GLOBAL_OBJ.onerror = function (...)` —— **覆盖属性**,
 *   不是 addEventListener;覆盖完还会打上 `__SENTRY_INSTRUMENTED__ = true`。
 *   而 `window.onerror` 这个属性本身就是 `error` 事件的监听器 ——
 *   它和我们自己 `window.addEventListener('error', …)` **两个都会跑**。
 *   ⇒ 此前本文件自己又装了一份 ⇒ 每个未捕获错误 / 未处理 rejection
 *     **被上报两次**(Sentry 里两条内容几乎相同的事件)。
 *   ⇒ 现在:全局捕获交给 SDK,本文件**不再**自己装监听器。
 *     回归网见 `utils/__tests__/sentry.global.test.ts`(真 SDK,断言
 *     `window.onerror.__SENTRY_INSTRUMENTED__ === true`)。
 *
 * ⚠️ **SDK .tree-shake 不掉**(2026-10-29 M109 修正):
 *   头注释原来写"build 时 tree-shake 走 default 分支" —— **不成立**。
 *   `import * as Sentry from '@sentry/react'` 是静态 import,而 initSentry()
 *   在 main.tsx 顶层被无条件调用 ⇒ Rollup 没法把这条链删掉,SDK 一定进包。
 *   能省的只是**运行时**:DSN 为空时根本不 init,不发任何网络请求。
 *   真要 tree-shake 得改成动态 import(且 initSentry 变异步)—— 收益不值那个复杂度。
 *
 * 注意:
 *   - Sentry 采样率默认 0.1(线上流量大),可在 env 调
 *   - **PII 默认不上报**(2026-10-29 M109 修正):
 *     Sentry v11 已经**移除了 `sendDefaultPii` 选项** —— 实测 v11.8.0 的
 *     @sentry/core / @sentry/browser / @sentry/react **全仓 0 处引用**。
 *     SDK 不再自动附带请求体 / 用户数据,只有**我们显式 attach 的**东西才会发出去。
 *     此前这里传着一个 `sendDefaultPii: false`,它**什么也没做**(死选项),
 *     反而让 tsc 报 TS2353(基线错误之一)。
 *     同时头注释里"beforeSend 钩子过滤 PII"也是**假的** —— beforeSend
 *     只过滤"取消"(AbortError / 框架自己的 CANCELED 形状),不过滤任何字段。
 *   - 关闭 Replay(框架层只做错误追踪,session replay 留业务侧启用)
 */
import * as Sentry from '@sentry/react'
import { useUserStore } from '@/stores/user'
import { useUIStore } from '@/stores/ui'

let initialized = false
let enabled = false

/**
 * 初始化 Sentry
 *   - 必须早于 ReactDOM.createRoot 调
 *   - VITE_SENTRY_DSN 空 → 完全跳过,零开销
 */
export function initSentry(): void {
  if (initialized) return
  initialized = true

  const dsn = (import.meta.env.VITE_SENTRY_DSN as string) || ''
  const env = (import.meta.env.MODE as string) || 'development'

  // 两道防线:空 DSN / dev 模式 → no-op
  if (!dsn) {
    return
  }
  if (env === 'development') {
    return
  }
  enabled = true

  const release = (import.meta.env.VITE_APP_VERSION as string) || 'unknown'
  const tracesSampleRate = Number(import.meta.env.VITE_SENTRY_TRACES_SAMPLE_RATE ?? '0.1')
  const replaysSampleRate = Number(import.meta.env.VITE_SENTRY_REPLAYS_SAMPLE_RATE ?? '0')

  Sentry.init({
    dsn,
    environment: env,
    release,
    tracesSampleRate,
    replaysSessionSampleRate: replaysSampleRate,
    // 业务层定制:beforeSend 把所有 error 透传(白名单由业务层加)
    beforeSend(event) {
      // 过滤已知 noise:AbortError(请求取消,M11)不算 bug
      const exc = event.exception?.values?.[0]
      // ⚠️ `exc.value` **不保证是字符串**(2026-10-29 M109):
      //   抛出的不是 Error(抛了个对象 / 数字)时,后端会把它塞成非字符串。
      //   此前直接 `exc?.value?.includes('aborted')` —— value 是 number 时
      //   `number.includes` 是 undefined ⇒ **TypeError**。
      //   而 beforeSend 抛异常 = 这条事件被 Sentry 内部 catch 掉直接丢弃 ⇒
      //   "过滤器自己炸了"反而把真实错误吞了。
      const value = typeof exc?.value === 'string' ? exc.value : ''
      if (exc?.type === 'AbortError' || value.includes('aborted')) {
        return null
      }
      // ⚠️ **框架自己的取消形状**(2026-10-29 M110):
      //   `{ code: 'CANCELED', message: 'canceled', canceled: true }`
      //   (真源:`utils/retry.ts` 的 `canceledError()` —— `api/client.ts` 的取消分支、
      //   以及**重试退避被打断**都走它)—— 它**不是** AbortError ⇒ 上一条过滤认不出它
      //   ⇒ 被取消的请求会作为"错误"上报(用户主动取消 = 正常操作,不是 bug)。
      //   Sentry 对非 Error 的 rejection 值会字符串化 ⇒ 认 `'"CANCELED"'`(**带引号**,
      //   免得误伤正文里恰好写着 CANCELED 的错误消息);同时认原始对象形态。
      const cancelCode = (exc?.value as { code?: unknown } | undefined)?.code
      if (cancelCode === 'CANCELED' || value.includes('"CANCELED"')) {
        return null
      }
      // axios 自己的 CanceledError(未经 client.ts 规范化时)
      if (exc?.type === 'CanceledError') {
        return null
      }
      return event
    },
    // 业务层定制:beforeBreadcrumb 屏蔽 console.log/debug
    beforeBreadcrumb(breadcrumb) {
      if (breadcrumb.category === 'console' && breadcrumb.level === 'log') {
        return null
      }
      return breadcrumb
    },
  })

  // 同步 user context(从 user store)
  syncUserContext()

  // 监听 user store 变化 → 自动 setUser / clearUser
  //
  // ⚠️ **两个维度都要盯**(2026-10-29 M109):
  //   · userInfo.id  —— 登录 / 登出 / 换用户
  //   · currentIdentity.id —— **切换身份**(switchIdentity 只 set currentIdentity,
  //     不动 userInfo)。此前只看 userInfo.id ⇒ 切换身份后 Sentry 里的 role
  //     一直停在旧身份上,按角色筛错误会筛错。
  useUserStore.subscribe((state, prev) => {
    if (
      state.userInfo?.id !== prev?.userInfo?.id ||
      state.currentIdentity?.id !== prev?.currentIdentity?.id
    ) {
      syncUserContext()
    }
  })

  // ⚠️ 这里**刻意不装** window.addEventListener('error' / 'unhandledrejection') ——
  //    SDK 的 globalHandlers 集成已经接管了,自己再装一份就是重复上报。
  //    详见文件头的说明。
}

/** 同步当前 user 信息到 Sentry context */
function syncUserContext(): void {
  if (!enabled) return
  const state = useUserStore.getState()
  const user = state.userInfo
  if (user?.id) {
    // ⚠️ role 来自**当前身份**的 role_code(2026-10-29 M109 修正):
    //   `UserInfo` 上**没有** roleCode 字段 —— 此前写的是 `user.roleCode`,
    //   恒为 undefined(它本身还是 tsc 的一个基线错误:sentry.ts:111)。
    //   真正的角色在 identity 上:用户可以在多个项目里是不同角色,
    //   身份切换后这个值跟着变(靠上面的订阅同步)。
    Sentry.setUser({
      id: String(user.id),
      username: user.username,
      role: state.currentIdentity?.role_code,
    })
  } else {
    Sentry.setUser(null)
  }
  // 加 tag 方便查询(2026-11-05 皮肤扁平化:theme 轴退役,改记皮肤 id ——
  // 皮肤即整体外观,含明暗信息)
  const lang = useUIStore.getState().lang
  const skin = useUIStore.getState().skin
  Sentry.setTag('app.lang', lang)
  Sentry.setTag('app.theme', skin)
}

/** 业务层手动上报(代替直接 Sentry.captureException) */
export function reportError(err: unknown, ctx?: Record<string, unknown>): void {
  if (!enabled) {
    // 没初始化 Sentry,降级 console
    // eslint-disable-next-line no-console
    console.error('[error-report]', err, ctx)
    return
  }
  Sentry.captureException(err, { extra: ctx })
}

/** 业务层打 breadcrumb(给后续 error 留上下文) */
export function reportBreadcrumb(msg: string, data?: Record<string, unknown>): void {
  if (!enabled) return
  Sentry.addBreadcrumb({ message: msg, data, level: 'info' })
}
