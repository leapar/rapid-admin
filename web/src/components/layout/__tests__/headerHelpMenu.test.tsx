/**
 * 顶栏 · 帮助下拉(2026-11-03 用户直报)
 *
 * ## 这条网守的是什么
 *
 * 用户报「右上角帮助按钮点了没反应」。它以前是一颗**死按钮**
 * (Tooltip + QuestionCircleOutlined,没有 onClick),要变成下拉:
 *
 *   · API 文档 → 站内路由 /api-docs(路由早就有,刻意不是菜单项 —— 保持,
 *     这里给入口);**当前页内跳转**,不是新标签(navigate)
 *   · 使用教程(GitBook)→ **新标签页**开 `{后端}/docs/`
 *     (window.open + noopener —— 开新页不丢后台现场,也不给新页 opener)
 *
 * ## 容易"看起来对、其实错"的地方
 *
 *  1. **「使用教程」绝不能走 `navigate`** —— /docs 是后端挂的静态站点,
 *     不在前端路由表里,SPA 路由一跳就是 NotFound。
 *  2. **window.open 必须带 'noopener,noreferrer'** —— 不带的话新页面拿到
 *     window.opener,教程页(后端渲染的第三方静态站)能反向操控后台页。
 *  3. **URL 来自 getBaseURL()** —— 写死同源 `/docs/` 在生产(API 独立域名)必 404。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 helpMenu 整个删掉、按钮改回无 onClick → 「下拉能展开」红
 *   · 把 `navigate('/api-docs')` 删掉 → 「API 文档」红(路径停在原地)
 *   · 把 guide 分支的 `window.open` 换成 `navigate('/docs/')` → 「使用教程」红
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { Header } from '@/components/layout/Header'
import { useUserStore } from '@/stores/user'
import type { Identity, UserInfo } from '@/types/user'

// 顶栏右侧那几个小组件跟这条网无关,而且各自会拉数据 / 起定时器 —— mock 成空
// (导出形态:InboxBell 具名,其余 default —— 见 headerAvatar.test.tsx 的头注释)
vi.mock('@/components/layout/IdentitySwitcher', () => ({ default: () => null }))
vi.mock('@/components/layout/InboxBell', () => ({ InboxBell: () => null }))
vi.mock('@/components/layout/ThemeSwitcher', () => ({ default: () => null }))
vi.mock('@/components/layout/LanguageSwitcher', () => ({ default: () => null }))
vi.mock('@/api/auth', () => ({ logout: vi.fn() }))
// 教程 URL 的后端 origin 来源 —— mock 掉环境差异,断言才能钉死
vi.mock('@/api/baseURL', () => ({ getBaseURL: vi.fn(() => 'https://api.example.com/rapid-admin/v1') }))

const IDENTITY: Identity = {
  id: 'i-1',
  role_code: 'role_super_admin',
  role_name: '超级管理员',
  project_id: '0',
  project_name: '',
  permissions: [],
  valid_from: '2026-01-01T00:00:00+08:00',
  valid_to: '',
  is_active: true,
  is_primary: true,
}

const USER: UserInfo = {
  id: 'u-1',
  username: 'zhangsan',
  nickname: '张三',
  real_name: '张三',
  phone: '138****1234',
  email: 'zhangsan@qq.com',
  avatar: '',
  identities: [IDENTITY],
  current_identity_id: 'i-1',
  is_platform_super_admin: true,
}

/** 路由探针:Header 里 navigate 之后,当前路径从这里读 */
function RouteProbe() {
  const location = useLocation()
  return <div data-testid="route-probe">{location.pathname}</div>
}

function renderHeader() {
  return render(
    <MemoryRouter initialEntries={['/some/page']}>
      <Header />
      <RouteProbe />
    </MemoryRouter>,
  )
}

/** 帮助按钮(图标按钮没有文字,靠 aria-label 定位 —— t('header.help') = 帮助文档) */
function helpButton(): HTMLElement {
  const el = screen.getByRole('button', { name: '帮助文档' })
  expect(el).not.toBeNull()
  return el
}

beforeEach(() => {
  vi.clearAllMocks()
  useUserStore.setState({
    userInfo: USER,
    currentIdentity: IDENTITY,
    isLoggedIn: true,
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('顶栏 · 帮助下拉(2026-11-03)', () => {
  it('点帮助按钮 → 下拉展开,两个入口都在(API 文档 / 使用教程)', async () => {
    renderHeader()
    fireEvent.click(helpButton())

    await waitFor(() => {
      // ⚠️ name 用正则:菜单项带 icon,aria-label(icon 的 "api"/"book")会
      //   拼进可访问名("api API 文档"),精确匹配会扑空
      expect(screen.getByRole('menuitem', { name: /API 文档/ })).not.toBeNull()
    })
    expect(screen.getByRole('menuitem', { name: /使用教程/ })).not.toBeNull()
  })

  it('API 文档 → 站内跳到 /api-docs(**当前页 navigate,不开新标签**)', async () => {
    // 挂个 spy 才能断言"没开新标签"(jsdom 的 window.open 不是默认 spy)
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(null)
    renderHeader()
    fireEvent.click(helpButton())

    const item = await screen.findByRole('menuitem', { name: /API 文档/ })
    fireEvent.click(item)

    await waitFor(() => expect(screen.getByTestId('route-probe').textContent).toBe('/api-docs'))
    // 站内路由跳转不该开新标签
    expect(openSpy).not.toHaveBeenCalled()
  })

  it('使用教程 → 新标签开 {API origin}/docs/(noopener,不带 /rapid-admin/v1 路径)', async () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(null)
    renderHeader()
    fireEvent.click(helpButton())

    const item = await screen.findByRole('menuitem', { name: /使用教程/ })
    fireEvent.click(item)

    await waitFor(() =>
      expect(openSpy).toHaveBeenCalledWith(
        'https://api.example.com/docs/',
        '_blank',
        'noopener,noreferrer',
      ),
    )
  })
})
