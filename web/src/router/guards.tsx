/**
 * 路由守卫
 */
import React, { useEffect } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { Spin } from 'antd'
import { useAuth } from '@/hooks/useAuth'
import { usePermission } from '@/hooks/usePermission'
import { usePermissionStore } from '@/stores/permission'
import { useUserStore, getToken } from '@/stores/user'
import { getCurrentUser } from '@/api/auth'
import { useI18n } from '@/locales'
import { BizCode } from '@/types/api'

/**
 * 判断一个错误是不是"会话失效"
 *
 * ⚠️ **code 可能是业务码,也可能是 HTTP 状态码** —— `api/client.ts` 的
 * `normalizeAxiosError` 在 body 里有 code 时给业务码(40100),没有才回落
 * HTTP 状态码(401)。两个都要认,否则"token 过期"会被当成普通网络错误
 * 去重试(白等一轮),而真正的网络错误反而被当成掉线直接登出。
 *
 * ⚠️ 401 这一族有**三个**业务码,不是一个:
 *   后端 `errcode.go:188` 把 `40100-40199` 整个映射成 HTTP 401,
 *   body 里带的是具体码 —— 40100 未登录 / 40101 token 过期 /
 *   40102 token 无效。三个的共同点是"这个会话已经没了",
 *   **都不该重试**(拿同一个坏 token 重发一次,结果必然一样)。
 *   只认 40100 的话,token 过期(40101,最常见的那种)会走重试分支:
 *   白等 800ms + 白打一次请求,然后才清登录态。
 */
function isAuthFailure(e: unknown): boolean {
  const code = (e as { code?: unknown } | undefined)?.code
  return (
    code === 401 ||
    code === BizCode.UNAUTHORIZED ||
    code === BizCode.TOKEN_EXPIRED ||
    code === BizCode.TOKEN_INVALID
  )
}

/**
 * 判断一个错误是不是"权限不足"(M146b)
 *
 * ⚠️ **403 和 401 是两件完全不同的事,混在一起会冤死用户。**
 *
 *   · **401** = 这个会话没了(token 过期 / 被吊销)⇒ 该清登录态、跳登录页;
 *   · **403** = 会话**有效**,只是这个身份对当前资源没权限
 *     ⇒ **绝不能清登录态** —— 新 token 就在浏览器里,会话好好的。
 *
 * 历史上这两者被混为一谈,造成了「右上角切身份 → 切完就退出」:
 * 切到一个权限受限的身份 → `/auth/me` 403 → PermissionLoader 走
 * "其他错误"分支 → 重试一次 → 还是 403 → `reset()` 清 token → 跳登录页。
 * 用户明明还带着有效会话,却被赶走了,而且**没法切回去**(页面已经没了)。
 *
 * 403 还是**确定性**的:重试一百次结果一样,所以它不进重试分支。
 */
function isForbidden(e: unknown): boolean {
  const code = (e as { code?: unknown } | undefined)?.code
  return code === 403 || code === BizCode.FORBIDDEN
}

/* ====================== AuthGuard(已登录) ====================== */

export const AuthGuard: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isLoggedIn } = useAuth()
  const location = useLocation()
  if (!isLoggedIn) {
    return <Navigate to={`/login?redirect=${encodeURIComponent(location.pathname)}`} replace />
  }
  return <>{children}</>
}

/* ====================== GuestGuard(未登录) ====================== */

export const GuestGuard: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isLoggedIn } = useAuth()
  if (isLoggedIn) {
    return <Navigate to="/" replace />
  }
  return <>{children}</>
}

/* ====================== PermissionGuard(路由级) ====================== */

export interface RoutePermissionGuardProps {
  /**
   * 所需权限码(任一)
   *
   * ⚠️ **M103:这里既可以写权限码,也可以写角色 code。**
   *   `routes.tsx` 里有一批"有 X 权限**或**是超管"的写法:
   *   `anyOf={[PermSystem.USER_LIST, RoleCode.SUPER_ADMIN]}`
   *   以前这个守卫只查 `permissionCodes`,而角色 code 永远不在那个集合里
   *   → **"或超管"那一支恒不命中**,整个兜底是死代码。
   *   角色 code 与权限码是两套不相交的命名空间(`role_*` vs `user:list` /
   *   `button.*`),所以"任一命中"地查两个集合不会歧义。
   */
  anyOf?: string[]
  /** 所需权限码(全部) */
  allOf?: string[]
  children: React.ReactNode
}

export const RoutePermissionGuard: React.FC<RoutePermissionGuardProps> = ({
  anyOf,
  allOf,
  children,
}) => {
  /**
   * ⚠️ M102:必须用 `usePermission()`(订阅 store),不能用
   * `usePermissionStore.getState()`。
   *
   * `getState()` 是一次性快照,**不订阅** → 权限变了这个守卫**不重渲染**,
   * 路由级的鉴权结果会停在旧权限上:
   *   切换身份后,新身份没有的页面**照样进得去**(前端不拦),
   *   撤掉的权限**还在放行**。
   * 之前 `PermissionGuard` / `RoleGuard` 用的是 `usePermission()`(响应式),
   * 只有这个路由级守卫是 `getState()` —— 三处不一致,只有这处是错的。
   */
  const { has, hasRole } = usePermission()

  // ⚠️ M103:权限码查 `permissionCodes`,角色 code 查 `roleCodes`(见上面 anyOf 注释)
  const satisfied = (code: string) => has(code) || hasRole(code)

  if (anyOf?.length && !anyOf.some(satisfied)) return <Navigate to="/forbidden" replace />
  if (allOf?.length && !allOf.every(satisfied)) return <Navigate to="/forbidden" replace />
  return <>{children}</>
}

