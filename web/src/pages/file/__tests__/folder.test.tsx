/**
 * 文件夹管理页 —— 搜索框(M82)+ 多级文件夹(M125)
 *
 * ## 这条网守的是什么
 *
 * ### 1. 搜索框(M82)
 *
 *   用户 2026-10-30 报「文件夹管理界面的 searchsection 里没有搜索输入框」。
 *
 *   根因在 `ManagementTable`:
 *
 *   ```ts
 *   const showSearchBar = onSearch || onReset || searchExtra || inlineToolBar
 *   ```
 *
 *   搜索**区**只要上面四项里任意一项就会渲染 —— 但里面的 `Input.Search`
 *   只在传了 `onSearch` 时才渲染。于是"只传 inlineToolBar"的页面
 *   渲染出一个**空搜索区**:看得见搜索区(有底色、有边框),里面没有输入框。
 *
 *   ⚠️ **这层是框架共用的**(17 个页面),所以光在本页补 `onSearch` 就够了 ——
 *     但**后端也得真的支持关键字**,否则搜索框能打字、回车不报错、
 *     列表原样不动(静默失效)。
 *
 * ### 2. 多级文件夹(M125)
 *
 *   用户 2026-10-30 报「要能创建多级文件夹,文件夹点击可以去子文件夹」。
 *
 *   这一页从"平铺列表"变成了"目录浏览器",三件事必须同时成立:
 *
 *   | 事 | 靠什么 | 少了会怎样 |
 *   |---|---|---|
 *   | 表格只列当前这一级 | 请求带 `parent_id` | 进去之后还是列的全部文件夹,像没进去 |
 *   | 点名字能进去 | URL 变 `?folder_id=` | 只能靠 URL 手改,用户进不去子文件夹 |
 *   | 面包屑显示层级 | 后端 `ancestors` | 进去了不知道自己在哪 |
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `onSearch` 从 `<ManagementTable>` 上摘掉 → 「有输入框」那条红;
 *   · 把 `listFolders` 的 `keyword` 去掉 → 「请求带上 keyword」那条红;
 *   · 把 `parentId: q.parentId` 换成 `parentId: undefined` → 「根级只列根级」红;
 *   · 把名字里的 `<a onClick>` 换成 `<span>` → 「点名字能进去」红;
 *   · 把 `setSearchParams` 那行删掉 → 「URL 跟着变」红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor, fireEvent, screen } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import FolderList from '../folder'
import {
  listFolders,
  listFolderUsers,
  createFolder,
  getFolder,
  listFiles,
  deleteFile,
  downloadFile,
} from '@/api/upload'
import { usePermissionStore } from '@/stores/permission'

vi.mock('@/api/upload', () => ({
  listFolders: vi.fn(),
  listFolderUsers: vi.fn(),
  createFolder: vi.fn(),
  deleteFolder: vi.fn(),
  getFolder: vi.fn(),
  // ⚠️ 文件区(2026-11-03)也会 import 这三个 —— mock 工厂替换整个模块,
  //   漏一个就是 "listFiles is not a function"(进了文件夹的用例全红)。
  listFiles: vi.fn(),
  deleteFile: vi.fn(),
  downloadFile: vi.fn(),
}))

const listMock = vi.mocked(listFolders)
const usersMock = vi.mocked(listFolderUsers)
const createMock = vi.mocked(createFolder)
const detailMock = vi.mocked(getFolder)
const filesMock = vi.mocked(listFiles)
const downloadMock = vi.mocked(downloadFile)
const deleteFileMock = vi.mocked(deleteFile)

/** 一个文件夹(字段与后端 service.FolderView 对齐) */
interface FolderRow {
  id: string
  user_id: string
  biz: string
  name: string
  parent_id: string
  cover_key: string
  cover_file_id: string
  file_count: number
  total_size: number
  is_default: boolean
  status: number
  created_at: string
  /** 完整路径(含自身) */
  path: string
  /** 直接子文件夹数 */
  child_count: number
  /** 封面直链;空串 = 没封面 */
  cover_url: string
}

function folderRow(
  id: string,
  name: string,
  extra: Partial<FolderRow> = {},
): FolderRow {
  return {
    id,
    user_id: '3',
    biz: 'default',
    name,
    parent_id: '0',
    cover_key: '',
    cover_file_id: '0',
    file_count: 0,
    total_size: 0,
    is_default: false,
    status: 1,
    created_at: '2026-10-30T08:00:00Z',
    path: name,
    child_count: 0,
    cover_url: '',
    ...extra,
  }
}

