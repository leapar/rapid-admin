/**
 * 个人中心 · M113 回归网(2026-10-29)
 *
 * ## 这条网守的是什么
 *
 * 用户 2026-10-29 提了五件事,这里守住其中能在前端测到的四件:
 *
 *  1. **基本信息表单回填的是真值**
 *     以前 `real_name` 读的是后端压根没有的字段 ⇒ 恒 undefined;
 *     `email` 读的是**脱敏后的** "z******@qq.com" ⇒ 用户改个昵称点保存,
 *     后端就把掩码串写回库,**把真邮箱覆盖掉**。
 *
 *  2. **改完资料,身份 / 超管标记不能被洗空**
 *     这是后端契约的镜像断言:后端 M113 起 `PATCH /auth/profile` 返的是
 *     `CurrentUserView`(带 identities / is_platform_super_admin),
 *     前端整份替换 store ⇒ 这些字段必须还在。
 *     (后端要是改回返裸 SysUser,本文件立刻变红。)
 *
 *  3. **改密不要短信验证码**
 *     以前弹窗第一屏是 SmsCodeGuard(发短信 + 校验),通过后才露出密码表单,
 *     而那条短信链路**压根发不出去** ⇒ 个人中心实际改不了密码。
 *     现在弹窗直接就是 旧密码 / 新密码 / 确认新密码。
 *
 *  4. **MFA 在界面上要讲清楚它是什么**
 *     以前只有一个"未开启"标签 + 一个禁用按钮,用户完全不知道那是什么。
 *
 *  5. **只读态也要显示当前值**
 *     表单值以前**只在点「编辑」那一下**才写入,<Form> 上又没有
 *     `initialValues` ⇒ 首帧是空的 ⇒ 不点编辑时三个输入框全是空的
 *     ⇒ 用户看到的是"这个账号没有昵称 / 真实姓名 / 邮箱"。
 *
 *  6. **左侧账号摘要卡片的取值不能"有时候不对"**
 *     用户报"编辑保存以后,左侧 card 会自动刷新,但左侧有时候会显示不对"。
 *     这一组守三处取值层的问题:空串没兜底成 "-"(`??` 不兜空串)、
 *     平台运维账号同时挂「普通用户」标签、身份角色名为空时不兜底。
 *
 * 另有一件在组件层测(头像兜底):`components/common/__tests__/userAvatar.test.tsx`。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `startEditProfile` 里的 `real_name: userInfo?.real_name ?? ''`
 *     改回 `(userInfo as any)?.real_name` → 「真实姓名要回填真值」变红
 *   · 把 `saveProfile` 里的 `setUserInfo(updated)` 改成
 *     `setUserInfo({ ...userInfo, nickname: updated.nickname })` →
 *     「身份 / 超管标记不能被洗空」变红
 *   · 把改密弹窗的 `<Form>` 换回 `{!pwdVerified ? <SmsCodeGuard/> : <Form/>}`
 *     → 「改密弹窗没有短信那一步」变红
 *   · 删掉 MFA 那个 `<Alert>` → 「MFA 要讲清楚」变红
 *   · 删掉那个"把 userInfo 同步进表单"的 effect → 「只读态也要显示当前值」变红
 *   · 把 `userInfo.email || '-'` 改回 `userInfo.email ?? '-'` →
 *     「账号没有邮箱时,那一格显示 "-"" 变红
 *   · 把标签那个条件改回 `!userInfo.is_platform_super_admin` →
 *     「平台运维账号不同时挂「普通用户」标签」变红
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react'
import { App } from 'antd'
import ProfilePage from '@/pages/profile'
import { useUserStore } from '@/stores/user'
import {
  updateProfile,
  changePassword,
  listSessions,
  revokeSessions,
  sendBindPhoneCode,
  bindPhone,
} from '@/api/auth'
import type { Identity, UserInfo } from '@/types/user'

vi.mock('@/api/auth', () => ({
  updateProfile: vi.fn(),
  changePassword: vi.fn(),
  listSessions: vi.fn(),
  revokeSessions: vi.fn(),
  sendBindPhoneCode: vi.fn(),
  bindPhone: vi.fn(),
  logout: vi.fn(),
  sendAccountSms: vi.fn(),
  enableMfa: vi.fn(),
  disableMfa: vi.fn(),
}))

const updateProfileMock = vi.mocked(updateProfile)
const changePasswordMock = vi.mocked(changePassword)
const listSessionsMock = vi.mocked(listSessions)
const revokeSessionsMock = vi.mocked(revokeSessions)
const sendBindPhoneCodeMock = vi.mocked(sendBindPhoneCode)
const bindPhoneMock = vi.mocked(bindPhone)

/**
 * ⚠️ `waitFor` 的默认超时是 **1s**,而 vitest 的 `testTimeout: 60_000` 管不到它
 *
 *   (全量跑 78 个文件时事件循环被抢空,antd 的校验提示渲染可能超过 1s
 *    ⇒ 报的是"假红",不是真 bug —— 仓库里 approval / login / forgot-password
 *    三份页面用例都定了同款常量,这里跟着走。)
 */
