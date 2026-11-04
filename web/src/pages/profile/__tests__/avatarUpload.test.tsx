/**
 * 个人中心 · 头像上传(M124,2026-10-30)
 *
 * ## 这条网守的是什么
 *
 * 用户报「个人中心用户头像现在不能设置」。
 *
 * ⚠️ **根因不是"后端不认"** —— `PATCH /auth/profile` 一直收一个 `avatar` 字段
 * (任意 URL),但**前端从来没提供过这个输入框** ⇒ 用户点不到任何地方。
 * 而且那条路本身带一个安全问题:贴外链的头像会渲染在**别人的浏览器**里
 * (管理端用户列表 / 顶栏)⇒ 那等于一个跟踪像素。
 *
 * ⇒ M124 把 `UpdateProfileDTO.Avatar` **删了**,新增 `POST /auth/avatar`
 *   (multipart,字段名 `file`),落盘走文件系统(biz = "avatar",存在**自己 id 底下**),
 *   返回**稳定直链**。
 *
 * ## 交互形态(用户 2026-10-30 明确要求)
 *
 * 「不要另外弄一个按钮,而是应该鼠标移入头像时候有个编辑 icon,
 *   点击可以上传更换头像,这样界面更清爽,或者直接点击头像区域就可以上传头像」
 *
 * ⇒ 头像本身就是触发器:悬停 → 浮出半透明遮罩 + 铅笔图标;点头像 → 打开文件框。
 *   **页面上没有「更换头像」按钮。**
 *
 * ## 四条最容易"看起来对、其实错"的地方
 *
 *  1. **上传成功后必须更新 store** ——
 *     个人中心的头像和**顶栏**的头像读的是同一份 `userInfo.avatar`。
 *     忘了 `setUserInfo` ⇒ 个人中心显示新头像、顶栏还是旧的。
 *
 *  2. **`setUserInfo` 是整份替换语义** —— 所以只能
 *     `setUserInfo({ ...userInfo, avatar: url })`,
 *     不能只塞一个 `{ avatar: url }` 进去(那会把 identities / 超管标记洗空)。
 *
 *  3. **触发器必须真的能打开文件框** ——
 *     antd 的 Upload 把 children 包在自己的 span 里,那个 span 才是带
 *     `onClick`(打开文件框)的那一层 ⇒ 断言要打在**它**身上。
 *
 *  4. **前端的两条拦截只是"友好提示",不是安全边界** ——
 *     真正的闸在服务端(大小 + 扩展名 + **文件头魔数**双重判定)。
 *     前端拦的是"用户明显传错了",目的是**当场给一句人话**。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `<Upload>` 从头像外面拿掉(头像不再是 children)
 *                                        → 「点头像能打开文件框」变红
 *   · 把 `hasControlInside` 改回 `true`(antd 默认)
 *                                        → 「触发器是个 role=button」变红
 *   · 删掉那层遮罩 / 删掉 onMouseEnter  → 「悬停时浮出编辑图标」变红
 *   · 把 `setUserInfo({...userInfo, avatar: view.url})` 改成
 *     `setUserInfo({ avatar: view.url } as UserInfo)`
 *                                        → 「身份列表 / 超管标记没被洗空」变红
 *   · 把 `handleAvatarUpload` 里的 `file.size > AVATAR_MAX_BYTES` 判据删掉
 *                                        → 「超过 5MB 前端先拦一道」变红
 *   · 把 `AVATAR_ACCEPT_TYPES.includes(file.type)` 判据删掉
 *                                        → 「不是图片类型前端先拦一道」变红
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from 'antd'
import ProfilePage from '@/pages/profile'
import { useUserStore } from '@/stores/user'
import { uploadAvatar } from '@/api/auth'
import type { Identity, UserInfo } from '@/types/user'

vi.mock('@/api/auth', () => ({
  updateProfile: vi.fn(),
  changePassword: vi.fn(),
  listSessions: vi.fn(),
  revokeSessions: vi.fn(),
  sendBindPhoneCode: vi.fn(),
  bindPhone: vi.fn(),
  uploadAvatar: vi.fn(),
  logout: vi.fn(),
}))

const uploadAvatarMock = vi.mocked(uploadAvatar)

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

/** 造一个 PNG 文件 */
function pngFile(name = 'me.png'): File {
  return new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], name, { type: 'image/png' })
}