/** 把当前 URL 的 query 渲染出来 —— 用来断言"页码/层级进了 URL" */
function LocationProbe() {
  const [sp] = useSearchParams()
  return (
    <div data-testid="location">
      {sp.get('folder_id') ?? ''}|{sp.get('user_id') ?? ''}
    </div>
  )
}

/**
 * 用户层的一行 —— 与后端 service.FolderUserView 对齐
 */
function userRow(userID: string, displayName: string, extra: Record<string, unknown> = {}) {
  return {
    user_id: userID,
    username: displayName,
    nickname: '',
    real_name: '',
    display_name: displayName,
    folder_count: 2,
    file_count: 5,
    total_size: 1024,
    ...extra,
  }
}

/**
 * 用户层接口的响应
 *
 * ⚠️ **cross_user / current_user_id 是这一页的层级判据** ——
 *   它们由后端给(前端不自己猜角色,见 M103 审计)。
 */
function userLayerResult(over: Record<string, unknown> = {}) {
  return {
    list: [],
    total: 0,
    page: 1,
    size: 20,
    cross_user: false,
    current_user_id: '3',
    ...over,
  }
}

function renderPage(initialEntries: string[] = ['/file/folder']) {
  return render(
    <App>
      <MemoryRouter initialEntries={initialEntries}>
        <LocationProbe />
        <FolderList />
      </MemoryRouter>
    </App>,
  )
}

/** 搜索区里的那个输入框(antd Input.Search) */
function searchInput(): HTMLInputElement {
  const el = document.querySelector('.ant-input-search input')
  expect(el, '搜索区里必须有一个输入框 —— 这正是用户报的那条').not.toBeNull()
  return el as HTMLInputElement
}

/** 等表格的数据行渲染出来(不能等"接口被调用过"—— 那证明不了数据回来了) */
async function waitForRows(count: number): Promise<HTMLElement[]> {
  let rows: HTMLElement[] = []
  await waitFor(() => {
    rows = [...document.querySelectorAll<HTMLElement>('.ant-table-tbody tr.ant-table-row')]
    expect(rows).toHaveLength(count)
  })
  return rows
}

describe('文件夹管理页 —— 搜索框(M82)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({
      list: [folderRow('1', '户型图'), folderRow('2', '业主照片')],
      total: 2,
      page: 1,
      size: 20,
    } as never)
    detailMock.mockResolvedValue({
      ...folderRow('1', '户型图'),
      files: [],
      ancestors: [],
      children: [],
    } as never)
    // ⚠️ **文件区的接口也要给个空列表** —— 进了文件夹(?folder_id=)面板会拉
    //   listFiles,没 mock 的返回值会让 useTable 卡在 pending / 抛 unhandled。
    filesMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 20 } as never)
    // ⚠️ **用户层接口也要 mock** —— 这一页靠它的 `cross_user` /
    //   `current_user_id` 判层级。没 mock → 抛错 → 页面停在错误态。
    usersMock.mockResolvedValue(userLayerResult() as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['folder:list', 'folder:create', 'folder:delete', 'file:list']),
      roleCodes: ['role_admin'],
    })
  })

  it('搜索区里真的有输入框(不是空的搜索区)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ 断言的是**输入框存在**,不是"搜索区存在" ——
    //   空搜索区照样有 .ant-search-section 之类的容器,只断言容器会假绿。
    expect(document.querySelector('.ant-input-search')).not.toBeNull()
    expect(searchInput()).toBeInTheDocument()
  })

  it('输入关键字回车 → 请求带上 keyword', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())
    listMock.mockClear()

    const input = searchInput()
    fireEvent.change(input, { target: { value: '户型图' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })

    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ keyword: '户型图' })),
    )
  })

  it('首屏请求不带 keyword(空关键字 = 不限,不是 keyword=空串)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const first = listMock.mock.calls[0]![0]!
    expect(first.keyword).toBeUndefined()
  })

  it('点重置 → 请求里的 keyword 没了,输入框里的字也清了', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const input = searchInput()
    fireEvent.change(input, { target: { value: '户型图' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })
    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ keyword: '户型图' })),
    )

    // 重置是页面上那个「重置」链接
    const resetLink = Array.from(document.querySelectorAll('a')).find((a) =>
      a.textContent?.includes('重置'),
    )
    expect(resetLink, '搜索区里必须有重置入口').toBeTruthy()
    fireEvent.click(resetLink as HTMLAnchorElement)

    await waitFor(() => {
      const last = listMock.mock.calls[listMock.mock.calls.length - 1]![0]!
      expect(last.keyword).toBeUndefined()
    })
    // ⚠️ 输入框里自己那份字也要清 —— 否则用户看着框里的词以为还在过滤
    await waitFor(() => expect(searchInput().value).toBe(''))
  })
})

