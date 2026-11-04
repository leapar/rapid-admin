/**
 * 文件夹管理 —— 卡片视图(M144)
 *
 * 为什么要有这个视图:文件夹列表跟文件列表是同一类问题 —— 表格里只能看到
 * "名字 / 文件数 / 大小",**看不到这个文件夹长什么样**。而文件夹是**有封面的**
 * (M125 起「设为封面」),封面恰恰是最直观的识别方式:一屏扫过去就知道
 * "哪个是图纸、哪个是照片、哪个是空的"。
 *
 * ⚠️ **跟文件列表的缩略图视图是同一套外壳**(同款卡片、同款分页器、同款
 *   遮罩 loading)—— 两处各写一遍就是两套视觉,用户切来切去会觉得是两个页面。
 *
 * ⚠️ **操作与表格视图完全一致** —— 同一份 `actionsFor`,
 *   收进封面区右上角的「⋯」菜单(2026-11-01):鼠标移入展开,操作从上到下排开。
 *
 * ⚠️ **分页器要自己渲染** —— 表格那份分页器长在 `<Table>` 里,换成自定义
 *   列表体(ManagementTable 的 `body` 槽位)之后它就没了。错误态同理。
 */
import { useEffect, useState } from 'react'
import { Card, Empty, Pagination, Spin, Tag, Tooltip, theme } from 'antd'
import { FolderOpenOutlined, FolderOutlined } from '@ant-design/icons'
import { CardActionsMenu } from '@/components/common/CardActionsMenu'
import { isInteractiveTarget } from '@/components/common/rowClick'
import type { RowAction } from '@/components/common/RowActions'
import { TableErrorState } from '@/components/common/TableErrorState'
import { useI18n } from '@/locales'
import { formatNumber } from '@/utils/format'
import { parentPathOf } from './folderPath'
import type { FolderView } from '@/api/upload'

/** 分页器参数 —— 与 useTable 返的 pagination 同形 */
export interface FolderGridPagination {
  current: number
  pageSize: number
  total: number
  showSizeChanger: boolean
  showQuickJumper: boolean
  showTotal: (total: number) => string
  onChange: (page: number, pageSize: number) => void
}

export interface FolderGridProps {
  folders: FolderView[]
  loading: boolean
  /** 最近一次加载的错误(成功时为 null/undefined) */
  error?: unknown
  onRetry?: () => void
  pagination: FolderGridPagination
  /** 每行的操作按钮 —— **与表格视图共用同一份**(同一个权限口径) */
  actionsFor: (folder: FolderView) => RowAction<FolderView>[]
  /** 点文件夹名 = 进入这个文件夹 */
  onEnter: (folder: FolderView) => void
  /**
   * 隐藏自带分页器(2026-11-03 混合列表)。
   *
   * ⚠️ **混合列表(文件夹卡片 + 文件卡片)只有一份分页器** ——
   *   文件夹是服务端分页、文件是一次拉全,两份分页器叠着
   *   用户不知道哪个管谁。混合体把分页器画在最底下,
   *   这时两个网格都要把这个自带的藏掉。
   */
  hidePagination?: boolean
}

/**
 * 父级路径 —— **与表格视图同一个函数**(folderPath.ts),
 * 两处各切一次迟早会对不上。
 */
interface FolderCardProps {
  folder: FolderView
  actionsFor: (folder: FolderView) => RowAction<FolderView>[]
  onEnter: (folder: FolderView) => void
}

