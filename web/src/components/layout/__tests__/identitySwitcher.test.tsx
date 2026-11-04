/**
 * 身份切换器 · 高敏角色必须短信二次验证(2026-10-28 M103)
 *
 * ## 这条网守的是什么
 *
 *   `IdentitySwitcher` 里有一份"高敏角色"名单:
 *
 *     const HIGH_RISK_ROLES = [RoleCode.SUPER_ADMIN, PLATFORM_OPS_LEAD, PLATFORM_OPS]
 *
 *   名单里的角色**切换时必须弹短信二次验证**;名单外的直接切。
 *
 *   2026-11-03 用户拍板(推翻 M104 的配套口径):**没绑手机号的高敏身份
 *   免验直接切** —— M104 的硬拒等于"没绑号的账号永远切不进高敏身份",
 *   对没配短信通道的部署是条死路。前后端同口径(后端 SwitchIdentity 对
 *   callerPhone == "" 跳过短信校验)。绑了手机号的仍然必须验(见下)。
 *
 *   ⚠️ M103 之前 `RoleCode.SUPER_ADMIN` 的值是 `role_platform_super_admin`
 *     —— **后端从来没签发过这个 code**,所以真超管的 role_code
 *     (`role_super_admin`) 压根不在名单里 →
 *     **切到超管身份不需要任何二次验证**。这是个安全洞,不是显示问题。
 *
 *   顺带守一条显示:角色名必须显示**翻译后的文案**
 *   (`ROLE_NAME_MAP[code]` 是 i18n key,不是文案 —— 直接渲染会显示
 *   "role.superAdmin" 这种原始 key)。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   把 `constants/roles.ts` 的 `SUPER_ADMIN` 改回 `role_platform_super_admin`
 *   → 本文件立刻变红(超管不再要求短信)。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from 'antd'
import { IdentitySwitcher } from '@/components/layout/IdentitySwitcher'
import { useUserStore } from '@/stores/user'
import { switchIdentity, getCurrentUser } from '@/api/auth'
import { resourceApi } from '@/api/role'
import { session } from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'
import type { Identity, UserInfo } from '@/types/user'

vi.mock('@/api/auth', () => ({
  switchIdentity: vi.fn(),
  sendSmsCode: vi.fn(),
  verifySmsCode: vi.fn(),
  // 2026-10-28(M104):短信改由服务端代发 —— 前端只有脱敏手机号,
  // 拿它发短信后端会拒(validPhone 要求 11 位纯数字)。
  sendAccountSms: vi.fn(),
  getCurrentUser: vi.fn(),
}))

vi.mock('@/api/role', () => ({
  // ⚠️ 实现放在 beforeEach 里(见下),工厂里只留空壳 ——
  //   `restoreMocks: true` 会把工厂里写的实现一起抹掉。
  resourceApi: { accessible: vi.fn() },
}))

const switchIdentityMock = vi.mocked(switchIdentity)
const getCurrentUserMock = vi.mocked(getCurrentUser)
const accessibleMock = vi.mocked(resourceApi.accessible)

function identity(id: string, roleCode: string, roleName: string, isPrimary = false): Identity {
  return {
    id,
    role_code: roleCode,
    role_name: roleName,
    project_id: '0',
    project_name: '',
    permissions: [],
    valid_from: '2026-01-01T00:00:00+08:00',
    valid_to: '',
    is_active: true,
    is_primary: isPrimary,
  }
}

/**
 * ⚠️ **身份上的 role_code 一律写后端字面量,不用 `RoleCode.X` 常量。**
 *
 *   理由:这份用例要守的是"**前端认的 code 和后端签发的 code 对不对得上**"。
 *   如果这里用 `RoleCode.SUPER_ADMIN`,那么常量一改,身份上的 code 也跟着改
 *   —— 于是"高敏名单里有没有它"永远是"有",测的就不是前后端是否一致,
 *   而是"常量自己跟自己一致" → **恒绿,探针打不中**。
 *
 *   写成字面量之后,把 `constants/roles.ts` 的 `SUPER_ADMIN` 改回
 *   `role_platform_super_admin`,本文件立刻变红 —— 那才是真的洞。
 */
const SUPER = identity('i-super', 'role_super_admin', '超级管理员')
const NORMAL = identity('i-normal', 'role_readonly', '只读查看员')
const CURRENT = identity('i-current', 'role_house_admin', '房源管理员', true)

const USER: UserInfo = {
  id: 'u1',
  username: 'admin',
  nickname: '管理员',
  phone: '138****1234',
  avatar: '',
  identities: [CURRENT, SUPER, NORMAL],
  current_identity_id: 'i-current',
  is_platform_super_admin: true,
}

function renderSwitcher() {
  return render(
    <App>
      <IdentitySwitcher />
    </App>,
  )
}

