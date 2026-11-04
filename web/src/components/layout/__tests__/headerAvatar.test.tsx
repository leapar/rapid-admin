/**
 * 顶栏 · 用户头像(M124,2026-10-30)
 *
 * ## 这条网守的是什么
 *
 * 用户报「右侧顶部栏用户名称旁边也要显示用户设置的头像」。
 *
 * ⚠️ **顶栏以前是一个写死的通用人形图标** ——
 *   `<Avatar size="small" icon={<UserOutlined/>} style={{backgroundColor:'#1677ff'}} />`
 *   不管用户设没设过头像,永远是同一个蓝底小人。
 *
 * ## 两条容易"看起来对、其实错"的地方
 *
 *  1. **必须走 `UserAvatar` 组件,不能直接用 antd 的 `<Avatar src=...>`** ——
 *     `src` 是**空串**时 antd 会渲染 `<img src="">` = **浏览器裂图**,
 *     而且空 src **不触发 onError** ⇒ antd 的 icon 兜底永远等不到。
 *     (详见 components/common/UserAvatar.tsx 的文件头。)
 *
 *  2. **没有头像时必须回落成首字母圆牌,不是裂图** ——
 *     这正是 `UserAvatar` 存在的理由。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `<UserAvatar>` 换回 `<Avatar icon={<UserOutlined/>}/>`
 *                                        → 「顶栏显示用户自己的头像」变红
 *   · 把 `src={userInfo?.avatar}` 改成 `src={undefined}`
 *                                        → 「有头像时显示那张图」变红
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Header } from '@/components/layout/Header'
import { useUserStore } from '@/stores/user'
import type { Identity, UserInfo } from '@/types/user'

// 顶栏右侧那四个小组件跟这条网无关,而且各自会拉数据 / 起定时器 —— mock 成空
//
// ⚠️ **导出形态不一样**:`InboxBell` 是**具名导出**(`export const InboxBell`),
//   另外三个是 default 导出 —— mock 时写错形态,vitest 报的是
//   「No "InboxBell" export is defined on the mock」。
vi.mock('@/components/layout/IdentitySwitcher', () => ({ default: () => null }))
vi.mock('@/components/layout/InboxBell', () => ({ InboxBell: () => null }))
vi.mock('@/components/layout/ThemeSwitcher', () => ({ default: () => null }))
vi.mock('@/components/layout/LanguageSwitcher', () => ({ default: () => null }))
vi.mock('@/api/auth', () => ({ logout: vi.fn() }))

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
  avatar: '/rapid-admin/u-1/avatar/9/me.png',
  identities: [IDENTITY],
  current_identity_id: 'i-1',
  is_platform_super_admin: true,
}

function renderHeader() {
  return render(
    <MemoryRouter>
      <Header />
    </MemoryRouter>,
  )
}

/** 顶栏里那个头像元素(.ant-avatar) */
function headerAvatar(container: HTMLElement): HTMLElement {
  const el = container.querySelector('.ant-avatar') as HTMLElement
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

describe('顶栏 · 用户名旁边显示自己的头像', () => {
  it('显示的是**用户自己那张头像**(不是写死的通用人形图标)', () => {
    const { container } = renderHeader()
    const img = headerAvatar(container).querySelector('img')
    expect(img).not.toBeNull()
    expect(img!.getAttribute('src')).toBe('/rapid-admin/u-1/avatar/9/me.png')
  })

  it('没有头像时回落成首字母圆牌,**不是裂图**', () => {
    // ⚠️ antd 的 `<Avatar src="">` 会渲染 `<img src="">` = 浏览器裂图,
    //   而且空 src 不触发 onError ⇒ 连兜底的机会都没有。
    //   `UserAvatar` 组件专门解决这个。
    useUserStore.setState({ userInfo: { ...USER, avatar: '' } })
    const { container } = renderHeader()
    const avatar = headerAvatar(container)
    expect(avatar.querySelector('img')).toBeNull()
    expect(avatar.textContent).toContain('张')
  })

  it('用户名仍然显示在头像旁边', () => {
    renderHeader()
    expect(screen.getByText('张三')).toBeInTheDocument()
  })
})