describe('文件夹管理页 —— 多级文件夹(M125)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({
      list: [folderRow('1', '户型图'), folderRow('2', '业主照片')],
      total: 2,
      page: 1,
      size: 20,
    } as never)
    detailMock.mockResolvedValue({
      ...folderRow('1', '户型图'),
      files: [],
      ancestors: [],
      children: [],
    } as never)
    // ⚠️ **文件区的接口也要给个空列表** —— 进了文件夹(?folder_id=)面板会拉
    //   listFiles,没 mock 的返回值会让 useTable 卡在 pending / 抛 unhandled。
    filesMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 20 } as never)
    createMock.mockResolvedValue({ id: '9' } as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['folder:list', 'folder:create', 'folder:delete', 'file:list']),
      roleCodes: ['role_admin'],
    })
  })

  it('根级视图只列根级(parent_id=0),不是全部层级', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const first = listMock.mock.calls[0]![0]!
    expect(first.parentId).toBe('0')
  })

  it('点文件夹名字 → URL 跟着变,列表只列它的直接子级', async () => {
    renderPage()
    await waitForRows(2)

    // 名字是链接 —— 点它就是"进入这个文件夹"
    fireEvent.click(screen.getByText('户型图'))

    // URL 变成 ?folder_id=1
    await waitFor(() =>
      expect(screen.getByTestId('location').textContent).toBe('1|'),
    )

    // 列表跟着换成"只列 1 的直接子级"
    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ parentId: '1' })),
    )
  })

  it('面包屑显示层级(根目录 / 祖先 / 当前)', async () => {
    detailMock.mockResolvedValue({
      ...folderRow('2', '图纸'),
      files: [],
      ancestors: [folderRow('1', '素材')],
      children: [],
    } as never)

    renderPage(['/file/folder?folder_id=2'])
    await waitFor(() => expect(detailMock).toHaveBeenCalledWith('2'))

    await waitFor(() => {
      const crumbs = document.querySelector('.ant-breadcrumb')
      expect(crumbs?.textContent).toContain('根目录')
      expect(crumbs?.textContent).toContain('素材')
      expect(crumbs?.textContent).toContain('图纸')
    })
  })

  it('深链进来时首屏就列那个文件夹的直接子级(不先列一遍根级)', async () => {
    renderPage(['/file/folder?folder_id=2'])
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const first = listMock.mock.calls[0]![0]!
    expect(first.parentId).toBe('2')
  })

  it('「新建子文件夹」带上 parent_id(建在那一行下面)', async () => {
    renderPage()
    await waitForRows(2)

    // 行操作:新建子文件夹
    const btn = screen.getAllByRole('button', { name: /新\s*建\s*子\s*文\s*件\s*夹/ })[0]!
    fireEvent.click(btn)

    // 弹窗里的名字输入框(antd Form.Item 的 name 会落到 input 的 id 上)
    const modal = await waitFor(() => {
      const el = document.querySelector('.ant-modal')
      expect(el).not.toBeNull()
      return el as HTMLElement
    })
    const input = modal.querySelector('#name') as HTMLInputElement
    expect(input, '新建弹窗里必须有名字输入框').not.toBeNull()
    fireEvent.change(input, { target: { value: '子目录' } })

    const okBtn = Array.from(modal.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('保'),
    )
    expect(okBtn, '弹窗必须有确定按钮').toBeTruthy()
    fireEvent.click(okBtn as HTMLButtonElement)

    await waitFor(() =>
      expect(createMock).toHaveBeenCalledWith(
        expect.objectContaining({ name: '子目录', parentId: '1' }),
      ),
    )
  })

  it('建子文件夹时不显示业务段输入框(后端会继承上级的)', async () => {
    renderPage()
    await waitForRows(2)

    fireEvent.click(screen.getAllByRole('button', { name: /新\s*建\s*子\s*文\s*件\s*夹/ })[0]!)

    const modal = await waitFor(() => {
      const el = document.querySelector('.ant-modal')
      expect(el).not.toBeNull()
      return el as HTMLElement
    })
    expect(modal.querySelector('#biz'), '建子文件夹时不该出现业务段输入框').toBeNull()
  })

  it('封面列显示封面缩略图(用户问的"封面在哪显示")', async () => {
    listMock.mockResolvedValue({
      list: [
        folderRow('1', '户型图', {
          cover_key: 'res/3/default/1/cover.png',
          cover_file_id: '5',
          cover_url: '/rapid-admin/3/default/1/cover.png',
        }),
      ],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)

    const img = document.querySelector('img.ant-image-img') as HTMLImageElement | null
    expect(img, '封面列必须渲染出封面图').not.toBeNull()
    expect(img?.getAttribute('src')).toBe('/rapid-admin/3/default/1/cover.png')
  })

  it('没有封面时显示占位,不是一张裂图', async () => {
    renderPage()
    await waitForRows(2)

    expect(document.querySelector('img.ant-image-img'), '没封面就不该有 img').toBeNull()
    expect(document.body.textContent).toContain('无封面')
  })

  it('有关键字时不限层级(根级也要能搜到子文件夹)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())
    listMock.mockClear()

    const input = searchInput()
    fireEvent.change(input, { target: { value: '图纸' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })

    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(
        expect.objectContaining({ keyword: '图纸', parentId: undefined }),
      ),
    )
  })
})

