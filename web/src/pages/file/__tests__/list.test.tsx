/**
 * 文件列表页 —— 「存储」列 + 「复制直链」
 *
 * ## 这条网守的是什么
 *
 * ### 1.「存储」列
 *
 *   `sys_file.storage` 是**逐行**记的(迁移期本地磁盘与 OSS 两种 provider 共存,
 *   先复制再切,所以一行一个值)。后端枚举是:
 *
 *   ```go
 *   FileStorageLocal FileStorage = 1 // 本地磁盘
 *   FileStorageOSS   FileStorage = 2 // 阿里云 OSS
 *   ```
 *
 *   页面以前写的是 `v === 1 ? 'OSS' : '本地'` —— **两个值搞反了**:
 *   本地磁盘上的文件被显示成 "OSS"(用户 2026-10-30 报的:
 *   文件明明躺在 server/data/files/... 下,列表却写着 OSS)。
 *
 *   而真躺在 OSS 上的文件反过来显示成"本地" —— 更坏,因为那一栏是排查
 *   "文件到底在不在 OSS"时唯一的线索,它说反了,排查就被带偏。
 *
 * ### 2.「复制直链」(2026-10-30 单桶单树模型)
 *
 *   存储侧改成"一个桶、一棵树"之后,**每个文件都有一条直链**,私密靠 key
 *   不可猜。所以列表页给了「复制直链」,让人把地址直接发出去。
 *
 *   ⚠️ **local provider 返的是同源相对路径**(`/rapid-admin/{uid}/{biz}/{fid}/{name}`)。
 *   直接复制出去,别人拿到的是个半截路径,拼不出能打开的地址 —— 必须补成
 *   绝对地址。而 OSS 返的是**已经是绝对地址**的 URL,再拼 origin 就成了
 *   `http://localhost:3000https://...`(双协议头,链接彻底废掉)。
 *
 * ## ⚠️ **等的是"渲染出来",不是"请求发出去了"**(2026-10-31)
 *
 *   下面 3 条原本都长这样:
 *
 *   ```ts
 *   await waitFor(() => expect(listMock).toHaveBeenCalled())   // ← 等的是"请求发出去了"
 *   const btn = screen.getAllByRole('button', { name: /复\s*制\s*直\s*链/ })[0]!
 *   ```
 *
 *   `listMock` 是 `vi.fn()` —— `toHaveBeenCalled()` 在**调用发生的那一刻**
 *   就为真;而「复制直链」按钮是**跟着列表行一起渲染出来的**,中间隔着
 *   `await listMock()` → setState → React 重新渲染。
 *
 *   ⇒ **满载时**(全量跑 104 个文件)这个间隔会被拉长到超过同步断言的时机 ⇒
 *     `getAllByRole` 抛 "Unable to find an accessible element with the role button"。
 *     单独跑这个文件是绿的 —— 这就是 commit `73ad299` 记过的"满载假红"。
 *
 *   ⇒ 正确写法是**等按钮渲染出来**。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `isOSSStorage(v)` 换成 `v === 1` → 「存储」两条都红;
 *   · 把常量 `FileStorage.OSS` 改成 1 → "OSS 那一行"红;
 *   · 去掉 `url.startsWith('/')` 那个判断 → 「相对路径补 origin」红,
 *     「OSS 绝对地址原样复制」也红;
 *   · 把 `getFileUrl` 的返回值换成 `''` → 复制到的是 origin 本身,红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor, screen, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import FileList from '../list'
import { listFiles, listFolders, getFileQuota, getFileUrl, getFolder, deleteFile } from '@/api/upload'
import { usePermissionStore } from '@/stores/permission'

vi.mock('@/api/upload', () => ({
  listFiles: vi.fn(),
  listFolders: vi.fn(),
  deleteFile: vi.fn(),
  setFolderCover: vi.fn(),
  uploadFileAuto: vi.fn(),
  getFileQuota: vi.fn(),
  downloadFile: vi.fn(),
  getFileUrl: vi.fn(),
  getFolder: vi.fn(),
}))

const listMock = vi.mocked(listFiles)
const foldersMock = vi.mocked(listFolders)
const quotaMock = vi.mocked(getFileQuota)
const urlMock = vi.mocked(getFileUrl)
const folderMock = vi.mocked(getFolder)
const deleteMock = vi.mocked(deleteFile)

/** 一行文件(字段与后端 service.FileView 对齐) */
function fileRow(storage: number, name: string, contentType = 'image/jpeg') {
  return {
    id: '1',
    folder_id: '2',
    user_id: '3',
    biz: 'default',
    file_key: `res/3/default/2/${name}`,
    file_name: name,
    file_size: 100,
    content_type: contentType,
    etag: 'e',
    storage,
    is_cover: false,
    status: 1,
    created_at: '2026-10-30T08:00:00Z',
  }
}

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <FileList />
      </MemoryRouter>
    </App>,
  )
}

