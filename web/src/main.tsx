/**
 * 应用入口
 * 1. 引入全局样式 (variables.css + global.css)
 * 2. Mock 启动
 * 3. 挂载前的一次性引导(恢复登录态 / 续期调度 / 空闲计时 / 公开配置 / Sentry)
 * 4. 渲染 <AppErrorBoundary><App /></AppErrorBoundary>
 *
 * ⚠️ **主题 / 语言 provider 与错误边界装配已搬走**(2026-10-29 M109):
 *   · `components/common/LangRoot.tsx` —— Antd ConfigProvider(主题 / 语言 / 浏览器标题)
 *   · `components/common/AppErrorBoundary.tsx` —— 错误边界三层装配
 *   原因见 AppErrorBoundary.tsx 的注释(核心:边界装配要能被单测覆盖)。
 */
import React from 'react'
import ReactDOM from 'react-dom/client'

import App from './App'
import './styles/global.css'
import './styles/variables.css'

// Mock 启动
import { installMock } from './mock'
installMock()

// 启动时恢复登录态(从 localStorage / sessionStorage)
// 必须在 React 挂载前同步执行, 否则首屏会闪一下"未登录"
import { hydrateUserStore } from './stores/user'
hydrateUserStore()

// M18: 启动时恢复 token 自动刷新调度(如果已登录)
//   - 必须放在 hydrateUserStore() 之后
//   - 同步检查 expireAt,如果已过期立即刷,否则 schedule
import { bootstrapTokenRefresh, getTokenSync } from './utils/tokenScheduler'
bootstrapTokenRefresh()

// M8: 建立跨标签页会话同步的订阅(2026-10-25)
//   - **必须在这里就建好**:BroadcastChannel 的消息不排队,订阅晚了会错过
//     兄弟标签页发来的"新 token 组"广播 —— 那样续期竞态就救不回来。
//   - 与是否登录无关:未登录时建着也没成本(没有消息可收)。
getTokenSync()

// 空闲超时 30min(SRS §2.2.1 #5 的 N,2026-10-21 定)—— 必须在 hydrate 之后,
//   否则首屏还没恢复登录态就开始计时。
//   与"记住我"正交:勾了照样 30min 掉线。
import { startIdleTimer } from './utils/idleTimer'
startIdleTimer()

// 匿名运行时开关(图形验证码开没开)—— 登录页要靠它决定渲不渲染验证码框。
//   - 必须登录前拉:登录页本身就是未登录态,拿不到挂在 Auth() 下的 /platform/configs
//   - 不 await:不阻塞首屏渲染;store 未拉到时按"开启"处理(见 stores/publicConfig.ts)
import { usePublicConfigStore } from './stores/publicConfig'
usePublicConfigStore.getState().fetch()

// M27: Sentry 错误上报(§2.5 #5 P1)—— 必须在 React 挂载前初始化
//   - VITE_SENTRY_DSN 为空时完全 no-op,零开销
//   - dev 模式自动 no-op
import { initSentry } from './utils/sentry'
initSentry()

// 错误边界装配(2026-10-29 M109 从本文件搬走)
import { AppErrorBoundary } from './components/common/AppErrorBoundary'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <AppErrorBoundary>
      <App />
    </AppErrorBoundary>
  </React.StrictMode>,
)