/**
 * M139 —— 用户层
 *
 * ## 这一层守的是什么
 *
 * 存储侧的目录形态是 `rapid-admin/{uid}/{biz}/{fid}/` —— **第一段就是用户**。
 * 用户 2026-10-31 报「文件夹管理列表的外层显示不对」,查下来是**两处**叠在一起:
 *
 * 1. 后端 `FolderService.List` 写死 `user_id = 调用方` ⇒ 超管**根本列不出
 *    别人的文件夹**(与设计文档 §10.2 矛盾);
 * 2. 界面上没有任何地方说明"这一行属于谁"。
 *
 * 于是这一页必须分层:**用户层 → 该用户的文件夹 → 子文件夹**。
 *
 * | 事 | 靠什么 | 少了会怎样 |
 * |---|---|---|
 * | 最外层是"用户" | `GET /folders/users` 的 `cross_user` | 超管看到的还是只有自己的文件夹 |
 * | 进谁的空间 | URL `?user_id=` + 请求带 `userId` | 点用户进不去,还是自己的 |
 * | 别人的空间只读 | `cross_user` + `current_user_id` | 超管能删别人的文件夹(设计文档 §10.2 禁止) |
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 * · 把 `showUserLayer` 改成 `!userId` → 「超管看到用户层」红;
 * · 把 `canWrite` 改成 `true` → 「别人的空间只读」红;
 * · 把请求里的 `userId: q.userId` 去掉 → 「进空间带上 user_id」红。
 */