/** 带初始地址渲染(给 URL 里带 folder_id 的那条路用) */
function renderPageAt(path: string) {
  return render(
    <App>
      <MemoryRouter initialEntries={[path]}>
        <FileList />
      </MemoryRouter>
    </App>,
  )
}

/**
 * 等表格的数据行**渲染出来**
 *
 * ⚠️ **不能等"接口被调用过"** ——
 *   `await waitFor(() => expect(listMock).toHaveBeenCalled())` 只能证明
 *   **请求发出去了**,证明不了**数据回来了、React 重渲染完了**。
 *   而这两者之间隔着一整轮 microtask + 一次 React 调度,
 *   满载时那段空窗足够长 —— 于是紧接着的 `querySelector` 拿到 undefined,
 *   报 "Cannot read properties of undefined (reading 'textContent')"。
 *   (2026-10-30 M121 全量跑实测:满载时这条红,单独跑绿。)
 *
 *   ⇒ 等的必须是**要断言的那个东西本身**。
 */
async function waitForRows(count: number): Promise<HTMLElement[]> {
  let rows: HTMLElement[] = []
  await waitFor(() => {
    rows = [...document.querySelectorAll<HTMLElement>('.ant-table-tbody tr.ant-table-row')]
    expect(rows).toHaveLength(count)
  })
  return rows
}

describe('文件列表页 —— 存储列(M82)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({
      list: [fileRow(1, 'local.jpg'), fileRow(2, 'remote.jpg')],
      total: 2,
      page: 1,
      size: 20,
    } as never)
    foldersMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 200 } as never)
    quotaMock.mockResolvedValue({
      used_bytes: 0,
      quota_bytes: 0,
      unlimited: true,
      remain_bytes: 0,
    } as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['file:list', 'file:read', 'file:delete', 'folder:upload']),
      roleCodes: ['role_admin'],
    })
  })

  it('storage=1(本地磁盘)显示「本地」,不是 OSS', async () => {
    renderPage()

    // 本地那一行
    // ⚠️ 选择器要带 .ant-table-row —— antd 5 在 tbody 顶上插一行
    //   `tr.ant-table-measure-row`(量列宽用的),它没有 .ant-table-row 类。
    const rows = await waitForRows(2)
    const localRow = rows[0]!
    expect(localRow.textContent).toContain('本地')
    // ⚠️ **不能**只断言"有本地" —— 那一行也可能同时显示着 OSS
    expect(localRow.textContent).not.toContain('OSS')
  })

  it('storage=2(阿里云 OSS)显示「OSS」', async () => {
    renderPage()

    const rows = await waitForRows(2)
    const ossRow = rows[1]!
    expect(ossRow.textContent).toContain('OSS')
    expect(ossRow.textContent).not.toContain('本地')
  })

  it('两行的存储标签不是同一个(防"整列都写死成 OSS")', async () => {
    renderPage()

    const rows = await waitForRows(2)
    expect(rows).toHaveLength(2)
    expect(rows[0]!.textContent).not.toBe(rows[1]!.textContent)
  })

  it('未知值不猜(不当成 OSS 显示)', async () => {
    listMock.mockResolvedValue({
      list: [fileRow(0, 'weird.jpg')],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()

    const rows = await waitForRows(1)
    const row = rows[0]!
    expect(row.textContent).not.toContain('OSS')
    expect(row.textContent).toContain('本地')
  })

  it('详情抽屉里的「存储」跟列表一致(两处不能一个 OSS 一个本地)', async () => {
    renderPage()

    // 行渲染出来之后才能点它的按钮
    await waitForRows(2)

    // 点第一行的「详情」—— 行操作按钮按 perm 显隐,上面的 setState 已放行 file:read
    const detailBtn = screen.getAllByRole('button', { name: /详\s*情/ })[0]!
    detailBtn.click()

    await waitFor(() => expect(screen.getByText('本地')).toBeInTheDocument())
    // 抽屉里那个「本地」和表格里的「本地」是同一个词 —— 断言的是抽屉存在,
    // 抽屉里的 storage 字段渲染成什么由 viewFields 的 render 决定(同一套判断)。
  })
})

