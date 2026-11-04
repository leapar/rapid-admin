/**
 * 文件列表页 —— 表格 / 缩略图双视图(M134)
 *
 * ## 这条网守的是什么
 *
 * ### 1. 缩略图靠列表里那条**稳定直链**(`FileView.url`),不逐条调接口
 *
 *   缩略图视图要给每个文件渲染一个 `<img src>`。如果每张图都先去调一次
 *   `/files/:id/url`,一页 50 条就是 50 个往返 —— 列表会卡住。
 *   ⇒ M134 起**列表接口就把直链带回来了**(服务端拼直链只是字符串拼接,
 *   不是签名,一次列表 N 条也就 N 次字符串拼接)。
 *
 *   反断言:切到缩略图视图**不许**调 `getFileUrl`。
 *
 * ### 2. 预览不出来的时候**不能画一张碎图**
 *
 *   `<img>` 加载失败(直链 403 / 对象没了)时浏览器画的是"碎图"图标,
 *   夹在一堆真图里,用户会以为文件传坏了。
 *   ⇒ 加载失败要换成占位图标 + "无法预览"。
 *
 * ### 3. 两种视图的**操作是同一份**
 *
 *   表格里"设为封面"能点、缩略图里点不了 = 用户看着就是 bug。
 *   ⇒ 缩略图里的「删除」走的是**同一个** `deleteFile`,不是另写一套。
 *
 * ### 4. 视图模式要**记住**
 *
 *   用户切到缩略图看一遍,下次进来还得再点一次 —— 这种界面状态最劝退。
 *
 * ## ⚠️ **等的是"渲染出来",不是"请求发出去了"**(2026-10-31)
 *
 *   `await waitFor(() => expect(listMock).toHaveBeenCalled())` 只能证明
 *   **请求发出去了**,证明不了**数据回来了、React 重渲染完了**。
 *   满载时这个间隔足够长 ⇒ 紧接着的 `querySelector` 拿到 undefined。
 *   ⇒ 等的必须是**要断言的那个东西本身**(卡片 / 图片)。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor, screen, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import FileList from '../list'
import { listFiles, listFolders, getFileQuota, deleteFile, getFileUrl } from '@/api/upload'
import { usePermissionStore } from '@/stores/permission'
import { storage } from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'

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
const deleteMock = vi.mocked(deleteFile)
const urlMock = vi.mocked(getFileUrl)

/** 一行文件(字段与后端 service.FileView 对齐;M134 起列表里带 url) */
function fileRow(opts: {
  id?: string
  name: string
  contentType?: string
  url?: string
  isCover?: boolean
  /** M141:音视频时长(ms);0 = 没探到 */
  durationMs?: number
  /** M141:视频封面直链;空 = 没有封面 */
  coverUrl?: string
  /** M145d:后端判定的"浏览器播不了"原因;空 = 能播 */
  mediaReason?: string
  /** M145d:容器名(MKV / FLV …) */
  mediaContainer?: string
  /** M145d:视频编码 */
  mediaVideoCodec?: string
  /** M145d:音频编码 */
  mediaAudioCodec?: string
  /** M145d:转码状态(0 不需要 / 1 待转 / 2 转码中 / 3 完成 / 4 失败) */
  transcodeStatus?: number
}) {
  return {
    id: opts.id ?? '1',
    folder_id: '2',
    user_id: '3',
    biz: 'default',
    file_key: `rapid-admin/3/default/2/${opts.name}`,
    file_name: opts.name,
    file_size: 2048,
    content_type: opts.contentType ?? 'image/jpeg',
    etag: 'e',
    storage: 1,
    is_cover: opts.isCover ?? false,
    status: 1,
    created_at: '2026-10-31T08:00:00Z',
    url: opts.url,
    duration_ms: opts.durationMs ?? 0,
    cover_url: opts.coverUrl,
    media_reason: opts.mediaReason,
    media_container: opts.mediaContainer,
    media_video_codec: opts.mediaVideoCodec,
    media_audio_codec: opts.mediaAudioCodec,
    transcode_status: opts.transcodeStatus,
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

/** 等表格的数据行**渲染出来**(M121 的规矩:等要断言的东西本身) */
async function waitForRows(count: number): Promise<HTMLElement[]> {
  let rows: HTMLElement[] = []
  await waitFor(() => {
    rows = [...document.querySelectorAll<HTMLElement>('.ant-table-tbody tr.ant-table-row')]
    expect(rows).toHaveLength(count)
  })
  return rows
}

/** 等缩略图**卡片**渲染出来 */
async function waitForCards(count: number): Promise<HTMLElement[]> {
  let cards: HTMLElement[] = []
  await waitFor(() => {
    cards = [...document.querySelectorAll<HTMLElement>('[data-testid="file-grid-card"]')]
    expect(cards).toHaveLength(count)
  })
  return cards
}

/** 切到缩略图视图 */
async function switchToGrid() {
  const radio = await waitFor(() => screen.getByRole('radio', { name: /缩\s*略\s*图/ }))
  fireEvent.click(radio)
  await waitFor(() => expect(document.querySelector('[data-testid="file-grid"]')).not.toBeNull())
}

/**
 * 展开卡片右上角的「⋯」操作菜单(2026-11-01 起操作收进了菜单)。
 *
 * ⚠️ **用 `mouseOver` 而不是 `click`** —— 用户要的是**鼠标移入展开**,
 *   这里必须真的走 hover 那条路(click 也留着,但那不是用户要的那条)。
 *
 * ⚠️ React 的 `onMouseEnter` 是从原生 `mouseover` 合成出来的
 *   (React 17+ 不监听原生 `mouseenter`)⇒ 必须派发 `mouseOver`,
 *   派发 `mouseEnter` 什么都不会发生(测试会假绿)。
 *
 * ⚠️ rc-trigger 的 hover 有 **100ms 延迟** ⇒ 必须 `waitFor` 等菜单出现,
 *   不能同步断言。
 */
async function openCardMenu(card: HTMLElement): Promise<HTMLElement> {
  const trigger = card.querySelector<HTMLElement>('[data-testid="card-actions-trigger"]')
  expect(trigger, '卡片右上角必须有「⋯」操作按钮').toBeTruthy()
  fireEvent.mouseOver(trigger!)
  return await waitFor(() => {
    const menu = document.querySelector<HTMLElement>('.ant-dropdown-menu')
    expect(menu, '悬浮后菜单必须展开').not.toBeNull()
    return menu!
  })
}

describe('文件列表页 —— 表格 / 缩略图双视图(M134)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // ⚠️ 视图模式是**记在 localStorage 里**的(M134)——
    //   不清掉的话,上一条用例切到缩略图,下一条用例首屏就是缩略图 ⇒ 假绿/假红。
    storage.remove(STORAGE_KEYS.FILE_LIST_VIEW)

    listMock.mockResolvedValue({
      list: [
        fileRow({ id: '1', name: 'plan.jpg', url: '/rapid-admin/3/default/2/plan.jpg' }),
        fileRow({ id: '2', name: 'notes.txt', contentType: 'text/plain' }),
      ],
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

  it('默认是表格视图(缩略图是"切过去"的,不是默认态)', async () => {
    renderPage()

    await waitForRows(2)
    expect(document.querySelector('[data-testid="file-grid"]')).toBeNull()
  })

  it('切到缩略图视图后,一个文件一张卡片', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()

    const cards = await waitForCards(2)
    expect(cards[0]!.textContent).toContain('plan.jpg')
    expect(cards[1]!.textContent).toContain('notes.txt')
    // 表格被换掉了(不是两个叠在一起)
    expect(document.querySelectorAll('.ant-table-tbody tr.ant-table-row')).toHaveLength(0)
  })

  it('图片文件渲染缩略图,src 就是列表里那条直链', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    const img = cards[0]!.querySelector('img')
    expect(img, '图片文件必须渲染 <img>').toBeTruthy()
    expect(img!.getAttribute('src')).toBe('/rapid-admin/3/default/2/plan.jpg')
  })

  it('缩略图**不许**逐条去调 getFileUrl(否则一页就是 N 个往返)', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()
    await waitForCards(2)

    expect(urlMock).not.toHaveBeenCalled()
  })

  it('非图片文件不渲染 <img>(摆的是占位图标)', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    expect(cards[1]!.querySelector('img')).toBeNull()
    // 占位图标是个 svg(antd 图标)
    expect(cards[1]!.querySelector('.anticon')).toBeTruthy()
  })

  it('图片但没有直链(后端签不出来)→ 不渲染 <img>,不画碎图', async () => {
    listMock.mockResolvedValue({
      list: [fileRow({ id: '1', name: 'broken.jpg' })],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)

    await switchToGrid()
    const cards = await waitForCards(1)

    expect(cards[0]!.querySelector('img')).toBeNull()
    expect(cards[0]!.querySelector('.anticon')).toBeTruthy()
  })

  it('直链加载失败 → 换成占位图标(不是浏览器那张碎图)', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    const img = cards[0]!.querySelector('img')!
    expect(img).toBeTruthy()
    fireEvent.error(img)

    await waitFor(() => expect(cards[0]!.querySelector('img')).toBeNull())
    expect(cards[0]!.querySelector('.anticon')).toBeTruthy()
  })

  it('直链变了之后,加载失败的记号要清掉(不是永远占位)', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    fireEvent.error(cards[0]!.querySelector('img')!)
    await waitFor(() => expect(cards[0]!.querySelector('img')).toBeNull())

    // 后端换了一条直链(比如 OSS 桶改成公共读之后重新拼出来的)
    listMock.mockResolvedValue({
      list: [
        fileRow({ id: '1', name: 'plan.jpg', url: '/rapid-admin/3/default/2/plan.jpg?v=2' }),
        fileRow({ id: '2', name: 'notes.txt', contentType: 'text/plain' }),
      ],
      total: 2,
      page: 1,
      size: 20,
    } as never)

    // 点「刷新配额」⇒ 顺带 reload 列表
    fireEvent.click(screen.getByRole('button', { name: /刷\s*新\s*配\s*额/ }))

    await waitFor(() => {
      const img = document.querySelector('[data-testid="file-grid-card"] img')
      expect(img).toBeTruthy()
      expect(img!.getAttribute('src')).toBe('/rapid-admin/3/default/2/plan.jpg?v=2')
    })
  })

  it('缩略图里的「删除」走的是同一个 deleteFile', async () => {
    deleteMock.mockResolvedValue(undefined)

    renderPage()
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    // ⚠️ 2026-11-01:操作收进右上角的「⋯」菜单 ⇒ 先展开再点
    const menu = await openCardMenu(cards[0]!)
    const del = Array.from(menu.querySelectorAll('.ant-dropdown-menu-item')).find((li) =>
      (li.textContent ?? '').replace(/\s/g, '').includes('删除'),
    )
    expect(del, '菜单里必须有「删除」').toBeTruthy()
    fireEvent.click(del as HTMLElement)

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
    expect(deleteMock).toHaveBeenCalledWith('1')
  })

  it('视图模式记下来了(下次进来还是缩略图)', async () => {
    renderPage()
    await waitForRows(2)

    await switchToGrid()

    // 落盘的是"信封"格式(utils/storage.ts),值在 .value 里
    const raw = localStorage.getItem(STORAGE_KEYS.FILE_LIST_VIEW)
    expect(raw).not.toBeNull()
    expect(JSON.parse(raw!).value).toBe('grid')
  })

  it('localStorage 里已经是缩略图模式 → 首屏就是缩略图', async () => {
    storage.set(STORAGE_KEYS.FILE_LIST_VIEW, 'grid')

    renderPage()

    await waitForCards(2)
    expect(document.querySelectorAll('.ant-table-tbody tr.ant-table-row')).toHaveLength(0)
  })

  it('缩略图视图也有分页器,翻页会重新拉数据', async () => {
    listMock.mockResolvedValue({
      list: [fileRow({ id: '1', name: 'plan.jpg', url: '/rapid-admin/3/default/2/plan.jpg' })],
      total: 40,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)

    await switchToGrid()
    await waitForCards(1)

    // 表格那份分页器随 <Table> 一起没了 —— 缩略图视图自己渲染一个
    const page2 = document.querySelector('.ant-pagination-item[title="2"]') as HTMLElement
    expect(page2, '缩略图视图必须自己有分页器').toBeTruthy()
    fireEvent.click(page2)

    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(expect.objectContaining({ page: 2 })),
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
    expect(document.querySelector('[data-testid="file-grid"]')).toBeNull()
  })

  // ── 样式契约(2026-10-31 用户报的三条)─────────────────────────────

  /**
   * 「封面」徽标必须**贴在卡片左上角**,而不是占一行文字流。
   *
   * ⚠️ **为什么这条是"高度一致"的保证**:徽标原来在卡片体的**直接子节点**里,
   *   于是"是封面"的文件比别的文件多一行 ⇒ 卡片高度参差不齐(用户报的原话:
   *   「这样就避免每个 card 的高度不一样」)。
   *   jsdom 量不出高度,所以这里锁**结构**:徽标绝对定位在缩略图盒子里,
   *   且**有封面 / 没封面的卡片体子节点数相同** ⇒ 流里没有多出来的东西。
   *
   * ⚠️ 2026-11-01:徽标从**右上角挪到左上角** —— 右上角让给了「⋯」操作按钮。
   */
  it('「封面」徽标贴在卡片左上角(绝对定位),不占文档流 ⇒ 卡片高度一致', async () => {
    listMock.mockResolvedValue({
      list: [
        fileRow({ id: '1', name: 'plan.jpg', url: '/rapid-admin/3/default/2/plan.jpg', isCover: true }),
        fileRow({ id: '2', name: 'notes.txt', contentType: 'text/plain' }),
      ],
      total: 2,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(2)

    await switchToGrid()
    const cards = await waitForCards(2)

    const badge = cards[0]!.querySelector('[data-testid="file-grid-cover"]')
    expect(badge, '是封面的文件必须有徽标').toBeTruthy()
    // ⚠️ 绝对定位 ⇒ 不进文档流 ⇒ 撑不高卡片
    expect((badge as HTMLElement).style.position).toBe('absolute')
    // ⚠️ 2026-11-01:在**左上角**(右上角是「⋯」操作按钮的位置)
    expect((badge as HTMLElement).style.left).toBe('0px')
    expect((badge as HTMLElement).style.right, '徽标不该再占右上角').toBe('')
    // ⚠️ 锚点是缩略图盒子,不是文字流里的独立一行
    expect(badge!.parentElement).toBe(cards[0]!.querySelector('[data-testid="file-grid-thumb"]'))
    // 没封面的那张不许有徽标
    expect(cards[1]!.querySelector('[data-testid="file-grid-cover"]')).toBeNull()

    // ⚠️ **关键不变式**:有封面 / 没封面,卡片体的直接子节点数**一样**
    const bodyChildCount = (c: HTMLElement) => c.querySelector('.ant-card-body')!.children.length
    expect(bodyChildCount(cards[0]!)).toBe(bodyChildCount(cards[1]!))
  })

  /**
   * 卡片上的操作**收进右上角的「⋯」菜单**(2026-11-01 用户提的:
   * 「下面的 RowActions 放到右上角作为一个下来的 menu,鼠标移入展开,
   * 把这些 action 从上到下展开」)。
   *
   * ⚠️ **为什么这条不能只断言"有个按钮"**:操作一旦被挪进菜单,
   *   「点得到吗 / 顺序对吗 / 还是同一份动作吗」才是会坏的地方。
   *
   * **可逆探针**:把 `CardActionsMenu` 换回卡片底部的 `<RowActions>` ⇒ 本条红。
   */
  it('操作收进右上角的「⋯」菜单:悬浮展开,动作从上到下排开', async () => {
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
    // ⚠️ 它就压在缩略图上 ⇒ 绝对定位,不占文档流
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
    const menu = await openCardMenu(cards[0]!)

    // ⚠️ **从上到下**:菜单项的顺序就是 actionsFor 的顺序
    const labels = Array.from(menu.querySelectorAll('.ant-dropdown-menu-item')).map((li) =>
      (li.textContent ?? '').replace(/\s/g, ''),
    )
    expect(labels.length, '菜单里要有全部动作').toBeGreaterThan(1)
    expect(labels[0]).toContain('详情')
    expect(labels.some((l) => l.includes('下载'))).toBe(true)
    expect(labels.some((l) => l.includes('设为封面'))).toBe(true)
    expect(labels.some((l) => l.includes('复制直链'))).toBe(true)
    expect(labels[labels.length - 1]).toContain('删除')
  })

  /**
   * **按文件格式给图标**(用户 2026-10-31:「要区分 pdf、word、excel、zip 等等
   * 文件格式作为文件图标」)。
   *
   * ⚠️ 判据是**文件名里的扩展名**,不是 content_type ——
   *   docx 的 MIME 是 80 个字符的
   *   `application/vnd.openxmlformats-officedocument.wordprocessingml.document`,
   *   而扩展名("xlsx")既短又是用户认得的那个。
   *
   *   反断言也有意义:**光看 content_type 区分不出 doc 和 docx**
   *   (两个不同的 MIME),按扩展名才能落到同一个"Word"图形上。
   */
  it('按文件格式给图标(pdf / word / excel / zip 各不相同)', async () => {
    listMock.mockResolvedValue({
      list: [
        fileRow({ id: '1', name: 'a.pdf', contentType: 'application/pdf' }),
        fileRow({
          id: '2',
          name: 'b.docx',
          contentType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
        }),
        fileRow({
          id: '3',
          name: 'c.xlsx',
          contentType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
        }),
        fileRow({ id: '4', name: 'd.zip', contentType: 'application/zip' }),
      ],
      total: 4,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(4)

    await switchToGrid()
    const cards = await waitForCards(4)

    expect(cards[0]!.querySelector('.anticon-file-pdf')).toBeTruthy()
    expect(cards[1]!.querySelector('.anticon-file-word')).toBeTruthy()
    expect(cards[2]!.querySelector('.anticon-file-excel')).toBeTruthy()
    expect(cards[3]!.querySelector('.anticon-file-zip')).toBeTruthy()

    // 副标题摆的是扩展名(不是那串 80 字符的 content_type)
    expect(cards[0]!.textContent).toContain('PDF')
    expect(cards[2]!.textContent).toContain('XLSX')
    expect(cards[0]!.textContent).not.toContain('application/pdf')
  })

  it('认不出的格式 → 通用文件图标(不能空着)', async () => {
    listMock.mockResolvedValue({
      list: [fileRow({ id: '1', name: 'a.qqq', contentType: 'application/octet-stream' })],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)

    await switchToGrid()
    const cards = await waitForCards(1)

    expect(cards[0]!.querySelector('.anticon-file-unknown')).toBeTruthy()
  })
})

/**
 * M141 —— 音视频时长 + 视频封面
 *
 * ## 这一段守的是什么
 *
 * 用户 2026-10-31 提的:「上传文件时候如果是视频文件,都调用环境变量里面默认的
 * ffmpeg 工具提取视频时长,截取封面,这样 grid 里面就可以显示视频封面,
 * 同时显示视频时长;如果上传的文件是音频格式的,封面就用音频图标,
 * 同时也要提取显示音频时长。」
 *
 * | 事 | 前端的判据 | 少了会怎样 |
 * |---|---|---|
 * | 视频显示封面 | `cover_url` 在不在 | 缩略图是裂图 / 摆个没意义的图标 |
 * | 音频用音频图标 | 扩展名(已有 `EXT_ICONS`) | 音频和 pdf 长得一样 |
 * | 媒体显示时长 | `duration_ms > 0` | 显示 "00:00",用户以为文件是空的 |
 *
 * ## ⚠️ **前端不判"是不是视频"**
 *
 * 判据就是 **`cover_url` 在不在** —— 前端再抄一份扩展名表,迟早跟后端那份
 * 对不上(后端还多认一堆冷门格式)。这条网守的正是这一点:
 * **有 `cover_url` 就渲染 `<img>`,哪怕它是个 .mkv**。
 */
describe('文件列表页 —— 音视频时长 / 视频封面(M141)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    storage.remove(STORAGE_KEYS.FILE_LIST_VIEW)
    listMock.mockResolvedValue({
      list: [],
      total: 0,
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

  it('视频:缩略图是后端抽的封面(cover_url),不是文件本体', async () => {
    listMock.mockResolvedValue({
      list: [
        fileRow({
          id: '1',
          name: '现场.mp4',
          contentType: 'video/mp4',
          // ⚠️ 文件本体的直链 —— 视频塞进 <img src> 是裂图,绝不能用它
          url: '/rapid-admin/3/default/2/现场.mp4',
          coverUrl: '/rapid-admin/3/default/2/__cover__1.jpg',
        }),
      ],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)
    await switchToGrid()
    const cards = await waitForCards(1)

    const img = cards[0]!.querySelector('img')
    expect(img, '有 cover_url 就必须渲染 <img>').toBeTruthy()
    expect(img!.getAttribute('src')).toBe('/rapid-admin/3/default/2/__cover__1.jpg')
    expect(img!.getAttribute('src')).not.toContain('.mp4')
  })

  it('视频但没探到封面(没装 ffmpeg)→ 摆视频图标,不画裂图', async () => {
    listMock.mockResolvedValue({
      list: [fileRow({ id: '1', name: '现场.mp4', contentType: 'video/mp4' })],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)
    await switchToGrid()
    const cards = await waitForCards(1)

    expect(cards[0]!.querySelector('img')).toBeNull()
    expect(cards[0]!.querySelector('.anticon-video-camera')).toBeTruthy()
  })

  it('音频:用音频图标(不是封面,音频没有封面)', async () => {
    listMock.mockResolvedValue({
      list: [fileRow({ id: '1', name: '会议.mp3', contentType: 'audio/mpeg' })],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)
    await switchToGrid()
    const cards = await waitForCards(1)

    expect(cards[0]!.querySelector('img'), '音频不该有封面图').toBeNull()
    expect(cards[0]!.querySelector('.anticon-sound'), '音频必须摆音频图标').toBeTruthy()
  })

  it('探到时长 → 卡片左下角有时长徽标', async () => {
    listMock.mockResolvedValue({
      list: [
        fileRow({
          id: '1',
          name: '现场.mp4',
          contentType: 'video/mp4',
          coverUrl: '/rapid-admin/3/default/2/__cover__1.jpg',
          durationMs: 205_000,
        }),
      ],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)
    await switchToGrid()
    const cards = await waitForCards(1)

    const badge = cards[0]!.querySelector('[data-testid="file-grid-duration"]')
    expect(badge, '探到时长的文件必须有徽标').toBeTruthy()
    expect(badge!.textContent).toBe('3:25')
  })

  it('没探到时长(0)→ **不显示**徽标(不是 "00:00")', async () => {
    listMock.mockResolvedValue({
      list: [fileRow({ id: '1', name: 'plan.jpg', url: '/rapid-admin/3/default/2/plan.jpg' })],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)
    await switchToGrid()
    const cards = await waitForCards(1)

    expect(
      cards[0]!.querySelector('[data-testid="file-grid-duration"]'),
      'duration_ms=0 是"不知道",不是"零秒" ⇒ 不该显示徽标',
    ).toBeNull()
    expect(cards[0]!.textContent).not.toContain('00:00')
  })

  it('表格视图里有一列「时长」,媒体显示 mm:ss,别的显示 "-"', async () => {
    listMock.mockResolvedValue({
      list: [
        fileRow({ id: '1', name: '现场.mp4', contentType: 'video/mp4', durationMs: 3_723_000 }),
        fileRow({ id: '2', name: 'plan.jpg', url: '/rapid-admin/3/default/2/plan.jpg' }),
      ],
      total: 2,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    const rows = await waitForRows(2)

    // 表头里有「时长」这一列 —— ⚠️ **按列号定位**,不要满行找 "-"
    //   (行里到处都是 "-",那断言等于没断言)
    const headCells = Array.from(document.querySelectorAll('.ant-table-thead th'))
    const durationCol = headCells.findIndex(
      (h) => (h.textContent ?? '').replace(/\s/g, '') === '时长',
    )
    expect(durationCol, '表格里必须有「时长」这一列').toBeGreaterThan(-1)

    const cellText = (row: HTMLElement) =>
      (row.querySelectorAll('td')[durationCol]?.textContent ?? '').trim()

    expect(cellText(rows[0]!)).toBe('1:02:03')
    // ⚠️ **不是媒体 ⇒ "-"** —— 显示成 00:00 会让用户以为文件是空的
    expect(cellText(rows[1]!)).toBe('-')
  })
})

/**
 * M145d —— 媒体「浏览器能不能播」徽标
 *
 * ## 这一段守的是什么
 *
 * 后端 `pkg/media` 判完"这个文件浏览器播不了"之后,前端**必须把它说出来**。
 * 用户传完一个 WMV,列表里跟别的文件长得一模一样,点开才发现播不了 ——
 * 那是"平台上传视频有问题"的观感,而**没有报错、没有提示**,最坏的一种状态。
 *
 * | 事 | 前端的判据 | 少了会怎样 |
 * |---|---|---|
 * | 标"浏览器不支持" | 后端给的 `media_reason` | 用户点开才发现播不了 |
 * | 标"转码中/已转码" | 后端给的 `transcode_status` | 用户以为传完就好了 |
 * | **转码完成 ⇒ 不许再标"不支持"** | 状态顺序 | 转码成功了还说播不了 = 骗用户 |
 *
 * ## ⚠️ **两个视图必须共用同一个徽标**
 *
 * 表格视图挂在文件名那一格(扫文件名时最该知道的事),
 * 缩略图视图挂在缩略图盒子的**右下角**(2026-11-01:左上角是「封面」徽标、
 * 右上角是「⋯」操作按钮、左下角是时长徽标 —— 四个各占一角)。
 * 两处**是同一个组件** —— 同一件事两套说法,改一处忘另一处是这类 bug 的开始。
 */
describe('文件列表页 —— 媒体「浏览器能不能播」徽标(M145d)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    storage.remove(STORAGE_KEYS.FILE_LIST_VIEW)
    listMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 20 } as never)
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

  it('表格视图:浏览器播不了的文件,文件名那格有「浏览器不支持」徽标', async () => {
    listMock.mockResolvedValue({
      list: [
        fileRow({
          id: '1',
          name: '老录像.wmv',
          contentType: 'video/x-ms-wmv',
          mediaReason: '视频编码 WMV 浏览器不支持',
          mediaContainer: 'asf',
          mediaVideoCodec: 'wmv3',
        }),
        fileRow({ id: '2', name: 'plan.jpg', url: '/rapid-admin/3/default/2/plan.jpg' }),
      ],
      total: 2,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    const rows = await waitForRows(2)

    // ⚠️ 挂在**文件名那一格** —— 别满行找,那一行到处是文字
    const nameCell = rows[0]!.querySelectorAll('td')[0]!
    const badge = nameCell.querySelector('[data-testid="file-media-status-unplayable"]')
    expect(badge, '播不了的文件必须有徽标').toBeTruthy()
    expect(badge!.textContent).toBe('浏览器不支持')

    // 能播的那个不许有徽标
    const okCell = rows[1]!.querySelectorAll('td')[0]!
    expect(okCell.querySelector('[data-testid^="file-media-status-"]')).toBeNull()
  })

  it('缩略图视图:同一个徽标贴在缩略图盒子的右下角', async () => {
    listMock.mockResolvedValue({
      list: [
        fileRow({
          id: '1',
          name: '老录像.wmv',
          contentType: 'video/x-ms-wmv',
          mediaReason: '视频编码 WMV 浏览器不支持',
        }),
      ],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    await waitForRows(1)
    await switchToGrid()
    const cards = await waitForCards(1)

    const badge = cards[0]!.querySelector('[data-testid="file-media-status-unplayable"]')
    expect(badge, '缩略图视图也必须有徽标').toBeTruthy()
    // ⚠️ 绝对定位 ⇒ 不进文档流 ⇒ 不会撑高卡片(与「封面」徽标同一个道理)
    const holder = (badge as HTMLElement).closest('div') as HTMLElement
    expect(holder.style.position).toBe('absolute')
    // ⚠️ 2026-11-01:挪到**右下角**(左上角是「封面」徽标、右上角是「⋯」按钮)
    expect(holder.style.bottom).toBe('0px')
    expect(holder.style.right).toBe('0px')
  })

  it('转码完成 ⇒ 显示「已转码」,**不是**「浏览器不支持」', async () => {
    listMock.mockResolvedValue({
      list: [
        fileRow({
          id: '1',
          name: '老录像.wmv',
          contentType: 'video/x-ms-wmv',
          // ⚠️ media_reason 还在 —— 那是**原文件**的判据;转码完成后
          //   文件已经能播了,还标"不支持"就是在骗用户。
          mediaReason: '视频编码 WMV 浏览器不支持',
          transcodeStatus: 3,
        }),
      ],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    const rows = await waitForRows(1)

    const nameCell = rows[0]!.querySelectorAll('td')[0]!
    expect(nameCell.querySelector('[data-testid="file-media-status-unplayable"]')).toBeNull()
    const done = nameCell.querySelector('[data-testid="file-media-status-done"]')
    expect(done, '转码完成要标「已转码」').toBeTruthy()
    expect(done!.textContent).toBe('已转码')
  })

  it('转码中 ⇒ 显示「转码中」', async () => {
    listMock.mockResolvedValue({
      list: [
        fileRow({
          id: '1',
          name: '老录像.wmv',
          contentType: 'video/x-ms-wmv',
          mediaReason: '视频编码 WMV 浏览器不支持',
          transcodeStatus: 2,
        }),
      ],
      total: 1,
      page: 1,
      size: 20,
    } as never)

    renderPage()
    const rows = await waitForRows(1)

    const nameCell = rows[0]!.querySelectorAll('td')[0]!
    const running = nameCell.querySelector('[data-testid="file-media-status-running"]')
    expect(running, '转码中要标「转码中」').toBeTruthy()
    expect(running!.textContent).toBe('转码中')
  })
})
