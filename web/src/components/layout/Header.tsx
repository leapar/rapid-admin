/**
 * Header —— 顶部栏 (白系, 毛玻璃)
 *
 * 配色:
 *   - 背景: rgba(255,255,255,0.78) + backdrop-filter: blur(12px)
 *   - 文字: var(--app-text) #1f2937
 *   - 阴影: var(--app-shadow-l1) 营造"漂浮"分离感
 *   - 底边: 0.5px 浅灰(全站细线刻度)
 *
 * 退化: 不支持 backdrop-filter 的浏览器自动 fallback 到不透明白底 (CSS 兼容写法)
 */
// ⚠️ import type 而不是 import —— React 在本文件里只当类型用(React.FC /
//   React.CSSProperties),实值导入会被 consistent-type-imports 判成 error。
import type React from 'react'
import { Layout, Space, Button, Dropdown, Tooltip, App } from 'antd'
import {
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  LogoutOutlined,
  UserOutlined,
  FullscreenOutlined,
  QuestionCircleOutlined,
  ExclamationCircleOutlined,
  ApiOutlined,
  BookOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { useUIStore } from '@/stores/ui'
import { usePublicConfigStore } from '@/stores/publicConfig'
import { useAuth } from '@/hooks/useAuth'
import { useI18n } from '@/locales'
import { UserAvatar } from '@/components/common/UserAvatar'
import { logout as logoutApi } from '@/api/auth'
import { getBaseURL } from '@/api/baseURL'
import { gitbookDocsUrl } from './helpMenu'
import IdentitySwitcher from './IdentitySwitcher'
import { InboxBell } from './InboxBell'
import ThemeSwitcher from './ThemeSwitcher'
import SkinSwitcher from './SkinSwitcher'
import LanguageSwitcher from './LanguageSwitcher'

const { Header: AntHeader } = Layout

export const Header: React.FC = () => {
  const { t } = useI18n()
  const navigate = useNavigate()
  const { modal } = App.useApp()
  const collapsed = useUIStore((s) => s.sidebarCollapsed)
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)
  // 平台名(后端 branding 配置下发,开源通用化)。空串 = 没配 → 落回语言包。
  const appName = usePublicConfigStore((s) => s.appName)
  const { userInfo, logout } = useAuth()

  /**
   * 退出登录 — 先调后端 revoke jti,再清本地
   * 失败时仍清本地(避免用户卡死),给个非阻塞 warning
   */
  const handleLogout = () => {
    modal.confirm({
      title: t('header.logoutTitle'),
      icon: <ExclamationCircleOutlined />,
      content: t('header.logoutDesc'),
      okText: t('header.logoutOk'),
      okButtonProps: { danger: true },
      cancelText: t('common.cancel'),
      maskClosable: false,
      onOk: async () => {
        try {
          await logoutApi()
        } catch {
          /* 网络错误时仍清本地 */
        }
        logout()
        window.location.href = '/login'
      },
    })
  }

  const userMenu = {
    items: [
      { key: 'profile', icon: <UserOutlined />, label: t('header.profile') },
      { key: 'logout', icon: <LogoutOutlined />, label: t('header.logout'), danger: true },
    ],
    onClick: ({ key }: { key: string }) => {
      if (key === 'profile') {
        navigate('/profile')
      } else if (key === 'logout') {
        handleLogout()
      }
    },
  }

  /**
   * 帮助下拉(2026-11-03 用户直报:帮助按钮以前是死的,点了没反应)。
   *
   *   · API 文档 → 站内路由 /api-docs(刻意不是菜单项,这是文档页的既有定位,
   *     保持不变 —— 只从这里给个入口);
   *   · 使用教程 → 后端挂的 GitBook(/docs,同 swagger 一道 BasicAuth),
   *     新标签页打开 —— 教程是"看的时候不想丢掉后台现场"的那种页面。
   *
   * ⚠️ URL 不能写死:后端 origin 取决于部署(见 helpMenu.ts 的规则说明)。
   */
  const helpMenu = {
    items: [
      { key: 'api-docs', icon: <ApiOutlined />, label: t('header.apiDocs') },
      { key: 'guide', icon: <BookOutlined />, label: t('header.userGuide') },
    ],
    onClick: ({ key }: { key: string }) => {
      if (key === 'api-docs') {
        navigate('/api-docs')
      } else if (key === 'guide') {
        window.open(gitbookDocsUrl(getBaseURL(), window.location.origin), '_blank', 'noopener,noreferrer')
      }
    },
  }

  // 头部图标按钮统一样式 (深色, 圆形, hover 浅橙)
  const iconBtnStyle: React.CSSProperties = {
    color: 'var(--app-text-secondary)',
  }

  return (
    <AntHeader
      style={{
        // §2.8 #19: 顶栏背景走 CSS 变量, light 透明 + 暗色半透 slate
        // (light 用 78% 白, dark 用 78% 深 slate, 都用 --app-surface-bg-strong 做基色)
        background: 'color-mix(in srgb, var(--app-surface-bg-strong) 78%, transparent)',
        backdropFilter: 'saturate(180%) blur(12px)',
        WebkitBackdropFilter: 'saturate(180%) blur(12px)',
        padding: '0 20px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        height: 56,
        lineHeight: '56px',
        borderBottom: '0.5px solid var(--app-split)',
        // L1 阴影: 营造与内容区的漂浮分离
        boxShadow: 'var(--app-shadow-l1)',
        position: 'sticky',
        top: 0,
        zIndex: 10,
      }}
    >
      <Space size="middle" align="center">
        <Button
          type="text"
          icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
          onClick={toggleSidebar}
          style={iconBtnStyle}
        />
        {/* 品牌名 (替代原 Footer + Sider 品牌块) */}
        <span
          style={{
            fontSize: 16,
            fontWeight: 600,
            color: 'var(--app-text)',
            letterSpacing: '-0.01em',
            whiteSpace: 'nowrap',
            userSelect: 'none',
          }}
        >
          {appName || t('layout.brandLong')}
        </span>
      </Space>

      <Space size="middle">
        <LanguageSwitcher />
        <SkinSwitcher />
        <ThemeSwitcher />
        <Tooltip title={document.fullscreenElement ? t('header.exitFullscreen') : t('header.fullscreen')}>
          <Button
            type="text"
            icon={<FullscreenOutlined />}
            onClick={() => {
              if (document.fullscreenElement) {
                document.exitFullscreen()
              } else {
                document.documentElement.requestFullscreen()
              }
            }}
            style={iconBtnStyle}
          />
        </Tooltip>
        {/*
         * ⚠️ 嵌套顺序同 LanguageSwitcher(Dropdown 外、Tooltip 内):Tooltip
         *   直接包 Button,ref 落在真实 DOM 上;rc-trigger 原生支持嵌套 Trigger,
         *   事件会组合后透传到 Button(详见 LanguageSwitcher.tsx 的注释)。
         */}
        <Dropdown menu={helpMenu} placement="bottomRight" trigger={['click']}>
          <Tooltip title={t('header.help')}>
            {/* aria-label:图标按钮没有文字,测试和无障碍都靠它定位 */}
            <Button
              type="text"
              icon={<QuestionCircleOutlined />}
              style={iconBtnStyle}
              aria-label={t('header.help')}
            />
          </Tooltip>
        </Dropdown>
        {/*
         * M72 在这里换掉了写死的 count={3} 假角标,接上真未读数;但当时没有
         * 列表页,铃铛点不动 —— 当时的选择是宁可留一个哑弹,也不交付"只有
         * 图标没有内容"的半截列表页。M74 把列表弹层补上,整块搬进 InboxBell,
         * 角标逻辑(unreadCount 来自 useEventStream)、点击、标已读都在那里。
         */}
        <InboxBell />
        <Dropdown menu={userMenu} placement="bottomRight">
          <a onClick={(e) => e.preventDefault()}>
            <Space style={{ cursor: 'pointer' }}>
              {/*
                ⚠️ **M124(2026-10-30):这里以前是写死的通用人形图标**
                `<Avatar icon={<UserOutlined/>}>` —— 不管用户设没设过头像,
                顶栏永远显示同一个蓝底人形。
                现在走 `UserAvatar`:有头像显示自己的,没有就回落成首字母圆牌。

                ⚠️ **为什么必须是 `UserAvatar` 而不是 antd 的 `<Avatar src=...>`**:
                `src` 是空串时 antd 会渲染 `<img src="">` = **浏览器裂图**,
                而且空 src **不触发 onError** ⇒ 兜底永远等不到。
                (详见 components/common/UserAvatar.tsx 的文件头。)
              */}
              <UserAvatar
                size="small"
                src={userInfo?.avatar}
                name={userInfo?.nickname ?? userInfo?.username}
              />
              <span style={{ color: 'var(--app-text)', fontWeight: 500 }}>
                {userInfo?.nickname ?? userInfo?.username}
              </span>
            </Space>
          </a>
        </Dropdown>
        <IdentitySwitcher />
      </Space>
    </AntHeader>
  )
}

export default Header