/** 造一个"超过 5 MB"的文件(直接改 size,不真的分配 5 MB 内存) */
function oversizeFile(): File {
  const f = pngFile('huge.png')
  Object.defineProperty(f, 'size', { value: 5 * 1024 * 1024 + 1 })
  return f
}

function renderPage() {
  return render(
    <App>
      <ProfilePage />
    </App>,
  )
}

/** 那个隐藏的 `<input type="file">` */
function fileInput(container: HTMLElement): HTMLInputElement {
  const input = container.querySelector('input[type="file"]') as HTMLInputElement
  expect(input).not.toBeNull()
  return input
}

/** 头像的触发器(包着 UserAvatar 的那个 span) */
function trigger(container: HTMLElement): HTMLElement {
  const el = container.querySelector('.avatar-upload-trigger') as HTMLElement
  expect(el).not.toBeNull()
  return el
}

/** 那层"悬停才浮出来"的遮罩 */
function overlay(container: HTMLElement): HTMLElement {
  const el = container.querySelector('.avatar-upload-overlay') as HTMLElement
  expect(el).not.toBeNull()
  return el
}

/**
 * "选"一个文件
 *
 * ⚠️ **rc-upload 的 `beforeUpload` 是异步调用的**(在 `Promise.all().then()` 里),
 *   所以断言必须 `await waitFor(...)`,不能同步断。
 * ⚠️ **rc-upload 不会按 `accept` 属性过滤普通文件**(它只在 `directory` 模式下过滤),
 *   所以"非图片类型被拦下"这条守的是**我们自己**的 MIME 判据,不是 antd 的。
 */
async function selectFile(file: File, container: HTMLElement) {
  fireEvent.change(fileInput(container), { target: { files: [file] } })
}

beforeEach(() => {
  vi.clearAllMocks()
  useUserStore.setState({
    userInfo: USER,
    currentIdentity: IDENTITY,
    isLoggedIn: true,
  })
})