describe('文件夹管理页 —— 用户层(M139)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({
      list: [folderRow('1', '户型图'), folderRow('2', '业主照片')],
      total: 2,
      page: 1,
      size: 20,
      owner_name: '张三',
    } as never)
    detailMock.mockResolvedValue({
      ...folderRow('1', '户型图'),
      files: [],
      ancestors: [],
      children: [],
    } as never)
    // ⚠️ **文件区的接口也要给个空列表** —— 进了文件夹(?folder_id=)面板会拉
    //   listFiles,没 mock 的返回值会让 useTable 卡在 pending / 抛 unhandled。
    filesMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 20 } as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['folder:list', 'folder:create', 'folder:delete', 'file:list']),
      roleCodes: ['role_admin'],
    })
  })

  it('普通用户:直接进自己的文件夹,不显示用户层', async () => {
    // ⚠️ **用户层接口照样有数据** —— 判据是 `cross_user`,不是"有没有数据"。
    //   只按数据判的话,普通用户会看到一层"只有我自己"的噪音。
    usersMock.mockResolvedValue(
      userLayerResult({ list: [userRow('3', '张三')], total: 1 }) as never,
    )

    renderPage()
    await waitForRows(2)

    expect(document.body.textContent).not.toContain('张三')
    expect(document.body.textContent).toContain('户型图')
  })

  it('普通用户:请求不带 user_id(默认口径就是"我的")', async () => {
    usersMock.mockResolvedValue(userLayerResult() as never)
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const first = listMock.mock.calls[0]![0]!
    expect(first.userId).toBeUndefined()
  })

  it('超管:最外层是用户层(先选用户,再看文件夹)', async () => {
    usersMock.mockResolvedValue(
      userLayerResult({
        list: [userRow('7', '张伟'), userRow('8', '李娜')],
        total: 2,
        cross_user: true,
        current_user_id: '3',
      }) as never,
    )

    renderPage()
    await waitFor(() => expect(usersMock).toHaveBeenCalled())

    await waitFor(() => expect(document.body.textContent).toContain('张伟'))
    expect(document.body.textContent).toContain('李娜')
    // ⚠️ **文件夹层此刻不能出现** —— 层级判定没到就渲染文件夹层的话,
    //   超管会先闪一下自己的文件夹列表再跳到用户层。
    expect(document.body.textContent).not.toContain('户型图')
  })

  it('超管:点用户名 → URL 带上 user_id,请求也带上 userId', async () => {
    usersMock.mockResolvedValue(
      userLayerResult({
        list: [userRow('7', '张伟')],
        total: 1,
        cross_user: true,
        current_user_id: '3',
      }) as never,
    )

    renderPage()
    await waitFor(() => expect(document.body.textContent).toContain('张伟'))

    fireEvent.click(screen.getByText('张伟'))

    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('|7'))
    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ userId: '7' })),
    )
  })

  it('超管:别人的空间是只读的(没有「新建」,也没有「删除」)', async () => {
    usersMock.mockResolvedValue(
      userLayerResult({ cross_user: true, current_user_id: '3' }) as never,
    )

    renderPage(['/file/folder?user_id=7'])
    await waitForRows(2)

    expect(
      screen.queryAllByRole('button', { name: /新\s*建\s*文\s*件\s*夹/ }),
      '别人的空间里不该有「新建文件夹」',
    ).toHaveLength(0)
    expect(
      screen.queryAllByRole('button', { name: /删\s*除/ }),
      '别人的空间里不该有「删除」',
    ).toHaveLength(0)
    // 说清楚是只读,免得用户以为按钮坏了
    expect(document.body.textContent).toContain('只读')
  })

  it('超管:自己的空间照常能「新建」「删除」', async () => {
    usersMock.mockResolvedValue(
      userLayerResult({ cross_user: true, current_user_id: '3' }) as never,
    )

    renderPage(['/file/folder?user_id=3'])
    await waitForRows(2)

    expect(
      screen.queryAllByRole('button', { name: /新\s*建\s*文\s*件\s*夹/ }),
      '自己的空间里必须有「新建文件夹」',
    ).not.toHaveLength(0)
    expect(
      screen.queryAllByRole('button', { name: /删\s*除/ }),
      '自己的空间里必须有「删除」',
    ).not.toHaveLength(0)
  })

  it('超管:面包屑第一级是「用户」,能点回用户层', async () => {
    usersMock.mockResolvedValue(
      userLayerResult({ cross_user: true, current_user_id: '3' }) as never,
    )

    renderPage(['/file/folder?user_id=7'])
    await waitForRows(2)

    const crumbs = document.querySelector('.ant-breadcrumb')
    expect(crumbs?.textContent).toContain('用户')

    fireEvent.click(screen.getByText('用户'))
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('|'))
  })
})

/**
 * 2026-11-01 —— 「点整行进入」
 *
 * 用户提的:「文件夹管理里面的列表要支持点击进入,这样交互会更好」。
 *
 * ⚠️ **名字那一列本来就是链接**(用户 2026-10-30 提的),这一轮做的是
 *   **整行都能点** —— 一屏几十行,只点名字那几个字太费劲。
 *
 * ## 这条网守的是什么
 *
 * | 事 | 靠什么 | 少了会怎样 |
 * |---|---|---|
 * | 点行 → 进入那个文件夹 | `onRow` + `enterOnRow` | 用户还得瞄准名字那几个字 |
 * | **点行内的按钮不进入** | `isInteractiveTarget` | **点「删除」顺带把文件夹打开** —— 确认弹窗一闪就没,层级还多了一层,用户以为按钮坏了 |
 * | 点标签 / 名字链接不重复触发 | 同上 | 一次点击做两件事 |
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 * · 把 `tableProps` 里的 `onRow` 去掉 → 「点行能进去」红;
 * · 把 `enterOnRow` 里的 `if (isInteractiveTarget(e.target)) return` 删掉
 *   → 「点按钮不进入」红;
 * · 把 `isInteractiveTarget` 改成永远返回 false → 同上。
 */