/** 打开下拉菜单 */
async function openDropdown() {
  fireEvent.click(screen.getByText('管理员'))
  await waitFor(() => expect(screen.getAllByText('超级管理员').length).toBeGreaterThan(0))
}

beforeEach(() => {
  vi.clearAllMocks()
  switchIdentityMock.mockResolvedValue({
    token: 't',
    refresh_token: 'r',
    expires_in: 3600,
    refresh_expires_in: 86400,
    user: { ...USER, current_identity_id: 'i-current' },
  } as never)
  // 切换成功后会刷新可访问资源(getCurrentUser + accessible),
  // 这里给个正常返回,免得走进 catch 的失败分支
  getCurrentUserMock.mockResolvedValue({ user: USER } as never)
  // ⚠️ M132:accessible 的实现**必须写在 beforeEach 里**,不能只写在
  //   vi.mock 工厂里 ——
  //   vitest 配置开了 `restoreMocks: true`,每个用例前会把所有 mock
  //   reset 掉(工厂里那句 `.mockResolvedValue([])` 会被一起抹掉)。
  //   工厂里只留个空壳时:accessible() 返回 undefined ⇒ fetchAccessible
  //   里 `for (const r of apiResources)` 直接抛 ⇒ 走进 catch 的
  //   降级分支 ⇒ **"切换成功"这条路根本没被测到**
  //   (测试里那句 "免得走进 catch 分支" 的注释,其实一直没生效)。
  accessibleMock.mockResolvedValue([])
  useUserStore.setState({
    userInfo: USER,
    currentIdentity: CURRENT,
    isLoggedIn: true,
  })
  // 会话落在 sessionStorage(activeBackend 以 refresh token 的落点为准)
  session.clear()
  session.set(STORAGE_KEYS.TOKEN, 'OLD_ACCESS')
  session.set(STORAGE_KEYS.REFRESH_TOKEN, 'OLD_REFRESH')
})

afterEach(() => {
  session.clear()
})

