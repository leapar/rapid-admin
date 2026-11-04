/**
 * Mock 数据 —— 开发期使用
 *
 * 使用方式:
 * - .env.development 中 VITE_USE_MOCK=true
 * - main.tsx 检测后 import 这个文件
 * - 这里覆盖 axios adapter,实现请求拦截
 *
 * 注意:此为骨架,仅在 VITE_USE_MOCK=true 时启用
 */
import { BizCode } from '@/types/api'
import { tKey } from '@/locales'
import type { AxiosRequestConfig } from 'axios'

interface MockResponse {
  code: number
  message: string
  data: any
  request_id: string
  timestamp: number
}

/** 业务模块 mock 集合 */
const mocks: Record<string, (config: AxiosRequestConfig) => any> = {
  // 示例:GET /rapid-admin/v1/auth/me
  //
  // ⚠️ **M103:角色名不再写死中文,改成从字典取(`tKey`)。**
  //   两个原因:
  //     ① 「写死中文 i18n 守门网」:字典里一旦有了 `role.superAdmin`
  //        (= '超级管理员'),这里写死的同款中文就变成"重复" → 守门网变红。
  //        而 mock 返的是**服务端响应体**,里面的中文是**服务端数据**
  //        (角色名那种),不是前端界面文案 —— 守门网管的是后者。
  //        从字典取,两边都不冲突:mock 里没有写死中文,英文环境下
  //        mock 数据也跟着翻译。
  //     ② mock 的角色 code 以前是 `super_admin` —— **后端从来没签发过**
  //        这个 code(真值是 `role_super_admin`),跟 B2 是同一类错。
  'GET:/auth/me': () => ({
    id: 'mock-user-1',
    username: 'admin',
    realName: tKey('role.superAdmin'),
    phone: '13800138000',
    email: 'admin@rapid-admin.local',
    departmentId: 'dept-1',
    departmentName: '系统运维部',
    identities: [
      {
        id: 'identity-1',
        roleCode: 'role_super_admin',
        roleName: tKey('role.superAdmin'),
        deptId: 'dept-1',
        deptName: '系统运维部',
      },
    ],
    permissions: ['*'],
    createdAt: '2025-01-01T00:00:00Z',
  }),

  'GET:/dashboard/overview': () => ({
    file_total: 42,
    file_today: 3,
    file_storage_bytes: 1073741824,
    folder_total: 6,
    user_online: 2,
  }),

  // 兜底:返回空成功
  // ⚠️ 分页字段是 `size`(后端 response.PageData 就是 list/total/page/size)
  __default__: () => ({ list: [], total: 0, page: 1, size: 20 }),
}

function matchMock(method: string, url: string) {
  const cleanUrl = url.replace(/^\/admin\/v1/, '').split('?')[0]
  const key = `${method.toUpperCase()}:${cleanUrl}`
  return mocks[key] || mocks['__default__']
}

export function installMock() {
  if (import.meta.env.VITE_USE_MOCK !== 'true') return

  // 这里不直接覆盖 axios,而是暴露一个 mockApi
  // 业务层如果想用 mock,可以引入 mockApi
  console.info('[mock] installed, VITE_USE_MOCK=true')
}

export function mockResponse(data: any, message = 'success'): MockResponse {
  return {
    code: BizCode.SUCCESS,
    message,
    data,
    request_id: `mock-${Date.now()}`,
    timestamp: Date.now(),
  }
}

export { mocks, matchMock }