describe('文件夹管理页 —— 点整行进入(2026-11-01)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({
      list: [
        // ⚠️ 带 `path: 'root/户型图'` ⇒ 名字那列会多一个「所在目录」标签,
        //   正好用来验「点标签不进入」
        folderRow('1', '户型图', { path: 'root/户型图', child_count: 3 }),
        folderRow('2', '业主照片'),
      ],
      total: 2,
      page: 1,
      size: 20,
    } as never)
    detailMock.mockResolvedValue({
      ...folderRow('1', '户型图'),
      files: [],
      ancestors: [],
      children: [],
    } as never)
    // ⚠️ **文件区的接口也要给个空列表** —— 进了文件夹(?folder_id=)面板会拉
    //   listFiles,没 mock 的返回值会让 useTable 卡在 pending / 抛 unhandled。
    filesMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 20 } as never)
    usersMock.mockResolvedValue(userLayerResult() as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['folder:list', 'folder:create', 'folder:delete', 'file:list']),
      roleCodes: ['role_admin'],
    })
  })

  it('点行的空白处 → 进入这个文件夹(URL 跟着变)', async () => {
    renderPage()
    const rows = await waitForRows(2)

    // ⚠️ **行上带着可点的标记** —— antd 5 没有默认的行悬停底色,
    //   不给这个 class 的话用户根本看不出哪一行能点。
    expect(rows[0]!.className).toContain('row-clickable')

    // 点第一行(不是名字那一列 —— 名字是链接,那是另一条路)
    fireEvent.click(rows[0]!)

    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('1|'))
    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ parentId: '1' })),
    )
  })

  it('点行内的「删除」→ 只弹删除确认,**不**进入那个文件夹', async () => {
    renderPage()
    await waitForRows(2)

    const delBtn = screen.getAllByRole('button', { name: /删\s*除/ })[0]!
    fireEvent.click(delBtn)

    // 删除确认弹窗开了
    await waitFor(() => expect(document.querySelector('.ant-modal')).not.toBeNull())
    // ⚠️ **但没有进入那个文件夹** —— 这一轮最容易踩的坑:
    //   按钮的点击会冒泡到行上,不挡的话就是"点删除顺带打开文件夹"。
    expect(screen.getByTestId('location').textContent).toBe('|')
    expect(listMock).not.toHaveBeenCalledWith(expect.objectContaining({ parentId: '1' }))
  })

  it('点行内的标签(所在目录 / 子文件夹数)→ 不进入', async () => {
    renderPage()
    const rows = await waitForRows(2)

    const tag = rows[0]!.querySelector<HTMLElement>('.ant-tag')
    expect(tag, '这一行必须有标签(所在目录 / 子文件夹数)').toBeTruthy()
    fireEvent.click(tag!)

    expect(screen.getByTestId('location').textContent).toBe('|')
    expect(listMock).not.toHaveBeenCalledWith(expect.objectContaining({ parentId: '1' }))
  })

  it('用户层:点行 → 进入那个用户的空间', async () => {
    usersMock.mockResolvedValue(
      userLayerResult({
        list: [userRow('7', '张伟')],
        total: 1,
        cross_user: true,
        current_user_id: '3',
      }) as never,
    )

    renderPage()
    await waitFor(() => expect(usersMock).toHaveBeenCalled())
    const rows = await waitForRows(1)

    fireEvent.click(rows[0]!)

    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('|7'))
    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ userId: '7' })),
    )
  })
})

