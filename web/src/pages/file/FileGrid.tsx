/**
 * 文件列表 —— 缩略图视图(M134)
 *
 * 为什么要有这个视图:文件列表里混着图纸、照片、PDF、压缩包,表格视图只能
 * 看文件名和大小,**看不出这个文件到底是什么**。缩略图视图把图片直接铺开,
 * 一屏扫过去就知道传了什么。
 *
 * ⚠️ **不引 `@ant-design/x`** —— 那个包的 `Upload` / `Image` 增强组件体积大,
 *   而这里要的只是"一个卡片网格"。自己用 antd 的 Card 拼,几十行就够,
 *   也不给整仓多加一个依赖。
 *
 * ⚠️ **缩略图靠 `file.url` 那条稳定直链**(M134 起列表里就带这个字段),
 *   **不逐条调 `getFileUrl`** —— 一页 50 条就是 50 个往返,列表会卡住。
 *
 * ⚠️ **分页器要自己渲染** —— 表格那份分页器长在 `<Table>` 里,换成自定义
 *   列表体(ManagementTable 的 `body` 槽位)之后它就没了。错误态同理。
 *
 * ## ⚠️ **样式必须跟着全站走**(2026-10-31 用户报「样式跟整个界面不符合」)
 *
 * 1. **颜色一律取主题 token**(`theme.useToken()`),不写死 #fafafa / #8c8c8c。
 *    本仓有暗色主题(useUIStore.theme → antd darkAlgorithm),写死的颜色
 *    切过去不会跟着变 —— 那才是"不符合整个界面"的真正来源。
 * 2. **操作收进卡片右上角的「⋯」菜单**(`<CardActionsMenu>`,2026-11-01)。
 *    五个操作(查看/下载/设为封面/复制链接/删除)原来在卡片底部排成一行,
 *    把卡片撑到 300px 宽才放得下 ⇒ 窄屏换行、卡片高度参差。
 *    现在是**一个图标按钮**,鼠标移入展开,操作**从上到下**排开。
 *    ⚠️ 动作来源仍是**同一份** `actionsFor` + 同一套可见性判定
 *    (`visibleRowActions`)⇒ 表格里能删、卡片里也能删。
 * 3. **「封面」徽标绝对定位到卡片左上角** —— 它原来在文字流里,
 *    于是"是封面"的文件比别的文件多一行,**卡片高度参差不齐**。
 *    移进缩略图区(绝对定位)后不占文档流 ⇒ 所有卡片一样高。
 *    (右上角让给了「⋯」操作按钮;媒体状态徽标挪到右下角 —— 三角不打架。)
 * 4. **按文件格式给图标**(pdf / word / excel / zip / 图像 / 音视频 / 代码 / 三维),
 *    判据是**文件名里的扩展名**(content_type 区分不出 doc 与 docx,
 *    而且 docx 的 MIME 有 80 个字符,卡片上根本放不下)。
 */
import { useEffect, useState, type ReactNode } from 'react'
import { Card, Empty, Pagination, Spin, Tag, Tooltip, theme } from 'antd'
import {
  BlockOutlined,
  CodeOutlined,
  DatabaseOutlined,
  FileExcelOutlined,
  FileImageOutlined,
  FileMarkdownOutlined,
  FilePdfOutlined,
  FilePptOutlined,
  FileTextOutlined,
  FileUnknownOutlined,
  FileWordOutlined,
  FileZipOutlined,
  SoundOutlined,
  VideoCameraOutlined,
} from '@ant-design/icons'
import { CardActionsMenu } from '@/components/common/CardActionsMenu'
import type { RowAction } from '@/components/common/RowActions'
import { TableErrorState } from '@/components/common/TableErrorState'
import { useI18n } from '@/locales'
import { formatNumber } from '@/utils/format'
import type { FileView } from '@/api/upload'
import { MediaStatusTag } from './MediaStatusTag'

/** 分页器参数 —— 与 useTable 返的 pagination 同形 */
export interface FileGridPagination {
  current: number
  pageSize: number
  total: number
  showSizeChanger: boolean
  showQuickJumper: boolean
  showTotal: (total: number) => string
  onChange: (page: number, pageSize: number) => void
}

