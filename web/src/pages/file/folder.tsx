/**
 * 文件夹管理 —— M82(平铺列表)/ M125(多级文件夹 + 封面)/ M139(用户层)
 *
 * 框架页。文件系统的目录形态是 `rapid-admin/{uid}/{biz}/{fid}/`
 * (设计文档 §3.4)—— **第一段就是用户**。所以这一页照着它分层:
 *
 *   用户层(谁有文件夹) → 该用户的文件夹 → 子文件夹
 *
 * ## 为什么多了一层"用户"(M139)
 *
 * 以前这一页把"调用者自己的根级文件夹"直接当成最外层,于是:
 *
 *   · 超管**根本列不出别人的文件夹** —— 后端 `FolderService.List` 写死了
 *     `user_id = 调用者`(而设计文档 §10.2 明说超管可跨用户**读**);
 *   · 界面上没有任何地方说明"这一行属于谁"。
 *
 * M139 起:
 *
 *   · `GET /biz/folders/users` —— 列出"有文件夹的用户"(用户层),
 *     顺带带回 `cross_user` / `current_user_id` 两个标志位;
 *   · `GET /biz/folders?user_id=` —— 按用户过滤(只有超管能传别人的 id);
 *   · URL 用 `?user_id=` 记着当前在谁的空间,`?folder_id=` 记着当前文件夹。
 *
 * ## 别人的空间只能看
 *
 * 设计文档 §10.2:超管可跨用户**读**,不能跨用户**写**。
 * 前端在别人的空间里把「新建 / 删除」藏起来(那只是体验),
 * 真正的拦截在后端(`loadFolderForWrite` → `callerOwns`,返 404)。
 *
 * ## 刻意不做的事
 *
 *   · **重命名 / 移动**:后端没有这两个接口。页面上的每个按钮都必须对应一个
 *     真实存在的后端动作 —— 前端先画一个按钮,后端再补接口,中间那段时间
 *     点下去是 404。
 *   · **拖拽移动**:同上,而且它要改的是"改一个文件夹的 parent_id"这种
 *     容易漏掉唯一索引的事,值得单开一轮。
 */
import type React from 'react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { App, Breadcrumb, Empty, Image, Pagination, Radio, Space, Spin, Tag, Tooltip } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  AppstoreOutlined,
  FolderOpenOutlined,
  FolderOutlined,
  InboxOutlined,
  PictureOutlined,
  PlusOutlined,
  TableOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { ManagementTable } from '@/components/common/ManagementTable'
import { enterOnRow, isInteractiveTarget } from '@/components/common/rowClick'
import { EditModal, type EditField } from '@/components/common/EditModal'
import { ViewDrawer, type ViewField } from '@/components/common/ViewDrawer'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { RowActions, type RowAction } from '@/components/common/RowActions'
import { TableErrorState } from '@/components/common/TableErrorState'
import { AuthButton } from '@/components/auth/AuthButton'
import { useTable } from '@/hooks/useTable'
import { useI18n } from '@/locales'
import { PermFile } from '@/constants/permissions'
import {
  listFolders,
  listFolderUsers,
  createFolder,
  deleteFolder,
  getFolder,
  listFiles,
  deleteFile,
  downloadFile,
  type FileView,
  type FolderDetail,
  type FolderPageResult,
  type FolderUserPageResult,
  type FolderUserView,
  type FolderView,
} from '@/api/upload'
import { storage } from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'
import type { PageRequest } from '@/types/api'
import { FolderGrid } from './FolderGrid'
import { FileGrid } from './FileGrid'
import { MediaStatusTag } from './MediaStatusTag'
import { parentPathOf } from './folderPath'

/**
 * 查询条件
 *
 * ⚠️ **parentId 是必填的 string** —— 后端是三态:
 *   · 不传 = 不限层级(老调用方 / 下拉框);
 *   · "0" = 只列根级;
 *   · id = 只列它的直接子级。
 *   用 string 而不是 number 是因为后端 DTO 是 `json:"parent_id,string"`,
 *   传数字会被 Go 的 encoding/json 拒掉。
 *
 * ⚠️ **userId 不传 = 列调用方自己的**(后端三态,和 parentId 一个套路)。
 *   只有超管能传别人的 id —— 后端对普通用户返 403。
 */
type FolderQuery = PageRequest & { biz?: string; keyword?: string; parentId: string; userId?: string; size: number }

/** 用户层的查询条件 */
type UserQuery = PageRequest & { keyword?: string; size: number }

/** 新建文件夹的上下文(挂在哪个父级下) */
interface CreateTarget {
  /** 父级 id;"0" = 根级 */
  parentId: string
  /** 父级的路径,用来在弹窗里告诉用户"会建在哪" */
  parentPath: string
  /** true = 继承上级的业务段(建子文件夹时)—— 这时**不显示 biz 输入框** */
  inheritBiz: boolean
}

