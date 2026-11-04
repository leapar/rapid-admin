/**
 * 文件夹管理页 —— 表格 / 卡片双视图(M144)
 *
 * ## 这条网守的是什么
 *
 * 用户 2026-10-31 提的:「文件夹管理列表也加卡片模式」。
 *
 * 文件夹是**有封面的**(M125 起「设为封面」),而表格里只有
 * "名字 / 文件数 / 大小" —— **看不出这个文件夹长什么样**。
 * 卡片视图把封面铺开,一屏扫过去就知道"哪个是图纸、哪个是照片、哪个是空的"。
 *
 * | 事 | 靠什么 | 少了会怎样 |
 * |---|---|---|
 * | 切视图 | Radio.Group 两态开关 | 用户找不到入口 |
 * | 卡片自己画分页器 | `ManagementTable` 的 `body` 槽位 | 表格那份分页器随 `<Table>` 一起没了 |
 * | **两种视图的操作是同一份** | `actionsFor` | 表格里能删、卡片里删不了 = 用户看着就是 bug |
 * | 视图模式要记住 | localStorage | 每次进来都要重设一次 |
 *
 * ## ⚠️ **可逆探针(拆掉实现看会不会红)**
 *
 * · 把 `body={...}` 去掉 → 「切到卡片视图后,一个文件夹一张卡片」红;
 * · 把 `actionsFor(row)` 换回列里内联的那份 → 「卡片里的操作跟表格一致」红;
 * · 把 `storage.set` 去掉 → 「视图模式记下来了」红;
 * · 把 FolderGrid 里的 `<Pagination>` 删掉 → 「卡片视图也有分页器」红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor, screen, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import FolderList from '../folder'
import { listFolders, listFolderUsers, getFolder, listFiles } from '@/api/upload'
import { usePermissionStore } from '@/stores/permission'
import { storage } from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'

vi.mock('@/api/upload', () => ({
  listFolders: vi.fn(),
  listFolderUsers: vi.fn(),
  createFolder: vi.fn(),
  deleteFolder: vi.fn(),
  getFolder: vi.fn(),
  // ⚠️ 2026-11-03 混合列表:页面无条件 import 这三个 —— mock 工厂替换整个
  //   模块,漏一个就是 "No listFiles export is defined on the mock"(整文件红)。
  //   本文件的用例都在根级 / 用户层(不进文件夹),listFiles 实际不会被调,
  //   但 import 形态必须齐。
  listFiles: vi.fn(),
  deleteFile: vi.fn(),
  downloadFile: vi.fn(),
}))

const listMock = vi.mocked(listFolders)
const usersMock = vi.mocked(listFolderUsers)
const detailMock = vi.mocked(getFolder)
const filesMock = vi.mocked(listFiles)

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
  path: string
  child_count: number
  cover_url: string
}

function folderRow(id: string, name: string, extra: Partial<FolderRow> = {}): FolderRow {
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
    created_at: '2026-10-31T08:00:00Z',
    path: name,
    child_count: 0,
    cover_url: '',
    ...extra,
  }
}

function renderPage(initialEntries: string[] = ['/file/folder']) {
  return render(
    <App>
      <MemoryRouter initialEntries={initialEntries}>
        <FolderList />
      </MemoryRouter>
    </App>,
  )
}

/** 等表格的数据行渲染出来 */
async function waitForRows(count: number): Promise<HTMLElement[]> {
  let rows: HTMLElement[] = []
  await waitFor(() => {
    rows = [...document.querySelectorAll<HTMLElement>('.ant-table-tbody tr.ant-table-row')]
    expect(rows).toHaveLength(count)
  })
  return rows
}

/** 等卡片渲染出来 */
async function waitForCards(count: number): Promise<HTMLElement[]> {
  let cards: HTMLElement[] = []
  await waitFor(() => {
    cards = [...document.querySelectorAll<HTMLElement>('[data-testid="folder-grid-card"]')]
    expect(cards).toHaveLength(count)
  })
  return cards
}