const WAIT = { timeout: 10_000 }

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

/**
 * ⚠️ **这里的 userInfo 必须写成"后端 M113 之后的形状"**:
 *   带 `real_name`、邮箱是**真邮箱**(不是掩码串)、带 identities 与超管标记。
 *   如果后端哪天把 CurrentUserView 改回裸 SysUser,本文件的 USER 就会
 *   与线上形状对不上 —— 那正是要变红的时候。
 */
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
  mfa_enabled: false,
}

function renderPage() {
  return render(
    <App>
      <ProfilePage />
    </App>,
  )
}

/** 点「编辑」进入编辑态 */
function enterEdit() {
  fireEvent.click(screen.getByRole('button', { name: /编\s*辑/ }))
}

/** 打开「安全设置」页签 */
async function openSecurityTab() {
  fireEvent.click(screen.getByRole('tab', { name: /安全设置/ }))
  await waitFor(() => expect(screen.getByText('登录密码')).toBeInTheDocument(), WAIT)
}

beforeEach(() => {
  vi.clearAllMocks()
  useUserStore.setState({
    userInfo: USER,
    currentIdentity: IDENTITY,
    isLoggedIn: true,
  })
  listSessionsMock.mockResolvedValue({
    active: [],
    history: { list: [], total: 0, page: 1, size: 20 },
  })
  revokeSessionsMock.mockResolvedValue({ revoked: 0 })
  updateProfileMock.mockResolvedValue(USER)
  // ⚠️ 故意**不 resolve**:成功路径会 `window.location.href = '/login?...'`,
  //   jsdom 里那是一次"未实现的导航",只会刷噪音。本文件要守的是
  //   "提交时发了什么参数",不是"跳转对不对"。
  changePasswordMock.mockReturnValue(new Promise<void>(() => {}) as never)
})

describe('个人中心 · 基本信息可编辑(用户报「为什么不能修改个人基本信息」)', () => {
  it('「编辑」按钮必须是**可点的**', () => {
    // ⚠️ 这条守的是 M113 挖出来的**真凶**:
    //   「编辑 / 保存 / 取消」三个按钮原先写在 `<Form disabled={!editingProfile}>` 里面,
    //   而 antd 5 的 Form 用 DisabledContextProvider 包住子树、Button 又从
    //   DisabledContext 取禁用态 ⇒ 只读态时**「编辑」按钮自己也是灰的**
    //   ⇒ 永远进不了编辑态 ⇒ 基本信息根本改不了。
    renderPage()
    expect(screen.getByRole('button', { name: /编\s*辑/ })).toBeEnabled()
  })

  it('点「编辑」后,真实姓名回填的是**真值**,不是空串', () => {
    renderPage()
    enterEdit()
    const input = screen.getByLabelText('真实姓名') as HTMLInputElement
    expect(input.value).toBe('张三')
  })

  it('邮箱回填的是**真邮箱**,不是 z******@qq.com 那种掩码串', () => {
    renderPage()
    enterEdit()
    const input = screen.getByLabelText('邮箱') as HTMLInputElement
    expect(input.value).toBe('zhangsan@qq.com')
    expect(input.value).not.toContain('*')
  })

  it('保存时把 nickname / real_name / email 一起发出去', async () => {
    renderPage()
    enterEdit()
    fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '新昵称' } })
    fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }))

    await waitFor(() => expect(updateProfileMock).toHaveBeenCalled(), WAIT)
    expect(updateProfileMock.mock.calls[0]![0]).toMatchObject({
      nickname: '新昵称',
      real_name: '张三',
      email: 'zhangsan@qq.com',
    })
  })

  it('保存成功后,身份列表 / 超管标记**不能被洗空**', async () => {
    // 这是后端契约的镜像断言:
    // 后端 M113 起 PATCH /auth/profile 返的是 CurrentUserView。
    // 前端整份替换 store ⇒ 少一个字段 = 界面上少一块。
    updateProfileMock.mockResolvedValue({ ...USER, nickname: '新昵称' })
    renderPage()
    enterEdit()
    fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '新昵称' } })
    fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }))

    await waitFor(() => expect(useUserStore.getState().userInfo?.nickname).toBe('新昵称'), WAIT)
    const after = useUserStore.getState().userInfo
    expect(after?.identities).toHaveLength(1)
    expect(after?.is_platform_super_admin).toBe(true)
    expect(after?.current_identity_id).toBe('i-1')
  })
})