describe('个人中心 · 头像本身就是上传入口(用户报「头像现在不能设置」)', () => {
  it('页面上**没有**单独的「更换头像」按钮 —— 触发器就是头像', () => {
    // ⚠️ 用户 2026-10-30 明确要求:"不要另外弄一个按钮,这样界面更清爽"。
    //   这条守住"别后来又加回一个按钮"。
    //
    // ⚠️ **断言要打在"原生 <button>"上,不能打 `role="button"`** ——
    //   触发器自己就是个 `role="button"` 的 <span>(antd Upload 包的那一层),
    //   按 role 查会把它自己算进去,这条永远红。
    //   要区分的是:**有没有多出一个原生按钮**。
    const { container } = renderPage()
    // 头像触发器在
    expect(trigger(container)).toBeInTheDocument()
    // 但**没有**一个独立的原生 <button> 叫「更换头像」
    const nativeButtons = Array.from(container.querySelectorAll('button')).filter((b) =>
      /更换头像/.test(b.textContent ?? ''),
    )
    expect(nativeButtons).toHaveLength(0)
  })

  it('触发器是个 **role=button**(读屏 / 键盘都能用)', () => {
    // ⚠️ antd 的 Upload 默认 `hasControlInside = true`
    //   (意思是"children 里已经有控件了,别再加 role=button / tabIndex"),
    //   那是给 `<Upload><Button/></Upload>` 准备的。
    //   我们的 children 是一张**头像**(不是控件)⇒ 必须传 false,
    //   否则触发器对读屏软件和键盘用户**根本不是一个可聚焦的按钮**。
    renderPage()
    expect(screen.getByRole('button', { name: /更换头像/ })).toBeInTheDocument()
  })

  it('鼠标悬停在头像上 → 浮出「编辑」遮罩(默认是藏着的)', async () => {
    const { container } = renderPage()
    expect(overlay(container).style.opacity).toBe('0')

    // ⚠️ 必须用 mouseOver / mouseOut —— React 的 onMouseEnter 是由
    //   **mouseover / mouseout** 合成出来的(EnterLeaveEventPlugin),
    //   直接 fireEvent.mouseEnter 派发的是原生 mouseenter,React **不听**。
    fireEvent.mouseOver(trigger(container))
    await waitFor(() => expect(overlay(container).style.opacity).toBe('1'), WAIT)

    fireEvent.mouseOut(trigger(container))
    await waitFor(() => expect(overlay(container).style.opacity).toBe('0'), WAIT)
  })

  it('**点击头像区域**就能打开文件选择框', async () => {
    // ⚠️ jsdom 没有文件选择框 ⇒ 断言打在**隐藏 input 的 click 被调用**上。
    //   真正带 onClick(打开文件框)的那一层是 antd Upload 自己包的 span,
    //   不是我们写的触发器 ⇒ 事件必须能冒泡到它。
    const { container } = renderPage()
    const input = fileInput(container)
    const clickSpy = vi.spyOn(input, 'click')

    fireEvent.click(trigger(container))

    expect(clickSpy).toHaveBeenCalled()
    clickSpy.mockRestore()
  })

  it('选一张 PNG → 调 uploadAvatar,并把返回的直链写进 store', async () => {
    uploadAvatarMock.mockResolvedValue({
      url: '/rapid-admin/u-1/avatar/9/me.png',
      file_id: '9',
      file_name: 'me.png',
      file_size: 4,
    })
    const { container } = renderPage()

    await selectFile(pngFile(), container)

    await waitFor(() => expect(uploadAvatarMock).toHaveBeenCalled(), WAIT)
    expect(uploadAvatarMock.mock.calls[0]![0]).toBeInstanceOf(File)
    expect(uploadAvatarMock.mock.calls[0]![0].type).toBe('image/png')

    await waitFor(
      () => expect(useUserStore.getState().userInfo?.avatar).toBe('/rapid-admin/u-1/avatar/9/me.png'),
      WAIT,
    )
  })

  it('上传成功后,身份列表 / 超管标记**不能被洗空**', async () => {
    // ⚠️ 这条守的是 `setUserInfo` 的**整份替换语义**:
    //   `setUserInfo` 换掉的是整个 userInfo 对象,所以只能
    //   `{ ...userInfo, avatar: url }` —— 要是有人图省事写成
    //   `setUserInfo({ avatar: url } as UserInfo)`,
    //   identities / is_platform_super_admin 就**整个没了**
    //   (界面上表现为:换完头像,身份标签和「超级管理员」标签一起消失)。
    uploadAvatarMock.mockResolvedValue({
      url: '/rapid-admin/u-1/avatar/9/me.png',
      file_id: '9',
      file_name: 'me.png',
      file_size: 4,
    })
    const { container } = renderPage()

    await selectFile(pngFile(), container)

    await waitFor(() => expect(useUserStore.getState().userInfo?.avatar).toBeTruthy(), WAIT)
    const after = useUserStore.getState().userInfo
    expect(after?.identities).toHaveLength(1)
    expect(after?.is_platform_super_admin).toBe(true)
    expect(after?.current_identity_id).toBe('i-1')
    expect(after?.nickname).toBe('张三')
  })

  it('超过 5 MB:前端先拦一道,**不发请求**', async () => {
    // ⚠️ 后端也会拒(上限同样是 5 MB),但那时用户已经等了一次网络往返。
    //   前端这道拦的是"用户明显传错了",不是安全边界。
    const { container } = renderPage()

    await selectFile(oversizeFile(), container)

    await waitFor(() => expect(screen.getByText('头像不能超过 5 MB')).toBeInTheDocument(), WAIT)
    expect(uploadAvatarMock).not.toHaveBeenCalled()
    expect(useUserStore.getState().userInfo?.avatar).toBe('')
  })

  it('不是图片类型:前端先拦一道,**不发请求**', async () => {
    const pdf = new File([new Uint8Array([0x25, 0x50])], 'doc.pdf', { type: 'application/pdf' })
    const { container } = renderPage()

    await selectFile(pdf, container)

    await waitFor(
      () =>
        expect(
          screen.getByText('头像只支持 PNG / JPEG / GIF / WEBP / BMP 格式'),
        ).toBeInTheDocument(),
      WAIT,
    )
    expect(uploadAvatarMock).not.toHaveBeenCalled()
    expect(useUserStore.getState().userInfo?.avatar).toBe('')
  })

  it('上传失败时,store 里的头像**不能变**(不能乐观更新成空)', async () => {
    uploadAvatarMock.mockRejectedValue(new Error('boom'))
    const { container } = renderPage()

    await selectFile(pngFile(), container)

    await waitFor(() => expect(uploadAvatarMock).toHaveBeenCalled(), WAIT)
    // 错误提示由 http 层统一弹(这里 mock 掉了整个 api 模块,不会弹)
    await waitFor(() => expect(useUserStore.getState().userInfo?.avatar).toBe(''), WAIT)
  })
})