function FolderCard({ folder, actionsFor, onEnter }: FolderCardProps) {
  const { t } = useI18n()
  const { token } = theme.useToken()

  /**
   * 封面加载失败 ⇒ 换成文件夹图标。
   *
   * ⚠️ **必须处理** —— `<img>` 加载失败时浏览器画的是"碎图"图标,
   *   跟真的封面混在一起,用户会以为这个文件夹坏了。
   */
  const [coverBroken, setCoverBroken] = useState(false)

  /**
   * **封面地址变了就把"加载失败"的记号清掉**。
   *
   * ⚠️ 卡片是按 `folder.id` 做 key 的 ⇒ 同一条重新加载后**还是同一个组件
   *   实例**,`coverBroken` 会一直粘着 true ⇒ 用户给文件夹换了封面,
   *   刷新列表还是旧的占位图标。
   */
  useEffect(() => {
    setCoverBroken(false)
  }, [folder.cover_url])

  const hasCover = !!folder.cover_url && !coverBroken
  const parentPath = parentPathOf(folder.path)

  return (
    /**
     * ⚠️ `size="small"` ⇒ 内边距走 antd 的 `bodyPaddingSM`(=12),
     *   跟表格视图那些小控件的密度是一档的。
     *
     * ⚠️ **`hoverable` + 整卡可点**(2026-11-01 用户提的「文件夹管理列表
     *   要支持点击进入」)—— 卡片视图里"能点"这件事比表格更需要提示:
     *   表格有行悬停底色,卡片没有 ⇒ 只靠 cursor 用户看不出来。
     *   `hoverable` 给的是 antd 自带的悬停阴影(暗色主题也跟)。
     *
     * ⚠️ **行内的交互元素不接**(「⋯」菜单 / 名字链接 / 标签)——
     *   判定见 `components/common/rowClick.ts`。
     *   ⚠️ 「⋯」菜单是 **portal** 渲染的(DOM 上不在卡片里),但 React 事件
     *     沿组件树冒泡 ⇒ 不挡的话,点菜单里的「删除」会顺带把文件夹打开。
     */
    <Card
      size="small"
      data-testid="folder-grid-card"
      hoverable
      onClick={(e) => {
        if (isInteractiveTarget(e.target)) return
        onEnter(folder)
      }}
    >
      {/**
       * 封面区 —— **固定 4:3**(跟文件卡片同款),所以每张卡片这一段一样高。
       *
       * ⚠️ `position: relative` 是给压在上面的两个角标当锚点的
       *   (左上「默认」/ 右上「⋯」)。
       */}
      <div
        data-testid="folder-grid-thumb"
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
        {hasCover ? (
          <img
            src={folder.cover_url}
            alt={folder.name}
            loading="lazy"
            onError={() => setCoverBroken(true)}
            style={{ width: '100%', height: '100%', objectFit: 'cover' }}
          />
        ) : (
          /**
           * 没有封面(还没设过 / 封面文件被删了 / 加载失败)⇒ 摆文件夹图标。
           *
           * ⚠️ **有子文件夹的用开口图标** —— 跟表格视图一致(开口 = 里面还有东西)。
           */
          <Tooltip title={t('page.fileFolder.noCover')}>
            <span style={{ fontSize: 36, color: token.colorTextQuaternary, lineHeight: 1 }}>
              {folder.child_count > 0 ? <FolderOpenOutlined /> : <FolderOutlined />}
            </span>
          </Tooltip>
        )}

        {/**
         * 「默认」徽标 —— **绝对定位在左上角**,跟文件卡片的「封面」徽标同位
         * (2026-11-01 从右上角挪过来:右上角让给了「⋯」操作按钮)。
         *
         * ⚠️ **为什么绝对定位**:它原来在文字流里,于是"默认文件夹"比别的
         *   文件夹多出一行 ⇒ **卡片高度参差不齐**。移进封面区后不占文档流。
         */}
        {folder.is_default ? (
          <Tag
            color="blue"
            style={{ position: 'absolute', top: 0, left: 0, margin: 0 }}
            data-testid="folder-grid-default"
          >
            {t('field.isDefault')}
          </Tag>
        ) : null}

        {/**
         * 「⋯」操作菜单 —— **绝对定位在封面区右上角**(用户 2026-11-01 提的:
         * 原来五个操作排在卡片底部,把卡片撑得很宽还换行)。
         *
         * ⚠️ 动作来自**同一份** `actionsFor`,判定走 `visibleRowActions`
         *   (与表格视图同一个函数)⇒ 两种视图的操作永远一致。
         *
         * ⚠️ **贴边(0)**:Card 自身有内边距(antd `bodyPaddingSM` = 12),
         *   封面这块本来就已经缩进一圈了,角标再留 8px 就等于缩两圈。
         */}
        <div style={{ position: 'absolute', top: 0, right: 0 }}>
          <CardActionsMenu<FolderView> row={folder} actions={actionsFor(folder)} />
        </div>
      </div>

      {/* 文件夹名 —— 链接,点它就是"进入这个文件夹" */}
      <Tooltip title={folder.path}>
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
          <a onClick={() => onEnter(folder)}>{folder.name}</a>
        </div>
      </Tooltip>

      {/**
       * 所在目录 —— 同名文件夹可能有好几个("A/图纸"、"B/图纸"),光看名字分不清。
       * 根级文件夹没有父级路径,这时不显示这一行(免得挂一个空的 tag)。
       */}
      {parentPath ? (
        <div style={{ marginTop: 2 }}>
          <Tag color="default">{parentPath}</Tag>
        </div>
      ) : null}

      {/* 文件数 · 大小 —— 次级文字(三级文字色) */}
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
        {t('page.fileFolder.fileCountValue', undefined, { n: folder.file_count ?? 0 })}
        {' · '}
        {formatNumber.fileSize(folder.total_size)}
      </div>
      {/**
       * ⚠️ **操作不在这里了** —— 挪到封面区右上角的「⋯」菜单
       *   (见上面 `CardActionsMenu` 那一段)。原来几个操作排在这一行,
       *   把卡片撑到 280px 宽才放得下,窄屏还换行 ⇒ 卡片高度参差。
       */}
    </Card>
  )
}

