/**
 * 多 baseURL 通道 —— M31 §2.3 #11 P1
 *
 * 场景:同仓三端共用一份 axios 客户端
 *   - admin   (后台管理)   默认
 *   - owner   (业主小程序)  H5 复用
 *   - supplier(供应商端)    后续扩展
 *
 * 用法:
 *   1) 全局默认:setDefaultChannel('owner')        ← 从 '@/api/client' 导入
 *   2) 单请求:http.get(url, params, { baseURL: 'owner' })
 *   3) 临时块:withChannel('owner', () => ownerApi.list())  ← 从 '@/api/client' 导入
 *
 * ⚠️ M101 更正:上面第 3 条以前写的是 `http.withBaseURL(...)` ——
 *   **这个方法从来就不存在**(导出名是 `withChannel`,而且是独立导出、
 *   不挂在 `http` 对象上)。照着注释写的人会拿到 undefined。
 *   同理 `http.setDefaultChannel` 也不存在 —— `setDefaultChannel` 是
 *   独立导出,不在 `http` 对象上。
 *
 * 配置来源:.env 的 VITE_API_BASE_<CHANNEL> 大写通道名
 *   VITE_API_BASE_ADMIN=/rapid-admin/v1
 *   VITE_API_BASE_OWNER=/owner/v1
 *   VITE_API_BASE_SUPPLIER=/supplier/v1
 * 未配则 fallback VITE_API_BASE,再 fallback '/rapid-admin/v1'
 */

export type Channel = 'admin' | 'owner' | 'supplier' | (string & {})

const DEFAULT_FALLBACK = '/rapid-admin/v1'

/** 取一个通道的 baseURL */
export function getBaseURL(channel?: Channel): string {
  if (!channel || channel === 'admin') {
    return (import.meta.env.VITE_API_BASE_ADMIN as string)
      || (import.meta.env.VITE_API_BASE as string)
      || DEFAULT_FALLBACK
  }
  const key = `VITE_API_BASE_${channel.toUpperCase()}`
  return (import.meta.env[key] as string)
    || (import.meta.env.VITE_API_BASE as string)
    || DEFAULT_FALLBACK
}

/** 列出所有已注册通道(扫描 env) */
export function listChannels(): Channel[] {
  const out: Channel[] = ['admin']
  const env = (import.meta.env as unknown) as Record<string, string | undefined>
  for (const k of Object.keys(env)) {
    if (k.startsWith('VITE_API_BASE_') && k !== 'VITE_API_BASE_ADMIN') {
      const suffix = k.replace('VITE_API_BASE_', '').toLowerCase()
      if (suffix) out.push(suffix)
    }
  }
  return out
}
