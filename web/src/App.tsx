/**
 * 根组件 —— 挂载路由
 * 不在此处理业务逻辑,仅做 Router 注入 + 全局兜底(Banner / ErrorBoundary)
 */
import { RouterProvider } from 'react-router-dom'
import { router } from '@/router'
import { OfflineBanner } from '@/components/common/OfflineBanner'

export default function App() {
  return (
    <>
      <OfflineBanner />
      <RouterProvider router={router} />
    </>
  )
}
