/**
 * 文件管理 —— 文件列表(M82)
 *
 * 框架页,不是业务页:这里管的是**当前登录用户自己**的文件
 * (后端按 caller.user_id 过滤,前端传什么都不影响归属,见设计文档 §3.1.1)。
 *
 * 四个动作的权限码直接用 api 码,没有另建 `button.file.*`:
 *   上传 = file:upload、下载 = file:read、删除 = file:delete、设封面 = folder:upload
 * 这四个 api 路由和四个按钮是 1:1 的,再复制一套 button 资源只会多出
 * 「按了按钮但接口 403」和「接口能调但按钮不显示」两种不一致的组合。
 */
import type React from 'react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { App, Button, Progress, Radio, Select, Space, Tag, Tooltip, Upload, type UploadProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  AppstoreOutlined,
  InboxOutlined,
  PictureOutlined,
  ReloadOutlined,
  TableOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import { useSearchParams } from 'react-router-dom'
import { ManagementTable } from '@/components/common/ManagementTable'
import { ViewDrawer, type ViewField } from '@/components/common/ViewDrawer'
import { ConfirmDialog } from '@/components/common/ConfirmDialog'
import { RowActions, type RowAction } from '@/components/common/RowActions'
import { AuthButton } from '@/components/auth/AuthButton'
import { useTable } from '@/hooks/useTable'
import { useI18n } from '@/locales'
import { usePermissionStore } from '@/stores/permission'
import { PermFile } from '@/constants/permissions'
import { isOSSStorage, canBeCover } from '@/constants/file'
import {
  downloadFile,
  listFiles,
  listFolders,
  deleteFile,
  setFolderCover,
  uploadFileAuto,
  getFileQuota,
  getFileUrl,
  getFolder,
  type FileListQuery,
  type FileView,
  type FolderView,
  type QuotaView,
} from '@/api/upload'
import { copyToClipboard } from '@/utils/clipboard'
import { formatNumber } from '@/utils/format'
import { storage } from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'
import type { PageRequest, PageResponse } from '@/types/api'
import { FileGrid } from './FileGrid'
import { MediaStatusTag } from './MediaStatusTag'

/**
 * useTable 的 query 类型:泛型约束是 `Q extends PageRequest`,
 * 而 PageRequest 里的每页条数字段(`size`)是**可选**的。
 * 这里再交叉一个必填的 `size`,好让 `q.size` 取数时是 number 而不是
 * number | undefined。
 */
type FileQuery = PageRequest & FileListQuery & { size: number }

/** 图片类内容类型 —— 决定行首图标和"设为封面"要不要给 */
const isImage = (ct: string) => (ct || '').toLowerCase().startsWith('image/')

const formatTime = (v: string) => (v ? v.slice(0, 19).replace('T', ' ') : '-')