describe('个人中心 · 只读态也要显示当前值(用户报「不点编辑时输入框不显示内容」)', () => {
  it('没点「编辑」时,昵称 / 真实姓名 / 邮箱就已经是当前值', async () => {
    // ⚠️ 这条守的是 M113-b 挖出来的**真凶**:
    //   表单值以前**只在 `startEditProfile`(点「编辑」那一下)才被写入**,
    //   而 `<Form>` 上没有 `initialValues`
    //   ⇒ 首帧表单是空的 ⇒ 只读态三个输入框全是空的
    //   ⇒ 用户看到的是"这个账号没有昵称 / 真实姓名 / 邮箱"。
    renderPage()
    await waitFor(() => {
      expect((screen.getByLabelText('昵称') as HTMLInputElement).value).toBe('张三')
      expect((screen.getByLabelText('真实姓名') as HTMLInputElement).value).toBe('张三')
      expect((screen.getByLabelText('邮箱') as HTMLInputElement).value).toBe('zhangsan@qq.com')
    }, WAIT)
  })

  it('只读态的输入框是**禁用**的(改不动),但**有内容**(不是空的)', async () => {
    renderPage()
    const input = screen.getByLabelText('昵称') as HTMLInputElement
    expect(input).toBeDisabled()
    await waitFor(() => expect(input.value).toBe('张三'), WAIT)
    // 邮箱也不能是掩码串(M113 之前的 desensitize 会把它变成 z******@qq.com)
    const email = screen.getByLabelText('邮箱') as HTMLInputElement
    await waitFor(() => expect(email.value).not.toContain('*'), WAIT)
  })

  it('点「取消」回到只读态后,显示的仍是当前值(不是刚才编辑到一半的内容)', async () => {
    renderPage()
    enterEdit()
    fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '改到一半' } })
    fireEvent.click(screen.getByRole('button', { name: /取\s*消/ }))
    await waitFor(() => {
      expect((screen.getByLabelText('昵称') as HTMLInputElement).value).toBe('张三')
    }, WAIT)
  })

  it('编辑态下 userInfo 被换掉(比如 token 续期),**不能冲掉正在改的内容**', async () => {
    // ⚠️ 这条守的是同步逻辑的另一半:effect 只在**非编辑态**写表单。
    //   要是无脑写,token 续期把 store 换掉时,用户正在改的昵称会被
    //   悄悄改回旧值 —— 用户以为改好了,一保存又存回去。
    renderPage()
    enterEdit()
    fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '新昵称' } })
    // store 换了一个新对象(模拟续期 / 切身份后重新拉 /auth/me)
    act(() => {
      useUserStore.setState({ userInfo: { ...USER, nickname: '服务端改过的昵称' } })
    })
    await waitFor(() => {
      expect((screen.getByLabelText('昵称') as HTMLInputElement).value).toBe('新昵称')
    }, WAIT)
  })
})