/* ====================== PermissionLoader(权限加载) ====================== */

/**
 * 应用启动时:
 * 1. 校验 token(若有)
 * 2. 拉取当前用户 + 权限 + 菜单
 * 3. 注入到 store
 *
 * 2026-10-18 修复: 之前这里手动拼 menu + api, 没拉 button,导致 RowAction 全空
 *   - 现在改走 usePermissionStore.fetchAccessible() 统一逻辑(里面含 menu/api/button)
 *   - 走 fetchAccessible 同时避免和切换身份流程逻辑分叉
 */
export const PermissionLoader: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { t } = useI18n()
  const [loading, setLoading] = React.useState(true)
  const setUserInfoFromServer = useUserStore((s) => s.setUserInfoFromServer)
  const reset = useUserStore((s) => s.reset)
  const fetchAccessible = usePermissionStore((s) => s.fetchAccessible)

  useEffect(() => {
    let disposed = false

    const init = async (attempt: number): Promise<void> => {
      const token = getToken()
      if (!token) return
      try {
        // 2026-10-18:后端 /auth/me 返 { user, resources } wrapper,解出 user 才能
        //   让 userInfo.is_platform_super_admin / userInfo.identities 真正可用
        const { user } = await getCurrentUser()
        // ⚠️ 2026-10-21:这里此前调的是 setLogin 且**故意不传 remember**
        // (让默认 true 生效)。后果是每次刷新页面都把会话重新写进
        // localStorage —— 不勾"记住我"的 sessionStorage 登录,第一次 F5
        // 就被升级成永久会话,关掉浏览器依然登录。"记住我"到这一层就白做了。
        // 现在改调 setUserInfoFromServer:只回填 userInfo + 按已选身份定位,
        // **不碰 token、不重新决定持久化策略**。启动路径不再有资格改写这个选择。
        setUserInfoFromServer(user)
        // 统一走 store.fetchAccessible(2026-10-18 修复:含 button.* 拉取,RowAction 才有 button 粒度 perm)
        await fetchAccessible()
      } catch (e) {
        /**
         * ⚠️ M102:以前这里是 `catch { reset(); ... }` —— **任何**失败都当
         * "登录态失效",于是:
         *   · 网络闪断 / 后端 500 → **清 token + 跳登录页**,用户得重新输密码;
         *   · 错误还被 `catch {}` 整个吞掉,日志里一个字都没有,事后查无可查。
         * 而同一个错误在 `useEventStream` 的 `onPermChanged` 里是明确
         * "不清登录态"的 —— 两处语义不一致。
         *
         * 现在按错误性质分开:
         *   · **会话失效(401 / 40100)** → 照旧清登录态。**不重试** ——
         *     重试只会让每次 token 过期都多打一遍 401。
         *   · **权限不足(403 / 40300)** → **保持登录态**(M146b),也不重试。
         *   · **其他错误(网络 / 5xx)** → 退避后**重试一次**;还失败才清登录态。
         *     一次瞬时抖动不该把人踢回登录页。
         */
        if (isForbidden(e)) {
          // ⚠️ M146b:403 = 会话有效、只是这个身份没权限。
          //   这里**什么都不清** —— token 留着、登录态留着,用户可以在
          //   右上角切回一个有权限的身份(IdentitySwitcher 读的是落盘的
          //   userInfo.identities,hydrate 已经把它恢复了)。
          //   菜单会是空的(权限没拉到),这是"这个身份确实没权限"的如实反映。
          // eslint-disable-next-line no-console
          console.error('[PermissionLoader] 当前身份无权访问(403),保持登录态', e)
          return
        }
        if (!isAuthFailure(e) && attempt < 1) {
          await new Promise((r) => setTimeout(r, 800))
          if (disposed) return
          return init(attempt + 1)
        }
        // eslint-disable-next-line no-console
        console.error('[PermissionLoader] 加载用户/权限失败,按登录态失效处理', e)
        reset()
        usePermissionStore.getState().reset()
      }
    }

    void init(0).finally(() => {
      if (!disposed) setLoading(false)
    })

    return () => {
      disposed = true
    }
  }, [setUserInfoFromServer, fetchAccessible, reset])

  if (loading) {
    return (
      <div
        style={{
          height: '100vh',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 12,
        }}
      >
        {/* ⚠️ M102:`tip` 在 antd 5 的默认(非 nest / 非 fullscreen)模式下
            **不生效** —— 它只在 nested / fullscreen 模式下才渲染成文案。
            也就是说这里原来那句 `tip={t('common.loading')}` 是**死代码**:
            "加载中..."从来没显示过,取而代之的是每次启动都打一条
            `[antd: Spin] tip only work in nest or fullscreen pattern.` 警告。
            改成自己渲染文案。 */}
        <Spin size="large" />
        <span style={{ color: 'var(--app-text-tertiary)' }}>{t('common.loading')}</span>
      </div>
    )
  }
  return <>{children}</>
}