/** 切到卡片视图 */
async function switchToGrid() {
  const radio = await waitFor(() => screen.getByRole('radio', { name: /卡\s*片/ }))
  fireEvent.click(radio)
  await waitFor(() => expect(document.querySelector('[data-testid="folder-grid"]')).not.toBeNull())
}

/**
 * 展开卡片右上角的「⋯」操作菜单,返回菜单项的文字(2026-11-01 起操作收进了菜单)。
 *
 * ⚠️ **用 `mouseOver` 而不是 `click`** —— 用户要的是**鼠标移入展开**,
 *   这里必须真的走 hover 那条路。
 *
 * ⚠️ React 的 `onMouseEnter` 是从原生 `mouseover` 合成出来的
 *   (React 17+ 不监听原生 `mouseenter`)⇒ 必须派发 `mouseOver`。
 *
 * ⚠️ rc-trigger 的 hover 有 **100ms 延迟** ⇒ 必须 `waitFor` 等菜单出现。
 */
async function openCardMenuLabels(card: HTMLElement): Promise<string[]> {
  const trigger = card.querySelector<HTMLElement>('[data-testid="card-actions-trigger"]')
  expect(trigger, '卡片右上角必须有「⋯」操作按钮').toBeTruthy()
  fireEvent.mouseOver(trigger!)
  const menu = await waitFor(() => {
    const el = document.querySelector<HTMLElement>('.ant-dropdown-menu')
    expect(el, '悬浮后菜单必须展开').not.toBeNull()
    return el!
  })
  return Array.from(menu.querySelectorAll('.ant-dropdown-menu-item')).map((li) =>
    (li.textContent ?? '').replace(/\s/g, ''),
  )
}