/**
 * 混合列表(2026-11-03 用户验收反馈)
 *
 * ## 这条网守的是什么
 *
 * 用户验收 M179 后要求:「上面文件夹表 + 下面文件区」两个列表合成**一个**,
 * 像 Windows 资源管理器:同一个列表里先子文件夹后文件。
 *
 * | 事 | 靠什么 | 少了会怎样 |
 * |---|---|---|
 * | 文件行出现在同一张表里 | `mixedRows` 合并 + `kind` 判别 | 两个列表又叠回来了 |
 * | 先文件夹后文件 | 合并顺序 folders → files | 资源管理器惯例被破坏 |
 * | 行操作按 kind 出 | `actionsFor` / `fileActionsFor` | 文件行出现「新建子文件夹」这种张冠李戴的动作 |
 * | 点文件行 = 详情 | `onRow` 按 kind 分支 | 文件行点了"进入"一个不存在的文件夹 |
 * | rowKey 按 kind 拼前缀 | `folder-` / `file-` | 两张表的主键撞了,React 行错位 |
 * | 删文件刷两处 | `reloadFiles` + `foldersTable.reload` | 文件夹卡片上的 file_count 还是旧数 |
 * | 分页只翻文件夹、文件不重复 | 文件只在第 1 页并入,搜索时不并 | 翻页时同一批文件反复出现,像列表坏了 |
 * | 文件总数常驻 | `mixedShowTotal`("文件夹 X 个 · 文件 Y 个") | 翻到第 2 页的用户以为文件夹里没文件 |
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 * · 把 `mixedRows` 换回 `foldersTable.tableProps.dataSource` → 「文件行出现在
 *   同一张表里」红;
 * · 把合并顺序反过来 → 「先文件夹后文件」红;
 * · 把操作列的 kind 分支删掉 → 「行操作按 kind 出」红;
 * · 把 `onRow` 的 `else setViewingFile(row.file)` 改成 goToFolder → 「点文件行 = 详情」红;
 * · 把 rowKey 前缀去掉 → 「rowKey 按 kind 拼前缀」红(rowKey 仍是字符串,需配合
 *   重复 id 的夹具才能真红 —— 本组断言走 DOM 行定位,间接覆盖);
 * · 把删除 onOk 里的 `reloadFiles()` 删掉 → 「删文件刷两处」红;
 * · 把 `showFiles` 的 `foldersPage === 1` 去掉 → 「翻页后文件不重复」红。
 */

/** 一个文件(字段与后端 service.FileView 对齐) */
function fileRow(id: string, name: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    folder_id: '5',
    user_id: '3',
    biz: 'default',
    file_key: `res/3/default/5/${name}`,
    file_name: name,
    file_size: 2048,
    content_type: 'image/png',
    etag: 'abc',
    storage: 1,
    is_cover: false,
    status: 1,
    created_at: '2026-11-03T08:00:00Z',
    duration_ms: 0,
    ...extra,
  }
}