describe('文件列表页 —— 「复制直链」(单桶单树模型)', () => {
  /** clipboard.writeText 的替身 */
  let writeText: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.clearAllMocks()
    writeText = vi.fn().mockResolvedValue(undefined)
    // ⚠️ jsdom **没有实现** navigator.clipboard —— 不塞进去的话
    //   copyToClipboard 会走 textarea + execCommand 降级,而 jsdom 里
    //   execCommand 也不存在 ⇒ 复制静默失败,断言永远等不到。
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })

    listMock.mockResolvedValue({
      list: [fileRow(1, 'local.jpg'), fileRow(2, 'remote.jpg')],
      total: 2,
      page: 1,
      size: 20,
    } as never)
    foldersMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 200 } as never)
    quotaMock.mockResolvedValue({
      used_bytes: 0,
      quota_bytes: 0,
      unlimited: true,
      remain_bytes: 0,
    } as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['file:list', 'file:read', 'file:delete', 'folder:upload']),
      roleCodes: ['role_admin'],
    })
  })

  it('local 的相对路径要补成绝对地址(否则复制出去拼不开)', async () => {
    urlMock.mockResolvedValue({ url: '/rapid-admin/3/default/2/local.jpg' })

    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ antd 5 的 Button 会在汉字之间插空格,匹配要容忍
    // ⚠️ 等**按钮渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitFor(() =>
      expect(
        screen.getAllByRole('button', { name: /复\s*制\s*直\s*链/ }).length,
      ).toBeGreaterThan(0),
    )
    const btn = screen.getAllByRole('button', { name: /复\s*制\s*直\s*链/ })[0]!
    btn.click()

    await waitFor(() => expect(writeText).toHaveBeenCalled())
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/rapid-admin/3/default/2/local.jpg`)
  })

  it('OSS 的绝对地址原样复制(不能再拼 origin)', async () => {
    const ossURL = 'https://rapid-admin-prod.oss-cn-hangzhou.aliyuncs.com/rapid-admin/3/default/2/remote.jpg'
    urlMock.mockResolvedValue({ url: ossURL })

    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ 等**按钮渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitFor(() =>
      expect(
        screen.getAllByRole('button', { name: /复\s*制\s*直\s*链/ }).length,
      ).toBeGreaterThan(0),
    )
    const btn = screen.getAllByRole('button', { name: /复\s*制\s*直\s*链/ })[0]!
    btn.click()

    await waitFor(() => expect(writeText).toHaveBeenCalled())
    expect(writeText).toHaveBeenCalledWith(ossURL)
  })

  it('取地址失败时复制不了,而且不静默 —— 走错误提示', async () => {
    urlMock.mockRejectedValue(new Error('生成访问地址失败'))

    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ 等**按钮渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitFor(() =>
      expect(
        screen.getAllByRole('button', { name: /复\s*制\s*直\s*链/ }).length,
      ).toBeGreaterThan(0),
    )
    const btn = screen.getAllByRole('button', { name: /复\s*制\s*直\s*链/ })[0]!
    btn.click()

    // 没有复制成功,而且页面上给出了错误提示(不是悄悄什么都不发生)
    await waitFor(() => expect(screen.getByText('生成访问地址失败')).toBeInTheDocument())
    expect(writeText).not.toHaveBeenCalled()
  })
})

/**
 * 「设为封面」必须有图片(M125)
 *
 * ⚠️ **后端已经拒了非图片**(`service/folder_impl.go::SetCover` →
 *   "只有图片才能设为封面"),前端要**先**把它禁掉 ——
 *   否则用户点下去吃一个 400,而错误信息里没有"为什么是图片"这句。
 *
 * ⚠️ **SVG 也算非图片** —— SVG 里能塞 `<script>`,而封面会被"点开看大图"
 *   打开成同源文档。前后端口径必须一致(后端 `isCoverImage` 排除 SVG)。
 */
describe('文件列表页 —— 「设为封面」只给图片(M125)', () => {
  /** 「设为封面」那个行操作按钮 */
  function coverButton(): HTMLButtonElement {
    const btn = screen.getAllByRole('button', { name: /设\s*为\s*封\s*面/ })[0]
    expect(btn, '行操作里必须有「设为封面」').toBeTruthy()
    return btn as HTMLButtonElement
  }

  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({
      list: [fileRow(1, 'a.txt', 'text/plain')],
      total: 1,
      page: 1,
      size: 20,
    } as never)
    foldersMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 200 } as never)
    quotaMock.mockResolvedValue({
      used_bytes: 0,
      quota_bytes: 0,
      unlimited: true,
      remain_bytes: 0,
    } as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['file:list', 'file:read', 'file:delete', 'folder:upload']),
      roleCodes: ['role_admin'],
    })
  })

  it('文本文件:「设为封面」是禁用的(不是点了才报错)', async () => {
    renderPage()
    await waitForRows(1)

    expect(coverButton().disabled).toBe(true)
  })

  it('SVG:也是禁用的(前端口径必须与后端一致)', async () => {
    listMock.mockResolvedValue({
      list: [fileRow(1, 'logo.svg', 'image/svg+xml')],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)

    expect(coverButton().disabled).toBe(true)
  })

  it('图片:是能点的', async () => {
    listMock.mockResolvedValue({
      list: [fileRow(1, 'cover.png', 'image/png')],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)

    expect(coverButton().disabled).toBe(false)
  })

  it('不在文件夹里的文件:禁用的(没有"封面"这个概念)', async () => {
    listMock.mockResolvedValue({
      list: [{ ...fileRow(1, 'cover.png', 'image/png'), folder_id: '0' }],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)

    expect(coverButton().disabled).toBe(true)
  })
})

/**
 * 文件夹下拉用**路径**当标签(M125)
 *
 * ⚠️ 多级文件夹之后同一个用户下可以有 "A/图纸" 和 "B/图纸" ——
 *   下拉框里都显示"图纸"的话,用户根本分不清自己选的是哪个。
 */
describe('文件列表页 —— 文件夹下拉用路径当标签(M125)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({
      list: [fileRow(1, 'a.png', 'image/png')],
      total: 1,
      page: 1,
      size: 20,
    } as never)
    foldersMock.mockResolvedValue({
      list: [
        {
          id: '11',
          user_id: '3',
          biz: 'default',
          name: '图纸',
          parent_id: '5',
          cover_key: '',
          cover_file_id: '0',
          file_count: 2,
          total_size: 0,
          is_default: false,
          status: 1,
          created_at: '2026-10-30T08:00:00Z',
          path: 'A/图纸',
          child_count: 0,
          cover_url: '',
        },
      ],
      total: 1,
      page: 1,
      size: 200,
    } as never)
    quotaMock.mockResolvedValue({
      used_bytes: 0,
      quota_bytes: 0,
      unlimited: true,
      remain_bytes: 0,
    } as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['file:list', 'file:read', 'file:delete', 'folder:upload']),
      roleCodes: ['role_admin'],
    })
  })

  it('下拉里显示的是完整路径,不是光秃秃的名字', async () => {
    renderPage()
    await waitForRows(1)

    // 打开文件夹下拉
    const selector = document.querySelector('.ant-select-selector') as HTMLElement
    expect(selector, '页面里必须有文件夹下拉').not.toBeNull()
    fireEvent.mouseDown(selector)

    await waitFor(() =>
      expect(document.querySelector('.ant-select-dropdown')).not.toBeNull(),
    )
    await waitFor(() =>
      expect(document.body.textContent).toContain('A/图纸'),
    )
    // ⚠️ **不能只断言"有 A/图纸"** —— 名字"图纸"本来就在标签里,
    //   得证明标签里**带的是路径**。
    //   而且不能只看第一个选项 —— 第一个是"全部文件夹"。
    const labels = [...document.querySelectorAll('.ant-select-item-option-content')].map(
      (el) => el.textContent ?? '',
    )
    expect(labels.some((l) => l.includes('A/图纸'))).toBe(true)
  })
})

/**
 * 搜索区那个「已用多少空间」必须**跟着文件夹筛选走**(M135)
 *
 * ## 用户报的那句
 *
 * > 「文件列表界面 search-section 里面的已用多少空间这个数值,
 * > 不会跟随切换文件夹而变化。」
 *
 * ## 根因不是"忘了刷新",是**语义错配**
 *
 *   那个数是 `quota.used_bytes`(`sys_user.storage_used_bytes`,**账号总用量**)
 *   —— 它**按定义**就不跟随文件夹变。但它就摆在文件夹筛选框旁边、连
 *   "已用"两个字都没有,用户理所当然以为它说的是**当前文件夹**。
 *
 *   ⇒ 修法不是"让它刷新",是**把两个口径拆开**:
 *   · **本文件夹**(搜索区,紧挨着筛选框):选中文件夹时显示它的
 *     `total_size` / `file_count`。
 *     ⚠️ 口径与文件列表**完全一致** —— 后端 `folderFileStats` 统计的是
 *     "直接放在这个文件夹里的文件"(不递归子文件夹),文件列表按
 *     `folder_id` 过滤也是同一口径 ⇒ 标签上的数 = 表格里那些文件的大小。
 *   · **总用量**(工具栏):账号级,永远不变,标签写明「总用量」。
 *
 * ## ⚠️ 反断言同样重要
 *
 *   "切文件夹后标签变了"这条,一个**每次都显示账号总用量**的实现也能通过
 *   —— 只要它换了别的数。所以下面每条都额外断言"**另一个数没变**"。
 */
describe('文件列表页 —— 用量跟着文件夹走(M135)', () => {
  /** 一个文件夹(字段与后端 service.FolderView 对齐) */
  function folderRow(id: string, path: string, fileCount: number, totalSize: number) {
    return {
      id,
      user_id: '3',
      biz: 'default',
      name: path.split('/').pop() ?? path,
      parent_id: '0',
      cover_key: '',
      cover_file_id: '0',
      file_count: fileCount,
      total_size: totalSize,
      is_default: false,
      status: 1,
      created_at: '2026-10-31T08:00:00Z',
      path,
      child_count: 0,
      cover_url: '',
    }
  }

  /** 打开文件夹下拉(antd 5 懒渲染,不开一个选项都不在 DOM 里) */
  async function openFolderDropdown() {
    const selector = document.querySelector('.ant-select-selector') as HTMLElement
    expect(selector, '页面里必须有文件夹下拉').toBeTruthy()
    fireEvent.mouseDown(selector)
    await waitFor(() => expect(document.querySelector('.ant-select-dropdown')).toBeTruthy())
  }

  /** 点某个文件夹选项 */
  async function pickFolder(path: string) {
    await openFolderDropdown()
    const option = Array.from(document.querySelectorAll('.ant-select-item-option')).find((el) =>
      (el.textContent ?? '').includes(path),
    )
    expect(option, `下拉里必须有 ${path}`).toBeTruthy()
    fireEvent.click(option!)
  }

  /** 作用域用量标签 */
  function scopeTag(): HTMLElement | null {
    return document.querySelector('[data-testid="scope-usage"]')
  }

  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({
      list: [fileRow(1, 'a.png', 'image/png')],
      total: 1,
      page: 1,
      size: 20,
    } as never)
    foldersMock.mockResolvedValue({
      list: [
        folderRow('11', 'A/图纸', 2, 1024),
        folderRow('22', 'B/图纸', 5, 5 * 1024 * 1024),
      ],
      total: 2,
      page: 1,
      size: 200,
    } as never)
    quotaMock.mockResolvedValue({
      used_bytes: 100 * 1024 * 1024,
      quota_bytes: 0,
      unlimited: true,
      remain_bytes: 0,
    } as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['file:list', 'file:read', 'file:delete', 'folder:upload']),
      roleCodes: ['role_admin'],
    })
  })

  it('没选文件夹时不显示「本文件夹」标签(那时只有账号总用量)', async () => {
    renderPage()
    await waitForRows(1)

    expect(scopeTag()).toBeNull()
  })

  it('选中文件夹后,搜索区显示的是**那个文件夹**的大小和个数', async () => {
    renderPage()
    await waitForRows(1)

    await pickFolder('A/图纸')

    // ⚠️ 等**标签渲染出来**,不是等"请求发出去了"(M121/M127 的规矩)
    await waitFor(() => expect(scopeTag()).not.toBeNull())
    const tag = scopeTag()!
    expect(tag.textContent).toContain('本文件夹')
    expect(tag.textContent).toContain('1.0 KB')
    expect(tag.textContent).toContain('2 个文件')
    // ⚠️ 反断言:不能是账号总用量(100.0 MB)—— 那是另一个口径
    expect(tag.textContent).not.toContain('100.0 MB')
  })

  it('**切到另一个文件夹,数字跟着变**(用户报的就是这句)', async () => {
    renderPage()
    await waitForRows(1)

    await pickFolder('A/图纸')
    await waitFor(() => expect(scopeTag()?.textContent).toContain('1.0 KB'))

    await pickFolder('B/图纸')
    await waitFor(() => expect(scopeTag()?.textContent).toContain('5.0 MB'))
    expect(scopeTag()!.textContent).toContain('5 个文件')
    // 反断言:不能还停在上一个文件夹的数
    expect(scopeTag()!.textContent).not.toContain('1.0 KB')
  })

  it('工具栏那个是**账号级**总用量,不跟随文件夹(两个口径别混)', async () => {
    renderPage()
    await waitForRows(1)

    expect(document.body.textContent).toContain('总用量')
    expect(document.body.textContent).toContain('100.0 MB')

    await pickFolder('A/图纸')
    await waitFor(() => expect(scopeTag()).not.toBeNull())

    // 账号总用量没变 —— 它就是账号级的
    expect(document.body.textContent).toContain('100.0 MB')
  })

  it('文件夹不在下拉列表里(URL 直接带 folder_id)也要能显示用量', async () => {
    // 下拉只拉前 200 条,URL 带进来的 id 可能不在里面 ⇒ 得单独拉详情
    folderMock.mockResolvedValue({ ...folderRow('99', 'C/图纸', 7, 2048) } as never)

    renderPageAt('/?folder_id=99')
    await waitForRows(1)

    await waitFor(() => expect(scopeTag()?.textContent).toContain('2.0 KB'))
    expect(folderMock).toHaveBeenCalledWith('99')
    expect(scopeTag()!.textContent).toContain('7 个文件')
  })

  it('文件夹拉不到(被删了)时**不显示标签**,也不把页面搞挂', async () => {
    folderMock.mockRejectedValue(new Error('文件夹不存在'))

    renderPageAt('/?folder_id=99')
    await waitForRows(1)

    await waitFor(() => expect(folderMock).toHaveBeenCalled())
    expect(scopeTag()).toBeNull()
    // 表格还在(没被搞挂)
    expect(document.querySelectorAll('.ant-table-tbody tr.ant-table-row')).toHaveLength(1)
  })

  it('删完文件要重拉文件夹列表(不然标签上还是删之前的数)', async () => {
    deleteMock.mockResolvedValue(undefined)

    renderPage()
    await waitForRows(1)
    await pickFolder('A/图纸')
    await waitFor(() => expect(scopeTag()).not.toBeNull())

    // 挂载时拉过一次
    expect(foldersMock).toHaveBeenCalledTimes(1)

    // 点第一行的「删除」
    fireEvent.click(screen.getAllByRole('button', { name: /删\s*除/ })[0]!)

    // 确认框里的「确认」
    // ⚠️ antd 5 的 Button 会在汉字之间插空格 ⇒ 匹配前先把空白去掉
    const modal = await waitFor(() => {
      const el = document.querySelector('.ant-modal')
      expect(el).not.toBeNull()
      return el as HTMLElement
    })
    const okBtn = Array.from(modal.querySelectorAll('button')).find((b) =>
      (b.textContent ?? '').replace(/\s/g, '').includes('确认'),
    )
    expect(okBtn, '确认框里必须有确定按钮').toBeTruthy()
    fireEvent.click(okBtn as HTMLButtonElement)

    await waitFor(() => expect(deleteMock).toHaveBeenCalled())
    // ⚠️ **这条守的是"删完要重拉文件夹列表"** —— 不重拉的话,
    //   标签上的大小/个数还是删之前的值(而文件已经没了)。
    await waitFor(() => expect(foldersMock).toHaveBeenCalledTimes(2))
  })
})