const formatSize = (bytes: number) => {
  if (!bytes || bytes < 0) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

const formatTime = (v: string) => (v ? v.slice(0, 19).replace('T', ' ') : '-')

/**
 * 一次最多并入列表的文件数。
 *
 * ⚠️ **后端 PageResult 的 MaxSize 是 100** —— 传再大也被静默截断
 *   (M107 审计踩过:page_size:1000 实际只回 100)。文件超过 100 个时
 *   列表尾部会有"查看全部"的提示,兜底去文件列表页。
 */
const FILES_LOAD_SIZE = 100

/**
 * 混合列表的一行(2026-11-03 用户验收反馈:M179 的"上面文件夹表 + 下面文件区"
 * 两个列表合成**一个**,像 Windows 资源管理器:先子文件夹后文件)。
 *
 * 判别字段 `kind` 决定:名称列的图标、中间列显示什么、操作列出哪些动作、
 * 点行是"进入"还是"看详情" —— 所有按行分支的地方都只认这一个字段,
 * 不写"有没有 file_size 这种字段"之类的形状判断(文件夹以后加了
 * 同名字段那种判断会静默走错分支)。
 */
type MixedRow =
  | { kind: 'folder'; folder: FolderView }
  | { kind: 'file'; file: FileView }

/** 行模型收窄:is-folder / is-file,列渲染里按行分支用 */
const isFolderRow = (row: MixedRow): row is Extract<MixedRow, { kind: 'folder' }> =>
  row.kind === 'folder'

/**
 * 父级路径 —— **与卡片视图同一个函数**(folderPath.ts)。
 */

const FolderList: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  /** 当前所在的用户空间 id(空 = 还没进任何人的空间) */
  const userId = searchParams.get('user_id') ?? ''
  /** 当前所在文件夹 id(空 = 该用户的根级) */
  const folderId = searchParams.get('folder_id') ?? ''
  /** 传给后端的层级过滤值 */
  const parentId = folderId || '0'

  // ===== 用户层 =====

  /**
   * 用户层的表格 —— 同时也是**层级判定的唯一依据**
   *
   * ⚠️ **能不能跨用户是后端说了算** —— `cross_user` / `current_user_id`
   *   来自 `GET /biz/folders/users` 的响应。
   *
   * ⚠️ **前端不自己猜角色** —— 前端那份 role code 和后端对不上过
   *   (M103 审计:超管切身份免短信验证那个安全洞就是这么来的)。
   */
  const usersTable = useTable<UserQuery, FolderUserView, FolderUserPageResult>({
    initialQuery: { keyword: '' },
    fetch: (q) => listFolderUsers({ keyword: q.keyword, page: q.page, size: q.size }),
  })

  /**
   * ⚠️ **层级判定的结果没到之前不渲染任何一层** ——
   *
   *   超管第一次打开这一页时,如果先渲染"文件夹层",会先闪一下**他自己的**
   *   文件夹列表,然后才跳到用户层 —— 用户会以为点错了。而且那一下闪出来的
   *   「新建」按钮是可点的(点了会建到超管自己名下,不是当前看到的那一层)。
   *
   *   所以判定依据没到就转圈。
   */
  const flagsReady = usersTable.raw !== null || usersTable.error !== null

  const crossUser = usersTable.raw?.cross_user === true
  const currentUserId = usersTable.raw?.current_user_id ?? ''
  /** 用户层可见 = 调用者能跨用户,且当前没进任何人的空间 */
  const showUserLayer = crossUser && !userId
  /** 当前空间能不能写 —— 别人的空间只能读(设计文档 §10.2) */
  const canWrite = !crossUser || (!!userId && userId === currentUserId)

  // ===== 文件夹层 =====
  const foldersTable = useTable<FolderQuery, FolderView, FolderPageResult>({
    // ⚠️ **首屏就用 URL 里的层级** —— 初始值只在挂载时取一次,
    //   深链进来时必须是"直接进那个文件夹",不能先列一遍根级再跳。
    initialQuery: { parentId, userId: userId || undefined },
    fetch: (q): Promise<FolderPageResult> =>
      listFolders({
        biz: q.biz,
        keyword: q.keyword,
        /**
         * ⚠️ **有关键字时不限层级** —— 根级搜"图纸"要能搜到 "A/图纸"。
         *   死守当前层级的话,多级之后根级搜索就是个摆设(子文件夹永远搜不到),
         *   而用户看不出为什么。
         */
        parentId: q.keyword?.trim() ? undefined : q.parentId,
        userId: q.userId,
        page: q.page,
        size: q.size,
      }),
  })

  /**
   * 当前文件夹**直接包含**的文件 —— 混合列表的后半段(先子文件夹后文件)。
   *
   * ⚠️ **为什么不用 useTable**:文件不是本列表的分页主角 —— 文件夹才是
   *   (服务端分页在它身上)。文件一次拉全(≤ FILES_LOAD_SIZE),这里
   *   只需要"列表 + 总数 + loading"三个数,拉一套 useTable 反而要把它的
   *   分页器藏起来再绕过它的查询状态。
   */
  const [filesState, setFilesState] = useState<{ loading: boolean; list: FileView[]; total: number }>({
    loading: false,
    list: [],
    total: 0,
  })
  const [viewingFile, setViewingFile] = useState<FileView | null>(null)
  const [deletingFile, setDeletingFile] = useState<FileView | null>(null)

  /**
   * **文件行什么时候进列表**(分页方案,2026-11-03 用户验收反馈定的):
   *
   *   · 只在**当前文件夹层**(folderId 非空)—— 根级没有"直接包含的文件";
   *   · 只在**文件夹第 1 页** —— 翻页是"浏览更多子文件夹",同一批文件
   *     跟着每一页重复显示,用户会以为列表坏了;第一页合并保持
   *     "点进文件夹第一眼看到全部内容"(资源管理器语义);
   *   · **有关键字时不并** —— 搜索列的是全部层级的文件夹,此时再插进
   *     "当前文件夹的文件"口径就乱了。
   *
   *   兜底:统计条常驻"文件夹 X 个 · 文件 Y 个"(showTotal),翻页 / 搜索
   *   时文件总数照样看得见;文件超过一次拉取上限时另有"查看全部"入口。
   */
  const foldersPage = foldersTable.query.page ?? 1
  const foldersKeyword = foldersTable.query.keyword?.trim() ?? ''
  const showFiles = !!folderId && !foldersKeyword && foldersPage === 1

  /**
   * ⚠️ **列表请求用自增序号防竞态** —— 快速切换文件夹 / 翻页时,旧请求
   *   后到会把新文件夹的文件盖上来(请求生命周期纪律,与列表页同一套)。
   */
  const filesSeqRef = useRef(0)
  const fetchFiles = useCallback(async () => {
    const seq = ++filesSeqRef.current
    if (!folderId) {
      setFilesState({ loading: false, list: [], total: 0 })
      return
    }
    setFilesState((s) => ({ ...s, loading: true }))
    try {
      const r = await listFiles({ folderId, page: 1, size: FILES_LOAD_SIZE })
      if (seq === filesSeqRef.current) {
        setFilesState({ loading: false, list: r.list ?? [], total: r.total ?? 0 })
      }
    } catch {
      // ⚠️ 失败**不打断**文件夹列表(主列表照常);拦截器已弹过 toast,
      //   这里不再弹第二个(纪律:一次失败一个 toast)。文件行缺席 +
      //   重新触发入口(换页 / 换层 / 删完文件重拉)都还在。
      if (seq === filesSeqRef.current) setFilesState({ loading: false, list: [], total: 0 })
    }
  }, [folderId])

  useEffect(() => {
    // 换了文件夹:旧文件夹的文件行**立刻**清掉 —— 不然请求在飞的那几百毫秒里,
    // 列表会先闪一帧上一个文件夹的文件(比"短暂空白"糟得多)。
    setFilesState((s) => (s.list.length ? { ...s, list: [] } : s))
  }, [folderId])

  useEffect(() => {
    if (!showFiles) {
      // 翻页 / 搜索时文件行不进列表 —— 行清掉,但 **total 要留住**:
      // 统计条("文件夹 X 个 · 文件 Y 个")在每一页都得说得出文件总数。
      setFilesState((s) => (s.list.length ? { loading: false, list: [], total: s.total } : s))
      return
    }
    fetchFiles()
  }, [showFiles, fetchFiles])

  /** 文件增删之后重拉本层文件(删除文件夹不影响它 —— 删的是子树的文件) */
  const reloadFiles = useCallback(() => {
    if (folderId) fetchFiles()
  }, [fetchFiles, folderId])

  /**
   * 文件行的操作 —— **与文件列表页(list.tsx)同一套动作、同一批 perm 码**,
   * 零新码:详情 / 下载 = `file:read`,删除 = `file:delete`。
   *
   * ⚠️ 只读空间(别人的,设计文档 §10.2)不渲染「删除」—— 真闸在后端,
   *   前端藏按钮只是体验。
   */
  const fileActionsFor = useCallback(
    (_file: FileView): RowAction<FileView>[] => {
      const actions: RowAction<FileView>[] = [
        {
          key: 'view',
          label: t('common.detail'),
          perm: PermFile.READ,
          onClick: setViewingFile,
        },
        {
          key: 'download',
          label: t('page.fileList.download'),
          perm: PermFile.READ,
          onClick: async (f) => {
            try {
              await downloadFile(f.id, f.file_name)
            } catch (e) {
              message.error((e as Error).message)
            }
          },
        },
      ]
      if (canWrite) {
        actions.push({
          key: 'delete',
          label: t('common.delete'),
          perm: PermFile.DELETE,
          danger: true,
          onClick: setDeletingFile,
        })
      }
      return actions
    },
    [canWrite, message, t],
  )

  /** 当前文件夹的详情(面包屑的祖先链从这儿来) */
  const [detail, setDetail] = useState<FolderDetail | null>(null)
  const [creating, setCreating] = useState<CreateTarget | null>(null)
  const [viewing, setViewing] = useState<FolderView | null>(null)
  const [deleting, setDeleting] = useState<FolderView | null>(null)

  /**
   * **视图模式**(表格 / 卡片)—— 记在 localStorage 里(M144)。
   *
   * ⚠️ **为什么文件夹也需要卡片视图**:表格里只能看到
   *   "名字 / 文件数 / 大小",**看不到这个文件夹长什么样**。而文件夹是
   *   **有封面的**(M125 起「设为封面」),封面恰恰是最直观的识别方式 ——
   *   一屏扫过去就知道"哪个是图纸、哪个是照片、哪个是空的"。
   *
   * ⚠️ **跟文件列表的视图设置分开记** —— 共用一个 key 的话,在文件列表
   *   切到缩略图,进文件夹页也会被带着变。两个页面各管各的。
   */
  const [viewMode, setViewMode] = useState<'table' | 'grid'>(() =>
    storage.get<string>(STORAGE_KEYS.FILE_FOLDER_VIEW) === 'grid' ? 'grid' : 'table',
  )

  const changeViewMode = useCallback((mode: 'table' | 'grid') => {
    setViewMode(mode)
    storage.set(STORAGE_KEYS.FILE_FOLDER_VIEW, mode)
  }, [])

  /**
   * URL 变了 → 查询条件跟着变
   *
   * ⚠️ **只在真的不一样时才 setQuery** —— 否则每次 render 都产生一个新 query
   *   对象,useTable 的 effect 会被无限触发(发请求 → setState → render → …)。
   *
   * ⚠️ **依赖里只能放 setQuery(它是稳定的)** —— `foldersTable` 这个对象
   *   每次 render 都是新的,放进去等于每次 render 都跑一遍这个 effect。
   */
  const { setQuery: setFoldersQuery } = foldersTable
  useEffect(() => {
    const nextUserId = userId || undefined
    setFoldersQuery((prev) =>
      prev.parentId === parentId && prev.userId === nextUserId
        ? prev
        : { ...prev, parentId, userId: nextUserId, page: 1 },
    )
  }, [parentId, userId, setFoldersQuery])

  /**
   * 拉当前文件夹的详情 —— 面包屑的祖先链只能从后端要
   *
   * ⚠️ **文件夹可能已经不存在了**(被别人删了 / 链接是旧的)——
   *   这时回根级,别把用户卡在一个什么都列不出来的空壳页面上。
   */
  useEffect(() => {
    if (!folderId) {
      setDetail(null)
      return
    }
    let cancelled = false
    getFolder(folderId)
      .then((d) => {
        if (!cancelled) setDetail(d)
      })
      .catch(() => {
        if (cancelled) return
        setDetail(null)
        // ⚠️ **退回"当前用户的根级",不是退回用户层** ——
        //   当前用户是谁还在 URL 里,别把用户踢回最外层。
        setSearchParams(userId ? { user_id: userId } : {}, { replace: true })
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [folderId])

  /** 进入某个用户的空间(空 = 回用户层) */
  const goToUser = useCallback(
    (id: string) => {
      setSearchParams(id ? { user_id: id } : {}, { replace: false })
    },
    [setSearchParams],
  )

  /** 进入某个文件夹(空 = 回该用户的根级) */
  const goToFolder = useCallback(
    (id: string) => {
      const next: Record<string, string> = {}
      // ⚠️ **user_id 要一起带上** —— 面包屑点回上一级时不能掉回用户层。
      if (userId) next.user_id = userId
      if (id) next.folder_id = id
      setSearchParams(next, { replace: false })
    },
    [setSearchParams, userId],
  )

  /** 当前空间的归属人展示名(后端算好的;空 = 退回显示 id) */
  const ownerName = foldersTable.raw?.owner_name || ''
  const ownerLabel = ownerName || currentUserId || userId || t('page.fileFolder.mySpace')

  const crumbs = useMemo(() => {
    const items: { title: React.ReactNode }[] = []
    // ⚠️ **超管才看得到"用户层"这一级** —— 普通用户的空间就是自己的,
    //   多这一级是噪音。
    if (crossUser) {
      items.push({
        title: <a onClick={() => goToUser('')}>{t('page.fileFolder.userLayer')}</a>,
      })
    }
    items.push({
      title: <a onClick={() => goToFolder('')}>{t('page.fileFolder.rootDir')}</a>,
    })
    for (const a of detail?.ancestors ?? []) {
      items.push({
        title: <a onClick={() => goToFolder(a.id)}>{a.name}</a>,
      })
    }
    if (detail) items.push({ title: detail.name })
    return items
  }, [crossUser, detail, goToFolder, goToUser, t])

  // ===== 用户层的表格 =====

  const userColumns: ColumnsType<FolderUserView> = [
    {
      title: t('field.user'),
      dataIndex: 'display_name',
      width: 260,
      render: (v: string, row: FolderUserView) => (
        <Space size={6}>
          <UserOutlined />
          {/* ⚠️ **名字是链接** —— 点它就是"进入这个用户的文件夹空间" */}
          <a onClick={() => goToUser(row.user_id)}>{v}</a>
          {/* 登录名:同名的人靠它区分 */}
          {row.username && row.username !== v ? (
            <Tooltip title={row.username}>
              <Tag color="default">{row.username}</Tag>
            </Tooltip>
          ) : null}
        </Space>
      ),
    },
    {
      title: t('field.folderCount'),
      dataIndex: 'folder_count',
      width: 110,
      render: (v: number) => (v > 0 ? <Tag color="blue">{v}</Tag> : 0),
    },
    { title: t('field.fileCount'), dataIndex: 'file_count', width: 100, render: (v: number) => v ?? 0 },
    {
      title: t('field.totalSize'),
      dataIndex: 'total_size',
      width: 120,
      render: (v: number) => formatSize(v),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 160,
      fixed: 'right',
      render: (_: unknown, row: FolderUserView) => {
        const actions: RowAction<FolderUserView>[] = [
          {
            key: 'enter',
            label: t('page.fileFolder.enterSpace'),
            perm: PermFile.FOLDER_LIST,
            onClick: (r) => goToUser(r.user_id),
          },
        ]
        return <RowActions<FolderUserView> row={row} actions={actions} />
      },
    },
  ]

  // ===== 文件夹层的表格 =====

  /**
   * 每行的操作按钮 —— **表格视图和卡片视图共用这一份**(M144)。
   *
   * ⚠️ 之前这段是写在列的 `render` 里的,卡片视图拿不到。
   *   两处各写一份的话,权限码 / 禁用条件迟早对不上 ——
   *   表格里"删除"能点、卡片里点不了,用户看着就是 bug。
   */
  const actionsFor = useMemo(
    () =>
      (row: FolderView): RowAction<FolderView>[] => {
        const actions: RowAction<FolderView>[] = [
          {
            key: 'enter',
            label: t('page.fileFolder.enter'),
            perm: PermFile.FOLDER_LIST,
            onClick: (r) => goToFolder(r.id),
          },
          {
            key: 'view',
            label: t('common.detail'),
            perm: PermFile.FOLDER_LIST,
            onClick: setViewing,
          },
          {
            key: 'sub',
            label: t('page.fileFolder.createSubFolder'),
            perm: PermFile.FOLDER_CREATE,
            onClick: (r) => setCreating({ parentId: r.id, parentPath: r.path, inheritBiz: true }),
          },
          {
            key: 'files',
            label: t('page.fileFolder.viewFiles'),
            perm: PermFile.LIST,
            // 带着 folder_id 跳文件列表,那边会把它吃进查询条件
            onClick: (r) => navigate(`/file/list?folder_id=${encodeURIComponent(r.id)}`),
          },
          {
            key: 'delete',
            label: t('common.delete'),
            perm: PermFile.FOLDER_DELETE,
            danger: true,
            disabled: !!row.is_default,
            disabledTip: t('page.fileFolder.defaultNotDeletable'),
            onClick: setDeleting,
          },
        ]
        // ⚠️ **别人的空间不能写** —— 设计文档 §10.2:超管可跨用户读,
        //   不能跨用户写。前端把写操作藏起来只是体验,后端 loadFolderForWrite
        //   才是真闸(它返 404,不是 403 —— 别泄露"这个 id 存在")。
        const writable = canWrite
          ? actions
          : actions.filter((a) => a.key !== 'delete' && a.key !== 'sub')
        return writable
      },
    [canWrite, goToFolder, navigate, t],
  )

  /**
   * 混合列表的列(2026-11-03:两个列表合成一个)。
   *
   * ## 列怎么取舍
   *
   * 文件夹行和文件行的"自然列"不一样(文件夹有子文件夹数 / 文件数 /
   * 总大小,文件有大小 / 类型),混进一张表后**列名是共享的、格子里
   * 各取所需**,对不上的格子画「—」:
   *
   *   · 名称:按 kind 摆图标(文件夹开口/闭口、文件 image/其他 ——
   *     跟 list.tsx 同一套判据),文件夹名是链接(进入),文件名不是
   *     (点行看详情,见 onRow);
   *   · 封面/预览:文件夹摆封面直链,文件摆缩略图直链(url / 视频封面帧,
   *     都是**稳定直链**能直接塞 `<img src>`,FileGrid 同判据);
   *   · 子文件夹 / 文件数:只有文件夹有;
   *   · 大小:文件夹是子树总大小,文件是本文件大小 —— 同一列"大小"
   *     两种口径,格子里都看得懂(文件不会有大到 GB 的目录歧义,
   *     文件夹不会跟"单文件大小"混淆,因为它的行里有子文件夹数);
   *   · 时间:文件夹是创建时间,文件是上传时间 —— 对文件来说上传即创建。
   */
  const columns: ColumnsType<MixedRow> = [
    {
      title: t('field.name'),
      dataIndex: 'name',
      width: 280,
      render: (_: unknown, row: MixedRow) => {
        if (isFolderRow(row)) {
          const f = row.folder
          const parentPath = parentPathOf(f.path)
          return (
            <Space size={6}>
              {f.child_count > 0 ? <FolderOpenOutlined /> : <FolderOutlined />}
              {/* ⚠️ 名字是链接 —— 点它就是"进入这个文件夹"(用户 2026-10-30 提的) */}
              <a onClick={() => goToFolder(f.id)}>{f.name}</a>
              {/* 所在目录:同名文件夹可能有好几个("A/图纸"、"B/图纸"),光看名字分不清 */}
              {parentPath ? (
                <Tooltip title={f.path}>
                  <Tag color="default">{parentPath}</Tag>
                </Tooltip>
              ) : null}
            </Space>
          )
        }
        const f = row.file
        return (
          <Space size={6}>
            {/* ⚠️ 图标判据与文件列表页(list.tsx)同一套:image/ 摆图片图标 */}
            {(f.content_type || '').toLowerCase().startsWith('image/') ? (
              <PictureOutlined />
            ) : (
              <InboxOutlined />
            )}
            <Tooltip title={f.file_key}>
              <span>{f.file_name}</span>
            </Tooltip>
            {/* 媒体「能不能播」徽标 —— 与文件列表页同一个组件,不另判一遍 */}
            <MediaStatusTag
              media_reason={f.media_reason}
              media_container={f.media_container}
              media_video_codec={f.media_video_codec}
              media_audio_codec={f.media_audio_codec}
              transcode_status={f.transcode_status}
            />
          </Space>
        )
      },
    },
    {
      /**
       * 封面 / 预览 —— 「设置为封面以后在哪里显示这个封面」的答案之一。
       *
       * ⚠️ 文件夹:cover_url 是稳定直链,没有时是**空串**(不是报错)——
       *   显示"无封面"。文件:图片用本体重定向、视频用后端抽的封面帧
       *   (url / cover_url,FileGrid 同判据),都没有时「—」。
       */
      title: t('field.cover'),
      dataIndex: 'cover_url',
      width: 90,
      render: (_: unknown, row: MixedRow) => {
        const src = isFolderRow(row) ? row.folder.cover_url : row.file.url || row.file.cover_url || ''
        if (!src) {
          // 文件夹明确说"无封面";文件没有"封面"概念,占位从简
          return isFolderRow(row) ? (
            <span style={{ color: 'var(--app-text-tertiary)' }}>{t('page.fileFolder.noCover')}</span>
          ) : (
            <span style={{ color: 'var(--app-text-tertiary)' }}>—</span>
          )
        }
        return (
          <Image
            src={src}
            alt={t('field.cover')}
            width={48}
            height={48}
            style={{ objectFit: 'cover', borderRadius: 4 }}
          />
        )
      },
    },
    {
      title: t('field.childFolders'),
      dataIndex: 'child_count',
      width: 100,
      // 只有文件夹有子文件夹;文件格子画「—」(0 会被误读成"没有子文件夹")
      render: (_: unknown, row: MixedRow) =>
        isFolderRow(row) ? (
          row.folder.child_count > 0 ? (
            <Tag color="blue">{row.folder.child_count}</Tag>
          ) : (
            0
          )
        ) : (
          '—'
        ),
    },
    {
      title: t('field.fileCount'),
      dataIndex: 'file_count',
      width: 90,
      render: (_: unknown, row: MixedRow) => (isFolderRow(row) ? (row.folder.file_count ?? 0) : '—'),
    },
    {
      title: t('field.totalSize'),
      dataIndex: 'total_size',
      width: 110,
      // 文件夹 = 子树总大小;文件 = 本文件大小
      render: (_: unknown, row: MixedRow) =>
        formatSize(isFolderRow(row) ? row.folder.total_size : row.file.file_size),
    },
    {
      title: t('field.biz'),
      dataIndex: 'biz',
      width: 100,
      // 两边都有 biz(文件也挂在业务段下)
      render: (_: unknown, row: MixedRow) => (isFolderRow(row) ? row.folder.biz : row.file.biz),
    },
    {
      title: t('field.isDefault'),
      dataIndex: 'is_default',
      width: 80,
      // 默认文件夹是后端按 biz 懒建的(上传时 folder_id=0 落到它),
      // 删掉之后下次上传会再懒建一个同名同 id 语义的
      render: (_: unknown, row: MixedRow) =>
        isFolderRow(row) && row.folder.is_default ? <Tag color="blue">{t('field.isDefault')}</Tag> : null,
    },
    {
      title: t('common.createTime'),
      dataIndex: 'created_at',
      width: 170,
      // 文件夹是创建时间,文件是上传时间 —— 对文件来说上传即创建
      render: (_: unknown, row: MixedRow) => formatTime(isFolderRow(row) ? row.folder.created_at : row.file.created_at),
    },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 300,
      fixed: 'right',
      /**
       * 按 kind 出动作:文件夹 = 进入/详情/建子文件夹/查看文件/删除
       * (与卡片视图共用 `actionsFor`);文件 = 详情/下载/删除
       * (与文件列表页同一套 perm 码,零新码)。
       */
      render: (_: unknown, row: MixedRow) =>
        isFolderRow(row) ? (
          <RowActions<FolderView> row={row.folder} actions={actionsFor(row.folder)} />
        ) : (
          <RowActions<FileView> row={row.file} actions={fileActionsFor(row.file)} />
        ),
    },
  ]

  const createFields: EditField[] = [
    {
      name: 'name',
      label: t('field.folderName'),
      required: true,
      rules: [{ required: true, min: 1, max: 64 }],
      // 告诉用户"会建在哪" —— 多级之后"建在哪"是个真问题
      help: creating
        ? t('page.fileFolder.createInPath', undefined, { path: creating.parentPath })
        : undefined,
    },
    // ⚠️ **建子文件夹时不显示 biz** —— 后端会继承上级的业务段,
    //   而这里如果让人填,填错了后端会直接报错("子文件夹必须与上级文件夹
    //   属于同一业务段"),白填一趟。
    ...(creating?.inheritBiz
      ? []
      : [
          {
            name: 'biz',
            label: t('page.fileFolder.bizSegment'),
            // 不填 = 后端用默认业务段。路径第二段就是它,填错等于文件传到别的目录去了
            rules: [{ max: 32 }],
          } as EditField,
        ]),
  ]

  /**
   * 新建弹窗的初始值
   *
   * ⚠️ **必须稳定引用** —— EditModal 有一个 effect 依赖 `initialValues`,
   *   而它每次都会 `form.resetFields()` + `setFieldsValue()`。传字面量
   *   `{ name: '' }` 的话这个对象每次 render 都是新的 ⇒ effect 每次 render
   *   都跑 ⇒ **用户正在输入的名字被清空**(而且是输入一个字符清一次)。
   */
  const createInitialValues = useMemo(() => ({ name: '' }), [])

  const viewFields: ViewField<FolderView>[] = [
    { name: 'name', label: t('field.folderName') },
    { name: 'path', label: t('page.fileFolder.fullPath') },
    { name: 'biz', label: t('page.fileFolder.bizSegment') },
    {
      name: 'cover_url',
      label: t('field.cover'),
      render: (v: string) =>
        v ? (
          <Image
            src={v}
            alt={t('field.cover')}
            width={320}
            style={{ objectFit: 'contain', borderRadius: 4 }}
          />
        ) : (
          t('page.fileFolder.noCover')
        ),
    },
    { name: 'child_count', label: t('field.childFolders') },
    { name: 'file_count', label: t('field.fileCount') },
    { name: 'total_size', label: t('field.totalSize'), render: (v: number) => formatSize(v) },
    { name: 'is_default', label: t('page.fileFolder.defaultFolder'), render: (v: number) => (v ? t('common.yes') : t('common.no')) },
    { name: 'cover_file_id', label: t('page.fileFolder.coverFileId') },
    { name: 'cover_key', label: t('page.fileFolder.coverKey') },
    { name: 'created_at', label: t('common.createTime'), render: (v: string) => formatTime(v) },
  ]

  /** 文件行的详情抽屉字段 —— 与文件列表页同一批(口径:文件自己,不含文件夹统计) */
  const fileViewFields: ViewField<FileView>[] = [
    { name: 'file_name', label: t('field.fileName') },
    { name: 'file_size', label: t('field.size'), render: (v: number) => formatSize(v) },
    { name: 'content_type', label: t('field.type') },
    // ⚠️ key 展示给排障用(与 list.tsx 同一个理由)
    { name: 'file_key', label: t('page.fileList.objectKey') },
    { name: 'created_at', label: t('field.uploadTime'), render: (v: string) => formatTime(v) },
  ]

  /**
   * 混合列表的数据:先子文件夹后文件(资源管理器惯例)。
   * 文件行只在 `showFiles` 时并入(见上面分页方案那段注释)。
   */
  const mixedRows: MixedRow[] = useMemo(
    () => [
      ...(foldersTable.tableProps.dataSource ?? []).map((f): MixedRow => ({ kind: 'folder', folder: f })),
      ...(showFiles ? filesState.list.map((f): MixedRow => ({ kind: 'file', file: f })) : []),
    ],
    [foldersTable.tableProps.dataSource, filesState.list, showFiles],
  )

  /**
   * 统计条:**文件夹 X 个 · 文件 Y 个**。
   *
   * ⚠️ 文件不参与翻页(只在第一页合并进列表),但它的总数必须**每一页都
   *   看得见** —— 不然翻到第 2 页的用户会以为这个文件夹没有文件。
   */
  const mixedShowTotal = useCallback(
    () => t('page.fileFolder.mixedTotal', undefined, { folders: foldersTable.total, files: filesState.total }),
    [filesState.total, foldersTable.total, t],
  )

  /** 文件超过一次拉取上限时的兜底入口(表格 / 卡片视图共用) */
  const truncatedFiles = showFiles && filesState.total > filesState.list.length

  // ===== 层级还没判定出来 =====
  if (!flagsReady) {
    return (
      <div style={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Spin />
      </div>
    )
  }

  // ===== 用户层 =====
  if (showUserLayer) {
    return (
      <div style={{ height: '100%' }}>
        <ManagementTable<FolderUserView>
          pageDescription={t('page.fileFolder.userLayerDescription')}
          onSearch={(kw) => usersTable.search({ keyword: kw })}
          onReset={() => usersTable.reset()}
          searchPlaceholder={t('page.fileFolder.userLayerSearchPlaceholder')}
          columns={userColumns}
          tableProps={{
            ...usersTable.tableProps,
            rowKey: 'user_id',
            /**
             * ⚠️ **点整行进入这个用户的空间**(2026-11-01 用户提的
             *   「文件夹管理列表要支持点击进入」)——
             *   名字那一列本来就是链接,现在整行都能点。
             *   行内的按钮 / 名字链接 / 标签由 `enterOnRow` 统一挡掉。
             */
            onRow: enterOnRow<FolderUserView>((row) => goToUser(row.user_id)),
          }}
          scrollX={800}
        />
      </div>
    )
  }

  // ===== 文件夹层(混合列表)=====
  return (
    /**
     * ⚠️ **一个列表,不是两个**(2026-11-03 用户验收反馈:M179 交付的
     *   "上面文件夹表 + 下面文件区"两个列表,改成像 Windows 资源管理器
     *   那样的**混合列表**:先子文件夹后文件)。数据在 `mixedRows` 合并,
     *   ManagementTable 重新成为唯一列表,外层回到单容器。
     */
    <div style={{ height: '100%' }}>
      <ManagementTable<MixedRow>
        pageDescription={
          crossUser
            ? canWrite
              ? t('page.fileFolder.ownSpaceDescription', undefined, { name: ownerLabel })
              : t('page.fileFolder.otherSpaceDescription', undefined, { name: ownerLabel })
            : t('page.fileFolder.description')
        }
        /*
         * ⚠️ **必须有 onSearch** ——
         *
         * ManagementTable 的 `showSearchBar = onSearch || onReset || searchExtra || inlineToolBar`,
         * 只传 inlineToolBar 时搜索区**照样渲染**,但里面
         * `Input.Search` 只在传了 onSearch 时才渲染 ⇒ 用户看到的是
         * "搜索区里没有搜索框"(用户 2026-10-30 报的)。
         */
        onSearch={(kw) => foldersTable.search({ keyword: kw })}
        onReset={() => foldersTable.reset()}
        searchPlaceholder={t('common.placeholder.searchFolder')}
        headerExtra={
          <div style={{ padding: '8px 0' }}>
            <Breadcrumb items={crumbs} />
            {/* ⚠️ **有关键字时列的是全部层级** —— 不说清楚的话,
                用户会以为搜出来的是"当前文件夹里的",而实际上连别的分支也搜进来了。 */}
            {foldersTable.query.keyword?.trim() ? (
              <Tag color="orange" style={{ marginTop: 4 }}>
                {t('page.fileFolder.searchAllLevels')}
              </Tag>
            ) : null}
            {/* ⚠️ **别人的空间是只读的** —— 说清楚,免得用户以为按钮坏了。
                真正的拦截在后端(前端藏按钮只是体验)。 */}
            {crossUser && !canWrite ? (
              <Tag color="orange" style={{ marginTop: 4 }}>
                {t('page.fileFolder.readOnlySpace')}
              </Tag>
            ) : null}
            {/**
             * ⚠️ 文件超过一次拉取上限(FILES_LOAD_SIZE)时,列表里只摆得下
             *   前一段 —— 这里给"看全部"的兜底入口(带 folder_id 跳文件列表,
             *   那边按文件夹过滤是同一口径)。
             */}
            {truncatedFiles ? (
              <div style={{ marginTop: 4 }}>
                <a onClick={() => navigate(`/file/list?folder_id=${encodeURIComponent(folderId)}`)}>
                  {t('page.fileFolder.viewAllFiles', undefined, { n: filesState.total })}
                </a>
              </div>
            ) : null}
          </div>
        }
        inlineToolBar={[
          /**
           * ⚠️ **视图切换用 `Radio.Group optionType="button"`,不用 `Segmented`** ——
           *   本仓的**筛选器**统一是 Select(2026-10-26/29 两次把 Segmented 换掉),
           *   两态开关的现成写法是 Radio.Group(见文件列表的视图切换)。
           *   这里跟着已有的走,不自创一套。
           */
          <Radio.Group
            key="viewMode"
            size="small"
            optionType="button"
            buttonStyle="solid"
            value={viewMode}
            onChange={(e) => changeViewMode(e.target.value as 'table' | 'grid')}
            aria-label={t('page.fileFolder.viewMode')}
          >
            <Radio.Button value="table">
              <TableOutlined /> {t('page.fileFolder.viewTable')}
            </Radio.Button>
            <Radio.Button value="grid">
              <AppstoreOutlined /> {t('page.fileFolder.viewGrid')}
            </Radio.Button>
          </Radio.Group>,
          // ⚠️ **别人的空间里不给"新建"** —— 后端 Create 永远建在调用方
          //   自己名下,在这儿建等于建到了别人的地方去(而且界面上看不出来)。
          ...(canWrite
            ? [
                <AuthButton
                  key="create"
                  type="primary"
                  icon={<PlusOutlined />}
                  perm={PermFile.FOLDER_CREATE}
                  onClick={() =>
                    setCreating({
                      parentId: folderId || '0',
                      parentPath: detail?.path ?? t('page.fileFolder.rootDir'),
                      inheritBiz: !!folderId,
                    })
                  }
                >
                  {t('page.fileFolder.createFolder')}
                </AuthButton>,
              ]
            : []),
        ]}
        columns={columns}
        tableProps={{
          ...foldersTable.tableProps,
          // ⚠️ **混合数据**:文件夹(当前页)+ 文件(showFiles 时并入)。
          //    先文件夹后文件 —— 资源管理器惯例。
          dataSource: mixedRows,
          /**
           * ⚠️ **rowKey 必须按 kind 拼前缀** —— 文件夹 id 和文件 id 是两张表
           *   的主键,裸用 id 会撞(比如文件夹 "5" 和文件 "5"),React key
           *   撞了的行为很怪(行错位 / 状态串行)。
           */
          rowKey: (r: MixedRow) => (isFolderRow(r) ? `folder-${r.folder.id}` : `file-${r.file.id}`),
          // 两个请求任一在跑都算加载中(文件慢时列表也不能看着"就这些了")
          loading: foldersTable.tableProps.loading || filesState.loading,
          // ⚠️ 统计条换成混合口径 —— 分页只管文件夹,文件总数常驻(见 mixedShowTotal)
          pagination: { ...foldersTable.tableProps.pagination, showTotal: mixedShowTotal },
          /**
           * ⚠️ **点整行**:文件夹行 = 进入(2026-11-01 用户提的,
           *   保留);文件行 = 详情(文件没有"进入"语义,详情是最自然的落点,
           *   与文件列表页抽屉同一口径)。
           *
           *   ⚠️ **行内的按钮 / 名字链接 / 封面预览 / 标签由 `isInteractiveTarget`
           *     统一挡掉** —— 不挡的话,点「删除」会顺带把行打开
           *     (与 enterOnRow 同一套判定,只是这里要按 kind 分支所以手写)。
           */
          onRow: (row: MixedRow) => ({
            onClick: (e: React.MouseEvent<HTMLElement>) => {
              if (isInteractiveTarget(e.target)) return
              if (isFolderRow(row)) goToFolder(row.folder.id)
              else setViewingFile(row.file)
            },
            style: { cursor: 'pointer' },
            className: 'row-clickable',
          }),
        }}
        scrollX={1300}
        /**
         * ⚠️ **卡片视图**(M144):传了它 `<Table>` 就被换掉(ManagementTable
         *   的 `body` 槽位规矩)。混合卡片体:文件夹卡片在前(FolderGrid),
         *   文件卡片在后(FileGrid),**一份分页器**画在最底下 —— 两个网格
         *   自带的都藏掉(hidePagination),两份分页器叠着用户不知道哪个管谁。
         */
        body={
          viewMode === 'grid' ? (
            <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
              <div style={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
                {foldersTable.tableProps.dataSource.length > 0 ? (
                  <FolderGrid
                    folders={foldersTable.tableProps.dataSource}
                    loading={foldersTable.tableProps.loading}
                    error={foldersTable.tableProps.error}
                    onRetry={foldersTable.tableProps.onRetry}
                    pagination={{ ...foldersTable.tableProps.pagination, showTotal: mixedShowTotal }}
                    actionsFor={actionsFor}
                    onEnter={(f) => goToFolder(f.id)}
                    hidePagination
                  />
                ) : null}
                {showFiles && filesState.list.length > 0 ? (
                  <>
                    {/**
                     * 分区小标题 —— 混在一张网格里,不说"这批是文件"的话,
                     * 文件卡片和文件夹卡片(图标兜底的那种)看起来是一路的。
                     */}
                    <div style={{ paddingTop: 12, paddingBottom: 4, display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span style={{ fontWeight: 600 }}>{t('page.fileFolder.filesTitle')}</span>
                      <Tag color="blue">{filesState.total}</Tag>
                    </div>
                    <FileGrid
                      files={filesState.list}
                      loading={filesState.loading}
                      // 文件拉取失败不打断混合体(见 fetchFiles 的 catch),错误态走空
                      pagination={{
                        current: 1,
                        pageSize: Math.max(filesState.list.length, 1),
                        total: filesState.list.length,
                        showSizeChanger: false,
                        showQuickJumper: false,
                        showTotal: () => '',
                        onChange: () => {},
                      }}
                      actionsFor={fileActionsFor}
                      hidePagination
                    />
                  </>
                ) : null}
                {foldersTable.tableProps.dataSource.length === 0 && filesState.list.length === 0 ? (
                  // 两边都空:优先展示文件夹那侧的错误态 / 空态(它是分页主角)
                  foldersTable.tableProps.error ? (
                    <TableErrorState variant="empty" onRetry={foldersTable.tableProps.onRetry} />
                  ) : (
                    <Empty description={t('common.noData')} style={{ marginTop: 48 }} />
                  )
                ) : null}
              </div>
              <div style={{ flexShrink: 0, paddingTop: 12, display: 'flex', justifyContent: 'flex-end' }}>
                <Pagination
                  size="small"
                  current={foldersTable.tableProps.pagination.current}
                  pageSize={foldersTable.tableProps.pagination.pageSize}
                  total={foldersTable.tableProps.pagination.total}
                  showSizeChanger={foldersTable.tableProps.pagination.showSizeChanger}
                  showQuickJumper={foldersTable.tableProps.pagination.showQuickJumper}
                  showTotal={mixedShowTotal}
                  onChange={foldersTable.tableProps.pagination.onChange}
                />
              </div>
            </div>
          ) : undefined
        }
      />

      <EditModal<{ name: string; biz?: string }>
        title={t('page.fileFolder.createFolder')}
        open={!!creating}
        initialValues={createInitialValues}
        fields={createFields}
        onCancel={() => setCreating(null)}
        onSubmit={async (vals) => {
          await createFolder({
            name: vals.name,
            // ⚠️ 建子文件夹时**不传 biz** —— 后端继承上级的
            biz: creating?.inheritBiz ? undefined : vals.biz,
            parentId: creating?.parentId || '0',
          })
          message.success(t('common.success'))
          setCreating(null)
          foldersTable.reload()
        }}
      />

      <ViewDrawer<FolderView>
        title={t('page.fileFolder.detailTitle')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
      />

      <ConfirmDialog
        open={!!deleting}
        danger
        title={`${t('page.fileFolder.deleteTitlePrefix')}${deleting?.name}?`}
        description={
          `${t('page.fileFolder.deleteDescPrefix')}${deleting?.file_count ?? 0}${t('page.fileFolder.deleteDescCount')}${formatSize(deleting?.total_size ?? 0)}${t('page.fileFolder.deleteDescSuffix')}` +
          // ⚠️ **删文件夹是删整棵子树**(M125)—— 子文件夹里的文件也会一起没,
          //   不说清楚的话用户以为只删了当前这一层。
          (deleting && deleting.child_count > 0
            ? ' ' + t('page.fileFolder.deleteDescSub', undefined, { n: deleting.child_count })
            : '')
        }
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          await deleteFolder(deleting.id)
          message.success(t('common.success'))
          setDeleting(null)
          foldersTable.reload()
        }}
      />

      {/** 文件行的详情抽屉 —— 与文件列表页同一批字段(文件自己,不含文件夹统计) */}
      <ViewDrawer<FileView>
        title={t('page.fileList.detailTitle')}
        open={!!viewingFile}
        data={viewingFile}
        fields={fileViewFields}
        onClose={() => setViewingFile(null)}
      />

      {/**
       * 文件删除确认 —— 文案与文件列表页同款。
       * ⚠️ 删完要刷**两处**:本层文件(少了一个)+ 文件夹列表
       *   (file_count / total_size 是文件夹接口给的,得跟着变)。
       */}
      <ConfirmDialog
        open={!!deletingFile}
        danger
        title={`${t('resource.upload_delete')} ${deletingFile?.file_name}?`}
        description={t('page.fileList.deleteDesc')}
        onCancel={() => setDeletingFile(null)}
        onOk={async () => {
          if (!deletingFile) return
          await deleteFile(deletingFile.id)
          message.success(t('common.success'))
          setDeletingFile(null)
          reloadFiles()
          foldersTable.reload()
        }}
      />
    </div>
  )
}

export default FolderList