describe('文件夹管理页 —— 混合列表(2026-11-03)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({
      list: [folderRow('1', '户型图'), folderRow('2', '业主照片')],
      total: 2,
      page: 1,
      size: 20,
    } as never)
    detailMock.mockResolvedValue({
      ...folderRow('5', '图纸'),
      files: [],
      ancestors: [],
      children: [],
    } as never)
    filesMock.mockResolvedValue({
      list: [fileRow('11', '总平面.png'), fileRow('12', '立面.dwg', { content_type: 'application/octet-stream' })],
      total: 2,
      page: 1,
      size: 100,
    } as never)
    usersMock.mockResolvedValue(userLayerResult() as never)
    usePermissionStore.setState({
      permissionCodes: new Set([
        'folder:list', 'folder:create', 'folder:delete', 'file:list', 'file:read', 'file:delete',
      ]),
      roleCodes: ['role_admin'],
    })
  })

  it('进文件夹后是**一个**列表:先文件夹行后文件行(资源管理器顺序)', async () => {
    renderPage(['/file/folder?folder_id=5'])

    const rows = await waitForRows(4)
    // 前 2 行是文件夹,后 2 行是文件 —— 顺序就是资源管理器的惯例
    expect(rows[0]!.textContent).toContain('户型图')
    expect(rows[1]!.textContent).toContain('业主照片')
    expect(rows[2]!.textContent).toContain('总平面.png')
    expect(rows[3]!.textContent).toContain('立面.dwg')
  })

  it('根级(没进任何文件夹)不拉文件、不出现文件行', async () => {
    renderPage()
    await waitForRows(2)

    expect(filesMock).not.toHaveBeenCalled()
    expect(document.body.textContent).not.toContain('总平面.png')
  })

  it('行操作按 kind 区分:文件夹行有「新建子文件夹」,文件行有「下载」且没有', async () => {
    renderPage(['/file/folder?folder_id=5'])
    const rows = await waitForRows(4)

    // 文件夹行的动作(前 2 行)
    expect(rows[0]!.textContent).toContain('新建子文件夹')
    // 文件行的动作(后 2 行):有下载,没有"新建子文件夹"
    expect(rows[2]!.textContent).toContain('下载')
    expect(rows[2]!.textContent).not.toContain('新建子文件夹')

    // 下载走 downloadFile(id, 文件名) —— 与文件列表页同一个函数
    const dlBtn = Array.from(rows[2]!.querySelectorAll('button')).find((b) =>
      (b.textContent ?? '').replace(/\s/g, '').includes('下载'),
    )
    expect(dlBtn, '文件行必须有「下载」按钮').toBeTruthy()
    fireEvent.click(dlBtn!)
    await waitFor(() => expect(downloadMock).toHaveBeenCalledWith('11', '总平面.png'))
  })

  it('点文件行空白处 → 详情抽屉(不是"进入文件夹")', async () => {
    renderPage(['/file/folder?folder_id=5'])
    const rows = await waitForRows(4)

    fireEvent.click(rows[2]!)

    await waitFor(() => expect(document.querySelector('.ant-drawer-open')).not.toBeNull())
    expect(document.body.textContent).toContain('总平面.png')
    expect(document.body.textContent).toContain('res/3/default/5/总平面.png')
  })

  it('删文件:确认 → deleteFile + **刷两处**(本层文件 + 文件夹列表)', async () => {
    renderPage(['/file/folder?folder_id=5'])
    const rows = await waitForRows(4)
    listMock.mockClear()
    filesMock.mockClear()

    // 文件行(第 3 行)里的「删除」—— 前两行是文件夹的删除,不能点错
    const delBtn = Array.from(rows[2]!.querySelectorAll('button')).find((b) =>
      (b.textContent ?? '').replace(/\s/g, '').includes('删除'),
    )
    expect(delBtn, '文件行必须有「删除」按钮').toBeTruthy()
    fireEvent.click(delBtn!)

    const modal = await waitFor(() => {
      const el = document.querySelector('.ant-modal')
      expect(el).not.toBeNull()
      return el as HTMLElement
    })
    const okBtn = modal.querySelector('.ant-btn-primary') as HTMLButtonElement
    expect(okBtn, '删除确认弹窗必须有确定按钮').not.toBeNull()
    fireEvent.click(okBtn)

    await waitFor(() => expect(deleteFileMock).toHaveBeenCalledWith('11'))
    // ⚠️ 文件计数在文件夹接口上 —— 不刷的话卡片上的 file_count 还是旧数
    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ parentId: '5' })),
    )
    await waitFor(() => expect(filesMock).toHaveBeenCalled())
  })

  it('翻到第 2 页:文件行**不**重复出现,统计条常驻文件总数', async () => {
    listMock.mockResolvedValue({
      list: [folderRow('21', 'B1'), folderRow('22', 'B2')],
      total: 45,
      page: 2,
      size: 20,
    } as never)

    renderPage(['/file/folder?folder_id=5'])
    await waitForRows(4)

    const page2 = document.querySelector('.ant-pagination-item[title="2"]') as HTMLElement
    expect(page2, '这一组必须有分页器(total=45)').toBeTruthy()
    fireEvent.click(page2)

    // 第 2 页只有文件夹行 —— 文件不跟着每一页重复
    const rows = await waitForRows(2)
    expect(rows[0]!.textContent).toContain('B1')
    expect(document.body.textContent).not.toContain('总平面.png')
    // 但文件总数常驻在统计条里,翻页的用户照样知道文件夹里有文件
    await waitFor(() => expect(document.body.textContent).toContain('文件 2 个'))
  })

  it('关键字搜索(跨层级)时文件行不并入 —— 口径不能搅在一起', async () => {
    renderPage(['/file/folder?folder_id=5'])
    await waitForRows(4)

    const input = searchInput()
    fireEvent.change(input, { target: { value: '图纸' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })

    // 搜索列的是全部层级的文件夹,当前文件夹的文件不再插进来
    await waitForRows(2)
    expect(document.body.textContent).not.toContain('总平面.png')
  })
})