describe('文件夹管理页 —— 表格 / 卡片双视图(M144)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // ⚠️ 视图模式是**记在 localStorage 里**的 —— 不清掉的话,
    //   上一条用例切到卡片,下一条用例首屏就是卡片 ⇒ 假绿/假红。
    storage.remove(STORAGE_KEYS.FILE_FOLDER_VIEW)

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
    // ⚠️ 用户层接口也要 mock —— `cross_user` 是这一页的层级判据
    usersMock.mockResolvedValue({
      list: [],
      total: 0,
      page: 1,
      size: 20,
      cross_user: false,
      current_user_id: '3',
    } as never)
    // ⚠️ 混合列表(2026-11-03)会按需拉文件 —— 根级用例不触发,给个空页兜底
    filesMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 100 } as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['folder:list', 'folder:create', 'folder:delete', 'file:list']),
      roleCodes: ['role_admin'],
    })
  })

  it('默认是表格视图(卡片是"切过去"的,不是默认态)', async () => {
    renderPage()
    await waitForRows(2)
    expect(document.querySelector('[data-testid="folder-grid"]')).toBeNull()
  })

  it('切到卡片视图后,一个文件夹一张卡片', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()

    const cards = await waitForCards(2)
    expect(cards[0]!.textContent).toContain('户型图')
    expect(cards[1]!.textContent).toContain('业主照片')
    // 表格被换掉了(不是两个叠在一起)
    expect(document.querySelectorAll('.ant-table-tbody tr.ant-table-row')).toHaveLength(0)
  })

  it('有封面 → 卡片上渲染那张封面', async () => {
    listMock.mockResolvedValue({
      list: [
        folderRow('1', '户型图', {
          cover_key: 'res/3/default/1/cover.png',
          cover_file_id: '5',
          cover_url: '/rapid-admin/3/default/1/cover.png',
        }),
        folderRow('2', '业主照片'),
      ],
      total: 2,
      page: 1,
      size: 20,
      owner_name: '张三',
    } as never)

    renderPage()
    await waitForRows(2)
    await switchToGrid()
    const cards = await waitForCards(2)

    const img = cards[0]!.querySelector('img')
    expect(img, '有封面的文件夹必须渲染封面图').toBeTruthy()
    expect(img!.getAttribute('src')).toBe('/rapid-admin/3/default/1/cover.png')
    // 没封面的那张不许有 img(不能画一张裂图)
    expect(cards[1]!.querySelector('img')).toBeNull()
  })

  it('没有封面 → 摆文件夹图标(不是一张裂图)', async () => {
    renderPage()
    await waitForRows(2)
    await switchToGrid()
    const cards = await waitForCards(2)

    expect(cards[0]!.querySelector('img')).toBeNull()
    expect(cards[0]!.querySelector('.anticon-folder'), '没封面也要有个文件夹图标').toBeTruthy()
  })

  it('封面加载失败 → 换成文件夹图标(不是浏览器那张碎图)', async () => {
    listMock.mockResolvedValue({
      list: [
        folderRow('1', '户型图', { cover_url: '/rapid-admin/3/default/1/cover.png' }),
      ],
      total: 1,
      page: 1,
      size: 20,
      owner_name: '张三',
    } as never)

    renderPage()
    await waitForRows(1)
    await switchToGrid()
    const cards = await waitForCards(1)

    const img = cards[0]!.querySelector('img')!
    expect(img).toBeTruthy()
    fireEvent.error(img)

    await waitFor(() => expect(cards[0]!.querySelector('img')).toBeNull())
    expect(cards[0]!.querySelector('.anticon-folder')).toBeTruthy()
  })

  it('「默认」徽标贴在封面区左上角(不占文档流 ⇒ 卡片高度一致)', async () => {
    listMock.mockResolvedValue({
      list: [
        folderRow('1', '户型图', { is_default: true }),
        folderRow('2', '业主照片'),
      ],
      total: 2,
      page: 1,
      size: 20,
      owner_name: '张三',
    } as never)

    renderPage()
    await waitForRows(2)
    await switchToGrid()
    const cards = await waitForCards(2)

    const badge = cards[0]!.querySelector('[data-testid="folder-grid-default"]')
    expect(badge, '默认文件夹必须有徽标').toBeTruthy()
    expect((badge as HTMLElement).style.position).toBe('absolute')
    // ⚠️ 2026-11-01:在**左上角**(右上角让给了「⋯」操作按钮)
    expect((badge as HTMLElement).style.left).toBe('0px')
    expect((badge as HTMLElement).style.right, '徽标不该再占右上角').toBe('')
    expect(badge!.parentElement).toBe(cards[0]!.querySelector('[data-testid="folder-grid-thumb"]'))
    // 没默认的那张不许有徽标
    expect(cards[1]!.querySelector('[data-testid="folder-grid-default"]')).toBeNull()

    // ⚠️ **关键不变式**:有默认 / 没默认,卡片体的直接子节点数**一样**
    const bodyChildCount = (c: HTMLElement) => c.querySelector('.ant-card-body')!.children.length
    expect(bodyChildCount(cards[0]!)).toBe(bodyChildCount(cards[1]!))
  })

  it('卡片里的操作跟表格一致(同一份 actionsFor)', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    // 五个操作一个都不能少(进入 / 详情 / 新建子文件夹 / 查看文件 / 删除)
    const labels = await openCardMenuLabels(cards[0]!)
    expect(labels.some((l) => l.includes('进入'))).toBe(true)
    expect(labels.some((l) => l.includes('详情'))).toBe(true)
    expect(labels.some((l) => l.includes('新建子文件夹'))).toBe(true)
    expect(labels.some((l) => l.includes('查看文件'))).toBe(true)
    expect(labels.some((l) => l.includes('删除'))).toBe(true)
  })

  it('卡片里的操作收进右上角的「⋯」菜单:悬浮展开,操作从上到下排开', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    // 卡片底部不再并排铺一排文字按钮(那是旧形态)
    expect(
      cards[0]!.querySelectorAll('.ant-btn-link').length,
      '操作不该再并排铺在卡片底部',
    ).toBe(0)

    // 右上角有那个「⋯」按钮
    const trigger = cards[0]!.querySelector<HTMLElement>('[data-testid="card-actions-trigger"]')
    expect(trigger, '卡片右上角必须有「⋯」操作按钮').toBeTruthy()
    // ⚠️ 它就压在封面图上 ⇒ 绝对定位,不占文档流
    expect((trigger!.parentElement as HTMLElement).style.position).toBe('absolute')
    expect((trigger!.parentElement as HTMLElement).style.right).toBe('0px')
    // ⚠️ **竖着的三个点**(`EllipsisOutlined` = ⋮),不是横着的(`MoreOutlined` = ⋯)
    //   —— 用户 2026-11-01 明确要求的
    expect(
      trigger!.querySelector('.anticon-ellipsis'),
      '触发按钮要用竖着的三个点(anticon-ellipsis)',
    ).toBeTruthy()
    expect(trigger!.querySelector('.anticon-more'), '不该用横着的三个点').toBeNull()
    // ⚠️ **28×20 的长方形**(2026-11-01 用户提的:「现在是 28*28,应该改矮一点」
    //   「改矮一点,改宽一点,变成长方形」)
    //   —— 本仓 `Button.controlHeightSM` = 28,small 按钮默认 28×28,太高
    expect(trigger!.style.height, '触发按钮要压到 20px 高').toBe('20px')
    expect(trigger!.style.width, '触发按钮要宽过 20px(长方形)').toBe('28px')

    // 悬浮展开
    const labels = await openCardMenuLabels(cards[0]!)
    // ⚠️ **从上到下**:菜单项的顺序就是 actionsFor 的顺序
    expect(labels[0]).toContain('进入')
    expect(labels[labels.length - 1]).toContain('删除')
  })

  it('别人的空间(只读)→ 卡片里也没有「删除」「新建子文件夹」', async () => {
    usersMock.mockResolvedValue({
      list: [],
      total: 0,
      page: 1,
      size: 20,
      cross_user: true,
      current_user_id: '3',
    } as never)

    renderPage(['/file/folder?user_id=7'])
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    const labels = await openCardMenuLabels(cards[0]!)
    expect(labels.some((l) => l.includes('删除')), '别人的空间里不该有「删除」').toBe(false)
    expect(labels.some((l) => l.includes('新建子文件夹')), '别人的空间里不该有「新建子文件夹」').toBe(
      false,
    )
  })

  it('卡片视图也有分页器,翻页会重新拉数据', async () => {
    listMock.mockResolvedValue({
      list: [folderRow('1', '户型图')],
      total: 40,
      page: 1,
      size: 20,
      owner_name: '张三',
    } as never)

    renderPage()
    await waitForRows(1)
    await switchToGrid()
    await waitForCards(1)

    // 表格那份分页器随 <Table> 一起没了 —— 卡片视图自己渲染一个
    const page2 = document.querySelector('.ant-pagination-item[title="2"]') as HTMLElement
    expect(page2, '卡片视图必须自己有分页器').toBeTruthy()
    fireEvent.click(page2)

    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ page: 2 })),
    )
  })

  it('视图模式记下来了(下次进来还是卡片)', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()

    // 落盘的是"信封"格式(utils/storage.ts),值在 .value 里
    const raw = localStorage.getItem(STORAGE_KEYS.FILE_FOLDER_VIEW)
    expect(raw).not.toBeNull()
    expect(JSON.parse(raw!).value).toBe('grid')
  })

  it('localStorage 里已经是卡片模式 → 首屏就是卡片', async () => {
    storage.set(STORAGE_KEYS.FILE_FOLDER_VIEW, 'grid')

    renderPage()

    await waitForCards(2)
    expect(document.querySelectorAll('.ant-table-tbody tr.ant-table-row')).toHaveLength(0)
  })

  it('点文件夹名 → 进入这个文件夹(URL 跟着变)', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    const link = Array.from(cards[0]!.querySelectorAll('a')).find((a) =>
      (a.textContent ?? '').includes('户型图'),
    )
    expect(link, '卡片上的文件夹名必须能点').toBeTruthy()
    fireEvent.click(link as HTMLAnchorElement)

    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ parentId: '1' })),
    )
  })

  it('切回表格视图,表格还在(不是切坏了)', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()
    await waitForCards(2)

    const radio = await waitFor(() => screen.getByRole('radio', { name: /表\s*格/ }))
    fireEvent.click(radio)

    await waitForRows(2)
    expect(document.querySelector('[data-testid="folder-grid"]')).toBeNull()
  })
})