export interface FileGridProps {
  files: FileView[]
  loading: boolean
  /** 最近一次加载的错误(成功时为 null/undefined) */
  error?: unknown
  onRetry?: () => void
  pagination: FileGridPagination
  /** 每行的操作按钮 —— **与表格视图共用同一份**(同一个权限口径) */
  actionsFor: (file: FileView) => RowAction<FileView>[]
  /**
   * 隐藏自带分页器(2026-11-03 混合列表)。
   *
   * ⚠️ 文件夹管理页的混合列表里文件是**一次拉全的**(不分页),
   *   这份分页器没有意义 —— 混合体在最底下画唯一的文件夹分页器。
   */
  hidePagination?: boolean
}

/**
 * ⚠️ **按文件格式给图标**(2026-10-31 用户提的:「要区分 pdf、word、excel、
 *   zip 等等文件格式作为文件图标」)。
 *
 * **判据是"文件名里的扩展名",不是 content_type** ——
 * ⚠️ content_type 这条路**没法区分格式**:
 *   · doc / docx 是两个 MIME(application/msword vs
 *     application/vnd.openxmlformats-officedocument.wordprocessingml.document),
 *     excel / ppt 同理;
 *   · 有的浏览器/上传器干脆给 `application/octet-stream`,还有的给空;
 *   · 而"扩展名"是用户**看得见、也认得**的那个东西(文件名尾巴)。
 * ⇒ 扩展名当主判据,content_type 只在**文件名没有扩展名**时兜底。
 *
 * ⚠️ **SVG 归到 image/** —— SVG 走 `<img src>` 是安全的(浏览器把 SVG 当图片
 *   解析,不执行脚本),只有被"当文档打开"才有风险(那正是 `canBeCover`
 *   把 SVG 排除掉的原因)。
 */

/** 从文件名里取扩展名(小写,不含点);没有就空串 */
function extOf(name: string): string {
  const i = (name || '').lastIndexOf('.')
  // ⚠️ i <= 0 ⇒ 前面没有路径分隔的"点"(.gitignore 这种隐藏文件不算扩展名)
  if (i <= 0 || i === (name || '').length - 1) return ''
  return name.slice(i + 1).toLowerCase()
}

/**
 * 扩展名 → 图标。
 *
 * ⚠️ **图标本身不配色** —— 本仓设计系统只有橙/灰/红/绿几档(见 variables.css),
 *   给 8 种格式各配一个颜色就是往设计系统里塞 8 个新颜色。
 *   格式靠**图形**区分,颜色留给语义(危险/成功/禁用)。
 */