describe('IdentitySwitcher · 高敏角色', () => {
  it('切超管身份必须先过短信二次验证(不能直接调切换接口)', async () => {
    renderSwitcher()
    await openDropdown()

    // 点下拉里「超级管理员」那个标签
    fireEvent.click(screen.getAllByText('超级管理员')[0]!)

    // ⚠️ 关键断言:切换接口**没有被调用**
    expect(switchIdentityMock).not.toHaveBeenCalled()
    // 短信二次验证的弹窗出现了(标题来自 i18n)
    await waitFor(() => expect(screen.getByText('切换高敏感身份')).toBeInTheDocument())
  })

  it('没绑手机号 → 高敏身份免验直接切(2026-11-03 用户拍板)', async () => {
    // phone 为空 ⇔ 后端 sys_user.phone 为空(/auth/me 上未绑号返 "")
    useUserStore.setState({ userInfo: { ...USER, phone: '' } })

    renderSwitcher()
    await openDropdown()
    fireEvent.click(screen.getAllByText('超级管理员')[0]!)

    // ⚠️ 关键断言:不弹短信 Modal,**直接**调切换接口(不带验证码)
    await screen.findByText(/已切换到/)
    expect(switchIdentityMock.mock.calls[0]).toEqual(['i-super', undefined])
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('非高敏角色直接切,不用短信', async () => {
    renderSwitcher()
    await openDropdown()

    // 「只读查看员」不在高敏名单里 → 直接调切换接口
    fireEvent.click(screen.getAllByText('只读查看员')[0]!)

    await waitFor(() => expect(switchIdentityMock).toHaveBeenCalled())
    expect(switchIdentityMock.mock.calls[0]![0]).toBe('i-normal')

    // ⚠️ M132:**先等切换整个走完,再断言弹窗不存在**。
    //   只等"接口被调了"是不够的 —— 那只证明**请求发出去了**,
    //   异步流程(等响应 → 写 store → 刷权限 → 弹提示)还悬在半空。
    //   短信弹窗完全可能在断言**之后**才冒出来(比如"切完再对高敏身份
    //   补一次确认"这种改法)⇒ 断言白绿。
    //   成功提示是这条链的**最后一环**,它出现 ⇒ 流程已经走完。
    await screen.findByText(/已切换到/)
    // 没有弹短信弹窗
    expect(screen.queryByText('切换高敏感身份')).toBeNull()
    // ⚠️ M132:再查一遍"**有没有弹窗**"本身,而不是只查某句标题 ——
    //   查标题的话,弹窗换个文案(或标题被 i18n 改掉)就查不出来了,
    //   而"弹了窗"这件事还是发生了 ⇒ 断言白绿。
    //   `role="dialog"` 是 rc-dialog 自己在容器上挂的,跟文案无关。
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})

describe('IdentitySwitcher · 角色名显示', () => {
  it('显示的是翻译后的角色名,不是 i18n key', async () => {
    renderSwitcher()
    // 当前身份是房源管理员,头部那个 Tag 应该显示"房源管理员"而不是 role.houseAdmin
    await waitFor(() => expect(screen.getAllByText('房源管理员').length).toBeGreaterThan(0))
    expect(screen.queryByText('role.houseAdmin')).toBeNull()
    expect(screen.queryByText('role.superAdmin')).toBeNull()
  })

  it('下拉里的角色名也是翻译后的文案', async () => {
    renderSwitcher()
    await openDropdown()
    expect(screen.queryByText('role.readonly')).toBeNull()
    expect(screen.getAllByText('只读查看员').length).toBeGreaterThan(0)
  })
})

/**
 * M146b —— 「切身份 → 切完就退出」的前端那一半
 *
 * 这条链上有两个独立缺陷,后端那一个在 M146a(casbin 策略同步跳过 SQLite
 * ⇒ 非超管身份一律 403),前端那一个在这里。
 *
 * 前端那一个是**两处**配合出来的:
 *   ① 切换成功后刷权限失败 → `window.location.reload()`
 *      ⇒ 整个应用重启,SPA 内存态全丢;
 *   ② reload 之后 PermissionLoader 重新拉 `/auth/me` → 同样 403
 *      ⇒ 旧代码把 403 当"登录态失效" → 清 token → 跳登录页。
 *
 * 正确语义:**403 = 会话有效、只是这个身份没权限** ⇒ 保留登录态、保留新 token,
 * 让用户能在右上角再切一次。
 */
describe('IdentitySwitcher · 切换后的状态(M146b)', () => {
  it('切换成功后 userInfo 跟着服务端走(current_identity_id 不停在旧身份)', async () => {
    // 服务端说:切完之后的当前身份是 i-normal
    switchIdentityMock.mockResolvedValue({
      token: 'NEW_ACCESS',
      refresh_token: 'NEW_REFRESH',
      expires_in: 3600,
      refresh_expires_in: 86400,
      user: { ...USER, current_identity_id: 'i-normal' },
    } as never)

    renderSwitcher()
    await openDropdown()
    fireEvent.click(screen.getAllByText('只读查看员')[0]!)

    await waitFor(() => expect(switchIdentityMock).toHaveBeenCalled())
    await screen.findByText(/已切换到/)

    // ⚠️ 这一条以前是错的:store 只 set 了 currentIdentity,
    //   userInfo.current_identity_id 停在切之前的 'i-current' ⇒
    //   个人中心("当前身份")显示的是**切之前的角色名**。
    expect(useUserStore.getState().userInfo?.current_identity_id).toBe('i-normal')
    expect(useUserStore.getState().currentIdentity?.id).toBe('i-normal')
  })

  it('新 token 要落盘(刷新 token 也要 —— 配了单次使用轮换,丢了就续不了期)', async () => {
    switchIdentityMock.mockResolvedValue({
      token: 'NEW_ACCESS',
      refresh_token: 'NEW_REFRESH',
      expires_in: 3600,
      refresh_expires_in: 86400,
      user: { ...USER, current_identity_id: 'i-normal' },
    } as never)

    renderSwitcher()
    await openDropdown()
    fireEvent.click(screen.getAllByText('只读查看员')[0]!)

    await waitFor(() => expect(switchIdentityMock).toHaveBeenCalled())
    await screen.findByText(/已切换到/)

    expect(session.get<string>(STORAGE_KEYS.TOKEN)).toBe('NEW_ACCESS')
    expect(session.get<string>(STORAGE_KEYS.REFRESH_TOKEN)).toBe('NEW_REFRESH')
  })

  it('权限刷新失败(403)→ **不 reload 页面**、保留登录态、给明确提示', async () => {
    // 抓 window.location.reload —— M146b 之前这里就是 reload,
    // 而 reload 之后 PermissionLoader 拿到同样的 403 → 清登录态 → 跳登录页。
    const reload = vi.fn()
    const originalLocation = window.location
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...originalLocation, reload },
    })

    try {
      getCurrentUserMock.mockRejectedValue({ code: 403, message: 'forbidden' })

      renderSwitcher()
      await openDropdown()
      fireEvent.click(screen.getAllByText('只读查看员')[0]!)

      await waitFor(() => expect(switchIdentityMock).toHaveBeenCalled())
      // 提示:切换生效了,但权限清单没拉到
      await screen.findByText(/权限清单加载失败/)

      // ⚠️ 关键断言:**没有 reload**
      expect(reload).not.toHaveBeenCalled()
      // 会话还在(新 token 已落盘),登录态没被清
      expect(session.get<string>(STORAGE_KEYS.TOKEN)).toBe('t')
      expect(useUserStore.getState().isLoggedIn).toBe(true)
    } finally {
      Object.defineProperty(window, 'location', {
        configurable: true,
        value: originalLocation,
      })
    }
  })
})