/**
 * 2026-11-01 —— 卡片视图的「点整张卡进入」
 *
 * 用户提的:「文件夹管理里面的列表要支持点击进入」。
 * 表格视图那一半在 `folder.test.tsx` 里(同一份 `onRow` 判定),
 * 这一半守的是**卡片**。
 *
 * ## ⚠️ **这一半比表格那一半更容易出事**
 *
 * 卡片右上角的「⋯」菜单是 **portal** 渲染的 —— DOM 上它不在卡片里,
 * 但 **React 事件沿组件树冒泡** ⇒ 菜单项的点击照样会冒到卡片的 onClick 上。
 * 不挡的话:点「⋯」里的「删除」会**顺带把那个文件夹打开**。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 * · 把 `FolderCard` 上的 `onClick` 去掉 → 「点卡片进入」红;
 * · 把 `isInteractiveTarget` 改成永远返回 false → 「点菜单不进入」红;
 * · 把 `.ant-dropdown` 从选择器里去掉 → 「点菜单项不进入」红。
 */
describe('文件夹管理页 —— 卡片视图点整卡进入(2026-11-01)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    storage.remove(STORAGE_KEYS.FILE_FOLDER_VIEW)

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
    usersMock.mockResolvedValue({
      list: [],
      total: 0,
      page: 1,
      size: 20,
      cross_user: false,
      current_user_id: '3',
    } as never)
    usePermissionStore.setState({
      permissionCodes: new Set(['folder:list', 'folder:create', 'folder:delete', 'file:list']),
      roleCodes: ['role_admin'],
    })
  })

  it('点卡片(封面)→ 进入这个文件夹', async () => {
    renderPage()
    await waitForRows(2)
    await switchToGrid()
    const cards = await waitForCards(2)

    const thumb = cards[0]!.querySelector<HTMLElement>('[data-testid="folder-grid-thumb"]')
    expect(thumb, '卡片必须有封面区').toBeTruthy()
    fireEvent.click(thumb!)

    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ parentId: '1' })),
    )
  })

  it('点右上角的「⋯」→ 菜单展开,但**不**进入那个文件夹', async () => {
    renderPage()
    await waitForRows(2)
    await switchToGrid()
    const cards = await waitForCards(2)

    const trigger = cards[0]!.querySelector<HTMLElement>('[data-testid="card-actions-trigger"]')
    expect(trigger).toBeTruthy()
    fireEvent.click(trigger!)

    await waitFor(() => expect(document.querySelector('.ant-dropdown-menu')).not.toBeNull())
    // ⚠️ **光打开菜单不算进入** —— 按钮的点击会冒泡到卡片上
    expect(listMock).not.toHaveBeenCalledWith(expect.objectContaining({ parentId: '1' }))
  })

  it('点「⋯」菜单里的「详情」→ 打开详情抽屉,但**不**进入那个文件夹', async () => {
    renderPage()
    await waitForRows(2)
    await switchToGrid()
    const cards = await waitForCards(2)

    const trigger = cards[0]!.querySelector<HTMLElement>('[data-testid="card-actions-trigger"]')
    fireEvent.mouseOver(trigger!)
    const menu = await waitFor(() => {
      const el = document.querySelector<HTMLElement>('.ant-dropdown-menu')
      expect(el, '悬浮后菜单必须展开').not.toBeNull()
      return el!
    })
    const detailItem = Array.from(menu.querySelectorAll<HTMLElement>('.ant-dropdown-menu-item')).find(
      (li) => (li.textContent ?? '').replace(/\s/g, '').includes('详情'),
    )
    expect(detailItem, '菜单里必须有「详情」').toBeTruthy()
    fireEvent.click(detailItem!)

    await waitFor(() => expect(document.querySelector('.ant-drawer')).not.toBeNull())
    // ⚠️ **菜单项的点击也会冒泡回卡片**(菜单是 portal 渲染的,DOM 上不在卡片里,
    //   但 React 事件沿组件树冒泡)⇒ 不挡的话这里会顺带把文件夹打开。
    expect(listMock).not.toHaveBeenCalledWith(expect.objectContaining({ parentId: '1' }))
  })
})