const EXT_ICONS: Record<string, ReactNode> = {
  // 文档
  pdf: <FilePdfOutlined />,
  doc: <FileWordOutlined />,
  docx: <FileWordOutlined />,
  rtf: <FileWordOutlined />,
  odt: <FileWordOutlined />,
  xls: <FileExcelOutlined />,
  xlsx: <FileExcelOutlined />,
  xlsm: <FileExcelOutlined />,
  csv: <FileExcelOutlined />,
  ods: <FileExcelOutlined />,
  ppt: <FilePptOutlined />,
  pptx: <FilePptOutlined />,
  odp: <FilePptOutlined />,
  // 压缩包
  zip: <FileZipOutlined />,
  rar: <FileZipOutlined />,
  '7z': <FileZipOutlined />,
  tar: <FileZipOutlined />,
  gz: <FileZipOutlined />,
  bz2: <FileZipOutlined />,
  // 纯文本 / 标记
  txt: <FileTextOutlined />,
  log: <FileTextOutlined />,
  md: <FileMarkdownOutlined />,
  markdown: <FileMarkdownOutlined />,
  // 图片
  jpg: <FileImageOutlined />,
  jpeg: <FileImageOutlined />,
  png: <FileImageOutlined />,
  gif: <FileImageOutlined />,
  webp: <FileImageOutlined />,
  bmp: <FileImageOutlined />,
  svg: <FileImageOutlined />,
  tif: <FileImageOutlined />,
  tiff: <FileImageOutlined />,
  ico: <FileImageOutlined />,
  // 音视频
  mp4: <VideoCameraOutlined />,
  mov: <VideoCameraOutlined />,
  avi: <VideoCameraOutlined />,
  mkv: <VideoCameraOutlined />,
  wmv: <VideoCameraOutlined />,
  flv: <VideoCameraOutlined />,
  webm: <VideoCameraOutlined />,
  mp3: <SoundOutlined />,
  wav: <SoundOutlined />,
  flac: <SoundOutlined />,
  aac: <SoundOutlined />,
  m4a: <SoundOutlined />,
  ogg: <SoundOutlined />,
  // 代码 / 数据
  json: <CodeOutlined />,
  xml: <CodeOutlined />,
  yaml: <CodeOutlined />,
  yml: <CodeOutlined />,
  js: <CodeOutlined />,
  ts: <CodeOutlined />,
  jsx: <CodeOutlined />,
  tsx: <CodeOutlined />,
  py: <CodeOutlined />,
  go: <CodeOutlined />,
  java: <CodeOutlined />,
  sql: <DatabaseOutlined />,
  db: <DatabaseOutlined />,
  sqlite: <DatabaseOutlined />,
  // CAD / 三维(本项目的交付物)
  dwg: <BlockOutlined />,
  dxf: <BlockOutlined />,
  rvt: <BlockOutlined />,
  ifc: <BlockOutlined />,
  skp: <BlockOutlined />,
  obj: <BlockOutlined />,
  fbx: <BlockOutlined />,
  glb: <BlockOutlined />,
  gltf: <BlockOutlined />,
  '3ds': <BlockOutlined />,
  max: <BlockOutlined />,
}

/** content_type → 图标(文件名**没有扩展名**时的兜底) */
function iconByContentType(contentType: string): ReactNode {
  const ct = (contentType || '').toLowerCase()
  if (ct.includes('pdf')) return <FilePdfOutlined />
  if (ct.includes('word') || ct.includes('rtf')) return <FileWordOutlined />
  if (ct.includes('excel') || ct.includes('spreadsheet') || ct.includes('csv')) return <FileExcelOutlined />
  if (ct.includes('powerpoint') || ct.includes('presentation')) return <FilePptOutlined />
  if (ct.includes('zip') || ct.includes('compressed') || ct.includes('tar')) return <FileZipOutlined />
  if (ct.startsWith('image/')) return <FileImageOutlined />
  if (ct.startsWith('video/')) return <VideoCameraOutlined />
  if (ct.startsWith('audio/')) return <SoundOutlined />
  if (ct.startsWith('text/') || ct.includes('markdown')) return <FileTextOutlined />
  if (ct.includes('json') || ct.includes('xml')) return <CodeOutlined />
  return <FileUnknownOutlined />
}

/** 这个文件该摆哪个图标(扩展名优先,content_type 兜底) */
function fileIcon(file: FileView): ReactNode {
  const ext = extOf(file.file_name)
  if (ext && EXT_ICONS[ext]) return EXT_ICONS[ext]
  return iconByContentType(file.content_type)
}

interface FileCardProps {
  file: FileView
  actionsFor: (file: FileView) => RowAction<FileView>[]
}

