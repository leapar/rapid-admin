/**
 * Header —— 顶部栏(皮肤沉浸式)
 *
 * 配色全部来自皮肤对象(useSkin):
 *   - 背景: comp.layoutHeaderBg —— 浅玻璃(white/black:78% 透 + blur)或
 *     沉浸实底(gov 国旗红 / museum 深棕 / tech 深蓝)
 *   - 文字: comp.headerTextColor 系,**内联 remap 到 --app-text* 变量** ——
 *     子组件(品牌名/用户名/铃铛/身份切换)都消费这些变量,自动跟随顶栏
 *     深浅;antd text Button 不吃 CSS 变量继承,由 global.css 的
 *     `.ant-layout-header .ant-btn-text` 规则统一接管
 *   - 底边: comp.headerBorderColor
 *
 * 退化: 不支持 backdrop-filter 的浏览器自动 fallback 到不透明底 (CSS 兼容写法)
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
import { useSkin } from '@/theme/useSkin'
import { UserAvatar } from '@/components/common/UserAvatar'
import { logout as logoutApi } from '@/api/auth'
import { getBaseURL } from '@/api/baseURL'
import { gitbookDocsUrl } from './helpMenu'
import IdentitySwitcher from './IdentitySwitcher'
import { InboxBell } from './InboxBell'
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

  // 皮肤 comp(2026-11-05 沉浸式顶栏):背景/文字/底边单一来源
  // ⚠️ 必须走 useSkin()(带 registry 变更订阅):皮肤编辑器试穿是同 id
  //   换对象,只订阅 store 的话顶栏背景在试穿中不动(用户报过这个 bug)
  const headerComp = useSkin().comp

  // 头部图标按钮统一样式 (跟随顶栏文字色, 见文件头 —— 沉浸式顶栏下是浅色)
  const iconBtnStyle: React.CSSProperties = {
    color: 'var(--app-text-secondary)',
  }

  return (
    <AntHeader
      style={{
        // 顶栏背景/文字/底边全部来自皮肤对象(沉浸式:gov 红实底 / museum 深棕 /
        // tech 深蓝;white/black 白/黑玻璃)—— 单一来源是 skin.comp
        background: headerComp.layoutHeaderBg,
        // ⚠️ 内联 remap --app-text* 到顶栏专用色:子组件(品牌名/用户名/铃铛/
        //   身份切换)都消费这些变量,深底顶栏自动切换浅色文字
        ...({
          '--app-text': headerComp.headerTextColor,
          '--app-text-secondary': headerComp.headerTextSecondaryColor,
          '--app-text-tertiary': headerComp.headerTextTertiaryColor,
        } as React.CSSProperties),
        backdropFilter: 'saturate(180%) blur(12px)',
        WebkitBackdropFilter: 'saturate(180%) blur(12px)',
        padding: '0 20px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        height: 56,
        lineHeight: '56px',
        borderBottom: `0.5px solid ${headerComp.headerBorderColor}`,
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