describe('个人中心 · 改密不要短信验证码(用户报「不要发送短信验证码」)', () => {
  it('弹窗里直接就是旧密码 / 新密码 / 确认新密码,没有短信那一步', async () => {
    renderPage()
    await openSecurityTab()
    fireEvent.click(screen.getByRole('button', { name: /修改密码/ }))

    await waitFor(() => expect(screen.getByLabelText('旧密码')).toBeInTheDocument(), WAIT)
    expect(screen.getByLabelText('新密码')).toBeInTheDocument()
    expect(screen.getByLabelText('确认新密码')).toBeInTheDocument()

    // 短信那一步的任何痕迹都不该在(只看弹窗内部:
    // 「安全设置」页签上「手机绑定」那张卡片是另一个功能,
    // 全局搜"短信验证"会误伤它)
    const dialog = within(screen.getByRole('dialog'))
    expect(dialog.queryByText(/发送验证码/)).toBeNull()
    expect(dialog.queryByLabelText(/验证码/)).toBeNull()
    expect(dialog.queryByText(/短信验证/)).toBeNull()
  })

  it('提交时只带旧密码 + 新密码(没有短信码)', async () => {
    renderPage()
    await openSecurityTab()
    fireEvent.click(screen.getByRole('button', { name: /修改密码/ }))
    await waitFor(() => expect(screen.getByLabelText('旧密码')).toBeInTheDocument(), WAIT)

    fireEvent.change(screen.getByLabelText('旧密码'), { target: { value: 'Old@123456' } })
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'New@123456' } })
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'New@123456' } })
    fireEvent.click(screen.getByRole('button', { name: /确认修改/ }))

    await waitFor(() => expect(changePasswordMock).toHaveBeenCalled(), WAIT)
    expect(changePasswordMock).toHaveBeenCalledWith('Old@123456', 'New@123456')
    // 只有两个参数 —— 没有第三个短信码
    expect(changePasswordMock.mock.calls[0]).toHaveLength(2)
  })

  it('两次新密码不一致:拦下来,不调接口', async () => {
    renderPage()
    await openSecurityTab()
    fireEvent.click(screen.getByRole('button', { name: /修改密码/ }))
    await waitFor(() => expect(screen.getByLabelText('旧密码')).toBeInTheDocument(), WAIT)

    fireEvent.change(screen.getByLabelText('旧密码'), { target: { value: 'Old@123456' } })
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'New@123456' } })
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'Other@123456' } })
    fireEvent.click(screen.getByRole('button', { name: /确认修改/ }))

    await waitFor(() => expect(screen.getByText('两次输入的密码不一致')).toBeInTheDocument(), WAIT)
    expect(changePasswordMock).not.toHaveBeenCalled()
  })
})

describe('个人中心 · MFA 到底是个什么东西(用户报「二次验证是什么」)', () => {
  it('界面上要讲清楚它是什么、用来干什么、当前什么状态', async () => {
    renderPage()
    await openSecurityTab()

    // 是什么
    expect(
      screen.getByText('二次验证(MFA)= 登录除了密码,还要再验一次身份'),
    ).toBeInTheDocument()
    // 用来干什么(防什么)
    expect(screen.getByText(/即使密码被别人知道了/)).toBeInTheDocument()
    // 当前状态:未开启
    expect(screen.getByText('未开启')).toBeInTheDocument()
    // ⚠️ M150:按钮不再是禁用的 —— 点它弹出「发码(scene=mfa)→ 填码」弹窗
    fireEvent.click(screen.getByRole('button', { name: /开启 MFA/ }))
    await waitFor(() => expect(screen.getByText('开启二次验证')).toBeInTheDocument(), WAIT)
    expect(screen.getByText(/验证通过后二次验证立即生效/)).toBeInTheDocument()
  })
})

