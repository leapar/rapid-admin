/**
 * BasicLayout —— 后台主布局
 *
 * 滚动策略 (各区域独立滚动):
 *   - 外层 Layout: height: 100vh (强制不超视口, 不驱动 body 滚动)
 *   - Sider: 子容器 flex column, Menu 区域 flex: 1 + overflow-y: auto (菜单长时内部滚动)
 *   - Content: overflow: hidden, 内层 div 自带 overflow-y: auto (内容长时内部滚动)
 *   - 表格: ManagementTable 已配 scroll.y, 表格内部独立滚动
 *
 * 配色 (白系):
 *   - Sider 白底 + 浅边框
 *   - Header 半透明白 + 毛玻璃
 *   - 品牌名移至 Header 左侧 (替代原 Sider logo 块)
 *   - 删除 Footer (品牌名已在 Header)
 */
import React from 'react'
import { Layout } from 'antd'
import { Outlet, useLocation } from 'react-router-dom'
import { useUIStore } from '@/stores/ui'
import { useTabsStore } from '@/stores/tabs'
import { useEventStream } from '@/hooks/useEventStream'
import Sidebar from './Sidebar'
import Header from './Header'
import Breadcrumb from './Breadcrumb'

const { Sider, Content } = Layout

export const BasicLayout: React.FC = () => {
  const collapsed = useUIStore((s) => s.sidebarCollapsed)
  const { pathname } = useLocation()
  const openTab = useTabsStore((s) => s.open)

  // M72 —— 打开通用事件通道(SSE)。整个登录态生命周期都持有一条连接:
  // 权限刷新 / 定时任务状态 / 站内信未读数 全走这一条。
  useEventStream()

  // 每次 pathname 变化(初次或路由跳转)就登记一条缓存槽位
  // - 已存在:什么都不做
  // - 不存在:加到 tabs 列表尾部(后面的 patchCache 要往这条槽位里写)
  React.useEffect(() => {
    openTab(pathname)
  }, [pathname, openTab])

  // 软 keepAlive:切走时把当前内容区滚动位置写到 tab.cache.scrollY,
  // 切回同一个 pathname 时从下面这段恢复。
  // ⚠️ 这个 effect 声明在 openTab 之后 —— React 同一个组件里按声明顺序跑,
  //   所以"新 tab 的槽位"一定先建好,下面的 patchCache 才写得进去。
  const patchCache = useTabsStore((s) => s.patchCache)
  const contentRef = React.useRef<HTMLDivElement>(null)
  const prevPathRef = React.useRef(pathname)
  React.useEffect(() => {
    if (prevPathRef.current !== pathname) {
      // 离开旧路由:存旧 scrollY
      const prev = contentRef.current
      if (prev) {
        patchCache(prevPathRef.current, { scrollY: prev.scrollTop })
      }
      // 进入新路由:恢复 scrollY
      const cur = contentRef.current
      if (cur) {
        const tab = useTabsStore.getState().tabs.find((t) => t.key === pathname)
        const sy = tab?.cache?.scrollY
        if (typeof sy === 'number') {
          // 等内容渲染完
          requestAnimationFrame(() => {
            if (contentRef.current) contentRef.current.scrollTop = sy
          })
        } else {
          cur.scrollTop = 0
        }
      }
      prevPathRef.current = pathname
    }
  }, [pathname, patchCache])

  return (
    <Layout style={{ height: '100vh', overflow: 'hidden' }}>
      <Sider
        trigger={null}
        collapsible
        collapsed={collapsed}
        width={240}
        collapsedWidth={64}
        style={{
          // ⚠️ 不写死白底 —— 深色模式跟随 CSS 变量切换(variables.css 暗色段)
          borderRight: '0.5px solid var(--app-split)',
          background: 'var(--app-surface-bg-strong)',
          height: '100vh',
        }}
      >
        {/*
         * 关键: 菜单区独立滚动
         *   - Sider 子容器 flex column, Menu 区域 flex: 1 + overflow-y: auto
         *   - 配 min-height: 0 (flex 子项 overflow 必要条件)
         *   - height: 100% 必须有: Sider 本身不是 display: flex, 这里 flex: 1 无效,
         *     必须显式 100% 撑满 Sider, Sidebar 的 overflowY: auto 才有约束面
         */}
        <div
          style={{
            height: '100%',
            minHeight: 0,
            display: 'flex',
            flexDirection: 'column',
          }}
        >
          <Sidebar />
        </div>
      </Sider>
      <Layout style={{ background: 'transparent' }}>
        <Header />
        <Content
          style={{
            margin: '0 16px',
            background: 'transparent',
            overflow: 'hidden',
            height: 'calc(100vh - 56px)',
          }}
        >
          {/*
           * 内容区不滚, 由业务页内的 ManagementTable Card body 接管滚动
           *  - Breadcrumb: 固定高度
           *  - Outlet 容器: flex:1 + overflow: auto(2026-10-18 加)
           *    业务页(如 /system/config 走 Collapse+Form,没 ManagementTable 接管滚动)
           *    内容超长时,这里出现页面内滚动条,而不是被 overflow:hidden 的祖先裁掉
           *    minHeight: 0 是 flex 子项 overflow 生效的必要条件
           */}
          <div
            style={{
              background: 'transparent',
              padding: '10px 0 16px 0',
              height: '100%',
              display: 'flex',
              flexDirection: 'column',
              minHeight: 0,
            }}
          >
            {/*
             * ⚠️ **2026-10-25:顶部的 TabsBar 标签条已删除**
             *   (用户:「顶部的 TabsBar 没什么用,而且整个界面不搭」)。
             *   原来这里渲染 <TabsBar />,在 Breadcrumb 上方一行。
             *   多 tab 相关的 store / hook 一并清掉,详见 stores/tabs.ts 的说明。
             */}
            <Breadcrumb />
            {/*
             * 2026-10-18:overflow 拆成 overflow-y:auto + overflow-x:hidden
             *   之前 overflow:auto 会让任何子页内部宽元素(Descriptions / List / Code / long Tag)
             *   撑出整个内容区的横向滚动条,即使页内 Card 自己 overflow:auto 也救不回来
             *   横向滚动应该由各页内 Card body 接管,Layout 永远不出横向滚动条
             *
             * 2026-10-19:加 ref 用于切 tab 时保存/恢复 scrollY(软 keepAlive)
             */}
            <div
              ref={contentRef}
              style={{ flex: 1, minHeight: 0, overflowX: 'hidden', overflowY: 'auto' }}
            >
              <Outlet />
            </div>
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}

export default BasicLayout