function FileCard({ file, actionsFor }: FileCardProps) {
  const { t } = useI18n()
  const { token } = theme.useToken()
  /**
   * 图片加载失败(直链 403 / 对象没了 / 地址过期)⇒ 换成占位图标。
   *
   * ⚠️ **必须处理** —— `<img>` 加载失败时浏览器画的是一张"碎图"图标,
   *   在 antd 的卡片里跟真图混在一起,用户会以为文件传坏了。
   */
  const [imgBroken, setImgBroken] = useState(false)

  const isImage = (file.content_type || '').toLowerCase().startsWith('image/')

  /**
   * 缩略图地址 —— **图片用文件本体,视频用后端抽出来的封面帧**。
   *
   * ⚠️ **视频的封面不是文件本体** —— mp4 塞进 `<img src>` 是裂图。
   *   后端 M141 起给视频带一个 `cover_url`(ffmpeg 抽的第一帧,jpg)。
   *
   * ⚠️ **音频没有封面** —— 后端**不抽音频帧**(抽出来的波形对唱片封面没意义,
   *   还白占一份存储)。所以音频落回图标分支,而 mp3/wav/flac… 在
   *   EXT_ICONS 里已经映射成 SoundOutlined —— 这就是"音频用音频图标"。
   *
   * ⚠️ **前端不判"是不是视频"** —— 判据就是 `cover_url` 在不在。
   *   前端再抄一份扩展名表,迟早跟后端那份对不上(后端还多认一堆冷门格式)。
   */
  const thumbSrc = isImage ? file.url : file.cover_url
  /** 有缩略图地址(不管加载成功没)—— 用来区分"预览不出来"和"本来就没预览" */
  const hasThumb = !!thumbSrc
  const showImage = hasThumb && !imgBroken

  /**
   * **缩略图地址变了就把"加载失败"的记号清掉**。
   *
   * ⚠️ 卡片是按 `file.id` 做 key 的 ⇒ 同一条文件重新加载后**还是同一个组件实例**,
   *   `imgBroken` 会一直粘着 true。
   *   ⇒ 症状:OSS 桶没开公共读(直链 403)→ 满屏占位图标;
   *   管理员把桶改好了 → 用户刷新列表,**还是满屏占位图标**(得切一次视图才复位)。
   */
  useEffect(() => {
    setImgBroken(false)
  }, [thumbSrc])

  /** 扩展名(小写,不含点)—— 图标和副标题都用它 */
  const ext = extOf(file.file_name)

  return (
    /**
     * ⚠️ `size="small"` ⇒ antd 的 Card body 内边距走 `bodyPaddingSM`(=12,
     *   card/style/index.js 的 prepareComponentToken 里写死的),
     *   跟表格视图那些小控件的密度是一档的;**不要再手写 padding 覆盖它**。
     */
    <Card size="small" data-testid="file-grid-card">
      {/**
       * 缩略图区 —— **固定 4:3**,所以每张卡片的这一段一样高。
       *
       * ⚠️ `position: relative` 是给压在上面的三个角标当锚点的
       *   (左上「封面」/ 右上「⋯」/ 右下媒体状态 —— antd 的 Card 本身是
       *   relative,但锚点在 body 内部的这个盒子上)。
       */}
      <div
        data-testid="file-grid-thumb"
        style={{
          position: 'relative',
          aspectRatio: '4 / 3',
          borderRadius: token.borderRadiusSM,
          overflow: 'hidden',
          background: token.colorFillQuaternary,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        {showImage ? (
          <img
            src={thumbSrc}
            alt={file.file_name}
            loading="lazy"
            onError={() => setImgBroken(true)}
            style={{ width: '100%', height: '100%', objectFit: 'cover' }}
          />
        ) : (
          /**
           * 图片类文件没有直链(后端签不出来)或直链加载失败 ⇒ 明确告诉用户
           * "这个预览不出来",而不是摆一个看不出含义的通用图标。
           *
           * ⚠️ **非图片文件摆的是"格式图标"** —— pdf / word / excel / zip 各有各的
           *   图形(用户 2026-10-31 提的)。tooltip 报的是**扩展名**
           *   (用户认得的那个),不是 content_type。
           */
          <Tooltip
            title={
              hasThumb
                ? t('page.fileList.previewUnavailable')
                : (ext || file.content_type || undefined)
            }
          >
            <span style={{ fontSize: 36, color: token.colorTextQuaternary, lineHeight: 1 }}>
              {fileIcon(file)}
            </span>
          </Tooltip>
        )}

        {/**
         * 「封面」徽标 —— **绝对定位在卡片左上角**(2026-11-01 从右上角挪过来:
         * 右上角让给了「⋯」操作按钮)。
         *
         * ⚠️ **为什么必须绝对定位**:它原来在文字流里,于是"是封面"的文件
         *   比别的文件多出一行 ⇒ **卡片高度参不齐**,一屏看过去全是参差的
         *   边。移进缩略图区后不占文档流 ⇒ 所有卡片一样高。
         *
         * ⚠️ 徽标本身用 `<Tag color="gold">` —— **跟表格视图那一个同款**
         *   (表格里它就挂在文件名后面),别自己再造一个样式。
         */}
        {file.is_cover ? (
          <Tag
            color="gold"
            style={{ position: 'absolute', top: 0, left: 0, margin: 0 }}
            data-testid="file-grid-cover"
          >
            {t('page.fileList.coverTag')}
          </Tag>
        ) : null}

        {/**
         * 「⋯」操作菜单 —— **绝对定位在卡片右上角**(用户 2026-11-01 提的:
         * 原来五个操作排在卡片底部,把卡片撑得很宽还换行)。
         *
         * ⚠️ 动作来自**同一份** `actionsFor`,判定走 `visibleRowActions`
         *   (与表格视图同一个函数)⇒ 两种视图的操作永远一致。
         *
         * ⚠️ **贴边(0)**:Card 自身有内边距(antd `bodyPaddingSM` = 12),
         *   缩略图这块本来就已经缩进一圈了,角标再留 8px 就等于缩两圈。
         */}
        <div style={{ position: 'absolute', top: 0, right: 0 }}>
          <CardActionsMenu<FileView> row={file} actions={actionsFor(file)} />
        </div>

        {/**
         * 时长徽标 —— **绝对定位在缩略图左下角**(M141)。
         *
         * ⚠️ **只有探到时长的文件才有** —— 后端 `duration_ms = 0` 表示
         *   "不是媒体 / 没装 ffmpeg / 探测失败",那是**不知道**,
         *   显示成 "00:00" 会让用户以为文件是空的 ⇒ 直接不显示。
         *
         * ⚠️ **颜色取主题 token,不用写死** —— `colorBgMask` 是 antd 自己的
         *   遮罩色,`colorTextLightSolid` 是"浮层上的文字色"。
         *   写死 rgba(0,0,0,.62) / #fff 的话,暗色主题下浮层会变得不对劲。
         */}
        {file.duration_ms > 0 ? (
          <span
            data-testid="file-grid-duration"
            style={{
              position: 'absolute',
              left: 0,
              bottom: 0,
              padding: '0 6px',
              borderRadius: token.borderRadiusSM,
              background: token.colorBgMask,
              color: token.colorTextLightSolid,
              fontSize: token.fontSizeSM,
              lineHeight: '18px',
              // ⚠️ 等宽数字 —— 时长是跳着变的(0:59 → 1:00),不定宽的话
              //   徽标宽度会抖。
              fontVariantNumeric: 'tabular-nums',
            }}
          >
            {formatNumber.mediaDuration(file.duration_ms)}
          </span>
        ) : null}

        {/**
         * 媒体「浏览器能不能播」徽标(M145d)—— **绝对定位在缩略图右下角**。
         *
         * ⚠️ **必须绝对定位**:放进文字流里的话,只有部分文件带这枚徽标 ⇒
         *   **卡片高度参不齐**(「封面」徽标当初就是因为这个才挪进缩略图区的,
         *   见本文件头部的第 3 条)。
         *
         * ⚠️ 位置是**右下角**:左上角是「封面」徽标、右上角是「⋯」操作按钮、
         *   左下角是时长徽标 —— 四个各占一角,互不遮挡。
         */}
        <div style={{ position: 'absolute', bottom: 0, right: 0, maxWidth: '70%' }}>
          <MediaStatusTag
            media_reason={file.media_reason}
            media_container={file.media_container}
            media_video_codec={file.media_video_codec}
            media_audio_codec={file.media_audio_codec}
            transcode_status={file.transcode_status}
          />
        </div>
      </div>

      {/* 文件名 —— 字号跟表格正文一档(12),超长省略,行高固定 */}
      <Tooltip title={file.file_key}>
        <div
          style={{
            marginTop: 8,
            fontSize: token.fontSize,
            color: token.colorText,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {file.file_name}
        </div>
      </Tooltip>

      {/**
       * 大小 · 扩展名 —— 次级文字(三级文字色)。
       *
       * ⚠️ **这里摆的是扩展名,不是 content_type**:
       *   content_type 对 docx 是
       *   `application/vnd.openxmlformats-officedocument.wordprocessingml.document`
       *   —— 80 个字符,一行放不下,截断之后只剩一串没意义的乱码。
       *   而扩展名("XLSX")既短又是用户认得的那个。
       */}
      <div
        style={{
          marginTop: 2,
          fontSize: token.fontSize,
          color: token.colorTextTertiary,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {formatNumber.fileSize(file.file_size)}
        {ext ? ` · ${ext.toUpperCase()}` : ''}
      </div>
      {/**
       * ⚠️ **操作不在这里了** —— 挪到缩略图右上角的「⋯」菜单
       *   (见上面 `CardActionsMenu` 那一段)。原来五个操作排在这一行,
       *   把卡片撑到 300px 宽才放得下,窄屏还换行 ⇒ 卡片高度参差。
       */}
    </Card>
  )
}

export function FileGrid({ files, loading, error, onRetry, pagination, actionsFor, hidePagination }: FileGridProps) {
  const { t } = useI18n()
  const showError = error !== null && error !== undefined
  const hasFiles = files.length > 0

  return (
    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      {showError && !hasFiles ? (
        // 首屏就失败 → 降级占位(和表格视图的 emptyText 一致)
        <TableErrorState variant="empty" onRetry={onRetry} />
      ) : !hasFiles ? (
        <Empty description={t('common.noData')} style={{ marginTop: 48 }} />
      ) : (
        <>
          {/**
           * ⚠️ **加载遮罩自己画,不用 Spin 的嵌套模式** ——
           *   antd 5.29 的 `Spin` 把 `style` 落在**转圈那个元素**上,不是外层
           *   容器上,套不进 flex 布局(实测读 node_modules/antd/lib/spin/index.js:
           *   `mergedStyle` 给了 `spinElement`,嵌套容器只拿 `wrapperClassName`)。
           *   自己画一层 absolute 遮罩,位置和尺寸都可控。
           */}
          <div style={{ flex: 1, minHeight: 0, position: 'relative', overflow: 'hidden' }}>
            <div style={{ height: '100%', overflowY: 'auto' }}>
              <div
                data-testid="file-grid"
                style={{
                  display: 'grid',
                  // ⚠️ **200px 下限**(2026-11-01 用户定的):操作收进右上角的
                  //   「⋯」菜单之后,卡片里最宽的就是文件名一行 ⇒ 200px 够放。
                  //   以前是 300px,理由是"一排 5 个操作按钮约 264px 宽",
                  //   那个理由已经不成立了(按钮不再并排)。
                  gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
                  gap: 16,
                  alignContent: 'start',
                }}
              >
                {files.map((f) => (
                  <FileCard key={f.id} file={f} actionsFor={actionsFor} />
                ))}
              </div>
            </div>
            {loading ? (
              <div
                style={{
                  position: 'absolute',
                  inset: 0,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  background: 'rgba(255,255,255,0.55)',
                }}
              >
                <Spin />
              </div>
            ) : null}
          </div>
          {/**
           * ⚠️ **分页器自己渲染** —— 表格那份长在 `<Table>` 里,换成自定义
           *   列表体之后它跟着没了(ManagementTable 的 `body` 槽位说明里写了
           *   这条规矩)。参数原样来自 useTable,行为跟表格视图一致。
           *   `hidePagination`(混合列表)时由混合体画唯一那份。
           */}
          {!hidePagination ? (
            <div style={{ flexShrink: 0, paddingTop: 12, display: 'flex', justifyContent: 'flex-end' }}>
              <Pagination
                size="small"
                current={pagination.current}
                pageSize={pagination.pageSize}
                total={pagination.total}
                showSizeChanger={pagination.showSizeChanger}
                showQuickJumper={pagination.showQuickJumper}
                showTotal={pagination.showTotal}
                onChange={pagination.onChange}
              />
            </div>
          ) : null}
        </>
      )}
    </div>
  )
}

export default FileGrid