export function FolderGrid({
  folders,
  loading,
  error,
  onRetry,
  pagination,
  actionsFor,
  onEnter,
  hidePagination,
}: FolderGridProps) {
  const { t } = useI18n()
  const showError = error !== null && error !== undefined
  const hasFolders = folders.length > 0

  return (
    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      {showError && !hasFolders ? (
        // 首屏就失败 → 降级占位(和表格视图的 emptyText 一致)
        <TableErrorState variant="empty" onRetry={onRetry} />
      ) : !hasFolders ? (
        <Empty description={t('common.noData')} style={{ marginTop: 48 }} />
      ) : (
        <>
          {/**
           * ⚠️ **加载遮罩自己画,不用 Spin 的嵌套模式** ——
           *   antd 5.29 的 `Spin` 把 `style` 落在**转圈那个元素**上,不是外层
           *   容器上,套不进 flex 布局。自己画一层 absolute 遮罩。
           */}
          <div style={{ flex: 1, minHeight: 0, position: 'relative', overflow: 'hidden' }}>
            <div style={{ height: '100%', overflowY: 'auto' }}>
              <div
                data-testid="folder-grid"
                style={{
                  display: 'grid',
                  // ⚠️ **200px 下限**(2026-11-01 用户定的):操作收进右上角的
                  //   「⋯」菜单之后,卡片里最宽的就是文件夹名一行 ⇒ 200px 够放。
                  //   以前是 280px,理由是"一排 5 个操作按钮约 264px 宽",
                  //   那个理由已经不成立了(按钮不再并排)。
                  gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
                  gap: 16,
                  alignContent: 'start',
                }}
              >
                {folders.map((f) => (
                  <FolderCard key={f.id} folder={f} actionsFor={actionsFor} onEnter={onEnter} />
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
           *   列表体之后它跟着没了。参数原样来自 useTable,行为跟表格视图一致。
           *   `hidePagination`(混合列表)时由混合体在最底下画唯一那份。
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

export default FolderGrid