describe('个人中心 · 历史登录记录的行区高度跟着视口走(用户报「高度要动态计算,不要写死」)', () => {
  /**
   * 改一下 `window.innerHeight`(jsdom 默认 768)。
   *
   * ⚠️ **必须用 `Object.defineProperty`,不能直接赋值** ——
   *   jsdom 的 `innerHeight` 是**访问器属性**,直接 `window.innerHeight = 1000`
   *   在严格模式下会抛 TypeError / 静默失败(值没变 ⇒ 断言拿到的还是 768
   *   ⇒ 期望值算错 ⇒ 变成假红)。`configurable: true` 才改得动,
   *   也才改得回去。
   */
  function setViewportHeight(h: number): () => void {
    const original = window.innerHeight
    Object.defineProperty(window, 'innerHeight', { value: h, configurable: true, writable: true })
    return () => {
      Object.defineProperty(window, 'innerHeight', {
        value: original,
        configurable: true,
        writable: true,
      })
    }
  }

  /** 切到「登录设备」页签,等表格的行区高度写进 DOM */
  async function rowAreaHeight(): Promise<string> {
    renderPage()
    fireEvent.click(screen.getByRole('tab', { name: /登录设备/ }))
    await waitFor(() => {
      const body = document.querySelector('.ant-table-body') as HTMLElement | null
      expect(body).not.toBeNull()
      expect(body!.style.maxHeight).not.toBe('')
    }, WAIT)
    const body = document.querySelector('.ant-table-body') as HTMLElement
    return body.style.maxHeight
  }

  // ⚠️ **2026-10-31:高度来源从"容器"换成了"视口"** ——
  //   实现从 `measureHistory`(量容器高 − 表头 − 分页)换成了
  //   `scroll={{ y: window.innerHeight - 660 }}`(用户手动改的,commit 706bfb1
  //   「SessionPanel 滚动条」)。**测试要跟着改**,否则它会一直红 ——
  //   而且是"断言着一个已经不存在的实现"的那种红,查起来最费劲。
  //
  //   但用户那句原始诉求(**不要写死**)照样成立,所以守的点从
  //   "跟着容器走"换成"**跟着视口走**"。
  //
  //   断言点选的是 rc-table 的落点:`scroll.y` 被写成 `.ant-table-body` 的
  //   `style.maxHeight`(rc-table/lib/Table.js:284 `maxHeight: scroll.y`)。
  //
  //   ⚠️ **两条用例的期望值差 400px(= 两个视口高度之差)** ——
  //   所以"写死一个常数"(比如 360)会让**两条一起红**;
  //   只测一条的话,常数恰好等于那一条的期望值就糊过去了。
  it('行区高度跟着视口高度走,不是一个写死的常数', async () => {
    const restore = setViewportHeight(1000)
    try {
      expect(await rowAreaHeight()).toBe('340px') // 1000 − 660
    } finally {
      restore()
    }
  })

  it('视口更高,行区也更高(守住"不是常数"这条)', async () => {
    const restore = setViewportHeight(1400)
    try {
      expect(await rowAreaHeight()).toBe('740px') // 1400 − 660
    } finally {
      restore()
    }
  })
})

describe('个人中心 · 手机号只在「安全设置」露一次(用户报「基本信息不要显示手机号」)', () => {
  it('「基本信息」页签里没有手机号字段', () => {
    renderPage()
    // 表单里只有 昵称 / 真实姓名 / 邮箱 三项
    expect(screen.queryByLabelText('手机号')).toBeNull()
    expect(screen.getByLabelText('昵称')).toBeInTheDocument()
    expect(screen.getByLabelText('真实姓名')).toBeInTheDocument()
    expect(screen.getByLabelText('邮箱')).toBeInTheDocument()
    // ⚠️ 整页的"手机号"文字**只剩左边那张账号摘要卡片**有 ——
    //   页签内部(antd 的 role=tabpanel)一个都不该有。
    //   (以前这里有一个只读的 Input + 一句"换绑手机请走安全设置")
    const pane = screen.getByRole('tabpanel')
    expect(within(pane).queryByText('手机号')).toBeNull()
  })

  it('「安全设置」页签里仍然看得到已绑定的手机号', async () => {
    renderPage()
    await openSecurityTab()
    expect(screen.getByText('手机绑定')).toBeInTheDocument()
    // 手机号本身是脱敏的(138****1234),走 SensitiveField 渲染
    // ⚠️ 左边那张账号摘要卡片里也有同一个手机号 ⇒ 必须限定在「手机绑定」卡片里找
    const bindingCard = screen.getByText('手机绑定').closest('.ant-card')
    expect(bindingCard).not.toBeNull()
    expect(within(bindingCard as HTMLElement).getByText('138****1234')).toBeInTheDocument()
  })
})

/**
 * 左侧账号摘要卡片(M113-c)
 *
 * 用户报"基本信息编辑保存以后,会自动刷新左侧 card 里面的信息内容,
 * 但是左侧有时候会显示不对"。这一组守的是**取值层**的三处:
 * 空串没兜底 / 平台运维同时挂「普通用户」/ 身份角色名为空时不兜底。
 *
 * ⚠️ 这三处都是"**有数据的账号看不出来、没数据的账号看得出**"的那种 ——
 *   正好对上用户说的"**有时候**显示不对"。
 */