const FileList: React.FC = () => {
  const { t } = useI18n()
  const { message } = App.useApp()
  const [searchParams, setSearchParams] = useSearchParams()
  // 上传权限:用 store 的选择器而不是 AuthButton 的 getState() —— 前者订阅了
  // 权限变化(切身份后能立刻更新),后者只在首次渲染时算一次
  const canUpload = usePermissionStore((s) => s.has(PermFile.UPLOAD))

  const { tableProps, search, reset, reload, query, setQuery } = useTable<FileQuery, FileView>({
    fetch: (q): Promise<PageResponse<FileView>> =>
      listFiles({
        folderId: q.folderId,
        biz: q.biz,
        status: q.status,
        keyword: q.keyword,
        page: q.page,
        size: q.size,
      }).then((r) => ({
        list: r.list,
        total: r.total,
        page: r.page,
        size: r.size,
      })),
  })

  const [folders, setFolders] = useState<FolderView[]>([])
  const [quota, setQuota] = useState<QuotaView | null>(null)
  const [viewing, setViewing] = useState<FileView | null>(null)
  const [deleting, setDeleting] = useState<FileView | null>(null)
  const [covering, setCovering] = useState<FileView | null>(null)
  const [uploading, setUploading] = useState(false)

  /**
   * **视图模式**(表格 / 缩略图)—— 记在 localStorage 里(M134)。
   *
   * ⚠️ **为什么要记**:文件列表里图纸和照片混着,表格视图看不出"这个文件
   *   到底是什么"(缩略图视图一眼就能看出来)。用户切到缩略图看一遍,
   *   下次进来还得再点一次 —— 这种每次都要重设的界面状态最劝退。
   *
   * ⚠️ **走 `storage` 而不是裸 localStorage** —— key 集中在 STORAGE_KEYS,
   *   落盘格式(信封)也跟别的会话键一致。
   */
  const [viewMode, setViewMode] = useState<'table' | 'grid'>(() =>
    storage.get<string>(STORAGE_KEYS.FILE_LIST_VIEW) === 'grid' ? 'grid' : 'table',
  )

  const changeViewMode = useCallback((mode: 'table' | 'grid') => {
    setViewMode(mode)
    storage.set(STORAGE_KEYS.FILE_LIST_VIEW, mode)
  }, [])

  /**
   * 上传进度汇总。
   *
   * ⚠️ **自己画进度条,不用 antd Upload 内置的文件列表**:
   * 内置列表(listType="text")会渲染在工具栏**按钮的正下方**,把整张表往下顶。
   * 而这张表是 `height:100%` + 固定 scroll.y 的(见 ManagementTable 的测高逻辑),
   * 工具栏一变高,表格底部的分页器就会被 Card 的 overflow:hidden 裁掉。
   * 文件列表本身有专门的表格页看,工具栏只留一条汇总进度就够了。
   *
   * 用 ref 存每个文件的进度、state 只存汇总值:每次 onProgress 都 setState 会让
   * 整页重渲染(表格列定义在内联函数里,重渲染成本不低)。
   */
  const uploadPercentRef = useRef<Record<string, number>>({})
  const [uploadSummary, setUploadSummary] = useState<{ count: number; percent: number } | null>(null)

  const syncUploadSummary = useCallback(() => {
    const values = Object.values(uploadPercentRef.current)
    if (values.length === 0) {
      setUploadSummary(null)
      return
    }
    const sum = values.reduce((a, b) => a + b, 0)
    setUploadSummary({ count: values.length, percent: Math.round(sum / values.length) })
  }, [])

  const refreshQuota = useCallback(() => {
    getFileQuota().then(setQuota).catch(() => setQuota(null))
  }, [])

  const refreshFolders = useCallback(() => {
    // 文件夹下拉只取第一页 200 条:选择器没有虚拟滚动,一次拉上千条会卡住
    listFolders({ page: 1, size: 200 })
      .then((r) => setFolders(r.list))
      .catch(() => setFolders([]))
  }, [])

  useEffect(() => {
    refreshQuota()
    refreshFolders()
  }, [refreshQuota, refreshFolders])

  /**
   * **当前作用域的用量**(M135)—— 跟着文件夹筛选走的那个数。
   *
   * ⚠️ **M135 修的就是用户报的这句**:
   *   「文件列表界面 search-section 里面的已用多少空间这个数值,
   *   不会跟随切换文件夹而变化」。
   *
   *   查下来是**语义错配**,不是刷新漏了:那个数是 `quota.used_bytes`
   *   (账号总用量,`sys_user.storage_used_bytes`),它**按定义**就不会跟随
   *   文件夹变。但它就摆在文件夹筛选框旁边、连"已用"两个字都没有 ——
   *   用户理所当然以为它说的是当前文件夹。
   *
   *   ⇒ 拆成两个数,各说各的:
   *   · **本文件夹**(搜索区,紧挨着文件夹筛选):选中文件夹时显示它的
   *     `total_size` / `file_count`。
   *     ⚠️ 口径与文件列表**完全一致**:后端 `folderFileStats` 统计的是
   *     "直接放在这个文件夹里的文件"(不递归子文件夹),而文件列表按
   *     `folder_id` 过滤也是同一口径 ⇒ 标签上那个数 = 表格里那些文件的
   *     总大小,不会对不上。
   *   · **总用量**(工具栏):账号级,永远不变,标签写明「总用量」。
   */
  const selectedFolderId = query.folderId || ''

  /** 已知文件夹(下拉列表那 200 条里)的统计 —— 直接取,不用再请求 */
  const knownFolder = useMemo(
    () => folders.find((f) => f.id === selectedFolderId) ?? null,
    [folders, selectedFolderId],
  )

  /**
   * URL 带进来的 `folder_id` 可能**不在下拉列表的前 200 条里**
   * (文件夹页的「查看文件」就是这么跳过来的)⇒ 那时列表里查不到它的统计,
   * 单独拉一次详情。
   *
   * ⚠️ 拉失败(文件夹被删了 / 没权限)就当"没有统计" —— 标签不显示,
   *   不能让整个列表页挂掉。
   */
  const [detailStats, setDetailStats] = useState<{ id: string; size: number; count: number } | null>(null)
  useEffect(() => {
    if (!selectedFolderId || knownFolder) {
      setDetailStats(null)
      return
    }
    let cancelled = false
    getFolder(selectedFolderId)
      .then((d) => {
        if (!cancelled) {
          setDetailStats({
            id: selectedFolderId,
            size: d.total_size ?? 0,
            count: d.file_count ?? 0,
          })
        }
      })
      .catch(() => {
        if (!cancelled) setDetailStats(null)
      })
    return () => {
      cancelled = true
    }
  }, [selectedFolderId, knownFolder])

  /**
   * 作用域用量。没选文件夹时**不显示** —— 那时"账号总用量"就是全部,工具栏那个数已经说清了。
   *
   * ⚠️ **缺的那份用 `undefined` 表示,不用 `null`** —— 本仓 eslint 是 `eqeqeq`,
   * `size == null` 会直接报错;而且 `total_size` / `file_count` 在类型上就是
   * `number`(不是 `number | null`),真的"没有"只可能是两个分支都没命中。
   */
  const scopeUsage = useMemo(() => {
    if (!selectedFolderId) return null
    let size: number | undefined
    let count: number | undefined
    if (knownFolder) {
      size = knownFolder.total_size
      count = knownFolder.file_count
    } else if (detailStats?.id === selectedFolderId) {
      size = detailStats.size
      count = detailStats.count
    }
    if (size === undefined || count === undefined) return null
    return { size, count }
  }, [selectedFolderId, knownFolder, detailStats])

  // 文件夹页的「查看文件」带着 folder_id 跳过来 → 吃进查询条件
  useEffect(() => {
    const fid = searchParams.get('folder_id')
    if (fid) setQuery((prev) => ({ ...prev, folderId: fid, page: 1 }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams])

  /**
   * antd Upload 的自定义上传。
   *
   * ⚠️ **必须回调 options.onProgress**:这一页要能传 500MB 的文件,
   * 不报进度的话用户看到的是"点了然后什么都没发生",唯一的判断依据是等。
   *
   * ⚠️ **必须回调 onSuccess / onError**:antd 只认这两个决定每个文件的状态,
   * 都不调的话那一行永远转圈。
   */
  const uploadProps: UploadProps = useMemo(
    () => ({
      multiple: true,
      // 见上面 uploadSummary 的注释:不用内置列表,自己画汇总进度
      showUploadList: false,
      customRequest: async (options) => {
        const file = options.file as File
        const name = file.name
        // ⚠️ 进度表的 key 用 antd 给的 uid,**不要用文件名**:同一次多选里
        // 两个同名文件(不同目录下的 a.txt)会撞 key,后一个把前一个的进度顶掉,
        // 前一个先结束还会把整条进度记录删掉 —— 表现是进度条中途消失。
        const key = (options.file as { uid?: string }).uid ?? name
        uploadPercentRef.current[key] = 0
        setUploading(true)
        syncUploadSummary()
        try {
          const view = await uploadFileAuto(
            file,
            { folderId: query.folderId, biz: query.biz },
            (percent) => {
              uploadPercentRef.current[key] = percent
              syncUploadSummary()
              options.onProgress?.({ percent })
            },
          )
          options.onSuccess?.(view)
        } catch (e) {
          options.onError?.(e as Error)
          message.error(`${name}: ${(e as Error).message}`)
        } finally {
          delete uploadPercentRef.current[key]
          syncUploadSummary()
          setUploading(Object.keys(uploadPercentRef.current).length > 0)
          refreshQuota()
          /**
           * ⚠️ **M135:文件夹列表也得刷** —— 它带 `total_size` / `file_count`,
           *   而搜索区那个"本文件夹"标签就是拿这两个字段算的。不刷的话
           *   传完文件,标签上的大小还是传之前的数(下拉框里的 `(N)` 也是)。
           */
          refreshFolders()
          reload()
        }
      },
    }),
    [message, query.folderId, query.biz, refreshQuota, refreshFolders, reload, syncUploadSummary],
  )

  /**
   * 每行的操作按钮 —— **表格视图和缩略图视图共用这一份**(M134)。
   *
   * ⚠️ 之前这段是写在列的 `render` 里的,缩略图视图拿不到。
   *   两处各写一份的话,权限码 / 禁用条件迟早对不上 ——
   *   表格里"设为封面"能点、缩略图里点不了,用户看着就是 bug。
   */
  const actionsFor = useMemo(
    () =>
      (row: FileView): RowAction<FileView>[] => [
        {
          key: 'view',
          label: t('common.detail'),
          perm: PermFile.READ,
          onClick: setViewing,
        },
        {
          key: 'download',
          label: t('page.fileList.download'),
          perm: PermFile.READ,
          onClick: async (r) => {
            try {
              await downloadFile(r.id, r.file_name)
            } catch (e) {
              message.error((e as Error).message)
            }
          },
        },
        {
          key: 'cover',
          label: t('page.fileList.setCover'),
          perm: PermFile.FOLDER_UPLOAD,
          /**
           * ⚠️ **只有图片能当封面**(M125)。
           *
           * 后端 `service/folder_impl.go::SetCover` 会拒非图片
           * ("只有图片才能设为封面"),前端要**先**把它禁掉 ——
           * 否则用户点下去吃一个 400,而错误信息里没有"为什么是图片"这句。
           *
           * ⚠️ 判据必须与后端同口径:`canBeCover` 同样**排除 SVG**
           *   (SVG 能塞 `<script>`,封面会被"点开看大图"打开成同源文档)。
           */
          disabled: !row.folder_id || row.folder_id === '0' || !canBeCover(row.content_type),
          disabledTip:
            !row.folder_id || row.folder_id === '0'
              ? t('page.fileList.notInFolder')
              : t('page.fileList.coverOnlyImage'),
          onClick: setCovering,
        },
        {
          key: 'copyLink',
          label: t('page.fileList.copyDirectLink'),
          perm: PermFile.READ,
          onClick: async (r) => {
            try {
              const { url } = await getFileUrl(r.id)
              // ⚠️ local provider 返的是**同源相对路径**(`/rapid-admin/...`)。
              //   直接复制出去别人拼不出完整地址 —— 补成绝对地址。
              const absolute = url.startsWith('/') ? `${window.location.origin}${url}` : url
              const ok = await copyToClipboard(absolute)
              if (ok) message.success(t('page.fileList.copyDirectLinkDone'))
              else message.error(t('page.fileList.copyDirectLinkFailed'))
            } catch (e) {
              message.error((e as Error).message)
            }
          },
        },
        {
          key: 'delete',
          label: t('common.delete'),
          perm: PermFile.DELETE,
          danger: true,
          onClick: setDeleting,
        },
      ],
    [message, t],
  )

  const columns: ColumnsType<FileView> = [
    {
      title: t('field.fileName'),
      dataIndex: 'file_name',
      width: 260,
      fixed: 'left',
      ellipsis: true,
      render: (v: string, row) => (
        <Space size={6}>
          {isImage(row.content_type) ? <PictureOutlined /> : <InboxOutlined />}
          <Tooltip title={row.file_key}>
            <span>{v}</span>
          </Tooltip>
          {row.is_cover ? <Tag color="gold">{t('page.fileList.coverTag')}</Tag> : null}
          {/**
           * 媒体「能不能播」徽标(M145d)—— 与缩略图视图**共用同一个组件**。
           *
           * ⚠️ **必须挂在文件名这一格** —— 挂在别的列里,用户扫文件名的时候
           *   看不到;而"这个文件浏览器播不了"恰恰是**扫文件名时最该知道**的事。
           */}
          <MediaStatusTag
            media_reason={row.media_reason}
            media_container={row.media_container}
            media_video_codec={row.media_video_codec}
            media_audio_codec={row.media_audio_codec}
            transcode_status={row.transcode_status}
          />
        </Space>
      ),
    },
    { title: t('field.size'), dataIndex: 'file_size', width: 100, render: (v: number) => formatNumber.fileSize(v) },
    /**
     * 时长 —— **音视频才有**(M141)。
     *
     * ⚠️ **不是媒体 / 没装 ffmpeg / 探测失败,后端都给 0** ——
     *   这里渲染成 "-" 而不是 "00:00"。显示成 00:00 会让用户以为文件是空的。
     */
    {
      title: t('field.mediaDuration'),
      dataIndex: 'duration_ms',
      width: 100,
      render: (v: number) => formatNumber.mediaDuration(v),
    },
    { title: t('field.type'), dataIndex: 'content_type', width: 180, ellipsis: true },
    { title: t('field.biz'), dataIndex: 'biz', width: 100 },
    {
      title: t('field.storage'),
      dataIndex: 'storage',
      width: 90,
      // ⚠️ **枚举值别写死** —— 后端 model.FileStorage 是 1=本地 / 2=OSS。
      //   这里以前写的是 `v === 1 ? 'OSS' : '本地'`,**两个值反了**:
      //   本地磁盘上的文件被标成 "OSS"(用户 2026-10-30 报的)。
      render: (v: number) => (
        <Tag color={isOSSStorage(v) ? 'blue' : 'default'}>
          {isOSSStorage(v) ? t('page.fileList.storageOSS') : t('page.fileList.storageLocal')}
        </Tag>
      ),
    },
    { title: t('field.uploadTime'), dataIndex: 'created_at', width: 170, render: (v: string) => formatTime(v) },
    {
      title: t('common.action'),
      dataIndex: '__actions',
      width: 320,
      fixed: 'right',
      render: (_: unknown, row: FileView) => <RowActions<FileView> row={row} actions={actionsFor(row)} />,
    },
  ]

  const viewFields: ViewField<FileView>[] = [
    { name: 'file_name', label: t('field.fileName') },
    { name: 'file_size', label: t('field.size'), render: (v: number) => formatNumber.fileSize(v) },
    /** 时长 —— 音视频才有,不是媒体时是 "-" */
    {
      name: 'duration_ms',
      label: t('field.mediaDuration'),
      render: (v: number) => formatNumber.mediaDuration(v),
    },
    { name: 'content_type', label: t('field.type') },
    { name: 'biz', label: t('field.biz') },
    { name: 'storage', label: t('field.storage'), render: (v: number) => (isOSSStorage(v) ? t('page.fileList.storageOSS') : t('page.fileList.storageLocal')) },
    { name: 'folder_id', label: t('page.fileList.folderId') },
    // ⚠️ key 展示给排障用。正常路径下没人需要它,但"文件传上去了打开是 500"
    // 这类问题的第一手证据就是 key 和后端 root 对不上。
    { name: 'file_key', label: t('page.fileList.objectKey') },
    { name: 'etag', label: 'ETag' },
    { name: 'created_at', label: t('field.uploadTime'), render: (v: string) => formatTime(v) },
  ]

  return (
    <div style={{ height: '100%' }}>
      <ManagementTable<FileView>
        pageDescription={t('page.fileList.description')}
        searchPlaceholder={t('common.placeholder.searchFile')}
        onSearch={(kw) => search({ keyword: kw })}
        onReset={() => {
          reset()
          // URL 里的 folder_id 一起清掉。不清的话地址栏写着"只看某个文件夹"、
          // 表格却是全量 —— 两边对不上,复现问题时会绕半天。
          setSearchParams({}, { replace: true })
        }}
        searchExtra={[
          <Select
            key="folder"
            placeholder={t('common.placeholder.filterFolder')}
            allowClear
            showSearch
            optionFilterProp="label"
            style={{ width: 200 }}
            value={query.folderId}
            onChange={(v) => setQuery((prev) => ({ ...prev, folderId: v, page: 1 }))}
            options={[
              { value: '', label: t('page.fileList.allFolders') },
              ...folders.map((f) => ({
                value: f.id,
                /**
                 * ⚠️ **用 path 而不是 name** —— 多级文件夹之后同一个用户下
                 *   可以有 "A/图纸" 和 "B/图纸",下拉框里都显示"图纸"就分不清
                 *   选的是哪个。path 是后端按祖先链算好的,前端不自己拼。
                 */
                label: `${f.path || f.name}(${f.file_count ?? 0})`,
              })),
            ]}
          />,
          /**
           * ⚠️ **M135:这个数必须跟着文件夹筛选走** —— 它就摆在筛选框旁边,
           *   摆的是账号总用量的话,用户理所当然以为它说的是当前文件夹
           *   (2026-10-31 用户报的就是这个)。账号总用量挪到右边工具栏,
           *   而且标明了「总用量」。
           */
          scopeUsage ? (
            <Tag key="scope" color="blue" data-testid="scope-usage">
              {t('page.fileList.scopeFolder')} <strong>{formatNumber.fileSize(scopeUsage.size)}</strong>
              <span style={{ marginLeft: 4 }}>
                {t('page.fileList.fileCount', undefined, { n: scopeUsage.count })}
              </span>
            </Tag>
          ) : null,
        ]}
        inlineToolBar={[
          /**
           * ⚠️ **视图切换用 `Radio.Group optionType="button"`,不用 `Segmented`** ——
           *   本仓的**筛选器**统一是 Select(2026-10-26/29 两次把 Segmented 换掉),
           *   两态开关的现成写法是 Radio.Group(见 dashboard 的时间范围、
           *   审批详情页的轮次选择)。这里跟着已有的走,不自创一套。
           */
          <Radio.Group
            key="viewMode"
            size="small"
            optionType="button"
            buttonStyle="solid"
            value={viewMode}
            onChange={(e) => changeViewMode(e.target.value as 'table' | 'grid')}
            aria-label={t('page.fileList.viewMode')}
          >
            <Radio.Button value="table">
              <TableOutlined /> {t('page.fileList.viewTable')}
            </Radio.Button>
            <Radio.Button value="grid">
              <AppstoreOutlined /> {t('page.fileList.viewGrid')}
            </Radio.Button>
          </Radio.Group>,
          <Space key="quota" size={8}>
            {quota ? (
              quota.unlimited ? (
                /**
                 * ⚠️ **不限配额时没有"用了百分之几"可显示** —— 这一格显示的是
                 *   账号总用量,标签写明「总用量」,免得跟左边那个"本文件夹"
                 *   的数混成一回事(一个账号级、一个文件夹级)。
                 */
                <Tag color="default">
                  {t('page.fileList.totalUsage')} <strong>{formatNumber.fileSize(quota.used_bytes)}</strong>
                </Tag>
              ) : (
                <Tooltip
                  title={t('page.fileList.quotaTooltip', undefined, {
                    used: formatNumber.fileSize(quota.used_bytes),
                    quota: formatNumber.fileSize(quota.quota_bytes),
                  })}
                >
                  <Progress
                    type="circle"
                    size={28}
                    percent={quota.quota_bytes ? Math.round((quota.used_bytes / quota.quota_bytes) * 100) : 0}
                  />
                </Tooltip>
              )
            ) : null}
            {uploadSummary ? (
              <Tooltip title={`${t('page.fileList.uploadingPrefix')}${uploadSummary.count}${t('page.fileList.uploadingSuffix')}`}>
                <Progress percent={uploadSummary.percent} size="small" style={{ width: 160 }} />
              </Tooltip>
            ) : null}
            <AuthButton icon={<ReloadOutlined />} onClick={() => { refreshQuota(); refreshFolders(); reload() }}>
              {t('page.fileList.refreshQuota')}
            </AuthButton>
            {/* ⚠️ **Upload 在外、Button 在内**(antd 官方写法),而且**不能套 AuthButton**:
                AuthButton 自己就是一个 antd <Button>,套上去会渲染成
                <button><span><button> —— 嵌套交互元素,点击行为不可控。
                权限判断改成「没有 file:upload 就不渲染 Upload」,语义跟 AuthButton 一致。 */}
            {canUpload ? (
              <Upload {...uploadProps}>
                <Button type="primary" icon={<UploadOutlined />} loading={uploading}>
                  {t('page.fileList.clickOrDropUpload')}
                </Button>
              </Upload>
            ) : null}
          </Space>,
        ]}
        columns={columns}
        tableProps={tableProps}
        scrollX={1460}
        /**
         * ⚠️ **缩略图视图**(M134):传了它 `<Table>` 就被换掉,分页器和错误态
         *   由 FileGrid 自己渲染(ManagementTable 的 `body` 槽位规矩)。
         *   搜索区 / 工具栏 / 配额这些**两边共用** —— 复制一份搜索区等于
         *   两份上传逻辑、两份筛选状态,迟早对不上。
         */
        body={
          viewMode === 'grid' ? (
            <FileGrid
              files={tableProps.dataSource}
              loading={tableProps.loading}
              error={tableProps.error}
              onRetry={tableProps.onRetry}
              pagination={tableProps.pagination}
              actionsFor={actionsFor}
            />
          ) : undefined
        }
      />

      <ViewDrawer<FileView>
        title={t('page.fileList.detailTitle')}
        open={!!viewing}
        data={viewing}
        fields={viewFields}
        onClose={() => setViewing(null)}
      />

      <ConfirmDialog
        open={!!deleting}
        danger
        title={`${t('resource.upload_delete')} ${deleting?.file_name}?`}
        description={t('page.fileList.deleteDesc')}
        onCancel={() => setDeleting(null)}
        onOk={async () => {
          if (!deleting) return
          await deleteFile(deleting.id)
          message.success(t('common.success'))
          setDeleting(null)
          refreshQuota()
          // ⚠️ M135 同上:删完文件,那个文件夹的大小/个数变了,列表要跟着刷
          refreshFolders()
          reload()
        }}
      />

      <ConfirmDialog
        open={!!covering}
        title={t('page.fileList.setFolderCoverTitle')}
        description={
          covering
            ? `${t('page.fileList.setFolderCoverPrefix')}${covering.file_name}${t('page.fileList.setFolderCoverSuffix')}`
            : ''
        }
        onCancel={() => setCovering(null)}
        onOk={async () => {
          if (!covering) return
          await setFolderCover(covering.folder_id, covering.id)
          message.success(t('common.success'))
          setCovering(null)
          reload()
        }}
      />

      {/* 文件夹页的「查看文件」带着 folder_id 跳过来,这里把它吃进查询条件。
          用 URL 而不是 store 传状态:刷新后条件还在,也能直接把链接发给同事。 */}
    </div>
  )
}

export default FileList