describe('个人中心 · 左侧账号摘要卡片的取值(用户报「左侧有时候显示不对」)', () => {
  /** 左侧那张卡片(账号 / 手机号 / 邮箱 / 身份数 / 当前身份) */
  function leftCard(): HTMLElement {
    // 「账号」这个 label 只在左卡片的信息表里出现 ⇒ 拿它当锚点
    const card = screen.getByText('账号').closest('.ant-card')
    expect(card).not.toBeNull()
    return card as HTMLElement
  }

  /** 信息表里某一行的值单元格文本 */
  function descValue(label: string): string | undefined {
    const rows = Array.from(leftCard().querySelectorAll('tr'))
    const row = rows.find((r) => r.querySelector('th')?.textContent?.trim() === label)
    return row?.querySelector('td')?.textContent?.trim()
  }

  it('账号没有邮箱时,那一格显示 "-" 而不是空白', () => {
    // ⚠️ 这条守的是 `userInfo.email ?? '-'`:
    //   `??` 只在 null / undefined 时兜底,**空串不兜底**
    //   ⇒ 超级管理员 seed 压根没填邮箱(email = "")⇒ 那格渲染成**空白单元格**,
    //     和旁边有值的行对不齐 —— 这就是"有时候显示不对"的一种。
    useUserStore.setState({ userInfo: { ...USER, email: '' } })
    renderPage()

    expect(descValue('邮箱')).toBe('-')
  })

  it('有邮箱时显示真邮箱(对照组)', () => {
    renderPage()
    expect(descValue('邮箱')).toBe('zhangsan@qq.com')
  })

  it('平台运维账号不同时挂「普通用户」标签', () => {
    // ⚠️ 以前第三个条件只判 `!is_platform_super_admin`
    //   ⇒ 运维账号(ops=true / super=false)会**同时**亮出
    //     「平台运营员」和「普通用户」两个互斥的标签。
    useUserStore.setState({
      userInfo: { ...USER, is_platform_super_admin: false, is_platform_ops: true },
    })
    renderPage()

    const card = leftCard()
    expect(within(card).getByText('平台运营员')).toBeInTheDocument()
    expect(within(card).queryByText('普通用户')).toBeNull()
  })

  it('普通用户账号照旧挂「普通用户」标签(对照组)', () => {
    useUserStore.setState({
      userInfo: { ...USER, is_platform_super_admin: false, is_platform_ops: false },
    })
    renderPage()

    const card = leftCard()
    expect(within(card).getByText('普通用户')).toBeInTheDocument()
    expect(within(card).queryByText('平台运营员')).toBeNull()
  })

  it('当前身份的角色名为空时,那一格兜底成 "-"', () => {
    useUserStore.setState({
      userInfo: {
        ...USER,
        identities: [{ ...IDENTITY, role_name: '' }],
      },
    })
    renderPage()

    expect(descValue('当前身份')).toBe('-')
  })
})

/**
 * 个人中心 · 换绑手机(C5,2026-10-29)
 *
 * ## 这组用例守的是什么
 *
 * 「安全设置 → 手机绑定 → 换绑手机」以前是一个 **disabled 的按钮**,
 * tooltip 写着「Phase 6 真实化」⇒ 这条链路**从来没通过过**。
 *
 * 现在是真流程:填**新**手机号 → 验证码发到**新号** → 填码 → 确认换绑。
 *
 * ## 三条最容易被"看起来对、其实错"的地方
 *
 *  1. **验证码发给的是新号**,不是旧号。
 *     前端这边如果传错了(比如传成 userInfo.phone),后端会把码发到旧号上,
 *     用户在新号上等不到短信 —— 而界面上一切正常,没有任何报错。
 *
 *  2. **换绑成功后 store 要整份替换**。
 *     后端返的是最新的 CurrentUserView;前端要是只改 phone 一个字段,
 *     identities / 超管标记就还留在旧对象里(这次侥幸没事,下次就出事)。
 *
 *  3. **手机号格式不对时前端要先拦一道**。
 *     后端也会拒(validPhone),但那时用户已经点了两次按钮、白等了两秒。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 <Button disabled> 加回去            → 「按钮不再是禁用的」变红
 *   · 把 handleSendBindCode 里的 phone 换成 userInfo.phone
 *                                          → 「发给后端的是**新**手机号」变红
 *   · 把 setUserInfo(updated) 改成 setUserInfo({...userInfo, phone: updated.phone})
 *                                          → 「身份列表没被洗空」变红
 *   · 把内层 Form.Item 的 name="code" 去掉  → 「确认换绑时…」变红
 *                                          (实测:expected [ '13900001111', undefined ]
 *                                           to deeply equal [ '13900001111', '123456' ])
 *     ⇒ 这条网真守住了"**验证码值真的到了后端**",不是摆设。
 *
 *   ⚠️ **反过来的一条(也记在这儿,免得下次有人重写一遍)**:
 *     把 name="code" 从内层**提到外层** Form.Item —— 本组用例**照样全绿**。
 *     原因:Space.Compact 把注入的 onChange 透传给根 div,而 React 的 onChange
 *     是冒泡的合成事件 ⇒ 值照样进 form store。
 *     所以"两层写法"不是"少了就提交不了"的开关,而是让输入框保持**受控**。
 */
describe('个人中心 · 换绑手机(以前是禁用按钮 +「Phase 6 真实化」)', () => {
  /** 打开「安全设置」页签并点开换绑弹窗 */
  async function openBindDialog() {
    renderPage()
    await openSecurityTab()
    fireEvent.click(screen.getByRole('button', { name: /换绑手机/ }))
    await waitFor(() => expect(screen.getByLabelText('新手机号')).toBeInTheDocument(), WAIT)
  }

  it('「换绑手机」按钮不再是禁用的', async () => {
    renderPage()
    await openSecurityTab()
    expect(screen.getByRole('button', { name: /换绑手机/ })).toBeEnabled()
  })

  it('点开之后,弹窗里有「新手机号」和「验证码」两个输入框', async () => {
    await openBindDialog()
    expect(screen.getByLabelText('新手机号')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('请输入 6 位验证码')).toBeInTheDocument()
  })

  it('点「发送验证码」时,把**新手机号**发给后端(不是旧号)', async () => {
    sendBindPhoneCodeMock.mockResolvedValue(undefined)
    await openBindDialog()
    fireEvent.change(screen.getByLabelText('新手机号'), { target: { value: '13900001111' } })
    fireEvent.click(screen.getByRole('button', { name: /发送验证码/ }))

    await waitFor(() => expect(sendBindPhoneCodeMock).toHaveBeenCalled(), WAIT)
    expect(sendBindPhoneCodeMock.mock.calls[0]![0]).toBe('13900001111')
    // ⚠️ 旧号(USER.phone 是脱敏的 138****1234)绝不能被发出去
    expect(sendBindPhoneCodeMock.mock.calls[0]![0]).not.toBe(USER.phone)
  })

  it('手机号格式不对时**不发请求**(前端先拦一道,别让用户白等)', async () => {
    await openBindDialog()
    fireEvent.change(screen.getByLabelText('新手机号'), { target: { value: '123' } })
    fireEvent.click(screen.getByRole('button', { name: /发送验证码/ }))

    await waitFor(
      () => expect(screen.getByText('请输入有效的 11 位手机号')).toBeInTheDocument(),
      WAIT,
    )
    expect(sendBindPhoneCodeMock).not.toHaveBeenCalled()
  })

  it('确认换绑时,把「新号 + 验证码」一起发出去', async () => {
    bindPhoneMock.mockResolvedValue({ ...USER, phone: '139****1111' })
    await openBindDialog()
    fireEvent.change(screen.getByLabelText('新手机号'), { target: { value: '13900001111' } })
    fireEvent.change(screen.getByPlaceholderText('请输入 6 位验证码'), {
      target: { value: '123456' },
    })
    fireEvent.click(screen.getByRole('button', { name: /确认换绑/ }))

    await waitFor(() => expect(bindPhoneMock).toHaveBeenCalled(), WAIT)
    expect(bindPhoneMock.mock.calls[0]).toEqual(['13900001111', '123456'])
  })

  it('换绑成功后,store 里的手机号换成新的,且身份列表**没被洗空**', async () => {
    bindPhoneMock.mockResolvedValue({ ...USER, phone: '139****1111' })
    await openBindDialog()
    fireEvent.change(screen.getByLabelText('新手机号'), { target: { value: '13900001111' } })
    fireEvent.change(screen.getByPlaceholderText('请输入 6 位验证码'), {
      target: { value: '123456' },
    })
    fireEvent.click(screen.getByRole('button', { name: /确认换绑/ }))

    await waitFor(() => expect(useUserStore.getState().userInfo?.phone).toBe('139****1111'), WAIT)
    const after = useUserStore.getState().userInfo
    expect(after?.identities).toHaveLength(1)
    expect(after?.is_platform_super_admin).toBe(true)
  })
})
